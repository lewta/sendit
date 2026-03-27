---
title: "Documentation"
weight: 1
description: "Reference documentation for sendit — a flexible traffic generator for HTTP, DNS, WebSocket, gRPC, and browser targets."
---

Welcome to the sendit reference documentation.

## Terminal UI

Run `sendit start --tui` for a live dashboard — no extra config needed:

```
sendit — q or ctrl-c to stop

Mode      rate_limited · 60 rpm · 4 workers
Running   1m 23s

Requests  312 total · 308 ok · 4 errors (1.3%)
Latency   avg 45ms · p95 118ms

          ▁▂▂▃▄▄▅▆▇▆▅▄▃▃▂▃▄▅▆▇▆▅▄▅▆▇█▇▆▅▄▃▂▁▂▃▄
```

The sparkline shows the latency distribution of the last 128 requests. Press `q` or `ctrl-c` to stop — the engine shuts down gracefully. Falls back to plain log output automatically when stdout is not a TTY (Docker, CI, pipes). See the [CLI Reference](cli/#terminal-ui---tui) for details.

## Quick tools — no config required

Two commands let you verify connectivity instantly without writing a config file:

| Command | What it does |
|---|---|
| [`sendit probe <target>`](cli/#probe-flags) | Test a single HTTP, DNS, or WebSocket endpoint in a loop. Auto-detects the driver from the URL. Works like `ping` for web targets. |
| [`sendit pinch <host:port>`](cli/#pinch-flags) | Check whether a TCP or UDP port is open on a remote host, repeating on an interval. |

```sh
sendit probe https://example.com            # HTTP — prints status, latency, bytes per request
sendit probe example.com                    # DNS  — prints RCODE and latency per query
sendit probe wss://echo.websocket.org       # WebSocket — prints status and connect latency
sendit probe wss://echo.websocket.org --send 'ping'  # WebSocket — send+recv round-trip
sendit pinch example.com:443                # TCP  — prints open/closed/filtered per check
sendit pinch 8.8.8.8:53 --type udp         # UDP  — prints open/closed/open|filtered per check
```

Press Ctrl-C to stop and print a summary. See the [CLI Reference](cli/) for all flags.

## Sections

| Section | What you'll find |
|---|---|
| [Getting Started](getting-started/) | Install, build, validate a config, run your first traffic, and deploy with Docker |
| [Configuration](configuration/) | Every config key, its type, default, and description |
| [Pacing Modes](pacing/) | `human`, `rate_limited`, `scheduled`, and `burst` — how request timing works |
| [Drivers](drivers/) | `http`, `browser`, `dns`, `websocket`, `grpc` — options and examples for each |
| [Metrics](metrics/) | Prometheus metrics exposed by sendit and how to scrape them |
| [CLI Reference](cli/) | All commands and flags |
| [Dependencies](dependencies/) | Direct dependencies, their purpose, and their licences |
| [OpenSSF Best Practices](ossf/) | Evidence and criteria for the OpenSSF passing badge |
| [Security](security/) | Security policy, supported versions, and how to report a vulnerability |
