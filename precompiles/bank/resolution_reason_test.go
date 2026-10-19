package bank_test

import (
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/bank"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/address"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

// resolutionDecodeReason decodes Error(string) revert data into its text.
func resolutionDecodeReason(t *testing.T, data []byte) string {
	require.GreaterOrEqual(t, len(data), 4)
	require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, data[:4])
	reason, err := abi.UnpackRevert(data)
	require.NoError(t, err)
	return reason
}

func TestResolutionBankReason(t *testing.T) {
	testApp := testkeeper.EVMTestApp
	k := &testApp.EvmKeeper
	ctx, _ := testApp.GetContextForDeliverTx(nil).CacheContext()
	const planHeight = int64(100)

	p, err := bank.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)

	call := func(height int64, caller common.Address, input []byte, value *big.Int, readOnly bool) ([]byte, error) {
		stateDB := state.NewDBImpl(ctx.WithBlockHeight(height), k, false)
		res, _, runErr := p.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, caller, caller, input, 1_000_000, value, nil, readOnly, false)
		return res, runErr
	}
	revert := func(height int64, caller common.Address, input []byte, value *big.Int, readOnly bool) []byte {
		res, runErr := call(height, caller, input, value, readOnly)
		require.Equal(t, vm.ErrExecutionReverted, runErr)
		return res
	}

	holder := common.HexToAddress("0x0000000000000000000000000000000000000abc")
	payableBalance, err := p.ABI.Pack(bank.BalanceMethod, holder, "uhpx")
	require.NoError(t, err)
	zeroAddrBalance, err := p.ABI.Pack(bank.BalanceMethod, common.Address{}, "uhpx")
	require.NoError(t, err)
	emptyDenomBalance, err := p.ABI.Pack(bank.BalanceMethod, holder, "")
	require.NoError(t, err)
	unboundSendNative, err := p.ABI.Pack(bank.SendNativeMethod, sdk.AccAddress(holder.Bytes()).String())
	require.NoError(t, err)

	// Below the merge plan a failed bank call keeps the empty revert data it
	// returned before the plan.
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), pcommon.MergeUpgrade)
	require.Nil(t, revert(planHeight-1, holder, payableBalance, big.NewInt(1), false))
	require.Nil(t, revert(planHeight-1, holder, zeroAddrBalance, nil, false))
	require.Nil(t, revert(planHeight-1, holder, emptyDenomBalance, nil, false))

	// From the plan height every framework refusal reverts Error(string) with
	// its registry code through RevertReason.
	for _, height := range []int64{planHeight, planHeight + 1} {
		require.Equal(t, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function", resolutionDecodeReason(t, revert(height, holder, payableBalance, big.NewInt(1), false)))
		require.Equal(t, "LXP_PRECOMPILE_INVALID_ADDR: invalid addr", resolutionDecodeReason(t, revert(height, holder, zeroAddrBalance, nil, false)))
		// A caller without a bound account cannot pay from it.
		require.Equal(t, "LXP_PRECOMPILE_INVALID_ADDR: invalid addr", resolutionDecodeReason(t, revert(height, holder, unboundSendNative, big.NewInt(1), false)))
		// A bank failure outside the registry keeps its text as the reason.
		require.Equal(t, "invalid denom", resolutionDecodeReason(t, revert(height, holder, emptyDenomBalance, nil, false)))
		require.Equal(t, "cannot call sendNative from staticcall", resolutionDecodeReason(t, revert(height, holder, unboundSendNative, big.NewInt(1), true)))
	}

	// A payment with a wei remainder into the bank precompile's account is
	// refused with the wei remainder code, never LXP_PRECISION.
	keepers := testApp.GetPrecompileKeepers()
	value := new(big.Int).Add(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil), big.NewInt(1))
	bankAcc := keepers.EVMK().GetPaxAddressOrDefault(ctx, common.HexToAddress(bank.BankAddress))
	coin, err := pcommon.HandlePaymentUhpx(ctx, bankAcc, sdk.AccAddress(holder.Bytes()), value, keepers.BankK(), keepers.EVMK(), nil, 0)
	require.Equal(t, sdk.Coin{}, coin)
	const weiText = "selected precompile function does not allow payment with non-zero wei remainder: received 1000000000000000001"
	require.EqualError(t, err, weiText)
	reason := resolutionDecodeReason(t, pcommon.LXPReason(err))
	require.Equal(t, "LXP_PRECOMPILE_WEI_REMAINDER: "+weiText, reason)
	require.NotContains(t, reason, "LXP_PRECISION")

	// A kernel account is the module address derived from its account id; its
	// balance reads through bank at that derived address once an EVM address
	// is bound to it.
	accountID := common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000c0ffe")
	kernelAcc := sdk.AccAddress(address.Module("layerx", accountID.Bytes()))
	require.Len(t, kernelAcc, 32)
	funds := sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(4242)))
	require.NoError(t, k.BankKeeper().MintCoins(ctx, types.ModuleName, funds))
	require.NoError(t, k.BankKeeper().SendCoinsFromModuleToAccount(ctx, types.ModuleName, kernelAcc, funds))
	kernelEVM := common.HexToAddress("0x00000000000000000000000000000000000c0ffe")
	readBalance := func() *big.Int {
		input, packErr := p.ABI.Pack(bank.BalanceMethod, kernelEVM, "uhpx")
		require.NoError(t, packErr)
		res, runErr := call(planHeight, kernelEVM, input, nil, false)
		require.NoError(t, runErr)
		out, unpackErr := p.ABI.Unpack(bank.BalanceMethod, res)
		require.NoError(t, unpackErr)
		require.Len(t, out, 1)
		amount, ok := out[0].(*big.Int)
		require.True(t, ok)
		return amount
	}
	// Unbound, the EVM address casts to its own 20-byte account, which holds nothing.
	require.Equal(t, "0", readBalance().String())
	k.SetAddressMapping(ctx, kernelAcc, kernelEVM)
	require.Equal(t, "4242", readBalance().String())
	require.Equal(t, sdk.NewInt(4242), k.BankKeeper().GetBalance(ctx, kernelAcc, "uhpx").Amount)
}
