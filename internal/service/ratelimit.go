package service

import (
	"net/http"

	"golang.org/x/time/rate"
)

// RateLimitConfig defines rate limiting parameters.
type RateLimitConfig struct {
	RPS     float64           // Requests per second
	Status  int               // HTTP status code when limited (default 429)
	Headers map[string]string // Response headers
	Body    string            // Response body
}

// RateLimiter limits requests using a token bucket.
type RateLimiter struct {
	limiter *rate.Limiter
	config  RateLimitConfig
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	if config.Status == 0 {
		config.Status = http.StatusTooManyRequests
	}
	// Burst allows small spikes up to the RPS value
	burst := int(config.RPS)
	if burst < 1 {
		burst = 1
	}
	return &RateLimiter{
		limiter: rate.NewLimiter(rate.Limit(config.RPS), burst),
		config:  config,
	}
}

// Allow checks if a request is allowed. Returns true if under the limit.
func (r *RateLimiter) Allow() bool {
	return r.limiter.Allow()
}

// WriteError writes a rate limit response.
func (r *RateLimiter) WriteError(w http.ResponseWriter) {
	for k, v := range r.config.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(r.config.Status)
	if r.config.Body != "" {
		w.Write([]byte(r.config.Body))
	}
}
