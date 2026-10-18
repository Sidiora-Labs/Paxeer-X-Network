// Package cache is the gateway's Redis hot-cache for small media objects. It
// sits in front of R2 so the edge serves frequently-requested logos/banners/og
// cards without an origin round-trip, while large objects bypass the cache and
// stream straight from R2 (see internal/serve).
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const keyPrefix = "gw:obj:"

// Object is a cached media object: its bytes plus the headers needed to serve
// it verbatim (Body is base64-encoded by the JSON codec in Redis).
type Object struct {
	ContentType string `json:"ct"`
	ETag        string `json:"etag"`
	Body        []byte `json:"body"`
}

// Cache is a typed Redis object cache.
type Cache struct {
	redis *goredis.Client
	ttl   time.Duration
}

// New constructs a Cache with the given TTL.
func New(redis *goredis.Client, ttl time.Duration) *Cache {
	return &Cache{redis: redis, ttl: ttl}
}

// Get returns the cached object for (bucket,key). found is false on a miss.
func (c *Cache) Get(ctx context.Context, bucket, key string) (Object, bool, error) {
	value, err := c.redis.Eval(ctx, `local n=redis.call('STRLEN',KEYS[1]);if n>1500000 then return redis.error_reply('media cache oversized') end;return redis.call('GET',KEYS[1])`, []string{redisKey(bucket, key)}).Result()
	var raw []byte
	if err == nil {
		v, ok := value.(string)
		if !ok {
			return Object{}, false, errors.New("invalid media cache")
		}
		raw = []byte(v)
	}
	if err == goredis.Nil {
		return Object{}, false, nil
	}
	if err != nil {
		return Object{}, false, err
	}
	if len(raw) > 1500000 {
		return Object{}, false, errors.New("oversized media cache")
	}
	var obj Object
	if json.Unmarshal(raw, &obj) != nil || !validObject(obj) {
		return Object{}, false, errors.New("corrupt media cache")
	}
	return obj, true, nil
}

// Set stores obj for (bucket,key) under the configured TTL.
func (c *Cache) Set(ctx context.Context, bucket, key string, obj Object) error {
	if !validObject(obj) || c.ttl.Milliseconds() <= 0 || c.ttl > 24*time.Hour {
		return errors.New("invalid media cache object or TTL")
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return c.redis.Eval(ctx, `redis.call('SET',KEYS[1],ARGV[1],'PX',ARGV[2]);redis.call('DEL',KEYS[2]);return 1`, []string{redisKey(bucket, key), redisKey(bucket, key) + ":missing"}, raw, c.ttl.Milliseconds()).Err()
}

func redisKey(bucket, key string) string {
	h := sha256.Sum256([]byte(bucket + "\x00" + key))
	return keyPrefix + hex.EncodeToString(h[:])
}
func validObject(o Object) bool {
	return (o.ContentType == "image/png" || o.ContentType == "image/jpeg" || o.ContentType == "image/webp" || o.ContentType == "application/octet-stream") && len(o.Body) <= 1<<20 && len(o.ContentType) <= 128 && len(o.ETag) <= 128 && !strings.ContainsAny(o.ContentType+o.ETag, "\r\n")
}

func (c *Cache) Missing(ctx context.Context, bucket, key string) (bool, error) {
	n, err := c.redis.Exists(ctx, redisKey(bucket, key)+":missing").Result()
	return n == 1, err
}

func (c *Cache) SetMissing(ctx context.Context, bucket, key string) error {
	return c.redis.Eval(ctx, `if redis.call('EXISTS',KEYS[1])==0 then redis.call('SET',KEYS[2],'1','EX',30) end;return 1`, []string{redisKey(bucket, key), redisKey(bucket, key) + ":missing"}).Err()
}

func (c *Cache) Evict(ctx context.Context, bucket, key string) error {
	if c == nil || c.redis == nil {
		return errors.New("media cache unavailable")
	}
	return c.redis.Del(ctx, redisKey(bucket, key), redisKey(bucket, key)+":missing").Err()
}
