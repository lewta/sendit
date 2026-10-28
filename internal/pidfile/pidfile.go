// Package pidfile owns Sendit's daemon process record lifecycle.
package pidfile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	recordPrefix   = "sendit-pid-v1 "
	maxRecordBytes = 64
	lockByteOffset = maxRecordBytes + 1
)

// ErrInactive indicates that a PID record is not held by a running Sendit owner.
var ErrInactive = errors.New("PID record is not held by a running sendit process")

// Owner retains the PID file lock for a running daemon.
type Owner struct {
	path string
	file *os.File
	info os.FileInfo
}

// Handle retains a validated PID record while a control operation executes.
type Handle struct {
	file *os.File
	pid  int
}

// DefaultPath returns the per-user PID record path.
func DefaultPath() (string, error) {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolving user cache directory: %w", err)
	}
	if cacheDir == "" {
		return "", errors.New("resolving user cache directory: empty path")
	}
	return filepath.Join(cacheDir, "sendit", "sendit.pid"), nil
}

// Acquire exclusively creates and locks a PID record for the current process.
func Acquire(path string) (*Owner, error) {
	if path == "" {
		return nil, errors.New("PID file path is empty")
	}
	defaultPath, err := DefaultPath()
	if err == nil && filepath.Clean(path) == filepath.Clean(defaultPath) {
		if err := ensureDefaultDirectory(filepath.Dir(defaultPath)); err != nil {
			return nil, err
		}
	}

	file, err := createLocked(path)
	if err != nil {
		return nil, fmt.Errorf("creating PID file: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspecting created PID file: %w", err)
	}
	owner := &Owner{path: path, file: file, info: info}
	record := recordPrefix + strconv.Itoa(os.Getpid()) + "\n"
	if _, err := io.WriteString(file, record); err != nil {
		_ = owner.removeOwnedPath()
		_ = unlock(file)
		_ = file.Close()
		return nil, fmt.Errorf("writing PID record: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = owner.removeOwnedPath()
		_ = unlock(file)
		_ = file.Close()
		return nil, fmt.Errorf("syncing PID record: %w", err)
	}
	return owner, nil
}

// OpenActive validates an active PID record and retains its handle.
func OpenActive(path string) (*Handle, error) {
	file, err := openValidated(path)
	if err != nil {
		return nil, err
	}
	pid, err := readRecord(file)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	active, err := lockedByOwner(file)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("checking PID file ownership lock: %w", err)
	}
	if !active {
		_ = file.Close()
		return nil, ErrInactive
	}
	return &Handle{file: file, pid: pid}, nil
}

// PID returns the validated process identifier.
func (h *Handle) PID() int { return h.pid }

// CheckActive confirms that the record owner still holds its lifetime lock.
func (h *Handle) CheckActive() error {
	active, err := lockedByOwner(h.file)
	if err != nil {
		return fmt.Errorf("checking PID file ownership lock: %w", err)
	}
	if !active {
		return ErrInactive
	}
	return nil
}

// Close releases the control handle.
func (h *Handle) Close() error {
	if h == nil || h.file == nil {
		return nil
	}
	err := h.file.Close()
	h.file = nil
	return err
}

// Close removes only the path still naming this owner's file, then releases the lock.
func (o *Owner) Close() error {
	if o == nil || o.file == nil {
		return nil
	}
	removeErr := o.removeOwnedPath()
	unlockErr := unlock(o.file)
	closeErr := o.file.Close()
	o.file = nil
	return errors.Join(removeErr, unlockErr, closeErr)
}

func (o *Owner) removeOwnedPath() error {
	current, err := os.Lstat(o.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("inspecting PID path during cleanup: %w", err)
	}
	if !os.SameFile(o.info, current) {
		return nil
	}
	if err := removeOwnedFile(o.file, o.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing PID file: %w", err)
	}
	return nil
}

func cleanupCreatedFile(file *os.File, path string) {
	info, err := file.Stat()
	if err == nil {
		current, pathErr := os.Lstat(path)
		if pathErr == nil && os.SameFile(info, current) {
			_ = removeOwnedFile(file, path)
		}
	}
	_ = file.Close()
}

func readRecord(file *os.File) (int, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seeking PID record: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxRecordBytes+1))
	if err != nil {
		return 0, fmt.Errorf("reading PID record: %w", err)
	}
	if len(data) > maxRecordBytes {
		return 0, fmt.Errorf("PID record exceeds %d bytes", maxRecordBytes)
	}
	text := string(data)
	if !strings.HasPrefix(text, recordPrefix) || !strings.HasSuffix(text, "\n") {
		return 0, errors.New("PID record has unsupported format")
	}
	value := strings.TrimSuffix(strings.TrimPrefix(text, recordPrefix), "\n")
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != value {
		return 0, errors.New("PID record has invalid process identifier")
	}
	return int(parsed), nil
}
