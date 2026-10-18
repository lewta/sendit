//go:build integration

package engine_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lewta/sendit/internal/config"
	"github.com/lewta/sendit/internal/engine"
	"github.com/lewta/sendit/internal/metrics"
	"github.com/lewta/sendit/internal/task"
)

func TestIntegrationHTTPBodyTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "x")
	}))
	defer srv.Close()
	cfg := testCfg([]config.TargetConfig{{URL: srv.URL, Type: "http", Weight: 1}})
	cfg.Limits.MaxWorkers = 1
	eng, err := engine.New(cfg, metrics.Noop())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var results []task.Result
	eng.SetObserver(func(result task.Result) {
		results = append(results, result)
		cancel()
	})
	eng.Run(ctx)
	if len(results) == 0 {
		t.Fatal("no dispatch result")
	}
	for _, result := range results {
		if !errors.Is(result.Error, io.ErrUnexpectedEOF) {
			t.Errorf("truncated response dispatched as success: error = %v", result.Error)
		}
		if result.BytesRead != 1 || result.StatusCode != http.StatusOK || result.Meta["http_protocol"] != "HTTP/1.1" {
			t.Errorf("partial response fields lost: %+v", result)
		}
	}
}
