package engine

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"
)

func fillFixture() Fill {
	return Fill{ChainID: "125", EmittingContract: "emitter", CodeEpoch: "v1", TransactionHash: "tx", PoolAddress: "pool", Sender: "actor", Position: ChainOrder{1, 1, 1}, BlockTimestamp: 3600, TimestampAuthenticated: true, Side: "buy", TokenAmountRaw: "4000000", UsdlAmountRaw: "2000000", PaidInputRaw: "2000000", FeeRaw: "100", FeeDisposition: "distributed_usdl", InputDerivationQuality: "receipt_incomplete", TokenDecimals: 6, UsdlDecimals: 6}
}
func TestExactArithmetic(t *testing.T) {
	f := fillFixture()
	p, e := f.Price()
	if e != nil || p != (Rational{"1", "2"}) {
		t.Fatal(p, e)
	}
	f.Side = "sell"
	f.PaidInputRaw = "4000000"
	f.UsdlAmountRaw = "1800000"
	f.FeeDisposition = "retained_token_reserve"
	p, e = f.Price()
	if e != nil || p != (Rational{"9", "20"}) {
		t.Fatal(p, e)
	}
	for _, s := range []string{"-1", "+1", "1e6", "1.0", "01", "", "115792089237316195423570985008687907853269984665640564039457584007913129639936"} {
		if _, e := RawUint256(s); e == nil {
			t.Fatal("accepted", s)
		}
	}
	for _, tc := range []struct {
		r    Rational
		want string
	}{{Rational{"1", "8"}, "0.12"}, {Rational{"3", "8"}, "0.38"}} {
		got, e := tc.r.Render(2)
		if e != nil || got != tc.want {
			t.Fatal(got, e)
		}
	}
	if c, _ := (Rational{"9007199254740993", "1"}).Compare(Rational{"9007199254740994", "1"}); c != -1 {
		t.Fatal("precision")
	}
	f.TokenAmountRaw = "0"
	if _, e = f.Price(); e == nil {
		t.Fatal("zero accepted")
	}
}
func TestExactBuckets(t *testing.T) {
	for _, tc := range []struct {
		ts   int64
		tf   string
		want int64
	}{{59, "1m", 0}, {60, "1m", 60}, {299, "5m", 0}, {300, "5m", 300}, {3599, "1h", 0}, {3600, "1h", 3600}} {
		got, e := BucketStart(tc.ts, tc.tf)
		if e != nil || got != tc.want {
			t.Fatal(tc, got, e)
		}
	}
	for _, pair := range [][2]string{{"2026-10-11T23:59:59Z", "2026-10-05T00:00:00Z"}, {"2026-10-12T00:00:00Z", "2026-10-12T00:00:00Z"}, {"2024-02-29T12:00:00Z", "2024-02-26T00:00:00Z"}, {"2027-01-01T00:00:00Z", "2026-12-28T00:00:00Z"}} {
		a, _ := time.Parse(time.RFC3339, pair[0])
		b, _ := time.Parse(time.RFC3339, pair[1])
		got, e := BucketStart(a.Unix(), "1w")
		if e != nil || got != b.Unix() {
			t.Fatal(got, e)
		}
	}
	if _, e := BucketStart(0, "1M"); !errors.Is(e, ErrUnsupportedResolution) {
		t.Fatal(e)
	}
}
func TestExactOrder(t *testing.T) {
	f := fillFixture()
	f.TokenAmountRaw = "1000000"
	var c *ExactCandle
	for _, v := range []struct {
		order uint64
		price string
	}{{3, "3000000"}, {1, "2000000"}, {2, "5000000"}} {
		f.Position.TransactionIndex = v.order
		f.UsdlAmountRaw = v.price
		f.PaidInputRaw = v.price
		next, e := FoldExact(c, f, "1h", true)
		if e != nil {
			t.Fatal(e)
		}
		c = &next
	}
	if c.Open.Numerator != "2" || c.High.Numerator != "5" || c.Low.Numerator != "2" || c.Close.Numerator != "3" || c.Revision != 3 {
		t.Fatal(c)
	}
}
func TestExactPostgres(t *testing.T) {
	dsn := os.Getenv("L10_TEST_POSTGRES")
	if dsn == "" {
		t.Fatal("L10_TEST_POSTGRES required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := "kd_48ae65ff33f9dcf5330f2fbc"
	ddl, _ := store.ExactSchemaDDL(schema)
	if _, e = pool.Exec(ctx, ddl); e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
	st, _ := store.NewExact(pool, schema)
	f := fillFixture()
	var wg sync.WaitGroup
	errs := make(chan error, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- ProcessExactFill(ctx, st, f, 1) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	read := func(p string) ExactCandle {
		var b []byte
		if e := pool.QueryRow(ctx, "SELECT snapshot FROM "+schema+".chart_exact WHERE pool=$1 AND interval='1h'", p).Scan(&b); e != nil {
			t.Fatal(e)
		}
		var c ExactCandle
		if e := json.Unmarshal(b, &c); e != nil {
			t.Fatal(e)
		}
		return c
	}
	c := read("pool")
	if c.Trades != 1 || c.Revision != 1 || c.VolumeUsdlRaw != "2000000" {
		t.Fatal(c)
	}
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			g := f
			g.PoolAddress = "race"
			g.TransactionHash = "race" + strconv.Itoa(i)
			g.UsdlAmountRaw = strconv.Itoa((i + 1) * 1000000)
			g.PaidInputRaw = g.UsdlAmountRaw
			if e := ProcessExactFill(ctx, st, g, 1); e != nil {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	c = read("race")
	if c.Trades != 2 || c.VolumeUsdlRaw != "3000000" {
		t.Fatal(c)
	}
	if _, e = pool.Exec(ctx, "CREATE FUNCTION "+schema+".reject_week() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.interval='1w' THEN RAISE EXCEPTION 'forced rollback'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_week BEFORE INSERT ON "+schema+".chart_outbox FOR EACH ROW EXECUTE FUNCTION "+schema+".reject_week()"); e != nil {
		t.Fatal(e)
	}
	g := f
	g.TransactionHash = "rollback"
	if e = ProcessExactFill(ctx, st, g, 1); e == nil {
		t.Fatal("missing rollback")
	}
	var n int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".chart_inbox WHERE tx_hash='rollback'").Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".chart_outbox WHERE pool='pool'").Scan(&n); e != nil || n != 7 {
		t.Fatal(n, e)
	}
	if _, e = pool.Exec(ctx, "DROP TRIGGER reject_week ON "+schema+".chart_outbox"); e != nil {
		t.Fatal(e)
	}
	if e = ProcessExactFill(ctx, st, g, 2); e != nil {
		t.Fatal(e)
	}
	g.TransactionHash = "stale"
	if e = ProcessExactFill(ctx, st, g, 1); !errors.Is(e, store.ErrStaleOwner) {
		t.Fatal(e)
	}

	// Cross-pool conflict freezes the original and incoming pool durably.
	cross := f
	cross.PoolAddress = "original"
	cross.TransactionHash = "cross-pool"
	if e = ProcessExactFill(ctx, st, cross, 1); e != nil {
		t.Fatal(e)
	}
	cross.PoolAddress = "incoming"
	if e = ProcessExactFill(ctx, st, cross, 1); !errors.Is(e, store.ErrIntegrity) {
		t.Fatal("cross-pool conflict", e)
	}
	for _, p := range []string{"original", "incoming"} {
		cross.PoolAddress = p
		cross.TransactionHash = "after-conflict-" + p
		if e = ProcessExactFill(ctx, st, cross, 1); !errors.Is(e, store.ErrIntegrity) {
			t.Fatal("pool not frozen", p, e)
		}
	}
	// Corrupted stored aggregate rolls back earlier interval writes and inbox.
	if _, e = pool.Exec(ctx, "UPDATE "+schema+".chart_exact SET snapshot=jsonb_set(snapshot,'{volumeUsdlRaw}','\"bad\"') WHERE pool='race' AND interval='1h'"); e != nil {
		t.Fatal(e)
	}
	corrupt := f
	corrupt.PoolAddress = "race"
	corrupt.TransactionHash = "corrupt-snapshot"
	var before int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".chart_outbox WHERE pool='race'").Scan(&before); e != nil {
		t.Fatal(e)
	}
	if e = ProcessExactFill(ctx, st, corrupt, 1); !errors.Is(e, ErrInvalidFill) {
		t.Fatal("corrupt snapshot did not fail cleanly", e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".chart_outbox WHERE pool='race'").Scan(&n); e != nil || n != before {
		t.Fatal("corruption left partial outbox", n, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+schema+".chart_inbox WHERE tx_hash='corrupt-snapshot'").Scan(&n); e != nil || n != 0 {
		t.Fatal("corruption left claim", n, e)
	}
	g = f
	g.UsdlAmountRaw = "3000000"
	g.PaidInputRaw = g.UsdlAmountRaw
	if e = ProcessExactFill(ctx, st, g, 2); !errors.Is(e, store.ErrIntegrity) {
		t.Fatal(e)
	}
	g.TransactionHash = "frozen"
	if e = ProcessExactFill(ctx, st, g, 2); !errors.Is(e, store.ErrIntegrity) {
		t.Fatal(e)
	}
}

func TestExactFeeBasisAndLargeVolumes(t *testing.T) {
	f := fillFixture()
	priced, net := "2000000", "1999900"
	f.PricedInputRaw = &priced
	f.AmmInputAfterFeeRaw = &net
	f.InputDerivationQuality = "exact"
	p, e := f.FeeExclusivePrice()
	if e != nil || p == nil {
		t.Fatal(p, e)
	}
	if text, _ := p.Render(6); text != "0.499975" {
		t.Fatal(text)
	}
	f.InputDerivationQuality = "receipt_incomplete"
	f.PricedInputRaw = nil
	f.AmmInputAfterFeeRaw = nil
	if p, e := f.FeeExclusivePrice(); e != nil || p != nil {
		t.Fatal("invented fee-exclusive price", p, e)
	}
	f.UsdlAmountRaw = "9007199254740993"
	f.PaidInputRaw = f.UsdlAmountRaw
	first, e := FoldExact(nil, f, "1h", true)
	if e != nil {
		t.Fatal(e)
	}
	f.UsdlAmountRaw = "9007199254740994"
	f.PaidInputRaw = f.UsdlAmountRaw
	f.Position.TransactionIndex++
	second, e := FoldExact(&first, f, "1h", false)
	if e != nil {
		t.Fatal(e)
	}
	if second.VolumeUsdlRaw != "18014398509481987" {
		t.Fatal(second.VolumeUsdlRaw)
	}
	if cmp, _ := second.High.Compare(first.High); cmp != 1 {
		t.Fatal("one-unit high collapsed")
	}
	f.TimestampAuthenticated = false
	if _, e := f.Price(); e == nil {
		t.Fatal("missing authenticated timestamp accepted")
	}
}

func TestFaultCanonicalCandleReceiptReference(t *testing.T) {
	first := seriesSource(1, 1700000000, "")
	last := seriesSource(2, 1700000001, "")
	bar, err := FoldQualified(nil, last, "1m", false)
	if err != nil {
		t.Fatal(err)
	}
	bar, err = FoldQualified(&bar, first, "1m", false)
	if err != nil {
		t.Fatal(err)
	}
	state, err := DescribeSeries(bar, SeriesCheckpoint{Height: 500, Timestamp: 1700000600}, time.Unix(1700000601, 0))
	if err != nil {
		t.Fatal(err)
	}
	state.DisplayRevision = 2
	mapped, err := CanonicalCandle(state, map[string]accounting.FillV1{first.SourceFillID: first, last.SourceFillID: last})
	if err != nil {
		t.Fatal(err)
	}
	if mapped.Open.Source.SourceReference == nil || *mapped.Open.Source.SourceReference != first.Provenance.ReceiptSHA256 || mapped.Close.Source.SourceReference == nil || *mapped.Close.Source.SourceReference != last.Provenance.ReceiptSHA256 {
		t.Fatal("canonical price lost exact receipt source reference", mapped)
	}
	invalid := last
	invalid.Provenance.ReceiptSHA256 = ""
	if _, err = CanonicalCandle(state, map[string]accounting.FillV1{first.SourceFillID: first, last.SourceFillID: invalid}); err == nil {
		t.Fatal("missing actual receipt provenance accepted")
	}
}
