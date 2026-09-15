package otp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
)

// ErrInvalidDigits is returned by Generate when the generator was constructed
// with an unsupported code length.
var ErrInvalidDigits = errors.New("otp: invalid code length")

// maxCodeDigits bounds numeric code length at the range where 10^n stays
// comfortably representable.
const maxCodeDigits = 18

// CodeGenerator produces uniformly-random N-digit numeric verification/reset
// codes (e.g. SMS/email OTPs) and provides SHA-256 hashing with
// constant-time verification so only the hash need be persisted.
//
// Generation uses crypto/rand exclusively (never math/rand) and is unbiased:
// it draws a uniform integer in [0, 10^N), so every code — including those
// with leading zeros — is equally likely.
type CodeGenerator interface {
	// Generate returns a fresh random numeric code of the configured length,
	// zero-padded to preserve leading zeros.
	Generate(ctx context.Context) (string, error)
	// Hash returns the hex-encoded digest of code to persist instead of the
	// plaintext. When the generator was constructed with a server pepper
	// (NewCodeGeneratorWithPepper) this is HMAC-SHA256(pepper, code);
	// otherwise it is a bare SHA-256(code).
	Hash(code string) string
	// Verify reports whether code matches the previously stored hash, using
	// a constant-time comparison.
	Verify(hash, code string) bool
}

type codeGenerator struct {
	digits int
	// bound is 10^digits, the exclusive upper bound for uniform sampling.
	bound *big.Int
	// pepper, when non-empty, keys an HMAC-SHA256 over the code so a leaked
	// hash is not brute-forceable from the 10^digits preimage space.
	pepper []byte
}

// NewCodeGenerator returns a CodeGenerator that emits codes of the given
// digit length. Supported lengths are 1..18; an out-of-range length does not
// panic, and the returned generator's Generate reports ErrInvalidDigits.
//
// It hashes with a bare SHA-256, which is weak for a short numeric OTP: a
// 6-digit code has only 10^6 preimages and is reversible from a leaked hash.
// Prefer NewCodeGeneratorWithPepper with an injected server secret.
func NewCodeGenerator(digits int) CodeGenerator {
	return NewCodeGeneratorWithPepper(digits, nil)
}

// NewCodeGeneratorWithPepper returns a CodeGenerator that hashes codes with
// HMAC-SHA256 keyed by pepper, a server-held secret injected from a secret
// manager and never hardcoded. Without it, offline brute-force over the
// 10^digits preimage space is trivial.
//
// Rotating the pepper invalidates in-flight codes, which is acceptable
// because OTPs are short-TTL and the user requests a new one. A nil or empty
// pepper is equivalent to NewCodeGenerator.
func NewCodeGeneratorWithPepper(digits int, pepper []byte) CodeGenerator {
	g := &codeGenerator{digits: digits}
	if len(pepper) > 0 {
		g.pepper = append([]byte(nil), pepper...) // defensive copy
	}
	if digits >= 1 && digits <= maxCodeDigits {
		g.bound = new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)
	}
	return g
}

func (g *codeGenerator) Generate(_ context.Context) (string, error) {
	if g.bound == nil {
		return "", fmt.Errorf("%w: %d (supported 1..%d)", ErrInvalidDigits, g.digits, maxCodeDigits)
	}
	// rand.Int performs uniform rejection sampling internally over [0,
	// bound), backed by crypto/rand — no modulo bias, no math/rand.
	n, err := rand.Int(rand.Reader, g.bound)
	if err != nil {
		return "", fmt.Errorf("otp: generate code: %w", err)
	}
	return fmt.Sprintf("%0*d", g.digits, n), nil
}

func (g *codeGenerator) Hash(code string) string {
	if len(g.pepper) > 0 {
		mac := hmac.New(sha256.New, g.pepper)
		_, _ = mac.Write([]byte(code)) // hash.Hash.Write never returns an error
		return hex.EncodeToString(mac.Sum(nil))
	}
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func (g *codeGenerator) Verify(hash, code string) bool {
	if hash == "" || code == "" {
		return false
	}
	want := g.Hash(code)
	// ConstantTimeCompare returns 0 on length mismatch; both operands are
	// fixed-length hex digests in the matching case.
	return subtle.ConstantTimeCompare([]byte(want), []byte(hash)) == 1
}
