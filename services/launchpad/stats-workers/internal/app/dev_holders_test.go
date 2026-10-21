package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sharedconfig "github.com/Sidiora-Technologies/KindleLaunch/shared/config"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// TestDevAppServesHoldersContract builds the real development App (migrations,
// pgxpool, Redis, router, CORS, rate limit) against ephemeral Postgres + Redis
// and checks GET /v1/pools/{pool}/holders answers the IF_HOLDERS contract
// instead of the plain-text 404 it returned before dev registration (D4).
func TestDevAppServesHoldersContract(t *testing.T) {
	ctx := context.Background()
	dsn, pg := internaltest.NewPostgresWithDSN(t)
	redisURL := internaltest.NewRedisURL(t)
	const origin = "https://web.dev.example"
	cfg := config.Config{
		BaseEnv:            sharedconfig.BaseEnv{DatabaseURL: dsn, RedisURL: redisURL, NodeEnv: "development", ChainID: 125},
		WebhookHMACSecret:  strings.Repeat("s", 32),
		CORSAllowedOrigins: origin,
	}
	a, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("app.New(development): %v", err)
	}
	t.Cleanup(a.Close)
	server := httptest.NewServer(a.Router)
	t.Cleanup(server.Close)

	// Seed through the dev pipeline's own tables, with a checksummed pool key
	// (the dev consumers store event addresses verbatim).
	st := store.New(pg)
	pool := "0xAbCdEf0000000000000000000000000000000001"
	token := "0xABCDEF0000000000000000000000000000000002"
	if _, err := st.InsertInitialPoolStats(ctx, store.InitialPoolStats{PoolAddress: pool, TokenAddress: token, Price: "1", MarketCap: "1", High24h: "1", Low24h: "1", CreatedAt: 1, UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	holders := []struct{ wallet, balance string }{
		{fmt.Sprintf("0x%040x", 0xa1), "900000000000000000000"}, // numerically largest
		{fmt.Sprintf("0x%040x", 0xa2), "80000000000000000000"},  // text-larger than a1
		{fmt.Sprintf("0x%040x", 0xa3), "7"},
		{"not-an-address", "5"}, // excluded: malformed wallet
	}
	for _, h := range holders {
		if _, err := pg.Exec(ctx, `INSERT INTO stats.pool_holders (pool_address, holder_address, balance, pct_of_supply, last_updated) VALUES ($1,$2,$3,'0',1)`, pool, h.wallet, h.balance); err != nil {
			t.Fatal(err)
		}
	}

	get := func(t *testing.T, path string) (*http.Response, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		req.Header.Set("Accept", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res, body
	}
	type page struct {
		Data struct {
			Items []struct {
				Wallet       string  `json:"wallet"`
				TokenAddress string  `json:"tokenAddress"`
				BalanceRaw   string  `json:"balanceRaw"`
				Share        *string `json:"share"`
			} `json:"items"`
			Coverage string `json:"coverage"`
		} `json:"data"`
		Quality    string  `json:"quality"`
		NextCursor *string `json:"nextCursor"`
	}

	var cursor string
	t.Run("seeded pool returns 200 contract-valid holders with CORS", func(t *testing.T) {
		res, body := get(t, "/v1/pools/"+strings.ToLower(pool)+"/holders?limit=2")
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", res.StatusCode, body)
		}
		if got := res.Header.Get("Access-Control-Allow-Origin"); got != origin {
			t.Fatalf("CORS allow-origin=%q", got)
		}
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("content-type=%q", ct)
		}
		if err := interfaces.ValidateJSON("HoldersResponse", body); err != nil {
			t.Fatalf("HoldersResponse contract: %v\n%s", err, body)
		}
		var p page
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Data.Items) != 2 || p.Data.Items[0].Wallet != holders[0].wallet || p.Data.Items[0].BalanceRaw != holders[0].balance || p.Data.Items[1].Wallet != holders[1].wallet {
			t.Fatalf("items not in numeric balance order: %s", body)
		}
		if p.Data.Items[0].TokenAddress != strings.ToLower(token) || p.Data.Items[0].Share != nil || p.Data.Coverage != "partial" || p.Quality != "partial" || p.NextCursor == nil {
			t.Fatalf("unexpected page shape: %s", body)
		}
		cursor = *p.NextCursor
	})

	t.Run("cursor continues to the final page", func(t *testing.T) {
		res, body := get(t, "/v1/pools/"+pool+"/holders?limit=2&cursor="+cursor)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d body=%s", res.StatusCode, body)
		}
		if err := interfaces.ValidateJSON("HoldersResponse", body); err != nil {
			t.Fatalf("HoldersResponse contract: %v\n%s", err, body)
		}
		var p page
		if err := json.Unmarshal(body, &p); err != nil {
			t.Fatal(err)
		}
		if len(p.Data.Items) != 1 || p.Data.Items[0].Wallet != holders[2].wallet || p.NextCursor != nil {
			t.Fatalf("final page: %s", body)
		}
	})

	errorCases := []struct {
		name, path, code string
		status           int
	}{
		{"unknown pool", fmt.Sprintf("/v1/pools/0x%040x/holders", 0xdead), "NOT_FOUND", http.StatusNotFound},
		{"malformed cursor", "/v1/pools/" + pool + "/holders?cursor=bogus", "CURSOR_INVALID", http.StatusBadRequest},
		{"unknown field", "/v1/pools/" + pool + "/holders?offset=1", "FIELD_UNKNOWN", http.StatusUnprocessableEntity},
		{"malformed pool", "/v1/pools/0x12/holders", "FIELD_INVALID", http.StatusUnprocessableEntity},
	}
	for _, tc := range errorCases {
		t.Run(tc.name+" returns the JSON error envelope", func(t *testing.T) {
			res, body := get(t, tc.path)
			if res.StatusCode != tc.status {
				t.Fatalf("status=%d body=%s", res.StatusCode, body)
			}
			if got := res.Header.Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("CORS allow-origin=%q", got)
			}
			if err := interfaces.ValidateJSON("Error", body); err != nil {
				t.Fatalf("Error contract: %v\n%s", err, body)
			}
			var envelope struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil || envelope.Code != tc.code {
				t.Fatalf("code=%q err=%v body=%s", envelope.Code, err, body)
			}
		})
	}
}
