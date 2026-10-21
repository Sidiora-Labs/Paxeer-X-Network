package streams

import (
	"context"
	"errors"
	"fmt"
	"github.com/redis/go-redis/v9"
	"os"
	"testing"
	"time"
)

func TestActualRedisViewerDedupThrottleRecoveryAndQuota(t *testing.T) {
	url := os.Getenv("L40_TEST_REDIS_URL")
	if url == "" {
		t.Fatal("L40_TEST_REDIS_URL is required; no silent fixture skip")
	}
	options, e := redis.ParseURL(url)
	if e != nil {
		t.Fatal(e)
	}
	client := redis.NewClient(options)

	ctx := context.Background()
	ns := fmt.Sprintf("kd-29e4009f8b95235d0159d804-%d", time.Now().UnixNano())
	counter, e := NewViewerCounter(client, ns, 2)
	if e != nil {
		t.Fatal(e)
	}
	stream := "11111111-1111-4111-8111-111111111111"
	session := "22222222-2222-4222-8222-222222222222"
	members, meta := counter.keys(stream)
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		defer client.Close()
		if e := client.Del(c, members, meta).Err(); e != nil {
			t.Error(e)
		}
	})
	if got, e := counter.Heartbeat(ctx, stream, session); !errors.Is(e, ErrUnavailable) || !got.Stale {
		t.Fatal(got, e)
	}
	if e = counter.Initialize(ctx, stream); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 10; i++ {
		got, e := counter.Heartbeat(ctx, stream, session)
		if e != nil || got.Count != 1 || !got.Stale || got.Accepted != (i == 0) {
			t.Fatal(i, got, e)
		}
	}
	if _, e = counter.Heartbeat(ctx, stream, "33333333-3333-4333-8333-333333333333"); e != nil {
		t.Fatal(e)
	}
	if _, e = counter.Heartbeat(ctx, stream, "44444444-4444-4444-8444-444444444444"); !errors.Is(e, ErrQuota) {
		t.Fatal(e)
	}
	// Set actual Redis sorted-set timestamps to exercise boundary pruning without sleeping90seconds.
	if e = client.ZAdd(ctx, members, redis.Z{Score: float64(time.Now().Add(-16 * time.Second).UnixMilli()), Member: session}).Err(); e != nil {
		t.Fatal(e)
	}
	if got, e := counter.Heartbeat(ctx, stream, session); e != nil || !got.Accepted || got.Count != 2 {
		t.Fatal(got, e)
	}
	if e = client.ZAdd(ctx, members, redis.Z{Score: float64(time.Now().Add(-91 * time.Second).UnixMilli()), Member: session}).Err(); e != nil {
		t.Fatal(e)
	}
	if got, e := counter.Heartbeat(ctx, stream, "33333333-3333-4333-8333-333333333333"); e != nil || got.Count != 1 {
		t.Fatal(got, e)
	}
	if e = client.Set(ctx, meta, time.Now().Add(-time.Second).UnixMilli(), time.Hour).Err(); e != nil {
		t.Fatal(e)
	}
	if got, e := counter.Heartbeat(ctx, stream, "33333333-3333-4333-8333-333333333333"); e != nil || got.Stale {
		t.Fatal(got, e)
	}
	if e = client.Del(ctx, members).Err(); e != nil {
		t.Fatal(e)
	}
	if got, e := counter.Heartbeat(ctx, stream, session); !errors.Is(e, ErrUnavailable) || !got.Stale {
		t.Fatal(got, e)
	}
	if e = counter.Initialize(ctx, stream); e != nil {
		t.Fatal(e)
	}
	if got, e := counter.Heartbeat(ctx, stream, session); e != nil || !got.Stale || got.Count != 1 {
		t.Fatal(got, e)
	}
	if e = client.Del(ctx, members, meta).Err(); e != nil {
		t.Fatal(e)
	}
	if got, e := counter.Heartbeat(ctx, stream, session); !errors.Is(e, ErrUnavailable) || !got.Stale {
		t.Fatal(got, e)
	}
	if _, e = counter.Heartbeat(ctx, stream, "127.0.0.1"); !errors.Is(e, ErrInvalid) {
		t.Fatal(e)
	}
}
