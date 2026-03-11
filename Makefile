.PHONY: build test test-unit test-integration test-coverage lint fmt clean install

BINARY_NAME=loom
SERVER_NAME=loom-server
BUILD_DIR=bin

build:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/loom
	go build -o $(BUILD_DIR)/$(SERVER_NAME) ./cmd/loom-server

build-cli:
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/loom

test:
	go test ./... -race

test-unit:
	go test ./internal/... -race

test-integration:
	go test ./test/integration/... -race

test-coverage:
	go test ./... -race -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

lint:
	golangci-lint run

fmt:
	gofmt -w .
	goimports -w .

clean:
	rm -rf $(BUILD_DIR) coverage.out

install:
	go install ./cmd/loom
