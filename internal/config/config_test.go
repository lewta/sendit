package config

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeTempFile writes content to a file with the given name in a temp dir.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return p
}

// writeTemp writes content to a temporary YAML file and returns the path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.yaml")
	if err != nil {
		t.Fatalf("creating temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writing temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// minimalValidYAML is a minimal config that passes validation.
const minimalValidYAML = `
pacing:
  mode: human
  requests_per_minute: 10
  jitter_factor: 0.3
  min_delay_ms: 500
  max_delay_ms: 3000
limits:
  max_workers: 2
  max_browser_workers: 1
  cpu_threshold_pct: 80
  memory_threshold_mb: 256
rate_limits:
  default_rps: 1.0
backoff:
  initial_ms: 500
  max_ms: 30000
  multiplier: 2.0
  max_attempts: 3
targets:
  - url: "https://example.com"
    weight: 1
    type: http
daemon:
  log_level: info
  log_format: text
`

func TestLoad_Valid(t *testing.T) {
	path := writeTemp(t, minimalValidYAML)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.Pacing.Mode != "human" {
		t.Errorf("pacing.mode = %q, want %q", cfg.Pacing.Mode, "human")
	}
	if cfg.Pacing.RequestsPerMinute != 10 {
		t.Errorf("requests_per_minute = %v, want 10", cfg.Pacing.RequestsPerMinute)
	}
	if len(cfg.Targets) != 1 {
		t.Errorf("targets len = %d, want 1", len(cfg.Targets))
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	path := writeTemp(t, ":::invalid yaml:::")
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Config with only required fields — defaults should fill the rest.
	yaml := `
targets:
  - url: "https://example.com"
    weight: 1
    type: http
`
	path := writeTemp(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Pacing.Mode != "human" {
		t.Errorf("default pacing.mode = %q, want human", cfg.Pacing.Mode)
	}
	if cfg.Pacing.RequestsPerMinute != 20 {
		t.Errorf("default rpm = %v, want 20", cfg.Pacing.RequestsPerMinute)
	}
	if cfg.Limits.MaxWorkers != 4 {
		t.Errorf("default max_workers = %d, want 4", cfg.Limits.MaxWorkers)
	}
	if cfg.Limits.MaxBrowserWorkers != 1 {
		t.Errorf("default max_browser_workers = %d, want 1", cfg.Limits.MaxBrowserWorkers)
	}
	if cfg.RateLimits.DefaultRPS != 0.5 {
		t.Errorf("default default_rps = %v, want 0.5", cfg.RateLimits.DefaultRPS)
	}
	if cfg.Backoff.InitialMs != 1000 {
		t.Errorf("default backoff.initial_ms = %d, want 1000", cfg.Backoff.InitialMs)
	}
	if cfg.Daemon.LogLevel != "info" {
		t.Errorf("default log_level = %q, want info", cfg.Daemon.LogLevel)
	}
	if cfg.Daemon.LogFormat != "text" {
		t.Errorf("default log_format = %q, want text", cfg.Daemon.LogFormat)
	}
}

func TestValidate_PacingMode(t *testing.T) {
	for _, mode := range []string{"human", "rate_limited", "scheduled"} {
		extra := ""
		if mode == "scheduled" {
			extra = `
  schedule:
    - cron: "0 9 * * 1-5"
      duration_minutes: 30
      requests_per_minute: 10`
		}
		yaml := `
pacing:
  mode: ` + mode + extra + `
targets:
  - url: "https://example.com"
    weight: 1
    type: http
`
		path := writeTemp(t, yaml)
		if _, err := Load(path); err != nil {
			t.Errorf("mode %q: unexpected error: %v", mode, err)
		}
	}

	// Invalid mode.
	path := writeTemp(t, strings.ReplaceAll(minimalValidYAML, "mode: human", "mode: burst"))
	if _, err := Load(path); err == nil {
		t.Error("expected error for invalid pacing mode, got nil")
	}
}

func TestValidate_ScheduledRequiresEntries(t *testing.T) {
	yaml := `
pacing:
  mode: scheduled
targets:
  - url: "https://example.com"
    weight: 1
    type: http
`
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error: scheduled mode with no schedule entries")
	}
	if !strings.Contains(err.Error(), "schedule") {
		t.Errorf("error should mention 'schedule', got: %v", err)
	}
}

func TestValidate_EmptyTargets(t *testing.T) {
	yaml := `
targets: []
`
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for empty targets, got nil")
	}
	if !strings.Contains(err.Error(), "targets") {
		t.Errorf("error should mention 'targets', got: %v", err)
	}
}

func TestValidate_InvalidTargetType(t *testing.T) {
	yaml := `
targets:
  - url: "https://example.com"
    weight: 1
    type: grpc
`
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid target type, got nil")
	}
}

func TestValidate_ZeroWeight(t *testing.T) {
	yaml := `
targets:
  - url: "https://example.com"
    weight: 0
    type: http
`
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for zero weight, got nil")
	}
}

func TestValidate_JitterFactor(t *testing.T) {
	// jitter_factor must be in [0,1]
	yaml := strings.ReplaceAll(minimalValidYAML, "jitter_factor: 0.3", "jitter_factor: 1.5")
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for jitter_factor > 1")
	}
}

func TestValidate_MinMaxDelay(t *testing.T) {
	yaml := strings.ReplaceAll(minimalValidYAML, "max_delay_ms: 3000", "max_delay_ms: 100")
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error when max_delay_ms < min_delay_ms")
	}
}

func TestValidate_BackoffMultiplier(t *testing.T) {
	yaml := strings.ReplaceAll(minimalValidYAML, "multiplier: 2.0", "multiplier: 0.5")
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for multiplier <= 1")
	}
}

func TestValidate_LogLevel(t *testing.T) {
	yaml := strings.ReplaceAll(minimalValidYAML, "log_level: info", "log_level: verbose")
	path := writeTemp(t, yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for invalid log_level")
	}
}

func TestValidate_AllTargetTypes(t *testing.T) {
	types := []string{"http", "browser", "dns", "websocket"}
	for _, typ := range types {
		yaml := `
targets:
  - url: "https://example.com"
    weight: 1
    type: ` + typ + `
`
		path := writeTemp(t, yaml)
		if _, err := Load(path); err != nil {
			t.Errorf("type %q: unexpected error: %v", typ, err)
		}
	}
}

func TestValidate_PerDomainRateLimits(t *testing.T) {
	yaml := `
pacing:
  mode: human
  requests_per_minute: 10
  jitter_factor: 0.3
  min_delay_ms: 500
  max_delay_ms: 3000
limits:
  max_workers: 2
  max_browser_workers: 1
  cpu_threshold_pct: 80
  memory_threshold_mb: 256
rate_limits:
  default_rps: 1.0
  per_domain:
    - domain: "example.com"
      rps: 0.1
backoff:
  initial_ms: 500
  max_ms: 30000
  multiplier: 2.0
  max_attempts: 3
targets:
  - url: "https://example.com"
    weight: 1
    type: http
daemon:
  log_level: info
  log_format: text
`
	path := writeTemp(t, yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.RateLimits.PerDomain) != 1 {
		t.Errorf("per_domain len = %d, want 1", len(cfg.RateLimits.PerDomain))
	}
	if cfg.RateLimits.PerDomain[0].Domain != "example.com" {
		t.Errorf("domain = %q, want example.com", cfg.RateLimits.PerDomain[0].Domain)
	}
}

// --- targets_file tests ---

func TestTargetsFile_BasicLoad(t *testing.T) {
	targetsContent := `
# comment line
https://example.com http
example.com         dns

https://other.com   http 3
`
	targetsPath := writeTempFile(t, "targets.txt", targetsContent)
	yaml := strings.ReplaceAll(minimalValidYAML, "targets:\n  - url: \"https://example.com\"\n    weight: 1\n    type: http", "") +
		"\ntargets_file: " + strconv.Quote(targetsPath)
	cfgPath := writeTemp(t, yaml)

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 3 {
		t.Fatalf("expected 3 targets, got %d", len(cfg.Targets))
	}

	// First entry: default weight (1).
	if cfg.Targets[0].URL != "https://example.com" {
		t.Errorf("target[0].URL = %q", cfg.Targets[0].URL)
	}
	if cfg.Targets[0].Type != "http" {
		t.Errorf("target[0].Type = %q", cfg.Targets[0].Type)
	}
	if cfg.Targets[0].Weight != 1 {
		t.Errorf("target[0].Weight = %d, want 1", cfg.Targets[0].Weight)
	}

	// Second entry: dns.
	if cfg.Targets[1].Type != "dns" {
		t.Errorf("target[1].Type = %q, want dns", cfg.Targets[1].Type)
	}

	// Third entry: explicit weight 3.
	if cfg.Targets[2].Weight != 3 {
		t.Errorf("target[2].Weight = %d, want 3", cfg.Targets[2].Weight)
	}
}

func TestTargetsFile_DefaultsApplied(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "https://example.com http\n")
	yaml := `
targets_file: ` + strconv.Quote(targetsPath) + `
target_defaults:
  weight: 7
  http:
    method: POST
    timeout_s: 20
    headers:
      User-Agent: "TestAgent/1.0"
  dns:
    resolver: "1.1.1.1:53"
    record_type: AAAA
`
	cfgPath := writeTemp(t, yaml)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(cfg.Targets))
	}
	tgt := cfg.Targets[0]
	if tgt.Weight != 7 {
		t.Errorf("Weight = %d, want 7", tgt.Weight)
	}
	if tgt.HTTP.Method != "POST" {
		t.Errorf("HTTP.Method = %q, want POST", tgt.HTTP.Method)
	}
	if tgt.HTTP.TimeoutS != 20 {
		t.Errorf("HTTP.TimeoutS = %d, want 20", tgt.HTTP.TimeoutS)
	}
	// Viper lowercases all map keys, so "User-Agent" → "user-agent".
	if tgt.HTTP.Headers["user-agent"] != "TestAgent/1.0" {
		t.Errorf("user-agent header = %q, want TestAgent/1.0", tgt.HTTP.Headers["user-agent"])
	}
	// DNS defaults should also be present even though this is an http target.
	if cfg.TargetDefaults.DNS.Resolver != "1.1.1.1:53" {
		t.Errorf("TargetDefaults.DNS.Resolver = %q, want 1.1.1.1:53", cfg.TargetDefaults.DNS.Resolver)
	}
}

func TestTargetsFile_CombinesWithInlineTargets(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "https://from-file.com http\n")
	yaml := `
targets:
  - url: "https://inline.com"
    weight: 5
    type: http
targets_file: ` + strconv.Quote(targetsPath) + `
`
	cfgPath := writeTemp(t, yaml)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("expected 2 targets (inline + file), got %d", len(cfg.Targets))
	}
	if cfg.Targets[0].URL != "https://inline.com" {
		t.Errorf("first target should be inline, got %q", cfg.Targets[0].URL)
	}
	if cfg.Targets[1].URL != "https://from-file.com" {
		t.Errorf("second target should be from file, got %q", cfg.Targets[1].URL)
	}
}

func TestTargetsFile_IgnoresBlankAndComments(t *testing.T) {
	content := `
# first comment

https://a.com http

# second comment
https://b.com dns
`
	targetsPath := writeTempFile(t, "targets.txt", content)
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Errorf("expected 2 targets, got %d", len(cfg.Targets))
	}
}

func TestTargetsFile_FileNotFound(t *testing.T) {
	yaml := `targets_file: "/nonexistent/path/targets.txt"` + "\n"
	cfgPath := writeTemp(t, yaml)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for missing targets_file, got nil")
	}
	if !strings.Contains(err.Error(), "targets_file") {
		t.Errorf("error should mention 'targets_file', got: %v", err)
	}
}

func TestTargetsFile_InvalidFormat_MissingType(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "https://example.com\n")
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for line with missing type")
	}
}

func TestTargetsFile_InvalidFormat_BadType(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "https://example.com grpc\n")
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for unknown type 'grpc'")
	}
	if !strings.Contains(err.Error(), "grpc") {
		t.Errorf("error should mention 'grpc', got: %v", err)
	}
}

func TestTargetsFile_InvalidFormat_BadWeight(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "https://example.com http notanumber\n")
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for non-numeric weight")
	}
}

func TestTargetsFile_InvalidFormat_ZeroWeight(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "https://example.com http 0\n")
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected error for zero weight")
	}
}

func TestTargetsFile_AllTypes(t *testing.T) {
	content := `
https://a.com     http
https://b.com     browser
example.com       dns
wss://c.com/feed  websocket
`
	targetsPath := writeTempFile(t, "targets.txt", content)
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 4 {
		t.Fatalf("expected 4 targets, got %d", len(cfg.Targets))
	}
	types := []string{"http", "browser", "dns", "websocket"}
	for i, want := range types {
		if cfg.Targets[i].Type != want {
			t.Errorf("target[%d].Type = %q, want %q", i, cfg.Targets[i].Type, want)
		}
	}
}

func TestTargetsFile_EmptyFileFailsValidation(t *testing.T) {
	targetsPath := writeTempFile(t, "targets.txt", "# only comments\n\n")
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	_, err := Load(cfgPath)
	if err == nil {
		t.Fatal("expected validation error for empty targets list")
	}
	if !strings.Contains(err.Error(), "targets") {
		t.Errorf("error should mention 'targets', got: %v", err)
	}
}

func TestTargetsFile_DefaultWeight_FallsBackToOne(t *testing.T) {
	// target_defaults.weight not set → should default to 1.
	targetsPath := writeTempFile(t, "targets.txt", "https://example.com http\n")
	yaml := "targets_file: " + strconv.Quote(targetsPath) + "\n"
	cfgPath := writeTemp(t, yaml)
	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Weight != 1 {
		t.Errorf("default weight = %d, want 1", cfg.Targets[0].Weight)
	}
}
