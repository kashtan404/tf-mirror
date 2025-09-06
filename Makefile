.PHONY: all build clean test fmt vet deps downloader server install

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=gofmt
GOVET=$(GOCMD) vet

# Binary name
BINARY=tf-mirror

# Build directory
BUILD_DIR=bin

# Version information
VERSION?=1.0.0
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME?=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS=-ldflags "-X tf-mirror/internal/common.BuildVersion=$(VERSION) -X tf-mirror/internal/common.Commit=$(COMMIT) -X tf-mirror/internal/common.BuildTime=$(BUILD_TIME)"

all: build

# Build application
build:
	mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY) ./cmd/tf-mirror

# Install dependencies (includes semver for version filtering)
deps:
	$(GOMOD) tidy
	$(GOMOD) download

# Run tests
test:
	$(GOTEST) -v ./...

# Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

# Format code
fmt:
	$(GOFMT) -s -w .

# Run go vet
vet:
	$(GOVET) ./...

# Run all checks
check: fmt vet test

# Clean build artifacts
clean:
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f coverage.out coverage.html

# Build for multiple platforms
build-all: build-linux build-darwin build-windows

build-linux:
	mkdir -p $(BUILD_DIR)/linux
	GOOS=linux GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/linux/$(BINARY)-linux-amd64 ./cmd/tf-mirror

build-darwin:
	mkdir -p $(BUILD_DIR)/darwin
	GOOS=darwin GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/darwin/$(BINARY)-darwin-amd64 ./cmd/tf-mirror

build-windows:
	mkdir -p $(BUILD_DIR)/windows
	GOOS=windows GOARCH=amd64 $(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/windows/$(BINARY)-windows-amd64.exe ./cmd/tf-mirror

# Install binary to GOPATH/bin
install:
	$(GOCMD) install $(LDFLAGS) ./cmd/tf-mirror

# Install semver dependency for version filtering
semver:
	$(GOGET) github.com/blang/semver/v4

# Run in downloader mode locally (for development)
run-downloader:
	$(GOCMD) run ./cmd/tf-mirror --mode downloader --help

# Run in server mode locally (for development)
run-server:
	$(GOCMD) run ./cmd/tf-mirror --mode server --help

# Create release packages
release: clean build-all
	mkdir -p $(BUILD_DIR)/release
	cd $(BUILD_DIR) && tar -czf release/tf-mirror-$(VERSION)-linux-amd64.tar.gz linux/
	cd $(BUILD_DIR) && tar -czf release/tf-mirror-$(VERSION)-darwin-amd64.tar.gz darwin/
	cd $(BUILD_DIR) && zip -r release/tf-mirror-$(VERSION)-windows-amd64.zip windows/

# Docker build (if needed)
docker-build:
	docker build -t ademidovx/tf-mirror:$(VERSION) .
	docker build -t ademidovx/tf-mirror:latest .

# Help
help:
	@echo "Available targets:"
	@echo "  all          - Build application"
	@echo "  build        - Build application"
	@echo "  deps         - Install dependencies"
	@echo "  test         - Run tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  fmt          - Format code"
	@echo "  vet          - Run go vet"
	@echo "  check        - Run all checks (fmt, vet, test)"
	@echo "  clean        - Clean build artifacts"
	@echo "  build-all    - Build for all platforms"
	@echo "  install      - Install binary to GOPATH/bin"
	@echo "  release      - Create release packages"
	@echo "  run-downloader - Run in downloader mode (dev)"
	@echo "  run-server   - Run in server mode (dev)"
	@echo "  help         - Show this help"
