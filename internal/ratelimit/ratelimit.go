package ratelimit

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
)

// Registry maintains per-domain token bucket rate limiters.
type Registry struct {
	mu         sync.Mutex
	limiters   map[string]*rate.Limiter
	defaultRPS float64
	perDomain  map[string]float64
}

// NewRegistry creates a Registry with the given defaults and per-domain overrides.
func NewRegistry(defaultRPS float64, perDomain map[string]float64) *Registry {
	return &Registry{
		limiters:   make(map[string]*rate.Limiter),
		defaultRPS: defaultRPS,
		perDomain:  perDomain,
	}
}

// Wait blocks until the rate limiter for the given domain allows the request,
// or until ctx is cancelled.
func (r *Registry) Wait(ctx context.Context, domain string) error {
	lim := r.getLimiter(domain)
	return lim.Wait(ctx)
}

func (r *Registry) getLimiter(domain string) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()

	if lim, ok := r.limiters[domain]; ok {
		return lim
	}

	rps := r.defaultRPS
	if override, ok := r.perDomain[domain]; ok {
		rps = override
	}

	lim := rate.NewLimiter(rate.Limit(rps), 1)
	r.limiters[domain] = lim
	return lim
}
