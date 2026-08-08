GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: build test test-race integration lint verify

build:
	$(GO) build ./cmd/sendit

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

integration:
	$(GO) test -tags integration -race -v ./internal/engine/...

lint:
	$(GOLANGCI_LINT) run ./...

verify: build lint test-race integration
