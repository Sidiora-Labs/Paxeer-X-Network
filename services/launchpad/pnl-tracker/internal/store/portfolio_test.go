package store_test

import (
	"context"
	"testing"
)

func TestPortfolioEnrichesAndSumsNetWorth(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	ensurePortfolioSchemas(t, st)

	// User holds 1 token in p1 (priced 2.5 USDL) and 1 token in p2 (priced 1 USDL).
	mustFold(t, st, trade("0xa-0", "0xu", "0xp1", "0xt1", true, usdl1, oneToken, 1, 100))
	mustFold(t, st, trade("0xb-0", "0xu", "0xp2", "0xt2", true, usdl1, oneToken, 2, 200))

	seedStats(t, st, "0xp1", "0xt1", "2500000", "5000000", "100")
	seedStats(t, st, "0xp2", "0xt2", "1000000", "2000000", "-50")
	seedMeta(t, st, "0xt1", "0xp1", "Token One", "ONE")
	// p2 has no metadata row -> symbol/name default to "".

	positions, total, err := st.Portfolio(ctx, "0xu")
	if err != nil {
		t.Fatalf("portfolio: %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("positions = %d, want 2", len(positions))
	}
	// total = 2.5 USDL (p1) + 1 USDL (p2) = 3.5 USDL.
	if total != "3500000" {
		t.Errorf("totalValueUsdl = %s, want 3500000", total)
	}
	// Newest-first: p2 (ts 200) then p1 (ts 100).
	if positions[0].PoolAddress != "0xp2" || positions[1].PoolAddress != "0xp1" {
		t.Fatalf("ordering = %s,%s", positions[0].PoolAddress, positions[1].PoolAddress)
	}
	p1 := positions[1]
	if p1.PriceWad == nil || *p1.PriceWad != "2500000" {
		t.Errorf("p1 priceWad = %v, want 2500000", p1.PriceWad)
	}
	if p1.MarketCapUsdl == nil || *p1.MarketCapUsdl != "5000000" {
		t.Errorf("p1 marketCap = %v", p1.MarketCapUsdl)
	}
	if p1.TokenSymbol != "ONE" || p1.TokenName != "Token One" {
		t.Errorf("p1 meta = %s/%s", p1.TokenSymbol, p1.TokenName)
	}
	if p1.TokenLogo != nil {
		t.Errorf("tokenLogo should be null (served by media/metadata)")
	}
	// p2 has no metadata -> empty symbol/name, still present.
	p2 := positions[0]
	if p2.TokenSymbol != "" || p2.TokenName != "" {
		t.Errorf("p2 meta should be empty, got %s/%s", p2.TokenSymbol, p2.TokenName)
	}
}

func TestPortfolioEmptyUser(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	ensurePortfolioSchemas(t, st)
	positions, total, err := st.Portfolio(ctx, "0xnobody")
	if err != nil {
		t.Fatalf("portfolio: %v", err)
	}
	if len(positions) != 0 {
		t.Errorf("positions = %d, want 0", len(positions))
	}
	if total != "0" {
		t.Errorf("totalValueUsdl = %s, want 0", total)
	}
}
