package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RPS: 100,
	})

	// First burst of requests should all be allowed
	for range 50 {
		require.True(t, rl.Allow())
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RPS: 5,
	})

	// Exhaust the burst (burst = RPS = 5)
	for range 5 {
		require.True(t, rl.Allow())
	}

	// Next request should be blocked
	require.False(t, rl.Allow())
}

func TestRateLimiter_DefaultStatus(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RPS: 10,
	})
	require.Equal(t, http.StatusTooManyRequests, rl.config.Status)
}

func TestRateLimiter_CustomStatus(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RPS:    10,
		Status: http.StatusServiceUnavailable,
	})
	require.Equal(t, http.StatusServiceUnavailable, rl.config.Status)
}

func TestRateLimiter_WriteError(t *testing.T) {
	rl := NewRateLimiter(RateLimitConfig{
		RPS:     10,
		Status:  429,
		Headers: map[string]string{"Retry-After": "1"},
		Body:    `{"error":"rate_limited"}`,
	})

	w := httptest.NewRecorder()
	rl.WriteError(w)

	require.Equal(t, 429, w.Code)
	require.Equal(t, "1", w.Header().Get("Retry-After"))
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.JSONEq(t, `{"error":"rate_limited"}`, w.Body.String())
}
