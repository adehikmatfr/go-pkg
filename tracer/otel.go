// Package tracer wires up an OpenTelemetry TracerProvider exporting spans via
// OTLP/HTTP, and installs it as the global tracer/propagator.
package tracer

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer starts spans and releases the underlying exporter on Close.
type Tracer interface {
	Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span)
	Close()
}

// Config configures the OTLP/HTTP exporter and sampling behavior.
type Config struct {
	// EndpointURL is a bare "host:port" (no scheme), e.g. "tempo:4318".
	EndpointURL string
	ServiceName string
	// SampleRatio is the fraction of traces to sample, in [0, 1]. Zero value
	// (unset) defaults to 1.0 (sample everything) to preserve prior behavior
	// for existing callers; set it explicitly in high-throughput production
	// services to control exporter/collector load.
	SampleRatio float64
}

type tracerModule struct {
	tracerProvider *tracesdk.TracerProvider
	serviceName    string
}

// New builds and installs the global TracerProvider. Callers must invoke
// Close on the returned Tracer during shutdown to flush pending spans.
func New(cfg *Config) (Tracer, error) {
	tp, err := initTracerProvider(cfg)
	if err != nil {
		return nil, err
	}

	return &tracerModule{
		tracerProvider: tp,
		serviceName:    cfg.ServiceName,
	}, nil
}

func initTracerProvider(cfg *Config) (*tracesdk.TracerProvider, error) {
	ctx := context.Background()

	client := otlptracehttp.NewClient(
		otlptracehttp.WithEndpoint(cfg.EndpointURL),
		otlptracehttp.WithInsecure(),
	)

	exp, err := otlptrace.New(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exp),
		tracesdk.WithSampler(tracesdk.ParentBased(tracesdk.TraceIDRatioBased(sampleRatio(cfg.SampleRatio)))),
		tracesdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(
		propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		),
	)

	return tp, nil
}

func sampleRatio(r float64) float64 {
	if r <= 0 {
		return 1
	}
	return r
}

func (o *tracerModule) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	if o.tracerProvider == nil {
		return ctx, trace.SpanFromContext(ctx)
	}

	tracer := o.tracerProvider.Tracer(o.serviceName)
	return tracer.Start(ctx, spanName, opts...)
}

// Close flushes and shuts down the exporter. Safe to call even if New failed
// to fully initialize.
func (o *tracerModule) Close() {
	if o.tracerProvider == nil {
		return
	}

	if err := o.tracerProvider.Shutdown(context.Background()); err != nil {
		log.Error().Err(err).Msg("failed to shut down OpenTelemetry tracer provider")
	}
}
