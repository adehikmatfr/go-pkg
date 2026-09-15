package rsajwt

import (
	"crypto/rsa"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// DefaultTokenTTL is the lifetime applied to minted tokens when no explicit
// TTL is configured.
const DefaultTokenTTL = 15 * time.Minute

// claims is the token's claim set: RegisteredClaims plus this package's
// private roles/scope claims.
type claims struct {
	jwt.RegisteredClaims
	Roles []string `json:"roles"`
	Scope string   `json:"scope"`
}

// IssuerConfig configures an RS256 token issuer.
type IssuerConfig struct {
	// Issuer is the "iss" claim placed on every minted token. Required.
	Issuer string
	// Audience is the "aud" claim list placed on every minted token. At least
	// one audience is required.
	Audience []string
	// SigningKey is the RSA private key used to sign tokens. Required.
	SigningKey *rsa.PrivateKey
	// KeyID is the "kid" written into the token header so a KeySetProvider can
	// select the matching public key. Strongly recommended for key rotation.
	KeyID string
	// TTL is the token lifetime. If zero, DefaultTokenTTL is used.
	TTL time.Duration
}

type rsaIssuer struct {
	issuer     string
	audience   []string
	signingKey *rsa.PrivateKey
	keyID      string
	ttl        time.Duration
}

// NewIssuer constructs an Issuer that mints RS256 tokens.
func NewIssuer(cfg IssuerConfig) (Issuer, error) {
	if cfg.SigningKey == nil {
		return nil, ErrNoSigningKey
	}
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("rsajwt: issuer is required: %w", ErrInvalidToken)
	}
	if len(cfg.Audience) == 0 {
		return nil, fmt.Errorf("rsajwt: at least one audience is required: %w", ErrInvalidToken)
	}

	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}

	return &rsaIssuer{
		issuer:     cfg.Issuer,
		audience:   append([]string(nil), cfg.Audience...),
		signingKey: cfg.SigningKey,
		keyID:      cfg.KeyID,
		ttl:        ttl,
	}, nil
}

// Issue mints a signed token for actor. See Issuer.Issue.
func (i *rsaIssuer) Issue(actor Actor) (IssuedToken, error) {
	if actor.ID == "" {
		return IssuedToken{}, fmt.Errorf("rsajwt: actor ID is required: %w", ErrInvalidToken)
	}

	now := time.Now()
	exp := now.Add(i.ttl)
	jti := uuid.NewString()

	roles := actor.Roles
	if roles == nil {
		roles = []string{}
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    i.issuer,
			Audience:  i.audience,
			Subject:   actor.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
			ID:        jti,
		},
		Roles: roles,
		Scope: strings.Join(actor.Scopes, " "),
	})
	if i.keyID != "" {
		token.Header["kid"] = i.keyID
	}

	signed, err := token.SignedString(i.signingKey)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("rsajwt: sign token: %w", err)
	}

	return IssuedToken{
		Token:     signed,
		TokenID:   jti,
		ExpiresAt: exp.Unix(),
		IssuedAt:  now.Unix(),
	}, nil
}
