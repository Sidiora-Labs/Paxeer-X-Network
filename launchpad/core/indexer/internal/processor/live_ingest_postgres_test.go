package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/publisher"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/runtimedocs"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/source"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store/canonicaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Live ingestion runs against the real chain-125 nodes (no fixture, no
// containers): INDEXER_LIVE_INGEST=1 with NETWORK_RPC_URL (primary, pruned)
// and INDEXER_HISTORICAL_RPC_URL (archive). INDEXER_LIVE_INGEST_FROM and
// INDEXER_LIVE_INGEST_TO bound the range (defaults: the real market's creation
// block through the primary head at test start).
func liveEndpoints(t *testing.T) (string, string) {
	t.Helper()
	if os.Getenv("INDEXER_LIVE_INGEST") != "1" {
		t.Skip("live chain-125 ingestion requires INDEXER_LIVE_INGEST=1")
	}
	primary, historical := os.Getenv("NETWORK_RPC_URL"), os.Getenv("INDEXER_HISTORICAL_RPC_URL")
	if primary == "" || historical == "" {
		t.Fatal("NETWORK_RPC_URL and INDEXER_HISTORICAL_RPC_URL are required")
	}
	return primary, historical
}

type rpcBlockAnswer struct {
	Result *struct {
		Number string `json:"number"`
		Hash   string `json:"hash"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func rawBlock(ctx context.Context, url, tag string) (rpcBlockAnswer, error) {
	var answer rpcBlockAnswer
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber", "params": []any{tag, false}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return answer, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return answer, err
	}
	defer response.Body.Close()
	err = json.NewDecoder(response.Body).Decode(&answer)
	return answer, err
}

func reportedHash(t *testing.T, ctx context.Context, url string, height uint64) common.Hash {
	t.Helper()
	answer, err := rawBlock(ctx, url, fmt.Sprintf("0x%x", height))
	if err != nil || answer.Result == nil {
		t.Fatalf("block %d from %s: %v %+v", height, url, err, answer.Error)
	}
	return common.HexToHash(answer.Result.Hash)
}

func headHeight(t *testing.T, ctx context.Context, url string) uint64 {
	t.Helper()
	answer, err := rawBlock(ctx, url, "latest")
	if err != nil || answer.Result == nil {
		t.Fatalf("head from %s: %v", url, err)
	}
	h, err := strconv.ParseUint(strings.TrimPrefix(answer.Result.Number, "0x"), 16, 64)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func envHeight(t *testing.T, name string, fallback uint64) uint64 {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	h, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return h
}

type liveIngestor struct {
	processor *Processor
	durable   *atomic.Uint64
	authority *receipt.RPCQualification
	source    *source.CommittedSource
	store     *store.Store
}

// newLiveIngestor wires the production qualified ingestion exactly as
// app.New does for NODE_ENV=production (reported block identity, anchor,
// archive failover, legacy projection, pinned reserves), over actual
// PostgreSQL with the production schema.
func newLiveIngestor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, primary, historical string, anchor receipt.BlockAnchor) liveIngestor {
	t.Helper()
	inputs := realReceiptInputs(t)
	inputs.Anchor = anchor
	options, _, err := runtimedocs.ReceiptOptions(inputs)
	if err != nil {
		t.Fatal(err)
	}
	tuning := liveArchiveTuning(t)
	client, closeClient, err := receipt.NewBoundedRPCClientWithArchive(ctx, primary, historical, receipt.ArchiveOptions{Concurrency: tuning.Concurrency, AttemptTimeout: tuning.AttemptTimeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeClient)
	authority, err := receipt.NewRPCQualification(ctx, client, options)
	if err != nil {
		t.Fatalf("production authority: %v", err)
	}
	if err = authority.ConfigureArchiveReads(tuning.Concurrency, tuning.Readahead); err != nil {
		t.Fatal(err)
	}
	committed, err := runtimedocs.CommittedOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	committed.Endpoints[runtimedocs.PrimaryEndpointID] = primary
	committed.HistoricalEndpoints = map[string]string{"paxeer-historical": historical}
	committed.HistoricalConcurrency, committed.HistoricalReadahead, committed.HistoricalAttemptTimeout = tuning.Concurrency, tuning.Readahead, tuning.AttemptTimeout
	rpcSource, err := source.NewCommittedSource(ctx, committed)
	if err != nil {
		t.Fatalf("committed source: %v", err)
	}
	t.Cleanup(func() { _ = rpcSource.Close() })
	guard, err := queue.NewEpochGuard("recovery", 1, "railway-prod")
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.NewLifecycleStore(pool, guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	policy := delivery.Policy{Subjects: map[string]bool{publisher.CanonicalCreationSubject: true, accounting.MarketSubject: true, ReplaySubjectV2: true}, EventTypes: map[string]bool{publisher.CanonicalCreationEventType: true, accounting.MarketSubject: true, ReplaySubjectV2: true}}
	durable, err := delivery.NewStoreWithEpochGuard(pool, "indexer", policy, guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	partitions, err := delivery.NewPartitionStore(pool, "indexer")
	if err != nil {
		t.Fatal(err)
	}
	stager, err := publisher.NewDurableStager(durable, options.Manifest.DeploymentVersion, map[string]string{publisher.CanonicalCreationEventType: publisher.CanonicalCreationSubject, accounting.MarketSubject: accounting.MarketSubject})
	if err != nil {
		t.Fatal(err)
	}
	height := new(atomic.Uint64)
	height.Store(anchor.Height)
	live := &source.LiveQualifiedSource{RPC: rpcSource, Authority: authority, DurableHeight: height.Load, PriorHash: func(c context.Context, h uint64) (common.Hash, error) {
		if h == anchor.Height {
			return anchor.Hash, nil
		}
		var hash string
		err := pool.QueryRow(c, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2`, realStream, int64(h)).Scan(&hash)
		return common.HexToHash(hash), err
	}}
	p := New(Deps{Store: st, Decoder: decode.NewDecoder(), EventEmitterAddress: deployment.Current().Roles["EventEmitter"].Address, ChainID: 125, QualifiedBatchSource: live, CanonicalStreamID: realStream, CanonicalOwner: "indexer-live-test", AccountingPartitions: partitions, Durable: stager, AccountingProjectionVersion: "accounting-market-v1", LegacyProjection: true, Reserves: authority})
	return liveIngestor{processor: p, durable: height, authority: authority, source: rpcSource, store: st}
}

// liveArchiveTuning applies the production INDEXER_HISTORICAL_* knobs (and
// their defaults) exactly as app.New does.
func liveArchiveTuning(t *testing.T) config.ArchiveTuning {
	t.Helper()
	cfg := config.Config{HistoricalConcurrency: 48, HistoricalReadahead: 256, HistoricalAttemptTimeoutMS: 8000}
	for name, field := range map[string]*int{"INDEXER_HISTORICAL_CONCURRENCY": &cfg.HistoricalConcurrency, "INDEXER_HISTORICAL_READAHEAD": &cfg.HistoricalReadahead, "INDEXER_HISTORICAL_ATTEMPT_TIMEOUT_MS": &cfg.HistoricalAttemptTimeoutMS} {
		if raw := os.Getenv(name); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			*field = v
		}
	}
	tuning, err := cfg.ArchiveTuning()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("archive tuning: concurrency=%d readahead=%d attempt=%s", tuning.Concurrency, tuning.Readahead, tuning.AttemptTimeout)
	return tuning
}

// logArchiveCounters reports the work and error counters of both readers.
func (l liveIngestor) logArchiveCounters(t *testing.T) {
	t.Helper()
	m, tm, a := l.source.Metrics(), l.source.TransportMetrics(), l.authority.ArchiveReadMetrics()
	retries, overloads := receipt.ArchiveTransportCounters()
	t.Logf("committed: blocks=%d headerFetches=%d readaheadHits=%d bloomSkipped=%d receiptBlocks=%d retries=%d timeouts=%d throttles=%d", m.BlocksFetched, m.HeaderFetches, m.ReadaheadHits, m.BloomSkipped, m.ReceiptFallbacks, tm.RetryAttempts, tm.Timeouts, tm.Throttles)
	t.Logf("authority: readaheadHits=%d bloomSkipped=%d receiptReads=%d archiveRetries=%d archiveOverloads=%d", a.ReadaheadHits, a.BloomSkipped, a.ReceiptReads, retries, overloads)
}

func seedWatermark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, height uint64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE indexer.indexer_ingest_watermarks SET raw_complete_height=$2, typed_complete_height=$2 WHERE chain_id=125 AND stream_id=$1`, realStream, int64(height)); err != nil {
		t.Fatal(err)
	}
}

func (l liveIngestor) ingest(t *testing.T, ctx context.Context, from, to uint64) (time.Duration, int) {
	t.Helper()
	started := time.Now()
	retries := 0
	for next := from; next <= to; {
		end := min(next+63, to)
		if err := l.processor.ProcessBatch(ctx, int64(next), int64(end)); err != nil {
			retries++
			if retries > 20 || ctx.Err() != nil {
				t.Fatalf("batch %d..%d: %v", next, end, err)
			}
			t.Logf("batch %d..%d retry: %v", next, end, err)
			time.Sleep(time.Second)
			continue
		}
		l.durable.Store(end)
		if (end-from+1)/1024 != (next-from)/1024 || end == to {
			elapsed := time.Since(started)
			t.Logf("ingested %d..%d (%d blocks) in %s: %.1f blocks/s", from, end, end-from+1, elapsed.Round(time.Millisecond), float64(end-from+1)/elapsed.Seconds())
		}
		next = end + 1
	}
	return time.Since(started), retries
}

// Fresh production fleet from the real market's creation block through the
// current head, reading blocks the primary has pruned from the archive.
func TestLiveProductionIngestFromCreationPostgres(t *testing.T) {
	primary, historical := liveEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	from := envHeight(t, "INDEXER_LIVE_INGEST_FROM", 33963716)
	to := envHeight(t, "INDEXER_LIVE_INGEST_TO", headHeight(t, ctx, primary))
	anchor := receipt.BlockAnchor{Height: from - 1, Hash: reportedHash(t, ctx, historical, from-1)}
	_, pool := canonicaltest.New(t)
	applyProductionSchema(t, ctx, pool)
	seedWatermark(t, ctx, pool, anchor.Height)
	l := newLiveIngestor(t, ctx, pool, primary, historical, anchor)
	elapsed, retries := l.ingest(t, ctx, from, to)
	t.Logf("BACKFILL %d..%d: %d blocks in %s (%.1f blocks/s, %d retried batches)", from, to, to-from+1, elapsed.Round(time.Second), float64(to-from+1)/elapsed.Seconds(), retries)
	l.logArchiveCounters(t)
	if from > 33963716 || to < 33963733 {
		return
	}

	var creationHash, firstFill, firstFillSide string
	var pools, swaps int
	if err := pool.QueryRow(ctx, `SELECT (SELECT block_hash FROM indexer.indexer_canonical_creations WHERE pool=$1), (SELECT f.transaction_hash FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs r USING (chain_id,transaction_hash,log_index) WHERE f.pool_address=$1 ORDER BY r.height, r.transaction_index, r.log_index LIMIT 1), (SELECT f.payload->>'side' FROM indexer.indexer_accounting_fills f JOIN indexer.indexer_raw_logs r USING (chain_id,transaction_hash,log_index) WHERE f.pool_address=$1 ORDER BY r.height, r.transaction_index, r.log_index LIMIT 1), (SELECT count(*) FROM indexer.pools WHERE lower(pool_address)=$1), (SELECT count(*) FROM indexer.swaps WHERE lower(pool_address)=$1)`, realMarketPool).Scan(&creationHash, &firstFill, &firstFillSide, &pools, &swaps); err != nil {
		t.Fatalf("canonical market rows: %v", err)
	}
	if creationHash != "0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b" || firstFill != "0x3d28a4d088c87b4eca7507e74b47bdc01ae30ff4b1e478dc404a1caa9921a503" || firstFillSide != "buy" || pools != 1 || swaps < 1 {
		t.Fatalf("creation=%s firstFill=%s side=%s pools=%d swaps=%d", creationHash, firstFill, firstFillSide, pools, swaps)
	}
}

var earliestPattern = regexp.MustCompile(`earliest available is (\d+)`)

// The primary has pruned the lower half of the range: the archive serves it
// under the same node-reported identity rule, then the primary takes over.
func TestLiveArchiveFailoverAcrossPrimaryFloorPostgres(t *testing.T) {
	primary, historical := liveEndpoints(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	answer, err := rawBlock(ctx, primary, "0x1")
	if err != nil || answer.Error == nil {
		t.Fatalf("primary is not pruned: %v %+v", err, answer.Result)
	}
	match := earliestPattern.FindStringSubmatch(answer.Error.Message)
	if match == nil {
		t.Fatalf("pruning floor not reported: %s", answer.Error.Message)
	}
	floor, _ := strconv.ParseUint(match[1], 10, 64)
	// Margin keeps the range below the moving floor until ingestion reaches it.
	from, to := floor-200, floor+200
	anchor := receipt.BlockAnchor{Height: from - 1, Hash: reportedHash(t, ctx, historical, from-1)}
	_, pool := canonicaltest.New(t)
	applyProductionSchema(t, ctx, pool)
	seedWatermark(t, ctx, pool, anchor.Height)
	l := newLiveIngestor(t, ctx, pool, primary, historical, anchor)
	elapsed, _ := l.ingest(t, ctx, from, to)
	t.Logf("ingested %d blocks across primary floor %d in %s (%.1f blocks/s)", to-from+1, floor, elapsed.Round(time.Millisecond), float64(to-from+1)/elapsed.Seconds())
	l.logArchiveCounters(t)
	for _, h := range []uint64{from, floor - 1, to} {
		var stored string
		if err = pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE height=$1`, int64(h)).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if want := strings.ToLower(reportedHash(t, ctx, historical, h).Hex()); stored != want {
			t.Fatalf("height %d stored %s, archive reports %s", h, stored, want)
		}
	}
	// Committed heads carry the node-reported hash (what pnl and user verify),
	// never go-ethereum's recomputed header hash.
	head, err := l.source.ObserveHead(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.store.RetainCommittedHead(ctx, realStream, int64(head.Height), strings.ToLower(head.Hash.Hex()), head.SourceID); err != nil {
		t.Fatal(err)
	}
	var committedHead string
	if err = pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_committed_heads_v2 WHERE chain_id=125 AND height=$1`, int64(head.Height)).Scan(&committedHead); err != nil {
		t.Fatal(err)
	}
	if want := strings.ToLower(reportedHash(t, ctx, primary, head.Height).Hex()); committedHead != want {
		t.Fatalf("committed head %d stored %s, node reports %s", head.Height, committedHead, want)
	}
	recomputed, err := receiptHeaderHash(ctx, primary, head.Height)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ToLower(recomputed.Hex()) == committedHead {
		t.Fatal("chain 125 header hash unexpectedly equals the reported hash")
	}
	answer, err = rawBlock(ctx, primary, fmt.Sprintf("0x%x", from))
	if err != nil || answer.Error == nil || !receipt.IsPruned(fmt.Errorf("%s", answer.Error.Message)) {
		t.Fatalf("range start %d was not pruned on the primary; archive failover not exercised", from)
	}
}

func receiptHeaderHash(ctx context.Context, url string, height uint64) (common.Hash, error) {
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_getBlockByNumber", "params": []any{fmt.Sprintf("0x%x", height), false}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return common.Hash{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return common.Hash{}, err
	}
	defer response.Body.Close()
	var envelope struct {
		Result types.Header `json:"result"`
	}
	if err = json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return common.Hash{}, err
	}
	return envelope.Result.Hash(), nil
}
