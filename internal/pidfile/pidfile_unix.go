//go:build linux || darwin || freebsd

package pidfile

import (
	"errors"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func ensureDefaultDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("creating private PID directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspecting private PID directory: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("private PID directory is not a real directory")
	}
	if int64(stat.Uid) != int64(os.Geteuid()) {
		return errors.New("private PID directory has unsafe ownership")
	}
	if err := os.Chmod(path, 0o700); err != nil { //nolint:gosec // owner-only directory requires execute permission
		return fmt.Errorf("securing private PID directory: %w", err)
	}
	return nil
}

func createLocked(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := validateFile(file); err != nil {
		cleanupCreatedFile(file, path)
		return nil, err
	}
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		cleanupCreatedFile(file, path)
		return nil, err
	}
	return file, nil
}

func openValidated(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	if err := validateFile(file); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func validateFile(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("PID record is not a regular file")
	}
	if int64(stat.Uid) != int64(os.Geteuid()) {
		return errors.New("PID record is not owned by the current user")
	}
	if stat.Mode&0o077 != 0 || stat.Mode&0o600 != 0o600 {
		return errors.New("PID record has unsafe permissions")
	}
	if stat.Nlink != 1 {
		return errors.New("PID record has multiple hard links")
	}
	return nil
}

func lockedByOwner(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_SH|unix.LOCK_NB)
	if err == nil {
		if unlockErr := unix.Flock(int(file.Fd()), unix.LOCK_UN); unlockErr != nil {
			return false, unlockErr
		}
		return false, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return true, nil
	}
	return false, err
}

func unlock(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func removeOwnedFile(_ *os.File, path string) error {
	return os.Remove(path)
}
