package task

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/lewta/sendit/internal/config"
)

// Task is a single unit of work dispatched to a driver.
type Task struct {
	URL    string
	Type   string // http | browser | dns | websocket
	Config config.TargetConfig
}

// Result holds the outcome of a driver execution.
type Result struct {
	Task       Task
	StatusCode int
	Duration   time.Duration
	BytesRead  int64
	Error      error
}

// Selector picks tasks by weight using the Vose alias method for O(1) selection.
type Selector struct {
	targets []config.TargetConfig
	alias   []int
	prob    []float64
	n       int
}

// NewSelector builds the alias table from the target list.
// Panics if targets is empty.
func NewSelector(targets []config.TargetConfig) (*Selector, error) {
	n := len(targets)
	if n == 0 {
		return nil, fmt.Errorf("selector requires at least one target")
	}

	totalWeight := 0
	for _, t := range targets {
		totalWeight += t.Weight
	}
	if totalWeight <= 0 {
		return nil, fmt.Errorf("total weight must be > 0")
	}

	prob := make([]float64, n)
	alias := make([]int, n)

	// Scaled probabilities so each slot has expected value 1.
	scaled := make([]float64, n)
	for i, t := range targets {
		scaled[i] = float64(t.Weight) * float64(n) / float64(totalWeight)
	}

	small := make([]int, 0, n)
	large := make([]int, 0, n)

	for i, p := range scaled {
		if p < 1.0 {
			small = append(small, i)
		} else {
			large = append(large, i)
		}
	}

	for len(small) > 0 && len(large) > 0 {
		l := small[len(small)-1]
		small = small[:len(small)-1]
		g := large[len(large)-1]
		large = large[:len(large)-1]

		prob[l] = scaled[l]
		alias[l] = g
		scaled[g] = (scaled[g] + scaled[l]) - 1.0

		if scaled[g] < 1.0 {
			small = append(small, g)
		} else {
			large = append(large, g)
		}
	}

	for _, g := range large {
		prob[g] = 1.0
	}
	for _, l := range small {
		prob[l] = 1.0
	}

	return &Selector{
		targets: targets,
		alias:   alias,
		prob:    prob,
		n:       n,
	}, nil
}

// Pick selects a target with probability proportional to its weight.
func (s *Selector) Pick() Task {
	i := rand.Intn(s.n) //nolint:gosec
	var idx int
	if rand.Float64() < s.prob[i] { //nolint:gosec
		idx = i
	} else {
		idx = s.alias[i]
	}
	t := s.targets[idx]
	return Task{
		URL:    t.URL,
		Type:   t.Type,
		Config: t,
	}
}
