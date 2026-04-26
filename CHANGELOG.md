# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

If a release patches a publicly known vulnerability, it will be noted explicitly
under the affected version with a reference to the CVE or advisory.

---

## [Unreleased]

---

## [1.2.1] - 2026-04-26

### Changed
- Bump `modernc.org/sqlite` from 1.48.1 to 1.50.0
- Bump `golang.org/x/net` from 0.52.0 to 0.53.0
- Bump `github.com/rs/zerolog` from 1.35.0 to 1.35.1
- Bump `goreleaser/goreleaser-action` from 7.0.0 to 7.1.0
- Bump `github/codeql-action` from 4.35.1 to 4.35.2
- Bump `actions/create-github-app-token` from 3.0.0 to 3.1.1
- Bump `actions/upload-artifact` from 7.0.0 to 7.0.1

---

## [1.2.0] - 2026-04-05

### Added
- `auth` block per target: `bearer`, `basic`, `header`, and `query` authentication for `http` and `websocket` targets
- Token values resolved at dispatch time from literal config values or environment variables (`token_env`, `username_env`, `password_env`)
- Literal token/password in config triggers a startup warning log — use env-var references in production
- `github.com/cucumber/godog` (Cucumber for Go) — BDD feature tests covering all four auth types, both literal and env-var token sources, and the error path for unset env vars
- `auth` block supported in `target_defaults` to apply shared credentials to all file-loaded targets

---

## [1.1.1] - 2026-03-27

### Fixed
- Docs site descriptions (site root, docs root, `hugo.toml`) updated to include `gRPC` alongside HTTP, DNS, WebSocket, and browser
- Metrics reference: `type` label now lists `grpc` as a valid value

---

## [1.1.0] - 2026-03-27

### Added
- `type: grpc` driver — executes unary gRPC calls using server reflection; no `.proto` files required. URL format: `grpc://host:port/Service/Method` (plaintext) or `grpcs://` (TLS). JSON body is unmarshalled to protobuf via reflection. gRPC status codes are mapped to HTTP-like codes so the engine's error classifier and backoff work uniformly. Connections and method descriptors are cached per address.
- `grpc` block in `TargetConfig` and `TargetDefaultsConfig` with fields: `body`, `timeout_s`, `tls`, `insecure`
- gRPC driver documented in Drivers, Configuration, and Dependencies docs pages

---

## [1.0.0] - 2026-03-24

### Added
- `sendit start --tui`: live terminal UI powered by Bubble Tea; displays mode, running time, request counts (total/ok/errors), avg/p95 latency, and a latency sparkline; auto-falls back to plain log output when stdout is not a TTY
- `internal/tui` package: `State` (lock-free shared counters + latency ring buffer), Bubble Tea `model`, and `Run` entry point
- `Engine.SetObserver(fn)`: hook called after every dispatched result, used by the TUI and available for future integrations

### Stability commitment
v1.0.0 marks a compatibility guarantee: CLI flags, config schema, and Prometheus metric names will not have breaking changes without a major version bump.

---

## [0.15.3] - 2026-03-24

### Fixed
- `CLAUDE.md`: correct Go download URL (was `go1.22`, now `go1.24`); add `burst` pacing mode to architecture notes; add `internal/output` and `internal/pcap` to key packages table
- Docs site (`_index.md`): add `burst` to pacing modes description in sections table
- Docs site (`dependencies.md`): remove `howett.net/plist` from direct dependencies table (it is an indirect dependency); correct count from 13 to 12; remove from licence compatibility table
- CI: fuzz job now tolerates Go fuzz engine's `context deadline exceeded` on `-fuzztime` expiry (known Go behaviour, not a test failure); real findings are still caught via corpus entry detection (`scripts/fuzz.sh`)

---

## [0.15.0] - 2026-03-22

### Added
- Test coverage raised from 62% to 71% overall; `internal/metrics` from 44% to 94%
- New unit tests for `detectProbeType`, `probeRcodeLabel`, `probeFormatBytes`, `probeSummary`, `pinchSummary`, `isConnRefused`, `printDryRun` (all pacing modes)
- New unit tests for `chromeBookmarks`, `walkChromeNode`, `firefoxDefaultProfile`, `firefoxFallbackProfile`, `historyDBInfo`
- New unit tests for `metrics.New()` and `metrics.ServeHTTP` (`/healthz` and `/metrics` routes via live server)

---

## [0.14.2] - 2026-03-21

Distribution-only release — no functional changes. Brings the AUR package
up to date with the current latest release after the initial AUR publication
landed on v0.11.2 (out-of-sequence due to unblocking prerequisites).

### Added
- AUR package: `sendit` is now installable via AUR helpers (`yay -S sendit`,
  `paru -S sendit`); GoReleaser generates and pushes a `PKGBUILD` to
  `aur.archlinux.org/sendit.git` on every release; the `.pkg.tar.zst` direct
  install option from v0.11.1 remains available for users without an AUR helper

---

## [0.14.1] - 2026-03-21

### Added
- `mode: burst` pacing mode fires requests at full worker concurrency with no
  inter-request delay; intended for internal infrastructure testing and load
  experiments against services you own
- `pacing.ramp_up_s` config field: optional linear ramp-up for burst mode;
  inter-request delay decreases from ~50 ms × ramp_up_s down to zero over the
  specified number of seconds, preventing a cold-start spike
- `--duration <duration>` flag on `sendit start`: auto-stops the engine after
  the specified wall-clock time (e.g. `--duration 5m`); triggers the same
  graceful shutdown as SIGTERM; **required** when `pacing.mode` is `burst`

### Changed
- Config validation now accepts `mode: burst` and skips the `requests_per_minute`
  check for burst mode (the field is unused in that mode)
- Dry-run output now displays burst mode with ramp-up and duration summary,
  including a reminder that burst is intended for internal infrastructure

---

## [0.14.0] - 2026-03-21

### Added
- Safari bookmarks support in `sendit generate --from-bookmarks safari`: reads
  `~/Library/Safari/Bookmarks.plist` (binary and XML plist formats) using
  `howett.net/plist`; extracts all HTTP/HTTPS bookmark URLs recursively from
  nested folders
- Fixture-based unit tests for browser history and bookmark reading: Chrome-style
  SQLite history, Firefox `places.sqlite` history and bookmarks, and Safari plist
  bookmarks; each test creates a minimal in-process database with known data and
  verifies URL extraction, weight capping, and limit enforcement

---

## [0.13.4] - 2026-03-21

### Added
- Table of contents added to `README.md`, `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`,
  and `ROADMAP.md` using GitHub-compatible anchor links

---

## [0.13.3] - 2026-03-21

### Added
- `docs/content/docs/dependencies.md` — lists all 12 direct dependencies with
  purpose, licence, and alternatives review findings
- `docs/content/docs/ossf.md` — published OpenSSF Best Practices evidence page
  (supersedes the gitignored local working document)

### Changed
- `go mod tidy` confirmed clean — no unused indirect dependencies
- Removed `ossf-gap-audit.md` from `.gitignore`; content published via docs site

---

## [0.13.2] - 2026-03-21

### Added
- Benchmark suite covering the hot paths in the dispatch pipeline:
  - `BenchmarkSelectorPick` (1/10/100 targets) — confirms O(1) Vose alias behaviour
  - `BenchmarkClassifyStatusCode` / `BenchmarkClassifyError` — ~6–8 ns/op, zero allocs
  - `BenchmarkRegistryWait` — token-bucket acquire at unlimited rate (~100 ns/op)
  - `BenchmarkDispatch` — full dispatch cycle with a no-op driver (~1 µs/op)
- `bench` CI job runs `go test -bench=. -benchmem` on every PR and stores
  results as a `bench-results` workflow artifact

### Changed
- Upgraded `actions/upload-artifact` from v4 to v7.0.0 in `bench` CI job
- Replaced `golang/govulncheck-action` with direct `go install + govulncheck ./...`
  to eliminate an internal node20 dependency
- Documented upcoming CodeQL file-coverage-on-PRs behaviour change (April 2026)
  in `security.yml`; accepted as default since Codecov covers per-file PR coverage

---

## [0.13.1] - 2026-03-21

### Added
- Codecov integration: `go test` now runs with `-coverprofile=coverage.txt
  -covermode=atomic` in CI and uploads to [codecov.io](https://codecov.io/gh/lewta/sendit)
  via `codecov/codecov-action@v5.5.3` (SHA-pinned); Codecov badge added to README
- `codecov.yml` — coverage gate: project coverage must not drop more than 2%
  relative to base branch; new code in each PR must be at least 50% covered

---

## [0.12.7] - 2026-03-21

### Changed
- Bumped `github.com/chromedp/chromedp` from 0.14.2 to 0.15.0
- Bumped `modernc.org/sqlite` from 1.46.1 to 1.47.0
- Bumped `github/codeql-action` from 4.32.6 to 4.34.1 (Actions pin)
- Bumped `dorny/paths-filter` from 3.0.2 to 4.0.1 (Actions pin)

---

## [0.12.6] - 2026-03-21

### Changed
- Answered all passing-level criteria on the OpenSSF Best Practices platform,
  bringing the badge to **passing** (99%); `test_most` left as unknown pending
  Codecov integration (v0.13.1)

---

## [0.12.5] - 2026-03-19

### Added
- Five native Go fuzz functions covering the four highest-value input surfaces:
  `FuzzLoad` (config YAML parser), `FuzzSelector` (Vose alias selector),
  `FuzzClassifyError` and `FuzzClassifyStatusCode` (error classifiers),
  `FuzzWriteRecord` (PCAP record encoder)
- `fuzz` CI job runs each target for 30 s on every PR, satisfying the OSSF
  Scorecard `Fuzzing` check

---

## [0.12.4] - 2026-03-19

### Added
- OpenSSF Best Practices badge in `README.md` (project ID 12213 at
  bestpractices.coreinfrastructure.org); satisfies the OSSF Scorecard
  `CII-Best-Practices` check

---

## [0.12.3] - 2026-03-19

### Changed
- Required status checks (`lint`, `test`) added to the `baseline-branch-rule`
  branch ruleset, satisfying the OSSF Scorecard `Branch-Protection` check

### Dependencies
- `golang.org/x/net` → 0.52.0
- `actions/upload-artifact` → v7, `actions/create-github-app-token` → v3,
  `ossf/scorecard-action` → 2.4.3, `github/codeql-action` → v4,
  `actions/attest-build-provenance` → v4

---

## [0.12.2] - 2026-03-13

### Security
- SLSA provenance attestations attached to every release artifact via
  `actions/attest-build-provenance`; verifiable with `gh attestation verify`.
  Satisfies the OSSF Scorecard `Signed-Releases` check

---

## [0.12.1] - 2026-03-13

### Security
- All GitHub Actions dependencies pinned to full commit SHAs across all five
  workflow files; Docker base images pinned to digest in `docker/Dockerfile`.
  Satisfies the OSSF Scorecard `Pinned-Dependencies` check

---

## [0.12.0] - 2026-03-13

### Security
- Workflow token permissions scoped to least privilege across `ci.yml`,
  `release.yml`, and `docs.yml`. Satisfies the OSSF Scorecard
  `Token-Permissions` check

---

## [0.11.1] - 2026-03-13

### Added
- Arch Linux `.pkg.tar.zst` package produced by GoReleaser on every release;
  install with `pacman -U`
- zsh completion installed to `/usr/share/zsh/site-functions/_sendit` for Arch

---

## [0.11.0] - 2026-03-13

### Added
- `sendit generate` command produces a ready-to-use `config.yaml` from a
  targets file, a crawled seed URL, or local browser history/bookmarks
- Flags: `--targets-file`, `--url`, `--crawl`, `--depth`, `--max-pages`,
  `--ignore-robots`, `--from-history`, `--from-bookmarks`, `--history-limit`,
  `--output`

---

## [0.10.6] - 2026-03-12

### Added
- Synthetic PCAP output from per-request telemetry — no root or `CAP_NET_RAW`
  required; uses LINKTYPE_USER0 (147) in pure Go (`internal/pcap`)
- `--capture <file>` flag on `sendit start` writes a PCAP while the engine runs
- `sendit export --pcap <results.jsonl>` converts a previous JSONL result file
  to PCAP post-run

---

## [0.10.5] - 2026-03-12

### Security
- macOS binaries for darwin/amd64 and darwin/arm64 are now code-signed with a
  Developer ID Application certificate and notarized via Apple's App Store
  Connect API using `anchore/quill`; Gatekeeper accepts them without user
  intervention
- Removed the temporary Homebrew caveats workaround added in v0.10.3

---

## [0.10.4] - 2026-03-09

### Security
- Added `SECURITY.md` with supported versions, private reporting process
  (GitHub private advisory), 48 h acknowledgement and 7-day resolution targets,
  and coordinated disclosure policy
- Enabled GitHub private vulnerability reporting
- Enabled Dependabot security-fix PRs
- Added `dismiss_stale_reviews_on_push: true` to branch ruleset
- Added OSSF Scorecard weekly workflow; results published to GitHub Security tab
- Added `docs/content/docs/security.md` and `docs/static/.well-known/security.txt`

---

## [0.10.3] - 2026-03-08

### Fixed
- Homebrew cask and Scoop manifest generation now reads checksums from the
  published release instead of rebuilding archives, ensuring deterministic
  checksums

---

## [0.10.2] - 2026-03-08

### Fixed
- Added `-trimpath` for fully reproducible builds
- Use `CommitDate` instead of `Date` in ldflags for deterministic build output

---

## [0.10.1] - 2026-03-08

### Dependencies
- `github.com/miekg/dns` → 1.1.72
- `github.com/spf13/cobra` → 1.10.2
- `github.com/rs/zerolog` → 1.34.0
- `golang.org/x/time` → 0.14.0
- `actions/upload-pages-artifact` → v4

---

## [0.10.0] - 2026-03-08

### Added
- Homebrew tap: `brew install lewta/tap/sendit` (auto-updated by GoReleaser);
  bundles shell completions for bash, zsh, and fish
- Linux packages: `.deb` and `.rpm` for linux/amd64 and linux/arm64 with shell
  completions and man page
- Scoop bucket: `scoop install lewta/sendit` for Windows users

---

## [0.9.0] - 2026-03-04

### Added
- `sendit probe` now supports `wss://` targets; connects, optionally sends a
  message, waits for a reply, and prints latency per round-trip

---

## [0.8.2] - 2026-03-04

### Fixed
- Switched license badge to a static shields.io URL to avoid intermittent
  shields.io API failures

---

## [0.8.1] - 2026-03-04

### Added
- Expanded OS/arch build matrix: freebsd/amd64, linux/386, linux/armv7
- Documented known Windows limitation (browser driver unavailable on Windows)

---

## [0.8.0] - 2026-03-04

### Added
- `domain` label added to `sendit_requests_total`, `sendit_errors_total`, and
  `sendit_request_duration_seconds` so individual targets can be distinguished
  in Prometheus dashboards

> **Breaking change:** existing dashboards and alerts using these metrics must
> be updated to include the new `domain` label.

---

## [0.7.0] - 2026-03-03

### Added
- Multi-stage `Dockerfile` (`golang:1.24-alpine` builder → `alpine` runtime)
  under `docker/`
- `docker-compose.yml` with optional Prometheus + Grafana sidecars via
  `--profile observability`
- `/healthz` endpoint on the metrics port for container liveness checks
- `--foreground` set by default in the container entrypoint

---

## [0.6.3] - 2026-03-02

### Added
- `sendit pinch <host:port>` subcommand for TCP/UDP port connectivity checks

---

## [0.6.2] - 2026-03-02

### Fixed
- Switched Hugo theme to Lotus Docs v0.2.0; fixes broken home page layout
- Added PR build check for docs to catch Hugo errors before merge

---

## [0.6.1] - 2026-03-02

### Changed
- Switched documentation theme from Docsy to Lotus Docs v0.2.0 for a simpler,
  Node.js-free build

---

## [0.6.0] - 2026-03-02

### Added
- Public documentation site built with Hugo and hosted on GitHub Pages at
  `https://lewta.github.io/sendit/`
- Pages: getting started, configuration reference, pacing modes, drivers,
  metrics, CLI reference
- Automatic deployment on every push to `main`

---

## [0.5.5] - 2026-03-01

### Added
- Unit tests for all CLI commands
- Unit tests for `Engine.Reload()` covering hot-reload behaviour

---

## [0.5.4] - 2026-03-01

### Added
- `sendit reload` subcommand sends `SIGHUP` to a running instance via its PID
  file, making hot-reload a first-class CLI operation

### Fixed
- Migrated WebSocket driver from the deprecated `nhooyr.io/websocket` to its
  maintained fork `github.com/coder/websocket`

---

## [0.5.3] - 2026-03-01

### Fixed
- GoReleaser `archives.format` → `formats` field update (reverted and re-applied
  correctly to avoid breaking the release pipeline)
- govulncheck CI step no longer triggers a duplicate Authorization header error

---

## [0.5.2] - 2026-03-01

### Dependencies
- `github.com/chromedp/chromedp` → 0.14.2
- `github.com/prometheus/client_golang` → 1.23.2
- `github.com/shirou/gopsutil/v3` → 3.24.5
- `github.com/spf13/viper` → 1.21.0
- `github/codeql-action` → v4

---

## [0.5.1] - 2026-02-28

### Added
- Config hot-reload on `SIGHUP`: targets and weights swapped atomically,
  pacing and rate-limit registries updated in-place, diff logged on change
  (originally scoped as v0.4.0)
- Security CI: `govulncheck` scans dependencies against the Go vulnerability
  database; `gosec` SAST linter added to golangci-lint; CodeQL semantic
  analysis added; Dependabot weekly dependency PRs enabled
  (originally scoped as v0.5.0)

### Fixed
- Resolved all gosec findings introduced when enabling the gosec linter

---

## [0.3.2] - 2026-02-28

### Added
- golangci-lint added to CI; runs on every PR

### Fixed
- `gofmt -s` formatting applied across the codebase
- Removed unused `baseYAMLWithoutTargets` constant flagged by staticcheck

---

## [0.3.1] - 2026-02-28

### Fixed
- Release pipeline infrastructure fix

---

## [0.3.0] - 2026-02-28

### Added
- `sendit probe <target>` subcommand for interactive single-target testing
  (like `ping` for web targets)
- Auto-detects driver type from URL scheme (`https://` → http, bare hostname →
  dns)
- Flags: `--type`, `--interval`, `--timeout`, `--resolver`, `--record-type`
- Prints one line per request with status, latency, and bytes (HTTP) or rcode
  (DNS); prints min/avg/max summary on Ctrl-C

---

## [0.2.0] - 2026-02-28

### Added
- `output` config section: `file`, `format` (`jsonl` | `csv`), `append` flag
- Dedicated writer goroutine consumes results non-blocking to the dispatch loop
- File is truncated or appended on startup based on the `append` setting

---

## [0.1.0] - 2026-02-28

### Added
- Four driver types: HTTP, headless browser (chromedp), DNS (miekg/dns),
  WebSocket (coder/websocket)
- Three pacing modes: `human` (uniform random delay), `rate_limited` (token
  bucket), `scheduled` (cron windows with per-window RPM)
- Weighted target selection using the Vose alias method (O(1) picks)
- Prometheus metrics with per-domain rate limiting and decorrelated jitter
  backoff (AWS-style)
- CPU and memory resource gates: dispatch pauses when either threshold is
  exceeded
- `--dry-run` flag to preview effective config without sending traffic
- Integration test suite covering the full dispatch pipeline

---

[Unreleased]: https://github.com/lewta/sendit/compare/v1.2.1...HEAD
[1.2.1]: https://github.com/lewta/sendit/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/lewta/sendit/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/lewta/sendit/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/lewta/sendit/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/lewta/sendit/compare/v0.15.3...v1.0.0
[0.15.3]: https://github.com/lewta/sendit/compare/v0.15.0...v0.15.3
[0.15.0]: https://github.com/lewta/sendit/compare/v0.14.2...v0.15.0
[0.14.2]: https://github.com/lewta/sendit/compare/v0.14.1...v0.14.2
[0.14.1]: https://github.com/lewta/sendit/compare/v0.14.0...v0.14.1
[0.14.0]: https://github.com/lewta/sendit/compare/v0.13.4...v0.14.0
[0.13.4]: https://github.com/lewta/sendit/compare/v0.13.3...v0.13.4
[0.13.3]: https://github.com/lewta/sendit/compare/v0.13.2...v0.13.3
[0.13.2]: https://github.com/lewta/sendit/compare/v0.13.1...v0.13.2
[0.13.1]: https://github.com/lewta/sendit/compare/v0.12.7...v0.13.1
[0.12.7]: https://github.com/lewta/sendit/compare/v0.12.6...v0.12.7
[0.12.6]: https://github.com/lewta/sendit/compare/v0.12.5...v0.12.6
[0.12.5]: https://github.com/lewta/sendit/compare/v0.12.4...v0.12.5
[0.12.4]: https://github.com/lewta/sendit/compare/v0.12.3...v0.12.4
[0.12.3]: https://github.com/lewta/sendit/compare/v0.12.2...v0.12.3
[0.12.2]: https://github.com/lewta/sendit/compare/v0.12.1...v0.12.2
[0.12.1]: https://github.com/lewta/sendit/compare/v0.12.0...v0.12.1
[0.12.0]: https://github.com/lewta/sendit/compare/v0.11.1...v0.12.0
[0.11.1]: https://github.com/lewta/sendit/compare/v0.11.0...v0.11.1
[0.11.0]: https://github.com/lewta/sendit/compare/v0.10.6...v0.11.0
[0.10.6]: https://github.com/lewta/sendit/compare/v0.10.5...v0.10.6
[0.10.5]: https://github.com/lewta/sendit/compare/v0.10.4...v0.10.5
[0.10.4]: https://github.com/lewta/sendit/compare/v0.10.3...v0.10.4
[0.10.3]: https://github.com/lewta/sendit/compare/v0.10.2...v0.10.3
[0.10.2]: https://github.com/lewta/sendit/compare/v0.10.1...v0.10.2
[0.10.1]: https://github.com/lewta/sendit/compare/v0.10.0...v0.10.1
[0.10.0]: https://github.com/lewta/sendit/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/lewta/sendit/compare/v0.8.2...v0.9.0
[0.8.2]: https://github.com/lewta/sendit/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/lewta/sendit/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/lewta/sendit/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/lewta/sendit/compare/v0.6.3...v0.7.0
[0.6.3]: https://github.com/lewta/sendit/compare/v0.6.2...v0.6.3
[0.6.2]: https://github.com/lewta/sendit/compare/v0.6.1...v0.6.2
[0.6.1]: https://github.com/lewta/sendit/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/lewta/sendit/compare/v0.5.5...v0.6.0
[0.5.5]: https://github.com/lewta/sendit/compare/v0.5.4...v0.5.5
[0.5.4]: https://github.com/lewta/sendit/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/lewta/sendit/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/lewta/sendit/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/lewta/sendit/compare/v0.3.2...v0.5.1
[0.3.2]: https://github.com/lewta/sendit/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/lewta/sendit/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/lewta/sendit/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/lewta/sendit/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/lewta/sendit/releases/tag/v0.1.0
