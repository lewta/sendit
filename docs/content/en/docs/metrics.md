---
title: "Metrics"
linkTitle: "Metrics"
weight: 5
description: "Prometheus metrics exposed by sendit and how to scrape them."
---

sendit exposes a Prometheus scrape endpoint when `metrics.enabled` is `true`.

## Enable metrics

```yaml
metrics:
  enabled: true
  prometheus_port: 9090
```

Metrics are then available at `http://localhost:9090/metrics`.

## Metric reference

| Metric | Type | Labels | Description |
|---|---|---|---|
| `sendit_requests_total` | Counter | `type`, `status_code` | Total requests dispatched, by driver type and status code |
| `sendit_errors_total` | Counter | `type`, `error_class` | Total errors, by driver type and error class (`transient` or `permanent`) |
| `sendit_request_duration_seconds` | Histogram | `type` | Request latency distribution, by driver type |
| `sendit_bytes_read_total` | Counter | `type` | Total bytes received, by driver type |

### Label values

**`type`** matches the `type` field in your target config: `http`, `browser`, `dns`, or `websocket`.

**`status_code`** is the HTTP status code (e.g. `200`, `429`, `503`) or the DNS-mapped equivalent (see [Drivers — DNS](../drivers/#dns)).

**`error_class`** is one of:
- `transient` — errors that trigger backoff and retry (e.g. HTTP 429/503, DNS SERVFAIL, network failures)
- `permanent` — errors that are logged and skipped with no retry (e.g. HTTP 404, DNS NXDOMAIN)

## Scrape config example

```yaml
# prometheus.yml
scrape_configs:
  - job_name: "sendit"
    static_configs:
      - targets: ["localhost:9090"]
```

## No-op mode

When `metrics.enabled: false` (the default), sendit uses a no-op metrics implementation internally — there are no nil pointer checks and no Prometheus HTTP listener is started.
