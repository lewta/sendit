# Contributing to sendit

Thanks for your interest in contributing. This document covers everything you need to get started.

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
