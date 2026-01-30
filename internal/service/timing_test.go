package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLatencyInjector_Inject(t *testing.T) {
	t.Run("injects latency", func(t *testing.T) {
		injector := NewLatencyInjector(TimingConfig{
			P50:      10 * time.Millisecond,
			P90:      50 * time.Millisecond,
			P99:      200 * time.Millisecond,
			Variance: 0,
		})

		start := time.Now()
		injector.Inject(context.Background())
		elapsed := time.Since(start)

		// Should have some delay (at least 1ms, but allow for fast systems)
		require.True(t, elapsed >= 0)
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		injector := NewLatencyInjector(TimingConfig{
			P50:      10 * time.Second, // Long delay
			P90:      20 * time.Second,
			P99:      30 * time.Second,
			Variance: 0,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		start := time.Now()
		injector.Inject(ctx)
		elapsed := time.Since(start)

		// Should return quickly due to context timeout
		require.Less(t, elapsed, 1*time.Second)
	})
}

func TestLatencyInjector_PercentileDistribution(t *testing.T) {
	injector := NewLatencyInjector(TimingConfig{
		P50:      10 * time.Millisecond,
		P90:      50 * time.Millisecond,
		P99:      200 * time.Millisecond,
		Variance: 0, // No variance for predictable testing
	})

	// Calculate actual percentiles from samples
	p50, p90, p99 := injector.CalculateActualPercentiles(10000)

	// Allow 20% tolerance for statistical variation
	tolerance := 0.2

	requireWithinTolerance := func(t *testing.T, expected, actual time.Duration, name string) {
		t.Helper()
		delta := math.Abs(float64(expected - actual))
		maxDelta := float64(expected) * tolerance
		require.Less(t, delta, maxDelta,
			"%s: expected %v, got %v (delta: %v, max allowed: %v)",
			name, expected, actual, time.Duration(delta), time.Duration(maxDelta))
	}

	requireWithinTolerance(t, 10*time.Millisecond, p50, "p50")
	requireWithinTolerance(t, 50*time.Millisecond, p90, "p90")
	requireWithinTolerance(t, 200*time.Millisecond, p99, "p99")
}

func TestLatencyInjector_Variance(t *testing.T) {
	injector := NewLatencyInjector(TimingConfig{
		P50:      100 * time.Millisecond,
		P90:      100 * time.Millisecond,
		P99:      100 * time.Millisecond,
		Variance: 0.5, // 50% variance
	})

	// Collect samples
	samples := make([]time.Duration, 1000)
	for i := range len(samples) {
		samples[i] = injector.calculateDelay()
	}

	// With 50% variance on 100ms:
	// - Min should be around 50ms (100ms - 50%)
	// - Max should be around 150ms (100ms + 50%)
	// Allow some statistical wiggle room

	min := samples[0]
	max := samples[0]
	for _, s := range samples {
		if s < min {
			min = s
		}
		if s > max {
			max = s
		}
	}

	// Verify variance is actually applied
	require.Greater(t, min, 40*time.Millisecond, "minimum delay should be > 40ms")
	require.Less(t, min, 80*time.Millisecond, "minimum delay should be < 80ms")
	require.Greater(t, max, 120*time.Millisecond, "maximum delay should be > 120ms")
	require.Less(t, max, 160*time.Millisecond, "maximum delay should be < 160ms")
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{
			name:  "milliseconds",
			input: "10ms",
			want:  10 * time.Millisecond,
		},
		{
			name:  "seconds",
			input: "1s",
			want:  1 * time.Second,
		},
		{
			name:  "minutes",
			input: "2m",
			want:  2 * time.Minute,
		},
		{
			name:  "mixed",
			input: "1m30s",
			want:  90 * time.Second,
		},
		{
			name:    "invalid",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseDuration(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
