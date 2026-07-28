// Package jwt provides thin, opinionated helpers around golang-jwt for
// issuing and validating HS256 bearer tokens carrying a subject and role.
package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned for any parse/validation failure (expired,
// malformed, wrong signing method, bad signature). Callers should treat it as
// "unauthenticated" without inspecting further — the underlying reason isn't
// exposed to avoid leaking validation internals to clients.
var ErrInvalidToken = errors.New("jwt: invalid or expired token")

// Claims is the reference claim set — Subject typically holds a user/subject
// identifier, Role backs simple role-based authorization checks. Extend with
// further app-specific fields (tenant id, ...) as needed.
type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}

// Signer issues and validates tokens for a single, fixed secret. Depend on
// this interface (rather than the concrete type returned by NewSigner) in
// code that needs to be unit-testable with a fake/mock implementation.
type Signer interface {
	// Generate signs a token for subject with the given role, valid for ttl.
	// ttl must be positive.
	Generate(subject, role string, ttl time.Duration) (string, error)
	// Parse validates tokenString and returns its claims, or ErrInvalidToken.
	Parse(tokenString string) (*Claims, error)
}

type hmacSigner struct {
	secret []byte
}

// NewSigner returns a Signer using HMAC-SHA256 with secret. secret must be
// non-empty and kept confidential.
func NewSigner(secret string) (Signer, error) {
	if secret == "" {
		return nil, errors.New("jwt: secret must not be empty")
	}
	return &hmacSigner{secret: []byte(secret)}, nil
}

func (s *hmacSigner) Generate(subject, role string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("jwt: ttl must be positive")
	}

	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
		Role: role,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *hmacSigner) Parse(tokenString string) (*Claims, error) {
	claims := &Claims{}

	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		// Reject anything other than HMAC to prevent "alg confusion" attacks,
		// where a token forged with a different algorithm (e.g. "none", or an
		// asymmetric public key mistaken for an HMAC secret) would otherwise
		// be accepted.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
}

// Generate is a convenience one-shot wrapper around NewSigner(secret).Generate
// for callers that don't need to reuse a Signer. Prefer NewSigner directly
// when issuing multiple tokens or when the caller needs to be mockable.
func Generate(secret, subject, role string, ttl time.Duration) (string, error) {
	signer, err := NewSigner(secret)
	if err != nil {
		return "", err
	}
	return signer.Generate(subject, role, ttl)
}

// Parse is a convenience one-shot wrapper around NewSigner(secret).Parse.
func Parse(secret, tokenString string) (*Claims, error) {
	signer, err := NewSigner(secret)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return signer.Parse(tokenString)
}
