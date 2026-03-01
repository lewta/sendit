---
title: "sendit"
linkTitle: "sendit"
description: "A flexible traffic generator for HTTP, DNS, WebSocket, and browser targets."
---

{{< blocks/cover title="sendit" image_anchor="top" height="full" >}}
<a class="btn btn-lg btn-primary me-3 mb-4" href="/sendit/docs/">
  Documentation <i class="fas fa-arrow-alt-circle-right ms-2"></i>
</a>
<a class="btn btn-lg btn-secondary me-3 mb-4" href="https://github.com/lewta/sendit/releases/latest">
  Download <i class="fab fa-github ms-2 "></i>
</a>
<p class="lead mt-5">Simulate realistic user web traffic across HTTP, browser, DNS, and WebSocket — politely and precisely.</p>
{{< blocks/link-down color="info" >}}
{{< /blocks/cover >}}

{{% blocks/lead color="primary" %}}
**sendit** is a Go CLI tool for generating controlled, realistic traffic against web targets.

It never bursts aggressively — every request is paced through a configurable delay, resource gate, and per-domain rate limit before a worker slot is acquired.
{{% /blocks/lead %}}

{{% blocks/section color="dark" type="row" %}}
{{% blocks/feature icon="fa-bolt" title="Four driver types" %}}
**HTTP**, headless **browser** (chromedp), **DNS** (miekg), and **WebSocket** — all under one config file.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-clock" title="Three pacing modes" %}}
**human** (random delay), **rate_limited** (token bucket), and **scheduled** (cron windows) so traffic looks exactly like you want it to.
{{% /blocks/feature %}}

{{% blocks/feature icon="fa-shield-alt" title="Polite by design" %}}
Per-domain token buckets, decorrelated jitter backoff, and CPU/RAM resource gates ensure sendit never hammers targets or the local machine.
{{% /blocks/feature %}}
{{% /blocks/section %}}

{{% blocks/section %}}
## Quick install

```sh
git clone https://github.com/lewta/sendit
cd sendit
go build -o sendit ./cmd/sendit

# Test an endpoint immediately — no config needed
./sendit probe https://example.com
```

Or grab a pre-built binary from the [releases page](https://github.com/lewta/sendit/releases/latest).
{{% /blocks/section %}}
