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

func TestPECOROrdersABIGolden(t *testing.T) {
	abiBz, err := os.ReadFile("abi/orders.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)

	goldenBz, err := os.ReadFile("testdata/orders.golden.json")
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
	require.Len(t, parsed.Methods, 40)
	require.Len(t, parsed.Events, 8)
	require.Len(t, parsed.Errors, 33)
	require.Len(t, served, 81)

	require.Equal(t, "0x947de5b1", hexutil.Encode(parsed.Methods["placeLimitBuy"].ID))
	require.Equal(t, "nonpayable", parsed.Methods["placeLimitBuy"].StateMutability)
	require.Equal(t, "view", parsed.Methods["canExecuteLimitOrder"].StateMutability)
	require.False(t, parsed.HasReceive())
	require.False(t, parsed.HasFallback())

	created := parsed.Events["LimitOrderCreated"]
	require.Equal(t, "0x7df754202cad14218ee77bcea0466d971b8145466b889f56634e2e8e66d17404", created.ID.Hex())
	indexed := make([]bool, len(created.Inputs))
	for i, in := range created.Inputs {
		indexed[i] = in.Indexed
	}
	require.Equal(t, []bool{true, true, false, false, false}, indexed)

	require.Equal(t, "0xbb2875c3", served["error InsufficientOutput"].ID)
	require.Equal(t, "0x5274afe7", served["error SafeERC20FailedOperation"].ID)
	require.Equal(t, "0x28d7a641", served["error TooManyActiveOrders"].ID)
	require.Equal(t, "0xfbc03578", served["error CannotCancel"].ID)
}
