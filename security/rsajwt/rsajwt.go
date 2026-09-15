// Package rsajwt issues and verifies RS256-signed JSON Web Tokens carrying an
// actor's identity, roles, and scopes for edge/zero-trust authentication —
// where a token is minted once by an issuer service and verified offline, by
// signature and key-set lookup alone, by every other service that receives it.
//
// This is a distinct trust model from security/jwt: security/jwt signs and
// verifies with one shared HMAC secret (useful when the same service, or a
// tightly-coupled set of services holding the same secret, does both).
// rsajwt separates minting (holds the RSA private key) from verifying (holds
// only public keys via a KeySetProvider), so a token can be trusted by many
// services that never see the signing key, and keys can rotate via "kid"
// without redeploying every verifier.
package rsajwt

import (
	"context"
	"crypto/rsa"
	"errors"
	"slices"
)

// Sentinel errors returned by this package. Implementations wrap them, so
// match with errors.Is — the underlying cause stays available for logging
// while the category stays stable.
var (
	// ErrInvalidToken rejects a token that failed to parse, whose signature
	// did not verify, or whose issuer/audience claim failed validation.
	ErrInvalidToken = errors.New("rsajwt: invalid token")
	// ErrTokenExpired is a distinct category from ErrInvalidToken so a caller
	// can prompt a refresh instead of a full re-authentication.
	ErrTokenExpired = errors.New("rsajwt: token expired")
	// ErrNoSigningKey indicates the issuer has no usable RSA signing key, or a
	// KeySetProvider has no usable public key.
	ErrNoSigningKey = errors.New("rsajwt: no signing key configured")
)

// Actor is the authenticated principal carried by a token.
type Actor struct {
	// ID is the stable identifier of the principal (the token's "sub" claim).
	ID string
	// Roles are the authorization roles granted to the principal.
	Roles []string
	// Scopes are fine-grained authorization scopes, carried in the
	// OAuth-standard space-delimited "scope" claim.
	Scopes []string
	// Issuer is the "iss" claim the token was minted with.
	Issuer string
	// Audience is the "aud" claim list the token was minted for.
	Audience []string
	// TokenID is the token's unique "jti" claim, useful for audit/revocation.
	TokenID string
}

// HasRole reports whether the actor was granted role. Comparison is exact and
// case-sensitive.
func (a Actor) HasRole(role string) bool {
	return slices.Contains(a.Roles, role)
}

// IssuedToken is a minted token plus metadata a caller can record for audit
// without re-parsing it.
type IssuedToken struct {
	// Token is the compact signed JWT placed in an Authorization bearer header.
	Token string
	// TokenID is the token's "jti".
	TokenID string
	// ExpiresAt is the token's "exp", in Unix seconds.
	ExpiresAt int64
	// IssuedAt is the token's "iat", in Unix seconds.
	IssuedAt int64
}

// Issuer mints signed access tokens for authenticated actors.
type Issuer interface {
	// Issue mints a compact RS256-signed JWT for actor.
	Issue(actor Actor) (IssuedToken, error)
}

// Verifier returns the trusted actor behind a token.
type Verifier interface {
	// Verify checks the RS256 signature against the configured key set plus
	// the issuer, audience, and expiry, and wraps a sentinel error from this
	// package on failure.
	Verify(ctx context.Context, token string) (Actor, error)
}

// KeySetProvider supplies the public keys used to verify token signatures,
// keyed by "kid" (key ID), so a verifier can run against a fixed key set in
// tests and a refreshing one in production. Implementations must be safe for
// concurrent use.
type KeySetProvider interface {
	// KeySet returns the public keys trusted for verification.
	KeySet(ctx context.Context) (KeySet, error)
}

// KeySet maps a token's "kid" header to the public key that should verify it.
type KeySet map[string]*rsa.PublicKey

// contextKey is unexported so the Actor context key cannot collide with keys
// set by other packages.
type contextKey struct{}

var actorContextKey = contextKey{}

// WithActor returns a copy of ctx carrying actor.
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorContextKey, actor)
}

// ActorFromContext extracts the Actor injected by WithActor. The boolean is
// false when no actor is present.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorContextKey).(Actor)
	return actor, ok
}
