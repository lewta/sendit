package main

import (
	"context"
	"fmt"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/task"
	"github.com/spf13/cobra"
)

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
