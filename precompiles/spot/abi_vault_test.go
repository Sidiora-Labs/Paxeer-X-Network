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

func TestPECORVaultABIGolden(t *testing.T) {
	abiBz, err := os.ReadFile("abi/vault.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)

	goldenBz, err := os.ReadFile("testdata/vault.golden.json")
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
	require.Len(t, parsed.Methods, 42)
	require.Len(t, parsed.Events, 14)
	require.Len(t, parsed.Errors, 12)
	require.Len(t, served, 68)

	require.Equal(t, "0x47e7ef24", hexutil.Encode(parsed.Methods["deposit"].ID))
	require.Equal(t, "payable", parsed.Methods["depositNative"].StateMutability)
	require.Equal(t, "0xf3fef3a3", hexutil.Encode(parsed.Methods["withdraw"].ID))
	require.True(t, parsed.HasReceive())
	require.Equal(t, "payable", parsed.Receive.StateMutability)
	require.Equal(t, "0xdcbc1c05240f31ff3ad067ef1ee35ce4997762752e3a095284754544f4c709d7", parsed.Events["Deposit"].ID.Hex())
	require.Equal(t, "0xcfab6517", served["error WethOnly"].ID)
	require.Equal(t, "0x118cdaa7", served["error OwnableUnauthorizedAccount"].ID)
}
