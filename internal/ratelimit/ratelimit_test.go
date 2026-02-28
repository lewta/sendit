package ratelimit

import (
	"context"
	"testing"
	"time"
)

// --- Registry tests ---

func TestRegistry_WaitAllowsHighRPS(t *testing.T) {
	reg := NewRegistry(100.0, nil) // 100 rps — should not block in practice
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Fire 5 requests against the same domain; all should pass well within timeout.
	for i := 0; i < 5; i++ {
		if err := reg.Wait(ctx, "fast.example.com"); err != nil {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}
}

func TestRegistry_WaitRespectsContextCancel(t *testing.T) {
	// Very low RPS so the second call will exceed the context deadline.
	reg := NewRegistry(0.01, nil) // one request per 100 seconds
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// First call should succeed (bucket starts full).
	if err := reg.Wait(ctx, "slow.example.com"); err != nil {
		t.Fatalf("first wait: unexpected error: %v", err)
	}

	// Second call: rate.Limiter.Wait detects that the required 100s delay
	// exceeds the context deadline and returns a context error immediately.
	err := reg.Wait(ctx, "slow.example.com")
	if err == nil {
		t.Fatal("expected context error for second wait at very low RPS, got nil")
	}
}

func TestRegistry_PerDomainOverride(t *testing.T) {
	perDomain := map[string]float64{
		"fast.com": 1000.0,
		"slow.com": 0.01,
	}
	reg := NewRegistry(1.0, perDomain)

	// Fast domain should not block at all for a few requests.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	for i := 0; i < 5; i++ {
		if err := reg.Wait(ctx, "fast.com"); err != nil {
			t.Fatalf("fast domain request %d failed: %v", i, err)
		}
	}
}

func TestRegistry_LazilySeparatesDomains(t *testing.T) {
	reg := NewRegistry(100.0, nil)
	ctx := context.Background()

	// Multiple different domains should each get their own limiter.
	domains := []string{"alpha.com", "beta.com", "gamma.com"}
	for _, d := range domains {
		if err := reg.Wait(ctx, d); err != nil {
			t.Errorf("domain %s: unexpected error: %v", d, err)
		}
	}

	reg.mu.Lock()
	count := len(reg.limiters)
	reg.mu.Unlock()

	if count != len(domains) {
		t.Errorf("expected %d limiters, got %d", len(domains), count)
	}
}
