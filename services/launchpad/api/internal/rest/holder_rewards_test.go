package rest_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	holderRewardsPoolSeventh = "0x7777777777777777777777777777777777777777"
	holderRewardsPoolEighth  = "0x8888888888888888888888888888888888888888"
	holderRewardsHolderOne   = "0xd1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1"
	holderRewardsHolderTwo   = "0xd2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2d2"
	holderRewardsHolderThree = "0xd3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3d3"
	holderRewardsHolderFour  = "0xd4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4d4"
)

func holderRewardsPoolID(address string) string { return "0x" + strings.Repeat(address[2:4], 32) }

func insertIndexerPool(t *testing.T, pool *pgxpool.Pool, address, token string, createdAt int64) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$4,$5,$6,$7,2,$8)`, "0x"+strings.ToUpper(address[2:6])+address[6:], token, poolReadsCreator, "0x"+strings.Repeat("0", 39)+"1", holderRewardsPoolID(address), int64(address[2]), createdAt, "0x"+strings.Repeat(address[2:4], 32)); err != nil {
		t.Fatal(err)
	}
}

func insertFeeDistribution(t *testing.T, pool *pgxpool.Pool, address string, strategy int, amount string, block int64, logIndex int) {
	t.Helper()
	id := "0x" + strings.Repeat("f", 64) + "-" + strconv.FormatInt(block, 10) + "-" + strconv.Itoa(logIndex)
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.fee_distributions(id,pool_id,nft_id,strategy,amount,recipient,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8,$9)`, id, holderRewardsPoolID(address), strategy, amount, "0x"+strings.Repeat("a", 40), block, 1760000000+block, "0x"+strings.Repeat("f", 64), logIndex); err != nil {
		t.Fatal(err)
	}
}

func insertFeeStrategyChange(t *testing.T, pool *pgxpool.Pool, address string, oldStrategy, newStrategy int, block int64, logIndex int) {
	t.Helper()
	id := "0x" + strings.Repeat("e", 64) + "-" + strconv.FormatInt(block, 10) + "-" + strconv.Itoa(logIndex)
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.fee_strategy_changes(id,pool_id,nft_id,old_strategy,new_strategy,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,1,$3,$4,$5,$6,$7,$8)`, id, holderRewardsPoolID(address), oldStrategy, newStrategy, block, 1760000000+block, "0x"+strings.Repeat("e", 64), logIndex); err != nil {
		t.Fatal(err)
	}
}

func insertAirdropClaim(t *testing.T, pool *pgxpool.Pool, address, holder, amount string, block int64, logIndex int) {
	t.Helper()
	id := "0x" + strings.Repeat("d", 64) + "-" + strconv.FormatInt(block, 10) + "-" + strconv.Itoa(logIndex)
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.airdrop_claims(id,pool_id,pool_address,nft_id,holder,amount,epoch,event_name,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,$3,1,$4,$5,'1','AirdropClaimed',$6,$7,$8,$9)`, id, holderRewardsPoolID(address), "0x"+strings.ToUpper(address[2:6])+address[6:], holder, amount, block, 1760000000+block, "0x"+strings.Repeat("d", 64), logIndex); err != nil {
		t.Fatal(err)
	}
}

func insertCommittedHead(t *testing.T, pool *pgxpool.Pool, height int64, integrity string) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `INSERT INTO indexer.indexer_committed_heads(chain_id,stream_id,height,block_hash,integrity,source_id) VALUES(125,'fixture',$1,$2,$3,'fixture')`, height, "0x"+strings.Repeat("ab", 32), integrity); err != nil {
		t.Fatal(err)
	}
}

func holderRewardItems(t *testing.T, envelope map[string]any) []map[string]any {
	t.Helper()
	raw := envelope["data"].(map[string]any)["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		items = append(items, item.(map[string]any))
	}
	return items
}

func holderRewardOrder(t *testing.T, items []map[string]any) []string {
	t.Helper()
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item["poolAddress"].(string))
	}
	return out
}

func TestHolderRewardsProductionRead(t *testing.T) {
	f := newPoolReadsFixture(t, "")
	ctx := context.Background()
	heads, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/migrations/60001.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.pool.Exec(ctx, string(heads)); err != nil {
		t.Fatalf("committed heads migration: %v", err)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/stats"), 503, "DEPENDENCY_UNAVAILABLE")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools"), 503, "DEPENDENCY_UNAVAILABLE")
	insertCommittedHead(t, f.pool, 900, "healthy")

	tokenSeventh := "0x" + strings.Repeat("7", 39) + "a"
	tokenEighth := "0x" + strings.Repeat("8", 39) + "a"
	insertIndexerPool(t, f.pool, holderRewardsPoolSeventh, tokenSeventh, 1770000000)
	insertIndexerPool(t, f.pool, holderRewardsPoolEighth, tokenEighth, 1770000001)
	insertFeeDistribution(t, f.pool, f.first, 2, "1000", 100, 0)
	insertFeeDistribution(t, f.pool, f.first, 2, "2000", 110, 0)
	insertAirdropClaim(t, f.pool, f.first, holderRewardsHolderOne, "600", 101, 0)
	insertAirdropClaim(t, f.pool, f.first, "0x"+strings.ToUpper(holderRewardsHolderOne[2:]), "400", 101, 1)
	insertAirdropClaim(t, f.pool, f.first, holderRewardsHolderTwo, "2000", 111, 0)
	insertFeeDistribution(t, f.pool, poolReadsSecond, 2, "5000", 120, 0)
	insertFeeDistribution(t, f.pool, poolReadsSecond, 1, "999", 121, 0)
	insertFeeStrategyChange(t, f.pool, poolReadsSecond, 2, 1, 121, 1)
	insertAirdropClaim(t, f.pool, poolReadsSecond, holderRewardsHolderTwo, "2500", 120, 1)
	insertAirdropClaim(t, f.pool, poolReadsSecond, holderRewardsHolderThree, "2500", 120, 2)
	insertFeeStrategyChange(t, f.pool, holderRewardsPoolSeventh, 0, 2, 130, 0)
	insertAirdropClaim(t, f.pool, holderRewardsPoolEighth, holderRewardsHolderFour, "1", 140, 0)
	for _, row := range []struct {
		address, marketCap string
	}{{f.first, "150000.5"}, {poolReadsSecond, "2000000"}} {
		if _, err = f.pool.Exec(ctx, `INSERT INTO stats.pool_stats(pool_address,token_address,market_cap,created_at,updated_at) VALUES($1,$2,$3,1760000000,1760000000) ON CONFLICT (pool_address) DO UPDATE SET market_cap=EXCLUDED.market_cap`, row.address, "0x"+strings.Repeat("3", 40), row.marketCap); err != nil {
			t.Fatal(err)
		}
	}

	stats := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/stats"), "HolderRewardsStatsResponse")
	data := stats["data"].(map[string]any)
	if data["cumulativeDistributedRaw"] != "8000" || data["walletsRewarded"] != float64(4) || data["generatedAt"] == nil || stats["quality"] != "complete" || stats["revision"] != "900" {
		t.Fatalf("stats: %s", string(mustMarshal(stats)))
	}
	watermark := data["watermark"].(map[string]any)
	if watermark["height"] != "900" || watermark["blockHash"] != "0x"+strings.Repeat("ab", 32) || watermark["integrity"] != "healthy" || !reflectEqualJSON(stats["watermark"], data["watermark"]) {
		t.Fatalf("stats watermark: %v", watermark)
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/stats?limit=1"), 400, "QUERY_INVALID")

	cases := []struct {
		name   string
		path   string
		status int
		code   string
	}{
		{"unknown field", "/v2/holder-rewards/pools?order=desc", 422, "FIELD_UNKNOWN"},
		{"bad sort", "/v2/holder-rewards/pools?sort=volume", 422, "FIELD_INVALID"},
		{"limit above range", "/v2/holder-rewards/pools?limit=101", 422, "FIELD_INVALID"},
		{"limit not integer", "/v2/holder-rewards/pools?limit=1.5", 422, "FIELD_INVALID"},
		{"empty cursor", "/v2/holder-rewards/pools?cursor=", 400, "CURSOR_INVALID"},
		{"garbage cursor", "/v2/holder-rewards/pools?cursor=%21", 400, "CURSOR_INVALID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertV2Error(t, getV2(t, f.srv.URL+tc.path), tc.status, tc.code)
		})
	}

	rewards := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools"), "HolderRewardsPoolsResponse")
	items := holderRewardItems(t, rewards)
	if order := holderRewardOrder(t, items); len(order) != 3 || order[0] != poolReadsSecond || order[1] != f.first || order[2] != holderRewardsPoolSeventh {
		t.Fatalf("rewards order: %v", order)
	}
	if rewards["quality"] != "partial" || rewards["nextCursor"] != nil || rewards["revision"] != "900" {
		t.Fatalf("rewards envelope: %s", string(mustMarshal(rewards)))
	}
	second, first, seventh := items[0], items[1], items[2]
	if second["distributedRaw"] != "5000" || second["holdersPaid"] != float64(2) || second["strategyActive"] != false || second["tokenAddress"] != "0x"+strings.Repeat("4", 40) || second["createdAt"] != "2025-10-09T08:53:20.000Z" {
		t.Fatalf("second pool: %v", second)
	}
	if first["distributedRaw"] != "3000" || first["holdersPaid"] != float64(2) || first["strategyActive"] != true || first["createdAt"] != "2025-10-09T08:53:20.000Z" {
		t.Fatalf("first pool: %v", first)
	}
	if seventh["distributedRaw"] != "0" || seventh["holdersPaid"] != float64(0) || seventh["strategyActive"] != true || seventh["tokenAddress"] != tokenSeventh || seventh["createdAt"] != "2026-02-02T02:40:00.000Z" {
		t.Fatalf("seventh pool: %v", seventh)
	}

	tie := []string{f.first, poolReadsSecond}
	sort.Strings(tie)
	holders := holderRewardOrder(t, holderRewardItems(t, assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?sort=holders"), "HolderRewardsPoolsResponse")))
	if len(holders) != 3 || holders[0] != tie[0] || holders[1] != tie[1] || holders[2] != holderRewardsPoolSeventh {
		t.Fatalf("holders order: %v", holders)
	}
	mcap := holderRewardOrder(t, holderRewardItems(t, assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?sort=mcap"), "HolderRewardsPoolsResponse")))
	if len(mcap) != 3 || mcap[0] != poolReadsSecond || mcap[1] != f.first || mcap[2] != holderRewardsPoolSeventh {
		t.Fatalf("mcap order: %v", mcap)
	}
	age := holderRewardOrder(t, holderRewardItems(t, assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?sort=age"), "HolderRewardsPoolsResponse")))
	if len(age) != 3 || age[0] != holderRewardsPoolSeventh || age[1] != tie[0] || age[2] != tie[1] {
		t.Fatalf("age order: %v", age)
	}

	page := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?limit=1"), "HolderRewardsPoolsResponse")
	cursor, _ := page["nextCursor"].(string)
	if cursor == "" || len(holderRewardItems(t, page)) != 1 {
		t.Fatalf("first page: %s", string(mustMarshal(page)))
	}
	next := assertV2Success(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?limit=2&cursor="+cursor), "HolderRewardsPoolsResponse")
	if order := holderRewardOrder(t, holderRewardItems(t, next)); len(order) != 2 || order[0] != f.first || order[1] != holderRewardsPoolSeventh || next["nextCursor"] != nil {
		t.Fatalf("second page: %s", string(mustMarshal(next)))
	}
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?sort=age&cursor="+cursor), 409, "RESNAPSHOT_REQUIRED")
	var decoded map[string]any
	rawCursor, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || json.Unmarshal(rawCursor, &decoded) != nil {
		t.Fatal(err)
	}
	decoded["offset"] = float64(50)
	beyond, _ := json.Marshal(decoded)
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?cursor="+base64.RawURLEncoding.EncodeToString(beyond)), 400, "CURSOR_INVALID")

	insertCommittedHead(t, f.pool, 901, "quarantined")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/stats"), 503, "INTEGRITY_QUARANTINED")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools"), 503, "INTEGRITY_QUARANTINED")
	assertV2Error(t, getV2(t, f.srv.URL+"/v2/holder-rewards/pools?cursor="+cursor), 503, "INTEGRITY_QUARANTINED")
}
