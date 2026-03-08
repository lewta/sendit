---
title: "Getting Started"
linkTitle: "Getting Started"
weight: 1
description: "Install sendit, validate a config, and run your first traffic."
---

## Prerequisites

- **Chrome or Chromium** — only required for `type: browser` targets
- The `sendit` binary runs on Linux, macOS, and Windows

## Install

### Homebrew (macOS and Linux)

```sh
brew install lewta/tap/sendit
```

Shell completions for bash, zsh, and fish are installed automatically.

### Scoop (Windows)

```powershell
scoop bucket add lewta https://github.com/lewta/scoop-bucket
scoop install sendit
```

### Linux packages

Download the `.deb` or `.rpm` package for your architecture from the [releases page](https://github.com/lewta/sendit/releases/latest):

```sh
# Debian / Ubuntu
sudo dpkg -i sendit_<version>_linux_amd64.deb

# Fedora / RHEL / CentOS
sudo rpm -i sendit_<version>_linux_amd64.rpm
```

Shell completions are installed to the system completion directories automatically.

### Binary download

Download the pre-built archive for your platform from the [releases page](https://github.com/lewta/sendit/releases/latest), extract it, and place the binary in your `$PATH`.

### Build from source

Requires **Go 1.24+**.

```sh
git clone https://github.com/lewta/sendit
cd sendit
go build -o sendit ./cmd/sendit
./sendit version
```

## Test an endpoint without a config file

`sendit probe` needs no config — it auto-detects the driver from the URL:

```sh
# HTTP (https:// or http://)
./sendit probe https://example.com

# DNS (bare hostname)
./sendit probe example.com

# WebSocket (wss:// or ws://) — connect only
./sendit probe wss://echo.websocket.org

# WebSocket — send a message and measure round-trip latency
./sendit probe wss://echo.websocket.org --send 'ping'
```

Each request prints status, latency, and bytes (HTTP) or RCODE (DNS) or status code (WebSocket). Press Ctrl-C for a summary:

```
Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
^C

--- https://example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 90ms / 142ms
```

See the [CLI Reference](../cli/#probe-flags) for all `probe` flags.

`sendit pinch` checks TCP/UDP port connectivity in the same style — useful for verifying a service is reachable before running traffic against it:

```sh
# TCP (default)
./sendit pinch example.com:80

# UDP
./sendit pinch 8.8.8.8:53 --type udp
```

```
Pinching example.com:80 (tcp) — Ctrl-C to stop

  open            142ms
  open             38ms
^C

--- example.com:80 ---
2 sent, 2 open, 0 closed/filtered
min/avg/max latency: 38ms / 90ms / 142ms
```

See the [CLI Reference](../cli/#pinch-flags) for all `pinch` flags.

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

## Run with Docker

The `docker/` directory in the repository contains a ready-to-use setup. No binary download needed — the image builds from source.

```sh
# Copy and edit the example config
cp docker/config.yaml docker/my-config.yaml

# Build and run
cd docker
docker compose up --build
```

Prometheus metrics and the `/healthz` liveness endpoint are exposed on port **9090**:

```sh
curl localhost:9090/healthz
# {"status":"ok"}
```

To also start Prometheus and Grafana:

```sh
docker compose --profile observability up --build
```

| Service | Port | URL |
|---------|------|-----|
| sendit | 9090 | `http://localhost:9090/metrics` |
| Prometheus | 9091 | `http://localhost:9091` |
| Grafana | 3000 | `http://localhost:3000` |

See [docker/config.yaml](https://github.com/lewta/sendit/blob/main/docker/config.yaml) for the Docker-optimised config example.

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
