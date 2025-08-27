#!/bin/bash

SDIR=$(cd $(dirname $0)/../.. && pwd)

GO_VERSION=1.25.0
GO_PACKAGE=go${GO_VERSION}.linux-amd64.tar.gz
GO_FILE=$SDIR/${GO_PACKAGE}

if [ ! -f "${GO_FILE}" ]; then
    wget wget https://go.dev/dl/${GO_PACKAGE} -O ${GO_FILE} || exit 1
fi

docker build --rm \
    -t container-registry.cpqd.com.br/docker-dev/cpqd/i2/alfred:1.0.0 \
    -f ${SDIR}/docker/deploy/Dockerfile \
    --build-arg GO_VERSION=${GO_VERSION} \
    ${SDIR}
