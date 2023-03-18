package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cpqd.com.br/alfred/internal/catalog"
	"cpqd.com.br/alfred/internal/publisher"
)

const max_upload_size = 10 * 1024 * 1024

const server_version = "Alfred 1.0"
const PUBLISH_ENDPOINT = "/v1/publish/"
const ENUMERATE_ENDPOINT = "/v1/enumerate/"

var busy sync.Mutex

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
}

func extract_context(path string, endpoint string) string {
	if !strings.HasSuffix(endpoint, "/") {
		endpoint = endpoint + "/"
	}
	if !strings.HasPrefix(path, endpoint) {
		return ""
	}
	return path[len(endpoint):]
}

func publish_handler(w http.ResponseWriter, r *http.Request) {
	cname := extract_context(r.URL.Path, PUBLISH_ENDPOINT)
	context, ok := environments[cname]
	if len(cname) == 0 || !ok {
		send_error(400, "Unkown environment", w)
		return
	}

	if r.Method != "POST" {
		send_error(400, "Unsupported method", w)
		return
	}

	if r.Header.Get("Content-Type") != "application/octet-stream" {
		send_error(400, "Unsupported content type", w)
		return
	}

	size, err := strconv.Atoi(r.Header.Get("Content-Length"))
	if err != nil || size < 0 || size > max_upload_size {
		send_error(400, "Payload size out of range", w)
		return
	}

	version, err := catalog.ParseVersion(r.URL.Query().Get("version"))
	if err != nil {
		send_error(400, "Invalid semantic version", w)
		return
	}

	pub := catalog.Publication{
		Product:      r.URL.Query().Get("product"),
		Version:      version,
		ShortVersion: version.GetShortVersion(),
		Format:       catalog.FormatType(r.URL.Query().Get("type")),
		Language:     catalog.Language(r.URL.Query().Get("lang")),
		Date:         time.Now(),
	}

	if err := pub.Validate(); err != nil {
		send_error(400, err.Error(), w)
		return
	}

	// start of critical region
	busy.Lock()
	defer busy.Unlock()

	// publish the resource
	summary, err := publisher.Publish(defaultProd, &pub, r.Body)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}
	// update catalog
	context.Catalog.AddPublication(&pub)
	context.Save()

	send_object(http.StatusOK, summary, w)
}

func enumerate_handler(w http.ResponseWriter, r *http.Request) {
	cname := strings.TrimPrefix(r.URL.Path, ENUMERATE_ENDPOINT)
	context, ok := environments[cname]
	if !ok {
		send_error(400, "Unkown environment", w)
		return
	}

	type Result struct {
		Name   string `json:"name"`
		Latest string `json:"ver"`
	}
	entries := make(map[string]*Result, 0)

	for _, entry := range context.Catalog.Products {
		entries[entry.Name] = &Result{Name: entry.Name, Latest: entry.Latest.ToString()}
	}

	send_object(200, entries, w)
}

func enumerate_product_handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
}

var defaultProd = "/tmp/alfred/prod" // default path for production
var defaultTest = "/tmp/alfred/test" // default path for testing
var server_done = make(chan int)
var server *http.Server

func install_signal_hook() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		server.Shutdown(nil)
		server_done <- 1
	}()
}

func load_configuration() (*Config, error) {
	tmp, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return nil, err
	}
	cpath := path.Join(tmp, "config.json")
	fmt.Printf("Loading configuration from '%s'\n", cpath)
	return OpenConfiguration(cpath)
}

var environments = make(map[string]*publisher.Publisher)

func main() {
	install_signal_hook()

	config, err := load_configuration()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, entry := range config.Environments {
		context, err := publisher.NewPublisher(path.Join(entry.Path, "catalog.json"))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		environments[entry.Name] = context
		fmt.Printf("Initialized environment '%s' at '%s'\n", entry.Name, entry.Path)
	}

	mux := http.NewServeMux()
	mux.HandleFunc(PUBLISH_ENDPOINT, publish_handler)
	mux.HandleFunc(ENUMERATE_ENDPOINT, enumerate_handler)
	server = &http.Server{Addr: fmt.Sprintf("%s:%d", config.Manager.Host, config.Manager.Port), Handler: mux}
	server.ListenAndServe()
	select {
	case <-server_done:
	}

	for _, context := range environments {
		context.Save()
	}
}
