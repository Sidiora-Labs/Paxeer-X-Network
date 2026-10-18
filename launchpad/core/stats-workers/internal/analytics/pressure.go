// Package analytics provides deterministic primitives; route and consumer wiring
// is performed at the serial integration gate.
package analytics

import (
	"errors"
	"math/big"
	"regexp"
)

var aggregateDecimal = regexp.MustCompile(`^(0|[1-9][0-9]*)$`)

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,77})$`)

func quantity(s string) (*big.Int, error) {
	if !decimal.MatchString(s) {
		return nil, errors.New("invalid raw quantity")
	}
	n, _ := new(big.Int).SetString(s, 10)
	if n.BitLen() > 256 {
		return nil, errors.New("quantity exceeds uint256")
	}
	return n, nil
}

// Pressure separates count shares from USDL6 raw notional. Shares truncate to
// integer basis points; direction compares the exact rational, before rounding.
type Pressure struct {
	BuyCount         int64    `json:"buy_count"`
	SellCount        int64    `json:"sell_count"`
	BuyCountShare    *int64   `json:"buy_count_share_bp"`
	BuyNotionalShare *int64   `json:"buy_notional_share_bp"`
	Direction        string   `json:"direction"`
	Flags            []string `json:"flags"`
	BuyUSDLRaw       string   `json:"buy_usdl_raw"`
	SellUSDLRaw      string   `json:"sell_usdl_raw"`
}

func share(b, s *big.Int) *int64 {
	total := new(big.Int).Add(b, s)
	if total.Sign() == 0 {
		return nil
	}
	n := new(big.Int).Quo(new(big.Int).Mul(b, big.NewInt(10000)), total).Int64()
	return &n
}
func CalculatePressure(buys, sells int64, buyRaw, sellRaw string, wash bool) (Pressure, error) {
	p := Pressure{BuyCount: buys, SellCount: sells, Direction: "unavailable", Flags: []string{}, BuyUSDLRaw: buyRaw, SellUSDLRaw: sellRaw}
	if buys < 0 || sells < 0 {
		return p, errors.New("negative counts")
	}
	b, e := aggregateQuantity(buyRaw, buys)
	if e != nil {
		return p, e
	}
	s, e := aggregateQuantity(sellRaw, sells)
	if e != nil {
		return p, e
	}
	bc, sc := big.NewInt(buys), big.NewInt(sells)
	total := new(big.Int).Add(bc, sc)
	p.BuyCountShare = share(bc, sc)
	p.BuyNotionalShare = share(b, s)
	if total.Sign() == 0 {
		p.Flags = append(p.Flags, "no_activity")
	} else {
		p.Direction = "neutral"
		lhs := new(big.Int).Mul(bc, big.NewInt(10000))
		if lhs.Cmp(new(big.Int).Mul(total, big.NewInt(5500))) >= 0 {
			p.Direction = "bullish"
		} else if lhs.Cmp(new(big.Int).Mul(total, big.NewInt(4500))) <= 0 {
			p.Direction = "bearish"
		}
	}
	if total.Cmp(big.NewInt(5)) < 0 {
		p.Flags = append(p.Flags, "low_activity")
	}
	if wash {
		p.Flags = append(p.Flags, "suspected_wash")
	}
	return p, nil
}

func aggregateQuantity(raw string, count int64) (*big.Int, error) {
	if len(raw) > 97 || !aggregateDecimal.MatchString(raw) || count < 0 {
		return nil, ErrInvalidInput
	}
	n, ok := new(big.Int).SetString(raw, 10)
	maximum := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	maximum.Mul(maximum, big.NewInt(count))
	if !ok || n.Cmp(maximum) > 0 {
		return nil, ErrInvalidInput
	}
	return n, nil
}
