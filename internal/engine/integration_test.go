//go:build integration

package engine_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/miekg/dns"
)

// testCfg constructs a *config.Config suitable for fast integration tests:
//   - rate_limited pacing at 600 RPM (~10 req/s)
//   - resource gate never blocks (CPU=100%, Mem=999999 MB)
//   - generous per-domain rate limit (100 RPS)
//   - short backoff windows for fast retries
//   - metrics disabled to avoid port conflicts
func testCfg(targets []config.TargetConfig) *config.Config {
	return &config.Config{
		Pacing: config.PacingConfig{
			Mode:              "rate_limited",
			RequestsPerMinute: 600,
		},
		Limits: config.LimitsConfig{
			MaxWorkers:        10,
			MaxBrowserWorkers: 1,
			CPUThresholdPct:   100,    // never over threshold
			MemoryThresholdMB: 999999, // never over threshold
		},
		RateLimits: config.RateLimitsConfig{
			DefaultRPS: 100,
		},
		Backoff: config.BackoffConfig{
			InitialMs:   100,
			MaxMs:       500,
			Multiplier:  2.0,
			MaxAttempts: 3,
		},
		Targets: targets,
	}
}

// TestIntegration_HTTP_HappyPath verifies that the engine dispatches HTTP
// requests and records at least 3 successful completions.
func TestIntegration_HTTP_HappyPath(t *testing.T) {
	var counter atomic.Int64
	var once sync.Once
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if counter.Add(1) >= 3 {
			once.Do(func() { close(done) })
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := testCfg([]config.TargetConfig{
		{URL: srv.URL, Type: "http", Weight: 1},
	})

	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		eng.Run(ctx)
	}()

	select {
	case <-done:
		cancel()
	case <-ctx.Done():
		t.Errorf("timed out waiting for 3 requests; got %d", counter.Load())
	}
	<-runDone

	if n := counter.Load(); n < 3 {
		t.Errorf("expected >= 3 requests, got %d", n)
	}
}

// TestIntegration_HTTP_Backoff429 verifies that the engine retries requests
// after receiving 429 responses, proving the backoff-and-retry path works.
func TestIntegration_HTTP_Backoff429(t *testing.T) {
	var calls atomic.Int64
	done := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		// Signal on the first request to get through (n == 3).
		if n == 3 {
			close(done)
		}
	}))
	defer srv.Close()

	cfg := testCfg([]config.TargetConfig{
		{URL: srv.URL, Type: "http", Weight: 1},
	})

	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		eng.Run(ctx)
	}()

	select {
	case <-done:
		cancel()
	case <-ctx.Done():
		t.Errorf("timed out: engine did not retry past 429; total calls = %d", calls.Load())
	}
	<-runDone

	if n := calls.Load(); n <= 2 {
		t.Errorf("expected > 2 total requests (engine should retry past 429), got %d", n)
	}
}

// TestIntegration_HTTP_GracefulShutdown verifies that Run() returns promptly
// after context cancellation and does not hang while in-flight tasks drain.
func TestIntegration_HTTP_GracefulShutdown(t *testing.T) {
	started := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Signal the first time a handler fires.
		select {
		case started <- struct{}{}:
		default:
		}
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testCfg([]config.TargetConfig{
		{URL: srv.URL, Type: "http", Weight: 1},
	})

	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		eng.Run(ctx)
	}()

	// Wait until at least one request has been dispatched.
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("no request dispatched within 5s")
	}

	cancelAt := time.Now()
	cancel()

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() did not return within 2s after cancel")
	}

	// The HTTP driver propagates context cancellation, so in-flight requests
	// abort quickly. We verify Run() returns well under 600ms.
	if elapsed := time.Since(cancelAt); elapsed > 600*time.Millisecond {
		t.Errorf("Run() took too long after cancel: %v", elapsed)
	}
}

// TestIntegration_ResourceGate verifies that setting CPUThresholdPct to 0
// (always over threshold) blocks all dispatch — no requests reach the server.
func TestIntegration_ResourceGate(t *testing.T) {
	var counter atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		counter.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := testCfg([]config.TargetConfig{
		{URL: srv.URL, Type: "http", Weight: 1},
	})
	cfg.Limits.CPUThresholdPct = 0 // cpu% is always >= 0 → always over threshold

	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	eng.Run(ctx)

	if n := counter.Load(); n != 0 {
		t.Errorf("expected 0 requests (resource gate should block all), got %d", n)
	}
}

// TestIntegration_DNS_Happy verifies the engine dispatches DNS queries to a
// local miekg/dns stub resolver and records at least one successful response.
func TestIntegration_DNS_Happy(t *testing.T) {
	var counter atomic.Int64

	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		counter.Add(1)
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true
		if len(r.Question) > 0 {
			m.Answer = []dns.RR{&dns.A{
				Hdr: dns.RR_Header{
					Name:   r.Question[0].Name,
					Rrtype: dns.TypeA,
					Class:  dns.ClassINET,
					Ttl:    60,
				},
				A: net.ParseIP("127.0.0.1").To4(),
			}}
		}
		_ = w.WriteMsg(m)
	})

	// Bind to a random UDP port so parallel test runs don't conflict.
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	addr := pc.LocalAddr().String()

	dnsSrv := &dns.Server{PacketConn: pc, Net: "udp", Handler: mux}
	go dnsSrv.ActivateAndServe() //nolint:errcheck
	defer dnsSrv.Shutdown()      //nolint:errcheck

	cfg := testCfg([]config.TargetConfig{{
		URL:    "example.com",
		Type:   "dns",
		Weight: 1,
		DNS: config.DNSConfig{
			Resolver:   addr,
			RecordType: "A",
		},
	}})

	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eng.Run(ctx)

	if n := counter.Load(); n < 1 {
		t.Errorf("expected >= 1 DNS query, got %d", n)
	}
}

// TestIntegration_WebSocket verifies the engine establishes WebSocket
// connections via the websocket driver against a local httptest server.
func TestIntegration_WebSocket(t *testing.T) {
	var counter atomic.Int64

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true, // skip origin check for local test
		})
		if err != nil {
			return
		}
		counter.Add(1)
		defer conn.CloseNow()

		// Drain reads until the client closes the connection.
		readCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(readCtx); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	cfg := testCfg([]config.TargetConfig{{
		URL:       "ws://" + srv.Listener.Addr().String(),
		Type:      "websocket",
		Weight:    1,
		WebSocket: config.WebSocketConfig{DurationS: 1},
	}})

	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	eng.Run(ctx)

	if n := counter.Load(); n < 1 {
		t.Errorf("expected >= 1 WebSocket connection, got %d", n)
	}
}
