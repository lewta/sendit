//go:build windows

package main

import (
	"errors"

	"github.com/lewta/sendit/internal/pidfile"
	"golang.org/x/sys/windows"
)

func stopProcess(*pidfile.Handle) error {
	return errors.New("graceful stop is not supported on Windows; stop the foreground process")
}

func reloadProcess(*pidfile.Handle) error {
	return errors.New("reload is not supported on Windows; restart the process")
}

func processAlive(record *pidfile.Handle) error {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(record.PID()))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return errProcessNotRunning
		}
		return err
	}
	defer windows.CloseHandle(handle) //nolint:errcheck
	if err := record.CheckActive(); err != nil {
		if errors.Is(err, pidfile.ErrInactive) {
			return errProcessNotRunning
		}
		return err
	}
	status, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		return err
	}
	switch status {
	case uint32(windows.WAIT_TIMEOUT):
		return nil
	case uint32(windows.WAIT_OBJECT_0):
		return errProcessNotRunning
	default:
		return errors.New("unexpected process wait status")
	}
}
