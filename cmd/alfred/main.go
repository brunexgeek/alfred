package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
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

const UPLOAD_MIME_TYPE = "application/gzip"
const REMOTE_USER = "X-Remote-User"

//go:embed web/index.html
//go:embed web/bootstrap.min.css
var resources embed.FS

// TODO make it configurable
const max_upload_size = 15 * 1024 * 1024

const server_version = "Alfred 1.0"
const PUBLISH_ENDPOINT = "/v1/publish"
const ENUMERATE_ENDPOINT = "/v1/enumerate"
const ENVIRONMENTS_ENDPOINT = "/v1/environments"
const WEB_ENDPOINT = "/web"

var indexPending int32 = 0

type ErrorInfo struct {
	Message string `json:"message"`
}

type EnvironmentEntry struct {
	Environment *catalog.Environment
	Permissions Permissions
	Users       map[string]Permissions
}

var environments = make(map[string]*EnvironmentEntry)

func (env *EnvironmentEntry) CheckPermission(user string, method string) error {
	// ignore users whose name are invalid/unsafe
	if len(user) > 0 && !catalog.IsValidName(user) {
		return fmt.Errorf("invalid user name")
	}

	source := &env.Permissions
	if len(user) > 0 {
		perms, ok := env.Users[user]
		if !ok {
			source = &perms
		}
	}

	result := false
	switch method {
	case http.MethodPut:
		result = source.Put

	case http.MethodDelete:
		result = source.Delete

	case http.MethodGet:
		result = source.Get
	}
	if !result {
		return fmt.Errorf("forbidden")
	}
	return nil
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
	w.Header().Add("Content-Length", "0")
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

func parse_resource_ref(path string) ([]string, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	length := len(parts)
	if length == 0 {
		return nil, fmt.Errorf("missing environment name")
	}
	if length >= 1 && !catalog.IsValidName(parts[0]) {
		return nil, fmt.Errorf("invalid environment name")
	}
	if length >= 2 && !catalog.IsValidName(parts[1]) {
		return nil, fmt.Errorf("invalid product name")
	}
	if length >= 3 && !catalog.IsValidLanguage(parts[2]) {
		return nil, fmt.Errorf("invalid language")
	}
	if length >= 4 && !catalog.IsValidVersion(parts[3]) {
		return nil, fmt.Errorf("invalid version")
	}
	if length >= 5 && !catalog.IsValidFormat(parts[4]) {
		return nil, fmt.Errorf("invalid format")
	}
	if length > 5 {
		return nil, fmt.Errorf("invalid resource name")
	}

	return parts, nil
}

func updateIndices(env *catalog.Environment) {
	log := extra.GetDefaultLog()

	if atomic.CompareAndSwapInt32(&indexPending, 0, 1) {
		log.Debugf("Trying to update index")
		err := env.UpdateWebIndices()
		if err != nil {
			log.Error(err)
		}
		atomic.StoreInt32(&indexPending, 0)
	}
}

func publish_handler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	// parse and validate the resource path
	resource, err := parse_resource_ref(r.URL.Path)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}
	if len(resource) != 5 {
		send_error(400, "incomplete resource name", w)
		return
	}

	if strings.ToLower(r.Header.Get("Content-Type")) != UPLOAD_MIME_TYPE {
		send_error(400, fmt.Sprintf("invalid content type; expected '%s'", UPLOAD_MIME_TYPE), w)
		return
	}
	content := r.Body

	version, _ := catalog.ParseVersion(resource[3])
	pub := catalog.Publication{
		Product:  resource[1],
		Language: catalog.LanguageCode(resource[2]),
		Version:  version,
		Format:   catalog.FormatCode(resource[4]),
		Date:     time.Now(),
	}

	env, ok := environments[resource[0]]
	if !ok {
		send_error(404, "environment not found", w)
		return
	}
	if err := env.CheckPermission(r.Header.Get(REMOTE_USER), http.MethodPut); err != nil {
		send_error(403, err.Error(), w)
		return
	}

	// publish the resource
	summary, err := publisher.Publish(env.Environment.Parameters.Path, &pub, content)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	// update catalog
	env.Environment.AddPublication(&pub)
	// update index pages if an update is not in progress
	updateIndices(env.Environment)

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

func remove_handler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	resource, err := parse_resource_ref(r.URL.Path)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	env, ok := environments[resource[0]]
	if !ok {
		send_error(404, "environment not found", w)
		return
	}
	if err := env.CheckPermission(r.Header.Get(REMOTE_USER), http.MethodDelete); err != nil {
		send_error(403, err.Error(), w)
		return
	}

	entry, err := env.Environment.DeletePublication(resource)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}
	log.Infof("Removed publication '%s'", entry.Path)
	// update index pages if an update is not in progress
	updateIndices(env.Environment)

	send_empty(200, w)
}

func metadata_handler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	resource, err := parse_resource_ref(r.URL.Path)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}

	env, ok := environments[resource[0]]
	if !ok {
		send_error(404, "environment not found", w)
		return
	}
	if err := env.CheckPermission(r.Header.Get(REMOTE_USER), http.MethodGet); err != nil {
		send_error(403, err.Error(), w)
		return
	}

	content, err := env.Environment.GetPublication(resource)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}
	log.Infof("Retrieved publication '%s'", r.URL.Path)

	w.Header().Set("Server", server_version)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(content)
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
	if r.Method == http.MethodGet {
		metadata_handler(w, r)
		return
	}
	send_error(405, "method not allowed", w)
}

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
		environment := catalog.NewEnvironment(&entry.Parameters)
		if err != nil {
			log.Error(err)
			os.Exit(1)
		}
		environment.ScanEnvironment(entry.Path)
		err = environment.UpdateWebIndices()
		if err != nil {
			log.Error(err)
			os.Exit(1)
		}

		environments[entry.Name] = &EnvironmentEntry{Environment: environment, Permissions: entry.Permissions}
		log.Infof("Initialized environment '%s' at '%s'\n", entry.Name, entry.Path)
	}

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
