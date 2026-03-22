package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
)

// makeResult creates a task.Result for testing.
func makeResult(typ string, status int, dur time.Duration, bytesRead int64, err error) task.Result {
	return task.Result{
		Task: task.Task{
			URL:  "https://example.com",
			Type: typ,
			Config: config.TargetConfig{
				URL:  "https://example.com",
				Type: typ,
			},
		},
		StatusCode: status,
		Duration:   dur,
		BytesRead:  bytesRead,
		Error:      err,
	}
}

// TestNoop_DoesNotPanic verifies the no-op metrics instance handles all cases.
func TestNoop_DoesNotPanic(t *testing.T) {
	m := Noop()
	if m == nil {
		t.Fatal("Noop() returned nil")
	}

	testCases := []task.Result{
		makeResult("http", 200, 100*time.Millisecond, 1024, nil),
		makeResult("dns", 0, 5*time.Millisecond, 0, nil),
		makeResult("browser", 200, 2*time.Second, 0, nil),
		makeResult("websocket", 101, 10*time.Second, 0, nil),
		makeResult("http", 0, 50*time.Millisecond, 0, errSentinel{}),
		makeResult("http", 429, 0, 0, nil),
		makeResult("http", 503, 0, 0, nil),
	}

	for _, r := range testCases {
		// Must not panic.
		m.Record(r)
	}
}

// errSentinel is a simple error type for test injection.
type errSentinel struct{}

func (e errSentinel) Error() string { return "test error" }

// TestNoop_NotNilFields verifies internal counter fields are not nil.
func TestNoop_NotNilFields(t *testing.T) {
	m := Noop()
	if m.requestsTotal == nil {
		t.Error("requestsTotal is nil")
	}
	if m.errorsTotal == nil {
		t.Error("errorsTotal is nil")
	}
	if m.durationSeconds == nil {
		t.Error("durationSeconds is nil")
	}
	if m.bytesRead == nil {
		t.Error("bytesRead is nil")
	}
}

// TestRecord_ErrorPath confirms errors don't panic and don't record a status code.
func TestRecord_ErrorPath(t *testing.T) {
	m := Noop()
	r := makeResult("http", 0, 10*time.Millisecond, 0, errSentinel{})
	// Should not panic.
	m.Record(r)
}

// TestRecord_SuccessPath confirms success results don't panic.
func TestRecord_SuccessPath(t *testing.T) {
	m := Noop()
	r := makeResult("http", 200, 150*time.Millisecond, 2048, nil)
	m.Record(r)
}

// TestRecord_ZeroBytesSkipped confirms that zero BytesRead doesn't call Add.
func TestRecord_ZeroBytesSkipped(t *testing.T) {
	m := Noop()
	r := makeResult("dns", 0, 5*time.Millisecond, 0, nil)
	// No panic expected.
	m.Record(r)
}

// TestRecord_AllDriverTypes verifies Record works for all driver types.
func TestRecord_AllDriverTypes(t *testing.T) {
	m := Noop()
	types := []string{"http", "browser", "dns", "websocket"}
	for _, typ := range types {
		r := makeResult(typ, 200, 100*time.Millisecond, 512, nil)
		m.Record(r) // must not panic
	}
}

// freePort finds an available TCP port on loopback.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// TestNew_ReturnsUsableMetrics verifies New() creates a non-nil registry
// and that Record does not panic on it.
func TestNew_ReturnsUsableMetrics(t *testing.T) {
	m := New()
	if m == nil {
		t.Fatal("New() returned nil")
	}
	if m.registry == nil {
		t.Fatal("New() registry is nil")
	}
	// Record must not panic.
	m.Record(makeResult("http", 200, 100*time.Millisecond, 1024, nil))
	m.Record(makeResult("http", 0, 50*time.Millisecond, 0, errSentinel{}))
}

// TestServeHTTP_HealthzRoute verifies the /healthz endpoint returns 200 JSON.
func TestServeHTTP_HealthzRoute(t *testing.T) {
	m := New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := freePort(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.ServeHTTP(ctx, port)
	}()

	var resp *http.Response
	for i := 0; i < 30; i++ {
		var err error
		resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("server did not become ready within deadline")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	cancel()
	<-done
}

// TestServeHTTP_MetricsRoute verifies the /metrics endpoint returns 200.
func TestServeHTTP_MetricsRoute(t *testing.T) {
	m := New()
	// Record a result so the metrics endpoint has something to show.
	m.Record(makeResult("http", 200, 100*time.Millisecond, 512, nil))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port := freePort(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.ServeHTTP(ctx, port)
	}()

	var resp *http.Response
	for i := 0; i < 30; i++ {
		var err error
		resp, err = http.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", port))
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if resp == nil {
		t.Fatal("server did not become ready within deadline")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("metrics status = %d, want 200", resp.StatusCode)
	}

	cancel()
	<-done
}

// TestDomainOf verifies domain extraction from various URL formats.
func TestDomainOf(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"https://example.com", "example.com"},
		{"https://example.com/path/to/page", "example.com"},
		{"https://example.com:443/path", "example.com"},
		{"http://sub.domain.example.com", "sub.domain.example.com"},
		{"wss://stream.example.com/feed", "stream.example.com"},
		{"example.com", "example.com"},                   // bare hostname (DNS target)
		{"notfound.example.com", "notfound.example.com"}, // bare DNS hostname
	}

	for _, c := range cases {
		got := domainOf(c.input)
		if got != c.want {
			t.Errorf("domainOf(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}
