package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/ratelimit"
	"github.com/adehikmatfr/go-pkg/v2/ratelimit/memory"
)

func TestNewInvalidConfig(t *testing.T) {
	if _, err := memory.New(ratelimit.Config{}); err == nil {
		t.Fatal("New() with zero-value config should return an error")
	}
}

func TestTakeAllowsUpToCapacityThenDenies(t *testing.T) {
	limiter, err := memory.New(ratelimit.Config{Tokens: 2, Interval: time.Minute})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = limiter.Close(context.Background()) }()

	ctx := context.Background()
	for i := 0; i < 2; i++ {
		allowed, _, err := limiter.Take(ctx, "key-1")
		if err != nil {
			t.Fatalf("Take() error: %v", err)
		}
		if !allowed {
			t.Fatalf("Take() call %d = denied, want allowed", i+1)
		}
	}

	allowed, retryAfter, err := limiter.Take(ctx, "key-1")
	if err != nil {
		t.Fatalf("Take() error: %v", err)
	}
	if allowed {
		t.Error("Take() over capacity = allowed, want denied")
	}
	if retryAfter <= 0 {
		t.Error("Take() denial should carry a positive retryAfter hint")
	}
}

func TestTakeIsPerKey(t *testing.T) {
	limiter, err := memory.New(ratelimit.Config{Tokens: 1, Interval: time.Minute})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = limiter.Close(context.Background()) }()

	ctx := context.Background()
	allowedA, _, _ := limiter.Take(ctx, "a")
	allowedB, _, _ := limiter.Take(ctx, "b")
	if !allowedA || !allowedB {
		t.Error("Take() for two distinct keys should both be allowed independently")
	}
}

func TestReset(t *testing.T) {
	limiter, err := memory.New(ratelimit.Config{Tokens: 1, Interval: time.Minute})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() { _ = limiter.Close(context.Background()) }()

	ctx := context.Background()
	if allowed, _, _ := limiter.Take(ctx, "key-1"); !allowed {
		t.Fatal("first Take() should be allowed")
	}
	if allowed, _, _ := limiter.Take(ctx, "key-1"); allowed {
		t.Fatal("second Take() before Reset should be denied")
	}

	if err := limiter.Reset(ctx, "key-1"); err != nil {
		t.Fatalf("Reset() error: %v", err)
	}
	if allowed, _, _ := limiter.Take(ctx, "key-1"); !allowed {
		t.Error("Take() after Reset should be allowed again")
	}
}

func TestCloseThenOperationsReturnErrClosed(t *testing.T) {
	limiter, err := memory.New(ratelimit.Config{Tokens: 1, Interval: time.Minute})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	ctx := context.Background()
	if err := limiter.Close(ctx); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	if _, _, err := limiter.Take(ctx, "key-1"); !errors.Is(err, ratelimit.ErrClosed) {
		t.Errorf("Take() after Close() error = %v, want wrapping ErrClosed", err)
	}
	if err := limiter.Reset(ctx, "key-1"); !errors.Is(err, ratelimit.ErrClosed) {
		t.Errorf("Reset() after Close() error = %v, want wrapping ErrClosed", err)
	}
	if err := limiter.Close(ctx); err != nil {
		t.Errorf("Close() called twice should be idempotent, got error: %v", err)
	}
}
