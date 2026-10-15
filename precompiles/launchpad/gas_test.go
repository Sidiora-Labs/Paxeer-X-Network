package launchpad

import (
	"math/big"
	"testing"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestRequiredGasCoversEveryWrite(t *testing.T) {
	require.Len(t, roles, 11)
	require.Len(t, roleABIs, 11)
	check := func(t *testing.T, parsed abi.ABI) {
		t.Helper()
		require.NotEmpty(t, parsed.Methods)
		p := pcommon.NewPrecompile(parsed, PrecompileExecutor{}, common.Address{}, PrecompileName)
		for _, method := range parsed.Methods {
			t.Run(method.Sig, func(t *testing.T) {
				writes := Writes(method.Name)
				switch method.StateMutability {
				case "view", "pure":
					require.Zero(t, writes)
				case "nonpayable", "payable":
					require.Positive(t, writes, "missing write gas for %s", method.Sig)
				default:
					t.Fatalf("unknown mutability %q", method.StateMutability)
				}
				require.Equal(t, writes, Writes(method.RawName))
				require.Equal(t, uint64(3064)+5000*writes, p.RequiredGas(method.ID))
			})
		}
	}
	for _, role := range roles {
		t.Run(role, func(t *testing.T) {
			parsed, ok := roleABIs[role]
			require.True(t, ok)
			check(t, parsed)
		})
	}
	t.Run("direct", func(t *testing.T) {
		check(t, pcommon.MustGetABI(f, "abi.json"))
	})
	require.Zero(t, Writes("unknownMethod"))
}

func TestRequiredGasIsPinned(t *testing.T) {
	require.Equal(t, uint64(3000), BaseGas)
	require.Equal(t, uint64(16), GasPerByte)
	require.Equal(t, uint64(5000), GasPerWrite)
	require.Equal(t, uint64(3000), Gas(0, 0))
	require.Equal(t, uint64(45112), Gas(132, 8))

	pool := common.Address{19: 1}
	recipient := common.Address{19: 2}
	for _, tc := range []struct {
		role   string
		method string
		args   []interface{}
		bytes  int
		gas    uint64
	}{
		{"Router", "buy", []interface{}{pool, big.NewInt(1), big.NewInt(0), big.NewInt(1)}, 132, 45112},
		{"Router", "sell", []interface{}{pool, big.NewInt(1), big.NewInt(0), big.NewInt(1)}, 132, 35112},
		{"Router", "createMarket", []interface{}{"Token", "TKN", uint8(0), common.Address{}}, 260, 67160},
		{"Router", "swapTokenForToken", []interface{}{pool, recipient, big.NewInt(1), big.NewInt(0), big.NewInt(1)}, 164, 75624},
		{"SidioraPool", "pause", nil, 4, 8064},
		{"SidioraPool", "getPoolInfo", nil, 4, 3064},
		{"FeesRouter", "claimFees", []interface{}{big.NewInt(1)}, 36, 18576},
		{"SidioraNFT", "safeTransferFrom", []interface{}{pool, recipient, big.NewInt(1)}, 100, 49600},
		{"SidioraNFT", "safeTransferFrom0", []interface{}{pool, recipient, big.NewInt(1), []byte{1}}, 196, 51136},
		{"Treasury", "deposit", []interface{}{pool, big.NewInt(1)}, 68, 14088},
	} {
		t.Run(tc.role+"/"+tc.method, func(t *testing.T) {
			parsed := roleABIs[tc.role]
			input, err := parsed.Pack(tc.method, tc.args...)
			require.NoError(t, err)
			require.Len(t, input, tc.bytes)
			p := pcommon.NewPrecompile(parsed, PrecompileExecutor{}, common.Address{}, PrecompileName)
			require.Equal(t, tc.gas, p.RequiredGas(input))
		})
	}
	for _, tc := range []struct {
		method string
		writes uint64
	}{
		{"createMarketWithPermit", 12},
		{"buyWithPermit", 8},
		{"sellWithPermit", 6},
		{"swapTokenForTokenWithPermit", 14},
	} {
		t.Run(tc.method, func(t *testing.T) {
			require.Equal(t, tc.writes, Writes(tc.method))
		})
	}
}
