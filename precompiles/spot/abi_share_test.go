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

func TestPECORVaultShareABIGolden(t *testing.T) {
	abiBz, err := os.ReadFile("abi/share.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)

	goldenBz, err := os.ReadFile("testdata/share.golden.json")
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
	require.Len(t, served, 25)

	require.Equal(t, "0xa9059cbb", hexutil.Encode(parsed.Methods["transfer"].ID))
	require.Equal(t, "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", parsed.Events["Transfer"].ID.Hex())
	require.Equal(t, "0xe450d38c", served["error ERC20InsufficientBalance"].ID)
	require.Equal(t, "view", parsed.Methods["balanceOf"].StateMutability)
	require.Equal(t, "nonpayable", parsed.Methods["mint"].StateMutability)
	require.True(t, parsed.Events["Transfer"].Inputs[0].Indexed)
	require.True(t, parsed.Events["Transfer"].Inputs[1].Indexed)
	require.False(t, parsed.Events["Transfer"].Inputs[2].Indexed)
	require.Len(t, parsed.Constructor.Inputs, 4)
	require.False(t, parsed.HasReceive())
	require.False(t, parsed.HasFallback())
}
