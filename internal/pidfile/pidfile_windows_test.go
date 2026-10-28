//go:build windows

package pidfile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsMandatoryOwnerLockDoesNotCoverRecordBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close() //nolint:errcheck
	reader, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening active record under mandatory lock: %v", err)
	}
	if _, err := readRecord(reader); err != nil {
		_ = reader.Close()
		t.Fatalf("reading active record under mandatory lock: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	handle, err := OpenActive(path)
	if err != nil {
		t.Fatalf("opening active record under mandatory lock: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeUnlockedRecordForTest(t *testing.T, path string, record []byte) {
	t.Helper()
	file, err := createLocked(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := unlock(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := file.Write(record); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsAcquireAppliesProtectedACLAndRemovesRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := openValidated(path)
	if err != nil {
		_ = owner.Close()
		t.Fatalf("opening protected record: %v", err)
	}
	if err := file.Close(); err != nil {
		_ = owner.Close()
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("PID path after owner close: %v, want not exist", err)
	}
}

func TestWindowsAcquireDefaultCreatesProtectedDirectoryAndRecord(t *testing.T) {
	localAppData := t.TempDir()
	t.Setenv("LocalAppData", localAppData)
	path, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(localAppData, "sendit", "sendit.pid")
	if path != want {
		t.Fatalf("DefaultPath = %q, want %q", path, want)
	}
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := openValidated(path)
	if err != nil {
		_ = owner.Close()
		t.Fatalf("opening protected default record: %v", err)
	}
	if err := file.Close(); err != nil {
		_ = owner.Close()
		t.Fatal(err)
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default PID path after owner close: %v, want not exist", err)
	}
}

func TestWindowsDescriptorValidationAcceptsSafeControlNormalization(t *testing.T) {
	expected, err := currentUserSecurityDescriptor(true)
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := windows.SecurityDescriptorFromString(strings.Replace(expected.String(), "D:P", "D:PAI", 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsDescriptor(normalized, expected); err != nil {
		t.Fatalf("safe descriptor normalization rejected: %v", err)
	}
}

func TestWindowsDescriptorValidationRejectsAdditionalPrincipal(t *testing.T) {
	expected, err := currentUserSecurityDescriptor(true)
	if err != nil {
		t.Fatal(err)
	}
	broad, err := windows.SecurityDescriptorFromString(strings.Replace(expected.String(), "D:P", "D:P(A;OICI;FR;;;WD)", 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateWindowsDescriptor(broad, expected); err == nil {
		t.Fatal("descriptor with an additional principal was accepted")
	}
}

func TestWindowsOpenActiveRejectsHardLinkedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	owner, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close() //nolint:errcheck
	if err := os.Link(path, path+".alias"); err != nil {
		t.Skipf("creating Windows hard-link test fixture: %v", err)
	}
	if handle, err := OpenActive(path); err == nil {
		_ = handle.Close()
		t.Fatal("OpenActive accepted a hard-linked PID record")
	}
}

func TestWindowsInactiveProbeDoesNotConflictWithConcurrentReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sendit.pid")
	writeUnlockedRecordForTest(t, path, []byte("sendit-pid-v1 1234\n"))
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
	if err := lockWindows(first, false); err != nil {
		t.Fatal(err)
	}
	defer unlock(first) //nolint:errcheck

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
