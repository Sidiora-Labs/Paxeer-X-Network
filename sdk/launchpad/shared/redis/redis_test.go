package redis

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// newRedisURL returns the already running Redis named by L09_REDIS_URL; the
// test fails immediately when it is unset (containers are never started).
func newRedisURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("L09_REDIS_URL")
	if u == "" {
		t.Fatal("L09_REDIS_URL is not set: point it at a running Redis logical database reserved for these tests (redis://:password@host:port/N); tests never start containers")
	}
	t.Cleanup(func() {
		c, e := NewClient(u)
		if e != nil {
			t.Error(e)
			return
		}
		defer c.Close()
		if e = c.Del(context.Background(), "kd-421d7541932d2fd2136b3973:legacy:bad", "kd-421d7541932d2fd2136b3973:legacy:k1", "kd-421d7541932d2fd2136b3973:legacy:k2").Err(); e != nil {
			t.Error(e)
		}
	})
	return u
}

func TestNewClientBadURL(t *testing.T) {
	t.Parallel()
	if _, err := NewClient("not a url"); err == nil {
		t.Fatal("want error for bad redis url")
	}
}

func TestPubSubConstructorsBadURL(t *testing.T) {
	t.Parallel()
	if _, err := NewPublisher("not a url"); err == nil {
		t.Error("NewPublisher bad url should error")
	}
	if _, err := NewSubscriber("not a url"); err == nil {
		t.Error("NewSubscriber bad url should error")
	}
}

func TestCacheErrorPaths(t *testing.T) {
	ctx := context.Background()
	url := newRedisURL(t)
	rdb, err := NewClient(url)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer rdb.Close()

	// Decode error: store invalid JSON, then typed CacheGet must error.
	if err := rdb.Set(ctx, "kd-421d7541932d2fd2136b3973:legacy:bad", "not-json", 0).Err(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := CacheGet[map[string]int](ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:bad"); err == nil {
		t.Error("CacheGet on invalid JSON should error")
	}

	// Encode error: a channel cannot be JSON-marshalled.
	type unencodable struct {
		C chan int `json:"c"`
	}
	if err := CacheSet(ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:enc", unencodable{C: make(chan int)}, 0); err == nil {
		t.Error("CacheSet of unmarshalable value should error")
	}

	// GetOrSet propagates a fetch error.
	if _, err := CacheGetOrSet(ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:miss", func() (int, error) {
		return 0, errors.New("fetch failed")
	}, time.Minute); err == nil {
		t.Error("CacheGetOrSet should propagate fetch error")
	}
}

func TestSubscribeHandlerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url := newRedisURL(t)

	sub, err := NewSubscriber(url)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	errc, err := sub.Subscribe(ctx, "kd-421d7541932d2fd2136b3973:legacy:indexer:swap", func(context.Context, []byte) error {
		return errors.New("handler boom")
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	pub, err := NewPublisher(url)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer pub.Close()

	deadline := time.After(5 * time.Second)
	for {
		if err := pub.Publish(ctx, "kd-421d7541932d2fd2136b3973:legacy:indexer:swap", map[string]int{"x": 1}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		select {
		case herr := <-errc:
			if herr == nil {
				t.Fatal("expected handler error on errc")
			}
			return
		case <-time.After(150 * time.Millisecond):
		case <-deadline:
			t.Fatal("handler error not surfaced in time")
		}
	}
}

func TestClientAndCacheIntegration(t *testing.T) {
	ctx := context.Background()
	url := newRedisURL(t)

	rdb, err := NewClient(url)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping: %v", err)
	}

	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	// Miss.
	if _, ok, err := CacheGet[payload](ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k1"); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
	// Set + hit.
	want := payload{Name: "sidiora", Count: 7}
	if err := CacheSet(ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k1", want, time.Minute); err != nil {
		t.Fatalf("CacheSet: %v", err)
	}
	got, ok, err := CacheGet[payload](ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k1")
	if err != nil || !ok || got != want {
		t.Fatalf("CacheGet = %+v ok=%v err=%v", got, ok, err)
	}
	// Invalidate.
	if err := CacheInvalidate(ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k1"); err != nil {
		t.Fatalf("CacheInvalidate: %v", err)
	}
	if _, ok, _ := CacheGet[payload](ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k1"); ok {
		t.Fatal("expected miss after invalidate")
	}

	// GetOrSet: first call fetches, second call serves cache.
	calls := 0
	fetch := func() (payload, error) { calls++; return payload{Name: "lazy", Count: calls}, nil }
	v1, err := CacheGetOrSet(ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k2", fetch, time.Minute)
	if err != nil || v1.Count != 1 {
		t.Fatalf("GetOrSet first = %+v err=%v", v1, err)
	}
	v2, err := CacheGetOrSet(ctx, rdb, "kd-421d7541932d2fd2136b3973:legacy:k2", fetch, time.Minute)
	if err != nil || v2.Count != 1 || calls != 1 {
		t.Fatalf("GetOrSet second = %+v calls=%d err=%v (should serve cache)", v2, calls, err)
	}
}

func TestPublishJSON(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Nil client is a no-op (services without Redis still run).
	if err := PublishJSON(ctx, nil, "kd-421d7541932d2fd2136b3973:legacy:stats:update", []byte(`{}`)); err != nil {
		t.Fatalf("PublishJSON(nil) = %v, want nil", err)
	}

	url := newRedisURL(t)
	sub, err := NewSubscriber(url)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	const channel = "kd-421d7541932d2fd2136b3973:legacy:stats:update"
	received := make(chan string, 1)
	if _, err := sub.Subscribe(ctx, channel, func(_ context.Context, payload []byte) error {
		received <- string(payload)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	rdb, err := NewClient(url)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer rdb.Close()

	// PublishJSON forwards the RAW bytes verbatim (no re-encoding) so a cache
	// payload is delivered byte-for-byte to subscribers.
	raw := []byte(`{"poolAddress":"0xabc","price":"100"}`)
	deadline := time.After(5 * time.Second)
	for {
		if err := PublishJSON(ctx, rdb, channel, raw); err != nil {
			t.Fatalf("PublishJSON: %v", err)
		}
		select {
		case got := <-received:
			if got != string(raw) {
				t.Fatalf("payload = %s, want %s", got, raw)
			}
			return
		case <-time.After(150 * time.Millisecond):
		case <-deadline:
			t.Fatal("did not receive published message in time")
		}
	}
}

func TestPubSubIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url := newRedisURL(t)

	sub, err := NewSubscriber(url)
	if err != nil {
		t.Fatalf("NewSubscriber: %v", err)
	}
	defer sub.Close()

	const channel = "kd-421d7541932d2fd2136b3973:legacy:indexer:swap"
	received := make(chan string, 1)
	if _, err := sub.Subscribe(ctx, channel, func(_ context.Context, payload []byte) error {
		received <- string(payload)
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	pub, err := NewPublisher(url)
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	defer pub.Close()

	// Give the subscription a moment to register, then publish.
	type swap struct {
		Pool string `json:"pool"`
	}
	deadline := time.After(5 * time.Second)
	for {
		if err := pub.Publish(ctx, channel, swap{Pool: "0xabc"}); err != nil {
			t.Fatalf("Publish: %v", err)
		}
		select {
		case got := <-received:
			if got != `{"pool":"0xabc"}` {
				t.Fatalf("payload = %s", got)
			}
			return
		case <-time.After(150 * time.Millisecond):
			// retry publish (subscriber may not be ready yet)
		case <-deadline:
			t.Fatal("did not receive published message in time")
		}
	}
}
