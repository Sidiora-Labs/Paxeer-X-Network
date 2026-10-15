package types

import (
	"strings"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// Market is one listed perpetual market and its Chainflow feed symbol.
// Prices are integers at 18 decimals.
type Market struct {
	ID              uint64  `json:"id"`
	Symbol          string  `json:"symbol"`
	ChainflowSymbol string  `json:"chainflow_symbol"`
	MinPrice        sdk.Int `json:"min_price"`
	MaxPrice        sdk.Int `json:"max_price"`
	Enabled         bool    `json:"enabled"`
}

func newMarket(id uint64, symbol string) Market {
	return Market{
		ID:              id,
		Symbol:          symbol,
		ChainflowSymbol: symbol,
		MinPrice:        sdk.OneInt(),
		MaxPrice:        sdk.NewIntWithDecimal(1, 30),
		Enabled:         true,
	}
}

// Markets is the market table in id order.
var Markets = []Market{
	newMarket(0, "BTC"),
	newMarket(1, "ETH"),
	newMarket(2, "SOL"),
	newMarket(3, "AVAX"),
	newMarket(4, "LINK"),
	newMarket(5, "TSLA"),
	newMarket(6, "NVDA"),
	newMarket(7, "NAS100"),
	newMarket(8, "XAU"),
	newMarket(9, "SPX500"),
	newMarket(10, "GOOGL"),
	newMarket(11, "PAX"),
	newMarket(12, "SID"),
	newMarket(13, "HYPE"),
	newMarket(14, "XRP"),
	newMarket(15, "ASTER"),
	newMarket(16, "TRUMP"),
	newMarket(17, "BNB"),
}

// ResponseSymbol returns the symbol Chainflow answers with, PERP_<SYMBOL>_USDC.
func ResponseSymbol(m Market) string {
	return "PERP_" + m.ChainflowSymbol + "_USDC"
}

// PathName returns the lower-case symbol of the Chainflow feed path.
func PathName(m Market) string {
	return strings.ToLower(m.ChainflowSymbol)
}
