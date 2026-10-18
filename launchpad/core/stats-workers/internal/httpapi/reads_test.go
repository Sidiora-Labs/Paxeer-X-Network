package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// insertHolder writes a stats.pool_holders row directly (balance + pct as text).
func insertHolder(t *testing.T, ctx context.Context, st *store.Store, pool, holder, balance, pct string) {
	t.Helper()
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO stats.pool_holders (pool_address, holder_address, balance, pct_of_supply, last_updated)
		VALUES ($1,$2,$3,$4,1)`, pool, holder, balance, pct); err != nil {
		t.Fatalf("insert holder: %v", err)
	}
}

func insertTx(t *testing.T, ctx context.Context, st *store.Store, id, pool, sender string, isBuy bool, amountIn, amountOut string, ts int64) {
	t.Helper()
	if err := st.InsertTransaction(ctx, store.TransactionRow{
		ID: id, PoolAddress: pool, Sender: sender, IsBuy: isBuy,
		AmountIn: amountIn, AmountOut: amountOut, Price: "1000", Fee: "1",
		BlockTimestamp: ts, TxHash: id,
	}); err != nil {
		t.Fatalf("insert tx: %v", err)
	}
}

func newReadRouter(st *store.Store) http.Handler {
	r := chi.NewRouter()
	httpapi.RegisterCrossTokenSwaps(r, st)
	httpapi.RegisterPoolHolders(r, st)
	httpapi.RegisterPoolTransactions(r, st)
	httpapi.RegisterPoolAnalytics(r, st)
	return r
}

func TestHolderListRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	const pool = "0xhl"
	seedInitial(t, ctx, st, pool, nil)
	insertHolder(t, ctx, st, pool, "0xbig", "500000000000000", "5000")
	insertHolder(t, ctx, st, pool, "0xmid", "300000000000000", "3000")
	insertHolder(t, ctx, st, pool, "0xsmall", "100000000000000", "1000")

	type resp struct {
		Holders []struct {
			HolderAddress string `json:"holderAddress"`
			Rank          int    `json:"rank"`
		} `json:"holders"`
		Total  int `json:"total"`
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
	}

	t.Run("page 1 ranks from offset, total counts all", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/holders?limit=2&offset=0", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got resp
		decode(t, rec, &got)
		if got.Total != 3 || got.Limit != 2 || got.Offset != 0 || len(got.Holders) != 2 {
			t.Fatalf("page1 = %+v", got)
		}
		if got.Holders[0].HolderAddress != "0xbig" || got.Holders[0].Rank != 1 ||
			got.Holders[1].HolderAddress != "0xmid" || got.Holders[1].Rank != 2 {
			t.Fatalf("ordering/rank wrong: %+v", got.Holders)
		}
	})

	t.Run("page 2 continues the rank from the offset", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/holders?limit=2&offset=2", nil, nil)
		var got resp
		decode(t, rec, &got)
		if len(got.Holders) != 1 || got.Holders[0].HolderAddress != "0xsmall" || got.Holders[0].Rank != 3 {
			t.Fatalf("page2 = %+v", got.Holders)
		}
	})
}

func TestHolderDistributionRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	type walletEntry struct {
		Address string `json:"address"`
		Balance string `json:"balance"`
		PctBps  int64  `json:"pctBps"`
		Rank    int    `json:"rank"`
	}
	type resp struct {
		TotalHolders int `json:"totalHolders"`
		Brackets     []struct {
			Label string `json:"label"`
			Count int    `json:"count"`
		} `json:"brackets"`
		Top10Pct        string        `json:"top10Pct"`
		WalletMap       []walletEntry `json:"walletMap"`
		WalletMapTotal  int           `json:"walletMapTotal"`
		WalletMapLimit  int           `json:"walletMapLimit"`
		WalletMapOffset int           `json:"walletMapOffset"`
	}

	t.Run("brackets, concentration and paginated walletMap", func(t *testing.T) {
		const pool = "0xdist"
		seedInitial(t, ctx, st, pool, nil)
		insertHolder(t, ctx, st, pool, "0xa", "500000000000000", "5000") // 50% -> >10%
		insertHolder(t, ctx, st, pool, "0xb", "100000000000000", "1000") // 10% -> >10%
		insertHolder(t, ctx, st, pool, "0xc", "010000000000000", "100")  // 1% -> 1-5%

		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/holders/distribution?limit=2&offset=0", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got resp
		decode(t, rec, &got)
		if got.TotalHolders != 3 {
			t.Fatalf("totalHolders = %d, want 3", got.TotalHolders)
		}
		// S-6 pagination: limit=2 returns 2 of 3 wallet entries.
		if got.WalletMapTotal != 3 || got.WalletMapLimit != 2 || got.WalletMapOffset != 0 || len(got.WalletMap) != 2 {
			t.Fatalf("walletMap paging = total:%d limit:%d offset:%d len:%d",
				got.WalletMapTotal, got.WalletMapLimit, got.WalletMapOffset, len(got.WalletMap))
		}
		if got.WalletMap[0].Address != "0xa" || got.WalletMap[0].Rank != 1 || got.WalletMap[0].PctBps != 5000 {
			t.Fatalf("walletMap[0] = %+v", got.WalletMap[0])
		}
		// top10Pct = sum(top-10 balances)*10000/supply = (5000+1000+100) bps as concentration string.
		if got.Top10Pct != "6100" {
			t.Errorf("top10Pct = %s, want 6100", got.Top10Pct)
		}
		// Two holders at >=10% land in the ">10%" bracket.
		var over10 int
		for _, b := range got.Brackets {
			if b.Label == ">10%" {
				over10 = b.Count
			}
		}
		if over10 != 2 {
			t.Errorf(">10%% bracket count = %d, want 2", over10)
		}
	})

	t.Run("empty pool returns zeroed distribution", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/0xempty/holders/distribution", nil, nil)
		var got resp
		decode(t, rec, &got)
		if got.TotalHolders != 0 || len(got.WalletMap) != 0 {
			t.Fatalf("empty = %+v", got)
		}
	})
}

func TestWhalesRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	const pool = "0xwhales"
	seedInitial(t, ctx, st, pool, strptr("0xcreator"))
	insertHolder(t, ctx, st, pool, "0xcreator", "500000000000000", "5000") // whale + creator
	insertHolder(t, ctx, st, pool, "0xwhale2", "100000000000000", "1000")  // whale (>=1%)
	insertHolder(t, ctx, st, pool, "0xminnow", "000010000000000", "10")    // 0.1% -> filtered out

	type whale struct {
		Rank          int    `json:"rank"`
		HolderAddress string `json:"holderAddress"`
		PctOfSupply   int64  `json:"pctOfSupply"`
		IsCreator     bool   `json:"isCreator"`
	}
	type resp struct {
		WhaleThresholdPct float64 `json:"whaleThresholdPct"`
		WhaleCount        int     `json:"whaleCount"`
		Whales            []whale `json:"whales"`
	}

	rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/whales", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var got resp
	decode(t, rec, &got)
	if got.WhaleThresholdPct != 1 {
		t.Errorf("whaleThresholdPct = %v, want 1", got.WhaleThresholdPct)
	}
	if got.WhaleCount != 2 || len(got.Whales) != 2 {
		t.Fatalf("whaleCount = %d (whales %d), want 2 (minnow filtered)", got.WhaleCount, len(got.Whales))
	}
	if got.Whales[0].HolderAddress != "0xcreator" || !got.Whales[0].IsCreator || got.Whales[0].Rank != 1 {
		t.Fatalf("whale[0] = %+v, want creator flagged at rank 1", got.Whales[0])
	}
	if got.Whales[1].HolderAddress != "0xwhale2" || got.Whales[1].IsCreator {
		t.Fatalf("whale[1] = %+v, want non-creator", got.Whales[1])
	}
}

func TestCreatorActivityRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	type resp struct {
		CreatorAddress any    `json:"creatorAddress"`
		CurrentBalance string `json:"currentBalance"`
		Summary        struct {
			BuyCount          int    `json:"buyCount"`
			SellCount         int    `json:"sellCount"`
			HasSold           bool   `json:"hasSold"`
			TotalBoughtTokens string `json:"totalBoughtTokens"`
			TotalSoldTokens   string `json:"totalSoldTokens"`
			NetTokenBalance   string `json:"netTokenBalance"`
		} `json:"summary"`
		Message string `json:"message"`
	}

	t.Run("summarises buys/sells/net and current balance", func(t *testing.T) {
		const pool = "0xcreatoract"
		seedInitial(t, ctx, st, pool, strptr("0xcreator"))
		insertTx(t, ctx, st, "b-1", pool, "0xcreator", true, "0", "100", 200) // buy 100 out
		insertTx(t, ctx, st, "s-1", pool, "0xcreator", false, "40", "0", 100) // sell 40 in
		insertHolder(t, ctx, st, pool, "0xcreator", "60", "1")

		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/creator-activity", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var got resp
		decode(t, rec, &got)
		if got.CurrentBalance != "60" {
			t.Errorf("currentBalance = %s, want 60", got.CurrentBalance)
		}
		s := got.Summary
		if s.BuyCount != 1 || s.SellCount != 1 || !s.HasSold ||
			s.TotalBoughtTokens != "100" || s.TotalSoldTokens != "40" || s.NetTokenBalance != "60" {
			t.Fatalf("summary = %+v", s)
		}
	})

	t.Run("pool without a recorded creator returns a message", func(t *testing.T) {
		const pool = "0xnocreator"
		seedInitial(t, ctx, st, pool, nil)
		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/creator-activity", nil, nil)
		var got resp
		decode(t, rec, &got)
		if got.CreatorAddress != nil || got.Message == "" {
			t.Fatalf("expected nil creator + message, got %+v", got)
		}
	})

	t.Run("absent pool is 404", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/0xmissing/creator-activity", nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestRiskRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	type resp struct {
		RiskRating  int      `json:"riskRating"`
		RiskLevel   string   `json:"riskLevel"`
		RiskFactors []string `json:"riskFactors"`
		Details     struct {
			HasCreatorSold bool `json:"hasCreatorSold"`
			IsNew          bool `json:"isNew"`
		} `json:"details"`
	}

	t.Run("maps rating to level and surfaces factors", func(t *testing.T) {
		const pool = "0xrisk"
		if _, err := st.Pool().Exec(ctx, `
			INSERT INTO stats.pool_stats (pool_address, token_address, risk_rating, risk_factors, top10_concentration, creator_holdings_pct, created_at, updated_at)
			VALUES ($1,'0xtok',75,'["new","creator_sold"]','8500','6000',100,100)`, pool); err != nil {
			t.Fatalf("seed: %v", err)
		}
		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/risk", nil, nil)
		var got resp
		decode(t, rec, &got)
		if got.RiskRating != 75 || got.RiskLevel != "high" {
			t.Fatalf("rating=%d level=%s, want 75/high", got.RiskRating, got.RiskLevel)
		}
		if len(got.RiskFactors) != 2 || !got.Details.IsNew || !got.Details.HasCreatorSold {
			t.Fatalf("factors/details = %+v", got)
		}
	})

	t.Run("absent pool is 404", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/0xmissingrisk/risk", nil, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestTransactionsRoute(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	const pool = "0xtxroute"
	insertTx(t, ctx, st, "b-300", pool, "0xa", true, "1", "2", 300)
	insertTx(t, ctx, st, "s-200", pool, "0xb", false, "3", "4", 200)
	insertTx(t, ctx, st, "b-100", pool, "0xc", true, "5", "6", 100)

	type resp struct {
		Transactions []store.TransactionRow `json:"transactions"`
		Limit        int                    `json:"limit"`
		Offset       int                    `json:"offset"`
	}

	t.Run("default returns all, newest first", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/transactions", nil, nil)
		var got resp
		decode(t, rec, &got)
		if len(got.Transactions) != 3 || got.Transactions[0].ID != "b-300" || got.Limit != 50 {
			t.Fatalf("all = %+v", got)
		}
	})

	t.Run("type=buy filters to buys", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/"+pool+"/transactions?type=buy", nil, nil)
		var got resp
		decode(t, rec, &got)
		if len(got.Transactions) != 2 {
			t.Fatalf("buys = %d, want 2", len(got.Transactions))
		}
		for _, tx := range got.Transactions {
			if !tx.IsBuy {
				t.Fatalf("non-buy in buy filter: %+v", tx)
			}
		}
	})
}

func TestCrossTokenSwapRoutes(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	h := newReadRouter(st)

	mk := func(id, sender, tokenIn, tokenOut string, ts int64) store.CrossTokenSwapRow {
		return store.CrossTokenSwapRow{
			ID: id, Sender: sender, TokenIn: tokenIn, TokenOut: tokenOut,
			PoolIn: "0xpin", PoolOut: "0xpout", AmountIn: "10", IntermediateUsdl: "5",
			AmountOut: "20", FeeIn: "1", FeeOut: "1", BlockTimestamp: ts, TxHash: id,
		}
	}
	// Stored lower-cased; routes lower-case before querying.
	_ = st.InsertCrossTokenSwap(ctx, mk("w-2", "0xwallet", "0xa", "0xb", 200))
	_ = st.InsertCrossTokenSwap(ctx, mk("w-1", "0xwallet", "0xtarget", "0xc", 100))
	_ = st.InsertCrossTokenSwap(ctx, mk("o-1", "0xother", "0xtarget", "0xd", 150))

	type resp struct {
		Swaps []store.CrossTokenSwapRow `json:"swaps"`
	}

	t.Run("by wallet, newest first, mixed-case input lower-cased", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/cross-token-swaps/0xWALLET", nil, nil)
		var got resp
		decode(t, rec, &got)
		if len(got.Swaps) != 2 || got.Swaps[0].ID != "w-2" || got.Swaps[1].ID != "w-1" {
			t.Fatalf("by wallet = %+v", got.Swaps)
		}
	})

	t.Run("by token matches token_in or token_out across wallets", func(t *testing.T) {
		rec := serve(t, h, http.MethodGet, "/stats/cross-token-swaps/token/0xTARGET", nil, nil)
		var got resp
		decode(t, rec, &got)
		if len(got.Swaps) != 2 {
			t.Fatalf("by token = %d swaps, want 2", len(got.Swaps))
		}
	})
}
