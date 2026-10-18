package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/migrate"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Only checked-in public local fixture credentials; each test creates and drops
// its own database under the assigned lane namespace. No shared schema writes.
func historyPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("L11_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "kd_a5099b09994e633500d81d10_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if _, err = admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Errorf("cleanup %s: %v", name, err)
		}
		admin.Close(ctx)
	})
	ownedURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ownedURL.Path = "/" + name
	ownedDSN := ownedURL.String()
	cfg, err := pgxpool.ParseConfig(ownedDSN)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 2
	if err := migrate.Up(ctx, ownedDSN); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
func historyURL(extra string) string { return "/history?symbol=" + udfPool + "&resolution=1&" + extra }

type failingHistory struct {
	calls int
	err   error
}

func (s *failingHistory) QueryHistory(context.Context, HistoryRequest, int) ([]store.CandleRow, error) {
	s.calls++
	return nil, s.err
}
func TestHistoryInvalidInputsNeverQuery(t *testing.T) {
	cases := []string{"from=0&to=100&countback=0", "from=0&to=100&countback=5001", "from=no&to=100", "from=1&to=1", "from=2&to=1", "from=0&to=1000000000000", "from=0&to=100&chainId=1", "from=0&to=100&from=1", "from=0&to=31708801", "to=100&countback=bad", "from=0&to=1.5", "from=-1&to=100"}
	source := &failingHistory{}
	h := HistoryHandler{Source: source}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("GET", historyURL(q), nil))
			if rec.Code != 422 {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
	if source.calls != 0 {
		t.Fatalf("invalid inputs queried %d times", source.calls)
	}
}
func TestHistoryDependencyNeverNoData(t *testing.T) {
	source := &failingHistory{err: errors.New("down")}
	h := HistoryHandler{Source: source}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", historyURL("from=0&to=100"), nil))
	if rec.Code != 503 || strings.Contains(rec.Body.String(), "no_data") || !strings.Contains(rec.Body.String(), `"retryable":true`) {
		t.Fatal(rec.Code, rec.Body.String())
	}
}
func TestResolutionVocabulary(t *testing.T) {
	for _, s := range supportedResolutions() {
		n, e := ResolutionSeconds(s)
		if e != nil || n <= 0 {
			t.Fatal(s, n, e)
		}
	}
	for _, s := range []string{"", "1M", "0", "99", "1m", "d"} {
		if _, e := ResolutionSeconds(s); e == nil {
			t.Fatal("accepted", s)
		}
	}
	for s, n := range map[string]int64{"D": 86400, "1D": 86400, "W": 604800, "1W": 604800} {
		got, e := ResolutionSeconds(s)
		if e != nil || got != n {
			t.Fatal(s, got, e)
		}
	}
}
func TestCanonicalPrecisionAndVendorBoundary(t *testing.T) {
	rows := []store.CandleRow{{CandleStart: 60, Open: "1", High: "123456789012345678901234567890", Low: "1", Close: "1000000000000000001", VolumeUsdl: "1000001"}}
	b, e := CanonicalHistory(rows)
	if e != nil {
		t.Fatal(e)
	}
	if b[0].Open != "0.000000000000000001" || b[0].Close != "1.000000000000000001" || b[0].Volume != "1.000001" {
		t.Fatalf("precision lost: %+v", b)
	}
	rec := httptest.NewRecorder()
	writeHistoryResponse(rec, rows)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var v map[string]interface{}
	json.Unmarshal(rec.Body.Bytes(), &v)
	high := v["h"].([]interface{})[0].(float64)
	expected := 123456789012.345678901234567890
	if math.Abs(high-expected) > math.Abs(expected)*1e-15 {
		t.Fatal(high)
	}
	if rows[0].Close != "1000000000000000001" {
		t.Fatal("mutated stored value")
	}
	for _, raw := range []string{"NaN", "-1", "1.0", " 1", "+1", "01", strings.Repeat("9", 1025)} {
		rows[0].Open = raw
		rec := httptest.NewRecorder()
		writeHistoryResponse(rec, rows)
		if rec.Code != 503 {
			t.Fatal("accepted corrupt", raw)
		}
	}
	if _, e := displayNumber("1e999"); e == nil {
		t.Fatal("accepted infinity")
	}
	if _, e := displayNumber("1e-999"); e == nil {
		t.Fatal("accepted underflow")
	}
}
func seedHistory(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO candles.candles (pool_address,timeframe,candle_start,open,high,low,close,volume_usdl,volume_token,buy_volume_usdl,sell_volume_usdl,trade_count,unique_traders,large_trade_count,last_trade_ts,sequence_num,mcap_open,mcap_high,mcap_low,mcap_close) SELECT $1,'1m',i*60,'1','2','1','2','1000001','0','0','0',1,1,0,i*60,$2-i,'0','0','0','0' FROM generate_series(1,$2::int) i`, udfPool, n)
	if err != nil {
		t.Fatal(err)
	}
}
func TestHistoryNativePostgresPaginationAndCountback(t *testing.T) {
	pool := historyPostgres(t)
	seedHistory(t, pool, 5002)
	key := []byte("synthetic-test-cursor-key-32-bytes-only")
	h := HistoryHandler{Source: PostgresHistorySource{pool}, SigningKey: key}
	first, body := doGET(t, h, historyURL("from=60&to=300180"))
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	ts := body["t"].([]interface{})
	if len(ts) != 5000 || ts[0] != float64(60) || ts[4999] != float64(300000) {
		t.Fatal("wrong first page")
	}
	cursor, ok := body["nextCursor"].(string)
	if !ok || len(cursor) > 256 {
		t.Fatal("missing bounded cursor", body)
	}
	second, b := doGET(t, h, historyURL("from=60&to=300180&cursor="+url.QueryEscape(cursor)))
	if second.Code != 200 {
		t.Fatal(second.Body.String())
	}
	tail := b["t"].([]interface{})
	if len(tail) != 2 || tail[0] != float64(300060) || tail[1] != float64(300120) {
		t.Fatal("wrong second page", tail)
	}
	_, adjacent := doGET(t, h, historyURL("from=60&to=120"))
	if len(adjacent["t"].([]interface{})) != 1 {
		t.Fatal("inclusive upper bound")
	}
	_, cb := doGET(t, h, historyURL("to=300120&countback=2"))
	cts := cb["t"].([]interface{})
	if len(cts) != 2 || cts[0] != float64(300000) || cts[1] != float64(300060) {
		t.Fatal("countback used sequence order", cts)
	}
	for _, path := range []string{historyURL("from=60&to=300180&cursor=" + url.QueryEscape(cursor+"x")), historyURL("from=60&to=300240&cursor=" + url.QueryEscape(cursor))} {
		rec, _ := doGET(t, h, path)
		if rec.Code != 400 {
			t.Fatal("accepted changed cursor", rec.Code)
		}
	}
	_, empty := doGET(t, h, historyURL("from=300180&to=300240"))
	if empty["s"] != "no_data" {
		t.Fatal(empty)
	}

}

func TestHistoryNativeDeadlineAndCancellation(t *testing.T) {
	pool := historyPostgres(t)
	ctx := context.Background()
	a, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Release()
	b, e := pool.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Release()
	h := HistoryHandler{Source: PostgresHistorySource{pool}}
	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest("GET", historyURL("from=0&to=120"), nil))
	if rec.Code != 503 || time.Since(start) > 3*time.Second || strings.Contains(rec.Body.String(), "no_data") {
		t.Fatal("deadline not enforced", rec.Code, time.Since(start), rec.Body.String())
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", historyURL("from=0&to=120"), nil).WithContext(canceled))
	if rec.Code != 503 {
		t.Fatal("cancellation not enforced", rec.Code)
	}
}

func TestHistoryCursorExpiryAndMissingKey(t *testing.T) {
	r := HistoryRequest{Symbol: udfPool, Timeframe: "1m", From: 60, To: 600}
	key := []byte("synthetic-test-cursor-key-32-bytes-only")
	h := HistoryHandler{SigningKey: key}
	token, e := h.encodeCursor(r, 120)
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := h.applyCursor(r, token)
	if e != nil || parsed.From != 120 {
		t.Fatal(parsed, e)
	}
	for _, other := range []HistoryRequest{{Symbol: udfPool, Timeframe: "5m", From: 60, To: 600}, {Symbol: udfPool, Timeframe: "1m", From: 61, To: 600}, {Symbol: "0x0000000000000000000000000000000000001111", Timeframe: "1m", From: 60, To: 600}} {
		if _, e := h.applyCursor(other, token); e == nil {
			t.Fatal("cursor did not bind filter", other)
		}
	}
	if _, e := (HistoryHandler{}).encodeCursor(r, 120); e == nil {
		t.Fatal("unsigned cursor accepted")
	}
	if _, e := h.applyCursor(r, strings.Repeat("a", 257)); e == nil {
		t.Fatal("oversized cursor accepted")
	}
	// Sign an actually expired payload with the same injected test key.
	body, _ := json.Marshal(historyCursor{fingerprint(r), 120, time.Now().Add(-time.Minute).Unix()})
	mac := hmac.New(sha256.New, key)
	mac.Write(body)
	expired := base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if _, e := h.applyCursor(r, expired); e == nil {
		t.Fatal("expired cursor accepted")
	}
}

func TestHistoryRequestIdentityAndUnknownFields(t *testing.T) {
	for _, path := range []string{
		"/history?symbol=ABC&resolution=1&from=0&to=120",
		historyURL("from=0&to=120&chainId="),
		historyURL("from=0&to=120&cursor="),
		historyURL("from=0&to=120&unexpected=1"),
		historyURL("from=0&to=120&unused=" + strings.Repeat("x", 16385)),
	} {
		source := &failingHistory{}
		rec, _ := doGET(t, HistoryHandler{Source: source}, path)
		if rec.Code != 422 || source.calls != 0 {
			t.Fatal("invalid request queried source", rec.Code, source.calls)
		}
	}
}

func TestHistoryMalformedEncodingNeverQueries(t *testing.T) {
	for _, query := range []string{
		"from=0&to=120&countback=%zz",
		"from=0&to=120&countback=1;ignored=2",
		"from=0&to=120&cursor=%",
		"from=0&to=120&unrecognized%=1",
	} {
		rec := httptest.NewRecorder()
		(HistoryHandler{Source: PostgresHistorySource{}}).ServeHTTP(rec, httptest.NewRequest("GET", historyURL(query), nil))
		if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"code":"FIELD_INVALID"`) {
			t.Fatalf("query=%q status=%d body=%s", query, rec.Code, rec.Body.String())
		}
	}
}

func TestCanonicalAndVendorHistoryHaveSeparatePageBounds(t *testing.T) {
	rows := make([]store.CandleRow, MaxHistoryBars)
	for i := range rows {
		rows[i] = store.CandleRow{CandleStart: int64(i) * 60, Open: "1", High: "2", Low: "1", Close: "2", VolumeUsdl: "1000001"}
	}
	for _, count := range []int{100, 101, 2000} {
		if bars, err := CanonicalHistory(rows[:count]); err != nil || len(bars) != count {
			t.Fatalf("canonical count=%d bars=%d err=%v", count, len(bars), err)
		}
	}
	if _, err := CanonicalHistory(rows[:2001]); err == nil {
		t.Fatal("canonical 2001 accepted")
	}
	recorder := httptest.NewRecorder()
	writeHistoryResponse(recorder, rows)
	if recorder.Code != 200 {
		t.Fatalf("vendor5000 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if _, err := parseHistory(url.Values{"symbol": {udfPool}, "resolution": {"1"}, "to": {"400000"}, "countback": {"5000"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := parseHistory(url.Values{"symbol": {udfPool}, "resolution": {"1"}, "to": {"400000"}, "countback": {"5001"}}); err == nil {
		t.Fatal("vendor5001 accepted")
	}
}

func TestHistoryCancelled5000BarQueryReleasesConnection(t *testing.T) {
	pool := historyPostgres(t)
	seedHistory(t, pool, MaxHistoryBars)
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "LOCK TABLE candles.candles IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		(HistoryHandler{Source: PostgresHistorySource{Pool: pool}}).ServeHTTP(response, httptest.NewRequest("GET", historyURL("to=300060&countback=5000"), nil).WithContext(requestCtx))
	}()
	observed := false
	for i := 0; i < 100; i++ {
		if err = blocker.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%FROM candles.candles%LIMIT $5%')").Scan(&observed); err != nil {
			t.Fatal(err)
		}
		if observed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !observed {
		t.Fatal("5000-bar query never reached the actual PostgreSQL lock")
	}
	canceledAt := time.Now()
	cancel()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("query outlived cancellation", ctx.Err())
	}
	if response.Code != 503 || strings.Contains(response.Body.String(), "no_data") || time.Since(canceledAt) > time.Second {
		t.Fatal(response.Code, response.Body.String(), time.Since(canceledAt))
	}
	available, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("canceled query leaked pool connection", err)
	}
	if err = available.QueryRow(ctx, "SELECT 1").Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	available.Release()
	if pool.Stat().AcquiredConns() != 1 {
		t.Fatal("only table blocker may retain its connection", pool.Stat().AcquiredConns())
	}
}

func TestHistorySQLTimeoutReleasesConnection(t *testing.T) {
	pool := historyPostgres(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "LOCK TABLE candles.candles IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response, body := doGET(t, HistoryHandler{Source: PostgresHistorySource{Pool: pool}}, historyURL("from=0&to=120"))
	if response.Code != 503 || body["retryable"] != true || time.Since(started) > 2*time.Second || body["s"] == "no_data" {
		t.Fatal(response.Code, body, time.Since(started))
	}
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal("timed out query leaked connection", err)
	}
	connection.Release()
}

func TestHistoryLegacyRegistrationRefusesUnboundedRead(t *testing.T) {
	pool := historyPostgres(t)
	st := store.New(pool)
	response, body := doGET(t, udfHistory(st), historyURL("from=0&to=120"))
	if response.Code != 503 || body["code"] != "DEPENDENCY_UNAVAILABLE" || body["s"] == "no_data" {
		t.Fatal(response.Code, body)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("legacy read acquired SQL connection")
	}
}

func FuzzHistoryValidatedInputs(f *testing.F) {
	for _, count := range []string{"1", "5000", "5001", "0", "-1", "NaN", "999999999999999999999"} {
		f.Add(count, "1", udfPool, "", "0", "120")
	}
	f.Add("5000", "1W", udfPool, "tampered.signature", "0", "31708801")
	f.Fuzz(func(t *testing.T, count, resolution, address, cursor, from, to string) {
		if len(count)+len(resolution)+len(address)+len(cursor)+len(from)+len(to) > 32768 {
			return
		}
		query := url.Values{"countback": {count}, "resolution": {resolution}, "symbol": {address}, "from": {from}, "to": {to}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		request, err := parseHistory(query)
		if err != nil {
			return
		}
		if err = validateHistoryRequest(request); err != nil {
			t.Fatal("parser admitted invalid request", request, err)
		}
		if cursor != "" {
			h := HistoryHandler{SigningKey: []byte("synthetic-test-cursor-key-32-bytes-only")}
			continued, err := h.applyCursor(request, cursor)
			if err == nil && validateHistoryRequest(continued) != nil {
				t.Fatal("cursor escaped request bounds", continued)
			}
		}
	})
}
