#!/bin/bash -e

SDIR=$(cd $(dirname $0) && pwd)
go build -o /tmp/alfred ${SDIR}/cmd/alfred

