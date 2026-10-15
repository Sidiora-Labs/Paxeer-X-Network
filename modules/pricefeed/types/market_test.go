package types

import (
	"sort"
	"testing"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

var feedSymbols = []string{"BTC", "ETH", "SOL", "AVAX", "LINK", "HYPE", "XRP", "ASTER", "TRUMP", "BNB", "SID", "PAX", "TSLA", "NVDA", "GOOGL", "XAU", "NAS100", "SPX500"}

func TestMarketTable(t *testing.T) {
	if len(Markets) != 18 {
		t.Fatalf("got %d markets, want 18", len(Markets))
	}
	got := make([]string, 0, len(Markets))
	for i, m := range Markets {
		if m.ID != uint64(i) {
			t.Fatalf("market %d has id %d", i, m.ID)
		}
		if m.ChainflowSymbol != m.Symbol {
			t.Fatalf("market %d chainflow symbol %q, want %q", i, m.ChainflowSymbol, m.Symbol)
		}
		if !m.MinPrice.Equal(sdk.OneInt()) {
			t.Fatalf("market %d min price %s, want 1", i, m.MinPrice)
		}
		if m.MaxPrice.String() != "1000000000000000000000000000000" {
			t.Fatalf("market %d max price %s, want 10^30", i, m.MaxPrice)
		}
		if !m.Enabled {
			t.Fatalf("market %d disabled", i)
		}
		got = append(got, m.Symbol)
	}
	want := append([]string(nil), feedSymbols...)
	sort.Strings(got)
	sort.Strings(want)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("symbol set %v, want %v", got, want)
		}
	}
	for id, symbol := range map[uint64]string{0: "BTC", 7: "NAS100", 12: "SID", 17: "BNB"} {
		if Markets[id].Symbol != symbol {
			t.Fatalf("market %d is %q, want %q", id, Markets[id].Symbol, symbol)
		}
	}
}

func TestMarketChainflowForms(t *testing.T) {
	cases := []struct {
		id                         uint64
		request, response, pathSeg string
	}{
		{0, "BTC", "PERP_BTC_USDC", "btc"},
		{9, "SPX500", "PERP_SPX500_USDC", "spx500"},
	}
	for _, c := range cases {
		m := Markets[c.id]
		if m.ChainflowSymbol != c.request {
			t.Fatalf("market %d request symbol %q, want %q", c.id, m.ChainflowSymbol, c.request)
		}
		if got := ResponseSymbol(m); got != c.response {
			t.Fatalf("market %d response symbol %q, want %q", c.id, got, c.response)
		}
		if got := PathName(m); got != c.pathSeg {
			t.Fatalf("market %d path name %q, want %q", c.id, got, c.pathSeg)
		}
	}
}
