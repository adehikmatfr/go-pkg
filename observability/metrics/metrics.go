// Package metrics defines the Recorder port: a small, vendor-agnostic API
// for recording application metrics (counters, gauges, histograms).
// Concrete backends live in subpackages (e.g. metrics/otel, metrics/prometheus)
// that implement this interface — swapping the backend is a one-line
// constructor change for the consumer, nothing else.
package metrics

import "context"

// Labels are string key/value pairs attached to a single metric observation,
// mapped onto whatever label/attribute mechanism the backend uses.
type Labels map[string]string

// Recorder records application metrics. Recording a metric must never be
// able to fail the caller's request: every method here is fire-and-forget
// from the caller's point of view — a misconfigured or unreachable metrics
// backend is a Recorder implementation detail (logged internally by the
// adapter), not something business logic should branch on.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Recorder interface {
	// IncCounter increments the named monotonic counter by 1.
	IncCounter(ctx context.Context, name string, labels Labels)
	// AddCounter increments the named monotonic counter by delta, which must
	// be non-negative.
	AddCounter(ctx context.Context, name string, delta float64, labels Labels)
	// SetGauge records the named gauge's current value, replacing whatever
	// value was last recorded for the same label set.
	SetGauge(ctx context.Context, name string, value float64, labels Labels)
	// ObserveHistogram records a single observation of value for the named
	// histogram.
	ObserveHistogram(ctx context.Context, name string, value float64, labels Labels)
	// Close releases any resource held by the recorder (an exporter, a
	// background flush goroutine, an HTTP server exposing /metrics). Call it
	// during shutdown.
	Close() error
}
