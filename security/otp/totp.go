package otp

import (
	"context"
	"errors"
	"fmt"

	pkgotp "github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

// TOTPParams configures TOTP provisioning and validation. The zero value is
// valid and yields RFC/Google-Authenticator defaults: 30s period, 6 digits,
// SHA-1, no skew.
type TOTPParams struct {
	// Period is the time step in seconds. Zero means 30.
	Period uint
	// Skew is the number of periods before/after now to accept on
	// validation. A value of 1 tolerates one step of clock drift on each
	// side; values above 1 are discouraged.
	Skew uint
	// Digits is the passcode length. Zero means six.
	Digits Digits
	// Algorithm is the HMAC hash. Zero value means SHA-1.
	Algorithm Algorithm
}

func (p TOTPParams) period() uint {
	if p.Period == 0 {
		return 30
	}
	return p.Period
}

func (p TOTPParams) digits() Digits {
	if !p.Digits.Valid() {
		return DigitsSix
	}
	return p.Digits
}

// TOTPProvisioner issues new TOTP secrets and their otpauth:// provisioning
// URIs.
type TOTPProvisioner interface {
	// Provision creates a new random TOTP secret for issuer and accountName.
	Provision(ctx context.Context, issuer, accountName string) (Secret, error)
}

// TOTPValidator validates time-based one-time passcodes against a stored
// secret, honoring the configured skew. It reads the current time from an
// injected Clock so behavior is deterministic under test.
type TOTPValidator interface {
	// Validate reports whether code is a valid passcode for secret at the
	// clock's current time, within the configured skew. It returns an error
	// only for malformed input, never for a merely incorrect code.
	Validate(ctx context.Context, secret, code string) (bool, error)
	// CurrentCode returns the passcode for secret at the clock's current
	// time.
	CurrentCode(ctx context.Context, secret string) (string, error)
}

type totpProvisioner struct {
	params TOTPParams
}

// NewTOTPProvisioner returns a TOTPProvisioner configured with params.
func NewTOTPProvisioner(params TOTPParams) TOTPProvisioner {
	return &totpProvisioner{params: params}
}

func (p *totpProvisioner) Provision(_ context.Context, issuer, accountName string) (Secret, error) {
	if issuer == "" {
		return Secret{}, ErrMissingIssuer
	}
	if accountName == "" {
		return Secret{}, ErrMissingAccountName
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Period:      p.params.period(),
		Digits:      toVendorDigits(p.params.digits()),
		Algorithm:   toVendorAlgorithm(p.params.Algorithm),
	})
	if err != nil {
		return Secret{}, fmt.Errorf("otp: provision totp: %w", err)
	}

	return Secret{
		Base32Secret: key.Secret(),
		URI:          key.URL(),
		Issuer:       issuer,
		AccountName:  accountName,
	}, nil
}

type totpValidator struct {
	clock  Clock
	params TOTPParams
}

// NewTOTPValidator returns a TOTPValidator that reads the current time from
// clock. A nil clock uses time.Now.
func NewTOTPValidator(clock Clock, params TOTPParams) TOTPValidator {
	if clock == nil {
		clock = systemClock{}
	}
	return &totpValidator{clock: clock, params: params}
}

func (v *totpValidator) Validate(_ context.Context, secret, code string) (bool, error) {
	if secret == "" {
		return false, ErrEmptySecret
	}
	if code == "" {
		return false, ErrEmptyCode
	}

	ok, err := totp.ValidateCustom(code, secret, v.clock.Now(), totp.ValidateOpts{
		Period:    v.params.period(),
		Skew:      v.params.Skew,
		Digits:    toVendorDigits(v.params.digits()),
		Algorithm: toVendorAlgorithm(v.params.Algorithm),
	})
	if err != nil {
		// An input-length mismatch is a non-match, not a fault; only
		// secret-decoding problems surface as errors.
		if errors.Is(err, pkgotp.ErrValidateInputInvalidLength) {
			return false, nil
		}
		return false, fmt.Errorf("otp: %w", ErrInvalidSecret)
	}
	return ok, nil
}

func (v *totpValidator) CurrentCode(_ context.Context, secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	code, err := totp.GenerateCodeCustom(secret, v.clock.Now(), totp.ValidateOpts{
		Period:    v.params.period(),
		Digits:    toVendorDigits(v.params.digits()),
		Algorithm: toVendorAlgorithm(v.params.Algorithm),
	})
	if err != nil {
		return "", fmt.Errorf("otp: %w", ErrInvalidSecret)
	}
	return code, nil
}
