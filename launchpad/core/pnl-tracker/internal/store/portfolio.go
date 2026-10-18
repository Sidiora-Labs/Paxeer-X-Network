package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlmath"
)

// PortfolioPosition is a position enriched with live market context and token
// metadata (pnl.ts PortfolioPosition). The embedded PositionRow promotes its
// camelCase JSON fields, so the wire shape is UserPosition + the extra fields.
type PortfolioPosition struct {
	PositionRow
	PriceWad      *string `json:"priceWad"`
	MarketCapUsdl *string `json:"marketCapUsdl"`
	TokenSymbol   string  `json:"tokenSymbol"`
	TokenName     string  `json:"tokenName"`
	TokenLogo     *string `json:"tokenLogo"`
}

// Portfolio returns a user's positions enriched with stats.pool_stats market
// data and metadata.token_metadata (both cross-schema, same DB — i2/L3), plus
// the total mark-to-market USDL value of all holdings (sum of priceWad*holdings
// / WAD, computed with math/big — i1). Positions are returned newest-first.
func (s *Store) Portfolio(ctx context.Context, user string) ([]PortfolioPosition, string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.user_address, p.pool_address, p.token_address,
		       p.total_usdl_spent, p.total_tokens_bought, p.total_usdl_received, p.total_tokens_sold,
		       p.avg_cost_basis, p.current_holdings, p.realized_pnl_usdl,
		       p.first_buy_ts, p.last_trade_ts, p.trade_count,
		       ps.price, ps.market_cap, m.symbol, m.name
		FROM pnl.user_positions p
		LEFT JOIN stats.pool_stats ps ON ps.pool_address = p.pool_address
		LEFT JOIN metadata.token_metadata m ON m.token_address = p.token_address
		WHERE p.user_address = $1
		ORDER BY p.last_trade_ts DESC, p.pool_address ASC`, strings.ToLower(user))
	if err != nil {
		return nil, "", fmt.Errorf("store: portfolio: %w", err)
	}
	defer rows.Close()

	out := []PortfolioPosition{}
	total := "0"
	for rows.Next() {
		var pp PortfolioPosition
		var symbol, name *string
		if err := rows.Scan(
			&pp.UserAddress, &pp.PoolAddress, &pp.TokenAddress,
			&pp.TotalUsdlSpent, &pp.TotalTokensBought, &pp.TotalUsdlReceived, &pp.TotalTokensSold,
			&pp.AvgCostBasis, &pp.CurrentHoldings, &pp.RealizedPnlUsdl,
			&pp.FirstBuyTs, &pp.LastTradeTs, &pp.TradeCount,
			&pp.PriceWad, &pp.MarketCapUsdl, &symbol, &name,
		); err != nil {
			return nil, "", fmt.Errorf("store: scan portfolio: %w", err)
		}
		if symbol != nil {
			pp.TokenSymbol = *symbol
		}
		if name != nil {
			pp.TokenName = *name
		}
		// tokenLogo is served by media/metadata; left null here.

		// Accumulate net worth from the live mark of each holding.
		if pp.PriceWad != nil {
			val, err := pnlmath.HoldingValue(*pp.PriceWad, pp.CurrentHoldings)
			if err != nil {
				return nil, "", err
			}
			total, err = db.BigintAdd(total, val)
			if err != nil {
				return nil, "", err
			}
		}
		out = append(out, pp)
	}
	return out, total, rows.Err()
}
