package common_test

import (
	"bytes"
	"math/big"
	"os"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxverify"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

// retireGatedExecutor applies the call site contract of RetiredAt: the gate
// runs first in Execute and a retired method returns RetiredRevert.
type retireGatedExecutor struct {
	upgrades *upgradekeeper.Keeper
	name     string
}

func (e *retireGatedExecutor) RequiredGas([]byte, *abi.Method) uint64 { return 0 }

func (e *retireGatedExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, error) {
	if common.RetiredAt(ctx, e.upgrades, common.MergeUpgrade, ctx.BlockHeight()) {
		return common.RetiredRevert(e.name), vm.ErrExecutionReverted
	}
	return []byte("ran"), nil
}

func TestRetiredAtMergePlan(t *testing.T) {
	const actHeight = int64(200)
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx, _ := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).CacheContext()
	upgrades := k.UpgradeKeeper()
	abiBz, err := os.ReadFile("erc20_abi.json")
	require.NoError(t, err)
	newAbi, err := abi.JSON(bytes.NewReader(abiBz))
	require.NoError(t, err)
	input, err := newAbi.Pack("decimals")
	require.NoError(t, err)
	verifyAddr := ethcommon.HexToAddress(layerxverify.LayerXVerifyAddress)
	precompile := common.NewPrecompile(newAbi, &retireGatedExecutor{upgrades: upgrades, name: layerxverify.PrecompileName}, verifyAddr, layerxverify.PrecompileName)
	run := func(height int64) ([]byte, error) {
		stateDB := state.NewDBImpl(ctx.WithBlockHeight(height), k, false)
		return precompile.Run(&vm.EVM{StateDB: stateDB}, ethcommon.Address{}, ethcommon.Address{}, input, big.NewInt(0), true, false, nil)
	}
	keysAt := func(height int64) map[ethcommon.Address]bool {
		set := k.CustomPrecompiles(ctx.WithBlockHeight(height))
		keys := make(map[ethcommon.Address]bool, len(set))
		for addr := range set {
			keys[addr] = true
		}
		return keys
	}

	// Without a bound reader and before the plan is applied nothing retires.
	require.False(t, common.RetiredAt(ctx, nil, common.MergeUpgrade, actHeight))
	require.False(t, common.RetiredAt(ctx, upgrades, common.MergeUpgrade, actHeight))
	before := keysAt(actHeight - 1)

	upgrades.SetDone(ctx.WithBlockHeight(actHeight), common.MergeUpgrade)

	// Below H_act the method runs and the precompile set is unchanged.
	require.False(t, common.RetiredAt(ctx, upgrades, common.MergeUpgrade, actHeight-1))
	res, err := run(actHeight - 1)
	require.NoError(t, err)
	require.Equal(t, []byte("ran"), res)
	require.Equal(t, before, keysAt(actHeight-1))

	// The read is free: the caller's gas meter is not charged.
	meter := sdk.NewGasMeter(1_000_000, 1, 1)
	require.True(t, common.RetiredAt(ctx.WithGasMeter(meter), upgrades, common.MergeUpgrade, actHeight))
	require.Equal(t, uint64(0), meter.GasConsumed())

	// From H_act the method reverts with LXP_MODULE_DISABLED naming the
	// precompile, and the address keeps its registration.
	for _, height := range []int64{actHeight, actHeight + 1} {
		require.True(t, common.RetiredAt(ctx, upgrades, common.MergeUpgrade, height))
		res, err = run(height)
		require.Equal(t, vm.ErrExecutionReverted, err)
		require.Equal(t, common.RetiredRevert(layerxverify.PrecompileName), res)
		require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, res[:4])
		stringType, typeErr := abi.NewType("string", "", nil)
		require.NoError(t, typeErr)
		decoded, unpackErr := abi.Arguments{{Type: stringType}}.Unpack(res[4:])
		require.NoError(t, unpackErr)
		require.Equal(t, []interface{}{"LXP_MODULE_DISABLED: module disabled; layerxVerify retired"}, decoded)
		require.Equal(t, before[verifyAddr], keysAt(height)[verifyAddr])
	}

	// Another plan name does not retire the precompile.
	require.False(t, common.RetiredAt(ctx, upgrades, "v7.0-reads", actHeight+1))
}
