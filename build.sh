#!/bin/bash -e

VERSION=1.0.0
OUTPUT=/tmp

cd $(dirname $0)

if [ "$1" != "fast" ]; then
    COMMIT=$(git rev-parse --short HEAD)

    if (! grep -q "${COMMIT}" "cmd/alfred/version.go"); then
        echo -e "package main\nconst ALFRED_VERSION = \"${VERSION} ${COMMIT}\"" > cmd/alfred/version.go
    fi
fi

go build -o $OUTPUT/alfred cmd/alfred/main.go cmd/alfred/config.go cmd/alfred/version.go

