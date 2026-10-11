package teeattestor_test

import (
	"bytes"
	"math/big"
	"testing"
	"time"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/teeattestor"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

func TestResolutionThreshold3of5(t *testing.T) {
	h := newHarness(t, 5)
	set := h.keeper.GetAttestorSet(h.stateDB.Ctx())
	require.Len(t, set.Attestors, 5)
	require.Equal(t, uint32(3), set.Threshold)
	for i, key := range h.keys {
		require.True(t, set.Has(key.Signer))
		require.Equal(t, set.Attestors[i].Signer, key.Signer)
		if i > 0 {
			require.Equal(t, -1, bytes.Compare(h.keys[i-1].Signer[:], key.Signer[:]))
		}
	}
	want := teeattestor.Attestation{Family: 0, Mrtd: mrtd, ReportData: reportData, Timestamp: blockOpen - 60}
	for mask := 0; mask < 1<<len(h.keys); mask++ {
		var signers []xwebtestutil.Attestor
		for i, key := range h.keys {
			if mask&(1<<i) != 0 {
				signers = append(signers, key)
			}
		}
		quote := h.quote(0, blockOpen-60, false, uint64(mask), signers...)
		if len(signers) >= 3 {
			require.Equal(t, want, attestation(t, h.view("verify", uint8(0), quote)), mask)
			require.Equal(t, want, attestation(t, h.view("verifyAndExpect", uint8(0), quote, reportData)), mask)
			continue
		}
		h.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain, uint8(0), quote)
		h.refuses("verifyAndExpect", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain, uint8(0), quote, reportData)
		ret, _, err := h.evm.StaticCall(caller, h.precompile.Address(), h.input("verify", uint8(0), quote), suppliedGas)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)
		require.Equal(t, pcommon.RevertReason(h.stateDB.GetPrecompileError()), ret)
	}
	var outsider xwebtestutil.Attestor
	for _, key := range xwebtestutil.Attestors(6) {
		if !set.Has(key.Signer) {
			outsider = key
		}
	}
	require.False(t, set.Has(outsider.Signer))
	require.NotEqual(t, xwebtypes.Address20{}, outsider.Signer)
	h.refuses("verify", "LXP_AGENT_TEE_UNTRUSTED_CHAIN: untrusted chain", agentcommon.ErrTEEUntrustedChain,
		uint8(0), h.quote(0, blockOpen-60, false, 7, h.keys[0], h.keys[1], outsider))
	require.Equal(t, set, h.keeper.GetAttestorSet(h.stateDB.Ctx()))
}

func TestResolutionRegistered(t *testing.T) {
	testApp := app.Setup(t, false, true, false)
	keepers := testApp.GetPrecompileKeepers()
	address := ethcommon.HexToAddress(teeattestor.TeeAttestorAddress)
	set := precompiles.GetCustomPrecompiles(app.LatestUpgrade, keepers)
	versions, found := set[address]
	require.True(t, found)
	require.Len(t, versions, 2)
	served, ok := versions[precompiles.MergeUpgrade].(precompiles.IPrecompile)
	require.True(t, ok)
	require.Same(t, served, versions[app.LatestUpgrade])
	require.Equal(t, address, served.Address())
	require.Equal(t, "tee attestor", served.GetName())
	require.EqualError(t, precompiles.RegisterMergeConstructor(address, teeattestor.NewMergePrecompile),
		"merge constructor for "+address.Hex()+" already registered")

	k := &testApp.EvmKeeper
	k.SetCustomPrecompileActivation([]evmkeeper.CustomPrecompileActivation{{
		Upgrade: precompiles.MergeUpgrade,
		Modules: map[ethcommon.Address][]string{address: {"layerxagent"}},
	}})
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Unix(int64(blockOpen), 0))
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	const planHeight = int64(20)
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), precompiles.MergeUpgrade)
	for _, key := range xwebtestutil.Attestors(5) {
		require.NoError(t, testApp.XWebKeeper.RegisterAttestor(ctx, xwebtypes.MsgRegisterAttestor{
			Authority: xwebtypes.DefaultAuthority(), Attestor: key.Registration(),
		}))
	}
	k.SetCustomPrecompiles(set, app.LatestUpgrade)
	for _, height := range []int64{1, planHeight - 1} {
		require.NotContains(t, k.CustomPrecompiles(ctx.WithBlockHeight(height)), address, height)
	}
	a := served.GetABI()
	input, err := a.Pack("rootCount", uint8(0))
	require.NoError(t, err)
	for _, height := range []int64{planHeight, planHeight + 1} {
		at := k.CustomPrecompiles(ctx.WithBlockHeight(height))
		require.Same(t, served, at[address], height)
		db := state.NewDBImpl(ctx.WithBlockHeight(height), k, false)
		ret, err := at[address].Run(&vm.EVM{StateDB: db}, ethcommon.Address{}, address, input, nil, true, false, nil)
		require.NoError(t, err, height)
		count, err := a.Methods["rootCount"].Outputs.Unpack(ret)
		require.NoError(t, err)
		require.Equal(t, big.NewInt(5), count[0], height)
	}
	db := state.NewDBImpl(ctx.WithBlockHeight(planHeight-1), k, false)
	ret, err := served.Run(&vm.EVM{StateDB: db}, ethcommon.Address{}, address, input, nil, true, false, nil)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_AGENT_NOT_ACTIVE: "+precompiles.MergeUpgrade+" not active at height 19", reason)
	require.ErrorIs(t, db.GetPrecompileError(), agentcommon.ErrNotActive)
}
