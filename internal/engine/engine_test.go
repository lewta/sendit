package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/task"
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

func TestDispatch_RateLimitsCrossHostRedirectDestination(t *testing.T) {
	var dstRequests atomic.Int32
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dstRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer dst.Close()

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusFound)
	}))
	defer src.Close()
	srcURL, err := url.Parse(src.URL)
	if err != nil {
		t.Fatalf("parsing source URL: %v", err)
	}
	srcURL.Host = "localhost:" + srcURL.Port()

	target := config.TargetConfig{
		URL:    srcURL.String(),
		Type:   "http",
		Weight: 1,
		HTTP: config.HTTPConfig{
			AllowCrossHostRedirects: true,
			TimeoutS:                1,
		},
	}
	cfg := baseCfg([]config.TargetConfig{target})
	cfg.RateLimits = config.RateLimitsConfig{
		DefaultRPS: 100,
		PerDomain: []config.DomainRateLimit{
			{Domain: hostname(dst.URL), RPS: 0.001},
		},
	}

	eng, err := New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Consume the destination host's burst token. The redirected request must
	// wait on that host's limiter and should time out before reaching dst.
	if err := eng.rl.Load().Wait(context.Background(), hostname(dst.URL)); err != nil {
		t.Fatalf("pre-consuming destination limiter: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	results := make(chan task.Result, 1)
	eng.SetObserver(func(result task.Result) {
		results <- result
	})

	if err := eng.pool.Acquire(ctx, target.Type); err != nil {
		t.Fatalf("pool.Acquire: %v", err)
	}
	eng.dispatch(ctx, task.Task{URL: target.URL, Type: target.Type, Config: target})

	select {
	case result := <-results:
		if result.Error == nil {
			t.Fatal("result.Error = nil, want redirect destination rate-limit wait error")
		}
	default:
		t.Fatal("observer did not receive dispatch result")
	}
	if dstRequests.Load() != 0 {
		t.Errorf("redirect destination received %d requests, want 0", dstRequests.Load())
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
