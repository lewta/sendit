---
title: "Pacing Modes"
linkTitle: "Pacing"
weight: 3
description: "How sendit controls request timing: human, rate_limited, scheduled, and burst."
---

The `pacing` section of your config controls how requests are spaced over time. All modes gate dispatch **before** acquiring a worker slot, so a slow domain cannot stall the dispatch loop or starve other targets.

## `human` mode

Adds a random delay uniformly sampled from `[min_delay_ms, max_delay_ms]` before each request. This produces bursty but bounded traffic that resembles a real user.

`requests_per_minute` and `jitter_factor` are ignored in this mode.

```yaml
pacing:
  mode: human
  min_delay_ms: 800    # 0.8s minimum
  max_delay_ms: 8000   # 8s maximum
```

## `rate_limited` mode

Uses an `x/time/rate` token bucket at `requests_per_minute` with up to 200 ms of random jitter added after each token acquisition. This produces smooth, predictable throughput.

```yaml
pacing:
  mode: rate_limited
  requests_per_minute: 30
```

At 30 RPM the dispatch loop fires roughly once every 2 seconds, plus a small jitter.

## `scheduled` mode

Opens active windows defined by cron expressions. Within each window the mode behaves exactly like `rate_limited` at the window's own RPM. Between windows dispatch is paused (polling every 5 s).

```yaml
pacing:
  mode: scheduled
  schedule:
    - cron: "0 9 * * 1-5"      # weekdays at 09:00
      duration_minutes: 30
      requests_per_minute: 40
    - cron: "0 14 * * 1-5"     # weekdays at 14:00
      duration_minutes: 60
      requests_per_minute: 20
```

**Cron format:** standard 5-field (`minute hour dom month dow`). The engine uses UTC.

## `burst` mode

Fires requests as fast as worker slots allow with no inter-request delay. Intended for **internal or owned infrastructure** — load testing, chaos experiments, or benchmarking your own services.

> **Important:** `mode: burst` requires `--duration` on `sendit start`. The engine refuses to run a burst session without a time bound. This is a deliberate safety gate — never point burst at external targets you do not control.

```yaml
pacing:
  mode: burst
  ramp_up_s: 30   # optional: linearly ramp from slow to full speed over 30 s
```

`requests_per_minute`, `min_delay_ms`, `max_delay_ms`, `jitter_factor`, and `schedule` are all ignored in burst mode.

The **resource gate** (`cpu_threshold_pct`, `memory_threshold_mb`) still applies — the local machine always protects itself. **Backoff** still engages on repeated errors so a failing target does not get hammered indefinitely.

### `ramp_up_s`

An optional soft ramp-up that prevents a cold-start spike. When set, the inter-request delay decreases linearly from a high initial value down to zero over the specified number of seconds. With `ramp_up_s: 30` the initial delay is ~1.5 s and reaches zero after 30 s. Set to `0` (the default) for immediate full-speed dispatch.

### Running a burst session

```sh
# 5-minute burst with a 30-second ramp-up
sendit start --config config/burst.yaml --duration 5m

# Dry-run to preview settings before firing
sendit start --config config/burst.yaml --duration 5m --dry-run
```

## Dispatch pipeline

The pacing delay is just the first gate. After it fires, the request flows through:

```
Scheduler.Wait        pacing delay
  → resource.Admit    pause if CPU or RAM over threshold
  → backoff.Wait      per-domain delay after transient errors
  → ratelimit.Wait    per-domain token bucket
  → pool.Acquire      global semaphore + browser sub-semaphore
  → go driver.Execute
```

This ordering ensures that slow or rate-limited domains never consume worker slots while waiting, and pacing keeps the overall request rate bounded regardless of per-domain behaviour.
