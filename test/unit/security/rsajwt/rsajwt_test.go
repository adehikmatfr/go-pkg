package rsajwt_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/security/rsajwt"
)

func generateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func newIssuerVerifier(t *testing.T, ttl time.Duration) (rsajwt.Issuer, rsajwt.Verifier) {
	t.Helper()
	key := generateKey(t)

	issuer, err := rsajwt.NewIssuer(rsajwt.IssuerConfig{
		Issuer:     "identity",
		Audience:   []string{"internal"},
		SigningKey: key,
		KeyID:      "kid-1",
		TTL:        ttl,
	})
	if err != nil {
		t.Fatalf("NewIssuer() error: %v", err)
	}

	keys, err := rsajwt.NewStaticKeySet(rsajwt.KeySet{"kid-1": &key.PublicKey})
	if err != nil {
		t.Fatalf("NewStaticKeySet() error: %v", err)
	}

	verifier, err := rsajwt.NewVerifier(rsajwt.VerifierConfig{
		Issuer:   "identity",
		Audience: "internal",
		Keys:     keys,
	})
	if err != nil {
		t.Fatalf("NewVerifier() error: %v", err)
	}

	return issuer, verifier
}

func TestIssueAndVerify(t *testing.T) {
	issuer, verifier := newIssuerVerifier(t, time.Hour)

	issued, err := issuer.Issue(rsajwt.Actor{ID: "user-1", Roles: []string{"admin"}, Scopes: []string{"asset.create", "asset.edit"}})
	if err != nil {
		t.Fatalf("Issue() error: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("Issue() returned an empty token")
	}

	actor, err := verifier.Verify(context.Background(), issued.Token)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}
	if actor.ID != "user-1" {
		t.Errorf("actor.ID = %q, want %q", actor.ID, "user-1")
	}
	if !actor.HasRole("admin") {
		t.Errorf("actor.HasRole(%q) = false, want true", "admin")
	}
	if len(actor.Scopes) != 2 || actor.Scopes[0] != "asset.create" || actor.Scopes[1] != "asset.edit" {
		t.Errorf("actor.Scopes = %v, want [asset.create asset.edit]", actor.Scopes)
	}
	if actor.TokenID != issued.TokenID {
		t.Errorf("actor.TokenID = %q, want %q", actor.TokenID, issued.TokenID)
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	issuer, verifier := newIssuerVerifier(t, time.Millisecond)

	issued, err := issuer.Issue(rsajwt.Actor{ID: "user-1"})
	if err != nil {
		t.Fatalf("Issue() error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if _, err := verifier.Verify(context.Background(), issued.Token); err == nil {
		t.Fatal("Verify() with expired token should return an error")
	} else if !errors.Is(err, rsajwt.ErrTokenExpired) {
		t.Errorf("Verify() error = %v, want wrapping ErrTokenExpired", err)
	}
}

func TestVerifyWrongAudience(t *testing.T) {
	key := generateKey(t)

	issuer, err := rsajwt.NewIssuer(rsajwt.IssuerConfig{
		Issuer:     "identity",
		Audience:   []string{"other-service"},
		SigningKey: key,
		KeyID:      "kid-1",
	})
	if err != nil {
		t.Fatalf("NewIssuer() error: %v", err)
	}
	issued, err := issuer.Issue(rsajwt.Actor{ID: "user-1"})
	if err != nil {
		t.Fatalf("Issue() error: %v", err)
	}

	keys, err := rsajwt.NewStaticKeySet(rsajwt.KeySet{"kid-1": &key.PublicKey})
	if err != nil {
		t.Fatalf("NewStaticKeySet() error: %v", err)
	}
	verifier, err := rsajwt.NewVerifier(rsajwt.VerifierConfig{Issuer: "identity", Audience: "internal", Keys: keys})
	if err != nil {
		t.Fatalf("NewVerifier() error: %v", err)
	}

	if _, err := verifier.Verify(context.Background(), issued.Token); err == nil {
		t.Fatal("Verify() with mismatched audience should return an error")
	} else if !errors.Is(err, rsajwt.ErrInvalidToken) {
		t.Errorf("Verify() error = %v, want wrapping ErrInvalidToken", err)
	}
}

func TestVerifyUnknownKid(t *testing.T) {
	_, verifier := newIssuerVerifier(t, time.Hour)

	otherKey := generateKey(t)
	otherIssuer, err := rsajwt.NewIssuer(rsajwt.IssuerConfig{
		Issuer:     "identity",
		Audience:   []string{"internal"},
		SigningKey: otherKey,
		KeyID:      "kid-2",
	})
	if err != nil {
		t.Fatalf("NewIssuer() error: %v", err)
	}
	issued, err := otherIssuer.Issue(rsajwt.Actor{ID: "user-1"})
	if err != nil {
		t.Fatalf("Issue() error: %v", err)
	}

	if _, err := verifier.Verify(context.Background(), issued.Token); err == nil {
		t.Fatal("Verify() with an unknown kid should return an error")
	}
}

func TestNewIssuerRequiresSigningKey(t *testing.T) {
	_, err := rsajwt.NewIssuer(rsajwt.IssuerConfig{Issuer: "identity", Audience: []string{"internal"}})
	if !errors.Is(err, rsajwt.ErrNoSigningKey) {
		t.Errorf("NewIssuer() error = %v, want wrapping ErrNoSigningKey", err)
	}
}

func TestIssueRequiresActorID(t *testing.T) {
	issuer, _ := newIssuerVerifier(t, time.Hour)
	if _, err := issuer.Issue(rsajwt.Actor{}); err == nil {
		t.Fatal("Issue() with an empty actor ID should return an error")
	}
}

func TestActorContext(t *testing.T) {
	actor := rsajwt.Actor{ID: "user-1"}
	ctx := rsajwt.WithActor(context.Background(), actor)

	got, ok := rsajwt.ActorFromContext(ctx)
	if !ok {
		t.Fatal("ActorFromContext() ok = false, want true")
	}
	if got.ID != actor.ID {
		t.Errorf("ActorFromContext() ID = %q, want %q", got.ID, actor.ID)
	}

	if _, ok := rsajwt.ActorFromContext(context.Background()); ok {
		t.Error("ActorFromContext() on an empty context should report ok = false")
	}
}

func TestCachedKeySet(t *testing.T) {
	key := generateKey(t)
	calls := 0
	fetch := func(context.Context) (rsajwt.KeySet, error) {
		calls++
		return rsajwt.KeySet{"kid-1": &key.PublicKey}, nil
	}

	provider, err := rsajwt.NewCachedKeySet(fetch, time.Hour)
	if err != nil {
		t.Fatalf("NewCachedKeySet() error: %v", err)
	}

	if _, err := provider.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet() error: %v", err)
	}
	if _, err := provider.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet() error: %v", err)
	}
	if calls != 1 {
		t.Errorf("fetch called %d times, want 1 (cache should serve the second call)", calls)
	}
}

func TestNewCachedKeySetRequiresFetcher(t *testing.T) {
	if _, err := rsajwt.NewCachedKeySet(nil, time.Hour); err == nil {
		t.Fatal("NewCachedKeySet(nil, ...) should return an error")
	}
}

func TestNewStaticKeySetRequiresKeys(t *testing.T) {
	if _, err := rsajwt.NewStaticKeySet(rsajwt.KeySet{}); !errors.Is(err, rsajwt.ErrNoSigningKey) {
		t.Errorf("NewStaticKeySet(empty) error = %v, want wrapping ErrNoSigningKey", err)
	}
}
