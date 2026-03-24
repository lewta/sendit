package tui

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/task"
)

func result(dur time.Duration, err error) task.Result {
	return task.Result{Duration: dur, Error: err}
}

func TestState_Record_counters(t *testing.T) {
	s := NewState()

	s.Record(result(10*time.Millisecond, nil))
	s.Record(result(20*time.Millisecond, nil))
	s.Record(result(0, errors.New("fail")))

	sn := s.Snapshot()
	if sn.Total != 3 {
		t.Errorf("Total: want 3, got %d", sn.Total)
	}
	if sn.Success != 2 {
		t.Errorf("Success: want 2, got %d", sn.Success)
	}
	if sn.Errors != 1 {
		t.Errorf("Errors: want 1, got %d", sn.Errors)
	}
	if len(sn.Latencies) != 2 {
		t.Errorf("Latencies len: want 2, got %d", len(sn.Latencies))
	}
}

func TestState_Record_ringOverwrite(t *testing.T) {
	s := NewState()
	// Fill more than ringSize entries.
	for i := range ringSize + 10 {
		s.Record(result(time.Duration(i)*time.Millisecond, nil))
	}
	sn := s.Snapshot()
	if len(sn.Latencies) != ringSize {
		t.Errorf("Latencies len: want %d, got %d", ringSize, len(sn.Latencies))
	}
}

func TestState_Record_concurrent(t *testing.T) {
	s := NewState()
	var wg sync.WaitGroup
	const n = 500
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Record(result(time.Millisecond, nil))
		}()
	}
	wg.Wait()
	sn := s.Snapshot()
	if sn.Total != n {
		t.Errorf("Total: want %d, got %d", n, sn.Total)
	}
}

func TestSnapshot_Avg(t *testing.T) {
	sn := Snapshot{Latencies: []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}}
	if got := sn.Avg(); got != 20*time.Millisecond {
		t.Errorf("Avg: want 20ms, got %v", got)
	}
}

func TestSnapshot_Avg_empty(t *testing.T) {
	var sn Snapshot
	if got := sn.Avg(); got != 0 {
		t.Errorf("Avg on empty: want 0, got %v", got)
	}
}

func TestSnapshot_P95(t *testing.T) {
	lats := make([]time.Duration, 100)
	for i := range lats {
		lats[i] = time.Duration(i+1) * time.Millisecond // 1ms…100ms
	}
	sn := Snapshot{Latencies: lats}
	p95 := sn.P95()
	// 95th percentile of 1..100 ms should be 95ms.
	if p95 != 95*time.Millisecond {
		t.Errorf("P95: want 95ms, got %v", p95)
	}
}

func TestSnapshot_P95_single(t *testing.T) {
	sn := Snapshot{Latencies: []time.Duration{5 * time.Millisecond}}
	if got := sn.P95(); got != 0 {
		t.Errorf("P95 with 1 sample: want 0, got %v", got)
	}
}

func TestSparkline_length(t *testing.T) {
	lats := make([]time.Duration, 20)
	for i := range lats {
		lats[i] = time.Duration(i+1) * time.Millisecond
	}
	spark := sparkline(lats, 40)
	if len([]rune(spark)) != 20 {
		t.Errorf("sparkline rune len: want 20, got %d", len([]rune(spark)))
	}
}

func TestSparkline_truncatesAtMaxWidth(t *testing.T) {
	lats := make([]time.Duration, 50)
	for i := range lats {
		lats[i] = time.Duration(i+1) * time.Millisecond
	}
	spark := sparkline(lats, 10)
	if len([]rune(spark)) != 10 {
		t.Errorf("sparkline rune len: want 10, got %d", len([]rune(spark)))
	}
}

func TestFormatInt(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
	}
	for _, c := range cases {
		got := formatInt(c.n)
		if got != c.want {
			t.Errorf("formatInt(%d): want %q, got %q", c.n, c.want, got)
		}
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m 30s"},
		{3700 * time.Second, "1h 1m 40s"},
	}
	for _, c := range cases {
		got := formatElapsed(c.d)
		if got != c.want {
			t.Errorf("formatElapsed(%v): want %q, got %q", c.d, c.want, got)
		}
	}
}
