package resource

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

const pollInterval = 2 * time.Second

// Monitor polls CPU and memory usage and provides an Admit gate that
// blocks dispatch when resources are over threshold.
type Monitor struct {
	cpuThresholdPct   float64
	memThresholdBytes uint64

	cond      *sync.Cond
	cpuPct    float64
	memUsedMB uint64
	overLimit bool

	ready chan struct{} // closed once first poll completes
}

// New creates a Monitor and starts polling in the background.
// Call Stop (via context cancellation) to halt the poller.
func New(cpuThresholdPct float64, memThresholdMB uint64) *Monitor {
	m := &Monitor{
		cpuThresholdPct:   cpuThresholdPct,
		memThresholdBytes: memThresholdMB,
		ready:             make(chan struct{}),
	}
	m.cond = sync.NewCond(&sync.Mutex{})
	return m
}

// Start begins the background polling goroutine; it stops when ctx is cancelled.
func (m *Monitor) Start(ctx context.Context) {
	go m.poll(ctx)
}

func (m *Monitor) poll(ctx context.Context) {
	first := true
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		m.sample()
		if first {
			close(m.ready)
			first = false
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Monitor) sample() {
	cpuPcts, err := cpu.Percent(0, false)
	cpuPct := 0.0
	if err == nil && len(cpuPcts) > 0 {
		cpuPct = cpuPcts[0]
	}

	vmStat, err := mem.VirtualMemory()
	memUsedMB := uint64(0)
	if err == nil {
		memUsedMB = vmStat.Used / (1024 * 1024)
	}

	over := cpuPct >= m.cpuThresholdPct || memUsedMB >= m.memThresholdBytes

	m.cond.L.Lock()
	m.cpuPct = cpuPct
	m.memUsedMB = memUsedMB
	m.overLimit = over
	m.cond.L.Unlock()
	m.cond.Broadcast() // wake any Admit callers waiting on the cond

	if over {
		log.Debug().
			Float64("cpu_pct", cpuPct).
			Uint64("mem_used_mb", memUsedMB).
			Msg("resource monitor: over threshold, dispatch paused")
	}
}

// Admit blocks until resources are below threshold or ctx is cancelled.
// It waits for the first poll to complete before evaluating.
// Admit wakes immediately when the poller records a new sample, so it
// responds within one poll interval rather than busy-polling.
func (m *Monitor) Admit(ctx context.Context) error {
	// Wait for the first sample.
	select {
	case <-m.ready:
	case <-ctx.Done():
		return ctx.Err()
	}

	// context.AfterFunc fires a Broadcast when ctx is cancelled, which
	// unblocks cond.Wait below without a separate polling goroutine.
	stop := context.AfterFunc(ctx, func() { m.cond.Broadcast() })
	defer stop()

	m.cond.L.Lock()
	defer m.cond.L.Unlock()

	for {
		if !m.overLimit {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		m.cond.Wait()
	}
}

// Stats returns the most recently sampled CPU% and memory usage in MB.
func (m *Monitor) Stats() (cpuPct float64, memUsedMB uint64) {
	m.cond.L.Lock()
	defer m.cond.L.Unlock()
	return m.cpuPct, m.memUsedMB
}
