package engine

import (
	"context"
	"fmt"
	"net/url"
	"sync/atomic"

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
	cfg       atomic.Pointer[config.Config]
	pool      *Pool
	scheduler *Scheduler
	selector  atomic.Pointer[task.Selector]
	rl        atomic.Pointer[ratelimit.Registry]
	backoff   atomic.Pointer[ratelimit.BackoffRegistry]
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
		pool:      NewPool(cfg.Limits.MaxWorkers, cfg.Limits.MaxBrowserWorkers),
		scheduler: NewScheduler(cfg.Pacing),
		monitor:   resource.New(cfg.Limits.CPUThresholdPct, cfg.Limits.MemoryThresholdMB),
		metrics:   m,
		drivers: map[string]driver.Driver{
			"http":      driver.NewHTTPDriver(),
			"browser":   driver.NewBrowserDriver(),
			"dns":       driver.NewDNSDriver(),
			"websocket": driver.NewWebSocketDriver(),
		},
	}

	e.cfg.Store(cfg)
	e.selector.Store(sel)
	e.rl.Store(ratelimit.NewRegistry(cfg.RateLimits.DefaultRPS, perDomain))
	e.backoff.Store(ratelimit.NewBackoffRegistry(
		cfg.Backoff.InitialMs,
		cfg.Backoff.MaxMs,
		cfg.Backoff.Multiplier,
		cfg.Backoff.MaxAttempts,
	))

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

	cfg := e.cfg.Load()
	log.Info().
		Str("mode", cfg.Pacing.Mode).
		Int("max_workers", cfg.Limits.MaxWorkers).
		Msg("engine started")

	for {
		// --- Pacing delay ---
		if err := e.scheduler.Wait(ctx); err != nil {
			break
		}

		t := e.selector.Load().Pick()

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

	// Snapshot the registries once so that a concurrent Reload cannot
	// swap them mid-dispatch.
	rl := e.rl.Load()
	bo := e.backoff.Load()

	// --- Backoff wait ---
	if err := bo.Wait(ctx, host); err != nil {
		return // context cancelled
	}

	// --- Per-domain rate limit ---
	if err := rl.Wait(ctx, host); err != nil {
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
			if bo.Attempts(host) < bo.MaxAttempts() {
				delay := bo.RecordError(host)
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
		if bo.Attempts(host) < bo.MaxAttempts() {
			delay := bo.RecordError(host)
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
		bo.RecordSuccess(host)
		log.Info().
			Str("url", t.URL).
			Str("type", t.Type).
			Int("status", result.StatusCode).
			Dur("duration", result.Duration).
			Int64("bytes", result.BytesRead).
			Msg("task complete")
	}
}

// Reload atomically applies a new configuration to the running engine.
// Targets, rate limits, backoff, and pacing are updated in-place.
// Changes to pacing mode, resource limits, or scheduled windows require a restart.
func (e *Engine) Reload(newCfg *config.Config) error {
	old := e.cfg.Load()

	// Log target diff.
	logTargetsDiff(old.Targets, newCfg.Targets)

	// Swap Selector.
	sel, err := task.NewSelector(newCfg.Targets)
	if err != nil {
		return fmt.Errorf("hot-reload: building selector: %w", err)
	}
	e.selector.Store(sel)

	// Swap rate-limit registry.
	perDomain := make(map[string]float64, len(newCfg.RateLimits.PerDomain))
	for _, d := range newCfg.RateLimits.PerDomain {
		perDomain[d.Domain] = d.RPS
	}
	e.rl.Store(ratelimit.NewRegistry(newCfg.RateLimits.DefaultRPS, perDomain))

	// Swap backoff registry.
	e.backoff.Store(ratelimit.NewBackoffRegistry(
		newCfg.Backoff.InitialMs, newCfg.Backoff.MaxMs,
		newCfg.Backoff.Multiplier, newCfg.Backoff.MaxAttempts,
	))

	// Update pacing (or warn if mode change requires restart).
	if old.Pacing.Mode != newCfg.Pacing.Mode {
		log.Warn().Str("old", old.Pacing.Mode).Str("new", newCfg.Pacing.Mode).
			Msg("hot-reload: pacing mode change requires restart")
	} else {
		e.scheduler.UpdatePacing(newCfg.Pacing)
	}

	// Warn if resource limits changed.
	if old.Limits != newCfg.Limits {
		log.Warn().Msg("hot-reload: resource limit changes (workers, cpu, memory) require restart")
	}

	e.cfg.Store(newCfg)
	log.Info().Msg("hot-reload: config reloaded")
	return nil
}

func logTargetsDiff(old, next []config.TargetConfig) {
	oldSet := make(map[string]bool, len(old))
	for _, t := range old {
		oldSet[t.URL] = true
	}
	for _, t := range next {
		if !oldSet[t.URL] {
			log.Info().Str("url", t.URL).Msg("hot-reload: target added")
		}
	}
	newSet := make(map[string]bool, len(next))
	for _, t := range next {
		newSet[t.URL] = true
	}
	for _, t := range old {
		if !newSet[t.URL] {
			log.Info().Str("url", t.URL).Msg("hot-reload: target removed")
		}
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
