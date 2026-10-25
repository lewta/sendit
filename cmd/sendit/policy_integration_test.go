//go:build integration && !windows

package main

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The never-active cron keeps the real engine paused: no target connection is
// made. This exercises the actual command, signal handler, and engine reload.
func TestIntegrationPolicySIGHUP(t *testing.T) {
	const base = policyConfig + "pacing: {mode: scheduled, schedule: [{cron: '0 0 31 2 *', duration_minutes: 1, requests_per_minute: 1}]}\ndaemon: {log_format: json}\n"
	path := writePolicyConfig(t, base)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestIntegrationPolicyChild$")
	child.Env = append(os.Environ(), "SENDIT_POLICY_CHILD_CONFIG="+path)
	reader, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = child.Stdout
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() })
	lines := make(chan string, 64)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	waitFor := func(marker string) {
		t.Helper()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("child stopped before %q", marker)
				}
				if strings.Contains(line, marker) {
					return
				}
				if strings.Contains(line, "dispatching task") {
					t.Fatal("inactive schedule dispatched")
				}
			case <-ctx.Done():
				t.Fatalf("waiting for %q: %v", marker, ctx.Err())
			}
		}
	}
	waitFor("engine started")
	for _, step := range []struct{ request, marker string }{
		{"capability_requests: {env_names: [A]}\n", "config reloaded"},
		{"", "config reloaded"},
		{"capability_requests: {env_names: [B]}\n", "environment variable denied: B"},
		{"policy: {mode: trusted}\n", "authority fields are operator-only"},
		{"capability_requests: {env_names: []}\n", "config reloaded"},
		{"capability_requests: {env_names: [A]}\n", "environment variable denied: A"},
	} {
		if err := os.WriteFile(path, []byte(base+step.request), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := child.Process.Signal(syscall.SIGHUP); err != nil {
			t.Fatal(err)
		}
		waitFor(step.marker)
	}
	if err := child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	waitFor("engine stopped")
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationPolicyChild(t *testing.T) {
	path := os.Getenv("SENDIT_POLICY_CHILD_CONFIG")
	if path == "" {
		t.Skip("subprocess signal fixture")
	}
	cmd := startCmd()
	cmd.SetArgs([]string{"--config", path, "--foreground", "--config-trust", "restricted", "--allow-env", "A", "--allow-env", "B", "--log-level", "debug", "--duration", "15s"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}
