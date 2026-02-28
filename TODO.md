# TODO

Remaining improvements identified during the design/performance review.
Items 1-6 and 8 have been implemented (commits 197655e, c59b3dd).

---

## Tier 1 — Foundational (do first)

### ~~A. Naming and rebranding~~ ✓ DONE
Project renamed to `sendit`. Module path, binary, CLI, metrics prefix, PID file, and all
import paths updated. GitHub repo: https://github.com/lewta/sendit (branch: main).

### ~~7. Reuse `dns.Client` across queries~~ ✓ DONE
Client allocated once in `NewDNSDriver()` and reused across all `Execute` calls.

### ~~8. Fix unsafe type assertion on `Scheduler.limiter`~~ ✓ DONE
Switched to `atomic.Pointer[rate.Limiter]` — nil by default, no type assertion needed.

---

## Tier 2 — Core quality for distribution

### ~~C. Integration / smoke tests~~ ✓ DONE
Six `//go:build integration` tests in `internal/engine/integration_test.go` exercise the full
dispatch pipeline against real local servers (httptest, miekg/dns stub, nhooyr.io WebSocket).
Tests cover: HTTP happy path, 429 backoff/retry, graceful shutdown, resource gate (CPU=0),
DNS A-record query, and WebSocket connection. Run with:
```sh
go test -tags integration -race -v ./internal/engine/...
```

### ~~B. Dry-run mode~~ ✓ DONE
`--dry-run` flag added to `sendit start`. Prints target breakdown (url, type, weight, effective %),
pacing parameters for the configured mode, and concurrency/resource limits, then exits without
making any requests.

### ~~9. Use a measurement interval in `cpu.Percent`~~ ✓ DONE
Passing `200 * time.Millisecond` to `cpu.Percent` for stable, non-noisy readings.

### ~~10. Isolate Prometheus registry in `metrics.New()`~~ ✓ DONE
`New()` now registers on a fresh `prometheus.NewRegistry()`. `ServeHTTP` promoted to a method on `Metrics` and uses `promhttp.HandlerFor` with the isolated registry.

---

## Tier 3 — Meaningful features for distribution

### F. Container deployment
Package the tool as a Docker image for easy portability and scheduled runs in CI or on a server.

- `Dockerfile` — multi-stage build: `golang:1.22-alpine` builder → `alpine` runtime image
- `docker-compose.yml` — optional Prometheus + Grafana sidecar for out-of-the-box metrics dashboards
- Config mounted as a volume (`-v ./config:/config`) so the image stays generic
- Pass `--foreground` by default in the entrypoint (PID files have no use inside a container)
- Document `docker run` and compose usage in README

### E. Output / result export
Write request results to a file in addition to (or instead of) logging, for offline analysis.

- Add `output` section to config: `file`, `format` (`jsonl` | `csv`), `append` (bool)
- Each `task.Result` is serialised and written by a dedicated writer goroutine (non-blocking to dispatch)
- Rotate or truncate on startup based on `append` flag
- Complements Prometheus metrics for environments where a scrape endpoint is inconvenient

### D. Config hot-reload
Reload configuration on SIGHUP without restarting the process, preserving in-flight requests.

- Engine receives the new `Config` and diffs it against the running state
- Targets and weights are swapped atomically via `task.Selector`
- Pacing, rate-limit, and backoff registries are updated in-place where possible; recreated where not
- Log a summary of what changed (added/removed targets, updated limits)
- Document the `kill -HUP <pid>` workflow in README

---

## Tier 3.5 — Docs site

### H. Documentation site (Hugo)
Build a public docs site using [Hugo](https://gohugo.io) and host it on GitHub Pages.

Structure:
- `docs/` — Hugo site root (content, layouts, static assets, hugo.toml)
- Pages to write:
  - **Getting started** — install, build, first run
  - **Configuration reference** — all YAML fields with defaults and examples (mirrors README but browsable)
  - **Pacing modes** — human / rate_limited / scheduled with diagrams
  - **Drivers** — HTTP, browser, DNS, WebSocket behaviour and options
  - **Metrics** — Prometheus metric names, labels, and example queries
  - **CLI reference** — start / stop / status / validate / completion flags

Deployment:
- GitHub Actions workflow (`.github/workflows/docs.yml`) that runs `hugo --minify` and publishes to `gh-pages` branch on every push to `main`
- Set GitHub Pages source to the `gh-pages` branch in repo settings
- Custom domain optional — default will be `https://lewta.github.io/sendit`

Notes:
- Choose a clean Hugo theme suited to CLI/developer tools (e.g. Docsy, Geekdoc, or PaperMod)
- Keep the README as the quick-start; the docs site is the full reference
- Add a "docs" badge to the README linking to the site once live

---

## Tier 4 — Revisit later

### G. TUI (terminal dashboard)
Replace plain log output with a live terminal UI using [Bubble Tea](https://github.com/charmbracelet/bubbletea) that shows traffic activity at a glance.

Proposed layout:
```
┌─ sendit ──────────────────────────────────────────────┐
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

Implementation notes:
- Add `--tui` flag to `sendit start`; plain log output remains the default
- Engine publishes `task.Result` events to a buffered channel; TUI model reads from it
- Keep TUI optional — no Bubble Tea dependency pulled in unless the flag is used (build tag or lazy init)
- Graceful fallback to plain logs when stdout is not a TTY
