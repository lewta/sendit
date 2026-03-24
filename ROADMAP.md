# Roadmap

Features planned for future releases of sendit. Contributions are welcome — open an issue to discuss before starting work on a large item.

## Contents

**Completed**
- [v0.1.0 — Initial release ✓](#v010--initial-release-)
- [v0.2.0 — Result export ✓](#v020--result-export-)
- [v0.3.0 — Probe command ✓](#v030--probe-command-)
- [v0.4.0 — Config hot-reload ✓](#v040--config-hot-reload-)
- [v0.5.0 — Security CI ✓](#v050--security-ci-)
- [v0.6.0 — Documentation site ✓](#v060--documentation-site-)
- [v0.7.0 — Container support ✓](#v070--container-support-)
- [v0.8.0 — Observability improvements ✓](#v080--observability-improvements-)
- [v0.9.0 — Probe WebSocket ✓](#v090--probe-websocket-)
- [v0.10.0 — Distribution ✓](#v0100--distribution-)
- [v0.10.4 — Repository security hardening ✓](#v0104--repository-security-hardening-)
- [v0.10.5 — macOS code signing and notarization ✓](#v0105--macos-code-signing-and-notarization-)
- [v0.10.6 — Packet capture ✓](#v0106--packet-capture-)
- [v0.11.0 — Config generator ✓](#v0110--config-generator-)
- [v0.11.1 — Arch Linux package ✓](#v0111--arch-linux-package-)
- [v0.12.0 — OSSF Scorecard: Token-Permissions ✓](#v0120--ossf-scorecard-token-permissions-)
- [v0.12.1 — OSSF Scorecard: Pinned-Dependencies ✓](#v0121--ossf-scorecard-pinned-dependencies-)
- [v0.12.2 — OSSF Scorecard: Signed-Releases ✓](#v0122--ossf-scorecard-signed-releases-)
- [v0.12.3 — OSSF Scorecard: Branch-Protection ✓](#v0123--ossf-scorecard-branch-protection--dependency-updates-)
- [v0.12.4 — OSSF Scorecard: CII Best Practices ✓](#v0124--ossf-scorecard-cii-best-practices-)
- [v0.12.5 — OSSF Scorecard: Fuzzing ✓](#v0125--ossf-scorecard-fuzzing-)
- [v0.12.6 — OpenSSF Best Practices: gap audit ✓](#v0126--openssf-best-practices-gap-audit-)
- [v0.13.0 — Changelog and release notes ✓](#v0130--changelog-and-release-notes-)
- [v0.13.1 — Test coverage ✓](#v0131--test-coverage-)
- [v0.13.2 — Benchmark suite ✓](#v0132--benchmark-suite-)
- [v0.13.3 — Dependency audit ✓](#v0133--dependency-audit-)
- [v0.13.4 — Table of contents ✓](#v0134--table-of-contents-for-key-documents-)
- [v0.11.2 — AUR package ✓](#v0112--aur-package)
- [v0.14.0 — Safari bookmarks + browser history tests ✓](#v0140--safari-bookmarks--browser-history-tests-)
- [v0.14.1 — Burst pacing mode + `--duration` flag ✓](#v0141--burst-pacing-mode--duration-flag-)
- [v0.14.2 — AUR latest sync ✓](#v0142--aur-latest-sync-)
- [v0.15.0 — Test coverage improvement ✓](#v0150--test-coverage-improvement-)
- [v0.15.1 — Integration test suite expansion ✓](#v0151--integration-test-suite-expansion)
- [v0.15.2 — Codecov Test Analytics ✓](#v0152--codecov-test-analytics)
- [v0.15.3 — Docs audit + fuzz CI fix ✓](#v0153--docs-audit--fuzz-ci-fix)

**Planned**
- [v1.0.0 — TUI + stable API](#v100--tui--stable-api)

**Research**
- [Non-standard traffic driver](#research--non-standard-traffic-driver)
- [Aggressive / burst pacing mode ✓ (promoted to v0.14.1)](#research--aggressive--burst-pacing-mode)
- [Browser history and bookmarks harvesting ✓ (shipped in v0.11.0 / v0.14.0)](#research--browser-history-and-bookmarks-harvesting)
- [Live packet capture](#research--live-packet-capture-future)
- [Multi-browser support (post-v1.0.0)](#research--multi-browser-support-post-v100)

---

## v0.1.0 — Initial release ✓

- Four driver types: HTTP, headless browser (chromedp), DNS, WebSocket
- Three pacing modes: `human` (random delay), `rate_limited` (token bucket), `scheduled` (cron windows)
- Weighted target selection using the Vose alias method (O(1) picks)
- Prometheus metrics with per-domain rate limiting and decorrelated jitter backoff
- CPU and memory resource gates that pause dispatch when thresholds are exceeded
- `--dry-run` flag to preview effective config before sending traffic
- Integration test suite covering the full dispatch pipeline

---

## v0.2.0 — Result export ✓

Write request results to a file for offline analysis, complementing the Prometheus scrape endpoint.

- New `output` config section: `file`, `format` (`jsonl` | `csv`), `append` (bool)
- A dedicated writer goroutine consumes results non-blocking to the dispatch loop
- Truncates or appends on startup based on the `append` flag

---

## v0.3.0 — Probe command ✓

A `sendit probe <target>` subcommand for interactively testing a single HTTP or DNS endpoint
in a loop — no config file needed. Works like `ping` for web targets.

- Auto-detects type from URL scheme (`https://` → http, bare hostname → dns)
- `--type`, `--interval`, `--timeout`, `--resolver`, `--record-type` flags
- Prints one line per request with status, latency, and bytes (HTTP) or rcode (DNS)
- Prints a summary (sent, ok, errors, min/avg/max latency) on Ctrl-C

```
$ sendit probe https://example.com

Probing https://example.com (http) — Ctrl-C to stop

  200   142ms  1.2 KB
  200    38ms  1.2 KB
^C

--- https://example.com ---
2 sent, 2 ok, 0 error(s)
min/avg/max latency: 38ms / 90ms / 142ms
```

---

## v0.4.0 — Config hot-reload ✓

Reload configuration on `SIGHUP` without restarting the process or dropping in-flight requests.

- Targets and weights swapped atomically via the existing `task.Selector`
- Pacing, rate-limit, and backoff registries updated in-place where possible
- Logs a diff of what changed (added/removed targets, updated limits)

---

## v0.5.0 — Security CI ✓

Automated security scanning integrated into every PR and a weekly scheduled run.

- **`govulncheck`** — scans all dependencies against the Go vulnerability database (vuln.go.dev); fails the build on any known CVE
- **`gosec`** — SAST linter added to golangci-lint; checks for insecure patterns in source code (weak crypto, command injection, file permission issues, etc.)
- **CodeQL** — GitHub's semantic analysis engine; results surface in the repository Security tab
- **Dependabot** — weekly automated PRs for stale Go module and GitHub Actions dependencies

---

## Pending patches ✓

Small improvements tracked as GitHub issues that will ship as patch releases before the next minor version.

- **WebSocket driver migration** ✓ — migrate `internal/driver/websocket.go` from the deprecated `nhooyr.io/websocket` to its maintained fork `github.com/coder/websocket` ([#23](https://github.com/lewta/sendit/issues/23))
- **`sendit reload` command** ✓ — send `SIGHUP` to a running instance via its PID file, making hot-reload a first-class CLI operation consistent with `sendit stop` ([#26](https://github.com/lewta/sendit/issues/26))

---

## v0.6.0 — Documentation site ✓

Public reference documentation hosted on GitHub Pages.

- Built with [Hugo](https://gohugo.io), source under `docs/`
- Pages: getting started, configuration reference, pacing modes, drivers, metrics, CLI reference
- Deployed automatically on every push to `main` via GitHub Actions

---

## v0.7.0 — Container support ✓

Package sendit as a Docker image for portability and scheduled runs in CI or on a server.

- Multi-stage `Dockerfile`: `golang:1.24-alpine` builder → `alpine` runtime (files under `docker/`)
- `docker-compose.yml` with optional Prometheus + Grafana sidecars via `--profile observability`
- Config mounted as a volume so the image stays generic
- `--foreground` set by default in the entrypoint (PID files are not useful inside a container)
- `/healthz` endpoint on the metrics port for container liveness checks

---

## v0.8.0 — Observability improvements ✓

Better visibility into per-target behaviour from Prometheus metrics.

- Add a `domain` label to `sendit_requests_total`, `sendit_errors_total`, and `sendit_request_duration_seconds` so individual targets can be distinguished in dashboards
- Note: this is a breaking change to existing metric label sets — update any dashboards or alerts accordingly

---

## v0.9.0 — Probe WebSocket ✓

Complete driver coverage in the probe tool.

- Extend `sendit probe` to support `wss://` targets; connects, optionally sends a message, waits for a reply, and prints latency per round-trip

---

## v0.10.0 — Distribution ✓

Make sendit easy to install without building from source across all supported platforms.

- **Homebrew tap** — `brew install lewta/tap/sendit`; new `lewta/homebrew-tap` repo auto-updated by GoReleaser on each release via the `brews:` config section; formula bundles shell completions for bash, zsh, and fish
- **Linux packages** — `.deb` and `.rpm` artifacts added to each release via GoReleaser `nfpms:`; covers apt users (Debian, Ubuntu) and yum/dnf users (Fedora, RHEL, CentOS); targets linux/amd64 and linux/arm64; bundles shell completions and a man page
- **Scoop bucket** — `scoop install lewta/sendit`; new `lewta/scoop-bucket` repo auto-updated by GoReleaser via the `scoops:` config section; provides Windows package manager parity with Homebrew
- **Shell completion install docs** — update `docs/content/docs/cli.md` with per-method install instructions: Homebrew (automatic via formula), `.deb`/`.rpm` (bundled), and binary download (manual `source` commands for bash/zsh/fish)

---

## v0.10.4 — Repository security hardening ✓

Establish a clear vulnerability disclosure process and harden CI/CD supply-chain security.

- **`SECURITY.md`** — security policy file defining supported versions, the reporting process (GitHub private advisory), response timelines (48 h acknowledgement, 7-day resolution target), and coordinated disclosure policy
- **Private vulnerability reporting** — enable GitHub's private vulnerability reporting so reporters can submit CVEs without opening a public issue
- **Dependabot security updates** — enable automated security-fix PRs (distinct from the version-update PRs already in place)
- **Branch ruleset hardening** — set `dismiss_stale_reviews_on_push: true` so post-approval pushes require re-review
- **OSSF Scorecard** — add `scorecard.yml` GitHub Actions workflow; runs weekly and on every push to `main`; publishes results to the GitHub Security tab as SARIF
- **Docs — Security page** — add `docs/content/docs/security.md` summarising the policy, supported versions, and how to report
- **Docs — `security.txt`** — add `docs/static/.well-known/security.txt` (RFC 9116) so automated scanners and researchers can discover the disclosure contact and policy URL

---

## v0.10.5 — macOS code signing and notarization ✓

Sign and notarize the darwin binaries so macOS Gatekeeper accepts them without any user intervention. Fixes [#95](https://github.com/lewta/sendit/issues/95).

- **GoReleaser `notarize` block** — use `anchore/quill` (cross-platform; runs on the existing `ubuntu-latest` runner, no macOS runner needed) to sign darwin/amd64 and darwin/arm64 binaries with a Developer ID Application certificate and submit them to Apple's notarization service via the App Store Connect API before archiving
- **GitHub secrets** — `MACOS_SIGN_P12` (base64 `.p12`), `MACOS_SIGN_PASSWORD`, `NOTARIZE_KEY` (base64 `.p8`), `NOTARIZE_KEY_ID`, `NOTARIZE_ISSUER_ID`; all sourced from the lewta Apple Developer account
- **Remove caveats workaround** — once notarization is in place, remove the temporary `caveats` stanza added to the Homebrew cask in v0.10.3

---

## v0.11.0 — Config generator ✓

A `sendit generate` subcommand that produces a ready-to-use `config.yaml` from a targets file or a seed URL, reducing the time-to-first-traffic for new users.

- **From a targets file** — parse an existing `targets_file` (url + type + optional weight, one per line) and emit a full `config.yaml` with sensible defaults for pacing, limits, backoff, and per-target driver settings
- **From a seed URL (`--crawl`)** — for HTTP targets, optionally crawl the seed domain up to a configurable depth/page limit, discover in-domain links, and add each unique path as a weighted `http` target; respects `robots.txt` by default (`--ignore-robots` to override)
- **From browser history (`--from-history`)** — read the local browser history database and emit all visited HTTP/HTTPS URLs as weighted `http` targets; weight derived from visit count so frequently visited pages appear more often in traffic (see Research item below)
- **From browser bookmarks (`--from-bookmarks`)** — read the local browser bookmarks file and emit bookmarked HTTP/HTTPS URLs as equally-weighted `http` targets
- **Output** — writes to stdout by default; `--output <file>` writes to a file, prompting before overwriting
- **Flags**:
  - `--targets-file <path>` — generate from an existing targets file
  - `--url <url>` — seed URL for crawl-based generation (implies `--crawl`)
  - `--crawl` — enable in-domain page discovery for HTTP targets
  - `--depth <n>` — maximum crawl depth (default: `2`)
  - `--max-pages <n>` — maximum number of pages to discover (default: `50`)
  - `--ignore-robots` — skip `robots.txt` enforcement during crawl
  - `--from-history <browser>` — harvest visited URLs from local browser history (`chrome` | `firefox` | `safari`)
  - `--from-bookmarks <browser>` — harvest bookmarked URLs from local browser bookmarks (`chrome` | `firefox` | `safari`)
  - `--history-limit <n>` — cap the number of URLs imported from history (default: `100`, ordered by visit count descending)
  - `--output <file>` — write config to a file instead of stdout

Example:

```sh
# From a targets file
sendit generate --targets-file config/targets.txt > config/generated.yaml

# From a seed URL with crawling
sendit generate --url https://example.com --crawl --depth 2 --output config/generated.yaml

# From Chrome history (top 50 most-visited pages)
sendit generate --from-history chrome --history-limit 50 --output config/generated.yaml

# From Firefox bookmarks
sendit generate --from-bookmarks firefox --output config/generated.yaml
```

**Documentation deliverables** (required as part of the same release):

- **CLI help** — `Use`, `Short`, and `Long` descriptions on the `generate` command and all flags, consistent with the style of `probe` and `pinch`
- **`README.md`** — add `sendit generate` to the CLI commands usage block and command table; add a Generate section with both usage modes and example output, alongside the existing Probe and Pinch sections
- **`docs/content/docs/cli.md`** — add `generate` to the commands block and table; add a `generate` flags section with both modes, flag reference, and annotated example output
- **`docs/content/docs/getting-started.md`** — add a "Generate a config from a URL" subsection under the quick-start flow so new users discover the crawl mode as the fastest path to a working config

---

## v0.11.1 — Arch Linux package ✓

Make `sendit` installable as a native Arch Linux package so Arch and Arch-based users (e.g. Omarchy) can install it from the releases page without building from source.

- **GoReleaser `nfpms: archlinux`** — add `archlinux` to the `nfpms: formats` list; GoReleaser produces a `.pkg.tar.zst` artifact on every release
- **Shell completions** — zsh completion installed to `/usr/share/zsh/site-functions/_sendit` (Arch convention; deb/rpm continue to use `/usr/share/zsh/vendor-completions/`)
- **Docs** — update `README.md` and `docs/content/docs/getting-started.md` with the `pacman -U` install command for Arch / Omarchy users

```sh
# Arch Linux / Omarchy (and other Arch-based distros)
sudo pacman -U sendit_<version>_linux_amd64.pkg.tar.zst
```

---

## v0.11.2 — AUR package ✓

Make `sendit` installable via Arch User Repository helpers so Arch Linux and Arch-based users (e.g. Omarchy) can install with a single command:

```sh
yay -S sendit    # or: paru -S sendit
```

**Prerequisites (manual setup before implementation):**

- Register `sendit` as an AUR package at [aur.archlinux.org](https://aur.archlinux.org)
- Generate a dedicated SSH key pair: `ssh-keygen -t ed25519 -C "sendit-aur"`
- Add the **public** key to your AUR account profile
- Add the **private** key as the `AUR_SSH_KEY` GitHub Actions secret

**Implementation:**

- Add `aurs:` block to `.goreleaser.yaml` pointing at `ssh://aur@aur.archlinux.org/sendit.git`; GoReleaser generates and pushes a `PKGBUILD` on every release that downloads the source tarball and verifies its SHA-256 against `checksums.txt` — no binary distribution needed
- Add `AUR_SSH_KEY: "placeholder"` to the env blocks in `goreleaser-check` and `goreleaser-snapshot` CI jobs so template evaluation passes on PRs without the real secret
- Update `README.md` and `docs/content/docs/getting-started.md` to document `yay`/`paru` install alongside the `.pkg.tar.zst` download option added in v0.11.1

---

## v0.14.1 — Burst pacing mode + `--duration` flag ✓

Add an explicit opt-in `burst` pacing mode for internal infrastructure testing and controlled load experiments. sendit stays polite by default — burst requires being asked nicely.

**Design principles:**

- `mode: burst` is set in the config file, not a runtime flag — it is a deliberate configuration choice, not something that can be accidentally triggered
- `--duration` is **required** when `mode: burst`; the engine refuses to start a burst run without a time bound; this is the primary safety gate that prevents open-ended hammering
- The resource gate (`cpu_threshold_pct`, `memory_threshold_mb`) still applies — the local machine always protects itself
- Backoff still engages on repeated errors — burst mode does not disable error handling
- Clearly documented as intended for internal or owned infrastructure; pointing burst at external targets you do not control is out of scope and discouraged

**Implementation:**

- **`mode: burst`** in the `pacing:` config block — fires requests as fast as worker slots allow with no inter-request delay; `min_delay_ms` / `max_delay_ms` / `requests_per_minute` are ignored
- **`ramp_up_s`** — optional field in the `pacing:` block; linearly increases active workers from 1 to `max_workers` over the specified number of seconds; applies to `burst` mode only; prevents a cold-start spike against the target
- **`--duration <duration>`** on `sendit start` — auto-stops the engine after the specified wall-clock time (e.g. `--duration 5m`, `--duration 30s`); **required when `mode: burst`**, optional otherwise; on expiry the engine performs a graceful shutdown (drains in-flight requests) identical to SIGTERM
- **Config validation** — `config.Load` returns an error if `mode: burst` and `--duration` was not passed; enforced at startup, not silently defaulted
- **README key properties** — update "Never bursts aggressively" to reflect the opt-in design
- **Docs** — burst mode documented in `docs/content/docs/pacing.md` with an explicit "internal use" callout; `--duration` flag documented in `docs/content/docs/cli.md`

---

## v0.14.2 — AUR latest sync ✓

Distribution-only patch. The initial AUR publication in v0.11.2 was out-of-sequence
(version number lower than the current latest), leaving the AUR pointing at old
binaries. This release updates the AUR `PKGBUILD` to the current latest so
`yay -S sendit` installs up-to-date code.

---

## v0.12.0 — OSSF Scorecard: Token-Permissions ✓

Harden GitHub Actions workflow token permissions to follow the principle of least privilege. Fixes the `Token-Permissions` check (currently 0/10).

- **`ci.yml`** — added `permissions: read-all` at the top level; the workflow has no write needs
- **`release.yml`** — replaced top-level `contents: write` with `permissions: read-all` and scoped `contents: write` to the `release` job only
- **`docs.yml`** — moved `pages: write` and `id-token: write` from the top level to the `deploy` job only; `build` job only needs `contents: read`

---

## v0.12.1 — OSSF Scorecard: Pinned-Dependencies ✓

Pin all GitHub Actions dependencies to their full commit SHA and all Docker base images to their digest. Fixes the `Pinned-Dependencies` check (currently 0/10).

- **GitHub Actions** — replaced all `uses: action/name@vX.Y.Z` references across all five workflow files with `uses: action/name@<sha>  # vX.Y.Z`; also aligned `docs.yml` from checkout@v4/setup-go@v5 to v6
- **Docker images** — pinned `golang:1.24-alpine` and `alpine:3.21` in `docker/Dockerfile` to their `@sha256:…` digests
- **Dependabot** — already configured for `github-actions` weekly updates; will keep pinned SHAs current automatically

---

## v0.12.2 — OSSF Scorecard: Signed-Releases ✓

Attach SLSA provenance attestations to every release artifact so consumers can verify the build was produced by this repository's CI without tampering. Fixes the `Signed-Releases` check (currently 0/10).

- **`actions/attest-build-provenance`** — added as the final step of the `release` job; generates GitHub-native SLSA provenance for all archives (`.tar.gz`, `.zip`), packages (`.deb`, `.rpm`, `.pkg.tar.zst`), and `checksums.txt`; attestations are stored in GitHub's attestation store and verifiable with `gh attestation verify`
- **`release.yml` permissions** — added `id-token: write` and `attestations: write` to the `release` job
- **Docs** — added "Build provenance" section to `docs/content/docs/security.md` with verification instructions

---

## v0.12.3 — OSSF Scorecard: Branch-Protection + dependency updates ✓

Raise the `Branch-Protection` check by adding required status checks to the `baseline-branch-rule` ruleset, and bump all stale GitHub Actions and Go module dependencies.

- **Required status checks** — added `lint` and `test` CI jobs as required checks so PRs cannot be merged until both pass
- **Admin bypass preserved** — the `RepositoryRole/Admin` bypass actor is intentionally retained while the project is single-maintainer; can be removed when a second maintainer is added
- **Dependency updates** — bumped `golang.org/x/net` to 0.52.0; updated `actions/upload-artifact` to v7, `actions/create-github-app-token` to v3, `ossf/scorecard-action` to 2.4.3, `github/codeql-action` to v4, and `actions/attest-build-provenance` to v4 (all SHA-pinned)

---

## v0.12.5 — OSSF Scorecard: Fuzzing ✓

Integrate fuzz testing to catch parser and input-handling bugs that unit tests miss. Fixes the `Fuzzing` check (currently 0/10).

The Scorecard check accepts native Go fuzz functions (`func FuzzXxx(f *testing.F)`), which require no external service — just `go test -fuzz`.

- **`internal/config`** — `FuzzLoad`: feeds arbitrary YAML bytes through the config loader via a temp file; catches panics and unexpected parse errors on malformed input
- **`internal/task`** — `FuzzSelector`: fuzzes the Vose alias selector with arbitrary-length weight slices; validates O(1) pick invariants under edge-case inputs (empty slice, zero weights, single element, skewed distributions)
- **`internal/ratelimit`** — `FuzzClassifyError` + `FuzzClassifyStatusCode`: fuzz both classifiers across all possible inputs; validates every result maps to a defined `ErrorClass`
- **`internal/pcap`** — `FuzzWriteRecord`: fuzzes the PCAP record writer with arbitrary result fields (URL, type, status, duration, bytes) including oversized payloads that exercise the `snapLen` truncation path
- **`fuzz` CI job** — runs each target with `-fuzztime=30s` on every PR

## v0.12.4 — OSSF Scorecard: CII Best Practices ✓

Register the project on the OpenSSF Best Practices platform and link the badge. Fixes the `CII-Best-Practices` check (currently 0/10).

- **Register** — project registered at [bestpractices.coreinfrastructure.org/projects/12213](https://bestpractices.coreinfrastructure.org/projects/12213)
- **Badge** — OpenSSF Best Practices badge added to `README.md` alongside the existing OSSF Scorecard badge

---

## v0.12.6 — OpenSSF Best Practices: gap audit ✓

Worked through all [passing-level criteria](https://www.bestpractices.dev/en/criteria/0) on the Best Practices platform to bring the badge from its initial state to **passing** (99%).

- **Basics** — all 13 criteria answered Met; evidence URLs linked for description, licence, CONTRIBUTING.md, and docs site
- **Change control** — all 9 criteria answered Met; release notes and CVE policy evidenced via CHANGELOG.md and GitHub Releases
- **Reporting** — all 8 criteria answered Met; SECURITY.md, private advisory, and 14-day response policy evidenced
- **Quality** — all 13 criteria answered Met; `test_most` evidenced via Codecov (v0.13.1); test policy evidenced via CONTRIBUTING.md
- **Security** — all criteria answered Met or N/A; crypto delegated to stdlib TLS, SLSA provenance evidences delivery integrity, govulncheck + Dependabot evidence vulnerability management
- **Analysis** — all 8 criteria answered Met or N/A; golangci-lint/CodeQL for static analysis, fuzz tests + race detector for dynamic analysis

---

## v0.13.0 — Changelog and release notes ✓

Establish a proper changelog and add authored release notes to every GitHub release — past and future.

- **`CHANGELOG.md`** — human-authored changelog in [Keep a Changelog](https://keepachangelog.com) format covering all releases from v0.1.0 to v0.12.5; CVE note policy documented in the header
- **Retroactive release notes** — all 33 GitHub releases (v0.1.0–v0.12.5) updated with authored descriptions via `gh release edit`
- **GoReleaser changelog groups** — `changelog:` block in `.goreleaser.yaml` now groups future release notes by type: New features, Bug fixes, Security, CI/build/dependencies

---

## v0.13.1 — Test coverage ✓

Surface test coverage metrics so regressions are visible in CI and PRs.

- **Codecov integration** — `go test -coverprofile=coverage.txt -covermode=atomic` in the `test` CI job uploads to [codecov.io](https://codecov.io/gh/lewta/sendit) via `codecov/codecov-action@v5.5.3` (SHA-pinned); Codecov badge added to `README.md`
- **Coverage gate** — `codecov.yml` configures a project gate (≤2% drop vs base branch) and a patch gate (≥50% coverage on new code per PR)

---

## v0.13.2 — Benchmark suite ✓

Add Go benchmarks for the hot paths in the dispatch loop so performance regressions are caught before they reach `main`.

- **`internal/task`** — `BenchmarkSelectorPick` across 1, 10, and 100 targets; confirms O(1) Vose alias behaviour (~28–34 ns/op, zero allocs)
- **`internal/ratelimit`** — `BenchmarkClassifyStatusCode` (~6 ns/op), `BenchmarkClassifyError` (~8 ns/op), `BenchmarkRegistryWait` (~100 ns/op); all zero allocs
- **`internal/engine`** — `BenchmarkDispatch` with a no-op driver stub (~1 µs/op, 3 allocs); covers backoff check, rate-limit check, and metrics recording
- **CI** — `bench` job runs `go test -bench=. -benchmem -run='^$'` on every PR and stores `bench.txt` as a `bench-results` artifact

---

## v0.13.3 — Dependency audit ✓

Review and tighten the dependency tree before committing to a stable v1.0.0 API.

- **`go mod tidy`** — module graph confirmed clean; no unused indirect dependencies
- **Licence audit** — all 12 direct dependencies carry permissive licences (MIT, ISC, BSD-3-Clause, Apache-2.0); all compatible with the project's MIT licence
- **Alternatives review** — `x/net/html` (no stdlib HTML parser), `x/time/rate` (no stdlib token bucket), `viper` (env-overlay config complexity), `zerolog` (zero-allocation over `log/slog`) all retained as justified; all other deps are the only practical choice for their driver or feature
- **`docs/content/docs/dependencies.md`** — published page listing all 12 direct deps with purpose, licence, and alternatives rationale
- **`docs/content/docs/ossf.md`** — published OpenSSF Best Practices evidence page (supersedes local working document)

---

## v0.14.0 — Safari bookmarks + browser history tests ✓

Complete the browser input sources introduced in v0.11.0: add Safari bookmarks
support and add fixture-based unit tests for all SQLite and plist reading paths.

- **Safari bookmarks** — `sendit generate --from-bookmarks safari` now reads
  `~/Library/Safari/Bookmarks.plist` using `howett.net/plist` (MIT); handles
  both binary and XML plist formats; recursively extracts HTTP/HTTPS URLs from
  nested bookmark folders; non-http schemes (e.g. `reading-list://`) are silently
  skipped; macOS-only (errors clearly on Linux)
- **Fixture-based tests** — added unit tests for all SQLite-backed paths using
  in-process databases created with `modernc.org/sqlite`:
  - Chrome history: `historyFromSQLite` with a `urls` table fixture; verifies
    URL filtering, visit-count weight capping (max 10), and `--history-limit`
  - Firefox bookmarks: `firefoxBookmarks` with a `moz_places + moz_bookmarks`
    fixture; verifies `JOIN` query and non-http exclusion
  - Safari bookmarks: `safariBookmarks` with an XML plist fixture; verifies
    recursive folder descent, URL filtering, and weight assignment
- **Research item closed** — "Browser history and bookmarks harvesting" research
  is complete; core feature shipped in v0.11.0, Safari bookmarks completed here

---

## v0.13.4 — Table of contents for key documents ✓

Add a table of contents to the four main project documents so readers can navigate long files without scrolling.

- **`README.md`** — TOC covering all 15 top-level sections using GitHub-compatible anchor links
- **`ROADMAP.md`** — TOC listing every milestone (completed, planned, research) with anchor links
- **`CONTRIBUTING.md`** — TOC covering all 10 contribution workflow sections
- **`CODE_OF_CONDUCT.md`** — TOC covering all 7 main sections

---

## v0.15.0 — Test coverage improvement ✓

Raise overall test coverage from its current **62.1%** toward **~75%** before the
v1.0.0 stability commitment. The audit identified three categories of uncovered code:
intentionally untestable (needs real Chrome, live network, OS process), structurally
hard (engine dispatch loop), and straightforwardly testable but missing tests.
This milestone targets the third category and as much of the second as is practical.

**Current per-package coverage (baseline):**

| Package | Coverage | Primary gap |
|---|---|---|
| `cmd/sendit` (main) | 48.9% | `probe*`, `pinch*`, `printDryRun`, `validateCmd` all 0% |
| `cmd/sendit` (generate) | ~70% | `chromeBookmarks`, path-resolution functions all 0% |
| `internal/engine` | 55.7% | `Run`, `dispatch` 0%; `Start` 45%; `UpdatePacing` 56% |
| `internal/metrics` | 44.1% | `New`, `ServeHTTP` both 0% |
| `internal/driver` | 64.2% | `browser.Execute` 0% — intentionally skipped (needs Chrome) |
| `internal/ratelimit` | 97.6% | — |
| `internal/task` | 97.6% | — |
| `internal/resource` | 100% | — |

**Deliverables:**

- **Pure helper functions** — unit tests for all zero-coverage pure functions in
  `cmd/sendit/main.go`: `probeRcodeLabel`, `probeFormatBytes`, `probeSummary`,
  `detectProbeType`, `pinchSummary`, `isConnRefused`, `printDryRun`; these have
  no external dependencies and are straightforwardly table-driven
- **Chrome bookmarks** — fixture-based tests for `chromeBookmarks` and
  `walkChromeNode` using a synthetic Chrome `Bookmarks` JSON file; mirrors the
  Firefox and Safari fixture tests added in v0.14.0
- **Browser path resolution** — tests for `chromePath`, `firefoxPath`,
  `firefoxDefaultProfile`, `firefoxFallbackProfile` using temp directories;
  validates OS-specific path logic without touching the real filesystem
- **`historyDBInfo`** — expand SQLite fixture tests to cover the 11% currently
  missed (error paths and alternate schema branches)
- **Metrics** — tests for `metrics.New` (Prometheus registry initialisation) and
  `metrics.ServeHTTP` (`/metrics` and `/healthz` endpoints) using `httptest`
- **Engine dispatch integration** — a short-lived integration test that runs
  `engine.Run` with a stub no-op driver and a 100 ms timeout; exercises `dispatch`
  and the full pipeline (scheduler → resource gate → pool → driver); kept in a
  separate `_integration_test.go` file with a build tag so it does not run in
  the unit-test path
- **`scheduler.UpdatePacing` and `scheduler.Start`** — targeted tests for the
  remaining uncovered branches (mode switches, cron window lifecycle)
- **`validateCmd`** — extend existing tests to cover the uncovered flag/path branches

**Intentionally not targeted (documented as such):**

- `browser.Execute` — requires a real Chrome binary; skip annotation already in
  place in `driver_test.go`; noted in a `// coverage: intentionally skipped` comment
- `main()` entry point, `initLogger`, `writePID` — OS-level side effects; not
  unit-testable
- `probeWS`, `pinchTCP`, `pinchUDP` — require live network connections; out of
  scope for unit tests; candidate for a future integration test suite

---

## v0.15.1 — Integration test suite expansion ✓

The engine integration test infrastructure already exists
(`internal/engine/integration_test.go`, `//go:build integration`, 7 tests, CI job).
This milestone widens its scope, fills the missing scenarios, and wires integration
coverage into Codecov.

**Current state:**
- 7 integration tests in `internal/engine/` covering HTTP happy path, HTTP 429
  backoff, graceful shutdown, resource gate, DNS, PCAP, and WebSocket
- CI `integration` job runs `go test -race -tags integration -v ./internal/engine/...`
- Integration tests do **not** contribute to Codecov (no `-coverprofile` in the job)
- No cmd-level or CLI-level integration tests

**Deliverables:**

- **Widen CI scope** — change the integration job from `./internal/engine/...` to
  `./...` so any future integration-tagged tests in other packages are automatically
  picked up
- **Codecov integration coverage** — add `-coverprofile=integration-coverage.out`
  to the integration CI job and upload to Codecov with `flags: integration`; this
  surfaces engine dispatch, `Run`, and `dispatch` coverage that unit tests cannot reach
- **Hot-reload during dispatch** — integration test that starts the engine, waits for
  at least 3 requests, calls `Reload()` with a new target list, then verifies
  subsequent requests hit the new target; exercises the live reload path under real
  concurrency
- **Burst mode + `--duration`** — integration test that configures `mode: burst` with
  a short `ramp_up_s` and runs the engine with a context timeout; verifies requests
  are dispatched and that the engine stops cleanly at the deadline
- **Output writer end-to-end** — integration test that enables `output.enabled` with a
  temp JSONL file, dispatches ≥5 requests, and verifies the file contains valid
  newline-delimited JSON records with correct `url`, `status`, and `duration_ms` fields;
  complements the existing PCAP test
- **Per-domain rate-limit enforcement** — integration test that sets a per-domain RPS
  of 2 against a local httptest server, dispatches requests over a measured window, and
  asserts the observed RPS does not materially exceed the configured limit; catches
  regressions in the rate-limit registry wiring
- **cmd integration tests** — test the Cobra commands directly (not via `exec.Command`)
  using a shared test helper that invokes `rootCmd.Execute()` with args and a captured
  stdout buffer:
  - `validate` — valid config → exit 0; invalid config → exit 1 with error text
  - `start --dry-run` — prints dry-run summary, does not start the engine
  - `generate --targets-file` — emits valid YAML to stdout given a temp targets file
  - `version` — prints version string

**Not targeted:**
- `probe` and `pinch` network integration (require live external endpoints)
- `start` full run via CLI binary subprocess (covered by engine integration tests at
  the library level; binary-level testing deferred to a future E2E suite)

---

## v0.15.2 — Codecov Test Analytics ✓

Surface per-test pass/fail data in Codecov so failed test names and messages appear
directly in PR comments, removing the need to dig into CI logs.

**Approach:**

- Replace the raw `go test` call in the CI `test` job with
  [`gotestsum`](https://github.com/gotestyourself/gotestsum), which wraps `go test`
  and emits a JUnit XML report alongside the existing coverage profile:
  ```sh
  gotestsum --junitfile junit.xml -- -race -coverprofile=coverage.txt -covermode=atomic ./...
  ```
- Add a second Codecov upload step using `codecov/test-results-action` with
  `if: ${{ !cancelled() }}` so results are uploaded even when tests fail
- Pin both the `gotestsum` install and the action to commit SHAs (consistent with
  existing policy)

**Features unlocked:**

- Failed test names + failure messages shown in PR comments without opening CI logs
- Flaky test detection — tests that fail on `main` are flagged separately from new
  failures introduced by the PR
- Per-test duration tracking over time in the Codecov dashboard

---

## v1.0.0 — TUI + stable API

Terminal dashboard and commitment to a stable public API. By this point the OSSF Scorecard improvements (v0.12.x) will be in place; the `Contributors` check is expected to improve naturally as the project gains visibility following the TUI release.

- ✓ Live terminal UI using [Bubble Tea](https://github.com/charmbracelet/bubbletea) behind a `--tui` flag; plain log output remains the default
- ✓ Graceful fallback to plain logs when stdout is not a TTY (`ModeCharDevice` detection; zerolog silenced when TUI active)
- ✓ `internal/tui` package: `State` (lock-free ring buffer + atomic counters), Bubble Tea model with sparkline, `Run` entry point
- ✓ `Engine.SetObserver` hook — called after every dispatch; zero coupling to TUI internals
- v1.0.0 marks a stability commitment: CLI flags, config schema, and Prometheus metric names will not have breaking changes without a major version bump

```
┌─ sendit ──────────────────────────────────────────────────┐
│ mode: human   workers: 2/4   uptime: 00:04:32             │
├───────────────────────────────────────────────────────────┤
│ RECENT REQUESTS                                           │
│  200  GET  https://httpbin.org/get          142ms  12 KB  │
│  200  DNS  example.com                        4ms         │
│  429  GET  https://httpbin.org/status/429   201ms  ↩ 8s   │
│  200  GET  https://httpbin.org/get           98ms   9 KB  │
├───────────────────────────────────────────────────────────┤
│ TOTALS          requests: 312   errors: 4   bytes: 1.1 MB │
│ RATE LIMITS     httpbin.org ████░░ 0.8 rps               │
└───────────────────────────────────────────────────────────┘
```

---

## v0.10.6 — Packet capture ✓

Write the network traffic generated by a sendit session as a PCAP file for analysis in Wireshark or similar tools. Promoted from Research; see the Research section below for the full investigation notes. Closes [#70](https://github.com/lewta/sendit/issues/70).

- **Synthetic PCAP from result data** — generate a valid PCAP file from sendit's per-request telemetry (URL, timing, bytes, status); no root or `CAP_NET_RAW` privilege required; output is approximate (no TCP-level framing) but sufficient for replay and latency analysis in Wireshark
- **`--capture <file>` flag on `sendit start`** — write a PCAP to the specified path while the engine runs; file is finalised on clean shutdown or SIGTERM
- **`sendit export --pcap <results.jsonl>`** — post-run conversion of a JSONL result file to PCAP, enabling capture from any previous run
- **Output format** — PCAP (`.pcap`) for maximum tool compatibility
- **Docs** — document the `--capture` flag, the export subcommand, and the external-tooling alternative (`tcpdump` / `tshark` alongside sendit; Kubeshark for Docker deployments)

---

## Research — Non-standard traffic driver

Investigate adding a driver for non-standard or application-layer protocols that don't fit the existing HTTP/DNS/WebSocket/browser model.

Areas to explore:
- Protocol candidates: gRPC, raw TCP, ICMP, SMTP, FTP, custom binary protocols
- Whether a generic `raw` driver with a user-supplied payload and framing spec is preferable to per-protocol drivers
- How RCODEs / response codes map to the existing unified error classifier
- Connection pooling and state management for connection-oriented protocols
- What a config schema for non-HTTP targets looks like (no URL scheme, port-based, payload templating)

---

## Research — Aggressive / burst pacing mode ✓ (promoted to v0.14.1)

Investigation complete; promoted to a versioned milestone. See [v0.14.1](#v0141--burst-pacing-mode) below.

Original research notes: investigate a `burst` or `aggressive` pacing mode for scenarios where politeness constraints should be relaxed — load testing, internal infrastructure, or controlled chaos experiments.

Areas to explore:
- A `burst` mode that fires requests as fast as worker slots allow with no inter-request delay
- Configurable concurrency ramp-up (e.g. linearly increase workers to max over a warm-up period)
- Whether the existing resource gate (`cpu_threshold_pct`, `memory_threshold_mb`) is sufficient protection or needs a hard cap on total requests/duration
- A `--duration` flag for `start` that auto-stops after a fixed wall-clock time, useful for timed load runs
- How backoff and per-domain rate limits interact with burst mode (bypass, warn, or error)

---

## Research — Browser history and bookmarks harvesting ✓ (shipped in v0.11.0 / v0.14.0)

Investigation complete. Core feature shipped in v0.11.0; Safari bookmarks and fixture-based tests completed in v0.14.0. Related to [#49](https://github.com/lewta/sendit/issues/49) — the same browser automation knowledge applies to both driving traffic and sourcing targets.

Areas to explore:

- **Chrome / Chromium history** — `History` SQLite file (`urls` table, `visit_count` column) located at:
  - Linux: `~/.config/google-chrome/Default/History`
  - macOS: `~/Library/Application Support/Google/Chrome/Default/History`
  - Chrome must be closed or the file opened read-only (SQLite WAL mode may allow concurrent reads)
- **Chrome bookmarks** — `Bookmarks` JSON file in the same `Default/` directory; parse the `roots` tree recursively to extract `url` entries
- **Firefox history** — `places.sqlite` (`moz_places` table, `visit_count` column) at `~/.mozilla/firefox/<profile>/places.sqlite`; bookmarks share the same file via `moz_bookmarks`
- **Firefox profile discovery** — `profiles.ini` in the Firefox config dir; the default profile must be auto-detected when no explicit path is given
- **Safari** — history in `~/Library/Safari/History.db` (SQLite); bookmarks in `~/Library/Safari/Bookmarks.plist` (binary plist); macOS only
- **Cross-platform path resolution** — abstract browser profile paths behind an OS+browser lookup so the same flag works on Linux and macOS without manual path configuration
- **Filtering** — HTTP/HTTPS URLs only; strip query strings and fragments optionally; de-duplicate by normalised URL; respect `--history-limit`
- **Weight derivation** — map `visit_count` to a target weight (e.g. log-scaled) so high-traffic pages appear more frequently in generated traffic without dominating the distribution entirely
- **Privacy considerations** — document that history/bookmark data never leaves the local machine; the generated `config.yaml` contains only URLs, not browsing metadata

## Research — Packet capture output ✓ (shipped in v0.10.6)

Investigation notes for the packet capture feature shipped in v0.10.6. Related to [#70](https://github.com/lewta/sendit/issues/70).

Areas to explore:

- **External tooling (short-term)** — document how to run `tcpdump` or `tshark` alongside sendit to capture traffic; for Docker deployments, Kubeshark or a tcpdump sidecar are natural fits; this requires no code changes
- **Synthetic PCAP from result data** — sendit already collects URL, timing, bytes-read, and status code per request; investigate generating a valid PCAP file from this data without raw packet access; no root/CAP_NET_RAW privilege required; output would be approximate (no TCP-level detail) but sufficient for replay and latency analysis
- **Live capture via gopacket** — use `github.com/google/gopacket` (libpcap bindings) to capture packets on the network interface filtered by the sendit process; requires root or `CAP_NET_RAW`; adds a heavy CGO dependency that conflicts with the current `CGO_ENABLED=0` build
- **eBPF PID-filtered capture** — use eBPF to capture only packets originating from the sendit PID; avoids libpcap but requires a modern Linux kernel (5.8+) and elevated privileges
- **Output format** — PCAP (`.pcap`) for maximum tool compatibility; PCAPNG (`.pcapng`) if metadata per-packet is needed
- **Integration point** — a `--capture <file>` flag on `sendit start` or a post-run `sendit export --pcap` subcommand

Shipped approach: synthetic PCAP from result data using LINKTYPE_USER0 (147) in pure Go (`internal/pcap`). No CGO, libpcap, or elevated privileges required. `--capture <file>` flag added to `sendit start`; `sendit export --pcap <results.jsonl>` added for post-run conversion.

---

## Research — Live packet capture (future)

The v0.10.6 synthetic PCAP provides request-level telemetry but no TCP/IP framing. Future work to investigate true packet-level capture:

- **Live capture via gopacket** — use `github.com/google/gopacket` (libpcap bindings) to capture actual packets on the network interface, filtered by the sendit process; produces real PCAP with TCP/IP headers; requires root or `CAP_NET_RAW`; adds a CGO dependency that conflicts with the current `CGO_ENABLED=0` build; consider a build tag to keep the default binary CGO-free
- **eBPF PID-filtered capture** — use eBPF (e.g. `github.com/cilium/ebpf`) to capture only packets originating from the sendit PID, avoiding the promiscuous-mode overhead of libpcap; requires a modern Linux kernel (5.8+ for BTF, 5.15+ recommended) and `CAP_BPF` or root; no CGO, but Linux-only
- **PCAPNG** — upgrade the output format to PCAPNG (`.pcapng`) if per-packet metadata (interface name, comment fields, custom blocks) is needed; `PCAPNG` is backwards-compatible with Wireshark but adds format complexity
- **Docker / Kubernetes** — for containerised deployments, document how to use a `tcpdump` sidecar (or Kubeshark for Kubernetes) to capture traffic alongside sendit without modifying the binary

---

## Research — Repository security hardening ✓ (shipped in v0.10.4)

Review and enable GitHub's built-in security features to give the project a clear vulnerability disclosure process and broader automated dependency scanning.

Areas to explore:
- **Security policy** — add a `SECURITY.md` defining the supported versions and the process for reporting vulnerabilities (e.g. email or GitHub private reporting)
- **Private vulnerability disclosure** — enable GitHub's private vulnerability reporting feature so reporters can submit CVEs without opening a public issue; evaluate whether the default advisory workflow fits the project
- **Dependabot alerts** — confirm Dependabot security alerts are enabled (distinct from the Dependabot version-update PRs already in place); review alert thresholds and whether auto-dismiss rules are appropriate
- **Branch protection hardening** — review current branch protection rules on `main` for gaps (e.g. required signed commits, dismiss stale reviews on push)
- **OSSF Scorecard** — evaluate adding the OpenSSF Scorecard action to surface a public supply-chain security score
- **Docs site — security page** — add a dedicated Security page to the docs site summarising the security policy, supported versions, and how to report a vulnerability; link from the homepage and CLI reference
- **Docs site — `security.txt`** — add a `/.well-known/security.txt` (RFC 9116) to the GitHub Pages site (`docs/static/.well-known/security.txt`) so automated scanners and researchers can discover the disclosure contact and policy URL machine-readably

---

## Research — Multi-browser support (post-v1.0.0)

Investigate extending the `browser` driver to support Firefox and WebKit/Safari in addition to the current Chrome/Chromium. Deferred from v0.14.3 after research (March 2026) concluded no viable path exists today that is compatible with sendit's statically compiled, CGO-free, single-binary distribution model.

Full research findings are in [#49](https://github.com/lewta/sendit/issues/49#issuecomment-4106692916). Summary of why each option was rejected:

- **playwright-go** (`github.com/playwright-community/playwright-go`) — spawns a bundled Node.js subprocess at runtime; cannot be embedded in a static Go binary; incompatible with `CGO_ENABLED=0` distribution
- **Firefox via chromedp (CDP)** — Firefox dropped CDP support in Firefox 129 (mid-2024); removed from the Selenium ecosystem in early 2025; chromedp has no WebDriver BiDi implementation
- **rod** (`github.com/go-rod/rod`) — Chromium-only; same limitation as current chromedp; no multi-browser gain

**Unblocking condition:** A production-ready, CGO-free Go client for **WebDriver BiDi** (the cross-browser successor to CDP). Chrome, Firefox, and Safari all support or are implementing BiDi. Once a viable Go BiDi library emerges, revisit this item with an `engine: chromium|firefox|webkit` field under the `browser:` target block.

Areas to re-evaluate when revisiting:
- Go WebDriver BiDi client maturity (watch `seleniumhq/selenium` Go bindings and community alternatives)
- Per-task allocator model compatibility — does the library support spawn-per-task or require a shared browser instance?
- Headless browser availability on `ubuntu-latest` for non-Chromium engines
- Docker image strategy — single image vs separate `browser-expanded` tag with pre-installed browsers
