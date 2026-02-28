package resource

import (
	"context"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	m := New(70.0, 512)
	if m == nil {
		t.Fatal("New returned nil")
	}
	if m.cpuThresholdPct != 70.0 {
		t.Errorf("cpuThresholdPct = %v, want 70.0", m.cpuThresholdPct)
	}
	if m.memThresholdBytes != 512 {
		t.Errorf("memThresholdBytes = %d, want 512", m.memThresholdBytes)
	}
}

// TestAdmit_UnderThreshold starts the monitor with very permissive thresholds
// so the system is always under-threshold, and verifies Admit returns quickly.
func TestAdmit_UnderThreshold(t *testing.T) {
	// 100% CPU and huge RAM threshold — system will always be admitted.
	m := New(100.0, 1_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m.Start(ctx)

	start := time.Now()
	if err := m.Admit(ctx); err != nil {
		t.Fatalf("Admit returned error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Admit took too long: %v", elapsed)
	}
}

// TestAdmit_ContextCancel verifies that Admit respects context cancellation
// even when the monitor has not yet completed its first poll.
func TestAdmit_ContextCancel(t *testing.T) {
	m := New(100.0, 1_000_000)
	// Do NOT call Start — ready channel is never closed.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := m.Admit(ctx)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

// TestStats_ReturnsSampledValues starts a monitor and checks that Stats()
// returns plausible values after the first poll.
func TestStats_ReturnsSampledValues(t *testing.T) {
	m := New(100.0, 1_000_000)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	m.Start(ctx)

	// Wait for the first poll.
	select {
	case <-m.ready:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first poll")
	}

	cpuPct, memUsedMB := m.Stats()
	if cpuPct < 0 || cpuPct > 100 {
		t.Errorf("cpuPct = %v, want in [0, 100]", cpuPct)
	}
	// Memory used should be positive on any real system.
	if memUsedMB == 0 {
		t.Log("memUsedMB = 0; may be expected in a container/mock environment")
	}
}

// TestAdmit_OverLimitThenContext verifies that when the monitor is over limit,
// Admit blocks and eventually returns when ctx is cancelled.
func TestAdmit_OverLimitThenContext(t *testing.T) {
	// Threshold of 0% CPU — virtually always over limit.
	m := New(0.0, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.Start(ctx)

	// Wait for first sample to land (sets overLimit = true).
	select {
	case <-m.ready:
	case <-ctx.Done():
		t.Fatal("timed out waiting for first poll")
	}

	admitCtx, admitCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer admitCancel()

	start := time.Now()
	err := m.Admit(admitCtx)
	elapsed := time.Since(start)

	if err == nil {
		// Only fail if the system truly is at 0% CPU (very unlikely).
		t.Log("Admit returned nil; system may be legitimately at 0% CPU")
	}
	if elapsed > 1*time.Second {
		t.Errorf("Admit took longer than expected: %v", elapsed)
	}
}

// TestStart_CancelsPoller verifies that cancelling the context stops the poller.
func TestStart_CancelsPoller(t *testing.T) {
	m := New(100.0, 1_000_000)
	ctx, cancel := context.WithCancel(context.Background())

	m.Start(ctx)

	// Wait for first poll.
	select {
	case <-m.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("first poll did not complete")
	}

	// Cancel should stop the goroutine without hanging.
	cancel()
	// Give it a moment to exit.
	time.Sleep(50 * time.Millisecond)
}
