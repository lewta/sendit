package driver_test

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/driver"
	"github.com/lewta/sendit/internal/task"
	dns "github.com/miekg/dns"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
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

// sftpTask builds a minimal sftp task.
func sftpTask(rawURL string, cfg config.SFTPConfig) task.Task {
	c := config.TargetConfig{URL: rawURL, Type: "sftp", SFTP: cfg}
	return task.Task{URL: rawURL, Type: "sftp", Config: c}
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

func TestHTTPDriver_CustomAuthHeader_NotForwardedToCrossHostRedirect(t *testing.T) {
	var redirectedRequests atomic.Int32
	var gotHeader string
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedRequests.Add(1)
		gotHeader = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer dst.Close()

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusFound)
	}))
	defer src.Close()

	drv := driver.NewHTTPDriver()
	t1 := httpTask(src.URL, config.HTTPConfig{TimeoutS: 5})
	t1.Config.Auth = config.AuthConfig{Type: "header", HeaderName: "X-API-Key", Token: "secret"}
	result := drv.Execute(context.Background(), t1)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != http.StatusFound {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusFound)
	}
	if redirectedRequests.Load() != 0 {
		t.Errorf("redirect target received %d requests, want 0", redirectedRequests.Load())
	}
	if gotHeader != "" {
		t.Errorf("redirect target received auth header %q, want empty", gotHeader)
	}
}

func TestHTTPDriver_CustomAuthHeader_ForwardedToCrossHostRedirectWhenAllowed(t *testing.T) {
	var redirectedRequests atomic.Int32
	var gotHeader string
	var limitedHost string
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedRequests.Add(1)
		gotHeader = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer dst.Close()
	dstURL, err := url.Parse(dst.URL)
	if err != nil {
		t.Fatalf("parsing dst URL: %v", err)
	}

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusFound)
	}))
	defer src.Close()

	drv := driver.NewHTTPDriverWithRedirectLimiter(func(ctx context.Context, host string) error {
		limitedHost = host
		return nil
	})
	t1 := httpTask(src.URL, config.HTTPConfig{TimeoutS: 5, AllowCrossHostRedirects: true})
	t1.Config.Auth = config.AuthConfig{Type: "header", HeaderName: "X-API-Key", Token: "secret"}
	result := drv.Execute(context.Background(), t1)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if redirectedRequests.Load() != 1 {
		t.Errorf("redirect target received %d requests, want 1", redirectedRequests.Load())
	}
	if limitedHost != dstURL.Hostname() {
		t.Errorf("redirect limiter saw host %q, want %q", limitedHost, dstURL.Hostname())
	}
	if gotHeader != "secret" {
		t.Errorf("redirect target received auth header %q, want secret", gotHeader)
	}
}

func TestHTTPDriver_CrossHostRedirectLimiterBlocksRedirect(t *testing.T) {
	errLimited := errors.New("redirect host rate limited")
	var redirectedRequests atomic.Int32
	dst := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirectedRequests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer dst.Close()

	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dst.URL, http.StatusFound)
	}))
	defer src.Close()

	drv := driver.NewHTTPDriverWithRedirectLimiter(func(ctx context.Context, host string) error {
		return errLimited
	})
	t1 := httpTask(src.URL, config.HTTPConfig{TimeoutS: 5, AllowCrossHostRedirects: true})
	result := drv.Execute(context.Background(), t1)

	if !errors.Is(result.Error, errLimited) {
		t.Fatalf("Error = %v, want %v", result.Error, errLimited)
	}
	if redirectedRequests.Load() != 0 {
		t.Errorf("redirect target received %d requests, want 0", redirectedRequests.Load())
	}
}

func TestHTTPDriver_CustomAuthHeader_PreservedOnSameHostRedirect(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		gotHeader = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	drv := driver.NewHTTPDriver()
	t1 := httpTask(srv.URL+"/start", config.HTTPConfig{TimeoutS: 5})
	t1.Config.Auth = config.AuthConfig{Type: "header", HeaderName: "X-API-Key", Token: "secret"}
	result := drv.Execute(context.Background(), t1)

	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if gotHeader != "secret" {
		t.Errorf("server received auth header %q, want secret", gotHeader)
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

// --- SFTP driver ---

type sshSubsystemRequest struct {
	Subsystem string
}

func startSFTPServer(t *testing.T, root string) string {
	t.Helper()

	hostKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(hostKey)
	if err != nil {
		t.Fatalf("creating signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == "testuser" && string(password) == "secret" {
				return nil, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}
	serverConfig.AddHostKey(signer)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go serveSFTPConn(conn, serverConfig, root)
		}
	}()

	return lis.Addr().String()
}

func serveSFTPConn(conn net.Conn, serverConfig *ssh.ServerConfig, root string) {
	_, chans, reqs, err := ssh.NewServerConn(conn, serverConfig)
	if err != nil {
		_ = conn.Close()
		return
	}
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}

		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go func() {
			defer channel.Close()
			for req := range requests {
				var payload sshSubsystemRequest
				if req.Type != "subsystem" || ssh.Unmarshal(req.Payload, &payload) != nil || payload.Subsystem != "sftp" {
					_ = req.Reply(false, nil)
					continue
				}
				_ = req.Reply(true, nil)
				server, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(root))
				if err != nil {
					return
				}
				_ = server.Serve()
				_ = server.Close()
				return
			}
		}()
	}
}

func TestSFTPErrorToHTTP(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{context.DeadlineExceeded, 504},
		{os.ErrPermission, 403},
		{os.ErrNotExist, 404},
		{errors.New("ssh: handshake failed: no common algorithm"), 502},
		{errors.New("ssh: unable to authenticate"), 401},
	}
	for _, tc := range cases {
		if got := driver.SFTPErrorToHTTP(tc.err); got != tc.want {
			t.Errorf("SFTPErrorToHTTP(%v) = %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestSFTPDriver_UploadDownloadList(t *testing.T) {
	root := t.TempDir()
	addr := startSFTPServer(t, root)
	drv := driver.NewSFTPDriver()
	baseCfg := config.SFTPConfig{
		Username:      "testuser",
		Password:      "secret",
		TimeoutS:      5,
		Insecure:      true,
		FileSizeBytes: 12,
	}

	uploadPath := filepath.ToSlash(filepath.Join(root, "upload.bin"))
	upload := drv.Execute(context.Background(), sftpTask("sftp://"+addr+uploadPath, baseCfg))
	if upload.Error != nil {
		t.Fatalf("upload Error = %v", upload.Error)
	}
	if upload.StatusCode != 200 {
		t.Fatalf("upload StatusCode = %d, want 200", upload.StatusCode)
	}
	if upload.BytesRead != 12 {
		t.Errorf("upload BytesRead = %d, want 12", upload.BytesRead)
	}
	uploaded, err := os.ReadFile(filepath.Join(root, "upload.bin"))
	if err != nil {
		t.Fatalf("reading uploaded file: %v", err)
	}
	if string(uploaded) != "abcdefghijkl" {
		t.Errorf("uploaded payload = %q, want deterministic 12-byte payload", string(uploaded))
	}
	if upload.Meta["sftp_auth_methods"] != "password" {
		t.Errorf("sftp_auth_methods = %q, want password", upload.Meta["sftp_auth_methods"])
	}
	if upload.Meta["sftp_server_version"] == "" {
		t.Error("expected sftp_server_version metadata")
	}
	if upload.Meta["sftp_host_key_type"] == "" || upload.Meta["sftp_host_key_fp"] == "" {
		t.Errorf("expected host key metadata, got type=%q fp=%q", upload.Meta["sftp_host_key_type"], upload.Meta["sftp_host_key_fp"])
	}

	if err := os.WriteFile(filepath.Join(root, "download.bin"), []byte("download-payload"), 0o600); err != nil {
		t.Fatalf("writing download fixture: %v", err)
	}
	downloadCfg := baseCfg
	downloadCfg.Operation = "download"
	downloadPath := filepath.ToSlash(filepath.Join(root, "download.bin"))
	download := drv.Execute(context.Background(), sftpTask("sftp://"+addr+downloadPath, downloadCfg))
	if download.Error != nil {
		t.Fatalf("download Error = %v", download.Error)
	}
	if download.StatusCode != 200 {
		t.Fatalf("download StatusCode = %d, want 200", download.StatusCode)
	}
	if download.BytesRead != int64(len("download-payload")) {
		t.Errorf("download BytesRead = %d, want %d", download.BytesRead, len("download-payload"))
	}

	listCfg := baseCfg
	listCfg.Operation = "list"
	listPath := filepath.ToSlash(root)
	list := drv.Execute(context.Background(), sftpTask("sftp://"+addr+listPath, listCfg))
	if list.Error != nil {
		t.Fatalf("list Error = %v", list.Error)
	}
	if list.StatusCode != 200 {
		t.Fatalf("list StatusCode = %d, want 200", list.StatusCode)
	}
	if list.Meta["sftp_entry_count"] != "2" {
		t.Errorf("sftp_entry_count = %q, want 2", list.Meta["sftp_entry_count"])
	}
}

func TestSFTPDriver_EICARUpload(t *testing.T) {
	root := t.TempDir()
	addr := startSFTPServer(t, root)
	drv := driver.NewSFTPDriver()
	cfg := config.SFTPConfig{
		Username: "testuser",
		Password: "secret",
		TimeoutS: 5,
		Insecure: true,
		EICAR:    true,
	}

	eicarPath := filepath.ToSlash(filepath.Join(root, "eicar.txt"))
	result := drv.Execute(context.Background(), sftpTask("sftp://"+addr+eicarPath, cfg))
	if result.Error != nil {
		t.Fatalf("Error = %v", result.Error)
	}
	if result.StatusCode != 200 {
		t.Fatalf("StatusCode = %d, want 200", result.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(root, "eicar.txt"))
	if err != nil {
		t.Fatalf("reading eicar upload: %v", err)
	}
	if !strings.Contains(string(data), "EICAR-STANDARD-ANTIVIRUS-TEST-FILE") {
		t.Errorf("uploaded data does not contain EICAR string: %q", string(data))
	}
}

func TestSFTPDriver_DoesNotReuseConnectionAcrossAuthMaterial(t *testing.T) {
	root := t.TempDir()
	addr := startSFTPServer(t, root)
	drv := driver.NewSFTPDriver()
	goodCfg := config.SFTPConfig{
		Username:      "testuser",
		Password:      "secret",
		TimeoutS:      5,
		Insecure:      true,
		FileSizeBytes: 4,
	}

	firstPath := filepath.ToSlash(filepath.Join(root, "first.bin"))
	first := drv.Execute(context.Background(), sftpTask("sftp://"+addr+firstPath, goodCfg))
	if first.Error != nil {
		t.Fatalf("first Error = %v", first.Error)
	}
	if first.StatusCode != 200 {
		t.Fatalf("first StatusCode = %d, want 200", first.StatusCode)
	}

	badCfg := goodCfg
	badCfg.Password = "wrong"
	secondPath := filepath.ToSlash(filepath.Join(root, "second.bin"))
	second := drv.Execute(context.Background(), sftpTask("sftp://"+addr+secondPath, badCfg))
	if second.StatusCode == 200 {
		t.Fatal("second StatusCode = 200, want authentication failure instead of cached connection reuse")
	}
	if _, err := os.Stat(filepath.Join(root, "second.bin")); !os.IsNotExist(err) {
		t.Fatalf("second file exists or stat failed with unexpected error: %v", err)
	}
}

func TestSFTPDriver_DownloadMissingFileMaps404(t *testing.T) {
	root := t.TempDir()
	addr := startSFTPServer(t, root)
	drv := driver.NewSFTPDriver()
	cfg := config.SFTPConfig{
		Operation: "download",
		Username:  "testuser",
		Password:  "secret",
		TimeoutS:  5,
		Insecure:  true,
	}

	missingPath := filepath.ToSlash(filepath.Join(root, "missing.bin"))
	result := drv.Execute(context.Background(), sftpTask("sftp://"+addr+missingPath, cfg))
	if result.Error != nil {
		t.Fatalf("Error = %v, want nil Go error with status mapping", result.Error)
	}
	if result.StatusCode != 404 {
		t.Errorf("StatusCode = %d, want 404", result.StatusCode)
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
