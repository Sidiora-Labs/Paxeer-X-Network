package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func accountingFixture() accounting.FillV1 {
	f := accounting.FillV1{
		Version:       accounting.FillVersion,
		Position:      accounting.SourcePosition{ChainID: "125", Height: "100", TransactionIndex: "0", LogIndex: "7", TransactionHash: "0x" + strings.Repeat("a", 64), BlockHash: "0x" + strings.Repeat("b", 64)},
		ActualEmitter: "0x" + strings.Repeat("1", 40), PoolAddress: "0x" + strings.Repeat("2", 40),
		Token: accounting.Asset{Address: "0x" + strings.Repeat("3", 40), Decimals: 18}, Settlement: accounting.Asset{Address: "0x" + strings.Repeat("4", 40), Decimals: 6},
		Side: "buy", TokenAmountRaw: "332665999332665", USDLAmountRaw: "5000000000", PaidInputRaw: "5000000000", FeeRaw: "15000000", InputDerivationQuality: "receipt_incomplete", FeeDisposition: "distributed_usdl",
		Attribution: accounting.Attribution{TransactionSender: "0x" + strings.Repeat("5", 40), Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Execution:   accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		Provenance:  accounting.Provenance{DeploymentID: "synthetic-chart-arithmetic", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch-1", PoolCodeEpoch: "epoch-1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}, BlockTimestampSeconds: "1700000000",
	}
	f.SourceFillID = accounting.SourceFillIdentity(f.Position, f.Provenance.DeploymentID)
	return f
}
func TestAccountingQualifiedExactSources(t *testing.T) {
	first := accountingFixture()
	copied, err := engine.QualifiedFill(first)
	if err != nil {
		t.Fatal(err)
	}
	if copied.PricedInputRaw != nil || copied.AmmInputAfterFeeRaw != nil || copied.Sender != "" {
		t.Fatal("unavailable input/actor authority invented", copied)
	}
	candle, err := engine.FoldQualified(nil, first, "1m", false)
	if err != nil {
		t.Fatal(err)
	}
	numerator := new(big.Int).Mul(big.NewInt(5000000000), new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	denominator := new(big.Int).Mul(big.NewInt(332665999332665), big.NewInt(1000000))
	expected := new(big.Rat).SetFrac(numerator, denominator)
	if candle.Open.Numerator != expected.Num().String() || candle.Open.Denominator != expected.Denom().String() {
		t.Fatal("exact rational price changed", candle.Open)
	}
	later := first
	later.Position.TransactionIndex = "1"
	later.Position.LogIndex = "8"
	later.Position.TransactionHash = "0x" + strings.Repeat("e", 64)
	later.USDLAmountRaw = "7000000000"
	later.PaidInputRaw = later.USDLAmountRaw
	later.SourceFillID = accounting.SourceFillIdentity(later.Position, later.Provenance.DeploymentID)
	second, err := engine.FoldQualified(&candle, later, "1m", false)
	if err != nil {
		t.Fatal(err)
	}
	if second.OpenSource.Position != first.Position || second.HighSource.Position != later.Position || second.CloseSource.Position != later.Position || second.LowSource.Position != first.Position || second.Token != first.Token || second.Settlement != first.Settlement {
		t.Fatal("qualified source/asset identity lost", second)
	}
	if second.VolumeUsdlRaw != "12000000000" || second.UniqueTraders != 0 {
		t.Fatal("volume or unavailable actor changed", second)
	}
	wire, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip engine.QualifiedCandle
	if err = json.Unmarshal(wire, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if roundtrip.CloseSource.Position.BlockHash != later.Position.BlockHash || roundtrip.CloseSource.Position.TransactionHash != later.Position.TransactionHash {
		t.Fatal("qualified block/transaction hashes missing")
	}
	conflict := later
	conflict.Position.BlockHash = "0x" + strings.Repeat("f", 64)
	if _, err = engine.FoldQualified(&second, conflict, "1m", false); err == nil {
		t.Fatal("same chain order conflicting block accepted")
	}
	inconsistent := second
	inconsistent.FirstPosition.LogIndex++
	if _, err = engine.FoldQualified(&inconsistent, later, "1m", false); err == nil {
		t.Fatal("qualified/legacy coordinate disagreement accepted")
	}
}
func TestAccountingQualifiedOuterTransactionPostgres(t *testing.T) {
	dsn := os.Getenv("DELIVERY_TEST_DSN")
	prefix := os.Getenv("DELIVERY_TEST_SCHEMA")
	if dsn == "" || !strings.HasPrefix(prefix, "kd_") || len(prefix) > 45 {
		t.Fatal("explicit private PostgreSQL fixture and bounded kd_ namespace required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 25432 || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("refusing non-fixture PostgreSQL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := prefix + "_chart_qualified"
	q := pgx.Identifier{schema}.Sanitize()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA "+q); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+q+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	ddl, err := store.ExactSchemaDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	exact, err := store.NewExact(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	source := accountingFixture()
	inspect := func(want int) {
		t.Helper()
		var inbox, effects, outbox int
		err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+q+".chart_inbox),(SELECT count(*) FROM "+q+".chart_exact),(SELECT count(*) FROM "+q+".chart_outbox)").Scan(&inbox, &effects, &outbox)
		if err != nil {
			t.Fatal(err)
		}
		if inbox != want || effects != want*len(engine.ExactIntervals) || outbox != want*len(engine.ExactIntervals) {
			t.Fatalf("durable inbox/effect/outbox=%d/%d/%d", inbox, effects, outbox)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ApplyQualifiedTx(ctx, tx, exact, source, 1); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	inspect(0)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ApplyQualifiedTx(ctx, tx, exact, source, 1); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	inspect(1)
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.ApplyQualifiedTx(ctx, tx, exact, source, 1); err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	inspect(1)
}

func TestSeriesTransactionalMembershipAndRebuildPostgres(t *testing.T) {
	dsn := os.Getenv("DELIVERY_TEST_DSN")
	prefix := os.Getenv("DELIVERY_TEST_SCHEMA")
	if dsn == "" || !strings.HasPrefix(prefix, "kd_") || len(prefix) > 45 {
		t.Fatal("explicit private fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 25432 || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("refusing non-fixture PostgreSQL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	schema := prefix + "_chart_series"
	q := pgx.Identifier{schema}.Sanitize()
	defer pool.Exec(context.Background(), "DROP SCHEMA "+q+" CASCADE")
	for _, ddl := range []func(string) (string, error){store.ExactSchemaDDL, store.SeriesSchemaDDL, store.RebuildSchemaDDL} {
		sql, err := ddl(schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	exact, err := store.NewExact(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	source := accountingFixture()
	source.BlockTimestampSeconds = "1700000040"
	actorA := "0x" + strings.Repeat("a", 40)
	actorB := "0x" + strings.Repeat("b", 40)
	source.Attribution.Quality = "exact_receipt"
	source.Attribution.Evidence = []accounting.EvidenceRef{{Kind: "receipt", ArtifactSHA256: strings.Repeat("c", 64), ArtifactID: "arithmetic-vector", LogIndex: nil}}
	checkpoint := engine.SeriesCheckpoint{Height: 1000, Timestamp: 1700001000}
	for i := 0; i < 10; i++ {
		actor := actorA
		if i%2 == 0 {
			actor = actorB
		}
		source.Attribution.Trader = &actor
		source.Position.TransactionHash = fmt.Sprintf("0x%064x", i+1)
		source.Position.Height = fmt.Sprint(100 + i)
		source.SourceFillID = accounting.SourceFillIdentity(source.Position, source.Provenance.DeploymentID)
		if i == 0 {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = engine.ApplySeriesTx(ctx, tx, exact, source, 1, checkpoint); err != nil {
				tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err = tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
		}
		for delivery := 0; delivery < 2; delivery++ {
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err = engine.ApplySeriesTx(ctx, tx, exact, source, 1, checkpoint); err != nil {
				tx.Rollback(ctx)
				t.Fatal(err)
			}
			if err = tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	var raw []byte
	var traders int
	if err = pool.QueryRow(ctx, `SELECT snapshot FROM `+q+`.chart_exact WHERE interval='1m'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var candle engine.SeriesCandle
	if err = json.Unmarshal(raw, &candle); err != nil {
		t.Fatal(err)
	}
	if candle.Trades != 10 || candle.Revision != 10 || candle.UniqueTraders != 2 {
		t.Fatal(candle)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM `+q+`.chart_traders WHERE interval='1m'`).Scan(&traders); err != nil || traders != 2 {
		t.Fatal(traders, err)
	}
	redisOptions, err := redis.ParseURL(internaltest.NewRedisURL(t))
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClient(redisOptions)
	defer cache.Close()
	if err = cache.Set(ctx, "charts:traders:disposable", 2, time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	if err = cache.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	restarted, err := store.NewExact(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	bars, err := engine.ReadSeries(ctx, restarted, source.PoolAddress, "1m", 1700000040, 1700000100, false)
	if err != nil || len(bars) != 1 || bars[0].UniqueTraders != 2 {
		t.Fatal(bars, err)
	}
	if _, err = engine.RecomputeRollups(ctx, restarted, 8); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = exact.AdvanceSeriesTx(ctx, tx, source.PoolAddress, 999, 1700000999, false)
	tx.Rollback(ctx)
	if err != store.ErrSourceWatermarkRegression {
		t.Fatal("regressing watermark accepted", err)
	}
}

func faultFixture(t *testing.T, suffix string) (context.Context, *pgxpool.Pool, *store.ExactStore) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	t.Cleanup(cancel)
	prefix := os.Getenv("DELIVERY_TEST_SCHEMA")
	if !strings.HasPrefix(prefix, "kd_") || len(prefix) > 40 {
		t.Fatal("bounded private PostgreSQL namespace required")
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("DELIVERY_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 25432 || cfg.ConnConfig.Database != "kindle_fixture" {
		t.Fatal("refusing non-fixture PostgreSQL")
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	schema := prefix + "_fault_" + suffix
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, err := pool.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		pool.Close()
	})
	for _, ddl := range []func(string) (string, error){store.ExactSchemaDDL, store.SeriesSchemaDDL, store.RebuildSchemaDDL, delivery.SchemaDDL, delivery.PartitionSchemaDDL} {
		sql, err := ddl(schema)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.NewExact(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	return ctx, pool, st
}
func faultFill(index int) accounting.FillV1 {
	fill := accountingFixture()
	fill.BlockTimestampSeconds = "1700000040"
	fill.Token.Decimals = 6
	fill.TokenAmountRaw = "1000000"
	fill.FeeRaw = "100"
	fill.USDLAmountRaw = strconv.Itoa((index + 1) * 1000000)
	fill.PaidInputRaw = fill.USDLAmountRaw
	fill.Position.Height = strconv.Itoa(100 + index)
	fill.Position.TransactionHash = fmt.Sprintf("0x%064x", index+1)
	fill.Position.TransactionIndex = strconv.Itoa(index)
	fill.SourceFillID = accounting.SourceFillIdentity(fill.Position, fill.Provenance.DeploymentID)
	actor := "0x" + strings.Repeat("a", 40)
	fill.Attribution.Trader = &actor
	fill.Attribution.Quality = "exact_receipt"
	fill.Attribution.Evidence = []accounting.EvidenceRef{{Kind: "receipt", ArtifactSHA256: strings.Repeat("c", 64), ArtifactID: "fault-arithmetic-vector"}}
	return fill
}
func faultApply(ctx context.Context, st *store.ExactStore, fill accounting.FillV1, epoch int64) error {
	tx, err := st.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = engine.ApplySeriesTx(ctx, tx, st, fill, epoch, engine.SeriesCheckpoint{Height: 1000, Timestamp: 1700001000}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func faultState(t *testing.T, ctx context.Context, st *store.ExactStore) string {
	t.Helper()
	parts := []string{}
	for _, table := range []string{"chart_inbox", "chart_exact", "chart_traders", "chart_outbox", "chart_series_clock", "chart_pool_fences"} {
		parts = append(parts, "'"+table+"',(SELECT COALESCE(jsonb_agg(v ORDER BY v::text),'[]'::jsonb) FROM (SELECT to_jsonb(t) AS v FROM "+st.Schema+"."+table+" t) s)")
	}
	var state string
	if err := st.Pool.QueryRow(ctx, "SELECT jsonb_build_object("+strings.Join(parts, ",")+")::text").Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}
func TestFaultPostgresMutationAbortMatrix(t *testing.T) {
	ctx, pool, st := faultFixture(t, "abort")
	first := faultFill(0)
	if err := faultApply(ctx, st, first, 1); err != nil {
		t.Fatal(err)
	}
	baseline := faultState(t, ctx, st)
	if _, err := pool.Exec(ctx, "CREATE FUNCTION "+st.Schema+".fault_abort() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'chart fault boundary' USING ERRCODE='P0001'; END $$"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"chart_inbox", "chart_traders", "chart_exact", "chart_outbox"} {
		for _, timing := range []string{"BEFORE", "AFTER"} {
			t.Run(table+"_"+timing, func(t *testing.T) {
				if _, err := pool.Exec(ctx, "CREATE TRIGGER fault_abort "+timing+" INSERT OR UPDATE ON "+st.Schema+"."+table+" FOR EACH ROW EXECUTE FUNCTION "+st.Schema+".fault_abort()"); err != nil {
					t.Fatal(err)
				}
				fill := faultFill(1)
				actor := "0x" + strings.Repeat("b", 40)
				fill.Attribution.Trader = &actor
				err := faultApply(ctx, st, fill, 2)
				var postgres *pgconn.PgError
				if !errors.As(err, &postgres) || postgres.Code != "P0001" {
					t.Fatalf("expected actual PostgreSQL abort: %v", err)
				}
				if got := faultState(t, ctx, st); got != baseline {
					t.Fatal("abort changed persisted totals, inbox, trader, fence, clock or visible snapshot", got)
				}
				if _, err = pool.Exec(ctx, "DROP TRIGGER fault_abort ON "+st.Schema+"."+table); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
	fill := faultFill(1)
	if err := faultApply(ctx, st, fill, 2); err != nil {
		t.Fatal(err)
	}
	after := faultState(t, ctx, st)
	if err := faultApply(ctx, st, fill, 2); err != nil {
		t.Fatal(err)
	}
	if got := faultState(t, ctx, st); got != after {
		t.Fatal("redelivery after abort duplicated state")
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT snapshot FROM "+st.Schema+".chart_exact WHERE interval='1m'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var bar engine.SeriesCandle
	if err := json.Unmarshal(raw, &bar); err != nil {
		t.Fatal(err)
	}
	if bar.Trades != 2 || bar.Revision != 2 || bar.UniqueTraders != 1 || bar.VolumeUsdlRaw != "3000000" {
		t.Fatal(bar)
	}
}
func TestFaultSimultaneousOwnerHandoff(t *testing.T) {
	ctx, pool, st := faultFixture(t, "fence")
	if err := faultApply(ctx, st, faultFill(0), 1); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE "+st.Schema+".chart_pool_fences SET epoch=2 WHERE pool=$1", faultFill(0).PoolAddress); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, epoch := range []int64{1, 2} {
		wg.Add(1)
		go func(epoch int64) { defer wg.Done(); <-ready; errs <- faultApply(ctx, st, faultFill(1), epoch) }(epoch)
	}
	close(ready)
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(errs)
	stale, committed := 0, 0
	for err := range errs {
		if errors.Is(err, store.ErrStaleOwner) {
			stale++
		} else if err == nil {
			committed++
		} else {
			t.Fatal(err)
		}
	}
	if stale != 1 || committed != 1 {
		t.Fatal(stale, committed)
	}
	baseline := faultState(t, ctx, st)
	if err = faultApply(ctx, st, faultFill(2), 1); !errors.Is(err, store.ErrStaleOwner) {
		t.Fatal("stale owner wrote after handoff", err)
	}
	if got := faultState(t, ctx, st); got != baseline {
		t.Fatal("stale owner changed state")
	}
	parts, err := delivery.NewPartitionStore(pool, st.Schema)
	if err != nil {
		t.Fatal(err)
	}
	key := delivery.ConsumerKey{ConsumerID: "fault", ProjectionGeneration: "live", PartitionKind: "market", PartitionKey: "125:" + faultFill(0).PoolAddress}
	lease, err := parts.Acquire(ctx, key, "replica_a", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = parts.Acquire(ctx, key, "replica_b", time.Second); !errors.Is(err, delivery.ErrLease) {
		t.Fatal("overlap accepted", err)
	}
	time.Sleep(1100 * time.Millisecond)
	replacement, err := parts.Acquire(ctx, key, "replica_b", time.Second)
	if err != nil || replacement.Epoch <= lease.Epoch {
		t.Fatal(replacement, err)
	}
}
func TestFaultDelayedFillPreservesImmutableSource(t *testing.T) {
	ctx, pool, st := faultFixture(t, "late")
	later := faultFill(2)
	if err := faultApply(ctx, st, later, 1); err != nil {
		t.Fatal(err)
	}
	var immutableBefore string
	if err := pool.QueryRow(ctx, "SELECT fill::text FROM "+st.Schema+".chart_inbox").Scan(&immutableBefore); err != nil {
		t.Fatal(err)
	}
	if err := faultApply(ctx, st, faultFill(0), 1); err != nil {
		t.Fatal(err)
	}
	var immutableAfter string
	if err := pool.QueryRow(ctx, "SELECT fill::text FROM "+st.Schema+".chart_inbox WHERE tx_hash=$1", later.Position.TransactionHash).Scan(&immutableAfter); err != nil {
		t.Fatal(err)
	}
	if immutableBefore != immutableAfter {
		t.Fatal("late contribution rewrote immutable source")
	}
	var raw []byte
	if err := pool.QueryRow(ctx, "SELECT snapshot FROM "+st.Schema+".chart_exact WHERE interval='1m'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var bar engine.SeriesCandle
	if err := json.Unmarshal(raw, &bar); err != nil {
		t.Fatal(err)
	}
	if bar.Revision != 2 || bar.Open.Numerator != "1" || bar.Close.Numerator != "3" || bar.OpenSource.Position != faultFill(0).Position || bar.CloseSource.Position != later.Position || bar.CorrectionReason != "delayed_ingestion" {
		t.Fatal(bar)
	}
	var revisions int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+st.Schema+".chart_outbox WHERE interval='1m' AND payload->>'correctionReason'='delayed_ingestion'").Scan(&revisions); err != nil || revisions != 2 {
		t.Fatal(revisions, err)
	}
}

type faultBroker struct {
	t                      *testing.T
	url, directory, binary string
	command                *exec.Cmd
}

func newFaultBroker(t *testing.T) *faultBroker {
	t.Helper()
	binary, err := exec.LookPath("nats-server")
	if err != nil {
		t.Fatal("actual local nats-server required", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	b := &faultBroker{t: t, url: fmt.Sprintf("nats://127.0.0.1:%d", port), directory: t.TempDir(), binary: binary}
	t.Cleanup(b.stop)
	return b
}
func (b *faultBroker) start() *nats.Conn {
	b.t.Helper()
	u, err := url.Parse(b.url)
	if err != nil {
		b.t.Fatal(err)
	}
	log, err := os.OpenFile(filepath.Join(b.directory, "nats.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		b.t.Fatal(err)
	}
	b.command = exec.Command(b.binary, "-js", "-sd", filepath.Join(b.directory, "data"), "-a", "127.0.0.1", "-p", u.Port())
	b.command.Stdout = log
	b.command.Stderr = log
	if err = b.command.Start(); err != nil {
		log.Close()
		b.t.Fatal(err)
	}
	log.Close()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		nc, err := nats.Connect(b.url, nats.NoReconnect(), nats.Timeout(100*time.Millisecond))
		if err == nil {
			b.t.Cleanup(nc.Close)
			return nc
		}
		time.Sleep(25 * time.Millisecond)
	}
	b.t.Fatal("actual JetStream server did not become available")
	return nil
}
func (b *faultBroker) stop() {
	if b.command == nil {
		return
	}
	command := b.command
	b.command = nil
	if err := command.Process.Signal(syscall.SIGINT); err != nil && !errors.Is(err, os.ErrProcessDone) {
		b.t.Error(err)
	}
	if err := command.Wait(); err != nil {
		b.t.Error(err)
	}
}
func faultProvision(t *testing.T, ctx context.Context, nc *nats.Conn, subject, durable string) (*natsdriver.Publisher, *natsdriver.Consumer) {
	t.Helper()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{Name: "CHART_FAULTS", Subjects: []string{"candle.updated.v2", accounting.MarketSubject}, Storage: jetstream.FileStorage, Retention: jetstream.LimitsPolicy, MaxAge: 72 * time.Hour, Duplicates: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	cfg := natsdriver.ConsumerConfig{Stream: "CHART_FAULTS", Durable: durable, Subject: subject, Batch: 64, MaxAckPending: 64, MaxWaiting: 1, MaxDeliver: 10, FetchWait: 200 * time.Millisecond, AckWait: time.Second}
	brokerCfg, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stream.CreateOrUpdateConsumer(ctx, brokerCfg); err != nil {
		t.Fatal(err)
	}
	publisher, err := natsdriver.NewPublisher(nc, cfg.Stream)
	if err != nil {
		t.Fatal(err)
	}
	consumer, err := natsdriver.NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(consumer.Close)
	return publisher, consumer
}
func faultFetch(t *testing.T, ctx context.Context, c *natsdriver.Consumer) []*natsdriver.Message {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := c.Fetch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(messages) > 0 {
			return messages
		}
	}
	t.Fatal("real broker delivery unavailable")
	return nil
}
func TestFaultBrokerTenMinuteOutageAndPostPublishCrash(t *testing.T) {
	ctx, pool, st := faultFixture(t, "broker")
	broker := newFaultBroker(t)
	nc := broker.start()
	publisher, _ := faultProvision(t, ctx, nc, "candle.updated.v2", "candles")
	broker.stop()
	nc.Close()
	outageStart := time.Now()
	for i := 0; i < 3; i++ {
		if err := faultApply(ctx, st, faultFill(i), 1); err != nil {
			t.Fatal(err)
		}
	}
	baseline := faultState(t, ctx, st)
	if _, err := st.PublishNextCommitted(ctx, publisher, "candle.updated.v2"); err == nil {
		t.Fatal("broker outage reported publication")
	}
	if got := faultState(t, ctx, st); got != baseline {
		t.Fatal("broker outage changed committed chart state")
	}
	timer := time.NewTimer(time.Until(outageStart.Add(10 * time.Minute)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	if time.Since(outageStart) < 10*time.Minute {
		t.Fatal("ten minute broker outage not measured")
	}
	nc = broker.start()
	publisher, reader := faultProvision(t, ctx, nc, "candle.updated.v2", "candles")
	if _, err := pool.Exec(ctx, "CREATE FUNCTION "+st.Schema+".reject_publication_mark() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'post-publish interruption' USING ERRCODE='P0001'; END $$; CREATE TRIGGER reject_publication_mark BEFORE UPDATE ON "+st.Schema+".chart_outbox FOR EACH ROW EXECUTE FUNCTION "+st.Schema+".reject_publication_mark()"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PublishNextCommitted(ctx, publisher, "candle.updated.v2"); err == nil {
		t.Fatal("post publish SQL abort not injected")
	}
	initial := faultFetch(t, ctx, reader)
	if len(initial) != 1 {
		t.Fatal("interrupted relay published unexpected snapshots", len(initial))
	}
	first := initial[0]
	if err := first.Ack(ctx); err != nil {
		t.Fatal(err)
	}
	if got := faultState(t, ctx, st); got != baseline {
		t.Fatal("post publish abort changed durable revision or mark")
	}
	if _, err := pool.Exec(ctx, "DROP TRIGGER reject_publication_mark ON "+st.Schema+".chart_outbox"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	restarted, err := store.NewExact(pool, st.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for {
		published, err := restarted.PublishNextCommitted(ctx, publisher, "candle.updated.v2")
		if err != nil {
			t.Fatal(err)
		}
		if !published {
			break
		}
	}
	rows, err := pool.Query(ctx, "SELECT payload FROM "+st.Schema+".chart_outbox ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]bool{}
	for rows.Next() {
		var payload []byte
		if err = rows.Scan(&payload); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		expected[delivery.Hash(payload)] = true
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(expected) != 3*len(engine.ExactIntervals) {
		t.Fatal("durable snapshot revisions missing", len(expected))
	}
	actual := map[string]bool{}
	duplicateSeen := false
	for len(actual) < len(expected) {
		for _, message := range faultFetch(t, ctx, reader) {
			digest := delivery.Hash(message.Wire)
			if !expected[digest] {
				t.Fatal("broker phantom snapshot", string(message.Wire))
			}
			actual[digest] = true
			if message.EventID == first.EventID {
				duplicateSeen = true
				if string(message.Wire) != string(first.Wire) {
					t.Fatal("same revision mutated after relay restart")
				}
			}
			if err = message.Ack(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !duplicateSeen {
		t.Fatal("post publish crash replay not observed outside broker dedup window")
	}
	var unpublished int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+st.Schema+".chart_outbox WHERE published_at IS NULL").Scan(&unpublished); err != nil || unpublished != 0 {
		t.Fatal(unpublished, err)
	}
}
func TestFaultCommitBeforeAckThenRealBrokerRedelivery(t *testing.T) {
	ctx, _, st := faultFixture(t, "ack")
	broker := newFaultBroker(t)
	nc := broker.start()
	publisher, reader := faultProvision(t, ctx, nc, accounting.MarketSubject, "fills")
	fill := faultFill(0)
	wire, err := json.Marshal(fill)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.Publish(ctx, accounting.MarketSubject, fill.SourceFillID, wire); err != nil {
		t.Fatal(err)
	}
	messages := faultFetch(t, ctx, reader)
	if len(messages) != 1 {
		t.Fatal(len(messages))
	}
	decoded, err := accounting.DecodeFillV1(messages[0].Wire)
	if err != nil {
		t.Fatal(err)
	}
	if err = faultApply(ctx, st, decoded, 1); err != nil {
		t.Fatal(err)
	}
	baseline := faultState(t, ctx, st)
	reader.Close()
	nc.Close()
	nc, err = nats.Connect(broker.url, nats.NoReconnect())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	_, reader = faultProvision(t, ctx, nc, accounting.MarketSubject, "fills")
	redelivery := faultFetch(t, ctx, reader)
	if len(redelivery) != 1 || redelivery[0].Deliveries < 2 || redelivery[0].EventID != messages[0].EventID {
		t.Fatal("committed unacknowledged fill not redelivered")
	}
	decoded, err = accounting.DecodeFillV1(redelivery[0].Wire)
	if err != nil {
		t.Fatal(err)
	}
	if err = faultApply(ctx, st, decoded, 1); err != nil {
		t.Fatal(err)
	}
	if got := faultState(t, ctx, st); got != baseline {
		t.Fatal("committed redelivery incremented monetary or revision state")
	}
	if err = redelivery[0].Ack(ctx); err != nil {
		t.Fatal(err)
	}
	if err = st.RecordAttempt(ctx, "fault", "live", fill.SourceFillID, delivery.Hash(wire), int64(messages[0].Sequence), "committed", true); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	publisher, err = natsdriver.NewPublisher(nc, "CHART_FAULTS")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = publisher.Publish(ctx, accounting.MarketSubject, fill.SourceFillID, wire); err != nil {
		t.Fatal(err)
	}
	republished := faultFetch(t, ctx, reader)
	if len(republished) != 1 || republished[0].Sequence == messages[0].Sequence {
		t.Fatal("real broker did not replay outside dedup window")
	}
	if err = st.RecordAttempt(ctx, "fault", "live", fill.SourceFillID, delivery.Hash(wire), int64(republished[0].Sequence), "received", false); err != nil {
		t.Fatal("identical canonical publication at new broker offset quarantined", err)
	}
	if err = faultApply(ctx, st, fill, 1); err != nil {
		t.Fatal(err)
	}
	if got := faultState(t, ctx, st); got != baseline {
		t.Fatal("new broker offset duplicated committed economics")
	}
	if err = republished[0].Ack(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestFaultPoisonQuarantineAndAuditedReprocessing(t *testing.T) {
	ctx, pool, st := faultFixture(t, "poison")
	broker := newFaultBroker(t)
	nc := broker.start()
	publisher, reader := faultProvision(t, ctx, nc, accounting.MarketSubject, "poison")
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{"market": true}}
	deliveries, err := delivery.NewStore(pool, st.Schema, policy)
	if err != nil {
		t.Fatal(err)
	}
	partitions, err := delivery.NewPartitionStore(pool, st.Schema)
	if err != nil {
		t.Fatal(err)
	}
	rpcURL := os.Getenv("L10_TEST_RPC_URL")
	u, err := url.Parse(rpcURL)
	if err != nil || u.Hostname() != "127.0.0.1" {
		t.Fatal("real private RPC fixture required")
	}
	rpc, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	defer rpc.Close()
	cfg := SourceVerifiedAccountingConfig{Pool: pool, Deliveries: deliveries, Partitions: partitions, Exact: st, ConsumerID: "fault", Generation: "live", ProjectionVersion: "market-v1", Owner: "fault-owner", SourceStream: "canonical", SourcePartitionSchema: st.Schema, RPC: rpc, MaxReplayBlocks: 4096, MaxReplayFills: 10000, SeriesV2: true, StreamWriter: &delivery.StreamWriter{Schema: "api_stream"}}
	consumer, err := NewSourceVerifiedAccountingConsumer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	first := faultFill(0)
	if err = faultApply(ctx, st, first, 1); err != nil {
		t.Fatal(err)
	}
	for i, kind := range []string{"negative_uint256", "overflow_uint256", "missing_timestamp", "unknown_chain"} {
		t.Run(kind, func(t *testing.T) {
			fill := faultFill(i + 1)
			digest, err := accounting.FillDigest(fill)
			if err != nil {
				t.Fatal(err)
			}
			position := accounting.PartitionPosition{ProjectionVersion: "market-v1", PartitionKind: "market", PartitionKey: "125:" + fill.PoolAddress, Sequence: "1", SourceEventID: fill.SourceFillID, SourceOrder: fill.Position, PayloadDigest: digest, EffectIdentity: accounting.EffectIdentity(fill.SourceFillID, "market", "market-v1", "", "market", "125:"+fill.PoolAddress)}
			publication, err := accounting.NewMarketPublication(fill, []accounting.PartitionPosition{position})
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "negative_uint256":
				publication.Fill.TokenAmountRaw = "-1"
			case "overflow_uint256":
				publication.Fill.TokenAmountRaw = "115792089237316195423570985008687907853269984665640564039457584007913129639936"
			case "missing_timestamp":
				publication.Fill.BlockTimestampSeconds = ""
			case "unknown_chain":
				publication.Fill.Position.ChainID = "1"
			}
			payload, err := json.Marshal(publication)
			if err != nil {
				t.Fatal(err)
			}
			envelope := delivery.Envelope{EventID: publication.PublicationID, EventType: "market", SchemaVersion: 1, ChainID: 125, SourceOrder: delivery.SourceOrder{Height: fill.Position.Height, TransactionIndex: fill.Position.TransactionIndex, LogIndex: fill.Position.LogIndex}, EmittedAt: time.Now().UTC(), TraceID: "fault_poison", PayloadHash: delivery.Hash(payload), Payload: payload}
			wire, err := envelope.Encode(policy, accounting.MarketSubject)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = publisher.Publish(ctx, accounting.MarketSubject, envelope.EventID, wire); err != nil {
				t.Fatal(err)
			}
			messages := faultFetch(t, ctx, reader)
			message := messages[0]
			if i == 0 {
				if _, err = pool.Exec(ctx, "CREATE FUNCTION "+st.Schema+".reject_attempt() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'transient chart database abort' USING ERRCODE='40001'; END $$; CREATE TRIGGER reject_attempt BEFORE INSERT ON "+st.Schema+".chart_delivery_attempts FOR EACH ROW EXECUTE FUNCTION "+st.Schema+".reject_attempt()"); err != nil {
					t.Fatal(err)
				}
				err = consumer.Consume(ctx, message)
				var aborted *pgconn.PgError
				if !errors.As(err, &aborted) || aborted.Code != "40001" {
					t.Fatal("transient database abort not retained as retryable", err)
				}
				var quarantined int
				if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+st.Schema+".delivery_ordered_quarantine").Scan(&quarantined); err != nil || quarantined != 0 {
					t.Fatal("transient database outage quarantined source", quarantined, err)
				}
				if _, err = pool.Exec(ctx, "DROP TRIGGER reject_attempt ON "+st.Schema+".chart_delivery_attempts"); err != nil {
					t.Fatal(err)
				}
			}
			if err = consumer.Consume(ctx, message); err == nil {
				t.Fatal("permanent poison accepted")
			}
			var original []byte
			var reason string
			if err = pool.QueryRow(ctx, "SELECT envelope,failure_class FROM "+st.Schema+".delivery_ordered_quarantine WHERE envelope_hash=$1", delivery.Hash(wire)).Scan(&original, &reason); err != nil {
				t.Fatal(err)
			}
			if string(original) != string(wire) || reason != string(delivery.InnerSchema) {
				t.Fatal("quarantine lost original poison or durable reason", reason)
			}
			var inbox int
			if err = pool.QueryRow(ctx, "SELECT count(*) FROM "+st.Schema+".chart_inbox").Scan(&inbox); err != nil || inbox != 1 {
				t.Fatal("poison mutated immutable source", inbox, err)
			}
			bars, err := engine.ReadSeries(ctx, st, first.PoolAddress, "1m", 1700000040, 1700000100, false)
			if err != nil || len(bars) != 1 || bars[0].CompletenessStatus != "backfill_incomplete" {
				t.Fatal("poison certified false completeness", bars, err)
			}
			if err = message.Ack(ctx); err != nil {
				t.Fatal(err)
			}
			if err = st.AuthorizeReprocess(ctx, "fault", "reprocess_invalid", publication.PublicationID, strings.Repeat("0", 64), delivery.Hash(wire), "decoder-2", "operator"); !errors.Is(err, store.ErrIntegrity) {
				t.Fatal("unrecorded reprocess authorized", err)
			}
			if err = st.AuthorizeReprocess(ctx, "fault", "reprocess_same", publication.PublicationID, delivery.Hash(wire), delivery.Hash(wire), "accounting-fill-v1", "operator"); !errors.Is(err, store.ErrIntegrity) {
				t.Fatal("unchanged decoder reprocess authorized", err)
			}
			if err = st.AuthorizeReprocess(ctx, "fault", "reprocess_changed", publication.PublicationID, delivery.Hash(wire), delivery.Hash(wire), "decoder-2", "operator"); err == nil || err.Error() != "registered_decoder_change_authority_required" {
				t.Fatal("missing decoder authority did not fail closed", err)
			}
			correctedPayload, err := accounting.NewMarketPublication(fill, []accounting.PartitionPosition{position})
			if err != nil {
				t.Fatal(err)
			}
			corrected, err := json.Marshal(correctedPayload)
			if err != nil {
				t.Fatal(err)
			}
			envelope.Payload = corrected
			envelope.PayloadHash = delivery.Hash(corrected)
			correctedWire, err := envelope.Encode(policy, accounting.MarketSubject)
			if err != nil {
				t.Fatal(err)
			}
			if err = st.AuthorizeReprocess(ctx, "fault", "reprocess_valid", publication.PublicationID, delivery.Hash(wire), delivery.Hash(correctedWire), "accounting-fill-v1", "operator"); err != nil {
				t.Fatal(err)
			}
			if err = st.CheckReprocess(ctx, "fault", "reprocess_valid", publication.PublicationID, delivery.Hash(correctedWire)); err != nil {
				t.Fatal(err)
			}
			if err = st.CheckReprocess(ctx, "fault", "reprocess_valid", publication.PublicationID, delivery.Hash(wire)); !errors.Is(err, store.ErrIntegrity) {
				t.Fatal("audit authorized wrong corrected bytes", err)
			}
		})
	}
	if err = st.RecordAttempt(ctx, "fault", "live", "committed", strings.Repeat("a", 64), 100, "received", false); err != nil {
		t.Fatal(err)
	}
	if err = st.RecordAttemptResult(ctx, "fault", "live", "committed", "committed", true); err != nil {
		t.Fatal(err)
	}
	if err = st.RecordAttemptResult(ctx, "fault", "live", "committed", "ack_failed", false); err != nil {
		t.Fatal(err)
	}
	var resolved bool
	if err = pool.QueryRow(ctx, "SELECT resolved FROM "+st.Schema+".chart_delivery_attempts WHERE publication_id='committed'").Scan(&resolved); err != nil || !resolved {
		t.Fatal("ack failure regressed committed attempt", resolved, err)
	}
}

func TestFaultCanonicalConflictFreezesBothPools(t *testing.T) {
	ctx, pool, st := faultFixture(t, "conflict")
	first := faultFill(0)
	if err := faultApply(ctx, st, first, 1); err != nil {
		t.Fatal(err)
	}
	original := faultState(t, ctx, st)
	conflict := first
	conflict.PoolAddress = "0x" + strings.Repeat("6", 40)
	conflict.USDLAmountRaw = "2000000"
	conflict.PaidInputRaw = conflict.USDLAmountRaw
	err := faultApply(ctx, st, conflict, 1)
	var integrity *delivery.IntegrityFailure
	if !errors.As(err, &integrity) || integrity.Class != delivery.IdentityConflict || len(integrity.AffectedPools) != 2 {
		t.Fatal("canonical conflict did not name both series", err)
	}
	if got := faultState(t, ctx, st); got != original {
		t.Fatal("conflicting canonical fill changed source or monetary state")
	}
	wire, err := json.Marshal(conflict)
	if err != nil {
		t.Fatal(err)
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{"market": true}}
	deliveries, err := delivery.NewStore(pool, st.Schema, policy)
	if err != nil {
		t.Fatal(err)
	}
	input := delivery.QuarantineInput{ConsumerID: "fault", Generation: "live", Subject: accounting.MarketSubject, PublicationID: conflict.SourceFillID, Wire: wire, WireSHA256: accounting.Digest(delivery.Hash(wire)), Class: delivery.IdentityConflict, AffectedPools: integrity.AffectedPools, EvidenceDigest: integrity.EvidenceDigest}
	if err = deliveries.QuarantineAfterRollback(ctx, input, func(ctx context.Context, tx pgx.Tx, pools []string) error {
		return st.FreezePoolsTx(ctx, tx, pools, input.EvidenceDigest)
	}); err != nil {
		t.Fatal(err)
	}
	var frozen, incidents int
	if err = pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+st.Schema+".chart_pool_fences WHERE frozen),(SELECT count(*) FROM "+st.Schema+".delivery_ordered_quarantine WHERE failure_class='identity_conflict')").Scan(&frozen, &incidents); err != nil || frozen != 2 || incidents != 1 {
		t.Fatal("conflict did not persist freeze and integrity evidence", frozen, incidents, err)
	}
	if _, err = engine.ReadSeries(ctx, st, first.PoolAddress, "1m", 1700000040, 1700000100, false); !errors.Is(err, store.ErrIntegrity) {
		t.Fatal("frozen series still served as healthy", err)
	}
	if err = faultApply(ctx, st, faultFill(1), 2); !errors.Is(err, store.ErrIntegrity) {
		t.Fatal("frozen series accepted mutation", err)
	}
}

func TestFaultUnavailableActorAndRetentionGuard(t *testing.T) {
	ctx, pool, st := faultFixture(t, "actor")
	fill := faultFill(0)
	fill.Attribution.Trader = nil
	fill.Attribution.Quality = "unavailable"
	fill.Attribution.Evidence = []accounting.EvidenceRef{}
	if err := faultApply(ctx, st, fill, 1); err != nil {
		t.Fatal(err)
	}
	var snapshot []byte
	if err := pool.QueryRow(ctx, "SELECT snapshot FROM "+st.Schema+".chart_exact WHERE interval='1m'").Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var bar engine.SeriesCandle
	if err := json.Unmarshal(snapshot, &bar); err != nil {
		t.Fatal(err)
	}
	if bar.UnknownActorTradeCount != 1 || bar.UniqueTraders != 0 || bar.VolumeUsdlRaw != "1000000" || bar.Trades != 1 {
		t.Fatal("unavailable actor invented or discarded economic contribution", bar)
	}
	if deleted, err := st.DeleteRetainedRange(ctx, fill.PoolAddress, "1m", 1700000040, 1700000100); err == nil || deleted != 0 {
		t.Fatal("retention dropped recoverability proof", deleted, err)
	}
	var count, incidents int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM "+st.Schema+".chart_exact WHERE interval='1m'),(SELECT count(*) FROM "+st.Schema+".chart_recoverability_incidents)").Scan(&count, &incidents); err != nil || count != 1 || incidents != 1 {
		t.Fatal("retention failed without durable recoverability incident", count, incidents, err)
	}
}
