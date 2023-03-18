.phony: build


build: cmd/alfred/main.go cmd/alfred/config.go
	go build -o alfred cmd/alfred/main.go cmd/alfred/config.go