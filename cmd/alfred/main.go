package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"cpqd.com.br/alfred/internal/catalog"
	"cpqd.com.br/alfred/internal/extra"
	"cpqd.com.br/alfred/internal/publisher"
)

const max_upload_size = 10 * 1024 * 1024

const server_version = "Alfred 1.0"
const PUBLISH_ENDPOINT = "/v1/publish"
const ENUMERATE_ENDPOINT = "/v1/enumerate"
const WEB_ENDPOINT = "/"

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

type Part struct {
	Name        string
	ContentType string
	Data        []byte
}

func extract_parts(r *http.Request) (map[string]Part, error) {
	if r.Method != "POST" {
		return nil, fmt.Errorf("Unsupported method")
	}

	mtype, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !strings.HasPrefix(mtype, "multipart/") {
		return nil, fmt.Errorf("Expected multipart data")
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
	Version     string `json:"ver"`
	Format      string `json:"fmt"`
	Language    string `json:"lang"`
}

func publish_handler(w http.ResponseWriter, r *http.Request) {
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

	context, ok := environments[request.Environment]
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
		Product:      strings.ToLower(request.Product),
		Version:      version,
		ShortVersion: version.GetShortVersion(),
		Format:       catalog.FormatType(request.Format),
		Language:     catalog.Language(request.Language),
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
	summary, err := publisher.Publish(defaultProd, &pub, attachment)
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
var servers []*http.Server = make([]*http.Server, 0)
var killme = false

func install_signal_hook() {
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt)
	go func() {
		<-c
		if killme {
			os.Exit(1)
		}
		killme = true
		go func() {
			for _, server := range servers {
				server.Shutdown(nil)
			}
			server_done <- 1
		}()
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

	fmt.Printf("Alfred %s\n", ALFRED_VERSION)

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

	for _, env := range config.Environments {
		create_file_server(env)
	}

	for i, server := range servers {
		go func(env Environment, server *http.Server) {
			fmt.Printf("[%s] Listening at %s:%d\n", env.Name, env.Host, env.Port)
			err := server.ListenAndServe()
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				fmt.Printf("[%s] Error listening at %s:%d\n", env.Name, env.Host, env.Port)
			}
		}(config.Environments[i], server)
	}

	// start API
	address := fmt.Sprintf("%s:%d", config.Manager.Host, config.Manager.Port)
	mux := http.NewServeMux()
	mux.HandleFunc(PUBLISH_ENDPOINT, publish_handler)
	//mux.HandleFunc(ENUMERATE_ENDPOINT, enumerate_handler)
	//mux.Handle(WEB_ENDPOINT, http.FileServer(http.Dir("cmd/alfred/web")))
	server := &http.Server{Addr: address, Handler: mux}
	go server.ListenAndServe()
	fmt.Printf("[API] Listening at http://%s\n", address)
	servers = append(servers, server)

	select {
	case <-server_done:
	}

	for _, context := range environments {
		context.Save()
	}
}

func http_error(code int, msg string, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, msg)
}

var file_re, _ = regexp.Compile("/\\..|catalog.json$")

type FilteredServer struct {
	Root   string
	server http.Handler
}

func NewFilteredServer(root string) FilteredServer {
	if strings.HasSuffix(root, "/") {
		root = root[:1]
	}
	return FilteredServer{
		Root: root, server: http.FileServer(http.Dir(root)),
	}
}

func (h FilteredServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const denied = "[D] Requested '%s'\n"

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		fmt.Printf(denied, r.URL.Path)
		http_error(405, "Method Not Allowed", w)
		return
	}

	// block requests trying to escape root
	fpath := path.Join(h.Root, "/", r.URL.Path)
	//fmt.Println(h.Root, "/", r.URL.Path, fpath)
	if !strings.HasPrefix(fpath, h.Root) {
		fmt.Printf(denied, r.URL.Path)
		http_error(404, "Not Found", w)
		return
	}
	r.URL.Path = strings.TrimPrefix(fpath, h.Root)
	if len(r.URL.Path) == 0 {
		r.URL.Path = "/"
	}

	// block requests using regex filter
	if file_re.Match([]byte(r.URL.Path)) {
		fmt.Printf(denied, r.URL.Path)
		http_error(403, "Forbidden", w)
		return
	}

	info, err := os.Stat(fpath)
	if err != nil {
		fmt.Printf(denied, r.URL.Path)
		http_error(404, "Not Found", w)
		return
	}
	if info.IsDir() { // TODO: check for 'index.html'
		fmt.Printf(denied, r.URL.Path)
		http_error(403, "Forbidden", w)
		return
	}

	fmt.Printf("[A] Requested '%s'\n", r.URL.Path)
	h.server.ServeHTTP(w, r)
}

func create_file_server(env Environment) {
	address := fmt.Sprintf("%s:%d", env.Host, env.Port)
	//mux := http.NewServeMux()
	//mux.Handle("/", NewFilteredServer(env.Path))
	//server := &http.Server{Addr: address, Handler: mux}
	server := &http.Server{Addr: address, Handler: NewFilteredServer(env.Path)}
	servers = append(servers, server)
}
