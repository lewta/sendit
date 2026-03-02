---
title: "Configuration Reference"
linkTitle: "Configuration"
weight: 2
description: "Every top-level config key with type, default, and description."
---

sendit is configured via a YAML file. Every section has defaults — only override what you need.

See [config/example.yaml](https://github.com/lewta/sendit/blob/main/config/example.yaml) for a fully annotated example.

## `pacing`

Controls how requests are spaced in time. See [Pacing Modes](../pacing/) for details.

| Field | Type | Default | Description |
|---|---|---|---|
| `mode` | string | `human` | `human` \| `rate_limited` \| `scheduled` |
| `requests_per_minute` | float | `20` | Target RPM — used by `rate_limited` and `scheduled` only |
| `jitter_factor` | float | `0.4` | Reserved for future modes; unused in current pacing logic |
| `min_delay_ms` | int | `800` | Minimum inter-request delay for `human` mode (ms) |
| `max_delay_ms` | int | `8000` | Maximum inter-request delay for `human` mode (ms) |
| `schedule` | list | `[]` | Cron windows — required when `mode: scheduled` |

## `limits`

Concurrency and local resource thresholds.

| Field | Type | Default | Description |
|---|---|---|---|
| `max_workers` | int | `4` | Max simultaneous requests across all drivers |
| `max_browser_workers` | int | `1` | Sub-limit for concurrent headless browser instances |
| `cpu_threshold_pct` | float | `60.0` | Pause dispatch when CPU exceeds this percentage |
| `memory_threshold_mb` | int | `512` | Pause dispatch when RAM in use exceeds this value (MB) |

> **Note:** `memory_threshold_mb` defaults to 512 MB. Set it above your system's idle memory footprint (e.g. `8192` on a 16 GB machine) to avoid inadvertently blocking dispatch.

## `rate_limits`

Per-domain token buckets applied after the pacing delay and before acquiring a worker slot.

| Field | Type | Default | Description |
|---|---|---|---|
| `default_rps` | float | `0.5` | RPS applied to all domains not in `per_domain` |
| `per_domain` | list | `[]` | List of `{domain, rps}` overrides |

```yaml
rate_limits:
  default_rps: 0.5
  per_domain:
    - domain: "example.com"
      rps: 0.2
    - domain: "api.example.com"
      rps: 1.0
```

## `backoff`

Retry behaviour on transient errors (HTTP 429/502/503/504, DNS SERVFAIL, network failures).

| Field | Type | Default | Description |
|---|---|---|---|
| `initial_ms` | int | `1000` | Base delay for the first retry (ms) |
| `max_ms` | int | `120000` | Maximum delay cap (ms) |
| `multiplier` | float | `2.0` | Exponential growth factor per attempt |
| `max_attempts` | int | `3` | Stop retrying after this many consecutive failures per domain |

Permanent errors (HTTP 400/403/404, DNS NXDOMAIN/REFUSED) are logged and skipped immediately with no retry.

## `targets`

Inline list of endpoints. Each target has a `weight` for weighted random selection (Vose alias method, O(1) per pick).

```yaml
targets:
  - url: "https://example.com"
    weight: 10
    type: http
    http:
      method: GET
      timeout_s: 15
```

See [Drivers](../drivers/) for per-driver field reference.

## `targets_file` and `target_defaults`

Load targets from a plain-text file instead of (or in addition to) the inline `targets` list.

**File format** — one entry per line: `<url> <type> [weight]`

```
# config/targets.txt
https://example.com      http   5
https://api.example.com  http   3
example.com              dns    2
wss://ws.example.com     websocket
```

`target_defaults` supplies remaining fields for every file-loaded target:

```yaml
targets_file: "config/targets.txt"

target_defaults:
  weight: 1
  http:
    method: GET
    timeout_s: 15
  dns:
    resolver: "8.8.8.8:53"
    record_type: A
```

| `target_defaults` field | Default | Description |
|---|---|---|
| `weight` | `1` | Selection weight when omitted from the file |
| `http.method` | `GET` | HTTP verb |
| `http.timeout_s` | `15` | Request timeout (seconds) |
| `browser.timeout_s` | `30` | Page load timeout (seconds) |
| `dns.resolver` | `8.8.8.8:53` | DNS resolver address |
| `dns.record_type` | `A` | DNS record type |
| `websocket.duration_s` | `30` | How long to hold the connection open (seconds) |

## `output`

Optional result export to a file for offline analysis.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable result export |
| `file` | string | `sendit-results.jsonl` | Output file path |
| `format` | string | `jsonl` | `jsonl` (one JSON object per line) \| `csv` |
| `append` | bool | `false` | Append to an existing file instead of truncating on start |

Each JSONL record contains: `ts`, `url`, `type`, `status`, `duration_ms`, `bytes`, `error`.

## `metrics`

Optional Prometheus exposition endpoint.

```yaml
metrics:
  enabled: true
  prometheus_port: 9090   # GET http://localhost:9090/metrics
```

See [Metrics](../metrics/) for the full metric table.

## `daemon`

Process management settings.

| Field | Type | Default | Description |
|---|---|---|---|
| `pid_file` | string | `/tmp/sendit.pid` | Written by `start` unless `--foreground` is set |
| `log_level` | string | `info` | `debug` \| `info` \| `warn` \| `error` |
| `log_format` | string | `text` | `text` (coloured console) \| `json` |
