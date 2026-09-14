package tracer_test

import (
	"context"
	"testing"

	"github.com/adehikmatfr/go-pkg/v2/observability/tracer"
)

func TestNewBuildsTracer(t *testing.T) {
	// otlptracehttp connects lazily, so New should succeed even against an
	// endpoint with nothing listening.
	tr, err := tracer.New(&tracer.Config{EndpointURL: "127.0.0.1:1", ServiceName: "test-service"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer tr.Close()

	ctx, span := tr.Start(context.Background(), "test-span")
	if ctx == nil {
		t.Error("Start() returned nil context")
	}
	span.End()
}

func TestNewWithExplicitSampleRatio(t *testing.T) {
	// Exercises the "valid fraction preserved" branch of the unexported
	// sampleRatio helper (TestNewBuildsTracer above exercises the "zero
	// defaults to 1" branch via SampleRatio's zero value).
	tr, err := tracer.New(&tracer.Config{EndpointURL: "127.0.0.1:1", ServiceName: "test-service", SampleRatio: 0.5})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer tr.Close()

	ctx, span := tr.Start(context.Background(), "test-span")
	if ctx == nil {
		t.Error("Start() returned nil context")
	}
	span.End()
}
