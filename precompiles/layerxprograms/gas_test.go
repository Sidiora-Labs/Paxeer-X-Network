package layerxprograms_test

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxprograms"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

type gasClass int

const (
	classRead gasClass = iota
	classWrite
)

type gasRow struct {
	selector  string
	class     gasClass
	surcharge uint64
}

// d2Table is the D2 gas table of the 0x101D contract with the selector of
// every method.
var d2Table = map[string]gasRow{
	"deploy":           {"bde3ff66", classWrite, 100000},
	"upgrade":          {"a860929f", classWrite, 100000},
	"call":             {"54bd2b48", classWrite, 50000},
	"registryRead":     {"fb28c4b1", classWrite, 0},
	"transfer":         {"59849fe7", classWrite, 0},
	"registerAccount":  {"fc786007", classWrite, 0},
	"setExitRoute":     {"23330dd5", classWrite, 0},
	"deprecate":        {"86ba1a99", classWrite, 0},
	"tombstone":        {"a512f1a9", classWrite, 0},
	"exitValue":        {"4dc50730", classWrite, 0},
	"exitValueBounded": {"ade5d9e6", classWrite, 0},
	"sandboxExecute":   {"ff04aa78", classWrite, 50000},
	"sandboxFund":      {"f11e5752", classWrite, 0},
	"sandboxActivate":  {"64e1674e", classWrite, 0},
	"retireStorage":    {"1a5fe5ff", classWrite, 2*addr.LayerXBindSignatureGas + 20000},
	"simulateCall":     {"3044bd56", classRead, 50000},

	"getProgram":                 {"b1ffb3d4", classRead, 0},
	"getInterface":               {"8efc7240", classRead, 0},
	"getProgramOwner":            {"5d6be786", classRead, 0},
	"getProgramAccount":          {"9677b10c", classRead, 0},
	"getProgramAccountById":      {"7a599bd0", classRead, 0},
	"getAccountProfile":          {"0f2d0496", classRead, 0},
	"getValueAccount":            {"24460e23", classRead, 0},
	"getStateHead":               {"0833650b", classRead, 0},
	"getWindDown":                {"98e4e41d", classRead, 0},
	"getExitRoute":               {"3ee6f18d", classRead, 0},
	"getSandbox":                 {"b8b4f8bc", classRead, 0},
	"getFeeSchedule":             {"8d8bfb15", classRead, 0},
	"getCurrentFeeSchedule":      {"bbe2e42d", classRead, 0},
	"quoteCallFee":               {"d67f9ffa", classRead, 0},
	"getPendingFeeSchedule":      {"e8b5cd95", classRead, 0},
	"getMeteringSchedule":        {"a842e85b", classRead, 0},
	"getCurrentMeteringSchedule": {"543c7a2f", classRead, 0},
	"getWebRequest":              {"61bc2ca4", classRead, 0},
	"getRetirementControl":       {"dcee2e41", classRead, 0},
}

func programsABI(t *testing.T) abi.ABI {
	bz, err := os.ReadFile("abi.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(bz))
	require.NoError(t, err)
	return parsed
}

// expectedGas derives the D2 charge from KVGasConfig: read 1000 + 3 x len,
// write 2000 + 30 x len, plus the method surcharge.
func expectedGas(row gasRow, length int) uint64 {
	kv := storetypes.KVGasConfig()
	if row.class == classWrite {
		return kv.WriteCostFlat + kv.WriteCostPerByte*uint64(length) + row.surcharge
	}
	return kv.ReadCostFlat + kv.ReadCostPerByte*uint64(length) + row.surcharge
}

func calldata(t *testing.T, selector string, length int) []byte {
	id, err := hex.DecodeString(selector)
	require.NoError(t, err)
	require.GreaterOrEqual(t, length, len(id))
	input := make([]byte, length)
	copy(input, id)
	return input
}

func TestRequiredGasSelectors(t *testing.T) {
	parsed := programsABI(t)
	require.Len(t, parsed.Methods, len(d2Table))
	for name, method := range parsed.Methods {
		row, ok := d2Table[name]
		require.True(t, ok, name)
		require.Equal(t, row.selector, hex.EncodeToString(method.ID), name)
	}
}

func TestRequiredGasTable(t *testing.T) {
	kv := storetypes.KVGasConfig()
	require.Equal(t, storetypes.Gas(1000), kv.ReadCostFlat)
	require.Equal(t, storetypes.Gas(3), kv.ReadCostPerByte)
	require.Equal(t, storetypes.Gas(2000), kv.WriteCostFlat)
	require.Equal(t, storetypes.Gas(30), kv.WriteCostPerByte)
	require.Equal(t, uint64(4000), addr.LayerXBindSignatureGas)

	for name, row := range d2Table {
		for _, length := range []int{4, 36, 400, 4100} {
			require.Equal(t, expectedGas(row, length), layerxprograms.RequiredGas(calldata(t, row.selector, length)), "%s with %d input bytes", name, length)
		}
	}

	// Acceptance values of the D2 table.
	require.Equal(t, uint64(1108), layerxprograms.RequiredGas(calldata(t, "b1ffb3d4", 36)))
	require.Equal(t, uint64(64000), layerxprograms.RequiredGas(calldata(t, "54bd2b48", 400)))
	require.Equal(t, uint64(2000+30*400+100000), layerxprograms.RequiredGas(calldata(t, "bde3ff66", 400)))
	require.Equal(t, uint64(2000+30*400+100000), layerxprograms.RequiredGas(calldata(t, "a860929f", 400)))
	require.Equal(t, uint64(64000), layerxprograms.RequiredGas(calldata(t, "ff04aa78", 400)))
	require.Equal(t, uint64(1000+3*400+50000), layerxprograms.RequiredGas(calldata(t, "3044bd56", 400)))
	require.Equal(t, uint64(2000+30*400+2*4000+20000), layerxprograms.RequiredGas(calldata(t, "1a5fe5ff", 400)))
	require.Equal(t, uint64(2000+30*400), layerxprograms.RequiredGas(calldata(t, "59849fe7", 400)))
	require.Equal(t, uint64(2000+30*4), layerxprograms.RequiredGas(calldata(t, "64e1674e", 4)))

	// A real ABI encoding is charged over its whole calldata.
	parsed := programsABI(t)
	getProgram, err := parsed.Pack("getProgram", [32]byte{1})
	require.NoError(t, err)
	require.Len(t, getProgram, 36)
	require.Equal(t, uint64(1108), layerxprograms.RequiredGas(getProgram))
}

func TestRequiredGasUnknownSelector(t *testing.T) {
	// Input without a known selector is charged the read cost of the input.
	unknown := calldata(t, "deadbeef", 36)
	require.Equal(t, uint64(1000+3*36), layerxprograms.RequiredGas(unknown))
	require.Equal(t, uint64(1000+3*4), layerxprograms.RequiredGas(unknown[:4]))
	require.Equal(t, uint64(1000+3*3), layerxprograms.RequiredGas([]byte{0xb1, 0xff, 0xb3}))
	require.Equal(t, uint64(1000), layerxprograms.RequiredGas(nil))

	// The 0x101D interface refuses the selector with the registry reason.
	precompile := pcommon.NewPrecompile(programsABI(t), nil, ethcommon.HexToAddress("0x000000000000000000000000000000000000101D"), "layerxprograms").WithLXPReasons()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	stateDB := state.NewDBImpl(ctx, &testkeeper.EVMTestApp.EvmKeeper, false)
	res, err := precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, unknown, big.NewInt(0), false, false, nil)
	require.Equal(t, vm.ErrExecutionReverted, err)
	require.GreaterOrEqual(t, len(res), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, res[:4])
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	values, err := abi.Arguments{{Type: stringType}}.Unpack(res[4:])
	require.NoError(t, err)
	require.Equal(t, []interface{}{"LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xdeadbeef"}, values)
}
