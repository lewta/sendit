# Contributing to sendit

Thanks for your interest in contributing. This document covers everything you need to get started.

## Contents

- [Prerequisites](#prerequisites)
- [Development workflow](#development-workflow)
- [Branch naming](#branch-naming)
- [Commit messages](#commit-messages)
- [Contribution requirements](#contribution-requirements)
- [Testing policy](#testing-policy)
- [Running tests and the linter](#running-tests-and-the-linter)
- [CI checks](#ci-checks)
- [What to work on](#what-to-work-on)
- [Releases](#releases)

---

## Prerequisites

- **Go 1.24+**
- **Chrome or Chromium** — only required if you work on or test the `browser` driver

```sh
# Verify your Go version
go version  # should print go1.24 or later
```

---

## Development workflow

sendit follows a standard **fork → branch → PR** model.

1. **Fork** the repository on GitHub.
2. **Clone** your fork locally:
   ```sh
   git clone https://github.com/<your-username>/sendit
   cd sendit
   ```
3. **Create a branch** from `main`:
   ```sh
   git checkout -b fix/my-bug-fix
   ```
4. **Make your changes**, then run tests and the linter (see below).
5. **Push** to your fork and open a pull request against `lewta/sendit:main`.

A maintainer will review your PR. All CI checks must be green and at least one maintainer approval is required before merge.

---

## Branch naming

| Prefix | Use for |
|--------|---------|
| `feature/` | New functionality |
| `fix/` | Bug fixes |
| `docs/` | Documentation only |
| `ci/` | CI / workflow changes |
| `build/` | Build system or dependency changes |

---

## Commit messages

Use the imperative form with a short type prefix:

```
fix: correct rate limit not resetting after reload
feature: add websocket probe support
docs: update configuration reference for pacing modes
ci: pin govulncheck action to avoid duplicate auth header
build: replace deprecated goreleaser archives.format field
```

- Keep the subject line under 72 characters
- Add a blank line and a longer explanation if the change warrants it
- Reference issues with `Closes #N` or `Fixes #N` in the body

---

## Contribution requirements

All pull requests must meet the following requirements before they will be reviewed:

- **Tests** — new functionality must include tests (see [Testing policy](#testing-policy) below); bug fixes should include a regression test where practical
- **Green CI** — all CI checks must pass (`lint`, `test`, `fuzz`, `govulncheck`, `CodeQL`)
- **Formatted code** — Go files must be formatted with `gofmt -s -w .` before committing
- **Lint-clean** — `golangci-lint run ./...` must produce no new warnings
- **Documentation** — user-facing changes (new flags, config fields, behaviours) must be reflected in the relevant `docs/content/docs/` page and, where appropriate, in the CLI `--help` text

---

## Testing policy

sendit requires tests for all major new functionality. This applies to every PR that adds or changes a feature, driver, pacing mode, or public API surface.

**What is required:**

- New packages and non-trivial functions must have unit tests covering the happy path and significant error paths
- Changes to the dispatch pipeline or driver behaviour must include or update integration tests in `internal/engine/`
- New input-handling or parser code (config loading, flag parsing, file formats) should include a fuzz function in a `*_fuzz_test.go` file following the pattern in `internal/config/config_fuzz_test.go`

**What is not required:**

- Tests for trivial wrappers, generated code, or one-line functions where the test would only mirror the implementation
- End-to-end tests that require external services (e.g. live DNS resolvers, real HTTPS endpoints) — use stubs or the existing integration test harness instead

**Running the full test matrix locally before opening a PR:**

```sh
# Unit tests with race detector
go test -race ./...

# Integration tests
go test -tags integration -race -v ./internal/engine/...

# Fuzz targets (30 s each — same duration as CI)
go test -fuzz=FuzzLoad          -fuzztime=30s ./internal/config/
go test -fuzz=FuzzSelector      -fuzztime=30s ./internal/task/
go test -fuzz=FuzzClassifyError -fuzztime=30s ./internal/ratelimit/
go test -fuzz=FuzzWriteRecord   -fuzztime=30s ./internal/pcap/
```

---

## Running tests and the linter

```sh
# Unit tests
go test ./...

# Unit tests with race detector (required before opening a PR)
go test -race ./...

# Integration tests (spins up local HTTP, DNS, and WebSocket servers)
go test -tags integration -race -v ./internal/engine/...

# Linter (golangci-lint v2)
golangci-lint run ./...

# Validate a config file
./sendit validate --config config/example.yaml
```

If `golangci-lint` is not installed:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
```

---

## CI checks

Every PR must pass all of the following before it can merge:

| Check | Tool |
|-------|------|
| **lint** | golangci-lint (govet, staticcheck, gosec, misspell, unconvert, gofmt, goimports) |
| **test** | `go test -race ./...` |
| **govulncheck** | Scans dependencies against the Go vulnerability database |
| **CodeQL** | GitHub semantic analysis |

---

## What to work on

- Check the [open issues](https://github.com/lewta/sendit/issues) for bugs and accepted feature requests.
- Check the [roadmap](ROADMAP.md) for planned work — comment on the relevant issue before starting a large item so we can avoid duplicated effort.
- Small fixes and documentation improvements are always welcome without prior discussion.

---

## Releases

Releases are cut by maintainers. Contributors do not need to tag versions or create GitHub releases — just get your PR merged and it will be included in the next release.
