package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/lewta/sendit/internal/pidfile"
	"github.com/spf13/cobra"
)

var errProcessNotRunning = errors.New("process is not running")

// --- stop ---

func stopCmd() *cobra.Command {
	pidFile, defaultErr := pidfile.DefaultPath()

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a running traffic generator daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if defaultErr != nil && pidFile == "" {
				return defaultErr
			}
			handle, err := pidfile.OpenActive(pidFile)
			if err != nil {
				return fmt.Errorf("reading PID file %s: %w", pidFile, err)
			}
			defer handle.Close() //nolint:errcheck
			pid := handle.PID()
			if err := stopProcess(handle); err != nil {
				return fmt.Errorf("stopping process %d: %w", pid, err)
			}

			fmt.Printf("Sent SIGTERM to process %d\n", pid)
			return nil
		},
	}

	cmd.Flags().StringVar(&pidFile, "pid-file", pidFile, "Path to PID file")
	return cmd
}

// --- reload ---

func reloadCmd() *cobra.Command {
	pidFile, defaultErr := pidfile.DefaultPath()

	cmd := &cobra.Command{
		Use:   "reload",
		Short: "Reload the config of a running sendit daemon",
		Long: `Send SIGHUP to a running sendit daemon to reload its configuration.

Targets, rate limits, backoff settings, and pacing parameters are reloaded
atomically with no dropped requests. Changes to pacing mode, worker count,
CPU/memory limits, or output settings require a full restart.

Note: SIGHUP is not available on Windows. Use a full restart to reload
config on Windows.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if defaultErr != nil && pidFile == "" {
				return defaultErr
			}
			handle, err := pidfile.OpenActive(pidFile)
			if err != nil {
				return fmt.Errorf("reading PID file %s: %w", pidFile, err)
			}
			defer handle.Close() //nolint:errcheck
			pid := handle.PID()
			if err := reloadProcess(handle); err != nil {
				return fmt.Errorf("reloading process %d: %w", pid, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Sent reload signal to pid %d\n", pid)
			return nil
		},
	}

	cmd.Flags().StringVar(&pidFile, "pid-file", pidFile, "Path to PID file")
	return cmd
}

// --- status ---

func statusCmd() *cobra.Command {
	pidFile, defaultErr := pidfile.DefaultPath()

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check whether the traffic generator daemon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			if defaultErr != nil && pidFile == "" {
				return defaultErr
			}
			handle, err := pidfile.OpenActive(pidFile)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Fprintf(cmd.OutOrStdout(), "Not running (no PID file at %s)\n", pidFile)
					return nil
				}
				return fmt.Errorf("reading PID file %s: %w", pidFile, err)
			}
			defer handle.Close() //nolint:errcheck
			pid := handle.PID()
			return reportProcessStatus(cmd.OutOrStdout(), pid, processAlive(handle))
		},
	}

	cmd.Flags().StringVar(&pidFile, "pid-file", pidFile, "Path to PID file")
	return cmd
}

func reportProcessStatus(out io.Writer, pid int, statusErr error) error {
	if statusErr == nil {
		fmt.Fprintf(out, "Running (PID %d)\n", pid)
		return nil
	}
	if errors.Is(statusErr, errProcessNotRunning) {
		fmt.Fprintf(out, "Not running (process %d)\n", pid)
		return nil
	}
	return fmt.Errorf("checking process %d: %w", pid, statusErr)
}
