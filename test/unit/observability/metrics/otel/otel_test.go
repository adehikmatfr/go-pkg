package otel_test

import (
	"context"
	"testing"
	"time"

	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/adehikmatfr/go-pkg/v2/observability/metrics"
	otelmetrics "github.com/adehikmatfr/go-pkg/v2/observability/metrics/otel"
)

func TestNewDoesNotDialEagerly(t *testing.T) {
	// otlpmetrichttp connects lazily on the first periodic export, so New
	// should succeed even against an endpoint with nothing listening.
	rec, err := otelmetrics.New(otelmetrics.Config{EndpointURL: "127.0.0.1:1", ServiceName: "test-service"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = rec.Close() }()

	rec.IncCounter(context.Background(), "requests_total", nil)
}

func TestNewWithExplicitExportInterval(t *testing.T) {
	// Exercises the "valid interval preserved" branch of the unexported
	// exportInterval helper (TestNewDoesNotDialEagerly exercises the "zero
	// defaults to 15s" branch via ExportInterval's zero value).
	rec, err := otelmetrics.New(otelmetrics.Config{
		EndpointURL:    "127.0.0.1:1",
		ServiceName:    "test-service",
		ExportInterval: time.Second,
	})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = rec.Close() }()
}

// newTestRecorder builds a metrics.Recorder against a real MeterProvider
// backed by a metricsdk.ManualReader, so tests can collect and assert on
// actually-recorded data without a live OTLP collector — the same seam
// pattern datastore/storage/gcs uses with memblob via NewFromBucket.
func newTestRecorder(t *testing.T) (metrics.Recorder, *metricsdk.ManualReader) {
	t.Helper()
	reader := metricsdk.NewManualReader()
	provider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
	rec := otelmetrics.NewFromProvider(provider, "test-service")
	t.Cleanup(func() { _ = rec.Close() })
	return rec, reader
}

func collect(t *testing.T, reader *metricsdk.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

func TestIncCounter(t *testing.T) {
	rec, reader := newTestRecorder(t)
	ctx := context.Background()

	rec.IncCounter(ctx, "requests_total", metrics.Labels{"route": "/health"})
	rec.IncCounter(ctx, "requests_total", metrics.Labels{"route": "/health"})

	m, ok := findMetric(collect(t, reader), "requests_total")
	if !ok {
		t.Fatal("requests_total metric not found after collect")
	}
	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("Data type = %T, want metricdata.Sum[float64]", m.Data)
	}
	if len(sum.DataPoints) != 1 {
		t.Fatalf("len(DataPoints) = %d, want 1", len(sum.DataPoints))
	}
	if got := sum.DataPoints[0].Value; got != 2 {
		t.Errorf("Value = %v, want 2", got)
	}
}

func TestAddCounter(t *testing.T) {
	rec, reader := newTestRecorder(t)
	ctx := context.Background()

	rec.AddCounter(ctx, "bytes_sent_total", 128, nil)
	rec.AddCounter(ctx, "bytes_sent_total", 32, nil)

	m, ok := findMetric(collect(t, reader), "bytes_sent_total")
	if !ok {
		t.Fatal("bytes_sent_total metric not found after collect")
	}
	sum := m.Data.(metricdata.Sum[float64])
	if got := sum.DataPoints[0].Value; got != 160 {
		t.Errorf("Value = %v, want 160", got)
	}
}

func TestSetGauge(t *testing.T) {
	rec, reader := newTestRecorder(t)
	ctx := context.Background()

	rec.SetGauge(ctx, "queue_depth", 7, nil)
	rec.SetGauge(ctx, "queue_depth", 3, nil)

	m, ok := findMetric(collect(t, reader), "queue_depth")
	if !ok {
		t.Fatal("queue_depth metric not found after collect")
	}
	gauge, ok := m.Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("Data type = %T, want metricdata.Gauge[float64]", m.Data)
	}
	if got := gauge.DataPoints[0].Value; got != 3 {
		t.Errorf("Value = %v, want the last-recorded value 3, got %v", got, got)
	}
}

func TestObserveHistogram(t *testing.T) {
	rec, reader := newTestRecorder(t)
	ctx := context.Background()

	rec.ObserveHistogram(ctx, "request_duration_seconds", 0.25, nil)
	rec.ObserveHistogram(ctx, "request_duration_seconds", 0.75, nil)

	m, ok := findMetric(collect(t, reader), "request_duration_seconds")
	if !ok {
		t.Fatal("request_duration_seconds metric not found after collect")
	}
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("Data type = %T, want metricdata.Histogram[float64]", m.Data)
	}
	if got := hist.DataPoints[0].Count; got != 2 {
		t.Errorf("Count = %d, want 2", got)
	}
	if got := hist.DataPoints[0].Sum; got != 1.0 {
		t.Errorf("Sum = %v, want 1.0", got)
	}
}

func TestLabelsBecomeDistinctTimeseries(t *testing.T) {
	rec, reader := newTestRecorder(t)
	ctx := context.Background()

	rec.IncCounter(ctx, "requests_total", metrics.Labels{"route": "/a"})
	rec.IncCounter(ctx, "requests_total", metrics.Labels{"route": "/b"})
	rec.IncCounter(ctx, "requests_total", metrics.Labels{"route": "/b"})

	m, ok := findMetric(collect(t, reader), "requests_total")
	if !ok {
		t.Fatal("requests_total metric not found after collect")
	}
	sum := m.Data.(metricdata.Sum[float64])
	if len(sum.DataPoints) != 2 {
		t.Fatalf("len(DataPoints) = %d, want 2 distinct label sets", len(sum.DataPoints))
	}
}

func TestClose(t *testing.T) {
	rec, _ := newTestRecorder(t)
	if err := rec.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	// Close is idempotent per the underlying MeterProvider's own contract.
	if err := rec.Close(); err != nil {
		t.Fatalf("second Close() error: %v", err)
	}
}
