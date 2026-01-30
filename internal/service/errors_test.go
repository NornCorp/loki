package service

import (
	"math"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorInjector_ShouldInject(t *testing.T) {
	t.Run("no errors configured", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{})
		require.Nil(t, injector.ShouldInject())
	})

	t.Run("zero rate never injects", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{
			{
				Name:   "never",
				Rate:   0.0,
				Status: 500,
			},
		})

		// Try many times - should never inject
		for range 1000 {
			require.Nil(t, injector.ShouldInject())
		}
	})

	t.Run("rate of 1.0 always injects", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{
			{
				Name:   "always",
				Rate:   1.0,
				Status: 500,
			},
		})

		// Try many times - should always inject
		for range 100 {
			errCfg := injector.ShouldInject()
			require.NotNil(t, errCfg)
			require.Equal(t, "always", errCfg.Name)
			require.Equal(t, 500, errCfg.Status)
		}
	})

	t.Run("first matching error wins", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{
			{
				Name:   "first",
				Rate:   1.0,
				Status: 500,
			},
			{
				Name:   "second",
				Rate:   1.0,
				Status: 503,
			},
		})

		// First error should always win
		errCfg := injector.ShouldInject()
		require.NotNil(t, errCfg)
		require.Equal(t, "first", errCfg.Name)
	})
}

func TestErrorInjector_InjectionRate(t *testing.T) {
	// Test that actual injection rate is close to configured rate
	expectedRate := 0.1 // 10%
	injector := NewErrorInjector([]*ErrorConfig{
		{
			Name:   "test_error",
			Rate:   expectedRate,
			Status: 503,
		},
	})

	// Sample many times
	samples := 10000
	injected := 0
	for range samples {
		if injector.ShouldInject() != nil {
			injected++
		}
	}

	actualRate := float64(injected) / float64(samples)

	// Allow 10% tolerance (e.g., 0.09-0.11 for 0.1)
	tolerance := 0.01
	delta := math.Abs(expectedRate - actualRate)

	require.Less(t, delta, tolerance,
		"expected rate ~%v, got %v (delta: %v, max allowed: %v)",
		expectedRate, actualRate, delta, tolerance)
}

func TestErrorInjector_WriteError(t *testing.T) {
	t.Run("writes status and body", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{})
		errCfg := &ErrorConfig{
			Name:   "test",
			Status: 503,
			Body:   `{"error":"service_unavailable"}`,
		}

		w := httptest.NewRecorder()
		injector.WriteError(w, errCfg)

		require.Equal(t, 503, w.Code)
		require.Equal(t, "application/json", w.Header().Get("Content-Type"))
		require.JSONEq(t, `{"error":"service_unavailable"}`, w.Body.String())
	})

	t.Run("writes custom headers", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{})
		errCfg := &ErrorConfig{
			Name:   "test",
			Status: 429,
			Headers: map[string]string{
				"Retry-After":    "60",
				"X-Custom-Header": "test-value",
			},
			Body: `{"error":"rate_limited"}`,
		}

		w := httptest.NewRecorder()
		injector.WriteError(w, errCfg)

		require.Equal(t, 429, w.Code)
		require.Equal(t, "60", w.Header().Get("Retry-After"))
		require.Equal(t, "test-value", w.Header().Get("X-Custom-Header"))
		require.JSONEq(t, `{"error":"rate_limited"}`, w.Body.String())
	})

	t.Run("empty body", func(t *testing.T) {
		injector := NewErrorInjector([]*ErrorConfig{})
		errCfg := &ErrorConfig{
			Name:   "test",
			Status: 500,
			Body:   "",
		}

		w := httptest.NewRecorder()
		injector.WriteError(w, errCfg)

		require.Equal(t, 500, w.Code)
		require.Empty(t, w.Body.String())
	})
}

func TestErrorInjectionStats(t *testing.T) {
	stats := NewErrorInjectionStats()

	// Record some requests and injections
	stats.RecordRequest()
	stats.RecordRequest()
	stats.RecordRequest()
	stats.RecordRequest()
	stats.RecordInjection("error1")
	stats.RecordInjection("error2")
	stats.RecordRequest()
	stats.RecordInjection("error1")

	require.Equal(t, 5, stats.TotalRequests)
	require.Equal(t, 2, stats.InjectedCount["error1"])
	require.Equal(t, 1, stats.InjectedCount["error2"])

	// Check rates
	require.InDelta(t, 0.4, stats.Rate("error1"), 0.01) // 2/5 = 0.4
	require.InDelta(t, 0.2, stats.Rate("error2"), 0.01) // 1/5 = 0.2
	require.Equal(t, 0.0, stats.Rate("nonexistent"))
}
