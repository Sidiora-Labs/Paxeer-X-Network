package abi_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
)

func e18(units int64) *big.Int { return new(big.Int).Mul(big.NewInt(units), big.NewInt(1e18)) }

func milli18(milli int64) *big.Int { return new(big.Int).Mul(big.NewInt(milli), big.NewInt(1e15)) }

func TestPackEveryEvent(t *testing.T) {
	parsed, err := perpsabi.ABI()
	require.NoError(t, err)

	userA := common.HexToAddress("0x00000000000000000000000000000000000000a1")
	userB := common.HexToAddress("0x00000000000000000000000000000000000000b2")
	usdl := common.HexToAddress("0x00000000000000000000000000000000000000c3")
	module := common.HexToAddress("0x0000000000000000000000000000000000001015")
	cloid := [16]byte{0xc1, 0x01, 0x1d, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e}
	n := big.NewInt
	entry, ok := new(big.Int).SetString("60651960784313725490196", 10)
	require.True(t, ok)

	records := []struct {
		name string
		args []interface{}
	}{
		{"VaultCreated", []interface{}{userA, module}},
		{"CollateralDeposited", []interface{}{userA, n(250000000), n(250000000)}},
		{"CollateralWithdrawn", []interface{}{userA, n(150000000), n(100000000)}},
		{"CollateralTransferred", []interface{}{userA, userB, n(60000000)}},
		{"OrderPlaced", []interface{}{n(1), userA, n(0), uint8(0), true, e18(59000), new(big.Int).Mul(n(599971), big.NewInt(1e16))}},
		{"OrderOptions", []interface{}{n(1), uint8(0), false, e18(59000), n(0), uint64(0), cloid}},
		{"OrderCancelled", []interface{}{n(1), userA}},
		{"OrderExpired", []interface{}{n(4), userA, n(0)}},
		{"OrderTriggered", []interface{}{n(3), n(0), e18(61050)}},
		{"OrderExecuted", []interface{}{n(5), n(7), e18(60050)}},
		{"Fill", []interface{}{n(0), n(1), n(5), userA, userB, true, e18(60000), e18(600), milli18(90), milli18(270)}},
		{"PositionOpened", []interface{}{n(7), userA, n(0), true, e18(990), e18(10), e18(60000), usdl, n(100000000)}},
		{"PositionModified", []interface{}{n(7), milli18(1517760), e18(99), n(99000000)}},
		{"PositionEntryPriceChanged", []interface{}{n(7), e18(60000), entry}},
		{"PositionClosed", []interface{}{n(7), userA, n(0), milli18(1006500), e18(61000), milli18(16500), true}},
		{"PositionTpSlSet", []interface{}{n(7), e18(66000), e18(57000)}},
		{"TradeSettled", []interface{}{n(7), userA, n(0), uint8(1), milli18(1006500), e18(61000), milli18(16500), n(452925000000000000), milli18(-50), n(15997075)}},
		{"FeeCollected", []interface{}{n(7), userB, n(0), uint8(0), milli18(270), n(270000), usdl}},
		{"MarkPriceUpdated", []interface{}{n(0), e18(60095), e18(60100), e18(60090)}},
		{"FundingRateUpdated", []interface{}{n(0), n(3472222222), n(300000000000000)}},
		{"FundingSettled", []interface{}{n(0), n(12500000000000), milli18(-1500), milli18(1500)}},
		{"PositionFundingApplied", []interface{}{n(7), userA, n(0), milli18(-750), e18(99), n(99000000)}},
		{"OpenInterestChanged", []interface{}{n(0), e18(600), e18(600), e18(600), true}},
		{"Liquidation", []interface{}{n(7), userA, n(0), e18(59000), n(0), common.Address{}}},
		{"BackstopLiquidation", []interface{}{userA, n(0), e18(60000), e18(59990), e18(18)}},
		{"InsuranceContribution", []interface{}{usdl, n(18000000), uint8(1)}},
		{"InsuranceDraw", []interface{}{n(0), n(400000000), n(4600000000)}},
		{"VaultDeficit", []interface{}{usdl, n(1000000000)}},
		{"ADLExecuted", []interface{}{n(7), e18(600)}},
		{"ADLMatched", []interface{}{n(0), n(8), userB, e18(600), e18(60000), n(42)}},
	}
	require.Len(t, records, 30)
	require.Len(t, parsed.Events, 30)

	seen := make(map[string]bool, len(records))
	for _, record := range records {
		t.Run(record.name, func(t *testing.T) {
			event, ok := parsed.Events[record.name]
			require.True(t, ok)
			require.False(t, seen[record.name])
			seen[record.name] = true

			topics, data, err := perpsabi.PackEvent(record.name, record.args...)
			require.NoError(t, err)

			var indexed abi.Arguments
			for _, input := range event.Inputs {
				if input.Indexed {
					indexed = append(indexed, input)
				}
			}
			require.Len(t, topics, 1+len(indexed))
			require.Equal(t, event.ID, topics[0])

			decoded := make(map[string]interface{}, len(event.Inputs))
			require.NoError(t, abi.ParseTopicsIntoMap(decoded, indexed, topics[1:]))
			require.NoError(t, event.Inputs.UnpackIntoMap(decoded, data))
			require.Len(t, decoded, len(event.Inputs))

			for i, input := range event.Inputs {
				got := decoded[input.Name]
				if want, isBig := record.args[i].(*big.Int); isBig {
					gotBig, ok := got.(*big.Int)
					require.True(t, ok, input.Name)
					require.Zero(t, want.Cmp(gotBig), input.Name)
					continue
				}
				require.Equal(t, record.args[i], got, input.Name)
			}
		})
	}

	topics, data, err := perpsabi.PackEvent("CollateralDeposited", userA, n(250000000), n(250000000))
	require.NoError(t, err)
	require.Equal(t, []common.Hash{
		parsed.Events["CollateralDeposited"].ID,
		common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a1"),
	}, topics)
	require.Equal(t, append(common.LeftPadBytes(n(250000000).Bytes(), 32), common.LeftPadBytes(n(250000000).Bytes(), 32)...), data)

	topics, data, err = perpsabi.PackEvent("OrderCancelled", n(1), userA)
	require.NoError(t, err)
	require.Equal(t, common.BigToHash(n(1)), topics[1])
	require.Empty(t, data)

	_, _, err = perpsabi.PackEvent("MarginLocked", userA)
	require.EqualError(t, err, "perps: unknown event MarginLocked")
	_, _, err = perpsabi.PackEvent("VaultCreated", userA)
	require.EqualError(t, err, "perps: event VaultCreated takes 2 arguments, got 1")
	_, _, err = perpsabi.PackEvent("VaultCreated", "not an address", module)
	require.EqualError(t, err, "perps: event VaultCreated argument user: want common.Address, got string")
	_, _, err = perpsabi.PackEvent("OrderCancelled", uint64(1), userA)
	require.ErrorContains(t, err, "perps: event OrderCancelled argument orderId: ")
	_, _, err = perpsabi.PackEvent("CollateralDeposited", userA, "250000000", n(250000000))
	require.ErrorContains(t, err, "perps: event CollateralDeposited: ")
}
