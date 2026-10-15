package rest

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/auth"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
)

func accountRequest(handler http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func accountResponse(t *testing.T, rec *httptest.ResponseRecorder, code int) map[string]json.RawMessage {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("status %d, want %d", rec.Code, code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatal("response is not JSON")
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func accountOK(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	body := accountResponse(t, rec, http.StatusOK)
	wantKeys(t, body, "ok")
	if !field[bool](t, body, "ok") {
		t.Fatal("ok is false")
	}
}

func accountError(t *testing.T, rec *httptest.ResponseRecorder, code int, message string) {
	t.Helper()
	body := accountResponse(t, rec, code)
	wantKeys(t, body, "error")
	if field[string](t, body, "error") != message {
		t.Fatalf("error = %s, want %q", body["error"], message)
	}
}

func accountLogin(t *testing.T, mux *http.ServeMux) (common.Address, string) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	address := crypto.PubkeyToAddress(key.PublicKey)
	body := accountResponse(t, accountRequest(mux, "POST", "/v2/auth/challenge", "", `{"address":"`+address.Hex()+`"}`), 200)
	message := field[string](t, body, "message")
	sig, err := crypto.Sign(accounts.TextHash([]byte(message)), key)
	if err != nil {
		t.Fatal(err)
	}
	sig[crypto.RecoveryIDOffset] += 27
	raw, err := json.Marshal(map[string]string{"address": address.Hex(), "message": message, "signature": hexutil.Encode(sig)})
	if err != nil {
		t.Fatal(err)
	}
	body = accountResponse(t, accountRequest(mux, "POST", "/v2/auth/verify", "", string(raw)), 200)
	wantNumber(t, body, "expires_in", 3600)
	token := field[string](t, body, "token")
	if token == "" {
		t.Fatal("empty token")
	}
	return address, token
}

func accountExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestAccountV2(t *testing.T) {
	url := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	accountExec(t, db, `DROP SCHEMA IF EXISTS perps CASCADE`)
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var recorded int
	if err := db.QueryRow(`SELECT count(*) FROM perps.schema_migrations WHERE file IN ('0003_delivery.sql', '0004_accounts.sql')`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != 2 {
		t.Fatalf("migration records %d, want 2", recorded)
	}
	now := time.Now().UTC().Truncate(time.Second)
	clock := func() time.Time { return now }
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	service, err := auth.NewService(db, hex.EncodeToString(secret[:]), clock)
	if err != nil {
		t.Fatal(err)
	}
	routes, err := NewAccountV2(db, service, clock)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAccountV2(nil, service, clock); err == nil {
		t.Fatal("nil database accepted")
	}
	if _, err := NewAccountV2(db, nil, clock); err == nil {
		t.Fatal("nil auth accepted")
	}
	mux := http.NewServeMux()
	routes.Register(mux)
	mux.HandleFunc("POST /v2/auth/challenge", service.Challenge)
	mux.HandleFunc("POST /v2/auth/verify", service.Verify)
	addressA, tokenA := accountLogin(t, mux)
	addressB, tokenB := accountLogin(t, mux)
	request := func(method, path, token, body string) *httptest.ResponseRecorder {
		return accountRequest(mux, method, path, token, body)
	}
	generate := func(t *testing.T, token string) string {
		t.Helper()
		body := accountResponse(t, request("POST", "/v2/referrals/generate", token, ""), 200)
		wantKeys(t, body, "referralCode")
		code := field[string](t, body, "referralCode")
		raw, err := hex.DecodeString(code)
		if err != nil || len(raw) != 16 {
			t.Fatal("invalid generated referral code")
		}
		return code
	}
	createAlert := func(t *testing.T, token, payload string) int64 {
		t.Helper()
		body := accountResponse(t, request("POST", "/v2/alerts", token, payload), 200)
		wantKeys(t, body, "ok", "id")
		if !field[bool](t, body, "ok") {
			t.Fatal("ok is false")
		}
		id := field[int64](t, body, "id")
		if id <= 0 {
			t.Fatal("invalid alert id")
		}
		return id
	}

	t.Run("daily_pnl", func(t *testing.T) {
		address := common.HexToAddress("0x00000000000000000000000000000000000000a1")
		day := now.Truncate(24 * time.Hour)
		for i, pnl := range []string{"-2.500000000000000001", "3.125000000000000001"} {
			accountExec(t, db, `INSERT INTO perps.user_pnl_daily (account, day, trading_pnl, fees, funding, realized_pnl, volume, trades)
                VALUES ($1, $2, $3, 0, 0, $3, $4, $5)`, address.Bytes(), day.AddDate(0, 0, i), e18(t, pnl), e18(t, "10.25"), i+1)
		}
		body := accountResponse(t, request("GET", "/v2/user/"+address.Hex()+"/daily-pnl", "", ""), 200)
		wantKeys(t, body, "dailyPnl")
		got := field[[]dailyPnlRow](t, body, "dailyPnl")
		want := []dailyPnlRow{{day.Format(time.DateOnly), "-2500000000000000001", "10250000000000000000", 1}, {day.AddDate(0, 0, 1).Format(time.DateOnly), "3125000000000000001", "10250000000000000000", 2}}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("daily pnl %+v, want %+v", got, want)
		}
		for _, row := range field[[]map[string]json.RawMessage](t, body, "dailyPnl") {
			wantKeys(t, row, "date", "pnl", "volume", "trades")
		}
		body = accountResponse(t, request("GET", "/v2/user/"+addressB.Hex()+"/daily-pnl", "", ""), 200)
		if string(body["dailyPnl"]) != "[]" {
			t.Fatal("empty account did not return []")
		}
		accountError(t, request("GET", "/v2/user/invalid/daily-pnl", "", ""), 400, "invalid address")
	})

	t.Run("leaderboard_shape", func(t *testing.T) {
		for i, address := range []common.Address{addressA, addressB} {
			accountExec(t, db, `INSERT INTO perps.leaderboard ("window", rank, account, realized_pnl, volume_usd, trades_count, win_rate, avg_hold_time_seconds, open_positions)
                VALUES ('all', $1, $2, $3, $4, $5, $6, 0, 0)`, i+1, address.Bytes(), e18(t, []string{"9.500000000000000001", "-3.000000000000000001"}[i]), e18(t, []string{"10", "20"}[i]), i+2, []string{"75.125", "25"}[i])
		}
		body := accountResponse(t, request("GET", "/v2/leaderboard?metric=pnl&period=all&limit=200", "", ""), 200)
		wantKeys(t, body, "leaderboard")
		want := []accountLeaderboardRow{{1, strings.ToLower(addressA.Hex()), "9500000000000000001", "10000000000000000000", 2, 75.125}, {2, strings.ToLower(addressB.Hex()), "-3000000000000000001", "20000000000000000000", 3, 25}}
		if got := field[[]accountLeaderboardRow](t, body, "leaderboard"); !reflect.DeepEqual(got, want) {
			t.Fatalf("leaderboard %+v, want %+v", got, want)
		}
		for _, row := range field[[]map[string]json.RawMessage](t, body, "leaderboard") {
			wantKeys(t, row, "rank", "address", "pnl", "volume", "trades", "win_rate")
		}
		body = accountResponse(t, request("GET", "/v2/leaderboard?metric=volume&limit=1", "", ""), 200)
		want = []accountLeaderboardRow{{1, strings.ToLower(addressB.Hex()), "-3000000000000000001", "20000000000000000000", 3, 25}}
		if got := field[[]accountLeaderboardRow](t, body, "leaderboard"); !reflect.DeepEqual(got, want) {
			t.Fatalf("volume leaderboard %+v, want %+v", got, want)
		}
		for _, period := range []string{"7d", "30d"} {
			body = accountResponse(t, request("GET", "/v2/leaderboard?period="+period, "", ""), 200)
			if string(body["leaderboard"]) != "[]" {
				t.Fatal("period leaked all-time rows")
			}
		}
		for _, param := range []string{"metric", "period", "limit"} {
			accountError(t, request("GET", "/v2/leaderboard?"+param+"=invalid", "", ""), 400, "invalid "+param)
		}
	})

	t.Run("alerts_create_delete", func(t *testing.T) {
		id := createAlert(t, tokenA, `{"alertType":"price_above","marketId":"0","threshold":"123.456789012345678901"}`)
		var owner []byte
		var kind, market, threshold string
		var enabled bool
		var triggered sql.NullTime
		var created time.Time
		if err := db.QueryRow(`SELECT address, alert_type, market_id, threshold, enabled, triggered_at, created_at FROM perps.user_alerts WHERE id = $1`, id).Scan(&owner, &kind, &market, &threshold, &enabled, &triggered, &created); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(owner, addressA.Bytes()) || kind != "price_above" || market != "0" || threshold != "123.456789012345678901" || !enabled || triggered.Valid || !created.Equal(now) {
			t.Fatal("stored alert fields differ")
		}
		accountOK(t, request("DELETE", fmt.Sprintf("/v2/alerts/%d", id), tokenA, ""))
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM perps.user_alerts WHERE id = $1`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("deleted alert remains")
		}
		accountError(t, request("DELETE", fmt.Sprintf("/v2/alerts/%d", id), tokenA, ""), 404, "alert not found")
		id = createAlert(t, tokenA, `{"alertType":"liquidation"}`)
		var absent bool
		if err := db.QueryRow(`SELECT market_id IS NULL AND threshold IS NULL FROM perps.user_alerts WHERE id = $1`, id).Scan(&absent); err != nil {
			t.Fatal(err)
		}
		if !absent {
			t.Fatal("optional alert fields are not null")
		}
		createAlert(t, tokenA, `{"alertType":"price_above","marketId":1,"threshold":0}`)
		createAlert(t, tokenA, `{"alertType":"price_above","marketId":"`+bigint.Uint256Max+`"}`)
		for _, tc := range []struct{ body, message string }{
			{`{"alertType":""}`, "invalid alert type"},
			{`{"alertType":"price_above","marketId":-1}`, "invalid market id"},
			{`{"alertType":"price_above","marketId":1.5}`, "invalid market id"},
			{`{"alertType":"price_above","marketId":"` + bigint.Uint256Max + `0"}`, "invalid market id"},
			{`{"alertType":"price_above","threshold":"NaN"}`, "invalid threshold"},
			{`{"alertType":"price_above","threshold":1e400}`, "invalid threshold"},
			{`{"alertType":"price_above","address":"` + addressB.Hex() + `"}`, "invalid request body"},
			{`{"alertType":"price_above"} {}`, "invalid request body"},
			{`{"alertType":"` + strings.Repeat("x", 4096) + `"}`, "invalid request body"},
		} {
			accountError(t, request("POST", "/v2/alerts", tokenA, tc.body), 400, tc.message)
		}
		accountError(t, request("DELETE", "/v2/alerts/0", tokenA, ""), 400, "invalid id")
	})

	t.Run("alerts_other_account_404", func(t *testing.T) {
		id := createAlert(t, tokenB, `{"alertType":"price_below"}`)
		accountError(t, request("DELETE", fmt.Sprintf("/v2/alerts/%d", id), tokenA, ""), 404, "alert not found")
		var owner []byte
		if err := db.QueryRow(`SELECT address FROM perps.user_alerts WHERE id=$1`, id).Scan(&owner); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(owner, addressB.Bytes()) {
			t.Fatal("alert ownership changed")
		}
	})

	t.Run("referral_generate_apply", func(t *testing.T) {
		code := generate(t, tokenA)
		if generate(t, tokenA) != code {
			t.Fatal("generation changed existing code")
		}
		payload := `{"code":"` + code + `"}`
		accountOK(t, request("POST", "/v2/referrals/apply", tokenB, payload))
		var owner []byte
		var applied time.Time
		var count int
		if err := db.QueryRow(`SELECT referred_by, applied_at FROM perps.referrals WHERE address=$1`, addressB.Bytes()).Scan(&owner, &applied); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(owner, addressA.Bytes()) || !applied.Equal(now) {
			t.Fatal("referral binding differs")
		}
		if err := db.QueryRow(`SELECT count(*) FROM perps.referrals WHERE referred_by=$1`, addressA.Bytes()).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("referral count %d, want 1", count)
		}
		accountError(t, request("POST", "/v2/referrals/apply", tokenB, payload), 400, "invalid or already applied referral code")
		codeB := generate(t, tokenB)
		if codeB == code {
			t.Fatal("duplicate referral code")
		}
		addressC, tokenC := accountLogin(t, mux)
		var wg sync.WaitGroup
		responses := make(chan *httptest.ResponseRecorder, 2)
		for _, candidate := range []string{code, codeB} {
			wg.Add(1)
			go func(code string) {
				defer wg.Done()
				responses <- request("POST", "/v2/referrals/apply", tokenC, `{"code":"`+code+`"}`)
			}(candidate)
		}
		wg.Wait()
		close(responses)
		successes, refusals := 0, 0
		for rec := range responses {
			if rec.Code == 200 {
				accountOK(t, rec)
				successes++
			} else {
				accountError(t, rec, 400, "invalid or already applied referral code")
				refusals++
			}
		}
		if successes != 1 || refusals != 1 {
			t.Fatalf("concurrent apply: %d accepted, %d refused", successes, refusals)
		}
		if err := db.QueryRow(`SELECT count(*) FROM perps.referrals WHERE address=$1 AND referred_by IS NOT NULL`, addressC.Bytes()).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("concurrent application did not produce one binding")
		}
		accountError(t, request("POST", "/v2/referrals/apply", tokenA, `{"code":"unknown"}`), 400, "invalid or already applied referral code")
	})

	t.Run("referral_self_refused", func(t *testing.T) {
		code := generate(t, tokenA)
		accountError(t, request("POST", "/v2/referrals/apply", tokenA, `{"code":"`+code+`"}`), 400, "invalid or already applied referral code")
		var absent bool
		if err := db.QueryRow(`SELECT referred_by IS NULL AND applied_at IS NULL FROM perps.referrals WHERE address=$1`, addressA.Bytes()).Scan(&absent); err != nil {
			t.Fatal(err)
		}
		if !absent {
			t.Fatal("self referral stored")
		}
	})

	t.Run("competition_join", func(t *testing.T) {
		var id int64
		if err := db.QueryRow(`INSERT INTO perps.competitions (name,metric,start_at,end_at,prize_pool,status)
            VALUES ('Trading competition','pnl',$1,$2,0,'active') RETURNING id`, now.Add(-time.Hour), now.Add(time.Hour)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/v2/competitions/%d/join", id)
		accountOK(t, request("POST", path, tokenA, `{"address":"`+addressB.Hex()+`"}`))
		accountOK(t, request("POST", path, tokenA, ""))
		var owner []byte
		var joined time.Time
		if err := db.QueryRow(`SELECT address,joined_at FROM perps.competition_entries WHERE competition_id=$1`, id).Scan(&owner, &joined); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(owner, addressA.Bytes()) || !joined.Equal(now) {
			t.Fatal("competition entry differs")
		}
		for _, end := range []time.Time{now, now.Add(-time.Second)} {
			accountExec(t, db, `UPDATE perps.competitions SET end_at=$2 WHERE id=$1`, id, end)
			accountError(t, request("POST", path, tokenB, ""), 400, "competition unavailable")
		}
		var count int
		if err := db.QueryRow(`SELECT count(*) FROM perps.competition_entries WHERE competition_id=$1`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("duplicate or late competition entry stored")
		}
		accountError(t, request("POST", "/v2/competitions/9223372036854775807/join", tokenB, ""), 400, "competition unavailable")
		accountError(t, request("POST", "/v2/competitions/invalid/join", tokenB, ""), 400, "invalid id")
	})

	t.Run("unauthenticated_401", func(t *testing.T) {
		for _, tc := range []struct{ method, path string }{
			{"POST", "/v2/alerts"}, {"DELETE", "/v2/alerts/1"}, {"POST", "/v2/referrals/generate"},
			{"POST", "/v2/referrals/apply"}, {"POST", "/v2/competitions/1/join"},
		} {
			accountError(t, request(tc.method, tc.path, "", `{}`), 401, "missing bearer token")
			accountError(t, request(tc.method, tc.path, "invalid", `{}`), 401, "invalid token")
			now = now.Add(auth.TokenTTL)
			accountError(t, request(tc.method, tc.path, tokenA, `{}`), 401, "invalid token")
			now = now.Add(-auth.TokenTTL)
		}
	})
}
