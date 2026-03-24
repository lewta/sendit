//go:build integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalCfg returns a valid YAML config string suitable for cmd-level tests.
func minimalCfg(t *testing.T, extras string) string {
	t.Helper()
	return `
pacing:
  mode: rate_limited
  requests_per_minute: 60
limits:
  max_workers: 2
  max_browser_workers: 1
  cpu_threshold_pct: 80
  memory_threshold_mb: 512
rate_limits:
  default_rps: 10
backoff:
  initial_ms: 100
  max_ms: 1000
  multiplier: 2.0
  max_attempts: 3
targets:
  - url: "https://example.com"
    weight: 1
    type: http
` + extras
}

func writeCfg(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestIntegrationCmd_Validate_Valid verifies that validate exits 0 and prints
// "config valid" for a well-formed config.
func TestIntegrationCmd_Validate_Valid(t *testing.T) {
	cfgPath := writeCfg(t, minimalCfg(t, ""))

	cmd := validateCmd()
	cmd.SetArgs([]string{"--config", cfgPath})

	var out string
	var err error
	out = captureStdout(t, func() {
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("validate returned error for valid config: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "config valid") {
		t.Errorf("expected 'config valid' in output, got: %q", out)
	}
}

// TestIntegrationCmd_Validate_Invalid verifies that validate exits non-zero
// for a malformed config (unknown pacing mode).
func TestIntegrationCmd_Validate_Invalid(t *testing.T) {
	bad := writeCfg(t, `
pacing:
  mode: not_a_real_mode
limits:
  max_workers: 2
  max_browser_workers: 1
  cpu_threshold_pct: 80
  memory_threshold_mb: 512
rate_limits:
  default_rps: 10
backoff:
  initial_ms: 100
  max_ms: 1000
  multiplier: 2.0
  max_attempts: 3
targets:
  - url: "https://example.com"
    weight: 1
    type: http
`)

	cmd := validateCmd()
	cmd.SetArgs([]string{"--config", bad})

	if err := cmd.Execute(); err == nil {
		t.Fatal("validate returned nil error for invalid config; expected non-zero exit")
	}
}

// TestIntegrationCmd_Start_DryRun verifies that start --dry-run prints the
// dry-run summary and does not block or start the engine.
func TestIntegrationCmd_Start_DryRun(t *testing.T) {
	cfgPath := writeCfg(t, minimalCfg(t, ""))

	cmd := startCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "--dry-run"})

	var got string
	var err error
	got = captureStdout(t, func() {
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("start --dry-run returned error: %v\noutput: %s", err, got)
	}
	if !strings.Contains(got, "rate_limited") {
		t.Errorf("expected pacing mode in dry-run output, got: %q", got)
	}
	if !strings.Contains(got, "https://example.com") {
		t.Errorf("expected target URL in dry-run output, got: %q", got)
	}
}

// TestIntegrationCmd_Generate_TargetsFile verifies that generate --targets-file
// emits valid YAML to stdout containing a targets section.
func TestIntegrationCmd_Generate_TargetsFile(t *testing.T) {
	dir := t.TempDir()
	targetsPath := filepath.Join(dir, "targets.txt")
	if err := os.WriteFile(targetsPath, []byte("https://example.com http 1\nhttps://other.example.com http 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := generateCmd()
	cmd.SetArgs([]string{"--targets-file", targetsPath})

	var got string
	var err error
	got = captureStdout(t, func() {
		err = cmd.Execute()
	})
	if err != nil {
		t.Fatalf("generate --targets-file returned error: %v\noutput: %s", err, got)
	}
	if !strings.Contains(got, "targets:") {
		t.Errorf("expected 'targets:' in generated YAML, got: %q", got)
	}
	if !strings.Contains(got, "example.com") {
		t.Errorf("expected target URL in generated YAML, got: %q", got)
	}
}

// TestIntegrationCmd_Version verifies that the version command prints a version
// string without error.
func TestIntegrationCmd_Version(t *testing.T) {
	cmd := versionCmd()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version returned error: %v", err)
	}
}
