package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/policy"
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

func TestDispatchRechecksEnvironmentPolicyAtResolutionBoundary(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	t.Setenv("SENDIT_ALLOWED_TOKEN", "allowed-secret")
	t.Setenv("SENDIT_DENIED_TOKEN", "denied-secret")

	access, err := policy.New(policy.ModeRestricted, policy.Grants{EnvNames: []string{"SENDIT_ALLOWED_TOKEN"}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := fmt.Sprintf("targets: [{url: %q, type: http, weight: 1, auth: {type: bearer, token_env: SENDIT_ALLOWED_TOKEN}}]\n", srv.URL)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWithPolicy(path, access)
	if err != nil {
		t.Fatal(err)
	}
	// Model a post-load mutation/future caller bug. The getenv sink must enforce
	// the immutable config policy rather than trusting parser-time validation.
	cfg.Targets[0].Auth.TokenEnv = "SENDIT_DENIED_TOKEN"
	eng, err := New(cfg, metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan task.Result, 1)
	eng.SetObserver(func(result task.Result) { results <- result })
	if err := eng.pool.Acquire(t.Context(), "http"); err != nil {
		t.Fatal(err)
	}
	eng.dispatch(t.Context(), task.Task{URL: cfg.Targets[0].URL, Type: "http", Config: cfg.Targets[0]})
	result := <-results
	if result.Error == nil || !strings.Contains(result.Error.Error(), "environment variable denied: SENDIT_DENIED_TOKEN") {
		t.Fatalf("dispatch error = %v", result.Error)
	}
	if requests.Load() != 0 {
		t.Fatalf("denied environment lookup sent %d request(s)", requests.Load())
	}
}

func TestNewRechecksOutputPolicyBeforeAnyTruncation(t *testing.T) {
	root := t.TempDir()
	outputPath := filepath.Join(root, "results.jsonl")
	pcapPath := filepath.Join(root, "capture.pcap")
	if err := os.WriteFile(outputPath, []byte("preserve-output"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pcapPath, []byte("preserve-pcap"), 0o600); err != nil {
		t.Fatal(err)
	}
	overwrite := true
	access, err := policy.New(policy.ModeRestricted, policy.Grants{OutputRoot: &root, Overwrite: &overwrite})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := fmt.Sprintf("output: {enabled: true, file: %q, pcap_file: %q}\ntargets: [{url: https://example.com, type: http, weight: 1}]\n", outputPath, pcapPath)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWithPolicy(path, access)
	if err != nil {
		t.Fatal(err)
	}
	// Model a post-load mutation: the actual open boundary must recheck and the
	// first destination must remain untouched if the second is denied.
	cfg.Output.PCAPFile = filepath.Join(t.TempDir(), "outside.pcap")
	if _, err := New(cfg, metrics.Noop()); err == nil || !strings.Contains(err.Error(), "config policy") {
		t.Fatalf("expected sink denial, got %v", err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil || string(got) != "preserve-output" {
		t.Fatalf("partial initialization modified first output: %q, %v", got, err)
	}
}

func TestNewRestrictedOutputRejectsSymlinkAndHardlinkAliases(t *testing.T) {
	root := t.TempDir()
	overwrite := true
	access, err := policy.New(policy.ModeRestricted, policy.Grants{OutputRoot: &root, Overwrite: &overwrite})
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]string)
	link := filepath.Join(root, "symlink.jsonl")
	if err := os.Symlink(outside, link); err == nil {
		paths["symlink"] = link
	}
	hardlink := filepath.Join(root, "hardlink.jsonl")
	if err := os.Link(outside, hardlink); err == nil {
		paths["hardlink"] = hardlink
	}
	for name, outputPath := range paths {
		t.Run(name, func(t *testing.T) {
			cfgPath := filepath.Join(t.TempDir(), "config.yaml")
			data := fmt.Sprintf("output: {enabled: true, file: %q}\ntargets: [{url: https://example.com, type: http, weight: 1}]\n", outputPath)
			if err := os.WriteFile(cfgPath, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.LoadWithPolicy(cfgPath, access)
			if name == "symlink" && err != nil {
				// Canonical containment may reject an out-of-root final symlink at
				// preflight; either way the sink must not modify it.
			} else if err == nil {
				_, err = New(cfg, metrics.Noop())
			}
			if err == nil {
				t.Fatal("alias accepted")
			}
			got, readErr := os.ReadFile(outside)
			if readErr != nil || string(got) != "preserve" {
				t.Fatalf("alias denial modified outside file: %q, %v", got, readErr)
			}
		})
	}
}

func TestNewRejectsSameNewOutputAndPCAPWithoutLeavingFile(t *testing.T) {
	root := t.TempDir()
	destination := filepath.Join(root, "same-new-output")
	overwrite := true
	access, err := policy.New(policy.ModeRestricted, policy.Grants{OutputRoot: &root, Overwrite: &overwrite})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := fmt.Sprintf("output: {enabled: true, file: %q, pcap_file: %q}\ntargets: [{url: https://example.com, type: http, weight: 1}]\n", destination, destination)
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadWithPolicy(path, access)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(cfg, metrics.Noop()); err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("same destination error = %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("denied same destination left a file: %v", err)
	}
}

func TestReloadRejectsOutputChangesBeforeAnySwapOrFileEffect(t *testing.T) {
	initial := []config.TargetConfig{{URL: "https://old.example.com", Weight: 1, Type: "http"}}
	eng, err := New(baseCfg(initial), metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}

	outputPath := filepath.Join(t.TempDir(), "reload-created.jsonl")
	replacement := baseCfg([]config.TargetConfig{{URL: "https://new.example.com", Weight: 1, Type: "http"}})
	replacement.Output = config.OutputConfig{Enabled: true, File: outputPath, Format: "jsonl"}
	if err := eng.Reload(replacement); err == nil || !strings.Contains(err.Error(), "output settings require restart") {
		t.Fatalf("reload error = %v", err)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("rejected reload created output: %v", err)
	}
	if got := eng.cfg.Load().Targets[0].URL; got != initial[0].URL {
		t.Fatalf("rejected reload stored target %q", got)
	}
	for i := 0; i < 10; i++ {
		if got := eng.selector.Load().Pick().URL; got != initial[0].URL {
			t.Fatalf("rejected reload swapped selector to %q", got)
		}
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
