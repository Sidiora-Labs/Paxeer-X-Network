package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlcache"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

const oneToken = "1000000000000000000"

func TestPositionsAndPositionRoutes(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterReads(r, st, rdb)

	foldBuy(t, ctx, st, "0xa-0", "0xu", "0xp1", "0xt1", "1000000", oneToken, 1, 100)

	t.Run("positions list caches in Redis (PG+Redis)", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xU/positions", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var resp struct {
			User      string              `json:"user"`
			Positions []store.PositionRow `json:"positions"`
		}
		decode(t, rec, &resp)
		if resp.User != "0xu" || len(resp.Positions) != 1 || resp.Positions[0].PoolAddress != "0xp1" {
			t.Fatalf("resp = %+v", resp)
		}
		if n, _ := rdb.Exists(ctx, pnlcache.KeyPositions("0xu")).Result(); n != 1 {
			t.Fatal("positions cache not populated")
		}
	})

	t.Run("positions cache hit served verbatim", func(t *testing.T) {
		if err := rdb.Set(ctx, pnlcache.KeyPositions("0xcached"), `{"user":"0xcached","positions":[{"poolAddress":"0xsentinel"}]}`, 0).Err(); err != nil {
			t.Fatalf("seed cache: %v", err)
		}
		rec := serve(t, r, http.MethodGet, "/users/0xcached/positions", nil, nil)
		var resp struct {
			Positions []store.PositionRow `json:"positions"`
		}
		decode(t, rec, &resp)
		if len(resp.Positions) != 1 || resp.Positions[0].PoolAddress != "0xsentinel" {
			t.Fatalf("expected cached sentinel, got %+v", resp.Positions)
		}
	})

	t.Run("single position found / 404", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xu/positions/0xp1", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var pos store.PositionRow
		decode(t, rec, &pos)
		if pos.PoolAddress != "0xp1" || pos.TotalUsdlSpent != "1000000" {
			t.Fatalf("pos = %+v", pos)
		}
		miss := serve(t, r, http.MethodGet, "/users/0xu/positions/0xmissing", nil, nil)
		if miss.Code != http.StatusNotFound {
			t.Fatalf("missing status = %d, want 404", miss.Code)
		}
	})
}

func TestTradesRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	r := chi.NewRouter()
	httpapi.RegisterReads(r, st, rdb)

	foldBuy(t, ctx, st, "0xa-0", "0xu", "0xp1", "0xt1", "1000000", oneToken, 1, 100)
	foldBuy(t, ctx, st, "0xb-0", "0xu", "0xp2", "0xt2", "3000000", oneToken, 2, 200)

	t.Run("all trades", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xu/trades", nil, nil)
		var resp struct {
			Trades []store.TradeRow `json:"trades"`
			Limit  int              `json:"limit"`
		}
		decode(t, rec, &resp)
		if len(resp.Trades) != 2 || resp.Limit != 50 {
			t.Fatalf("resp = %+v", resp)
		}
	})

	t.Run("pool filter + pagination", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xu/trades?pool=0xp1&limit=10", nil, nil)
		var resp struct {
			Trades []store.TradeRow `json:"trades"`
		}
		decode(t, rec, &resp)
		if len(resp.Trades) != 1 || resp.Trades[0].PoolAddress != "0xp1" {
			t.Fatalf("filtered = %+v", resp.Trades)
		}
	})

	t.Run("time window filter", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xu/trades?from=150", nil, nil)
		var resp struct {
			Trades []store.TradeRow `json:"trades"`
		}
		decode(t, rec, &resp)
		if len(resp.Trades) != 1 || resp.Trades[0].PoolAddress != "0xp2" {
			t.Fatalf("windowed = %+v", resp.Trades)
		}
	})

	t.Run("empty user returns empty array not null", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xempty/trades", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var resp map[string]any
		decode(t, rec, &resp)
		arr, ok := resp["trades"].([]any)
		if !ok || len(arr) != 0 {
			t.Fatalf("trades = %v, want []", resp["trades"])
		}
	})
}

func TestPortfolioRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	internaltest.EnsureStatsSchema(t, st.Pool())
	internaltest.EnsureMetadataSchema(t, st.Pool())
	r := chi.NewRouter()
	httpapi.RegisterReads(r, st, rdb)

	foldBuy(t, ctx, st, "0xa-0", "0xu", "0xp1", "0xt1", "1000000", oneToken, 1, 100)
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, price_change_24h)
		VALUES ('0xp1','0xt1','2500000','5000000','100')`); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO metadata.token_metadata (token_address, pool_address, name, symbol, created_at)
		VALUES ('0xt1','0xp1','Token One','ONE',1)`); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	t.Run("net worth + enrichment, then cache populated (PG+Redis)", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/users/0xu/portfolio", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var resp struct {
			User           string                    `json:"user"`
			TotalValueUsdl string                    `json:"totalValueUsdl"`
			Positions      []store.PortfolioPosition `json:"positions"`
		}
		decode(t, rec, &resp)
		if resp.TotalValueUsdl != "2500000" {
			t.Errorf("totalValueUsdl = %s, want 2500000", resp.TotalValueUsdl)
		}
		if len(resp.Positions) != 1 || resp.Positions[0].TokenSymbol != "ONE" {
			t.Fatalf("positions = %+v", resp.Positions)
		}
		if n, _ := rdb.Exists(ctx, pnlcache.KeyPortfolio("0xu")).Result(); n != 1 {
			t.Fatal("portfolio cache not populated")
		}
	})
}
