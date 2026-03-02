---
title: "Getting Started"
linkTitle: "Getting Started"
weight: 1
description: "Install sendit, validate a config, and run your first traffic."
---

## Prerequisites

- **Go 1.22+** — required to build from source
- **Chrome or Chromium** — only required for `type: browser` targets
- The `sendit` binary runs on Linux, macOS, and Windows

## Build from source

```sh
git clone https://github.com/lewta/sendit
cd sendit
go build -o sendit ./cmd/sendit
./sendit version
```

## Pre-built binaries

Download the latest binary for your platform from the [releases page](https://github.com/lewta/sendit/releases/latest).

## Test an endpoint without a config file

`sendit probe` needs no config — it auto-detects the driver from the URL:

```sh
# HTTP (https:// or http://)
./sendit probe https://example.com

# DNS (bare hostname)
./sendit probe example.com
```

Each request prints status, latency, and bytes (HTTP) or RCODE (DNS). Press Ctrl-C for a summary:

```
Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
^C

--- https://example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 90ms / 142ms
```

## Create a config file

Copy the [example config](https://github.com/lewta/sendit/blob/main/config/example.yaml) as a starting point:

```sh
cp config/example.yaml config/my.yaml
# edit config/my.yaml to your targets
```

Validate it before running:

```sh
./sendit validate --config config/my.yaml
# config valid
```

## Run

```sh
./sendit start --config config/my.yaml --log-level debug
```

By default `start` writes a PID file to `/tmp/sendit.pid` so you can manage the process:

```sh
./sendit status   # is it alive?
./sendit reload   # hot-reload config without restart
./sendit stop     # send SIGTERM, wait for in-flight requests to finish
```

Use `--foreground` to skip the PID file (useful in containers or CI).

## Dry-run mode

Preview effective config — targets, weights, pacing — without sending any traffic:

```sh
./sendit start --config config/my.yaml --dry-run
```

```
Config: config/my.yaml  ✓ valid

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
