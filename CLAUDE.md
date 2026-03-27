# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Environment

Go is **not in PATH** on this machine. Download it first:

```sh
curl -Lo /tmp/go124.tar.gz https://go.dev/dl/go1.24.0.linux-amd64.tar.gz
tar -C /tmp -xzf /tmp/go124.tar.gz
export PATH=/tmp/go/bin:$PATH
```

Then all standard `go` commands work normally.

## Common Commands

```sh
# Build
go build -o sendit ./cmd/sendit

# Run tests
go test ./...
go test -race ./...

# Run a single package's tests
go test ./internal/engine/...

# Run a single test
go test -run TestScheduler ./internal/engine/

# Vet
go vet ./...

# Validate config without starting
./sendit validate --config config/example.yaml

# Run with debug logging (foreground, no PID file)
./sendit start --config config/test.yaml --foreground --log-level debug
```

## Architecture

The engine runs a single-threaded dispatch loop that gates each task through a sequential pipeline before handing it to a worker goroutine:

```
Scheduler.Wait → resource.Admit → pool.Acquire → go dispatch()
                                                      ↳ backoff.Wait → ratelimit.Wait → driver.Execute
```

Backoff and per-domain rate-limit waits happen **inside** the goroutine so a slow domain cannot stall the dispatch loop and starve other domains.

### Key packages

| Package | Role |
|---|---|
| `internal/engine` | `Engine` owns the dispatch loop. `Scheduler` handles pacing (human/rate_limited/scheduled/burst). `Pool` is a semaphore with a sub-semaphore for browser workers. |
| `internal/config` | Viper-backed YAML loader. `schema.go` defines all struct types. Validates on load; `targets_file` is parsed here too. |
| `internal/task` | `Task`/`Result` types. `Selector` uses the Vose alias method for O(1) weighted random picks. |
| `internal/ratelimit` | `Registry` — per-domain `x/time/rate` token buckets. `BackoffRegistry` — decorrelated jitter backoff (AWS-style); shared by all domains, keyed by hostname. `ClassifyError`/`ClassifyStatusCode` unify error handling across all driver types. |
| `internal/driver` | `Driver` interface with five implementations: `http`, `browser` (chromedp), `dns` (miekg/dns), `websocket` (coder/websocket), `grpc` (google.golang.org/grpc + reflection). DNS RCODEs and gRPC status codes are mapped to HTTP-like status codes so the engine's error classifier works uniformly. |
| `internal/resource` | gopsutil CPU/RAM poller. `Admit()` blocks dispatch when either threshold is exceeded. |
| `internal/metrics` | Prometheus counters/histograms. `Noop()` returns a no-op implementation when metrics are disabled — avoids nil checks everywhere. |
| `internal/output` | JSONL/CSV result writer. A dedicated goroutine drains results non-blocking to the dispatch loop. |
| `internal/pcap` | Synthetic PCAP writer (LINKTYPE_USER0/147). No CGO or root required. |

### Pacing modes

- **`human`** — uniform random delay between `min_delay_ms` and `max_delay_ms`; `requests_per_minute` is ignored.
- **`rate_limited`** — `x/time/rate` token bucket at `requests_per_minute` plus ≤200 ms random jitter.
- **`scheduled`** — cron expressions open windows; within each window behaves like `rate_limited`. Outside a window, `scheduledWait` polls every 5 s. The `Scheduler.limiter` `atomic.Value` is **only populated** in `rate_limited` and `scheduled` modes — the `mode: human` path never touches it, so casting it is safe only after checking the mode.
- **`burst`** — fires requests as fast as worker slots allow with no inter-request delay. Requires `--duration` on `sendit start`. Optional `ramp_up_s` linearly increases speed from slow to full over N seconds.

### Browser driver

Each browser task spawns its own `chromedp.ExecAllocator`. This is intentional — it prevents memory accumulation across long runs at the cost of per-task Chrome startup time.

### Config loading

`config.Load` in `internal/config/config.go` uses Viper with `mapstructure` tags. All defaults are set via `viper.SetDefault` before unmarshalling. The `targets_file` is read and appended to `cfg.Targets` after YAML parse, with `target_defaults` applied to each file-loaded entry.

## Definition of Done

Every PR that ships a user-facing change **must** update all relevant surfaces before the PR is opened — not as a follow-up. Work through this checklist before pushing:

### CHANGELOG.md
- [ ] Entry added to `[Unreleased]` describing what changed

### ROADMAP.md
- [ ] If a planned milestone is completed: mark it ✓ in the Contents index and section header, move it out of **Planned** into the completed list

### Code surfaces (as applicable)
- [ ] `internal/config/schema.go` — new config structs/fields
- [ ] `internal/config/config.go` — validation rules, `loadTargetsFile` valid types, `setDefaults`
- [ ] `internal/engine/engine.go` — new driver registered in the `drivers` map

### Docs site (`docs/content/docs/`)
- [ ] `drivers.md` — new driver section with fields table, status code mapping, config example
- [ ] `configuration.md` — new config block in `target_defaults` table and YAML example
- [ ] `dependencies.md` — new direct deps, count updated, licence row updated
- [ ] `_index.md` — driver list in the Sections table
- [ ] `getting-started.md` — if there is a new top-level workflow or command
- [ ] `cli.md` — new flags

### Repository root
- [ ] `README.md` — all of the following that apply:
  - Protocol description (first paragraph)
  - `targets_file` type list and example
  - `target_defaults` YAML block and table
  - `targets` config examples
  - Architecture driver list
  - Architecture driver-specific section (status code mapping, etc.)
  - Verification table (add row; fix any examples that used the new type as an "invalid" example)
- [ ] `config/example.yaml` — new block in `target_defaults`; commented target example

### After the PR merges
- [ ] If releasing: move `[Unreleased]` to versioned section, add comparison link, push tag

## Roadmap

Planned features are tracked in `ROADMAP.md`.
