package rsajwt_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/security/rsajwt"
)

func TestNewStaticKeySetRejectsNilKey(t *testing.T) {
	if _, err := rsajwt.NewStaticKeySet(rsajwt.KeySet{"kid-1": nil}); !errors.Is(err, rsajwt.ErrNoSigningKey) {
		t.Errorf("NewStaticKeySet(nil key) error = %v, want wrapping ErrNoSigningKey", err)
	}
}

func TestNewCachedKeySetDefaultsRefreshInterval(t *testing.T) {
	key := generateKey(t)
	fetch := func(context.Context) (rsajwt.KeySet, error) {
		return rsajwt.KeySet{"kid-1": &key.PublicKey}, nil
	}

	provider, err := rsajwt.NewCachedKeySet(fetch, 0)
	if err != nil {
		t.Fatalf("NewCachedKeySet() error: %v", err)
	}
	if _, err := provider.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet() error: %v", err)
	}
}

func TestCachedKeySetFetchErrorOnColdCache(t *testing.T) {
	wantErr := errors.New("boom")
	provider, err := rsajwt.NewCachedKeySet(func(context.Context) (rsajwt.KeySet, error) {
		return nil, wantErr
	}, time.Hour)
	if err != nil {
		t.Fatalf("NewCachedKeySet() error: %v", err)
	}

	if _, err := provider.KeySet(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("KeySet() error = %v, want wrapping %v", err, wantErr)
	}
}

func TestCachedKeySetRefreshesInBackgroundWhenStale(t *testing.T) {
	key := generateKey(t)
	var calls atomic.Int32
	fetch := func(context.Context) (rsajwt.KeySet, error) {
		calls.Add(1)
		return rsajwt.KeySet{"kid-1": &key.PublicKey}, nil
	}

	provider, err := rsajwt.NewCachedKeySet(fetch, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("NewCachedKeySet() error: %v", err)
	}

	// Cold load.
	if _, err := provider.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet() error: %v", err)
	}

	// Let the cache go stale, then serve-stale-and-refresh-in-background.
	time.Sleep(20 * time.Millisecond)
	if _, err := provider.KeySet(context.Background()); err != nil {
		t.Fatalf("KeySet() error: %v", err)
	}

	// The background refresh runs asynchronously; give it a moment to land.
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if calls.Load() < 2 {
		t.Errorf("fetch called %d times, want at least 2 (background refresh should have run)", calls.Load())
	}
}
