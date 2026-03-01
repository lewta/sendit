package engine

import (
	"testing"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/metrics"
)

func baseCfg(targets []config.TargetConfig) *config.Config {
	return &config.Config{
		Pacing: config.PacingConfig{
			Mode:              "rate_limited",
			RequestsPerMinute: 60,
		},
		Limits: config.LimitsConfig{
			MaxWorkers:        2,
			MaxBrowserWorkers: 1,
			CPUThresholdPct:   100,
			MemoryThresholdMB: 999999,
		},
		RateLimits: config.RateLimitsConfig{DefaultRPS: 10},
		Backoff: config.BackoffConfig{
			InitialMs:   100,
			MaxMs:       1000,
			Multiplier:  2.0,
			MaxAttempts: 3,
		},
		Targets: targets,
	}
}

func TestReload_SwapsTargets(t *testing.T) {
	initial := []config.TargetConfig{
		{URL: "https://a.example.com", Weight: 1, Type: "http"},
	}
	eng, err := New(baseCfg(initial), metrics.Noop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	updated := []config.TargetConfig{
		{URL: "https://b.example.com", Weight: 1, Type: "http"},
	}
	newCfg := baseCfg(updated)
	if err := eng.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// After reload the stored config must reflect the new targets.
	got := eng.cfg.Load().Targets
	if len(got) != 1 || got[0].URL != "https://b.example.com" {
		t.Errorf("cfg.Targets after reload = %v, want b.example.com", got)
	}

	// The selector must serve tasks from the new target list.
	for range 20 {
		task := eng.selector.Load().Pick()
		if task.URL != "https://b.example.com" {
			t.Errorf("selector returned %q after reload, want b.example.com", task.URL)
		}
	}
}

func TestReload_SwapsRateLimits(t *testing.T) {
	targets := []config.TargetConfig{
		{URL: "https://a.example.com", Weight: 1, Type: "http"},
	}
	eng, err := New(baseCfg(targets), metrics.Noop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	newCfg := baseCfg(targets)
	newCfg.RateLimits = config.RateLimitsConfig{
		DefaultRPS: 99,
		PerDomain:  []config.DomainRateLimit{{Domain: "a.example.com", RPS: 42}},
	}
	if err := eng.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := eng.cfg.Load().RateLimits.DefaultRPS; got != 99 {
		t.Errorf("DefaultRPS after reload = %v, want 99", got)
	}
}

func TestReload_SwapsBackoff(t *testing.T) {
	targets := []config.TargetConfig{
		{URL: "https://a.example.com", Weight: 1, Type: "http"},
	}
	eng, err := New(baseCfg(targets), metrics.Noop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	newCfg := baseCfg(targets)
	newCfg.Backoff = config.BackoffConfig{
		InitialMs:   200,
		MaxMs:       5000,
		Multiplier:  3.0,
		MaxAttempts: 5,
	}
	if err := eng.Reload(newCfg); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	bo := eng.backoff.Load()
	if got := bo.MaxAttempts(); got != 5 {
		t.Errorf("backoff MaxAttempts after reload = %d, want 5", got)
	}
}

func TestReload_PacingModeChangeNoError(t *testing.T) {
	targets := []config.TargetConfig{
		{URL: "https://a.example.com", Weight: 1, Type: "http"},
	}
	eng, err := New(baseCfg(targets), metrics.Noop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Changing pacing mode should log a warning but not return an error.
	newCfg := baseCfg(targets)
	newCfg.Pacing.Mode = "human"
	newCfg.Pacing.MinDelayMs = 100
	newCfg.Pacing.MaxDelayMs = 500
	if err := eng.Reload(newCfg); err != nil {
		t.Fatalf("Reload with mode change returned error: %v", err)
	}
}

func TestHostname(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/path?q=1", "example.com"},
		{"https://sub.domain.org:8080/foo", "sub.domain.org"},
		{"http://localhost", "localhost"},
		{"wss://stream.example.com/feed", "stream.example.com"},
		{"example.com", "example.com"}, // raw hostname (DNS target)
		{"", ""},
	}
	for _, tc := range tests {
		got := hostname(tc.input)
		if got != tc.want {
			t.Errorf("hostname(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
