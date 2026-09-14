package jwt_test

import (
	"errors"
	"testing"
	"time"

	golangjwt "github.com/golang-jwt/jwt/v5"

	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

func TestNewSignerEmptySecret(t *testing.T) {
	if _, err := jwt.NewSigner(""); err == nil {
		t.Fatal("jwt.NewSigner(\"\") should return an error")
	}
}

func TestSignerGenerateAndParse(t *testing.T) {
	signer, err := jwt.NewSigner("super-secret")
	if err != nil {
		t.Fatalf("jwt.NewSigner() error: %v", err)
	}

	token, err := signer.Generate("user-1", "admin", time.Minute)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	if token == "" {
		t.Fatal("Generate() returned empty token")
	}

	claims, err := signer.Parse(token)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
}

func TestSignerGenerateInvalidTTL(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	if _, err := signer.Generate("user-1", "admin", 0); err == nil {
		t.Error("Generate() with zero ttl should error")
	}
	if _, err := signer.Generate("user-1", "admin", -time.Minute); err == nil {
		t.Error("Generate() with negative ttl should error")
	}
}

func TestSignerParseExpiredToken(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	token, err := signer.Generate("user-1", "admin", time.Nanosecond)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	if _, err := signer.Parse(token); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("Parse() expired token error = %v, want %v", err, jwt.ErrInvalidToken)
	}
}

func TestSignerParseWrongSecret(t *testing.T) {
	signer, _ := jwt.NewSigner("secret-a")
	token, _ := signer.Generate("user-1", "admin", time.Minute)

	other, _ := jwt.NewSigner("secret-b")
	if _, err := other.Parse(token); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("Parse() with wrong secret error = %v, want %v", err, jwt.ErrInvalidToken)
	}
}

func TestSignerParseMalformedToken(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	if _, err := signer.Parse("not-a-jwt"); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("Parse() malformed token error = %v, want %v", err, jwt.ErrInvalidToken)
	}
}

// TestSignerRejectsNonHMACAlg guards against "alg confusion": a token signed
// with "none" must never be accepted regardless of the configured secret.
func TestSignerRejectsNonHMACAlg(t *testing.T) {
	claims := jwt.Claims{
		RegisteredClaims: golangjwt.RegisteredClaims{
			Subject:   "user-1",
			ExpiresAt: golangjwt.NewNumericDate(time.Now().Add(time.Minute)),
		},
		Role: "admin",
	}
	token := golangjwt.NewWithClaims(golangjwt.SigningMethodNone, claims)
	tokenString, err := token.SignedString(golangjwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("failed to construct none-alg token: %v", err)
	}

	signer, _ := jwt.NewSigner("secret")
	if _, err := signer.Parse(tokenString); !errors.Is(err, jwt.ErrInvalidToken) {
		t.Errorf("Parse() of none-alg token error = %v, want %v", err, jwt.ErrInvalidToken)
	}
}

func TestPackageLevelGenerateAndParse(t *testing.T) {
	token, err := jwt.Generate("secret", "user-1", "admin", time.Minute)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	claims, err := jwt.Parse("secret", token)
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
	}
}

func TestPackageLevelGenerateEmptySecret(t *testing.T) {
	if _, err := jwt.Generate("", "user-1", "admin", time.Minute); err == nil {
		t.Error("Generate() with empty secret should error")
	}
}

// signerMock is a minimal fake demonstrating Signer is consumable as an
// interface by code outside this package (e.g. an HTTP middleware under test).
type signerMock struct {
	generateFn func(subject, role string, ttl time.Duration) (string, error)
	parseFn    func(tokenString string) (*jwt.Claims, error)
}

func (m *signerMock) Generate(subject, role string, ttl time.Duration) (string, error) {
	return m.generateFn(subject, role, ttl)
}

func (m *signerMock) Parse(tokenString string) (*jwt.Claims, error) {
	return m.parseFn(tokenString)
}

func TestSignerInterfaceIsMockable(t *testing.T) {
	var s jwt.Signer = &signerMock{
		generateFn: func(subject, role string, ttl time.Duration) (string, error) {
			return "fake-token", nil
		},
		parseFn: func(tokenString string) (*jwt.Claims, error) {
			return &jwt.Claims{Role: "mocked"}, nil
		},
	}

	token, err := s.Generate("u", "r", time.Minute)
	if err != nil || token != "fake-token" {
		t.Fatalf("mock Generate() = %q, %v", token, err)
	}

	claims, err := s.Parse(token)
	if err != nil || claims.Role != "mocked" {
		t.Fatalf("mock Parse() = %+v, %v", claims, err)
	}
}
