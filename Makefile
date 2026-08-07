export GOTOOLCHAIN := auto

.PHONY: build test lint
build:
	go build -trimpath -ldflags="-s -w" -o cb ./cmd/cb
test:
	go test ./...
lint:
	go vet ./...
