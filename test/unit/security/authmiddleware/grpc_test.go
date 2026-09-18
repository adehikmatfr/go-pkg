package authmiddleware_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/adehikmatfr/go-pkg/v2/security/authmiddleware"
	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

// captureHandler records the context it was called with and returns a fixed
// response, so a test can assert both "handler was called" and "claims were
// injected" without a real gRPC server.
func captureHandler(t *testing.T) (grpc.UnaryHandler, *context.Context) {
	t.Helper()
	var captured context.Context
	return func(ctx context.Context, req interface{}) (interface{}, error) {
		captured = ctx
		return "ok", nil
	}, &captured
}

func TestUnaryServerInterceptorValidToken(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	token, err := signer.Generate("user-1", "admin", time.Minute)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	interceptor := authmiddleware.UnaryServerInterceptor(signer)
	handler, captured := captureHandler(t)

	md := metadata.Pairs("authorization", "Bearer "+token)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	if err != nil {
		t.Fatalf("interceptor() error: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("interceptor() response = %v, want %q", resp, "ok")
	}

	claims, ok := authmiddleware.ClaimsFromContext(*captured)
	if !ok {
		t.Fatal("handler's context has no claims")
	}
	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
	}
}

func TestUnaryServerInterceptorMissingMetadata(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	interceptor := authmiddleware.UnaryServerInterceptor(signer)
	handler, _ := captureHandler(t)

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	assertUnauthenticated(t, err)
}

func TestUnaryServerInterceptorMissingAuthorizationKey(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	interceptor := authmiddleware.UnaryServerInterceptor(signer)
	handler, _ := captureHandler(t)

	ctx := metadata.NewIncomingContext(context.Background(), metadata.MD{})
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	assertUnauthenticated(t, err)
}

func TestUnaryServerInterceptorInvalidScheme(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	interceptor := authmiddleware.UnaryServerInterceptor(signer)
	handler, _ := captureHandler(t)

	md := metadata.Pairs("authorization", "Basic abc")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	assertUnauthenticated(t, err)
}

func TestUnaryServerInterceptorInvalidToken(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	interceptor := authmiddleware.UnaryServerInterceptor(signer)
	handler, _ := captureHandler(t)

	md := metadata.Pairs("authorization", "Bearer not-a-real-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	assertUnauthenticated(t, err)
}

func assertUnauthenticated(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("interceptor() error = nil, want codes.Unauthenticated")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unauthenticated {
		t.Fatalf("interceptor() error = %v, want a codes.Unauthenticated status", err)
	}
}
