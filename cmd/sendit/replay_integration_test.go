//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/output"
	"github.com/lewta/sendit/internal/task"
	"github.com/miekg/dns"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

func readReplayFile(t *testing.T, path string) []output.ReplayRecord {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records, err := output.ReadReplay(f)
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func executeReplayRequest(t *testing.T, req *output.ReplayRequest) (output.ReplayRecord, string) {
	t.Helper()
	records := replayRecords(200)
	records[0].URL = req.URL
	records[0].Type = req.Type
	records[0].Envelope.Request = req
	in := writeReplayFile(t, records)
	out := filepath.Join(t.TempDir(), "replayed.csv")
	cmd := replayCmd()
	var summary bytes.Buffer
	cmd.SetOut(&summary)
	cmd.SetArgs([]string{"--input", in, "--output", out})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		t.Fatal(err)
	}
	results := readReplayFile(t, out)
	if len(results) != 1 {
		t.Fatalf("results=%d", len(results))
	}
	return results[0], summary.String()
}

func TestIntegrationReplayCapturedHTTP(t *testing.T) {
	observed := make(chan string, 32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", 500)
			return
		}
		observed <- r.Method + " " + r.URL.RequestURI() + " " + string(body)
		w.WriteHeader(200)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "capture.jsonl")
	// This test measures replay fidelity, not host CPU admission. Production
	// config validation still limits CPU thresholds to 100 percent.
	cfg := &config.Config{Pacing: config.PacingConfig{Mode: "rate_limited", RequestsPerMinute: 600}, Limits: config.LimitsConfig{MaxWorkers: 2, MaxBrowserWorkers: 1, CPUThresholdPct: math.Inf(1), MemoryThresholdMB: 999999}, RateLimits: config.RateLimitsConfig{DefaultRPS: 100}, Backoff: config.BackoffConfig{InitialMs: 10, MaxMs: 100, Multiplier: 2, MaxAttempts: 3}, Output: config.OutputConfig{Enabled: true, File: path, Format: "jsonl"}, Targets: []config.TargetConfig{{URL: server.URL + "/users/{{seq}}", Type: "http", Weight: 1, HTTP: config.HTTPConfig{Method: "POST", Body: `{"id":{"uuid":"{{uuid}}","seq":{{seq}}}}`, TimeoutS: 2}}}}
	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var completed atomic.Int32
	eng.SetObserver(func(r task.Result) {
		if r.Error == nil && completed.Add(1) == 3 {
			cancel()
		}
	})
	eng.Run(ctx)
	captured := readReplayFile(t, path)
	if len(captured) != 3 {
		t.Fatalf("captured %d records", len(captured))
	}
	var before []string
	for range captured {
		select {
		case v := <-observed:
			before = append(before, v)
		default:
			t.Fatal("missing capture request")
		}
	}
	dest := filepath.Join(t.TempDir(), "replay.jsonl")
	cmd := replayCmd()
	var summary bytes.Buffer
	cmd.SetOut(&summary)
	cmd.SetArgs([]string{"--input", path, "--rate", "2", "--output", dest})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var after []string
	for range captured {
		select {
		case v := <-observed:
			after = append(after, v)
		default:
			t.Fatal("missing replay request")
		}
	}
	slices.Sort(before)
	slices.Sort(after)
	if !slices.Equal(before, after) {
		t.Fatalf("payload changed: %v vs %v", before, after)
	}
	replayed := readReplayFile(t, dest)
	if len(replayed) != 3 || replayed[0].Envelope.RunID == captured[0].Envelope.RunID {
		t.Fatal("bad replay identity")
	}
	if !strings.Contains(summary.String(), "3 requests; 0 failures") {
		t.Fatal(summary.String())
	}
	// Replay replay's output once more to exercise the recursive format contract.
	cmd = replayCmd()
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{"--input", dest, "--rate", "1000"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationReplayHTTPRedirects(t *testing.T) {
	var hits atomic.Int32
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer dest.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same" {
			http.Redirect(w, r, "/done", 302)
		} else if r.URL.Path == "/done" {
			w.WriteHeader(200)
		} else {
			http.Redirect(w, r, strings.Replace(dest.URL, "127.0.0.1", "localhost", 1), 302)
		}
	}))
	defer source.Close()
	for _, allow := range []bool{false, true} {
		r, _ := executeReplayRequest(t, &output.ReplayRequest{URL: source.URL, Type: "http", HTTP: &output.ReplayHTTP{Method: "GET", TimeoutS: 2, AllowCrossHostRedirects: allow}})
		if allow && r.Status != 200 {
			t.Fatal(r)
		}
		if !allow && hits.Load() != 0 {
			t.Fatal("cross-host redirect escaped policy")
		}
	}
	if hits.Load() != 1 {
		t.Fatal("allowed redirect missing")
	}
	r, _ := executeReplayRequest(t, &output.ReplayRequest{URL: source.URL + "/same", Type: "http", HTTP: &output.ReplayHTTP{Method: "GET", TimeoutS: 2}})
	if r.Status != 200 {
		t.Fatal(r)
	}
}

func TestIntegrationReplayDNS(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	questions := make(chan dns.Question, 2)
	srv := &dns.Server{PacketConn: conn, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		questions <- r.Question[0]
		reply := new(dns.Msg)
		reply.SetReply(r)
		reply.Rcode = dns.RcodeServerFailure
		if err := w.WriteMsg(reply); err != nil {
			t.Error(err)
		}
	})}
	go func() { _ = srv.ActivateAndServe() }()
	defer srv.Shutdown() //nolint:errcheck
	r, summary := executeReplayRequest(t, &output.ReplayRequest{URL: "example.com", Type: "dns", DNS: &output.ReplayDNS{Resolver: conn.LocalAddr().String(), RecordType: "AAAA"}})
	if r.Status != 503 || !strings.Contains(summary, "1 failures") {
		t.Fatal(r, summary)
	}
	q := <-questions
	if q.Qtype != dns.TypeAAAA || q.Name != "example.com." {
		t.Fatal(q)
	}
}

func TestIntegrationReplayWebSocket(t *testing.T) {
	seen := make(chan string, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.CloseNow() //nolint:errcheck
		for range 2 {
			typ, b, err := c.Read(r.Context())
			if err != nil {
				return
			}
			seen <- string(b)
			if err := c.Write(r.Context(), typ, b); err != nil {
				return
			}
		}
		_, _, _ = c.Read(r.Context())
	}))
	defer srv.Close()
	result, _ := executeReplayRequest(t, &output.ReplayRequest{URL: "ws" + strings.TrimPrefix(srv.URL, "http"), Type: "websocket", WebSocket: &output.ReplayWebSocket{DurationS: 1, SendMessages: []string{`{"nested":{"literal":"{{uuid}}"}}`, "second"}, ExpectMessages: 2}})
	if result.Status != 101 {
		t.Fatal(result)
	}
	if a, b := <-seen, <-seen; a != `{"nested":{"literal":"{{uuid}}"}}` || b != "second" {
		t.Fatal(a, b)
	}
}

func TestIntegrationReplayGRPC(t *testing.T) {
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	cert := certServer.TLS.Certificates[0]
	certServer.Close()
	for _, mode := range []string{"plain", "tls-scheme", "tls-forced", "tls-verify-fails", "no-reflection"} {
		t.Run(mode, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var options []grpc.ServerOption
			if strings.HasPrefix(mode, "tls") {
				options = append(options, grpc.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})))
			}
			srv := grpc.NewServer(options...)
			hs := health.NewServer()
			hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
			healthpb.RegisterHealthServer(srv, hs)
			if mode != "no-reflection" {
				reflection.Register(srv)
			}
			go func() { _ = srv.Serve(listener) }()
			defer srv.Stop()
			scheme := "grpc://"
			if mode == "tls-scheme" {
				scheme = "grpcs://"
			}
			bodies := []string{`{"service":""}`, `{"service":"missing"}`, ""}
			if mode == "no-reflection" || mode == "tls-verify-fails" {
				bodies = bodies[:1]
			}
			for _, body := range bodies {
				req := &output.ReplayRequest{URL: scheme + listener.Addr().String() + "/grpc.health.v1.Health/Check", Type: "grpc", GRPC: &output.ReplayGRPC{Body: body, TimeoutS: 1, TLS: strings.HasPrefix(mode, "tls"), Insecure: mode == "tls-scheme" || mode == "tls-forced"}}
				result, summary := executeReplayRequest(t, req)
				failed := mode == "no-reflection" || mode == "tls-verify-fails" || strings.Contains(body, "missing")
				if failed {
					if !strings.Contains(summary, "1 failures") {
						t.Fatal(result, summary)
					}
				} else if result.Status != 200 {
					t.Fatal(result, summary)
				}
			}
		})
	}
}

func TestIntegrationReplaySIGINT(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX signal smoke; context cancellation covered on all platforms")
	}
	seen := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case seen <- struct{}{}:
		default:
		}
	}))
	defer srv.Close()
	records := replayRecords(200)
	records[0].URL = srv.URL
	records[0].Envelope.Request.URL = srv.URL
	in := writeReplayFile(t, records)
	out := filepath.Join(t.TempDir(), "out.jsonl")
	bin := filepath.Join(t.TempDir(), "sendit")
	build := exec.Command("go", "build", "-o", bin, ".")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "replay", "--input", in, "--output", out, "--loop", "--loop-delay", "1h")
	var log bytes.Buffer
	cmd.Stdout = &log
	cmd.Stderr = &log
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-seen:
	case <-ctx.Done():
		t.Fatal("no request")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("signal shutdown: %v %s", err, log.String())
	}
	if records := readReplayFile(t, out); len(records) != 1 {
		t.Fatal("shutdown did not drain output")
	}
}

func TestIntegrationReplayBrowser(t *testing.T) {
	if os.Getenv("SENDIT_TEST_BROWSER") != "1" {
		t.Skip("set SENDIT_TEST_BROWSER=1 with Chrome installed for browser execution smoke")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `<div id="ready">ready</div>`) }))
	defer srv.Close()
	r, _ := executeReplayRequest(t, &output.ReplayRequest{URL: srv.URL, Type: "browser", Browser: &output.ReplayBrowser{WaitForSelector: "#ready", Scroll: true, TimeoutS: 5}})
	if r.Status != 200 {
		t.Fatal(r)
	}
}
