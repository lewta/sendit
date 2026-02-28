package engine

import (
	"context"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"
)

// Scheduler controls inter-request timing based on the configured pacing mode.
type Scheduler struct {
	cfg config.PacingConfig

	// minDelayMs / maxDelayMs are used in human mode and are reloadable.
	minDelayMs atomic.Int64
	maxDelayMs atomic.Int64

	// activeRPM is used in rate_limited / scheduled mode.
	activeRPM atomic.Value // stores float64

	// inWindow indicates whether a cron window is currently active.
	inWindow atomic.Bool

	// limiter is only set in rate_limited / scheduled mode; nil otherwise.
	limiter atomic.Pointer[rate.Limiter]
}

// NewScheduler creates a Scheduler from the pacing config.
func NewScheduler(cfg config.PacingConfig) *Scheduler {
	s := &Scheduler{cfg: cfg}

	s.minDelayMs.Store(int64(cfg.MinDelayMs))
	s.maxDelayMs.Store(int64(cfg.MaxDelayMs))

	switch cfg.Mode {
	case "rate_limited":
		rpm := cfg.RequestsPerMinute
		s.activeRPM.Store(rpm)
		s.limiter.Store(rate.NewLimiter(rate.Limit(rpm/60.0), 1))
	case "scheduled":
		s.inWindow.Store(false)
	default: // human
	}

	return s
}

// Start launches background goroutines needed by the scheduler (cron for scheduled mode).
func (s *Scheduler) Start(ctx context.Context) {
	if s.cfg.Mode != "scheduled" {
		return
	}

	// A single AfterFunc timer replaces the per-window goroutine that was
	// previously spawned on every cron firing. This prevents goroutine
	// accumulation when the same window fires many times over a long run.
	var (
		closeMu    sync.Mutex
		closeTimer *time.Timer
	)

	c := cron.New()

	for _, entry := range s.cfg.Schedule {
		e := entry // capture
		_, err := c.AddFunc(e.Cron, func() {
			rpm := e.RequestsPerMinute
			log.Info().Float64("rpm", rpm).Msg("scheduled window opening")
			lim := rate.NewLimiter(rate.Limit(rpm/60.0), 1)
			s.limiter.Store(lim)
			s.activeRPM.Store(rpm)
			s.inWindow.Store(true)

			// Reset the single close timer so only one window-close is pending.
			duration := time.Duration(e.DurationMinutes) * time.Minute
			closeMu.Lock()
			if closeTimer != nil {
				closeTimer.Stop()
			}
			closeTimer = time.AfterFunc(duration, func() {
				s.inWindow.Store(false)
				log.Info().Msg("scheduled window closed")
			})
			closeMu.Unlock()
		})
		if err != nil {
			log.Error().Err(err).Str("cron", e.Cron).Msg("invalid cron expression")
		}
	}

	c.Start()
	go func() {
		<-ctx.Done()
		c.Stop()
		// Stop any pending window-close timer so it doesn't fire after shutdown.
		closeMu.Lock()
		if closeTimer != nil {
			closeTimer.Stop()
		}
		closeMu.Unlock()
	}()
}

// Wait implements the pacing delay for the current mode.
// It blocks until it is appropriate to dispatch the next request.
func (s *Scheduler) Wait(ctx context.Context) error {
	switch s.cfg.Mode {
	case "human":
		return s.humanWait(ctx)
	case "rate_limited":
		return s.rateLimitedWait(ctx)
	case "scheduled":
		return s.scheduledWait(ctx)
	default:
		return s.humanWait(ctx)
	}
}

func (s *Scheduler) humanWait(ctx context.Context) error {
	minMs := s.minDelayMs.Load()
	maxMs := s.maxDelayMs.Load()

	var delayMs int64
	if maxMs <= minMs {
		delayMs = minMs
	} else {
		delayMs = rand.Int63n(maxMs-minMs+1) + minMs //nolint:gosec
	}

	return sleepCtx(ctx, time.Duration(delayMs)*time.Millisecond)
}

// UpdatePacing updates reloadable pacing parameters at runtime.
// Mode changes are not supported — callers should warn and skip.
func (s *Scheduler) UpdatePacing(cfg config.PacingConfig) {
	switch s.cfg.Mode {
	case "human":
		s.minDelayMs.Store(int64(cfg.MinDelayMs))
		s.maxDelayMs.Store(int64(cfg.MaxDelayMs))
		log.Info().Int("min_ms", cfg.MinDelayMs).Int("max_ms", cfg.MaxDelayMs).
			Msg("hot-reload: human pacing updated")
	case "rate_limited":
		rpm := cfg.RequestsPerMinute
		s.limiter.Store(rate.NewLimiter(rate.Limit(rpm/60.0), 1))
		s.activeRPM.Store(rpm)
		log.Info().Float64("rpm", rpm).Msg("hot-reload: rate_limited pacing updated")
	case "scheduled":
		log.Warn().Msg("hot-reload: scheduled pacing changes require restart")
	}
}

func (s *Scheduler) rateLimitedWait(ctx context.Context) error {
	lim := s.limiter.Load()
	if err := lim.Wait(ctx); err != nil {
		return err
	}
	// Small additional jitter to avoid thundering herd.
	jitterMs := rand.Intn(200) //nolint:gosec
	return sleepCtx(ctx, time.Duration(jitterMs)*time.Millisecond)
}

func (s *Scheduler) scheduledWait(ctx context.Context) error {
	// If outside a cron window, block until context cancels.
	if !s.inWindow.Load() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			// Re-check window state.
			return nil
		}
	}
	return s.rateLimitedWait(ctx)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
