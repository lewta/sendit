package main

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// --- helpers ---

func printDryRun(path string, cfg *config.Config, duration time.Duration) error {
	// Sort a copy by weight descending.
	sorted := make([]config.TargetConfig, len(cfg.Targets))
	copy(sorted, cfg.Targets)
	slices.SortFunc(sorted, func(a, b config.TargetConfig) int {
		return b.Weight - a.Weight
	})
	var examples []task.Task
	if len(sorted) > 0 {
		selector, err := task.NewSelector(sorted)
		if err != nil {
			return fmt.Errorf("building dry-run examples: %w", err)
		}
		examples = selector.Examples()
	}

	fmt.Printf("Config: %s  ✓ valid\n\n", path)

	// Compute total weight.
	totalWeight := 0
	for _, t := range cfg.Targets {
		totalWeight += t.Weight
	}

	fmt.Printf("Targets (%d):\n", len(sorted))
	fmt.Printf("  %-40s %-10s %-10s %-8s %s\n", "URL", "TYPE", "WEIGHT", "SHARE", "HTTP POLICY")
	for i, t := range sorted {
		share := 0.0
		if totalWeight > 0 {
			share = float64(t.Weight) / float64(totalWeight) * 100
		}
		policy := "-"
		if t.Type == "http" {
			policy = map[int]string{0: "auto", 1: "HTTP/1.1", 2: "HTTP/2 (HTTPS)"}[t.HTTP.HTTPVersion]
		}
		fmt.Printf("  %-40s %-10s %-10d %-8s %s\n", examples[i].URL, t.Type, t.Weight, fmt.Sprintf("%.1f%%", share), policy)
	}
	fmt.Printf("  Total weight: %d\n", totalWeight)
	fmt.Println()

	// Pacing.
	p := cfg.Pacing
	switch p.Mode {
	case "human":
		fmt.Printf("Pacing:\n  mode: human | delay: %dms–%dms (random uniform)\n", p.MinDelayMs, p.MaxDelayMs)
	case "rate_limited":
		rps := p.RequestsPerMinute / 60.0
		fmt.Printf("Pacing:\n  mode: rate_limited | rpm: %.0f (~%.2f rps) | jitter: ≤200ms\n", p.RequestsPerMinute, rps)
	case "scheduled":
		fmt.Printf("Pacing:\n  mode: scheduled\n")
		for i, s := range p.Schedule {
			fmt.Printf("  [%d] cron: %q  duration: %dm  rpm: %.0f\n", i, s.Cron, s.DurationMinutes, s.RequestsPerMinute)
		}
	case "burst":
		rampUp := "none"
		if p.RampUpS > 0 {
			rampUp = fmt.Sprintf("%ds", p.RampUpS)
		}
		dur := "unlimited (SIGTERM to stop)"
		if duration > 0 {
			dur = duration.String()
		}
		fmt.Printf("Pacing:\n  mode: burst | ramp_up: %s | duration: %s\n", rampUp, dur)
		fmt.Println("  ⚠  burst mode is intended for internal or owned infrastructure only")
	default:
		fmt.Printf("Pacing:\n  mode: %s\n", p.Mode)
	}
	fmt.Println()

	// Limits.
	l := cfg.Limits
	fmt.Printf("Limits:\n  workers: %d (browser: %d) | cpu: %.0f%% | memory: %d MB\n",
		l.MaxWorkers, l.MaxBrowserWorkers, l.CPUThresholdPct, l.MemoryThresholdMB)
	return nil
}

func initLogger(level, format string) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	if format == "text" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		})
	}
}

func writePID(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
