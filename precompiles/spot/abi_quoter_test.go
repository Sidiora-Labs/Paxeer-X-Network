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

func TestPECORQuoterABIGolden(t *testing.T) {
	abiBz, err := os.ReadFile("abi/quoter.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)

	goldenBz, err := os.ReadFile("testdata/quoter.golden.json")
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
	require.Len(t, parsed.Methods, 17)
	require.Empty(t, parsed.Events)
	require.Len(t, parsed.Errors, 3)

	require.Equal(t, "0x6f6ac6a2", hexutil.Encode(parsed.Methods["quoteMarketBuy"].ID))
	require.Equal(t, "0x3425eb94", hexutil.Encode(parsed.Methods["batchQuote"].ID))
	require.Equal(t, "nonpayable", parsed.Methods["setPecor"].StateMutability)
	require.Equal(t, "view", parsed.Methods["quoteMarketSell"].StateMutability)
	require.Len(t, parsed.Methods["quoteExactIn"].Outputs[0].Type.TupleElems, 23)
	require.Equal(t, "0xb307c2d2", served["error TwapWindowInvalid"].ID)
	require.Equal(t, "0x19abf40e", served["error StalePrice"].ID)
	require.Equal(t, "0x118cdaa7", served["error OwnableUnauthorizedAccount"].ID)
}
