package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store/canonicaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/protocol/bindings"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/ethereum/go-ethereum/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// These are synthetic ABI-encoded arithmetic/event fixtures, not chain receipts.
func fixtureBatch(t *testing.T) store.CanonicalBatch {
	t.Helper()
	a, err := bindings.EventEmitterMetaData.GetAbi()
	if err != nil {
		t.Fatal(err)
	}
	poolID := common.HexToHash("0x" + strings.Repeat("1", 64))
	addr := common.HexToAddress("0x" + strings.Repeat("2", 40))
	token := common.HexToAddress("0x" + strings.Repeat("3", 40))
	owner := common.HexToAddress("0x" + strings.Repeat("4", 40))
	hash := "0x" + strings.Repeat("5", 64)
	encode := func(kind string, idx uint32, topics []common.Hash, values ...any) store.RawRecord {
		ev := a.Events[kind]
		data, err := ev.Inputs.NonIndexed().Pack(values...)
		if err != nil {
			t.Fatal(err)
		}
		all := []string{strings.ToLower(ev.ID.Hex())}
		for _, h := range topics {
			all = append(all, strings.ToLower(h.Hex()))
		}
		return store.RawRecord{TransactionHash: fmt.Sprintf("0x%064x", 100+idx/2), BlockHash: hash, Emitter: strings.ToLower(addr.Hex()), Height: 101, TransactionIndex: idx / 2, LogIndex: idx, Topics: all, Data: data}
	}
	market := encode("MarketCreated", 0, []common.Hash{poolID, common.BytesToHash(token.Bytes()), common.BytesToHash(owner.Bytes())}, addr, owner, big.NewInt(123456), big.NewInt(101))
	swaps := make([]store.RawRecord, 3)
	for i := range swaps {
		idx := uint32(i + 1)
		amount := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 200), big.NewInt(int64(idx)))
		swaps[i] = encode("Swap", idx, []common.Hash{poolID, common.BytesToHash(owner.Bytes())}, true, amount, new(big.Int).Add(amount, big.NewInt(1)), big.NewInt(7), big.NewInt(9), big.NewInt(123456), big.NewInt(101))
	}
	return store.CanonicalBatch{ChainID: 125, StreamID: "live", Owner: "A", BatchID: "batch101", PreviousHeight: 100, Blocks: []store.BlockCompleteness{{Height: 101, Hash: hash, RawCount: 4, FetchComplete: true, RequiredDecodeComplete: true}}, Records: []store.RawRecord{swaps[2], swaps[0], market, swaps[1]}}
}
func prepare(t *testing.T, pool *pgxpool.Pool) store.CanonicalBatch {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.indexer_ingest_watermarks VALUES(125,'live',100,100)`); err != nil {
		t.Fatal(err)
	}
	b := fixtureBatch(t)
	fence, err := store.New(pool).AcquireLease(ctx, 125, "live", "A", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	b.Fence = fence
	return b
}
func processorFor(pool *pgxpool.Pool) *Processor {
	return New(Deps{Store: store.New(pool), Decoder: decode.NewDecoder(), ChainID: 125})
}
func process(ctx context.Context, pool *pgxpool.Pool, b store.CanonicalBatch) error {
	return processorFor(pool).ProcessCanonicalBatch(ctx, b, map[uint64]int64{101: 123456}, nil)
}
func assertRows(t *testing.T, pool *pgxpool.Pool, committed bool) {
	t.Helper()
	ctx := context.Background()
	var raw, pools, swaps, blocks, outbox, commits, height, typed int64
	err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM indexer.indexer_raw_logs),(SELECT count(*) FROM indexer.pools),(SELECT count(*) FROM indexer.swaps),(SELECT count(*) FROM indexer.indexer_block_completeness),(SELECT count(*) FROM indexer.indexer_ingest_outbox),(SELECT count(*) FROM indexer.indexer_ingest_commits),raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks`).Scan(&raw, &pools, &swaps, &blocks, &outbox, &commits, &height, &typed)
	if err != nil {
		t.Fatal(err)
	}
	actual := []int64{raw, pools, swaps, blocks, outbox, commits, height, typed}
	want := []int64{0, 0, 0, 0, 0, 0, 100, 100}
	if committed {
		want = []int64{4, 1, 3, 1, 4, 1, 101, 101}
	}
	if fmt.Sprint(actual) != fmt.Sprint(want) {
		t.Fatalf("durable rows raw/pools/swaps/complete/outbox/audit/rawHeight/typedHeight=%v want=%v", actual, want)
	}
	t.Logf("durable restart rows raw/pools/swaps/complete/outbox/audit/rawHeight/typedHeight=%v", actual)
}
func TestCanonicalProcessorSemanticPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx := context.Background()
	// Explicit empty intents cannot silently suppress projected events.
	missing := b
	missing.Outbox = []store.OutboxIntent{}
	if err := process(ctx, pool, missing); err == nil {
		t.Fatal("explicit missing intents accepted")
	}
	assertRows(t, pool, false)
	if err := process(ctx, pool, b); err != nil {
		t.Fatal(err)
	}
	assertRows(t, pool, true)
	rows, err := pool.Query(ctx, `SELECT s.log_index,s.amount_in,s.pool_address,o.payload FROM indexer.swaps s JOIN indexer.indexer_ingest_outbox o ON s.tx_hash=o.transaction_hash AND s.log_index=o.log_index ORDER BY s.block_number,s.log_index`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var index int
		var amount, poolAddress string
		var payload []byte
		if err = rows.Scan(&index, &amount, &poolAddress, &payload); err != nil {
			t.Fatal(err)
		}
		var event struct {
			Args           map[string]any `json:"args"`
			BlockTimestamp int64          `json:"blockTimestamp"`
		}
		if err = json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		expected := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 200), big.NewInt(int64(index))).String()
		if amount != expected || event.Args["amountIn"] != expected || poolAddress != "0x"+strings.Repeat("2", 40) || event.Args["poolAddress"] != poolAddress || event.BlockTimestamp != 123456 {
			t.Fatalf("typed/outbox exactness index=%d amount=%s payload=%s pool=%s", index, amount, payload, poolAddress)
		}
		count++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("swaps=%d", count)
	}
	rawRows, err := pool.Query(ctx, `SELECT transaction_index,log_index FROM indexer.indexer_raw_logs ORDER BY height,transaction_index,log_index`)
	if err != nil {
		t.Fatal(err)
	}
	defer rawRows.Close()
	order := []string{}
	for rawRows.Next() {
		var tx, index int
		if err = rawRows.Scan(&tx, &index); err != nil {
			t.Fatal(err)
		}
		order = append(order, fmt.Sprintf("%d:%d", tx, index))
	}
	if err = rawRows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(order, ",") != "0:0,0:1,1:2,1:3" {
		t.Fatalf("canonical same-second order=%v", order)
	}
	t.Logf("shuffled arrival same-second canonical order=%v; raw quantities exceed 2^200 and remain exact", order)
}
func TestCanonicalProcessorIntentMismatchPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx := context.Background()
	// Capture the real projected envelopes from an accepted batch, then retry in
	// a fresh fixture with exactly one altered raw quantity assertion.
	if err := process(ctx, pool, b); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT transaction_hash,log_index,event_kind,payload FROM indexer.indexer_ingest_outbox ORDER BY log_index`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var intent store.OutboxIntent
		if err = rows.Scan(&intent.TransactionHash, &intent.LogIndex, &intent.Kind, &intent.Payload); err != nil {
			t.Fatal(err)
		}
		b.Outbox = append(b.Outbox, intent)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err = json.Unmarshal(b.Outbox[1].Payload, &event); err != nil {
		t.Fatal(err)
	}
	event["args"].(map[string]any)["amountIn"] = "1"
	b.Outbox[1].Payload, err = json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	_, second := canonicaltest.New(t)
	fresh := prepare(t, second)
	fresh.Outbox = b.Outbox
	if err = process(ctx, second, fresh); err == nil {
		t.Fatal("mismatched quantity assertion accepted")
	}
	assertRows(t, second, false)
}

func TestCanonicalProcessorCrashRestartPostgres(t *testing.T) {
	// PostgreSQL backend termination is a real transaction crash. Triggers are
	// installed only in each exact private fixture DB, never in product schemas.
	boundaries := []struct {
		name, table, operation, condition string
		deferred                          bool
	}{
		{"raw0", "indexer_raw_logs", "INSERT", "NEW.log_index=0", false}, {"raw1", "indexer_raw_logs", "INSERT", "NEW.log_index=1", false}, {"raw2", "indexer_raw_logs", "INSERT", "NEW.log_index=2", false}, {"raw3", "indexer_raw_logs", "INSERT", "NEW.log_index=3", false},
		{"typed_pool", "pools", "INSERT", "true", false}, {"typed_swap1", "swaps", "INSERT", "NEW.log_index=1", false}, {"typed_swap2", "swaps", "INSERT", "NEW.log_index=2", false}, {"typed_swap3", "swaps", "INSERT", "NEW.log_index=3", false},
		{"completeness", "indexer_block_completeness", "INSERT", "true", false},
		{"outbox0", "indexer_ingest_outbox", "INSERT", "NEW.log_index=0", false}, {"outbox1", "indexer_ingest_outbox", "INSERT", "NEW.log_index=1", false}, {"outbox2", "indexer_ingest_outbox", "INSERT", "NEW.log_index=2", false}, {"outbox3", "indexer_ingest_outbox", "INSERT", "NEW.log_index=3", false},
		{"watermark", "indexer_ingest_watermarks", "UPDATE", "true", false}, {"commit_audit", "indexer_ingest_commits", "INSERT", "true", false}, {"commit_boundary", "indexer_ingest_commits", "INSERT", "true", true},
	}
	for _, boundary := range boundaries {
		t.Run(boundary.name, func(t *testing.T) {
			dsn, pool := canonicaltest.New(t)
			b := prepare(t, pool)
			ctx := context.Background()
			sql := `CREATE FUNCTION indexer.l06_crash() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF ` + boundary.condition + ` THEN PERFORM pg_catalog.pg_terminate_backend(pg_catalog.pg_backend_pid()); END IF; RETURN NEW; END$$;`
			prefix := "CREATE TRIGGER"
			deferClause := ""
			if boundary.deferred {
				prefix = "CREATE CONSTRAINT TRIGGER"
				deferClause = " DEFERRABLE INITIALLY DEFERRED"
			}
			sql += prefix + ` l06_crash AFTER ` + boundary.operation + ` ON indexer.` + boundary.table + deferClause + ` FOR EACH ROW EXECUTE FUNCTION indexer.l06_crash()`
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
			if err := process(ctx, pool, b); err == nil {
				t.Fatal("backend crash was not observed")
			} else {
				t.Logf("actual processor backend terminated at %s: %v", boundary.name, err)
			}
			// Fresh connection pool and fresh Processor simulate restart, not transaction
			// local inspection through a connection which could still see uncommitted rows.
			restarted, err := pgxpool.New(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			assertRows(t, restarted, false)
			if _, err = restarted.Exec(ctx, `DROP TRIGGER l06_crash ON indexer.`+boundary.table+`; DROP FUNCTION indexer.l06_crash()`); err != nil {
				t.Fatal(err)
			}
			if err = process(ctx, restarted, b); err != nil {
				t.Fatalf("restart replay: %v", err)
			}
			assertRows(t, restarted, true)
		})
	}
}
func TestCanonicalProcessorConcurrentFencingPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx := context.Background()
	st := store.New(pool)
	run := func(a, z store.CanonicalBatch) (error, error) {
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)
		var ea, ez error
		go func() { defer wg.Done(); <-start; ea = process(ctx, pool, a) }()
		go func() { defer wg.Done(); <-start; ez = process(ctx, pool, z) }()
		close(start)
		wg.Wait()
		return ea, ez
	}
	// Two active processes with the same current fence race a real PostgreSQL
	// row lock and expected checkpoint. Exactly one commits.
	other := b
	other.BatchID = "competing101"
	ea, ez := run(b, other)
	if !((ea == nil && errors.Is(ez, store.ErrWatermark)) || (ez == nil && errors.Is(ea, store.ErrWatermark))) {
		t.Fatalf("same fence commit race A=%v B=%v", ea, ez)
	}
	assertRows(t, pool, true)
	// Reset only this owned DB, then race a paused old owner against its successor.
	if _, err := pool.Exec(ctx, `TRUNCATE indexer.indexer_raw_logs,indexer.pools,indexer.swaps,indexer.indexer_block_completeness,indexer.indexer_ingest_outbox,indexer.indexer_ingest_commits; UPDATE indexer.indexer_ingest_watermarks SET raw_complete_height=100,typed_complete_height=100`); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseLease(ctx, 125, "live", "A", b.Fence); err != nil {
		t.Fatal(err)
	}
	fence, err := st.AcquireLease(ctx, 125, "live", "B", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	successor := b
	successor.Owner = "B"
	successor.Fence = fence
	successor.BatchID = "leaderB101"
	ea, ez = run(b, successor)
	if !errors.Is(ea, store.ErrFenced) || ez != nil {
		t.Fatalf("replacement commit race old=%v new=%v", ea, ez)
	}
	assertRows(t, pool, true)
	t.Logf("concurrent old/new fenced writers: old=%v new=%v token=%d", ea, ez, fence)
}
func TestCanonicalProcessorCancellationPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	if _, err := pool.Exec(context.Background(), `CREATE FUNCTION indexer.l06_pause() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_catalog.pg_sleep(5); RETURN NEW; END$$; CREATE TRIGGER l06_pause AFTER INSERT ON indexer.pools FOR EACH ROW EXECUTE FUNCTION indexer.l06_pause()`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := process(ctx, pool, b); err == nil {
		t.Fatal("canceled processor committed")
	}
	assertRows(t, pool, false)
	if _, err := pool.Exec(context.Background(), `DROP TRIGGER l06_pause ON indexer.pools; DROP FUNCTION indexer.l06_pause()`); err != nil {
		t.Fatal(err)
	}
	if err := process(context.Background(), pool, b); err != nil {
		t.Fatal(err)
	}
	assertRows(t, pool, true)
}

func TestCanonicalLeaseExpiresDuringProjectionPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx := context.Background()
	st := store.New(pool)
	if _, err := pool.Exec(ctx, `UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp()+interval '100 milliseconds' WHERE chain_id=125 AND stream_id='live'`); err != nil {
		t.Fatal(err)
	}
	err := st.CommitProjectedCanonicalTx(ctx, b, func(ctx context.Context, c store.CanonicalTx) ([]store.OutboxIntent, error) {
		_, err := c.Tx.Exec(ctx, `SELECT pg_sleep(0.2)`)
		return nil, err
	})
	if !errors.Is(err, store.ErrFenced) {
		t.Fatalf("lease expiry did not fence active transaction: %v", err)
	}
	assertRows(t, pool, false)
	if err = st.RenewLease(ctx, 125, "live", "A", b.Fence, 30*time.Second); !errors.Is(err, store.ErrFenced) {
		t.Fatalf("expired leader revived: %v", err)
	}
	fence, err := st.AcquireLease(ctx, 125, "live", "B", 30*time.Second)
	if err != nil || fence <= b.Fence {
		t.Fatalf("replacement fence=%d err=%v", fence, err)
	}
	if err = process(ctx, pool, b); !errors.Is(err, store.ErrFenced) {
		t.Fatalf("paused old leader accepted: %v", err)
	}
	b.Owner = "B"
	b.Fence = fence
	if err = process(ctx, pool, b); err != nil {
		t.Fatal(err)
	}
	assertRows(t, pool, true)
}
func TestCanonicalMissingDeliveryPartitionRollsBackPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx := context.Background()
	partitions, err := delivery.NewPartitionStore(pool, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	raw := b.Records[0]
	position := accounting.SourcePosition{ChainID: "125", Height: "101", TransactionIndex: fmt.Sprint(raw.TransactionIndex), LogIndex: fmt.Sprint(raw.LogIndex), TransactionHash: raw.TransactionHash, BlockHash: raw.BlockHash}
	input := delivery.AppendInput{ProjectionVersion: "market_v1", PartitionKind: "market", PartitionKey: "125:" + raw.Emitter, SourceEventID: accounting.HashTuple("raw", raw.TransactionHash, position.LogIndex), EffectIdentity: accounting.HashTuple("effect", raw.TransactionHash, position.LogIndex), PayloadDigest: accounting.Digest(delivery.Hash(raw.Data)), SourceOrder: position}
	err = store.New(pool).CommitProjectedCanonicalTx(ctx, b, func(ctx context.Context, c store.CanonicalTx) ([]store.OutboxIntent, error) {
		_, err := partitions.AppendTx(ctx, c.Tx, input)
		return nil, err
	})
	var pgerr *pgconn.PgError
	if !errors.As(err, &pgerr) || pgerr.Code != "42P01" {
		t.Fatalf("actual missing partition table failure expected: %v", err)
	}
	assertRows(t, pool, false)
}
func TestCanonicalDatabaseOutageAcknowledgesNothingPostgres(t *testing.T) {
	dsn, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	observer, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close(context.Background())
	var name string
	if err = observer.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(name, "kd_2c5416329a7e470a0541ecb6_") {
		t.Fatal("database outage only allowed on exact fixture")
	}
	if _, err = observer.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS false"); err != nil {
		t.Fatal(err)
	}
	defer observer.Exec(context.Background(), "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS true")
	if _, err = observer.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}
	if err = store.New(pool).IngestReady(ctx, 125, "live", "A", b.Fence); err == nil {
		t.Fatal("readiness remained true during database outage")
	}
	if err = process(ctx, pool, b); err == nil {
		t.Fatal("uncommitted event acknowledged during database outage")
	}
	var height, count int64
	if err = observer.QueryRow(ctx, `SELECT raw_complete_height,(SELECT count(*) FROM indexer.indexer_ingest_commits) FROM indexer.indexer_ingest_watermarks`).Scan(&height, &count); err != nil || height != 100 || count != 0 {
		t.Fatalf("outage durable state=%d/%d err=%v", height, count, err)
	}
	if _, err = observer.Exec(ctx, "ALTER DATABASE "+pgx.Identifier{name}.Sanitize()+" ALLOW_CONNECTIONS true"); err != nil {
		t.Fatal(err)
	}
	if err = process(ctx, pool, b); err != nil {
		t.Fatal(err)
	}
	assertRows(t, pool, true)
}

func TestCanonicalPreservesEmittedCallerWithoutInventingBeneficiaryPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	p := processorFor(pool)
	origins := map[string]string{}
	for _, raw := range b.Records {
		origins[raw.TransactionHash] = "0x" + strings.Repeat("9", 40)
	}
	if err := p.ProcessCanonicalBatch(context.Background(), b, map[uint64]int64{101: 123456}, origins); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(context.Background(), `SELECT s.sender,o.payload FROM indexer.swaps s JOIN indexer.indexer_ingest_outbox o ON s.tx_hash=o.transaction_hash AND s.log_index=o.log_index WHERE o.event_kind='Swap'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var caller string
		var payload []byte
		if err = rows.Scan(&caller, &payload); err != nil {
			t.Fatal(err)
		}
		var event struct {
			Args struct {
				Sender string              `json:"sender"`
				Actors decode.ActorContext `json:"actors"`
			} `json:"args"`
		}
		if err = json.Unmarshal(payload, &event); err != nil {
			t.Fatal(err)
		}
		if caller != "0x"+strings.Repeat("4", 40) || event.Args.Sender != caller || event.Args.Actors.EventCaller == nil || *event.Args.Actors.EventCaller != caller || event.Args.Actors.TransactionOrigin != nil || event.Args.Actors.Beneficiary != nil || event.Args.Actors.Recipient != nil || event.Args.Actors.AttributionConfidence != "unknown" {
			t.Fatalf("unverified txFrom replaced emitted caller or invented actor: %s %s", caller, payload)
		}
		count++
	}
	if err = rows.Err(); err != nil || count != 3 {
		t.Fatalf("actor rows=%d err=%v", count, err)
	}
}
