package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/task"
)

func replayRecords(statuses ...int) []output.ReplayRecord {
	records := make([]output.ReplayRecord, len(statuses))
	base := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	for i, status := range statuses {
		url := fmt.Sprintf("http://example.com/%d", i)
		records[i] = output.ReplayRecord{Line: i + 1, URL: url, Type: "http", Status: status, Envelope: output.ReplayEnvelope{Version: 1, RunID: "405d88de-3fb2-42d8-bfb7-f365907ed78c", Sequence: uint64(i + 1), StartedAt: base.Add(time.Duration(i) * time.Second), Replayable: true, Request: &output.ReplayRequest{URL: url, Type: "http", HTTP: &output.ReplayHTTP{Method: "GET", Body: "{{literal}}", TimeoutS: 15}}}}
	}
	return records
}

func TestPrepareReplay(t *testing.T) {
	records := replayRecords(200, 503, 200, 500)
	for rate, want := range map[float64]time.Duration{0.5: 4 * time.Second, 1: 2 * time.Second, 2: time.Second, math.MaxFloat64: 0} {
		items, err := prepareReplay(records, replayOptions{Rate: rate, Filter: "status=5xx"})
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 || items[0].Offset != 0 || items[1].Offset != want {
			t.Fatalf("rate %v: %+v", rate, items)
		}
	}
	for _, rate := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1), math.SmallestNonzeroFloat64} {
		if _, err := prepareReplay(records, replayOptions{Rate: rate}); err == nil {
			t.Fatalf("accepted rate %v", rate)
		}
	}
	for _, opts := range []replayOptions{{Rate: 1, Filter: "status=500"}, {Rate: 1, LoopDelay: -1}, {Rate: 1, Loop: true}} {
		input := records
		if opts.Loop {
			input = nil
		}
		if _, err := prepareReplay(input, opts); err == nil {
			t.Fatal("accepted invalid options")
		}
	}
	records = replayRecords(499, 500, 599, 600, 0, 429)
	records[0].Envelope.Replayable = false
	records[0].Envelope.Request = nil
	records[0].Envelope.Reason = "auth_configured"
	items, err := prepareReplay(records, replayOptions{Rate: 1, Filter: "status=5xx"})
	if err != nil || len(items) != 2 {
		t.Fatalf("filter: %v", err)
	}
	if _, err := prepareReplay(records, replayOptions{Rate: 1}); err == nil {
		t.Fatal("selected non-replayable record accepted")
	}
	if items, err := prepareReplay(nil, replayOptions{Rate: 1}); err != nil || len(items) != 0 {
		t.Fatal("empty non-loop failed")
	}
}

type replayDriverFunc func(context.Context, task.Task) task.Result

func (f replayDriverFunc) Execute(ctx context.Context, v task.Task) task.Result { return f(ctx, v) }

func TestRunReplayOverlapsAndKeepsSchedule(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		items, err := prepareReplay(replayRecords(200, 200, 200), replayOptions{Rate: 2})
		if err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		var mu sync.Mutex
		starts := map[string]time.Duration{}
		drv := replayDriverFunc(func(_ context.Context, v task.Task) task.Result {
			mu.Lock()
			starts[v.URL] = time.Since(start)
			mu.Unlock()
			time.Sleep(2 * time.Second)
			return task.Result{Task: v, StatusCode: 200}
		})
		var results []task.Result
		summary, err := runReplay(context.Background(), items, replayOptions{Rate: 2}, map[string]driver.Driver{"http": drv}, func(r task.Result) error { results = append(results, r); return nil })
		if err != nil || summary.Requests != 3 || summary.Failures != 0 {
			t.Fatalf("%+v %v", summary, err)
		}
		for i, item := range items {
			if starts[item.Record.URL] != time.Duration(i)*500*time.Millisecond {
				t.Fatalf("timing drift: %v", starts)
			}
		}
		slices.SortFunc(results, func(a, b task.Result) int {
			if a.Capture.Sequence < b.Capture.Sequence {
				return -1
			}
			return 1
		})
		for i, r := range results {
			if r.Capture.Sequence != uint64(i+1) || r.Capture.RunID == items[i].Record.Envelope.RunID || r.Task.Config.HTTP.Body != "{{literal}}" {
				t.Fatalf("capture/payload %+v", r)
			}
		}
	})
}

func TestRunReplayLoopsAndCancels(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				opts := replayOptions{Rate: 2, Loop: true, LoopDelay: delay}
				items, err := prepareReplay(replayRecords(200), opts)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				start := time.Now()
				var results []task.Result
				var starts []time.Duration
				drv := replayDriverFunc(func(_ context.Context, v task.Task) task.Result {
					starts = append(starts, time.Since(start))
					time.Sleep(2 * time.Second)
					return task.Result{Task: v, StatusCode: 503}
				})
				summary, err := runReplay(ctx, items, opts, map[string]driver.Driver{"http": drv}, func(r task.Result) error {
					results = append(results, r)
					if len(results) == 3 {
						cancel()
					}
					return nil
				})
				if err != nil || summary.Requests != 3 || summary.Failures != 3 {
					t.Fatalf("%+v %v", summary, err)
				}
				for i, r := range results {
					if r.Capture.Sequence != uint64(i+1) || r.Capture.RunID != results[0].Capture.RunID || starts[i] != time.Duration(i)*(2*time.Second+delay) {
						t.Fatalf("loop boundary: %v %+v", starts, r.Capture)
					}
				}
			})
		})
	}
}

func TestRunReplayCancellationAndOutputFailure(t *testing.T) {
	for _, phase := range []string{"before", "active", "wait", "loop", "write"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				opts := replayOptions{Rate: 1}
				if phase == "loop" {
					opts.Loop = true
					opts.LoopDelay = time.Hour
				}
				items, err := prepareReplay(replayRecords(200, 200), opts)
				if err != nil {
					t.Fatal(err)
				}
				if phase == "loop" {
					items = items[:1]
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if phase == "before" {
					cancel()
				} else if phase != "write" {
					go func() { time.Sleep(500 * time.Millisecond); cancel() }()
				}
				calls := 0
				finished := 0
				sentinel := errors.New("write failed")
				drv := replayDriverFunc(func(ctx context.Context, v task.Task) task.Result {
					calls++
					if phase == "active" {
						<-ctx.Done()
					}
					finished++
					return task.Result{Task: v, StatusCode: 200}
				})
				summary, err := runReplay(ctx, items, opts, map[string]driver.Driver{"http": drv}, func(r task.Result) error {
					if phase == "write" {
						cancel()
						return sentinel
					}
					return nil
				})
				want := 1
				if phase == "before" {
					want = 0
				}
				if calls != want || finished != calls || summary.Requests != uint64(calls) {
					t.Fatalf("calls=%d finished=%d summary=%+v", calls, finished, summary)
				}
				if phase == "write" {
					if !errors.Is(err, sentinel) {
						t.Fatal(err)
					}
				} else if err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

func TestRunReplayLosslessOutputAndMissingDriver(t *testing.T) {
	records := make([]int, 600)
	for i := range records {
		records[i] = 200
	}
	rs := replayRecords(records...)
	for i := range rs {
		rs[i].Envelope.StartedAt = rs[0].Envelope.StartedAt
	}
	items, err := prepareReplay(rs, replayOptions{Rate: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runReplay(context.Background(), items, replayOptions{Rate: 1}, nil, nil); err == nil {
		t.Fatal("missing driver accepted")
	}
	count := 0
	drv := replayDriverFunc(func(_ context.Context, v task.Task) task.Result { return task.Result{Task: v, StatusCode: 200} })
	summary, err := runReplay(context.Background(), items, replayOptions{Rate: 1}, map[string]driver.Driver{"http": drv}, func(r task.Result) error { count++; return nil })
	if err != nil || summary.Requests != 600 || count != 600 {
		t.Fatalf("lost output: %+v count=%d err=%v", summary, count, err)
	}
}

func TestReplayFailed(t *testing.T) {
	for _, tc := range []struct {
		status int
		err    error
		want   bool
	}{{200, nil, false}, {101, nil, false}, {500, nil, true}, {503, nil, true}, {404, nil, true}, {200, context.Canceled, true}, {0, errors.New("network"), true}} {
		if got := replayFailed(task.Result{StatusCode: tc.status, Error: tc.err}); got != tc.want {
			t.Fatal(tc)
		}
	}
}

func TestRunReplaySlowWriterDoesNotPaceDispatch(t *testing.T) {
	items, err := prepareReplay(replayRecords(200, 200, 200), replayOptions{Rate: 1000})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	drv := replayDriverFunc(func(_ context.Context, v task.Task) task.Result {
		entered <- struct{}{}
		return task.Result{Task: v, StatusCode: 200}
	})
	done := make(chan error, 1)
	go func() {
		_, err := runReplay(ctx, items, replayOptions{Rate: 1000}, map[string]driver.Driver{"http": drv}, func(task.Result) error {
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		done <- err
	}()
	for range 3 {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("writer blocked subsequent dispatch")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestRunReplayMultiworkerCancellationDrainsBeforeClose(t *testing.T) {
	records := replayRecords(200, 200, 200, 200)
	for i := range records {
		records[i].Envelope.StartedAt = records[0].Envelope.StartedAt
	}
	items, err := prepareReplay(records, replayOptions{Rate: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan struct{}, len(items))
	releaseWrite := make(chan struct{})
	writing := make(chan struct{}, len(items))
	var results []task.Result
	drv := replayDriverFunc(func(ctx context.Context, v task.Task) task.Result {
		started <- struct{}{}
		<-ctx.Done()
		return task.Result{Task: v, Error: ctx.Err()}
	})
	done := make(chan error, 1)
	go func() {
		summary, err := runReplay(ctx, items, replayOptions{Rate: 1}, map[string]driver.Driver{"http": drv}, func(r task.Result) error {
			writing <- struct{}{}
			<-releaseWrite
			results = append(results, r)
			return nil
		})
		if summary.Requests != 4 || summary.Failures != 4 {
			err = fmt.Errorf("bad summary: %+v", summary)
		}
		done <- err
	}()
	for range items {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("workers not started")
		}
	}
	cancel()
	select {
	case <-writing:
	case <-time.After(3 * time.Second):
		t.Fatal("no cancellation output")
	}
	select {
	case <-done:
		t.Fatal("returned before final write could finish")
	default:
	}
	close(releaseWrite)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatal("lost cancellation results")
	}
	// The CLI's finalization follows runReplay; by this point every write completed.
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, r := range results {
		if err := output.EncodeJSONL(enc, r); err != nil {
			t.Fatal(err)
		}
	}
	parsed, err := output.ReadReplay(&b)
	if err != nil || len(parsed) != 4 {
		t.Fatalf("cancellation output not replayable: %v", err)
	}
}
