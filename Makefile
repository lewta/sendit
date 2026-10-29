GO ?= go
GOLANGCI_LINT ?= golangci-lint

.PHONY: build test test-race test-browser integration lint release-check compose-security vulnerability-check verify

build:
	$(GO) build ./cmd/sendit

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-browser:
	SENDIT_TEST_BROWSER=1 $(GO) test -race -tags integration -run '^(TestBrowserDriver|TestIntegrationReplayBrowser)' ./internal/driver ./cmd/sendit

integration:
	$(GO) test -tags integration -race -v ./...

lint:
	$(GOLANGCI_LINT) run ./...

release-check:
	bash .github/scripts/validate-release-input_test.sh
	.github/scripts/verify-macos-release_test.sh
	.github/scripts/resolve-release-tag_test.sh

compose-security:
	GO="$(GO)" bash docker/verify-compose-security.sh

vulnerability-check:
	GO="$(GO)" bash .github/scripts/evaluate-govulncheck_test.sh
	GO="$(GO)" bash .github/scripts/verify-build-contexts_test.sh

verify: build lint test-race integration release-check compose-security vulnerability-check
