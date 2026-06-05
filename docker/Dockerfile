# Build stage
FROM golang:1.25 AS builder

WORKDIR /build

COPY ./cmd ./cmd
COPY ./internal ./internal
COPY ./go.mod ./go.mod
COPY ./build.sh ./build.sh

RUN chmod +x build.sh && ./build.sh

# Final imagem
FROM debian:bookworm-slim

RUN mkdir -p /opt

COPY --from=builder /tmp/alfred /opt/alfred

RUN chmod +x /opt/alfred

CMD ["/opt/alfred", "/opt/config.json"]
