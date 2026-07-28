// Package client provides gRPC and REST client helpers with tracing,
// timeouts, and retry support built in.
package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// ClientTracer starts spans for outgoing calls; tracer.Tracer satisfies it.
type ClientTracer interface {
	Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span)
	Close()
}

// GRPCClient builds gRPC client connections.
type GRPCClient interface {
	NewClient(ctx context.Context) (*grpc.ClientConn, error)
}

// Backoff configures the retry backoff used while (re)connecting.
type Backoff struct {
	BaseDelay  int     // Initial delay for backoff, in milliseconds.
	Multiplier float64 // Multiplier applied to the delay after each retry.
	Jitter     float64 // Fraction of randomness added to the delay.
	MaxDelay   int     // Maximum delay between retries, in milliseconds.
}

// ConnectParams configures gRPC's connection establishment behavior.
type ConnectParams struct {
	MinConnectTimeout int // Minimum time to wait for connection establishment, in milliseconds.
	Backoff           Backoff
}

// GrpcConfig configures the target server and connection behavior.
type GrpcConfig struct {
	Host          string
	Port          int
	ConnectParams ConnectParams
	// TLS enables transport security using the host's system CA pool. Leave
	// false only for local development or when the connection is otherwise
	// secured (e.g. a service mesh sidecar) — plaintext gRPC must not be used
	// over an untrusted network in production.
	TLS bool
}

// GRPCOpts constructs a GRPCModule.
type GRPCOpts struct {
	Cfg    *GrpcConfig
	Tracer ClientTracer
}

// GRPCModule is the default GRPCClient implementation.
type GRPCModule struct {
	cfg    *GrpcConfig
	tracer ClientTracer
}

// NewGRPCClient builds a GRPCClient from opts.
func NewGRPCClient(opts *GRPCOpts) GRPCClient {
	return &GRPCModule{
		cfg:    opts.Cfg,
		tracer: opts.Tracer,
	}
}

// NewClient creates a new gRPC client connection using the module's
// configuration. grpc.NewClient itself does not dial eagerly; the connection
// is established lazily on first RPC (or immediately if WithBlock were set,
// which this does not do).
func (m *GRPCModule) NewClient(ctx context.Context) (*grpc.ClientConn, error) {
	_, span := m.tracer.Start(ctx, "grpc.NewClient")
	defer span.End()

	target := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	dialOpts := m.getDefaultDialOptions()
	dialOpts = append(dialOpts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))

	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("client: failed to connect to %s: %w", target, err)
	}

	return conn, nil
}

// getDefaultDialOptions constructs the gRPC DialOptions shared by every call.
func (m *GRPCModule) getDefaultDialOptions() []grpc.DialOption {
	connParamsConf := m.cfg.ConnectParams
	backoffConf := connParamsConf.Backoff

	return []grpc.DialOption{
		grpc.WithTransportCredentials(m.transportCredentials()),
		grpc.WithUnaryInterceptor(unaryInterceptor),
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay:  time.Duration(backoffConf.BaseDelay) * time.Millisecond,
				Multiplier: backoffConf.Multiplier,
				Jitter:     backoffConf.Jitter,
				MaxDelay:   time.Duration(backoffConf.MaxDelay) * time.Millisecond,
			},
			MinConnectTimeout: time.Duration(connParamsConf.MinConnectTimeout) * time.Millisecond,
		}),
	}
}

func (m *GRPCModule) transportCredentials() credentials.TransportCredentials {
	if m.cfg.TLS {
		return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return insecure.NewCredentials()
}

// unaryInterceptor logs every outgoing RPC's method, duration, and error.
func unaryInterceptor(
	ctx context.Context,
	method string,
	req, reply interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption,
) error {
	start := time.Now()
	err := invoker(ctx, method, req, reply, cc, opts...)

	event := log.Info()
	if err != nil {
		event = log.Error().Err(err)
	}
	event.Str("method", method).Dur("duration", time.Since(start)).Msg("grpc client call")

	return err
}
