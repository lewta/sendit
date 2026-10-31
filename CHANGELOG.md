## [Unreleased]

### Security
- Verify the dedicated release App in approved run `41109942873`: the temporary token exposed only `lewta/sendit`, the protected-main SHA matched, no repository write was attempted, and post-job cleanup reported token revocation; remove the one-time diagnostic afterward. This did not independently prove installation-wide App scope outside that token request, write permission, tag creation, tag-rule bypass or immutability, signing or notarization, or release publication.
- Replace the production publisher's generic repository write token with short-lived, environment-gated GitHub App tokens scoped to `lewta/sendit`; prepare candidate-derived completions in a separate credential-free job, retain only contents/workflow writes needed for exact tag creation and release publication, request a fresh token before publishing, and attempt token revocation after the job.
- Require explicit protected-main release dispatch with an immutable approved commit/tag tuple, separate trusted control checkout, exact tag rechecks, published-release recovery gates, and strict tap checksums before credentials; pin GoReleaser to v2.19.0.
- Validate advisory traces and OSV metadata against their expected Go contexts before retaining source artifacts; reject malformed or diagnostic substitutions and support offline checks with the macOS Python 3.9 baseline.
- Pin the existing source-mode govulncheck gate and OSV metadata complement; fail closed on incomplete evidence or reachable symbols, retain sanitized context-bound results for all release targets and integration tests, and enforce the existing advisory exception's package-closure condition and expiry.
- Upgrade `golang.org/x/net` to v0.62.0 to fix GO-2026-6860, a quadratic-time HTML parsing denial of service reachable through URL crawling.
- Move daemon PID records to a private per-user directory by default; create and lock them exclusively without following links, reject unsafe or legacy records before signaling, use Linux pidfds for stable targets, and remove only the file identity owned by the running process.
- Require operator-supplied Grafana administrator credentials and loopback-only publication in the optional observability profile; provide an explicit local anonymous Viewer override and pin Prometheus 3.15.1/Grafana 13.2.4 image digests.
- Enforce restricted configuration grants at secondary-file, SFTP key/host-file, authentication environment, and local result/PCAP output boundaries; recheck file identity before destructive effects and reject output symlink/hard-link aliases.
- Make URL crawling public-network-only by default, require explicit CIDR grants for special-use destinations, validate every resolved address before each dial, disable ambient proxies, and bound redirects, discovery state, robots/sitemap metadata, and aggregate response bytes.
- Bound gRPC reflection descriptor count, aggregate bytes, and dependency depth; deduplicate stable identities and replace quadratic retry registration with a cancellation-aware topological pass.

### Compatibility notes
- Pushing a version tag no longer publishes a release. Maintainers dispatch `release-production.yml` from protected `main` with a strict version tag and full commit SHA; AUR and tap recovery also require the exact published release tag and SHA. Deployment requires the separately managed protected environment, credential rotation/relocation, tag rules, and retirement of historical workflow identities.
- Stop daemons with the old binary before upgrading, then restart them. Hardened controls reject the old unbound numeric PID-file format. Trusted configs retain explicit custom `daemon.pid_file` and `--pid-file` paths on lock-capable local storage; restricted configs must use the operator-owned per-user default.
- Windows supports hardened PID status records but not graceful `stop` or `reload`; run in the foreground and restart. Darwin and FreeBSD verify the lock and record immediately before signaling, but retain the platform's small final PID exit/reuse race.

### Added
- Run the PID-file security regression suite natively on macOS and Windows in CI, in addition to the existing Linux verification gate.
- Add an immutable, operator-owned configuration policy with trusted compatibility mode, opt-in restricted mode, shared `start`/`validate` grant flags, request-only YAML narrowing, value-free policy summaries, and monotonic authority across accepted SIGHUP reloads. Reject authority-shaped YAML before secondary file reads.
- Include local observability policy, unsafe-fixture regression checks, and credential-free Compose rendering checks in `make verify` (requires Docker CLI with Compose, without starting containers).

### Fixed
- Fail PCAP export promptly on malformed or truncated JSONL instead of retrying the same undecodable input indefinitely.
- Enforce SFTP timeouts and shutdown cancellation during upload, download, and directory-list operations; evict interrupted cached connections so retries use a clean session.
- Reject HTTPS-to-HTTP redirects at every HTTP driver hop before sending the cleartext request, including traffic runs, probe, and replay.
- Preserve credential-free sendit-only Compose operation by moving optional dashboards into the explicitly selected `docker-compose.observability.yml`; check no-profile rendering without Grafana credentials alongside authenticated and local-demo policies.

### Fixed
- Require explicit policy grants for special-use network destinations and fail closed for reserved/unallocated IPv6 space. Support native Windows drive and UNC grants with metadata-based junction/symlink resolution and platform-aware containment while retaining strict POSIX path rules.

### Changed
- Bumped github.com/pkg/sftp from v1.13.11 to v1.13.12 for stricter malformed-packet handling and upstream fixes.
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.38.3 to v4.39.1 for upstream fixes.

## [1.8.3] - 2026-10-21

### Security
- Upgrade Go to 1.26.10 across local, docs-module, and digest-pinned Docker build toolchains to block trust in the intermediate certificate authorities affected by CVE-2026-111943.

### Added
- Pin golangci-lint alongside Go for mise setup and add `make test-browser` for explicit browser-driver and replay verification.

### Changed
- Fail requested browser tests when Chrome or Chromium is unavailable and align agent, contributor, and PR checklists with the canonical Make verification targets.

## [1.8.2] - 2026-10-18

### Fixed
- Report truncated, canceled, and timed-out HTTP response-body reads as failures while retaining partial byte counts, response status, and protocol metadata.
- Honor cancellation and timeouts during SSH handshake and SFTP startup so stalled connections cannot block engine shutdown; keep completed connections reusable.
- Remove URL userinfo from CSV result URLs, including malformed URLs, and omit associated error text, matching JSONL credential protection.

### Changed
- Bumped golang.org/x/net from v0.60.0 to v0.61.0 and aligned the golang.org/x module set for upstream fixes.
- Bumped github.com/prometheus/client_golang from v1.24.1 to v1.25.0 for upstream fixes and behavior changes.
- Bumped github.com/chromedp/chromedp from v0.20.1 to v0.21.0 for upstream fixes and module consolidation.
- Bumped the github/codeql-action group from v4.38.2 to v4.38.3 for upstream fixes.
- Bumped ossf/scorecard-action from v2.4.4 to v2.5.0, updating Scorecard to v5.6.0 and avoiding token-value logging during option validation.
- Bumped goreleaser/goreleaser-action from v7.2.3 to v7.3.0, enabling reuse of GoReleaser from the runner tool cache and including upstream dependency fixes.
- Bumped actions/create-github-app-token from v3.2.0 to v3.3.0 for updated permission inputs and upstream dependency fixes.

## [1.8.1] - 2026-10-11

### Changed
- Upgraded chromedp to v0.20.1 and migrated the browser driver to its typed action, CSS selector, and evaluation APIs.
- Bumped github.com/go-viper/mapstructure/v2 from 2.4.0 to 2.5.0 for upstream fixes.
- Bumped go.yaml.in/yaml/v3 from 3.0.4 to 3.0.5 for upstream fixes.
- Bumped actions/upload-artifact from 7.0.1 to 7.0.2 for upstream fixes.

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
