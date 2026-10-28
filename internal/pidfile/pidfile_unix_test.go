//go:build linux || darwin || freebsd

package pidfile

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func writeUnlockedRecordForTest(t *testing.T, path string, record []byte) {
	t.Helper()
	if err := os.WriteFile(path, record, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireDefaultCreatesPrivateDirectoryAndRecord(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close() //nolint:errcheck

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("PID directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("PID file mode = %o, want 600", got)
	}
}

func TestOpenActiveRejectsHardLinkedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close() //nolint:errcheck
	if err := os.Link(path, path+".alias"); err != nil {
		t.Fatal(err)
	}
	if handle, err := OpenActive(path); err == nil {
		_ = handle.Close()
		t.Fatal("OpenActive accepted a hard-linked PID record")
	}
}

func TestInactiveProbeDoesNotConflictWithConcurrentReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	if err := os.WriteFile(path, []byte("sendit-pid-v1 1234\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := openValidated(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close() //nolint:errcheck
	second, err := openValidated(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close() //nolint:errcheck
	if err := unix.Flock(int(first.Fd()), unix.LOCK_SH|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer unix.Flock(int(first.Fd()), unix.LOCK_UN) //nolint:errcheck

	active, err := lockedByOwner(second)
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("concurrent reader lock was mistaken for an active owner")
	}
	if _, err := OpenActive(path); !errors.Is(err, ErrInactive) {
		t.Fatalf("OpenActive error = %v, want ErrInactive", err)
	}
}
