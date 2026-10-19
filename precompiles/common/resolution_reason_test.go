package common_test

import (
	"bytes"
	"errors"
	"math"
	"math/big"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func resolutionERC20ABI(t *testing.T) abi.ABI {
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)
	return parsed
}

// resolutionDecodeReason decodes Error(string) revert data into its text.
func resolutionDecodeReason(t *testing.T, data []byte) string {
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, data[:4])
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	values, err := abi.Arguments{{Type: stringType}}.Unpack(data[4:])
	require.NoError(t, err)
	require.Len(t, values, 1)
	text, ok := values[0].(string)
	require.True(t, ok)
	return text
}

// resolutionRun runs a precompile built without an executor, so every result
// comes from the framework refusal paths alone.
func resolutionRun(t *testing.T, precompile *common.Precompile, input []byte) []byte {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	stateDB := state.NewDBImpl(ctx, k, false)
	res, err := precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(0), false, false, nil)
	require.Equal(t, vm.ErrExecutionReverted, err)
	return res
}

func TestResolutionReasonPrefix(t *testing.T) {
	// A kernel-code refusal already carries its LXP_ code and is kept as it is.
	kernelErr := errors.New(common.LXPCodeName(-400) + ": insufficient balance")
	require.Equal(t, "LXP_INSUFFICIENT_BALANCE: insufficient balance", resolutionDecodeReason(t, common.LXPReason(kernelErr)))
	require.Equal(t, common.RevertReason(kernelErr), common.LXPReason(kernelErr))

	// A registered module error gains its module-prefixed registry code.
	moduleErr := sdkerrors.Wrapf(xwebtypes.ErrWrongFee, "paid %d, fee %d", 1, 2)
	require.Equal(t, "LXP_XWEB_WRONG_FEE: paid 1, fee 2: paid amount is not the current fee", resolutionDecodeReason(t, common.LXPReason(moduleErr)))

	// An error the registry does not list reverts as LXP_UNKNOWN with its text.
	require.Equal(t, "LXP_UNKNOWN: unknown; boom", resolutionDecodeReason(t, common.LXPReason(errors.New("boom"))))

	// Framework refusals keep their text and map to their registry codes.
	argErr := common.ValidateArgsLength([]interface{}{""}, 2)
	require.EqualError(t, argErr, "expected 2 arguments but got 1")
	require.Equal(t, "LXP_PRECOMPILE_ARG_COUNT: expected 2 arguments but got 1", resolutionDecodeReason(t, common.LXPReason(argErr)))
	payErr := common.ValidateNonPayable(big.NewInt(1))
	require.EqualError(t, payErr, "sending funds to a non-payable function")
	require.Equal(t, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function", resolutionDecodeReason(t, common.LXPReason(payErr)))
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	_, addrErr := common.GetPaxAddressFromArg(ctx, ethcommon.Address{}, &testkeeper.EVMTestApp.EvmKeeper)
	require.EqualError(t, addrErr, "invalid addr")
	require.Equal(t, "LXP_PRECOMPILE_INVALID_ADDR: invalid addr", resolutionDecodeReason(t, common.LXPReason(addrErr)))
	_, shortErr := common.ExtractMethodID([]byte{0x01})
	require.EqualError(t, shortErr, "input too short to extract method ID")

	// Through Run, a precompile opted in reverts every framework refusal with
	// an LXP_ reason; the previous opt-in keeps the bare text.
	erc20 := resolutionERC20ABI(t)
	lxp := common.NewPrecompile(erc20, nil, ethcommon.Address{}, "test").WithLXPReasons()
	require.Equal(t, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID", resolutionDecodeReason(t, resolutionRun(t, lxp, []byte{0x01})))
	require.Equal(t, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xdeadbeef", resolutionDecodeReason(t, resolutionRun(t, lxp, []byte{0xde, 0xad, 0xbe, 0xef})))
	balanceOf := erc20.Methods["balanceOf"]
	_, unpackErr := balanceOf.Inputs.Unpack(nil)
	require.Error(t, unpackErr)
	require.Equal(t, "LXP_UNKNOWN: unknown; "+unpackErr.Error(), resolutionDecodeReason(t, resolutionRun(t, lxp, balanceOf.ID)))
	plain := common.NewPrecompile(erc20, nil, ethcommon.Address{}, "test").WithRevertReasons()
	require.Equal(t, "input too short to extract method ID", resolutionDecodeReason(t, resolutionRun(t, plain, []byte{0x01})))

	// The dynamic-gas base opted in reverts with an LXP_ reason from the merge
	// plan height and keeps today's empty data below it.
	const planHeight = int64(100)
	cacheCtx, _ := ctx.CacheContext()
	k := &testkeeper.EVMTestApp.EvmKeeper
	dynamic, err := addr.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.NoError(t, err)
	dynamic = dynamic.WithLXPReasons()
	runDynamic := func(height int64) []byte {
		stateDB := state.NewDBImpl(cacheCtx.WithBlockHeight(height), k, false)
		res, _, runErr := dynamic.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, []byte{0x01}, 0, big.NewInt(0), nil, false, false)
		require.Equal(t, vm.ErrExecutionReverted, runErr)
		return res
	}
	require.Nil(t, runDynamic(planHeight))
	k.UpgradeKeeper().SetDone(cacheCtx.WithBlockHeight(planHeight), common.MergeUpgrade)
	require.Nil(t, runDynamic(planHeight-1))
	require.Equal(t, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID", resolutionDecodeReason(t, runDynamic(planHeight)))
	require.Equal(t, "LXP_PRECOMPILE_INPUT_TOO_SHORT: input too short to extract method ID", resolutionDecodeReason(t, runDynamic(planHeight+1)))
}

func TestResolutionWeiRemainder(t *testing.T) {
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	keepers := testkeeper.EVMTestApp.GetPrecompileKeepers()
	value := new(big.Int).Add(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), big.NewInt(1))
	payer := sdk.AccAddress(ethcommon.HexToAddress("0x0000000000000000000000000000000000000abc").Bytes())
	precompileAddr := sdk.AccAddress(ethcommon.HexToAddress("0x0000000000000000000000000000000000001019").Bytes())

	coin, err := common.HandlePaymentUhpx(ctx, precompileAddr, payer, value, keepers.BankK(), keepers.EVMK(), nil, 0)
	require.Equal(t, sdk.Coin{}, coin)
	const text = "selected precompile function does not allow payment with non-zero wei remainder: received 1000000000000000001"
	require.EqualError(t, err, text)

	// The bare reason path keeps today's bytes; the registry path names the code.
	require.Equal(t, text, resolutionDecodeReason(t, common.RevertReason(err)))
	reason := resolutionDecodeReason(t, common.LXPReason(err))
	require.Equal(t, "LXP_PRECOMPILE_WEI_REMAINDER: "+text, reason)
	require.NotContains(t, reason, "LXP_PRECISION")
}

func TestResolutionFixedGasTable(t *testing.T) {
	erc20 := resolutionERC20ABI(t)
	table := common.GasTable{
		"balanceOf": {},
		"transfer":  {Write: true, Surcharge: 50000},
	}
	// No executor: the charge can only come from the table.
	precompile := common.NewPrecompile(erc20, nil, ethcommon.Address{}, "test").WithGasTable(table)

	small, err := erc20.Pack("balanceOf", ethcommon.HexToAddress("0x0000000000000000000000000000000000000001"))
	require.NoError(t, err)
	large, err := erc20.Pack("balanceOf", ethcommon.HexToAddress("0xffffffffffffffffffffffffffffffffffffffff"))
	require.NoError(t, err)
	// Read row: 1000 + 3 x 32 argument bytes, the same for both calls.
	require.Equal(t, uint64(1096), precompile.RequiredGas(small))
	require.Equal(t, uint64(1096), precompile.RequiredGas(large))

	transfer, err := erc20.Pack("transfer", ethcommon.HexToAddress("0x0000000000000000000000000000000000000002"), big.NewInt(1))
	require.NoError(t, err)
	bigTransfer, err := erc20.Pack("transfer", ethcommon.HexToAddress("0x0000000000000000000000000000000000000003"), new(big.Int).Lsh(big.NewInt(1), 200))
	require.NoError(t, err)
	// Write row with surcharge: 2000 + 30 x 64 argument bytes + 50000.
	require.Equal(t, uint64(53920), precompile.RequiredGas(transfer))
	require.Equal(t, uint64(53920), precompile.RequiredGas(bigTransfer))

	require.Equal(t, uint64(math.MaxUint64), common.MethodGas{Write: true, Surcharge: math.MaxUint64}.Gas(make([]byte, 32)))

	// The dynamic-gas base reads its required gas from the same table.
	dynamic, err := addr.NewPrecompile(testkeeper.EVMTestApp.GetPrecompileKeepers())
	require.NoError(t, err)
	dynamic = dynamic.WithGasTable(common.GasTable{addr.GetPaxAddressMethod: {Surcharge: 700}})
	input, err := dynamic.GetABI().Pack(addr.GetPaxAddressMethod, ethcommon.HexToAddress("0x0000000000000000000000000000000000000004"))
	require.NoError(t, err)
	require.Equal(t, uint64(1000+3*32+700), dynamic.RequiredGas(input))
}

func TestResolutionUnknownSelector(t *testing.T) {
	erc20 := resolutionERC20ABI(t)
	precompile := common.NewPrecompile(erc20, nil, ethcommon.Address{}, "test").
		WithGasTable(common.GasTable{"balanceOf": {}}).
		WithLXPReasons()

	transfer, err := erc20.Pack("transfer", ethcommon.HexToAddress("0x0000000000000000000000000000000000000002"), big.NewInt(1))
	require.NoError(t, err)
	// A method without a row is charged the refusal only, never DefaultGasCost.
	require.Equal(t, common.UnknownMethodCallGas, precompile.RequiredGas(transfer))
	require.NotEqual(t, common.DefaultGasCost(transfer[4:], true), precompile.RequiredGas(transfer))
	require.Equal(t, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0xa9059cbb", resolutionDecodeReason(t, resolutionRun(t, precompile, transfer)))

	// A selector outside the ABI is refused the same way.
	unknown := []byte{0x12, 0x34, 0x56, 0x78}
	require.Equal(t, common.UnknownMethodCallGas, precompile.RequiredGas(unknown))
	require.Equal(t, "LXP_PRECOMPILE_UNKNOWN_METHOD: no method with id: 0x12345678", resolutionDecodeReason(t, resolutionRun(t, precompile, unknown)))

	// A listed method is charged its row.
	balanceOf, err := erc20.Pack("balanceOf", ethcommon.HexToAddress("0x0000000000000000000000000000000000000001"))
	require.NoError(t, err)
	require.Equal(t, uint64(1096), precompile.RequiredGas(balanceOf))
}
