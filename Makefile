.DEFAULT_GOAL := help
.DELETE_ON_ERROR:

GO ?= go
GOLANGCI_LINT_VERSION := v2.15.0
GOVULNCHECK_VERSION := v1.9.0
TOOLS_DIR := $(CURDIR)/.bin
GOLANGCI_LINT := $(TOOLS_DIR)/golangci-lint/$(GOLANGCI_LINT_VERSION)/golangci-lint
GOVULNCHECK := $(TOOLS_DIR)/govulncheck/$(GOVULNCHECK_VERSION)/govulncheck

# Stripped by default; override with `make build LDFLAGS=` for a debug binary.
LDFLAGS ?= -s -w

.PHONY: help setup hooks tools fmt fmt-check lint vet test coverage tidy tidy-check verify vuln build run docker check

help: ## List available development commands.
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  %-14s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

setup: tools hooks ## Install development tools and enable Git hooks.

hooks: ## Enable version-controlled Git hooks in this repository.
	chmod +x .githooks/pre-commit
	git config --local core.hooksPath .githooks

tools: $(GOLANGCI_LINT) $(GOVULNCHECK) ## Install pinned tools in the project's .bin directory.

$(GOLANGCI_LINT):
	mkdir -p "$(@D)"
	curl --fail --silent --show-error --location "https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh" --output "$(@D)/install.sh"
	sh "$(@D)/install.sh" -b "$(@D)" $(GOLANGCI_LINT_VERSION)
	test -x "$@"

$(GOVULNCHECK):
	mkdir -p "$(@D)"
	GOBIN="$(@D)" $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)

fmt: $(GOLANGCI_LINT) ## Format Go code and organize imports.
	"$(GOLANGCI_LINT)" fmt ./...

fmt-check: $(GOLANGCI_LINT) ## Check formatting without modifying files.
	"$(GOLANGCI_LINT)" fmt --diff ./...

lint: $(GOLANGCI_LINT) ## Run static analysis checks.
	"$(GOLANGCI_LINT)" run ./...

vet: ## Run Go's built-in static analysis.
	$(GO) vet ./...

test: ## Run tests with the race detector and randomized test order.
	$(GO) test -race -shuffle=on -count=1 ./...

coverage: ## Write test coverage reports to coverage/.
	mkdir -p coverage
	$(GO) test -race -shuffle=on -count=1 -covermode=atomic -coverprofile=coverage/coverage.out ./...
	$(GO) tool cover -func=coverage/coverage.out

tidy: ## Update go.mod and go.sum to match the dependencies in use.
	$(GO) mod tidy

tidy-check: ## Check that module files are up to date without modifying them.
	$(GO) mod tidy -diff

verify: ## Verify the integrity of downloaded modules.
	$(GO) mod verify

vuln: $(GOVULNCHECK) ## Check for known vulnerabilities (requires internet access).
	"$(GOVULNCHECK)" ./...

build: ## Build the application as bin/timetracker.
	mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/timetracker ./cmd/timetracker

run: ## Run the application.
	$(GO) run ./cmd/timetracker

docker: ## Build the Docker image with the builder tag derived from go.mod (needs a Docker daemon).
	GO_VERSION="$$(sed -n 's/^go[[:space:]]\{1,\}\([0-9][0-9.]*\).*/\1/p' go.mod)-alpine"; \
	echo "builder tag: $$GO_VERSION"; \
	docker build --build-arg GO_VERSION="$$GO_VERSION" .

check: fmt-check lint vet test tidy-check verify build vuln ## Run all CI checks.
