BIN       := bin
PKG       := ./...
GOFLAGS   ?=
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: ci
ci: tidy-check fmt-check vet test ## Everything CI runs

.PHONY: build
build: ## Build the icpt CLI
	go build $(GOFLAGS) -ldflags "-X main.version=$(VERSION)" -o $(BIN)/icpt ./cmd/icpt

.PHONY: install
install: ## Install icpt into GOBIN
	go install -ldflags "-X main.version=$(VERSION)" ./cmd/icpt

.PHONY: test
test: ## Run the tests with the race detector
	go test -race -count=1 $(PKG)

.PHONY: test-short
test-short: ## Run the tests without the race detector
	go test -count=1 $(PKG)

.PHONY: cover
cover: ## Run the tests and open the coverage report
	go test -coverprofile=coverage.out -covermode=atomic $(PKG)
	go tool cover -html=coverage.out

.PHONY: vet
vet: ## go vet
	go vet $(PKG)

.PHONY: lint
lint: ## golangci-lint (install: https://golangci-lint.run)
	golangci-lint run

.PHONY: fmt
fmt: ## Format
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail when something is unformatted
	@out=$$(gofmt -l . | grep -v '^$$' || true); \
	if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: tidy-check
tidy-check: ## Fail when go.mod/go.sum are not tidy
	@cp go.mod go.mod.bak && cp go.sum go.sum.bak
	@go mod tidy
	@if ! diff -q go.mod go.mod.bak >/dev/null || ! diff -q go.sum go.sum.bak >/dev/null; then \
		mv go.mod.bak go.mod; mv go.sum.bak go.sum; \
		echo "go.mod/go.sum are not tidy; run: make tidy"; exit 1; \
	fi
	@mv go.mod.bak go.mod && mv go.sum.bak go.sum

.PHONY: vuln
vuln: ## Check dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@latest $(PKG)

.PHONY: example
example: ## Run the example service on :8080
	go run ./examples/notes

.PHONY: clean
clean: ## Remove build output
	go clean
	rm -rf ./bin ./coverage.out
