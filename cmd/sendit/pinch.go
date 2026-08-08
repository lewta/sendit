package main

import (
	"context"
	"fmt"
	"net"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

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
