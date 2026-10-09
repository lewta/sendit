//go:build integration

package main

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/task"
)

func TestIntegrationHTTPVersionCaptureReplay(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SSL_CERT_FILE process trust fixture is Linux-only; direct TLS driver tests are portable")
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != "POST" || !strings.Contains(string(body), `"name":"literal"`) {
			t.Errorf("payload changed: %s %s", r.Method, body)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()
	dir := t.TempDir()
	cert := filepath.Join(dir, "roots.pem")
	if err := os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestIntegrationHTTPVersionChild$")
	cmd.Env = append(os.Environ(), "SENDIT_HTTP_VERSION_CHILD=1", "SENDIT_HTTP_VERSION_URL="+srv.URL, "SENDIT_HTTP_VERSION_DIR="+dir, "SSL_CERT_FILE="+cert, "SSL_CERT_DIR="+dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("capture/replay: %v\n%s", err, b)
	}
}

func TestIntegrationHTTPVersionChild(t *testing.T) {
	if os.Getenv("SENDIT_HTTP_VERSION_CHILD") != "1" {
		t.Skip("subprocess helper for fresh certificate trust")
	}
	url, dir := os.Getenv("SENDIT_HTTP_VERSION_URL"), os.Getenv("SENDIT_HTTP_VERSION_DIR")
	for _, version := range []int{0, 1, 2} {
		in := filepath.Join(dir, fmt.Sprintf("capture-%d.jsonl", version))
		out := filepath.Join(dir, fmt.Sprintf("replayed-%d.jsonl", version))
		cfg := &config.Config{Pacing: config.PacingConfig{Mode: "rate_limited", RequestsPerMinute: 60}, Limits: config.LimitsConfig{MaxWorkers: 1, MaxBrowserWorkers: 1, CPUThresholdPct: math.Inf(1), MemoryThresholdMB: 999999}, RateLimits: config.RateLimitsConfig{DefaultRPS: 100}, Backoff: config.BackoffConfig{InitialMs: 10, MaxMs: 100, Multiplier: 2, MaxAttempts: 3}, Output: config.OutputConfig{Enabled: true, File: in, Format: "jsonl"}, Targets: []config.TargetConfig{{URL: url + "/{{seq}}", Type: "http", Weight: 1, HTTP: config.HTTPConfig{HTTPVersion: version, Method: "POST", Body: `{"name":"literal","id":"{{uuid}}"}`, TimeoutS: 2}}}}
		eng, err := engine.New(cfg, metrics.Noop())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		eng.SetObserver(func(task.Result) { cancel() })
		eng.Run(ctx)
		cancel()
		captured := readReplayFile(t, in)
		if len(captured) != 1 || captured[0].Envelope.Version != 2 || captured[0].Envelope.Request.Task().Config.HTTP.HTTPVersion != version {
			t.Fatal("capture lost policy")
		}
		cmd := replayCmd()
		cmd.SetOut(io.Discard)
		cmd.SetArgs([]string{"--input", in, "--output", out})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		replayed := readReplayFile(t, out)
		if len(replayed) != 1 || replayed[0].Envelope.Request.HTTP.HTTPVersion != version || replayed[0].Envelope.Request.HTTP.Body != captured[0].Envelope.Request.HTTP.Body {
			t.Fatal("replay changed request")
		}
		want := "HTTP/2.0"
		if version == 1 {
			want = "HTTP/1.1"
		}
		for _, path := range []string{in, out} {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var record struct {
				Protocol string `json:"http_protocol"`
				Status   int    `json:"status"`
			}
			if err := json.Unmarshal(b, &record); err != nil {
				t.Fatal(err)
			}
			if record.Protocol != want || record.Status != 200 {
				t.Fatalf("mode %d: %+v", version, record)
			}
		}
		// A v1 capture must remain automatic regardless of the source response.
		raw, err := os.ReadFile(in)
		if err != nil {
			t.Fatal(err)
		}
		legacy := strings.Replace(string(raw), `"version":2`, `"version":1`, 1)
		legacy = strings.Replace(legacy, fmt.Sprintf(`"http_version":%d,`, version), "", 1)
		legacyPath := filepath.Join(dir, fmt.Sprintf("v1-%d.jsonl", version))
		if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd = replayCmd()
		cmd.SetOut(io.Discard)
		cmd.SetArgs([]string{"--input", legacyPath, "--output", out})
		if err := cmd.Execute(); err != nil {
			t.Fatal(err)
		}
		converted := readReplayFile(t, out)
		if converted[0].Envelope.Version != 2 || converted[0].Envelope.Request.HTTP.HTTPVersion != 0 {
			t.Fatal("v1 recapture not automatic v2")
		}
	}
}
