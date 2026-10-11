package common_test

import (
	"bytes"
	"errors"
	"math/big"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestValidateArgsLength(t *testing.T) {
	err := common.ValidateArgsLength(nil, 0)
	require.Nil(t, err)
	err = common.ValidateArgsLength([]interface{}{1, ""}, 2)
	require.Nil(t, err)
	err = common.ValidateArgsLength([]interface{}{""}, 2)
	require.NotNil(t, err)
}

func TestValidteNonPayable(t *testing.T) {
	err := common.ValidateNonPayable(nil)
	require.Nil(t, err)
	err = common.ValidateNonPayable(big.NewInt(0))
	require.Nil(t, err)
	err = common.ValidateNonPayable(big.NewInt(1))
	require.NotNil(t, err)
}

func TestHandlePrecompileError(t *testing.T) {
	_, evmAddr := testkeeper.MockAddressPair()
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	stateDB := state.NewDBImpl(ctx, k, false)
	evm := &vm.EVM{StateDB: stateDB}

	// assert no panic under various conditions
	common.HandlePrecompileError(nil, evm, "no_error")
	common.HandlePrecompileError(types.NewAssociationMissingErr(evmAddr.Hex()), evm, "association")
	common.HandlePrecompileError(errors.New("other error"), evm, "other")
}

type MockPrecompileExecutor struct {
	throw bool
}

func (e *MockPrecompileExecutor) RequiredGas([]byte, *abi.Method) uint64 {
	return 0
}

func (e *MockPrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	ctx.EventManager().EmitEvent(sdk.NewEvent("test"))
	if e.throw {
		return nil, errors.New("test")
	}
	return []byte("success"), nil
}

func TestPrecompileRun(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)
	precompile := common.NewPrecompile(newAbi, &MockPrecompileExecutor{throw: false}, ethcommon.Address{}, "test")
	stateDB := state.NewDBImpl(ctx, k, false)
	res, err := precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(0), false, false, nil)
	require.Equal(t, []byte("success"), res)
	require.Nil(t, err)
	require.NotEmpty(t, stateDB.Ctx().EventManager().Events())
	stateDB.WithCtx(ctx.WithEventManager(sdk.NewEventManager()))
	precompile = common.NewPrecompile(newAbi, &MockPrecompileExecutor{throw: true}, ethcommon.Address{}, "test")
	res, err = precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(0), false, false, nil)
	require.Nil(t, res)
	require.NotNil(t, err)
	// should not emit any event
	require.Empty(t, stateDB.Ctx().EventManager().Events())
}

type MockDynamicGasPrecompileExecutor struct {
	throw     bool
	evmKeeper utils.EVMKeeper
}

func (e *MockDynamicGasPrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	ctx.EventManager().EmitEvent(sdk.NewEvent("test"))
	if e.throw {
		return nil, 0, errors.New("test")
	}
	return []byte("success"), 0, nil
}

func (e *MockDynamicGasPrecompileExecutor) EVMKeeper() utils.EVMKeeper {
	return e.evmKeeper
}

func TestDynamicGasPrecompileRun(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil)
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)
	precompile := common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{throw: false, evmKeeper: k}, ethcommon.Address{}, "test")
	stateDB := state.NewDBImpl(ctx, k, false)
	res, _, err := precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 0, big.NewInt(0), nil, false, false)
	require.Equal(t, []byte("success"), res)
	require.Nil(t, err)
	require.NotEmpty(t, stateDB.Ctx().EventManager().Events())
	stateDB.WithCtx(ctx.WithEventManager(sdk.NewEventManager()))
	precompile = common.NewDynamicGasPrecompile(newAbi, &MockDynamicGasPrecompileExecutor{throw: true, evmKeeper: k}, ethcommon.Address{}, "test")
	res, _, err = precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, 0, big.NewInt(0), nil, false, false)
	require.Nil(t, res)
	require.NotNil(t, err)
	// should not emit any event
	require.Empty(t, stateDB.Ctx().EventManager().Events())
}

type revertReasonsDynamicGasExecutor struct {
	evmKeeper utils.EVMKeeper
}

func (e *revertReasonsDynamicGasExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, suppliedGas uint64, _ *tracing.Hooks) (ret []byte, remainingGas uint64, err error) {
	return []byte("partial"), 7, errors.New("insufficient funds")
}

func (e *revertReasonsDynamicGasExecutor) EVMKeeper() utils.EVMKeeper {
	return e.evmKeeper
}

func TestDynamicGasRevertReasons(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	const planHeight = int64(100)
	ctx, _ := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).CacheContext()
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.Nil(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.Nil(t, err)
	input, err := newAbi.Pack("decimals")
	require.Nil(t, err)

	// Error(string) of "insufficient funds": selector, offset 0x20, length 18,
	// then the text padded to 32 bytes.
	want := ethcommon.FromHex("0x08c379a0" +
		"0000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000012" +
		"696e73756666696369656e742066756e64730000000000000000000000000000")
	require.Equal(t, want, common.RevertReason(errors.New("insufficient funds")))
	// Error(string) of "input too short to extract method ID" (36 bytes).
	wantShort := ethcommon.FromHex("0x08c379a0" +
		"0000000000000000000000000000000000000000000000000000000000000020" +
		"0000000000000000000000000000000000000000000000000000000000000024" +
		"696e70757420746f6f2073686f727420746f2065787472616374206d6574686f" +
		"6420494400000000000000000000000000000000000000000000000000000000")

	run := func(t *testing.T, height int64, reasons bool, in []byte) []byte {
		precompile := common.NewDynamicGasPrecompile(newAbi, &revertReasonsDynamicGasExecutor{evmKeeper: k}, ethcommon.Address{}, "test")
		if reasons {
			precompile = precompile.WithRevertReasons()
		}
		stateDB := state.NewDBImpl(ctx.WithBlockHeight(height), k, false)
		res, _, err := precompile.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, in, 0, big.NewInt(0), nil, false, false)
		require.Equal(t, vm.ErrExecutionReverted, err)
		return res
	}

	// Before the plan is applied, the flag leaves today's bytes.
	require.Equal(t, []byte("partial"), run(t, planHeight, true, input))
	require.Nil(t, run(t, planHeight, true, []byte{0x01}))

	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), common.MergeUpgrade)

	// Below the plan height the flag leaves today's bytes.
	require.Equal(t, []byte("partial"), run(t, planHeight-1, true, input))
	require.Nil(t, run(t, planHeight-1, true, []byte{0x01}))
	// Without the flag the bytes equal today's at and above the plan height.
	require.Equal(t, []byte("partial"), run(t, planHeight, false, input))
	require.Nil(t, run(t, planHeight+1, false, []byte{0x01}))
	// With the flag at and above the plan height every refusal is Error(string).
	require.Equal(t, want, run(t, planHeight, true, input))
	require.Equal(t, want, run(t, planHeight+1, true, input))
	require.Equal(t, wantShort, run(t, planHeight, true, []byte{0x01}))
}
