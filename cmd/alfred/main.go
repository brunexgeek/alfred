package main

import (
	"bytes"
	"embed"
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

	"log"

	"cpqd.com.br/alfred/internal/catalog"
	"cpqd.com.br/alfred/internal/extra"
	ahttp "cpqd.com.br/alfred/internal/http"
	"cpqd.com.br/alfred/internal/publisher"
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

	env, ok := environments[request.Environment]
	if len(request.Environment) == 0 || !ok {
		send_error(400, "Unkown environment", w)
		return
	}
	context := env.Publisher

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
	summary, err := publisher.Publish(env.Environment.Path, &pub, attachment)
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
	env, ok := environments[cname]
	if !ok {
		send_error(400, "Unkown environment", w)
		return
	}
	context := env.Publisher

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

type EnvironmentInfo struct {
	Publisher   *publisher.Publisher
	Environment Environment
}

var environments = make(map[string]*EnvironmentInfo)

func main() {
	install_signal_hook()
	initialize_globals()

	fmt.Printf("Alfred %s\n", ALFRED_VERSION)

	config, err := load_configuration()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	for _, entry := range config.Environments {
		fmt.Println(path.Join(entry.Path, "catalog.json"))
		context, err := publisher.NewPublisher(path.Join(entry.Path, "catalog.json"))
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Printf("Catalog with %d entries\n", len(context.Catalog.Products))
		env := &EnvironmentInfo{Publisher: context, Environment: entry}
		environments[entry.Name] = env
		fmt.Printf("Initialized environment '%s' at '%s'\n", entry.Name, entry.Path)

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
	mux.HandleFunc(ENVIRONMENTS_ENDPOINT, environment_handler)
	mux.Handle(WEB_ENDPOINT, ahttp.FileServer(http.FS(resources)))
	server := &http.Server{Addr: address, Handler: mux}
	go server.ListenAndServe()
	fmt.Printf("[API] Listening at http://%s\n", address)
	servers = append(servers, server)

	select {
	case <-server_done:
	}

	for _, env := range environments {
		env.Publisher.Save()
	}
}

func http_error(code int, msg string, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(code)
	fmt.Fprintln(w, msg)
}

type GlobalValues struct {
	block_regex *regexp.Regexp
	product_url *regexp.Regexp
	format_url  *regexp.Regexp
	version_url *regexp.Regexp
	full_url    *regexp.Regexp
	proxies     []string
}

var globals = GlobalValues{proxies: make([]string, 0)}

func initialize_globals() error {
	var err error
	globals.block_regex, err = regexp.Compile("/\\..|catalog.json$")
	if err != nil {
		return err
	}
	globals.product_url, err = regexp.Compile(fmt.Sprintf("^/%s/?$", catalog.RE_PRODUCT_NAME))
	if err != nil {
		return err
	}
	globals.format_url, err = regexp.Compile(fmt.Sprintf("^/%s/%s/?$", catalog.RE_PRODUCT_NAME, catalog.RE_FORMAT))
	if err != nil {
		return err
	}
	globals.version_url, err = regexp.Compile(fmt.Sprintf("^/%s/%s/%s/?$", catalog.RE_PRODUCT_NAME, catalog.RE_FORMAT, catalog.RE_URL_VERSION))
	if err != nil {
		return err
	}
	globals.full_url, err = regexp.Compile(fmt.Sprintf("^/%s/%s/%s/%s/(.*)$", catalog.RE_PRODUCT_NAME, catalog.RE_FORMAT, catalog.RE_URL_VERSION, catalog.RE_LANGUAGE))
	if err != nil {
		return err
	}
	return nil
}

type FilteredServer struct {
	Root      string
	server    http.Handler
	Publisher *publisher.Publisher
}

func NewFilteredServer(root string, pub *publisher.Publisher) FilteredServer {
	if strings.HasSuffix(root, "/") {
		root = root[:1]
	}
	return FilteredServer{
		Root:      root,
		server:    ahttp.FileServer(http.Dir(root)),
		Publisher: pub,
	}
}

func (h FilteredServer) log_error(r *http.Request, status int, msg string) {
	const mask = "[%s] %d '%s' %s\n"
	log.Printf(mask, ahttp.GetRealAddress(r, globals.proxies), status, r.URL.Path, msg)
}

func (h FilteredServer) redirect_to(w http.ResponseWriter, url string) {
	w.Header().Add("Content-Length", "0")
	w.Header().Add("Location", url)
	w.WriteHeader(302)
}

func (h FilteredServer) handle_product(w http.ResponseWriter, r *http.Request) {
	matches := globals.product_url.FindStringSubmatch(r.URL.Path)
	if matches == nil {
		h.log_error(r, 404, "Product not found")
		http_error(404, "Not found", w)
		return
	}

	if product, ok := h.Publisher.Catalog.Products[matches[1]]; ok && len(product.Publications) > 0 {
		var choice *catalog.Publication
		for _, pub := range product.Publications {
			if pub.Version == product.Latest {
				if pub.Language == catalog.PT {
					choice = pub
				} else if choice == nil {
					choice = pub
				}
			}
		}

		if choice == nil {
			h.log_error(r, 404, "Latest version do not exists")
			http_error(404, "Not found", w)
			return
		}

		// look for the latest HTML publication

		new_url := fmt.Sprintf("/%s/%s/%s/%s/%s",
			matches[1],
			catalog.HTML,
			choice.ShortVersion.ToString(),
			choice.Language,
			matches[5])
		h.redirect_to(w, new_url)
	} else {
		h.log_error(r, 404, "Product not found")
		http_error(404, "Not found", w)
	}
}

func (h FilteredServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const mask = "[%s] %d '%s'\n"

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		fmt.Printf(mask, ahttp.GetRealAddress(r, globals.proxies), 405, r.URL.Path)
		http_error(405, "Method Not Allowed", w)
		return
	}

	// block requests using regex filter
	if globals.block_regex.Match([]byte(r.URL.Path)) {
		h.log_error(r, 403, "Not allowed")
		http_error(403, "Forbidden", w)
		return
	}

	// check for dynamic generated content
	matches := globals.full_url.FindStringSubmatch(r.URL.Path)
	if matches == nil {
		//fmt.Printf(mask, ahttp.GetRealAddress(r, globals.proxies), 404, r.URL.Path)
		//http_error(404, "Invalid", w)
		h.handle_product(w, r)
		return
	}

	if matches[3] == "latest" {
		if product, ok := h.Publisher.Catalog.Products[matches[1]]; ok {
			new_url := fmt.Sprintf("/%s/%s/%s/%s%s",
				matches[1],
				matches[2],
				product.Latest.GetShortVersion().ToString(),
				matches[4],
				strings.TrimPrefix(r.URL.Path, matches[0]))
			w.Header().Add("Content-Length", "0")
			w.Header().Add("Location", new_url)
			w.WriteHeader(302)
			return
		} else {
			fmt.Printf(mask, ahttp.GetRealAddress(r, globals.proxies), 404, r.URL.Path)
			http_error(404, "Product Not Found", w)
			return
		}
	}

	fmt.Printf(mask, ahttp.GetRealAddress(r, globals.proxies), 200, r.URL.Path)
	h.server.ServeHTTP(w, r)
}

func create_file_server(env *EnvironmentInfo) {
	address := fmt.Sprintf("%s:%d", env.Environment.Host, env.Environment.Port)
	//mux := http.NewServeMux()
	//mux.Handle("/", NewFilteredServer(env.Path))
	//server := &http.Server{Addr: address, Handler: mux}
	server := &http.Server{Addr: address, Handler: NewFilteredServer(env.Environment.Path, env.Publisher)}
	servers = append(servers, server)
}
