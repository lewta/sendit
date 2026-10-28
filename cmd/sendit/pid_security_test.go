package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lewta/sendit/internal/pidfile"
)

func TestWritePIDRefusesToOverwriteExistingRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	const original = "4242"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	owner, err := pidfile.Acquire(path)
	if owner != nil {
		_ = owner.Close()
	}
	if err == nil {
		t.Error("writePID overwrote an existing PID record")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("existing PID record = %q, want unchanged %q", got, original)
	}
}

func TestStartFailsClosedWhenPIDRecordExists(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "sendit.pid")
	const original = "existing-record-must-survive"
	if err := os.WriteFile(pidPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "config.yaml")
	configBody := fmt.Sprintf("targets: [{url: https://example.com, type: http, weight: 1}]\ndaemon: {pid_file: %q}\n", pidPath)
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := startCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--config", configPath, "--duration", "10ms"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "acquiring PID file") {
		t.Fatalf("start did not fail closed on the existing PID record: %v", err)
	}
	got, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("existing PID record = %q, want unchanged %q", got, original)
	}
}

func TestForegroundDryRunDoesNotRequireUserCacheDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	configBody := `
pacing:
  mode: scheduled
  schedule:
    - cron: "0 0 31 2 *"
      duration_minutes: 1
      requests_per_minute: 1
targets:
  - url: https://example.com
    type: http
    weight: 1
`
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := startCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--config", configPath, "--foreground", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("foreground dry-run with no cache directory: %v", err)
	}
}

func TestWritePIDAllowsOnlyOneConcurrentOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	start := make(chan struct{})
	type result struct {
		owner *pidfile.Owner
		err   error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			owner, err := pidfile.Acquire(path)
			results <- result{owner: owner, err: err}
		}()
	}
	close(start)

	successes := 0
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			defer result.owner.Close() //nolint:errcheck
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent PID owners = %d, want exactly 1", successes)
	}
}

func TestReadPIDRejectsNonPositiveAndNonCanonicalValues(t *testing.T) {
	for _, value := range []string{
		"0",
		"sendit-pid-v1 0\n",
		"sendit-pid-v1 -1\n",
		"sendit-pid-v1 +1\n",
		"sendit-pid-v1 01\n",
		"sendit-pid-v1 1\n2",
		"sendit-pid-v1 1",
		"sendit-pid-v1 " + strings.Repeat("9", 128) + "\n",
	} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sendit.pid")
			if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
				t.Fatal(err)
			}
			if handle, err := pidfile.OpenActive(path); err == nil {
				_ = handle.Close()
				t.Fatalf("OpenActive(%q) accepted a noncanonical record", value)
			}
		})
	}
}
