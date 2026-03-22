package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
)

// writePIDFile writes pid to a temp file and returns the path.
func writePIDFile(t *testing.T, pid int) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "sendit.pid")
	if err := os.WriteFile(f, []byte(fmt.Sprintf("%d", pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// --- stopCmd ---

func TestStopCmd_MissingPIDFile(t *testing.T) {
	cmd := stopCmd()
	cmd.SetArgs([]string{"--pid-file", "/tmp/sendit-no-such-file.pid"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing PID file, got nil")
	}
}

func TestStopCmd_InvalidPIDFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(f, []byte("not-a-number"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := stopCmd()
	cmd.SetArgs([]string{"--pid-file", f})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid PID, got nil")
	}
}

func TestStopCmd_SendsSIGTERM(t *testing.T) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	defer signal.Stop(ch)

	pid := os.Getpid()
	cmd := stopCmd()
	cmd.SetArgs([]string{"--pid-file", writePIDFile(t, pid)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stopCmd returned error: %v", err)
	}

	select {
	case sig := <-ch:
		if sig != syscall.SIGTERM {
			t.Errorf("got signal %v, want SIGTERM", sig)
		}
	case <-time.After(time.Second):
		t.Error("SIGTERM not received within 1s")
	}
}

// --- reloadCmd ---

func TestReloadCmd_MissingPIDFile(t *testing.T) {
	cmd := reloadCmd()
	cmd.SetArgs([]string{"--pid-file", "/tmp/sendit-no-such-file.pid"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for missing PID file, got nil")
	}
}

func TestReloadCmd_InvalidPIDFile(t *testing.T) {
	f := filepath.Join(t.TempDir(), "bad.pid")
	if err := os.WriteFile(f, []byte("not-a-number"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := reloadCmd()
	cmd.SetArgs([]string{"--pid-file", f})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for invalid PID, got nil")
	}
}

func TestReloadCmd_SendsSIGHUP(t *testing.T) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGHUP)
	defer signal.Stop(ch)

	pid := os.Getpid()
	var out bytes.Buffer
	cmd := reloadCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--pid-file", writePIDFile(t, pid)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("reloadCmd returned error: %v", err)
	}

	select {
	case sig := <-ch:
		if sig != syscall.SIGHUP {
			t.Errorf("got signal %v, want SIGHUP", sig)
		}
	case <-time.After(time.Second):
		t.Error("SIGHUP not received within 1s")
	}

	want := fmt.Sprintf("Sent reload signal to pid %d\n", pid)
	if got := out.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

// --- startCmd flags ---

// TestStartCmd_CaptureFlag verifies that the --capture flag is registered on
// startCmd. If it is accidentally removed, sendit start --capture <file> will
// silently reject the flag and the PCAP file will never be written.
func TestStartCmd_CaptureFlag(t *testing.T) {
	cmd := startCmd()
	f := cmd.Flags().Lookup("capture")
	if f == nil {
		t.Fatal("--capture flag not registered on startCmd; users will see 'unknown flag: --capture'")
	}
	if f.DefValue != "" {
		t.Errorf("--capture default = %q, want empty string", f.DefValue)
	}
}

// TestStartCmd_DurationFlag verifies --duration is registered on startCmd.
func TestStartCmd_DurationFlag(t *testing.T) {
	cmd := startCmd()
	if f := cmd.Flags().Lookup("duration"); f == nil {
		t.Fatal("--duration flag not registered on startCmd")
	}
}

// TestStartCmd_BurstRequiresDuration verifies that starting with pacing.mode=burst
// but no --duration returns a clear error rather than running indefinitely.
func TestStartCmd_BurstRequiresDuration(t *testing.T) {
	// Write a minimal burst config to a temp file.
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "burst.yaml")
	cfgContent := `
pacing:
  mode: burst
limits:
  max_workers: 1
  max_browser_workers: 1
  cpu_threshold_pct: 80
  memory_threshold_mb: 512
rate_limits:
  default_rps: 1
backoff:
  initial_ms: 100
  max_ms: 1000
  multiplier: 2.0
  max_attempts: 1
targets:
  - url: "https://example.com"
    weight: 1
    type: http
`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := startCmd()
	cmd.SetArgs([]string{"--config", cfgPath})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when burst mode used without --duration, got nil")
	}
	if !containsAny(err.Error(), "--duration", "burst") {
		t.Errorf("error message should mention --duration or burst, got: %v", err)
	}
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// --- exportCmd registration and flags ---

// TestRootCmd_ExportCommandRegistered verifies that exportCmd is added to
// rootCmd in init(). If the AddCommand call is accidentally removed, users
// will see 'unknown command "export"'.
func TestRootCmd_ExportCommandRegistered(t *testing.T) {
	for _, sub := range rootCmd.Commands() {
		if sub.Name() == "export" {
			return
		}
	}
	t.Fatal("export command not registered in rootCmd; users will see 'unknown command \"export\"'")
}

// TestExportCmd_PCAPFlag verifies --pcap is registered on exportCmd.
func TestExportCmd_PCAPFlag(t *testing.T) {
	cmd := exportCmd()
	if f := cmd.Flags().Lookup("pcap"); f == nil {
		t.Fatal("--pcap flag not registered on exportCmd")
	}
}

// TestExportCmd_OutputFlag verifies --output is registered on exportCmd.
func TestExportCmd_OutputFlag(t *testing.T) {
	cmd := exportCmd()
	if f := cmd.Flags().Lookup("output"); f == nil {
		t.Fatal("--output flag not registered on exportCmd")
	}
}

// TestExportCmd_PCAPRequired verifies that running export without --pcap
// returns a descriptive error rather than a panic or silent no-op.
func TestExportCmd_PCAPRequired(t *testing.T) {
	cmd := exportCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when --pcap is omitted, got nil")
	}
}

// --- statusCmd ---

func TestStatusCmd_MissingPIDFile(t *testing.T) {
	// statusCmd treats a missing PID file as "not running" — no error returned.
	cmd := statusCmd()
	cmd.SetArgs([]string{"--pid-file", "/tmp/sendit-no-such-file.pid"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("statusCmd returned unexpected error: %v", err)
	}
}

func TestStatusCmd_RunningProcess(t *testing.T) {
	pid := os.Getpid()
	cmd := statusCmd()
	cmd.SetArgs([]string{"--pid-file", writePIDFile(t, pid)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("statusCmd returned error for live process: %v", err)
	}
}

func TestStatusCmd_DeadProcess(t *testing.T) {
	// PID 0 is never a valid user process; Signal(0) on it returns an error,
	// which statusCmd treats as "not running" without returning an error itself.
	cmd := statusCmd()
	cmd.SetArgs([]string{"--pid-file", writePIDFile(t, 99999999)})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("statusCmd returned unexpected error for dead process: %v", err)
	}
}

// --- detectProbeType ---

func TestDetectProbeType(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com", "http"},
		{"http://example.com", "http"},
		{"wss://echo.websocket.org", "websocket"},
		{"ws://localhost:8080", "websocket"},
		{"example.com", "dns"},
		{"", "dns"},
	}
	for _, tc := range cases {
		if got := detectProbeType(tc.input); got != tc.want {
			t.Errorf("detectProbeType(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// --- probeRcodeLabel ---

func TestProbeRcodeLabel(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{200, "NOERROR"},
		{404, "NXDOMAIN"},
		{403, "REFUSED"},
		{503, "SERVFAIL"},
		{500, "RCODE_500"},
		{0, "RCODE_0"},
	}
	for _, tc := range cases {
		if got := probeRcodeLabel(tc.status); got != tc.want {
			t.Errorf("probeRcodeLabel(%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// --- probeFormatBytes ---

func TestProbeFormatBytes(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{2048, "2.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{2 * 1024 * 1024, "2.0 MB"},
	}
	for _, tc := range cases {
		if got := probeFormatBytes(tc.n); got != tc.want {
			t.Errorf("probeFormatBytes(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// --- isConnRefused ---

func TestIsConnRefused(t *testing.T) {
	if !isConnRefused(errors.New("dial tcp: connection refused")) {
		t.Error("expected true for 'connection refused' error")
	}
	if isConnRefused(errors.New("context deadline exceeded")) {
		t.Error("expected false for non-connection-refused error")
	}
}

// captureStdout redirects os.Stdout via a pipe, calls fn, then restores it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	_ = r.Close()
	return buf.String()
}

// --- probeSummary ---

func TestProbeSummary_NoSuccess(t *testing.T) {
	out := captureStdout(t, func() {
		probeSummary("example.com", 3, 0, 0, 0, 0)
	})
	if !strings.Contains(out, "3 sent") {
		t.Errorf("expected '3 sent' in output, got: %q", out)
	}
	if !strings.Contains(out, "0 ok") {
		t.Errorf("expected '0 ok' in output, got: %q", out)
	}
	if strings.Contains(out, "latency") {
		t.Errorf("expected no latency line when success=0, got: %q", out)
	}
}

func TestProbeSummary_WithSuccess(t *testing.T) {
	out := captureStdout(t, func() {
		probeSummary("example.com", 3, 2,
			50*time.Millisecond, 150*time.Millisecond, 200*time.Millisecond)
	})
	if !strings.Contains(out, "3 sent") {
		t.Errorf("expected '3 sent' in output, got: %q", out)
	}
	if !strings.Contains(out, "latency") {
		t.Errorf("expected latency line when success>0, got: %q", out)
	}
}

// --- pinchSummary ---

func TestPinchSummary_NoOpen(t *testing.T) {
	out := captureStdout(t, func() {
		pinchSummary("example.com:80", 3, 0, 0, 0, 0)
	})
	if !strings.Contains(out, "3 sent") {
		t.Errorf("expected '3 sent' in output, got: %q", out)
	}
	if !strings.Contains(out, "0 open") {
		t.Errorf("expected '0 open' in output, got: %q", out)
	}
	if strings.Contains(out, "latency") {
		t.Errorf("expected no latency line when open=0, got: %q", out)
	}
}

func TestPinchSummary_WithOpen(t *testing.T) {
	out := captureStdout(t, func() {
		pinchSummary("example.com:80", 3, 2,
			10*time.Millisecond, 100*time.Millisecond, 110*time.Millisecond)
	})
	if !strings.Contains(out, "latency") {
		t.Errorf("expected latency line when open>0, got: %q", out)
	}
}

// --- printDryRun ---

func makeDryRunConfig(mode string) *config.Config {
	cfg := &config.Config{}
	cfg.Targets = []config.TargetConfig{
		{URL: "https://example.com", Type: "http", Weight: 10},
		{URL: "example.com", Type: "dns", Weight: 3},
	}
	cfg.Pacing.Mode = mode
	cfg.Pacing.MinDelayMs = 800
	cfg.Pacing.MaxDelayMs = 8000
	cfg.Pacing.RequestsPerMinute = 60
	cfg.Limits.MaxWorkers = 4
	cfg.Limits.MaxBrowserWorkers = 1
	cfg.Limits.CPUThresholdPct = 60
	cfg.Limits.MemoryThresholdMB = 512
	return cfg
}

func TestPrintDryRun_HumanMode(t *testing.T) {
	cfg := makeDryRunConfig("human")
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 0)
	})
	if !strings.Contains(out, "human") {
		t.Errorf("expected 'human' pacing in output, got: %q", out)
	}
	if !strings.Contains(out, "https://example.com") {
		t.Errorf("expected target URL in output, got: %q", out)
	}
}

func TestPrintDryRun_RateLimitedMode(t *testing.T) {
	cfg := makeDryRunConfig("rate_limited")
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 0)
	})
	if !strings.Contains(out, "rate_limited") {
		t.Errorf("expected 'rate_limited' in output, got: %q", out)
	}
}

func TestPrintDryRun_ScheduledMode(t *testing.T) {
	cfg := makeDryRunConfig("scheduled")
	cfg.Pacing.Schedule = []config.ScheduleEntry{
		{Cron: "0 9 * * 1-5", DurationMinutes: 480, RequestsPerMinute: 30},
	}
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 0)
	})
	if !strings.Contains(out, "scheduled") {
		t.Errorf("expected 'scheduled' in output, got: %q", out)
	}
	if !strings.Contains(out, "0 9 * * 1-5") {
		t.Errorf("expected cron expression in output, got: %q", out)
	}
}

func TestPrintDryRun_BurstMode(t *testing.T) {
	cfg := makeDryRunConfig("burst")
	cfg.Pacing.RampUpS = 30
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 60*time.Second)
	})
	if !strings.Contains(out, "burst") {
		t.Errorf("expected 'burst' in output, got: %q", out)
	}
	if !strings.Contains(out, "30s") {
		t.Errorf("expected ramp_up duration in output, got: %q", out)
	}
}

func TestPrintDryRun_BurstMode_NoRampUp(t *testing.T) {
	cfg := makeDryRunConfig("burst")
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 0)
	})
	if !strings.Contains(out, "none") {
		t.Errorf("expected 'none' ramp_up in output, got: %q", out)
	}
	if !strings.Contains(out, "unlimited") {
		t.Errorf("expected 'unlimited' duration in output, got: %q", out)
	}
}

func TestPrintDryRun_UnknownMode(t *testing.T) {
	cfg := makeDryRunConfig("foobar")
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 0)
	})
	if !strings.Contains(out, "foobar") {
		t.Errorf("expected unknown mode in output, got: %q", out)
	}
}

func TestPrintDryRun_EmptyTargets(t *testing.T) {
	cfg := makeDryRunConfig("human")
	cfg.Targets = nil
	out := captureStdout(t, func() {
		printDryRun("config/test.yaml", cfg, 0)
	})
	if !strings.Contains(out, "Targets (0)") {
		t.Errorf("expected 'Targets (0)' in output, got: %q", out)
	}
}
