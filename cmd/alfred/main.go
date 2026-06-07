package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"brunexgeek/alfred/internal/catalog"
	"brunexgeek/alfred/internal/extra"
	"brunexgeek/alfred/internal/publisher"
)

//go:embed web/index.html
//go:embed web/bootstrap.min.css
var resources embed.FS

const max_upload_size = 10 * 1024 * 1024

const server_version = "Alfred 1.0"
const PUBLISH_ENDPOINT = "/v1/publish"
const ENUMERATE_ENDPOINT = "/v1/enumerate"
const ENVIRONMENTS_ENDPOINT = "/v1/environments"
const WEB_ENDPOINT = "/"

var indexPending int32 = 0

type ErrorInfo struct {
	Message string `json:"message"`
}

func write_json(obj any, w http.ResponseWriter) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	w.Write(data)
	return nil
}

func send_object(status int, obj any, w http.ResponseWriter) error {
	w.Header().Set("Server", server_version)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return write_json(obj, w)
}

func send_error(status int, message string, w http.ResponseWriter) {
	data, err := json.Marshal(ErrorInfo{Message: message})
	if err != nil {
		data = make([]byte, 0)
	}
	w.Header().Set("Server", server_version)
	http.Error(w, string(data), status)

	extra.GetDefaultLog().Errorf("HTTP %d - %s", status, message)
}

type Part struct {
	Name        string
	ContentType string
	Data        []byte
}

func extract_parts(r *http.Request) (map[string]Part, error) {
	if r.Method != "POST" {
		return nil, fmt.Errorf("unsupported method")
	}

	mtype, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mtype, "multipart/") {
		return nil, fmt.Errorf("expected multipart data")
	}

	parts := make(map[string]Part, 0)
	reader := multipart.NewReader(r.Body, params["boundary"])
	for {
		p, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data, err := extra.ReadAll(p, max_upload_size)
		if err != nil {
			return nil, err
		}
		parts[p.FormName()] = Part{
			Name:        p.FormName(),
			ContentType: p.Header.Get("Content-Type"),
			Data:        data}
	}
	return parts, nil
}

type PublishRequest struct {
	Environment string `json:"env"`
	Product     string `json:"prod"`
	Title       string `json:"title"`
	Version     string `json:"ver"`
	Format      string `json:"fmt"`
	Language    string `json:"lang"`
}

func publish_handler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()
	parts, err := extract_parts(r)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	var request PublishRequest
	if entry, ok := parts["params"]; ok {
		err := json.Unmarshal(entry.Data, &request)
		if err != nil {
			send_error(400, "Invalid JSON object at multipart entry named 'params'", w)
			return
		}
	} else {
		send_error(400, "Missing multipart entry named 'params'", w)
		return
	}

	var attachment io.Reader
	if entry, ok := parts["attachment"]; ok {
		attachment = bytes.NewReader(entry.Data)
	} else {
		send_error(400, "Missing multipart entry named 'params'", w)
		return
	}

	cat, ok := environments[request.Environment]
	if len(request.Environment) == 0 || !ok {
		send_error(400, "Unkown environment", w)
		return
	}

	version, err := catalog.ParseVersion(request.Version)
	if err != nil {
		send_error(400, "Invalid semantic version", w)
		return
	}

	pub := catalog.Publication{
		Product:  strings.ToLower(request.Product),
		Version:  version,
		Format:   catalog.FormatCode(request.Format),
		Language: catalog.LanguageCode(request.Language),
		Date:     time.Now(),
	}

	if err := pub.Validate(); err != nil {
		send_error(400, err.Error(), w)
		return
	}

	// publish the resource
	summary, err := publisher.Publish(cat.Parameters.Path, &pub, attachment)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	// update catalog
	cat.AddPublication(&pub)
	// update index pages if an update is not in progress
	if atomic.CompareAndSwapInt32(&indexPending, 0, 1) {
		log.Debugf("Trying to update index")
		err = cat.UpdateWebIndices()
		if err != nil {
			log.Error(err)
		}
		atomic.StoreInt32(&indexPending, 0)
	}

	var result struct {
		publisher.Summary
		URL string
	}
	result.Count = summary.Count
	result.Size = summary.Size
	result.URL = fmt.Sprintf("%s/", pub.DataPath())

	log.Infof("Published %s", pub.DataPath())

	send_object(http.StatusOK, result, w)
}

func enumerate_handler(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	cname := params.Get("env")
	env, ok := environments[cname]
	if !ok {
		send_error(400, "Unkown environment", w)
		return
	}

	send_object(200, env, w)
}

func environment_handler(w http.ResponseWriter, r *http.Request) {
	type Result struct {
		Envs []string `json:"envs"`
	}
	result := Result{}

	for key := range environments {
		result.Envs = append(result.Envs, key)
	}

	send_object(200, result, w)
}

var server_done = make(chan int)
var servers []*http.Server = make([]*http.Server, 0)
var killme = false

func install_signal_hook() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		if killme {
			os.Exit(1)
		}
		killme = true
		go func() {
			for _, server := range servers {
				server.Shutdown(context.TODO())
			}
			server_done <- 1
		}()
	}()
}

func default_config() (string, error) {
	tmp, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return "", err
	}
	cpath := path.Join(tmp, "config.json")
	return cpath, nil
}

func load_configuration(cpath string) (*Config, error) {
	extra.GetDefaultLog().Infof("Loading configuration from '%s'\n", cpath)
	return OpenConfiguration(cpath)
}

var environments = make(map[string]*catalog.Environment)

func main() {
	install_signal_hook()
	log := extra.GetDefaultLog()

	log.Infof("Alfred %s\n", ALFRED_VERSION)

	cpath := ""
	if len(os.Args) == 1 {
		var err error
		cpath, err = default_config()
		if err != nil {
			log.Error(err)
			os.Exit(1)
		}
	} else {
		cpath = os.Args[1]
	}

	config, err := load_configuration(cpath)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	extra.SetDefaultLevel(extra.ParseLevel(config.LogLevel))
	log = extra.NewLogger()

	for _, entry := range config.Environments {
		environment := catalog.NewEnvironment(entry)
		if err != nil {
			log.Error(err)
			os.Exit(1)
		}
		environment.ScanEnvironment(entry.Path)
		log.Infof("Found environment '%s' at '%s' with %d products\n",
			environment.Parameters.Name,
			environment.Parameters.Path,
			len(environment.Root.Children))
		err = environment.UpdateWebIndices()
		if err != nil {
			log.Error(err)
			os.Exit(1)
		}

		environments[entry.Name] = environment
		log.Infof("Initialized environment '%s' at '%s'\n", entry.Name, entry.Path)
	}

	stripped, _ := fs.Sub(resources, "web")

	// start API
	address := fmt.Sprintf("%s:%d", config.Manager.Host, config.Manager.Port)
	mux := http.NewServeMux()
	mux.HandleFunc(PUBLISH_ENDPOINT, publish_handler)
	mux.HandleFunc(ENUMERATE_ENDPOINT, enumerate_handler)
	mux.HandleFunc(ENVIRONMENTS_ENDPOINT, environment_handler)
	mux.Handle(WEB_ENDPOINT, http.FileServer(http.FS(stripped)))
	server := &http.Server{Addr: address, Handler: mux}
	go server.ListenAndServe()
	log.Infof("Manager API listening at http://%s\n", address)
	servers = append(servers, server)

	select {
	case <-server_done:
	}
}
