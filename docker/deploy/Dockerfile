FROM ubuntu:24.04

ARG GO_VERSION=1.25.0
ENV GO_PACKAGE=go${GO_VERSION}.linux-amd64.tar.gz
ENV BUILD_DIR=/tmp/alfred-build

LABEL maintainer="Bruno Costa <brunoc@cpqd.com.br>"

COPY cmd      ${BUILD_DIR}/cmd
COPY internal ${BUILD_DIR}/internal
COPY go.mod   ${BUILD_DIR}
COPY build.sh ${BUILD_DIR}
COPY ${GO_PACKAGE} ${BUILD_DIR}
RUN cd ${BUILD_DIR} && \
    tar -xf ${BUILD_DIR}/${GO_PACKAGE} && \
    rm -f ${GO_PACKAGE} && \
    mkdir -p /opt/alfred && \
    ${BUILD_DIR}/go/bin/go build -o /opt/alfred/alfred ${BUILD_DIR}/cmd/alfred/main.go ${BUILD_DIR}/cmd/alfred/config.go ${BUILD_DIR}/cmd/alfred/version.go && \
    ${BUILD_DIR}/go/bin/go clean && \
    rm -rf ${BUILD_DIR} /root/.cache/go-build

ENTRYPOINT ["/opt/alfred/alfred", "/data/config.json"]
