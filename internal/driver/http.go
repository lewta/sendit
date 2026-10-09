package driver

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/task"
	"github.com/rs/zerolog/log"
)

// RedirectLimiter is called before the HTTP driver follows a redirect to a
// different host.
type RedirectLimiter func(ctx context.Context, host string) error

// HTTPDriver executes HTTP requests.
type HTTPDriver struct {
	client          *http.Client
	transports      [3]*http.Transport
	redirectLimiter RedirectLimiter
}

// NewHTTPDriver creates an HTTPDriver with a shared transport.
func NewHTTPDriver() *HTTPDriver {
	return NewHTTPDriverWithRedirectLimiter(nil)
}

// NewHTTPDriverWithRedirectLimiter creates an HTTPDriver that asks
// redirectLimiter for permission before following cross-host redirects.
func NewHTTPDriverWithRedirectLimiter(redirectLimiter RedirectLimiter) *HTTPDriver {
	newTransport := func() *http.Transport {
		return &http.Transport{MaxIdleConns: 100, MaxIdleConnsPerHost: 10, IdleConnTimeout: 90 * time.Second}
	}
	// Clone initializes automatic HTTP/2 and copies its ALPN configuration.
	// Start independently so each policy advertises only its own protocols.
	automatic, h1, h2 := newTransport(), newTransport(), newTransport()
	h1.Protocols = new(http.Protocols)
	h1.Protocols.SetHTTP1(true)
	h2.Protocols = new(http.Protocols)
	h2.Protocols.SetHTTP2(true)
	h2.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if cs.NegotiatedProtocol != "h2" {
				return fmt.Errorf("http_version 2 requires server ALPN h2; HTTP/1.1 fallback is disabled")
			}
			return nil
		},
	}
	return &HTTPDriver{
		redirectLimiter: redirectLimiter,
		client:          &http.Client{Transport: automatic},
		transports:      [3]*http.Transport{automatic, h1, h2},
	}
}

// Apply the scheme requirement to every RoundTrip, including redirect hops.
type httpsOnlyTransport struct{ *http.Transport }

func (t httpsOnlyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" {
		return nil, fmt.Errorf("http_version 2 requires HTTPS; cleartext HTTP/2 is not supported")
	}
	return t.Transport.RoundTrip(r)
}

func (d *HTTPDriver) redirectPolicy(allowCrossHost bool) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}

		if strings.EqualFold(req.URL.Host, via[len(via)-1].URL.Host) {
			return nil
		}

		if !allowCrossHost {
			return http.ErrUseLastResponse
		}

		if d.redirectLimiter == nil {
			return nil
		}

		host := req.URL.Hostname()
		if host == "" {
			return nil
		}
		return d.redirectLimiter(req.Context(), host)
	}
}

// Execute performs the HTTP request described by t.
func (d *HTTPDriver) Execute(ctx context.Context, t task.Task) task.Result {
	cfg := t.Config.HTTP
	if err := config.ValidateHTTPVersion(cfg.HTTPVersion); err != nil {
		return task.Result{Task: t, Error: err}
	}

	timeoutS := cfg.TimeoutS
	if timeoutS <= 0 {
		timeoutS = 15
	}
	method := cfg.Method
	if method == "" {
		method = http.MethodGet
	}

	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	var bodyReader io.Reader
	if cfg.Body != "" {
		bodyReader = strings.NewReader(cfg.Body)
	}

	req, err := http.NewRequestWithContext(reqCtx, method, t.URL, bodyReader)
	if err != nil {
		return task.Result{Task: t, Error: fmt.Errorf("creating request: %w", err)}
	}

	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	if err := applyAuth(req, t.Config.Auth); err != nil {
		return task.Result{Task: t, Error: err}
	}

	start := time.Now()
	clientCopy := *d.client
	if cfg.HTTPVersion == 1 {
		clientCopy.Transport = d.transports[1]
	}
	if cfg.HTTPVersion == 2 {
		clientCopy.Transport = httpsOnlyTransport{d.transports[2]}
	}
	clientCopy.CheckRedirect = d.redirectPolicy(cfg.AllowCrossHostRedirects)
	client := &clientCopy
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	var meta map[string]string
	if resp != nil {
		meta = map[string]string{"http_protocol": resp.Proto}
		log.Debug().Str("http_protocol", resp.Proto).Msg("HTTP response protocol")
	}

	if err != nil {
		return task.Result{Task: t, Duration: elapsed, Meta: meta, Error: redactQueryAuthError(err, req.URL.String(), t.Config.Auth)}
	}
	defer resp.Body.Close()

	n, _ := io.Copy(io.Discard, resp.Body)

	return task.Result{
		Task:       t,
		StatusCode: resp.StatusCode,
		Duration:   elapsed,
		BytesRead:  n,
		Meta:       meta,
	}
}
