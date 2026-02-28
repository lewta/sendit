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

## v0.3.0 — Config hot-reload

Reload configuration on `SIGHUP` without restarting the process or dropping in-flight requests.

- Targets and weights swapped atomically via the existing `task.Selector`
- Pacing, rate-limit, and backoff registries updated in-place where possible
- Logs a diff of what changed (added/removed targets, updated limits)

---

## v0.4.0 — Container support

Package sendit as a Docker image for portability and scheduled runs in CI or on a server.

- Multi-stage `Dockerfile`: `golang:1.22-alpine` builder → `alpine` runtime
- `docker-compose.yml` with optional Prometheus + Grafana sidecars for out-of-the-box dashboards
- Config mounted as a volume so the image stays generic
- `--foreground` set by default in the entrypoint (PID files are not useful inside a container)

---

## v0.5.0 — Documentation site

Public reference documentation hosted on GitHub Pages.

- Built with [Hugo](https://gohugo.io), source under `docs/`
- Pages: getting started, configuration reference, pacing modes, drivers, metrics, CLI reference
- Deployed automatically on every push to `main` via GitHub Actions

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
