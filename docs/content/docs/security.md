---
title: "Security"
linkTitle: "Security"
weight: 90
description: "Security policy, supported versions, and how to report a vulnerability."
---

## Supported versions

sendit follows a rolling release model. Only the latest stable release receives security updates.

| Version       | Supported |
|---------------|-----------|
| Latest stable | ✓         |
| All previous  | ✗         |

## Security hardening

The [latest stable release](https://github.com/lewta/sendit/releases/latest) receives security updates. Security hardening from the July 2026 Codex Security scan shipped in `v1.2.5` and remains included in current releases. That hardening keeps supported workflows intact while tightening several defaults:

- Cross-host HTTP redirects are blocked by default; set `http.allow_cross_host_redirects: true` only when cross-host redirects are intentional.
- HTTPS-to-HTTP redirects are rejected at every hop before a cleartext request is sent; cross-host permission does not override the downgrade check.
- During normal engine runs, opt-in cross-host redirects pass through per-domain rate limiting before the redirected request is sent. Replay intentionally bypasses engine rate limiting to preserve recorded timing.
- `sendit generate --url` keeps all crawl requests on the seed origin, checks every resolved address before dialing, disables ambient proxies, and bounds response bytes plus discovery/robots/sitemap/redirect state.
- The optional Prometheus metrics listener binds to loopback by default because metric labels include target domains; set `metrics.bind_address: 0.0.0.0` only when exposing metrics intentionally.
- Scheduled pacing remains paused outside active cron windows instead of dispatching after each poll interval.

No CVE has been assigned for this release. See the [v1.2.5 release notes](https://github.com/lewta/sendit/releases/tag/v1.2.5) for the full changelog and artifacts.

## Configuration trust policy

Target configs are trusted operator input by default. `start` and `validate` accept `--config-trust=restricted` and explicit grants for environment names, read roots, one output root, overwrite, and private-network CIDRs. Prefer this mode for downloaded, CI-supplied, or multi-tenant configs. Grant authority comes only from CLI arguments controlled by the operator; YAML requests can only retain or narrow it. Accepted reloads cannot broaden or restore removed authority. Invalid policy flags fail before config reads, and authority-shaped YAML is rejected before secondary file reads or engine startup.

Restricted mode rechecks authentication environment names when credentials are resolved, reads `targets_file`, `vars_file`, SFTP private keys, and `known_hosts` only through granted paths, and prepares result/PCAP outputs inside the granted root. New output files are created exclusively; restricted output rejects non-regular files, symlinks, and hard links, and rechecks the opened inode and overwrite grant immediately before truncation. A failed second output preflight does not truncate the first destination. Output settings are fixed for the process lifetime; reload rejects changes or authority revocation for an active result/PCAP sink. Trusted mode preserves existing config behavior.

This policy is not a process sandbox or a general destination allow-list. Normal HTTP, browser, DNS, WebSocket, gRPC, and SFTP target connections retain their driver behavior; the private-network field remains part of the monotonic authority contract but does not claim interception of every target driver. Metrics listeners, browser child processes, remote SFTP paths, and application-level effects are separate boundaries. Continue reviewing configs and use OS/container isolation appropriate to their source.

Daemon PID records default to a private per-user cache directory and are exclusively created, link-checked, lifetime-locked, size-bounded, and removed only when the path still names the owned file. Startup fails closed on acquisition errors and control commands reject legacy or unlocked records before signaling. Restricted configs require this operator-owned default so untrusted YAML cannot choose a PID-file side effect; trusted configs retain custom paths on lock-capable local storage. Linux uses pidfds. Darwin and FreeBSD recheck the locked record immediately before signaling but retain a small exit/PID-reuse race. Windows applies a protected current-user/System ACL and provides handle-based status only; graceful stop and reload require a foreground process or external service manager. Custom trusted-mode paths remain an operator-controlled denial-of-service surface if another principal can pre-create entries in their directory.

Path checks resolve symlinks and enforce directory containment, reject sibling-prefix escapes and traversal components, require read paths to exist, and allow only a missing final output filename under an existing parent. Protected secondary inputs compare pre-open and opened file identity and enforce an 8 MiB cap. Protected outputs use exclusive create or delayed truncation and repeat containment, identity, regular-file, and link-count checks at commit. Windows drive and UNC paths use native metadata handles to resolve junctions as well as symlinks; device namespaces and ambiguous Win32 aliases are rejected. Canonical directory spelling is preserved so case-sensitive directories cannot widen a grant. Files can still be changed in place by another principal with filesystem write authority; policy checks do not replace filesystem permissions.

The address policy normalizes IPv4-mapped IPv6. `generate --url` requires explicit CIDR grants for special-use ranges, including private, loopback, link-local, shared, benchmark, documentation, protocol-assignment/anycast, transition, and reserved addresses. Invalid, unspecified, multicast, limited-broadcast, and zoned addresses remain denied even with grants. Ordinary public IPv4 excludes the [IANA special-purpose ranges](https://www.iana.org/assignments/iana-ipv4-special-registry/); public IPv6 must also belong to an [allocated RIR prefix](https://www.iana.org/assignments/ipv6-unicast-address-assignments/) and exclude [IPv6 special-purpose ranges](https://www.iana.org/assignments/iana-ipv6-special-registry/). The auditable snapshot was reviewed on 2026-10-24. Future IPv6 allocations fail closed until the snapshot is updated or the operator explicitly grants the prefix. The crawler rejects an entire mixed DNS answer before dialing and resolves again for every new connection. A literal private seed is not an implicit grant.

Policy summaries contain only mode, capability names, normalized paths, prefixes, and permission bits. They do not resolve or print environment values, file contents, or credentials. Existing dry-run examples and other logs may still include application data; the policy summary does not provide general redaction.

## Replay data handling

Replay envelopes exclude configured authentication, custom HTTP headers (including inactive blocks), URL userinfo, and SFTP credentials. Non-replayable records contain a reason instead of a request snapshot; there is no credential-saving override. The decoder checks executable fields rather than trusting a `replayable: true` claim.

CSV and JSONL telemetry remove URL userinfo, including from malformed URLs, and replace the associated error text with a generic omission message. This does not redact arbitrary application data in URLs, bodies, messages, metadata, or logs. Treat captures as sensitive application data; configured-credential exclusion is not general secret detection.

New output files use mode `0600`. Replay rejects existing POSIX outputs with group/other permissions. Replay, generate, and PCAP export reject symlink/hard-link output aliases before truncation; replay also rejects input/output identity. Restricted engine output keeps existing permissions when appending/truncating and requires explicit overwrite authority for destructive opens. Windows uses native access controls. Output failures may leave partial files; neither mode promises crash-durable transactions.

Proxy settings, proxy credentials, system trust stores, browser state, and destination behavior are not captured; drivers retain their existing environment behavior. Replay can execute up to 10,000 accepted records concurrently and does not apply engine resource/rate/backoff gates. See [CLI validation and execution](../cli/#validation-and-execution).

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Use GitHub's [private vulnerability reporting](https://github.com/lewta/sendit/security/advisories/new) to submit a report confidentially. You will receive acknowledgement within **48 hours** and a resolution target within **7 days**.

Please include:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept
- Any suggested fix or mitigation (optional)

## Disclosure policy

We follow coordinated disclosure. Once a fix is available, we will publish a [GitHub Security Advisory](https://github.com/lewta/sendit/security/advisories) and release a patched version simultaneously. We aim to resolve confirmed reports within 30 days of a confirmed report.

## Build provenance

Maintainers publish by explicitly dispatching `release-production.yml` from protected `main` with a strict SemVer tag (including valid prerelease/build metadata) and a full commit SHA on `main` ancestry. The workflow must match freshly fetched `main`; a newer main commit requires a fresh dispatch. Version-tag pushes do not publish. A separate trusted control checkout authorizes the immutable candidate before setup or publication, and existing tags must match exactly. Matching existing tags support retries; tags are never retargeted. AUR and tap recovery require the same exact published release tuple. Tap checksum filenames must match exactly and uniquely before an App token is minted. AUR recovery uses current trusted configuration and runs completion generation before the AUR key is supplied.

Publication jobs reference the `release-production` environment. After its approval gate, the production job mints short-lived GitHub App installation tokens scoped only to `lewta/sendit`: one immediately before exact tag creation and a fresh one immediately before release publication. The workflow explicitly requests only repository contents write and workflow write (plus GitHub's implicit metadata read); workflow write is required when a release commit contains workflow-file changes. Candidate completion code and the release-configuration check run in a separate credential-free preparation job; the privileged runner downloads only the resulting completion artifacts, and GoReleaser uses the freshly authorized control configuration with candidate hooks disabled. The ordinary workflow token remains contents-read-only, checkouts never persist credentials, and the pinned token action attempts to revoke both tokens when the job ends.

The dedicated `sendit-release` App must be installed only on `lewta/sendit` with **Contents: read and write** plus **Workflows: read and write**; metadata remains read-only. Store its client ID as the `release-production` environment variable `RELEASE_APP_CLIENT_ID`, and store its private key as the environment secret `RELEASE_APP_PRIVATE_KEY`. The environment requires review by `lewta`, allows that reviewer to approve their own run, disables administrator bypass, and limits deployments to protected `main`. This is a solo-approval gate, not two-person separation. Use separate active version-tag rulesets: restrict creation with only this App allowed to bypass, then restrict update and deletion with no bypass actor, including the App. Relocate and rotate the other publication credentials, disable historical privileged workflow identities, and keep ordinary workflow tokens' repository contents access read-only; restrict any other job-scoped writes to the required OIDC and artifact-attestation permissions. Source changes alone do not apply those live controls.

The App identity is not cryptographically bound to this workflow. Anyone who can read its private key can mint a repository-scoped installation token, and a token exposed during a run can exercise its contents/workflow permissions until it expires or the action revokes it (normally at job end). Environment approval, secret scoping, immutable action pins, exact repository/permission requests, tag rules, and audit review reduce that risk; they do not turn the App into a step-level capability. Use a separate App and key for production releases, never reuse the tap App, and revoke the key or uninstall the App if a setup verification exposes unexpected scope.

Before the first production release, maintainers may separately approve one manual run of `release-app-auth-check.yml` from protected `main`, supplying the exact full `main` commit SHA. The diagnostic waits for the same `release-production` environment approval, requests a token with the production Contents/Workflows write permissions, and then makes only two GET requests: it requires the installation token to expose exactly `lewta/sendit`, and it requires the repository's `main` ref to match the supplied dispatch SHA. It does not check out or run repository code, print the token or API responses, create a tag, or publish a release. The pinned token action attempts revocation in its post-job step even when a later step fails; abrupt runner loss can still prevent that cleanup.

A successful diagnostic proves that the stored client ID and private key can mint the requested repository-scoped token and read the expected repository state. Because it deliberately performs no write, it does **not** prove tag creation, the tag rulesets' bypass/immutability behavior, signing or notarization credentials, or release publication. Preserve the workflow run and audit evidence, then remove this one-time diagnostic and its policy assertions through a normal reviewed pull request. If authentication fails or the repository scope is unexpected, revoke the App key or uninstall the App before any retry.

All release artifacts from v0.12.2 onwards include [SLSA provenance attestations](https://slsa.dev) generated by GitHub Actions. You can verify that a downloaded artifact was produced by this repository's CI pipeline and has not been tampered with:

```sh
gh attestation verify <artifact> --owner lewta
```

Requires the [GitHub CLI](https://cli.github.com) (v2.49.0+). On success the command prints the signing certificate details and exits 0; on failure it exits non-zero.

Example:

```sh
gh attestation verify 'sendit_<version>_linux_amd64.tar.gz' --owner lewta
# Current releases identify .github/workflows/release-production.yml as the publisher.
# Historical releases retain their original workflow identity.
```

## Security policy

The full security policy is maintained in [SECURITY.md](https://github.com/lewta/sendit/blob/main/SECURITY.md) in the repository root.
