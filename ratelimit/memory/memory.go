// Package memory is a ratelimit adapter backed by an in-process token-bucket
// store (github.com/sethvargo/go-limiter/memorystore). It has no external
// dependency and is the right choice for a single-instance service or tests;
// a multi-instance deployment needs a shared-state adapter instead (e.g. a
// future Redis-backed one) so every instance enforces the same bucket.
package memory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	limiter "github.com/sethvargo/go-limiter"
	"github.com/sethvargo/go-limiter/memorystore"

	"github.com/adehikmatfr/go-pkg/v2/ratelimit"
)

// rateLimiter is the memorystore-backed ratelimit.RateLimiter. The vendor
// store is held here and never exposed.
//
// closed is tracked explicitly rather than relying solely on the vendor
// store's own stopped state: memorystore's Set (which Reset calls) does not
// check it, so Reset would otherwise silently succeed after Close.
type rateLimiter struct {
	store    limiter.Store
	tokens   uint64
	interval time.Duration
	closed   atomic.Bool
}

var _ ratelimit.RateLimiter = (*rateLimiter)(nil)

// New constructs an in-memory token-bucket ratelimit.RateLimiter from cfg. It
// returns an error if the configuration is invalid. The returned limiter owns
// a background sweep goroutine; callers must Close it when done to avoid a
// leak.
func New(cfg ratelimit.Config) (ratelimit.RateLimiter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store, err := memorystore.New(&memorystore.Config{
		Tokens:   cfg.Tokens,
		Interval: cfg.Interval,
	})
	if err != nil {
		return nil, fmt.Errorf("ratelimit/memory: create store: %w", err)
	}

	return &rateLimiter{store: store, tokens: cfg.Tokens, interval: cfg.Interval}, nil
}

// Take consumes a token for key and reports whether the request is allowed.
func (l *rateLimiter) Take(ctx context.Context, key string) (bool, time.Duration, error) {
	if l.closed.Load() {
		return false, 0, ratelimit.ErrClosed
	}

	_, _, reset, ok, err := l.store.Take(ctx, key)
	if err != nil {
		// Normalize the store's ErrStopped to the port sentinel so callers
		// never import the vendor package.
		if errors.Is(err, limiter.ErrStopped) {
			return false, 0, ratelimit.ErrClosed
		}
		return false, 0, fmt.Errorf("ratelimit/memory: take token for key: %w", err)
	}

	retryAfter := l.retryAfter(reset)
	if !ok && retryAfter <= 0 {
		// A denial always carries a positive wait hint so callers can set
		// Retry-After.
		retryAfter = l.interval
	}

	return ok, retryAfter, nil
}

// retryAfter converts the store's absolute refill timestamp (nanoseconds
// since the Unix epoch) into a duration relative to now. It never returns a
// negative value.
func (l *rateLimiter) retryAfter(resetNanos uint64) time.Duration {
	// A reset past the int64 range is a corrupt timestamp, so fail safe with
	// no hint.
	if resetNanos == 0 || resetNanos > math.MaxInt64 {
		return 0
	}
	resetAt := time.Unix(0, int64(resetNanos))
	d := time.Until(resetAt)
	if d < 0 {
		return 0
	}
	return d
}

// Reset refills key's bucket to the configured capacity. It is a no-op for
// keys that have never been observed.
func (l *rateLimiter) Reset(ctx context.Context, key string) error {
	if l.closed.Load() {
		return ratelimit.ErrClosed
	}

	if err := l.store.Set(ctx, key, l.tokens, l.interval); err != nil {
		if errors.Is(err, limiter.ErrStopped) {
			return ratelimit.ErrClosed
		}
		return fmt.Errorf("ratelimit/memory: reset key: %w", err)
	}
	return nil
}

// Close releases the underlying store. It is idempotent.
func (l *rateLimiter) Close(ctx context.Context) error {
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}
	if err := l.store.Close(ctx); err != nil {
		return fmt.Errorf("ratelimit/memory: close store: %w", err)
	}
	return nil
}
