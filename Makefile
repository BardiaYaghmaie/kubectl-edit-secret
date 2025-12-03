.PHONY: build build-all clean install test lint fmt

BINARY_NAME := kubectl-edit_secret
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE) -s -w"

# Build for current platform
build:
	go build $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/kubectl-edit_secret/

# Build for all platforms
build-all: build-linux build-darwin build-windows

build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-amd64 ./cmd/kubectl-edit_secret/
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-linux-arm64 ./cmd/kubectl-edit_secret/

build-darwin:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-amd64 ./cmd/kubectl-edit_secret/
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-darwin-arm64 ./cmd/kubectl-edit_secret/

build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY_NAME)-windows-amd64.exe ./cmd/kubectl-edit_secret/

# Install to kubectl plugins directory
install: build
	@mkdir -p $(HOME)/.krew/bin
	cp bin/$(BINARY_NAME) $(HOME)/.krew/bin/
	@echo "Installed to $(HOME)/.krew/bin/$(BINARY_NAME)"
	@echo "Make sure $(HOME)/.krew/bin is in your PATH"

# Install directly to /usr/local/bin (requires sudo)
install-global: build
	sudo cp bin/$(BINARY_NAME) /usr/local/bin/
	@echo "Installed to /usr/local/bin/$(BINARY_NAME)"

# Clean build artifacts
clean:
	rm -rf bin/
	rm -rf dist/

# Run tests
test:
	go test -v ./...

# Run linter
lint:
	golangci-lint run ./...

# Format code
fmt:
	go fmt ./...

# Download dependencies
deps:
	go mod download
	go mod tidy

# Create release archives
release: build-all
	@mkdir -p dist
	cd bin && tar -czf ../dist/$(BINARY_NAME)-linux-amd64.tar.gz $(BINARY_NAME)-linux-amd64
	cd bin && tar -czf ../dist/$(BINARY_NAME)-linux-arm64.tar.gz $(BINARY_NAME)-linux-arm64
	cd bin && tar -czf ../dist/$(BINARY_NAME)-darwin-amd64.tar.gz $(BINARY_NAME)-darwin-amd64
	cd bin && tar -czf ../dist/$(BINARY_NAME)-darwin-arm64.tar.gz $(BINARY_NAME)-darwin-arm64
	cd bin && zip ../dist/$(BINARY_NAME)-windows-amd64.zip $(BINARY_NAME)-windows-amd64.exe
	@echo "Release archives created in dist/"

# Print version
version:
	@echo $(VERSION)

# Help
help:
	@echo "kubectl-edit-secret Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make build         Build for current platform"
	@echo "  make build-all     Build for all platforms"
	@echo "  make install       Install to ~/.krew/bin"
	@echo "  make install-global Install to /usr/local/bin (requires sudo)"
	@echo "  make clean         Remove build artifacts"
	@echo "  make test          Run tests"
	@echo "  make deps          Download dependencies"
	@echo "  make release       Create release archives"
	@echo "  make help          Show this help"

