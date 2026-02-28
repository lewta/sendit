package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "sendit",
	Short: "Realistic web traffic generator",
	Long: `sendit simulates realistic user web traffic across HTTP, headless
browser, DNS, and WebSocket protocols.

Targets are defined in a YAML config file under 'targets' (inline) and/or
loaded from a plain-text file via 'targets_file'. Both can be used together.

Use 'sendit validate' to check a config before running.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(validateCmd())
}

// --- start ---

func startCmd() *cobra.Command {
	var (
		cfgPath    string
		foreground bool
		logLevel   string
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the traffic generator",
		Long: `Start the traffic generator engine.

Targets can be defined inline in the config YAML under 'targets:',
loaded from a plain-text file via 'targets_file:', or both combined.

targets_file format (one entry per line):
  <url> <type> [weight]

  url     Full URL (https://, wss://) or bare hostname for dns targets
  type    http | browser | dns | websocket
  weight  Optional positive integer (default: target_defaults.weight)
  #       Lines beginning with '#' and blank lines are ignored

Example targets_file:
  https://example.com  http  5
  example.com          dns

Default field values for file-loaded targets (method, timeout, resolver,
etc.) are configured under 'target_defaults:' in the YAML.

The engine shuts down gracefully on SIGINT or SIGTERM, waiting for all
in-flight requests to complete before exiting.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			// CLI flag overrides config log level.
			lvl := cfg.Daemon.LogLevel
			if logLevel != "" {
				lvl = logLevel
			}
			initLogger(lvl, cfg.Daemon.LogFormat)

			if !foreground {
				if err := writePID(cfg.Daemon.PIDFile); err != nil {
					log.Warn().Err(err).Msg("could not write PID file")
				}
				defer os.Remove(cfg.Daemon.PIDFile) //nolint:errcheck
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			var m *metrics.Metrics
			if cfg.Metrics.Enabled {
				m = metrics.New()
				go m.ServeHTTP(ctx, cfg.Metrics.PrometheusPort)
			} else {
				m = metrics.Noop()
			}

			eng, err := engine.New(cfg, m)
			if err != nil {
				return fmt.Errorf("creating engine: %w", err)
			}

			eng.Run(ctx)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config/example.yaml", "Path to YAML config file")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Skip writing the PID file (process always runs in foreground)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Override log level (debug|info|warn|error)")

	return cmd
}

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

// --- validate ---

func validateCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a config file",
		Long: `Parse and validate a config file without starting the engine.

Checks all config fields, pacing modes, concurrency limits, per-target
settings, and backoff parameters.

If 'targets_file' is set in the config, that file is also read and parsed
as part of validation — a missing file, malformed line, unknown driver
type, or invalid weight is reported here before any traffic is sent.

Exits 0 and prints "config valid" on success.
Exits non-zero and prints the validation error on failure.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			fmt.Println("config valid")
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config/example.yaml", "Path to YAML config file")
	return cmd
}

// --- helpers ---

func initLogger(level, format string) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(lvl)

	if format == "text" {
		log.Logger = log.Output(zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: time.RFC3339,
		})
	}
}

func writePID(path string) error {
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
