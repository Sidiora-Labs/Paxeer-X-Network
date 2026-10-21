package rest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	leaderboardWalletA = "0xaaaa000000000000000000000000000000000001"
	leaderboardWalletB = "0xbbbb000000000000000000000000000000000002"
	leaderboardWalletC = "0xcccc000000000000000000000000000000000003"
)

// insertWindowFill commits one accounting fill with an explicit side, trader
// attribution and block timestamp so window membership and ranking are
// driven by real FillV1 documents in stats.accounting_market_commits.
func insertWindowFill(t *testing.T, pool *pgxpool.Pool, address string, sequence int, side, usdl, tokenAmount, fee string, sender string, trader *string, blockTimestamp int64) {
	t.Helper()
	position := poolReadsPosition(int64(1000+sequence), int64(sequence))
	position.TransactionHash = "0x" + strings.Repeat(address[2:4], 32)
	paid, disposition := usdl, "distributed_usdl"
	if side == "sell" {
		paid, disposition = tokenAmount, "retained_token_reserve"
	}
	feeBps := uint16(30)
	attribution := accounting.Attribution{TransactionSender: sender, Quality: "unavailable", Evidence: []accounting.EvidenceRef{}}
	if trader != nil {
		attribution.Trader = trader
		attribution.Quality = "exact_receipt"
	}
	fill := accounting.FillV1{Version: accounting.FillVersion, Position: position, ActualEmitter: address, PoolAddress: address, Side: side, TokenAmountRaw: tokenAmount, USDLAmountRaw: usdl, PaidInputRaw: paid, FeeRaw: fee, FeeDisposition: disposition, Execution: accounting.ExecutionPolicy{Quality: "qualified_execution", FeeBPS: &feeBps, Evidence: []accounting.EvidenceRef{}}, Attribution: attribution, Provenance: accounting.Provenance{DeploymentID: "fixture125", ABIRevision: "sidiora-v1", EmitterCodeEpoch: "epoch1", PoolCodeEpoch: "epoch1", ReceiptSHA256: strings.Repeat("c", 64), HeaderSHA256: strings.Repeat("d", 64)}, BlockTimestampSeconds: strconv.FormatInt(blockTimestamp, 10)}
	fill.SourceFillID = accounting.SourceFillIdentity(fill.Position, fill.Provenance.DeploymentID)
	raw, err := json.Marshal(fill)
	if err != nil {
		t.Fatal(err)
	}
	partition, _ := json.Marshal(accounting.PartitionPosition{ProjectionVersion: "v1", PartitionKind: "market", PartitionKey: "125:" + address, Sequence: strconv.Itoa(sequence), SourceEventID: fill.SourceFillID, SourceOrder: fill.Position})
	if _, err = pool.Exec(context.Background(), `INSERT INTO stats.accounting_market_commits(publication_id,source_fill_id,source_fact_digest,projection_version,pool,bucket_start,position,fill,sequence,revision) VALUES($1,$2,$3,'v1',$4,$5,$6,$7,$8,$8)`, "pub-"+address+"-"+strconv.Itoa(sequence), fill.SourceFillID, strings.Repeat("e", 64), address, blockTimestamp-blockTimestamp%60, partition, raw, sequence); err != nil {
		t.Fatal(err)
	}
}

func leaderboardEntries(t *testing.T, envelope map[string]any) ([]map[string]any, map[string]any) {
	t.Helper()
	snapshot := envelope["data"].(map[string]any)["snapshot"].(map[string]any)
	raw := snapshot["entries"].([]any)
	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entries = append(entries, item.(map[string]any))
	}
	return entries, snapshot
}

func TestLeaderboardProductionRead(t *testing.T) {
	f := newPoolReadsFixture(t, "")
	now := time.Now().Unix()
	walletA, walletB, walletC := leaderboardWalletA, leaderboardWalletB, leaderboardWalletC
	insertWindowFill(t, f.pool, f.first, 10, "buy", "1000000", "500000000000000000000", "3000", walletA, &walletA, now-3600)
	insertWindowFill(t, f.pool, f.first, 11, "sell", "2500000", "400000000000000000000", "1000", walletA, &walletA, now-1800)
	insertWindowFill(t, f.pool, f.first, 12, "buy", "4000000", "900000000000000000000", "12000", walletB, nil, now-900)
	insertWindowFill(t, f.pool, f.first, 13, "buy", "500000", "100000000000000000000", "1500", walletC, &walletC, now-3*86400)
	// Realised PnL: A sells 400 of 500 bought for 1000000 (basis 800000) for
	// 2500000; B sells 100 of 900 bought for 4000000 (basis 444444) for 300000
	// without trader attribution; C sells 50 of 100 bought for 500000 (basis
	// 250000) for 100000 two days ago. Buy-only wallets realise nothing.
	insertWindowFill(t, f.pool, f.first, 14, "sell", "300000", "100000000000000000000", "0", walletB, nil, now-600)
	insertWindowFill(t, f.pool, f.first, 15, "sell", "100000", "50000000000000000000", "0", walletC, &walletC, now-2*86400)

	cases := []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{"unknown category", "/v2/leaderboards/whales?window=24h", 404, "NOT_FOUND"},
		{"missing window", "/v2/leaderboards/trader", 422, "FIELD_INVALID"},
		{"bad window", "/v2/leaderboards/trader?window=1h", 422, "FIELD_INVALID"},
		{"limit below range", "/v2/leaderboards/trader?window=24h&limit=0", 422, "FIELD_INVALID"},
		{"limit above range", "/v2/leaderboards/trader?window=24h&limit=101", 422, "FIELD_INVALID"},
		{"unknown field", "/v2/leaderboards/trader?window=24h&sort=pnl", 422, "FIELD_UNKNOWN"},
		{"conflicting repeated field", "/v2/leaderboards/trader?window=24h&window=7d", 400, "QUERY_INVALID"},
		{"repeated field", "/v2/leaderboards/trader?window=24h&window=24h", 422, "FIELD_UNKNOWN"},
		{"empty cursor", "/v2/leaderboards/trader?window=24h&cursor=", 400, "CURSOR_INVALID"},
		{"garbage cursor", "/v2/leaderboards/trader?window=24h&cursor=%21%21", 400, "CURSOR_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertV2Error(t, getV2(t, f.srv.URL+tc.path), tc.status, tc.code)
		})
	}

	day := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=24h"), "LeaderboardsResponse")
	entries, snapshot := leaderboardEntries(t, day)
	if len(entries) != 2 || entries[0]["wallet"] != walletA || entries[0]["metricRaw"] != "1700000" || entries[0]["metricSigned"] != "1700000" || entries[0]["quality"] != "complete" || entries[0]["rank"] != float64(1) {
		t.Fatalf("trader 24h first entry: %v", entries)
	}
	if entries[1]["wallet"] != walletB || entries[1]["metricRaw"] != "144444" || entries[1]["metricSigned"] != "-144444" || entries[1]["quality"] != "partial" || entries[1]["rank"] != float64(2) {
		t.Fatalf("trader 24h second entry: %v", entries)
	}
	if snapshot["category"] != "trader" || snapshot["window"] != "24h" || snapshot["methodologyVersion"] == "" || day["quality"] != "partial" || day["nextCursor"] != nil {
		t.Fatalf("trader 24h snapshot: %s", string(mustMarshal(day)))
	}
	watermark := snapshot["watermark"].(map[string]any)
	if watermark["height"] != "1015" || watermark["projectionRevision"] != "15" || watermark["integrity"] != "healthy" || watermark["blockHash"] != "0x"+strings.Repeat("cd", 32) || day["revision"] != "15" {
		t.Fatalf("watermark: %v revision=%v", watermark, day["revision"])
	}
	if !reflectEqualJSON(day["watermark"], snapshot["watermark"]) {
		t.Fatalf("envelope watermark must equal snapshot watermark: %v vs %v", day["watermark"], snapshot["watermark"])
	}

	week := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d"), "LeaderboardsResponse")
	entries, _ = leaderboardEntries(t, week)
	if len(entries) != 3 || entries[0]["wallet"] != walletA || entries[1]["wallet"] != walletB || entries[2]["wallet"] != walletC || entries[2]["metricSigned"] != "-150000" {
		t.Fatalf("trader 7d: %v", entries)
	}

	volume := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/volume?window=24h"), "LeaderboardsResponse")
	entries, _ = leaderboardEntries(t, volume)
	if len(entries) != 2 || entries[0]["wallet"] != walletB || entries[0]["metricRaw"] != "4300000" || entries[1]["wallet"] != walletA || entries[1]["metricRaw"] != "3500000" || entries[1]["quality"] != "complete" {
		t.Fatalf("volume 24h: %v", entries)
	}
	if _, signed := entries[0]["metricSigned"]; signed {
		t.Fatalf("volume entries carry no signed metric: %v", entries[0])
	}

	creator := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/creator?window=30d"), "LeaderboardsResponse")
	entries, _ = leaderboardEntries(t, creator)
	if len(entries) != 1 || entries[0]["wallet"] != poolReadsCreator || entries[0]["metricRaw"] != "16500" || entries[0]["quality"] != "partial" {
		t.Fatalf("creator 30d: %v", entries)
	}

	first := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d&limit=1"), "LeaderboardsResponse")
	cursor, _ := first["nextCursor"].(string)
	pageEntries, pageSnapshot := leaderboardEntries(t, first)
	if cursor == "" || len(pageEntries) != 1 || pageEntries[0]["wallet"] != walletA {
		t.Fatalf("first page: %s", string(mustMarshal(first)))
	}
	second := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d&limit=1&cursor="+cursor), "LeaderboardsResponse")
	pageEntries, secondSnapshot := leaderboardEntries(t, second)
	if len(pageEntries) != 1 || pageEntries[0]["wallet"] != walletB || pageEntries[0]["rank"] != float64(2) || secondSnapshot["snapshotId"] != pageSnapshot["snapshotId"] || second["nextCursor"] == nil {
		t.Fatalf("second page: %s", string(mustMarshal(second)))
	}
	third := assertV2Success(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d&limit=5&cursor="+second["nextCursor"].(string)), "LeaderboardsResponse")
	pageEntries, _ = leaderboardEntries(t, third)
	if len(pageEntries) != 1 || pageEntries[0]["wallet"] != walletC || pageEntries[0]["rank"] != float64(3) || third["nextCursor"] != nil {
		t.Fatalf("third page: %s", string(mustMarshal(third)))
	}

	var decoded map[string]any
	rawCursor, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || json.Unmarshal(rawCursor, &decoded) != nil {
		t.Fatal(err)
	}
	decoded["snapshotId"] = "00000000-0000-0000-0000-000000000000"
	stale, _ := json.Marshal(decoded)
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d&cursor="+base64.RawURLEncoding.EncodeToString(stale)), 409, "RESNAPSHOT_REQUIRED")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/volume?window=7d&cursor="+cursor), 409, "RESNAPSHOT_REQUIRED")
	decoded["snapshotId"] = pageSnapshot["snapshotId"]
	decoded["offset"] = float64(99)
	beyond, _ := json.Marshal(decoded)
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d&cursor="+base64.RawURLEncoding.EncodeToString(beyond)), 400, "CURSOR_INVALID")
	decoded["offset"] = float64(1)
	decoded["extra"] = true
	unknown, _ := json.Marshal(decoded)
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=7d&cursor="+base64.RawURLEncoding.EncodeToString(unknown)), 400, "CURSOR_INVALID")

	if _, err := f.pool.Exec(context.Background(), `DELETE FROM stats.accounting_market_commits`); err != nil {
		t.Fatal(err)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/leaderboards/trader?window=24h"), 503, "DEPENDENCY_UNAVAILABLE")
}

func reflectEqualJSON(a, b any) bool {
	left, errA := json.Marshal(a)
	right, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(left) == string(right)
}
