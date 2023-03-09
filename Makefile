.phony: build


build: cmd/alfred/main.go
	go build -o alfred cmd/alfred/main.go