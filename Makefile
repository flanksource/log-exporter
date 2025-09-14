# Log Exporter Makefile

# Variables
GO_VERSION := 1.25.1
BINARY_NAME := log-exporter
PKG := github.com/flanksource/log-exporter
BUILD_DIR := .
COVERAGE_OUT := coverage.out

# Installation paths
PREFIX ?= /usr/local
BINDIR := $(PREFIX)/bin
INSTALL := install

# Build information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Go build flags
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildDate=$(BUILD_DATE)
BUILD_FLAGS := -ldflags "$(LDFLAGS)"

# Default target
.DEFAULT_GOAL := help

##@ Help
.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: build
build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	go build $(BUILD_FLAGS) -o $(BINARY_NAME) .

.PHONY: install
install: build ## Install the binary to system PATH
	@echo "Installing $(BINARY_NAME) to $(BINDIR)..."
	@mkdir -p $(BINDIR)
	$(INSTALL) -m 755 $(BINARY_NAME) $(BINDIR)/$(BINARY_NAME)
	@echo "$(BINARY_NAME) installed successfully to $(BINDIR)/$(BINARY_NAME)"

.PHONY: uninstall
uninstall: ## Uninstall the binary from system PATH
	@echo "Uninstalling $(BINARY_NAME) from $(BINDIR)..."
	rm -f $(BINDIR)/$(BINARY_NAME)
	@echo "$(BINARY_NAME) uninstalled successfully"

.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -f $(BINARY_NAME)
	rm -f $(COVERAGE_OUT)
	go clean -cache -testcache

.PHONY: deps
deps: ## Download and verify dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod verify

.PHONY: tidy
tidy: ## Tidy go modules
	@echo "Tidying go modules..."
	go mod tidy

.PHONY: fmt
fmt: ## Format Go code
	@echo "Formatting code..."
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	@echo "Running go vet..."
	go vet ./...

.PHONY: lint
lint: fmt vet ## Run linting (fmt + vet)
	@echo "Linting completed"

##@ Testing
.PHONY: test
test: ## Run all tests
	@echo "Running all tests..."
	go test -v ./...

.PHONY: test-unit
test-unit: ## Run unit tests only (exclude integration tests)
	@echo "Running unit tests..."
	go test -v ./... -short

.PHONY: test-integration
test-integration: ## Run integration tests only
	@echo "Running integration tests..."
	go test -v ./pkg/opensearch -run TestOpenSearchIntegration

.PHONY: test-coverage
test-coverage: ## Run tests with coverage
	@echo "Running tests with coverage..."
	go test -v -coverprofile=$(COVERAGE_OUT) ./...
	go tool cover -html=$(COVERAGE_OUT) -o coverage.html
	@echo "Coverage report generated: coverage.html"


##@ Docker
.PHONY: docker-test
docker-test: ## Run tests that require Docker (integration tests)
	@echo "Checking Docker availability..."
	@docker info >/dev/null 2>&1 || (echo "Error: Docker is not running" && exit 1)
	@echo "Running Docker-dependent tests..."
	$(MAKE) test-integration

##@ Quality
.PHONY: check
check: lint test ## Run all checks (lint + test)

.PHONY: check-deps
check-deps: ## Check for outdated dependencies
	@echo "Checking for outdated dependencies..."
	go list -u -m all

.PHONY: security
security: ## Run security checks (go mod verify)
	@echo "Running security checks..."
	go mod verify
	@echo "Security checks passed"

##@ CI/CD
.PHONY: ci
ci: deps lint test ## Run CI pipeline (deps + lint + test)
	@echo "CI pipeline completed successfully"

.PHONY: ci-coverage
ci-coverage: deps lint test-coverage ## Run CI with coverage
	@echo "CI pipeline with coverage completed successfully"

##@ Development Tools
.PHONY: install-tools
install-tools: ## Install development tools
	@echo "Installing development tools..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: mock-test
mock-test: ## Run mock test script
	@echo "Running mock test script..."
	go run hack/test_opensearch.go

.PHONY: completion
completion: build ## Generate shell completions
	@echo "Generating shell completions..."
	./$(BINARY_NAME) completion bash > $(BINARY_NAME)-completion.bash
	./$(BINARY_NAME) completion zsh > $(BINARY_NAME)-completion.zsh
	./$(BINARY_NAME) completion fish > $(BINARY_NAME)-completion.fish

##@ Release
.PHONY: build-release
build-release: ## Build release binary with optimizations
	@echo "Building release binary..."
	CGO_ENABLED=0 go build -trimpath $(BUILD_FLAGS) -a -installsuffix cgo -o $(BINARY_NAME) .

.PHONY: version
version: ## Show version information
	@echo "Version: $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build Date: $(BUILD_DATE)"

##@ Documentation
.PHONY: docs
docs: build ## Generate CLI documentation
	@echo "CLI binary built. Use './$(BINARY_NAME) --help' for documentation"
	@echo "Or run './$(BINARY_NAME) completion <shell>' for shell completion"

.PHONY: show-targets
show-targets: ## Show all available make targets
	@echo "Available targets:"
	@$(MAKE) -pRrq -f $(firstword $(MAKEFILE_LIST)) : 2>/dev/null | awk -v RS= -F: '/(^|\n)# Files(\n|$$)/,/(^|\n)# Finished Make data base/ {if ($$1 !~ "^[#.]") {print $$1}}' | sort | grep -E -v -e '^[^[:alnum:]]' -e '^$@$$'

# Ensure build directory exists
$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)