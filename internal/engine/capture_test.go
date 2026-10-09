package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/output"
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
	stamp, err := eng.capture.Next()
	if err != nil || stamp.Sequence != 1 {
		t.Fatal("canceled admission consumed capture sequence")
	}
}

func TestDispatchCaptureAfterAdmission(t *testing.T) {
	for _, gate := range []string{"backoff", "rate-limit"} {
		t.Run(gate, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				target := config.TargetConfig{URL: "https://example.com", Type: "http", Weight: 1}
				cfg := baseCfg([]config.TargetConfig{target})
				cfg.RateLimits.DefaultRPS = 1
				eng, err := New(cfg, metrics.Noop())
				if err != nil {
					t.Fatal(err)
				}
				expected := time.Second
				if gate == "rate-limit" {
					if err := eng.rl.Load().Wait(context.Background(), "example.com"); err != nil {
						t.Fatal(err)
					}
				} else {
					expected = eng.backoff.Load().RecordError("example.com")
				}
				before := time.Now()
				eng.drivers["http"] = captureDriverFunc(func(_ context.Context, v task.Task) task.Result { return task.Result{Task: v, StatusCode: 200} })
				var result task.Result
				eng.SetObserver(func(r task.Result) { result = r })
				if err := eng.pool.Acquire(context.Background(), "http"); err != nil {
					t.Fatal(err)
				}
				eng.dispatch(context.Background(), task.Task{URL: target.URL, Type: "http", Config: target})
				if result.Capture.StartedAt.Sub(before) != expected || result.Capture.Sequence != 1 {
					t.Fatalf("capture before admission: %+v wanted elapsed %v", result.Capture, expected)
				}
			})
		})
	}
}

func TestReloadPreservesLateExpandedSnapshot(t *testing.T) {
	for _, typ := range []string{"http", "websocket"} {
		t.Run(typ, func(t *testing.T) {
			scheme := "https://"
			if typ == "websocket" {
				scheme = "wss://"
			}
			old := config.TargetConfig{URL: scheme + "old.example/{{seq}}", Type: typ, Weight: 1, Vars: map[string][]string{"name": {"{{literal}}"}}, HTTP: config.HTTPConfig{Method: "POST", Body: `{"id":"{{uuid}}","name":"{{name}}","seq":{{seq}}}`}, WebSocket: config.WebSocketConfig{DurationS: 1, SendMessages: []string{"{{uuid}}/{{seq}}/{{name}}"}}}
			eng, err := New(baseCfg([]config.TargetConfig{old}), metrics.Noop())
			if err != nil {
				t.Fatal(err)
			}
			original := eng.selector.Load().Pick()
			entered, release := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			eng.drivers[typ] = captureDriverFunc(func(ctx context.Context, v task.Task) task.Result {
				if v.URL == original.URL {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
				return task.Result{Task: v, StatusCode: 200}
			})
			records := make(chan task.Result, 2)
			eng.SetObserver(func(r task.Result) { records <- r })
			if err := eng.pool.Acquire(ctx, typ); err != nil {
				t.Fatal(err)
			}
			go eng.dispatch(ctx, original)
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("old request not started")
			}
			updated := old
			updated.URL = scheme + "new.example/{{seq}}"
			updated.Vars = map[string][]string{"name": {"new"}}
			if err := eng.Reload(baseCfg([]config.TargetConfig{updated})); err != nil {
				t.Fatal(err)
			}
			next := eng.selector.Load().Pick()
			if err := eng.pool.Acquire(ctx, typ); err != nil {
				t.Fatal(err)
			}
			eng.dispatch(ctx, next)
			newer := <-records
			close(release)
			older := <-records
			eng.pool.Wait()
			var b bytes.Buffer
			enc := json.NewEncoder(&b)
			for _, r := range []task.Result{newer, older} {
				if err := output.EncodeJSONL(enc, r); err != nil {
					t.Fatal(err)
				}
			}
			decoded, err := output.ReadReplay(&b)
			if err != nil {
				t.Fatal(err)
			}
			if len(decoded) != 2 || decoded[0].Envelope.RunID != decoded[1].Envelope.RunID || decoded[0].Envelope.Sequence != 1 || decoded[1].Envelope.Sequence != 2 {
				t.Fatal("reload changed capture identity")
			}
			got := decoded[0].Envelope.Request.Task()
			if got.URL != original.URL {
				t.Fatal("expanded URL changed")
			}
			if typ == "http" && got.Config.HTTP.Body != original.Config.HTTP.Body {
				t.Fatal("expanded body changed")
			}
			if typ == "websocket" && got.Config.WebSocket.SendMessages[0] != original.Config.WebSocket.SendMessages[0] {
				t.Fatal("expanded messages changed")
			}
			if old.HTTP.Body != `{"id":"{{uuid}}","name":"{{name}}","seq":{{seq}}}` || old.WebSocket.SendMessages[0] != "{{uuid}}/{{seq}}/{{name}}" {
				t.Fatal("source mutated")
			}
		})
	}
}
