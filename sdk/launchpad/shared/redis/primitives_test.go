package redis

import (
	"context"
	"encoding/json"
	r "github.com/redis/go-redis/v9"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestProjectionIsolation(t *testing.T) {
	a := KeyScope{"v1", "g1", "private", "wallet-a", "1"}
	b := a
	b.Principal = "wallet-b"
	ka, e := ProjectionKey(a, url.Values{"page": {"1"}})
	if e != nil {
		t.Fatal(e)
	}
	kb, _ := ProjectionKey(b, url.Values{"page": {"1"}})
	if ka == kb {
		t.Fatal("cross-principal collision")
	}
	b.Principal = ""
	if _, e = ProjectionKey(b, nil); e == nil {
		t.Fatal("missing principal accepted")
	}
	if _, e = ProjectionKey(a, url.Values{"page": {"1", "2"}}); e == nil {
		t.Fatal("duplicate variant accepted")
	}
}
func TestRealRedisPrimitives(t *testing.T) {
	u := os.Getenv("L09_REDIS_URL")
	if u == "" {
		t.Skip("requires real Redis L09_REDIS_URL")
	}
	ns := os.Getenv("L09_NAMESPACE")
	if ns == "" {
		t.Fatal("private namespace required")
	}
	o, e := r.ParseURL(u)
	if e != nil {
		t.Fatal(e)
	}
	c := r.NewClient(o)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e = c.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	ns += ":" + strconv.FormatInt(time.Now().UnixNano(), 10)
	keys := []string{ns + ":admit", ns + ":lease", ns + ":snapshot"}
	t.Cleanup(func() {
		cleanup := r.NewClient(o)
		defer cleanup.Close()
		if e := cleanup.Del(context.Background(), keys...).Err(); e != nil {
			t.Error(e)
		}
	})
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for replica := 0; replica < 10; replica++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client := r.NewClient(o)
			defer client.Close()
			for i := 0; i < 20; i++ {
				a, e := Admit(ctx, client, keys[0], 37, time.Minute)
				if e != nil {
					t.Error(e)
					return
				}
				if a.Allowed {
					admitted.Add(1)
				} else if a.RetryAfter <= 0 || a.RetryAfterSeconds() == "0" {
					t.Error("missing retry after")
				}
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 37 {
		t.Fatalf("aggregate admitted %d", admitted.Load())
	}
	token, e := AcquireRefresh(ctx, c, keys[1], 40*time.Millisecond)
	if e != nil || token == "" {
		t.Fatal(token, e)
	}
	time.Sleep(60 * time.Millisecond)
	newToken, e := AcquireRefresh(ctx, c, keys[1], time.Second)
	if e != nil || newToken == "" {
		t.Fatal(e)
	}
	if ok, e := ReleaseRefresh(ctx, c, keys[1], token); ok || e != nil {
		t.Fatal("old lease deleted current owner", e)
	}
	if ok, e := ReleaseRefresh(ctx, c, keys[1], newToken); !ok || e != nil {
		t.Fatal(e)
	}
	s := Snapshot{Generation: "g1", Revision: "9007199254740993", SourceWatermark: json.RawMessage(`{"height":"125"}`), FetchedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), Body: json.RawMessage(`{"value":2}`)}
	if ok, e := PutSnapshot(ctx, c, keys[2], s); !ok || e != nil {
		t.Fatal(e)
	}
	s.Revision = "9007199254740992"
	if ok, e := PutSnapshot(ctx, c, keys[2], s); ok || e != nil {
		t.Fatal("older snapshot replaced", e)
	}
	if ok, e := InvalidateSnapshot(ctx, c, keys[2], "g1", s.Revision); ok || e != nil {
		t.Fatal("older invalidation deleted", e)
	}
	if ok, e := InvalidateSnapshot(ctx, c, keys[2], "g1", "9007199254740994"); !ok || e != nil {
		t.Fatal(e)
	}
	if ok, e := PutSnapshot(ctx, c, keys[2], s); ok || e != nil {
		t.Fatal("old snapshot resurrected after invalidation", e)
	}
	if _, ok, e := GetSnapshot(ctx, c, keys[2]); ok || e != nil {
		t.Fatal("tombstone exposed", e)
	}
	c.Close()
	if a, e := Admit(ctx, c, keys[0], 37, time.Minute); e == nil || a.Allowed {
		t.Fatal("outage admitted")
	}
}
func TestJitterBounds(t *testing.T) {
	for i := 0; i < 1000; i++ {
		d, e := JitterTTL(time.Second, 200*time.Millisecond)
		if e != nil || d < 800*time.Millisecond || d > 1200*time.Millisecond {
			t.Fatal(d, e)
		}
	}
}

func TestRealRedisAbsentFloorAndEncodedBound(t *testing.T) {
	u := os.Getenv("L09_REDIS_URL")
	if u == "" {
		t.Skip("requires real Redis")
	}
	ns := os.Getenv("L09_NAMESPACE")
	if ns == "" {
		t.Fatal("namespace required")
	}
	o, e := r.ParseURL(u)
	if e != nil {
		t.Fatal(e)
	}
	c := r.NewClient(o)
	defer c.Close()
	ctx := context.Background()
	key := ns + ":floor:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	defer c.Del(ctx, key)
	if ok, e := InvalidateSnapshot(ctx, c, key, "g", "10"); !ok || e != nil {
		t.Fatal(e)
	}
	s := Snapshot{Generation: "g", Revision: "9", SourceWatermark: json.RawMessage(`null`), FetchedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), Body: json.RawMessage(`{"value":9}`)}
	if ok, e := PutSnapshot(ctx, c, key, s); ok || e != nil {
		t.Fatal("old snapshot resurrected absent invalidation", e)
	}
	if _, ok, e := GetSnapshot(ctx, c, key); ok || e != nil {
		t.Fatal("floor exposed", e)
	}
	ttl, e := c.PTTL(ctx, key).Result()
	if e != nil || ttl <= 0 || ttl > InvalidationFloorTTL {
		t.Fatal(ttl, e)
	}
	s.Revision = "11"
	s.SourceWatermark = json.RawMessage(`"` + strings.Repeat("x", MaxSnapshotBytes) + `"`)
	if ok, e := PutSnapshot(ctx, c, key, s); ok || e == nil {
		t.Fatal("oversized watermark accepted")
	}
	s.SourceWatermark = json.RawMessage(`null`)
	s.Body = json.RawMessage(`"` + strings.Repeat("<", MaxSnapshotBytes/2) + `"`)
	if ok, e := PutSnapshot(ctx, c, key, s); ok || e == nil {
		t.Fatal("JSON escape expansion accepted")
	}
	if ok, e := PutSnapshot(ctx, c, strings.Repeat("k", 513), s); ok || e == nil {
		t.Fatal("oversized key accepted")
	}
}
