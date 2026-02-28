package engine

import (
	"context"
	"fmt"
	"net/url"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/ratelimit"
	"github.com/lewta/sendit/internal/resource"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog/log"
)

// Engine orchestrates the dispatch loop.
type Engine struct {
	cfg       *config.Config
	pool      *Pool
	scheduler *Scheduler
	selector  *task.Selector
	rl        *ratelimit.Registry
	backoff   *ratelimit.BackoffRegistry
	monitor   *resource.Monitor
	metrics   *metrics.Metrics
	writer    *output.Writer
	drivers   map[string]driver.Driver
}

// New creates an Engine wired with all dependencies.
func New(cfg *config.Config, m *metrics.Metrics) (*Engine, error) {
	sel, err := task.NewSelector(cfg.Targets)
	if err != nil {
		return nil, err
	}

	perDomain := make(map[string]float64)
	for _, d := range cfg.RateLimits.PerDomain {
		perDomain[d.Domain] = d.RPS
	}

	e := &Engine{
		cfg:       cfg,
		pool:      NewPool(cfg.Limits.MaxWorkers, cfg.Limits.MaxBrowserWorkers),
		scheduler: NewScheduler(cfg.Pacing),
		selector:  sel,
		rl:        ratelimit.NewRegistry(cfg.RateLimits.DefaultRPS, perDomain),
		backoff: ratelimit.NewBackoffRegistry(
			cfg.Backoff.InitialMs,
			cfg.Backoff.MaxMs,
			cfg.Backoff.Multiplier,
			cfg.Backoff.MaxAttempts,
		),
		monitor: resource.New(cfg.Limits.CPUThresholdPct, cfg.Limits.MemoryThresholdMB),
		metrics: m,
		drivers: map[string]driver.Driver{
			"http":      driver.NewHTTPDriver(),
			"browser":   driver.NewBrowserDriver(),
			"dns":       driver.NewDNSDriver(),
			"websocket": driver.NewWebSocketDriver(),
		},
	}

	if cfg.Output.Enabled {
		w, err := output.New(cfg.Output)
		if err != nil {
			return nil, fmt.Errorf("creating output writer: %w", err)
		}
		e.writer = w
	}

	return e, nil
}

// Run starts the engine and blocks until ctx is cancelled.
// After ctx is cancelled it waits for all in-flight tasks to complete.
func (e *Engine) Run(ctx context.Context) {
	if e.writer != nil {
		defer e.writer.Close()
	}

	e.monitor.Start(ctx)
	e.scheduler.Start(ctx)

	log.Info().
		Str("mode", e.cfg.Pacing.Mode).
		Int("max_workers", e.cfg.Limits.MaxWorkers).
		Msg("engine started")

	for {
		// --- Pacing delay ---
		if err := e.scheduler.Wait(ctx); err != nil {
			break
		}

		t := e.selector.Pick()

		// --- Resource gate ---
		if err := e.monitor.Admit(ctx); err != nil {
			break
		}

		// --- Worker slot ---
		// Backoff and rate-limit waits happen inside the goroutine so that a
		// slow or rate-limited domain does not stall the dispatch loop and
		// starve all other domains.
		if err := e.pool.Acquire(ctx, t.Type); err != nil {
			break
		}

		go e.dispatch(ctx, t)
	}

	log.Info().Msg("engine shutting down, waiting for in-flight tasks")
	e.pool.Wait()
	log.Info().Msg("engine stopped")
}

func (e *Engine) dispatch(ctx context.Context, t task.Task) {
	defer e.pool.Release(t.Type)

	drv, ok := e.drivers[t.Type]
	if !ok {
		log.Error().Str("type", t.Type).Msg("unknown driver type")
		return
	}

	host := hostname(t.URL)

	// --- Backoff wait ---
	if err := e.backoff.Wait(ctx, host); err != nil {
		return // context cancelled
	}

	// --- Per-domain rate limit ---
	if err := e.rl.Wait(ctx, host); err != nil {
		return // context cancelled
	}

	log.Debug().
		Str("url", t.URL).
		Str("type", t.Type).
		Msg("dispatching task")

	result := drv.Execute(ctx, t)

	e.metrics.Record(result)

	if e.writer != nil {
		e.writer.Send(result)
	}

	if result.Error != nil {
		class := ratelimit.ClassifyError(result.Error)
		if class == ratelimit.ErrorClassFatal {
			return
		}
		if class == ratelimit.ErrorClassTransient {
			if e.backoff.Attempts(host) < e.backoff.MaxAttempts() {
				delay := e.backoff.RecordError(host)
				log.Warn().
					Str("host", host).
					Dur("backoff", delay).
					Err(result.Error).
					Msg("transient error, backing off")
			} else {
				log.Error().
					Str("host", host).
					Err(result.Error).
					Msg("max backoff attempts reached, skipping domain temporarily")
			}
		} else {
			log.Error().
				Str("url", t.URL).
				Err(result.Error).
				Msg("permanent error, skipping")
		}
		return
	}

	class := ratelimit.ClassifyStatusCode(result.StatusCode)
	switch class {
	case ratelimit.ErrorClassTransient:
		if e.backoff.Attempts(host) < e.backoff.MaxAttempts() {
			delay := e.backoff.RecordError(host)
			log.Warn().
				Str("host", host).
				Int("status", result.StatusCode).
				Dur("backoff", delay).
				Msg("transient HTTP error, backing off")
		}
	case ratelimit.ErrorClassPermanent:
		log.Error().
			Str("url", t.URL).
			Int("status", result.StatusCode).
			Msg("permanent HTTP error, skipping")
	case ratelimit.ErrorClassNone:
		e.backoff.RecordSuccess(host)
		log.Info().
			Str("url", t.URL).
			Str("type", t.Type).
			Int("status", result.StatusCode).
			Dur("duration", result.Duration).
			Int64("bytes", result.BytesRead).
			Msg("task complete")
	}
}

func hostname(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	host := u.Hostname()
	if host == "" {
		return rawURL
	}
	return host
}
