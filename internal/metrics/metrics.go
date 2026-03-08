package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/lewta/sendit/internal/task"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// Metrics holds Prometheus counters and histograms for the engine.
type Metrics struct {
	registry        *prometheus.Registry
	requestsTotal   *prometheus.CounterVec
	errorsTotal     *prometheus.CounterVec
	durationSeconds *prometheus.HistogramVec
	bytesRead       *prometheus.CounterVec
}

// New creates and registers a Metrics instance on an isolated registry,
// preventing double-registration panics when multiple instances are created
// (e.g. in tests).
func New() *Metrics {
	reg := prometheus.NewRegistry()

	m := &Metrics{
		registry: reg,
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sendit_requests_total",
			Help: "Total number of requests dispatched, by type, domain, and status code.",
		}, []string{"type", "domain", "status_code"}),

		errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sendit_errors_total",
			Help: "Total number of request errors, by type and domain.",
		}, []string{"type", "domain", "error_class"}),

		durationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "sendit_request_duration_seconds",
			Help:    "Request duration in seconds, by type and domain.",
			Buckets: prometheus.DefBuckets,
		}, []string{"type", "domain"}),

		bytesRead: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sendit_bytes_read_total",
			Help: "Total bytes read from responses, by type.",
		}, []string{"type"}),
	}

	reg.MustRegister(
		m.requestsTotal,
		m.errorsTotal,
		m.durationSeconds,
		m.bytesRead,
	)

	return m
}

// Noop returns a Metrics instance that does nothing (used when metrics disabled).
func Noop() *Metrics {
	return &Metrics{
		requestsTotal:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_requests"}, []string{"type", "domain", "status_code"}),
		errorsTotal:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_errors"}, []string{"type", "domain", "error_class"}),
		durationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "noop_duration"}, []string{"type", "domain"}),
		bytesRead:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_bytes"}, []string{"type"}),
	}
}

// Record observes the result of a completed task.
func (m *Metrics) Record(r task.Result) {
	t := r.Task.Type
	d := domainOf(r.Task.URL)
	m.durationSeconds.WithLabelValues(t, d).Observe(r.Duration.Seconds())

	if r.BytesRead > 0 {
		m.bytesRead.WithLabelValues(t).Add(float64(r.BytesRead))
	}

	if r.Error != nil {
		m.errorsTotal.WithLabelValues(t, d, "error").Inc()
		return
	}

	code := fmt.Sprintf("%d", r.StatusCode)
	m.requestsTotal.WithLabelValues(t, d, code).Inc()
}

// domainOf extracts the hostname from a URL string.
// For bare hostnames (DNS targets) it returns the string as-is.
func domainOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return rawURL
	}
	return u.Hostname() // strips port if present
}

// ServeHTTP starts the Prometheus metrics HTTP endpoint and shuts it down
// gracefully when ctx is cancelled. Call in a goroutine.
//
// Routes:
//   - /metrics — Prometheus scrape endpoint
//   - /healthz — liveness probe; always returns 200 {"status":"ok"}
func (m *Metrics) ServeHTTP(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Info().Str("addr", srv.Addr).Msg("prometheus metrics endpoint listening")

	go func() {
		<-ctx.Done()
		// ctx is already cancelled here; use a fresh context so the graceful
		// shutdown is not immediately aborted.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second) //nolint:gosec // G118: intentional — parent ctx is done, shutdown needs its own deadline
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("metrics server shutdown error")
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("metrics server error")
	}
}
