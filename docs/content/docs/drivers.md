---
title: "Drivers"
linkTitle: "Drivers"
weight: 4
description: "HTTP, browser, DNS, WebSocket, gRPC, and SFTP driver options and examples."
---

A **driver** is responsible for executing a single request and returning a result. Each target in your config specifies a `type` that selects the driver. All drivers map their results to HTTP-like status codes so the engine's error classifier, backoff, and metrics work uniformly.

## `auth` block

Any target (or `target_defaults`) can include an `auth` block to attach credentials to each request. The `http` and `websocket` drivers honour it; SFTP uses credentials from its `sftp` block; other drivers silently ignore it.

```yaml
targets:
  - url: "https://api.example.com/data"
    type: http
    auth:
      type: bearer
      token_env: API_TOKEN       # resolved from environment at dispatch time
```

| Field | Description |
|---|---|
| `type` | `bearer` \| `basic` \| `header` \| `query` |
| `token` | Literal token value (triggers a startup warning — prefer `token_env` in production) |
| `token_env` | Name of the environment variable holding the token |
| `username` / `username_env` | Basic auth username (literal or env var) |
| `password` / `password_env` | Basic auth password (literal or env var) — optional |
| `header_name` | Header name for `type: header` (e.g. `X-API-Key`) |
| `param_name` | Query parameter name for `type: query` (e.g. `api_key`) |

**Auth types:**

| Type | Effect |
|---|---|
| `bearer` | Adds `Authorization: Bearer <token>` header |
| `basic` | Adds `Authorization: Basic <base64(user:pass)>` header |
| `header` | Adds `<header_name>: <token>` header |
| `query` | Appends `?<param_name>=<token>` to the URL |

Token values are resolved **at dispatch time** — if the env var is unset when a request fires, the result carries an error and no request is made.

**Shared credentials via `target_defaults`:**

```yaml
target_defaults:
  auth:
    type: bearer
    token_env: API_TOKEN

targets_file: "config/targets.txt"
```

All file-loaded targets inherit the shared auth. Inline targets can override or omit it.

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
      allow_cross_host_redirects: false  # opt in to follow redirects to another host
```

| Field | Default | Description |
|---|---|---|
| `method` | `GET` | HTTP verb |
| `headers` | `{}` | Key-value map of request headers |
| `body` | `""` | Optional request body |
| `timeout_s` | `15` | Per-request timeout (seconds) |
| `allow_cross_host_redirects` | `false` | Follow redirects to a different host. Redirected hosts still use per-domain rate limits. Keep disabled when sending auth headers unless that forwarding is intended. |

> **Note:** HTTP header map keys are lowercased by the YAML parser (e.g. `User-Agent` is stored as `user-agent`). This is standard YAML behaviour.

**Non-standard ports:** include the port directly in the URL — Go's `net/http` client handles it natively:

```yaml
- url: "http://internal-api.example.com:8080/health"
  type: http
- url: "https://staging.example.com:8443/api"
  type: http
```

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

The `resolver` field is always `host:port`, so non-standard DNS ports are supported directly:

```yaml
dns:
  resolver: "192.168.1.1:5353"   # custom resolver on non-standard port
  record_type: A
```

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

**Non-standard ports:** include the port in the URL:

```yaml
- url: "wss://stream.example.com:9443/feed"
  type: websocket
```

## `grpc`

Executes a **unary gRPC call** using [google.golang.org/grpc](https://pkg.go.dev/google.golang.org/grpc). No `.proto` files are required — the driver uses [server reflection](https://grpc.io/docs/guides/reflection/) to discover request and response types at runtime, then marshals the JSON body to protobuf automatically.

```yaml
targets:
  - url: grpc://localhost:50051/helloworld.Greeter/SayHello
    weight: 10
    type: grpc
    grpc:
      body: '{"name": "world"}'   # JSON-encoded request (optional — defaults to empty message)
      timeout_s: 15               # per-call timeout in seconds
      tls: false                  # force TLS even when scheme is grpc://
      insecure: false             # skip TLS certificate verification
```

| Field | Default | Description |
|---|---|---|
| `body` | `""` | JSON-encoded request body. Must match the method's input proto type. Empty sends a default-constructed message. |
| `timeout_s` | `15` | Per-call timeout in seconds |
| `tls` | `false` | Force TLS even when the URL scheme is `grpc://` |
| `insecure` | `false` | Skip TLS certificate verification (combine with `tls: true` or `grpcs://` scheme) |

**URL scheme** selects transport security:

| Scheme | Transport |
|---|---|
| `grpc://host:port/Service/Method` | Plaintext |
| `grpcs://host:port/Service/Method` | TLS |

**gRPC status codes** are mapped to HTTP-like status codes so the engine's backoff and error classifier work uniformly:

| gRPC code | HTTP equivalent | Effect |
|---|---|---|
| OK (0) | 200 | success |
| InvalidArgument (3), OutOfRange (11) | 400 | permanent skip |
| Unauthenticated (16) | 401 | permanent skip |
| PermissionDenied (7) | 403 | permanent skip |
| NotFound (5) | 404 | permanent skip |
| AlreadyExists (6) | 409 | permanent skip |
| ResourceExhausted (8) | 429 | transient backoff |
| Unimplemented (12) | 501 | permanent skip |
| Unavailable (14) | 503 | transient backoff |
| DeadlineExceeded (4) | 504 | transient backoff |
| other | 500 | transient backoff |

**Prerequisite:** the gRPC server must have the [server reflection service](https://grpc.io/docs/guides/reflection/) enabled. Most frameworks enable it via a single line (e.g. `reflection.Register(s)` in Go). If reflection is not available, the driver returns an error immediately.

**Connection and descriptor caching:** connections and method descriptors are cached per address+TLS mode. Reflection is called only on the first request to each method; subsequent calls reuse the cached descriptor.

```yaml
# Multiple gRPC targets on the same server — connection is shared
targets:
  - url: grpc://api.example.com:50051/user.UserService/GetUser
    type: grpc
    weight: 8
    grpc:
      body: '{"user_id": "u-123"}'
  - url: grpc://api.example.com:50051/user.UserService/ListUsers
    type: grpc
    weight: 2
    grpc:
      body: '{}'
```

## `sftp`

Executes SFTP operations over SSH using [github.com/pkg/sftp](https://github.com/pkg/sftp) and [golang.org/x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh).

```yaml
targets:
  - url: sftp://sftp.example.com/uploads/test.bin
    weight: 2
    type: sftp
    sftp:
      username: testuser
      password: secret
      insecure: false
      operation: upload
      file_size_min_bytes: 1024
      file_size_max_bytes: 1048576
      allowed_host_key_types: [ssh-ed25519]
```

| Field | Default | Description |
|---|---|---|
| `port` | `22` | SSH port when the URL does not include one |
| `operation` | `upload` | `upload`, `download`, or `list` |
| `timeout_s` | `30` | Connection and operation timeout in seconds |
| `insecure` | `false` | Skip `~/.ssh/known_hosts` host-key verification; use only for trusted test hosts |
| `username` | `""` | SSH username; required |
| `password` | `""` | Password authentication; mutually exclusive with `private_key` |
| `private_key` | `""` | Private key file path or inline PEM string; mutually exclusive with `password` |
| `file_size_bytes` | `0` | Fixed upload payload size; omit or leave 0 to use a range or the default |
| `file_size_min_bytes` | `0` | Minimum random upload payload size |
| `file_size_max_bytes` | `0` | Maximum random upload payload size |
| `eicar` | `false` | Upload the EICAR test string instead of generated data; upload only |
| `allowed_ciphers` | `[]` | SSH cipher allow-list; empty accepts the library defaults |
| `allowed_kex` | `[]` | SSH key-exchange allow-list; empty accepts the library defaults |
| `allowed_host_key_types` | `[]` | SSH host-key algorithm allow-list; empty accepts the library defaults |
| `allowed_macs` | `[]` | SSH MAC allow-list; empty accepts the library defaults |

**Operations:**

| Operation | Effect |
|---|---|
| `upload` | Creates or truncates the remote path and writes generated payload bytes |
| `download` | Reads the remote path and reports bytes transferred |
| `list` | Lists the remote directory and adds `sftp_entry_count` to JSONL metadata |

**Payload sizing:** set `file_size_bytes` for a fixed upload size, or set `file_size_min_bytes` and `file_size_max_bytes` for a random size chosen per request. If no size is set, uploads use a small default payload. Set `eicar: true` only when you intentionally want antivirus or malware-scanning infrastructure to see the EICAR standard test string.

**Host keys:** by default, SFTP verifies host keys against `~/.ssh/known_hosts`. Set `insecure: true` only for trusted lab or ephemeral hosts where pinning is impractical.

**Algorithm policy:** use the `allowed_*` lists to enforce SSH policy. If the server cannot negotiate an allowed algorithm, the result is mapped to `502`.

```yaml
sftp:
  allowed_ciphers: [aes256-gcm@openssh.com, chacha20-poly1305@openssh.com]
  allowed_kex: [curve25519-sha256]
  allowed_host_key_types: [ssh-ed25519]
  allowed_macs: [hmac-sha2-256-etm@openssh.com]
```

**SFTP status codes** are mapped to HTTP-like status codes:

| Condition | HTTP equivalent | Effect |
|---|---|---|
| Success | 200 | success |
| Auth failure | 401 | permanent skip |
| Permission denied | 403 | permanent skip |
| Missing download path | 404 | permanent skip |
| Host key or algorithm policy mismatch | 502 | transient backoff |
| SFTP protocol error | 502 | transient backoff |
| Timeout | 504 | transient backoff |

**JSONL metadata:** SFTP results include `sftp_server_version`, `sftp_host_key_type`, `sftp_host_key_fp`, and `sftp_auth_methods` when available. `list` results also include `sftp_entry_count`.
