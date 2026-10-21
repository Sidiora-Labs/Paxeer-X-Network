package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
)

const (
	universeStream     = "paxeer-125-canonical"
	universeProjection = "accounting-v2"
	poolA              = "0x00000000000000000000000000000000000000a1"
	poolB              = "0x00000000000000000000000000000000000000b2"
	poolC              = "0x00000000000000000000000000000000000000c3"
	walletOne          = "0x1000000000000000000000000000000000000001"
	walletTwo          = "0x2000000000000000000000000000000000000002"
	usdlAddress        = "0x85fcd13735f4309833a503ee804ea32395851479"
)

// migrationSQL reads a registered production migration verbatim so the test
// schema is the one the fleet bootstrap applies, not a hand-written copy.
func migrationSQL(t *testing.T, id string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", "launchpad", "spec", "production-system", "integration", "migrations", id+".sql"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func productionUniverseDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := os.Getenv(internaltest.PostgresDSNEnv)
	if admin == "" {
		t.Skip(internaltest.PostgresDSNEnv + " not set; this test never starts containers")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, internaltest.NewExternalDatabase(t, admin))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	steps := []string{
		`CREATE SCHEMA indexer; CREATE SCHEMA stats; CREATE SCHEMA ranking; CREATE SCHEMA recovery; CREATE EXTENSION IF NOT EXISTS pgcrypto;
		 CREATE TABLE indexer.delivery_partition_positions(projection_version text,partition_kind text,partition_key text,sequence bigint,position jsonb)`,
		migrationSQL(t, "60000"), migrationSQL(t, "130000"), migrationSQL(t, "130001"), migrationSQL(t, "190001"),
		migrationSQL(t, "190000"), migrationSQL(t, "190002"), migrationSQL(t, "440000"),
		migrationSQL(t, "150002"), migrationSQL(t, "150003"), migrationSQL(t, "190004"), migrationSQL(t, "190003"),
		`INSERT INTO recovery.recovery_control(epoch,region,revision,checkpoint_sha256) VALUES(1,'fixture-loopback',1,repeat('a',64))`,
	}
	for i, step := range steps {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, step); err != nil {
			t.Fatalf("schema step %d: %v", i, err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	return pool
}

type universeFill struct {
	pool, side, wallet     string
	height, txIndex        int64
	timestamp              int64
	usdl, token, paid, fee string
}

func insertFill(t *testing.T, pool *pgxpool.Pool, sequence int64, f universeFill) {
	t.Helper()
	ctx := context.Background()
	txHash := fmt.Sprintf("0x%064x", sequence)
	blockHash := fmt.Sprintf("0x%064x", f.height)
	wallet := f.wallet
	fill := accounting.FillV1{
		Version: accounting.FillVersion, SourceFillID: txHash + ":0",
		Position:      accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(f.height, 10), TransactionIndex: strconv.FormatInt(f.txIndex, 10), LogIndex: "0", TransactionHash: txHash, BlockHash: blockHash},
		ActualEmitter: f.pool, PoolAddress: f.pool,
		Token: accounting.Asset{Address: "0x" + strings.Repeat("7", 40), Decimals: 18}, Settlement: accounting.Asset{Address: usdlAddress, Decimals: 6},
		Side: f.side, TokenAmountRaw: f.token, USDLAmountRaw: f.usdl, PaidInputRaw: f.paid, FeeRaw: f.fee,
		InputDerivationQuality: "legacy_unavailable", FeeDisposition: "distributed_usdl",
		Attribution:           accounting.Attribution{TransactionSender: wallet, Trader: &wallet, Payer: &wallet, Recipient: &wallet, Quality: "exact_receipt", Evidence: []accounting.EvidenceRef{}},
		Execution:             accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}},
		BlockTimestampSeconds: strconv.FormatInt(f.timestamp, 10),
	}
	payload, err := json.Marshal(fill)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%064x", sequence)
	if _, err = pool.Exec(ctx, `INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES(125,$1,0,$2,$3,$4,$5,'[]','\x00')`, txHash, f.height, f.txIndex, blockHash, f.pool); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO indexer.indexer_accounting_fills(source_fill_id,source_fact_digest,chain_id,transaction_hash,log_index,pool_address,payload) VALUES($1,$2,125,$3,0,$4,$5)`, fill.SourceFillID, digest, txHash, f.pool, payload); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO stats.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)`, uuid.NewString(), fill.SourceFillID, digest, universeProjection, f.pool, f.timestamp-f.timestamp%60, `{}`, payload, sequence); err != nil {
		t.Fatal(err)
	}
}

func insertCreation(t *testing.T, pool *pgxpool.Pool, address string, height, timestamp int64) {
	t.Helper()
	token := "0x" + strings.Repeat(address[len(address)-1:], 40)
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$3,$3,1,$4,$5,$6,0,0,$7,'{}','{}')`, token, address, walletOne, height, fmt.Sprintf("0x%064x", height), fmt.Sprintf("0x%064x", 1000+height), timestamp); err != nil {
		t.Fatal(err)
	}
}

type fixtureVisibility struct {
	server *httptest.Server
}

func newFixtureVisibility(t *testing.T, observed time.Time, pools map[string]bool) *HTTPRankingVisibility {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(RankingVisibility{Revision: strings.Repeat("e", 64), ObservedAt: observed, Complete: true, Pools: pools})
	}))
	t.Cleanup(server.Close)
	reader, err := NewHTTPRankingVisibility(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// TestProductionUniverseCompletesAndPublishes drives the production path end to
// end on the registered migrations: canonical creations and committed fills are
// frozen into a complete universe, features are derived from fills, every
// category completes and publishes, and the stored pages read back.
func TestProductionUniverseCompletesAndPublishes(t *testing.T) {
	pool := productionUniverseDatabase(t)
	ctx := context.Background()
	cutoff := time.Now().UTC().Truncate(time.Second)
	now := cutoff.Unix()
	insertCreation(t, pool, poolA, 100, now-2*86400)
	insertCreation(t, pool, poolC, 120, now-3*3600)
	insertCreation(t, pool, poolB, 150, now-1000)
	e18 := strings.Repeat("0", 18)
	// Pool A: an out-of-window buy (price 1.0), then an in-window buy (1.25)
	// and a partial sell (1.3) by a second wallet.
	insertFill(t, pool, 1, universeFill{pool: poolA, side: "buy", wallet: walletOne, height: 101, timestamp: now - 90000, usdl: "1000000000", token: "1000" + e18, paid: "1000000000", fee: "10000000"})
	insertFill(t, pool, 2, universeFill{pool: poolA, side: "buy", wallet: walletTwo, height: 160, timestamp: now - 600, usdl: "500000000", token: "400" + e18, paid: "500000000", fee: "5000000"})
	insertFill(t, pool, 3, universeFill{pool: poolA, side: "sell", wallet: walletTwo, height: 170, txIndex: 1, timestamp: now - 120, usdl: "130000000", token: "100" + e18, paid: "100" + e18, fee: "0"})
	// Pool C trades but is hidden by moderation.
	insertFill(t, pool, 4, universeFill{pool: poolC, side: "buy", wallet: walletOne, height: 180, timestamp: now - 60, usdl: "200000000", token: "100" + e18, paid: "200000000", fee: "2000000"})
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES(125,$1,'indexer-1',1,clock_timestamp()+interval '1 minute')`, universeStream)
	exec(`INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,200,200)`, universeStream)
	exec(`INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete,completed_at) VALUES(125,$1,200,$2,0,true,true,$3)`, universeStream, fmt.Sprintf("0x%064x", 200), cutoff.Add(-2*time.Second))

	visibility := newFixtureVisibility(t, cutoff.Add(-time.Second), map[string]bool{poolA: true, poolB: true, poolC: false})
	reader, err := NewCanonicalInputReader("indexer", "stats", universeStream, universeProjection, visibility)
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := NewFrozenInputs(pool, reader)
	if err != nil {
		t.Fatal(err)
	}
	runID := uuid.NewString()
	in, err := frozen.Freeze(ctx, runID, cutoff)
	if err != nil {
		t.Fatalf("freeze: %v", err)
	}
	if !in.Complete || in.CutoffHeight != 200 || len(in.Candidates) != 3 {
		t.Fatalf("universe: complete=%v height=%d candidates=%d", in.Complete, in.CutoffHeight, len(in.Candidates))
	}
	a := in.Candidates[0]
	if a.Pool.PoolAddress != poolA || a.Pool.Volume24h != "630.000000" || a.Pool.Volume1h != "630.000000" || a.Pool.Volume5m != "130.000000" || a.Pool.BuyCount24h != 1 || a.Pool.SellCount24h != 1 || a.Pool.UniqueTraders24h != 1 || a.Pool.HolderCount != 2 {
		t.Fatalf("pool A window features: %+v", a.Pool)
	}
	if a.Pool.MarketCap != "1690.000000" || a.Pool.PriceChange24h != "30.000000" || a.Depth != "1355.000000" || !a.SupplyVerified || a.Paused || a.Suppressed {
		t.Fatalf("pool A market features: %+v depth=%s", a.Pool, a.Depth)
	}
	if a.Activity.Confidence != "complete" || a.Activity.RawVolume != "630.000000" || a.Activity.AdjustedVolume != "370.000000" || a.NullableMetrics == nil || a.NullableMetrics.Version != MetricsVersion || a.NullableMetrics.Paused != nil {
		t.Fatalf("pool A activity: %+v metrics %+v", a.Activity, a.NullableMetrics)
	}
	b := in.Candidates[1]
	if b.Pool.PoolAddress != poolB || b.Pool.MarketCap != "0" || b.Depth != "0.000000" || b.SupplyVerified || b.Pool.UniqueTraders24h != 0 {
		t.Fatalf("pool B untraded features: %+v", b)
	}
	if !in.Candidates[2].Suppressed {
		t.Fatal("moderation verdict ignored")
	}

	guard, err := queue.NewEpochGuard("recovery", 1, "fixture-loopback")
	if err != nil {
		t.Fatal(err)
	}
	publication, err := NewPublicationStore(pool, "ranking", guard)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := ranker.NewCoordinator(frozen, publication)
	if err != nil {
		t.Fatal(err)
	}
	heavy, err := coordinator.Refresh(ctx, uuid.NewString(), cutoff, false)
	if err != nil || len(heavy) != 5 {
		t.Fatalf("heavy run: %v", err)
	}
	newest, err := coordinator.Refresh(ctx, uuid.NewString(), cutoff, true)
	if err != nil || len(newest) != 1 {
		t.Fatalf("new run: %v", err)
	}
	want := map[string][]string{"trending": {poolA}, "volume": {poolA}, "unusual": {poolA}, "breakout": {poolA}, "movers": {}, "new": {poolB, poolA}}
	for category, addresses := range want {
		head, err := publication.Current(ctx, category)
		if err != nil {
			t.Fatalf("%s head: %v", category, err)
		}
		if head.Stale(cutoff) || head.AlgorithmVersion != ranker.AlgorithmVersion || head.Total != int64(len(addresses)) {
			t.Fatalf("%s head: %+v", category, head)
		}
		page, err := publication.Page(ctx, head, 0, 100)
		if err != nil {
			t.Fatalf("%s page: %v", category, err)
		}
		for i, address := range addresses {
			if page.Items[i].Address != address {
				t.Fatalf("%s rank %d = %s, want %s", category, i+1, page.Items[i].Address, address)
			}
		}
	}
	// Trending's top 50 are excluded from movers by policy; the only eligible
	// pool is trending's leader, so movers is a complete empty ranking.
	for _, s := range heavy {
		if s.Category() == "movers" && (len(s.Exclusions()) != 3 || s.Exclusions()[0].Reason != "trending-top-50") {
			t.Fatalf("movers exclusions: %+v", s.Exclusions())
		}
	}

	// Production publication: every category head is committed to api_stream
	// (append_ranking) in the same transaction that marks it published.
	writer := delivery.StreamWriter{Schema: "api_stream"}
	committed := 0
	for {
		worked, err := publication.CommitStreamOnce(ctx, writer, 10)
		if err != nil {
			t.Fatalf("commit to api_stream: %v", err)
		}
		if !worked {
			break
		}
		committed++
	}
	if committed != 6 {
		t.Fatalf("committed %d ranking frames, want 6", committed)
	}
	var frame []byte
	var streamSequence int64
	if err = pool.QueryRow(ctx, `SELECT sequence,frame FROM api_stream.stream_projection WHERE topic='market.rankings.trending'`).Scan(&streamSequence, &frame); err != nil {
		t.Fatal(err)
	}
	var event v1.RankingEvent
	if err = v1.ValidateJSON("RankingEvent", frame); err != nil || json.Unmarshal(frame, &event) != nil || streamSequence != 1 {
		t.Fatalf("trending projection frame invalid: %v %s", err, frame)
	}
	if event.Topic != "market.rankings.trending" || string(event.Sequence) != "1" || len(event.Payload.Items) != 1 || event.Payload.Items[0].PoolAddress != poolA || event.Payload.ModelVersion != ranker.AlgorithmVersion || event.Watermark == nil || string(event.Watermark.Height) != "200" {
		t.Fatalf("trending event: %+v", event)
	}
	var outboxRows int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM api_stream.stream_delivery_outbox`).Scan(&outboxRows); err != nil || outboxRows != 6 {
		t.Fatalf("api_stream outbox rows %d: %v", outboxRows, err)
	}
	if _, err = delivery.ParseCommittedStreamFrame(frame); err != nil {
		t.Fatalf("committed frame rejected by the relay parser: %v", err)
	}
	health, err := publication.PublicationHealth(ctx, cutoff, 10)
	if err != nil || health.Pending != 0 || health.Exhausted != 0 {
		t.Fatalf("publication health: %+v %v", health, err)
	}
	// A forged frame (entries that are not the committed snapshot) is refused
	// by the api_stream function itself.
	forged := delivery.StreamUpdate{EventID: uuid.NewString(), Topic: "market.rankings.trending", Revision: "1", OccurredAt: event.OccurredAt, Watermark: mustJSON(t, event.Watermark), Payload: mustJSON(t, event.Payload), Source: delivery.StreamSourceProof{Kind: "ranking_commit", SourceID: event.Payload.SnapshotId, Sequence: "1", SourceDigest: strings.Repeat("0", 64)}}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.AppendTx(ctx, tx, forged); err == nil {
		t.Fatal("forged ranking frame committed")
	}
	_ = tx.Rollback(ctx)

	// A later heavy run (5 heads) supersedes the first and commits as the next stream
	// sequence; once committed and older than the 900 s cursor horizon the
	// superseded snapshots are pruned while the current heads stay readable.
	if _, err = coordinator.Refresh(ctx, uuid.NewString(), cutoff.Add(time.Second), false); err != nil {
		t.Fatalf("second heavy run: %v", err)
	}
	for {
		worked, err := publication.CommitStreamOnce(ctx, writer, 10)
		if err != nil {
			t.Fatalf("commit to api_stream: %v", err)
		}
		if !worked {
			break
		}
	}
	if err = pool.QueryRow(ctx, `SELECT sequence FROM api_stream.stream_projection WHERE topic='market.rankings.trending'`).Scan(&streamSequence); err != nil || streamSequence != 2 {
		t.Fatalf("trending stream sequence %d: %v", streamSequence, err)
	}
	pruned, err := publication.Prune(ctx, cutoff.Add(902*time.Second), 100)
	if err != nil || pruned != 5 {
		t.Fatalf("pruned %d superseded snapshots: %v", pruned, err)
	}
	if _, err = publication.Current(ctx, "trending"); err != nil {
		t.Fatalf("head pruned: %v", err)
	}

	natsURL := os.Getenv("RANKING_TEST_NATS_URL")
	if natsURL == "" {
		t.Log("RANKING_TEST_NATS_URL not set; api_stream relay to JetStream not exercised")
		return
	}
	nc, err := nats.Connect(natsURL, nats.Timeout(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	streamName := "RANKING_TEST_" + strings.ToUpper(uuid.NewString()[:8])
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: streamName, Subjects: []string{"market.rankings.*"}, Storage: jetstream.MemoryStorage, MaxMsgs: 1000, MaxBytes: 16 << 20, MaxAge: time.Hour, Discard: jetstream.DiscardNew}); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	defer func() { _ = js.DeleteStream(context.Background(), streamName) }()
	budget, err := delivery.NewWorkBudget(1, 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	relay, err := delivery.NewStreamOutboxRelay(pool, nc, delivery.StreamOutboxConfig{Stream: streamName, Owner: "ranking-test", Batch: 50, MaxAttempts: 3, LeaseDuration: 10 * time.Second, PublishTimeout: 2 * time.Second, RetryDelay: time.Second}, guard.Assert, budget)
	if err != nil {
		t.Fatal(err)
	}
	if err = relay.RunOnce(ctx); err != nil {
		t.Fatalf("relay: %v", err)
	}
	stream, err := js.Stream(ctx, streamName)
	if err != nil {
		t.Fatal(err)
	}
	info, err := stream.Info(ctx)
	if err != nil || info.State.Msgs != 11 {
		t.Fatalf("relayed messages: %v %v", info.State, err)
	}
	message, err := stream.GetLastMsgForSubject(ctx, "market.rankings.trending")
	if err != nil {
		t.Fatal(err)
	}
	var latest []byte
	if err = pool.QueryRow(ctx, `SELECT frame FROM api_stream.stream_projection WHERE topic='market.rankings.trending'`).Scan(&latest); err != nil || string(latest) != string(message.Data) {
		t.Fatalf("relayed frame differs from the committed projection: %v", err)
	}
	if err = relay.Ready(ctx, time.Minute); err != nil {
		t.Fatalf("relay backlog: %v", err)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestProductionUniverseRejectsIncoherentProjection keeps the fail-closed
// gates: a fill the indexer holds but stats has not committed blocks the run.
func TestProductionUniverseRejectsIncoherentProjection(t *testing.T) {
	pool := productionUniverseDatabase(t)
	ctx := context.Background()
	cutoff := time.Now().UTC().Truncate(time.Second)
	insertCreation(t, pool, poolA, 100, cutoff.Unix()-3600)
	insertFill(t, pool, 1, universeFill{pool: poolA, side: "buy", wallet: walletOne, height: 101, timestamp: cutoff.Unix() - 60, usdl: "1000000", token: "1" + strings.Repeat("0", 18), paid: "1000000", fee: "10000"})
	if _, err := pool.Exec(ctx, `DELETE FROM stats.accounting_market_commits`); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`INSERT INTO indexer.indexer_ingest_leases(chain_id,stream_id,owner_instance,fence_token,expires_at) VALUES(125,'` + universeStream + `','indexer-1',1,clock_timestamp()+interval '1 minute')`,
		`INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,'` + universeStream + `',200,200)`,
	} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete,completed_at) VALUES(125,$1,200,$2,0,true,true,$3)`, universeStream, fmt.Sprintf("0x%064x", 200), cutoff.Add(-2*time.Second)); err != nil {
		t.Fatal(err)
	}
	reader, err := NewCanonicalInputReader("indexer", "stats", universeStream, universeProjection, newFixtureVisibility(t, cutoff.Add(-time.Second), map[string]bool{poolA: true}))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := NewFrozenInputs(pool, reader)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = frozen.Freeze(ctx, uuid.NewString(), cutoff); err != ranker.ErrUnavailable {
		t.Fatalf("incoherent projection accepted: %v", err)
	}
}
