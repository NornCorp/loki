package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	RequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "polymorph_requests_total",
			Help: "Total number of requests handled",
		},
		[]string{"service", "handler", "status"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "polymorph_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "handler"},
	)

	StepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "polymorph_step_duration_seconds",
			Help:    "Step execution duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "handler", "step"},
	)

	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "polymorph_errors_total",
			Help: "Total number of errors",
		},
		[]string{"service", "handler", "type"},
	)
)

// Config holds metrics configuration.
type Config struct {
	Enabled bool
	Path    string // Prometheus scrape path (default "/metrics")
}

var (
	enabled bool
	path    string
)

// Init explicitly registers Prometheus collectors and stores config.
// Must be called before any metrics recording or serving.
func Init(cfg Config) {
	enabled = cfg.Enabled
	path = cfg.Path
	if !enabled {
		return
	}
	prometheus.MustRegister(RequestsTotal, RequestDuration, StepDuration, ErrorsTotal)
}

// IsEnabled returns whether metrics collection is active.
func IsEnabled() bool {
	return enabled
}

// Path returns the configured Prometheus scrape path.
func Path() string {
	return path
}

// RecordRequest records metrics for a completed request.
func RecordRequest(serviceName, handler string, status int, duration time.Duration) {
	RequestsTotal.WithLabelValues(serviceName, handler, strconv.Itoa(status)).Inc()
	RequestDuration.WithLabelValues(serviceName, handler).Observe(duration.Seconds())
}

// RecordStep records metrics for a completed step execution.
func RecordStep(serviceName, handler, stepName string, duration time.Duration) {
	StepDuration.WithLabelValues(serviceName, handler, stepName).Observe(duration.Seconds())
}

// RecordError records an error event.
func RecordError(serviceName, handler, errorType string) {
	ErrorsTotal.WithLabelValues(serviceName, handler, errorType).Inc()
}

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}
