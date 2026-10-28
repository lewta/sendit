//go:build darwin || freebsd

package main

import (
	"errors"
	"os"
	"syscall"

	"github.com/lewta/sendit/internal/pidfile"
)

func stopProcess(handle *pidfile.Handle) error   { return signalProcess(handle, syscall.SIGTERM) }
func reloadProcess(handle *pidfile.Handle) error { return signalProcess(handle, syscall.SIGHUP) }
func processAlive(handle *pidfile.Handle) error  { return signalProcess(handle, syscall.Signal(0)) }

func signalProcess(handle *pidfile.Handle, signal os.Signal) error {
	if err := handle.CheckActive(); err != nil {
		if errors.Is(err, pidfile.ErrInactive) {
			return errProcessNotRunning
		}
		return err
	}
	process, err := os.FindProcess(handle.PID())
	if err != nil {
		return err
	}
	if err := process.Signal(signal); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return errProcessNotRunning
		}
		return err
	}
	return nil
}
