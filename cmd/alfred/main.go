package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"

	"cpqd.com.br/alfred/internal/catalog"
	"cpqd.com.br/alfred/internal/publisher"
)

const max_upload_size = 10 * 1024 * 1024

type ErrorInfo struct {
	Message string `json:"message"`
}

func send_error(status int, message string, w http.ResponseWriter) {
	data, err := json.Marshal(ErrorInfo{Message: message})
	if err != nil {
		data = make([]byte, 0)
	}
	http.Error(w, string(data), status)
}

func publish_handler(w http.ResponseWriter, r *http.Request) {
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

	info := publisher.PublishParams{
		Name:     r.URL.Query().Get("product"),
		Version:  catalog.Version(r.URL.Query().Get("version")),
		Language: r.URL.Query().Get("lang"),
		Type:     catalog.VariantType(r.URL.Query().Get("type")),
	}
	if !info.IsValid() {
		send_error(400, "Invalid arguments", w)
		return
	}
	fmt.Println(info)
	err = publisher.Publish(defaultProd, info, r.Body)
	if err != nil {
		send_error(400, err.Error(), w)
		return
	}
	/*outFile, _ := os.Create("/tmp/ppp")
	io.Copy(outFile, r.Body)
	outFile.Close()*/

}

func progress_handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
}

func enumerate_handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hi there, I love %s!", r.URL.Path[1:])
}

var defaultProd = "/tmp/alfred/prod" // default path for production
var defaultTest = "/tmp/alfred/test" // default path for testing

func main() {
	server := http.NewServeMux()
	server.HandleFunc("/v1/publish", publish_handler)
	server.HandleFunc("/v1/progress", progress_handler)
	server.HandleFunc("/v1/enumerate", enumerate_handler)
	log.Fatal(http.ListenAndServe(":8080", server))
}
