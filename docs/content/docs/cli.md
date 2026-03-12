---
title: "CLI Reference"
linkTitle: "CLI"
weight: 6
description: "All sendit commands and their flags."
---

## Commands

```
sendit generate [--targets-file <path>] [--url <url>] [--from-history chrome|firefox|safari] [--from-bookmarks chrome|firefox] [--output <file>]
sendit start    [-c <path>] [--foreground] [--log-level debug|info|warn|error] [--dry-run] [--capture <file>]
sendit probe    <target>    [--type http|dns|websocket] [--interval 1s] [--timeout 5s] [--send <msg>]
sendit pinch    <host:port> [--type tcp|udp] [--interval 1s] [--timeout 5s]
sendit export   --pcap <results.jsonl> [--output <results.pcap>]
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
| `stop` | Send SIGTERM to the running instance via its PID file. Waits for in-flight requests to finish. |
| `reload` | Send SIGHUP to the running instance via its PID file to hot-reload config atomically. |
| `status` | Report whether the process in the PID file is still alive. |
| `validate` | Parse and validate a config file. Exits 0 on success, non-zero with a message on error. |
| `version` | Print version, commit hash, and build date. |
| `completion` | Generate shell autocompletion scripts for bash, zsh, fish, or powershell. |

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
| `--from-bookmarks` | `""` | Harvest bookmarked URLs: `chrome` \| `firefox` (Safari bookmarks not yet supported) |
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

### Generate from browser history / bookmarks

```sh
# Top 50 most-visited Chrome URLs
sendit generate --from-history chrome --history-limit 50 --output config/generated.yaml

# Firefox bookmarks
sendit generate --from-bookmarks firefox --output config/generated.yaml

# Combine sources (duplicates removed automatically)
sendit generate --url https://example.com --from-history chrome --output config/gen.yaml
```

Browser history weights are derived from visit count (capped at 10) so frequently visited pages appear proportionally more often without dominating the traffic distribution. The generated YAML includes the full pacing / limits / backoff skeleton with sensible defaults — edit those sections to tune behaviour before running.

> **Browser support**: Chrome/Chromium and Firefox are supported on Linux and macOS. Safari history is macOS-only. Safari bookmarks (binary plist format) are not yet supported — use `--from-history safari` instead.

## `start` flags

| Flag | Short | Default | Description |
|---|---|---|---|
| `--config` | `-c` | `config/example.yaml` | Path to YAML config file |
| `--foreground` | | `false` | Skip writing the PID file |
| `--log-level` | | *(from config)* | Override log level: `debug` \| `info` \| `warn` \| `error` |
| `--dry-run` | | `false` | Print config summary and exit without sending traffic |
| `--capture` | | `""` | Write a synthetic PCAP file while running; file is finalised on clean shutdown |

### Dry-run output example

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
