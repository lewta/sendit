//go:build !windows

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/pidfile"
	"golang.org/x/sys/unix"
)

func TestWritePIDRejectsSymlinkWithoutTouchingTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	pidPath := filepath.Join(dir, "sendit.pid")
	const original = "preserve-me"
	if err := os.WriteFile(target, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, pidPath); err != nil {
		t.Fatal(err)
	}

	owner, err := pidfile.Acquire(pidPath)
	if owner != nil {
		_ = owner.Close()
	}
	if err == nil {
		t.Error("writePID followed a symbolic link")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != original {
		t.Errorf("symlink target = %q, want unchanged %q", got, original)
	}
}

func TestReadPIDRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	pidPath := filepath.Join(dir, "sendit.pid")
	if err := os.WriteFile(target, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, pidPath); err != nil {
		t.Fatal(err)
	}

	if handle, err := pidfile.OpenActive(pidPath); err == nil {
		_ = handle.Close()
		t.Fatal("OpenActive followed a symbolic link")
	}
}

func TestPIDSignalTargetHelper(t *testing.T) {
	if os.Getenv("SENDIT_PID_SIGNAL_HELPER") != "1" {
		return
	}
	select {}
}

func TestStopRejectsUnsafePIDMetadataBeforeSignaling(t *testing.T) {
	assertStopRejectsPIDRecordBeforeSignaling(t, 0o666, "group/world-writable PID record")
}

func TestStopRejectsUnboundPIDRecordBeforeSignaling(t *testing.T) {
	assertStopRejectsPIDRecordBeforeSignaling(t, 0o600, "unbound legacy PID record")
}

func assertStopRejectsPIDRecordBeforeSignaling(t *testing.T, mode os.FileMode, reason string) {
	t.Helper()
	child := exec.Command(os.Args[0], "-test.run=^TestPIDSignalTargetHelper$") //nolint:gosec // fixed test binary and arguments
	child.Env = append(os.Environ(), "SENDIT_PID_SIGNAL_HELPER=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()

	pidPath := filepath.Join(t.TempDir(), "sendit.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		_ = child.Process.Kill()
		<-done
		t.Fatal(err)
	}
	if err := os.Chmod(pidPath, mode); err != nil {
		_ = child.Process.Kill()
		<-done
		t.Fatal(err)
	}

	cmd := stopCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetArgs([]string{"--pid-file", pidPath})
	err := cmd.Execute()

	exited := false
	select {
	case <-done:
		exited = true
	case <-time.After(500 * time.Millisecond):
	}
	if !exited {
		_ = child.Process.Kill()
		<-done
	}
	if err == nil {
		t.Errorf("stop accepted a %s", reason)
	}
	if exited {
		t.Errorf("stop signaled the target before rejecting a %s", reason)
	}
}

func TestStartCleanupDoesNotRemoveReplacementPIDRecord(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "sendit.pid")
	configPath := filepath.Join(dir, "config.yaml")
	configBody := fmt.Sprintf(`
pacing:
  mode: scheduled
  schedule:
    - cron: "0 0 31 2 *"
      duration_minutes: 1
      requests_per_minute: 1
targets:
  - url: "https://example.com"
    type: http
    weight: 1
daemon:
  pid_file: %q
`, pidPath)
	if err := os.WriteFile(configPath, []byte(configBody), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := startCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--config", configPath, "--duration", "1s"})
	result := make(chan error, 1)
	go func() { result <- cmd.Execute() }()

	deadline := time.After(time.Second)
	for {
		if _, err := os.Lstat(pidPath); err == nil {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("start returned before creating PID record: %v", err)
		case <-deadline:
			t.Fatal("start did not create PID record")
		case <-time.After(5 * time.Millisecond):
		}
	}

	ownedPath := pidPath + ".owned"
	if err := os.Rename(pidPath, ownedPath); err != nil {
		t.Fatal(err)
	}
	const replacement = "replacement-must-survive"
	if err := os.WriteFile(pidPath, []byte(replacement), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	got, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("replacement PID record was removed during cleanup: %v", err)
	}
	if string(got) != replacement {
		t.Fatalf("replacement PID record = %q, want %q", got, replacement)
	}
}

func TestReadPIDStopsAtContentLimitWithoutWaitingForEOF(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "sendit.pid")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	// Opening both ends in the parent avoids hanging this test when a hardened
	// reader rejects the FIFO from metadata without opening its read end.
	writer, err := os.OpenFile(fifo, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	done := make(chan error, 1)
	go func() {
		handle, err := pidfile.OpenActive(fifo)
		if handle != nil {
			_ = handle.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("OpenActive accepted a FIFO PID record")
		}
	case <-time.After(500 * time.Millisecond):
		_ = writer.Close()
		t.Fatal("OpenActive waited for FIFO content or EOF")
	}
}
