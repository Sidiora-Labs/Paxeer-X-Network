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

type pecorGoldenRow struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Signature string `json:"signature"`
	ID        string `json:"id"`
}

func TestPECORABIGolden(t *testing.T) {
	abiBz, err := os.ReadFile("abi/pecor.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)

	goldenBz, err := os.ReadFile("testdata/pecor.golden.json")
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
	require.Len(t, served, 78)

	require.Equal(t, "0x77a61952", hexutil.Encode(parsed.Methods["marketBuy"].ID))
	require.Equal(t, "nonpayable", parsed.Methods["multicall"].StateMutability)
	require.True(t, parsed.HasReceive())
	require.Equal(t, "payable", parsed.Receive.StateMutability)
	require.Equal(t, "0x750ebb7d", served["error MulticallFailed"].ID)
	require.Equal(t, "0xbb2875c3", served["error InsufficientOutput"].ID)
}
