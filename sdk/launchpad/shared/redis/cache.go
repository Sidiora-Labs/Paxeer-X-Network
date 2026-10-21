package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CacheGet returns the JSON-decoded value at key. found is false on cache miss.
func CacheGet[T any](ctx context.Context, rdb *redis.Client, key string) (value T, found bool, err error) {
	var zero T
	raw, err := rdb.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return zero, false, nil
	}
	if err != nil {
		return zero, false, fmt.Errorf("redis: cache get %q: %w", key, err)
	}
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return zero, false, fmt.Errorf("redis: cache decode %q: %w", key, err)
	}
	return v, true, nil
}

// CacheSet JSON-encodes data at key with an optional TTL (zero = no expiry).
func CacheSet[T any](ctx context.Context, rdb *redis.Client, key string, data T, ttl time.Duration) error {
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("redis: cache encode %q: %w", key, err)
	}
	if err := rdb.Set(ctx, key, b, ttl).Err(); err != nil {
		return fmt.Errorf("redis: cache set %q: %w", key, err)
	}
	return nil
}

// CacheInvalidate deletes a key.
func CacheInvalidate(ctx context.Context, rdb *redis.Client, key string) error {
	if err := rdb.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis: cache invalidate %q: %w", key, err)
	}
	return nil
}

// CacheGetOrSet returns the cached value, or calls fetch and caches the result
// (parity with TS cacheGetOrSet; stampede protection is added at the caller's
// singleflight layer per SECTION 17).
func CacheGetOrSet[T any](ctx context.Context, rdb *redis.Client, key string, fetch func() (T, error), ttl time.Duration) (T, error) {
	if v, ok, err := CacheGet[T](ctx, rdb, key); err != nil {
		return v, err
	} else if ok {
		return v, nil
	}
	fresh, err := fetch()
	if err != nil {
		return fresh, err
	}
	if err := CacheSet(ctx, rdb, key, fresh, ttl); err != nil {
		return fresh, err
	}
	return fresh, nil
}
