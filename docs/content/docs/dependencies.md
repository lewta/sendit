---
title: "Dependencies"
linkTitle: "Dependencies"
weight: 95
description: "Direct dependencies, their purpose, and their licences."
---

sendit has 21 direct runtime dependencies and 1 direct test dependency. All are permissive open-source licences
compatible with the project's [MIT licence](https://github.com/lewta/sendit/blob/main/LICENSE).

The module graph is managed with `go mod tidy` and kept minimal — no dependency
appears that cannot be justified by the table below.

HTTP/1.1 and HTTP/2 selection use Go 1.26.10's standard `net/http.Protocols` and TLS ALPN verification; no new transport dependency is required. HTTP/3/QUIC dependency and distribution choices remain tracked in [#320](https://github.com/lewta/sendit/issues/320).

## Direct dependencies

| Module | Version | Licence | Purpose |
|--------|---------|---------|---------|
| [`github.com/charmbracelet/bubbletea`](https://github.com/charmbracelet/bubbletea) | v1.3.10 | MIT | Elm-architecture TUI framework — powers the `--tui` terminal dashboard |
| [`github.com/charmbracelet/lipgloss`](https://github.com/charmbracelet/lipgloss) | v1.1.0 | MIT | Style definitions for the terminal UI (bold labels, colour-coded counters) |
| [`github.com/chromedp/chromedp`](https://github.com/chromedp/chromedp) | v0.21.0 | MIT | Browser automation via the Chrome DevTools Protocol — powers the `browser` driver |
| [`github.com/coder/websocket`](https://github.com/coder/websocket) | v1.8.15 | ISC | WebSocket client — powers the `websocket` driver |
| [`github.com/go-viper/mapstructure/v2`](https://github.com/go-viper/mapstructure) | v2.5.0 | MIT | Configuration decode hooks and Viper unmarshalling |
| [`github.com/miekg/dns`](https://github.com/miekg/dns) | v1.1.73 | BSD-3-Clause | Full-featured DNS client and server library — powers the `dns` driver |
| [`github.com/pkg/sftp`](https://github.com/pkg/sftp) | v1.13.11 | BSD-2-Clause | SFTP client and test server — powers the `sftp` driver |
| [`github.com/prometheus/client_golang`](https://github.com/prometheus/client_golang) | v1.25.0 | Apache-2.0 | Prometheus metrics exposition (`/metrics` endpoint) |
| [`github.com/robfig/cron/v3`](https://github.com/robfig/cron) | v3.0.1 | MIT | Cron expression parser — used by `scheduled` pacing mode to define active windows |
| [`github.com/rs/zerolog`](https://github.com/rs/zerolog) | v1.35.1 | MIT | Zero-allocation structured logger; `zerolog.Nop()` used internally for no-op metrics |
| [`github.com/shirou/gopsutil/v3`](https://github.com/shirou/gopsutil) | v3.24.5 | BSD-3-Clause | Cross-platform CPU and memory utilisation polling — powers the resource admission gate |
| [`github.com/spf13/cobra`](https://github.com/spf13/cobra) | v1.10.2 | Apache-2.0 | CLI framework — commands, flags, and shell completion generation |
| [`github.com/spf13/viper`](https://github.com/spf13/viper) | v1.21.0 | MIT | Config file loading with environment variable overlay and `mapstructure` unmarshalling |
| [`go.yaml.in/yaml/v3`](https://pkg.go.dev/go.yaml.in/yaml/v3) | v3.0.5 | MIT | Raw YAML syntax tree used to validate template variable key casing before Viper normalization |
| [`golang.org/x/crypto`](https://pkg.go.dev/golang.org/x/crypto) | v0.58.0 | BSD-3-Clause | `ssh` subpackage — SSH transport and algorithm policy controls for the `sftp` driver |
| [`golang.org/x/net`](https://pkg.go.dev/golang.org/x/net) | v0.62.0 | BSD-3-Clause | `html` subpackage — HTML parser used by the `generate` command to extract links |
| [`golang.org/x/time`](https://pkg.go.dev/golang.org/x/time) | v0.16.0 | BSD-3-Clause | `rate` subpackage — token-bucket rate limiter used by `rate_limited` and `scheduled` pacing |
| [`google.golang.org/grpc`](https://pkg.go.dev/google.golang.org/grpc) | v1.84.0 | Apache-2.0 | gRPC client and server — powers the `grpc` driver; includes reflection client and health service |
| [`google.golang.org/protobuf`](https://pkg.go.dev/google.golang.org/protobuf) | v1.36.12 | BSD-3-Clause | Dynamic protobuf messages and JSON/protobuf marshaling for the reflection-based `grpc` driver |
| [`howett.net/plist`](https://pkg.go.dev/howett.net/plist) | v1.0.1 | BSD-2-Clause | Property-list parser used by `generate` to read Safari bookmarks |
| [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) | v1.60.1 | BSD-3-Clause | Pure-Go SQLite driver (CGo-free) — used by `generate` to read Chrome/Firefox history and bookmark databases |

## Test dependencies

| Module | Version | Licence | Purpose |
|--------|---------|---------|---------|
| [`github.com/cucumber/godog`](https://github.com/cucumber/godog) | v0.16.0 | MIT | Cucumber/Gherkin BDD test framework — powers the `features/auth.feature` behavioural tests for the auth block |

## Development tools

The repository's `.mise.toml` pins Go and golangci-lint. `mise install` provisions both; `mise exec -- make verify` runs the canonical pre-PR gate without shell activation. See the [contributor setup guide](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md#prerequisites) for installation and configuration trust.

Offline advisory checks use Python 3.9 or newer with its standard library only;
macOS's system `/usr/bin/python3` is supported. All advisory entry points and test
runners honor `PYTHON` (default `python3`), for example
`PYTHON=/usr/bin/python3 make vulnerability-check`. The entry point reports a clear
version requirement before analysis if Python is older than 3.9. No Python package
installation or additional toolchain is required.

Browser-driver, chromedp, and browser-replay changes also require `mise exec -- make test-browser` with Chrome or Chromium installed. Browser execution tests are opt-in and are not included in the default gate. The browser target fails if the browser is unavailable.

These are development requirements, not additional runtime dependencies of the sendit binary.

## Advisory evidence

The existing Security `govulncheck` job installs
`golang.org/x/vuln/cmd/govulncheck@v1.9.0` and analyzes `./...` in source/symbol mode
with the project's pinned Go 1.26.10. The OSV Scanner v2.7.0 container action is
pinned to commit `0a65f82af05cd58cf587827b0bb0489548c18346`; it reads `go.mod` with
Go call analysis disabled and transitive metadata resolution retained. OSV is a module-coordinate
metadata complement, and makes no independent reachability claim. Its container
also avoids installing OSV's newer Go build requirement into the project toolchain.

Every run retains all nine release targets: Linux amd64/arm64/armv7/386,
Darwin amd64/arm64, Windows amd64/arm64, and FreeBSD amd64. These nine contexts
use `CGO_ENABLED=0`, matching release builds. Two additional Linux amd64 contexts
use `CGO_ENABLED=1`: the existing default `./...` source scan and an integration
scan adding `-test -tags=integration`. Each of the eleven contexts records the
commit, Go version, GOOS/GOARCH/GOARM,
tags, test selection, roots, module/package closure, and hashes of selected Go,
assembly, embedded, generated test-main, and module inputs. No context is
collapsed: identical package or module lists do not establish equivalent source
or call graphs. Changes to release targets require updating this coverage table
and the offline context tests.

Structured govulncheck output returns status zero for findings and has no terminal
completion record. The wrapper therefore records status and completion times only
after the scanner process returns, binding the result bytes to the selected
context. The evaluator requires strict JSON through EOF, exactly one expected
source/symbol config with scanner/Go/database provenance, a matching SBOM, and a
successful completion sidecar. Reachable symbol findings, scanner/database errors,
truncation, missing or mismatched context, and incomplete coverage block the job.
Finding modules and versions must match the selected closure, and every package
frame must match its recorded package-to-module coordinate. Conditional findings
must have one module or package frame; symbol traces require valid Go symbols on
every frame. All retained identifiers are validated by their field grammar and
context before source JSON is written. Valid reachable traces remain available as
blocking evidence; malformed source results retain only safe completion and error
metadata.
Module/package-only findings remain visible as conditional evidence and are not
described as reachable. OSV nonzero status for metadata findings is accepted only
when the pinned action produces a nonempty, complete, parseable result; missing or
invalid output blocks the final gate. The importer requires the supported pinned
result shape, the exact `go.mod` lockfile source and Go ecosystem, and coordinates
in the recorded full Go module graph. Error/incomplete/unknown result records,
unexpected contexts, and nonzero outcomes without finding evidence block. The
OSV source path is retained only as the canonical name `go.mod`.

The sanitized `vulnerability-evidence` Actions artifact is retained for 14 days,
including failed scans. It contains context manifests, source JSON, scanner/Go/DB
metadata (including the database last-modified time), completion sidecars,
evaluations, exception evidence, and an OSV metadata summary. Raw streams,
stderr text, absolute position paths, progress text, and advisory prose are never
uploaded. Raw govulncheck streams are temporary, limited to 64 MiB per stream,
and removed after evaluation. Only the named artifact directory is uploaded;
no environment dumps or request data are collected. Artifact access follows the
repository's Actions permissions; download access is not made private by the
sanitizer or upload action.

`osv-scanner.toml` retains GO-2026-5932 only until **2026-12-06**, while the affected
`golang.org/x/crypto/openpgp` family stays absent from every selected closure.
The gate checks both conditions independently of OSV's ignore handling. Review
the retained closures before expiry; an affected-package import or expiry blocks
the exception. GO-2026-6505 remains a conditional `otel/sdk` module-graph match;
it has no ignore entry and must not be called reachable without source/symbol
evidence. Reassess it whenever the SDK/exporter enters a selected package closure
or a source scan reports a callable symbol.

The local exception reader intentionally accepts only the repository's one known
`[[IgnoredVulns]]` block: unique `id`, `ignoreUntil`, and `reason` keys, simple
unescaped double-quoted strings, a bare ISO date, comments, and blank lines. Other
TOML forms, duplicate fields/blocks, unknown keys, and alternate quoting fail
closed and require policy review. This small parser supports Python 3.9 without a
TOML package dependency.

`make vulnerability-check` runs offline evaluator, workflow-policy, scanner-boundary,
and selected-source fixture tests without installing scanners or consulting a
database. It is included in `make verify` and CI. A passing fixture suite is not a
fresh database scan; the first retained CI result is still required for a current
advisory assessment. To enumerate local contexts with the pinned Go on PATH:

```sh
bash .github/scripts/verify-build-contexts.sh generate "$PWD" /tmp/sendit-contexts.json
bash .github/scripts/verify-build-contexts.sh verify /tmp/sendit-contexts.json
```

## Alternatives considered

The following alternatives were evaluated during the v0.13.3 audit and ruled out:

| Dependency | Alternative considered | Decision |
|------------|----------------------|----------|
| `golang.org/x/net/html` | stdlib | No stdlib equivalent — `x/net/html` is the canonical Go HTML parser |
| `golang.org/x/time/rate` | stdlib | No stdlib token-bucket; `time.Ticker` cannot express burst semantics |
| `golang.org/x/crypto/ssh` | stdlib | No SSH client in the standard library; `x/crypto/ssh` is the maintained Go SSH implementation |
| `github.com/pkg/sftp` | hand-rolled SFTP packets | Reusing the established SFTP client avoids implementing protocol framing and extension handling locally |
| `github.com/spf13/viper` | stdlib `os.Getenv` + manual YAML | Viper provides env-override, defaults, and `mapstructure` in one — justified for config complexity |
| `github.com/rs/zerolog` | stdlib `log/slog` (Go 1.21+) | zerolog's zero-allocation design and `Nop()` no-op are better suited for high-throughput dispatch paths |

## Licence compatibility

All dependency licences are permissive and compatible with the project's MIT licence:

| Licence | Dependencies |
|---------|-------------|
| MIT | `bubbletea`, `lipgloss`, `chromedp`, `mapstructure/v2`, `cron/v3`, `zerolog`, `viper`, `yaml/v3` |
| ISC | `coder/websocket` |
| BSD-2-Clause | `pkg/sftp`, `howett.net/plist` |
| BSD-3-Clause | `miekg/dns`, `gopsutil/v3`, `x/crypto`, `x/net`, `x/time`, `google.golang.org/protobuf`, `modernc.org/sqlite` |
| Apache-2.0 | `prometheus/client_golang`, `cobra`, `google.golang.org/grpc` |

ISC, BSD-2-Clause, and BSD-3-Clause are functionally equivalent to MIT for distribution purposes.
Apache-2.0 is compatible with MIT when distributing binaries (no copyleft restriction).
