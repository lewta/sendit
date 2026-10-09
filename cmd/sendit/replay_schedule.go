package main

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/ratelimit"
	"github.com/lewta/sendit/internal/task"
)

type replayOptions struct {
	Rate      float64
	Filter    string
	Loop      bool
	LoopDelay time.Duration
}
type replayItem struct {
	Record output.ReplayRecord
	Offset time.Duration
}
type replaySummary struct{ Requests, Failures uint64 }

func (o replayOptions) validate() error {
	if o.Rate <= 0 || math.IsNaN(o.Rate) || math.IsInf(o.Rate, 0) {
		return fmt.Errorf("--rate must be finite and greater than zero")
	}
	if o.Filter != "" && o.Filter != "status=5xx" {
		return fmt.Errorf("--filter supports only status=5xx")
	}
	if o.LoopDelay < 0 {
		return fmt.Errorf("--loop-delay must not be negative")
	}
	return nil
}

func prepareReplay(records []output.ReplayRecord, opts replayOptions) ([]replayItem, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	rate := new(big.Rat).SetFloat64(opts.Rate)
	var items []replayItem
	var first time.Time
	for _, r := range records {
		if opts.Filter != "" && (r.Status < 500 || r.Status > 599) {
			continue
		}
		if !r.Envelope.Replayable {
			return nil, fmt.Errorf("line %d: record is not replayable: %s", r.Line, r.Envelope.Reason)
		}
		if len(items) == 0 {
			first = r.Envelope.StartedAt
		}
		elapsed := r.Envelope.StartedAt.Sub(first)
		if elapsed < 0 || !first.Add(elapsed).Equal(r.Envelope.StartedAt) {
			return nil, fmt.Errorf("line %d: capture span overflows duration", r.Line)
		}
		scaled := new(big.Rat).Quo(new(big.Rat).SetInt64(int64(elapsed)), rate)
		ns := new(big.Int).Quo(scaled.Num(), scaled.Denom())
		if !ns.IsInt64() {
			return nil, fmt.Errorf("line %d: scaled offset overflows duration", r.Line)
		}
		items = append(items, replayItem{Record: r, Offset: time.Duration(ns.Int64())})
	}
	if len(items) == 0 && opts.Loop {
		return nil, fmt.Errorf("--loop requires at least one selected record")
	}
	return items, nil
}

func replayFailed(r task.Result) bool {
	class := ratelimit.ClassifyStatusCode(r.StatusCode)
	return r.Error != nil || class == ratelimit.ErrorClassTransient || class == ratelimit.ErrorClassPermanent
}

func waitReplay(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

// runReplay schedules independently of request duration and serialized result I/O.
func runReplay(parent context.Context, items []replayItem, opts replayOptions, drivers map[string]driver.Driver, write func(task.Result) error) (replaySummary, error) {
	var summary replaySummary
	for _, item := range items {
		if drivers[item.Record.Type] == nil {
			return summary, fmt.Errorf("line %d: replay driver is unavailable", item.Record.Line)
		}
	}
	if len(items) == 0 {
		return summary, nil
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	capture := task.NewCaptureClock()
	var wg sync.WaitGroup
	var mu sync.Mutex
	var runErr error
	fail := func(err error) {
		if runErr == nil {
			runErr = err
		}
		cancel()
	}
	for {
		start := time.Now()
		for _, item := range items {
			if err := waitReplay(ctx, time.Until(start.Add(item.Offset))); err != nil {
				break
			}
			if ctx.Err() != nil {
				break
			}
			stamp, err := capture.Next()
			if err != nil {
				mu.Lock()
				fail(err)
				mu.Unlock()
				break
			}
			v := item.Record.Envelope.Request.Task()
			drv := drivers[item.Record.Type]
			wg.Go(func() {
				result := drv.Execute(ctx, v)
				result.Capture = stamp
				mu.Lock()
				defer mu.Unlock()
				summary.Requests++
				if replayFailed(result) {
					summary.Failures++
				}
				if write != nil && runErr == nil {
					if err := write(result); err != nil {
						fail(err)
					}
				}
			})
		}
		wg.Wait()
		if ctx.Err() != nil || !opts.Loop {
			break
		}
		if err := waitReplay(ctx, opts.LoopDelay); err != nil {
			break
		}
	}
	return summary, runErr
}
