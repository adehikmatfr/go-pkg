// Package otp provides multi-factor primitives: RFC 6238 TOTP and RFC 4226
// HOTP provisioning/validation over github.com/pquerna/otp, plus a
// stdlib-only numeric verification-code generator and a single-use, TTL'd
// challenge store.
//
// Vendor types stay internal to this package; consumers depend only on the
// exported ports and domain structs. Secret and code generation use
// crypto/rand exclusively, verification uses crypto/subtle, and
// time-dependent behavior (TOTP skew, challenge TTL) runs off an injectable
// Clock so it is deterministic under test.
package otp

import (
	"errors"
	"time"
)

// Errors returned by the TOTP/HOTP ports. Match with errors.Is.
var (
	ErrEmptySecret        = errors.New("otp: empty secret")
	ErrEmptyCode          = errors.New("otp: empty code")
	ErrInvalidSecret      = errors.New("otp: invalid base32 secret")
	ErrMissingIssuer      = errors.New("otp: missing issuer")
	ErrMissingAccountName = errors.New("otp: missing account name")
)

// Clock supplies the current time, so TOTP validation and challenge TTLs are
// deterministic under test. A nil Clock passed to a constructor falls back to
// a real clock backed by time.Now.
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Algorithm is the HMAC hash function backing a TOTP/HOTP secret.
type Algorithm int

const (
	// AlgorithmSHA1 is the default and the only algorithm compatible with
	// Google Authenticator and most authenticator apps.
	AlgorithmSHA1 Algorithm = iota
	// AlgorithmSHA256 uses HMAC-SHA-256.
	AlgorithmSHA256
	// AlgorithmSHA512 uses HMAC-SHA-512.
	AlgorithmSHA512
)

// Digits is the number of decimal digits in a generated one-time passcode.
type Digits int

const (
	// DigitsSix is the default 6-digit passcode length.
	DigitsSix Digits = 6
	// DigitsEight is the 8-digit passcode length used by the RFC 6238 test
	// vectors.
	DigitsEight Digits = 8
)

// Valid reports whether the digit count is one of the supported lengths.
func (d Digits) Valid() bool {
	return d == DigitsSix || d == DigitsEight
}

// Secret is the result of provisioning a new TOTP/HOTP credential.
type Secret struct {
	// Base32Secret is the shared secret, base32-encoded without padding.
	// Store this (encrypted at rest); never log it.
	Base32Secret string
	// URI is the otpauth:// provisioning URI for QR-code rendering.
	URI string
	// Issuer is the provisioning issuer label.
	Issuer string
	// AccountName is the user-facing account label (e.g. an email address).
	AccountName string
}
