package spot

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func TestPECORStopOrdersABIGolden(t *testing.T) {
	abiBz, err := os.ReadFile("abi/stoporders.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)

	goldenBz, err := os.ReadFile("testdata/stoporders.golden.json")
	require.NoError(t, err)
	var rows []pecorGoldenRow
	dec := json.NewDecoder(bytes.NewReader(goldenBz))
	dec.DisallowUnknownFields()
	require.NoError(t, dec.Decode(&rows))

	served := map[string]pecorGoldenRow{}
	for name, m := range parsed.Methods {
		served["function "+name] = pecorGoldenRow{Kind: "function", Name: name, Signature: m.Sig, ID: hexutil.Encode(m.ID)}
	}
	for name, e := range parsed.Events {
		served["event "+name] = pecorGoldenRow{Kind: "event", Name: name, Signature: e.Sig, ID: e.ID.Hex()}
	}
	for name, e := range parsed.Errors {
		served["error "+name] = pecorGoldenRow{Kind: "error", Name: name, Signature: e.Sig, ID: hexutil.Encode(e.ID[:4])}
	}

	golden := map[string]pecorGoldenRow{}
	for _, row := range rows {
		key := row.Kind + " " + row.Name
		_, dup := golden[key]
		require.False(t, dup, "duplicate golden row %s", key)
		golden[key] = row
	}

	for key, row := range golden {
		got, ok := served[key]
		require.True(t, ok, "golden row %s %s missing from the served ABI", key, row.ID)
		require.Equal(t, row, got, key)
	}
	for key, got := range served {
		_, ok := golden[key]
		require.True(t, ok, "served ABI entry %s %s missing from the golden fixture", key, got.ID)
	}
	require.Len(t, parsed.Methods, 49)
	require.Len(t, parsed.Events, 10)
	require.Len(t, parsed.Errors, 38)
	require.Len(t, served, 97)

	require.Equal(t, "0x695970ef", served["function placeStopLoss"].ID)
	require.Equal(t, "placeStopLoss(address,address,uint256,uint256,uint256)", served["function placeStopLoss"].Signature)
	require.Equal(t, "nonpayable", parsed.Methods["placeStopLoss"].StateMutability)
	require.Equal(t, "0x1a617148", served["function executeStopLossOrder"].ID)
	require.Equal(t, "0x6b8f73c1", served["function activeStopLossOrderIds"].ID)
	require.Equal(t, "view", parsed.Methods["activeStopLossOrderIds"].StateMutability)
	require.False(t, parsed.HasReceive())
	require.False(t, parsed.HasFallback())
	require.Equal(t, "0x029daf69e7abd7c123fcc3c78a61be884c0f62c4e0780faf5de3b66d02b5b8f8", served["event StopLossOrderExecuted"].ID)
	require.Equal(t, "0xb9007dd5be35138b4f9bfc3c734722460cae1a8b97da401020d3ac2762d7655b", served["event StopLossOrderCreated"].ID)
	require.True(t, parsed.Events["StopLossOrderCreated"].Inputs[1].Indexed)
	require.Equal(t, "0x5274afe7", served["error SafeERC20FailedOperation"].ID)
	require.Equal(t, "0x9b2bea5b", served["error OrderBelowMinimumNotional"].ID)
	require.Equal(t, "0x28d7a641", served["error TooManyActiveOrders"].ID)
	require.Equal(t, "0xbb2875c3", served["error InsufficientOutput"].ID)
}
