BIN       := bin
PKG       := ./...
# Coverage has to be told which packages to measure. The tests live in
# tests/ and the code lives in src/, so without -coverpkg every run would
# report 0% — the test packages themselves contain no statements.
COVERPKG  := ./src/...,./internal/...,.
GOFLAGS   ?=
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE      ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- develop

.PHONY: ci
ci: tidy-check fmt-check vet test examples ## Everything CI runs

.PHONY: build
build: ## Build the icpt CLI into bin/
	go build $(GOFLAGS) -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/icpt ./cmd/icpt

.PHONY: install
install: ## Install icpt into GOBIN
	go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/icpt

.PHONY: test
test: ## Run the tests with the race detector
	go test -race -count=1 $(PKG)

.PHONY: test-short
test-short: ## Run the tests without the race detector
	go test -count=1 $(PKG)

.PHONY: cover
cover: ## Coverage report for src/ and internal/
	go test -coverpkg=$(COVERPKG) -coverprofile=coverage.out -covermode=atomic $(PKG)
	@go tool cover -func=coverage.out | tail -1

.PHONY: cover-html
cover-html: cover ## Coverage report, in a browser
	go tool cover -html=coverage.out

.PHONY: bench
bench: ## Run the benchmarks
	go test -run '^$$' -bench . -benchmem $(PKG)

.PHONY: examples
examples: ## Build and vet the examples module
	cd examples && go build ./... && go vet ./...

.PHONY: run-todo run-blog run-worker run-minimal run-webhooks run-queues run-scheduling run-sse run-event-emitter
run-minimal: ## Run the minimal example on :8080
	cd examples && go run ./minimal
run-todo: ## Run the todo example on :8080
	cd examples && go run ./todo
run-blog: ## Run the blog example on :8081
	cd examples && go run ./blog
run-worker: ## Run the worker example on :8082
	cd examples && go run ./worker
run-webhooks: ## Run the signed webhooks example on :8084
	cd examples && go run ./webhooks
run-queues: ## Run the queue example on :8085
	cd examples && go run ./queues
run-scheduling: ## Run the scheduling example on :8086
	cd examples && go run ./scheduling
run-sse: ## Run the SSE example on :8087
	cd examples && go run ./sse
run-event-emitter: ## Run the event emitter example on :8088
	cd examples && go run ./event-emitter

# ----------------------------------------------------------------- checks

.PHONY: vet
vet: ## go vet
	go vet $(PKG)

.PHONY: lint
lint: ## golangci-lint (install: https://golangci-lint.run)
	golangci-lint run

.PHONY: fmt
fmt: ## Format
	gofmt -w .
	cd examples && gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail when something is unformatted
	@out=$$(gofmt -l . ; cd examples && gofmt -l .); \
	if [ -n "$$out" ]; then echo "unformatted:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## go mod tidy, both modules
	go mod tidy
	cd examples && go mod tidy

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

.PHONY: doctor
doctor: build ## Run the CLI's own project check
	./$(BIN)/icpt doctor

# ---------------------------------------------------------------- release

.PHONY: apidiff
apidiff: ## What changed in the exported API since the last tag
	@last=$$(git describe --tags --abbrev=0 2>/dev/null || true); \
	if [ -z "$$last" ]; then echo "no tags yet; nothing to compare against"; exit 0; fi; \
	echo "comparing the exported API against $$last"; \
	go run golang.org/x/exp/cmd/gorelease@latest -base=$$last || true

.PHONY: release-dry
release-dry: ## Full release build, nothing published
	go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean --skip=publish,sign

.PHONY: release-check
release-check: ci lint vuln apidiff ## Everything to run before tagging
	@echo
	@echo "Now: update CHANGELOG.md, then see RELEASING.md"

# ------------------------------------------------------------------ chore

.PHONY: clean
clean: ## Remove build output
	go clean
	rm -rf ./bin ./dist ./coverage.out
