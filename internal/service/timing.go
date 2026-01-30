package service

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// TimingConfig defines latency injection parameters
type TimingConfig struct {
	P50      time.Duration // 50th percentile latency
	P90      time.Duration // 90th percentile latency
	P99      time.Duration // 99th percentile latency
	Variance float64       // Variance factor (0.0-1.0)
}

// LatencyInjector injects latency based on percentile distribution
type LatencyInjector struct {
	config TimingConfig
	rng    *rand.Rand
}

// NewLatencyInjector creates a new latency injector
func NewLatencyInjector(config TimingConfig) *LatencyInjector {
	return &LatencyInjector{
		config: config,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Inject adds latency based on percentile distribution
func (l *LatencyInjector) Inject(ctx context.Context) {
	delay := l.calculateDelay()

	select {
	case <-time.After(delay):
		return
	case <-ctx.Done():
		return
	}
}

// calculateDelay determines the delay based on percentile distribution
// This uses a simple approach: generate a random percentile, then interpolate
// between the configured percentile values
func (l *LatencyInjector) calculateDelay() time.Duration {
	// Generate random percentile (0-100)
	percentile := l.rng.Float64() * 100

	var baseDelay time.Duration

	// Interpolate based on percentile ranges
	switch {
	case percentile <= 50:
		// 0-50th percentile: use p50 as max
		baseDelay = l.config.P50
	case percentile <= 90:
		// 50-90th percentile: interpolate between p50 and p90
		ratio := (percentile - 50) / 40 // 0-1 in this range
		baseDelay = l.interpolate(l.config.P50, l.config.P90, ratio)
	case percentile <= 99:
		// 90-99th percentile: interpolate between p90 and p99
		ratio := (percentile - 90) / 9 // 0-1 in this range
		baseDelay = l.interpolate(l.config.P90, l.config.P99, ratio)
	default:
		// 99-100th percentile: use p99 as base
		baseDelay = l.config.P99
	}

	// Apply variance
	if l.config.Variance > 0 {
		// Add random variance: ±variance%
		varianceFactor := 1.0 + (l.rng.Float64()*2-1)*l.config.Variance
		baseDelay = time.Duration(float64(baseDelay) * varianceFactor)
	}

	return baseDelay
}

// interpolate linearly interpolates between two durations
func (l *LatencyInjector) interpolate(d1, d2 time.Duration, ratio float64) time.Duration {
	d1f := float64(d1)
	d2f := float64(d2)
	return time.Duration(d1f + (d2f-d1f)*ratio)
}

// ParseDuration parses a duration string (e.g., "10ms", "1s", "500ms")
func ParseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// CalculateActualPercentiles is a helper function for testing
// It runs the injector N times and calculates actual p50, p90, p99
func (l *LatencyInjector) CalculateActualPercentiles(samples int) (p50, p90, p99 time.Duration) {
	delays := make([]time.Duration, samples)

	for i := 0; i < samples; i++ {
		delays[i] = l.calculateDelay()
	}

	// Sort delays (simple bubble sort for small samples)
	for i := range len(delays) {
		for j := i + 1; j < len(delays); j++ {
			if delays[i] > delays[j] {
				delays[i], delays[j] = delays[j], delays[i]
			}
		}
	}

	// Calculate percentiles
	p50 = delays[int(math.Floor(float64(samples)*0.50))]
	p90 = delays[int(math.Floor(float64(samples)*0.90))]
	p99 = delays[int(math.Floor(float64(samples)*0.99))]

	return
}
