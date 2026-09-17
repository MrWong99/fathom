# Copyright 2026 Lukas Schmidt
# SPDX-License-Identifier: MIT

MODULE   := github.com/MrWong99/fathom
BIN_DIR  := bin
CMD_DIR  := ./cmd

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

GOFLAGS     ?=
GOTESTFLAGS ?= -race -count=1

# Spikes are separate Go modules under spikes/<name>/ so their heavy
# dependencies never leak into the product module.
SPIKE_MODS := $(dir $(wildcard spikes/*/go.mod))

.DEFAULT_GOAL := help

.PHONY: build
build: ## Build all cmd/ binaries into bin/ (static, CGO_ENABLED=0)
	@mkdir -p $(BIN_DIR)
	@for dir in $(CMD_DIR)/*/; do \
		name=$$(basename "$$dir"); \
		echo "Building cmd: $$name"; \
		CGO_ENABLED=0 go build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$$name ./$$dir; \
	done

.PHONY: test
test: ## Run all tests with race detection
	go test $(GOTESTFLAGS) ./...

.PHONY: test-short
test-short: ## Run tests, skipping long-running ones
	go test $(GOTESTFLAGS) -short ./...

.PHONY: test-cover
test-cover: ## Run tests with coverage summary
	@mkdir -p $(BIN_DIR)
	go test $(GOTESTFLAGS) -coverprofile=$(BIN_DIR)/coverage.out ./...
	go tool cover -func=$(BIN_DIR)/coverage.out | tail -1

.PHONY: fmt
fmt: ## Format with gofmt -s
	gofmt -s -w $$(find . -name '*.go' -not -path './bin/*')

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run ./...

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	golangci-lint run --fix ./...

.PHONY: check
check: fmt vet lint test ## Full local gate: fmt + vet + lint + test

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: spikes
spikes: ## Run every spike module's tests (spikes/<name>/go.mod)
	@if [ -z "$(SPIKE_MODS)" ]; then echo "no spike modules yet"; exit 0; fi
	@for d in $(SPIKE_MODS); do \
		echo "== spike: $$d"; \
		(cd $$d && go test $(GOTESTFLAGS) ./...) || exit 1; \
	done

.PHONY: clean
clean: ## Remove build artifacts
	$(RM) -r $(BIN_DIR)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'
