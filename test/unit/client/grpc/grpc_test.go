package grpc_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	ourgrpc "github.com/adehikmatfr/go-pkg/v2/client/grpc"
	grpcmock "github.com/adehikmatfr/go-pkg/v2/test/mock/client/grpc"
)

// testConnectParams gives the dialer a real MinConnectTimeout — the zero
// value would otherwise make grpc treat every connection attempt as
// instantly expired against a real listener.
var testConnectParams = ourgrpc.ConnectParams{MinConnectTimeout: 5000}

// expectTracerStart returns a ClientTracer mock expecting exactly one Start
// call and delegating it to a real no-op OpenTelemetry tracer, so the test
// doesn't need to hand-construct a trace.Span.
func expectTracerStart(t *testing.T) *grpcmock.ClientTracer {
	t.Helper()
	tr := grpcmock.NewClientTracer(t)
	tr.EXPECT().Start(mock.Anything, mock.Anything).RunAndReturn(
		func(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
			return noop.NewTracerProvider().Tracer("test").Start(ctx, name, opts...)
		},
	)
	return tr
}

// startPlaintextHealthServer starts a real, unencrypted gRPC server on a
// loopback port serving the standard health-check service, so tests can
// dial it with a real client instead of asserting on internal dial options.
func startPlaintextHealthServer(t *testing.T) (host string, port int) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error: %v", err)
	}

	srv := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)
	healthgrpc.RegisterHealthServer(srv, hs)

	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	addr := lis.Addr().(*net.TCPAddr)
	_, portStr, err := net.SplitHostPort(addr.String())
	if err != nil {
		t.Fatalf("SplitHostPort() error: %v", err)
	}
	p, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("Atoi() error: %v", err)
	}
	return addr.IP.String(), p
}

func TestNewGRPCClientBuildsModule(t *testing.T) {
	c := ourgrpc.NewGRPCClient(&ourgrpc.GRPCOpts{
		Cfg:    &ourgrpc.GrpcConfig{Host: "127.0.0.1", Port: 1},
		Tracer: grpcmock.NewClientTracer(t), // NewGRPCClient itself never calls the tracer
	})
	if c == nil {
		t.Fatal("NewGRPCClient() returned nil")
	}
}

func TestNewClientDoesNotDialEagerly(t *testing.T) {
	// grpc.NewClient only validates the target and prepares the connection;
	// it must not block or error even when nothing is listening, since the
	// actual dial happens lazily on first RPC.
	c := ourgrpc.NewGRPCClient(&ourgrpc.GRPCOpts{
		Cfg:    &ourgrpc.GrpcConfig{Host: "127.0.0.1", Port: 1},
		Tracer: expectTracerStart(t),
	})

	conn, err := c.NewClient(context.Background())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = conn.Close() }()
}

// TestInsecureClientReachesPlaintextServer proves the insecure dial path
// (transportCredentials + getDefaultDialOptions + unaryInterceptor, none of
// which are exported) actually works end-to-end: a real RPC over a real
// loopback connection succeeds.
func TestInsecureClientReachesPlaintextServer(t *testing.T) {
	host, port := startPlaintextHealthServer(t)

	c := ourgrpc.NewGRPCClient(&ourgrpc.GRPCOpts{
		Cfg:    &ourgrpc.GrpcConfig{Host: host, Port: port, TLS: false, ConnectParams: testConnectParams},
		Tracer: expectTracerStart(t),
	})

	conn, err := c.NewClient(context.Background())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := healthgrpc.NewHealthClient(conn).Check(ctx, &healthgrpc.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check() error: %v, want a successful plaintext RPC", err)
	}
	if resp.Status != healthgrpc.HealthCheckResponse_SERVING {
		t.Errorf("Status = %v, want SERVING", resp.Status)
	}
}

// TestTLSClientCannotReachPlaintextServer proves the TLS dial path is a
// genuinely different code path from the insecure one: a client configured
// for TLS fails its handshake against a server that never negotiates TLS.
func TestTLSClientCannotReachPlaintextServer(t *testing.T) {
	host, port := startPlaintextHealthServer(t)

	c := ourgrpc.NewGRPCClient(&ourgrpc.GRPCOpts{
		Cfg:    &ourgrpc.GrpcConfig{Host: host, Port: port, TLS: true},
		Tracer: expectTracerStart(t),
	})

	conn, err := c.NewClient(context.Background())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = healthgrpc.NewHealthClient(conn).Check(ctx, &healthgrpc.HealthCheckRequest{})
	if err == nil {
		t.Fatal("Check() over TLS against a plaintext server should fail, not succeed")
	}
}
