.PHONY: test build build-go web vet

test:
	go test ./...
	cd web && npm test

# The full build: the web UI first (it is embedded in the binary), then Go.
build: web build-go

build-go:
	go build -o bin/waypoint ./cmd/waypoint

# Builds the UI into internal/http/dist.
web:
	cd web && npm ci && npm run build

vet:
	go vet ./...
