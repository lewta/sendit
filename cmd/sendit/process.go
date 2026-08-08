package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/spf13/cobra"
)

// --- stop ---

func stopCmd() *cobra.Command {
	var pidFile string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a running traffic generator daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := readPID(pidFile)
			if err != nil {
				return fmt.Errorf("reading PID file %s: %w", pidFile, err)
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("finding process %d: %w", pid, err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("sending SIGTERM to %d: %w", pid, err)
			}

			fmt.Printf("Sent SIGTERM to process %d\n", pid)
			return nil
		},
	}

	cmd.Flags().StringVar(&pidFile, "pid-file", "/tmp/sendit.pid", "Path to PID file")
	return cmd
}

// --- reload ---

func reloadCmd() *cobra.Command {
	var pidFile string

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
			pid, err := readPID(pidFile)
			if err != nil {
				return fmt.Errorf("reading PID file %s: %w", pidFile, err)
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("finding process %d: %w", pid, err)
			}

			if err := proc.Signal(syscall.SIGHUP); err != nil {
				return fmt.Errorf("sending SIGHUP to pid %d: %w", pid, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Sent reload signal to pid %d\n", pid)
			return nil
		},
	}

	cmd.Flags().StringVar(&pidFile, "pid-file", "/tmp/sendit.pid", "Path to PID file")
	return cmd
}

// --- status ---

func statusCmd() *cobra.Command {
	var pidFile string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check whether the traffic generator daemon is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := readPID(pidFile)
			if err != nil {
				fmt.Printf("Not running (no PID file at %s)\n", pidFile)
				return nil
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				fmt.Printf("Not running (process %d not found)\n", pid)
				return nil
			}

			// Signal 0 checks if the process is alive without killing it.
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				fmt.Printf("Not running (process %d: %v)\n", pid, err)
				return nil
			}

			fmt.Printf("Running (PID %d)\n", pid)
			return nil
		},
	}

	cmd.Flags().StringVar(&pidFile, "pid-file", "/tmp/sendit.pid", "Path to PID file")
	return cmd
}
