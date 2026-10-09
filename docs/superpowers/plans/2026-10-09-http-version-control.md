# HTTP Version Control Implementation Plan

> Execute natively using executing-plans and TDD. Approved in conversation, including v2 captures and v1 read compatibility; the user explicitly authorized execution.

**Spec:** `docs/superpowers/specs/2026-10-09-http-version-control-design.md`

**Goal:** Implement config-selected H1 and HTTPS-only H2, actual protocol reporting, and replay policy fidelity for #252.

**Architecture:** Independent reusable Go transports selected per request; shared policy validation; versioned explicit replay snapshots. No new dependencies or H3 code.

## Global Constraints

Go 1.26.9+. Work from main in `feat/http-version-control`. Keep authentication, redirect limits, automatic negotiation and v1 capture semantics. No H1 request may be sent when H2 is forced. Config and replay preflight must reject malformed policy data before traffic/output truncation. Update changelog for each task.

## Review Focus

1. H2-only ALPN settings can still fall back when the server has no ALPN; require pre-request TLS validation (Task 2).
2. A redirect or templated scheme can bypass initial HTTPS checks; enforce at round-trip time (Tasks 1–2).
3. YAML weak decoding and aliases/merges can erase source types; inspect raw nodes (Task 1).
4. Requiring a new HTTP field in v1 would break existing captures; keep separate strict v1/v2 schemas (Task 3).
5. One shared mutable transport can cross-contaminate concurrent target policies; test concurrent mixed modes and reuse (Task 2).

### Task 1: Configuration

Files: `internal/config/schema.go`, `config.go`, new `http_version.go` and `http_version_test.go`.
Produces: `HTTPConfig.HTTPVersion int` and `ValidateHTTPVersion(int) error` for config, driver and replay consumers.

- [ ] Write load tests for valid integers 0/1/2, omitted fields, invalid raw types/overflow, defaults and alias/merge values, known plaintext rejection and deferred templated schemes. Run `go test ./internal/config -run HTTPVersion -count=1`; expect RED.
- [ ] Add the field and raw-node validation before Viper unmarshal; use scoped YAML mapping traversal including merges/aliases without changing unrelated coercion rules. Add enum checks for programmatic configs and unused defaults. Defer only scheme templates; runtime guard is mandatory.
- [ ] Run `go test -race ./internal/config`; expect PASS. Update changelog and commit `feat: validate HTTP version policy`.

### Task 2: Driver

Files: `internal/driver/http.go`, new package-local `http_version_test.go`.
Consumes: HTTPVersion and shared enum validation. Produces: strict policy transport behavior and `http_protocol` metadata.

- [ ] Write trusted local TLS tests for auto/H1/H2, H1-only and no-ALPN failure before handler/body delivery, plaintext input/redirect rejection. Run `go test ./internal/driver -run HTTPVersion -count=1`; expect RED.
- [ ] Create stable transport variants using Go Protocols; keep automatic transport unchanged. Add HTTPS round-tripper guard and h2-only VerifyConnection callback without disabling trust validation. Select transport on the per-request client copy. Report final response Proto in result metadata and credential-free debug events.
- [ ] Cover mixed concurrent modes, transport reuse, cert errors, redirect limiter, auth redaction, timeout/cancel and disabled-H2 environment. Run `go test -race ./internal/driver ./internal/engine`; expect PASS. Commit `feat: enforce HTTP protocol selection`.

### Task 3: Visibility And Replay

Files: `cmd/sendit/helpers.go`, `generate.go`, associated tests; `internal/output/replay.go`, `replay_decode.go`, codec tests/fuzz seeds; `cmd/sendit/replay*_test.go`.
Consumes: HTTPVersion, driver metadata. Produces: generator/dry-run policy, v2 writes with strict v1 reads.

- [ ] Write generator/dry-run tests and v2 snapshot tests first; preserve literal v1 fixtures as independent compatibility checks. Run focused tests, expect RED.
- [ ] Add configured-policy display and YAML emission. Write all new envelopes as v2; add required `http_version` to v2 HTTP fields and conversion; v1 forbids it and yields 0. Use shared enum validation and reject non-HTTPS forced H2 in executable snapshots. Keep top-level metadata extensible.
- [ ] Update test-only constructors/fixtures that intend new captures, retaining explicit v1 fixtures; fuzz both versions and malformed combinations. Run `go test -race ./cmd/sendit ./internal/output ./internal/pcap`; expect PASS. Commit `feat: preserve HTTP policy in replay captures`.

### Task 4: Integration And Documentation

Files: local HTTP integration tests; README/ROADMAP/CHANGELOG; config examples; Hugo configuration/drivers/CLI/dependencies pages and start/replay help.

- [ ] Add actual local TLS engine capture→replay checks for requested policy and observed protocol, trusted using test-local certificates without adding production insecure controls. Add late-invalid-record no-traffic/no-truncation coverage, v1→v2 recapture, and JSONL metadata checks.
- [ ] Track H3 separately with TLS-only scope and unresolved dependency/release strategy. Update #252 scope, mark only H1/H2 complete in roadmap, update v2 docs/example while retaining v1 read documentation. No release tag.
- [ ] Run `go test -tags integration -race ./...`, then full `make verify`, config/replay fuzz campaigns, govulncheck and Hugo. Do CPU-intensive fuzzing separately from full verification. Commit `docs: document HTTP version control`.
- [ ] Independent whole-branch review; reproduce/fix important findings with tests. Push focused branch and open PR `Closes #252`; monitor CI and report review status.
