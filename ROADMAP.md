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

## v0.6.0 — Documentation site ✓

Public reference documentation hosted on GitHub Pages.

- Built with [Hugo](https://gohugo.io), source under `docs/`
- Pages: getting started, configuration reference, pacing modes, drivers, metrics, CLI reference
- Deployed automatically on every push to `main` via GitHub Actions

---

## v0.7.0 — Container support ✓

Package sendit as a Docker image for portability and scheduled runs in CI or on a server.

- Multi-stage `Dockerfile`: `golang:1.24-alpine` builder → `alpine` runtime (files under `docker/`)
- `docker-compose.yml` with optional Prometheus + Grafana sidecars via `--profile observability`
- Config mounted as a volume so the image stays generic
- `--foreground` set by default in the entrypoint (PID files are not useful inside a container)
- `/healthz` endpoint on the metrics port for container liveness checks

---

## v0.8.0 — Observability improvements

Better visibility into per-target behaviour from Prometheus metrics.

- Add a `domain` label to `sendit_requests_total`, `sendit_errors_total`, and `sendit_request_duration_seconds` so individual targets can be distinguished in dashboards
- Note: this is a breaking change to existing metric label sets — update any dashboards or alerts accordingly

---

## v0.9.0 — Probe WebSocket

Complete driver coverage in the probe tool.

- Extend `sendit probe` to support `wss://` targets; connects, optionally sends a message, waits for a reply, and prints latency per round-trip

---

## v0.10.0 — Distribution

Make sendit easy to install without building from source.

- **Homebrew tap** — `brew install lewta/tap/sendit` as a distribution channel; tap repo auto-updated by GoReleaser on each release

---

## v0.11.0 — Config generator

A `sendit generate` subcommand that produces a ready-to-use `config.yaml` from a targets file or a seed URL, reducing the time-to-first-traffic for new users.

- **From a targets file** — parse an existing `targets_file` (url + type + optional weight, one per line) and emit a full `config.yaml` with sensible defaults for pacing, limits, backoff, and per-target driver settings
- **From a seed URL (`--crawl`)** — for HTTP targets, optionally crawl the seed domain up to a configurable depth/page limit, discover in-domain links, and add each unique path as a weighted `http` target; respects `robots.txt` by default (`--ignore-robots` to override)
- **From browser history (`--from-history`)** — read the local browser history database and emit all visited HTTP/HTTPS URLs as weighted `http` targets; weight derived from visit count so frequently visited pages appear more often in traffic (see Research item below)
- **From browser bookmarks (`--from-bookmarks`)** — read the local browser bookmarks file and emit bookmarked HTTP/HTTPS URLs as equally-weighted `http` targets
- **Output** — writes to stdout by default; `--output <file>` writes to a file, prompting before overwriting
- **Flags**:
  - `--targets-file <path>` — generate from an existing targets file
  - `--url <url>` — seed URL for crawl-based generation (implies `--crawl`)
  - `--crawl` — enable in-domain page discovery for HTTP targets
  - `--depth <n>` — maximum crawl depth (default: `2`)
  - `--max-pages <n>` — maximum number of pages to discover (default: `50`)
  - `--ignore-robots` — skip `robots.txt` enforcement during crawl
  - `--from-history <browser>` — harvest visited URLs from local browser history (`chrome` | `firefox` | `safari`)
  - `--from-bookmarks <browser>` — harvest bookmarked URLs from local browser bookmarks (`chrome` | `firefox` | `safari`)
  - `--history-limit <n>` — cap the number of URLs imported from history (default: `100`, ordered by visit count descending)
  - `--output <file>` — write config to a file instead of stdout

Example:

```sh
# From a targets file
sendit generate --targets-file config/targets.txt > config/generated.yaml

# From a seed URL with crawling
sendit generate --url https://example.com --crawl --depth 2 --output config/generated.yaml

# From Chrome history (top 50 most-visited pages)
sendit generate --from-history chrome --history-limit 50 --output config/generated.yaml

# From Firefox bookmarks
sendit generate --from-bookmarks firefox --output config/generated.yaml
```

**Documentation deliverables** (required as part of the same release):

- **CLI help** — `Use`, `Short`, and `Long` descriptions on the `generate` command and all flags, consistent with the style of `probe` and `pinch`
- **`README.md`** — add `sendit generate` to the CLI commands usage block and command table; add a Generate section with both usage modes and example output, alongside the existing Probe and Pinch sections
- **`docs/content/docs/cli.md`** — add `generate` to the commands block and table; add a `generate` flags section with both modes, flag reference, and annotated example output
- **`docs/content/docs/getting-started.md`** — add a "Generate a config from a URL" subsection under the quick-start flow so new users discover the crawl mode as the fastest path to a working config

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

---

## Research — Non-standard traffic driver

Investigate adding a driver for non-standard or application-layer protocols that don't fit the existing HTTP/DNS/WebSocket/browser model.

Areas to explore:
- Protocol candidates: gRPC, raw TCP, ICMP, SMTP, FTP, custom binary protocols
- Whether a generic `raw` driver with a user-supplied payload and framing spec is preferable to per-protocol drivers
- How RCODEs / response codes map to the existing unified error classifier
- Connection pooling and state management for connection-oriented protocols
- What a config schema for non-HTTP targets looks like (no URL scheme, port-based, payload templating)

---

## Research — Aggressive / burst pacing mode

Investigate a `burst` or `aggressive` pacing mode for scenarios where politeness constraints should be relaxed — load testing, internal infrastructure, or controlled chaos experiments.

Areas to explore:
- A `burst` mode that fires requests as fast as worker slots allow with no inter-request delay
- Configurable concurrency ramp-up (e.g. linearly increase workers to max over a warm-up period)
- Whether the existing resource gate (`cpu_threshold_pct`, `memory_threshold_mb`) is sufficient protection or needs a hard cap on total requests/duration
- A `--duration` flag for `start` that auto-stops after a fixed wall-clock time, useful for timed load runs
- How backoff and per-domain rate limits interact with burst mode (bypass, warn, or error)

---

## Research — Browser history and bookmarks harvesting

Investigate the feasibility of reading local browser history and bookmarks as input sources for `sendit generate --from-history` and `--from-bookmarks` (planned for v0.11.0). Related to [#49](https://github.com/lewta/sendit/issues/49) — the same browser automation knowledge applies to both driving traffic and sourcing targets.

Areas to explore:

- **Chrome / Chromium history** — `History` SQLite file (`urls` table, `visit_count` column) located at:
  - Linux: `~/.config/google-chrome/Default/History`
  - macOS: `~/Library/Application Support/Google/Chrome/Default/History`
  - Chrome must be closed or the file opened read-only (SQLite WAL mode may allow concurrent reads)
- **Chrome bookmarks** — `Bookmarks` JSON file in the same `Default/` directory; parse the `roots` tree recursively to extract `url` entries
- **Firefox history** — `places.sqlite` (`moz_places` table, `visit_count` column) at `~/.mozilla/firefox/<profile>/places.sqlite`; bookmarks share the same file via `moz_bookmarks`
- **Firefox profile discovery** — `profiles.ini` in the Firefox config dir; the default profile must be auto-detected when no explicit path is given
- **Safari** — history in `~/Library/Safari/History.db` (SQLite); bookmarks in `~/Library/Safari/Bookmarks.plist` (binary plist); macOS only
- **Cross-platform path resolution** — abstract browser profile paths behind an OS+browser lookup so the same flag works on Linux and macOS without manual path configuration
- **Filtering** — HTTP/HTTPS URLs only; strip query strings and fragments optionally; de-duplicate by normalised URL; respect `--history-limit`
- **Weight derivation** — map `visit_count` to a target weight (e.g. log-scaled) so high-traffic pages appear more frequently in generated traffic without dominating the distribution entirely
- **Privacy considerations** — document that history/bookmark data never leaves the local machine; the generated `config.yaml` contains only URLs, not browsing metadata

## Research — Repository security hardening

Review and enable GitHub's built-in security features to give the project a clear vulnerability disclosure process and broader automated dependency scanning.

Areas to explore:
- **Security policy** — add a `SECURITY.md` defining the supported versions and the process for reporting vulnerabilities (e.g. email or GitHub private reporting)
- **Private vulnerability disclosure** — enable GitHub's private vulnerability reporting feature so reporters can submit CVEs without opening a public issue; evaluate whether the default advisory workflow fits the project
- **Dependabot alerts** — confirm Dependabot security alerts are enabled (distinct from the Dependabot version-update PRs already in place); review alert thresholds and whether auto-dismiss rules are appropriate
- **Branch protection hardening** — review current branch protection rules on `main` for gaps (e.g. required signed commits, dismiss stale reviews on push)
- **OSSF Scorecard** — evaluate adding the OpenSSF Scorecard action to surface a public supply-chain security score
- **Docs site — security page** — add a dedicated Security page to the docs site summarising the security policy, supported versions, and how to report a vulnerability; link from the homepage and CLI reference
- **Docs site — `security.txt`** — add a `/.well-known/security.txt` (RFC 9116) to the GitHub Pages site (`docs/static/.well-known/security.txt`) so automated scanners and researchers can discover the disclosure contact and policy URL machine-readably
