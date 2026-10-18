// Package pnlmath holds the pure, money-exact PnL cost-basis math for
// core/pnl-tracker. Everything here is computed with math/big on decimal strings
// — never float (invariant i1, HARD rule). Realized PnL is SIGNED (it can go
// negative); all other quantities are non-negative uint256 raw amounts:
//
//   - USDL raw  (6 decimals):  totalUsdlSpent/Received, realizedPnlUsdl, usdlAmount
//   - token raw (18 decimals): totalTokensBought/Sold, currentHoldings, tokenAmount
//   - WAD       (18 decimals): avgCostBasis, priceWad
//
// The legacy adapter uses remaining-inventory weighted basis. Rich transfer and
// unknown-basis accounting uses internal/production/l16; legacy rows require a
// canonical archive rebuild before they can claim verified performance.
package pnlmath

import (
	"fmt"
	"math/big"
	"regexp"
)

// wad is the 1e18 fixed-point scale shared by price and cost basis.
var wad = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

// Position is the accumulated state of a (user, pool) position. Money fields are
// decimal strings (RealizedPnlUsdl is signed; the rest are non-negative).
type Position struct {
	TotalUsdlSpent    string
	TotalTokensBought string
	TotalUsdlReceived string
	TotalTokensSold   string
	AvgCostBasis      string // WAD
	CurrentHoldings   string
	RealizedPnlUsdl   string // signed
	FirstBuyTs        *int64
	LastTradeTs       int64
	TradeCount        int
}

// Trade is one folded swap leg, normalised to USDL/token amounts (a buy spends
// USDL for tokens; a sell receives USDL for tokens).
type Trade struct {
	IsBuy       bool
	UsdlAmount  string
	TokenAmount string
	Ts          int64
}

// parse turns a decimal string into a *big.Int, treating "" as 0. A leading '-'
// is accepted (realized PnL is signed); a nil/empty position field is the zero
// value, so freshly-seen positions fold cleanly.
func parse(name, s string) (*big.Int, error) {
	if s == "" {
		return new(big.Int), nil
	}
	if len(s) > 157 || !regexp.MustCompile(`^(0|-?[1-9][0-9]*)$`).MatchString(s) {
		return nil, fmt.Errorf("pnlmath: noncanonical %s", name)
	}
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("pnlmath: %s %q is not a valid integer string", name, s)
	}
	return v, nil
}

// Fold returns the position after applying trade to prior. It is pure (no I/O)
// and idempotent at the caller's layer: the store only calls Fold when a trade
// row is newly inserted, so redelivery never double-counts.
func Fold(prior Position, t Trade) (Position, error) {
	spent, err := parse("totalUsdlSpent", prior.TotalUsdlSpent)
	if err != nil {
		return Position{}, err
	}
	bought, err := parse("totalTokensBought", prior.TotalTokensBought)
	if err != nil {
		return Position{}, err
	}
	received, err := parse("totalUsdlReceived", prior.TotalUsdlReceived)
	if err != nil {
		return Position{}, err
	}
	sold, err := parse("totalTokensSold", prior.TotalTokensSold)
	if err != nil {
		return Position{}, err
	}
	priorAverage, err := parse("avgCostBasis", prior.AvgCostBasis)
	if err != nil {
		return Position{}, err
	}
	holdings, err := parse("currentHoldings", prior.CurrentHoldings)
	if err != nil {
		return Position{}, err
	}
	realized, err := parse("realizedPnlUsdl", prior.RealizedPnlUsdl)
	if err != nil {
		return Position{}, err
	}

	usdl, err := parse("usdlAmount", t.UsdlAmount)
	if err != nil {
		return Position{}, err
	}
	tok, err := parse("tokenAmount", t.TokenAmount)
	if err != nil {
		return Position{}, err
	}
	if spent.Sign() < 0 || bought.Sign() < 0 || received.Sign() < 0 || sold.Sign() < 0 || holdings.Sign() < 0 || priorAverage.Sign() < 0 || holdings.BitLen() > 256 || prior.TradeCount < 0 {
		return Position{}, fmt.Errorf("pnlmath: negative unsigned prior")
	}
	if usdl.Sign() < 0 || tok.Sign() <= 0 || usdl.BitLen() > 256 || tok.BitLen() > 256 {
		return Position{}, fmt.Errorf("pnlmath: trade amounts must be non-negative")
	}

	// Remaining cost is spend minus previously consumed cost. Unlike lifetime
	// average this survives complete disposal and rebuy; v2 tracks it explicitly.
	remainingCost := new(big.Int).Sub(spent, new(big.Int).Sub(received, realized))
	if remainingCost.Sign() < 0 || holdings.Sign() == 0 && remainingCost.Sign() != 0 {
		return Position{}, fmt.Errorf("pnlmath: corrupt remaining basis; rebuild required")
	}
	avgCost := new(big.Int)
	firstBuy := prior.FirstBuyTs

	if t.IsBuy {
		spent.Add(spent, usdl)
		bought.Add(bought, tok)
		holdings.Add(holdings, tok)
		remainingCost.Add(remainingCost, usdl)
		if firstBuy == nil {
			ts := t.Ts
			firstBuy = &ts
		}
	} else {
		// costOfSold = avgCostBasis * tokenAmount / WAD (USDL raw).
		if tok.Cmp(holdings) > 0 {
			return Position{}, fmt.Errorf("pnlmath: oversell attribution anomaly; reconcile unknown inventory")
		}
		costOfSold := new(big.Int)
		if holdings.Sign() > 0 {
			costOfSold.Mul(remainingCost, tok)
			costOfSold.Quo(costOfSold, holdings)
		}
		remainingCost.Sub(remainingCost, costOfSold)
		// realizedPnl += proceeds - costOfSold (signed).
		gain := new(big.Int).Sub(usdl, costOfSold)
		realized.Add(realized, gain)

		received.Add(received, usdl)
		sold.Add(sold, tok)
		holdings.Sub(holdings, tok)

	}

	if holdings.BitLen() > 256 {
		return Position{}, fmt.Errorf("pnlmath: inventory quantity overflow")
	}
	if holdings.Sign() > 0 {
		avgCost.Mul(remainingCost, wad)
		avgCost.Quo(avgCost, holdings)
	}
	lastTrade := prior.LastTradeTs
	if t.Ts > lastTrade {
		lastTrade = t.Ts
	}

	return Position{
		TotalUsdlSpent:    spent.String(),
		TotalTokensBought: bought.String(),
		TotalUsdlReceived: received.String(),
		TotalTokensSold:   sold.String(),
		AvgCostBasis:      avgCost.String(),
		CurrentHoldings:   holdings.String(),
		RealizedPnlUsdl:   realized.String(),
		FirstBuyTs:        firstBuy,
		LastTradeTs:       lastTrade,
		TradeCount:        prior.TradeCount + 1,
	}, nil
}

// HoldingValue returns priceWad * holdings / WAD — the mark-to-market USDL value
// of a token balance. Empty inputs (or a zero price) yield "0".
func HoldingValue(priceWad, holdings string) (string, error) {
	price, err := parse("priceWad", priceWad)
	if err != nil {
		return "", err
	}
	h, err := parse("currentHoldings", holdings)
	if err != nil {
		return "", err
	}
	if price.Sign() < 0 || h.Sign() < 0 {
		return "", fmt.Errorf("pnlmath: negative unsigned mark/holdings")
	}
	if priceWad == "" {
		return "", fmt.Errorf("pnlmath: mark unavailable")
	}
	if price.Sign() == 0 || h.Sign() == 0 {
		return "0", nil
	}
	v := new(big.Int).Mul(price, h)
	return v.Quo(v, wad).String(), nil
}

// TotalPnl is a deprecated legacy display adapter. It reconstructs basis from
// a truncated WAD average and therefore can lose raw cost residuals (cost=1,
// holdings=3e18 gives avg=0). It must not support verified public performance.
// Serial readers must use v2 exact remaining-cost Inventory.Unrealized and history.
// TotalPnl returns the signed total PnL in USDL raw:
// realizedPnlUsdl + (unrealizedMark - remainingCost), where
// unrealizedMark = priceWad*holdings/WAD and remainingCost = avgCostBasis*holdings/WAD.
// Empty price is unavailable and returns an error for nonempty inventory; explicit zero is a verified zero mark.
func TotalPnl(realizedPnlUsdl, avgCostBasis, holdings, priceWad string) (string, error) {
	realized, err := parse("realizedPnlUsdl", realizedPnlUsdl)
	if err != nil {
		return "", err
	}
	avg, err := parse("avgCostBasis", avgCostBasis)
	if err != nil {
		return "", err
	}
	h, err := parse("currentHoldings", holdings)
	if err != nil {
		return "", err
	}
	price, err := parse("priceWad", priceWad)
	if err != nil {
		return "", err
	}

	if avg.Sign() < 0 || h.Sign() < 0 || price.Sign() < 0 {
		return "", fmt.Errorf("pnlmath: negative unsigned valuation")
	}
	if priceWad == "" {
		if h.Sign() == 0 {
			return realized.String(), nil
		}
		return "", fmt.Errorf("pnlmath: mark unavailable")
	}
	unrealized := new(big.Int)
	if price.Sign() != 0 && h.Sign() != 0 {
		unrealized.Mul(price, h)
		unrealized.Quo(unrealized, wad)
	}
	remainingCost := new(big.Int)
	if avg.Sign() != 0 && h.Sign() != 0 {
		remainingCost.Mul(avg, h)
		remainingCost.Quo(remainingCost, wad)
	}

	total := new(big.Int).Add(realized, new(big.Int).Sub(unrealized, remainingCost))
	return total.String(), nil
}
