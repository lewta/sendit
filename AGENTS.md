# Repository guidance

This file is the tool-neutral source of truth for contributors and coding agents.

## Workflow

- `main` is protected. Make every change on a new branch and merge it through a pull request.
- Keep each branch and pull request focused on one logical change.
- Use semantic-versioning-aware, imperative commit subjects with the repository prefixes (`feat:`, `fix:`, `docs:`, `ci:`, `build:`, `chore:`, or `refactor:`).
- Update `CHANGELOG.md` under `[Unreleased]` for every change. Treat it as a required release artifact.
- User-facing changes must update CLI help, root documentation, the Hugo site under `docs/`, examples, and tests in the same pull request.
- Do not tag releases from feature branches. Releases are cut by maintainers after merge.

## Toolchain and verification

- Go 1.26.5 or newer is required. `.mise.toml` is the canonical local toolchain declaration.
- Chrome or Chromium is only required for browser-driver work.
- Run `make verify` before opening or updating a pull request. It builds, lints, runs race-enabled tests, and runs integration tests.
- Use narrower Make targets during development: `make test`, `make test-race`, `make integration`, and `make lint`.
- New behavior requires unit tests for happy and meaningful error paths. Dispatch or driver changes require integration coverage; new parsers should add fuzz coverage where practical.

## Architecture

The engine runs a single-threaded dispatch loop and hands admitted work to worker goroutines:

```text
Scheduler.Wait -> resource.Admit -> pool.Acquire -> go dispatch()
                                                    -> backoff.Wait
                                                    -> ratelimit.Wait
                                                    -> driver.Execute
```

Backoff and per-domain rate-limit waits deliberately happen inside the worker goroutine so one slow domain cannot stall all dispatch.

Key packages:

- `cmd/sendit`: Cobra CLI commands and process wiring.
- `internal/config`: Viper-backed YAML loading, defaults, target files, and validation.
- `internal/engine`: dispatch loop, scheduler, worker pool, reload, and orchestration.
- `internal/task`: task/result types and O(1) weighted selection using the Vose alias method.
- `internal/driver`: HTTP, browser, DNS, WebSocket, gRPC, and SFTP implementations.
- `internal/ratelimit`: per-domain token buckets, jitter backoff, and shared error classification.
- `internal/resource`: CPU and memory admission gate.
- `internal/metrics`: Prometheus metrics and no-op implementation.
- `internal/output`: asynchronous JSONL/CSV result writing.
- `internal/pcap`: synthetic PCAP output without CGO or root privileges.
- `internal/tui`: Bubble Tea terminal state and presentation.

## Definition of done

For every pull request:

- Add an appropriate entry to `CHANGELOG.md` under `[Unreleased]`.
- Add or update tests proportional to the behavior changed.
- Update relevant root docs and `docs/content/docs/` pages.
- Update `ROADMAP.md` only when completing or changing a planned milestone.
- Run `make verify` and report the result in the pull request.

For new drivers or config fields, also review:

- `internal/config/schema.go` and `internal/config/config.go`
- `internal/engine/engine.go` driver registration
- `config/example.yaml`
- `README.md`
- `docs/content/docs/drivers.md`
- `docs/content/docs/configuration.md`
- `docs/content/docs/dependencies.md`
- `docs/content/docs/cli.md` when flags or commands change

## Graphify

Graphify can maintain a generated knowledge graph under `graphify-out/`; generated output is intentionally ignored by Git.

- When `graphify-out/graph.json` exists, use `graphify query`, `graphify path`, or `graphify explain` before broad source searches for architecture questions.
- After structural code changes, run `graphify update .` when a local graph exists.
- Dirty generated graph files are expected and do not belong in pull requests.
