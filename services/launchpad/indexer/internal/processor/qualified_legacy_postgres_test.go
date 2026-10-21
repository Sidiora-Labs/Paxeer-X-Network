package processor

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/decode"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/internaltest/chainfixture"
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
	"github.com/jackc/pgx/v5/pgxpool"
)

const realStream = "chain125-canonical"

var (
	realMarketPool  = "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	realMarketToken = "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	realAnchor      = receipt.BlockAnchor{Height: 33963715, Hash: common.HexToHash("0x82477110c5fc791f6bef5a7d96636dc457e83daf59c8b802e264bada02f13395")}
)

func repositoryRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
}

func applyProductionSchema(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	ddl := []string{}
	for _, build := range []func(string) (string, error){delivery.SchemaDDL, delivery.PartitionSchemaDDL} {
		sql, err := build("indexer")
		if err != nil {
			t.Fatal(err)
		}
		ddl = append(ddl, sql)
	}
	for _, id := range []string{"60001", "190001"} {
		raw, err := os.ReadFile(filepath.Join(repositoryRoot(), "spec", "production-system", "integration", "migrations", id+".sql"))
		if err != nil {
			t.Fatal(err)
		}
		ddl = append(ddl, string(raw))
	}
	raw, err := os.ReadFile(filepath.Join(repositoryRoot(), "spec", "production-system", "integration", "migrations", "440000.sql"))
	if err != nil {
		t.Fatal(err)
	}
	ddl = append(ddl, `CREATE SCHEMA IF NOT EXISTS "recovery"`, string(raw), `INSERT INTO "recovery".recovery_control(singleton,epoch,region,revision,checkpoint_sha256) VALUES(true,1,'railway-prod',1,repeat('0',64))`)
	for _, statement := range ddl {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("production schema: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.indexer_ingest_watermarks(chain_id,stream_id,raw_complete_height,typed_complete_height) VALUES(125,$1,$2,$2)`, realStream, int64(realAnchor.Height)); err != nil {
		t.Fatal(err)
	}
}

func realReceiptInputs(t *testing.T) runtimedocs.Inputs {
	t.Helper()
	root := repositoryRoot()
	reviewed, err := os.ReadFile(filepath.Join(root, "protocol", "catalog", "reviewed.json"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := os.ReadFile(filepath.Join(root, "contracts", "artifacts", "contracts", "core", "SidioraERC20.sol", "SidioraERC20.json"))
	if err != nil {
		t.Fatal(err)
	}
	var compiled struct {
		DeployedBytecode string `json:"deployedBytecode"`
	}
	if err = json.Unmarshal(artifact, &compiled); err != nil {
		t.Fatal(err)
	}
	code, err := hex.DecodeString(strings.TrimPrefix(compiled.DeployedBytecode, "0x"))
	if err != nil {
		t.Fatal(err)
	}
	return runtimedocs.Inputs{Manifest: deployment.Current(), ReviewedCatalog: reviewed, ERC20RuntimeCode: code, PoolRuntimeHash: common.HexToHash("0xa9b304d6d0aa562f283b45978b84ced8840a9331f8ac946742670b1b02a2872d"), Anchor: realAnchor}
}

// The production ingestion path on chain 125 end to end, against actual
// PostgreSQL and the recorded node traffic of a real market: committed source
// with discovered emitters, reported-hash receipt qualification, canonical
// commit with accounting fills and creation facts, and the typed read tables
// (pools, swaps, pinned-reserve snapshots) every production reader uses.
func TestQualifiedProductionIngestionRealMarketPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	_, pool := canonicaltest.New(t)
	applyProductionSchema(t, ctx, pool)
	url := chainfixture.New(t).Serve(t)

	options, _, err := runtimedocs.ReceiptOptions(realReceiptInputs(t))
	if err != nil {
		t.Fatal(err)
	}
	client, closeClient, err := receipt.NewBoundedRPCClient(ctx, url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	authority, err := receipt.NewRPCQualification(ctx, client, options)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := runtimedocs.CommittedOptions(options)
	if err != nil {
		t.Fatal(err)
	}
	committed.Endpoints[runtimedocs.PrimaryEndpointID] = url
	rpcSource, err := source.NewCommittedSource(ctx, committed)
	if err != nil {
		t.Fatal(err)
	}
	defer rpcSource.Close()

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
	var height atomic.Uint64
	height.Store(realAnchor.Height)
	live := &source.LiveQualifiedSource{RPC: rpcSource, Authority: authority, DurableHeight: height.Load, PriorHash: func(c context.Context, h uint64) (common.Hash, error) {
		if h == realAnchor.Height {
			return realAnchor.Hash, nil
		}
		var hash string
		err := pool.QueryRow(c, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height=$2`, realStream, int64(h)).Scan(&hash)
		return common.HexToHash(hash), err
	}}
	p := New(Deps{Store: st, Decoder: decode.NewDecoder(), EventEmitterAddress: deployment.Current().Roles["EventEmitter"].Address, ChainID: 125, QualifiedBatchSource: live, CanonicalStreamID: realStream, CanonicalOwner: "indexer-test", AccountingPartitions: partitions, Durable: stager, AccountingProjectionVersion: "accounting-market-v1", LegacyProjection: true, Reserves: authority})
	for _, r := range [][2]int64{{33963716, 33963716}, {33963717, 33963733}, {33963734, 33963749}} {
		if err = p.ProcessBatch(ctx, r[0], r[1]); err != nil {
			t.Fatalf("batch %d..%d: %v", r[0], r[1], err)
		}
		height.Store(uint64(r[1]))
	}

	var token, creator, poolID string
	var nft *int64
	if err = pool.QueryRow(ctx, `SELECT token_address, creator, pool_id, nft_id FROM indexer.pools WHERE lower(pool_address)=$1`, realMarketPool).Scan(&token, &creator, &poolID, &nft); err != nil {
		t.Fatalf("legacy pool row: %v", err)
	}
	if strings.ToLower(token) != realMarketToken || strings.ToLower(creator) != "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154" || nft == nil || poolID != poolIDFor(common.HexToAddress(realMarketPool)) {
		t.Fatalf("legacy pool token=%s creator=%s poolId=%s nft=%v", token, creator, poolID, nft)
	}
	var swaps, buys int
	if err = pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE is_buy) FROM indexer.swaps WHERE lower(pool_address)=$1`, realMarketPool).Scan(&swaps, &buys); err != nil || swaps != 2 || buys != 1 {
		t.Fatalf("legacy swaps=%d buys=%d err=%v", swaps, buys, err)
	}
	rows, err := pool.Query(ctx, `SELECT block_number, virtual_reserve, real_reserve, token_reserve, price FROM indexer.pool_state_snapshots WHERE pool_id=$1 ORDER BY block_number`, poolID)
	if err != nil {
		t.Fatal(err)
	}
	snapshots := 0
	for rows.Next() {
		var block int64
		var virtual, real, tokenReserve, price string
		if err = rows.Scan(&block, &virtual, &real, &tokenReserve, &price); err != nil {
			t.Fatal(err)
		}
		hashRow := ""
		if err = pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE height=$1`, block).Scan(&hashRow); err != nil {
			t.Fatal(err)
		}
		pinned, err := authority.PoolReserves(ctx, common.HexToAddress(realMarketPool), common.HexToHash(hashRow))
		if err != nil {
			t.Fatal(err)
		}
		if virtual != pinned.VirtualUSDL.String() || real != pinned.RealUSDL.String() || tokenReserve != pinned.Token.String() || price != pinned.Price.String() || real == "0" {
			t.Fatalf("snapshot at %d differs from pinned reserves", block)
		}
		snapshots++
	}
	rows.Close()
	if snapshots != 2 {
		t.Fatalf("pinned reserve snapshots %d", snapshots)
	}
	var creations, fills int
	var buyHash, watermark string
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM indexer.indexer_canonical_creations WHERE pool=$1), (SELECT count(*) FROM indexer.indexer_accounting_fills), (SELECT block_hash FROM indexer.indexer_block_completeness WHERE height=33963733), (SELECT raw_complete_height::text FROM indexer.indexer_ingest_watermarks)`, realMarketPool).Scan(&creations, &fills, &buyHash, &watermark); err != nil {
		t.Fatal(err)
	}
	if creations != 1 || fills != 2 || watermark != "33963749" {
		t.Fatalf("canonical creations=%d fills=%d watermark=%s", creations, fills, watermark)
	}
	// Block identities are the node-reported hashes, never go-ethereum's
	// recomputed header hash.
	if buyHash != "0x50f4e42d7247aa1665d1aaebc33039c8fc8e493727a864085ba77910dda56f6b" {
		t.Fatalf("completeness stores %s, not the node-reported hash", buyHash)
	}
	head, err := rpcSource.BlockAt(ctx, 33963749)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.RetainCommittedHead(ctx, realStream, int64(head.Height), strings.ToLower(head.Hash.Hex()), head.SourceID); err != nil {
		t.Fatal(err)
	}
	var committedHash string
	if err = pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_committed_heads_v2 WHERE chain_id=125 AND height=33963749`).Scan(&committedHash); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = pool.QueryRow(ctx, `SELECT block_hash FROM indexer.indexer_block_completeness WHERE height=33963749`).Scan(&stored); err != nil || committedHash != stored {
		t.Fatalf("committed head %s, completed block %s (%v)", committedHash, stored, err)
	}
}
