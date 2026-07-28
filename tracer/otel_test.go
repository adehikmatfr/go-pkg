package tracer

import (
	"context"
	"testing"
)

func TestSampleRatio(t *testing.T) {
	cases := []struct {
		name  string
		input float64
		want  float64
	}{
		{"zero defaults to 1", 0, 1},
		{"negative defaults to 1", -0.5, 1},
		{"valid fraction is preserved", 0.25, 0.25},
		{"one is preserved", 1, 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sampleRatio(tc.input); got != tc.want {
				t.Errorf("sampleRatio(%v) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestNewBuildsTracer(t *testing.T) {
	// otlptracehttp connects lazily, so New should succeed even against an
	// endpoint with nothing listening.
	tr, err := New(&Config{EndpointURL: "127.0.0.1:1", ServiceName: "test-service"})
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

func TestUninitializedTracerStartIsSafe(t *testing.T) {
	var m tracerModule
	ctx, span := m.Start(context.Background(), "test-span")
	if ctx == nil || span == nil {
		t.Error("Start() on zero-value tracerModule should return a usable no-op span")
	}
	m.Close() // must not panic
}
