package perps_test

import (
	"math/big"
	"strings"
	"testing"

	perpstypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func usd(whole int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(whole), big.NewInt(1_000_000_000_000_000_000))
}

// fixed parses a decimal string as an 18-decimal fixed-point value.
func fixed(decimal string) *big.Int {
	whole, fraction, _ := strings.Cut(decimal, ".")
	negative := strings.HasPrefix(whole, "-")
	digits := strings.TrimPrefix(whole, "-") + fraction + strings.Repeat("0", 18-len(fraction))
	value, ok := new(big.Int).SetString(digits, 10)
	if !ok {
		panic("fixed: " + decimal)
	}
	if negative {
		value.Neg(value)
	}
	return value
}

// usdWord is a whole number of USD (or a price) in 18-decimal fixed point.
func usdWord(whole int64) uint256.Int {
	return *new(uint256.Int).Mul(uint256.NewInt(uint64(whole)), uint256.NewInt(1_000_000_000_000_000_000))
}

func fixedWord(t *testing.T, decimal string) uint256.Int {
	t.Helper()
	value, ok := new(big.Rat).SetString(decimal)
	require.True(t, ok, decimal)
	value.Mul(value, new(big.Rat).SetInt(big.NewInt(1_000_000_000_000_000_000)))
	require.True(t, value.IsInt(), decimal)
	var word uint256.Int
	require.False(t, word.SetFromBig(value.Num()))
	return word
}

func limitOrder(isLong bool, sizeUsd, leverage, price int64) orderParams {
	p := newOrderParams(isLong, perpstypes.OrderTypeLimit, perpstypes.TimeInForceGTC, sizeUsd, leverage)
	p.LimitPrice = ordersBig(price)
	return p
}

func eventsLimitOrder(isLong bool, tif perpstypes.TimeInForce, price, sizeUsd, leverage *big.Int) eventsOrder {
	order := newOrder(isLong, perpstypes.OrderTypeLimit, tif, sizeUsd, leverage)
	order.LimitPrice = price
	return order
}

// wordBig is the big.Int of a uint256 word returned by value.
func wordBig(word uint256.Int) *big.Int { return word.ToBig() }
