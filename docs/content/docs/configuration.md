---
title: "Configuration Reference"
linkTitle: "Configuration"
weight: 2
description: "Every top-level config key with type, default, and description."
---

sendit is configured via a YAML file. Every section has defaults — only override what you need.

See [config/example.yaml](https://github.com/lewta/sendit/blob/main/config/example.yaml) for a fully annotated example.

All supplied schedule entries are validated even when scheduled pacing is not active. Cron uses the scheduler's standard parser. Schedule duration/RPM, per-domain RPS, and memory thresholds must be positive; domains must not be blank; an enabled Prometheus port must be `1..65535`. Invalid reloads leave the running configuration unchanged.

## `capability_requests`

Optional requests narrow the operator's immutable policy. They never grant authority or choose a mode. Existing configs that omit this block retain the selected operator policy; the default CLI mode is `trusted`. Choose `--config-trust=restricted` with explicit CLI grants for downloaded, CI-supplied, and multi-tenant configs, while observing the [remaining non-sandbox boundaries](../security/#configuration-trust-policy).

```yaml
capability_requests:
  env_names: [API_TOKEN]
  read_roots: [/srv/sendit/inputs]
  output_root: /srv/sendit/results
  overwrite: false
  private_networks: [10.20.0.0/24]
```

Every requested name, directory, overwrite permission, and CIDR must already be covered by the operator ceiling. A request outside it rejects the entire config before secondary files are read. A narrower directory or subnet is allowed. Environment names match `[A-Za-z_][A-Za-z0-9_]*` exactly and case-sensitively. Directories must exist and use absolute, unambiguous paths; symlinks resolve to their canonical locations. Wildcards, relative paths, and `.`/`..` traversal components are rejected. Overwrite is independent of output-directory authority.

On Windows, native drive paths (`C:\Sendit\inputs`) and UNC paths (`\\fileserver\share\inputs`) are supported; use single-quoted YAML strings to preserve backslashes. Native metadata handles resolve symlinks and junctions before containment checks. Drive letters and UNC server/share names compare without ASCII case differences. Existing directory components use the exact canonical spelling returned by Windows, preserving boundaries on case-sensitive filesystems. Device/extended namespaces supplied by callers, drive-relative paths, alternate data streams, reserved device names, repeated interior separators, and trailing dot/space aliases are rejected. Missing output leaves require an existing resolved parent; resolver errors and dangling links fail closed. POSIX path rules are unchanged.

Omit a field to inherit its current ceiling. Use `env_names: []`, `read_roots: []`, `private_networks: []`, `output_root: ""`, or `overwrite: false` to remove authority. `null` is not a request value. Requests require literal mappings and typed values: aliases, anchors, merges, dotted request keys, duplicate/conflicting keys, and unknown request fields are rejected. Operator-shaped fields such as `config_trust`, `policy`, `grants`, and `allow_env` are invalid YAML authority. Ordinary data keys in variables and headers, and existing pacing modes, remain supported.

SIGHUP inherits the last accepted effective policy. An omitted field cannot restore an earlier permission. Rejected reloads keep the running policy; broadening it requires restarting with explicit operator authority. Output settings, including an operator-supplied `--capture` destination, are fixed for the process lifetime; a reload that changes or revokes authority for an active sink is rejected and requires a restart. The config loader reads one snapshot of the YAML and checks request syntax/authority before secondary file reads.

Restricted reads cover `targets_file`, every `vars_file`, file-backed SFTP private keys, and the default `~/.ssh/known_hosts` used when SFTP host-key verification is enabled. Grant the containing absolute directories with `--allow-read-root`; inline SFTP keys do not need a file grant. Secondary target/variable files are limited to 8 MiB and must remain regular files with stable identity while opened. Authentication environment variables are checked again when a request resolves them. Result and capture outputs are rechecked at open/commit and need `--allow-overwrite` before truncating an existing file.

## `pacing`

Controls how requests are spaced in time. See [Pacing Modes](../pacing/) for details.

| Field | Type | Default | Description |
|---|---|---|---|
| `mode` | string | `human` | `human` \| `rate_limited` \| `scheduled` \| `burst` |
| `requests_per_minute` | float | `20` | Target RPM — used by `rate_limited` and `scheduled` only |
| `jitter_factor` | float | `0.4` | Reserved for future modes; unused in current pacing logic |
| `min_delay_ms` | int | `800` | Minimum inter-request delay for `human` mode (ms) |
| `max_delay_ms` | int | `8000` | Maximum inter-request delay for `human` mode (ms) |
| `schedule` | list | `[]` | Validated cron windows — required when `mode: scheduled` |
| `ramp_up_s` | int | `0` | `burst` mode only; linearly decreases inter-request delay to zero without resizing the worker pool; `0` = immediate full-speed dispatch |

## `limits`

Concurrency and local resource thresholds.

| Field | Type | Default | Description |
|---|---|---|---|
| `max_workers` | int | `4` | Max simultaneous requests across all drivers |
| `max_browser_workers` | int | `1` | Sub-limit for concurrent headless browser instances |
| `cpu_threshold_pct` | float | `60.0` | Pause dispatch when CPU exceeds this percentage |
| `memory_threshold_mb` | int | `512` | Positive threshold; pause dispatch when RAM in use exceeds this value (MB) |

> **Note:** `memory_threshold_mb` defaults to 512 MB. Set it above your system's idle memory footprint (e.g. `8192` on a 16 GB machine) to avoid inadvertently blocking dispatch.

## `rate_limits`

Per-domain token buckets applied inside acquired workers, after scheduler pacing and resource admission.

| Field | Type | Default | Description |
|---|---|---|---|
| `default_rps` | float | `0.5` | RPS applied to all domains not in `per_domain` |
| `per_domain` | list | `[]` | List of overrides with a nonblank `domain` and positive `rps` |

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

### Request templating

Custom variables generate varied requests from one target:

```yaml
targets:
  - url: "https://api.example.com/users/{{user_id}}?request={{uuid}}"
    type: http
    weight: 10
    vars:
      user_id: [alice, bob]
    vars_file:
      region: "data/regions.txt"
    http:
      body: '{"region":"{{region}}","sequence":{{seq}},"at":{{timestamp}}}'
```

Each referenced custom variable is selected uniformly once per request and reused in all supported fields. `vars_file` maps names to newline-delimited files; relative paths resolve from the main YAML file's directory. Values are trimmed, blank lines are ignored, and each file must provide at least one value.

Names must match `[a-z][a-z0-9_]*`. `uuid`, `timestamp`, and `seq` are reserved built-ins for a random UUIDv4, Unix epoch seconds, and a per-target sequence starting at 1. Sequences reset on process start and successful reload. Expansion is single-pass, so placeholders inside selected values remain literal.

Configuration validation rejects duplicate `vars`/`vars_file` names, invalid or empty variables, unreadable or empty files, malformed placeholders, and unknown names. Supported fields are target URLs, `http.body`, `grpc.body`, and `websocket.send_messages`.

## `targets_file` and `target_defaults`

Load targets from a plain-text file instead of (or in addition to) the inline `targets` list.

**File format** — one entry per line: `<url> <type> [weight]`

```
# config/targets.txt
https://example.com             http      5
https://api.example.com         http      3
example.com                     dns       2
wss://ws.example.com            websocket
grpc://svc.example.com:50051/helloworld.Greeter/SayHello   grpc   4
sftp://sftp.example.com/uploads/test.bin                   sftp   2
```

`target_defaults` supplies remaining fields for every file-loaded target:

```yaml
targets_file: "config/targets.txt"

target_defaults:
  weight: 1
  vars:
    environment: [staging]
  vars_file:
    region: "config/regions.txt"
  http:
    method: GET
    timeout_s: 15
  dns:
    resolver: "8.8.8.8:53"
    record_type: A
  sftp:
    port: 22
    operation: upload
    timeout_s: 30
    insecure: false
    username: testuser
    password: secret
```

| `target_defaults` field | Default | Description |
|---|---|---|
| `weight` | `1` | Selection weight when omitted from the file |
| `vars` | `{}` | Shared inline template candidates for file-loaded targets |
| `vars_file` | `{}` | Shared variable-to-file mappings for file-loaded targets |
| `auth.type` | `""` | Auth type: `bearer` \| `basic` \| `header` \| `query` — see [Drivers](../drivers/#auth-block) |
| `http.method` | `GET` | HTTP verb |
| `http.http_version` | `0` | `0` automatic, `1` HTTP/1.1, `2` HTTPS-only HTTP/2 without fallback |
| `http.timeout_s` | `15` | Request timeout (seconds) |
| `http.allow_cross_host_redirects` | `false` | Follow redirects to a different host. Redirected hosts still use per-domain rate limits. HTTPS-to-HTTP redirects are always rejected before a cleartext request is sent. |
| `browser.timeout_s` | `30` | Page load timeout (seconds) |
| `dns.resolver` | `8.8.8.8:53` | DNS resolver address |
| `dns.record_type` | `A` | DNS record type |
| `websocket.duration_s` | `30` | How long to hold the connection open (seconds) |
| `grpc.timeout_s` | `15` | Per-call timeout (seconds) |
| `grpc.reflection_max_descriptors` | `256` | Maximum descriptors accepted from one reflection response; configurable up to `4096` |
| `grpc.reflection_max_bytes` | `4194304` | Maximum aggregate encoded descriptor bytes; configurable up to `16777216` |
| `grpc.reflection_max_dependency_depth` | `64` | Maximum local descriptor-import depth; configurable up to `256` |
| `sftp.port` | `22` | SSH port when the URL omits one |
| `sftp.operation` | `upload` | Operation: `upload` \| `download` \| `list` |
| `sftp.timeout_s` | `30` | SFTP connection and operation timeout (seconds) |
| `sftp.insecure` | `false` | Skip `~/.ssh/known_hosts` host-key verification; use only for trusted test hosts |

HTTP `http_version` is also accepted under inline `targets[].http`. It must be an actual YAML integer 0, 1, or 2, including in aliases/merged mappings and unused defaults. Strings, booleans, nulls, floats and out-of-range values are invalid. Defaults apply only to target-file entries. Mode 2 rejects known non-HTTPS URLs during config validation; templated schemes are checked after expansion at request time. See [HTTP driver selection](../drivers/#http-version-selection).

## `output`

Optional result export to a file for offline analysis.

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable result export |
| `file` | string | `sendit-results.jsonl` | Output file path |
| `format` | string | `jsonl` | `jsonl` (one JSON object per line) \| `csv` |
| `append` | bool | `false` | Append to an existing file instead of truncating on start |

Each JSONL record contains: `ts`, `url`, `type`, `status`, `duration_ms`, `bytes`, `error`. Drivers may add metadata fields; SFTP records include SSH handshake metadata and `sftp_entry_count` for list operations.

CSV and JSONL remove URL userinfo, including from malformed URLs, and replace associated error text with a generic omission message. Other URL data, bodies, messages, metadata, and logs are not covered by this redaction.

### Replay JSONL envelope

New engine results write envelope **v2**; v1 captures remain readable with automatic HTTP policy. V2 requires `http_version` in HTTP snapshots and captures requested policy, independently of the observed top-level `http_protocol`. The top-level fields and integer `duration_ms` remain compatible with existing consumers and PCAP export. V1-only sendit binaries reject v2 envelopes:

```json
{"ts":"2026-10-08T21:16:34Z","url":"https://example.com/users/1","type":"http","status":200,"duration_ms":42,"bytes":123,"http_protocol":"HTTP/2.0","replay":{"version":2,"run_id":"405d88de-3fb2-42d8-bfb7-f365907ed78c","sequence":1,"started_at":"2026-10-08T21:16:33.123456789Z","replayable":true,"request":{"url":"https://example.com/users/1","type":"http","http":{"http_version":2,"method":"POST","body":"{\"id\":1}","timeout_s":15,"allow_cross_host_redirects":false}}}}
```

`run_id` identifies one invocation; reload preserves it and the positive dispatch counter. `started_at` is captured after admission waits, before driver execution, from a wall-clock anchor plus monotonic elapsed time. File order is result serialization order. The request contains only its active driver block, with explicit effective defaults and already-expanded URL/body/messages; variable maps, auth, and custom headers are not serialized.

| Driver block | Required v2 fields |
|---|---|
| `http` | `http_version`, `method`, `body`, `timeout_s`, `allow_cross_host_redirects` |
| `browser` | `scroll`, `wait_for_selector`, `timeout_s` |
| `dns` | `resolver`, `record_type` |
| `websocket` | `duration_s`, `send_messages`, `expect_messages` |
| `grpc` | `body`, `timeout_s`, `tls`, `insecure`; optional reflection limits are preserved when present |

Every listed field is required and non-null, including false/empty values. Non-replayable envelopes instead have `replayable: false` and a `reason`, without `request`. Reasons are `auth_configured`, `custom_headers`, `url_userinfo`, `unsupported_type` (including SFTP), or `invalid_request`. Ad hoc results without capture metadata omit the envelope and cannot be replayed. `replay` is reserved against driver metadata overrides.

V1 HTTP snapshots have the same fields except `http_version`, which remains forbidden in v1 and is interpreted as automatic. Other driver blocks have identical required fields in both versions. New gRPC captures also preserve `reflection_max_descriptors`, `reflection_max_bytes`, and `reflection_max_dependency_depth`; older captures without those optional fields use the secure defaults. V2 accepts only HTTP versions 0–2 and requires an HTTPS URL for 2. Invalid policies fail full-file preflight before any replay traffic or output truncation.

Use `append: false`: the replay command accepts exactly one run and rejects mixed appended sessions. Ordinary engine output remains asynchronous/non-blocking and can drop results when full; sequence gaps are allowed, but missing source requests cannot be recovered. Ordinary output uses `0600` only when creating a file and does not tighten existing permissions. CSV stays result-only. The [replay command](../cli/#replay-flags) has stricter output checks and lossless delivery on success.

## `metrics`

Optional Prometheus exposition endpoint.

```yaml
metrics:
  enabled: true
  bind_address: 127.0.0.1
  prometheus_port: 9090  # when enabled, must be 1..65535
```

When enabled, two endpoints are served on `bind_address:prometheus_port`:

| Endpoint | Description |
|---|---|
| `GET /metrics` | Prometheus scrape endpoint |
| `GET /healthz` | Liveness probe — always returns `200 {"status":"ok"}` |

Metrics bind to loopback by default. Set `bind_address: 0.0.0.0` only when you intentionally expose the endpoint to another host or container network.

See [Metrics](../metrics/) for the full metric reference and label descriptions.

## `daemon`

Process management settings.

| Field | Type | Default | Description |
|---|---|---|---|
| `pid_file` | string | *per-user cache*/`sendit/sendit.pid` | Locked process record written by `start` unless `--foreground` is set; trusted configs may use explicit paths on lock-capable local storage, while restricted configs require the per-user default |
| `log_level` | string | `info` | `debug` \| `info` \| `warn` \| `error` |
| `log_format` | string | `text` | `text` (coloured console) \| `json` |
