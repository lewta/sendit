package main

import (
	"bytes"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"
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
