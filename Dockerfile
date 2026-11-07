# Build stage: compile a static binary.
FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pod-restart-exporter .

# Runtime stage: no shell, no package manager, runs as non-root.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /pod-restart-exporter /pod-restart-exporter
USER 65532:65532
EXPOSE 9101
ENTRYPOINT ["/pod-restart-exporter"]
