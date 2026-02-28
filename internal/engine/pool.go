package engine

import (
	"context"
	"sync"
)

// Pool manages a global concurrency semaphore and an optional browser sub-semaphore.
type Pool struct {
	global  chan struct{}
	browser chan struct{}
	wg      sync.WaitGroup
}

// NewPool creates a Pool with the given global and browser worker limits.
func NewPool(maxWorkers, maxBrowserWorkers int) *Pool {
	return &Pool{
		global:  make(chan struct{}, maxWorkers),
		browser: make(chan struct{}, maxBrowserWorkers),
	}
}

// Acquire obtains a global slot (and a browser slot for browser tasks).
// Blocks until slots are available or ctx is cancelled.
func (p *Pool) Acquire(ctx context.Context, taskType string) error {
	// Global slot.
	select {
	case p.global <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Browser sub-slot.
	if taskType == "browser" {
		select {
		case p.browser <- struct{}{}:
		case <-ctx.Done():
			<-p.global
			return ctx.Err()
		}
	}

	p.wg.Add(1)
	return nil
}

// Release frees the slots acquired for the given task type.
func (p *Pool) Release(taskType string) {
	if taskType == "browser" {
		<-p.browser
	}
	<-p.global
	p.wg.Done()
}

// Wait blocks until all in-flight tasks have completed.
func (p *Pool) Wait() {
	p.wg.Wait()
}
