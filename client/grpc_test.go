package client

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/credentials/insecure"
)

type noopClientTracer struct{}

func (noopClientTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return noop.NewTracerProvider().Tracer("test").Start(ctx, spanName, opts...)
}

func (noopClientTracer) Close() {}

func TestTransportCredentialsInsecureByDefault(t *testing.T) {
	m := &GRPCModule{cfg: &GrpcConfig{}}
	got := m.transportCredentials()
	if got.Info().SecurityProtocol != insecure.NewCredentials().Info().SecurityProtocol {
		t.Errorf("transportCredentials() = %v, want insecure", got.Info())
	}
}

func TestTransportCredentialsTLSWhenEnabled(t *testing.T) {
	m := &GRPCModule{cfg: &GrpcConfig{TLS: true}}
	got := m.transportCredentials()
	if got.Info().SecurityProtocol == insecure.NewCredentials().Info().SecurityProtocol {
		t.Error("transportCredentials() with TLS: true should not be insecure")
	}
}

func TestGetDefaultDialOptions(t *testing.T) {
	m := &GRPCModule{cfg: &GrpcConfig{
		ConnectParams: ConnectParams{
			MinConnectTimeout: 1000,
			Backoff: Backoff{
				BaseDelay:  100,
				Multiplier: 1.5,
				Jitter:     0.2,
				MaxDelay:   5000,
			},
		},
	}}

	opts := m.getDefaultDialOptions()
	if len(opts) != 3 {
		t.Errorf("getDefaultDialOptions() returned %d options, want 3 (credentials, interceptor, connect params)", len(opts))
	}
}

func TestNewClientDoesNotDialEagerly(t *testing.T) {
	// grpc.NewClient only validates the target and prepares the connection;
	// it must not block or error even when nothing is listening, since the
	// actual dial happens lazily on first RPC.
	m := &GRPCModule{
		cfg:    &GrpcConfig{Host: "127.0.0.1", Port: 1},
		tracer: noopClientTracer{},
	}

	conn, err := m.NewClient(context.Background())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = conn.Close() }()
}
