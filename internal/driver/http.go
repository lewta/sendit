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

// HTTPDriver executes HTTP requests.
type HTTPDriver struct {
	client *http.Client
}

// NewHTTPDriver creates an HTTPDriver with a shared transport.
func NewHTTPDriver() *HTTPDriver {
	return &HTTPDriver{
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
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

	start := time.Now()
	resp, err := d.client.Do(req)
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
