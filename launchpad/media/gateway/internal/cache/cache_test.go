package cache

import (
	"context"
	redis "github.com/redis/go-redis/v9"
	"strings"
	"testing"
	"time"
)

func TestRealRedisCacheBoundsAndIdentity(t *testing.T) {
	ctx := context.Background()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	c := New(db, time.Minute)
	bucket := "kd-fec98588ef6fe4334797a5de-" + time.Now().Format("150405000000000")
	key := "image"
	defer db.Del(ctx, redisKey(bucket, key))
	if redisKey("a:b", "c") == redisKey("a", "b:c") {
		t.Fatal("key collision")
	}
	if e := c.Set(ctx, bucket, key, Object{ContentType: "image/png", Body: []byte("synthetic")}); e != nil {
		t.Fatal(e)
	}
	o, ok, e := c.Get(ctx, bucket, key)
	if e != nil || !ok || string(o.Body) != "synthetic" {
		t.Fatal(o, ok, e)
	}
	if e := c.Set(ctx, bucket, key, Object{Body: make([]byte, (1<<20)+1)}); e == nil {
		t.Fatal("oversize")
	}
	if e := c.Set(ctx, bucket, key, Object{ETag: "x\r\nX: yes"}); e == nil {
		t.Fatal("header injection")
	}
	if e := db.Set(ctx, redisKey(bucket, key), strings.Repeat("x", 1500001), time.Minute).Err(); e != nil {
		t.Fatal(e)
	}
	if _, ok, e := c.Get(ctx, bucket, key); e == nil || ok {
		t.Fatal("oversize corruption accepted")
	}
}

func TestRealRedisMissingMarkerCannotSurvivePositivePublication(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	c := New(db, time.Minute)
	bucket := "l41-missing-" + time.Now().Format("150405000000000")
	key := "approved-image"
	defer c.Evict(context.Background(), bucket, key)
	if e := c.SetMissing(ctx, bucket, key); e != nil {
		t.Fatal(e)
	}
	if found, e := c.Missing(ctx, bucket, key); e != nil || !found {
		t.Fatal(found, e)
	}
	if e := c.Set(ctx, bucket, key, Object{ContentType: "image/png", Body: []byte("owned bytes")}); e != nil {
		t.Fatal(e)
	}
	if e := c.SetMissing(ctx, bucket, key); e != nil {
		t.Fatal(e)
	}
	if found, e := c.Missing(ctx, bucket, key); e != nil || found {
		t.Fatal("positive publication retained a negative entry", found, e)
	}
}
