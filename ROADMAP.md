# Roadmap

Features planned for future releases of sendit. Contributions are welcome — open an issue to discuss before starting work on a large item.

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

## v0.11.2 — AUR package

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

## v0.12.3 — OSSF Scorecard: Branch-Protection ✓

Raise the `Branch-Protection` check by adding required status checks to the `baseline-branch-rule` ruleset.

- **Required status checks** — added `lint` and `test` CI jobs as required checks so PRs cannot be merged until both pass
- **Admin bypass preserved** — the `RepositoryRole/Admin` bypass actor is intentionally retained while the project is single-maintainer; can be removed when a second maintainer is added

---

## v0.12.5 — OSSF Scorecard: Fuzzing

Integrate fuzz testing to catch parser and input-handling bugs that unit tests miss. Fixes the `Fuzzing` check (currently 0/10).

The Scorecard check accepts native Go fuzz functions (`func FuzzXxx(f *testing.F)`), which require no external service — just `go test -fuzz`.

Targets worth fuzzing:

- **`internal/config`** — `FuzzLoad`: feed arbitrary YAML bytes through the config loader; catches panics and unexpected parse errors on malformed input
- **`internal/task`** — `FuzzSelector`: fuzz the Vose alias selector with arbitrary weight slices; validates O(1) pick invariants under edge-case inputs (empty slice, zero weights, single element)
- **`internal/ratelimit`** — `FuzzClassifyError`: fuzz the error-string classifier with arbitrary error messages; ensures no panic and that every input maps to a valid status code
- **`internal/pcap`** — `FuzzWriteRecord`: fuzz the PCAP record writer with arbitrary result fields (URL, status, duration, bytes); catches any encoding panic

**Implementation:**

- Add `_fuzz_test.go` files in each target package containing one or more `FuzzXxx` functions with a small seed corpus (`f.Add(...)` calls covering representative inputs and known edge cases)
- Add a `fuzz` job to `ci.yml` that runs `go test -fuzz=. -fuzztime=30s` for each fuzz target on every PR — short enough to be fast, long enough to catch obvious regressions
- Seed corpora committed alongside tests so findings are reproducible

## v0.12.4 — OSSF Scorecard: CII Best Practices

Register the project on the OpenSSF Best Practices platform and link the badge. Fixes the `CII-Best-Practices` check (currently 0/10).

- **Register** — submit the project at [bestpractices.coreinfrastructure.org](https://bestpractices.coreinfrastructure.org) and work through the passing-level criteria
- **Badge** — add the Best Practices badge to `README.md` alongside the existing OSSF Scorecard badge
- **Gap analysis** — the passing criteria overlap significantly with what is already in place (CI, tests, SECURITY.md, license, documented versioning); document any gaps discovered during registration

---

## v1.0.0 — TUI + stable API

Terminal dashboard and commitment to a stable public API. By this point the OSSF Scorecard improvements (v0.12.x) will be in place; the `Contributors` check is expected to improve naturally as the project gains visibility following the TUI release.

- Live terminal UI using [Bubble Tea](https://github.com/charmbracelet/bubbletea) behind a `--tui` flag; plain log output remains the default
- Graceful fallback to plain logs when stdout is not a TTY
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

## Research — Aggressive / burst pacing mode

Investigate a `burst` or `aggressive` pacing mode for scenarios where politeness constraints should be relaxed — load testing, internal infrastructure, or controlled chaos experiments.

Areas to explore:
- A `burst` mode that fires requests as fast as worker slots allow with no inter-request delay
- Configurable concurrency ramp-up (e.g. linearly increase workers to max over a warm-up period)
- Whether the existing resource gate (`cpu_threshold_pct`, `memory_threshold_mb`) is sufficient protection or needs a hard cap on total requests/duration
- A `--duration` flag for `start` that auto-stops after a fixed wall-clock time, useful for timed load runs
- How backoff and per-domain rate limits interact with burst mode (bypass, warn, or error)

---

## Research — Browser history and bookmarks harvesting

Investigate the feasibility of reading local browser history and bookmarks as input sources for `sendit generate --from-history` and `--from-bookmarks` (planned for v0.11.0). Related to [#49](https://github.com/lewta/sendit/issues/49) — the same browser automation knowledge applies to both driving traffic and sourcing targets.

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
