package l16

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/db/production/coordinatortest"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// The recorded chain 125 market (testdata/indexer_real_market): created by
// 0x1dae…b154 through the router at block 33963716 (transaction 0xe200…cbfca),
// bought by the creator at 33963733 and sold at 33963749.
const (
	realMarketToken   = "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	realMarketPool    = "0xce20da50f92d21d8bee497910c782e9aa25b6263"
	realMarketCreator = "0x1dae78b53c9f39930a2ff2b6b7aac12ec75fb154"
	realMarketFactory = "0x3993d004166738d1069b5c4e0a923f7b9d5d8830"
	realMarketStream  = "chain125-canonical"
	realIndexerOwner  = "indexer-test"
	realCreationTx    = "0xe200486fb275ad363101dd09455b23842f1a002b839062732230fd4c908cbfca"
)

// realMarketEnv names the already running local fixture services and the
// chain 125 nodes: the primary (api5, ~100k blocks of state) and the archive
// (api2). Tests never start containers.
func realMarketEnv(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s required (PNL_TEST_POSTGRES_DSN: fixture PostgreSQL that may CREATE DATABASE/ROLE; L16_BASELINE_RPC: chain 125 primary node; L16_SECOND_RPC: chain 125 archive node; PNL_TEST_NATS_URL: fixture JetStream)", name)
	}
	return value
}

type realMarketStage struct {
	admin, runtime *pgxpool.Pool
	indexer        *pgxpool.Pool
	db             coordinatortest.Database
}

// loadRealMarketStage bootstraps the production schema exactly as the
// migration coordinator does (registered migrations, reviewed grants, the
// least-privilege indexer_runtime and the reviewed pnl_runtime grants) and loads one
// recorded indexer stage as the indexer left it. The stage's
// pnl.inventory_l16_baselines row (the indexer test's mirror of pnl's
// attestation) is withheld: pnl must issue its own. The recorded source lease
// is renewed, as the live indexer's heartbeat does.
func loadRealMarketStage(t *testing.T, ctx context.Context, stage string) *realMarketStage {
	t.Helper()
	db := coordinatortest.Bootstrap(t, realMarketEnv(t, "PNL_TEST_POSTGRES_DSN"), "indexer_runtime")
	raw, err := os.ReadFile(filepath.Join("testdata", "indexer_real_market", stage+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var tables map[string]json.RawMessage
	if err = json.Unmarshal(raw, &tables); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	s := &realMarketStage{admin: admin, db: db}
	pending := []string{}
	for name := range tables {
		if name == "pnl.inventory_l16_baselines" {
			continue
		}
		pending = append(pending, name)
	}
	sort.Strings(pending)
	for len(pending) > 0 {
		var failed []string
		var last error
		for _, name := range pending {
			if err = insertRecordedTable(ctx, admin, name, tables[name]); err != nil {
				failed, last = append(failed, name), err
			}
		}
		if len(failed) == len(pending) {
			t.Fatalf("recorded tables %v not loadable: %v", failed, last)
		}
		pending = failed
	}
	if _, err = admin.Exec(ctx, `UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp()+interval '10 minutes',last_heartbeat_at=clock_timestamp() WHERE chain_id=125 AND stream_id=$1`, realMarketStream); err != nil {
		t.Fatal(err)
	}
	for dsn, target := range map[string]**pgxpool.Pool{reviewedPnlRuntime(t, ctx, db, admin): &s.runtime, db.Runtimes["indexer_runtime"].DSN: &s.indexer} {
		pool, e := pgxpool.New(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(pool.Close)
		*target = pool
	}
	return s
}

// reviewedPnlRuntime logs in a least-privilege role holding exactly the
// reviewed pnl_runtime grants (grants.json) that exist in this bootstrap.
// coordinatortest cannot bootstrap pnl_runtime itself: grants.json grants it
// USAGE on sequences migration 180000 never creates (l18_funding_sequence_seq
// and the other l18_*_sequence_seq/revision_seq of tables without a serial),
// so the coordinator's GRANT fails. Those statements, and the ones on schemas
// this indexer-scoped plan does not create (api_stream), are skipped and
// logged; every privilege the inventory baseline path uses is applied as
// reviewed.
func reviewedPnlRuntime(t *testing.T, ctx context.Context, db coordinatortest.Database, admin *pgxpool.Pool) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(coordinatortest.RepositoryRoot(t), "launchpad", "spec", "production-system", "integration", "migrations", "grants.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Roles []production.RoleManifest `json:"roles"`
	}
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var manifest *production.RoleManifest
	for i := range doc.Roles {
		if doc.Roles[i].RuntimeRole == "pnl_runtime" && doc.Roles[i].Service == "core/pnl-tracker" {
			manifest = &doc.Roles[i]
		}
	}
	if manifest == nil {
		t.Fatal("grants.json has no core/pnl-tracker pnl_runtime manifest")
	}
	role := db.Rename("pnl_runtime")
	var secret [24]byte
	if _, err = rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	password := hex.EncodeToString(secret[:])
	var literal string
	if err = admin.QueryRow(ctx, "SELECT quote_literal($1)", password).Scan(&literal); err != nil {
		t.Fatal(err)
	}
	// coordinatortest provisions every reviewed role (NOLOGIN, suffixed) and
	// drops them with the database.
	if _, err = admin.Exec(ctx, "ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD "+literal); err != nil {
		t.Fatal(err)
	}
	scoped := *manifest
	scoped.RuntimeRole, scoped.OwnerReadGrants, scoped.SchemaCapabilities = role, nil, nil
	statements, err := scoped.GrantSQL()
	if err != nil {
		t.Fatal(err)
	}
	skipped := []string{}
	for _, statement := range statements {
		if _, err = admin.Exec(ctx, statement); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && (pgErr.Code == "42P01" || pgErr.Code == "3F000" || pgErr.Code == "42883") {
				skipped = append(skipped, pgErr.Message)
				continue
			}
			t.Fatalf("reviewed pnl_runtime grant %q: %v", statement, err)
		}
	}
	t.Logf("reviewed pnl_runtime grants applied; %d reference objects absent here: %s", len(skipped), strings.Join(skipped, "; "))
	u, err := url.Parse(db.AdminDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.UserPassword(role, password)
	return u.String()
}

func insertRecordedTable(ctx context.Context, admin *pgxpool.Pool, name string, rows json.RawMessage) error {
	parts := strings.SplitN(name, ".", 2)
	var keys []map[string]json.RawMessage
	if err := json.Unmarshal(rows, &keys); err != nil || len(keys) == 0 {
		return fmt.Errorf("%s: %v", name, err)
	}
	cols, err := admin.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema=$1 AND table_name=$2 AND is_generated='NEVER' ORDER BY ordinal_position`, parts[0], parts[1])
	if err != nil {
		return err
	}
	selected := []string{}
	for cols.Next() {
		var c string
		if err = cols.Scan(&c); err != nil {
			cols.Close()
			return err
		}
		if _, ok := keys[0][c]; ok {
			selected = append(selected, pgx.Identifier{c}.Sanitize())
		}
	}
	cols.Close()
	if len(selected) == 0 {
		return fmt.Errorf("%s: table absent from the production schema", name)
	}
	table := pgx.Identifier{parts[0], parts[1]}.Sanitize()
	list := strings.Join(selected, ",")
	_, err = admin.Exec(ctx, `INSERT INTO `+table+`(`+list+`) OVERRIDING SYSTEM VALUE SELECT `+list+` FROM json_populate_recordset(NULL::`+table+`,$1::json)`, string(rows))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

type realMarketAuthorities struct {
	primary, archive *BaselineAuthority
	ahead            *BaselineAhead
	guard            *queue.EpochGuard
}

// realMarketBaselineRuntime wires the production baseline runtime as app.New
// does: one authority per chain 125 node bound to the delivery document's
// identity (pnl/g1/pnl-1/inventory-v1) and the recovery epoch guard, and the
// ahead-of-fill issuer over both.
func realMarketBaselineRuntime(t *testing.T, ctx context.Context, s *realMarketStage) realMarketAuthorities {
	t.Helper()
	guard, err := queue.NewEpochGuard("recovery", coordinatortest.Epoch, coordinatortest.Region)
	if err != nil {
		t.Fatal(err)
	}
	primaryURL, archiveURL := realMarketEnv(t, "L16_BASELINE_RPC"), realMarketEnv(t, "L16_SECOND_RPC")
	if primaryURL == archiveURL {
		t.Fatal("distinct primary and archive nodes required")
	}
	out := realMarketAuthorities{guard: guard}
	for _, bind := range []struct {
		url    string
		target **BaselineAuthority
	}{{primaryURL, &out.primary}, {archiveURL, &out.archive}} {
		rpc, e := ethclient.DialContext(ctx, bind.url)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(rpc.Close)
		authority, e := NewBaselineAuthorityWithEpochGuard(s.runtime, rpc, "pnl", realMarketStream, "pnl", "g1", "pnl-1", "inventory-v1", guard.Assert)
		if e != nil {
			t.Fatal(e)
		}
		*bind.target = authority
	}
	if out.ahead, err = NewBaselineAhead(out.primary, out.archive, 32); err != nil {
		t.Fatal(err)
	}
	return out
}

// indexerVisibleBaseline is the read core/indexer performs before it
// publishes an inventory frame (processor/creation.go
// qualifiedInventoryEnrichment), run as the least-privilege indexer_runtime.
func indexerVisibleBaseline(t *testing.T, ctx context.Context, s *realMarketStage, wallet, owner string, fence int64) (string, bool) {
	t.Helper()
	var digest, creationHash, movementDigest, observed string
	var creationHeight int64
	err := s.indexer.QueryRow(ctx, `SELECT baseline_digest,creation_height,creation_block_hash,movement_digest,observed_balance_raw FROM pnl.inventory_l16_baselines_v1 WHERE projection_generation=$1 AND wallet=$2 AND token=$3 AND source_stream=$4 AND source_owner=$5 AND source_epoch=$6 ORDER BY baseline_digest LIMIT 1`, "g1", wallet, realMarketToken, realMarketStream, owner, fence).Scan(&digest, &creationHeight, &creationHash, &movementDigest, &observed)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("indexer baseline read: %v", err)
	}
	if creationHeight != 33963716 || creationHash != "0x81a18d822e327fbcbfe2be91b7f666bfa043016cb0e3f094c5bbabe91f9d192b" || observed != "0" || len(movementDigest) != 64 {
		t.Fatalf("baseline facts height=%d hash=%s observed=%s", creationHeight, creationHash, observed)
	}
	return digest, true
}

// renewIndexerLease moves the recorded source lease to a new owner and fence
// with the statement the indexer's acquisition runs after the prior lease
// expired (core/indexer store/canonical.go): every later canonical batch is
// bound to (owner, fence) and reads only baselines bound to it.
func renewIndexerLease(t *testing.T, ctx context.Context, s *realMarketStage, owner string) int64 {
	t.Helper()
	var fence int64
	if err := s.admin.QueryRow(ctx, `UPDATE indexer.indexer_ingest_leases SET owner_instance=$2,fence_token=fence_token+1,expires_at=clock_timestamp()+interval '10 minutes',last_heartbeat_at=clock_timestamp() WHERE chain_id=125 AND stream_id=$1 RETURNING fence_token`, realMarketStream, owner).Scan(&fence); err != nil {
		t.Fatal(err)
	}
	return fence
}

func recordedCreationMovements(t *testing.T, ctx context.Context, s *realMarketStage) []movement {
	t.Helper()
	rows, err := s.runtime.Query(ctx, `SELECT transaction_hash,log_index,height,transaction_index,block_hash,from_address,to_address,amount_raw FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND height=33963716 ORDER BY height,transaction_index,log_index`, realMarketToken)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []movement{}
	for rows.Next() {
		var m movement
		var logIndex, txIndex int64
		if err = rows.Scan(&m.TransactionHash, &logIndex, &m.Height, &txIndex, &m.BlockHash, &m.From, &m.To, &m.Amount); err != nil {
			t.Fatal(err)
		}
		m.LogIndex, m.TransactionIndex = uint32(logIndex), uint32(txIndex)
		out = append(out, m)
	}
	return out
}

// TestFactoryCreationSupplyMatchesDeployedCreationPath checks the creation
// rule against the indexed creation-block transfers of the real market: the
// constructor mint to the deployed factory followed by the factory's seed of
// the pool is accepted; the mint straight to the pool (the rule Capture used
// to require, which no factory-created market satisfies), a different
// factory, a partial seed or an extra creation transfer are refused.
func TestFactoryCreationSupplyMatchesDeployedCreationPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s := loadRealMarketStage(t, ctx, "creation")
	recorded := recordedCreationMovements(t, ctx, s)
	if len(recorded) != 2 || recorded[0].LogIndex != 2 || recorded[1].LogIndex != 10 || recorded[0].TransactionHash != realCreationTx || recorded[1].TransactionHash != realCreationTx {
		t.Fatalf("recorded creation transfers %+v", recorded)
	}
	supply, err := factoryCreationSupply(recorded, realCreationTx, realMarketFactory, realMarketPool)
	if err != nil || supply.String() != "1000000000000000" {
		t.Fatalf("deployed creation path refused: %v %v", supply, err)
	}
	authority := realMarketBaselineRuntime(t, ctx, s).primary
	if authority.factory != realMarketFactory {
		t.Fatalf("authority factory %s, deployment manifest SidioraFactory %s", authority.factory, realMarketFactory)
	}
	clone := func() []movement { return append([]movement(nil), recorded...) }
	direct := clone()[:1]
	direct[0].To = realMarketPool
	cases := map[string]struct {
		movements []movement
		factory   string
	}{
		"mint straight to pool":   {direct, realMarketFactory},
		"other factory":           {clone(), "0x" + strings.Repeat("7", 40)},
		"seed differs from mint":  {func() []movement { m := clone(); m[1].Amount = "999999999999999"; return m }(), realMarketFactory},
		"seed to another address": {func() []movement { m := clone(); m[1].To = realMarketCreator; return m }(), realMarketFactory},
		"seed before mint":        {[]movement{recorded[1], recorded[0]}, realMarketFactory},
		"extra creation transfer": {func() []movement {
			m := clone()
			extra := m[1]
			extra.LogIndex, extra.From, extra.To, extra.Amount = 11, realMarketPool, realMarketCreator, "1"
			return append(m, extra)
		}(), realMarketFactory},
	}
	for name, c := range cases {
		if _, err := factoryCreationSupply(c.movements, realCreationTx, c.factory, realMarketPool); !errors.Is(err, ErrBaselineUnavailable) {
			t.Fatalf("%s accepted (%v)", name, err)
		}
	}
}

// TestBaselineAheadIssuesCreatorBaselineBeforeFirstFillPostgres: with only
// the creation block committed by the real indexer, pnl's production
// ahead-of-fill issuer (least-privilege pnl_runtime, chain 125 nodes) captures
// the creator's fresh zero baseline through the factory creation path, bound
// to the indexer's current lease; the indexer (as indexer_runtime) reads it
// with its own query, and it is byte-for-byte the attestation the indexer's
// recorded inventory frames are bound to. A new indexer fence rebinds it.
func TestBaselineAheadIssuesCreatorBaselineBeforeFirstFillPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	s := loadRealMarketStage(t, ctx, "creation")
	traded := loadRecordedMirror(t)
	runtime := realMarketBaselineRuntime(t, ctx, s)
	if _, ok := indexerVisibleBaseline(t, ctx, s, realMarketCreator, realIndexerOwner, 1); ok {
		t.Fatal("baseline present before pnl issued it")
	}
	issued, err := runtime.ahead.RunOnce(ctx)
	if err != nil || issued != 1 {
		t.Fatalf("ahead issued %d (%v)", issued, err)
	}
	digest, ok := indexerVisibleBaseline(t, ctx, s, realMarketCreator, realIndexerOwner, 1)
	if out := os.Getenv("L16_RECORD_BASELINE_OUT"); out != "" {
		// Recording input for testdata/indexer_real_market: the indexer
		// pipeline is re-run with exactly this pnl-issued row.
		var row []byte
		if err = s.admin.QueryRow(ctx, `SELECT row_to_json(b)::text FROM pnl.inventory_l16_baselines b WHERE baseline_digest=$1`, digest).Scan(&row); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(out, row, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var attestation []byte
	if err = s.runtime.QueryRow(ctx, `SELECT attestation FROM pnl.inventory_l16_baselines WHERE baseline_digest=$1`, digest).Scan(&attestation); err != nil || !ok || digest != traded.digest || string(attestation) != string(traded.attestation) {
		t.Fatalf("pnl baseline %s (visible=%v, %v), indexer frames bound to %s:\npnl     %s\nindexer %s", digest, ok, err, traded.digest, attestation, traded.attestation)
	}
	if issued, err = runtime.ahead.RunOnce(ctx); err != nil || issued != 0 {
		t.Fatalf("second tick issued %d (%v)", issued, err)
	}
	fence := renewIndexerLease(t, ctx, s, "indexer-restarted")
	if _, ok = indexerVisibleBaseline(t, ctx, s, realMarketCreator, "indexer-restarted", fence); ok {
		t.Fatal("new fence sees a baseline before rebinding")
	}
	if err = runtime.ahead.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	rebound, ok := indexerVisibleBaseline(t, ctx, s, realMarketCreator, "indexer-restarted", fence)
	if !ok || rebound == digest {
		t.Fatalf("baseline not rebound to fence %d: %q", fence, rebound)
	}
	var count int
	if err = s.runtime.QueryRow(ctx, `SELECT count(*) FROM pnl.inventory_l16_baselines WHERE partition=$1`, "125:"+realMarketCreator+":"+realMarketToken).Scan(&count); err != nil || count != 2 {
		t.Fatalf("baselines %d (%v)", count, err)
	}
	// The factory and the pool moved the token at creation: no fresh baseline.
	for _, wallet := range []string{realMarketFactory, realMarketPool} {
		if _, err = runtime.primary.Capture(ctx, wallet, realMarketToken); !errors.Is(err, ErrBaselineUnavailable) {
			t.Fatalf("%s: %v", wallet, err)
		}
	}
}

type recordedMirror struct {
	digest      string
	attestation []byte
}

func loadRecordedMirror(t *testing.T) recordedMirror {
	t.Helper()
	return loadRecordedStageMirror(t, "traded")
}

// loadRecordedStageMirror reads the pnl-issued baseline row a recorded indexer
// stage's inventory frames are bound to.
func loadRecordedStageMirror(t *testing.T, stage string) recordedMirror {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "indexer_real_market", stage+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var tables struct {
		Baselines []struct {
			Digest      string `json:"baseline_digest"`
			Attestation string `json:"attestation"`
		} `json:"pnl.inventory_l16_baselines"`
	}
	if err = json.Unmarshal(raw, &tables); err != nil || len(tables.Baselines) != 1 || !strings.HasPrefix(tables.Baselines[0].Attestation, `\x`) {
		t.Fatalf("recorded mirror (%v)", err)
	}
	decoded := make([]byte, 0, len(tables.Baselines[0].Attestation)/2)
	hexed := tables.Baselines[0].Attestation[2:]
	for i := 0; i+1 < len(hexed); i += 2 {
		var b byte
		if _, err = fmt.Sscanf(hexed[i:i+2], "%02x", &b); err != nil {
			t.Fatal(err)
		}
		decoded = append(decoded, b)
	}
	return recordedMirror{digest: tables.Baselines[0].Digest, attestation: decoded}
}

// TestIndexerInventoryFramesConsumedByPnlPostgres joins the real indexer's
// accounting.inventory.v1 envelopes for the recorded market with pnl's
// production consumer: the envelopes are published to a JetStream stream on
// the fixture broker and consumed through natsdriver by the epoch-guarded
// AccountingConsumer as the reviewed pnl_runtime grants. pnl issues its own baseline (the
// recorded mirror is withheld), verifies each frame's fresh-same-owner
// evidence against canonical movements and chain 125, and advances the
// partition 1 -> 2 -> next 3. After an indexer fence change the open partition
// is rebound by the ahead issuer before any further fill.
func TestIndexerInventoryFramesConsumedByPnlPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	s := loadRealMarketStage(t, ctx, "traded")
	runtime := realMarketBaselineRuntime(t, ctx, s)
	partition := "125:" + realMarketCreator + ":" + realMarketToken

	// The creator already traded and pnl has no partition: the issuer must
	// not open the partition mid-history.
	if issued, err := runtime.ahead.RunOnce(ctx); err != nil || issued != 0 {
		t.Fatalf("ahead issued %d mid-history (%v)", issued, err)
	}

	consumeRecordedInventoryFrames(t, ctx, s, runtime, partition)
	var err error
	var uses, commits int
	var baselineDigest string
	if err = s.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM pnl.inventory_l16_baseline_uses WHERE partition=$1),(SELECT count(*) FROM pnl.delivery_partition_commits WHERE partition_key=$1),(SELECT baseline_digest FROM pnl.inventory_l16_baselines WHERE partition=$1 AND source_owner=$2 AND source_epoch=1)`, partition, realIndexerOwner).Scan(&uses, &commits, &baselineDigest); err != nil || uses != 2 || commits != 2 {
		t.Fatalf("baseline uses=%d commits=%d (%v)", uses, commits, err)
	}
	if mirror := loadRecordedMirror(t); baselineDigest != mirror.digest {
		t.Fatalf("pnl baseline %s, frames bound to %s", baselineDigest, mirror.digest)
	}
	var stateRaw []byte
	if err = s.admin.QueryRow(ctx, `SELECT state FROM pnl.inventory_l16 WHERE partition LIKE '%'||$1 ORDER BY revision DESC LIMIT 1`, ":"+partition).Scan(&stateRaw); err != nil {
		t.Fatal(err)
	}
	state, err := DecodeInventoryState(stateRaw)
	if err != nil {
		t.Fatal(err)
	}
	var bought, sold string
	if err = s.admin.QueryRow(ctx, `SELECT (SELECT payload->>'tokenAmountRaw' FROM indexer.indexer_accounting_fills WHERE payload->>'side'='buy'),(SELECT payload->>'tokenAmountRaw' FROM indexer.indexer_accounting_fills WHERE payload->>'side'='sell')`).Scan(&bought, &sold); err != nil {
		t.Fatal(err)
	}
	b, _ := accounting.Uint(bought)
	q, _ := accounting.Uint(sold)
	if state.Known != new(big.Int).Sub(b, q).String() || state.Unknown != "0" || state.IncompleteRealized {
		t.Fatalf("inventory after buy %s and sell %s: %s", bought, sold, stateRaw)
	}
	t.Logf("pnl inventory after the real buy and sell: %s", stateRaw)

	// Indexer restart: the open partition is rebound to the new fence before
	// the indexer commits the wallet's next fill.
	fence := renewIndexerLease(t, ctx, s, "indexer-restarted")
	if err = runtime.ahead.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := indexerVisibleBaseline(t, ctx, s, realMarketCreator, "indexer-restarted", fence); !ok {
		t.Fatalf("open partition not rebound to fence %d", fence)
	}
}

// consumeRecordedInventoryFrames publishes the recorded indexer's
// accounting.inventory.v1 envelopes to a JetStream stream on the fixture
// broker and runs pnl's production epoch-guarded AccountingConsumer (as the
// reviewed pnl_runtime) until partition reaches next_sequence 3.
func consumeRecordedInventoryFrames(t *testing.T, ctx context.Context, s *realMarketStage, runtime realMarketAuthorities, partition string) {
	t.Helper()
	rows, err := s.admin.Query(ctx, `SELECT event_id,subject,envelope FROM indexer.delivery_outbox WHERE subject=$1 ORDER BY created_at,event_id`, accounting.InventorySubject)
	if err != nil {
		t.Fatal(err)
	}
	type frame struct {
		id, subject string
		wire        []byte
	}
	frames := []frame{}
	for rows.Next() {
		var f frame
		if err = rows.Scan(&f.id, &f.subject, &f.wire); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, f)
	}
	rows.Close()
	if len(frames) != 2 {
		t.Fatalf("recorded inventory frames %d", len(frames))
	}

	nc, err := nats.Connect(realMarketEnv(t, "PNL_TEST_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream := fmt.Sprintf("PNL_L16_REAL_%d", time.Now().UnixNano())
	if _, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: stream, Subjects: []string{accounting.InventorySubject}, Storage: jetstream.FileStorage}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = js.DeleteStream(cleanup, stream)
	})
	cfg := natsdriver.ConsumerConfig{Stream: stream, Durable: "pnl_inventory", Subject: accounting.InventorySubject, Batch: 16, MaxAckPending: 64, MaxWaiting: 4, MaxDeliver: 100, FetchWait: 250 * time.Millisecond, AckWait: 30 * time.Second}
	broker, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = js.CreateOrUpdateConsumer(ctx, stream, broker); err != nil {
		t.Fatal(err)
	}
	publisher, err := natsdriver.NewPublisher(nc, stream)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if _, err = publisher.Publish(ctx, f.subject, f.id, f.wire); err != nil {
			t.Fatal(err)
		}
	}
	transport, err := natsdriver.NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(transport.Close)

	// The api_stream portfolio append (NewAccountingConsumerWithRuntime) is
	// not in this indexer-scoped schema plan; the inventory verification,
	// ordered partition and owned-leg effect are the production ones.
	consumer, err := NewAccountingConsumerWithEpochGuard(s.runtime, "pnl", "pnl", "g1", "pnl-1", "inventory-v1", runtime.primary, runtime.guard.Assert)
	if err != nil {
		t.Fatal(err)
	}
	running, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- consumer.Run(running, transport) }()
	var next int64
	for deadline := time.Now().Add(2 * time.Minute); time.Now().Before(deadline); {
		err = s.runtime.QueryRow(ctx, `SELECT next_sequence FROM pnl.delivery_partition_offsets WHERE consumer_id='pnl' AND projection_generation='g1' AND partition_kind='inventory' AND partition_key=$1`, partition).Scan(&next)
		if err == nil && next == 3 {
			break
		}
		select {
		case e := <-done:
			t.Fatalf("pnl consumer stopped: %v", e)
		case <-time.After(200 * time.Millisecond):
		}
	}
	stop()
	if e := <-done; e != nil && !errors.Is(e, context.Canceled) {
		t.Fatalf("pnl consumer: %v", e)
	}
	if next != 3 {
		var quarantined int
		_ = s.admin.QueryRow(ctx, `SELECT count(*) FROM pnl.delivery_ordered_quarantine`).Scan(&quarantined)
		t.Fatalf("partition next_sequence=%d quarantined=%d (%v)", next, quarantined, err)
	}
}

// TestInventoryFramesInFlightAcrossFenceChangePostgres: the recorded frames
// were built by the indexer against the baseline pnl issued for source lease
// (indexer-test, 1) (the recorded row, inserted byte for byte). The indexer's
// lease then moves to fence 2 before pnl consumes them: pnl bootstraps the
// partition from a fresh fence 2 baseline, and still accepts both in-flight
// frames because each names, through its evidence digest, the fence 1 row it
// was built against (an epoch not beyond the current fence); the baseline
// uses record that row.
func TestInventoryFramesInFlightAcrossFenceChangePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	s := loadRealMarketStage(t, ctx, "traded")
	raw, err := os.ReadFile(filepath.Join("testdata", "indexer_real_market", "traded.json"))
	if err != nil {
		t.Fatal(err)
	}
	var tables map[string]json.RawMessage
	if err = json.Unmarshal(raw, &tables); err != nil {
		t.Fatal(err)
	}
	if err = insertRecordedTable(ctx, s.admin, "pnl.inventory_l16_baselines", tables["pnl.inventory_l16_baselines"]); err != nil {
		t.Fatal(err)
	}
	mirror := loadRecordedMirror(t)
	fence := renewIndexerLease(t, ctx, s, "indexer-restarted")
	if fence != 2 {
		t.Fatalf("fence %d", fence)
	}
	runtime := realMarketBaselineRuntime(t, ctx, s)
	partition := "125:" + realMarketCreator + ":" + realMarketToken
	consumeRecordedInventoryFrames(t, ctx, s, runtime, partition)
	var uses, named int
	var current string
	if err = s.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM pnl.inventory_l16_baseline_uses WHERE partition=$1),(SELECT count(*) FROM pnl.inventory_l16_baseline_uses WHERE partition=$1 AND baseline_digest=$2),(SELECT baseline_digest FROM pnl.inventory_l16_baselines WHERE partition=$1 AND source_owner='indexer-restarted' AND source_epoch=$3)`, partition, mirror.digest, fence).Scan(&uses, &named, &current); err != nil {
		t.Fatal(err)
	}
	if uses != 2 || named != 2 || current == mirror.digest {
		t.Fatalf("baseline uses=%d naming the fence 1 row=%d, fence 2 baseline %s", uses, named, current)
	}
}

// TestBaselineAheadIssuesBaselineForDeferredFirstFillPostgres: the real
// indexer committed the creator's buy (33963733) before pnl issued any
// baseline (testdata deferred.json: the fill has its market frame, no
// inventory publication and no degradation decision, i.e. the indexer holds
// it for processor.InventoryBaselineWaitBlocks). The wallet has moved the
// token, so neither the creation nor the enrollee source selects it; the
// pending-fill source does, first, only while the fill is inside
// BaselineAheadFillWindowBlocks of the watermark. pnl's production issuer
// (least-privilege pnl_runtime, chain 125 nodes) captures the baseline once,
// bound to the indexer's lease; it is the row the indexer then used: the next
// real canonical commit (deferred_published.json, recorded with exactly this
// row) published the held buy and the sell, and pnl's production consumer
// verifies and applies both frames from sequence 1.
func TestBaselineAheadIssuesBaselineForDeferredFirstFillPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	s := loadRealMarketStage(t, ctx, "deferred")
	partition := "125:" + realMarketCreator + ":" + realMarketToken
	var fills, inventory, moved int
	var fillHeight, watermark int64
	if err := s.runtime.QueryRow(ctx, `SELECT (SELECT count(*) FROM indexer.indexer_accounting_fills_v1 WHERE payload->'attribution'->>'payer'=$1 AND payload->'attribution'->>'recipient'=$1 AND payload->'token'->>'address'=$2),(SELECT max((payload->'position'->>'height')::bigint) FROM indexer.indexer_accounting_fills_v1),(SELECT count(*) FROM indexer.indexer_accounting_publications_v1 WHERE payload->>'facet'='inventory'),(SELECT count(*) FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$2 AND (from_address=$1 OR to_address=$1)),(SELECT LEAST(raw_complete_height,typed_complete_height) FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$3)`, realMarketCreator, realMarketToken, realMarketStream).Scan(&fills, &fillHeight, &inventory, &moved, &watermark); err != nil {
		t.Fatal(err)
	}
	if fills != 1 || fillHeight != 33963733 || inventory != 0 || moved == 0 || watermark != 33963733 {
		t.Fatalf("deferred stage fills=%d at %d inventory publications=%d creator movements=%d watermark=%d", fills, fillHeight, inventory, moved, watermark)
	}
	var degraded int
	if err := s.admin.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_capability_degradation`).Scan(&degraded); err != nil || degraded != 0 {
		t.Fatalf("held fill already decided: %d (%v)", degraded, err)
	}
	runtime := realMarketBaselineRuntime(t, ctx, s)

	// The pending-fill window mirrors the indexer's deferral bound: the
	// commit that reaches fill+300 takes the fill's last decision.
	setWatermark := func(h int64) {
		t.Helper()
		if _, err := s.admin.Exec(ctx, `UPDATE indexer.indexer_ingest_watermarks SET raw_complete_height=$2,typed_complete_height=$2 WHERE chain_id=125 AND stream_id=$1`, realMarketStream, h); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		head int64
		want int
	}{{fillHeight + BaselineAheadFillWindowBlocks, 0}, {fillHeight + BaselineAheadFillWindowBlocks - 1, 1}, {fillHeight, 1}} {
		setWatermark(c.head)
		pending, err := runtime.ahead.candidates(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(pending) != c.want || c.want == 1 && (pending[0].wallet != realMarketCreator || pending[0].token != realMarketToken || pending[0].priority != baselinePriorityPendingFill) {
			t.Fatalf("watermark %d (fill %d): candidates %+v", c.head, fillHeight, pending)
		}
	}

	issued, err := runtime.ahead.RunOnce(ctx)
	if err != nil || issued != 1 {
		t.Fatalf("ahead issued %d (%v)", issued, err)
	}
	digest, ok := indexerVisibleBaseline(t, ctx, s, realMarketCreator, realIndexerOwner, 1)
	if !ok {
		t.Fatal("indexer cannot read the issued baseline")
	}
	var row, attestation []byte
	if err = s.admin.QueryRow(ctx, `SELECT row_to_json(b)::text,attestation FROM pnl.inventory_l16_baselines b WHERE baseline_digest=$1`, digest).Scan(&row, &attestation); err != nil {
		t.Fatal(err)
	}
	if out := os.Getenv("L16_RECORD_BASELINE_OUT"); out != "" {
		// Recording input for deferred_published.json.
		if err = os.WriteFile(out, row, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if issued, err = runtime.ahead.RunOnce(ctx); err != nil || issued != 0 {
		t.Fatalf("second tick issued %d (%v)", issued, err)
	}
	var count int
	if err = s.runtime.QueryRow(ctx, `SELECT count(*) FROM pnl.inventory_l16_baselines WHERE partition=$1`, partition).Scan(&count); err != nil || count != 1 {
		t.Fatalf("baselines %d (%v)", count, err)
	}

	// The indexer's next commit released the held buy against this row.
	published := loadRecordedStageMirror(t, "deferred_published")
	if published.digest != digest || string(published.attestation) != string(attestation) {
		t.Fatalf("pnl baseline %s, released frames bound to %s:\npnl     %s\nindexer %s", digest, published.digest, attestation, published.attestation)
	}
	p := loadRealMarketStage(t, ctx, "deferred_published")
	var held int
	if err = p.admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM indexer.indexer_ingest_outbox o JOIN indexer.indexer_accounting_fills f ON f.chain_id=o.chain_id AND f.transaction_hash=o.transaction_hash AND f.log_index=o.log_index WHERE o.event_kind=$1 AND (f.payload->'position'->>'height')::bigint=$2),(SELECT count(*) FROM indexer.indexer_capability_degradation)`, accounting.InventorySubject, fillHeight).Scan(&held, &degraded); err != nil || held != 1 || degraded != 0 {
		t.Fatalf("held buy inventory facets %d, degradations %d (%v)", held, degraded, err)
	}
	released := realMarketBaselineRuntime(t, ctx, p)
	if issued, err = released.ahead.RunOnce(ctx); err != nil || issued != 0 {
		t.Fatalf("ahead issued %d after every fill was published (%v)", issued, err)
	}
	consumeRecordedInventoryFrames(t, ctx, p, released, partition)
	var uses int
	if err = p.admin.QueryRow(ctx, `SELECT count(*) FROM pnl.inventory_l16_baseline_uses WHERE partition=$1 AND baseline_digest=$2`, partition, digest).Scan(&uses); err != nil || uses != 2 {
		t.Fatalf("baseline uses naming %s: %d (%v)", digest, uses, err)
	}
}
