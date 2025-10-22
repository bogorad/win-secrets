# Makefile for win-secrets

# Binary name
BINARY_NAME=win-secrets

# Version info from git, fallback to dev
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")

# Linker flags to embed version info
LDFLAGS=-ldflags "-w -s -X 'main.Version=$(VERSION)' -X 'main.Commit=$(COMMIT)' -X 'main.Date=$(DATE)'"

# Build for the current host platform (auto-detected)
.PHONY: build
build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BINARY_NAME).exe

# Build for Windows AMD64 (x64)
.PHONY: build-windows-amd64
build-windows-amd64:
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-amd64.exe

# Build for Windows ARM64
.PHONY: build-windows-arm64
build-windows-arm64:
	CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-windows-arm64.exe

# Build all Windows architectures
.PHONY: build-all
build-all: build-windows-amd64 build-windows-arm64
	@echo "Built binaries for all Windows platforms"

# Run tests
.PHONY: test
test:
	go test -v ./...

# Run tests with race detection
.PHONY: test-race
test-race:
	go test -race -v ./...

# Clean build artifacts
.PHONY: clean
clean:
	go clean
	rm -f $(BINARY_NAME).exe
	rm -f $(BINARY_NAME)-windows-amd64.exe
	rm -f $(BINARY_NAME)-windows-arm64.exe

# Format code
.PHONY: fmt
fmt:
	go fmt ./...

# Run linter (requires golangci-lint)
.PHONY: lint
lint:
	golangci-lint run

# Download dependencies
.PHONY: deps
deps:
	go mod download
	go mod tidy

# Display version info that will be embedded
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Date:    $(DATE)"

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  build                - Build for current platform (CGO disabled)"
	@echo "  build-windows-amd64  - Build for Windows x64"
	@echo "  build-windows-arm64  - Build for Windows ARM64"
	@echo "  build-all            - Build for all Windows platforms"
	@echo "  test                 - Run tests"
	@echo "  test

