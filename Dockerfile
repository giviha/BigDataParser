FROM golang:1.24-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler && rm -rf /var/lib/apt/lists/*
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.8 && \
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
WORKDIR /src
COPY go.mod ./
COPY api ./api
COPY internal ./internal
COPY cmd ./cmd
RUN protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative api/logs.proto
RUN go mod tidy
RUN CGO_ENABLED=0 go test ./...
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/agent ./cmd/agent && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/collector ./cmd/collector && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/loadgen ./cmd/loadgen

FROM scratch AS agent
COPY --from=build /bin/agent /agent
ENTRYPOINT ["/agent"]

FROM scratch AS collector
COPY --from=build /bin/collector /collector
ENTRYPOINT ["/collector"]


FROM alpine:3.22 AS source-alpine
COPY --from=build /bin/loadgen /loadgen
ENTRYPOINT ["/loadgen"]

FROM debian:bookworm-slim AS source-debian
COPY --from=build /bin/loadgen /loadgen
ENTRYPOINT ["/loadgen"]

FROM ubuntu:24.04 AS source-ubuntu
COPY --from=build /bin/loadgen /loadgen
ENTRYPOINT ["/loadgen"]
