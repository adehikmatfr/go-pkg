package authmiddleware

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

// metadataKey is the gRPC metadata key carrying the bearer token, matching
// the HTTP "Authorization" header convention. grpc's metadata.MD lower-cases
// keys, so this is compared case-insensitively regardless of how the client
// sent it.
const metadataKey = "authorization"

// UnaryServerInterceptor returns a grpc.UnaryServerInterceptor that
// validates the incoming call's bearer token (carried in the "authorization"
// metadata key) using signer and injects its Claims into the handler's
// context via WithClaims. A missing or invalid token short-circuits with a
// codes.Unauthenticated error and never calls handler.
func UnaryServerInterceptor(signer jwt.Signer) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		values := md.Get(metadataKey)
		if len(values) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization metadata")
		}

		token, err := BearerToken(values[0])
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization metadata")
		}

		claims, err := signer.Parse(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid token")
		}

		return handler(WithClaims(ctx, claims), req)
	}
}
