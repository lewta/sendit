GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: build test test-race integration lint release-check verify

build:
	$(GO) build ./cmd/sendit

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

integration:
	$(GO) test -tags integration -race -v ./...

lint:
	$(GOLANGCI_LINT) run ./...

release-check:
	.github/scripts/verify-macos-release_test.sh
	.github/scripts/resolve-release-tag_test.sh

verify: build lint test-race integration release-check
