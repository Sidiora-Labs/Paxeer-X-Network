package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting/sourcebootstrap"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const productionAddress = "0x1111111111111111111111111111111111111111"

// Source bootstrap layout shared by every fixture pool: one indexer stream with
// a commit holding the pool creations (heights 100..109) and a commit holding
// every fill (110..200). Each pool's fills start at its own base height.
const (
	productionSourceStream = "chain125-canonical"
	productionProjection   = "market-v1"
	productionGeneration   = "generation1"
	productionCreationsTo  = 109
	productionFillsTo      = 200
	productionBase         = 110
)

func productionBlockHash(height int64) string { return fmt.Sprintf("0x%064x", 0xb10c0000+height) }

func productionReadConfig() store.AccountingReadConfig {
	return store.AccountingReadConfig{ChartsSchema: "candles", StatsSchema: "stats", ChartsDeliverySchema: "candles", StatsDeliverySchema: "stats", ProjectionVersion: productionProjection, ProjectionGeneration: productionGeneration, ChartsConsumerID: "charts", StatsConsumerID: "stats", SourceStream: productionSourceStream, SourcePartitionSchema: "indexer"}
}

// productionAccountingFixture applies the registered read-model migrations and
// the indexer source migrations, lays out the indexer commits, and bootstraps
// productionAddress exactly as the stats and charts consumers do.
func productionAccountingFixture(t *testing.T) (*store.AccountingReadModel, *pgxpool.Pool) {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	ctx := context.Background()
	dir := "../../../../spec/production-system/integration/migrations/"
	for _, name := range []string{"80001", "80002", "100000", "130000", "60000"} {
		raw, err := os.ReadFile(dir + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(raw)); err != nil {
			t.Fatalf("registered migration %s: %v", name, err)
		}
	}
	// 80000 is the delivery template the coordinator renders into "indexer";
	// 190001's indexer_canonical_creations(_v1) already exists in the
	// internaltest read schema with the identical definition.
	raw, err := os.ReadFile(dir + "80000.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(raw)); err != nil {
		t.Fatalf("registered migration 80000: %v", err)
	}
	raw, err = os.ReadFile(dir + "190001.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.SplitAfter(string(raw), ";\n") {
		if strings.TrimSpace(statement) == "" || strings.HasPrefix(statement, "CREATE TABLE indexer.indexer_canonical_creations (") || strings.HasPrefix(statement, "CREATE VIEW indexer.indexer_canonical_creations_v1 ") {
			continue
		}
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatalf("registered migration 190001: %v", err)
		}
	}
	for h := int64(productionCreationsTo - 9); h <= productionFillsTo; h++ {
		if _, err = pool.Exec(ctx, `INSERT INTO indexer.indexer_block_completeness(chain_id,stream_id,height,block_hash,raw_count,fetch_complete,required_decode_complete) VALUES(125,$1,$2,$3,0,true,true)`, productionSourceStream, h, productionBlockHash(h)); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		id             string
		previous, last int64
	}{{"creations", productionCreationsTo - 10, productionCreationsTo}, {"fills", productionCreationsTo, productionFillsTo}} {
		if _, err = pool.Exec(ctx, `INSERT INTO indexer.indexer_ingest_commits(chain_id,stream_id,batch_id,previous_height,last_complete_height,block_hash,record_count,outbox_count) VALUES(125,$1,$2,$3,$4,$5,0,0)`, productionSourceStream, c.id, c.previous, c.last, productionBlockHash(c.last)); err != nil {
			t.Fatal(err)
		}
	}
	model, err := store.NewAccountingReadModel(pool, productionReadConfig())
	if err != nil {
		t.Fatal(err)
	}
	bootstrapProductionPool(t, pool, productionAddress, productionCreationsTo-9, productionBase, nil)
	return model, pool
}

// productionCheckpoints are the per-pool checkpoints the consumers derive from
// the fixture layout: StatsEvidence/ChartsEvidence (the producers' own tuple)
// filled from the values the fixture wrote, not read back from the database.
func productionCheckpoints(pool string, creationHeight int64, publication accounting.PublicationV1) (charts, stats accounting.Digest) {
	payload := publication.Positions[0].PayloadDigest
	stats = sourcebootstrap.StatsEvidence{SourceStream: productionSourceStream, Pool: pool, ProjectionVersion: productionProjection, PublicationID: publication.PublicationID, PayloadDigest: payload, InitialPreviousHeight: productionCreationsTo - 10, FirstCommitID: "fills", FirstCommitLastHeight: productionFillsTo, FirstCommitBlockHash: productionBlockHash(productionFillsTo)}.Checkpoint()
	charts = sourcebootstrap.ChartsEvidence{SourceStream: productionSourceStream, Pool: pool, ProjectionVersion: productionProjection, PublicationID: publication.PublicationID, PayloadDigest: payload, ReplayFromHeight: productionCreationsTo - 10, CreationTransactionHash: productionCreationTx(pool), CreationLogIndex: 1, FirstCommitID: "fills", FirstCommitLastHeight: productionFillsTo, FirstCommitBlockHash: productionBlockHash(productionFillsTo)}.Checkpoint()
	return charts, stats
}

func productionCreationTx(pool string) string { return "0x" + strings.Repeat("e", 24) + pool[2:] }

// bootstrapProductionPool writes pool's indexer source evidence (creation log,
// canonical creation, first fill raw log, the indexer partition position via
// delivery.PartitionStore.AppendTx and the market publication in the indexer
// outbox), then writes both consumers' delivery_partition_offsets rows through
// delivery.PartitionStore.BootstrapTx — the producers' own offsets path — with
// the checkpoint they derive. override, when set, replaces the checkpoints.
func bootstrapProductionPool(t *testing.T, db *pgxpool.Pool, pool string, creationHeight int64, base int, override func(charts, stats accounting.Digest) (accounting.Digest, accounting.Digest)) {
	t.Helper()
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql[:min(len(sql), 80)], err)
		}
	}
	factory := "0x" + strings.Repeat("f", 40)
	token := "0x" + strings.Repeat("3", 30) + pool[32:]
	creationTx := productionCreationTx(pool)
	topics, _ := json.Marshal([]string{fmt.Sprintf("0x%064x", 1), "0x" + strings.Repeat("0", 24) + token[2:], "0x" + strings.Repeat("0", 24) + pool[2:]})
	exec(`INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES(125,$1,1,$2,0,$3,$4,$5,$6)`, creationTx, creationHeight, productionBlockHash(creationHeight), factory, topics, []byte{1})
	exec(`INSERT INTO indexer.indexer_canonical_creations(token,pool,creator,factory,optical,nft_id,height,block_hash,transaction_hash,transaction_index,log_index,block_timestamp,provenance,payload) VALUES($1,$2,$3,$4,$5,1,$6,$7,$8,0,1,1700000000,'{}','{}')`, token, pool, "0x"+strings.Repeat("5", 40), factory, "0x"+strings.Repeat("6", 40), creationHeight, productionBlockHash(creationHeight), creationTx)

	fill, position, publication := productionFill(t, pool, base, 1, "")
	fillHeight, _ := strconv.ParseInt(fill.Position.Height, 10, 64)
	fillIndex, _ := strconv.ParseInt(fill.Position.TransactionIndex, 10, 64)
	exec(`INSERT INTO indexer.indexer_raw_logs(chain_id,transaction_hash,log_index,height,transaction_index,block_hash,emitter,topics,data) VALUES(125,$1,0,$2,$3,$4,$5,'[]',$6)`, fill.Position.TransactionHash, fillHeight, fillIndex, fill.Position.BlockHash, pool, []byte{2})
	indexerPartitions, err := delivery.NewPartitionStore(db, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	appended, err := indexerPartitions.AppendTx(ctx, tx, delivery.AppendInput{ProjectionVersion: productionProjection, PartitionKind: "market", PartitionKey: position.PartitionKey, SourceEventID: fill.SourceFillID, EffectIdentity: position.EffectIdentity, PayloadDigest: position.PayloadDigest, SourceOrder: fill.Position})
	if err != nil {
		tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if appended != position {
		t.Fatalf("indexer position %+v != fixture position %+v", appended, position)
	}
	payload, err := accounting.CanonicalPublicationV1(publication)
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO indexer.indexer_ingest_outbox(chain_id,transaction_hash,log_index,event_kind,payload) VALUES(125,$1,0,'accounting.market.v1',$2)`, fill.Position.TransactionHash, payload)

	chartsDigest, statsDigest := productionCheckpoints(pool, creationHeight, publication)
	if override != nil {
		chartsDigest, statsDigest = override(chartsDigest, statsDigest)
	}
	for _, binding := range []struct {
		schema, consumer string
		digest           accounting.Digest
	}{{"candles", "charts", chartsDigest}, {"stats", "stats", statsDigest}} {
		exec("INSERT INTO "+pgx.Identifier{binding.schema}.Sanitize()+".delivery_partition_leases VALUES($1,$2,'market',$3,'fixture',1,now()+interval '1 hour')", binding.consumer, productionGeneration, "125:"+pool)
		partitions, err := delivery.NewPartitionStore(db, binding.schema)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := db.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		lease := delivery.PartitionLease{Key: delivery.ConsumerKey{ConsumerID: binding.consumer, ProjectionGeneration: productionGeneration, PartitionKind: "market", PartitionKey: "125:" + pool}, Owner: "fixture", Epoch: 1}
		input := delivery.BootstrapInput{Lease: lease, CheckpointDigest: binding.digest, NextSequence: "1", PreviousEffectIdentity: ""}
		if err = partitions.BootstrapTx(ctx, tx, input, func(_ context.Context, _ pgx.Tx, in delivery.BootstrapInput) error {
			if in.CheckpointDigest != binding.digest || in.NextSequence != "1" || in.PreviousEffectIdentity != "" {
				return delivery.ErrIntegrity
			}
			return nil
		}); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO candles.chart_pool_fences VALUES('125',$1,1,false)", pool)
	exec("INSERT INTO stats.accounting_pool_fences(pool,frozen) VALUES($1,false)", pool)
}

// productionFill is pool's sequence-th fill (fills start at height base, 50 per
// block), its market partition position and its publication.
func productionFill(t *testing.T, pool string, base, sequence int, previous string) (accounting.FillV1, accounting.PartitionPosition, accounting.PublicationV1) {
	t.Helper()
	height := int64(base + (sequence-1)/50)
	f := accounting.FillV1{Version: accounting.FillVersion, Position: accounting.SourcePosition{ChainID: "125", Height: strconv.FormatInt(height, 10), TransactionIndex: strconv.Itoa((sequence - 1) % 50), LogIndex: "0", TransactionHash: fmt.Sprintf("0x%024x%s", sequence, pool[2:]), BlockHash: productionBlockHash(height)}, ActualEmitter: pool, PoolAddress: pool, Token: accounting.Asset{Address: "0x" + strings.Repeat("3", 40), Decimals: 18}, Settlement: accounting.Asset{Address: "0x" + strings.Repeat("4", 40), Decimals: 6}, Side: "buy", TokenAmountRaw: "1000000000000000000", USDLAmountRaw: "1000000", PaidInputRaw: "1000000", FeeRaw: "3000", InputDerivationQuality: "receipt_incomplete", FeeDisposition: "distributed_usdl", Attribution: accounting.Attribution{TransactionSender: "0x" + strings.Repeat("5", 40), Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Execution: accounting.ExecutionPolicy{Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}, Provenance: accounting.Provenance{DeploymentID: "fixture125", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch1", PoolCodeEpoch: "epoch1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}, BlockTimestampSeconds: "1700000000"}
	f.SourceFillID = accounting.SourceFillIdentity(f.Position, f.Provenance.DeploymentID)
	digest, err := accounting.FillDigest(f)
	if err != nil {
		t.Fatal(err)
	}
	position := accounting.PartitionPosition{ProjectionVersion: productionProjection, PartitionKind: "market", PartitionKey: "125:" + pool, Sequence: strconv.Itoa(sequence), PreviousEffectIdentity: previous, SourceEventID: f.SourceFillID, PayloadDigest: digest, SourceOrder: f.Position}
	position.EffectIdentity = accounting.EffectIdentity(f.SourceFillID, "market", productionProjection, "", "market", position.PartitionKey)
	publication, err := accounting.NewMarketPublication(f, []accounting.PartitionPosition{position})
	if err != nil {
		t.Fatal(err)
	}
	return f, position, publication
}

func insertProductionFill(t *testing.T, pool *pgxpool.Pool, sequence int, previous string) string {
	t.Helper()
	return insertPoolFill(t, pool, productionAddress, productionBase, sequence, previous)
}

// insertPoolFill commits pool's sequence-th fill to stats and advances both
// consumers' offsets past it, as the consumers' Effect/Advance hooks do.
func insertPoolFill(t *testing.T, pool *pgxpool.Pool, address string, base, sequence int, previous string) string {
	t.Helper()
	ctx := context.Background()
	f, position, publication := productionFill(t, address, base, sequence, previous)
	digest := position.PayloadDigest
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	positionRaw, err := json.Marshal(position)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "INSERT INTO stats.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,$4,$5,1699999980,$6,$7,$8,$8)", publication.PublicationID, f.SourceFillID, string(digest), productionProjection, address, positionRaw, raw, sequence); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []struct{ schema, consumer string }{{"candles", "charts"}, {"stats", "stats"}} {
		q := pgx.Identifier{binding.schema}.Sanitize()
		if _, err = tx.Exec(ctx, "INSERT INTO "+q+".delivery_partition_commits VALUES($1,$2,'market',$3,$4,$5,$6)", binding.consumer, productionGeneration, position.PartitionKey, sequence, position.EffectIdentity, positionRaw); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "UPDATE "+q+".delivery_partition_offsets SET next_sequence=$3,previous_effect_identity=$4 WHERE consumer_id=$1 AND partition_key=$2", binding.consumer, position.PartitionKey, sequence+1, position.EffectIdentity); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return position.EffectIdentity
}
func TestProductionCanonicalAccountingPagesKeepCommittedFrontier(t *testing.T) {
	model, pool := productionAccountingFixture(t)
	previous := ""
	for i := 1; i <= 150; i++ {
		previous = insertProductionFill(t, pool, i, previous)
	}
	request := store.AccountingPageRequest{Pool: productionAddress, Limit: 50}
	seen := map[string]bool{}
	var original store.AccountingPage
	lastHeight, lastTx := int64(productionBase+3), int64(0)
	for page := 0; page < 3; page++ {
		got, err := model.Trades(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Items) != 50 {
			t.Fatal("page size", len(got.Items))
		}
		if page == 0 {
			original = got
			pinned := int64(150)
			request.SnapshotSequence = &pinned
			request.ExpectedSnapshot = got.SnapshotID
			var watermark struct {
				Observed string `json:"observedAt"`
			}
			if json.Unmarshal(got.Watermark, &watermark) != nil {
				t.Fatal(string(got.Watermark))
			}
			request.SnapshotObservedAt = watermark.Observed
			previous = insertProductionFill(t, pool, 151, previous)
		} else if string(got.Watermark) != string(original.Watermark) || got.SnapshotID != original.SnapshotID {
			t.Fatal("cursor frontier or exact watermark moved", string(got.Watermark), string(original.Watermark))
		}
		for _, raw := range got.Items {
			var trade struct {
				ID       string                    `json:"eventId"`
				Amount   string                    `json:"usdlAmountRaw"`
				Position accounting.SourcePosition `json:"position"`
			}
			if json.Unmarshal(raw, &trade) != nil || seen[trade.ID] || trade.Amount != "1000000" {
				t.Fatal(string(raw))
			}
			height, _ := strconv.ParseInt(trade.Position.Height, 10, 64)
			txIndex, _ := strconv.ParseInt(trade.Position.TransactionIndex, 10, 64)
			if height > lastHeight || height == lastHeight && txIndex >= lastTx {
				t.Fatal("noncanonical ordering", trade.Position)
			}
			lastHeight, lastTx = height, txIndex
			seen[trade.ID] = true
		}
		if page < 2 && got.NextSequence == nil {
			t.Fatal("missing continuation")
		}
		if page == 2 && got.NextSequence != nil {
			t.Fatal("unexpected fourth page")
		}
		request.AfterSequence = got.NextSequence
	}
	if len(seen) != 150 {
		t.Fatal("missing identities", len(seen))
	}
	router := chi.NewRouter()
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err = rest.RegisterAccountingV2WithAdmission(router, model, []byte(strings.Repeat("a", 32)), nil, admission); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()
	cursor := ""
	httpSeen := map[string]bool{}
	originalWatermark := ""
	originalRevision := ""
	for page := 0; page < 4; page++ {
		endpoint := srv.URL + "/v2/pools/" + productionAddress + "/trades?limit=50"
		if cursor != "" {
			endpoint += "&cursor=" + url.QueryEscape(cursor)
		}
		response, err := http.Get(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatal(response.StatusCode, string(raw), err)
		}
		var result struct {
			Data struct {
				Items []struct {
					ID string `json:"eventId"`
				} `json:"items"`
			} `json:"data"`
			Watermark json.RawMessage `json:"watermark"`
			Revision  string          `json:"revision"`
			Next      *string         `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &result) != nil {
			t.Fatal(string(raw))
		}
		if page == 0 {
			originalWatermark = string(result.Watermark)
			originalRevision = result.Revision
			insertProductionFill(t, pool, 152, previous)
		} else if string(result.Watermark) != originalWatermark || result.Revision != originalRevision {
			t.Fatal("HTTP cursor watermark moved", string(raw))
		}
		for _, item := range result.Data.Items {
			if httpSeen[item.ID] {
				t.Fatal("HTTP duplicate", item.ID)
			}
			httpSeen[item.ID] = true
		}
		if result.Next == nil {
			if page != 3 {
				t.Fatal("HTTP early end", page)
			}
			cursor = ""
		} else {
			cursor = *result.Next
			if len(cursor) > 256 {
				t.Fatal("cursor input bound", len(cursor))
			}
		}
	}
	if len(httpSeen) != 151 || cursor != "" {
		t.Fatal("HTTP continuation omission or leak", len(httpSeen), cursor)
	}
	if _, err := pool.Exec(context.Background(), "UPDATE stats.accounting_pool_fences SET frozen=true WHERE pool=$1", productionAddress); err != nil {
		t.Fatal(err)
	}
	if _, err := model.Trades(context.Background(), request); err != store.ErrDataIntegrity {
		t.Fatal("quarantined source served", err)
	}
}

// TestAccountingReadModelDerivesPerPoolSourceCheckpoints: the stats and charts
// consumers store a different source bootstrap checkpoint for every pool, so
// the read model re-derives each pool's checkpoint from the indexer evidence.
// Two pools bootstrapped through the producers' offsets path are both served;
// a pool whose stored checkpoint does not match its evidence is refused.
func TestAccountingReadModelDerivesPerPoolSourceCheckpoints(t *testing.T) {
	model, db := productionAccountingFixture(t)
	ctx := context.Background()
	pools := []struct {
		address  string
		creation int64
		base     int
	}{{"0xce20da50f92d21d8bee497910c782e9aa25b6263", productionCreationsTo - 8, 120}, {"0x2222222222222222222222222222222222222222", productionCreationsTo - 7, 130}}
	stored := map[string]bool{}
	for _, p := range pools {
		bootstrapProductionPool(t, db, p.address, p.creation, p.base, nil)
		previous := ""
		for i := 1; i <= 3; i++ {
			previous = insertPoolFill(t, db, p.address, p.base, i, previous)
		}
		for _, schema := range []string{"candles", "stats"} {
			var digest string
			if err := db.QueryRow(ctx, "SELECT checkpoint_digest FROM "+pgx.Identifier{schema}.Sanitize()+".delivery_partition_offsets_v1 WHERE partition_key=$1", "125:"+p.address).Scan(&digest); err != nil {
				t.Fatal(err)
			}
			if stored[digest] {
				t.Fatal("checkpoint is not per pool", schema, digest)
			}
			stored[digest] = true
		}
		got, err := model.Trades(ctx, store.AccountingPageRequest{Pool: p.address, Limit: 10})
		if err != nil {
			t.Fatalf("pool %s refused: %v", p.address, err)
		}
		if len(got.Items) != 3 || got.Revision != "3" {
			t.Fatal("pool page", p.address, len(got.Items), got.Revision)
		}
		// A second read is served from the verified binding.
		if _, err = model.Trades(ctx, store.AccountingPageRequest{Pool: p.address, Limit: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if len(stored) != 4 {
		t.Fatal("expected four distinct checkpoints", len(stored))
	}

	// Offsets bootstrapped with a checkpoint the evidence does not produce
	// (another pool's) are refused for both consumers.
	forged := "0x3333333333333333333333333333333333333333"
	bootstrapProductionPool(t, db, forged, productionCreationsTo-6, 140, func(charts, stats accounting.Digest) (accounting.Digest, accounting.Digest) {
		return stats, charts
	})
	insertPoolFill(t, db, forged, 140, 1, "")
	if _, err := model.Trades(ctx, store.AccountingPageRequest{Pool: forged, Limit: 10}); !errors.Is(err, store.ErrDataIntegrity) {
		t.Fatal("mismatched source checkpoint served", err)
	}

	// A pool whose indexer evidence is gone is refused, even by a fresh model.
	if _, err := db.Exec(ctx, "DELETE FROM indexer.indexer_ingest_outbox WHERE payload->>'sourceFillId'=(SELECT source_fill_id FROM stats.accounting_market_commits WHERE pool=$1 AND sequence=1)", pools[1].address); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.NewAccountingReadModel(db, productionReadConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fresh.Trades(ctx, store.AccountingPageRequest{Pool: pools[1].address, Limit: 10}); !errors.Is(err, store.ErrDataIntegrity) {
		t.Fatal("pool without source evidence served", err)
	}
	if _, err = fresh.Trades(ctx, store.AccountingPageRequest{Pool: pools[0].address, Limit: 10}); err != nil {
		t.Fatal(err)
	}

	// The configuration requires the producers' source identity.
	for _, broken := range []func(*store.AccountingReadConfig){
		func(c *store.AccountingReadConfig) { c.SourceStream = "" },
		func(c *store.AccountingReadConfig) { c.SourcePartitionSchema = "" },
		func(c *store.AccountingReadConfig) { c.SourcePartitionSchema = "Indexer;" },
	} {
		c := productionReadConfig()
		broken(&c)
		if _, err = store.NewAccountingReadModel(db, c); !errors.Is(err, store.ErrAccountingNotReady) {
			t.Fatal("incomplete source binding accepted", c)
		}
	}
}
