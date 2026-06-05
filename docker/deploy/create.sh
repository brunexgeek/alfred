#!/bin/bash

SDIR=$(cd $(dirname $0)/../.. && pwd)

SUDO_PREFIX=""
if docker ps 2>&1 | grep -q "/var/run/docker.sock"; then
    echo "Using 'sudo' to run Docker"
    SUDO_PREFIX="sudo"
fi

${SUDO_PREFIX} docker build --rm \
    -t brunexgeek/alfred:0.1.0 \
    -f ${SDIR}/docker/deploy/Dockerfile \
    ${SDIR}
