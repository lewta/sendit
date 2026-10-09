package task

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Capture identifies a dispatch independently of completion/serialization order.
type Capture struct {
	RunID     string
	Sequence  uint64
	StartedAt time.Time
}

// CaptureClock anchors wall-clock timestamps to monotonic elapsed time.
type CaptureClock struct {
	mu       sync.Mutex
	runID    string
	anchor   time.Time
	sequence uint64
	elapsed  func() time.Duration
}

func NewCaptureClock() *CaptureClock {
	base := time.Now()
	return &CaptureClock{runID: randomUUID(), anchor: base.UTC(), elapsed: func() time.Duration { return time.Since(base) }}
}

func (c *CaptureClock) Next() (Capture, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sequence == math.MaxUint64 {
		return Capture{}, fmt.Errorf("capture sequence exhausted")
	}
	c.sequence++
	return Capture{RunID: c.runID, Sequence: c.sequence, StartedAt: c.anchor.Add(c.elapsed())}, nil
}
