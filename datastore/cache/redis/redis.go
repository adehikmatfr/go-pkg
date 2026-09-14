// Package redis is a cache.Cache adapter backed by go-redis/v8 for the
// common get/set/delete operations used by application caches.
package redis

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"

	"github.com/adehikmatfr/go-pkg/v2/datastore/cache"
)

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

// NewClient builds a cache.Cache backed by Redis. Like database/sql, go-redis
// connects lazily on first use, so this does not fail even if the server is
// unreachable — call Ping to verify connectivity eagerly.
func NewClient(cfg *Config) cache.Cache {
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
