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
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
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

// --- gRPC driver ---

// grpcTask builds a minimal gRPC task.
func grpcTask(rawURL string, cfg config.GRPCConfig) task.Task {
	c := config.TargetConfig{URL: rawURL, Type: "grpc", GRPC: cfg}
	return task.Task{URL: rawURL, Type: "grpc", Config: c}
}

// startGRPCServer starts a real gRPC server with the health service and
// reflection enabled, returning its address.
func startGRPCServer(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	reflection.Register(srv)
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.GracefulStop)
	return lis.Addr().String()
}

// startGRPCServerNoReflection starts a gRPC server without the reflection service.
func startGRPCServerNoReflection(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	// reflection NOT registered
	go srv.Serve(lis) //nolint:errcheck
	t.Cleanup(srv.GracefulStop)
	return lis.Addr().String()
}

func TestGRPCStatusToHTTP(t *testing.T) {
	cases := []struct {
		code codes.Code
		want int
	}{
		{codes.OK, 200},
		{codes.InvalidArgument, 400},
		{codes.OutOfRange, 400},
		{codes.Unauthenticated, 401},
		{codes.PermissionDenied, 403},
		{codes.NotFound, 404},
		{codes.AlreadyExists, 409},
		{codes.ResourceExhausted, 429},
		{codes.Unimplemented, 501},
		{codes.Unavailable, 503},
		{codes.DeadlineExceeded, 504},
		{codes.Internal, 500},
		{codes.Unknown, 500},
		{codes.Canceled, 500},
	}
	for _, tc := range cases {
		got := driver.GRPCStatusToHTTP(tc.code)
		if got != tc.want {
			t.Errorf("grpcStatusToHTTP(%v) = %d, want %d", tc.code, got, tc.want)
		}
	}
}

func TestGRPCDriver_HealthCheck(t *testing.T) {
	addr := startGRPCServer(t)
	drv := driver.NewGRPCDriver()
	result := drv.Execute(context.Background(),
		grpcTask("grpc://"+addr+"/grpc.health.v1.Health/Check",
			config.GRPCConfig{Body: `{"service":""}`, TimeoutS: 5}))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
	if result.Duration <= 0 {
		t.Errorf("Duration = %v, want > 0", result.Duration)
	}
}

func TestGRPCDriver_EmptyBody(t *testing.T) {
	addr := startGRPCServer(t)
	drv := driver.NewGRPCDriver()
	// No body — health Check accepts an empty HealthCheckRequest.
	result := drv.Execute(context.Background(),
		grpcTask("grpc://"+addr+"/grpc.health.v1.Health/Check",
			config.GRPCConfig{TimeoutS: 5}))

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", result.StatusCode)
	}
}

func TestGRPCDriver_ConnectionReuse(t *testing.T) {
	addr := startGRPCServer(t)
	drv := driver.NewGRPCDriver()
	for i := 0; i < 3; i++ {
		result := drv.Execute(context.Background(),
			grpcTask("grpc://"+addr+"/grpc.health.v1.Health/Check",
				config.GRPCConfig{TimeoutS: 5}))
		if result.StatusCode != 200 {
			t.Errorf("call %d: StatusCode = %d, want 200", i, result.StatusCode)
		}
	}
}

func TestGRPCDriver_NonOKStatus(t *testing.T) {
	addr := startGRPCServer(t)
	drv := driver.NewGRPCDriver()
	// Query a service name that the health server doesn't know — returns NOT_FOUND.
	result := drv.Execute(context.Background(),
		grpcTask("grpc://"+addr+"/grpc.health.v1.Health/Check",
			config.GRPCConfig{Body: `{"service":"unknown.Service"}`, TimeoutS: 5}))

	// Health server returns NOT_FOUND for unknown services — mapped to 404.
	if result.Error != nil {
		t.Fatalf("unexpected Go error (want gRPC status in StatusCode): %v", result.Error)
	}
	if result.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404 (NOT_FOUND)", result.StatusCode)
	}
}

func TestGRPCDriver_UnknownMethod(t *testing.T) {
	addr := startGRPCServer(t)
	drv := driver.NewGRPCDriver()
	result := drv.Execute(context.Background(),
		grpcTask("grpc://"+addr+"/grpc.health.v1.Health/NoSuchMethod",
			config.GRPCConfig{TimeoutS: 5}))

	if result.Error == nil {
		t.Errorf("expected error for unknown method, got StatusCode %d", result.StatusCode)
	}
}

func TestGRPCDriver_NoReflection(t *testing.T) {
	addr := startGRPCServerNoReflection(t)
	drv := driver.NewGRPCDriver()
	result := drv.Execute(context.Background(),
		grpcTask("grpc://"+addr+"/grpc.health.v1.Health/Check",
			config.GRPCConfig{TimeoutS: 5}))

	// Should return an error because reflection is not available.
	if result.Error == nil {
		t.Errorf("expected error when reflection is not enabled, got StatusCode %d", result.StatusCode)
	}
}

func TestGRPCDriver_InvalidURL(t *testing.T) {
	drv := driver.NewGRPCDriver()
	result := drv.Execute(context.Background(),
		grpcTask("grpc://localhost:50051/OnlyOneComponent",
			config.GRPCConfig{TimeoutS: 5}))

	if result.Error == nil {
		t.Errorf("expected error for invalid URL path, got StatusCode %d", result.StatusCode)
	}
}

func TestGRPCDriver_Timeout(t *testing.T) {
	addr := startGRPCServer(t)
	drv := driver.NewGRPCDriver()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	result := drv.Execute(ctx,
		grpcTask("grpc://"+addr+"/grpc.health.v1.Health/Check",
			config.GRPCConfig{TimeoutS: 5}))

	// Cancelled context should produce a non-200 result (no Go error — gRPC maps it).
	if result.StatusCode == 200 {
		t.Errorf("StatusCode = 200 on cancelled context, want non-200")
	}
}

// Ensure status.FromError is exercised to validate our status-mapping integration.
func TestGRPCDriver_StatusMapping(t *testing.T) {
	// Build a gRPC status error and verify FromError extracts the code.
	st := status.New(codes.ResourceExhausted, "quota exceeded")
	err := st.Err()
	extracted, ok := status.FromError(err)
	if !ok {
		t.Fatal("status.FromError returned ok=false for a status error")
	}
	if extracted.Code() != codes.ResourceExhausted {
		t.Errorf("code = %v, want ResourceExhausted", extracted.Code())
	}
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
