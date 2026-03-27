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

Two endpoints are available on the configured port:

| Endpoint | Description |
|----------|-------------|
| `GET /metrics` | Prometheus scrape endpoint |
| `GET /healthz` | Liveness probe — always returns `200 {"status":"ok"}` |

Useful for container health checks:

```sh
curl http://localhost:9090/healthz
# {"status":"ok"}
```

## Metric reference

| Metric | Type | Labels | Description |
|---|---|---|---|
| `sendit_requests_total` | Counter | `type`, `domain`, `status_code` | Total requests dispatched, by driver type, domain, and status code |
| `sendit_errors_total` | Counter | `type`, `domain`, `error_class` | Total errors, by driver type, domain, and error class |
| `sendit_request_duration_seconds` | Histogram | `type`, `domain` | Request latency distribution, by driver type and domain |
| `sendit_bytes_read_total` | Counter | `type` | Total bytes received, by driver type |

> **Breaking change (v0.8.0):** `sendit_requests_total`, `sendit_errors_total`, and `sendit_request_duration_seconds` gained a `domain` label. Update any existing dashboards or alert rules that match these metrics by label set.

### Label values

**`type`** matches the `type` field in your target config: `http`, `browser`, `dns`, `websocket`, or `grpc`.

**`domain`** is the hostname extracted from the target URL (e.g. `example.com`, `api.example.com`). For DNS targets with bare hostnames the value is the hostname itself.

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
