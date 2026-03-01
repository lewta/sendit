package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// Set by goreleaser via -ldflags at build time; fallback to "dev" for local builds.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "sendit",
	Short: "Realistic web traffic generator",
	Long: `sendit simulates realistic user web traffic across HTTP, headless
browser, DNS, and WebSocket protocols.

Targets are defined in a YAML config file under 'targets' (inline) and/or
loaded from a plain-text file via 'targets_file'. Both can be used together.

Use 'sendit probe <target>' to test a single endpoint interactively without
a config file — works like ping for HTTP and DNS targets.

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
	rootCmd.AddCommand(reloadCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(validateCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(probeCmd())
}

// --- probe ---

func probeCmd() *cobra.Command {
	var (
		driverType string
		interval   time.Duration
		timeout    time.Duration
		resolver   string
		recordType string
	)

	cmd := &cobra.Command{
		Use:   "probe <target>",
		Short: "Test a single endpoint in a loop (like ping for HTTP/DNS)",
		Long: `Probe an HTTP or DNS endpoint in a loop until stopped.

No config file is required. The driver type is auto-detected from the target:
  https:// or http:// prefix → http
  bare hostname              → dns

Examples:
  sendit probe https://example.com
  sendit probe example.com
  sendit probe example.com --type dns --record-type AAAA --resolver 1.1.1.1:53`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			if driverType == "" {
				driverType = detectProbeType(target)
			}
			if driverType != "http" && driverType != "dns" {
				return fmt.Errorf("probe supports http and dns targets; got type %q", driverType)
			}

			t := task.Task{
				URL:  target,
				Type: driverType,
				Config: config.TargetConfig{
					URL:    target,
					Type:   driverType,
					Weight: 1,
					HTTP: config.HTTPConfig{
						Method:   "GET",
						TimeoutS: int(timeout.Seconds()),
					},
					DNS: config.DNSConfig{
						Resolver:   resolver,
						RecordType: recordType,
					},
				},
			}

			var drv driver.Driver
			switch driverType {
			case "http":
				drv = driver.NewHTTPDriver()
			case "dns":
				drv = driver.NewDNSDriver()
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			header := fmt.Sprintf("Probing %s (http)", target)
			if driverType == "dns" {
				header = fmt.Sprintf("Probing %s (dns, %s @ %s)", target, strings.ToUpper(recordType), resolver)
			}
			fmt.Printf("\n%s — Ctrl-C to stop\n\n", header)

			var (
				total   int
				success int
				minDur  time.Duration
				maxDur  time.Duration
				sumDur  time.Duration
			)

			run := func() {
				execCtx, cancel := context.WithTimeout(ctx, timeout)
				result := drv.Execute(execCtx, t)
				cancel()

				total++
				dur := result.Duration.Round(time.Millisecond)

				if result.Error != nil {
					fmt.Printf("  ERR  %v\n", result.Error)
					return
				}

				success++
				sumDur += result.Duration
				if success == 1 || result.Duration < minDur {
					minDur = result.Duration
				}
				if result.Duration > maxDur {
					maxDur = result.Duration
				}

				if driverType == "dns" {
					fmt.Printf("  %-8s  %6s\n", probeRcodeLabel(result.StatusCode), dur)
				} else {
					fmt.Printf("  %3d  %6s  %s\n", result.StatusCode, dur, probeFormatBytes(result.BytesRead))
				}
			}

			// Fire immediately, then on each tick.
			run()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					probeSummary(target, total, success, minDur, maxDur, sumDur)
					return nil
				case <-ticker.C:
					run()
				}
			}
		},
	}

	cmd.Flags().StringVar(&driverType, "type", "", "Driver type: http|dns (auto-detected from target if omitted)")
	cmd.Flags().DurationVar(&interval, "interval", time.Second, "Delay between requests")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "Per-request timeout")
	cmd.Flags().StringVar(&resolver, "resolver", "8.8.8.8:53", "DNS resolver address (dns targets only)")
	cmd.Flags().StringVar(&recordType, "record-type", "A", "DNS record type (dns targets only)")

	return cmd
}

func detectProbeType(target string) string {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return "http"
	}
	return "dns"
}

func probeRcodeLabel(status int) string {
	switch status {
	case 200:
		return "NOERROR"
	case 404:
		return "NXDOMAIN"
	case 403:
		return "REFUSED"
	case 503:
		return "SERVFAIL"
	default:
		return fmt.Sprintf("RCODE_%d", status)
	}
}

func probeFormatBytes(n int64) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func probeSummary(target string, total, success int, minDur, maxDur, sumDur time.Duration) {
	errs := total - success
	fmt.Printf("\n--- %s ---\n", target)
	fmt.Printf("%d sent, %d ok, %d error(s)\n", total, success, errs)
	if success > 0 {
		avg := sumDur / time.Duration(success)
		fmt.Printf("min/avg/max latency: %s / %s / %s\n",
			minDur.Round(time.Millisecond),
			avg.Round(time.Millisecond),
			maxDur.Round(time.Millisecond),
		)
	}
}

// --- version ---

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("sendit %s (commit: %s, built: %s)\n", version, commit, buildDate)
		},
	}
}

// --- start ---

func startCmd() *cobra.Command {
	var (
		cfgPath    string
		foreground bool
		logLevel   string
		dryRun     bool
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
in-flight requests to complete before exiting.

Send SIGHUP to reload the config without restarting. Targets, rate limits,
backoff, and pacing are updated atomically with no dropped requests. Changes
to pacing mode or resource limits (workers, cpu, memory) require a restart.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			if dryRun {
				printDryRun(cfgPath, cfg)
				return nil
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

			// Hot-reload on SIGHUP.
			sighupCh := make(chan os.Signal, 1)
			signal.Notify(sighupCh, syscall.SIGHUP)
			go func() {
				for {
					select {
					case <-ctx.Done():
						signal.Stop(sighupCh)
						return
					case <-sighupCh:
						log.Info().Str("config", cfgPath).Msg("SIGHUP received, reloading config")
						newCfg, err := config.Load(cfgPath)
						if err != nil {
							log.Error().Err(err).Msg("hot-reload: invalid config, keeping current")
							continue
						}
						if err := eng.Reload(newCfg); err != nil {
							log.Error().Err(err).Msg("hot-reload: reload failed, keeping current")
						}
					}
				}
			}()

			eng.Run(ctx)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config/example.yaml", "Path to YAML config file")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Skip writing the PID file (process always runs in foreground)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Override log level (debug|info|warn|error)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print config summary and exit without sending any traffic")

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

// --- reload ---

func reloadCmd() *cobra.Command {
	var pidFile string

	cmd := &cobra.Command{
		Use:   "reload",
		Short: "Reload the config of a running sendit daemon",
		Long: `Send SIGHUP to a running sendit daemon to reload its configuration.

Targets, rate limits, backoff settings, and pacing parameters are reloaded
atomically with no dropped requests. Changes to pacing mode, worker count,
CPU/memory limits, or output settings require a full restart.`,
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

func printDryRun(path string, cfg *config.Config) {
	fmt.Printf("Config: %s  ✓ valid\n\n", path)

	// Compute total weight.
	totalWeight := 0
	for _, t := range cfg.Targets {
		totalWeight += t.Weight
	}

	// Sort a copy by weight descending.
	sorted := make([]config.TargetConfig, len(cfg.Targets))
	copy(sorted, cfg.Targets)
	slices.SortFunc(sorted, func(a, b config.TargetConfig) int {
		return b.Weight - a.Weight
	})

	fmt.Printf("Targets (%d):\n", len(sorted))
	fmt.Printf("  %-40s %-10s %-10s %s\n", "URL", "TYPE", "WEIGHT", "SHARE")
	for _, t := range sorted {
		share := 0.0
		if totalWeight > 0 {
			share = float64(t.Weight) / float64(totalWeight) * 100
		}
		fmt.Printf("  %-40s %-10s %-10d %.1f%%\n", t.URL, t.Type, t.Weight, share)
	}
	fmt.Printf("  Total weight: %d\n", totalWeight)
	fmt.Println()

	// Pacing.
	p := cfg.Pacing
	switch p.Mode {
	case "human":
		fmt.Printf("Pacing:\n  mode: human | delay: %dms–%dms (random uniform)\n", p.MinDelayMs, p.MaxDelayMs)
	case "rate_limited":
		rps := p.RequestsPerMinute / 60.0
		fmt.Printf("Pacing:\n  mode: rate_limited | rpm: %.0f (~%.2f rps) | jitter: ≤200ms\n", p.RequestsPerMinute, rps)
	case "scheduled":
		fmt.Printf("Pacing:\n  mode: scheduled\n")
		for i, s := range p.Schedule {
			fmt.Printf("  [%d] cron: %q  duration: %dm  rpm: %.0f\n", i, s.Cron, s.DurationMinutes, s.RequestsPerMinute)
		}
	default:
		fmt.Printf("Pacing:\n  mode: %s\n", p.Mode)
	}
	fmt.Println()

	// Limits.
	l := cfg.Limits
	fmt.Printf("Limits:\n  workers: %d (browser: %d) | cpu: %.0f%% | memory: %d MB\n",
		l.MaxWorkers, l.MaxBrowserWorkers, l.CPUThresholdPct, l.MemoryThresholdMB)
}

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
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
