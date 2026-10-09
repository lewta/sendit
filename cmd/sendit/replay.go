package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/task"
	"github.com/spf13/cobra"
)

func replayCmd() *cobra.Command {
	var inputPath, outputPath string
	opts := replayOptions{}
	cmd := &cobra.Command{Use: "replay", Short: "Replay captured requests from versioned JSONL", Args: cobra.NoArgs,
		Long: `Replay one capture run in recorded dispatch order with concurrent, scaled
start timing. Input must contain replay metadata; legacy result files cannot
reconstruct requests. Auth, custom headers, URL userinfo and SFTP are excluded.
Limits: 256 MiB input, 8 MiB per line, 10,000 records. The whole file is validated
before traffic. Normal pacing, backoff and resource limits are not applied.
--filter status=5xx selects original statuses 500–599 while preserving gaps.
Each --loop waits for active requests, then --loop-delay (not scaled by --rate).
Ctrl-C stops scheduling, cancels active requests and finalizes output.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if inputPath == "" {
				return fmt.Errorf("--input is required")
			}
			if err := opts.validate(); err != nil {
				return err
			}
			info, err := os.Stat(inputPath)
			if err != nil {
				return fmt.Errorf("opening replay input: %w", err)
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("--input must be a regular file")
			}
			input, err := os.Open(inputPath)
			if err != nil {
				return err
			}
			defer input.Close() //nolint:errcheck
			records, err := output.ReadReplay(input)
			if err != nil {
				return err
			}
			items, err := prepareReplay(records, opts)
			if err != nil {
				return err
			}
			var file *os.File
			var bw *bufio.Writer
			var write func(task.Result) error
			if outputPath != "" {
				file, err = openReplayOutput(input, outputPath)
				if err != nil {
					return err
				}
				bw = bufio.NewWriter(file)
				enc := json.NewEncoder(bw)
				write = func(r task.Result) error {
					if err := output.EncodeJSONL(enc, r); err != nil {
						return err
					}
					return bw.Flush()
				}
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			drivers := map[string]driver.Driver{"http": driver.NewHTTPDriver(), "browser": driver.NewBrowserDriver(), "dns": driver.NewDNSDriver(), "websocket": driver.NewWebSocketDriver(), "grpc": driver.NewGRPCDriver()}
			summary, runErr := runReplay(ctx, items, opts, drivers, write)
			if file != nil {
				runErr = finishReplayOutput(bw, file, runErr)
				if runErr != nil {
					runErr = fmt.Errorf("replay output %q may be partial: %w", outputPath, runErr)
				}
			}
			_, printErr := fmt.Fprintf(cmd.OutOrStdout(), "Replayed %d requests; %d failures.\n", summary.Requests, summary.Failures)
			return errors.Join(runErr, printErr)
		},
	}
	cmd.Flags().StringVar(&inputPath, "input", "", "Replay-capable JSONL file (required)")
	cmd.Flags().Float64Var(&opts.Rate, "rate", 1, "Start-timing multiplier, finite and greater than zero")
	cmd.Flags().StringVar(&opts.Filter, "filter", "", "Source-result filter (status=5xx)")
	cmd.Flags().BoolVar(&opts.Loop, "loop", false, "Repeat selected requests until interrupted")
	cmd.Flags().DurationVar(&opts.LoopDelay, "loop-delay", time.Second, "Delay between completed cycles, independent of --rate")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write replay results as JSONL to a different file")
	return cmd
}

func openReplayOutput(input *os.File, outputPath string) (*os.File, error) {
	inInfo, err := input.Stat()
	if err != nil {
		return nil, err
	}
	inputAbs, err := filepath.Abs(input.Name())
	if err != nil {
		return nil, err
	}
	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return nil, err
	}
	if inputAbs == outputAbs {
		return nil, fmt.Errorf("--input and --output must be different files")
	}
	if resolved, err := filepath.EvalSymlinks(outputAbs); err == nil {
		outputAbs = resolved
	}
	if info, err := os.Stat(outputAbs); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("--output must be a regular file")
		}
		if os.SameFile(inInfo, info) {
			return nil, fmt.Errorf("--input and --output refer to the same file")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	// Verify the opened inode before any truncation, including hard-link aliases.
	f, err := os.OpenFile(outputAbs, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	reject := func(err error) (*os.File, error) { return nil, errors.Join(err, f.Close()) }
	info, err := f.Stat()
	if err != nil {
		return reject(err)
	}
	if !info.Mode().IsRegular() || os.SameFile(inInfo, info) {
		return reject(fmt.Errorf("--output must be a different regular file"))
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return reject(fmt.Errorf("existing --output must have no group/other permission bits (use mode 0600)"))
	}
	if err := f.Truncate(0); err != nil {
		return reject(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return reject(err)
	}
	return f, nil
}

func finishReplayOutput(w *bufio.Writer, f io.Closer, runErr error) error {
	return errors.Join(runErr, w.Flush(), f.Close())
}
