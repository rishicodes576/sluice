SHELL := /bin/bash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
BIN     := bin/sluice

.PHONY: help build run test race cover lint vet bench demo docker sdk-ts sdk-ruby clean tidy

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the sluice binary
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/sluice

run: build ## Build and run with the example config
	$(BIN) serve --config config.example.yaml

test: ## Run unit + integration tests
	go test ./...

race: ## Run tests with the race detector
	go test -race ./...

cover: ## Run tests and print aggregate coverage
	go test -coverpkg=./internal/... -coverprofile=coverage.out ./... >/dev/null
	go tool cover -func=coverage.out | tail -1

lint: ## Run golangci-lint (if installed) plus go vet
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed; running vet only"
	go vet ./...

bench: ## Run benchmarks
	go test -run=^$$ -bench=. -benchmem ./internal/vectorstore/ ./internal/router/

demo: build ## Run a local end-to-end demo (starts the gateway, sends requests)
	./scripts/demo.sh

docker: ## Build the container image
	docker build -f deploy/docker/Dockerfile -t sluice:$(VERSION) --build-arg VERSION=$(VERSION) .

sdk-ts: ## Test the TypeScript SDK
	cd sdk/typescript && npm install && npm test

sdk-ruby: ## Test the Ruby SDK
	cd sdk/ruby && bundle install && bundle exec rspec

tidy: ## Tidy modules
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin coverage.out
