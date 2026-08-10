export GOTOOLCHAIN := auto

.PHONY: build test lint
build:
	go build -trimpath -ldflags="-s -w" -o prior ./cmd/prior
test:
	go test ./...
lint:
	go vet ./...
