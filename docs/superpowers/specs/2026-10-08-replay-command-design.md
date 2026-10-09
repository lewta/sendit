# Replay Command Design

## Goal

Add `sendit replay` so a future replay-capable JSONL result file can reproduce recorded dispatch order, relative scheduling, and expanded request data. Replay supports rate scaling, source-status filtering, looping, cancellation, and optional result output. Concurrent execution is best-effort: operating-system scheduling and remote services determine actual driver-entry, network-arrival, and completion order.

Issue [#253](https://github.com/lewta/sendit/issues/253) is the canonical tracker.

**Review status:** approved by the user after the code-grounded completeness review. Approval covers future-record-only replay, exclusion of auth/custom headers/SFTP, concurrent scheduling, a configurable one-second loop delay, run identity, explicit input limits, strict executable-field validation, and lossless replay output. Implementation planning follows this contract.

## Compatibility Contract

Current JSONL records contain result telemetry but omit request methods, bodies, headers, driver settings, precise start times, and dispatch order. Reconstructing those requests with defaults would silently change behavior. Replay therefore accepts only the versioned records introduced by this change and rejects legacy records with a line-specific explanation.

The existing top-level result fields remain compatible for current consumers. Each captured engine/replay JSONL result adds a nested replay envelope:

```json
{
  "ts": "2026-10-08T21:16:34Z",
  "url": "https://example.com/users/alice",
  "type": "http",
  "status": 200,
  "duration_ms": 42,
  "bytes": 123,
  "replay": {
    "version": 1,
    "run_id": "405d88de-3fb2-42d8-bfb7-f365907ed78c",
    "sequence": 42,
    "started_at": "2026-10-08T21:16:33.123456789Z",
    "replayable": true,
    "request": {
      "url": "https://example.com/users/alice",
      "type": "http",
      "http": {
        "method": "POST",
        "body": "{\"id\":\"7d8c...\"}",
        "timeout_s": 15,
        "allow_cross_host_redirects": false
      }
    }
  }
}
```

The request snapshot contains the expanded URL and only the active driver's behavior-affecting settings. Template definitions, candidate values, and variable-file paths are not stored. Driver-specific fields use the same snake-case names and value semantics as configuration YAML.

Replay output uses the same schema and can itself be replayed.

The existing telemetry types remain unchanged, including integer `duration_ms`. CSV remains result-only. Existing JSONL-to-PCAP conversion must tolerate the additive envelope. `replay` is a reserved metadata key that `Result.Meta` cannot override.

### Version 1 Request Fields

Every executable snapshot has `url`, `type`, and exactly one matching driver block. All fields listed below are required and non-null, including false booleans, empty strings, and empty message arrays. Capture materializes effective driver defaults rather than relying on future config-loader defaults. For file-loaded targets, retain their already-applied defaults; the fallback column applies only to unset values as interpreted by the current driver.

| Block | Complete allowed fields | Current driver fallback to materialize |
| --- | --- | --- |
| `http` | `method`, `body`, `timeout_s`, `allow_cross_host_redirects` | Empty method becomes `GET`; nonpositive timeout becomes 15 seconds |
| `browser` | `scroll`, `wait_for_selector`, `timeout_s` | Nonpositive timeout becomes 30 seconds |
| `dns` | `resolver`, `record_type` | Empty resolver becomes `8.8.8.8:53`; empty type becomes `A`; record type is uppercased |
| `websocket` | `duration_s`, `send_messages`, `expect_messages` | Nonpositive duration becomes 10 seconds; nil messages become an empty array |
| `grpc` | `body`, `timeout_s`, `tls`, `insecure` | Nonpositive timeout becomes 15 seconds; effective TLS is true for `grpcs://` or `tls: true` |

HTTP same-host redirects retain existing driver behavior; `allow_cross_host_redirects` controls only cross-host redirects. Replay preserves literal expanded data, including any remaining `{{...}}` text, without reading variable files or running template parsing again. It does not call `config.Load` to interpret the snapshot.

## Security Boundary

The added envelope does not persist configured credentials or custom headers. URLs, bodies, messages, and existing driver error text can contain sensitive application data; this is not a general secret detector. A result is marked `replayable: false`, includes a stable reason, and omits `request` when its task has any of these properties:

- Any populated `AuthConfig` field, even when `auth.type` is empty.
- Custom HTTP headers, even in an inactive HTTP block. WebSocket currently has no independent custom-header block; its configured headers come from auth and are already excluded.
- URL userinfo, including username-only userinfo, which can implicitly enable HTTP Basic authentication.
- An SFTP target, because every useful SFTP request requires credentials or key material.

Replay fails before dispatch if any selected record is marked non-replayable. A source filter may exclude such a record. No flag bypasses this restriction; persisting secrets is outside this feature.

Eligibility checks also run during decoding: an input's `replayable: true` claim cannot authorize auth, headers, SFTP, or URL userinfo. The executable wire structs contain no auth or header fields. Reasons are stable codes, evaluated in this order: `auth_configured`, `custom_headers`, `url_userinfo`, `unsupported_type`, `invalid_request`. Invalid expanded request shapes produce ordinary source results marked non-replayable, rather than preventing normal engine dispatch or emitting a misleading executable snapshot.

For URL-userinfo records, JSONL top-level `url` removes userinfo and top-level `error` uses a generic omission message if an error exists, rather than serializing potentially credential-bearing URL error text. This narrowly scoped exception is tested; redacting arbitrary application text and unrelated log output is outside this change.

Ambient environment and transport state are not captured: proxy settings/credentials, trust stores, browser installations, network resolution, and remote service state may differ at replay time. Existing drivers retain their current environment behavior. Documentation must describe this limit rather than promise credential-free network traffic or identical responses.

Unauthenticated HTTP, browser, DNS, WebSocket, and gRPC requests are replayable. Unknown target types fail validation rather than being skipped or run with defaults.

## Capture Semantics

The engine records metadata immediately before calling `driver.Execute`, after backoff and per-domain rate-limit waits. A short engine-local critical section assigns a positive sequence number and a dispatch timestamp together. Capture time uses one UTC wall-clock anchor plus monotonic elapsed time from that anchor, formatted as RFC3339Nano, so wall-clock corrections do not create backward timestamps or artificial gaps. The lock is released before `Execute`; the sequence represents dispatch admission, not guaranteed driver-entry order.

The resulting `task.Result` carries this metadata to the output writer. The writer creates the safe replay envelope from the already-expanded `task.Task`; it does not invoke template expansion again. This preserves selected variable values, UUIDs, timestamps, and sequence substitutions exactly.

Each engine invocation generates a UUIDv4 `run_id`; reload retains the run ID, time anchor, and capture counter. Sequence numbers may contain gaps when ordinary engine telemetry drops results through its existing non-blocking writer. Replay requires positive, unique sequence values, sorts records by sequence, and does not require contiguous values. File order is result serialization order and need not match dispatch order.

All input records must have one run ID. Files appended across engine invocations are explicitly rejected as mixed-run input, even if filtering would select only one run. Run selection and session merging are deferred; examples use `output.append: false`.

A replay invocation receives a fresh run ID and capture clock; its output sequences continue across every loop and never copy source sequences. Original capture drops cannot be recovered. Successful replay output is lossless for executed records, subject to the same published input limits when replayed later.

## Command Interface

```text
sendit replay --input results.jsonl [flags]

Flags:
  --input string          replay-capable JSONL file (required)
  --rate float            timing multiplier greater than zero (default 1)
  --filter string         source-result filter; supported value: status=5xx
  --loop                  repeat the selected requests until interrupted
  --loop-delay duration   delay between completed loops (default 1s)
  --output string         write replay results as JSONL
```

`--rate 2` halves original start gaps; `--rate 0.5` doubles them. Zero, negative, NaN, and infinite values are invalid. No filter replays every record. `status=5xx` selects source records with status 500 through 599; it does not include status zero, 429, or other errors.

Input and output must identify different regular files. Check canonical paths and existing file identity (`os.SameFile`) to catch relative, symlink, and hard-link aliases. Open the destination without truncation, verify its identity against the open input, then truncate only after successful preflight. No output is created or truncated for invalid input or flags. Concurrent external replacement of filesystem entries is not a supported workflow.

New replay output is created with mode `0600`. On POSIX, existing output with any group/other permission bits is rejected with a clear diagnostic before truncation; do not silently change its permissions. Windows retains native filesystem access controls. Ordinary engine output retains its existing creation-only permission policy, which documentation must distinguish from the replay command's policy.

Replay output is JSONL regardless of filename extension. No normal sendit configuration file is loaded.

## Parsing And Scheduling

Replay reads and validates the complete input before sending traffic or opening output destructively. The parser reports the JSONL line and field without echoing body/credential values.

### Structural Validation

- Every physical line contains one UTF-8 JSON object. CRLF and an unterminated final line are accepted; invalid UTF-8, blank/whitespace-only lines, trailing JSON values, and duplicate keys at any object depth are rejected.
- Every line, including filtered-out records, must be structurally valid. Require top-level `url`, `type`, and integer `status`, plus the v1 envelope. Other existing telemetry and top-level driver metadata remain compatible and are not interpreted as executable settings.
- Require non-null `version`, canonical UUIDv4 `run_id`, positive integer `sequence`, RFC3339Nano `started_at`, and boolean `replayable`. Sequences are parsed exactly as `uint64`, never through a floating-point map. Fractions, negatives, and overflow are errors.
- Reject unknown fields inside the envelope, request, and driver block. A replayable envelope requires a non-null request and forbids `reason`; a non-replayable envelope requires a known reason and forbids `request`. Unknown target types are errors; SFTP is recognized only as non-replayable.
- Top-level and request URL/type must agree. Only the matching driver block is permitted. Auth, headers, vars, vars_file, and additional driver blocks are not valid request fields.
- Validate the complete file for one run ID, unique sequence values, and nondecreasing timestamps in sequence order, before filtering. Zero gaps and sequence gaps are permitted.
- Only the eligibility error for a valid non-replayable record is filter-dependent. An excluded malformed envelope still fails preflight.

### Executable Request Validation

Validate without contacting destinations: scheme and host for HTTP/browser (`http`/`https`), WebSocket (`ws`/`wss`), and gRPC (`grpc`/`grpcs`); explicit numeric ports are in 1–65535; URL userinfo is forbidden. gRPC paths must identify a nonempty service and method (`/Service/Method`). HTTP method must be a nonempty valid HTTP token. DNS uses a valid question name, recognized record type, and host:port resolver syntax with a valid numeric port. Reject negative counts, invalid types, and unsafe duration arithmetic.

HTTP bodies and WebSocket messages remain literal arbitrary strings. gRPC reflection, protobuf-body validation, DNS/network errors, CSS selector execution, TLS verification, Chrome availability, and destination behavior are execution-time results. An invalid gRPC body can intentionally reproduce a recorded driver failure; offline preflight does not consult reflection or pretend to validate a remote schema.

### Input And Numeric Limits

V1 uses fixed, documented limits: 256 MiB per input file, 8 MiB per JSONL line excluding its line ending, and 10,000 records before filtering. Enforce limits during reads, not only with file-size checks. Load the bounded records into memory once for validation, sorting, and looping; no streaming side effects occur before preflight. Larger capture files require deliberate preprocessing to one bounded run. These limits avoid an unbounded parser and bound scheduled work without introducing a timing-altering worker queue.

All driver timeout/duration values are positive whole seconds after normalization and must fit both the platform `int` and their actual `time.Duration` arithmetic; WebSocket's additional 30 seconds must also fit. `expect_messages` is a nonnegative platform `int`. Reject capture spans that cannot be represented as a nonnegative `time.Duration`, detecting overflow rather than accepting `Time.Sub` saturation. Reject any scaled offset outside that range even when the rate is finite and positive. Compute/validate every selected offset before output setup or dispatch. Round representable fractional nanoseconds down; collapsed offsets use sequence order. Sequence-counter exhaustion ends replay with an error instead of wrapping.

After filtering, records are ordered by sequence. Their scheduled offset is:

```text
(record.started_at - first_selected.started_at) / rate
```

This preserves elapsed gaps between selected requests rather than compacting filtered traffic. Anchor each cycle's deadlines to a monotonic clock so request duration and output latency do not accumulate as pacing drift. At each deadline, replay launches `driver.Execute` in a goroutine, allowing requests to overlap. Equal deadlines launch in sequence order; overdue deadlines launch as soon as possible in that order, without dropping requests. Network arrival order and exact deadlines cannot be guaranteed by concurrent execution.

Replay invokes drivers directly and does not run the normal scheduler, backoff, rate limiter, resource gate, or metrics server because those would alter recorded timing. Up to the accepted record count may execute concurrently; this is an explicit resource ceiling, not a promise that arbitrary captures fit every machine. Driver constructors are created once per invocation and reused across loops.

One cycle waits for every active request to finish. With `--loop`, replay then waits `--loop-delay` and schedules the next cycle relative to a new cycle start. The delay is independent of `--rate`, must not be negative, and may be zero by explicit choice. Cancellation stops further scheduling, interrupts timing waits, propagates to active `Execute` calls, waits for replay-owned workers, and finalizes output.

Shutdown inherits driver behavior: DNS can leave an internal exchange pending until its timeout, WebSocket closing can take additional time, and some drivers may report a status without a cancellation error. Replay does not promise immediate network quiescence, rewrite returned statuses, or repair general driver lifecycle behavior as part of this feature.

## Results And Failures

Driver failures are ordinary replay results. Count a failure when `Result.Error != nil` or `ratelimit.ClassifyStatusCode` reports a transient or permanent error. Thus HTTP 500, DNS SERVFAIL, and mapped gRPC errors count even without Go errors. Count all completed `Execute` calls, including calls returning after cancellation, using the same predicate. One failed request does not suppress later starts. Source filtering depends only on the source status, not this new outcome.

Replay returns an error before dispatch for malformed input, legacy records, selected non-replayable records, invalid flags, unsupported types, or output setup failures. `--loop` with an empty input or a filter matching no records is rejected to prevent a busy loop. A non-looping replay with no selected records succeeds and reports zero requests.

Print a final request/failure count to command stdout after workers finish, with or without `--output`; runtime driver diagnostics may still go to stderr. A completed replay returns success even if requests fail. Signal cancellation and command-context cancellation are clean stops unless an independent input/output error occurred. Setup, parsing, numeric, and output errors produce a nonzero exit status.

### Reliable Replay Output

Reuse the ordinary JSONL record conversion/encoder, not `output.Writer.Send`'s lossy queue. Replay serializes result writes through a single error-reporting encoder (for example, a mutex-protected writer). Writing occurs after `Execute`, outside the scheduling path. Successful `--output` writes one record per executed request, including failures and cancellation results; more than 512 rapid completions must not drop data.

On the first encode/write failure, cancel further scheduling and active work, collect workers, finalize the file, and return an output error. Always check flush and close errors and preserve them alongside an earlier failure. Partial output may remain on error and is reported as such. Signal cancellation must not hide an output error. Crash/power-loss durability, atomic replacement, and `fsync` are not promised.

Ordinary engine telemetry keeps its current non-blocking/drop policy. Ad hoc results lacking capture metadata omit the envelope and are rejected as missing capture metadata by replay, like legacy records; the writer does not fabricate start times. Normal engine and replay paths must always attach capture metadata.

## Code Organization

- `internal/task` carries capture run ID, dispatch sequence, and anchored-monotonic timestamp on results, with a small shared concurrency-safe capture-clock helper for engine and replay.
- `internal/engine` captures those fields immediately before driver execution.
- `internal/output` owns the versioned JSONL wire structs, safe request-snapshot conversion, strict JSONL decoding, and common error-returning JSONL encoding. Ordinary asynchronous output and reliable replay output share record encoding rather than delivery semantics.
- `cmd/sendit` owns Cobra flags, signal handling, driver construction, scheduling, looping, and the final summary.

Existing driver implementations and the `driver.Driver` interface remain unchanged. The command uses the existing constructors for HTTP, browser, DNS, WebSocket, and gRPC. No new dependency is required.

## Acceptance Criteria And Planned Test Coverage

Tests below are planned, not implemented or executed. Reuse Go's `testing` package, existing local-server helpers, and `testing/synctest` for deterministic timer/worker checks; no third-party test or clock framework. Integration tests assert real driver behavior using local services. Concurrent tests assert dispatch order and overlap rather than deterministic server-arrival order.

| Requirement | Planned assertions | Location / level |
| --- | --- | --- |
| Cobra command and flags (#253) | Registration, help, all defaults, no positional arguments, missing input, unsupported filter, invalid rates/delays; invalid nearby config is never loaded | `cmd/sendit/replay_test.go`, unit |
| Versioned JSONL and malformed-input errors (#253) | Valid round-trip; legacy, absent/null fields, wrong types, duplicate keys, trailing values, blank lines, CRLF, final line without newline; error includes line/field but not payload | `internal/output/replay_test.go`, unit |
| Preflight before traffic | Executable first line followed by malformed last line or schedule overflow invokes no driver and leaves an existing output unchanged; excluded malformed records also fail | Command tests plus local HTTP integration |
| Strict executable snapshots | Required driver fields, mismatched URL/type, unknown/inactive blocks, injected auth/headers/SFTP, invalid method/scheme/port/DNS/gRPC path, literal template text preserved | Output parser and round-trip unit tests |
| Credential exclusions | Every auth mode, partial auth fields, env references, headers on active/inactive blocks, URL userinfo, SFTP; no request snapshot or secrets serialized; userinfo removed from JSONL URL/error; forged eligibility rejected | Output encoder/decoder unit tests |
| Supported driver fidelity (#253 dispatch reuse) | Nondefault fields and normalized defaults survive encode/decode for HTTP, browser, DNS, WebSocket, gRPC; compare to actual driver fallback behavior | Output unit tests |
| Run identity and completion order | Shuffled lines sort by sequence; gaps accepted; duplicates rejected; mixed appended runs rejected with or without filter; reload retains run/counter; replay loops use fresh, continuing capture metadata | Capture helper unit/race, output unit, engine integration |
| Clock stability and timing (#253) | Wall-clock jumps do not affect capture gaps; rates 0.5/1/2, equal and overdue deadlines, slow first request overlapping second, filtered gaps retained without accumulated duration drift | Capture helper and command scheduler tests using controlled elapsed time / `synctest` |
| Source filtering (#253) | Status 500 and 599 included; 499, 600, 0, 429 excluded; first selected offset is zero; excluded non-replayable record accepted; selected one errors | Command/parser unit tests |
| Arithmetic and size boundaries | Positive values above `2^53` stay exact; uint64 overflow/fractions; extreme timestamps; duration arithmetic including WebSocket +30; tiny/huge finite rates, NaN/Inf; >64 KiB payload accepted; file/line/count limits and one-over rejected without executing | Output and command table tests, bounded reader tests |
| Looping (#253) | Waits for all calls then default/explicit delay; rate does not scale loop delay; explicit zero allowed; negative rejected; empty/all-filtered loop errors; multi-loop output decodes and replays | Deterministic command tests |
| Request failures and summaries | HTTP 500, DNS SERVFAIL, gRPC non-OK, network errors counted; later records still launch; request failures alone exit successfully; output and no-output summaries agree | Unit plus local protocol integration |
| Cancellation | Already-canceled command context; pending deadline; active call; loop delay; deadline/cancellation race; no new dispatch after observed cancellation; workers collected before final summary/file close; output errors take precedence | Deterministic command tests, local-driver integration, one subprocess SIGINT smoke test |
| Reliable output (#253) | More than 512 rapid results retained; slow writer does not pace dispatch; encode/write/flush/close errors surfaced; cancellation drains started results; single encoder has no data race; resulting output is replayable | Unit tests with failing writer, local replay round-trip |
| Input protection | Same, relative, symlink, and hard-link aliases rejected before truncation; invalid input preserves output; directories/nonregular paths fail; new mode 0600 and existing permissive POSIX output rejection | Command filesystem tests; platform-specific assertions gated |
| HTTP end-to-end (#253) | Capture templated POST to local server, replay expanded method/body/URL without regeneration; same-host and cross-host redirect policy; preserve overlapping schedule with broad real-time tolerances; decode new results | `cmd/sendit/replay_integration_test.go`, integration |
| Other drivers end-to-end | Local DNS resolver/record-type fidelity, WebSocket messages, reflecting gRPC body/TLS mode; errors recorded; no public endpoints | Command integration using existing driver test patterns |
| Browser coverage boundary | Snapshot round-trip and dispatch selection tested without Chrome; separate local-page Chrome smoke skipped with explicit reason when unavailable | Unit plus opt-in/browser-available integration; do not report full browser execution coverage from mocks |
| Capture boundary | Metadata assigned after limiter/backoff waits and before `Execute`; immediate failures still captured; reverse completion order; expanded data and source config immutability under `-race` | `internal/engine/integration_test.go` and capture helper tests |
| Existing consumers | Integer duration unchanged, additive metadata accepted by PCAP exporter, CSV schema unchanged, metadata cannot overwrite `replay`; ordinary writer defaults/drop semantics retained | Existing output/PCAP suites extended |
| Parser robustness | Seed valid records for all types, nested bodies, partial envelopes, duplicates, forged eligibility, numeric extremes, invalid UTF-8 and truncated JSON; no panics/unbounded reads and accepted values satisfy validation invariants | `internal/output/replay_fuzz_test.go`, `FuzzReplayRecord` |

### Verification Wiring

- Change `make integration` to run `go test -tags integration -race -v ./...`, matching CI's existing all-package integration scope. This ensures required `make verify` actually executes command replay integration tests.
- Add `bash scripts/fuzz.sh -fuzz=FuzzReplayRecord -fuzztime=30s ./internal/output/` to CI's explicitly enumerated fuzz job and run it locally during implementation verification.
- Run focused unit/integration checks during development, then `make verify` before the PR. Race coverage must include concurrent capture, scheduling, cancellation, and output writing.
- Use local protocol servers and deterministic scheduling checks rather than external endpoints or exact wall-clock sleeps. Report skipped browser execution coverage explicitly.
- Add capture-helper/record-encoding benchmarks to quantify per-request capture overhead; compare ordinary and replay-capable JSONL without inventing an arbitrary performance gate.

## Documentation And Issue Hygiene

Update root/command help, `README.md`, Hugo CLI/configuration/drivers/security pages, and `CHANGELOG.md` together. Add a runnable capture-to-replay example using `config/example.yaml` output settings (`output.enabled`, `file`, `format: jsonl`, `append: false`) and a small replayable JSONL fixture. There is no `sendit start --output` flag to document.

Document credential exclusions, application-data sensitivity, ambient environment limits, bounded input, mixed-run rejection, best-effort concurrent ordering, result failure counts, output error semantics, and the creation-only permissions/drop policy of ordinary engine output.

Move released v1.7.0 from Planned to Completed in `ROADMAP.md`, fixing its TOC/heading anchors. Mark Replay complete only when implementation and verification finish; update its description to future-record-only replay and link #253. Keep unrelated research items open. Record the approved design/scope in #253 and use `Closes #253` in the implementation PR, verifying automatic closure after merge.

## Review Disposition

The review identified contracts missing from the initial spec: URL authentication, appended-run identity, monotonic scheduling, actual driver field/default semantics, checked numeric/input limits, reliable output, strict nested validation, failure counting, inherited shutdown behavior, file identity/permissions, and local/CI test wiring. All are addressed above; the revised contract has user approval.

Deliberately deferred: selecting or merging runs, reconstructing dropped source results, generalized filters, credential resolution or secret heuristics, exact packets/arrival order/ambient state, general driver lifecycle repairs, crash-durable file transactions, and unrelated release/roadmap work. These do not prevent the accepted scoped replay workflow; their boundaries must remain explicit in public docs.

## Non-Goals

- Approximate replay of legacy JSONL.
- Persisting or resolving credentials.
- SFTP replay.
- General filter expressions beyond `status=5xx`.
- Reapplying normal scheduler, rate-limit, backoff, resource, or metrics behavior.
- Reproducing packets, response bodies, random upload bytes, or browser binaries.
