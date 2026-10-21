package search_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/search"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSearchHTTPValidationAndDependencyErrors(t *testing.T) {
	router := chi.NewRouter()
	httpapi.RegisterCanonicalSearch(router, search.Store{})
	cases := []struct {
		path   string
		status int
	}{
		{"/v2/search?q=a&type=pool", 422},
		{"/v2/search?q=coin&type=pool&graduation=true", 400},
		{"/v2/search?q=coin&type=pool&limit=26", 422},
		{"/v2/search?q=coin&type=pool&limit=01", 422},
		{"/v2/search?q=coin&type=profile", 412},
		{"/v2/search?q=coin&type=pool", 503},
		{"/v2/search?q=coin&type=pool&filter=" + url.QueryEscape(`{"poolStatus":"graduated"}`), 422},
	}
	for _, c := range cases {
		request := httptest.NewRequest("GET", c.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != c.status {
			t.Fatal(c.path, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("error cache policy")
		}
	}
}
func TestExpandedSearchParserBounds(t *testing.T) {
	for _, query := range []url.Values{{"q": {"coin"}, "type": {"pool"}, "sort": {"relevance;DROP TABLE"}}, {"q": {"coin"}, "type": {"pool"}, "filter": {`{"unknown":true}`}}, {"q": {"coin", "other"}, "type": {"pool"}}, {"q": {"coin"}, "type": {"pool"}, "filter": {`{"poolStatus":"active","poolStatus":"paused"}`}}} {
		if _, err := search.ParseIntegratedRequest(query); err == nil {
			t.Fatal("unbounded/unknown query admitted")
		}
	}
	if _, err := search.ParseIntegratedRequest(url.Values{"q": {"coin"}, "type": {"pool"}, "filter": {`{"liquidityRange":{"asset":"USDL","decimals":6,"depth":"total","range":{"minInclusive":"1000000","maxExclusive":"2000000"}}}`}}); err != nil {
		t.Fatal(err)
	}
}

func httpSearchDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("L20_TEST_DSN")
	if dsn == "" {
		t.Fatal("L20_TEST_DSN required for actual HTTP/PostgreSQL corpus")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	database := "l20_http_" + hex.EncodeToString(nonce[:])
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{database}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.Database = database
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := admin.Exec(cleanup, "DROP DATABASE "+pgx.Identifier{database}.Sanitize()+" WITH (FORCE)")
		admin.Close()
		if err != nil {
			t.Error(err)
		}
	})
	ddl, err := os.ReadFile("schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO stats_search.generations_v2(generation,state,source_watermark,source_observed_at) VALUES('http_generation','active',1,clock_timestamp());INSERT INTO stats_search.active_v2 VALUES(true,'http_generation',nextval('stats_search.revision_v2'),1);CREATE SCHEMA users`); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	for _, name := range []string{"330000.sql", "350000.sql", "350001.sql"} {
		bytes, err := os.ReadFile("../../../../../launchpad/spec/production-system/integration/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, string(bytes)); err != nil {
			t.Fatal(name, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return pool
}
func corpusAddress(number int) string { return fmt.Sprintf("0x%040x", number) }
func TestExpandedHTTPSuccessfulMultilingualAndDeletedProfileCorpus(t *testing.T) {
	raw, err := os.ReadFile("quality-corpus.json")
	if err != nil {
		t.Fatal(err)
	}
	type queryCase struct {
		Query    string `json:"query"`
		Expected []int  `json:"expected"`
	}
	var corpus struct {
		Version   int `json:"version"`
		Documents []struct {
			Number int    `json:"number"`
			Name   string `json:"name"`
			Ticker string `json:"ticker"`
			Status string `json:"status"`
		} `json:"documents"`
		Cases    []queryCase `json:"cases"`
		Profiles []struct {
			Number      int    `json:"number"`
			DisplayName string `json:"displayName"`
			Deleted     bool   `json:"deleted"`
			Public      bool   `json:"public"`
		} `json:"profiles"`
		ProfileCases []queryCase `json:"profileCases"`
	}
	if json.Unmarshal(raw, &corpus) != nil || corpus.Version != 2 || len(corpus.ProfileCases) == 0 {
		t.Fatal("invalid corpus")
	}
	pool := httpSearchDatabase(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	labels := map[string]string{}
	for _, item := range corpus.Documents {
		q, err := search.Normalize(item.Name)
		if err != nil {
			t.Fatal(err)
		}
		ticker, err := search.Normalize(item.Ticker)
		if err != nil {
			t.Fatal(err)
		}
		wallet := corpusAddress(item.Number)
		d := search.IndexedDocument{Document: search.Document{Version: 1, ChainID: "125", PoolAddress: wallet, TokenAddress: corpusAddress(999), Name: item.Name, Ticker: item.Ticker, NameKey: q.Key, CanonicalTicker: strings.ToUpper(ticker.Key), LanguageHints: []string{}, ConfusableWarnings: []string{}, CreationEventID: fmt.Sprintf("corpus_%d", item.Number), ChainHeight: "1", MetadataRevision: 1, VisibilityRevision: 1, PoolStatus: item.Status, IndexedAt: now.Format("2006-01-02T15:04:05.000Z")}, Creator: corpusAddress(item.Number + 1000), CreatedAt: now.Add(-time.Hour), LifecycleRevision: 1, Volumes: map[string]string{}, RiskFlags: []string{}, SourceHeight: "1", SourceBlockHash: "0x" + strings.Repeat("1", 64)}
		if err = d.Validate(); err != nil {
			t.Fatal(err)
		}
		payload, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO stats_search.source_documents_v2 VALUES($1,$2)`, wallet, payload); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO stats_search.documents_v2(generation,pool_address,token_address,name_key,ticker_key,creator,created_at,pool_status,visibility_revision,revision,risk_flags,payload) VALUES('http_generation',$1,$2,$3,$4,$5,$6,$7,1,nextval('stats_search.revision_v2'),'{}',$8)`, wallet, d.TokenAddress, d.NameKey, strings.ToLower(d.CanonicalTicker), d.Creator, d.CreatedAt, d.PoolStatus, payload); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO stats_search.authority_v2 VALUES($1,1,true,false)`, wallet); err != nil {
			t.Fatal(err)
		}
		labels[wallet] = item.Name
	}
	for _, profile := range corpus.Profiles {
		account := uuid.New()
		wallet := corpusAddress(profile.Number)
		payload, _ := json.Marshal(map[string]any{"wallet": wallet, "displayName": profile.DisplayName, "bio": "private corpus bio excluded from search", "revision": "1"})
		visibility := "private"
		if profile.Public {
			visibility = "public_optin"
		}
		if _, err = pool.Exec(ctx, `INSERT INTO users.identity_accounts VALUES($1,0,$2);INSERT INTO users.identity_wallet_links VALUES($3,$1,125,$4,$5,'verified',$2)`, account, now, uuid.New(), wallet, visibility); err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO users.account_state(account_id,wallet,revision,deleted,profile,preferences,updated_at) VALUES($1,$2,1,$3,$4,'{}',$5)`, account, wallet, profile.Deleted, payload, now); err != nil {
			t.Fatal(err)
		}
		key, err := search.Normalize(profile.DisplayName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO stats_search.profile_documents_v2 VALUES($1,$2,$3,1,$4)`, wallet, profile.DisplayName, key.Key, now); err != nil {
			t.Fatal(err)
		}
		labels[wallet] = profile.DisplayName
	}
	if _, err = pool.Exec(ctx, `UPDATE stats_search.active_v2 SET revision=nextval('stats_search.revision_v2')`); err != nil {
		t.Fatal(err)
	}
	service, err := search.NewService(search.Store{Pool: pool}, nil, 4)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	httpapi.RegisterIntegratedSearch(router, service, nil)
	requestPage := func(kind, query string) search.IntegratedPage {
		t.Helper()
		request := httptest.NewRequest("GET", "/v2/search/expanded?"+url.Values{"q": {query}, "type": {kind}}.Encode(), nil)
		request.RemoteAddr = "192.0.2.20:42100"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatal(kind, query, response.Code, response.Body.String())
		}
		if response.Header().Get("ETag") == "" || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(response.Header())
		}
		var envelope struct {
			Data    search.IntegratedPage `json:"data"`
			Quality string                `json:"quality"`
		}
		if json.Unmarshal(response.Body.Bytes(), &envelope) != nil || envelope.Quality != "partial" {
			t.Fatal(response.Body.String())
		}
		if strings.Contains(response.Body.String(), "<script>") || strings.Contains(response.Body.String(), "<img") {
			t.Fatal("hostile markup was not JSON escaped")
		}
		if strings.Contains(response.Body.String(), "private corpus bio") {
			t.Fatal("private profile field disclosed")
		}
		return envelope.Data
	}
	for _, family := range []struct {
		kind  string
		cases []queryCase
	}{{"pool", corpus.Cases}, {"profile", corpus.ProfileCases}} {
		for _, c := range family.cases {
			page := requestPage(family.kind, c.Query)
			if len(page.Items) != len(c.Expected) {
				t.Fatal(family.kind, c.Query, page)
			}
			for index, number := range c.Expected {
				hit := page.Items[index]
				address := hit.PoolAddress
				if family.kind == "profile" {
					address = hit.Wallet
					if hit.PoolAddress != nil || hit.TokenAddress != nil {
						t.Fatal("profile gained fabricated chain associations")
					}
				}
				if address == nil || *address != corpusAddress(number) || hit.Label != labels[*address] {
					t.Fatal(c, hit)
				}
			}
		}
	}
	if _, err = pool.Exec(ctx, `UPDATE users.account_state SET deleted=true,revision=2 WHERE wallet=$1`, corpusAddress(701)); err != nil {
		t.Fatal(err)
	}
	if page := requestPage("profile", "Café"); len(page.Items) != 0 {
		t.Fatal("current deletion did not override stale indexed profile", page)
	}
	if _, err = pool.Exec(ctx, `UPDATE users.account_state SET profile=jsonb_set(profile,'{displayName}','"Café restored"') WHERE wallet=$1`, corpusAddress(701)); err != nil {
		t.Fatal(err)
	}
	if page := requestPage("profile", corpusAddress(701)); len(page.Items) != 0 {
		t.Fatal("late profile rewrite resurrected deleted account", page)
	}
	if _, err = pool.Exec(ctx, `UPDATE users.identity_wallet_links SET visibility='private' WHERE wallet=$1`, corpusAddress(702)); err != nil {
		t.Fatal(err)
	}
	if page := requestPage("profile", "مرحبا"); len(page.Items) != 0 {
		t.Fatal("revoked public visibility exposed cached profile", page)
	}
}

func TestSearchHTTPRejectsHostileMalformedParameters(t *testing.T) {
	router := chi.NewRouter()
	httpapi.RegisterCanonicalSearch(router, search.Store{})
	httpapi.RegisterIntegratedSearch(router, nil, nil)
	for _, test := range []struct {
		query  url.Values
		status int
	}{
		{url.Values{"q": {"0x" + strings.Repeat("z", 40)}, "type": {"pool"}}, 422},
		{url.Values{"q": {"0x" + strings.Repeat("1", 64)}, "type": {"pool"}}, 422},
		{url.Values{"q": {string([]byte{255, 255})}, "type": {"pool"}}, 422},
		{url.Values{"q": {"coin"}, "type": {"pool"}, "filter": {`{"poolStatus":"active","poolStatus":"paused"}`}}, 400},
		{url.Values{"q": {"coin"}, "type": {"pool"}, "filter": {`{"poolStatus":"active","pool\u0053tatus":"paused"}`}}, 400},
		{url.Values{"q": {"coin"}, "type": {"pool"}, "filter": {`{"creator":"0x'; DROP TABLE"}`}}, 422},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/v2/search?"+test.query.Encode(), nil))
		if response.Code != test.status {
			t.Fatal(test.query, response.Code, response.Body.String())
		}
		if response.Header().Get("Cache-Control") != "no-store" || response.Header().Get("X-Schema-Version") != "2" {
			t.Fatal(response.Header())
		}
	}
}

func TestExpandedHTTPDatabaseTimeoutRateLimitAndRecovery(t *testing.T) {
	pool := httpSearchDatabase(t)
	service, err := search.NewService(search.Store{Pool: pool}, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	httpapi.RegisterIntegratedSearch(router, service, nil)
	perform := func(ip string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest("GET", "/v2/search/expanded?"+url.Values{"q": {corpusAddress(1)}, "type": {"pool"}}.Encode(), nil)
		request.RemoteAddr = ip + ":42100"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	ctx := context.Background()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err = blocker.Exec(ctx, "LOCK TABLE stats_search.active_v2 IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response := perform("192.0.2.30")
	if response.Code != 503 || response.Header().Get("Retry-After") != "1" || time.Since(started) > time.Second {
		t.Fatal(response.Code, response.Body.String(), time.Since(started))
	}
	var failure struct {
		Code      string `json:"code"`
		Retryable bool   `json:"retryable"`
	}
	if json.Unmarshal(response.Body.Bytes(), &failure) != nil || failure.Code != "DEPENDENCY_UNAVAILABLE" || !failure.Retryable {
		t.Fatal(response.Body.String())
	}
	if err = blocker.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if response = perform("192.0.2.30"); response.Code != 200 || !strings.Contains(response.Body.String(), `"status":"not-indexed"`) {
		t.Fatal(response.Code, response.Body.String())
	}
	for i := 0; i < 120; i++ {
		if err = service.Admit("192.0.2.31"); err != nil {
			t.Fatal(err)
		}
	}
	response = perform("192.0.2.31")
	if response.Code != 429 || response.Header().Get("Retry-After") != "60" || !strings.Contains(response.Body.String(), "Search request budget exhausted") {
		t.Fatal(response.Code, response.Header(), response.Body.String())
	}
}
