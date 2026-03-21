package engine

import (
	"context"
	"io"
	"testing"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// noopDriver satisfies driver.Driver and returns immediately with a 200.
type noopDriver struct{}

func (noopDriver) Execute(_ context.Context, t task.Task) task.Result {
	return task.Result{Task: t, StatusCode: 200}
}

// BenchmarkDispatch measures a full dispatch cycle (backoff check, rate-limit
// check, driver execution, metrics recording) using a no-op driver so no
// network I/O is involved.
func BenchmarkDispatch(b *testing.B) {
	// Silence the global zerolog logger so log lines don't pollute bench output.
	log.Logger = zerolog.New(io.Discard)

	cfg := &config.Config{
		Pacing: config.PacingConfig{
			Mode:              "rate_limited",
			RequestsPerMinute: 1e9,
		},
		Limits: config.LimitsConfig{
			MaxWorkers: 1, // sequential benchmark; pool of 1 is sufficient
		},
		RateLimits: config.RateLimitsConfig{
			DefaultRPS: 1e9, // effectively unlimited; Wait never blocks
		},
		Backoff: config.BackoffConfig{
			InitialMs:   100,
			MaxMs:       30000,
			Multiplier:  2.0,
			MaxAttempts: 5,
		},
		Targets: []config.TargetConfig{
			{URL: "http://example.com", Type: "http", Weight: 1},
		},
	}

	e, err := New(cfg, metrics.Noop())
	if err != nil {
		b.Fatalf("New: %v", err)
	}
	e.drivers["http"] = noopDriver{}

	t := task.Task{
		URL:    "http://example.com",
		Type:   "http",
		Config: cfg.Targets[0],
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Acquire a pool slot as the dispatch loop would before spawning dispatch.
		if err := e.pool.Acquire(ctx, "http"); err != nil {
			b.Fatal(err)
		}
		e.dispatch(ctx, t) // defers pool.Release internally
	}
}
