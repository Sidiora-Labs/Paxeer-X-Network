package projectionstats

import (
	"context"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func repository(t *testing.T) (context.Context, *Repository) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	dsn := os.Getenv("L13_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	p, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	schema := fmt.Sprintf("kd_ee41a0fc076410744f7056fe_%d", time.Now().UnixNano())
	if _, e = p.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); e != nil {
		t.Fatalf("real PostgreSQL required: %v", e)
	}
	t.Cleanup(func() {
		cleanup, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		if _, e := p.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Errorf("cleanup: %v", e)
		}
	})
	r, e := NewRepository(p, schema)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = p.Exec(ctx, r.MigrationSQL()); e != nil {
		t.Fatal(e)
	}
	return ctx, r
}
func input(t Trade, timestamp int64) ApplyInput {
	return ApplyInput{t, PoolAssets{usdl, token, 18}, 30, timestamp, 10000, 0, "consumer-v1", "projection-v1"}
}
func count(t *testing.T, ctx context.Context, r *Repository, table string) int {
	t.Helper()
	var n int
	if e := r.pool.QueryRow(ctx, "SELECT count(*) FROM "+r.schema+"."+table).Scan(&n); e != nil {
		t.Fatal(e)
	}
	return n
}
func TestPostgresReplayConflictRollbackAndPublication(t *testing.T) {
	ctx, r := repository(t)
	in := input(fixture("buy", "7"), 119)
	a, e := r.Apply(ctx, in)
	if e != nil || !a.Accepted || a.Revision != 1 {
		t.Fatalf("%+v %v", a, e)
	}
	a, e = r.Apply(ctx, in)
	if e != nil || a.Accepted || a.Revision != 1 {
		t.Fatalf("duplicate: %+v %v", a, e)
	}
	conflict := in
	conflict.BlockTimestamp = 120
	if _, e = r.Apply(ctx, conflict); !errors.Is(e, ErrIntegrity) {
		t.Fatalf("conflict: %v", e)
	}
	for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
		if n := count(t, ctx, r, table); n != 1 {
			t.Fatalf("%s count %d", table, n)
		}
	}
	// Force a real SQL failure after the bucket update and verify rollback of inbox
	// and all effects rather than relying on a mock transaction callback.
	_, e = r.pool.Exec(ctx, "ALTER TABLE "+r.schema+".outbox ADD CONSTRAINT reject_fixture CHECK(event_id NOT LIKE '%:8')")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = r.Apply(ctx, input(fixture("buy", "8"), 119)); e == nil {
		t.Fatal("expected outbox SQL failure")
	}
	var volume string
	var revision int64
	if e = r.pool.QueryRow(ctx, "SELECT volume::text,revision FROM "+r.schema+".minute_buckets").Scan(&volume, &revision); e != nil || volume != "10000000" || revision != 1 {
		t.Fatal(volume, revision, e)
	}
	t.Logf("persisted rollback verification: volume=%s revision=%d inbox=%d", volume, revision, count(t, ctx, r, "inbox"))
	if count(t, ctx, r, "inbox") != 1 {
		t.Fatal("rollback retained inbox")
	}
	failure := errors.New("transport unavailable")
	if _, e = r.PublishPending(ctx, 100, func(context.Context, Notification) error { return failure }); !errors.Is(e, failure) {
		t.Fatal(e)
	}
	n, e := r.PublishPending(ctx, 100, func(_ context.Context, n Notification) error {
		if n.Revision != 1 || n.EventID != in.Trade.EventID {
			t.Fatal(n)
		}
		return nil
	})
	if e != nil || n != 1 {
		t.Fatal(n, e)
	}
	n, e = r.PublishPending(ctx, 100, func(context.Context, Notification) error {
		t.Fatal("republished acknowledged notification")
		return nil
	})
	if n != 0 || e != nil {
		t.Fatal(n, e)
	}
}
func TestPostgresConcurrentDuplicate(t *testing.T) {
	ctx, r := repository(t)
	var wg sync.WaitGroup
	results := make(chan ApplyResult, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, e := r.Apply(ctx, input(fixture("buy", "7"), 119))
			results <- a
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	accepted := 0
	for a := range results {
		if a.Accepted {
			accepted++
		}
	}
	if accepted != 1 || count(t, ctx, r, "trade_facts") != 1 {
		t.Fatal(accepted)
	}
}
func TestPostgresGroupsWindowsOrderingAndCursor(t *testing.T) {
	ctx, r := repository(t)
	for i, tr := range []Trade{fixture("buy", "1"), fixture("sell", "2"), fixture("buy", "3")} {
		tr.Position.TransactionIndex = fmt.Sprint(3 - i)
		canonicalFixture(&tr)
		if _, e := r.Apply(ctx, input(tr, 0)); e != nil {
			t.Fatal(e)
		}
	}
	a, e := r.AggregateWindow(ctx, "projection-v1", "80", 3599, 3600)
	if e != nil || a.VenueVolumeRaw != "20456789" || a.PoolFillCount != 3 || a.UniqueActors != 1 || a.UserActionCount != nil || len(a.Fees) != 2 {
		t.Fatalf("%+v %v", a, e)
	}
	if a.Fees[0].Raw != "2700" || a.Fees[1].Raw != "54000" {
		t.Fatal(a.Fees)
	}
	a, e = r.AggregateWindow(ctx, "projection-v1", "80", 3600, 3600)
	if e != nil || a.PoolFillCount != 0 || a.VenueVolumeRaw != "0" {
		t.Fatalf("expiry %+v %v", a, e)
	}
	f := Filter{poolAddress, "", "projection-v1"}
	page, e := r.Page(ctx, f, 1, "80", "")
	if e != nil || len(page.Trades) != 1 || page.Trades[0].Position.TransactionIndex != "3" || page.NextCursor == nil {
		t.Fatalf("%+v %v", page, e)
	}
	newer := fixture("buy", "4")
	newer.Position.Height = "81"
	canonicalFixture(&newer)
	if _, e = r.Apply(ctx, input(newer, 120)); e != nil {
		t.Fatal(e)
	}
	next, e := r.Page(ctx, f, 100, "80", *page.NextCursor)
	if e != nil || len(next.Trades) != 2 || next.Trades[0].Position.TransactionIndex != "2" {
		t.Fatalf("%+v %v", next, e)
	}
	if _, e = r.Page(ctx, f, 101, "80", ""); !errors.Is(e, ErrInvalid) {
		t.Fatal("limit accepted", e)
	}
	var closePayload []byte
	if e = r.pool.QueryRow(ctx, "SELECT close_payload FROM "+r.schema+".minute_buckets WHERE start=0").Scan(&closePayload); e != nil {
		t.Fatal(e)
	}
	if string(closePayload) == "" {
		t.Fatal("missing historical close")
	}
}

func TestPostgresThreeAssetGroupsAndHistoricalClose(t *testing.T) {
	ctx, r := repository(t)
	amounts := []string{"3", "5", "7", "11"}
	for i, fee := range amounts {
		tr := fixture("buy", fmt.Sprint(i+1))
		if i >= 2 {
			tr = fixture("sell", fmt.Sprint(i+1))
		}
		// Unknown priced input is deliberate: emitted fees remain authoritative.
		tr.PricedInputRaw = nil
		tr.AMMInputAfterFeeRaw = nil
		tr.InputDerivationQuality = "receipt_incomplete"
		tr.FeeRaw = fee
		assets := PoolAssets{usdl, token, 18}
		if i == 3 {
			assets.Token = "0x5555555555555555555555555555555555555555"
			tr.FeeAssetAddress = assets.Token
			tr.PoolAddress = "0x6666666666666666666666666666666666666666"
			tr.EmittingContract = tr.PoolAddress
		}
		// Descending arrival positions exercise canonical close selection.
		tr.Position.TransactionIndex = fmt.Sprint(5 - i)
		canonicalFixture(&tr)
		in := input(tr, 119)
		in.Assets = assets
		if _, e := r.Apply(ctx, in); e != nil {
			t.Fatal(e)
		}
	}
	a, e := r.AggregateWindow(ctx, "projection-v1", "80", 120, 3600)
	if e != nil || len(a.Fees) != 3 {
		t.Fatalf("%+v %v", a, e)
	}
	t.Logf("persisted aggregate: %+v", a)
	for i, expected := range []string{"7", "8", "11"} {
		if a.Fees[i].Raw != expected {
			t.Fatalf("fee groups %+v", a.Fees)
		}
	}
	snapshot, found, e := r.MinuteSnapshot(ctx, poolAddress, "projection-v1", 60, 18)
	if e != nil || !found || snapshot.Revision != 3 || snapshot.Close.Position.TransactionIndex != "5" || snapshot.Price == nil || snapshot.Price.Numerator != "10000000" {
		t.Fatalf("snapshot %+v %v %v", snapshot, found, e)
	}
	t.Logf("persisted canonical close: %+v price=%+v", snapshot, snapshot.Price)
	for _, tr := range []Trade{fixture("buy", "1"), fixture("buy", "2")} {
		// Recreate precisely the original envelope to verify replay invariance.
		i := 0
		if tr.Position.LogIndex == "2" {
			i = 1
		}
		tr.FeeRaw = amounts[i]
		tr.PricedInputRaw = nil
		tr.AMMInputAfterFeeRaw = nil
		tr.InputDerivationQuality = "receipt_incomplete"
		tr.Position.TransactionIndex = fmt.Sprint(5 - i)
		canonicalFixture(&tr)
		if out, e := r.Apply(ctx, input(tr, 119)); e != nil || out.Accepted {
			t.Fatal(out, e)
		}
	}
	a, e = r.AggregateWindow(ctx, "projection-v1", "80", 120, 3600)
	if e != nil || a.PoolFillCount != 4 {
		t.Fatal(a, e)
	}
}

func canonicalFixture(tr *Trade) {
	h, _ := strconv.ParseUint(tr.Position.Height, 10, 64)
	i, _ := strconv.ParseUint(tr.Position.TransactionIndex, 10, 64)
	tr.Position.TransactionHash = fmt.Sprintf("0x%064x", h*1000+i)
	tr.Position.BlockHash = fmt.Sprintf("0x%064x", h)
	tr.EventID = tr.Position.Identity()
}
func TestPostgresCanonicalIntegrityGuards(t *testing.T) {
	for _, scenario := range []string{"position_other_hash", "transaction_other_height", "transaction_other_index", "height_other_block_hash", "height_other_timestamp", "pool_other_assets"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, r := repository(t)
			base := input(fixture("buy", "7"), 119)
			if _, e := r.Apply(ctx, base); e != nil {
				t.Fatal(e)
			}
			bad := input(fixture("buy", "8"), 119)
			switch scenario {
			case "position_other_hash":
				bad.Trade.Position.LogIndex = "7"
				bad.Trade.Position.TransactionHash = fmt.Sprintf("0x%064x", 999)
			case "transaction_other_height":
				bad.Trade.Position.Height = "81"
				bad.Trade.Position.BlockHash = fmt.Sprintf("0x%064x", 81)
			case "transaction_other_index":
				bad.Trade.Position.TransactionIndex = "3"
			case "height_other_block_hash":
				bad.Trade.Position.TransactionHash = fmt.Sprintf("0x%064x", 999)
				bad.Trade.Position.TransactionIndex = "3"
				bad.Trade.Position.BlockHash = fmt.Sprintf("0x%064x", 81)
			case "height_other_timestamp":
				bad.BlockTimestamp = 120
			case "pool_other_assets":
				bad.Assets.Token = "0x5555555555555555555555555555555555555555"
			}
			bad.Trade.EventID = bad.Trade.Position.Identity()
			if _, e := r.Apply(ctx, bad); !errors.Is(e, ErrIntegrity) {
				t.Fatalf("%s: expected integrity error, got %v", scenario, e)
			}
			for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox", "canonical_blocks", "canonical_transactions", "canonical_positions", "canonical_pool_assets"} {
				if n := count(t, ctx, r, table); n != 1 {
					t.Fatalf("%s changed after conflict: %d", table, n)
				}
			}
			var volume string
			var revision int64
			if e := r.pool.QueryRow(ctx, "SELECT volume::text,revision FROM "+r.schema+".minute_buckets").Scan(&volume, &revision); e != nil || volume != "10000000" || revision != 1 {
				t.Fatal(volume, revision, e)
			}
		})
	}
}

func TestPostgresProjectionEnrichmentUpgrade(t *testing.T) {
	ctx, r := repository(t)
	unknown := fixture("buy", "7")
	unknown.PricedInputRaw = nil
	unknown.AMMInputAfterFeeRaw = nil
	unknown.InputDerivationQuality = "receipt_incomplete"
	if out, e := r.Apply(ctx, input(unknown, 119)); e != nil || !out.Accepted {
		t.Fatal(out, e)
	}
	exact := input(fixture("buy", "7"), 119)
	exact.ConsumerVersion = "consumer-v2"
	exact.ProjectionVersion = "projection-v2"
	exact.Trade.Recipient = ptr("0x7777777777777777777777777777777777777777")
	exact.Trade.RecipientAttribution = "exact_receipt"
	exact.Trade.RecipientEvidenceRefs = []string{"synthetic-fixture-receipt-not-mined"}
	exact.Trade.CodeEpoch = "qualified-fixture-source-v2"
	if out, e := r.Apply(ctx, exact); e != nil || !out.Accepted {
		t.Fatalf("enrichment upgrade: %+v %v", out, e)
	}
	if out, e := r.Apply(ctx, exact); e != nil || out.Accepted {
		t.Fatalf("upgrade replay: %+v %v", out, e)
	}
	for i, field := range []string{"fee", "output", "payment"} {
		bad := input(unknown, 119)
		bad.ConsumerVersion = fmt.Sprintf("consumer-v%d", 3+i)
		bad.ProjectionVersion = fmt.Sprintf("projection-v%d", 3+i)
		switch field {
		case "fee":
			bad.Trade.FeeRaw = "27001"
		case "output":
			bad.Trade.TokenAmountRaw = "2000001"
		case "payment":
			bad.Trade.USDLAmountRaw = "10000001"
			bad.Trade.PaidInputRaw = ptr("10000001")
		}
		if _, e := r.Apply(ctx, bad); !errors.Is(e, ErrIntegrity) {
			t.Fatalf("changed immutable %s: %v", field, e)
		}
	}
	for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
		if n := count(t, ctx, r, table); n != 2 {
			t.Fatalf("%s: count=%d", table, n)
		}
	}
	for _, table := range []string{"canonical_blocks", "canonical_transactions", "canonical_positions", "canonical_pool_assets"} {
		if n := count(t, ctx, r, table); n != 1 {
			t.Fatalf("%s: count=%d", table, n)
		}
	}
}

func TestPostgresTransactionSeamDefersCommitToOuterOwner(t *testing.T) {
	ctx, r := repository(t)
	in := input(fixture("buy", "7"), 119)
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.ApplyTx(ctx, tx, in)
	if err != nil || !result.Accepted {
		t.Fatal(result, err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
		if count(t, ctx, r, table) != 0 {
			t.Fatal("outer rollback retained effects", table)
		}
	}
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ApplyTx(ctx, tx, in); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"inbox", "trade_facts", "minute_buckets", "fee_groups", "outbox"} {
		if count(t, ctx, r, table) != 1 {
			t.Fatal("outer commit did not persist effect", table)
		}
	}
}

func TestPostgresAccountingFreezeIsMonotonicAndTransactional(t *testing.T) {
	ctx, r := repository(t)
	evidence := accounting.Digest(strings.Repeat("a", 64))
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.FreezePoolsTx(ctx, tx, []string{poolAddress}, evidence); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if count(t, ctx, r, "accounting_pool_fences") != 0 {
		t.Fatal("rolled back freeze persisted")
	}
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.FreezePoolsTx(ctx, tx, []string{poolAddress}, evidence); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = r.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.FreezePoolsTx(ctx, tx, []string{poolAddress}, accounting.Digest(strings.Repeat("b", 64))); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var frozen bool
	var recorded string
	if err = r.pool.QueryRow(ctx, "SELECT frozen,evidence_digest FROM "+r.schema+".accounting_pool_fences WHERE pool=$1", poolAddress).Scan(&frozen, &recorded); err != nil || !frozen || recorded != string(evidence) {
		t.Fatal(frozen, recorded, err)
	}
	if _, _, err = r.AccountingSnapshot(ctx, poolAddress, "projection-v1", 60); !errors.Is(err, ErrIntegrity) {
		t.Fatal("contradicted snapshot was served", err)
	}
}
