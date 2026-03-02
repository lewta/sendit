---
title: "Drivers"
linkTitle: "Drivers"
weight: 4
description: "HTTP, browser, DNS, and WebSocket driver options and examples."
---

A **driver** is responsible for executing a single request and returning a result. Each target in your config specifies a `type` that selects the driver. All drivers map their results to HTTP-like status codes so the engine's error classifier, backoff, and metrics work uniformly.

## `http`

Sends an HTTP/HTTPS request using Go's standard `net/http` client.

```yaml
targets:
  - url: "https://example.com/api"
    weight: 5
    type: http
    http:
      method: GET                        # GET | POST | PUT | DELETE | ...
      headers:
        User-Agent: "Mozilla/5.0 ..."
        Accept: "application/json"
      body: '{"key":"value"}'            # optional request body (string)
      timeout_s: 15                      # per-request timeout in seconds
```

| Field | Default | Description |
|---|---|---|
| `method` | `GET` | HTTP verb |
| `headers` | `{}` | Key-value map of request headers |
| `body` | `""` | Optional request body |
| `timeout_s` | `15` | Per-request timeout (seconds) |

> **Note:** HTTP header map keys are lowercased by the YAML parser (e.g. `User-Agent` is stored as `user-agent`). This is standard YAML behaviour.

## `browser`

Loads a page in a headless Chromium instance via [chromedp](https://github.com/chromedp/chromedp). Each task spawns its own `ExecAllocator` — no shared browser state — which prevents memory accumulation across long runs.

```yaml
targets:
  - url: "https://news.ycombinator.com"
    weight: 3
    type: browser
    browser:
      scroll: true                    # scroll to mid-page then bottom
      wait_for_selector: "#hnmain"    # CSS selector to wait for before returning
      timeout_s: 30                   # page load timeout in seconds
```

| Field | Default | Description |
|---|---|---|
| `scroll` | `false` | Scroll to mid-page then bottom after load |
| `wait_for_selector` | `""` | Wait for this CSS selector to be visible |
| `timeout_s` | `30` | Page load timeout (seconds) |

**Prerequisite:** Chrome or Chromium must be installed on the machine running sendit.

Use `max_browser_workers` in `limits` to cap concurrent browser instances independently of the global worker pool:

```yaml
limits:
  max_workers: 4
  max_browser_workers: 1   # at most 1 Chrome instance at a time
```

## `dns`

Resolves a hostname using [miekg/dns](https://github.com/miekg/dns) directly — no system resolver involved. DNS RCODEs are mapped to HTTP-like status codes:

| DNS RCODE | HTTP equivalent | Effect |
|---|---|---|
| NOERROR (0) | 200 | success |
| NXDOMAIN (3) | 404 | permanent skip (no retry) |
| REFUSED (5) | 403 | permanent skip (no retry) |
| SERVFAIL (2) | 503 | transient backoff |
| other | 502 | transient backoff |

```yaml
targets:
  - url: "example.com"
    weight: 3
    type: dns
    dns:
      resolver: "8.8.8.8:53"    # DNS server address
      record_type: A             # A | AAAA | MX | TXT | CNAME | ...
```

| Field | Default | Description |
|---|---|---|
| `resolver` | `8.8.8.8:53` | DNS server `host:port` |
| `record_type` | `A` | DNS record type to query |

## `websocket`

Opens a WebSocket connection using [coder/websocket](https://github.com/coder/websocket), optionally sends messages, and holds the connection open for a configurable duration.

```yaml
targets:
  - url: "wss://stream.example.com/feed"
    weight: 2
    type: websocket
    websocket:
      duration_s: 30                            # hold connection for this many seconds
      send_messages: ['{"type":"subscribe"}']   # messages to send on connect
      expect_messages: 1                        # wait to receive this many messages
```

| Field | Default | Description |
|---|---|---|
| `duration_s` | `30` | How long to hold the connection open (seconds) |
| `send_messages` | `[]` | List of text messages to send after connecting |
| `expect_messages` | `0` | Minimum messages to receive before considering success |
