package rest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// hsdDevelopmentWriterDDL creates the development tables the history, search and
// discovery reads query and that internaltest's shared read schema does not
// carry. Columns and types are copied from the development goose migrations
// that create them on Railway: core/indexer/migrations/00001_init.sql
// (indexer.swaps), media/metadata/migrations/00001_init.sql + 00002
// (metadata.token_metadata) and media/user/migrations/00001_init.sql
// (users.user_profiles).
const hsdDevelopmentWriterDDL = `
CREATE TABLE indexer.swaps (
	id text PRIMARY KEY NOT NULL,
	pool_id varchar(66) NOT NULL,
	pool_address varchar(42) NOT NULL,
	sender varchar(42) NOT NULL,
	router varchar(42),
	is_buy boolean NOT NULL,
	amount_in text NOT NULL,
	amount_out text NOT NULL,
	fee text NOT NULL,
	price text NOT NULL,
	block_number bigint NOT NULL,
	block_timestamp bigint NOT NULL,
	tx_hash varchar(66) NOT NULL,
	log_index integer NOT NULL
);
CREATE INDEX idx_swaps_pool_address ON indexer.swaps USING btree (pool_address);
CREATE SCHEMA metadata;
CREATE TABLE metadata.token_metadata (
	token_address varchar(42) PRIMARY KEY NOT NULL,
	pool_address varchar(42) NOT NULL,
	name text,
	symbol text,
	description text,
	website text,
	twitter text,
	telegram text,
	discord text,
	custom_tags text DEFAULT '[]',
	created_by varchar(42) NOT NULL,
	created_at bigint NOT NULL,
	updated_at bigint NOT NULL,
	decimals integer NOT NULL DEFAULT 6
);
CREATE SCHEMA users;
CREATE TABLE users.user_profiles (
	wallet_address varchar(42) PRIMARY KEY NOT NULL,
	display_name text,
	bio text,
	twitter text,
	telegram text,
	discord text,
	website text,
	created_at bigint NOT NULL,
	updated_at bigint NOT NULL
);
`

const (
	hsdDevPool     = "0x1111111111111111111111111111111111111111"
	hsdDevToken    = "0x2222222222222222222222222222222222222222"
	hsdDevCreator  = "0x3333333333333333333333333333333333333333"
	hsdDevPool2    = "0x4444444444444444444444444444444444444444"
	hsdDevToken2   = "0x5555555555555555555555555555555555555555"
	hsdDevWallet   = "0x6666666666666666666666666666666666666666"
	hsdDevUnknown  = "0x7777777777777777777777777777777777777777"
	hsdDevTxHash   = "0x8888888888888888888888888888888888888888888888888888888888888888"
	hsdDevPoolID   = "0x9999999999999999999999999999999999999999999999999999999999999999"
	hsdDevPoolID2  = "0x9999999999999999999999999999999999999999999999999999999999999998"
	hsdDevCallout1 = "6f1c2a8e-7d64-4bb1-9c39-0b1c3c4c1a01"
	hsdDevCallout2 = "6f1c2a8e-7d64-4bb1-9c39-0b1c3c4c1a02"
	hsdDevCallout3 = "6f1c2a8e-7d64-4bb1-9c39-0b1c3c4c1a03"
)

// hsdDevServer mounts exactly the development registration on a chi router over
// a real Postgres. withDiscovery applies core/discovery's embedded schema; the
// development database on Railway has no narrative_discovery schema at all.
func hsdDevServer(t *testing.T, withDiscovery bool) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, hsdDevelopmentWriterDDL); err != nil {
		t.Fatalf("development writer schema: %v", err)
	}
	if withDiscovery {
		if _, err := pool.Exec(ctx, discovery.Schema); err != nil {
			t.Fatalf("discovery schema: %v", err)
		}
	}
	for _, p := range []struct{ pool, token, id string }{{hsdDevPool, hsdDevToken, hsdDevPoolID}, {hsdDevPool2, hsdDevToken2, hsdDevPoolID2}} {
		if _, err := pool.Exec(ctx, `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$3,$4,1,1760000000,100,$5)`, p.pool, p.token, hsdDevCreator, p.id, hsdDevTxHash); err != nil {
			t.Fatalf("seed pool: %v", err)
		}
	}
	admission, err := rest.NewAdmission(100, 100, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	rest.RegisterDevelopmentHistorySearchDiscovery(r, store.New(pool, internaltest.NewRedis(t)), admission)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, pool
}

func hsdDevRequest(t *testing.T, method, target string, header http.Header) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body, resp.Header
}

// hsdExpectDocument asserts the status and that the body validates as the named
// contract document (Error for every non-2xx answer).
func hsdExpectDocument(t *testing.T, srv *httptest.Server, path string, status int, name string) []byte {
	t.Helper()
	code, body, header := hsdDevRequest(t, http.MethodGet, srv.URL+path, nil)
	if code != status {
		t.Fatalf("GET %s = %d, want %d: %s", path, code, status, body)
	}
	if err := interfaces.ValidateJSON(name, body); err != nil {
		t.Fatalf("GET %s body is not a valid %s: %v\n%s", path, name, err, body)
	}
	if header.Get("X-Schema-Version") != "2" {
		t.Fatalf("GET %s schema version header = %q", path, header.Get("X-Schema-Version"))
	}
	return body
}

func hsdExpectError(t *testing.T, srv *httptest.Server, method, path string, status int, code string) {
	t.Helper()
	got, body, _ := hsdDevRequest(t, method, srv.URL+path, nil)
	if got != status {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, got, status, body)
	}
	if err := interfaces.ValidateJSON("Error", body); err != nil {
		t.Fatalf("%s %s error body invalid: %v\n%s", method, path, err, body)
	}
	var e struct{ Code string }
	_ = json.Unmarshal(body, &e)
	if e.Code != code {
		t.Fatalf("%s %s code = %s, want %s", method, path, e.Code, code)
	}
}

func hsdDecodeEnvelope(t *testing.T, body []byte) (items []map[string]any, next *string) {
	t.Helper()
	var envelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data.Items, envelope.NextCursor
}

func TestDevelopmentTradesAndCandles(t *testing.T) {
	srv, pool := hsdDevServer(t, false)
	ctx := context.Background()
	candlesQuery := "/candles?interval=1h&from=2026-10-12T00:00:00.000Z&to=2026-10-13T00:00:00.000Z"

	// A pool the development pipeline holds no rows for yields valid empty documents.
	if items, next := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/trades", 200, "TradesResponse")); len(items) != 0 || next != nil {
		t.Fatalf("empty trades page = %v %v", items, next)
	}
	body := hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+candlesQuery, 200, "CandlesResponse")
	var candles struct {
		Data struct {
			SeriesModelVersion string `json:"seriesModelVersion"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &candles)
	if candles.Data.SeriesModelVersion != rest.DevelopmentSeriesModelVersion {
		t.Fatalf("series model = %q", candles.Data.SeriesModelVersion)
	}

	// Request validation follows the production contract parser.
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevUnknown+"/trades", 404, "NOT_FOUND")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevUnknown+candlesQuery, 404, "NOT_FOUND")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/trades?limit=101", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/trades?side=both", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/trades?cursor=abc", 400, "CURSOR_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/candles?interval=2h&from=2026-10-12T00:00:00.000Z&to=2026-10-13T00:00:00.000Z", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/candles?interval=1h&from=2026-10-13T00:00:00.000Z&to=2026-10-12T00:00:00.000Z", 422, "FIELD_INVALID")

	// A recorded sell: the development row carries no block hash or
	// transaction index, so it is quarantined rather than given a position.
	if _, err := pool.Exec(ctx, `INSERT INTO indexer.swaps(id,pool_id,pool_address,sender,is_buy,amount_in,amount_out,fee,price,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,$3,$4,false,'1000','990','10','1',200,1760300000,$5,0)`, hsdDevTxHash+"-0", hsdDevPoolID, hsdDevPool, hsdDevWallet, hsdDevTxHash); err != nil {
		t.Fatal(err)
	}
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/trades", 503, "INTEGRITY_QUARANTINED")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/trades?side=sell", 503, "INTEGRITY_QUARANTINED")
	if items, _ := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/trades?side=buy&limit=10", 200, "TradesResponse")); len(items) != 0 {
		t.Fatal("buy filter returned items")
	}
	hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool2+"/trades", 200, "TradesResponse")

	// Candle buckets: only the half-open requested range and timeframe count.
	start := time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC).Unix()
	if _, err := pool.Exec(ctx, `INSERT INTO candles.candles(pool_address,timeframe,candle_start,open,high,low,close,last_trade_ts,sequence_num) VALUES($1,'1h',$2,'1','1','1','1',$2,1)`, hsdDevPool, start); err != nil {
		t.Fatal(err)
	}
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+candlesQuery, 503, "INTEGRITY_QUARANTINED")
	hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/candles?interval=1m&from=2026-10-12T00:00:00.000Z&to=2026-10-13T00:00:00.000Z", 200, "CandlesResponse")
	hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/candles?interval=1h&from=2026-10-12T06:00:00.000Z&to=2026-10-13T00:00:00.000Z", 200, "CandlesResponse")
	hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/candles?interval=1h&from=2026-10-12T00:00:00.000Z&to=2026-10-12T05:00:00.000Z", 200, "CandlesResponse")
}

func hsdSeedSearch(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, m := range []struct{ token, pool, name, symbol string }{{hsdDevToken, hsdDevPool, "Doge Rocket", "DOGR"}, {hsdDevToken2, hsdDevPool2, "Dogwood_%", "WOOD"}} {
		if _, err := pool.Exec(ctx, `INSERT INTO metadata.token_metadata(token_address,pool_address,name,symbol,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,1,1)`, m.token, m.pool, m.name, m.symbol, hsdDevCreator); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users.user_profiles(wallet_address,display_name,created_at,updated_at) VALUES($1,'Doge Whale',1,1),($2,NULL,1,1)`, hsdDevWallet, hsdDevCreator); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentSearch(t *testing.T) {
	srv, pool := hsdDevServer(t, false)

	// Zero rows: valid empty documents.
	if items, _ := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q=doge&type=pool", 200, "SearchResponse")); len(items) != 0 {
		t.Fatal("search before metadata returned items")
	}
	hsdSeedSearch(t, pool)

	items, next := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q=DOG&type=pool&limit=10", 200, "SearchResponse"))
	if len(items) != 2 || next != nil || items[0]["poolAddress"] != hsdDevPool || items[0]["label"] != "Doge Rocket" || items[0]["id"] != "125:"+hsdDevPool {
		t.Fatalf("text pool search = %v", items)
	}
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q=dog&type=token&limit=1", 200, "SearchResponse"))
	if len(items) != 1 || items[0]["tokenAddress"] != hsdDevToken || items[0]["id"] != "125:"+hsdDevToken || items[0]["kind"] != "token" {
		t.Fatalf("limited token search = %v", items)
	}
	// LIKE wildcards in the query are literal.
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q="+url.QueryEscape("_%")+"&type=pool", 200, "SearchResponse"))
	if len(items) != 1 || items[0]["poolAddress"] != hsdDevPool2 {
		t.Fatalf("escaped search = %v", items)
	}
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q="+strings.ToUpper(hsdDevToken2[2:])+"&type=token", 200, "SearchResponse"))
	if len(items) != 0 {
		t.Fatalf("non-0x text search matched an address: %v", items)
	}
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q=0x"+strings.ToUpper(hsdDevToken2[2:])+"&type=token", 200, "SearchResponse"))
	if len(items) != 1 || items[0]["tokenAddress"] != hsdDevToken2 || items[0]["poolAddress"] != hsdDevPool2 {
		t.Fatalf("exact address search = %v", items)
	}
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q=0x111&type=pool", 200, "SearchResponse"))
	if len(items) != 1 || items[0]["poolAddress"] != hsdDevPool {
		t.Fatalf("partial address search = %v", items)
	}
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q=whale&type=profile", 200, "SearchResponse"))
	if len(items) != 1 || items[0]["wallet"] != hsdDevWallet || items[0]["label"] != "Doge Whale" || items[0]["poolAddress"] != nil {
		t.Fatalf("profile search = %v", items)
	}
	items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/search?q="+hsdDevCreator+"&type=profile", 200, "SearchResponse"))
	if len(items) != 1 || items[0]["label"] != hsdDevCreator {
		t.Fatalf("profile address search = %v", items)
	}

	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=d&type=pool", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=dog&type=wallet", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=dog", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=dog&type=pool&limit=26", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=dog&type=pool&sort=address", 400, "QUERY_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=0x"+strings.Repeat("a", 64)+"&type=pool", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search?q=0xzz&type=pool", 422, "FIELD_INVALID")
}

func TestDevelopmentExpandedSearch(t *testing.T) {
	srv, pool := hsdDevServer(t, false)
	hsdSeedSearch(t, pool)
	type page struct {
		Data struct {
			Items      []map[string]any `json:"items"`
			MatchKind  string           `json:"matchKind"`
			Status     string           `json:"status"`
			NextCursor json.RawMessage  `json:"nextCursor"`
		} `json:"data"`
	}
	var first page
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/search/expanded?q=dog&type=pool&limit=1&sort=address", 200, "ConnectedExpandedSearchResponse"), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Data.Items) != 1 || first.Data.Items[0]["creator"] != hsdDevCreator || first.Data.Items[0]["ticker"] != "DOGR" || first.Data.MatchKind != "text" || string(first.Data.NextCursor) == "null" {
		t.Fatalf("first expanded page = %+v", first.Data)
	}
	var second page
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/search/expanded?q=dog&type=pool&limit=1&cursor="+url.QueryEscape(string(first.Data.NextCursor)), 200, "ConnectedExpandedSearchResponse"), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.Data.Items) != 1 || second.Data.Items[0]["poolAddress"] != hsdDevPool2 || string(second.Data.NextCursor) != "null" {
		t.Fatalf("second expanded page = %+v", second.Data)
	}
	var missing page
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/search/expanded?q="+hsdDevUnknown+"&type=pool", 200, "ConnectedExpandedSearchResponse"), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Data.Status != "not-indexed" || len(missing.Data.Items) != 0 {
		t.Fatalf("unknown exact address = %+v", missing.Data)
	}
	// A cursor is bound to its query and page size.
	hsdExpectError(t, srv, http.MethodGet, "/v2/search/expanded?q=dog&type=pool&limit=2&cursor="+url.QueryEscape(string(first.Data.NextCursor)), 400, "CURSOR_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search/expanded?q=dog&type=pool&filter=%7B%7D", 501, "FEATURE_UNAVAILABLE")
	hsdExpectError(t, srv, http.MethodGet, "/v2/search/expanded?q=dog&type=pool&sort=volume", 422, "FIELD_INVALID")
}

func hsdPublishCallout(t *testing.T, pool *pgxpool.Pool, c discovery.Callout) {
	t.Helper()
	// The same documents core/discovery persistCallout writes.
	ref := discovery.SourceRef{ID: c.ID, EventID: fmt.Sprintf("callout:%s:%d", c.ID, c.Revision), Kind: "callout", CreatorPublicID: c.AuthorAccountID, Pools: []string{c.PoolAddress}, SourceRevision: c.Revision, VisibilityRevision: c.Revision, PublishedAt: c.CreatedAt, Public: !c.Deleted, Authorized: !c.Deleted, Tombstone: c.Deleted, Corrected: c.Revision > 1 && !c.Deleted}
	callout, _ := json.Marshal(c)
	source, _ := json.Marshal(ref)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO narrative_discovery.callouts(id,document) VALUES($1,$2)`, c.ID, callout); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO narrative_discovery.sources(id,kind,document,published_at,event_id,observed_at) VALUES($1,'callout',$2,$3,$4,$3)`, c.ID, source, c.CreatedAt, ref.EventID); err != nil {
		t.Fatal(err)
	}
}

func TestDevelopmentDiscoveryWithoutSchema(t *testing.T) {
	srv, _ := hsdDevServer(t, false)
	var feed discovery.FeedPage
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/discovery/feed", 200, "ConnectedDiscoveryFeedPage"), &feed); err != nil {
		t.Fatal(err)
	}
	if len(feed.Entries) != 0 || feed.Cursor != "" || feed.Algorithm != "chronological-v1" {
		t.Fatalf("feed without schema = %+v", feed)
	}
	if items, next := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/callouts", 200, "CalloutListResponse")); len(items) != 0 || next != nil {
		t.Fatal("callouts without schema returned items")
	}
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed/"+hsdDevCallout1, 404, "NOT_FOUND")
}

func TestDevelopmentDiscoveryReads(t *testing.T) {
	srv, pool := hsdDevServer(t, true)
	base := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	hsdExpectDocument(t, srv, "/v2/discovery/feed?order=relevance&limit=5", 200, "ConnectedDiscoveryFeedPage")
	if items, _ := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/callouts", 200, "CalloutListResponse")); len(items) != 0 {
		t.Fatal("empty callouts returned items")
	}
	hsdPublishCallout(t, pool, discovery.Callout{ID: hsdDevCallout1, AuthorAccountID: hsdDevWallet, PoolAddress: hsdDevPool, Body: "first", Direction: "bullish", Disclosure: "organic", ModerationStatus: "approved", CreatedAt: base, Revision: 1})
	hsdPublishCallout(t, pool, discovery.Callout{ID: hsdDevCallout2, AuthorAccountID: hsdDevCreator, PoolAddress: hsdDevPool, Body: "second", Direction: "bearish", Disclosure: "creator", ModerationStatus: "approved", PositionDisclosure: "holding", CreatedAt: base.Add(time.Minute), Revision: 1})
	hsdPublishCallout(t, pool, discovery.Callout{ID: hsdDevCallout3, AuthorAccountID: hsdDevWallet, PoolAddress: hsdDevPool2, Body: "", Direction: "neutral", Disclosure: "organic", ModerationStatus: "approved", CreatedAt: base.Add(2 * time.Minute), Revision: 2, Deleted: true})
	// A pool-kind source needs the external source authority development lacks.
	poolRef, _ := json.Marshal(discovery.SourceRef{ID: "pool:" + hsdDevPool2, EventID: "pool:1", Kind: "pool", CreatorPublicID: hsdDevCreator, Pools: []string{hsdDevPool2}, SourceRevision: 1, VisibilityRevision: 1, PublishedAt: base.Add(3 * time.Minute), Public: true, Authorized: true})
	if _, err := pool.Exec(context.Background(), `INSERT INTO narrative_discovery.sources(id,kind,document,published_at,event_id,observed_at) VALUES($1,'pool',$2,$3,'pool:1',$3)`, "pool:"+hsdDevPool2, poolRef, base.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}

	// Feed: newest first, only resolvable public callouts, cursor pagination.
	var page discovery.FeedPage
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/discovery/feed?limit=1", 200, "ConnectedDiscoveryFeedPage"), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Source.ID != hsdDevCallout2 || page.Entries[0].Body != "second" || page.Entries[0].PositionDisclosureKind != "user_declared" || page.Cursor == "" {
		t.Fatalf("feed page 1 = %+v", page)
	}
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/discovery/feed?limit=1&cursor="+page.Cursor, 200, "ConnectedDiscoveryFeedPage"), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 1 || page.Entries[0].Source.ID != hsdDevCallout1 || page.Cursor != "" {
		t.Fatalf("feed page 2 = %+v", page)
	}
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/discovery/feed", 200, "ConnectedDiscoveryFeedPage"), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Entries) != 2 || page.Cursor != "" {
		t.Fatalf("full feed = %+v", page)
	}
	code, body, _ := hsdDevRequest(t, http.MethodGet, srv.URL+"/v2/discovery/feed", http.Header{"Authorization": {"Bearer development"}})
	if code != 200 || interfaces.ValidateJSON("ConnectedDiscoveryFeedPage", body) != nil || !bytes.Contains(body, []byte(`"FallbackReason":"identity_unavailable"`)) {
		t.Fatalf("credentialed feed = %d %s", code, body)
	}
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed?cursor=notacursor", 400, "CURSOR_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed?limit=51", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed?order=hot", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed?page=2", 400, "QUERY_INVALID")

	// Feed details.
	var card discovery.FeedCard
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/discovery/feed/"+hsdDevCallout1, 200, "ConnectedDiscoveryFeedCard"), &card); err != nil {
		t.Fatal(err)
	}
	if card.State != "available" || card.Body != "first" {
		t.Fatalf("callout card = %+v", card)
	}
	if err := json.Unmarshal(hsdExpectDocument(t, srv, "/v2/discovery/feed/"+hsdDevCallout3, 200, "ConnectedDiscoveryFeedCard"), &card); err != nil {
		t.Fatal(err)
	}
	if card.State != "removed" || card.Body != "" {
		t.Fatalf("tombstone card = %+v", card)
	}
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed/pool:"+hsdDevPool2, 503, "DEPENDENCY_UNAVAILABLE")
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/feed/missing", 404, "NOT_FOUND")

	// Pool callouts: live callouts of the pool only, newest first, paginated.
	items, next := hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/callouts?limit=1", 200, "CalloutListResponse"))
	if len(items) != 1 || items[0]["calloutId"] != hsdDevCallout2 || items[0]["authorWallet"] != hsdDevCreator || next == nil {
		t.Fatalf("callouts page 1 = %v %v", items, next)
	}
	items, next = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool+"/callouts?limit=1&cursor="+*next, 200, "CalloutListResponse"))
	if len(items) != 1 || items[0]["calloutId"] != hsdDevCallout1 || items[0]["revision"] != "1" || next != nil {
		t.Fatalf("callouts page 2 = %v %v", items, next)
	}
	if items, _ = hsdDecodeEnvelope(t, hsdExpectDocument(t, srv, "/v2/pools/"+hsdDevPool2+"/callouts", 200, "CalloutListResponse")); len(items) != 0 {
		t.Fatalf("deleted callout listed: %v", items)
	}
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevUnknown+"/callouts", 404, "NOT_FOUND")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/callouts?limit=101", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool+"/callouts?cursor=zz", 400, "CURSOR_INVALID")

	// A callout whose author is an account id, not a wallet, cannot be a
	// contract Callout: the page is quarantined, not rewritten.
	hsdPublishCallout(t, pool, discovery.Callout{ID: "6f1c2a8e-7d64-4bb1-9c39-0b1c3c4c1a04", AuthorAccountID: "account-7", PoolAddress: hsdDevPool2, Body: "x", Direction: "neutral", Disclosure: "organic", ModerationStatus: "approved", CreatedAt: base, Revision: 1})
	hsdExpectError(t, srv, http.MethodGet, "/v2/pools/"+hsdDevPool2+"/callouts", 503, "INTEGRITY_QUARANTINED")
}

func TestDevelopmentDiscoveryLeaderboardsAndWrites(t *testing.T) {
	srv, _ := hsdDevServer(t, true)
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/leaderboards?start=2026-10-12T00:00:00Z&end=2026-10-13T00:00:00Z", 503, "DEPENDENCY_UNAVAILABLE")
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/leaderboards?start=2026-10-13T00:00:00Z&end=2026-10-12T00:00:00Z", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodGet, "/v2/discovery/leaderboards?start=yesterday&end=2026-10-12T00:00:00Z", 422, "FIELD_INVALID")
	hsdExpectError(t, srv, http.MethodPost, "/v2/discovery/callouts", 401, "AUTH_EXPIRED")
	hsdExpectError(t, srv, http.MethodPatch, "/v2/discovery/callouts/"+hsdDevCallout1, 401, "AUTH_EXPIRED")
	if code, _, header := hsdDevRequest(t, http.MethodGet, srv.URL+"/v2/discovery/callouts", nil); code != http.StatusMethodNotAllowed || !strings.Contains(header.Get("Allow"), "POST") {
		t.Fatalf("GET /v2/discovery/callouts = %d allow=%q", code, header.Get("Allow"))
	}
	if code, _, _ := hsdDevRequest(t, http.MethodGet, srv.URL+"/v2/discovery/callouts/"+hsdDevCallout1, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v2/discovery/callouts/{id} = %d", code)
	}
}

// Every development route is a frozen v2 contract operation.
func TestDevelopmentHistoryDiscoveryRoutesAreContractOperations(t *testing.T) {
	raw, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/contracts/v2/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	rest.RegisterDevelopmentHistorySearchDiscovery(router, store.New(nil, nil), nil)
	count := 0
	err = chi.Walk(router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if _, ok := document.Paths[path][strings.ToLower(method)]; !ok {
			t.Errorf("development route absent from the v2 contract: %s %s", method, path)
		}
		count++
		return nil
	})
	if err != nil || count != 10 {
		t.Fatalf("walk = %v, %d routes", err, count)
	}
}
