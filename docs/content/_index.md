---
title: "sendit"
description: "A flexible traffic generator for HTTP, DNS, WebSocket, and browser targets."
---

**sendit** is a Go CLI tool that simulates realistic user web traffic across HTTP, headless browser, DNS, and WebSocket protocols. It never bursts aggressively — every request is paced through a configurable delay, resource gate, and per-domain rate limit before a worker slot is acquired.

## Quick install

```sh
git clone https://github.com/lewta/sendit
cd sendit
go build -o sendit ./cmd/sendit

# Test an endpoint immediately — no config needed
./sendit probe https://example.com
```

Or grab a pre-built binary from the [releases page](https://github.com/lewta/sendit/releases/latest).

## Key properties

- **Four driver types** — HTTP, headless browser (chromedp), DNS (miekg), WebSocket
- **Three pacing modes** — `human` (random delay), `rate_limited` (token bucket), `scheduled` (cron windows)
- **Polite by design** — per-domain token buckets, decorrelated jitter backoff, CPU/RAM resource gates
- **Graceful shutdown** — waits for all in-flight requests to complete on SIGINT/SIGTERM

## Documentation

| | |
|---|---|
| [Getting Started](docs/getting-started/) | Install, validate a config, and run your first traffic |
| [Configuration](docs/configuration/) | Every config key with type, default, and description |
| [Pacing Modes](docs/pacing/) | `human`, `rate_limited`, and `scheduled` explained |
| [Drivers](docs/drivers/) | HTTP, browser, DNS, WebSocket options and examples |
| [Metrics](docs/metrics/) | Prometheus metrics and scrape config |
| [CLI Reference](docs/cli/) | All commands and flags |
