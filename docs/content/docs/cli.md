---
title: "CLI Reference"
linkTitle: "CLI"
weight: 6
description: "All sendit commands and their flags."
---

## Commands

```
sendit generate [--targets-file <path>] [--url <url>] [--from-history chrome|firefox|safari] [--from-bookmarks chrome|firefox|safari] [--output <file>]
sendit start    [-c <path>] [--foreground] [--log-level debug|info|warn|error] [--dry-run] [--capture <file>] [--duration <duration>] [--tui]
sendit probe    <target>    [--type http|dns|websocket] [--interval 1s] [--timeout 5s] [--send <msg>]
sendit pinch    <host:port> [--type tcp|udp] [--interval 1s] [--timeout 5s]
sendit export   --pcap <results.jsonl> [--output <results.pcap>]
sendit replay   --input <results.jsonl> [--rate 1] [--filter status=5xx] [--loop] [--loop-delay 1s] [--output <file>]
sendit stop     [--pid-file <path>]
sendit reload   [--pid-file <path>]
sendit status   [--pid-file <path>]
sendit validate [-c <path>]
sendit version
sendit completion <shell>
```

| Command | Description |
|---|---|
| `generate` | Generate a ready-to-use `config.yaml` from a targets file, a seed URL with in-domain crawling, or your local browser history/bookmarks. |
| `start` | Start the engine. Writes a PID file by default so `stop`/`status` can find the process; use `--foreground` to skip. |
| `probe` | Test a single HTTP, DNS, or WebSocket endpoint in a loop (like ping). No config file needed. |
| `pinch` | Check whether a TCP or UDP port is open on a remote host, repeating on an interval. No config file needed. |
| `export` | Convert a JSONL results file to PCAP format for analysis in Wireshark or tshark. |
| `replay` | Replay versioned captured requests with scaled concurrent timing, filtering, and optional looping. |
| `stop` | Send SIGTERM to the running instance via its PID file. Cancels active requests, then waits for workers to exit and output to flush. |
| `reload` | Send SIGHUP to the running instance via its PID file to hot-reload config atomically. |
| `status` | Report whether the process in the PID file is still alive. |
| `validate` | Parse and validate a config file. Exits 0 on success, non-zero with a message on error. |
| `version` | Print version, commit hash, and build date. |
| `completion` | Generate shell autocompletion scripts for bash, zsh, fish, or powershell. |

## `replay` flags

| Flag | Default | Description |
|---|---|---|
| `--input` | required | Replay-capable JSONL containing one capture run |
| `--rate` | `1` | Positive finite multiplier: `2` halves original start gaps; `0.5` doubles them |
| `--filter` | `""` | Only `status=5xx` (original status 500–599); excludes status 0 and 429 |
| `--loop` | `false` | Repeat selected records until interrupted |
| `--loop-delay` | `1s` | Nonnegative delay between completed cycles, independent of `--rate` |
| `--output` | `""` | New results as JSONL, independent of the filename extension |

### Capture and replay

Enable JSONL `output` in your capture config, with `append: false`. `start` does not have an `--output` flag. For a service running locally on port 8080, save this as `replay-capture.yaml`:

```yaml
pacing:
  mode: rate_limited
  requests_per_minute: 60
targets:
  - url: http://127.0.0.1:8080/users/{{seq}}
    type: http
    weight: 1
output:
  enabled: true
  file: results.jsonl
  format: jsonl
  append: false
```

```sh
sendit start --config replay-capture.yaml --foreground --duration 5s
sendit replay --input results.jsonl --rate 2 --output replay-results.jsonl
sendit replay --input results.jsonl --filter status=5xx --rate 0.5
sendit replay --input results.jsonl --loop --loop-delay 1s
```

### Validation and execution

- **Future records only:** legacy telemetry and CSV lack the request snapshot. [New JSONL](../configuration/#replay-jsonl-envelope) captures expanded values without regenerating UUIDs or template choices. No normal configuration is loaded during replay.
- **One run:** appended sessions with different run IDs are rejected before filtering. Sequence gaps are allowed, duplicates are not. Serialization order may differ from dispatch order; replay sorts by sequence.
- **Limits:** 256 MiB per file, 8 MiB per line excluding CRLF/LF, and 10,000 records before filtering. Input must be a regular file. It is read completely before the first request or destructive output opening.
- **Strict executable fields:** unknown versions/types/fields, missing/null required fields, duplicate JSON keys, blank lines, invalid UTF-8, trailing JSON values, invalid static request shapes, and unrepresentable timing are rejected with line-specific errors. An unterminated final line is accepted. Arbitrary top-level result metadata remains compatible. Remote schema/TLS/browser errors are execution results.
- **Eligibility:** SFTP, populated auth (including env references), HTTP headers even on inactive blocks, and URL userinfo are non-replayable. Filtering may exclude a valid non-replayable record, but never excuses malformed records. Empty non-looping selections succeed; empty looping selections error.
- **Timing:** selected gaps are retained and divided by rate, rounded down to nanoseconds. Equal/overdue deadlines launch in sequence order. Requests overlap; driver-entry/network-arrival order is not guaranteed. Normal scheduler/backoff/rate-limit/resource/metrics behavior is not used. Up to 10,000 accepted requests can execute concurrently.
- **Looping:** wait for every request and its output, then the unscaled loop delay. Explicit zero delay is supported. Output uses a fresh run ID and continuous sequence across loops; replaying it later remains subject to input limits.
- **Shutdown:** Ctrl-C/SIGTERM stops scheduling, cancels calls, waits for replay workers, and finalizes output. Existing driver close/exchange latency still applies; this does not promise immediate network quiescence.
- **Results:** final stdout is `Replayed N requests; M failures.` Go errors or classified status failures count, including returned cancellation failures. Request failures continue and exit successfully. Parsing/setup/output failures exit nonzero. Successful output retains every executed result; output write failure cancels remaining work, flush/close errors are surfaced, and partial files may remain.
- **Files:** input/output identity is checked before truncation, including relative/symlink/hard-link aliases. New outputs use mode `0600`; on POSIX existing outputs with group/other permissions are rejected. Windows uses native access controls. Concurrent external replacement of these paths is unsupported.

URLs, bodies, and messages may contain application secrets despite configured-credential exclusions. Proxy/environment state and remote responses are not recorded. See [replay data handling](../security/#replay-data-handling).

## `generate` flags

| Flag | Default | Description |
|---|---|---|
| `--targets-file` | `""` | Generate from an existing targets file (`url type [weight]` per line) |
| `--url` | `""` | Seed URL for crawl-based generation (implies `--crawl`) |
| `--crawl` | `false` | Enable in-domain page discovery (used with `--url`) |
| `--depth` | `2` | Maximum crawl depth |
| `--max-pages` | `50` | Maximum number of pages to discover |
| `--ignore-robots` | `false` | Skip `robots.txt` enforcement during crawl |
| `--from-history` | `""` | Harvest visited URLs from browser history: `chrome` \| `firefox` \| `safari` |
| `--from-bookmarks` | `""` | Harvest bookmarked URLs: `chrome` \| `firefox` \| `safari` |
| `--history-limit` | `100` | Maximum URLs to import from history, ordered by visit count |
| `--output` | *(stdout)* | Write config to a file instead of stdout; prompts before overwriting |

### Generate from a targets file

```sh
sendit generate --targets-file config/targets.txt
sendit generate --targets-file config/targets.txt --output config/generated.yaml
```

### Generate from a seed URL

```sh
# Crawl example.com, follow in-domain links up to depth 2, discover up to 50 pages
sendit generate --url https://example.com --depth 2 --output config/generated.yaml

# Skip robots.txt enforcement
sendit generate --url https://example.com --ignore-robots --output config/generated.yaml
```

When crawling from a seed URL, page links are kept in-domain. `robots.txt` is respected by default, and sitemap URLs or redirects discovered through robots.txt are only fetched when they remain on the seed origin. HTML pages larger than 5 MiB and sitemap XML larger than 1 MiB are skipped.

### Generate from browser history / bookmarks

```sh
# Top 50 most-visited Chrome URLs
sendit generate --from-history chrome --history-limit 50 --output config/generated.yaml

# Firefox bookmarks
sendit generate --from-bookmarks firefox --output config/generated.yaml

# Safari bookmarks (macOS only)
sendit generate --from-bookmarks safari --output config/generated.yaml

# Combine sources (duplicates removed automatically)
sendit generate --url https://example.com --from-history chrome --output config/gen.yaml
```

Browser history weights are derived from visit count (capped at 10) so frequently visited pages appear proportionally more often without dominating the traffic distribution. The generated YAML includes the full pacing / limits / backoff skeleton with sensible defaults — edit those sections to tune behaviour before running.

> **Browser support**: Chrome/Chromium and Firefox are supported on Linux and macOS. Safari history and bookmarks are macOS-only. Safari bookmarks are read from `~/Library/Safari/Bookmarks.plist` (binary and XML plist formats supported).

## `start` flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |
| `--foreground` | | `false` | Skip writing the PID file |
| `--log-level` | | *(from config)* | Override log level: `debug` \| `info` \| `warn` \| `error` |
| `--dry-run` | | `false` | Print config summary and exit without sending traffic |
| `--capture` | | `""` | Write a synthetic PCAP file while running; file is finalised on clean shutdown |
| `--duration` | | `0` (unlimited) | Auto-stop after this wall-clock duration (e.g. `5m`, `30s`, `1h`); **required** when `pacing.mode` is `burst` |
| `--tui` | | `false` | Enable the live terminal UI (requires a TTY; falls back to plain output with a warning when stdout is piped or redirected) |

`sendit validate` and `sendit start` read `vars_file` inputs and reject malformed or unknown request-template placeholders before sending traffic.

### Terminal UI (--tui)

When run on a TTY, `--tui` replaces the default log output with a live dashboard:

```sh
sendit start --config config/example.yaml --tui
```

```
sendit — q or ctrl-c to stop

Mode      rate_limited · 60 rpm · 4 workers
Running   1m 23s

Requests  312 total · 308 ok · 4 errors (1.3%)
Latency   avg 45ms · p95 118ms

          ▁▂▂▃▄▄▅▆▇▆▅▄▃▃▂▃▄▅▆▇▆▅▄▅▆▇█▇▆▅▄▃▂▁▂▃▄
```

The sparkline shows the last 128 successful request latencies, scaled from `▁` (fastest) to `█` (slowest). On SIGINT, SIGTERM, duration expiry, or TUI quit, sendit stops dispatch, waits for in-flight workers to exit, and flushes output before returning. Active network requests receive the canceled context and may abort.

When stdout is not a TTY (pipe, redirect, Docker, CI), `--tui` emits a warning and falls back to plain zerolog output.

### Dry-run output example

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

Templated targets show one expanded example URL. UUIDs, timestamps, and randomly selected custom values vary between dry runs. Dry-run does not print expanded bodies, variable maps, or authentication values.

## `probe` flags

| Flag | Default | Description |
|---|---|---|
| `--type` | *(auto-detected)* | Driver type: `http` \| `dns` \| `websocket` |
| `--interval` | `1s` | Delay between requests |
| `--timeout` | `5s` | Per-request timeout |
| `--resolver` | `8.8.8.8:53` | DNS resolver (dns targets only) |
| `--record-type` | `A` | DNS record type (dns targets only) |
| `--send` | `""` | Message to send after connecting (websocket only); waits for one reply and reports round-trip latency |

**Auto-detection rules:**

| Target format | Detected type |
|---|---|
| `https://example.com` | `http` |
| `http://example.com` | `http` |
| `wss://example.com` | `websocket` |
| `ws://example.com` | `websocket` |
| `example.com` | `dns` |

### HTTP probe example

Non-standard ports work by including the port in the URL:

```sh
./sendit probe https://example.com
./sendit probe http://localhost:8080/health
./sendit probe https://staging.example.com:8443/api
```

```
Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
^C

--- https://example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 90ms / 142ms
```

### DNS probe example

```sh
./sendit probe example.com --record-type A --resolver 1.1.1.1:53
```

```
Probing example.com (dns, A @ 1.1.1.1:53) — Ctrl-C to stop

  NOERROR    12ms
  NOERROR     8ms
^C

--- example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 8ms / 10ms / 12ms
```

### WebSocket probe example (connect only)

```sh
./sendit probe wss://echo.websocket.org
```

```
Probing wss://echo.websocket.org (websocket, connect only) — Ctrl-C to stop

  101    38ms
  101    41ms
^C

--- wss://echo.websocket.org ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 39ms / 41ms
```

### WebSocket probe example (send + receive round-trip)

```sh
./sendit probe wss://echo.websocket.org --send 'ping'
```

```
Probing wss://echo.websocket.org (websocket, send+recv) — Ctrl-C to stop

  101    42ms
  101    39ms
^C

--- wss://echo.websocket.org ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 39ms / 40ms / 42ms
```

## `pinch` flags

| Flag | Default | Description |
|---|---|---|
| `--type` | `tcp` | Protocol type: `tcp` \| `udp` |
| `--interval` | `1s` | Delay between checks |
| `--timeout` | `5s` | Per-check timeout |

**Status labels:**

| Label | Protocol | Meaning |
|---|---|---|
| `open` | TCP | Connection accepted |
| `closed` | TCP | Connection refused |
| `filtered` | TCP | No response (deadline exceeded) |
| `open` | UDP | Response data received |
| `closed` | UDP | ICMP port unreachable received |
| `open\|filtered` | UDP | Timeout — UDP is inherently ambiguous |

### TCP pinch example

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

### UDP pinch example

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

## `export` flags

| Flag | Default | Description |
|---|---|---|
| `--pcap` | *(required)* | JSONL results file to convert to PCAP |
| `--output` | *(input with `.pcap` extension)* | Output PCAP file path |

### PCAP export example

```sh
sendit export --pcap results.jsonl
# Exported 312 packets → results.pcap

sendit export --pcap results.jsonl --output /tmp/session.pcap
# Exported 312 packets → /tmp/session.pcap
```

The generated PCAP uses **LINKTYPE_USER0 (147)** — no IP/TCP framing. Each packet payload is a text record:

```
ts=2024-01-01T12:00:00Z url=https://example.com type=http status=200 duration_ms=142 bytes=1256 error=
```

Open in Wireshark; packets appear as raw data under the `USER0` dissector. Use the raw packet bytes view or **Follow → TCP Stream** to read individual records.

## `stop` / `reload` / `status` flags

| Flag | Default | Description |
|---|---|---|
| `--pid-file` | `/tmp/sendit.pid` | Path to PID file written by `start` |

> **Windows:** SIGHUP is not available on Windows. `sendit reload` will not work — use a full restart to pick up config changes.

## `validate` flags

`validate` checks every supplied schedule entry, even when scheduled pacing is not active. Cron uses the scheduler's standard parser. Schedule duration/RPM, per-domain RPS, and memory thresholds must be positive; domains must not be blank; an enabled Prometheus port must be `1..65535`. Invalid reloads leave the running configuration unchanged.

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |

## Shell completion

### Homebrew

Completions are installed automatically as part of the formula — no extra steps needed.

### Linux packages (`.deb` / `.rpm`)

Completions are bundled in the package and installed to the system completion directories — no extra steps needed.

### Binary download

```sh
# bash — add to ~/.bashrc
source <(sendit completion bash)

# zsh — add to ~/.zshrc
source <(sendit completion zsh)

# fish
sendit completion fish | source
```
