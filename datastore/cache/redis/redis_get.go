package redis

import (
	"context"
	"encoding/json"
	"errors"

	goredis "github.com/go-redis/redis/v8"

	"github.com/adehikmatfr/go-pkg/v2/datastore/cache"
)

// translateErr maps go-redis's own "not found" sentinel (redis.Nil) onto the
// cache port's ErrNotFound, so callers never need to import go-redis just to
// tell "key not found" apart from a real connection/read error.
func translateErr(err error) error {
	if errors.Is(err, goredis.Nil) {
		return cache.ErrNotFound
	}
	return err
}

func (c *client) GetString(ctx context.Context, key string) (string, error) {
	val, err := c.rdb.Get(ctx, key).Result()
	return val, translateErr(err)
}

func (c *client) GetInt(ctx context.Context, key string) (int, error) {
	val, err := c.rdb.Get(ctx, key).Int()
	return val, translateErr(err)
}

func (c *client) GetBool(ctx context.Context, key string) (bool, error) {
	val, err := c.rdb.Get(ctx, key).Bool()
	return val, translateErr(err)
}

func (c *client) GetObject(ctx context.Context, key string, obj interface{}) error {
	valBytes, err := c.rdb.Get(ctx, key).Bytes()
	if err != nil {
		return translateErr(err)
	}

	return json.Unmarshal(valBytes, obj)
}
