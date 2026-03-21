---
title: "OpenSSF Best Practices"
linkTitle: "OpenSSF"
weight: 92
description: "Evidence and criteria for the OpenSSF Best Practices passing badge."
---

sendit holds the [OpenSSF Best Practices **passing** badge](https://bestpractices.coreinfrastructure.org/projects/12213)
at 100% — all 67 passing-level criteria are answered Met or N/A.

[![OpenSSF Best Practices](https://bestpractices.coreinfrastructure.org/projects/12213/badge)](https://bestpractices.coreinfrastructure.org/projects/12213)

## Basics

| Criterion | Status | Evidence |
|-----------|--------|----------|
| `description_good` | Met | [README](https://github.com/lewta/sendit#readme) |
| `interact` | Met | [CONTRIBUTING.md](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md) |
| `contribution` | Met | [CONTRIBUTING.md](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md) |
| `contribution_requirements` | Met | [CONTRIBUTING.md#contribution-requirements](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md#contribution-requirements) |
| `floss_license` | Met | MIT — [LICENSE](https://github.com/lewta/sendit/blob/main/LICENSE) |
| `floss_license_osi` | Met | [opensource.org/license/mit](https://opensource.org/license/mit) |
| `license_location` | Met | [LICENSE](https://github.com/lewta/sendit/blob/main/LICENSE) |
| `documentation_basics` | Met | [lewta.github.io/sendit](https://lewta.github.io/sendit/) |
| `documentation_interface` | Met | [CLI Reference](https://lewta.github.io/sendit/docs/cli/) |
| `sites_https` | Met | All project URLs served over HTTPS |
| `discussion` | Met | [GitHub Issues](https://github.com/lewta/sendit/issues) |
| `english` | Met | All documentation and issue responses in English |
| `maintained` | Met | [Releases](https://github.com/lewta/sendit/releases) |

## Change Control

| Criterion | Status | Evidence |
|-----------|--------|----------|
| `repo_public` | Met | [github.com/lewta/sendit](https://github.com/lewta/sendit) |
| `repo_track` | Met | [Commit history](https://github.com/lewta/sendit/commits/main) |
| `repo_interim` | Met | All work via feature branches and PRs |
| `repo_distributed` | Met | Git |
| `version_unique` | Met | [Tags](https://github.com/lewta/sendit/tags) |
| `version_semver` | Met | Semantic Versioning — [Releases](https://github.com/lewta/sendit/releases) |
| `version_tags` | Met | [Tags](https://github.com/lewta/sendit/tags) |
| `release_notes` | Met | All releases have authored notes; [CHANGELOG.md](https://github.com/lewta/sendit/blob/main/CHANGELOG.md) |
| `release_notes_vulns` | Met | No CVEs fixed to date; CVE policy documented in CHANGELOG.md header |

## Reporting

| Criterion | Status | Evidence |
|-----------|--------|----------|
| `report_process` | Met | [GitHub Issues](https://github.com/lewta/sendit/issues) |
| `report_tracker` | Met | [GitHub Issues](https://github.com/lewta/sendit/issues) |
| `report_responses` | Met | All filed issues acknowledged |
| `enhancement_responses` | Met | [GitHub Issues](https://github.com/lewta/sendit/issues) |
| `report_archive` | Met | GitHub Issues — publicly searchable |
| `vulnerability_report_process` | Met | [SECURITY.md](https://github.com/lewta/sendit/blob/main/SECURITY.md) |
| `vulnerability_report_private` | Met | [Private advisory reporting](https://github.com/lewta/sendit/security/advisories) |
| `vulnerability_report_response` | Met | 14-day initial response documented in [SECURITY.md](https://github.com/lewta/sendit/blob/main/SECURITY.md) |

## Quality

| Criterion | Status | Evidence |
|-----------|--------|----------|
| `build` | Met | `go build ./cmd/sendit` — [CONTRIBUTING.md](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md#running-tests-and-the-linter) |
| `build_common_tools` | Met | Standard Go toolchain |
| `build_floss_tools` | Met | Go is FLOSS — [go.mod](https://github.com/lewta/sendit/blob/main/go.mod) |
| `test` | Met | `go test ./...` — [CONTRIBUTING.md](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md#running-tests-and-the-linter) |
| `test_invocation` | Met | `go test ./...` is the idiomatic Go test invocation |
| `test_most` | Met | [codecov.io/gh/lewta/sendit](https://codecov.io/gh/lewta/sendit) |
| `test_continuous_integration` | Met | [ci.yml](https://github.com/lewta/sendit/actions/workflows/ci.yml) |
| `test_policy` | Met | [CONTRIBUTING.md#testing-policy](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md#testing-policy) |
| `tests_are_added` | Met | Fuzz tests added with new input-handling functionality |
| `tests_documented_added` | Met | [CONTRIBUTING.md#testing-policy](https://github.com/lewta/sendit/blob/main/CONTRIBUTING.md#testing-policy) |
| `warnings` | Met | golangci-lint — [.golangci.yml](https://github.com/lewta/sendit/blob/main/.golangci.yml) |
| `warnings_fixed` | Met | CI fails on lint errors |
| `warnings_strict` | Met | gosec + staticcheck enabled |

## Security

| Criterion | Status | Evidence |
|-----------|--------|----------|
| `know_secure_design` | Met | Attested on platform |
| `know_common_errors` | Met | Attested on platform |
| `crypto_published` | Met | Uses stdlib `crypto/tls` only |
| `crypto_call` | Met | No crypto reimplemented — [go.mod](https://github.com/lewta/sendit/blob/main/go.mod) |
| `crypto_floss` | Met | stdlib crypto is FLOSS |
| `crypto_keylength` | Met | Delegated to stdlib TLS (NIST-compliant) |
| `crypto_working` | Met | No broken algorithms used |
| `crypto_weaknesses` | Met | stdlib TLS avoids known-weak algorithms |
| `crypto_pfs` | Met | TLS 1.3 default in Go stdlib provides PFS |
| `crypto_password_storage` | N/A | sendit stores no passwords |
| `crypto_random` | N/A | sendit generates no cryptographic keys |
| `delivery_mitm` | Met | HTTPS downloads + SLSA provenance + checksums.txt — [Releases](https://github.com/lewta/sendit/releases) |
| `delivery_unsigned` | Met | checksums.txt served only via HTTPS |
| `vulnerabilities_fixed_60_days` | Met | govulncheck blocks merge on any known CVE — [security.yml](https://github.com/lewta/sendit/blob/main/.github/workflows/security.yml) |
| `vulnerabilities_critical_fixed` | Met | govulncheck + Dependabot security PRs |
| `no_leaked_credentials` | Met | All secrets via GitHub Actions secrets only |

## Analysis

| Criterion | Status | Evidence |
|-----------|--------|----------|
| `static_analysis` | Met | golangci-lint + CodeQL — [ci.yml](https://github.com/lewta/sendit/blob/main/.github/workflows/ci.yml) |
| `static_analysis_common_vulnerabilities` | Met | gosec covers OWASP-class checks |
| `static_analysis_fixed` | Met | CI blocks merge on lint failure |
| `static_analysis_often` | Met | Runs on every PR |
| `dynamic_analysis` | Met | Native Go fuzz tests + `go test -race` — [ci.yml](https://github.com/lewta/sendit/blob/main/.github/workflows/ci.yml) |
| `dynamic_analysis_unsafe` | N/A | Go is a memory-safe language |
| `dynamic_analysis_enable_assertions` | Met | `go test -race` run in CI |
| `dynamic_analysis_fixed` | Met | Fuzz findings and race conditions addressed before merge |
