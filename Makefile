test:
	go test ./...

build:
	go build -o bin/waypoint ./cmd/waypoint

vet:
	go vet ./...

.PHONY: test build vet
