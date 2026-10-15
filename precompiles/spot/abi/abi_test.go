package abi_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"

	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
)

func TestABIEmbedLoads(t *testing.T) {
	cases := []struct {
		name   string
		load   func() (abi.ABI, error)
		events []string
	}{
		{"pecor", spotabi.PECOR, []string{"MarketOrderExecuted", "NativeSwap", "OwnershipTransferred", "Paused", "PriceImpactApplied", "SimpleSwap", "TieredFeeApplied", "TwapSanityUpdated", "Unpaused"}},
		{"orders", spotabi.Orders, []string{"LimitOrderCancelled", "LimitOrderCreated", "LimitOrderExecuted", "LimitOrderExpired", "LimitOrderModified", "OwnershipTransferred", "Paused", "Unpaused"}},
		{"stoporders", spotabi.StopOrders, []string{"OwnershipTransferred", "Paused", "StopLimitActivated", "StopLimitCancelled", "StopLimitExecuted", "StopLimitOrderCreated", "StopLossOrderCancelled", "StopLossOrderCreated", "StopLossOrderExecuted", "Unpaused"}},
		{"quoter", spotabi.Quoter, nil},
		{"vault", spotabi.Vault, []string{"Deposit", "LPWithdrawal", "NativeDeposit", "NativeWithdrawal", "OperatorUpdated", "OwnershipTransferred", "ReservesSync", "ReservesUpdated", "ShareTokenCreated", "TokenPauseUpdated", "TokenRegistered", "TransactionTrackerUpdated", "UtilizationCapUpdated", "Withdrawal"}},
		{"share", spotabi.Share, []string{"Approval", "OwnershipTransferred", "Transfer"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, err := c.load()
			require.NoError(t, err)
			require.Len(t, parsed.Events, len(c.events))
			for _, name := range c.events {
				require.Contains(t, parsed.Events, name)
			}
		})
	}
	orders, err := spotabi.Orders()
	require.NoError(t, err)
	require.Equal(t, []byte{0x53, 0x06, 0x10, 0x26}, orders.Methods["getLimitOrder"].ID)
	stops, err := spotabi.StopOrders()
	require.NoError(t, err)
	require.Equal(t, []byte{0x30, 0x7f, 0xf3, 0x24}, stops.Methods["getStopLimitOrder"].ID)
	require.Equal(t, []byte{0x4c, 0x34, 0xcf, 0x02}, stops.Methods["getStopLossOrder"].ID)
	vault, err := spotabi.Vault()
	require.NoError(t, err)
	require.Equal(t, []byte{0x14, 0xcb, 0xd9, 0x93}, vault.Methods["getRegisteredTokens"].ID)
	require.Equal(t, []byte{0x96, 0x15, 0x3f, 0x07}, vault.Methods["shareTokens"].ID)
}
