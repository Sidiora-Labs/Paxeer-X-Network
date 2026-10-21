package l16

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/jackc/pgx/v5/pgxpool"
)

func liveRequired(t *testing.T, name string) string {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		t.Fatalf("%s required for real PostgreSQL/committed Anvil/identity provider qualification", name)
	}
	return value
}

func liveProductionFixture(t *testing.T, prefix string) (context.Context, *LiveTransferWorker, auth.IdentityPrincipal) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	pool, e := pgxpool.New(ctx, liveRequired(t, "L16_TEST_DSN"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	rpc, e := ethclient.DialContext(ctx, liveRequired(t, "L16_BASELINE_RPC"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(rpc.Close)
	secondURL := liveRequired(t, "L16_SECOND_RPC")
	if secondURL == os.Getenv("L16_BASELINE_RPC") {
		t.Fatal("independent second node endpoint required")
	}
	second, e := ethclient.DialContext(ctx, secondURL)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(second.Close)
	epoch, e := strconv.ParseInt(liveRequired(t, "L16_RECOVERY_EPOCH"), 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	guard, e := queue.NewEpochGuard("recovery", epoch, liveRequired(t, "L16_RECOVERY_REGION"))
	if e != nil {
		t.Fatal(e)
	}
	schema := liveRequired(t, "L16_BASELINE_SCHEMA")
	authority, e := NewBaselineAuthorityWithEpochGuard(pool, rpc, schema, liveRequired(t, "L16_BASELINE_STREAM"), "live_pnl", liveRequired(t, "L16_BASELINE_GENERATION"), liveRequired(t, "L16_BASELINE_OWNER"), "inventory-v1", guard.Assert)
	if e != nil {
		t.Fatal(e)
	}
	outbox, e := delivery.NewStoreWithEpochGuard(pool, schema, delivery.Policy{Subjects: map[string]bool{RebuildSubject: true, "portfolio.snapshot.v2": true}, EventTypes: map[string]bool{RebuildSubject: true, "portfolio.snapshot.v2": true}}, guard.Assert)
	if e != nil {
		t.Fatal(e)
	}
	service, e := NewService(authority, second, [2]string{liveRequired(t, "L16_NODE_ONE_ID"), liveRequired(t, "L16_NODE_TWO_ID")}, liveRequired(t, "L16_RELEASE_DIGEST"), outbox)
	if e != nil {
		t.Fatal(e)
	}
	identity := auth.HTTPIdentityAuthority{Endpoint: liveRequired(t, "L16_IDENTITY_URL"), ServiceToken: liveRequired(t, "L16_IDENTITY_SERVICE_TOKEN"), Client: &http.Client{Timeout: 2 * time.Second}}
	worker, e := NewLiveTransferWorker(service, l17.Repository{Pool: pool}, identity, 1)
	if e != nil {
		t.Fatal(e)
	}
	request, e := http.NewRequestWithContext(ctx, "GET", "http://localhost/v3/me/portfolio", nil)
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set("Authorization", "Bearer "+liveRequired(t, prefix+"OWNER_ACCESS_TOKEN"))
	principal, e := identity.AuthenticateRequest(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	if principal.Wallet != liveRequired(t, prefix+"WALLET") {
		t.Fatal("real identity wallet does not own chain fixture")
	}
	return ctx, worker, principal
}

func TestLiveCanonicalExternalGiftPublishesAtomicUnknownBasis(t *testing.T) {
	ctx, worker, p := liveProductionFixture(t, "L16_GIFT_")
	summary, e := worker.RebuildOwned(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	token := liveRequired(t, "L16_GIFT_TOKEN")
	a := worker.service.authority
	var positionRaw []byte
	if e = a.pool.QueryRow(ctx, `SELECT payload FROM `+a.table("portfolio_snapshot_rows")+` WHERE snapshot_id=$1 AND payload->>'tokenAddress'=$2`, summary.SnapshotID, token).Scan(&positionRaw); e != nil {
		t.Fatal(e)
	}
	var position l17.Position
	if e = json.Unmarshal(positionRaw, &position); e != nil {
		t.Fatal(e)
	}
	if position.BalanceRaw != "5" || position.KnownQuantityRaw != "0" || position.UnknownQuantityRaw != "5" || position.RemainingCostUsdlRaw != nil || position.KnownRemainingCostUsdlRaw != "0" {
		t.Fatal(position)
	}
	var history, commits int
	var notice []byte
	var height int64
	var blockHash string
	if e = a.pool.QueryRow(ctx, `SELECT count(*) FROM `+a.table("portfolio_history_rows")+` WHERE snapshot_id=$1`, summary.SnapshotID).Scan(&history); e != nil || history != 1 {
		t.Fatal(history, e)
	}
	if e = a.pool.QueryRow(ctx, `SELECT count(*) FROM `+a.table("inventory_l16_live_commits_v1")+` WHERE snapshot_id=$1`, summary.SnapshotID).Scan(&commits); e != nil || commits != 1 {
		t.Fatal(commits, e)
	}
	if e = a.pool.QueryRow(ctx, `SELECT accepted_height,accepted_hash FROM `+a.table("inventory_l16_live_owners_v1")+` WHERE wallet=$1`, p.Wallet).Scan(&height, &blockHash); e != nil || strconv.FormatInt(height, 10) != summary.Watermark.Height || blockHash != summary.Watermark.BlockHash {
		t.Fatal(height, blockHash, e)
	}
	if e = a.pool.QueryRow(ctx, `SELECT envelope FROM `+a.table("delivery_outbox")+` WHERE subject='portfolio.snapshot.v2' AND convert_from(envelope,'UTF8')::jsonb->'payload'->>'snapshotId'=$1`, summary.SnapshotID).Scan(&notice); e != nil {
		t.Fatal(e)
	}
	var envelope delivery.Envelope
	if e = json.Unmarshal(notice, &envelope); e != nil {
		t.Fatal(e)
	}
	var actual l17.Notice
	if e = json.Unmarshal(envelope.Payload, &actual); e != nil || actual.SnapshotID != summary.SnapshotID || actual.Wallet != p.Wallet || actual.Revision != summary.Watermark.ProjectionRevision {
		t.Fatal(actual, e)
	}
	duplicate, e := worker.RebuildOwned(ctx, p)
	if e != nil || duplicate.SnapshotID != summary.SnapshotID {
		t.Fatal(duplicate, e)
	}
	altered := p
	altered.SessionID = "revoked-unissued-session"
	if e = worker.EnqueueOwned(ctx, altered); e == nil {
		t.Fatal("unissued session enrolled without current identity authority")
	}
}

func TestLiveDenseCanonicalTransfersUseCommittedReceiptPages(t *testing.T) {
	ctx, worker, p := liveProductionFixture(t, "L16_DENSE_")
	summary, e := worker.RebuildOwned(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	expected, e := strconv.Atoi(liveRequired(t, "L16_DENSE_EXPECTED_HISTORY_COUNT"))
	if e != nil || expected <= 10000 {
		t.Fatal("dense fixture must contain more than10000 genuine canonical token movements", e)
	}
	a := worker.service.authority
	var count int
	if e = a.pool.QueryRow(ctx, `SELECT count(*) FROM `+a.table("portfolio_history_rows")+` WHERE snapshot_id=$1`, summary.SnapshotID).Scan(&count); e != nil || count != expected {
		t.Fatal(count, expected, e)
	}
	var checkpointRaw []byte
	if e = a.pool.QueryRow(ctx, `SELECT c.payload FROM `+a.table("inventory_l16_checkpoints_v2")+` c JOIN `+a.table("inventory_l16_active_v2")+` active USING(generation,partition,snapshot_digest) WHERE active.partition=$1 ORDER BY c.day DESC LIMIT 1`, "125:"+p.Wallet+":"+liveRequired(t, "L16_DENSE_TOKEN")).Scan(&checkpointRaw); e != nil {
		t.Fatal(e)
	}
	var payload struct {
		Checkpoint Checkpoint `json:"checkpoint"`
	}
	if e = json.Unmarshal(checkpointRaw, &payload); e != nil || !payload.Checkpoint.CompleteHistory || payload.Checkpoint.Algorithm != Algorithm {
		t.Fatal(payload, e)
	}
}

func TestLiveCanonicalMixedBasisSaleDoesNotDoubleCountTransfer(t *testing.T) {
	ctx, worker, p := liveProductionFixture(t, "L16_MIXED_")
	summary, e := worker.RebuildOwned(ctx, p)
	if e != nil {
		t.Fatal(e)
	}
	active, e := worker.service.Active(ctx, p.Wallet, liveRequired(t, "L16_MIXED_TOKEN"))
	if e != nil {
		t.Fatal(e)
	}
	state := active.State
	if state.Known != "5" || state.Unknown != "5" || state.Cost != "50" || state.Realized != "25" || state.UnknownProceeds != "75" || state.CompleteRealized() != nil {
		t.Fatal(state)
	}
	a := worker.service.authority
	var trades, external int
	if e = a.pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE payload->>'kind'='trade'),count(*) FILTER(WHERE payload->>'kind'='transfer_in') FROM `+a.table("portfolio_history_rows")+` WHERE snapshot_id=$1`, summary.SnapshotID).Scan(&trades, &external); e != nil || trades != 2 || external != 1 {
		t.Fatal(trades, external, e)
	}
}
