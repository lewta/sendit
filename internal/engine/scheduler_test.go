package engine

import (
	"context"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
)

func humanCfg(minMs, maxMs int, jitter float64) config.PacingConfig {
	return config.PacingConfig{
		Mode:              "human",
		RequestsPerMinute: 20,
		JitterFactor:      jitter,
		MinDelayMs:        minMs,
		MaxDelayMs:        maxMs,
	}
}

func rateLimitedCfg(rpm float64) config.PacingConfig {
	return config.PacingConfig{
		Mode:              "rate_limited",
		RequestsPerMinute: rpm,
		JitterFactor:      0,
		MinDelayMs:        0,
		MaxDelayMs:        0,
	}
}

// TestScheduler_Human_DelayInBounds checks that humanWait produces delays
// within [min_delay_ms, max_delay_ms].
func TestScheduler_Human_DelayInBounds(t *testing.T) {
	const minMs, maxMs = 200, 400
	s := NewScheduler(humanCfg(minMs, maxMs, 0.4))
	ctx := context.Background()

	for i := 0; i < 20; i++ {
		start := time.Now()
		if err := s.Wait(ctx); err != nil {
			t.Fatalf("iter %d: Wait error: %v", i, err)
		}
		elapsed := time.Since(start)

		lo := time.Duration(minMs) * time.Millisecond
		hi := time.Duration(maxMs)*time.Millisecond + 20*time.Millisecond // small runtime tolerance

		if elapsed < lo {
			t.Errorf("iter %d: elapsed %v < min %v", i, elapsed, lo)
		}
		if elapsed > hi {
			t.Errorf("iter %d: elapsed %v > max %v", i, elapsed, hi)
		}
	}
}

// TestScheduler_Human_ContextCancel verifies Wait returns on cancellation.
func TestScheduler_Human_ContextCancel(t *testing.T) {
	s := NewScheduler(humanCfg(5000, 10000, 0)) // long delay
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := s.Wait(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Wait returned too late after cancel: %v", elapsed)
	}
}

// TestScheduler_RateLimited_ThrottlesRequests checks that N requests at a
// given RPM take at least (N-1)/RPS seconds.
func TestScheduler_RateLimited_ThrottlesRequests(t *testing.T) {
	const rpm = 600.0 // 10 rps → one token per 100ms
	s := NewScheduler(rateLimitedCfg(rpm))
	ctx := context.Background()

	const n = 5
	start := time.Now()
	for i := 0; i < n; i++ {
		if err := s.Wait(ctx); err != nil {
			t.Fatalf("iter %d: Wait error: %v", i, err)
		}
	}
	elapsed := time.Since(start)

	// At 10 rps, 5 requests should take at least (5-1)*100ms = 400ms.
	minExpected := time.Duration(n-1) * (time.Minute / time.Duration(rpm))
	if elapsed < minExpected-20*time.Millisecond {
		t.Errorf("elapsed %v < expected minimum %v", elapsed, minExpected)
	}
}

// TestScheduler_RateLimited_ContextCancel verifies cancellation works.
func TestScheduler_RateLimited_ContextCancel(t *testing.T) {
	const rpm = 0.01 // very slow
	s := NewScheduler(rateLimitedCfg(rpm))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// First call: bucket full, passes immediately.
	_ = s.Wait(ctx)

	// Second call: must wait ~6000s for next token.
	start := time.Now()
	err := s.Wait(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Wait returned too late: %v", elapsed)
	}
}

// TestScheduler_Scheduled_OutsideWindow blocks until context expires because
// no cron window is active at test time.
func TestScheduler_Scheduled_OutsideWindow(t *testing.T) {
	cfg := config.PacingConfig{
		Mode:              "scheduled",
		RequestsPerMinute: 60,
		Schedule: []config.ScheduleEntry{
			{
				Cron:              "0 3 31 2 *", // Feb 31 — never fires
				DurationMinutes:   1,
				RequestsPerMinute: 60,
			},
		},
	}
	s := NewScheduler(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	s.Start(ctx)

	// Wait should return quickly (hits the 5s poll timer, but context expires first).
	start := time.Now()
	_ = s.Wait(ctx) // may return nil or ctx error depending on timing
	elapsed := time.Since(start)

	if elapsed > 1*time.Second {
		t.Errorf("Wait in scheduled mode took too long: %v", elapsed)
	}
}

// TestSleepCtx_ShortDuration verifies sleepCtx respects context.
func TestSleepCtx_ShortDuration(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	if err := sleepCtx(ctx, 50*time.Millisecond); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("sleep too short: %v", elapsed)
	}
}

func TestSleepCtx_ZeroDuration(t *testing.T) {
	ctx := context.Background()
	start := time.Now()
	_ = sleepCtx(ctx, 0)
	if elapsed := time.Since(start); elapsed > 10*time.Millisecond {
		t.Errorf("zero sleep took too long: %v", elapsed)
	}
}

func TestSleepCtx_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	err := sleepCtx(ctx, 10*time.Second)
	if err == nil {
		t.Fatal("expected error after cancel, got nil")
	}
}
