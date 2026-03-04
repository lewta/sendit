# sendit

[![CI](https://img.shields.io/github/actions/workflow/status/lewta/sendit/ci.yml?branch=main&label=tests)](https://github.com/lewta/sendit/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lewta/sendit)](https://github.com/lewta/sendit/releases/latest)
[![Go version](https://img.shields.io/badge/go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Go Report Card](https://goreportcard.com/badge/github.com/lewta/sendit)](https://goreportcard.com/report/github.com/lewta/sendit)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

A Go CLI tool that simulates realistic user web traffic across HTTP, headless browser, DNS, and WebSocket protocols. Designed to blend into normal traffic baselines while being polite to both the local machine and target servers.

Key properties:

- Never bursts aggressively — all pacing is delay-gated before acquiring worker slots
- Per-domain token-bucket rate limits with decorrelated jitter backoff on transient errors
- Pauses dispatch when local CPU or RAM exceeds configurable thresholds
- Graceful shutdown: waits for all in-flight requests to complete on SIGINT/SIGTERM

---

## Quick Start

### Prerequisites

- Go 1.24+
- Chrome/Chromium (only required for `type: browser` targets)

### Build

```sh
git clone https://github.com/lewta/sendit
cd sendit
go build -o sendit ./cmd/sendit
```

### Test an endpoint without a config file

`sendit probe` works with no config — it auto-detects HTTP from `https://` and DNS from a bare hostname:

```sh
# HTTP
./sendit probe https://example.com

# DNS
./sendit probe example.com
```

### Validate your config

```sh
./sendit validate --config config/example.yaml
# config valid
```

### Run

```sh
./sendit start --config config/example.yaml --log-level debug
```

### Run with a targets file

Rather than listing every target in the YAML, you can point `targets_file` at a plain-text list:

```sh
# config/my-targets.txt
https://example.com   http   5
example.com           dns    2
```

```yaml
# config/simple.yaml
targets_file: "config/my-targets.txt"
target_defaults:
  http:
    timeout_s: 15
  dns:
    resolver: "8.8.8.8:53"
```

```sh
./sendit validate --config config/simple.yaml   # check the file parses cleanly
./sendit start   --config config/simple.yaml --log-level debug
```

---

## CLI Commands

```
sendit start    [-c <path>] [--foreground] [--log-level debug|info|warn|error] [--dry-run]
sendit probe    <target>   [--type http|dns|websocket] [--interval 1s] [--timeout 5s] [--send <msg>]
sendit pinch    <host:port> [--type tcp|udp] [--interval 1s] [--timeout 5s]
sendit stop     [--pid-file <path>]
sendit reload   [--pid-file <path>]
sendit status   [--pid-file <path>]
sendit validate [-c <path>]
sendit version
sendit completion <shell>
```

| Command      | Description |
|--------------|-------------|
| `start`      | Start the engine. Writes a PID file by default so `stop`/`status` can find the process; use `--foreground` to skip writing the PID file. |
| `probe`      | Test a single HTTP, DNS, or WebSocket endpoint in a loop (like ping). No config file required. |
| `pinch`      | Check whether a TCP or UDP port is open on a remote host, repeating on an interval. No config file required. |
| `stop`       | Send SIGTERM to a running instance via its PID file. |
| `reload`     | Send SIGHUP to a running instance via its PID file to reload the config atomically. Not available on Windows — use a full restart instead. |
| `status`     | Check whether the process in the PID file is still alive. |
| `validate`   | Parse and validate a config file without starting the engine. Exits 0 on success, non-zero with a message on failure. |
| `version`    | Print version, commit, and build date. |
| `completion` | Generate shell autocompletion scripts (bash, zsh, fish, powershell). |

### `start` flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |
| `--foreground` | | `false` | Skip writing the PID file (process always runs in the foreground) |
| `--log-level` | | *(from config)* | Override log level: `debug` \| `info` \| `warn` \| `error` |
| `--dry-run` | | `false` | Print config summary (targets, pacing, limits) and exit without sending traffic |

### `probe` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | *(auto-detected)* | Driver type: `http` \| `dns` \| `websocket` |
| `--interval` | `1s` | Delay between requests |
| `--timeout` | `5s` | Per-request timeout |
| `--resolver` | `8.8.8.8:53` | DNS resolver (dns targets only) |
| `--record-type` | `A` | DNS record type (dns targets only) |
| `--send` | `""` | Message to send after connecting (websocket only); waits for one reply and reports round-trip latency |

### `pinch` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--type` | `tcp` | Protocol type: `tcp` \| `udp` |
| `--interval` | `1s` | Delay between checks |
| `--timeout` | `5s` | Per-check timeout |

### `stop` / `reload` / `status` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--pid-file` | `/tmp/sendit.pid` | Path to the PID file written by `start` |

### `validate` flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |

---

## Dry-run mode

Pass `--dry-run` to `sendit start` to preview the effective configuration — target weights, pacing parameters, and resource limits — without sending any traffic:

```sh
./sendit start --config config/example.yaml --dry-run
```

```
Config: config/example.yaml  ✓ valid

Targets (4):
  URL                                      TYPE       WEIGHT     SHARE
  https://httpbin.org/get                  http       10         47.6%
  https://httpbin.org/status/200           http       5          23.8%
  https://news.ycombinator.com             browser    3          14.3%
  example.com                              dns        3          14.3%
  Total weight: 21

Pacing:
  mode: human | delay: 800ms–8000ms (random uniform)

Limits:
  workers: 4 (browser: 1) | cpu: 60% | memory: 512 MB
```

---

## Probe

`sendit probe <target>` tests a single HTTP, DNS, or WebSocket endpoint in a loop with no config file. Press Ctrl-C to stop and print a summary.

**Type auto-detection:**

| Target format | Detected type |
|---|---|
| `https://example.com` | `http` |
| `http://example.com` | `http` |
| `wss://example.com` | `websocket` |
| `ws://example.com` | `websocket` |
| `example.com` | `dns` |

Override with `--type http`, `--type dns`, or `--type websocket`.

**HTTP example:**

```sh
./sendit probe https://example.com
```

```
Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
  200   503ms  1.2 KB
^C

--- https://example.com ---
3 sent, 3 ok, 0 error(s)
min/avg/max latency: 38ms / 227ms / 503ms
```

**DNS example:**

```sh
./sendit probe example.com --record-type A --resolver 1.1.1.1:53
```

```
Probing example.com (dns, A @ 1.1.1.1:53) — Ctrl-C to stop

  NOERROR    12ms
  NOERROR     8ms
  NOERROR    11ms
^C

--- example.com ---
3 sent, 3 ok, 0 error(s)
min/avg/max latency: 8ms / 10ms / 12ms
```

**WebSocket example (connect only):**

```sh
./sendit probe wss://echo.websocket.org
```

```
Probing wss://echo.websocket.org (websocket, connect only) — Ctrl-C to stop

  101    38ms
  101    41ms
  101    36ms
^C

--- wss://echo.websocket.org ---
3 sent, 3 ok, 0 error(s)
min/avg/max latency: 36ms / 38ms / 41ms
```

**WebSocket example (send + receive round-trip):**

```sh
./sendit probe wss://echo.websocket.org --send 'ping'
```

```
Probing wss://echo.websocket.org (websocket, send+recv) — Ctrl-C to stop

  101    42ms
  101    39ms
  101    44ms
^C

--- wss://echo.websocket.org ---
3 sent, 3 ok, 0 error(s)
min/avg/max latency: 39ms / 41ms / 44ms
```

---

## Pinch

`sendit pinch <host:port>` checks whether a TCP or UDP port is open on a remote host, repeating on an interval. Press Ctrl-C to stop and print a summary. No config file required.

**TCP example:**

```sh
./sendit pinch example.com:80
```

```
Pinching example.com:80 (tcp) — Ctrl-C to stop

  open            142ms
  open             38ms
  closed            0ms  connection refused
^C

--- example.com:80 ---
3 sent, 2 open, 1 closed/filtered
min/avg/max latency: 38ms / 90ms / 142ms
```

**UDP example:**

```sh
./sendit pinch 8.8.8.8:53 --type udp
```

```
Pinching 8.8.8.8:53 (udp) — Ctrl-C to stop

  open              4ms
  open|filtered     5s   (no response within timeout)
^C

--- 8.8.8.8:53 ---
2 sent, 1 open, 1 closed/filtered
min/avg/max latency: 4ms / 4ms / 4ms
```

**Status labels:**

| Label | Protocol | Meaning |
|---|---|---|
| `open` | TCP | Connection accepted |
| `closed` | TCP | Connection refused |
| `filtered` | TCP | No response (deadline exceeded) |
| `open` | UDP | Response data received |
| `closed` | UDP | ICMP port unreachable received |
| `open\|filtered` | UDP | Timeout — UDP is inherently ambiguous |

---

## Docker

The `docker/` directory contains a ready-to-use Docker setup. The image is built from source so no binary download is needed.

### Quick start

```sh
# 1. Copy and edit the example config
cp docker/config.yaml docker/my-config.yaml

# 2. Build and run (config is mounted as a volume)
cd docker
docker compose up --build
```

Prometheus metrics are exposed on port **9090**. A liveness probe is available at `GET /healthz` on the same port.

### With Prometheus + Grafana

```sh
cd docker
docker compose --profile observability up --build
```

This starts three containers:

| Service | Port | Description |
|---------|------|-------------|
| `sendit` | 9090 | Main process + `/metrics` + `/healthz` |
| `prometheus` | 9091 | Scrapes sendit every 15 s |
| `grafana` | 3000 | Dashboard UI (anonymous access pre-enabled) |

### Config

Mount your config at `/etc/sendit/config.yaml`. For container deployments, set:

```yaml
metrics:
  enabled: true
  prometheus_port: 9090

daemon:
  log_format: json   # friendlier for log aggregators
```

The `--foreground` flag is set in the image entrypoint — PID files are not useful inside containers.

### Files

| File | Description |
|------|-------------|
| `docker/Dockerfile` | Multi-stage build (`golang:1.24-alpine` → `alpine`) |
| `docker/docker-compose.yml` | sendit + optional Prometheus/Grafana via `--profile observability` |
| `docker/config.yaml` | Docker-ready example config (metrics enabled, JSON logs) |
| `docker/prometheus.yml` | Prometheus scrape config targeting `sendit:9090` |

---

## Configuration Reference

See [`config/example.yaml`](config/example.yaml) for a full working example. Every section has defaults so you only need to specify what you want to override.

### `pacing`

Controls how requests are spaced in time.

| Field | Default | Description |
|-------|---------|-------------|
| `mode` | `human` | `human` \| `rate_limited` \| `scheduled` |
| `requests_per_minute` | `20` | Target RPM — used by `rate_limited` and `scheduled` modes only |
| `jitter_factor` | `0.4` | Unused in `human` mode; reserved for future modes |
| `min_delay_ms` | `800` | Minimum inter-request delay in `human` mode |
| `max_delay_ms` | `8000` | Maximum inter-request delay in `human` mode |
| `schedule` | `[]` | List of cron windows — required when `mode: scheduled` |

**Pacing modes:**

- **`human`** — random delay per request uniformly sampled from `[min_delay_ms, max_delay_ms]`. `requests_per_minute` and `jitter_factor` are ignored in this mode.
- **`rate_limited`** — token-bucket limiter at `requests_per_minute` plus a small random jitter after each token.
- **`scheduled`** — cron expressions open active windows; within each window behaves like `rate_limited` at the window's own RPM. Dispatch is paused between windows.

```yaml
pacing:
  mode: scheduled
  schedule:
    - cron: "0 9 * * 1-5"      # weekdays 09:00
      duration_minutes: 30
      requests_per_minute: 40
```

### `limits`

Concurrency and resource thresholds.

| Field | Default | Description |
|-------|---------|-------------|
| `max_workers` | `4` | Maximum simultaneous requests across all driver types |
| `max_browser_workers` | `1` | Sub-limit for concurrent headless browser instances |
| `cpu_threshold_pct` | `60.0` | Pause dispatch when CPU usage exceeds this percentage |
| `memory_threshold_mb` | `512` | Pause dispatch when RAM in use exceeds this value in MB |

> **Note:** `memory_threshold_mb` defaults to 512 MB, which is below baseline usage on most modern machines. Set this to a value above your system's idle memory footprint (e.g. `8192` for a 16 GB machine) to avoid blocking dispatch entirely.

### `rate_limits`

Per-domain token buckets applied after the pacing delay and before acquiring a worker slot.

| Field | Default | Description |
|-------|---------|-------------|
| `default_rps` | `0.5` | Requests per second applied to all domains not listed in `per_domain` |
| `per_domain` | `[]` | List of `{domain, rps}` overrides |

```yaml
rate_limits:
  default_rps: 0.5
  per_domain:
    - domain: "example.com"
      rps: 0.2
```

### `backoff`

Retry behaviour on transient errors (HTTP 429, 502, 503, 504, DNS SERVFAIL, network failures).

| Field | Default | Description |
|-------|---------|-------------|
| `initial_ms` | `1000` | Base delay for the first retry, in milliseconds |
| `max_ms` | `120000` | Maximum delay cap, in milliseconds |
| `multiplier` | `2.0` | Exponential growth factor per attempt |
| `max_attempts` | `3` | Stop retrying after this many consecutive failures for a domain |

Permanent errors (HTTP 400, 403, 404; DNS NXDOMAIN, REFUSED) are logged and skipped immediately with no retry. Context cancellation errors are dropped silently.

### `targets_file` and `target_defaults`

Instead of (or in addition to) listing targets inline, you can point `targets_file` at a plain-text file of URL/type pairs. Targets from the file are appended to any inline `targets` entries, so both can be used together.

**File format** — one entry per line:

```
<url> <type> [weight]
```

- `url` — full URL (`https://`, `wss://`) or a bare hostname for DNS targets
- `type` — one of `http` | `browser` | `dns` | `websocket`
- `weight` — optional positive integer; defaults to `target_defaults.weight` when omitted
- Lines starting with `#` and blank lines are ignored

```
# config/targets.txt
https://example.com      http   5
https://api.example.com  http   3
example.com              dns    2
wss://ws.example.com     websocket
```

**`target_defaults`** supplies the remaining fields (driver settings, default weight) for every target loaded from the file. Inline targets are unaffected and use whatever fields they specify directly.

```yaml
targets_file: "config/targets.txt"

target_defaults:
  weight: 1                    # used when weight is omitted from the file
  http:
    method: GET
    headers:
      User-Agent: "Mozilla/5.0 ..."
    timeout_s: 15
  browser:
    scroll: false
    timeout_s: 30
  dns:
    resolver: "8.8.8.8:53"
    record_type: A
  websocket:
    duration_s: 30
    expect_messages: 0
```

| `target_defaults` field | Default | Description |
|-------------------------|---------|-------------|
| `weight` | `1` | Selection weight for file targets with no explicit weight |
| `http.method` | `GET` | HTTP verb |
| `http.timeout_s` | `15` | Request timeout in seconds |
| `browser.timeout_s` | `30` | Page load timeout in seconds |
| `dns.resolver` | `8.8.8.8:53` | DNS resolver address |
| `dns.record_type` | `A` | DNS record type |
| `websocket.duration_s` | `30` | How long to hold the connection open |

> **Note:** HTTP header map keys are lowercased by the YAML parser (e.g. `User-Agent` is stored as `user-agent`). This applies to both inline targets and `target_defaults`.

### `targets`

List of endpoints to request. Each target has a `weight` controlling selection frequency relative to the others. Selection uses the Vose alias method (O(1) per pick).

```yaml
targets:
  - url: "https://example.com"
    weight: 10
    type: http
    http:
      method: GET
      headers:
        User-Agent: "Mozilla/5.0 ..."
      body: ""          # optional request body
      timeout_s: 15

  - url: "https://news.ycombinator.com"
    weight: 5
    type: browser
    browser:
      scroll: true                    # scroll to mid-page then bottom
      wait_for_selector: "#hnmain"    # wait for this CSS selector before returning
      timeout_s: 30

  - url: "example.com"
    weight: 3
    type: dns
    dns:
      resolver: "8.8.8.8:53"
      record_type: A          # A | AAAA | MX | TXT | CNAME | ...

  - url: "wss://stream.example.com/feed"
    weight: 2
    type: websocket
    websocket:
      duration_s: 30                          # hold connection open for this long
      send_messages: ['{"type":"subscribe"}'] # messages to send on connect
      expect_messages: 1                      # wait to receive this many messages
```

### `output`

Optional result export to a file for offline analysis.

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Enable result export |
| `file` | `sendit-results.jsonl` | Output file path |
| `format` | `jsonl` | `jsonl` (one JSON object per line) \| `csv` |
| `append` | `false` | Append to an existing file instead of truncating on start |

```yaml
output:
  enabled: true
  file: "results.jsonl"
  format: jsonl    # jsonl | csv
  append: false
```

Each JSONL record contains: `ts`, `url`, `type`, `status`, `duration_ms`, `bytes`, `error`.
CSV output writes a header row when `append: false`.

### `metrics`

Optional Prometheus exposition.

```yaml
metrics:
  enabled: true
  prometheus_port: 9090     # GET http://localhost:9090/metrics
```

Exposed metrics:

| Metric | Type | Labels |
|--------|------|--------|
| `sendit_requests_total` | Counter | `type`, `domain`, `status_code` |
| `sendit_errors_total` | Counter | `type`, `domain`, `error_class` |
| `sendit_request_duration_seconds` | Histogram | `type`, `domain` |
| `sendit_bytes_read_total` | Counter | `type` |

### `daemon`

```yaml
daemon:
  pid_file: "/tmp/sendit.pid"   # written by start unless --foreground is set
  log_level: info                   # debug | info | warn | error
  log_format: text                  # text (coloured console) | json
```

---

## Dispatch Pipeline

Every task flows through the following gates in order before a worker goroutine is launched:

```
Scheduler.Wait        pacing delay (human jitter / token bucket / cron window)
  → resource.Admit    pause if CPU or RAM over threshold
  → backoff.Wait      per-domain delay after transient errors
  → ratelimit.Wait    per-domain token bucket
  → pool.Acquire      global semaphore + browser sub-semaphore
  → go driver.Execute
  → pool.Release
```

This ordering ensures that slow or rate-limited domains do not consume worker slots while waiting.

---

## Architecture

```
cmd/sendit/main.go          cobra CLI
internal/config/                YAML loader, defaults, validator, targets_file parser
internal/task/                  Task & Result types; Vose alias weighted selector
internal/ratelimit/             Per-domain token-bucket registry; decorrelated jitter backoff
internal/resource/              gopsutil CPU/RAM monitor with Admit() gate
internal/driver/                HTTP · headless browser (chromedp) · DNS (miekg) · WebSocket
internal/engine/                Worker pool · scheduler · dispatch loop
internal/metrics/               Prometheus counters & histograms
internal/output/                JSONL / CSV result writer (non-blocking, goroutine-backed)
config/example.yaml             Full reference configuration (with target_defaults section)
config/targets.txt              Example targets file (url + type per line)
config/test.yaml                Lightweight HTTP+DNS config for local smoke-testing
docker/                         Container deployment: Dockerfile, docker-compose, example config
```

### Browser driver

Each browser task spawns its own `chromedp.ExecAllocator` — no shared browser state — which prevents memory accumulation from long-running sessions. The `max_browser_workers` sub-semaphore limits concurrent Chrome instances independently of the global worker pool.

### DNS driver

DNS RCODEs are mapped to HTTP-like status codes so the engine's unified error classifier works across all driver types:

| DNS RCODE | HTTP equivalent | Effect |
|-----------|----------------|--------|
| NOERROR (0) | 200 | success |
| NXDOMAIN (3) | 404 | permanent skip |
| REFUSED (5) | 403 | permanent skip |
| SERVFAIL (2) | 503 | transient backoff |
| other | 502 | transient backoff |

---

## Running Tests

```sh
go test ./...
go test -race ./...                                       # with race detector
go test -tags integration -race -v ./internal/engine/... # full pipeline integration tests
```

Integration tests spin up local HTTP, DNS, and WebSocket servers and exercise the complete dispatch pipeline including backoff, graceful shutdown, and the resource gate.

---

## Verification

| Scenario | How to test |
|----------|-------------|
| Config validation | `sendit validate --config config/example.yaml` → prints "config valid", exits 0 |
| HTTP traffic | Use `config/test.yaml` (points at httpbin.org); observe status codes in logs |
| DNS traffic | DNS targets in `config/test.yaml`; look for `type=dns status=200` log lines |
| targets_file | Set `targets_file: "config/targets.txt"` in a config; `validate` checks the file, `start` loads all entries |
| targets_file error | Point `targets_file` at a file with a bad line (e.g. `example.com grpc`) → `validate` prints the line number and error |
| target_defaults | Omit `method` from `target_defaults.http`; confirm requests default to GET in logs |
| Resource gate | Set `cpu_threshold_pct: 1` → logs show "resource monitor: over threshold, dispatch paused" |
| Rate limiting | Set `default_rps: 0.1`, `max_workers: 1` → ~1 req/10s per domain observed |
| Backoff | Point a target at a URL returning 429; observe exponential `backoff=` delay in WRN logs |
| Graceful shutdown | Send SIGTERM during active requests → process logs "engine stopped" after in-flight tasks finish |
| Dry-run | `sendit start --config config/example.yaml --dry-run` → prints target table, pacing, and limits then exits 0 |
| Result export | Set `output.enabled: true`, run briefly, inspect the output file for JSONL records |
| Probe (HTTP) | `sendit probe https://httpbin.org/get` → prints status/latency/bytes per request |
| Probe (DNS) | `sendit probe example.com` → prints NOERROR/latency per query |
| Pinch (TCP) | `sendit pinch example.com:80` → prints open/closed/filtered + latency per check |
| Pinch (UDP) | `sendit pinch 8.8.8.8:53 --type udp` → prints open/closed/open\|filtered per check |
| Docker | `cd docker && docker compose up --build` → container starts; `curl localhost:9090/healthz` returns `{"status":"ok"}` |
