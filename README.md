# sendit

[![CI](https://img.shields.io/github/actions/workflow/status/lewta/sendit/ci.yml?branch=main&label=tests)](https://github.com/lewta/sendit/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/lewta/sendit)](https://github.com/lewta/sendit/releases/latest)
[![Go version](https://img.shields.io/badge/go-1.26.6+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![OpenSSF Scorecard](https://api.securityscorecards.dev/projects/github.com/lewta/sendit/badge)](https://securityscorecards.dev/viewer/?uri=github.com/lewta/sendit)
[![OpenSSF Best Practices](https://bestpractices.coreinfrastructure.org/projects/12213/badge)](https://bestpractices.coreinfrastructure.org/projects/12213)
[![codecov](https://codecov.io/gh/lewta/sendit/graph/badge.svg)](https://codecov.io/gh/lewta/sendit)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

A Go CLI tool that simulates realistic user web traffic across HTTP, headless browser, DNS, WebSocket, gRPC, and SFTP protocols. Designed to blend into normal traffic baselines while being polite to both the local machine and target servers.

Key properties:

- Stays polite by default — scheduler and resource gates run before worker acquisition; `mode: burst` is available for internal infrastructure testing but requires an explicit time-bounded run (`--duration`) to start
- Per-domain token-bucket rate limits with decorrelated jitter backoff on transient errors
- Pauses dispatch when local CPU or RAM exceeds configurable thresholds
- On SIGINT, SIGTERM, duration expiry, or TUI quit, sendit stops dispatch, waits for in-flight workers to exit, and flushes output before returning. Active network requests receive the canceled context and may abort.

---

## Contents

- [Install](#install)
- [Quick Start](#quick-start)
- [CLI Commands](#cli-commands)
- [Dry-run mode](#dry-run-mode)
- [Generate](#generate)
- [Probe](#probe)
- [Pinch](#pinch)
- [Capture](#capture)
- [Docker](#docker)
- [Configuration Reference](#configuration-reference)
- [Dispatch Pipeline](#dispatch-pipeline)
- [Architecture](#architecture)
- [Running Tests](#running-tests)
- [Verification](#verification)
- [Security](#security)

---

## Install

### Homebrew (macOS / Linux)

```sh
brew install lewta/tap/sendit
```

Shell completions for bash, zsh, and fish are installed automatically.

### Linux packages

Download the package for your distro from the [latest release](https://github.com/lewta/sendit/releases/latest):

```sh
# Debian / Ubuntu
sudo dpkg -i sendit_*_linux_amd64.deb

# Fedora / RHEL / CentOS
sudo rpm -i sendit_*_linux_amd64.rpm

# Arch Linux / Omarchy — via AUR helper (recommended)
yay -S sendit-bin
# or: paru -S sendit-bin

# Arch Linux — direct package install (no AUR helper required)
sudo pacman -U sendit_*_linux_amd64.pkg.tar.zst
```

Shell completions are bundled and installed automatically.

### Windows (Scoop)

```sh
scoop bucket add lewta https://github.com/lewta/scoop-bucket
scoop install lewta/sendit
```

### Binary download

Download a pre-built binary for your platform from the [releases page](https://github.com/lewta/sendit/releases/latest), extract it, and place `sendit` somewhere in your `$PATH`.

### Build from source

```sh
git clone https://github.com/lewta/sendit
cd sendit
go build -o sendit ./cmd/sendit
```

---

## Quick Start

### Prerequisites

- Go 1.26.6+ (build from source only)
- Chrome/Chromium (only required for `type: browser` targets)

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
./sendit validate --config config/simple.yaml   # check parsing and constraints
./sendit start   --config config/simple.yaml --log-level debug
```

---

## CLI Commands

```
sendit generate [--targets-file <path>] [--url <url>] [--from-history chrome|firefox|safari] [--from-bookmarks chrome|firefox|safari] [--output <file>]
sendit start    [-c <path>] [--foreground] [--log-level debug|info|warn|error] [--dry-run] [--capture <file>] [--duration <duration>] [--tui]
sendit probe    <target>   [--type http|dns|websocket] [--interval 1s] [--timeout 5s] [--send <msg>]
sendit pinch    <host:port> [--type tcp|udp] [--interval 1s] [--timeout 5s]
sendit export   --pcap <results.jsonl> [--output <results.pcap>]
sendit stop     [--pid-file <path>]
sendit reload   [--pid-file <path>]
sendit status   [--pid-file <path>]
sendit validate [-c <path>]
sendit version
sendit completion <shell>
```

| Command      | Description |
|--------------|-------------|
| `generate`   | Generate a ready-to-use `config.yaml` from a targets file, a seed URL with in-domain crawling, or your local browser history/bookmarks. |
| `start`      | Start the engine. Writes a PID file by default so `stop`/`status` can find the process; use `--foreground` to skip writing the PID file. |
| `probe`      | Test a single HTTP, DNS, or WebSocket endpoint in a loop (like ping). No config file required. |
| `pinch`      | Check whether a TCP or UDP port is open on a remote host, repeating on an interval. No config file required. |
| `export`     | Convert a JSONL results file to PCAP format for analysis in Wireshark or tshark. |
| `stop`       | Send SIGTERM to a running instance via its PID file. |
| `reload`     | Send SIGHUP to a running instance via its PID file to reload the config atomically. Invalid configs leave the running configuration unchanged. Not available on Windows — use a full restart instead. |
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
| `--capture` | | `""` | Write a synthetic PCAP file while running; file is finalised on clean shutdown |
| `--duration` | | *(unlimited)* | Auto-stop after this wall-clock time (e.g. `5m`, `30s`); **required** when `pacing.mode: burst` |
| `--tui` | | `false` | Enable the live terminal UI (requires a TTY; falls back to plain output with a warning when stdout is piped or redirected) |

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

### `export` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--pcap` | *(required)* | JSONL results file to convert to PCAP |
| `--output` | *(input with `.pcap` extension)* | Output PCAP file path |

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

Targets (5):
  URL                                      TYPE       WEIGHT     SHARE
  https://httpbin.org/get                  http       10         43.5%
  https://httpbin.org/status/200           http       5          21.7%
  https://news.ycombinator.com             browser    3          13.0%
  example.com                              dns        3          13.0%
  https://httpbin.org/anything/users/alice/1?request=8d652a44-dc45-47a3-80da-33b47fc94709&at=1791490000 http 2 8.7%
  Total weight: 23

Pacing:
  mode: human | delay: 800ms–8000ms (random uniform)

Limits:
  workers: 4 (browser: 1) | cpu: 60% | memory: 512 MB
```

Templated targets show one expanded example URL. UUIDs, timestamps, and randomly selected custom values vary between dry runs.

---

## Generate

`sendit generate` produces a ready-to-use `config.yaml` from one or more input sources. Use it to get from zero to sending traffic in seconds without hand-editing YAML.

### From a targets file

```sh
sendit generate --targets-file config/targets.txt > config/generated.yaml
sendit validate --config config/generated.yaml
sendit start    --config config/generated.yaml
```

The targets file format is `<url> <type> [weight]` per line — the same format as `targets_file:` in the YAML config. Comments (`#`) and blank lines are ignored.

### From a seed URL with crawling

```sh
# Crawl example.com up to depth 2 and discover up to 50 in-domain pages
sendit generate --url https://example.com --depth 2 --max-pages 50 --output config/generated.yaml
```

The crawler fetches the seed URL, parses `<a href>` links, and follows in-domain links breadth-first. `robots.txt` is respected by default; sitemap URLs and redirects discovered through robots.txt are only fetched when they stay on the seed origin. HTML pages larger than 5 MiB and sitemap XML larger than 1 MiB are skipped. Pass `--ignore-robots` to skip robots.txt enforcement.

### From browser history

```sh
# Top 100 most-visited Chrome URLs (weight ∝ visit count, capped at 10)
sendit generate --from-history chrome --history-limit 100 --output config/generated.yaml

# Firefox or Safari
sendit generate --from-history firefox --output config/generated.yaml
sendit generate --from-history safari  --output config/generated.yaml  # macOS only
```

Visit count is mapped to a target weight (capped at 10) so frequently visited pages appear proportionally more often in the generated traffic without dominating it.

### From browser bookmarks

```sh
sendit generate --from-bookmarks chrome  --output config/generated.yaml
sendit generate --from-bookmarks firefox --output config/generated.yaml
sendit generate --from-bookmarks safari  --output config/generated.yaml  # macOS only
```

All bookmarked HTTP/HTTPS URLs are emitted as equal-weight targets. Sources can be combined:

```sh
sendit generate --url https://example.com --from-history chrome --history-limit 50 --output config/gen.yaml
```

### `generate` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--targets-file` | `""` | Generate from an existing targets file (`url type [weight]` per line) |
| `--url` | `""` | Seed URL for crawl-based generation (implies `--crawl`) |
| `--crawl` | `false` | Enable in-domain page discovery for HTTP targets (used with `--url`) |
| `--depth` | `2` | Maximum crawl depth |
| `--max-pages` | `50` | Maximum number of pages to discover |
| `--ignore-robots` | `false` | Skip `robots.txt` enforcement during crawl |
| `--from-history` | `""` | Harvest visited URLs from browser history: `chrome` \| `firefox` \| `safari` |
| `--from-bookmarks` | `""` | Harvest bookmarked URLs: `chrome` \| `firefox` \| `safari` |
| `--history-limit` | `100` | Maximum URLs to import from history (ordered by visit count descending) |
| `--output` | *(stdout)* | Write config to a file instead of stdout; prompts before overwriting |

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

## Capture

`sendit start --capture <file>` writes a synthetic PCAP alongside normal traffic. The file is finalised on clean shutdown (SIGINT/SIGTERM). No root, `CAP_NET_RAW`, or libpcap is required.

```sh
./sendit start --config config/example.yaml --capture session.pcap
# ... Ctrl-C to stop ...
# open session.pcap in Wireshark
```

`sendit export --pcap <results.jsonl>` converts a previously written JSONL results file to PCAP. Useful when you forgot to pass `--capture`, or when you want to post-process results from a long-running session.

```sh
./sendit start --config config/example.yaml
# output:
#   enabled: true
#   file: results.jsonl

sendit export --pcap results.jsonl
# Exported 312 packets → results.pcap
```

The PCAP uses **LINKTYPE_USER0 (147)** — there is no IP/TCP framing. Each packet payload is a text record:

```
ts=2024-01-01T12:00:00Z url=https://example.com type=http status=200 duration_ms=142 bytes=1256 error=
```

Open in Wireshark and use **Analyze → Follow → TCP Stream** (or the raw packet bytes view) to inspect individual request records.

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
  bind_address: 0.0.0.0
  prometheus_port: 9090

daemon:
  log_format: json   # friendlier for log aggregators
```

Metrics bind to loopback by default because metric labels include target domains. Container deployments that publish the metrics port should explicitly set `bind_address: 0.0.0.0`.

The `--foreground` flag is set in the image entrypoint — PID files are not useful inside containers.

### Files

| File | Description |
|------|-------------|
| `docker/Dockerfile` | Multi-stage build (`golang:1.26.6-alpine3.24` → `alpine`) |
| `docker/docker-compose.yml` | sendit + optional Prometheus/Grafana via `--profile observability` |
| `docker/config.yaml` | Docker-ready example config (metrics enabled, JSON logs) |
| `docker/prometheus.yml` | Prometheus scrape config targeting `sendit:9090` |

---

## Configuration Reference

See [`config/example.yaml`](config/example.yaml) for a full working example. Every section has defaults so you only need to specify what you want to override.

All supplied schedule entries are validated even when scheduled pacing is not active. Cron uses the scheduler's standard parser. Schedule duration/RPM, per-domain RPS, and memory thresholds must be positive; domains must not be blank; an enabled Prometheus port must be `1..65535`. Invalid reloads leave the running configuration unchanged.

### `pacing`

Controls how requests are spaced in time.

| Field | Default | Description |
|-------|---------|-------------|
| `mode` | `human` | `human` \| `rate_limited` \| `scheduled` \| `burst` |
| `requests_per_minute` | `20` | Target RPM — used by `rate_limited` and `scheduled` modes only |
| `jitter_factor` | `0.4` | Unused in `human` mode; reserved for future modes |
| `min_delay_ms` | `800` | Minimum inter-request delay in `human` mode |
| `max_delay_ms` | `8000` | Maximum inter-request delay in `human` mode |
| `schedule` | `[]` | List of validated cron windows — required when `mode: scheduled` |
| `ramp_up_s` | `0` | `burst` mode only; linearly decreases inter-request delay to zero without resizing the worker pool; `0` = immediate full-speed dispatch |

**Pacing modes:**

- **`human`** — random delay per request uniformly sampled from `[min_delay_ms, max_delay_ms]`. `requests_per_minute` and `jitter_factor` are ignored in this mode.
- **`rate_limited`** — token-bucket limiter at `requests_per_minute` plus a small random jitter after each token.
- **`scheduled`** — cron expressions open active windows; within each window behaves like `rate_limited` at the window's own RPM. Dispatch stays paused between windows; polling only checks whether a window has opened.
- **`burst`** — fires requests as fast as worker slots allow; optional `ramp_up_s` adds an inter-request delay that decreases to zero. Intended for internal infrastructure testing. **Requires `--duration`** on `sendit start` — the engine refuses to run an unbounded burst session.

```yaml
pacing:
  mode: scheduled
  schedule:
    - cron: "0 9 * * 1-5"      # weekdays 09:00
      duration_minutes: 30
      requests_per_minute: 40
```

```yaml
# Burst example — always pair with --duration when starting
pacing:
  mode: burst
  ramp_up_s: 30   # optional: decrease inter-request delay to zero over 30 s
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

Per-domain token buckets applied inside acquired workers, after scheduler pacing and resource admission.

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

- `url` — full URL (`https://`, `wss://`, `grpc://`, `sftp://`) or a bare hostname for DNS targets
- `type` — one of `http` | `browser` | `dns` | `websocket` | `grpc` | `sftp`
- `weight` — optional positive integer; defaults to `target_defaults.weight` when omitted
- Lines starting with `#` and blank lines are ignored

```
# config/targets.txt
https://example.com                                          http      5
https://api.example.com                                      http      3
example.com                                                  dns       2
wss://ws.example.com                                         websocket
grpc://svc.example.com:50051/helloworld.Greeter/SayHello    grpc      4
sftp://sftp.example.com/uploads/test.bin                    sftp      2
```

**`target_defaults`** supplies the remaining fields (driver settings, default weight) for every target loaded from the file. Inline targets are unaffected and use whatever fields they specify directly.

```yaml
targets_file: "config/targets.txt"

target_defaults:
  weight: 1                    # used when weight is omitted from the file
  vars:
    environment: [staging]     # shared template values for file-loaded targets
  vars_file:
    region: "config/regions.txt"
  auth:                        # optional: apply shared credentials to all file-loaded targets
    type: bearer
    token_env: API_TOKEN       # resolved from env at dispatch time
  http:
    method: GET
    headers:
      User-Agent: "Mozilla/5.0 ..."
    timeout_s: 15
    allow_cross_host_redirects: false
  browser:
    scroll: false
    timeout_s: 30
  dns:
    resolver: "8.8.8.8:53"
    record_type: A
  websocket:
    duration_s: 30
    expect_messages: 0
  grpc:
    timeout_s: 15
  sftp:
    port: 22
    operation: upload
    timeout_s: 30
    insecure: false
    username: testuser
    password: secret
```

| `target_defaults` field | Default | Description |
|-------------------------|---------|-------------|
| `weight` | `1` | Selection weight for file targets with no explicit weight |
| `vars` | `{}` | Shared inline template candidates for file-loaded targets |
| `vars_file` | `{}` | Shared variable-to-file mappings for file-loaded targets |
| `auth.type` | `""` | Auth type: `bearer` \| `basic` \| `header` \| `query` |
| `http.method` | `GET` | HTTP verb |
| `http.timeout_s` | `15` | Request timeout in seconds |
| `http.allow_cross_host_redirects` | `false` | Follow redirects to a different host. Redirected hosts still use per-domain rate limits. Keep disabled when sending auth headers unless that forwarding is intended. |
| `browser.timeout_s` | `30` | Page load timeout in seconds |
| `dns.resolver` | `8.8.8.8:53` | DNS resolver address |
| `dns.record_type` | `A` | DNS record type |
| `websocket.duration_s` | `30` | How long to hold the connection open |
| `grpc.timeout_s` | `15` | Per-call timeout in seconds |
| `sftp.port` | `22` | SSH port when the URL omits one |
| `sftp.operation` | `upload` | Operation: `upload` \| `download` \| `list` |
| `sftp.timeout_s` | `30` | SFTP connection and operation timeout in seconds |
| `sftp.insecure` | `false` | Skip `~/.ssh/known_hosts` host-key verification; use only for trusted test hosts |

> **Note:** HTTP header map keys are lowercased by the YAML parser (e.g. `User-Agent` is stored as `user-agent`). This applies to both inline targets and `target_defaults`.

### `targets`

List of endpoints to request. Each target has a `weight` controlling selection frequency relative to the others. Selection uses the Vose alias method (O(1) per pick).

#### Request templating

Use `{{name}}` placeholders to vary traffic without duplicating targets. For each request, sendit uniformly chooses one candidate per referenced custom variable and reuses it across the URL, HTTP body, gRPC body, and WebSocket send messages.

```yaml
targets:
  - url: "https://api.example.com/users/{{user_id}}?request={{uuid}}"
    type: http
    weight: 10
    vars:
      user_id: [alice, bob]
    vars_file:
      region: "data/regions.txt"
    http:
      method: POST
      body: '{"user":"{{user_id}}","region":"{{region}}","sequence":{{seq}},"at":{{timestamp}}}'
```

`vars_file` maps a variable name to a newline-delimited file. Relative paths resolve from the main YAML file's directory. Values are trimmed and blank lines are ignored. The file must contain at least one value.

Variable names must match `[a-z][a-z0-9_]*`. The reserved built-ins are `{{uuid}}` (random UUIDv4), `{{timestamp}}` (Unix epoch seconds), and `{{seq}}` (per-target counter starting at 1). Sequence counters reset on process start and successful config reload.

Expansion is a single pass: template-looking text inside a selected value remains literal. A variable cannot be defined in both `vars` and `vars_file`. Invalid names, empty values, unreadable files, malformed placeholders, and unknown variables fail configuration validation. `target_defaults.vars` and `target_defaults.vars_file` apply to targets loaded through `targets_file`.

Only target URLs, `http.body`, `grpc.body`, and `websocket.send_messages` are templated. Headers, authentication, and other driver settings are unchanged.

Non-standard ports are specified directly in the URL — no additional config needed:

```yaml
targets:
  - url: "http://internal-api.example.com:8080/health"
    weight: 1
    type: http
  - url: "wss://stream.example.com:9443/feed"
    weight: 1
    type: websocket
```

For DNS, non-standard resolver ports use the existing `host:port` format in `dns.resolver`:

```yaml
  - url: "example.com"
    type: dns
    dns:
      resolver: "192.168.1.1:5353"
```

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

  - url: "grpc://api.example.com:50051/helloworld.Greeter/SayHello"
    weight: 4
    type: grpc
    grpc:
      body: '{"name": "world"}'   # JSON-encoded request (optional — empty sends default-constructed message)
      timeout_s: 15
      # tls: false                # force TLS even when scheme is grpc://
      # insecure: false           # skip TLS certificate verification

  - url: "sftp://sftp.example.com/uploads/test.bin"
    weight: 2
    type: sftp
    sftp:
      username: testuser
      password: secret
      insecure: false
      operation: upload            # upload | download | list
      file_size_min_bytes: 1024
      file_size_max_bytes: 1048576
      allowed_host_key_types: [ssh-ed25519]

  # Auth — token values resolved from env vars at dispatch time.
  # Supported types: bearer, basic, header, query.
  - url: "https://api.example.com/data"
    weight: 3
    type: http
    auth:
      type: bearer
      token_env: API_TOKEN        # export API_TOKEN=<value> before starting

  - url: "https://api.example.com/search"
    weight: 2
    type: http
    auth:
      type: basic
      username: alice
      password_env: API_PASS

  - url: "https://api.example.com/v2/items"
    weight: 1
    type: http
    auth:
      type: header
      header_name: X-API-Key
      token_env: API_KEY

  - url: "https://api.example.com/v3/items"
    weight: 1
    type: http
    auth:
      type: query
      param_name: api_key
      token_env: API_KEY
```

Authentication applies to HTTP and WebSocket targets. Query authentication adds or replaces the configured parameter while preserving other query values. Environment-backed tokens are resolved at dispatch time. Results and exported output retain the expanded target URL, not the credential-bearing dial URL.

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

Each JSONL record contains: `ts`, `url`, `type`, `status`, `duration_ms`, `bytes`, `error`. Drivers may add metadata fields; SFTP records include SSH handshake and list metadata when available.
CSV output writes a header row when `append: false`.

### `metrics`

Optional Prometheus exposition.

```yaml
metrics:
  enabled: true
  bind_address: 127.0.0.1
  prometheus_port: 9090     # 1..65535; GET http://localhost:9090/metrics
```

Metrics bind to loopback by default because metric labels include target domains. Set `bind_address: 0.0.0.0` only when you intentionally expose `/metrics` to another host or container network.

Exposed metrics:

| Metric | Type | Labels |
|--------|------|--------|
| `sendit_requests_total` | Counter | `type`, `domain`, `status_code` |
| `sendit_errors_total` | Counter | `type`, `domain`, `error_class` |
| `sendit_request_duration_seconds` | Histogram | `type`, `domain` |
| `sendit_bytes_read_total` | Counter | `type` |

`sendit_errors_total` uses only `transient` and `permanent` `error_class` values. Classified failure statuses increment both `sendit_requests_total` and `sendit_errors_total`. Cancellation and deadline errors do not increment the error counter. WebSocket `101 Switching Protocols` is a successful request status.

Dashboards and alert rules matching the undocumented `error_class="error"` value must update their selectors to match `transient`, `permanent`, or both.

### `daemon`

```yaml
daemon:
  pid_file: "/tmp/sendit.pid"   # written by start unless --foreground is set
  log_level: info                   # debug | info | warn | error
  log_format: text                  # text (coloured console) | json
```

---

## Dispatch Pipeline

Scheduler and resource gates run before worker acquisition. Domain backoff and rate-limit waits run inside acquired workers, consuming slots while preventing a slow domain from blocking the single dispatch loop.

```text
Scheduler.Wait -> resource.Admit -> pool.Acquire -> go dispatch()
                                                -> backoff.Wait
                                                -> ratelimit.Wait
                                                -> driver.Execute
                                                -> pool.Release
```

---

## Architecture

```
cmd/sendit/                    Cobra CLI, split into focused command files
internal/config/                YAML loader, defaults, validator, targets_file parser
internal/task/                  Task & Result types; Vose alias weighted selector
internal/ratelimit/             Per-domain token-bucket registry; decorrelated jitter backoff
internal/resource/              gopsutil CPU/RAM monitor with Admit() gate
internal/driver/                HTTP · headless browser (chromedp) · DNS (miekg) · WebSocket · gRPC (reflection-based) · SFTP
internal/engine/                Worker pool · scheduler · dispatch loop
internal/metrics/               Prometheus counters & histograms
internal/output/                JSONL / CSV result writer (non-blocking, goroutine-backed)
internal/pcap/                  Synthetic PCAP writer and JSONL→PCAP exporter (pure Go, no CGO)
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

### gRPC driver

Executes unary gRPC calls using server reflection — no `.proto` files required. gRPC status codes are mapped to HTTP-like codes on the same principle:

| gRPC code | HTTP equivalent | Effect |
|-----------|----------------|--------|
| OK (0) | 200 | success |
| InvalidArgument (3), OutOfRange (11) | 400 | permanent skip |
| Unauthenticated (16) | 401 | permanent skip |
| PermissionDenied (7) | 403 | permanent skip |
| NotFound (5) | 404 | permanent skip |
| AlreadyExists (6) | 409 | permanent skip |
| ResourceExhausted (8) | 429 | transient backoff |
| Unimplemented (12) | 501 | permanent skip |
| Unavailable (14) | 503 | transient backoff |
| DeadlineExceeded (4) | 504 | transient backoff |
| other | 500 | transient backoff |

URL format: `grpc://host:port/Service/Method` (plaintext) or `grpcs://host:port/Service/Method` (TLS). The target server must have the [gRPC server reflection service](https://grpc.io/docs/guides/reflection/) enabled.

### SFTP driver

Executes SFTP `upload`, `download`, and `list` operations over SSH. By default, host keys are verified against `~/.ssh/known_hosts`; set `sftp.insecure: true` only for trusted lab or ephemeral hosts. Connections are cached per address, username, auth material, and SSH policy so targets with stricter algorithm settings do not reuse weaker sessions.

| SFTP condition | HTTP equivalent | Effect |
|----------------|----------------|--------|
| Success | 200 | success |
| Auth failure | 401 | permanent skip |
| Permission denied | 403 | permanent skip |
| Missing download path | 404 | permanent skip |
| Host key or algorithm policy mismatch | 502 | transient backoff |
| SFTP protocol error | 502 | transient backoff |
| Timeout | 504 | transient backoff |

JSONL output includes SFTP metadata when available: `sftp_server_version`, `sftp_host_key_type`, `sftp_host_key_fp`, `sftp_auth_methods`, and `sftp_entry_count` for list operations.

---

## Running Tests

```sh
make test        # unit tests
make test-race   # unit tests with the race detector
make integration # full pipeline integration tests
make lint        # golangci-lint
make verify      # canonical pre-PR verification
```

`make verify` builds the CLI, runs the linter, executes the full race-enabled
test suite, and runs the integration suite.

Integration tests spin up local HTTP, DNS, WebSocket, gRPC, and SFTP servers and exercise the complete dispatch pipeline including backoff, graceful shutdown, and the resource gate.

---

## Verification

| Scenario | How to test |
|----------|-------------|
| Config validation | `sendit validate --config config/example.yaml` → prints "config valid", exits 0 |
| HTTP traffic | Use `config/test.yaml` (points at httpbin.org); observe status codes in logs |
| DNS traffic | DNS targets in `config/test.yaml`; look for `type=dns status=200` log lines |
| targets_file | Set `targets_file: "config/targets.txt"` in a config; `validate` checks the file, `start` loads all entries |
| targets_file error | Point `targets_file` at a file with a bad line (e.g. `example.com ftp`) → `validate` prints the line number and error |
| gRPC traffic | Add a `type: grpc` target pointing at a service with reflection enabled (e.g. a local gRPC health server); observe `type=grpc status=200` log lines |
| Auth (bearer) | Add `auth: {type: bearer, token_env: MY_TOKEN}` to a target; `export MY_TOKEN=test`; observe `Authorization: Bearer test` in server logs |
| Auth (env unset) | Set `token_env` to an unset variable; observe error result in output and no request reaching the server |
| target_defaults | Omit `method` from `target_defaults.http`; confirm requests default to GET in logs |
| Resource gate | Set `cpu_threshold_pct: 1` → logs show "resource monitor: over threshold, dispatch paused" |
| Rate limiting | Set `default_rps: 0.1`, `max_workers: 1` → ~1 req/10s per domain observed |
| Backoff | Point a target at a URL returning 429; observe exponential `backoff=` delay in WRN logs |
| Graceful shutdown | Send SIGTERM during active requests → requests receive cancellation; process logs "engine stopped" after workers exit and output flushes |
| Dry-run | `sendit start --config config/example.yaml --dry-run` → prints target table, pacing, and limits then exits 0 |
| Result export | Set `output.enabled: true`, run briefly, inspect the output file for JSONL records |
| PCAP capture | `sendit start --config config/example.yaml --capture session.pcap` → stop after a few requests → open `session.pcap` in Wireshark; packets should appear with LINKTYPE_USER0 (147) |
| PCAP export | Run with `output.enabled: true`, then `sendit export --pcap results.jsonl` → `results.pcap` created; verify with `file results.pcap` or Wireshark |
| Probe (HTTP) | `sendit probe https://httpbin.org/get` → prints status/latency/bytes per request |
| Probe (DNS) | `sendit probe example.com` → prints NOERROR/latency per query |
| Pinch (TCP) | `sendit pinch example.com:80` → prints open/closed/filtered + latency per check |
| Pinch (UDP) | `sendit pinch 8.8.8.8:53 --type udp` → prints open/closed/open\|filtered per check |
| Non-standard port | Set `url: "http://localhost:8080"` in config → `sendit start` sends traffic to port 8080; or `sendit probe http://localhost:8080` |
| Docker | `cd docker && docker compose up --build` → container starts; `curl localhost:9090/healthz` returns `{"status":"ok"}` |

---

## Security

To report a vulnerability, use [GitHub private vulnerability reporting](https://github.com/lewta/sendit/security/advisories/new). See [SECURITY.md](SECURITY.md) for the full policy.
