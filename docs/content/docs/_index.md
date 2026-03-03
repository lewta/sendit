---
title: "Documentation"
weight: 1
description: "Reference documentation for sendit — a flexible traffic generator for HTTP, DNS, WebSocket, and browser targets."
---

Welcome to the sendit reference documentation.

## Quick tools — no config required

Two commands let you verify connectivity instantly without writing a config file:

| Command | What it does |
|---|---|
| [`sendit probe <target>`](cli/#probe-flags) | Test a single HTTP or DNS endpoint in a loop. Auto-detects the driver from the URL. Works like `ping` for web targets. |
| [`sendit pinch <host:port>`](cli/#pinch-flags) | Check whether a TCP or UDP port is open on a remote host, repeating on an interval. |

```sh
sendit probe https://example.com       # HTTP — prints status, latency, bytes per request
sendit probe example.com               # DNS  — prints RCODE and latency per query
sendit pinch example.com:443           # TCP  — prints open/closed/filtered per check
sendit pinch 8.8.8.8:53 --type udp    # UDP  — prints open/closed/open|filtered per check
```

Press Ctrl-C to stop and print a summary. See the [CLI Reference](cli/) for all flags.

## Sections

| Section | What you'll find |
|---|---|
| [Getting Started](getting-started/) | Install, build, validate a config, and run your first traffic |
| [Configuration](configuration/) | Every config key, its type, default, and description |
| [Pacing Modes](pacing/) | `human`, `rate_limited`, and `scheduled` — how request timing works |
| [Drivers](drivers/) | `http`, `browser`, `dns`, `websocket` — options and examples for each |
| [Metrics](metrics/) | Prometheus metrics exposed by sendit and how to scrape them |
| [CLI Reference](cli/) | All commands and flags |
