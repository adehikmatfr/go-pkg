package rsajwt

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// staticKeySet is a KeySetProvider backed by an immutable, in-memory key set —
// the right choice when verification keys are known at startup (e.g. loaded
// from config) and never change without a redeploy.
type staticKeySet struct {
	keys KeySet
}

// NewStaticKeySet builds a KeySetProvider from a fixed set of public keys
// keyed by kid. It is safe for concurrent use and never performs I/O.
func NewStaticKeySet(keys KeySet) (KeySetProvider, error) {
	if len(keys) == 0 {
		return nil, ErrNoSigningKey
	}
	copied := make(KeySet, len(keys))
	for kid, key := range keys {
		if key == nil {
			return nil, fmt.Errorf("rsajwt: nil public key for kid %q: %w", kid, ErrNoSigningKey)
		}
		copied[kid] = key
	}
	return &staticKeySet{keys: copied}, nil
}

// KeySet returns the static key set. See KeySetProvider.KeySet.
func (s *staticKeySet) KeySet(context.Context) (KeySet, error) {
	return s.keys, nil
}

// KeyFetcher loads the current set of trusted public keys from an external
// source (e.g. a remote JWKS endpoint, a secrets manager). It is the seam
// that keeps transport details out of this package: a caller supplies the
// fetch logic and cachedKeySet handles caching and refresh.
type KeyFetcher func(ctx context.Context) (KeySet, error)

// DefaultKeySetRefresh is the cache lifetime applied when none is configured.
const DefaultKeySetRefresh = 15 * time.Minute

// cachedKeySet is a KeySetProvider that serves a cached key set and refreshes
// it lazily when stale. A stale-but-present set is returned while a refresh
// runs in the background, so the verify path never blocks on the source.
type cachedKeySet struct {
	fetch    KeyFetcher
	interval time.Duration

	mu         sync.Mutex
	keys       KeySet
	fetchedAt  time.Time
	refreshing bool
}

// NewCachedKeySet builds a KeySetProvider that caches the keys returned by
// fetch and refreshes them after refreshInterval. The first call to KeySet
// performs a synchronous load; later calls serve the cache and trigger an
// asynchronous refresh once it goes stale. refreshInterval <= 0 applies
// DefaultKeySetRefresh.
func NewCachedKeySet(fetch KeyFetcher, refreshInterval time.Duration) (KeySetProvider, error) {
	if fetch == nil {
		return nil, fmt.Errorf("rsajwt: key fetcher is required: %w", ErrInvalidToken)
	}
	if refreshInterval <= 0 {
		refreshInterval = DefaultKeySetRefresh
	}
	return &cachedKeySet{fetch: fetch, interval: refreshInterval}, nil
}

// KeySet returns the cached key set, loading or refreshing it as needed. See
// KeySetProvider.KeySet.
func (c *cachedKeySet) KeySet(ctx context.Context) (KeySet, error) {
	c.mu.Lock()
	keys := c.keys
	stale := c.keys == nil || time.Since(c.fetchedAt) >= c.interval
	c.mu.Unlock()

	// Cold cache: load synchronously so the first verification has keys.
	if keys == nil {
		return c.refresh(ctx)
	}

	// Warm but stale: refresh in the background and serve the current set.
	if stale {
		c.triggerAsyncRefresh()
	}
	return keys, nil
}

// refresh loads a fresh key set and stores it.
func (c *cachedKeySet) refresh(ctx context.Context) (KeySet, error) {
	keys, err := c.fetch(ctx)
	if err != nil {
		return nil, fmt.Errorf("rsajwt: fetch key set: %w", err)
	}
	c.mu.Lock()
	c.keys = keys
	c.fetchedAt = time.Now()
	c.mu.Unlock()
	return keys, nil
}

// triggerAsyncRefresh starts a single background refresh if one is not
// already running. A refresh failure leaves the previous set in place.
func (c *cachedKeySet) triggerAsyncRefresh() {
	c.mu.Lock()
	if c.refreshing {
		c.mu.Unlock()
		return
	}
	c.refreshing = true
	c.mu.Unlock()

	go func() {
		defer func() {
			c.mu.Lock()
			c.refreshing = false
			c.mu.Unlock()
		}()
		// A fresh context, so the triggering request ending does not cancel
		// the refresh, bounded by the refresh interval so a hung source
		// cannot leak this goroutine and hold refreshing=true forever.
		ctx, cancel := context.WithTimeout(context.Background(), c.interval)
		defer cancel()
		_, _ = c.refresh(ctx)
	}()
}
