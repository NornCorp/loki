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
			Name: "loki_requests_total",
			Help: "Total number of requests handled",
		},
		[]string{"service", "handler", "status"},
	)

	RequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loki_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "handler"},
	)

	StepDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "loki_step_duration_seconds",
			Help:    "Step execution duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"service", "handler", "step"},
	)

	ErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "loki_errors_total",
			Help: "Total number of errors",
		},
		[]string{"service", "handler", "type"},
	)
)

func init() {
	prometheus.MustRegister(RequestsTotal, RequestDuration, StepDuration, ErrorsTotal)
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
