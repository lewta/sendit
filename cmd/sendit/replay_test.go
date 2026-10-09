package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/task"
)

func writeReplayFile(t *testing.T, records []output.ReplayRecord) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input.jsonl")
	var b bytes.Buffer
	for _, r := range records {
		if err := json.NewEncoder(&b).Encode(map[string]any{"url": r.URL, "type": r.Type, "status": r.Status, "replay": r.Envelope}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayCommandFlagsAndEmptyInput(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "replay" {
			found = true
		}
	}
	if !found {
		t.Fatal("command not registered")
	}
	path := writeReplayFile(t, nil)
	for _, args := range [][]string{{}, {"unexpected"}, {"--input", path, "--rate", "NaN"}, {"--input", path, "--rate", "0"}, {"--input", path, "--filter", "status=200"}, {"--input", path, "--loop-delay", "-1s"}, {"--input", path, "--loop"}, {"--input", t.TempDir()}} {
		cmd := replayCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	cmd := replayCmd()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetArgs([]string{"--input", path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "0 requests; 0 failures") {
		t.Fatal(b.String())
	}
	for flag, want := range map[string]string{"rate": "1", "loop": "false", "loop-delay": "1s", "filter": "", "input": "", "output": ""} {
		f := cmd.Flags().Lookup(flag)
		if f == nil || f.DefValue != want {
			t.Fatalf("flag %s", flag)
		}
	}
}

func TestReplayPreflightPreservesOutput(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer srv.Close()
	for _, mode := range []string{"malformed", "overflow", "excluded malformed"} {
		t.Run(mode, func(t *testing.T) {
			records := replayRecords(200, 200)
			for i := range records {
				records[i].URL = srv.URL
				records[i].Envelope.Request.URL = srv.URL
			}
			path := writeReplayFile(t, records)
			args := []string{"--input", path}
			if mode == "overflow" {
				args = append(args, "--rate", "1e-300")
			} else {
				f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("{broken}\n")
				if err != nil {
					t.Fatal(err)
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "excluded malformed" {
				args = append(args, "--filter", "status=5xx")
			}
			dest := filepath.Join(t.TempDir(), "out.jsonl")
			if err := os.WriteFile(dest, []byte("sentinel"), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := replayCmd()
			cmd.SetArgs(append(args, "--output", dest))
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			if err := cmd.Execute(); err == nil {
				t.Fatal("preflight accepted")
			}
			b, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != "sentinel" || calls.Load() != 0 {
				t.Fatal("preflight had side effects")
			}
		})
	}
}

func TestOpenReplayOutputAliasesAndPermissions(t *testing.T) {
	path := writeReplayFile(t, replayRecords(200))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	aliases := []string{path, filepath.Dir(path) + "/./input.jsonl"}
	for name, link := range map[string]func(string, string) error{"symlink": os.Symlink, "hardlink": os.Link} {
		alias := filepath.Join(t.TempDir(), name)
		if err := link(path, alias); err != nil {
			t.Logf("%s unsupported: %v", name, err)
		} else {
			aliases = append(aliases, alias)
		}
	}
	for _, alias := range aliases {
		if f, err := openReplayOutput(in, alias); err == nil {
			_ = f.Close()
			t.Fatalf("accepted alias %s", alias)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("input changed")
	}
	if f, err := openReplayOutput(in, t.TempDir()); err == nil {
		_ = f.Close()
		t.Fatal("directory accepted")
	}
	if f, err := openReplayOutput(in, os.DevNull); err == nil {
		_ = f.Close()
		t.Fatal("device accepted")
	}
	dest := filepath.Join(t.TempDir(), "output.csv")
	f, err := openReplayOutput(in, dest)
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Fatal(info.Mode())
		}
		if err := os.Chmod(dest, 0o644); err != nil { //nolint:gosec // test rejection of an existing permissive file
			t.Fatal(err)
		}
		if f, err := openReplayOutput(in, dest); err == nil {
			_ = f.Close()
			t.Fatal("permissive existing output accepted")
		}
	}
	if f, err := openReplayOutput(in, filepath.Join(dest, "missing")); err == nil {
		_ = f.Close()
		t.Fatal("invalid destination accepted")
	}
}

type replayFailWriter struct{ err error }

func (w replayFailWriter) Write([]byte) (int, error) { return 0, w.err }

type replayFailCloser struct {
	err    error
	closed bool
}

func (c *replayFailCloser) Close() error { c.closed = true; return c.err }

func TestFinishReplayOutputPreservesErrors(t *testing.T) {
	runErr, writeErr, closeErr := errors.New("run"), errors.New("write"), errors.New("close")
	bw := bufio.NewWriter(replayFailWriter{writeErr})
	if _, err := bw.WriteString("pending"); err != nil {
		t.Fatal(err)
	}
	closer := &replayFailCloser{err: closeErr}
	got := finishReplayOutput(bw, closer, runErr)
	if !closer.closed || !errors.Is(got, runErr) || !errors.Is(got, writeErr) || !errors.Is(got, closeErr) {
		t.Fatalf("lost error %v", got)
	}
}

func TestReplayCommandUsesContextAndNoConfig(t *testing.T) {
	t.Setenv("SENDIT_CONFIG", "/does/not/exist")
	input := writeReplayFile(t, replayRecords(200))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd := replayCmd()
	cmd.SetArgs([]string{"--input", input})
	var b bytes.Buffer
	cmd.SetOut(&b)
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "0 requests") {
		t.Fatal("ignored context:", b.String())
	}
}

func TestReplayEncoderReportsFailures(t *testing.T) {
	sentinel := errors.New("disk error")
	r := task.Result{Task: task.Task{URL: "https://example.com", Type: "http"}, StatusCode: 200}
	if err := output.EncodeJSONL(json.NewEncoder(replayFailWriter{sentinel}), r); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	stamp, err := task.NewCaptureClock().Next()
	if err != nil {
		t.Fatal(err)
	}
	stamp.StartedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	r.Capture = stamp
	if err := output.EncodeJSONL(json.NewEncoder(io.Discard), r); err == nil {
		t.Fatal("encoding error swallowed")
	}
}
