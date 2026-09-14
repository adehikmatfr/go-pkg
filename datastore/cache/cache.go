// Package cache defines the Cache port: the operation set most application
// caches need, independent of which backend actually stores the data.
// Concrete backends live in subpackages (e.g. cache/redis, cache/memory) that
// implement this interface — swapping the backend is a one-line constructor
// change for the consumer, nothing else.
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned when a key does not exist in the cache. Every
// adapter must translate its backend's own "not found" signal (e.g.
// go-redis's redis.Nil) into this sentinel, so callers never need to import
// an adapter-specific package just to check for a cache miss.
var ErrNotFound = errors.New("cache: key not found")

// Cache is the port every cache backend adapter implements. Every method
// takes a context so callers can bound a call with a deadline or cancel it
// on shutdown — an adapter must not hold or reuse a stored context.
type Cache interface {
	GetString(ctx context.Context, key string) (string, error)
	GetInt(ctx context.Context, key string) (int, error)
	GetBool(ctx context.Context, key string) (bool, error)
	GetObject(ctx context.Context, key string, obj interface{}) error

	SetString(ctx context.Context, key string, value string, expiration time.Duration) error
	SetInt(ctx context.Context, key string, value int, expiration time.Duration) error
	SetBool(ctx context.Context, key string, value bool, expiration time.Duration) error
	SetObject(ctx context.Context, key string, value interface{}, expiration time.Duration) error

	Delete(ctx context.Context, key string) error

	// Ping verifies connectivity — use during health checks or at startup to
	// fail fast rather than discovering a broken backend on first use. An
	// in-process adapter (e.g. memory) that has no connection to verify
	// simply returns nil.
	Ping(ctx context.Context) error
	// Close releases any resource the adapter holds (a connection pool, a
	// background goroutine). Call it during shutdown.
	Close() error
}
