# Roadmap

Features planned for future releases of sendit. Contributions are welcome — open an issue to discuss before starting work on a large item.

---

## v0.1.0 — Initial release ✓

- Four driver types: HTTP, headless browser (chromedp), DNS, WebSocket
- Three pacing modes: `human` (random delay), `rate_limited` (token bucket), `scheduled` (cron windows)
- Weighted target selection using the Vose alias method (O(1) picks)
- Prometheus metrics with per-domain rate limiting and decorrelated jitter backoff
- CPU and memory resource gates that pause dispatch when thresholds are exceeded
- `--dry-run` flag to preview effective config before sending traffic
- Integration test suite covering the full dispatch pipeline

---

## v0.2.0 — Result export ✓

Write request results to a file for offline analysis, complementing the Prometheus scrape endpoint.

- New `output` config section: `file`, `format` (`jsonl` | `csv`), `append` (bool)
- A dedicated writer goroutine consumes results non-blocking to the dispatch loop
- Truncates or appends on startup based on the `append` flag

---

## v0.3.0 — Probe command ✓

A `sendit probe <target>` subcommand for interactively testing a single HTTP or DNS endpoint
in a loop — no config file needed. Works like `ping` for web targets.

- Auto-detects type from URL scheme (`https://` → http, bare hostname → dns)
- `--type`, `--interval`, `--timeout`, `--resolver`, `--record-type` flags
- Prints one line per request with status, latency, and bytes (HTTP) or rcode (DNS)
- Prints a summary (sent, ok, errors, min/avg/max latency) on Ctrl-C

```
$ sendit probe https://example.com

Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
^C

--- https://example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 90ms / 142ms
```

---

## v0.4.0 — Config hot-reload ✓

Reload configuration on `SIGHUP` without restarting the process or dropping in-flight requests.

- Targets and weights swapped atomically via the existing `task.Selector`
- Pacing, rate-limit, and backoff registries updated in-place where possible
- Logs a diff of what changed (added/removed targets, updated limits)

---

## v0.5.0 — Security CI ✓

Automated security scanning integrated into every PR and a weekly scheduled run.

- **`govulncheck`** — scans all dependencies against the Go vulnerability database (vuln.go.dev); fails the build on any known CVE
- **`gosec`** — SAST linter added to golangci-lint; checks for insecure patterns in source code (weak crypto, command injection, file permission issues, etc.)
- **CodeQL** — GitHub's semantic analysis engine; results surface in the repository Security tab
- **Dependabot** — weekly automated PRs for stale Go module and GitHub Actions dependencies

---

## Pending patches ✓

Small improvements tracked as GitHub issues that will ship as patch releases before the next minor version.

- **WebSocket driver migration** ✓ — migrate `internal/driver/websocket.go` from the deprecated `nhooyr.io/websocket` to its maintained fork `github.com/coder/websocket` ([#23](https://github.com/lewta/sendit/issues/23))
- **`sendit reload` command** ✓ — send `SIGHUP` to a running instance via its PID file, making hot-reload a first-class CLI operation consistent with `sendit stop` ([#26](https://github.com/lewta/sendit/issues/26))

---

## v0.6.0 — Documentation site

Public reference documentation hosted on GitHub Pages.

- Built with [Hugo](https://gohugo.io), source under `docs/`
- Pages: getting started, configuration reference, pacing modes, drivers, metrics, CLI reference
- Deployed automatically on every push to `main` via GitHub Actions

---

## v0.7.0 — Container support

Package sendit as a Docker image for portability and scheduled runs in CI or on a server.

- Multi-stage `Dockerfile`: `golang:1.22-alpine` builder → `alpine` runtime
- `docker-compose.yml` with optional Prometheus + Grafana sidecars for out-of-the-box dashboards
- Config mounted as a volume so the image stays generic
- `--foreground` set by default in the entrypoint (PID files are not useful inside a container)
- `/healthz` endpoint on the metrics port for container liveness checks

---

## v0.8.0 — Observability improvements

Better visibility into per-target behaviour from Prometheus metrics.

- Add a `domain` label to `sendit_requests_total`, `sendit_errors_total`, and `sendit_request_duration_seconds` so individual targets can be distinguished in dashboards
- Note: this is a breaking change to existing metric label sets — update any dashboards or alerts accordingly

---

## v0.9.0 — Probe WebSocket + distribution

Complete driver coverage in the probe tool and make sendit easy to install.

- **Probe WebSocket** — extend `sendit probe` to support `wss://` targets; connects, optionally sends a message, waits for a reply, and prints latency per round-trip
- **Homebrew tap** — `brew install lewta/tap/sendit` as a distribution channel; tap repo auto-updated by GoReleaser on each release

---

## v1.0.0 — TUI + stable API

Terminal dashboard and commitment to a stable public API.

- Live terminal UI using [Bubble Tea](https://github.com/charmbracelet/bubbletea) behind a `--tui` flag; plain log output remains the default
- Graceful fallback to plain logs when stdout is not a TTY
- v1.0.0 marks a stability commitment: CLI flags, config schema, and Prometheus metric names will not have breaking changes without a major version bump

```
┌─ sendit ──────────────────────────────────────────────────┐
│ mode: human   workers: 2/4   uptime: 00:04:32             │
├───────────────────────────────────────────────────────────┤
│ RECENT REQUESTS                                           │
│  200  GET  https://httpbin.org/get          142ms  12 KB  │
│  200  DNS  example.com                        4ms         │
│  429  GET  https://httpbin.org/status/429   201ms  ↩ 8s   │
│  200  GET  https://httpbin.org/get           98ms   9 KB  │
├───────────────────────────────────────────────────────────┤
│ TOTALS          requests: 312   errors: 4   bytes: 1.1 MB │
│ RATE LIMITS     httpbin.org ████░░ 0.8 rps               │
└───────────────────────────────────────────────────────────┘
```
