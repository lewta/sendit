package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/tui"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// --- start ---

func startCmd() *cobra.Command {
	var (
		cfgPath     string
		foreground  bool
		logLevel    string
		dryRun      bool
		capturePath string
		duration    time.Duration
		tuiFlag     bool
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

			if capturePath != "" {
				cfg.Output.PCAPFile = capturePath
			}

			// burst mode requires --duration so runs are always time-bounded.
			if cfg.Pacing.Mode == "burst" && duration == 0 {
				return fmt.Errorf("--duration is required when pacing.mode is burst (e.g. --duration 5m)")
			}

			if dryRun {
				printDryRun(cfgPath, cfg, duration)
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

			// If --duration is set, wrap the context so the engine auto-stops.
			if duration > 0 {
				var durationCancel context.CancelFunc
				ctx, durationCancel = context.WithTimeout(ctx, duration)
				defer durationCancel()
				log.Info().Dur("duration", duration).Msg("run will auto-stop after duration")
			}

			var m *metrics.Metrics
			if cfg.Metrics.Enabled {
				m = metrics.New()
				go m.ServeHTTP(ctx, cfg.Metrics.BindAddress, cfg.Metrics.PrometheusPort)
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

			if tuiFlag {
				fi, err := os.Stdout.Stat()
				isTerminal := err == nil && (fi.Mode()&os.ModeCharDevice) != 0
				if isTerminal {
					zerolog.SetGlobalLevel(zerolog.Disabled)
					st := tui.NewState()
					eng.SetObserver(st.Record)
					go eng.Run(ctx)
					return tui.Run(ctx, st, cfg)
				}
				log.Warn().Msg("--tui: stdout is not a terminal, falling back to plain output")
			}

			eng.Run(ctx)
			return nil
		},
	}

	cmd.Flags().StringVarP(&cfgPath, "config", "c", "config/example.yaml", "Path to YAML config file")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Skip writing the PID file (process always runs in foreground)")
	cmd.Flags().StringVar(&logLevel, "log-level", "", "Override log level (debug|info|warn|error)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print config summary and exit without sending any traffic")
	cmd.Flags().StringVar(&capturePath, "capture", "", "Write a synthetic PCAP file while running (e.g. capture.pcap); finalised on clean shutdown")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Auto-stop after this wall-clock duration (e.g. 5m, 30s); required when pacing.mode is burst")
	cmd.Flags().BoolVar(&tuiFlag, "tui", false, "Enable the terminal UI (requires a TTY; silently ignored otherwise)")

	return cmd
}
