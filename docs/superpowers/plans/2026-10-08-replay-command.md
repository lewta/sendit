# Replay Command Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans for the user's selected native execution, or superpowers:subagent-driven-development if the user explicitly changes that choice. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement #253's future-record replay command, its capture format and tests, and the requested roadmap cleanup in one focused PR.

**Architecture:** Capture run identity and monotonic-anchored dispatch metadata around existing driver calls. Share a safe versioned JSONL codec between ordinary engine output and replay, but use reliable serialized writes for replay. The Cobra command preflights a bounded file, prepares a scaled schedule, executes existing drivers concurrently, and handles loops/cancellation without the normal engine scheduler.

**Tech Stack:** Go 1.26.6+, standard library (including `testing/synctest`), existing Cobra, DNS, WebSocket, gRPC, and Chrome driver dependencies. No new modules.

**Spec:** `docs/superpowers/specs/2026-10-08-replay-command-design.md` (user-approved revision; read it with this plan).

## Global Constraints

- Workspace: `.worktrees/replay-command`, branch `feat/replay-command`, base `7f6fce7` on `main`. Preserve unrelated root-checkout work.
- Go 1.26.6 or newer is required; `.mise.toml` is canonical. No new dependency is required.
- Version 1 accepts only future replay-capable JSONL. Legacy records, mixed runs, and selected non-replayable records fail before traffic.
- Fixed limits: **256 MiB per input file, 8 MiB per JSONL line excluding its line ending, and 10,000 records before filtering**.
- Replay flags: required `--input`; `--rate` default `1`; only `--filter status=5xx`; `--loop`; `--loop-delay` default `1s`; optional `--output`.
- Never persist configured auth/custom headers/SFTP in the request snapshot. Reject URL userinfo; preserve the spec's narrowly scoped JSONL redaction and ambient-state caveats.
- Schedule by sequence and scaled elapsed offsets; concurrency is best-effort, not guaranteed network arrival order. No rate limiter, backoff, resource gate, metrics server, or worker queue in replay.
- All flag, input, eligibility, and schedule validation precedes traffic and destructive output opening. Loops wait for workers, then the unscaled loop delay.
- Successful replay output loses no executed result; output errors are command failures. Ordinary engine output retains its lossy/non-blocking delivery contract.
- Keep existing telemetry field types and CSV schema; reserve `replay` against metadata collisions. Replay does not read config or re-expand templates.
- Use existing style, meaningful red/green tests, `CHANGELOG.md` under `[Unreleased]`, root/Hugo docs, CLI help, and runnable examples in this PR.
- No new release tag. Keep #253 open until the PR containing `Closes #253` merges. The plan is not execution approval.

## Review Focus

These five easily missed cases each have an owning test below:

1. JSONL is serialized out of dispatch order; sort by sequence and keep filtered elapsed gaps (Tasks 3–4).
2. A malformed or overflowing final record must cause zero traffic and leave an existing output byte-for-byte intact (Tasks 3–5).
3. An innocent-looking output path can alias input through a hard link or symlink; reject before truncation (Task 5).
4. Cancellation racing a write failure must still return the write failure, retain started results when writes succeed, and finish workers before close (Tasks 4–5).
5. A late completion from an old target configuration after reload must retain its original expanded snapshot and the same run ID (Tasks 1 and 6).

## File And Interface Map

| File | Responsibility |
| --- | --- |
| `internal/task/capture.go`, `capture_test.go` | Capture metadata, per-run clock/counter, concurrency and benchmarks |
| `internal/task/task.go` | Add metadata to `Result`; reuse package-local `randomUUID()` |
| `internal/engine/engine.go`, `engine_test.go`, `integration_test.go` | Stamp immediately before execution; preserve identity across reload |
| `internal/output/replay.go`, `replay_test.go` | Explicit v1 wire structs, snapshot/default normalization, shared request validation and conversion |
| `internal/output/replay_decode.go`, `replay_decode_test.go`, `replay_fuzz_test.go` | Strict record decoder, bounded file reader, run/order validation and fuzzing |
| `internal/output/writer.go`, `writer_test.go` | Shared JSONL encoding; envelope insertion, reserved metadata, compatibility |
| `cmd/sendit/replay_schedule.go`, `replay_schedule_test.go` | Filter/offset preparation, concurrent runner, summary, loops/cancellation |
| `cmd/sendit/replay.go`, `replay_test.go` | Cobra, preflight, safe file handling, signals, reliable output |
| `cmd/sendit/replay_integration_test.go` | Actual capture/replay and local-driver integration, signal smoke test |
| `cmd/sendit/root.go`, `internal/pcap/writer_test.go` | Command registration/help and existing-consumer regression checks |
| `Makefile`, `.github/workflows/ci.yml` | Include command integration locally and replay fuzzing in CI |
| Root/Hugo docs, `config/example.yaml`, `config/replay-example.jsonl` | Public contracts and runnable examples |

### Interfaces Shared Across Tasks

```go
// internal/task/capture.go
type Capture struct {
    RunID     string
    Sequence  uint64
    StartedAt time.Time
}
type CaptureClock struct {
    mu       sync.Mutex
    runID    string
    anchor   time.Time
    sequence uint64
    elapsed  func() time.Duration
}
func NewCaptureClock() *CaptureClock
func (c *CaptureClock) Next() (Capture, error)
// Add Capture Capture to task.Result. Its zero value means no metadata.

// internal/output/replay.go; JSON tags shown because they are the wire contract.
type ReplayEnvelope struct {
    Version    int            `json:"version"`
    RunID      string         `json:"run_id"`
    Sequence   uint64         `json:"sequence"`
    StartedAt  time.Time      `json:"started_at"`
    Replayable bool           `json:"replayable"`
    Reason     string         `json:"reason,omitempty"`
    Request    *ReplayRequest `json:"request,omitempty"`
}
type ReplayRequest struct {
    URL       string           `json:"url"`
    Type      string           `json:"type"`
    HTTP      *ReplayHTTP      `json:"http,omitempty"`
    Browser   *ReplayBrowser   `json:"browser,omitempty"`
    DNS       *ReplayDNS       `json:"dns,omitempty"`
    WebSocket *ReplayWebSocket `json:"websocket,omitempty"`
    GRPC      *ReplayGRPC      `json:"grpc,omitempty"`
}
type ReplayHTTP struct {
    Method                  string `json:"method"`
    Body                    string `json:"body"`
    TimeoutS                int    `json:"timeout_s"`
    AllowCrossHostRedirects bool   `json:"allow_cross_host_redirects"`
}
type ReplayBrowser struct {
    Scroll          bool   `json:"scroll"`
    WaitForSelector string `json:"wait_for_selector"`
    TimeoutS        int    `json:"timeout_s"`
}
type ReplayDNS struct {
    Resolver   string `json:"resolver"`
    RecordType string `json:"record_type"`
}
type ReplayWebSocket struct {
    DurationS      int      `json:"duration_s"`
    SendMessages   []string `json:"send_messages"`
    ExpectMessages int      `json:"expect_messages"`
}
type ReplayGRPC struct {
    Body     string `json:"body"`
    TimeoutS int    `json:"timeout_s"`
    TLS      bool   `json:"tls"`
    Insecure bool   `json:"insecure"`
}
func replayEnvelope(r task.Result) *ReplayEnvelope
func validateReplayRequest(r ReplayRequest) error
func (r ReplayRequest) Task() task.Task
func EncodeJSONL(enc *json.Encoder, r task.Result) error

// internal/output/replay_decode.go
const MaxReplayBytes = 256 << 20
const MaxReplayLineBytes = 8 << 20
const MaxReplayRecords = 10_000
type ReplayRecord struct {
    Line     int
    URL      string
    Type     string
    Status   int
    Envelope ReplayEnvelope
}
func DecodeReplayRecord(line []byte) (ReplayRecord, error)
func ReadReplay(r io.Reader) ([]ReplayRecord, error)

// cmd/sendit/replay_schedule.go
type replayOptions struct {
    Rate      float64
    Filter    string
    Loop      bool
    LoopDelay time.Duration
}
type replayItem struct {
    Record output.ReplayRecord
    Offset time.Duration
}
type replaySummary struct { Requests, Failures uint64 }
func prepareReplay(records []output.ReplayRecord, opts replayOptions) ([]replayItem, error)
func runReplay(ctx context.Context, items []replayItem, opts replayOptions,
    drivers map[string]driver.Driver, write func(task.Result) error) (replaySummary, error)

// cmd/sendit/replay.go
func replayCmd() *cobra.Command
func openReplayOutput(input *os.File, outputPath string) (*os.File, error)
func finishReplayOutput(w *bufio.Writer, f io.Closer, runErr error) error
```

Use concrete wire structs rather than serializing `config.TargetConfig`: that type includes credentials and has only `mapstructure` tags. Raw JSON field-presence validation is separate from typed decoding so absent/null/false are not conflated. Keep helper functions local unless a later task explicitly consumes them. `ReplayRequest.Task` only converts already-validated snapshots; it copies message slices and sets both task URL/type and config URL/type without invoking the selector.

---

### Task 1: Capture Identity And Monotonic Dispatch Metadata

**Files:** create `internal/task/capture.go`, `capture_test.go`; modify `internal/task/task.go`, `internal/engine/engine.go`, `engine_test.go`.

**Consumes:** existing `task.randomUUID`, `task.Result`, `Engine.dispatch` and `Reload`.
**Produces:** `Capture`, `CaptureClock`, `NewCaptureClock`, `Next`, and populated `Result.Capture`.

- [ ] **1. Write capture-clock tests first.** Include deterministic elapsed gaps and sequence exhaustion using package-local access, then concurrent calls (collect and sort captures before comparing). A representative check is:

```go
func TestCaptureClockUsesElapsedTime(t *testing.T) {
    c := NewCaptureClock()
    elapsed := time.Second
    c.elapsed = func() time.Duration { return elapsed }
    first, err := c.Next()
    if err != nil { t.Fatal(err) }
    elapsed += 250 * time.Millisecond
    second, err := c.Next()
    if err != nil { t.Fatal(err) }
    if first.RunID == "" || first.RunID != second.RunID ||
        first.Sequence != 1 || second.Sequence != 2 ||
        second.StartedAt.Sub(first.StartedAt) != 250*time.Millisecond {
        t.Fatalf("captures: %+v, %+v", first, second)
    }
}
```

- [ ] **2. Run red:** `go test ./internal/task -run TestCaptureClock -count=1`. Initially expect missing types; once present, assertions must detect wrong counter/time behavior. Add max-uint64 counter test proving it errors without wrapping and UTC anchor test independent of subsequent wall-clock reads.
- [ ] **3. Implement the small helper.** In `NewCaptureClock`, retain a monotonic `base := time.Now()`, set `anchor = base.UTC()`, set `elapsed = func() time.Duration { return time.Since(base) }`, and use existing `randomUUID()`. `Next` locks, checks exhaustion, increments sequence, and returns `anchor.Add(elapsed())`. No new clock library or exported injection API.
- [ ] **4. Add engine tests before wiring.** Extend existing dispatch/observer tests to require nonzero metadata after rate/backoff admission, metadata on immediate driver errors, and no capture for canceled pre-execution waits. Reload test holds an old request with channels, reloads targets, then releases it and asserts old task contents plus same run identity.
- [ ] **5. Wire a single `capture *task.CaptureClock` into `Engine.New`.** Do not reset it in `Reload`. Immediately before the existing `result := drv.Execute(ctx, t)`, call `Next`; on error log and skip that dispatch without calling the driver. Assign `result.Capture` before metrics/observer/output. Update engine literals in tests to initialize capture if they bypass `New`.
- [ ] **6. Run green:** `go test -race ./internal/task ./internal/engine`. Add `BenchmarkCaptureClockNext` (sequential and parallel) to quantify allocation/lock cost without imposing a performance threshold.
- [ ] **7. Inspect diff, update the pending replay changelog entry to mention capture groundwork, and commit intended files:** `feat: capture replay dispatch metadata`. Do not claim the command is implemented yet.

### Task 2: Safe Snapshots And Shared JSONL Encoding

**Files:** create `internal/output/replay.go`, `replay_test.go`; modify `internal/output/writer.go`, `writer_test.go` and the existing PCAP tests.

**Consumes:** Task 1's `Result.Capture`, existing config structs and `toJSONLRecord`.
**Produces:** wire structs, `replayEnvelope`, `validateReplayRequest`, `ReplayRequest.Task`, `EncodeJSONL`.

- [ ] **1. Write serialization/default tests for all five drivers.** The spec's field table is the complete allowlist. For each type assert nondefault values survive and config is not mutated; include nil WebSocket messages, file-loaded 30-second WebSocket duration versus raw-driver 10-second fallback, and effective gRPC TLS. Add a literal nested-JSON body containing `{{uuid}}` and prove no expansion occurs.

```go
func TestReplayEnvelopeRejectsPartialAuth(t *testing.T) {
    r := makeResult("https://example.com", "http", 200, time.Millisecond, 1, nil)
    c, err := task.NewCaptureClock().Next()
    if err != nil { t.Fatal(err) }
    r.Capture = c
    r.Task.Config.Auth.TokenEnv = "REPLAY_SECRET"
    got := replayEnvelope(r)
    if got == nil || got.Replayable || got.Request != nil || got.Reason != "auth_configured" {
        t.Fatalf("envelope = %+v", got)
    }
}
```

- [ ] **2. Run red:** `go test ./internal/output -run 'TestReplayEnvelope|TestReplaySnapshot' -count=1`.
- [ ] **3. Implement explicit snapshot construction and shared static validation.** Reject any nonzero `AuthConfig`, any HTTP headers, URL userinfo, SFTP/unknown type in the specified reason order. Normalize defaults on a copy. Require valid driver URL/method/DNS syntax and checked duration/count arithmetic; if invalid, emit `invalid_request` with no snapshot. Do not validate remote gRPC protobuf fields or parse HTTP bodies. Unsupported source types can be marked non-replayable, while the later reader still rejects unknown types.
- [ ] **4. Add credential/compatibility regressions before encoder integration.** Table-test every auth field/mode, active/inactive headers, username-only and password userinfo, SFTP keys/passwords, no capture metadata, and invalid shape. Serialize to a real buffer and assert credential markers never appear in the new envelope. Userinfo must disappear from top-level JSONL URL and replace any top-level error with a generic message, independent of which rejection reason wins. Verify unchanged CSV, integer `duration_ms`, PCAP conversion, and `Meta["replay"]` collision even for ad hoc metadata-free results.
- [ ] **5. Share encoding with ordinary output:**

```go
func EncodeJSONL(enc *json.Encoder, r task.Result) error {
    return enc.Encode(toJSONLRecord(r))
}
```

Insert the envelope into `toJSONLRecord`, reserve `replay` explicitly even when omitted, and sanitize only JSONL userinfo as specified. Keep `toRecord`/CSV semantics and `Writer.Send` unchanged. Change `runJSONL` to call the shared function and retain its existing delivery/error policy. Avoid adding flush/file ownership to this encoder.
- [ ] **6. Run green:** `go test -race ./internal/output ./internal/pcap ./internal/engine`. Add `BenchmarkReplayEncoding` with metadata-free and captured sub-benchmarks through `json.NewEncoder(io.Discard)`.
- [ ] **7. Update changelog coverage and commit:** `feat: encode safe replay request snapshots`.

### Task 3: Strict Bounded Replay Decoder

**Files:** create `internal/output/replay_decode.go`, `replay_decode_test.go`, `replay_fuzz_test.go`; add small fixture builders in test files only.

**Consumes:** Task 2 wire structs and `validateReplayRequest`.
**Produces:** exact constants, `ReplayRecord`, `DecodeReplayRecord`, `ReadReplay`.

- [ ] **1. Write a literal valid fixture independent of the encoder.** Every typed field must be present. Assert valid CRLF, no-final-newline, empty file, sequence above `2^53`, and nondefault driver blocks. Independently using a literal prevents encoder/decoder agreement from hiding a schema mistake.

```go
const validReplayLine = `{"url":"https://example.com","type":"http","status":503,"replay":{"version":1,"run_id":"405d88de-3fb2-42d8-bfb7-f365907ed78c","sequence":9007199254740993,"started_at":"2026-10-08T21:16:33.123456789Z","replayable":true,"request":{"url":"https://example.com","type":"http","http":{"method":"POST","body":"{}","timeout_s":15,"allow_cross_host_redirects":false}}}}`

func TestDecodeReplayRecordExactSequence(t *testing.T) {
    r, err := DecodeReplayRecord([]byte(validReplayLine))
    if err != nil { t.Fatal(err) }
    if r.Envelope.Sequence != uint64(9007199254740993) {
        t.Fatalf("sequence = %d", r.Envelope.Sequence)
    }
}
```

- [ ] **2. Run red:** `go test ./internal/output -run 'TestDecodeReplay|TestReadReplay' -count=1`.
- [ ] **3. Implement strict one-record decoding.** Check UTF-8 and `json.Valid` first. Walk object tokens with `json.Decoder.UseNumber` and a per-object key set to reject duplicates at any depth; JSON string payload contents are not nested executable objects. Validate required/non-null fields using `map[string]json.RawMessage` for each structural object, apply explicit key allowlists inside the envelope/request/driver block, then typed-decode. Keep optional top-level metadata forward-compatible. Check version, UUIDv4 syntax, exact positive sequence, timestamp, eligibility/reason exclusivity, type/URL agreement, and shared static validation. Errors name fields, not raw token values or secrets. Normalizing a malformed executable record is forbidden.
- [ ] **4. Add adversarial tables before completing the validator.** Legacy/missing/null/false distinction; unknown versions, fields and types; reason+request conflicts; SFTP marked executable; auth/header/vars injection; absent driver field; wrong driver block; URL mismatch, malformed host/port, invalid method, invalid resolver/record type/gRPC path; negative/overflowing timeouts; fractional/overflowing sequences; trailing objects, whitespace line, invalid UTF-8, escaped duplicate keys, duplicates in top-level metadata, and fake credentials in errors. Invalid expanded gRPC body JSON remains allowed as a driver-time failure.
- [ ] **5. Implement bounded physical-line reading.** Use `io.LimitedReader` with `MaxReplayBytes+1`, a buffered reader with capped accumulated line fragments, and per-line size enforcement before unmarshalling. Strip LF/CRLF only, not arbitrary whitespace. Stop after `MaxReplayRecords+1`; reject input-size overrun even if file stat was smaller. Preserve original `Line` on records. Read every line, then check one run ID, sort by sequence, reject duplicates and decreasing timestamps. Detect `time.Duration` saturation via `first.Add(last.Sub(first)).Equal(last)` before accepting the full capture span. No filtering happens here.
- [ ] **6. Exercise exact and one-over boundaries.** Tests use a streaming repeated-byte reader for 256 MiB limits rather than allocating multiple huge buffers; smaller internal reader-limit parameters may support boundary unit tests without exposing public knobs. Include >64 KiB valid bodies, 8 MiB line including versus excluding CRLF, 10,000-record count, mixed runs with disjoint sequences, shuffled serialization order, and valid gaps. A malformed final line fails the whole file.
- [ ] **7. Add `FuzzReplayRecord`.** Seed each protocol, non-replayable records, nested JSON bodies, duplicate keys, invalid UTF-8 and arithmetic extremes. Reject oversized fuzz inputs before parsing. For accepted records assert known version/type, positive exact sequence, required request/reason invariant and shared request validation; never dispatch or open files from fuzz input.
- [ ] **8. Run green:** `go test -race ./internal/output`; run `bash scripts/fuzz.sh -fuzz=FuzzReplayRecord -fuzztime=30s ./internal/output/`. Commit after updating changelog: `feat: validate replay JSONL input`.

### Task 4: Prepared Scheduling, Filtering, And Concurrent Runner

**Files:** create `cmd/sendit/replay_schedule.go`, `replay_schedule_test.go`.

**Consumes:** validated/sorted `[]output.ReplayRecord`, `ReplayRequest.Task`, Task 1 capture clock, existing `driver.Driver` and `ratelimit.ClassifyStatusCode`.
**Produces:** `replayOptions`, `replayItem`, `replaySummary`, `prepareReplay`, `runReplay`.

- [ ] **1. Write offset/filter table tests.** Construct records at 0s/1s/3s with statuses 200/503/500. After `status=5xx`, offsets are 0s/2s at rate 1 and 0s/1s at rate 2. Test rates 0.5, 1, 2, NaN, infinities, zero, negatives, smallest/largest finite rates; exact boundaries 499/500/599/600/0/429; selected versus excluded non-replayable records. Loop with no matches errors; ordinary no matches succeeds.

```go
func TestPrepareReplayPreservesFilteredGap(t *testing.T) {
    base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
    records := make([]output.ReplayRecord, 3)
    for i, seconds := range []int{0, 1, 3} {
        records[i].Line = i + 1
        records[i].Status = []int{200, 503, 500}[i]
        records[i].Envelope = output.ReplayEnvelope{
            Replayable: true, Sequence: uint64(i+1), StartedAt: base.Add(time.Duration(seconds)*time.Second),
            Request: &output.ReplayRequest{URL: "https://example.com", Type: "http",
                HTTP: &output.ReplayHTTP{Method: "GET", TimeoutS: 15}},
        }
    }
    items, err := prepareReplay(records, replayOptions{Rate: 2, Filter: "status=5xx"})
    if err != nil { t.Fatal(err) }
    if len(items) != 2 || items[0].Offset != 0 || items[1].Offset != time.Second {
        t.Fatalf("items = %+v", items)
    }
}
```

- [ ] **2. Run red:** `go test ./cmd/sendit -run 'TestPrepareReplay|TestRunReplay' -count=1`.
- [ ] **3. Implement checked preparation.** Validate options before selecting. Build all offsets before dispatch; non-replayable selected records produce line-specific reasons. Use `math/big.Rat` just for preflight offset division by the exact positive float64 rate, then integer quotient and `IsInt64` to floor nanoseconds and reject overflow without `float64(MaxInt64)` rounding errors. This standard-library calculation is outside the dispatch path. Record the original line in arithmetic diagnostics. Do not change the pre-filter run validation from Task 3.
- [ ] **4. Write runner behavioral tests with `testing/synctest`.** Use the real runner and minimal `driver.Driver` test doubles implemented as a function adapter in tests. A blocked first driver must not delay the second deadline; equal-deadline admission order is asserted via output capture sequences, not server arrival. Include overdue waits, no duration drift, fresh run metadata, loop barrier/default/zero delay, and counter continuation across loops. Writers collect real returned results for assertions.
- [ ] **5. Implement the runner around absolute deadlines.** Check that every selected type has a non-nil registered driver before scheduling any item. Create one `CaptureClock` per invocation and a child cancelable context. For each item wait with a context-aware timer until `cycleStart.Add(item.Offset)`, recheck cancellation, obtain capture metadata, and launch the goroutine. In the worker call the existing driver, attach capture, update counts, and serialize the optional `write` callback under a result mutex. Use `sync.Once` or the same mutex to record the first output failure and cancel scheduling. `sync.WaitGroup` covers the full worker including writing. No lock spans `Execute` and no output write occurs in the scheduling goroutine. Use explicit goroutine parameters/copies to preserve each record's task and metadata.

```go
func replayFailed(r task.Result) bool {
    class := ratelimit.ClassifyStatusCode(r.StatusCode)
    return r.Error != nil || class == ratelimit.ErrorClassTransient || class == ratelimit.ErrorClassPermanent
}
```

`replayFailed` is a local helper in `replay_schedule.go`; include table tests with nil Go error and statuses 500, 503, 101, 200, and gRPC-mapped failures. Count all completed calls, including canceled ones, using this exact predicate.
- [ ] **6. Add output/cancellation failure regressions.** Cover missing driver registration (zero calls, no panic), already canceled context, deadline, active calls, loop delay, cancellation+timer race, capture-counter exhaustion in the capture helper, and cancellation racing a writer error. More than 512 successful writes must all arrive. A slow writer blocks worker completion but not subsequent scheduling; an error cancels future scheduling, waits for workers, and returns the error even if the parent is canceled. Check no new dispatch after observed cancellation. Context stop alone returns nil error with accumulated counts.
- [ ] **7. Run green:** `go test -race ./cmd/sendit -run 'TestPrepareReplay|TestRunReplay|TestReplayFailed' -count=1`. Commit intended files/changelog: `feat: schedule concurrent request replay`.

### Task 5: Cobra Command And Safe Reliable Output

**Files:** create `cmd/sendit/replay.go`, `replay_test.go`; modify `cmd/sendit/root.go`.

**Consumes:** Task 3 `ReadReplay`, Task 4 preparation/runner, Task 2 `EncodeJSONL`.
**Produces:** registered `replayCmd`, `openReplayOutput`, `finishReplayOutput`, documented CLI flags and summary.

- [ ] **1. Write command tests first.** Invoke a fresh `replayCmd()` with captured output; assert registration and flag types/defaults, `cobra.NoArgs`, invalid flags/input/filter/rate/loop-delay, no config loading, and successful empty non-looping input. For registration, inspect `rootCmd.Commands()` without executing unrelated globals.
- [ ] **2. Write file preflight tests with real temp files.** Cover direct/relative/symlink/hard-link aliases; directory/nonregular inputs and outputs; preexisting permissive POSIX file rejection; mode 0600 creation; unwritable destinations; `.csv` destination still JSONL. Skip unsupported filesystem capabilities explicitly. A valid first request plus malformed or scaled-overflow final record must produce zero server calls and leave a preexisting output sentinel untouched.

```go
func TestReplayPreservesOutputOnBadInput(t *testing.T) {
    dir := t.TempDir()
    in, out := filepath.Join(dir, "in.jsonl"), filepath.Join(dir, "out.jsonl")
    if err := os.WriteFile(in, []byte("{invalid}\n"), 0o600); err != nil { t.Fatal(err) }
    if err := os.WriteFile(out, []byte("sentinel"), 0o600); err != nil { t.Fatal(err) }
    cmd := replayCmd()
    cmd.SetArgs([]string{"--input", in, "--output", out})
    if err := cmd.Execute(); err == nil { t.Fatal("expected input error") }
    got, err := os.ReadFile(out)
    if err != nil { t.Fatal(err) }
    if string(got) != "sentinel" { t.Fatalf("output changed: %q", got) }
}
```

- [ ] **3. Run red:** `go test ./cmd/sendit -run 'TestReplay|TestOpenReplayOutput|TestFinishReplayOutput' -count=1`.
- [ ] **4. Implement preflight order and file identity.** Validate flags, stat a regular input before opening (avoid blocking on FIFOs), read it through `ReadReplay`, prepare all offsets/eligibility, and only then inspect output. Reject known directories/nonregular paths before open; resolve existing symlinks, compare absolute/canonical names and stat identity. Open destination with `O_CREATE|O_WRONLY` and no `O_TRUNC`, stat both open descriptors and compare `os.SameFile`, enforce POSIX permissions, then truncate and seek to zero. Close all owned files on every error path. Do not add platform-specific atomic filesystem machinery for unsupported concurrent replacement.
- [ ] **5. Wire foreground execution and reliable writing.** Use `signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)`, not a background-only context. Initialize the five existing constructors once; no SFTP or normal config. Create `bufio.Writer` and `json.Encoder` only when output is requested. The runner serializes this callback:

```go
write := func(r task.Result) error {
    if err := output.EncodeJSONL(enc, r); err != nil { return err }
    return bw.Flush()
}
```

The callback must not silently log or drop errors. After `runReplay`, invoke `finishReplayOutput` that joins run/flush/close errors with `errors.Join`, calling close even if flush fails. Report partial-output path on failure; always wait for workers before finishing the writer. Print `Replayed N requests; M failures.` using `cmd.OutOrStdout()` and propagate a summary-write error. Source driver failures alone do not return a command error.
- [ ] **6. Test finalization independently.** Use `bufio.NewWriter` with a failing `io.Writer` and a close-recording `io.Closer` to prove both flush and close errors remain discoverable through `errors.Is`, including an earlier run failure. Keep these doubles in tests; no injectable production file factory. Test `EncodeJSONL` write errors with a failing writer and invalid encodable time value, then combine with Task 4's cancel/write-race tests.
- [ ] **7. Register with `rootCmd.AddCommand(replayCmd())` and add root `Long` usage.** Help explains exclusions, rate/filter/loop semantics, input limits, and input is future sendit JSONL, not arbitrary URLs.
- [ ] **8. Run green:** `go test -race ./cmd/sendit ./internal/output`. Commit files/changelog: `feat: expose replay command and reliable output`.

### Task 6: End-To-End Fidelity And Verification Wiring

**Files:** create `cmd/sendit/replay_integration_test.go`; modify `internal/engine/integration_test.go`, `Makefile`, `.github/workflows/ci.yml`; extend existing PCAP tests if Task 2's coverage needs an engine-produced fixture.

**Consumes:** complete capture, codec, runner and command from Tasks 1–5.
**Produces:** executable coverage for the spec's real-driver cases and local/CI wiring that actually runs it.

- [ ] **1. Add an HTTP capture-to-replay test before declaring feature complete.** Start a local `httptest.Server`, create real `engine.New` with a templated POST, output JSONL, and no credentials/headers. Wait for observed requests through channels, cancel and join engine. Decode capture, run `replayCmd` with its own output file, compare the multiset of expanded URL/body/methods, request count and sequence-based scheduling. Decode replay output and verify a new run ID, literal payloads and unique metadata. Serve blocked/fast requests to prove overlap; use broad timing tolerances only for the real network leg.

```go
// Minimal server behavior used by the integration test; observations are buffered
// to the test's known request count and drained before the next phase.
type observedReplayRequest struct { Method, Path, Body string }
seen := make(chan observedReplayRequest, 32)
server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    body, err := io.ReadAll(r.Body)
    if err != nil { http.Error(w, "read failed", 500); return }
    seen <- observedReplayRequest{r.Method, r.URL.RequestURI(), string(body)}
    w.WriteHeader(http.StatusOK)
}))
t.Cleanup(server.Close)
```

- [ ] **2. Test redirect policy explicitly.** Two local servers prove cross-host redirect rejection and opt-in forwarding, while a same-host redirect is followed. Use `127.0.0.1` versus `localhost` as existing redirect tests do if hostname comparison ignores ports. Do not let rate-limiter behavior obscure these replay tests.
- [ ] **3. Test actual other-driver dispatch.** Follow existing `internal/driver/driver_test.go` local server patterns: UDP DNS server with AAAA and SERVFAIL; WebSocket accept/echo recording messages with context-driven stop; gRPC health service plus reflection with empty/nonempty body, non-OK and reflection failure. Add local TLS gRPC coverage for `grpcs://`, forced TLS, and test-certificate `insecure` setting. Duplicate only small test-local server setup; do not export production helpers to share fixtures.
- [ ] **4. Test capture timing/reload boundary in engine integration.** Hold workers on controllable admission and execution channels; assert metadata after waits, error-result capture, reverse serialization order accepted, source config unchanged, and late old-config completion across reload keeping its original snapshot and continuous run/counter.
- [ ] **5. Add lifecycle smoke tests.** Browser snapshot/selection runs without Chrome; real browser local-page smoke requires an explicit environment opt-in and discoverable Chrome, otherwise `t.Skip` with the reason. On POSIX, build the CLI once into `t.TempDir`, start a looping local replay, wait until the server observes a request, send SIGINT, and require clean exit plus parsable drained output within a bounded shutdown timeout. Skip the signal test on Windows; context-cancellation unit tests still apply there.
- [ ] **6. Run focused integration:** `go test -tags integration -race -count=1 ./cmd/sendit ./internal/engine`. Fix failures against contracts, not by weakening timing assertions to mere execution counts.
- [ ] **7. Wire all-package integration and the explicit fuzz target:**

```make
integration:
	$(GO) test -tags integration -race -v ./...
```

```yaml
      - name: Fuzz replay record decoder
        run: bash scripts/fuzz.sh -fuzz=FuzzReplayRecord -fuzztime=30s ./internal/output/
```

Append the fuzz step to the existing `fuzz` job. Preserve its action pins and existing targets. Add `./internal/output/` to the existing benchmark command so the new encoding benchmark is collected alongside task/engine benchmarks.
- [ ] **8. Run `make integration` and the replay fuzz command.** Confirm command integration tests appear and note browser/signal skips precisely. Commit files/changelog: `test: cover replay fidelity and failure paths`.

### Task 7: Documentation, Roadmap Cleanup, And Final Verification

**Files:** modify `README.md`, `ROADMAP.md`, `CHANGELOG.md`, `config/example.yaml`, `docs/content/docs/cli.md`, `configuration.md`, `drivers.md`, `security.md`; create `config/replay-example.jsonl`. Reconcile approved spec/plan documentation statuses without implying unexecuted tests passed.

**Consumes:** completed and verified behavior of Tasks 1–6.
**Produces:** current docs/examples, completed roadmap milestones, PR evidence and issue links.

- [ ] **1. Update usage/command tables and output documentation.** Explain every flag/default, single-run limits, literal expanded requests, selected eligibility versus structural validation, all supported drivers, failure counting/exit statuses, best-effort concurrency, unscaled loop delay, inherited shutdown behavior, and lossless replay versus lossy ordinary capture. Show a complete v1 envelope with real field names and integer duration.
- [ ] **2. Add runnable capture instructions using existing output config, not an invented CLI flag:**

```yaml
pacing:
  mode: rate_limited
  requests_per_minute: 60
targets:
  - url: http://127.0.0.1:8080/users/{{seq}}
    type: http
    weight: 1
output:
  enabled: true
  file: results.jsonl
  format: jsonl
  append: false
```

```sh
# Save the YAML above as replay-capture.yaml and run a local service on port 8080.
sendit start --config replay-capture.yaml --foreground --duration 5s
sendit replay --input results.jsonl --rate 2 --output replay-results.jsonl
sendit replay --input results.jsonl --filter status=5xx
sendit replay --input results.jsonl --loop --loop-delay 1s
```

The focused YAML above avoids the non-replayable authenticated/header-bearing targets in the general example. Add output/replay guidance to `config/example.yaml` without silently changing its unrelated target semantics. `config/replay-example.jsonl` uses the Task 3 literal valid envelope with an inert example URL; tests decode it without sending external traffic.
- [ ] **3. Document sensitivity and compatibility limits.** Arbitrary URLs/bodies/messages can contain application secrets; no blanket credential-free guarantee. Auth/headers/SFTP/userinfo snapshots are excluded. Ambient proxy/trust-store/browser state is not captured. Explain permissions for new/reused replay outputs versus ordinary engine output, mixed appended-run rejection, source-record drops, partial output on failure and legacy/CSV non-replayability.
- [ ] **4. Clean up `ROADMAP.md` after behavioral tests pass.** Move v1.7.0 to Completed, add its completion mark and matching anchor, and move Replay to Completed with a #253 link and accurate implemented scope. Repair the stale claim that existing arbitrary JSONL can reproduce requests. Leave #252 and research issues open. Final changelog describes the implemented replay feature and compatibility changes, replacing the earlier planning-only line.
- [ ] **5. Check examples/help/docs and whitespace:**

```sh
go run ./cmd/sendit replay --help
git diff --check
go test ./internal/output ./cmd/sendit
```

Add a test that loads the checked-in JSONL example via `ReadReplay` and validates its executable snapshot, without driver execution. Check the Hugo CLI/configuration text uses `allow_cross_host_redirects`, real output config names and working roadmap anchors.
- [ ] **6. Run final required checks:**

```sh
GOLANGCI_LINT_CACHE=/tmp/opencode/golangci-cache-request-templating make verify GOLANGCI_LINT=/tmp/opencode/bin/golangci-lint
bash scripts/fuzz.sh -fuzz=FuzzReplayRecord -fuzztime=30s ./internal/output/
go test -run='^$' -bench='BenchmarkCaptureClock|BenchmarkReplay' -benchmem ./internal/task/ ./internal/output/
```

The existing temporary lint binary/cache were used successfully in this session; confirm their availability and matching Go support before execution. From the `docs/` working directory, use Hugo **0.147.1 extended** (the CI version) and run:

```sh
hugo mod verify
hugo --minify --baseURL "https://lewta.github.io/sendit/"
```

Report a tooling blocker rather than claiming a docs build passed without running it. Do not rerun unchanged full suites once green unless new changes/failures require it.
- [ ] **7. Self-check the spec matrix and request one whole-branch review.** Native execution uses one fresh reviewer on the full branch. Provide the approved spec, plan, base SHA and test evidence; fix meaningful findings with regressions and targeted/full checks as warranted. Verify all test rows have actual tests or an explicitly reported platform/browser skip.
- [ ] **8. Inspect status/diff/log, stage intended docs and commit:** `docs: document replay and complete roadmap milestones`. Use semantic imperative subjects for all task commits; follow repository identity without changing Git config. Only commit/push under the user's execution authorization.
- [ ] **9. Record progress in #253 and open the focused PR after authorization.** Include `Closes #253`, approved exclusions/limits, test commands/results, any skipped browser check, and roadmap cleanup. Inspect base diff, included commits and remote state before PR creation. Monitor checks and respond to review. Verify issue closure after an approved merge; do not mark it completed merely because a PR exists.

## Self-Review Coverage Ledger

| Approved spec section | Owning task(s) |
| --- | --- |
| Compatibility, wire fields, defaults, credential boundary | 2, 3, 7 |
| Capture run ID, monotonic timing, reload continuity | 1, 6 |
| Structural validation, mixed runs, sizes and integer precision | 3 |
| Rate/filter arithmetic, concurrency, order, loops | 4 |
| CLI, full preflight, file identity/permissions | 5 |
| Result counting, cancellation, reliable output errors | 4, 5, 6 |
| Real driver fidelity and exact expanded payload preservation | 2, 6 |
| Local/CI integration, race, fuzz and benchmarks | 1–6 |
| Docs, examples, roadmap and #253 hygiene | 7 |

Implementation remains gated on the user's review of this plan. Previously selected execution method: **native**, with one independent whole-branch review at the end.
