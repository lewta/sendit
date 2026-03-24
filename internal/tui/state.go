package tui

import (
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lewta/sendit/internal/task"
)

const ringSize = 128

// State is the shared counter store written by engine dispatch goroutines and
// read by the TUI render tick. All methods are safe for concurrent use.
type State struct {
	total   atomic.Int64
	success atomic.Int64
	errors  atomic.Int64
	start   time.Time

	mu      sync.Mutex
	ring    [ringSize]int64 // nanoseconds, circular
	ringPos int
	ringN   int // entries filled, capped at ringSize
}

// NewState returns an initialised State with the clock started at now.
func NewState() *State {
	return &State{start: time.Now()}
}

// Record is the engine observer callback; safe for concurrent use from many
// dispatch goroutines.
func (s *State) Record(r task.Result) {
	s.total.Add(1)
	if r.Error != nil {
		s.errors.Add(1)
		return
	}
	s.success.Add(1)
	ns := r.Duration.Nanoseconds()
	s.mu.Lock()
	s.ring[s.ringPos] = ns
	s.ringPos = (s.ringPos + 1) % ringSize
	if s.ringN < ringSize {
		s.ringN++
	}
	s.mu.Unlock()
}

// Snapshot is a consistent point-in-time read of the State.
type Snapshot struct {
	Total     int64
	Success   int64
	Errors    int64
	Elapsed   time.Duration
	Latencies []time.Duration // last ≤128 successful durations, oldest first
}

// Snapshot returns a consistent read of the current state.
func (s *State) Snapshot() Snapshot {
	s.mu.Lock()
	n := s.ringN
	lats := make([]time.Duration, n)
	for i := range n {
		idx := (s.ringPos - n + i + ringSize) % ringSize
		lats[i] = time.Duration(s.ring[idx])
	}
	s.mu.Unlock()

	return Snapshot{
		Total:     s.total.Load(),
		Success:   s.success.Load(),
		Errors:    s.errors.Load(),
		Elapsed:   time.Since(s.start),
		Latencies: lats,
	}
}

// Avg returns the mean latency across Latencies, or 0 if empty.
func (sn Snapshot) Avg() time.Duration {
	if len(sn.Latencies) == 0 {
		return 0
	}
	var sum int64
	for _, l := range sn.Latencies {
		sum += int64(l)
	}
	return time.Duration(sum / int64(len(sn.Latencies)))
}

// P95 returns the 95th-percentile latency, or 0 if fewer than 2 samples.
func (sn Snapshot) P95() time.Duration {
	n := len(sn.Latencies)
	if n < 2 {
		return 0
	}
	sorted := make([]int64, n)
	for i, l := range sn.Latencies {
		sorted[i] = int64(l)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(float64(n)*0.95)) - 1
	if idx < 0 {
		idx = 0
	}
	return time.Duration(sorted[idx])
}
