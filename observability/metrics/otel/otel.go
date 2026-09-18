// Package otel adapts observability/metrics.Recorder onto OpenTelemetry's
// metrics API, exporting via OTLP/HTTP — the same transport
// observability/tracer already uses for spans.
package otel

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	otelmetric "go.opentelemetry.io/otel/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"github.com/adehikmatfr/go-pkg/v2/observability/metrics"
)

// defaultExportInterval matches observability/tracer's precedent of a
// sensible production default the caller doesn't have to think about.
const defaultExportInterval = 15 * time.Second

// Config configures the OTLP/HTTP metrics exporter and export cadence.
type Config struct {
	// EndpointURL is a bare "host:port" (no scheme), e.g. "otel-collector:4318".
	EndpointURL string
	ServiceName string
	// ExportInterval is how often accumulated metrics are pushed to the
	// collector. Zero value (unset) defaults to 15s.
	ExportInterval time.Duration
}

type recorder struct {
	meter    otelmetric.Meter
	provider *metricsdk.MeterProvider

	mu         sync.Mutex
	counters   map[string]otelmetric.Float64Counter
	gauges     map[string]otelmetric.Float64Gauge
	histograms map[string]otelmetric.Float64Histogram

	closeOnce sync.Once
	closeErr  error
}

// New builds a metrics.Recorder backed by an OTLP/HTTP metrics exporter and
// installs it as the global MeterProvider. Callers must call Close during
// shutdown to flush pending metrics.
func New(cfg Config) (metrics.Recorder, error) {
	ctx := context.Background()

	exp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpoint(cfg.EndpointURL),
		otlpmetrichttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: create OTLP metric exporter: %w", err)
	}

	provider := metricsdk.NewMeterProvider(
		metricsdk.WithReader(metricsdk.NewPeriodicReader(exp, metricsdk.WithInterval(exportInterval(cfg.ExportInterval)))),
		metricsdk.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
		)),
	)
	otelapi.SetMeterProvider(provider)

	return NewFromProvider(provider, cfg.ServiceName), nil
}

// NewFromProvider wraps an already-built MeterProvider, holding every real
// method implementation. New dials a real OTLP endpoint and calls this;
// tests use it directly with a manual reader instead (mirrors
// datastore/storage/gcs's NewFromBucket / messaging/broker/kafka's
// NewConsumer precedent for making dial-free logic testable black-box).
func NewFromProvider(provider *metricsdk.MeterProvider, serviceName string) metrics.Recorder {
	return &recorder{
		meter:      provider.Meter(serviceName),
		provider:   provider,
		counters:   make(map[string]otelmetric.Float64Counter),
		gauges:     make(map[string]otelmetric.Float64Gauge),
		histograms: make(map[string]otelmetric.Float64Histogram),
	}
}

func exportInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultExportInterval
	}
	return d
}

// IncCounter increments the named counter by 1. See metrics.Recorder.
func (r *recorder) IncCounter(ctx context.Context, name string, labels metrics.Labels) {
	r.AddCounter(ctx, name, 1, labels)
}

// AddCounter increments the named counter by delta. See metrics.Recorder.
func (r *recorder) AddCounter(ctx context.Context, name string, delta float64, labels metrics.Labels) {
	c, err := r.counter(name)
	if err != nil {
		log.Error().Err(err).Str("metric", name).Msg("otel: failed to create counter instrument")
		return
	}
	c.Add(ctx, delta, otelmetric.WithAttributes(attributesFromLabels(labels)...))
}

// SetGauge records the named gauge's current value. See metrics.Recorder.
func (r *recorder) SetGauge(ctx context.Context, name string, value float64, labels metrics.Labels) {
	g, err := r.gauge(name)
	if err != nil {
		log.Error().Err(err).Str("metric", name).Msg("otel: failed to create gauge instrument")
		return
	}
	g.Record(ctx, value, otelmetric.WithAttributes(attributesFromLabels(labels)...))
}

// ObserveHistogram records a single observation. See metrics.Recorder.
func (r *recorder) ObserveHistogram(ctx context.Context, name string, value float64, labels metrics.Labels) {
	h, err := r.histogram(name)
	if err != nil {
		log.Error().Err(err).Str("metric", name).Msg("otel: failed to create histogram instrument")
		return
	}
	h.Record(ctx, value, otelmetric.WithAttributes(attributesFromLabels(labels)...))
}

// Close flushes and shuts down the exporter. Close is idempotent — a second
// call returns the same result as the first without shutting down the
// underlying MeterProvider again.
func (r *recorder) Close() error {
	r.closeOnce.Do(func() {
		if err := r.provider.Shutdown(context.Background()); err != nil {
			r.closeErr = fmt.Errorf("otel: shut down meter provider: %w", err)
		}
	})
	return r.closeErr
}

func (r *recorder) counter(name string) (otelmetric.Float64Counter, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c, nil
	}
	c, err := r.meter.Float64Counter(name)
	if err != nil {
		return nil, err
	}
	r.counters[name] = c
	return c, nil
}

func (r *recorder) gauge(name string) (otelmetric.Float64Gauge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gauges[name]; ok {
		return g, nil
	}
	g, err := r.meter.Float64Gauge(name)
	if err != nil {
		return nil, err
	}
	r.gauges[name] = g
	return g, nil
}

func (r *recorder) histogram(name string) (otelmetric.Float64Histogram, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if h, ok := r.histograms[name]; ok {
		return h, nil
	}
	h, err := r.meter.Float64Histogram(name)
	if err != nil {
		return nil, err
	}
	r.histograms[name] = h
	return h, nil
}

func attributesFromLabels(labels metrics.Labels) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}
