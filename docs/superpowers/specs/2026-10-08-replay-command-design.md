# Replay Command Design

## Goal

Add `sendit replay` so a future replay-capable JSONL result file can reproduce the original request start order, timing, and expanded request data. Replay supports rate scaling, source-status filtering, looping, cancellation, and optional result output.

Issue [#253](https://github.com/lewta/sendit/issues/253) is the canonical tracker.

## Compatibility Contract

Current JSONL records contain result telemetry but omit request methods, bodies, headers, driver settings, precise start times, and dispatch order. Reconstructing those requests with defaults would silently change behavior. Replay therefore accepts only the versioned records introduced by this change and rejects legacy records with a line-specific explanation.

The existing top-level result fields remain compatible for current consumers. Each new JSONL result adds a nested replay envelope:

```json
{
  "ts": "2026-10-08T21:16:34Z",
  "url": "https://example.com/users/alice",
  "type": "http",
  "status": 200,
  "duration_ms": 42.5,
  "bytes": 123,
  "replay": {
    "version": 1,
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
        "follow_redirects": true
      }
    }
  }
}
```

The request snapshot contains the expanded URL and only the active driver's behavior-affecting settings. Template definitions, candidate values, and variable-file paths are not stored. Driver-specific fields use the same snake-case names and value semantics as configuration YAML.

Replay output uses the same schema and can itself be replayed.

## Security Boundary

Result files do not persist configured credentials or custom headers. Request URLs and bodies may still contain sensitive application data, so output retains its existing mode `0600` protection and documentation warning. A result is marked `replayable: false`, includes a stable reason, and omits `request` when its task has any of these properties:

- An `auth` configuration.
- Custom HTTP or WebSocket headers.
- An SFTP target, because every useful SFTP request requires credentials or key material.

Replay fails before dispatch if any selected record is marked non-replayable. A source filter may exclude such a record. No flag bypasses this restriction; persisting secrets is outside this feature.

Unauthenticated HTTP, browser, DNS, WebSocket, and gRPC requests are replayable. Unknown target types fail validation rather than being skipped or run with defaults.

## Capture Semantics

The engine records metadata immediately before calling `driver.Execute`, after backoff and per-domain rate-limit waits. A short engine-local critical section assigns a monotonically increasing sequence number and an RFC3339Nano UTC start timestamp together, ensuring sequence and time cannot be observed in conflicting orders across workers.

The resulting `task.Result` carries this metadata to the output writer. The writer creates the safe replay envelope from the already-expanded `task.Task`; it does not invoke template expansion again. This preserves selected variable values, UUIDs, timestamps, and sequence substitutions exactly.

Sequence numbers are scoped to one engine run and may contain gaps when results are dropped by the existing non-blocking writer. Replay requires positive, unique sequence values, sorts records by sequence, and does not require them to be contiguous.

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

Input and output must identify different files. Replay output is JSONL regardless of filename extension. No normal sendit configuration file is loaded.

## Parsing And Scheduling

Replay reads and validates the complete input before sending traffic. The parser reports the JSONL line for malformed JSON or record errors and supports request bodies larger than `bufio.Scanner`'s default token limit. It validates:

- Replay envelope version, sequence, start timestamp, and replayability.
- Required request fields and the selected driver's request shape.
- Unique sequence values and nondecreasing timestamps in sequence order.
- Known target types.

After filtering, records are ordered by sequence. Their scheduled offset is:

```text
(record.started_at - first_selected.started_at) / rate
```

This preserves the elapsed gaps between selected requests rather than compacting filtered traffic. At each deadline, replay starts `driver.Execute` in a goroutine, allowing requests that originally overlapped to overlap again. Replay invokes drivers directly and does not run the normal scheduler, backoff, rate limiter, resource gate, or metrics server because those would alter recorded timing.

One cycle waits for every active request to finish. With `--loop`, replay then waits `--loop-delay` and schedules the next cycle relative to a new cycle start. The delay must not be negative. Cancellation interrupts scheduled waits, the loop delay, and active driver calls; replay waits for goroutines and closes output before returning.

## Results And Failures

Driver failures are ordinary replay results. They increment the final error count, are written when `--output` is set, and do not prevent later scheduled requests from starting.

Replay returns an error before dispatch for malformed input, legacy records, selected non-replayable records, invalid flags, unsupported types, or output setup failures. `--loop` with an empty input or a filter matching no records is rejected to prevent a busy loop. A non-looping replay with no selected records succeeds and reports zero requests.

Without `--output`, replay prints only a final count of requests and driver errors. Signal cancellation is a clean stop and prints the counts accumulated before cancellation.

## Code Organization

- `internal/task` carries request-start sequence and timestamp on results.
- `internal/engine` captures those fields immediately before driver execution.
- `internal/output` owns the versioned JSONL wire structs, safe request-snapshot conversion, JSONL decoding, and ordinary result encoding.
- `cmd/sendit` owns Cobra flags, signal handling, driver construction, scheduling, looping, and the final summary.

Existing driver implementations and the `driver.Driver` interface remain unchanged. The command uses the existing constructors for HTTP, browser, DNS, WebSocket, and gRPC. No new dependency is required.

## Testing

Tests cover:

- Replay envelope serialization for each supported driver.
- Rejection and redaction of auth, custom headers, and SFTP.
- Legacy, malformed, oversized, unsupported-version, duplicate-sequence, and unknown-type records.
- Rate, filter, input/output collision, empty-loop, and loop-delay validation.
- Source filtering without timing compaction.
- Concurrent start scheduling, loop boundaries, cancellation, and output draining.
- HTTP replay end to end, including method, expanded body, dispatch order, scaled start gaps, driver errors, and replayable output.
- Engine output of sequence, RFC3339Nano start time, and expanded request data.
- Fuzz coverage for JSONL record decoding.

The implementation updates root command help, `README.md`, `docs/content/docs/cli.md`, `docs/content/docs/configuration.md`, `docs/content/docs/drivers.md`, `docs/content/docs/security.md`, `CHANGELOG.md`, and `ROADMAP.md`. The roadmap moves released v1.7.0 out of Planned and marks Replay complete.

## Non-Goals

- Approximate replay of legacy JSONL.
- Persisting or resolving credentials.
- SFTP replay.
- General filter expressions beyond `status=5xx`.
- Reapplying normal scheduler, rate-limit, backoff, resource, or metrics behavior.
- Reproducing packets, response bodies, random upload bytes, or browser binaries.
