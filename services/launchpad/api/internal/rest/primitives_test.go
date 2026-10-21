package rest

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	redis "github.com/redis/go-redis/v9"
)

func TestL22PublicCacheCancellationBoundsAndIsolation(t *testing.T) {
	c, e := NewPublicCache(1024, 8, 2, 400*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	gate := make(chan struct{})
	entered := make(chan struct{})
	fetch := func(ctx context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(entered)
		}
		select {
		case <-gate:
			return []byte("original"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, e := c.GetOrFetch(firstCtx, "hot", time.Second, fetch); first <- e }()
	<-entered
	cancelFirst()
	if e = <-first; !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var cancelled, successful atomic.Int32
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if i%2 == 0 {
				cancel()
			}
			b, e := c.GetOrFetch(ctx, "hot", time.Second, fetch)
			if errors.Is(e, context.Canceled) {
				cancelled.Add(1)
			} else if e == nil && string(b) == "original" {
				successful.Add(1)
			} else {
				t.Errorf("waiter %q %v", b, e)
			}
		}(i)
	}
	close(gate)
	wg.Wait()
	if calls.Load() != 1 || cancelled.Load() != 500 || successful.Load() != 500 {
		t.Fatal(calls.Load(), cancelled.Load(), successful.Load())
	}
	b, e := c.GetOrFetch(context.Background(), "hot", time.Second, fetch)
	if e != nil {
		t.Fatal(e)
	}
	b[0] = 'X'
	b, e = c.GetOrFetch(context.Background(), "hot", time.Second, fetch)
	if string(b) != "original" {
		t.Fatal("caller mutated cache", string(b), e)
	}
	for i := 0; i < 100; i++ {
		_, e = c.GetOrFetch(context.Background(), strings.Repeat("k", i+1), time.Second, func(context.Context) ([]byte, error) { return make([]byte, 100), nil })
		if e != nil {
			t.Fatal(e)
		}
		bytes, entries, _ := c.Usage()
		if bytes > 1024 || entries > 8 {
			t.Fatal(bytes, entries)
		}
	}
	_, e = c.GetOrFetch(context.Background(), "huge", time.Second, func(context.Context) ([]byte, error) { return make([]byte, 1025), nil })
	if !errors.Is(e, ErrCacheBodyTooLarge) {
		t.Fatal(e)
	}
	bytes, entries, inflight := c.Usage()
	if bytes > 1024 || entries > 8 || inflight != 0 {
		t.Fatal(bytes, entries, inflight)
	}
	t.Log("1000 waiters; half cancelled; first caller cancelled independently; one fill; immutable cache bytes; LRU bounded")
}
func TestL22CacheSaturationDeadlineAndFailureRelease(t *testing.T) {
	c, _ := NewPublicCache(1024, 8, 1, 30*time.Millisecond)
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, e := c.GetOrFetch(context.Background(), "one", time.Second, func(ctx context.Context) ([]byte, error) { close(entered); <-ctx.Done(); return nil, ctx.Err() })
		done <- e
	}()
	<-entered
	if _, e := c.GetOrFetch(context.Background(), "two", time.Second, func(context.Context) ([]byte, error) { return nil, nil }); !errors.Is(e, ErrCacheFillSaturated) {
		t.Fatal(e)
	}
	if e := <-done; !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, _, n := c.Usage(); n != 0 {
		t.Fatal("fill leaked", n)
	}
	if _, e := c.GetOrFetch(context.Background(), "one", time.Second, func(context.Context) ([]byte, error) { return []byte("recovered"), nil }); e != nil {
		t.Fatal(e)
	}
}
func TestL22BatchBounds(t *testing.T) {
	var values []string
	for i := 0; i < 101; i++ {
		values = append(values, "0x"+strings.Repeat("1", 38)+string("0123456789abcdef"[i/16])+string("0123456789abcdef"[i%16]))
	}
	if _, e := PoolAddresses(strings.Join(values, ",")); e == nil {
		t.Fatal("oversized batch")
	}
	addresses, e := PoolAddresses(values[0] + "," + strings.ToUpper(values[0][0:0]) + values[0])
	if e != nil || len(addresses) != 1 {
		t.Fatal(addresses, e)
	}
}

func TestL22RealRedisCorruptionAndAggregateBounds(t *testing.T) {
	ctx := context.Background()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password", MaxRetries: -1})
	defer rdb.Close()
	if e := rdb.Ping(ctx).Err(); e != nil {
		t.Fatal(e)
	}
	a := "0x0a19ad96c21f31f2e55be1970a19ad96c21f31f2"
	b := "0x0a19ad96c21f31f2e55be1970a19ad96c21f31f3"
	generic := "kd-0a19ad96c21f31f2e55be197:corrupt-json"
	defer rdb.Del(ctx, "stats:"+a, "stats:"+b, generic)
	st := store.New(nil, rdb)
	assertIntegrity := func(pools ...string) {
		t.Helper()
		if result, e := st.StatsBatch(ctx, pools); !errors.Is(e, store.ErrDataIntegrity) || result != nil {
			t.Fatal(pools, len(result), e)
		}
	}
	for _, payload := range []string{"", `{"broken":`, strings.Repeat("x", store.PublicJSONMaxBytes+1)} {
		if e := rdb.Set(ctx, "stats:"+a, payload, time.Minute).Err(); e != nil {
			t.Fatal(e)
		}
		assertIntegrity(a)
		if e := rdb.Set(ctx, generic, payload, time.Minute).Err(); e != nil {
			t.Fatal(e)
		}
		if _, found, e := st.CachedJSON(ctx, generic); found || !errors.Is(e, store.ErrDataIntegrity) {
			t.Fatal(found, e)
		}
	}
	payload := `{"data":"` + strings.Repeat("x", 140<<10) + `"}`
	for _, addr := range []string{a, b} {
		if e := rdb.Set(ctx, "stats:"+addr, payload, time.Minute).Err(); e != nil {
			t.Fatal(e)
		}
	}
	// Both individual values are syntactically valid and below the byte cap;
	// together they exceed the aggregate bound.
	assertIntegrity(a, b)
	for _, addr := range []string{a, b} {
		if result, e := st.StatsBatch(ctx, []string{addr}); e != nil || len(result) != 1 {
			t.Fatal(addr, len(result), e)
		}
	}
}

func TestProductionAdmissionReservedCapacityAndCancellation(t *testing.T) {
	a, err := NewAdmission(10, 2, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	release, err := a.Acquire(context.Background(), AdmissionHistory)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err = a.Acquire(context.Background(), AdmissionCached); err == nil || time.Since(started) > 150*time.Millisecond {
		t.Fatal("public capacity exceeded or wait unbounded", err, time.Since(started))
	}
	recovery, err := a.Acquire(context.Background(), AdmissionRecovery)
	if err != nil {
		t.Fatal("reserved capacity unavailable", err)
	}
	health, err := a.Acquire(context.Background(), AdmissionHealth)
	if err != nil {
		t.Fatal("health reserve unavailable", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = a.Acquire(ctx, AdmissionRecovery); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	recovery()
	health()
	release()
	release()
	stats := a.Stats()
	if stats.Used != 0 || stats.PublicUsed != 0 || stats.Waiting != 0 || stats.Rejected != 1 || stats.Cancelled != 1 {
		t.Fatal(stats)
	}
}
func TestProductionAccountingCursorBoundRotationAndWatermark(t *testing.T) {
	old := AccountingV2Handler{SigningKey: []byte(strings.Repeat("a", 32))}
	request := store.AccountingPageRequest{Pool: "0x1111111111111111111111111111111111111111", Limit: 50, Side: "buy", SnapshotObservedAt: time.Now().Add(-time.Minute).UTC().Format("2006-01-02T15:04:05.000Z")}
	request.AfterOrder = &accounting.SourcePosition{ChainID: "125", Height: "9223372036854775806", TransactionIndex: "4294967295", LogIndex: "4294967295"}
	request.AfterEventID = strings.Repeat("a", 64)
	token, err := old.sign(request, false, "00000000-0000-7000-8000-000000000001", 9223372036854775805, 9223372036854775806)
	if err != nil || len(token) > 256 {
		t.Fatal(len(token), err)
	}
	rotated := AccountingV2Handler{SigningKey: []byte(strings.Repeat("b", 32)), PreviousSigningKeys: [][]byte{old.SigningKey}}
	page, err := rotated.cursor(request, false, token)
	if err != nil || page.SnapshotSequence == nil || *page.SnapshotSequence != 9223372036854775806 || page.SnapshotObservedAt != request.SnapshotObservedAt {
		t.Fatal(page, err)
	}
	request.Side = "sell"
	if _, err = rotated.cursor(request, false, token); err == nil {
		t.Fatal("filter-bound cursor crossed side")
	}
	request.Side = "buy"
	request.Pool = "0x2222222222222222222222222222222222222222"
	if _, err = rotated.cursor(request, false, token); err == nil {
		t.Fatal("pool-bound cursor crossed pool")
	}
	if _, err = rotated.cursor(page, false, token[:len(token)-1]+"!"); err == nil {
		t.Fatal("tampered cursor accepted")
	}
	expired := page
	expired.CursorExpires = time.Now().Add(-time.Second).Unix()
	token, err = rotated.sign(expired, false, page.ExpectedSnapshot, *page.AfterSequence, *page.SnapshotSequence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = rotated.cursor(page, false, token); err == nil {
		t.Fatal("expired cursor accepted")
	}
}
