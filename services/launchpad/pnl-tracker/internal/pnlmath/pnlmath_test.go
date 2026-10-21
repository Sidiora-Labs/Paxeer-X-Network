package pnlmath_test

import (
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlmath"
)

// foldAll applies every trade in order from the zero position, failing on error.
func foldAll(t *testing.T, trades ...pnlmath.Trade) pnlmath.Position {
	t.Helper()
	var p pnlmath.Position
	for i, tr := range trades {
		next, err := pnlmath.Fold(p, tr)
		if err != nil {
			t.Fatalf("fold trade %d: %v", i, err)
		}
		p = next
	}
	return p
}

func TestFoldBuysRecomputeAverageCost(t *testing.T) {
	// Buy 1 token @ 1 USDL, then 1 token @ 3 USDL -> avg cost 2 USDL/token (WAD).
	p := foldAll(t,
		pnlmath.Trade{IsBuy: true, UsdlAmount: "1000000", TokenAmount: "1000000000000000000", Ts: 100},
		pnlmath.Trade{IsBuy: true, UsdlAmount: "3000000", TokenAmount: "1000000000000000000", Ts: 200},
	)
	if p.TotalUsdlSpent != "4000000" {
		t.Errorf("totalUsdlSpent = %s, want 4000000", p.TotalUsdlSpent)
	}
	if p.TotalTokensBought != "2000000000000000000" {
		t.Errorf("totalTokensBought = %s, want 2e18", p.TotalTokensBought)
	}
	if p.CurrentHoldings != "2000000000000000000" {
		t.Errorf("currentHoldings = %s, want 2e18", p.CurrentHoldings)
	}
	if p.AvgCostBasis != "2000000" {
		t.Errorf("avgCostBasis = %s, want 2000000 (WAD)", p.AvgCostBasis)
	}
	if p.RealizedPnlUsdl != "0" {
		t.Errorf("realizedPnlUsdl = %s, want 0 (no sells)", p.RealizedPnlUsdl)
	}
	if p.FirstBuyTs == nil || *p.FirstBuyTs != 100 {
		t.Errorf("firstBuyTs = %v, want 100", p.FirstBuyTs)
	}
	if p.LastTradeTs != 200 {
		t.Errorf("lastTradeTs = %d, want 200", p.LastTradeTs)
	}
	if p.TradeCount != 2 {
		t.Errorf("tradeCount = %d, want 2", p.TradeCount)
	}
}

func TestFoldSellRealisesProfit(t *testing.T) {
	// Buy 1@1, buy 1@3 (avg 2), sell 1 token for 5 USDL -> realised 5-2 = 3 USDL.
	p := foldAll(t,
		pnlmath.Trade{IsBuy: true, UsdlAmount: "1000000", TokenAmount: "1000000000000000000", Ts: 1},
		pnlmath.Trade{IsBuy: true, UsdlAmount: "3000000", TokenAmount: "1000000000000000000", Ts: 2},
		pnlmath.Trade{IsBuy: false, UsdlAmount: "5000000", TokenAmount: "1000000000000000000", Ts: 3},
	)
	if p.RealizedPnlUsdl != "3000000" {
		t.Errorf("realizedPnlUsdl = %s, want 3000000", p.RealizedPnlUsdl)
	}
	if p.TotalUsdlReceived != "5000000" {
		t.Errorf("totalUsdlReceived = %s, want 5000000", p.TotalUsdlReceived)
	}
	if p.TotalTokensSold != "1000000000000000000" {
		t.Errorf("totalTokensSold = %s, want 1e18", p.TotalTokensSold)
	}
	if p.CurrentHoldings != "1000000000000000000" {
		t.Errorf("currentHoldings = %s, want 1e18", p.CurrentHoldings)
	}
	if p.AvgCostBasis != "2000000" {
		t.Errorf("avgCostBasis = %s, want 2000000 (unchanged by sell)", p.AvgCostBasis)
	}
}

func TestFoldSellRealisesLossAndSigned(t *testing.T) {
	// Buy 1 token @ 10 USDL, sell it for 4 USDL -> realised -6 USDL (signed).
	p := foldAll(t,
		pnlmath.Trade{IsBuy: true, UsdlAmount: "10000000", TokenAmount: "1000000000000000000", Ts: 1},
		pnlmath.Trade{IsBuy: false, UsdlAmount: "4000000", TokenAmount: "1000000000000000000", Ts: 2},
	)
	if p.RealizedPnlUsdl != "-6000000" {
		t.Errorf("realizedPnlUsdl = %s, want -6000000", p.RealizedPnlUsdl)
	}
	if p.CurrentHoldings != "0" {
		t.Errorf("currentHoldings = %s, want 0", p.CurrentHoldings)
	}
}

func TestFoldOversellRejected(t *testing.T) {
	_, err := pnlmath.Fold(pnlmath.Position{}, pnlmath.Trade{TokenAmount: "1", UsdlAmount: "100"})
	if err == nil {
		t.Fatal("oversell accepted")
	}
}
func TestRebuyRemainingBasis(t *testing.T) {
	p := foldAll(t, pnlmath.Trade{IsBuy: true, TokenAmount: "10", UsdlAmount: "100"}, pnlmath.Trade{TokenAmount: "10", UsdlAmount: "100"}, pnlmath.Trade{IsBuy: true, TokenAmount: "10", UsdlAmount: "200"})
	if p.AvgCostBasis != "20000000000000000000" || p.RealizedPnlUsdl != "0" {
		t.Fatalf("%+v", p)
	}
}

func TestFoldRejectsBadInput(t *testing.T) {
	if _, err := pnlmath.Fold(pnlmath.Position{}, pnlmath.Trade{IsBuy: true, UsdlAmount: "abc", TokenAmount: "1"}); err == nil {
		t.Error("expected error for non-numeric usdlAmount")
	}
	if _, err := pnlmath.Fold(pnlmath.Position{}, pnlmath.Trade{IsBuy: true, UsdlAmount: "-5", TokenAmount: "1"}); err == nil {
		t.Error("expected error for negative amount")
	}
	if _, err := pnlmath.Fold(pnlmath.Position{TotalUsdlSpent: "bad"}, pnlmath.Trade{IsBuy: true, UsdlAmount: "1", TokenAmount: "1"}); err == nil {
		t.Error("expected error for corrupt prior position")
	}
}

func TestHoldingValue(t *testing.T) {
	cases := []struct {
		name, price, holdings, want string
	}{
		{"one token at 2.5 USDL", "2500000", "1000000000000000000", "2500000"},
		{"zero price", "0", "1000000000000000000", "0"},
		{"zero holdings", "2500000", "0", "0"},

		{"half token", "2000000", "500000000000000000", "1000000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := pnlmath.HoldingValue(c.price, c.holdings)
			if err != nil {
				t.Fatalf("HoldingValue: %v", err)
			}
			if got != c.want {
				t.Errorf("HoldingValue(%s,%s) = %s, want %s", c.price, c.holdings, got, c.want)
			}
		})
	}
	if _, err := pnlmath.HoldingValue("x", "1"); err == nil {
		t.Error("expected error for bad price")
	}
}

func TestTotalPnl(t *testing.T) {
	// realised 3 USDL, holding 1 token, avg cost 2 USDL, live price 2.5 USDL:
	// total = 3 + (2.5 - 2) = 3.5 USDL.
	got, err := pnlmath.TotalPnl("3000000", "2000000", "1000000000000000000", "2500000")
	if err != nil {
		t.Fatalf("TotalPnl: %v", err)
	}
	if got != "3500000" {
		t.Errorf("TotalPnl = %s, want 3500000", got)
	}

	// With no live price, total is realised PnL only.
	got, err = pnlmath.TotalPnl("-6000000", "0", "0", "")
	if err != nil {
		t.Fatalf("TotalPnl: %v", err)
	}
	if got != "-6000000" {
		t.Errorf("TotalPnl (no mark) = %s, want -6000000", got)
	}

	// Underwater unrealised loss pushes a flat realised position negative:
	// realised 0, holding 1 token, avg cost 10, live price 4 -> 0 + (4-10) = -6.
	got, err = pnlmath.TotalPnl("0", "10000000", "1000000000000000000", "4000000")
	if err != nil {
		t.Fatalf("TotalPnl: %v", err)
	}
	if got != "-6000000" {
		t.Errorf("TotalPnl (underwater) = %s, want -6000000", got)
	}

	if _, err := pnlmath.TotalPnl("0", "0", "0", "bad"); err == nil {
		t.Error("expected error for bad price")
	}
}

func TestUnsignedMarksAndMissingPrice(t *testing.T) {
	for _, a := range [][2]string{{"-1", "1"}, {"1", "-1"}, {"", "1"}} {
		if _, e := pnlmath.HoldingValue(a[0], a[1]); e == nil {
			t.Fatal(a)
		}
	}
	for _, a := range [][3]string{{"-1", "1", "1"}, {"1", "-1", "1"}, {"1", "1", "-1"}, {"1", "1", ""}} {
		if _, e := pnlmath.TotalPnl("0", a[0], a[1], a[2]); e == nil {
			t.Fatal(a)
		}
	}
	v, e := pnlmath.TotalPnl("0", "1000000", "1000000000000000000", "0")
	if e != nil || v != "-1000000" {
		t.Fatal(v, e)
	}
}

func TestFoldRejectsCorruptUnsignedPriorAndBalanceOverflow(t *testing.T) {
	trade := pnlmath.Trade{IsBuy: true, UsdlAmount: "1", TokenAmount: "1"}
	for _, prior := range []pnlmath.Position{{AvgCostBasis: "-1"}, {TradeCount: -1}, {TotalUsdlSpent: "1", CurrentHoldings: "0"}, {CurrentHoldings: "115792089237316195423570985008687907853269984665640564039457584007913129639935"}} {
		if _, err := pnlmath.Fold(prior, trade); err == nil {
			t.Fatal("corrupt or overflowing inventory accepted", prior)
		}
	}
}
