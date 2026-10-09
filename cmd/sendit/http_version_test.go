package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lewta/sendit/internal/config"
)

func TestHTTPVersionGeneratedAndDryRun(t *testing.T) {
	for _, v := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(v), func(t *testing.T) {
			target := defaultTarget("https://example.com", "http", 1)
			target.HTTP.HTTPVersion = v
			var b bytes.Buffer
			formatConfig(&b, []config.TargetConfig{target})
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Targets[0].HTTP.HTTPVersion != v {
				t.Fatal("generator lost policy")
			}
			text := captureStdout(t, func() {
				if err := printDryRun(path, cfg, 0); err != nil {
					t.Fatal(err)
				}
			})
			label := map[int]string{0: "auto", 1: "HTTP/1.1", 2: "HTTP/2 (HTTPS)"}[v]
			if !strings.Contains(text, "HTTP POLICY") || !strings.Contains(text, label) {
				t.Fatalf("dry-run missing %s: %s", label, text)
			}
		})
	}
}

func TestHTTPVersionReplayPreflight(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer srv.Close()
	for _, version := range []int{-1, 2, 3} {
		records := replayRecords(200, 200)
		for i := range records {
			records[i].URL = srv.URL
			records[i].Envelope.Request.URL = srv.URL
		}
		records[1].Envelope.Request.HTTP.HTTPVersion = version
		path := writeReplayFile(t, records)
		out := filepath.Join(t.TempDir(), "output.jsonl")
		if err := os.WriteFile(out, []byte("sentinel"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := replayCmd()
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"--input", path, "--output", out})
		if err := cmd.Execute(); err == nil {
			t.Fatal("bad late policy accepted")
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 0 || string(b) != "sentinel" {
			t.Fatal("preflight had side effects")
		}
	}
}
