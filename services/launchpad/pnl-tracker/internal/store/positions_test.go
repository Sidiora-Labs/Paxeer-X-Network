package store_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

const (
	oneToken  = "1000000000000000000" // 1e18 token raw
	usdl1     = "1000000"             // 1 USDL (6-dec)
	usdl3     = "3000000"             // 3 USDL
	usdl5     = "5000000"             // 5 USDL
	usdl10    = "10000000"            // 10 USDL
	someUser  = "0xUSER"
	somePool  = "0xPool"
	someToken = "0xToken"
)

func TestFoldTradeAccumulatesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	// Buy 1 @ 1 USDL, buy 1 @ 3 USDL (avg 2), sell 1 @ 5 USDL -> realised 3 USDL.
	steps := []store.TradeInput{
		trade("0xb1-0", someUser, somePool, someToken, true, usdl1, oneToken, 10, 100),
		trade("0xb2-0", someUser, somePool, someToken, true, usdl3, oneToken, 11, 200),
		trade("0xs1-0", someUser, somePool, someToken, false, usdl5, oneToken, 12, 300),
	}
	for _, s := range steps {
		inserted, err := st.FoldTrade(ctx, s)
		if err != nil {
			t.Fatalf("fold %s: %v", s.ID, err)
		}
		if !inserted {
			t.Fatalf("fold %s: expected inserted=true", s.ID)
		}
	}

	pos, err := st.GetPosition(ctx, someUser, somePool)
	if err != nil || pos == nil {
		t.Fatalf("get position: pos=%v err=%v", pos, err)
	}
	if pos.UserAddress != "0xuser" || pos.PoolAddress != "0xpool" || pos.TokenAddress != "0xtoken" {
		t.Fatalf("position keys lowercased? %+v", pos)
	}
	if pos.TotalUsdlSpent != "4000000" || pos.AvgCostBasis != "2000000" {
		t.Errorf("spent=%s avgCost=%s, want 4000000 / 2000000", pos.TotalUsdlSpent, pos.AvgCostBasis)
	}
	if pos.RealizedPnlUsdl != "3000000" {
		t.Errorf("realizedPnlUsdl = %s, want 3000000", pos.RealizedPnlUsdl)
	}
	if pos.CurrentHoldings != oneToken {
		t.Errorf("currentHoldings = %s, want %s", pos.CurrentHoldings, oneToken)
	}
	if pos.TradeCount != 3 {
		t.Errorf("tradeCount = %d, want 3", pos.TradeCount)
	}
	if pos.FirstBuyTs == nil || *pos.FirstBuyTs != 100 || pos.LastTradeTs != 300 {
		t.Errorf("firstBuyTs=%v lastTradeTs=%d", pos.FirstBuyTs, pos.LastTradeTs)
	}

	// Redelivering the first buy must NOT double-count (inserted=false, state unchanged).
	inserted, err := st.FoldTrade(ctx, steps[0])
	if err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	if inserted {
		t.Fatal("redeliver: expected inserted=false (already folded)")
	}
	pos2, _ := st.GetPosition(ctx, someUser, somePool)
	if pos2.TotalUsdlSpent != "4000000" || pos2.TradeCount != 3 || pos2.RealizedPnlUsdl != "3000000" {
		t.Fatalf("redeliver mutated position: %+v", pos2)
	}
}

func TestFoldTradeRealisesLoss(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	// Buy 1 @ 10 USDL, sell it for 4 USDL -> realised -6 USDL (signed).
	if _, err := st.FoldTrade(ctx, trade("0xb-0", "0xa", "0xp", "0xt", true, usdl10, oneToken, 1, 10)); err != nil {
		t.Fatalf("buy: %v", err)
	}
	if _, err := st.FoldTrade(ctx, trade("0xs-0", "0xa", "0xp", "0xt", false, "4000000", oneToken, 2, 20)); err != nil {
		t.Fatalf("sell: %v", err)
	}
	pos, _ := st.GetPosition(ctx, "0xa", "0xp")
	if pos.RealizedPnlUsdl != "-6000000" {
		t.Fatalf("realizedPnlUsdl = %s, want -6000000", pos.RealizedPnlUsdl)
	}
	if pos.CurrentHoldings != "0" {
		t.Fatalf("currentHoldings = %s, want 0", pos.CurrentHoldings)
	}
}

func TestListPositionsAndTrades(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)

	// Two pools for one user, plus an unrelated user.
	mustFold(t, st, trade("0xa-0", "0xu", "0xp1", "0xt1", true, usdl1, oneToken, 1, 100))
	mustFold(t, st, trade("0xb-0", "0xu", "0xp2", "0xt2", true, usdl3, oneToken, 2, 200))
	mustFold(t, st, trade("0xc-0", "0xother", "0xp1", "0xt1", true, usdl1, oneToken, 3, 50))

	positions, err := st.ListPositions(ctx, "0xu")
	if err != nil {
		t.Fatalf("list positions: %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("positions = %d, want 2", len(positions))
	}
	// Ordered by last_trade_ts DESC -> pool2 (200) before pool1 (100).
	if positions[0].PoolAddress != "0xp2" || positions[1].PoolAddress != "0xp1" {
		t.Errorf("ordering = %s,%s", positions[0].PoolAddress, positions[1].PoolAddress)
	}

	t.Run("trade history filters by pool and paginates", func(t *testing.T) {
		all, err := st.ListTrades(ctx, "0xu", store.TradeFilter{Limit: 10})
		if err != nil {
			t.Fatalf("list trades: %v", err)
		}
		if len(all) != 2 {
			t.Fatalf("all trades = %d, want 2", len(all))
		}
		onlyP1, _ := st.ListTrades(ctx, "0xu", store.TradeFilter{Pool: "0xp1", Limit: 10})
		if len(onlyP1) != 1 || onlyP1[0].PoolAddress != "0xp1" {
			t.Fatalf("pool filter = %+v", onlyP1)
		}
		// Time window excludes the ts=100 trade.
		from := int64(150)
		windowed, _ := st.ListTrades(ctx, "0xu", store.TradeFilter{From: &from, Limit: 10})
		if len(windowed) != 1 || windowed[0].PoolAddress != "0xp2" {
			t.Fatalf("from filter = %+v", windowed)
		}
	})
}

// mustFold folds a trade and fails on error or a redelivery.
func mustFold(t *testing.T, st *store.Store, in store.TradeInput) {
	t.Helper()
	inserted, err := st.FoldTrade(context.Background(), in)
	if err != nil {
		t.Fatalf("fold %s: %v", in.ID, err)
	}
	if !inserted {
		t.Fatalf("fold %s: expected inserted=true", in.ID)
	}
}
