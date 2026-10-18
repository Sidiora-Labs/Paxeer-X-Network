package rest

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// Validator-level checks: every document the development market reads emit
// is marshalled through writeMarketEnvelope and must pass the frozen v2
// contract validator.

func marketDocumentWatermark(t *testing.T) store.ChainWatermark {
	t.Helper()
	wm, err := store.DevelopmentWatermark(store.DevelopmentHead{Height: 33832844, UpdatedAt: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}, "0x"+strings.Repeat("Ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	return wm
}

func marketDocumentPool() store.DevelopmentPool {
	fee := 100
	return store.DevelopmentPool{
		Pool: "0x" + strings.Repeat("a1", 20), Token: "0x" + strings.Repeat("0a", 20), Creator: "0x" + strings.Repeat("ce", 20), Optical: "0x" + strings.Repeat("0", 40),
		PoolID: "0x" + strings.Repeat("a1", 32), CreatedAt: 1760000000, CreatedHeight: 33800000, CreationTransaction: "0x" + strings.Repeat("12", 32),
		Reserves:   &store.PoolReserves{VirtualUsdl: "1000000000", RealUsdl: "250000000", Token: "800000000000000000000000000", Height: 33800001},
		StateEvent: &store.DevelopmentEventRef{TransactionHash: "0x" + strings.Repeat("34", 32), Height: 33800001, LogIndex: 4},
		FeeBps:     &fee,
	}
}

func marketDocumentPosition() accounting.SourcePosition {
	return accounting.SourcePosition{ChainID: "125", Height: "33800000", TransactionIndex: "2", LogIndex: "5", TransactionHash: "0x" + strings.Repeat("12", 32), BlockHash: "0x" + strings.Repeat("56", 32)}
}

func emitMarketDocument(t *testing.T, name string, data any, wm *store.ChainWatermark, next any) []byte {
	t.Helper()
	recorder := httptest.NewRecorder()
	writeMarketEnvelope(recorder, httptest.NewRequest("GET", "/v2/pools", nil), name, data, wm, "partial", next)
	body := recorder.Body.Bytes()
	if recorder.Code != 200 {
		t.Fatalf("%s: status %d body %s", name, recorder.Code, body)
	}
	if err := interfaces.ValidateJSON(name, body); err != nil {
		t.Fatalf("%s: %v %s", name, err, body)
	}
	return body
}

func TestDevelopmentMarketDocumentsValidate(t *testing.T) {
	wm := marketDocumentWatermark(t)
	row := marketDocumentPool()
	pool, err := developmentPoolDocument(row, marketDocumentPosition())
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := json.Marshal(store.PoolStatsRow{PoolAddress: row.Pool, TokenAddress: row.Token, Volume24h: "3600000", PriceChange24h: "-125", BuyCount24h: 4, SellCount24h: 2, HolderCount: 9, CreatedAt: 1760000000, UpdatedAt: 1760090000})
	stats, err := developmentPoolStats(row, legacy, marketDocumentPosition(), wm)
	if err != nil {
		t.Fatal(err)
	}
	if stats["changeBaselineQuality"] != "complete" || stats["priceChangeBps24h"] != "-125" {
		t.Fatalf("stats: %v", stats)
	}
	token := tokenDocument(store.PoolRegistryRow{Token: row.Token})
	emitMarketDocument(t, "PoolDetailResponse", map[string]any{"pool": pool, "token": token, "stats": stats}, &wm, nil)
	emitMarketDocument(t, "StatsResponse", map[string]any{"stats": stats}, &wm, nil)
	emitMarketDocument(t, "StatsBatchResponse", map[string]any{"items": []any{stats}, "missing": []string{"0x" + strings.Repeat("d4", 20)}}, &wm, nil)
	emitMarketDocument(t, "PoolListResponse", map[string]any{"items": []any{pool}}, &wm, encodePoolListCursor(poolListCursor{SnapshotID: "s", Offset: 1}))

	// Zero pools: empty documents, with and without a chain watermark.
	for _, mark := range []*store.ChainWatermark{&wm, nil} {
		emitMarketDocument(t, "PoolListResponse", map[string]any{"items": []any{}}, mark, nil)
		emitMarketDocument(t, "StatsBatchResponse", map[string]any{"items": []any{}, "missing": []string{}}, mark, nil)
		emitMarketDocument(t, "HoldersResponse", map[string]any{"items": []any{}, "coverage": "partial", "fromPosition": nil, "toPosition": nil, "exclusionPolicyVersion": developmentHolderPolicy, "excludedProtocolAddresses": []string{row.Pool}}, mark, nil)
	}
	emitMarketDocument(t, "RankingsResponse", map[string]any{"snapshot": map[string]any{"snapshotId": developmentSnapshotID(developmentRankingNamespace, "trending"), "category": "gainers", "modelVersion": developmentMoversModel, "computedAt": wm.ObservedAt, "watermark": wm, "items": []any{}, "totalEligible": "0", "truncated": false, "replace": true}}, &wm, nil)
	snapshot := &store.LeaderboardSnapshot{SnapshotID: developmentSnapshotID(developmentRankingNamespace, "leaderboard"), Category: "creator", Window: "30d", MethodologyVersion: store.DevelopmentLeaderboardMethodologyVersion, Entries: []store.LeaderboardEntry{}, Watermark: wm}
	emitMarketDocument(t, "LeaderboardsResponse", map[string]any{"snapshot": map[string]any{"snapshotId": snapshot.SnapshotID, "category": snapshot.Category, "methodologyVersion": snapshot.MethodologyVersion, "window": snapshot.Window, "entries": []any{}, "watermark": snapshot.Watermark}}, &wm, nil)
	emitMarketDocument(t, "RankingsResponse", map[string]any{"snapshot": map[string]any{"snapshotId": developmentSnapshotID(developmentRankingNamespace, "volume"), "category": "volume", "modelVersion": developmentRankingModel, "computedAt": wm.ObservedAt, "watermark": wm, "items": []any{map[string]any{"poolAddress": row.Pool, "category": "volume", "rank": 1, "score": developmentScore(1234.5), "modelVersion": developmentRankingModel, "computedAt": wm.ObservedAt}}, "totalEligible": "1", "truncated": false, "replace": true}}, &wm, nil)
}

func TestDevelopmentMarketDocumentsRejectInvalidInput(t *testing.T) {
	wm := marketDocumentWatermark(t)
	row := marketDocumentPool()
	legacy, _ := json.Marshal(store.PoolStatsRow{PoolAddress: row.Pool, Volume24h: "12.5", UpdatedAt: 1760090000})
	if _, err := developmentPoolStats(row, legacy, marketDocumentPosition(), wm); !errors.Is(err, store.ErrDataIntegrity) {
		t.Fatalf("fractional volume accepted: %v", err)
	}
	row.FeeBps = nil
	if _, err := developmentPoolDocument(row, marketDocumentPosition()); !errors.Is(err, errPoolStateUnavailable) {
		t.Fatalf("pool without observed fee accepted: %v", err)
	}
	if _, err := store.DevelopmentWatermark(store.DevelopmentHead{Height: 1}, "0x1234"); !errors.Is(err, store.ErrDataIntegrity) {
		t.Fatalf("short block hash accepted: %v", err)
	}
	recorder := httptest.NewRecorder()
	writeMarketEnvelope(recorder, httptest.NewRequest("GET", "/v2/pools", nil), "PoolListResponse", map[string]any{"items": []any{map[string]any{"poolAddress": "bad"}}}, &wm, "partial", nil)
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "INTEGRITY_QUARANTINED") || interfaces.ValidateJSON("Error", recorder.Body.Bytes()) != nil {
		t.Fatalf("invalid document escaped: %d %s", recorder.Code, recorder.Body.String())
	}
}
