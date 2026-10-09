package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/task"
)

type captureDriverFunc func(context.Context, task.Task) task.Result

func (f captureDriverFunc) Execute(ctx context.Context, v task.Task) task.Result { return f(ctx, v) }

func TestDispatchCaptureAcrossReload(t *testing.T) {
	old := config.TargetConfig{URL: "https://old.example", Type: "http", Weight: 1, HTTP: config.HTTPConfig{Body: "old"}}
	eng, err := New(baseCfg([]config.TargetConfig{old}), metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	eng.drivers["http"] = captureDriverFunc(func(ctx context.Context, v task.Task) task.Result {
		if v.URL == old.URL {
			close(entered)
			<-release
		}
		return task.Result{Task: v, Error: errors.New("expected failure")}
	})
	results := make(chan task.Result, 2)
	eng.SetObserver(func(v task.Result) { results <- v })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := eng.pool.Acquire(ctx, "http"); err != nil {
		t.Fatal(err)
	}
	go eng.dispatch(ctx, task.Task{URL: old.URL, Type: old.Type, Config: old})
	<-entered
	updated := old
	updated.URL = "https://new.example"
	updated.HTTP.Body = "new"
	if err := eng.Reload(baseCfg([]config.TargetConfig{updated})); err != nil {
		t.Fatal(err)
	}
	if err := eng.pool.Acquire(ctx, "http"); err != nil {
		t.Fatal(err)
	}
	eng.dispatch(ctx, task.Task{URL: updated.URL, Type: updated.Type, Config: updated})
	second := <-results
	close(release)
	first := <-results
	eng.pool.Wait()
	if first.Capture.RunID == "" || first.Capture.RunID != second.Capture.RunID || first.Capture.Sequence != 1 || second.Capture.Sequence != 2 || second.Capture.StartedAt.Before(first.Capture.StartedAt) {
		t.Fatalf("capture lost: %+v %+v", first.Capture, second.Capture)
	}
	if first.Task.Config.HTTP.Body != "old" || second.Task.Config.HTTP.Body != "new" {
		t.Fatal("reload changed in-flight request")
	}
}

func TestDispatchCaptureCanceledAdmission(t *testing.T) {
	target := config.TargetConfig{URL: "https://example.com", Type: "http", Weight: 1}
	eng, err := New(baseCfg([]config.TargetConfig{target}), metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	// Consume the initial token; canceled dispatch cannot reach Execute.
	if err := eng.rl.Load().Wait(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	calls := 0
	eng.drivers["http"] = captureDriverFunc(func(_ context.Context, v task.Task) task.Result {
		calls++
		return task.Result{Task: v, StatusCode: 200}
	})
	if err := eng.pool.Acquire(context.Background(), "http"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	eng.dispatch(ctx, task.Task{URL: target.URL, Type: target.Type, Config: target})
	if calls != 0 {
		t.Fatal("dispatched after canceled admission")
	}
}
