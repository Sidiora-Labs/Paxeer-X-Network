package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/broker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
)

// realPoolStatsV22 is the market.stats payload core/stats-workers'
// PublicStatsProjector commits for the real chain-125 pool after its first
// buy and sell (TestPublicMarketStatsFromRealPaxeerPool asserts the same
// hand-computed interface 2.2.0 values from the real receipts).
const realPoolStatsV22 = `{"poolAddress":"0xce20da50f92d21d8bee497910c782e9aa25b6263","volume24hRaw":"147278912","buys24h":"1","sells24h":"1","holderCount":"2","realUsdlBalanceRaw":"49721088","indicativeTvlUsdlRaw":null,"valuationMethod":null,"windowStart":"2026-10-12T07:12:37.000Z","windowEnd":"2026-10-13T07:12:37.000Z","sourceUpperPosition":{"chainId":"125","height":"33963749","transactionIndex":"0","logIndex":"2","transactionHash":"0xf5f9ed1136e71f03ffd44ae7f80ee192998b80fccd96ad3f75824fa9ef3c2845","blockHash":"0x1cd6b450aa87745be16a8760a928a2e7ddd1097e872bdcd99b31ff66f77c2945"},"watermark":{"chainId":"125","height":"33963749","blockHash":"0x1cd6b450aa87745be16a8760a928a2e7ddd1097e872bdcd99b31ff66f77c2945","projectionRevision":"1","observedAt":"2026-10-13T08:17:31.628Z","integrity":"healthy"},"totalSupplyValuationUsdlRaw":"10098226981","circulatingMarketCapUsdlRaw":"48505893","supplyValuationQuality":"verified","priceChangeBps24h":null,"valuationChangeUsdlRaw":null,"changeBaselineQuality":"insufficient_history","priceChangeBps5m":null,"priceChangeBps1h":null,"priceChangeBps6h":null,"changeBaselineQuality5m":"insufficient_history","changeBaselineQuality1h":"insufficient_history","changeBaselineQuality6h":"insufficient_history","spotPriceWad":"10098226981692","spotPriceUsdlRaw":"10","uniqueTraders24h":"1","txCount24h":"2","totalSupplyRaw":"1000000000000000","circulatingSupplyRaw":"4803406952560","circulationModel":"supply_minus_pool_reserve_v1","top10HolderShareBps":10000,"creatorHoldingShareBps":10000}`

const realPoolV22 = "0xce20da50f92d21d8bee497910c782e9aa25b6263"

var interface22Fields = []string{"priceChangeBps5m", "priceChangeBps1h", "priceChangeBps6h", "changeBaselineQuality5m", "changeBaselineQuality1h", "changeBaselineQuality6h", "spotPriceWad", "spotPriceUsdlRaw", "uniqueTraders24h", "txCount24h", "totalSupplyRaw", "circulatingSupplyRaw", "circulationModel", "top10HolderShareBps", "creatorHoldingShareBps"}

// TestPoolStatsInterface22FieldsRoundTrip serves the interface 2.2.0
// PoolStats fields unchanged through GET /v2/pools/{p}/stats, POST
// /v2/pools/stats:batch and GET /v2/pools/{p}, validating every response
// against the regenerated schemas, while a pre-2.2.0 document (the frozen
// IF_STATS example) keeps its exact bytes beside it.
func TestPoolStatsInterface22FieldsRoundTrip(t *testing.T) {
	pg, model, stream, first, firstPayload := productionReadsFixture(t)
	ctx := context.Background()
	for _, name := range []string{"80001", "80002", "100000", "130000"} {
		raw, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/migrations/" + name + ".sql")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pg.Exec(ctx, string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	realToken := "0x0f92fdd087c96dd3413ef98646b94d0d986192fa"
	insertRegistryPool(t, pg, realPoolV22, realToken, 14, true)
	insertPoolState(t, pg, realPoolV22, realToken)
	insertQualifiedFill(t, pg, realPoolV22, 1, 100)
	if interfaces.ValidateJSON("PoolStats", []byte(realPoolStatsV22)) != nil {
		t.Fatal("2.2.0 payload violates the regenerated PoolStats schema")
	}
	appendStats := func(eventID, revision, payload string) {
		t.Helper()
		var wm struct {
			Watermark json.RawMessage `json:"watermark"`
		}
		if err := json.Unmarshal([]byte(payload), &wm); err != nil {
			t.Fatal(err)
		}
		tx, err := pg.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		if err = stream.AppendProjectionTx(ctx, tx, broker.Frame{EventID: eventID, Topic: "market.stats." + realPoolV22, Sequence: revision, Revision: revision, OccurredAt: "2026-10-13T07:12:36.000Z", Watermark: wm.Watermark, Payload: json.RawMessage(payload)}); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	appendStats("real_stats_1", "1", realPoolStatsV22)
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	if err = rest.RegisterProductionReads(router, rest.ProductionReadOptions{Model: model, Stream: stream, Store: store.New(pg, internaltest.NewRedis(t)), Admission: admission, RegisterSnapshots: true}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(router)
	defer srv.Close()
	same := func(got json.RawMessage, want string) {
		t.Helper()
		var a, b map[string]any
		if json.Unmarshal(got, &a) != nil || json.Unmarshal([]byte(want), &b) != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("stats document changed in transit:\n got %s\nwant %s", got, want)
		}
	}
	var single struct {
		Data struct {
			Stats json.RawMessage `json:"stats"`
		} `json:"data"`
	}
	got := getV2(t, srv.URL+"/v2/pools/"+realPoolV22+"/stats")
	assertV2Success(t, got, "StatsResponse")
	if json.Unmarshal(got.body, &single) != nil {
		t.Fatal(string(got.body))
	}
	same(single.Data.Stats, realPoolStatsV22)

	got = getV2(t, srv.URL+"/v2/pools/"+realPoolV22)
	assertV2Success(t, got, "PoolDetailResponse")
	if json.Unmarshal(got.body, &single) != nil {
		t.Fatal(string(got.body))
	}
	same(single.Data.Stats, realPoolStatsV22)

	body, _ := json.Marshal(map[string]any{"poolAddresses": []string{realPoolV22, first}})
	response, err := http.Post(srv.URL+"/v2/pools/stats:batch", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	_, _ = raw.ReadFrom(response.Body)
	response.Body.Close()
	assertV2Success(t, v2Response{status: response.StatusCode, header: response.Header, body: raw.Bytes()}, "StatsBatchResponse")
	var batch struct {
		Data struct {
			Items   []json.RawMessage `json:"items"`
			Missing []string          `json:"missing"`
		} `json:"data"`
	}
	if json.Unmarshal(raw.Bytes(), &batch) != nil || len(batch.Data.Items) != 2 || len(batch.Data.Missing) != 0 {
		t.Fatal(raw.String())
	}
	same(batch.Data.Items[0], realPoolStatsV22)
	same(batch.Data.Items[1], string(firstPayload))
	for _, field := range interface22Fields {
		if !strings.Contains(string(batch.Data.Items[0]), `"`+field+`":`) || strings.Contains(string(batch.Data.Items[1]), `"`+field+`":`) {
			t.Fatalf("field %s: real %s / pre-2.2.0 %s", field, batch.Data.Items[0], batch.Data.Items[1])
		}
	}

	// A committed document whose fill count disagrees with buys+sells is an
	// integrity failure, never served; the honest next revision is.
	revise := func(doc, revision string) string {
		return strings.Replace(doc, `"projectionRevision":"1"`, `"projectionRevision":"`+revision+`"`, 1)
	}
	appendStats("real_stats_2", "2", revise(realPoolStatsV22, "2"))
	assertV2Success(t, getV2(t, srv.URL+"/v2/pools/"+realPoolV22+"/stats"), "StatsResponse")
	appendStats("real_stats_3", "3", revise(strings.Replace(realPoolStatsV22, `"txCount24h":"2"`, `"txCount24h":"3"`, 1), "3"))
	assertV2Error(t, getV2(t, srv.URL+"/v2/pools/"+realPoolV22+"/stats"), 503, "INTEGRITY_QUARANTINED")
}
