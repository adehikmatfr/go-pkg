// Package memory is a cache.Cache adapter backed by a process-local map.
// Use it for local development, tests, or a single-instance deployment where
// a real cache server isn't warranted — swap to cache/redis (or any other
// cache.Cache adapter) without touching any code that only depends on the
// cache.Cache port.
package memory

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"github.com/adehikmatfr/go-pkg/v2/datastore/cache"
)

type entry struct {
	value     []byte
	expiresAt time.Time
	hasExpiry bool
}

type client struct {
	mu   sync.RWMutex
	data map[string]entry
}

// NewClient builds a cache.Cache backed by an in-process map. There is
// nothing to dial, so it never fails to construct and Ping always succeeds.
func NewClient() cache.Cache {
	return &client{data: make(map[string]entry)}
}

func (c *client) get(key string) ([]byte, error) {
	c.mu.RLock()
	e, ok := c.data[key]
	c.mu.RUnlock()
	if !ok {
		return nil, cache.ErrNotFound
	}
	if e.hasExpiry && time.Now().After(e.expiresAt) {
		c.mu.Lock()
		delete(c.data, key)
		c.mu.Unlock()
		return nil, cache.ErrNotFound
	}
	return e.value, nil
}

func (c *client) set(key string, value []byte, expiration time.Duration) {
	e := entry{value: value}
	if expiration > 0 {
		e.hasExpiry = true
		e.expiresAt = time.Now().Add(expiration)
	}

	c.mu.Lock()
	c.data[key] = e
	c.mu.Unlock()
}

func (c *client) GetString(_ context.Context, key string) (string, error) {
	v, err := c.get(key)
	if err != nil {
		return "", err
	}
	return string(v), nil
}

func (c *client) GetInt(_ context.Context, key string) (int, error) {
	v, err := c.get(key)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(v))
}

func (c *client) GetBool(_ context.Context, key string) (bool, error) {
	v, err := c.get(key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(string(v))
}

func (c *client) GetObject(_ context.Context, key string, obj interface{}) error {
	v, err := c.get(key)
	if err != nil {
		return err
	}
	return json.Unmarshal(v, obj)
}

func (c *client) SetString(_ context.Context, key string, value string, expiration time.Duration) error {
	c.set(key, []byte(value), expiration)
	return nil
}

func (c *client) SetInt(_ context.Context, key string, value int, expiration time.Duration) error {
	c.set(key, []byte(strconv.Itoa(value)), expiration)
	return nil
}

func (c *client) SetBool(_ context.Context, key string, value bool, expiration time.Duration) error {
	c.set(key, []byte(strconv.FormatBool(value)), expiration)
	return nil
}

func (c *client) SetObject(_ context.Context, key string, value interface{}, expiration time.Duration) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.set(key, b, expiration)
	return nil
}

func (c *client) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	delete(c.data, key)
	c.mu.Unlock()
	return nil
}

func (c *client) Ping(_ context.Context) error { return nil }

func (c *client) Close() error { return nil }
