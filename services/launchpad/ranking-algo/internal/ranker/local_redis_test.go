package ranker

import (
	"context"
	"fmt"
	goredis "github.com/redis/go-redis/v9"
	"math"
	"os"
	"testing"
	"time"
)

func TestLocalRedisPublicationValidation(t *testing.T) {
	uri := os.Getenv("L19_REDIS_URL")
	if uri == "" {
		t.Fatal("L19_REDIS_URL required for real Redis qualification")
	}
	options, e := goredis.ParseURL(uri)
	if e != nil {
		t.Fatal(e)
	}
	rdb := goredis.NewClient(options)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	key := fmt.Sprintf("kd-25f7eaabd03e881749ed6ae1:ranking:%d", time.Now().UnixNano())
	defer rdb.Del(context.Background(), key)
	svc := NewService(nil, rdb, 2, nil)
	entries := []Scored{{"0xc", 1}, {"0xb", 1}, {"0xa", 1}}
	if e = svc.write(ctx, key, entries); e != nil {
		t.Fatal(e)
	}
	if n, e := rdb.ZCard(ctx, key).Result(); e != nil || n != 2 {
		t.Fatalf("bounded members: %d %v", n, e)
	}
	if _, e = rdb.ZScore(ctx, key, "0xc").Result(); e != goredis.Nil {
		t.Fatal("tie truncation must retain ascending addresses")
	}
	for _, score := range []float64{math.Inf(1), math.NaN(), -1} {
		if e = svc.write(ctx, key, []Scored{{"bad", score}}); e == nil {
			t.Fatal("invalid score published")
		}
	}
	if n, e := rdb.ZCard(ctx, key).Result(); e != nil || n != 2 {
		t.Fatal("invalid write replaced completed ranking")
	}
	if e = svc.write(ctx, key, nil); e != nil {
		t.Fatal(e)
	}
	if n, e := rdb.Exists(ctx, key).Result(); e != nil || n != 0 {
		t.Fatal("legacy empty semantics")
	}
}
