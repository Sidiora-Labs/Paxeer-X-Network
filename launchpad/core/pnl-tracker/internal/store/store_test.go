package store_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// newStore spins up a migrated Postgres container and returns a real Store.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(internaltest.NewPostgres(t))
}

// trade builds a TradeInput for the fold tests (1-token legs unless noted).
func trade(id, user, pool, token string, isBuy bool, usdl, tok string, block, ts int64) store.TradeInput {
	return store.TradeInput{
		ID: id, UserAddress: user, PoolAddress: pool, TokenAddress: token,
		IsBuy: isBuy, UsdlAmount: usdl, TokenAmount: tok, Price: "0", Fee: "0",
		BlockNumber: block, BlockTimestamp: ts, TxHash: id,
	}
}

// ensurePortfolioSchemas creates the stats + metadata subsets the portfolio /
// card reads join against.
func ensurePortfolioSchemas(t *testing.T, st *store.Store) {
	t.Helper()
	internaltest.EnsureStatsSchema(t, st.Pool())
	internaltest.EnsureMetadataSchema(t, st.Pool())
}

// seedStats inserts a stats.pool_stats market row (addresses lowercased to match
// the fold's stored keys).
func seedStats(t *testing.T, st *store.Store, pool, token, price, mcap, change24h string) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, price_change_24h)
		VALUES ($1,$2,$3,$4,$5)`, pool, token, price, mcap, change24h); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
}

// seedMeta inserts a metadata.token_metadata row.
func seedMeta(t *testing.T, st *store.Store, token, pool, name, symbol string) {
	t.Helper()
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO metadata.token_metadata (token_address, pool_address, name, symbol, created_at)
		VALUES ($1,$2,$3,$4,1)`, token, pool, name, symbol); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}
}

func TestCrossSchemaReads(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	internaltest.EnsureIndexerSchema(t, st.Pool())
	internaltest.EnsureStatsSchema(t, st.Pool())
	internaltest.EnsureMetadataSchema(t, st.Pool())

	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO indexer.pools (pool_address, token_address, creator, created_at)
		VALUES ('0xpool','0xtoken','0xcreator',1)`); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, price_change_24h)
		VALUES ('0xpool','0xtoken','2500000','2000000','1234')`); err != nil {
		t.Fatalf("seed stats: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO metadata.token_metadata (token_address, pool_address, name, symbol, created_at)
		VALUES ('0xtoken','0xpool','Dogecoin','DOGE',1)`); err != nil {
		t.Fatalf("seed metadata: %v", err)
	}

	t.Run("GetPoolToken resolves via indexer.pools", func(t *testing.T) {
		token, err := st.GetPoolToken(ctx, "0xpool")
		if err != nil || token != "0xtoken" {
			t.Fatalf("token = %q err = %v", token, err)
		}
		missing, err := st.GetPoolToken(ctx, "0xunknown")
		if err != nil || missing != "" {
			t.Fatalf("unknown token = %q err = %v", missing, err)
		}
	})

	t.Run("GetMarket reads stats.pool_stats", func(t *testing.T) {
		m, err := st.GetMarket(ctx, "0xpool")
		if err != nil || m == nil {
			t.Fatalf("market = %v err = %v", m, err)
		}
		if m.PriceWad != "2500000" || m.MarketCapUsdl != "2000000" || m.PriceChange24hBps != "1234" {
			t.Fatalf("market = %+v", m)
		}
		none, err := st.GetMarket(ctx, "0xunknown")
		if err != nil || none != nil {
			t.Fatalf("unknown market = %v err = %v", none, err)
		}
	})

	t.Run("GetTokenMeta reads metadata.token_metadata", func(t *testing.T) {
		meta, err := st.GetTokenMeta(ctx, "0xtoken")
		if err != nil || meta == nil {
			t.Fatalf("meta = %v err = %v", meta, err)
		}
		if meta.Symbol != "DOGE" || meta.Name != "Dogecoin" {
			t.Fatalf("meta = %+v", meta)
		}
	})
}
