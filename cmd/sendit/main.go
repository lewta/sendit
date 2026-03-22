package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/pcap"
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
a config file — works like ping for HTTP, DNS, and WebSocket targets.

Use 'sendit pinch <host:port>' to check TCP/UDP port connectivity without
a config file.

Use 'sendit generate' to generate a ready-to-use config.yaml from a
targets file, a seed URL with in-domain crawling, or your local browser
history or bookmarks — no manual config editing required.

Use 'sendit export --pcap <results.jsonl>' to convert a results file to
PCAP format for analysis in Wireshark or similar tools.

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
	rootCmd.AddCommand(pinchCmd())
	rootCmd.AddCommand(exportCmd())
	rootCmd.AddCommand(generateCmd())
}

// --- probe ---

func probeCmd() *cobra.Command {
	var (
		driverType string
		interval   time.Duration
		timeout    time.Duration
		resolver   string
		recordType string
		sendMsg    string
	)

	cmd := &cobra.Command{
		Use:   "probe <target>",
		Short: "Test a single endpoint in a loop (like ping for HTTP/DNS/WebSocket)",
		Long: `Probe an HTTP, DNS, or WebSocket endpoint in a loop until stopped.

No config file is required. The driver type is auto-detected from the target:
  https:// or http:// prefix → http
  wss:// or ws:// prefix     → websocket
  bare hostname              → dns

For WebSocket targets, each iteration connects, optionally sends a message and
waits for one reply, then closes the connection. Use --send to trigger the
send/receive round-trip measurement.

Examples:
  sendit probe https://example.com
  sendit probe example.com
  sendit probe example.com --type dns --record-type AAAA --resolver 1.1.1.1:53
  sendit probe wss://echo.example.com
  sendit probe wss://echo.example.com --send '{"type":"ping"}'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			if driverType == "" {
				driverType = detectProbeType(target)
			}
			if driverType != "http" && driverType != "dns" && driverType != "websocket" {
				return fmt.Errorf("probe supports http, dns, and websocket targets; got type %q", driverType)
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

			var header string
			switch driverType {
			case "dns":
				header = fmt.Sprintf("Probing %s (dns, %s @ %s)", target, strings.ToUpper(recordType), resolver)
			case "websocket":
				if sendMsg != "" {
					header = fmt.Sprintf("Probing %s (websocket, send+recv)", target)
				} else {
					header = fmt.Sprintf("Probing %s (websocket, connect only)", target)
				}
			default:
				header = fmt.Sprintf("Probing %s (http)", target)
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
				defer cancel()

				var (
					status int
					dur    time.Duration
					bytes  int64
					err    error
				)

				if driverType == "websocket" {
					status, dur, err = probeWS(execCtx, target, sendMsg)
				} else {
					result := drv.Execute(execCtx, t)
					status, dur, bytes, err = result.StatusCode, result.Duration, result.BytesRead, result.Error
				}

				total++
				displayDur := dur.Round(time.Millisecond)

				if err != nil {
					fmt.Printf("  ERR  %v\n", err)
					return
				}

				success++
				sumDur += dur
				if success == 1 || dur < minDur {
					minDur = dur
				}
				if dur > maxDur {
					maxDur = dur
				}

				switch driverType {
				case "dns":
					fmt.Printf("  %-8s  %6s\n", probeRcodeLabel(status), displayDur)
				case "websocket":
					fmt.Printf("  %3d  %6s\n", status, displayDur)
				default:
					fmt.Printf("  %3d  %6s  %s\n", status, displayDur, probeFormatBytes(bytes))
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

	cmd.Flags().StringVar(&driverType, "type", "", "Driver type: http|dns|websocket (auto-detected from target if omitted)")
	cmd.Flags().DurationVar(&interval, "interval", time.Second, "Delay between requests")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "Per-request timeout")
	cmd.Flags().StringVar(&resolver, "resolver", "8.8.8.8:53", "DNS resolver address (dns targets only)")
	cmd.Flags().StringVar(&recordType, "record-type", "A", "DNS record type (dns targets only)")
	cmd.Flags().StringVar(&sendMsg, "send", "", "Message to send after connecting (websocket only); waits for one reply and reports round-trip latency")

	return cmd
}

func detectProbeType(target string) string {
	switch {
	case strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://"):
		return "http"
	case strings.HasPrefix(target, "ws://") || strings.HasPrefix(target, "wss://"):
		return "websocket"
	default:
		return "dns"
	}
}

// probeWS dials a WebSocket endpoint, optionally sends sendMsg and reads one
// reply, then closes gracefully. Returns status 101 on success.
func probeWS(ctx context.Context, target, sendMsg string) (int, time.Duration, error) {
	start := time.Now()
	conn, _, err := websocket.Dial(ctx, target, nil)
	if err != nil {
		return 0, time.Since(start), fmt.Errorf("dial: %w", err)
	}
	defer conn.CloseNow() //nolint:errcheck

	if sendMsg != "" {
		if err := conn.Write(ctx, websocket.MessageText, []byte(sendMsg)); err != nil {
			return 0, time.Since(start), fmt.Errorf("send: %w", err)
		}
		if _, _, err := conn.Read(ctx); err != nil {
			return 0, time.Since(start), fmt.Errorf("recv: %w", err)
		}
	}

	conn.Close(websocket.StatusNormalClosure, "done") //nolint:errcheck,gosec
	return 101, time.Since(start), nil
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

// --- pinch ---

func pinchCmd() *cobra.Command {
	var (
		connType string
		interval time.Duration
		timeout  time.Duration
	)

	cmd := &cobra.Command{
		Use:   "pinch <host:port>",
		Short: "Check TCP/UDP port connectivity in a loop (like ping for ports)",
		Long: `Pinch a TCP or UDP port in a loop until stopped.

No config file is required. The target must be in host:port format.

Examples:
  sendit pinch example.com:80
  sendit pinch 8.8.8.8:53 --type udp
  sendit pinch localhost:8080 --interval 500ms --timeout 3s`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]

			if _, _, err := net.SplitHostPort(target); err != nil {
				return fmt.Errorf("invalid target %q: must be host:port (e.g. example.com:80)", target)
			}
			if connType != "tcp" && connType != "udp" {
				return fmt.Errorf("--type must be tcp or udp; got %q", connType)
			}

			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			fmt.Printf("\nPinching %s (%s) — Ctrl-C to stop\n\n", target, connType)

			var (
				total   int
				openCnt int
				minDur  time.Duration
				maxDur  time.Duration
				sumDur  time.Duration
			)

			run := func() {
				execCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()

				var (
					label string
					dur   time.Duration
					err   error
				)
				if connType == "tcp" {
					label, dur, err = pinchTCP(execCtx, target)
				} else {
					label, dur, err = pinchUDP(execCtx, target, timeout)
				}

				total++
				displayDur := dur.Round(time.Millisecond)

				if label == "open" {
					openCnt++
					sumDur += dur
					if openCnt == 1 || dur < minDur {
						minDur = dur
					}
					if dur > maxDur {
						maxDur = dur
					}
					fmt.Printf("  %-14s  %6s\n", label, displayDur)
				} else {
					var note string
					switch {
					case label == "open|filtered":
						note = "(no response within timeout)"
					case err != nil:
						note = err.Error()
					}
					fmt.Printf("  %-14s  %6s  %s\n", label, displayDur, note)
				}
			}

			// Fire immediately, then on each tick.
			run()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					pinchSummary(target, total, openCnt, minDur, maxDur, sumDur)
					return nil
				case <-ticker.C:
					run()
				}
			}
		},
	}

	cmd.Flags().StringVar(&connType, "type", "tcp", "Protocol type: tcp|udp")
	cmd.Flags().DurationVar(&interval, "interval", time.Second, "Delay between checks")
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "Per-check timeout")

	return cmd
}

func pinchTCP(ctx context.Context, target string) (string, time.Duration, error) {
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target)
	dur := time.Since(start)
	if err == nil {
		_ = conn.Close()
		return "open", dur, nil
	}
	if isConnRefused(err) {
		return "closed", dur, err
	}
	return "filtered", dur, err
}

func pinchUDP(ctx context.Context, target string, timeout time.Duration) (string, time.Duration, error) {
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", target)
	if err != nil {
		return "error", time.Since(start), err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	_, _ = conn.Write([]byte{}) // empty datagram triggers ICMP if port closed
	buf := make([]byte, 1)
	_, readErr := conn.Read(buf)
	dur := time.Since(start)
	if readErr == nil {
		return "open", dur, nil
	}
	if isConnRefused(readErr) {
		return "closed", dur, readErr
	}
	return "open|filtered", dur, nil // timeout — ambiguous
}

func isConnRefused(err error) bool {
	return strings.Contains(err.Error(), "connection refused")
}

func pinchSummary(target string, total, open int, minDur, maxDur, sumDur time.Duration) {
	closed := total - open
	fmt.Printf("\n--- %s ---\n", target)
	fmt.Printf("%d sent, %d open, %d closed/filtered\n", total, open, closed)
	if open > 0 {
		avg := sumDur / time.Duration(open)
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
		cfgPath     string
		foreground  bool
		logLevel    string
		dryRun      bool
		capturePath string
		duration    time.Duration
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
	cmd.Flags().StringVar(&capturePath, "capture", "", "Write a synthetic PCAP file while running (e.g. capture.pcap); finalised on clean shutdown")
	cmd.Flags().DurationVar(&duration, "duration", 0, "Auto-stop after this wall-clock duration (e.g. 5m, 30s); required when pacing.mode is burst")

	return cmd
}

// --- export ---

func exportCmd() *cobra.Command {
	var (
		pcapIn  string
		pcapOut string
	)

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export a results file to an alternative format",
		Long: `Convert a sendit result file to another format.

Currently supports converting a JSONL results file (written by the output
writer) to a synthetic PCAP file for analysis in Wireshark or tshark.

The PCAP uses LINKTYPE_USER0 (147) — no IP/TCP framing. Each packet payload
is a text record containing the URL, type, status code, latency, bytes, and
any error from the original request. No root or CAP_NET_RAW privilege is
required.

Examples:
  sendit export --pcap results.jsonl
  sendit export --pcap results.jsonl --output capture.pcap`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if pcapIn == "" {
				return fmt.Errorf("--pcap <results.jsonl> is required")
			}
			return pcap.Export(pcapIn, pcapOut)
		},
	}

	cmd.Flags().StringVar(&pcapIn, "pcap", "", "JSONL results file to convert to PCAP")
	cmd.Flags().StringVar(&pcapOut, "output", "", "Output PCAP file path (default: input file with .pcap extension)")

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

func printDryRun(path string, cfg *config.Config, duration time.Duration) {
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
	case "burst":
		rampUp := "none"
		if p.RampUpS > 0 {
			rampUp = fmt.Sprintf("%ds", p.RampUpS)
		}
		dur := "unlimited (SIGTERM to stop)"
		if duration > 0 {
			dur = duration.String()
		}
		fmt.Printf("Pacing:\n  mode: burst | ramp_up: %s | duration: %s\n", rampUp, dur)
		fmt.Println("  ⚠  burst mode is intended for internal or owned infrastructure only")
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
