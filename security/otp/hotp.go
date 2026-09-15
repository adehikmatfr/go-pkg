package otp

import (
	"context"
	"errors"
	"fmt"

	pkgotp "github.com/pquerna/otp"
	"github.com/pquerna/otp/hotp"
)

// HOTPParams configures HOTP provisioning and validation. The zero value
// yields 6 digits and SHA-1.
type HOTPParams struct {
	Digits    Digits
	Algorithm Algorithm
}

func (p HOTPParams) digits() Digits {
	if !p.Digits.Valid() {
		return DigitsSix
	}
	return p.Digits
}

// HOTPProvisioner issues new counter-based (RFC 4226) HOTP secrets and their
// otpauth:// provisioning URIs.
type HOTPProvisioner interface {
	// Provision creates a new random HOTP secret for issuer and accountName.
	Provision(ctx context.Context, issuer, accountName string) (Secret, error)
}

// HOTPValidator validates counter-based one-time passcodes. Unlike TOTP, the
// caller supplies the moving counter; there is no clock dependency.
type HOTPValidator interface {
	// Validate reports whether code matches the passcode for secret at
	// counter.
	Validate(ctx context.Context, secret string, counter uint64, code string) (bool, error)
	// CodeAt returns the passcode for secret at counter.
	CodeAt(ctx context.Context, secret string, counter uint64) (string, error)
}

type hotpProvisioner struct {
	params HOTPParams
}

// NewHOTPProvisioner returns an HOTPProvisioner configured with params.
func NewHOTPProvisioner(params HOTPParams) HOTPProvisioner {
	return &hotpProvisioner{params: params}
}

func (p *hotpProvisioner) Provision(_ context.Context, issuer, accountName string) (Secret, error) {
	if issuer == "" {
		return Secret{}, ErrMissingIssuer
	}
	if accountName == "" {
		return Secret{}, ErrMissingAccountName
	}

	key, err := hotp.Generate(hotp.GenerateOpts{
		Issuer:      issuer,
		AccountName: accountName,
		Digits:      toVendorDigits(p.params.digits()),
		Algorithm:   toVendorAlgorithm(p.params.Algorithm),
	})
	if err != nil {
		return Secret{}, fmt.Errorf("otp: provision hotp: %w", err)
	}

	return Secret{
		Base32Secret: key.Secret(),
		URI:          key.URL(),
		Issuer:       issuer,
		AccountName:  accountName,
	}, nil
}

type hotpValidator struct {
	params HOTPParams
}

// NewHOTPValidator returns an HOTPValidator configured with params.
func NewHOTPValidator(params HOTPParams) HOTPValidator {
	return &hotpValidator{params: params}
}

func (v *hotpValidator) Validate(_ context.Context, secret string, counter uint64, code string) (bool, error) {
	if secret == "" {
		return false, ErrEmptySecret
	}
	if code == "" {
		return false, ErrEmptyCode
	}

	ok, err := hotp.ValidateCustom(code, counter, secret, hotp.ValidateOpts{
		Digits:    toVendorDigits(v.params.digits()),
		Algorithm: toVendorAlgorithm(v.params.Algorithm),
	})
	if err != nil {
		return false, fmt.Errorf("otp: validate hotp: %w", mapVendorErr(err))
	}
	return ok, nil
}

func (v *hotpValidator) CodeAt(_ context.Context, secret string, counter uint64) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	code, err := hotp.GenerateCodeCustom(secret, counter, hotp.ValidateOpts{
		Digits:    toVendorDigits(v.params.digits()),
		Algorithm: toVendorAlgorithm(v.params.Algorithm),
	})
	if err != nil {
		return "", fmt.Errorf("otp: %w", ErrInvalidSecret)
	}
	return code, nil
}

// mapVendorErr translates pquerna validation errors into stable package
// sentinels so vendor errors never reach callers.
func mapVendorErr(err error) error {
	switch {
	case errors.Is(err, pkgotp.ErrValidateSecretInvalidBase32):
		return ErrInvalidSecret
	case errors.Is(err, pkgotp.ErrValidateInputInvalidLength):
		return ErrEmptyCode
	default:
		return ErrInvalidSecret
	}
}

// toVendorAlgorithm maps this package's Algorithm to the vendor type.
func toVendorAlgorithm(a Algorithm) pkgotp.Algorithm {
	switch a {
	case AlgorithmSHA256:
		return pkgotp.AlgorithmSHA256
	case AlgorithmSHA512:
		return pkgotp.AlgorithmSHA512
	default:
		return pkgotp.AlgorithmSHA1
	}
}

// toVendorDigits maps this package's Digits to the vendor type.
func toVendorDigits(d Digits) pkgotp.Digits {
	if d == DigitsEight {
		return pkgotp.DigitsEight
	}
	return pkgotp.DigitsSix
}
