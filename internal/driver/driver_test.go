package driver_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/task"
	dns "github.com/miekg/dns"
)

// httpTask builds a minimal http task pointing at the given URL.
func httpTask(url string, cfg config.HTTPConfig) task.Task {
	cfg2 := config.TargetConfig{URL: url, Type: "http", HTTP: cfg}
	return task.Task{URL: url, Type: "http", Config: cfg2}
}

// dnsTask builds a minimal dns task.
func dnsTask(host, resolver, recordType string) task.Task {
	cfg := config.TargetConfig{
		URL:  host,
		Type: "dns",
		DNS:  config.DNSConfig{Resolver: resolver, RecordType: recordType},
	}
	return task.Task{URL: host, Type: "dns", Config: cfg}
}

// wsTask builds a minimal websocket task.
func wsTask(url string, cfg config.WebSocketConfig) task.Task {
	c := config.TargetConfig{URL: url, Type: "websocket", WebSocket: cfg}
	return task.Task{URL: url, Type: "websocket", Config: c}
}

// --- HTTP driver ---

func TestHTTPDriver_200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	drv := driver.NewHTTPDriver()
	result := drv.Execute(context.Background(), httpTask(srv.URL, config.HTTPConfig{Method: "GET", TimeoutS: 5}))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.BytesRead <= 0 {
		t.Errorf("BytesRead = %d, want > 0", result.BytesRead)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

func TestHTTPDriver_4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	drv := driver.NewHTTPDriver()
	result := drv.Execute(context.Background(), httpTask(srv.URL, config.HTTPConfig{TimeoutS: 5}))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
	}
}

func TestHTTPDriver_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second) // longer than the driver timeout
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	drv := driver.NewHTTPDriver()
	result := drv.Execute(context.Background(), httpTask(srv.URL, config.HTTPConfig{TimeoutS: 1}))

	if result.Error == nil {
		t.Errorf("expected timeout error, got nil")
	}
}

func TestHTTPDriver_CustomHeaders(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	drv := driver.NewHTTPDriver()
	t1 := httpTask(srv.URL, config.HTTPConfig{
		TimeoutS: 5,
		Headers:  map[string]string{"x-test-header": "sendit-test"},
	})
	result := drv.Execute(context.Background(), t1)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if gotHeader != "sendit-test" {
		t.Errorf("server received header %q, want sendit-test", gotHeader)
	}
}

func TestHTTPDriver_POST_WithBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	drv := driver.NewHTTPDriver()
	t1 := httpTask(srv.URL, config.HTTPConfig{Method: "POST", Body: `{"key":"value"}`, TimeoutS: 5})
	result := drv.Execute(context.Background(), t1)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if gotBody != `{"key":"value"}` {
		t.Errorf("server received body %q, want {\"key\":\"value\"}", gotBody)
	}
}

// --- DNS driver ---

// startDNSServer starts a local miekg/dns server on a random UDP port and
// returns the address and a shutdown function. The provided handler func
// receives each query and populates the reply Rcode and answers.
func startDNSServer(t *testing.T, handler func(w dns.ResponseWriter, r *dns.Msg)) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	addr := pc.LocalAddr().String()

	mux := dns.NewServeMux()
	mux.HandleFunc(".", handler)
	srv := &dns.Server{PacketConn: pc, Net: "udp", Handler: mux}
	go srv.ActivateAndServe() //nolint:errcheck
	t.Cleanup(func() { _ = srv.Shutdown() })
	return addr
}

func TestDNSDriver_NOERROR(t *testing.T) {
	addr := startDNSServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Rcode = dns.RcodeSuccess
		_ = w.WriteMsg(m)
	})

	drv := driver.NewDNSDriver()
	result := drv.Execute(context.Background(), dnsTask("example.com", addr, "A"))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200 (NOERROR)", result.StatusCode)
	}
}

func TestDNSDriver_NXDOMAIN(t *testing.T) {
	addr := startDNSServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Rcode = dns.RcodeNameError
		_ = w.WriteMsg(m)
	})

	drv := driver.NewDNSDriver()
	result := drv.Execute(context.Background(), dnsTask("notfound.example.com", addr, "A"))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404 (NXDOMAIN)", result.StatusCode)
	}
}

func TestDNSDriver_SERVFAIL(t *testing.T) {
	addr := startDNSServer(t, func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Rcode = dns.RcodeServerFailure
		_ = w.WriteMsg(m)
	})

	drv := driver.NewDNSDriver()
	result := drv.Execute(context.Background(), dnsTask("example.com", addr, "A"))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 503 {
		t.Errorf("StatusCode = %d, want 503 (SERVFAIL)", result.StatusCode)
	}
}

func TestDNSDriver_UnreachableResolver(t *testing.T) {
	// Use a port that nothing is listening on.
	drv := driver.NewDNSDriver()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result := drv.Execute(ctx, dnsTask("example.com", "127.0.0.1:19998", "A"))

	if result.Error == nil {
		t.Errorf("expected error for unreachable resolver, got nil (status %d)", result.StatusCode)
	}
}

// --- WebSocket driver ---

func TestWebSocketDriver_Connect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow() //nolint:errcheck
		// Drain until client closes.
		readCtx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		for {
			if _, _, err := conn.Read(readCtx); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	drv := driver.NewWebSocketDriver()
	t1 := wsTask("ws://"+srv.Listener.Addr().String(), config.WebSocketConfig{DurationS: 1})
	result := drv.Execute(context.Background(), t1)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 101 {
		t.Errorf("StatusCode = %d, want 101", result.StatusCode)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

func TestWebSocketDriver_ServerClosesEarly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		// Close immediately with a normal closure.
		conn.Close(websocket.StatusNormalClosure, "bye") //nolint:errcheck,gosec
	}))
	defer srv.Close()

	drv := driver.NewWebSocketDriver()
	t1 := wsTask("ws://"+srv.Listener.Addr().String(), config.WebSocketConfig{DurationS: 1})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result := drv.Execute(ctx, t1)

	// The driver should return without hanging regardless of early server close.
	_ = result // either success or an error is acceptable; must not block
}

// --- Browser driver ---

func TestBrowserDriver_Skipped(t *testing.T) {
	// The browser driver requires a real Chrome/Chromium binary and is not
	// suitable for unit testing. Each task spawns its own chromedp.ExecAllocator
	// which requires Chrome to be installed. This test exists to document the
	// gap explicitly. Run manual smoke tests with `sendit start` and a
	// type:browser target to exercise the browser driver.
	t.Skip("browser driver requires Chrome — tested manually via sendit start")
	_ = strings.NewReader("") // suppress unused import if test body is empty
}
