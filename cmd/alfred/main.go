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

const server_name = "Alfred"
const WEB_ENDPOINT = "/web"

// used to control the critical region of updating indices
var indexPending int32 = 0

var environments = make(map[string]*EnvironmentEntry)

type EnvironmentEntry struct {
	Environment *catalog.Environment
	Permissions Permissions
	Users       map[string]Permissions
}

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

func writeJson(obj any, w http.ResponseWriter) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	w.Write(data)
	return nil
}

func sendError(status int, message string, w http.ResponseWriter) {
	type ErrorInfo struct {
		Message string `json:"message"`
	}

	data, err := json.Marshal(ErrorInfo{Message: message})
	if err != nil {
		data = make([]byte, 0)
	}
	w.Header().Set("Server", server_name)
	http.Error(w, string(data), status)

	extra.GetDefaultLog().Errorf("HTTP %d - %s", status, message)
}

func parseResource(path string) ([]string, error) {
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

func publishHandler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	// parse and validate the resource path
	resource, err := parseResource(r.URL.Path)
	if err != nil {
		sendError(400, err.Error(), w)
		return
	}
	if len(resource) != 5 {
		sendError(400, "incomplete resource name", w)
		return
	}

	if strings.ToLower(r.Header.Get("Content-Type")) != UPLOAD_MIME_TYPE {
		sendError(400, fmt.Sprintf("invalid content type; expected '%s'", UPLOAD_MIME_TYPE), w)
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
		sendError(404, "environment not found", w)
		return
	}
	if err := env.CheckPermission(r.Header.Get(REMOTE_USER), http.MethodPut); err != nil {
		sendError(403, err.Error(), w)
		return
	}

	// publish the resource
	summary, err := publisher.Publish(env.Environment.Parameters.Path, &pub, content)
	if err != nil {
		sendError(400, err.Error(), w)
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

	w.Header().Set("Server", server_name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	writeJson(result, w)
}

func removeHandler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	resource, err := parseResource(r.URL.Path)
	if err != nil {
		sendError(400, err.Error(), w)
		return
	}

	env, ok := environments[resource[0]]
	if !ok {
		sendError(404, "environment not found", w)
		return
	}
	if err := env.CheckPermission(r.Header.Get(REMOTE_USER), http.MethodDelete); err != nil {
		sendError(403, err.Error(), w)
		return
	}

	entry, err := env.Environment.DeletePublication(resource)
	if err != nil {
		sendError(400, err.Error(), w)
		return
	}
	log.Infof("Removed publication '%s'", entry.Path)
	// update index pages if an update is not in progress
	updateIndices(env.Environment)

	w.Header().Set("Server", server_name)
	w.Header().Add("Content-Length", "0")
	w.WriteHeader(200)
}

func metadataHandler(w http.ResponseWriter, r *http.Request) {
	log := extra.GetDefaultLog()

	resource, err := parseResource(r.URL.Path)
	if err != nil {
		sendError(400, err.Error(), w)
		return
	}

	env, ok := environments[resource[0]]
	if !ok {
		sendError(404, "environment not found", w)
		return
	}
	if err := env.CheckPermission(r.Header.Get(REMOTE_USER), http.MethodGet); err != nil {
		sendError(403, err.Error(), w)
		return
	}

	content, err := env.Environment.GetPublication(resource)
	if err != nil {
		sendError(400, err.Error(), w)
		return
	}
	log.Infof("Retrieved publication '%s'", r.URL.Path)

	w.Header().Set("Server", server_name)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(content)
}

var server_done = make(chan int)
var servers []*http.Server = make([]*http.Server, 0)
var killme = false

func installSignalHook() {
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

func loadConfiguration(cpath string) (*Config, error) {
	extra.GetDefaultLog().Infof("Loading configuration from '%s'\n", cpath)
	return OpenConfiguration(cpath)
}

func dispatcher(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		publishHandler(w, r)
		return
	}
	if r.Method == http.MethodDelete {
		removeHandler(w, r)
		return
	}
	if r.Method == http.MethodGet {
		metadataHandler(w, r)
		return
	}
	sendError(405, "method not allowed", w)
}

func main() {
	installSignalHook()
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

	config, err := loadConfiguration(cpath)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
	extra.SetDefaultLevel(extra.ParseLevel(config.LogLevel))
	log = extra.NewLogger()

	for _, entry := range config.Environments {
		environment, err := catalog.NewEnvironment(entry.Parameters)
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
