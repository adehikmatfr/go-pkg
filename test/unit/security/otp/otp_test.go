package otp_test

import (
	"context"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/security/otp"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestHOTPProvisionAndValidate(t *testing.T) {
	provisioner := otp.NewHOTPProvisioner(otp.HOTPParams{})
	secret, err := provisioner.Provision(context.Background(), "issuer", "user@example.com")
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}
	if secret.Base32Secret == "" || secret.URI == "" {
		t.Fatal("Provision() returned an empty secret or URI")
	}

	validator := otp.NewHOTPValidator(otp.HOTPParams{})
	code, err := validator.CodeAt(context.Background(), secret.Base32Secret, 1)
	if err != nil {
		t.Fatalf("CodeAt() error: %v", err)
	}

	ok, err := validator.Validate(context.Background(), secret.Base32Secret, 1, code)
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if !ok {
		t.Error("Validate() = false, want true for a freshly generated code")
	}

	ok, err = validator.Validate(context.Background(), secret.Base32Secret, 2, code)
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if ok {
		t.Error("Validate() = true, want false for a mismatched counter")
	}
}

func TestHOTPValidateEmptyInput(t *testing.T) {
	v := otp.NewHOTPValidator(otp.HOTPParams{})
	if _, err := v.Validate(context.Background(), "", 1, "123456"); err == nil {
		t.Error("Validate() with empty secret should return an error")
	}
	if _, err := v.Validate(context.Background(), "secret", 1, ""); err == nil {
		t.Error("Validate() with empty code should return an error")
	}
	if _, err := v.CodeAt(context.Background(), "", 1); err == nil {
		t.Error("CodeAt() with empty secret should return an error")
	}
}

func TestHOTPCodeAtInvalidSecret(t *testing.T) {
	v := otp.NewHOTPValidator(otp.HOTPParams{})
	if _, err := v.CodeAt(context.Background(), "not-valid-base32!!!", 1); err == nil {
		t.Error("CodeAt() with an invalid base32 secret should return an error")
	}
}

func TestHOTPValidateInvalidSecret(t *testing.T) {
	v := otp.NewHOTPValidator(otp.HOTPParams{})
	if _, err := v.Validate(context.Background(), "not-valid-base32!!!", 1, "123456"); err == nil {
		t.Error("Validate() with an invalid base32 secret should return an error")
	}
}

func TestHOTPEightDigitsAndAlgorithms(t *testing.T) {
	for _, alg := range []otp.Algorithm{otp.AlgorithmSHA1, otp.AlgorithmSHA256, otp.AlgorithmSHA512} {
		params := otp.HOTPParams{Digits: otp.DigitsEight, Algorithm: alg}
		provisioner := otp.NewHOTPProvisioner(params)
		secret, err := provisioner.Provision(context.Background(), "issuer", "user@example.com")
		if err != nil {
			t.Fatalf("Provision() error: %v", err)
		}

		validator := otp.NewHOTPValidator(params)
		code, err := validator.CodeAt(context.Background(), secret.Base32Secret, 1)
		if err != nil {
			t.Fatalf("CodeAt() error: %v", err)
		}
		if len(code) != 8 {
			t.Errorf("CodeAt() len = %d, want 8", len(code))
		}
		ok, err := validator.Validate(context.Background(), secret.Base32Secret, 1, code)
		if err != nil || !ok {
			t.Errorf("Validate() = (%v, %v), want (true, nil)", ok, err)
		}
	}
}

func TestHOTPProvisionRequiresIssuerAndAccount(t *testing.T) {
	p := otp.NewHOTPProvisioner(otp.HOTPParams{})
	if _, err := p.Provision(context.Background(), "", "user"); err == nil {
		t.Error("Provision() with empty issuer should return an error")
	}
	if _, err := p.Provision(context.Background(), "issuer", ""); err == nil {
		t.Error("Provision() with empty account name should return an error")
	}
}

func TestTOTPProvisionAndValidate(t *testing.T) {
	provisioner := otp.NewTOTPProvisioner(otp.TOTPParams{})
	secret, err := provisioner.Provision(context.Background(), "issuer", "user@example.com")
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	now := time.Now()
	validator := otp.NewTOTPValidator(fixedClock{t: now}, otp.TOTPParams{})
	code, err := validator.CurrentCode(context.Background(), secret.Base32Secret)
	if err != nil {
		t.Fatalf("CurrentCode() error: %v", err)
	}

	ok, err := validator.Validate(context.Background(), secret.Base32Secret, code)
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if !ok {
		t.Error("Validate() = false, want true for the current code")
	}

	future := fixedClock{t: now.Add(10 * time.Minute)}
	validatorFuture := otp.NewTOTPValidator(future, otp.TOTPParams{})
	ok, err = validatorFuture.Validate(context.Background(), secret.Base32Secret, code)
	if err != nil {
		t.Fatalf("Validate() error: %v", err)
	}
	if ok {
		t.Error("Validate() = true, want false for a code far outside the period+skew window")
	}
}

func TestTOTPValidatorNilClockUsesSystemTime(t *testing.T) {
	provisioner := otp.NewTOTPProvisioner(otp.TOTPParams{})
	secret, err := provisioner.Provision(context.Background(), "issuer", "user@example.com")
	if err != nil {
		t.Fatalf("Provision() error: %v", err)
	}

	validator := otp.NewTOTPValidator(nil, otp.TOTPParams{})
	code, err := validator.CurrentCode(context.Background(), secret.Base32Secret)
	if err != nil {
		t.Fatalf("CurrentCode() error: %v", err)
	}
	ok, err := validator.Validate(context.Background(), secret.Base32Secret, code)
	if err != nil || !ok {
		t.Errorf("Validate() = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestTOTPValidateEmptyInput(t *testing.T) {
	validator := otp.NewTOTPValidator(nil, otp.TOTPParams{})
	if _, err := validator.Validate(context.Background(), "", "123456"); err == nil {
		t.Error("Validate() with empty secret should return an error")
	}
	if _, err := validator.Validate(context.Background(), "secret", ""); err == nil {
		t.Error("Validate() with empty code should return an error")
	}
}

func TestCodeGeneratorGenerateHashVerify(t *testing.T) {
	gen := otp.NewCodeGenerator(6)

	code, err := gen.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if len(code) != 6 {
		t.Errorf("Generate() len = %d, want 6", len(code))
	}

	hash := gen.Hash(code)
	if !gen.Verify(hash, code) {
		t.Error("Verify() = false, want true for a matching code/hash pair")
	}
	if gen.Verify(hash, "000000") {
		t.Error("Verify() = true, want false for a mismatched code")
	}
}

func TestCodeGeneratorWithPepperDiffersFromBare(t *testing.T) {
	code := "123456"
	bare := otp.NewCodeGenerator(6)
	peppered := otp.NewCodeGeneratorWithPepper(6, []byte("server-secret"))

	if bare.Hash(code) == peppered.Hash(code) {
		t.Error("peppered hash should differ from the bare SHA-256 hash")
	}
}

func TestCodeGeneratorInvalidDigits(t *testing.T) {
	gen := otp.NewCodeGenerator(0)
	if _, err := gen.Generate(context.Background()); err == nil {
		t.Error("Generate() with 0 digits should return ErrInvalidDigits")
	}
}

func TestChallengeStorePutGetConsume(t *testing.T) {
	now := time.Now()
	store := otp.NewMemoryChallengeStore(fixedClock{t: now})
	ctx := context.Background()

	ch := otp.Challenge{ID: "chal-1", CodeHash: "hash", Purpose: "login"}
	if err := store.Put(ctx, ch, time.Minute); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	got, err := store.Get(ctx, "chal-1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.CodeHash != "hash" {
		t.Errorf("Get().CodeHash = %q, want %q", got.CodeHash, "hash")
	}

	if err := store.Consume(ctx, "chal-1"); err != nil {
		t.Fatalf("Consume() error: %v", err)
	}
	if _, err := store.Get(ctx, "chal-1"); err == nil {
		t.Error("Get() after Consume() should return an error (single-use)")
	}
}

func TestChallengeStoreExpiry(t *testing.T) {
	now := time.Now()
	clock := &mutableClock{t: now}
	store := otp.NewMemoryChallengeStore(clock)
	ctx := context.Background()

	if err := store.Put(ctx, otp.Challenge{ID: "chal-1"}, time.Second); err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	clock.t = now.Add(2 * time.Second)
	if _, err := store.Get(ctx, "chal-1"); err == nil {
		t.Error("Get() past TTL should return ErrChallengeExpired")
	}
}

func TestChallengeStoreNilClockUsesSystemTime(t *testing.T) {
	store := otp.NewMemoryChallengeStore(nil)
	ctx := context.Background()

	if err := store.Put(ctx, otp.Challenge{ID: "chal-1"}, time.Minute); err != nil {
		t.Fatalf("Put() error: %v", err)
	}
	if _, err := store.Get(ctx, "chal-1"); err != nil {
		t.Fatalf("Get() error: %v", err)
	}
}

func TestChallengeStoreInvalidInput(t *testing.T) {
	store := otp.NewMemoryChallengeStore(nil)
	ctx := context.Background()

	if err := store.Put(ctx, otp.Challenge{}, time.Minute); err == nil {
		t.Error("Put() with empty ID should return an error")
	}
	if err := store.Put(ctx, otp.Challenge{ID: "x"}, 0); err == nil {
		t.Error("Put() with non-positive TTL should return an error")
	}
	if _, err := store.Get(ctx, "missing"); err == nil {
		t.Error("Get() for an unknown id should return an error")
	}
	if err := store.Consume(ctx, "missing"); err == nil {
		t.Error("Consume() for an unknown id should return an error")
	}
}

type mutableClock struct{ t time.Time }

func (c *mutableClock) Now() time.Time { return c.t }
