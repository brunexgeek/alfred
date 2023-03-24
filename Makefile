.phony: build

build: cmd/alfred/main.go cmd/alfred/config.go
	echo "package main\\nconst ALFRED_VERSION = \"1.0.0 ($(shell git rev-parse --short HEAD))\"" > cmd/alfred/version.go
	go build -o alfred cmd/alfred/main.go cmd/alfred/config.go cmd/alfred/version.go