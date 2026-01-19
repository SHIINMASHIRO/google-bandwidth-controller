.PHONY: build build-controller build-agent clean test linux-amd64 linux-arm64 all

# Default target
all: build

# Build both binaries for current platform
build: build-controller build-agent

build-controller:
	go build -v -o controller ./cmd/controller

build-agent:
	go build -v -o agent ./cmd/agent

# Build for Linux AMD64
linux-amd64:
	GOOS=linux GOARCH=amd64 go build -v -o controller-linux-amd64 ./cmd/controller
	GOOS=linux GOARCH=amd64 go build -v -o agent-linux-amd64 ./cmd/agent

# Build for Linux ARM64
linux-arm64:
	GOOS=linux GOARCH=arm64 go build -v -o controller-linux-arm64 ./cmd/controller
	GOOS=linux GOARCH=arm64 go build -v -o agent-linux-arm64 ./cmd/agent

# Build for all platforms
linux: linux-amd64 linux-arm64

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	rm -f controller agent
	rm -f controller-* agent-*
	rm -f *.tar.gz

# Install dependencies
deps:
	go mod download
	go mod tidy

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run ./...

# Create release tarball
release: linux-amd64
	tar czf bandwidth-controller-linux-amd64.tar.gz \
		controller-linux-amd64 \
		agent-linux-amd64 \
		configs/ \
		scripts/ \
		deployments/ \
		README.md \
		LICENSE

# Help
help:
	@echo "Available targets:"
	@echo "  build          - Build binaries for current platform"
	@echo "  linux-amd64    - Build binaries for Linux AMD64"
	@echo "  linux-arm64    - Build binaries for Linux ARM64"
	@echo "  linux          - Build for all Linux platforms"
	@echo "  test           - Run tests"
	@echo "  clean          - Remove build artifacts"
	@echo "  deps           - Install dependencies"
	@echo "  fmt            - Format code"
	@echo "  lint           - Run linter"
	@echo "  release        - Create release tarball"
