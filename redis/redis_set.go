package redis

import (
	"context"
	"encoding/json"
	"time"
)

func (c *client) SetString(ctx context.Context, key string, value string, expiration time.Duration) error {
	return c.rdb.Set(ctx, key, value, expiration).Err()
}

func (c *client) SetInt(ctx context.Context, key string, value int, expiration time.Duration) error {
	return c.rdb.Set(ctx, key, value, expiration).Err()
}

func (c *client) SetBool(ctx context.Context, key string, value bool, expiration time.Duration) error {
	return c.rdb.Set(ctx, key, value, expiration).Err()
}

func (c *client) SetObject(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	valueToSave, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return c.rdb.Set(ctx, key, valueToSave, expiration).Err()
}

func (c *client) Delete(ctx context.Context, key string) error {
	return c.rdb.Del(ctx, key).Err()
}
