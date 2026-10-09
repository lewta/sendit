package task

import (
	"math"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestCaptureClockElapsedAndExhaustion(t *testing.T) {
	c := NewCaptureClock()
	elapsed := time.Second
	c.elapsed = func() time.Duration { return elapsed }
	first, err := c.Next()
	if err != nil {
		t.Fatal(err)
	}
	elapsed += 250 * time.Millisecond
	second, err := c.Next()
	if err != nil {
		t.Fatal(err)
	}
	if first.RunID == "" || first.RunID != second.RunID || first.Sequence != 1 || second.Sequence != 2 || second.StartedAt.Sub(first.StartedAt) != 250*time.Millisecond || first.StartedAt.Location() != time.UTC {
		t.Fatalf("captures: %+v %+v", first, second)
	}
	if !first.StartedAt.Equal(c.anchor.Add(time.Second)) {
		t.Fatal("not anchored to monotonic elapsed time")
	}
	c.sequence = math.MaxUint64
	if _, err := c.Next(); err == nil {
		t.Fatal("counter wrapped")
	}
}

func TestCaptureClockConcurrent(t *testing.T) {
	c := NewCaptureClock()
	captures := make([]Capture, 100)
	var wg sync.WaitGroup
	for i := range captures {
		wg.Go(func() {
			var err error
			captures[i], err = c.Next()
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	slices.SortFunc(captures, func(a, b Capture) int {
		if a.Sequence < b.Sequence {
			return -1
		}
		if a.Sequence > b.Sequence {
			return 1
		}
		return 0
	})
	for i, v := range captures {
		if v.Sequence != uint64(i+1) || v.RunID != captures[0].RunID {
			t.Fatalf("bad capture %+v", v)
		}
		if i > 0 && v.StartedAt.Before(captures[i-1].StartedAt) {
			t.Fatal("time went backwards")
		}
	}
}

func BenchmarkCaptureClockNext(b *testing.B) {
	b.Run("sequential", func(b *testing.B) {
		c := NewCaptureClock()
		b.ReportAllocs()
		for b.Loop() {
			_, _ = c.Next()
		}
	})
	b.Run("parallel", func(b *testing.B) {
		c := NewCaptureClock()
		b.ReportAllocs()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = c.Next()
			}
		})
	})
}
