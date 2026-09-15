// Package ratelimit is the rate-limiting port: a vendor-agnostic token-bucket
// RateLimiter interface, keyed per caller (e.g. an account id or IP hash),
// used to protect a sensitive or expensive operation from abuse.
//
// The storage/algorithm engine is internal to each adapter (e.g.
// ratelimit/memory); consumers depend only on RateLimiter and never see a
// vendor type, so the backend stays swappable without touching call sites.
package ratelimit

import (
	"context"
	"errors"
	"time"
)

// ErrClosed is returned by RateLimiter operations after the limiter has been
// closed. It is sentinel-matchable via errors.Is so callers can distinguish a
// shut-down limiter from a genuine denial.
var ErrClosed = errors.New("ratelimit: limiter is closed")

// RateLimiter enforces a token-bucket policy independently per key.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type RateLimiter interface {
	// Take consumes a single token for key and reports whether the request
	// is allowed. When denied, retryAfter is a positive best-effort wait
	// before the bucket refills; when allowed it is the time until the
	// interval resets and carries no obligation. A non-nil err (ErrClosed
	// for a closed limiter) always comes with allowed=false, so callers fail
	// closed.
	Take(ctx context.Context, key string) (allowed bool, retryAfter time.Duration, err error)

	// Reset clears any accumulated state for key, refilling its bucket to
	// the configured capacity. Resetting a key that has never been seen is a
	// no-op and returns no error.
	Reset(ctx context.Context, key string) error

	// Close releases resources held by the limiter (background goroutines,
	// the in-memory map, etc.). After Close, Take and Reset return an error
	// matching ErrClosed. Close is idempotent.
	Close(ctx context.Context) error
}

// Config controls a token-bucket RateLimiter, shared by every adapter.
type Config struct {
	// Tokens is the bucket capacity: the maximum number of requests
	// permitted per Interval for a single key. Must be greater than zero.
	Tokens uint64

	// Interval is the window over which Tokens are replenished. The bucket
	// refills to capacity at each interval boundary. Must be greater than
	// zero.
	Interval time.Duration
}

// Validate reports whether cfg is usable. Adapters call this before
// constructing their underlying store.
func (c Config) Validate() error {
	if c.Tokens == 0 {
		return errors.New("ratelimit: Tokens must be greater than zero")
	}
	if c.Interval <= 0 {
		return errors.New("ratelimit: Interval must be greater than zero")
	}
	return nil
}
