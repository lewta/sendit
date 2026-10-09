package task

import (
	cryptorand "crypto/rand"
	"fmt"
	"math/rand"
	"strconv"
	"sync/atomic"
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
	Capture    Capture
	Task       Task
	StatusCode int
	Duration   time.Duration
	BytesRead  int64
	Error      error
	Meta       map[string]string
}

// Selector picks tasks by weight using the Vose alias method for O(1) selection.
type Selector struct {
	targets       []config.TargetConfig
	templateNames [][]string
	sequences     []atomic.Uint64
	alias         []int
	prob          []float64
	n             int
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
	templateNames := make([][]string, n)
	for i, target := range targets {
		seen := make(map[string]bool)
		surfaces := []string{target.URL, target.HTTP.Body, target.GRPC.Body}
		surfaces = append(surfaces, target.WebSocket.SendMessages...)
		for _, surface := range surfaces {
			names, err := config.TemplateVariables(surface)
			if err != nil {
				return nil, fmt.Errorf("target %d: %w", i, err)
			}
			for _, name := range names {
				if seen[name] {
					continue
				}
				if candidates, exists := target.Vars[name]; exists {
					if len(candidates) == 0 {
						return nil, fmt.Errorf("target %d: variable %q has no values", i, name)
					}
				} else if name != "uuid" && name != "timestamp" && name != "seq" {
					return nil, fmt.Errorf("target %d: unknown variable %q", i, name)
				}
				templateNames[i] = append(templateNames[i], name)
				seen[name] = true
			}
		}
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
		targets:       targets,
		templateNames: templateNames,
		sequences:     make([]atomic.Uint64, n),
		alias:         alias,
		prob:          prob,
		n:             n,
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
	return s.taskAt(idx)
}

func (s *Selector) taskAt(index int) Task {
	t := s.targets[index]
	if len(s.templateNames[index]) == 0 {
		return Task{URL: t.URL, Type: t.Type, Config: t}
	}

	values := make(map[string]string, len(s.templateNames[index]))
	for _, name := range s.templateNames[index] {
		if candidates, exists := t.Vars[name]; exists {
			values[name] = candidates[rand.Intn(len(candidates))] //nolint:gosec
			continue
		}
		switch name {
		case "uuid":
			values[name] = randomUUID()
		case "timestamp":
			values[name] = strconv.FormatInt(time.Now().Unix(), 10)
		case "seq":
			values[name] = strconv.FormatUint(s.sequences[index].Add(1), 10)
		}
	}

	t.URL = config.ExpandTemplate(t.URL, values)
	t.HTTP.Body = config.ExpandTemplate(t.HTTP.Body, values)
	t.GRPC.Body = config.ExpandTemplate(t.GRPC.Body, values)
	if len(t.WebSocket.SendMessages) > 0 {
		messages := make([]string, len(t.WebSocket.SendMessages))
		for i, message := range t.WebSocket.SendMessages {
			messages[i] = config.ExpandTemplate(message, values)
		}
		t.WebSocket.SendMessages = messages
	}
	return Task{
		URL:    t.URL,
		Type:   t.Type,
		Config: t,
	}
}

// Examples expands every configured target once for dry-run output.
func (s *Selector) Examples() []Task {
	tasks := make([]Task, len(s.targets))
	for i := range s.targets {
		tasks[i] = s.taskAt(i)
	}
	return tasks
}

func randomUUID() string {
	var value [16]byte
	if _, err := cryptorand.Read(value[:]); err != nil {
		panic(fmt.Errorf("generating request UUID: %w", err))
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
