package rsajwt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// VerifierConfig configures a token verifier.
type VerifierConfig struct {
	// Issuer is the expected "iss" claim. Required.
	Issuer string
	// Audience is the expected "aud" value; a token is accepted when its
	// audience list contains it. Required.
	Audience string
	// Keys supplies the verification key set. Required.
	Keys KeySetProvider
	// AcceptableSkew tolerates small clock differences between issuer and
	// verifier when checking time-based claims. Defaults to zero.
	AcceptableSkew time.Duration
}

type rsaVerifier struct {
	issuer   string
	audience string
	keys     KeySetProvider
	skew     time.Duration
}

// NewVerifier constructs a Verifier that validates RS256 tokens against the
// key set supplied by cfg.Keys, enforcing issuer, audience, and expiry.
func NewVerifier(cfg VerifierConfig) (Verifier, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("rsajwt: issuer is required: %w", ErrInvalidToken)
	}
	if cfg.Audience == "" {
		return nil, fmt.Errorf("rsajwt: audience is required: %w", ErrInvalidToken)
	}
	if cfg.Keys == nil {
		return nil, fmt.Errorf("rsajwt: key set provider is required: %w", ErrInvalidToken)
	}
	return &rsaVerifier{
		issuer:   cfg.Issuer,
		audience: cfg.Audience,
		keys:     cfg.Keys,
		skew:     cfg.AcceptableSkew,
	}, nil
}

// Verify validates tokenString and returns the trusted Actor. See
// Verifier.Verify.
func (v *rsaVerifier) Verify(ctx context.Context, tokenString string) (Actor, error) {
	if tokenString == "" {
		return Actor{}, fmt.Errorf("rsajwt: empty token: %w", ErrInvalidToken)
	}

	keys, err := v.keys.KeySet(ctx)
	if err != nil {
		return Actor{}, fmt.Errorf("rsajwt: load key set: %w", err)
	}

	claims := &claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		// Reject anything other than RSA to prevent "alg confusion" attacks.
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, ErrInvalidToken
		}
		key, err := selectKey(keys, t.Header["kid"])
		if err != nil {
			return nil, err
		}
		return key, nil
	},
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(v.audience),
		jwt.WithLeeway(v.skew),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Actor{}, fmt.Errorf("rsajwt: %w", ErrTokenExpired)
		}
		return Actor{}, fmt.Errorf("rsajwt: verify token: %w: %w", ErrInvalidToken, err)
	}
	if !token.Valid {
		return Actor{}, ErrInvalidToken
	}
	if claims.Subject == "" {
		return Actor{}, fmt.Errorf("rsajwt: token missing subject: %w", ErrInvalidToken)
	}

	return actorFromClaims(claims), nil
}

// selectKey picks the verification key for kid. When the token carries no kid
// and exactly one key is configured, that key is used — the common
// single-key, no-rotation case.
func selectKey(keys KeySet, kid interface{}) (interface{}, error) {
	id, _ := kid.(string)
	if id != "" {
		if key, ok := keys[id]; ok {
			return key, nil
		}
		return nil, fmt.Errorf("rsajwt: no key for kid %q: %w", id, ErrNoSigningKey)
	}
	if len(keys) == 1 {
		for _, key := range keys {
			return key, nil
		}
	}
	return nil, fmt.Errorf("rsajwt: token carries no kid and key set is ambiguous: %w", ErrNoSigningKey)
}

// actorFromClaims builds the trusted Actor from a verified token's claims.
func actorFromClaims(c *claims) Actor {
	aud, _ := c.GetAudience()
	scopes := []string{}
	if c.Scope != "" {
		scopes = strings.Fields(c.Scope)
	}
	roles := c.Roles
	if roles == nil {
		roles = []string{}
	}
	return Actor{
		ID:       c.Subject,
		Roles:    roles,
		Scopes:   scopes,
		Issuer:   c.Issuer,
		Audience: aud,
		TokenID:  c.ID,
	}
}
