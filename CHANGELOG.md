## [Unreleased]

### Changed
- Bumped modernc.org/sqlite from 1.56.0 to 1.57.0 (semver-minor) for upstream bug fixes
- Bumped google.golang.org/grpc from 1.83.0 to 1.83.2 (semver-patch) for upstream bug fixes
- Bumped github/codeql-action group (init/autobuild/analyze/upload-sarif) from v4.37.7 to v4.37.9 for upstream fixes

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
