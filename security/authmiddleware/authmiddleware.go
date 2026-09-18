// Package authmiddleware provides reusable HTTP and gRPC server middleware
// that validates a bearer token issued by security/jwt and injects its
// claims into the request context, so a handler can read the caller's
// identity without re-parsing the token itself.
package authmiddleware

import (
	"context"
	"errors"
	"strings"

	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

// ErrMissingToken is returned when a request carries no usable bearer token
// (missing header, wrong scheme, or empty token).
var ErrMissingToken = errors.New("authmiddleware: missing bearer token")

// bearerPrefix is the standard "Authorization: Bearer <token>" scheme.
const bearerPrefix = "Bearer "

// BearerToken extracts the token from an "Authorization" header value in the
// form "Bearer <token>", or ErrMissingToken if the header is absent, uses a
// different scheme, or carries an empty token.
func BearerToken(header string) (string, error) {
	if !strings.HasPrefix(header, bearerPrefix) {
		return "", ErrMissingToken
	}
	token := strings.TrimSpace(header[len(bearerPrefix):])
	if token == "" {
		return "", ErrMissingToken
	}
	return token, nil
}

// contextKey is unexported so the Claims context key cannot collide with
// keys set by other packages.
type contextKey struct{}

var claimsContextKey = contextKey{}

// WithClaims returns a copy of ctx carrying claims.
func WithClaims(ctx context.Context, claims *jwt.Claims) context.Context {
	return context.WithValue(ctx, claimsContextKey, claims)
}

// ClaimsFromContext extracts the Claims injected by WithClaims. The boolean
// is false when no claims are present.
func ClaimsFromContext(ctx context.Context) (*jwt.Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*jwt.Claims)
	return claims, ok
}
