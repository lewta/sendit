package metrics

import (
	"context"
	"fmt"
	"net/http"

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
			Help: "Total number of requests dispatched, by type and status code.",
		}, []string{"type", "status_code"}),

		errorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "sendit_errors_total",
			Help: "Total number of request errors, by type.",
		}, []string{"type", "error_class"}),

		durationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "sendit_request_duration_seconds",
			Help:    "Request duration in seconds, by type.",
			Buckets: prometheus.DefBuckets,
		}, []string{"type"}),

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
		requestsTotal:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_requests"}, []string{"type", "status_code"}),
		errorsTotal:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_errors"}, []string{"type", "error_class"}),
		durationSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "noop_duration"}, []string{"type"}),
		bytesRead:       prometheus.NewCounterVec(prometheus.CounterOpts{Name: "noop_bytes"}, []string{"type"}),
	}
}

// Record observes the result of a completed task.
func (m *Metrics) Record(r task.Result) {
	t := r.Task.Type
	m.durationSeconds.WithLabelValues(t).Observe(r.Duration.Seconds())

	if r.BytesRead > 0 {
		m.bytesRead.WithLabelValues(t).Add(float64(r.BytesRead))
	}

	if r.Error != nil {
		m.errorsTotal.WithLabelValues(t, "error").Inc()
		return
	}

	code := fmt.Sprintf("%d", r.StatusCode)
	m.requestsTotal.WithLabelValues(t, code).Inc()
}

// ServeHTTP starts the Prometheus metrics HTTP endpoint and shuts it down
// gracefully when ctx is cancelled. Call in a goroutine.
func (m *Metrics) ServeHTTP(ctx context.Context, port int) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	log.Info().Str("addr", srv.Addr).Msg("prometheus metrics endpoint listening")

	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			log.Error().Err(err).Msg("metrics server shutdown error")
		}
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Msg("metrics server error")
	}
}
