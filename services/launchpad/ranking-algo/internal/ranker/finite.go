package ranker

import (
	"fmt"
	"math"
	"math/big"
)

// finiteScore is the v2 scoring path. Complete decimals and rational ratios
// avoid overflow before bounded float conversion; legacy exported functions
// retain their frozen callers until the serial versioned adapter is wired.
func finiteScore(category string, p PoolStat) (float64, error) {
	if err := ValidatePool(p); err != nil {
		return 0, err
	}
	ratio, e := BoundedRatio(p.Volume1h, p.Volume24h, 1e12)
	if e != nil {
		return 0, e
	}
	velocity := math.Min(ratio*24, 1e12)
	change, e := Decimal(p.PriceChange24h, true)
	if e != nil {
		return 0, e
	}
	change.Abs(change)
	bound := new(big.Rat).SetInt64(1000000000000)
	if change.Cmp(bound) > 0 {
		change = bound
	}
	price, _ := change.Float64()
	trades := float64(p.BuyCount24h) + float64(p.SellCount24h)
	switch category {
	case "trending":
		return math.Min(velocity, 10)/10*.4 + math.Min(float64(p.UniqueTraders24h)/50, 1)*.25 + math.Min(price/100, 1)*.2 + math.Min(trades/240, 1)*.15, nil
	case "breakout":
		return price / 100 * math.Log2(1+velocity) * math.Log2(math.Max(float64(p.HolderCount), 1)), nil
	case "movers":
		cap, e := Decimal(p.MarketCap, false)
		if e != nil || cap.Sign() == 0 {
			return 0, fmt.Errorf("input-invalid: market-cap")
		}
		// Positive tiny market cap can underflow conversion, so require a named
		// minimum rather than dividing by an approximately zero logarithm.
		min := new(big.Rat).SetFrac64(1, 1000000000000)
		if cap.Cmp(min) < 0 {
			return 0, fmt.Errorf("input-invalid: minimum-market-cap")
		}
		if cap.Cmp(bound) > 0 {
			cap = bound
		}
		f, _ := cap.Float64()
		return velocity * math.Log2(1+float64(p.HolderCount)) / (math.Log1p(f) / math.Ln10), nil
	case "unusual":
		short, e := BoundedRatio(p.Volume5m, p.Volume1h, 1e12)
		if e != nil {
			return 0, e
		}
		imbalance := 0.0
		if trades > 0 {
			imbalance = math.Abs(float64(p.BuyCount24h)-float64(p.SellCount24h)) / trades
		}
		concentration := 0.0
		if p.UniqueTraders24h > 0 {
			concentration = trades / float64(p.UniqueTraders24h)
		}
		return math.Min(short*12, 20)/20*.35 + math.Min(velocity, 10)/10*.25 + imbalance*.2 + math.Min(concentration/10, 1)*.2, nil
	case "volume":
		return BoundedRatio(p.Volume24h, "1", 1e12)
	case "new":
		return 0, nil
	}
	return 0, fmt.Errorf("input-invalid: category")
}
