package engine

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPool_AcquireRelease_Basic(t *testing.T) {
	p := NewPool(2, 1)
	ctx := context.Background()

	if err := p.Acquire(ctx, "http"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	p.Release("http")
}

func TestPool_Acquire_ContextCancel(t *testing.T) {
	p := NewPool(1, 1)
	ctx := context.Background()

	// Fill the single global slot.
	if err := p.Acquire(ctx, "http"); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	cancelCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := p.Acquire(cancelCtx, "http")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("Acquire returned too quickly: %v", elapsed)
	}

	p.Release("http") // clean up first slot
}

func TestPool_Browser_SubSemaphore(t *testing.T) {
	// 4 global slots, 1 browser slot.
	p := NewPool(4, 1)
	ctx := context.Background()

	// Acquire one browser slot.
	if err := p.Acquire(ctx, "browser"); err != nil {
		t.Fatalf("first browser Acquire: %v", err)
	}

	// Second browser slot should be blocked.
	cancelCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := p.Acquire(cancelCtx, "browser")
	if err == nil {
		t.Fatal("expected error: browser sub-semaphore should be full")
	}

	// Non-browser should still be acquirable (global still has room).
	if err := p.Acquire(ctx, "http"); err != nil {
		t.Fatalf("http Acquire while browser full: %v", err)
	}

	p.Release("browser")
	p.Release("http")
}

// TestPool_Wait verifies that Wait() blocks until all released.
func TestPool_Wait(t *testing.T) {
	p := NewPool(4, 1)
	ctx := context.Background()

	const goroutines = 4
	var released atomic.Int32

	for i := 0; i < goroutines; i++ {
		if err := p.Acquire(ctx, "http"); err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		for i := 0; i < goroutines; i++ {
			released.Add(1)
			p.Release("http")
		}
	}()

	waitDone := make(chan struct{})
	go func() {
		p.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		if released.Load() != goroutines {
			t.Errorf("Wait returned before all releases: released = %d", released.Load())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait timed out")
	}
}

// TestPool_Concurrency runs many goroutines through the pool to detect data races.
func TestPool_Concurrency(t *testing.T) {
	p := NewPool(3, 2)
	ctx := context.Background()

	var wg sync.WaitGroup
	const workers = 20

	for i := 0; i < workers; i++ {
		wg.Add(1)
		typ := "http"
		if i%5 == 0 {
			typ = "browser"
		}
		go func(taskType string) {
			defer wg.Done()
			if err := p.Acquire(ctx, taskType); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			p.Release(taskType)
		}(typ)
	}
	wg.Wait()
	p.Wait()
}

// TestPool_MaxConcurrency verifies that no more than maxWorkers goroutines
// hold a slot simultaneously.
func TestPool_MaxConcurrency(t *testing.T) {
	const max = 3
	p := NewPool(max, max)
	ctx := context.Background()

	var (
		mu      sync.Mutex
		current int
		peak    int
	)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := p.Acquire(ctx, "http"); err != nil {
				return
			}
			mu.Lock()
			current++
			if current > peak {
				peak = current
			}
			if current > max {
				t.Errorf("concurrency %d exceeds max %d", current, max)
			}
			mu.Unlock()

			time.Sleep(5 * time.Millisecond)

			mu.Lock()
			current--
			mu.Unlock()
			p.Release("http")
		}()
	}
	wg.Wait()

	if peak == 0 {
		t.Error("peak concurrency should be > 0")
	}
}
