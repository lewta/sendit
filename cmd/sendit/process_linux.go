//go:build linux

package main

import (
	"errors"

	"github.com/lewta/sendit/internal/pidfile"
	"golang.org/x/sys/unix"
)

func stopProcess(handle *pidfile.Handle) error   { return signalPIDFD(handle, unix.SIGTERM) }
func reloadProcess(handle *pidfile.Handle) error { return signalPIDFD(handle, unix.SIGHUP) }
func processAlive(handle *pidfile.Handle) error  { return signalPIDFD(handle, 0) }

func signalPIDFD(handle *pidfile.Handle, signal unix.Signal) error {
	fd, err := unix.PidfdOpen(handle.PID(), 0)
	if err != nil {
		if errors.Is(err, unix.ESRCH) {
			return errProcessNotRunning
		}
		return err
	}
	defer unix.Close(fd) //nolint:errcheck
	if err := handle.CheckActive(); err != nil {
		if errors.Is(err, pidfile.ErrInactive) {
			return errProcessNotRunning
		}
		return err
	}
	if err := unix.PidfdSendSignal(fd, signal, nil, 0); err != nil {
		if errors.Is(err, unix.ESRCH) {
			return errProcessNotRunning
		}
		return err
	}
	return nil
}
