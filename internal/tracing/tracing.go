package tracing

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Config holds tracing configuration.
type Config struct {
	Enabled     bool
	ServiceName string
	Endpoint    string  // OTLP HTTP endpoint (e.g. "localhost:4318")
	Sampler     string  // "always_on", "always_off", "parent_based", "ratio"
	Ratio       float64 // only used when Sampler = "ratio"
}

// Provider wraps the OTel TracerProvider for lifecycle management.
type Provider struct {
	tp *sdktrace.TracerProvider
}

// Init initializes the OpenTelemetry tracing provider.
// If cfg.Enabled is false, returns a nil *Provider (the nil-safe Shutdown handles this).
// If endpoint is empty, it falls back to OTEL_EXPORTER_OTLP_ENDPOINT env var.
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Create resource with service name
	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceNameKey.String(cfg.ServiceName),
	)

	// Create OTLP HTTP exporter
	opts := []otlptracehttp.Option{}
	if cfg.Endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
	}
	// If no endpoint and no env var, the exporter will fail to connect silently

	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Map sampler string to SDK sampler
	sampler := samplerFromConfig(cfg.Sampler, cfg.Ratio)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global provider and propagator
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("tracing initialized", "service", cfg.ServiceName, "sampler", cfg.Sampler)
	return &Provider{tp: tp}, nil
}

// samplerFromConfig maps a sampler name to an SDK sampler.
func samplerFromConfig(name string, ratio float64) sdktrace.Sampler {
	switch name {
	case "always_off":
		return sdktrace.NeverSample()
	case "parent_based":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "ratio":
		return sdktrace.TraceIDRatioBased(ratio)
	default: // "always_on" or empty
		return sdktrace.AlwaysSample()
	}
}

// Shutdown flushes and shuts down the tracing provider.
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.tp == nil {
		return nil
	}
	return p.tp.Shutdown(ctx)
}

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}
