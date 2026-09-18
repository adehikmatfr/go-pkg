package authmiddleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/security/authmiddleware"
	"github.com/adehikmatfr/go-pkg/v2/security/jwt"
)

func newProtectedServer(t *testing.T, signer jwt.Signer) *httptest.Server {
	t.Helper()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := authmiddleware.ClaimsFromContext(r.Context())
		if !ok {
			t.Fatal("handler called without claims in context")
		}
		w.Header().Set("X-Subject", claims.Subject)
		w.WriteHeader(http.StatusOK)
	})

	srv := httptest.NewServer(authmiddleware.HTTP(signer)(handler))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPValidToken(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	token, err := signer.Generate("user-1", "admin", time.Minute)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	srv := newProtectedServer(t, signer)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("X-Subject"); got != "user-1" {
		t.Errorf("X-Subject = %q, want %q", got, "user-1")
	}
}

func TestHTTPMissingHeader(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	srv := newProtectedServer(t, signer)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHTTPInvalidToken(t *testing.T) {
	signer, _ := jwt.NewSigner("secret")
	srv := newProtectedServer(t, signer)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer not-a-real-token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestHTTPWrongSecretToken(t *testing.T) {
	issuer, _ := jwt.NewSigner("issuer-secret")
	token, err := issuer.Generate("user-1", "admin", time.Minute)
	if err != nil {
		t.Fatalf("Generate() error: %v", err)
	}

	verifier, _ := jwt.NewSigner("verifier-secret")
	srv := newProtectedServer(t, verifier)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("StatusCode = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}
