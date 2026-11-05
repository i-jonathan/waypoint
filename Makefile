.PHONY: test e2e build build-go web vet

test:
	go test ./...
	cd web && npm test

# Browser tests against the real binary. They build their own copy in a temp folder.
e2e:
	cd web && npm run e2e

# The full build: the web UI first (it is embedded in the binary), then Go.
build: web build-go

build-go:
	go build -o bin/waypoint ./cmd/waypoint

# Builds the UI into internal/http/dist.
web:
	cd web && npm ci && npm run build

vet:
	go vet ./...
