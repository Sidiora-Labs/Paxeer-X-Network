package storage

import (
	"context"
	"fmt"
	redis "github.com/redis/go-redis/v9"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDurableLifecycle(t *testing.T) {
	ctx := context.Background()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	ns := "kd-fec98588ef6fe4334797a5de-" + time.Now().Format("150405000000000")
	l, e := NewLedger(db, ns)
	if e != nil {
		t.Fatal(e)
	}
	defer func() {
		ks, _ := db.Keys(ctx, ns+":*").Result()
		if len(ks) > 0 {
			db.Del(ctx, ks...)
		}
	}()
	d := strings.Repeat("a", 64)
	if s, e := l.Begin(ctx, "one", d, "purge"); e != nil || s != "requested" {
		t.Fatal(s, e)
	}
	if e = l.Advance(ctx, "one", "requested", "complete", "proof"); e == nil {
		t.Fatal("submission complete")
	}
	if e = l.Advance(ctx, "one", "requested", "accepted", ""); e != nil {
		t.Fatal(e)
	}
	l2, _ := NewLedger(db, ns)
	if s, e := l2.Begin(ctx, "one", d, "purge"); e != nil || s != "accepted" {
		t.Fatal(s, e)
	}
	if _, e = l.Begin(ctx, "one", strings.Repeat("b", 64), "purge"); e == nil {
		t.Fatal("identity reuse")
	}
	if e = l.Advance(ctx, "one", "accepted", "complete", ""); e == nil {
		t.Fatal("missing proof")
	}
	if e = l.Advance(ctx, "one", "accepted", "complete", "confirmed"); e != nil {
		t.Fatal(e)
	}
	key, _ := ApprovedKey([]byte("owned synthetic bytes"), "png256")
	h := strings.Split(key, "/")[1]
	if e = l.Allowed(ctx, key); e == nil {
		t.Fatal("unknown allowed")
	}
	for i, s := range []string{"approved", "blocked", "deleted"} {
		if e = l.Restrict(ctx, h, uint64(i+1), s); e != nil {
			t.Fatal(e)
		}
	}
	if e = l.Restrict(ctx, h, 4, "approved"); e == nil {
		t.Fatal("resurrection")
	}
	if e = l.AuthorizeDownload(ctx, key, 16*time.Minute); e == nil {
		t.Fatal("long presign")
	}
}
func TestApprovedKeyTraversal(t *testing.T) {
	for _, k := range []string{"private/original.png", "approved/../x", "approved/" + strings.Repeat("a", 64) + "/%2e%2e"} {
		if ValidApprovedKey(k) {
			t.Fatal(k)
		}
	}
}

func TestDurableExecutionSingleWinner(t *testing.T) {
	ctx := context.Background()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	ns := "kd-fec98588ef6fe4334797a5de-x" + time.Now().Format("150405000000000")
	l, _ := NewLedger(db, ns)
	defer func() {
		ks, _ := db.Keys(ctx, ns+":*").Result()
		if len(ks) > 0 {
			db.Del(ctx, ks...)
		}
	}()
	if _, e := l.Begin(ctx, "one", strings.Repeat("a", 64), "purge"); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ok, e := l.ClaimExecution(ctx, "one", fmt.Sprint(i))
			if e != nil {
				errs <- e
				return
			}
			if ok {
				if e = db.Incr(ctx, ns+":effect").Err(); e != nil {
					errs <- e
				}
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	if n, e := db.Get(ctx, ns+":effect").Int(); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	l2, _ := NewLedger(db, ns)
	if ok, e := l2.ClaimExecution(ctx, "one", "restart"); e != nil || ok {
		t.Fatal("restart reexecuted", ok, e)
	}
}

func TestDurableDuplicateAndOutOfOrderReceipts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db := redis.NewClient(&redis.Options{Addr: "127.0.0.1:26379", Password: "kindle_fixture_password"})
	defer db.Close()
	ns := "l41-receipts-" + fmt.Sprint(time.Now().UnixNano())
	ledger, e := NewLedger(db, ns)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Del(context.Background(), ledger.key("op", "one"))
	if _, e = ledger.Begin(ctx, "one", strings.Repeat("a", 64), "purge"); e != nil {
		t.Fatal(e)
	}
	if e = ledger.Advance(ctx, "one", "accepted", "complete", "receipt"); e == nil {
		t.Fatal("out of order completion accepted")
	}
	for i := 0; i < 2; i++ {
		if e = ledger.Advance(ctx, "one", "requested", "accepted", "provider-allocation"); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 2; i++ {
		if e = ledger.Advance(ctx, "one", "accepted", "complete", "receipt"); e != nil {
			t.Fatal(e)
		}
	}
	if e = ledger.Advance(ctx, "one", "accepted", "complete", "changed-receipt"); e == nil {
		t.Fatal("receipt conflict accepted")
	}
	if e = ledger.Advance(ctx, "one", "requested", "accepted", "provider-allocation"); e == nil {
		t.Fatal("older receipt regressed completion")
	}
	if state, e := ledger.Begin(ctx, "one", strings.Repeat("a", 64), "purge"); e != nil || state != "complete" {
		t.Fatal(state, e)
	}
}
