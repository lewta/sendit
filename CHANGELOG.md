## [Unreleased]

### Fixed
- Resolve package-tap updates from the exact release workflow commit, failing on missing or ambiguous releases instead of treating a manual dispatch's branch as its tag.

## [1.8.0] - 2026-10-09

This minor release adds request templating, request replay, and explicit HTTP/1.1–HTTP/2 selection, alongside Go and networking security updates.

### Compatibility notes
- Existing HTTP targets retain automatic negotiation when `http.http_version` is omitted or `0`. Forced HTTP/2 (`2`) requires HTTPS and ALPN `h2`, with no HTTP/1 fallback or plaintext h2c. HTTP/3 remains deferred to #320.
- Replay requires versioned request captures; older result-only JSONL files cannot reconstruct requests. New captures use envelope v2, while v1 captures remain readable with automatic HTTP policy. V1-only readers cannot read v2 captures.
- Replay excludes configured authentication, custom HTTP headers, URL userinfo, and SFTP. URLs and request bodies/messages may still contain sensitive application data.
- Building from source requires Go 1.26.9 or newer.

### Security
- Upgraded Go to 1.26.9 and `golang.org/x/net` to v0.60.0 to address the published HTTP, TLS, MIME, and HTML-template vulnerabilities reported by security CI (#318). Aligned local, docs-module, and digest-pinned Docker build toolchains with the fixed version.
- Omit URL userinfo from JSONL result URLs, including malformed URLs, and replace associated error text with a generic omission message.

### Added
- Added strict `http.http_version` configuration for automatic, HTTP/1.1, and HTTPS-only HTTP/2 policies, with raw type checks across aliases, singleton targets, merged mappings, and dotted defaults (#252).
- Enforced selected HTTP protocols across redirects without HTTP/1 fallback for forced HTTP/2, and exposed actual response protocol through debug logs and JSONL metadata.
- Added `sendit replay` for versioned JSONL captures with scaled concurrent timing, source-status filtering, looping, cancellation, and lossless replay output on success (#253).
- Added safe expanded request snapshots, run identity, monotonic-anchored dispatch timestamps, strict bounded preflight validation, and input/output alias protection. Legacy results and selected records containing configured auth, custom headers, URL userinfo, or SFTP cannot be replayed.
- Added per-request templating for target URLs, HTTP/gRPC bodies, and WebSocket messages with inline values, newline-delimited variable files, and UUID, timestamp, and sequence built-ins.

### Changed
- New replay captures use envelope v2 with explicit requested HTTP version; strict v1 captures remain readable with automatic protocol selection. Generator and dry-run output preserve/display the configured policy.
- Documented completed HTTP/1.1–HTTP/2 selection and tracked TLS-only HTTP/3 separately in #320.
- Included command-level integration coverage in `make verify` and added replay decoder fuzzing and encoding benchmarks to CI.
- Documented replay contracts and capture examples; moved released v1.7.0 and implemented Replay into completed roadmap milestones.

### Fixed
- Isolated protocol integration tests and the under-threshold admission test from host CPU saturation using test-only admission settings; production resource limits and explicit resource-gate tests retain their behavior.
- Preserve filesystem symlink/parent path semantics during replay output checks, and reject null messages, unpaired Unicode escapes, and malformed timestamps before replay traffic or output truncation.
- Wait for Apple notarization acceptance and validate final Darwin archive checksums, signatures, and notarization before the release workflow succeeds.

## [1.7.0] - 2026-10-08

### Changed
- Reconciled CLI syntax, bookmark support, dispatch and burst semantics, dependency versions, security release links, and roadmap history with the implementation.
- Validated cron schedules, schedule pacing values, per-domain rate limits, enabled Prometheus ports, and memory thresholds before startup and reload, rejecting fractional values for integer fields.
- Bumped google.golang.org/grpc from 1.83.2 to 1.84.0 for upstream fixes and behavior changes.
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.38.1 to v4.38.2 for upstream fixes.
- Bumped modernc.org/sqlite from 1.59.0 to 1.60.1 and aligned modernc.org/libc to 1.77.1 for upstream fixes.

### Fixed
- Made TUI exit cancel the engine and wait for engine shutdown and output flushing before `sendit start` returns.
- Applied query authentication to WebSocket handshakes while keeping resolved credentials out of result URLs.
- Corrected `sendit_errors_total` to emit `transient` and `permanent` classes and include classified status failures; dashboards matching the undocumented `error` class must update their selectors.

## [1.6.5] - 2026-09-12

### Changed
- Bumped codecov/codecov-action from 7.0.0 to 7.1.1 (semver-minor) for upstream fixes
- Bumped modernc.org/sqlite from 1.58.0 to 1.59.0 (semver-minor) for upstream bug fixes
- Bumped actions/deploy-pages from v5.0.0 to v5.0.1 to add capped backoff and jitter to GitHub Pages deployment polling.
- Bumped modernc.org/sqlite from 1.57.0 to 1.58.0 (semver-minor) for upstream bug fixes.
- Documented the GO-2026-5932 OSV exception because Sendit uses `golang.org/x/crypto` for SSH but does not import the affected OpenPGP packages.
- Bumped golang.org/x/crypto from 0.56.0 to 0.57.0 (semver-patch) for upstream bug fixes
- Bumped golang.org/x/net from 0.58.0 to 0.59.0 (semver-minor) for upstream bug fixes
- Bumped golang.org/x/time from 0.15.0 to 0.16.0 (semver-minor) for upstream bug fixes
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.37.9 to v4.38.0 for upstream fixes

## [1.6.4] - 2026-09-06

### Changed
- Bumped modernc.org/sqlite from 1.56.0 to 1.57.0 (semver-minor) for upstream bug fixes
- Bumped google.golang.org/grpc from 1.83.0 to 1.83.2 (semver-patch) for upstream bug fixes
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.37.7 to v4.37.9 for upstream fixes

### Fixed
- Upgraded golang.org/x/crypto from 0.55.0 to 0.56.0 to prevent malicious SSH peers from deadlocking SFTP connections (GO-2026-6354 and GO-2026-6355).

## [1.6.3] - 2026-08-22

### Changed
- Bumped github.com/miekg/dns from 1.1.72 to 1.1.73 (semver-patch) for upstream bug fixes
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.37.6 to v4.37.7 for upstream fixes

## [1.6.2] - 2026-08-19

### Changed
- Upgraded Go to 1.26.6 and aligned the golang.org/x module-tooling dependency chain around x/mod 0.40.0 to fix transparency-log verification vulnerabilities GO-2026-6179 and GO-2026-6180.
- Added a manual AUR-only recovery workflow for releases whose GitHub artifacts are already published and immutable.
- Prevented AUR recovery from attempting to modify an existing immutable GitHub release.
- Made AUR recovery use the current recovery configuration while building the selected release tag.
- Bumped google.golang.org/protobuf from 1.36.11 to 1.36.12 (semver-patch) for upstream bug fixes
- Bumped actions/attest-build-provenance from 4.1.1 to 4.2.2 (semver-minor) for upstream fixes

## [1.6.1] - 2026-08-08
### Changed
- Aligned local development, documentation, and the pinned Docker builder on Go 1.26.5; added canonical Make targets, tool-neutral contributor guidance, and reproducible CI tool versions.
- Split the Cobra CLI implementation into focused command files without changing command behavior.
- Bumped google.golang.org/grpc from 1.82.0 to 1.82.1 (semver-patch) for bug fixes and security updates
- Bumped modernc.org/sqlite from 1.54.0 to 1.55.0 (semver-minor) for upstream bug fixes
- Bumped google.golang.org/grpc from 1.82.1 to 1.83.0 (semver-minor) for upstream bug fixes
- Bumped modernc.org/sqlite from 1.55.0 to 1.56.0 (semver-minor) for upstream bug fixes
- Bumped github.com/cucumber/godog from 0.15.1 to 0.16.0 (semver-minor) for upstream bug fixes
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.37.3 to v4.37.6 for upstream fixes
- Bumped dorny/paths-filter from v4.0.2 to v4.0.3 (semver-patch) for upstream fixes
