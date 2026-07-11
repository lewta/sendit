package driver

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/lewta/sendit/internal/task"
)

// RedirectLimiter is called before the HTTP driver follows a redirect to a
// different host.
type RedirectLimiter func(ctx context.Context, host string) error

// HTTPDriver executes HTTP requests.
type HTTPDriver struct {
	client          *http.Client
	redirectLimiter RedirectLimiter
}

// NewHTTPDriver creates an HTTPDriver with a shared transport.
func NewHTTPDriver() *HTTPDriver {
	return NewHTTPDriverWithRedirectLimiter(nil)
}

// NewHTTPDriverWithRedirectLimiter creates an HTTPDriver that asks
// redirectLimiter for permission before following cross-host redirects.
func NewHTTPDriverWithRedirectLimiter(redirectLimiter RedirectLimiter) *HTTPDriver {
	return &HTTPDriver{
		redirectLimiter: redirectLimiter,
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
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
	clientCopy.CheckRedirect = d.redirectPolicy(cfg.AllowCrossHostRedirects)
	client := &clientCopy
	resp, err := client.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		return task.Result{Task: t, Duration: elapsed, Error: err}
	}
	defer resp.Body.Close()

	n, _ := io.Copy(io.Discard, resp.Body)

	return task.Result{
		Task:       t,
		StatusCode: resp.StatusCode,
		Duration:   elapsed,
		BytesRead:  n,
	}
}
