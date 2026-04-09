BINARY_NAME=ssmx
SSMCP=ssmcp
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -s -w"

GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test

.DEFAULT_GOAL := build

build:
	$(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME) .

build-ssmcp:
	$(GOBUILD) $(LDFLAGS) -o $(SSMCP)/$(SSMCP) ./ssmcp

install-ssmcp:
	$(GOBUILD) $(LDFLAGS) -o /usr/local/bin/$(SSMCP) ./ssmcp
	@echo "Installed $(SSMCP) to /usr/local/bin/"

build-all: clean
	GOOS=linux   GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 .
	GOOS=linux   GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 .
	GOOS=darwin  GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 .
	GOOS=darwin  GOARCH=arm64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 .
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe .

clean:
	$(GOCLEAN)
	rm -f $(BINARY_NAME) $(BINARY_NAME)-* ./$(SSMCP)/$(SSMCP) coverage.out coverage.html

test:
	$(GOTEST) -v -race ./...

test-coverage:
	$(GOTEST) -v -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run

fmt:
	go fmt ./...
	goimports -w .

audit:
	govulncheck ./...

deps:
	$(GOCMD) mod download
	$(GOCMD) mod tidy

install:
	$(GOBUILD) $(LDFLAGS) -o $(HOME)/.local/bin/$(BINARY_NAME) .
	@echo "Installed $(BINARY_NAME) to $(HOME)/.local/bin/"

install-system:
	$(GOBUILD) $(LDFLAGS) -o /usr/local/bin/$(BINARY_NAME) .
	@echo "Installed $(BINARY_NAME) to /usr/local/bin/"

release-dry:
	goreleaser release --snapshot --clean --skip-publish

help:
	@echo "Targets: build build-ssmcp build-all clean test test-coverage lint fmt audit deps install install-system install-ssmcp release-dry"

.PHONY: build build-ssmcp build-all clean test test-coverage lint fmt audit deps install install-system install-ssmcp release-dry help
