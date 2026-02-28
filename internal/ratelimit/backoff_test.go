package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"
)

// --- ClassifyStatusCode tests ---

func TestClassifyStatusCode(t *testing.T) {
	tests := []struct {
		code int
		want ErrorClass
	}{
		{200, ErrorClassNone},
		{201, ErrorClassNone},
		{204, ErrorClassNone},
		{301, ErrorClassPermanent},
		{400, ErrorClassPermanent},
		{403, ErrorClassPermanent},
		{404, ErrorClassPermanent},
		{429, ErrorClassTransient},
		{500, ErrorClassTransient},
		{502, ErrorClassTransient},
		{503, ErrorClassTransient},
		{504, ErrorClassTransient},
		{0, ErrorClassTransient}, // network error sentinel
	}
	for _, tc := range tests {
		got := ClassifyStatusCode(tc.code)
		if got != tc.want {
			t.Errorf("ClassifyStatusCode(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

// --- ClassifyError tests ---

func TestClassifyError_Nil(t *testing.T) {
	if got := ClassifyError(nil); got != ErrorClassNone {
		t.Errorf("ClassifyError(nil) = %v, want ErrorClassNone", got)
	}
}

func TestClassifyError_Canceled(t *testing.T) {
	if got := ClassifyError(context.Canceled); got != ErrorClassFatal {
		t.Errorf("ClassifyError(Canceled) = %v, want ErrorClassFatal", got)
	}
}

func TestClassifyError_DeadlineExceeded(t *testing.T) {
	if got := ClassifyError(context.DeadlineExceeded); got != ErrorClassFatal {
		t.Errorf("ClassifyError(DeadlineExceeded) = %v, want ErrorClassFatal", got)
	}
}

func TestClassifyError_OtherError(t *testing.T) {
	err := errors.New("connection reset")
	if got := ClassifyError(err); got != ErrorClassTransient {
		t.Errorf("ClassifyError(generic) = %v, want ErrorClassTransient", got)
	}
}

// --- BackoffRegistry tests ---

func newTestRegistry() *BackoffRegistry {
	return NewBackoffRegistry(100, 5000, 2.0, 3)
}

func TestBackoffRegistry_InitialState(t *testing.T) {
	r := newTestRegistry()
	if r.Attempts("unknown.com") != 0 {
		t.Error("fresh domain should have 0 attempts")
	}
}

func TestBackoffRegistry_RecordError_IncrementsAttempts(t *testing.T) {
	r := newTestRegistry()
	r.RecordError("host.com")
	if r.Attempts("host.com") != 1 {
		t.Errorf("attempts after 1 error = %d, want 1", r.Attempts("host.com"))
	}
	r.RecordError("host.com")
	if r.Attempts("host.com") != 2 {
		t.Errorf("attempts after 2 errors = %d, want 2", r.Attempts("host.com"))
	}
}

func TestBackoffRegistry_RecordError_ReturnsPositiveDelay(t *testing.T) {
	r := newTestRegistry()
	delay := r.RecordError("host.com")
	if delay <= 0 {
		t.Errorf("delay should be > 0, got %v", delay)
	}
}

func TestBackoffRegistry_RecordError_DelayWithinBounds(t *testing.T) {
	r := NewBackoffRegistry(100, 5000, 2.0, 10)
	// Run many iterations and ensure delay stays within [initial, max].
	for i := 0; i < 20; i++ {
		delay := r.RecordError("host.com")
		if delay < 100*time.Millisecond {
			t.Errorf("iteration %d: delay %v below initial_ms (100ms)", i, delay)
		}
		if delay > 5000*time.Millisecond {
			t.Errorf("iteration %d: delay %v exceeds max_ms (5000ms)", i, delay)
		}
	}
}

func TestBackoffRegistry_RecordSuccess_ResetsState(t *testing.T) {
	r := newTestRegistry()
	r.RecordError("host.com")
	r.RecordError("host.com")
	r.RecordSuccess("host.com")

	if r.Attempts("host.com") != 0 {
		t.Errorf("attempts after RecordSuccess = %d, want 0", r.Attempts("host.com"))
	}
}

func TestBackoffRegistry_Wait_NoBackoff(t *testing.T) {
	r := newTestRegistry()
	ctx := context.Background()
	// Domain with no recorded errors should return immediately.
	start := time.Now()
	if err := r.Wait(ctx, "clean.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Error("Wait on clean domain took too long")
	}
}

func TestBackoffRegistry_Wait_RespectsContextCancel(t *testing.T) {
	r := NewBackoffRegistry(10_000, 60_000, 2.0, 5) // very long delay
	r.RecordError("blocked.com")

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := r.Wait(ctx, "blocked.com")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("Wait took too long after cancel: %v", elapsed)
	}
}

func TestBackoffRegistry_MaxAttempts(t *testing.T) {
	r := NewBackoffRegistry(100, 5000, 2.0, 7)
	if r.MaxAttempts() != 7 {
		t.Errorf("MaxAttempts() = %d, want 7", r.MaxAttempts())
	}
}

func TestBackoffRegistry_ConcurrentAccess(t *testing.T) {
	r := newTestRegistry()
	done := make(chan struct{})

	for i := 0; i < 10; i++ {
		go func(n int) {
			domain := "host.com"
			for j := 0; j < 50; j++ {
				if n%3 == 0 {
					r.RecordSuccess(domain)
				} else {
					r.RecordError(domain)
				}
				r.Attempts(domain)
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

// TestBackoffRegistry_EvictsMaxedOutDomain verifies that a domain whose backoff
// delay has elapsed and that has reached max_attempts is removed from the map.
func TestBackoffRegistry_EvictsMaxedOutDomain(t *testing.T) {
	// maxAttempts=2, very short initial delay so nextAllowed expires quickly.
	r := NewBackoffRegistry(1, 10, 2.0, 2)

	r.RecordError("evict.com")
	r.RecordError("evict.com")
	// attempts == maxAttempts; nextAllowed is ~1-10ms in the future.

	// Sleep past the delay so remaining <= 0 in Wait.
	time.Sleep(20 * time.Millisecond)

	ctx := context.Background()
	if err := r.Wait(ctx, "evict.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Entry must have been evicted.
	if r.Attempts("evict.com") != 0 {
		t.Error("domain should have been evicted after max attempts + delay expired")
	}
}

func TestBackoffRegistry_IsolatesDomains(t *testing.T) {
	r := newTestRegistry()
	r.RecordError("a.com")
	r.RecordError("a.com")

	// b.com should be unaffected.
	if r.Attempts("b.com") != 0 {
		t.Errorf("b.com attempts should be 0, got %d", r.Attempts("b.com"))
	}

	r.RecordSuccess("a.com")
	if r.Attempts("a.com") != 0 {
		t.Errorf("a.com attempts should be 0 after success, got %d", r.Attempts("a.com"))
	}
}
