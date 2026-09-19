.PHONY: all build test vet lint clean

MODULE  := github.com/izhubs/izdeploy
BIN_DIR := bin
LDFLAGS := -s -w

all: build

build:
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/izdeploy ./cmd/izdeploy
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/izdeploy-agent ./cmd/izdeploy-agent
	go build -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/izdeploy-watchdog ./cmd/izdeploy-watchdog

test:
	go test -v -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

clean:
	go clean
	rm -rf $(BIN_DIR)
