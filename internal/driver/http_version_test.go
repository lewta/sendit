package driver

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func httpVersionTask(url string, version int) task.Task {
	return task.Task{URL: url, Type: "http", Config: config.TargetConfig{URL: url, Type: "http", HTTP: config.HTTPConfig{HTTPVersion: version, Method: "POST", Body: "payload", TimeoutS: 2}}}
}

func trustVersionServers(t *testing.T, d *HTTPDriver, servers ...*httptest.Server) {
	t.Helper()
	roots := x509.NewCertPool()
	for _, s := range servers {
		roots.AddCert(s.Certificate())
	}
	for _, tr := range d.transports {
		cfg := tr.TLSClientConfig
		if cfg == nil {
			cfg = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			cfg = cfg.Clone()
		}
		cfg.RootCAs = roots
		tr.TLSClientConfig = cfg
		tr.ForceAttemptHTTP2 = true
		t.Cleanup(tr.CloseIdleConnections)
	}
}

func versionServer(t *testing.T, h2 bool, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewUnstartedServer(handler)
	s.EnableHTTP2 = h2
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func TestHTTPVersionNegotiationAndReuse(t *testing.T) {
	var connections sync.Map
	srv := versionServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Store(r.Proto+" "+r.RemoteAddr, true)
		b, _ := io.ReadAll(r.Body)
		if string(b) != "payload" || r.Method != "POST" {
			t.Error("request changed")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	d := NewHTTPDriver()
	trustVersionServers(t, d, srv)
	for _, tc := range []struct {
		version int
		proto   string
	}{{0, "HTTP/2.0"}, {1, "HTTP/1.1"}, {2, "HTTP/2.0"}} {
		for range 2 {
			r := d.Execute(context.Background(), httpVersionTask(srv.URL, tc.version))
			if r.Error != nil || r.Meta["http_protocol"] != tc.proto {
				t.Fatalf("mode %d: %+v", tc.version, r)
			}
		}
	}
	count := 0
	connections.Range(func(_, _ any) bool { count++; return true })
	if count != 3 {
		t.Fatalf("transports did not reuse connections: %d", count)
	}
	var wg sync.WaitGroup
	for i := range 30 {
		wg.Go(func() {
			version := i % 3
			r := d.Execute(context.Background(), httpVersionTask(srv.URL, version))
			want := "HTTP/2.0"
			if version == 1 {
				want = "HTTP/1.1"
			}
			if r.Error != nil || r.Meta["http_protocol"] != want {
				t.Errorf("concurrent policy %d: %+v", version, r)
			}
		})
	}
	wg.Wait()
}

func TestHTTPVersionRejectsFallbackBeforeRequest(t *testing.T) {
	for _, noALPN := range []bool{false, true} {
		t.Run(map[bool]string{false: "h1-only", true: "no-alpn"}[noALPN], func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
			if noALPN {
				s.TLS = &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{}}
			}
			s.StartTLS()
			defer s.Close()
			// StartTLS sets defaults for nil NextProtos, but preserves the explicit empty slice.
			d := NewHTTPDriver()
			trustVersionServers(t, d, s)
			probeConfig := d.transports[0].TLSClientConfig.Clone()
			probeConfig.NextProtos = []string{"h2", "http/1.1"}
			conn, err := tls.Dial("tcp", s.Listener.Addr().String(), probeConfig)
			if err != nil {
				t.Fatal(err)
			}
			negotiated := conn.ConnectionState().NegotiatedProtocol
			_ = conn.Close()
			if noALPN && negotiated != "" {
				t.Fatalf("fixture unexpectedly advertised ALPN %q", negotiated)
			}
			r := d.Execute(context.Background(), httpVersionTask(s.URL, 2))
			if r.Error == nil || calls.Load() != 0 || r.Meta["http_protocol"] != "" {
				t.Fatalf("H2 downgraded: calls=%d result=%+v", calls.Load(), r)
			}
			for _, version := range []int{0, 1} {
				r = d.Execute(context.Background(), httpVersionTask(s.URL, version))
				if r.Error != nil || r.Meta["http_protocol"] != "HTTP/1.1" {
					t.Fatalf("H1 fallback not available in %d: %+v", version, r)
				}
			}
		})
	}
}

func TestHTTPVersionPlaintextAndInvalid(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer s.Close()
	d := NewHTTPDriver()
	for _, v := range []int{-1, 2, 3} {
		r := d.Execute(context.Background(), httpVersionTask(s.URL, v))
		if r.Error == nil {
			t.Fatalf("version %d accepted", v)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("sent invalid request")
	}
	for _, v := range []int{0, 1} {
		r := d.Execute(context.Background(), httpVersionTask(s.URL, v))
		if r.Error != nil || r.Meta["http_protocol"] != "HTTP/1.1" {
			t.Fatal(r)
		}
	}
}

func TestHTTPVersionRedirectPolicy(t *testing.T) {
	var badCalls atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { badCalls.Add(1) }))
	defer plain.Close()
	h1 := versionServer(t, false, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { badCalls.Add(1) }))
	h2 := versionServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	source := versionServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dest := plain.URL
		if r.URL.Path == "/h1" {
			dest = h1.URL
		}
		if r.URL.Path == "/h2" {
			dest = h2.URL
		}
		http.Redirect(w, r, dest, http.StatusTemporaryRedirect)
	}))
	var limited atomic.Int32
	d := NewHTTPDriverWithRedirectLimiter(func(context.Context, string) error { limited.Add(1); return nil })
	trustVersionServers(t, d, source, h1, h2)
	for _, path := range []string{"/plain", "/h1", "/h2"} {
		v := httpVersionTask(source.URL+path, 2)
		v.Config.HTTP.AllowCrossHostRedirects = true
		r := d.Execute(context.Background(), v)
		if (r.Error == nil) != (path == "/h2") {
			t.Fatalf("redirect %s: %+v", path, r)
		}
	}
	if badCalls.Load() != 0 || limited.Load() != 3 {
		t.Fatalf("bad requests %d; limiter %d", badCalls.Load(), limited.Load())
	}
	stopped := d.Execute(context.Background(), httpVersionTask(source.URL, 2))
	if stopped.Error != nil || stopped.StatusCode != 307 || stopped.Meta["http_protocol"] != "HTTP/2.0" {
		t.Fatal(stopped)
	}
}

func TestHTTPVersionTrustAuthAndCancellation(t *testing.T) {
	s := versionServer(t, true, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			<-r.Context().Done()
			return
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Error("auth lost")
		}
		w.WriteHeader(200)
	}))
	d := NewHTTPDriver()
	r := d.Execute(context.Background(), httpVersionTask(s.URL, 2))
	if r.Error == nil {
		t.Fatal("untrusted cert accepted")
	}
	trustVersionServers(t, d, s)
	v := httpVersionTask(s.URL, 2)
	v.Config.Auth = config.AuthConfig{Type: "bearer", Token: "secret"}
	r = d.Execute(context.Background(), v)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	r = d.Execute(ctx, httpVersionTask(s.URL+"/slow", 2))
	if r.Error == nil {
		t.Fatal("cancellation lost")
	}
	v = httpVersionTask(s.URL+"/slow", 2)
	v.Config.HTTP.TimeoutS = 1
	r = d.Execute(context.Background(), v)
	if r.Error == nil {
		t.Fatal("timeout lost")
	}
}

func TestHTTPVersionDebugLog(t *testing.T) {
	var b bytes.Buffer
	old := log.Logger
	log.Logger = zerolog.New(&b).Level(zerolog.DebugLevel)
	defer func() { log.Logger = old }()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	d := NewHTTPDriver()
	v := httpVersionTask(srv.URL+"/?private-marker=secret", 1)
	r := d.Execute(context.Background(), v)
	if r.Error != nil {
		t.Fatal(r.Error)
	}
	if !strings.Contains(b.String(), `"http_protocol":"HTTP/1.1"`) || strings.Contains(b.String(), "private-marker") {
		t.Fatal("missing or unsafe protocol debug event: ", b.String())
	}
}

func TestHTTPVersionDisabledH2(t *testing.T) {
	if os.Getenv("SENDIT_H2_DISABLED_TEST") == "1" {
		var calls atomic.Int32
		s := versionServer(t, true, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
		d := NewHTTPDriver()
		trustVersionServers(t, d, s)
		r := d.Execute(context.Background(), httpVersionTask(s.URL, 2))
		if r.Error == nil || calls.Load() != 0 {
			t.Fatal("disabled H2 fell back")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestHTTPVersionDisabledH2$") //nolint:gosec // re-executes this test binary with fixed arguments
	cmd.Env = append(os.Environ(), "SENDIT_H2_DISABLED_TEST=1", "GODEBUG=http2client=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("disabled H2: %v %s", err, b)
	}
}
