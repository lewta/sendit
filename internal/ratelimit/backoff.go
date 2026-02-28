package ratelimit

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

// ErrorClass categorises an HTTP error for backoff decisions.
type ErrorClass int

const (
	ErrorClassNone      ErrorClass = iota // success
	ErrorClassTransient                   // 429, 502, 503, 504 — back off and retry
	ErrorClassPermanent                   // 400, 403, 404 — log and skip
	ErrorClassFatal                       // context cancelled — drop silently
)

// ClassifyStatusCode returns the ErrorClass for the given status code.
// It is used for both HTTP status codes and DNS RCODEs (mapped to HTTP-like
// codes by the DNS driver before being passed here).
func ClassifyStatusCode(code int) ErrorClass {
	switch code {
	case 429, 502, 503, 504:
		return ErrorClassTransient
	case 400, 403, 404:
		return ErrorClassPermanent
	case 0:
		// network error / context cancelled treated as transient at call site
		return ErrorClassTransient
	default:
		if code >= 200 && code < 300 {
			return ErrorClassNone
		}
		if code >= 500 {
			return ErrorClassTransient
		}
		return ErrorClassPermanent
	}
}

// ClassifyError checks a Go error for context cancellation.
func ClassifyError(err error) ErrorClass {
	if err == nil {
		return ErrorClassNone
	}
	if err == context.Canceled || err == context.DeadlineExceeded {
		return ErrorClassFatal
	}
	return ErrorClassTransient
}

// domainBackoff tracks backoff state for a single domain.
type domainBackoff struct {
	mu          sync.Mutex
	attempts    int
	nextAllowed time.Time
}

// BackoffRegistry tracks backoff state per domain using decorrelated jitter.
type BackoffRegistry struct {
	mu         sync.Mutex
	domains    map[string]*domainBackoff
	initialMs  int
	maxMs      int
	multiplier float64
	maxAttempts int
}

// NewBackoffRegistry creates a BackoffRegistry from config values.
func NewBackoffRegistry(initialMs, maxMs int, multiplier float64, maxAttempts int) *BackoffRegistry {
	return &BackoffRegistry{
		domains:     make(map[string]*domainBackoff),
		initialMs:   initialMs,
		maxMs:       maxMs,
		multiplier:  multiplier,
		maxAttempts: maxAttempts,
	}
}

// RecordError notes a transient error for the given domain and updates backoff.
// Returns the delay that will be applied before the next attempt.
func (r *BackoffRegistry) RecordError(domain string) time.Duration {
	r.mu.Lock()
	db, ok := r.domains[domain]
	if !ok {
		db = &domainBackoff{}
		r.domains[domain] = db
	}
	r.mu.Unlock()

	db.mu.Lock()
	defer db.mu.Unlock()

	db.attempts++
	delay := r.decorrelatedJitter(db.attempts)
	db.nextAllowed = time.Now().Add(delay)
	return delay
}

// RecordSuccess resets the backoff state for the domain.
func (r *BackoffRegistry) RecordSuccess(domain string) {
	r.mu.Lock()
	delete(r.domains, domain)
	r.mu.Unlock()
}

// Wait blocks until the backoff delay for the domain has elapsed, or ctx is done.
// If the domain has reached max_attempts and its delay has expired, the entry is
// evicted so the map does not grow without bound.
func (r *BackoffRegistry) Wait(ctx context.Context, domain string) error {
	r.mu.Lock()
	db, ok := r.domains[domain]
	r.mu.Unlock()
	if !ok {
		return nil
	}

	db.mu.Lock()
	until := db.nextAllowed
	attempts := db.attempts
	db.mu.Unlock()

	remaining := time.Until(until)
	if remaining <= 0 {
		// Evict entries that have exhausted max attempts and served their delay.
		if attempts >= r.maxAttempts {
			r.mu.Lock()
			delete(r.domains, domain)
			r.mu.Unlock()
		}
		return nil
	}

	select {
	case <-time.After(remaining):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Attempts returns the current backoff attempt count for a domain.
func (r *BackoffRegistry) Attempts(domain string) int {
	r.mu.Lock()
	db, ok := r.domains[domain]
	r.mu.Unlock()
	if !ok {
		return 0
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.attempts
}

// MaxAttempts returns the configured maximum retry attempts.
func (r *BackoffRegistry) MaxAttempts() int {
	return r.maxAttempts
}

// decorrelatedJitter implements AWS-style decorrelated jitter backoff.
// delay = random(base, prev_delay * multiplier), capped at maxMs.
func (r *BackoffRegistry) decorrelatedJitter(attempt int) time.Duration {
	base := float64(r.initialMs)
	cap := float64(r.maxMs)

	// Exponential ceiling for this attempt.
	ceiling := base
	for i := 1; i < attempt; i++ {
		ceiling *= r.multiplier
		if ceiling > cap {
			ceiling = cap
			break
		}
	}

	// Random value between base and ceiling.
	jittered := base + rand.Float64()*(ceiling-base) //nolint:gosec
	if jittered > cap {
		jittered = cap
	}

	return time.Duration(jittered) * time.Millisecond
}
