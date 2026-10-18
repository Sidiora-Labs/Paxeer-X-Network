package holdings

import (
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"math/big"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestPersistedMillionHolderDistribution(t *testing.T) {
	ctx, r := phase2Repository(t)
	token, pool := addr(1), addr(2)
	if err := r.Discover(ctx, token, 18, "10", true); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	var peak atomic.Uint64
	stop := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				var state runtime.MemStats
				runtime.ReadMemStats(&state)
				for old := peak.Load(); state.HeapAlloc > old; old = peak.Load() {
					if peak.CompareAndSwap(old, state.HeapAlloc) {
						break
					}
				}
			}
		}
	}()
	defer func() { close(stop); <-joined }()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	powers := [10]string{}
	numerics := [10]pgtype.Numeric{}
	total := new(big.Int)
	power := big.NewInt(1)
	for i := range powers {
		powers[i] = power.String()
		numerics[i] = pgtype.Numeric{Int: new(big.Int).Set(power), Valid: true}
		total.Add(total, new(big.Int).Mul(power, big.NewInt(100000)))
		power.Mul(power, big.NewInt(10))
	}
	whale := new(big.Int).Exp(big.NewInt(10), big.NewInt(25), nil)
	whaleNumeric := pgtype.Numeric{Int: whale, Valid: true}
	adjustment := new(big.Int).Sub(whale, big.NewInt(1000000000))
	adjustment.Mul(adjustment, big.NewInt(10))
	total.Add(total, adjustment)
	for offset := 0; offset < 1000000; offset += 1000 {
		_, err = tx.CopyFrom(ctx, pgx.Identifier{r.schema[1 : len(r.schema)-1], "balances"}, []string{"token", "address", "balance", "category"}, pgx.CopyFromSlice(1000, func(i int) ([]any, error) {
			n := offset + i
			raw := numerics[n%10]
			if n >= 999900 && n%10 == 9 {
				raw = whaleNumeric
			}
			return []any{token, fmt.Sprintf("0x%040x", n+100), raw, "external_wallet"}, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+r.schema+".aggregates SELECT token,category,LEAST(length(balance::text)-1,9),count(*),sum(balance) FROM "+r.schema+".balances GROUP BY token,category,LEAST(length(balance::text)-1,9)"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE "+r.schema+".tokens SET supply=$2,revision=1 WHERE token=$1", token, total.String()); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	cursor, err := r.Materialize(ctx, token, pool, "")
	if err != nil {
		t.Fatal(err)
	}
	page, err := r.ReadPage(ctx, token, pool, cursor, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Distribution.AllPositiveCount != 1000000 || page.Distribution.ExternalWalletCount != 1000000 || page.Distribution.Supply != total.String() || len(page.Snapshot.Balances) != 100 {
		t.Fatal(page.Distribution)
	}
	for i, band := range page.Distribution.Bands {
		raw, _ := new(big.Int).SetString(powers[i], 10)
		raw.Mul(raw, big.NewInt(100000))
		if i == 9 {
			raw.Add(raw, adjustment)
		}
		if band.Count != 100000 || band.Raw != raw.String() {
			t.Fatal(i, band, raw)
		}
	}
	want := new(big.Int).Mul(whale, big.NewInt(100000))
	want.Quo(want, total)
	if page.Snapshot.Balances[0].Raw != whale.String() {
		t.Fatal(page.Snapshot.Balances[0])
	}
	if page.Distribution.Top10.BasisPoints == nil || *page.Distribution.Top10.BasisPoints != want.String() {
		t.Fatal(page.Distribution.Top10, want)
	}
	var final runtime.MemStats
	runtime.ReadMemStats(&final)
	if final.HeapAlloc > peak.Load() {
		peak.Store(final.HeapAlloc)
	}
	if peak.Load() > initial.HeapAlloc+64*1024*1024 {
		t.Fatalf("bounded fixture/read heap exceeded: baseline=%d peak=%d", initial.HeapAlloc, peak.Load())
	}
	t.Logf("holders=1000000 batches=1000 rows_per_batch=1000 baseline_heap=%d peak_heap=%d supply=%s", initial.HeapAlloc, peak.Load(), total.String())
}
