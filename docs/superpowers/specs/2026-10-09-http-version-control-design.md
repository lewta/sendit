# HTTP Version Control Design

Approved in conversation for issue #252; native execution authorized. Go 1.26.9+, no new dependencies. HTTP/3 is a separately tracked follow-up, not included in this delivery.

## Policy

Add integer `http.http_version`: omitted/0 preserves automatic HTTP/1.1 or TLS HTTP/2 negotiation; 1 forces HTTP/1.1 requests; 2 requires HTTPS and negotiates HTTP/2 before any HTTP request is sent. Other values, booleans, strings, nulls, floats and overflowing integers are rejected. Validate inline fields and defaults, including unused defaults; defaults continue to apply only to target-file entries. Other driver types do not acquire HTTP protocol controls.

Known plaintext targets with version 2 fail config validation. Templated schemes/URLs are checked after expansion by the driver. The same HTTPS/HTTP2 requirement applies to followed redirects. No cleartext h2c, no automatic H3, no new probe flag.

## Transport

Retain existing custom automatic transport behavior (do not substitute DefaultTransport, which would change proxy behavior). Reuse independently configured automatic/H1/H2 transports without mutating live settings per request. For strict TLS H2 use Go `http.Protocols` plus a TLS connection check requiring ALPN h2 before request transmission; certificate verification remains enabled. A request-level scheme guard also covers redirected plaintext requests. Keep auth, timeouts, body handling and redirect-limiter semantics.

Record the actual response `Proto` as `Meta["http_protocol"]` and log it at debug level from the driver so engine, probe and replay share reporting. The field describes the final response (including a policy-stopped redirect), not every hop; failures without a response omit it. No authenticated URLs or credential-bearing headers in the new log event.

## Visibility And Replay

Dry-run displays configured policy rather than claiming negotiation. Generated YAML preserves the field. New captures use envelope v2; v2 HTTP requests require integer `http_version`, including zero. Continue reading strict v1 HTTP captures without the field as automatic mode. Reject the new field in v1, missing/null/invalid policy in v2, and unsupported envelope versions. Non-HTTP request fields retain their existing schema in both versions. Capture requested policy, not negotiated protocol. Preserve literal expanded request data and all existing full-file preflight guarantees.

## Tests And Delivery

Tests cover raw numeric validation, aliases/merges/defaults, HTTPS and templated URL checks; trusted local TLS H2/H1/no-ALPN servers; zero application requests on failed forcing; plaintext redirects; mixed-version concurrency and connection reuse; auth/redirect limiter/cancellation/timeout behavior; negotiated metadata and debug logging; generator/dry-run; v1 compatibility/v2 strictness; actual capture/replay fidelity and parser fuzz seeds. Run make verify, govulncheck, replay/config fuzzing, and Hugo verification/build. One independent whole-branch review before the PR.

Update README, CLI help, Hugo pages, config examples, changelog and roadmap. Mark H1/H2 control complete; retain a linked planned HTTP/3 item and correct the old plaintext-H3 claim. Record narrowed scope on #252 and close it through the implementation PR.
