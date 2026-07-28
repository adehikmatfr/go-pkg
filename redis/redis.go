// Package redis is a thin, context-aware wrapper around go-redis/v8 for the
// common get/set/delete operations used by application caches.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// Redis is the operation set most application caches need. Every method
// takes a context so callers can bound a call with a deadline or cancel it
// on shutdown — the client does not hold or reuse a stored context.
type Redis interface {
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
	// fail fast rather than discovering a broken connection on first use.
	Ping(ctx context.Context) error
	// Close releases the underlying connection pool. Call it during shutdown.
	Close() error
}

type client struct {
	rdb *redis.Client
}

// Config addresses the Redis server to connect to.
type Config struct {
	Host     string
	Port     int
	Username string
	Password string
	DB       int
}

// NewClient builds a Redis client. Like database/sql, go-redis connects
// lazily on first use, so this does not fail even if the server is
// unreachable — call Ping to verify connectivity eagerly.
func NewClient(cfg *Config) Redis {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Username: cfg.Username,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	return &client{rdb: rdb}
}

func (c *client) Ping(ctx context.Context) error {
	return c.rdb.Ping(ctx).Err()
}

func (c *client) Close() error {
	return c.rdb.Close()
}
