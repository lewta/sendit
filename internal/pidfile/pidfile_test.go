package pidfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAcquireOwnsCanonicalRecordUntilClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}

	handle, err := OpenActive(path)
	if err != nil {
		_ = owner.Close()
		t.Fatal(err)
	}
	if got := handle.PID(); got != os.Getpid() {
		t.Errorf("PID = %d, want %d", got, os.Getpid())
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}

	// Keep a second validated handle across owner shutdown to prove callers can
	// recheck the lifetime lock immediately before a process operation.
	handle, err = OpenActive(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if err := handle.CheckActive(); !errors.Is(err, ErrInactive) {
		t.Fatalf("CheckActive after owner close = %v, want ErrInactive", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PID path after owner close: %v, want not exist", err)
	}
}

func TestOpenActiveRejectsUnlockedStructuredRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	writeUnlockedRecordForTest(t, path, []byte("sendit-pid-v1 1234\n"))
	if handle, err := OpenActive(path); !errors.Is(err, ErrInactive) {
		if handle != nil {
			_ = handle.Close()
		}
		t.Fatalf("OpenActive error = %v, want ErrInactive", err)
	}
}

func TestDefaultPathIsPerUserAndAbsolute(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("DefaultPath = %q, want absolute", path)
	}
	if filepath.Base(path) != "sendit.pid" || filepath.Base(filepath.Dir(path)) != "sendit" {
		t.Fatalf("DefaultPath = %q, want sendit/sendit.pid", path)
	}
}

func TestReadRecordRejectsPIDValuesOutsideSigned32BitRange(t *testing.T) {
	for _, value := range []string{"2147483648", "4294967295", "4294967296"} {
		t.Run(value, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "sendit.pid")
			if err := os.WriteFile(path, []byte(recordPrefix+value+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close() //nolint:errcheck
			if pid, err := readRecord(file); err == nil || !strings.Contains(err.Error(), "invalid process identifier") {
				t.Fatalf("readRecord(%q) = %d, %v; want invalid process identifier", value, pid, err)
			}
		})
	}
}
