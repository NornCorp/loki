package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// resetGlobalOTel resets the global OTel state for test isolation.
func resetGlobalOTel(t *testing.T) {
	t.Helper()
	otel.SetTracerProvider(noop.NewTracerProvider())
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
}

func TestInit_Disabled(t *testing.T) {
	resetGlobalOTel(t)

	provider, err := Init(context.Background(), Config{
		Enabled:     false,
		ServiceName: "test",
	})
	require.NoError(t, err)
	require.Nil(t, provider, "disabled tracing should return nil provider")

	// Global provider should remain noop
	tp := otel.GetTracerProvider()
	_, ok := tp.(*sdktrace.TracerProvider)
	require.False(t, ok, "global provider should not be an SDK TracerProvider when disabled")
}

func TestInit_Enabled(t *testing.T) {
	resetGlobalOTel(t)

	provider, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "test-svc",
		Sampler:     "always_on",
	})
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())

	// Global provider should be an SDK TracerProvider
	tp := otel.GetTracerProvider()
	_, ok := tp.(*sdktrace.TracerProvider)
	require.True(t, ok, "global provider should be an SDK TracerProvider")
}

func TestInit_CustomEndpoint(t *testing.T) {
	resetGlobalOTel(t)

	// Using a non-routable endpoint; we just verify Init succeeds
	// and the provider is created (the batcher won't export in tests).
	provider, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "test-svc",
		Endpoint:    "127.0.0.1:0",
		Sampler:     "always_on",
	})
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())
}

func TestInit_Sampler_AlwaysOff(t *testing.T) {
	resetGlobalOTel(t)

	provider, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "test-svc",
		Sampler:     "always_off",
	})
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())
}

func TestInit_Sampler_ParentBased(t *testing.T) {
	resetGlobalOTel(t)

	provider, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "test-svc",
		Sampler:     "parent_based",
	})
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())
}

func TestInit_Sampler_Ratio(t *testing.T) {
	resetGlobalOTel(t)

	provider, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "test-svc",
		Sampler:     "ratio",
		Ratio:       0.1,
	})
	require.NoError(t, err)
	require.NotNil(t, provider)
	defer provider.Shutdown(context.Background())
}

func TestInit_SetsPropagator(t *testing.T) {
	resetGlobalOTel(t)

	provider, err := Init(context.Background(), Config{
		Enabled:     true,
		ServiceName: "test-svc",
		Sampler:     "always_on",
	})
	require.NoError(t, err)
	defer provider.Shutdown(context.Background())

	prop := otel.GetTextMapPropagator()
	fields := prop.Fields()
	require.Contains(t, fields, "traceparent")
	require.Contains(t, fields, "baggage")
}

func TestShutdown_NilProvider(t *testing.T) {
	var p *Provider
	err := p.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestShutdown_NilTP(t *testing.T) {
	p := &Provider{tp: nil}
	err := p.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestSamplerFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		sampler string
		ratio   float64
		want    string
	}{
		{"always_on", "always_on", 0, "AlwaysOnSampler"},
		{"always_off", "always_off", 0, "AlwaysOffSampler"},
		{"parent_based", "parent_based", 0, "ParentBased"},
		{"ratio", "ratio", 0.5, "TraceIDRatioBased"},
		{"empty defaults to always_on", "", 0, "AlwaysOnSampler"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := samplerFromConfig(tt.sampler, tt.ratio)
			desc := s.Description()
			require.Contains(t, desc, tt.want)
		})
	}
}
