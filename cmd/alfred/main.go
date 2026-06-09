package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
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
const WEB_ENDPOINT = "/web"

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

func send_empty(status int, w http.ResponseWriter) {
	w.Header().Set("Server", server_version)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
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

type ResourceReference struct {
	catalog.Publication
	Environment string
}

func parse_resource_ref(r *http.Request) (*ResourceReference, error) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 5 {
		return nil, fmt.Errorf("Invalid resource path")
	}
	version, err := catalog.ParseVersion(parts[3])
	if err != nil {
		return nil, fmt.Errorf("Invalid version")
	}
	if !catalog.IsValidName(parts[0]) {
		return nil, fmt.Errorf("Invalid environment name")
	}

	result := ResourceReference{
		Publication: catalog.Publication{
			Product:  parts[1],
			Language: catalog.LanguageCode(parts[2]),
			Version:  version,
			Format:   catalog.FormatCode(parts[4]),
			Date:     time.Now(),
		},
		Environment: parts[0],
	}

	if err := result.Validate(); err != nil {
		return nil, err
	}

	return &result, nil
}

func publish_handler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	resource, err := parse_resource_ref(r)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	if ctype, ok := r.Header["Content-Type"]; !ok || strings.TrimSpace(strings.Join(ctype, "")) != "application/gzip" {
		send_error(400, "Invalid content type", w)
		return
	}
	attachment := r.Body

	env, ok := environments[resource.Environment]
	if !ok {
		send_error(404, "Unkown environment", w)
		return
	}

	// publish the resource
	summary, err := publisher.Publish(env.Parameters.Path, &resource.Publication, attachment)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	// update catalog
	env.AddPublication(&resource.Publication)
	// update index pages if an update is not in progress
	if atomic.CompareAndSwapInt32(&indexPending, 0, 1) {
		log.Debugf("Trying to update index")
		err = env.UpdateWebIndices()
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
	result.URL = fmt.Sprintf("%s/", resource.DataPath())

	log.Infof("Published %s", resource.DataPath())

	send_object(http.StatusOK, result, w)
}

func remove_handler(w http.ResponseWriter, r *http.Request) {
	resource, err := parse_resource_ref(r)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	send_empty(200, w)
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

func dispatcher(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		publish_handler(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		remove_handler(w, r)
		return
	}
	send_error(404, "Not found", w)
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

	// start API
	address := fmt.Sprintf("%s:%d", config.Manager.Host, config.Manager.Port)
	mux := http.NewServeMux()
	mux.Handle(WEB_ENDPOINT, http.FileServer(http.FS(resources)))
	mux.HandleFunc("/", dispatcher)
	server := &http.Server{Addr: address, Handler: mux}
	go server.ListenAndServe()
	log.Infof("Manager API listening at http://%s\n", address)
	servers = append(servers, server)

	select {
	case <-server_done:
	}
}
