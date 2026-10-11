package addr_test

import (
	"crypto/ed25519"
	"errors"
	"math/big"
	"testing"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

// TestResolutionAddrReason drives bindLayerX failures through the dynamic-gas
// reason path: below the merge plan's height the revert data stays empty, from
// it the data is Error(string) carrying an LXP_ reason, including for errors
// that carry no addr registry code.
func TestResolutionAddrReason(t *testing.T) {
	const planHeight = int64(100)
	testApp := testkeeper.EVMTestApp
	ctx := testApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(planHeight - 1).WithChainID("layerx-binding-test")
	ctx = ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	k := &testApp.EvmKeeper
	p, err := addr.NewPrecompile(testApp.GetPrecompileKeepers())
	require.NoError(t, err)
	k.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), pcommon.MergeUpgrade)

	_, caller := testkeeper.MockAddressPair()
	_, thief := testkeeper.MockAddressPair()
	didPublicKey, private := newDidKey(t)
	message, err := types.LayerXBindMessage(k.ChainID(ctx), caller, 0)
	require.NoError(t, err)
	signature := ed25519.Sign(private, message)

	bindInput, err := p.ABI.Pack(addr.BindLayerXMethod, didPublicKey, signature)
	require.NoError(t, err)
	truncated := append(append([]byte{}, p.ABI.Methods[addr.BindLayerXMethod].ID...), 0x01, 0x02)
	_, unpackErr := p.ABI.Methods[addr.BindLayerXMethod].Inputs.Unpack(truncated[4:])
	require.Error(t, unpackErr)

	call := func(stateDB *state.DBImpl, caller common.Address, input []byte, value *big.Int) ([]byte, error) {
		ret, _, err := p.RunAndCalculateGas(&vm.EVM{StateDB: stateDB}, caller, caller, input, 200000, value, nil, false, false)
		return ret, err
	}

	tests := []struct {
		name   string
		caller common.Address
		input  []byte
		value  *big.Int
		want   []byte
		reason string
		cause  error
	}{
		{
			name: "signature refused", caller: thief, input: bindInput, value: big.NewInt(0),
			want:   pcommon.LXPRevert("LXP_ADDR_LAYERX_SIGNATURE", ""),
			reason: "LXP_ADDR_LAYERX_SIGNATURE: layerx binding: signature refused",
			cause:  types.ErrLayerXSignature,
		},
		{
			name: "non-payable", caller: caller, input: bindInput, value: big.NewInt(1),
			want:   pcommon.RevertReason(errors.New("LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function")),
			reason: "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function",
		},
		{
			name: "undecodable arguments", caller: caller, input: truncated, value: big.NewInt(0),
			want:   pcommon.LXPRevert("LXP_UNKNOWN", unpackErr.Error()),
			reason: "LXP_UNKNOWN: unknown; " + unpackErr.Error(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Below the plan height the revert carries no data.
			ret, err := call(state.NewDBImpl(ctx.WithBlockHeight(planHeight-1), k, false), tt.caller, tt.input, tt.value)
			require.Equal(t, vm.ErrExecutionReverted, err)
			require.Nil(t, ret)

			// From the plan height it is Error(string) with an LXP_ reason.
			stateDB := state.NewDBImpl(ctx.WithBlockHeight(planHeight), k, false)
			ret, err = call(stateDB, tt.caller, tt.input, tt.value)
			require.Equal(t, vm.ErrExecutionReverted, err)
			require.Equal(t, tt.want, ret)
			require.Equal(t, []byte{0x08, 0xc3, 0x79, 0xa0}, ret[:4])
			reason, err := abi.UnpackRevert(ret)
			require.NoError(t, err)
			require.Equal(t, tt.reason, reason)
			refusal := stateDB.GetPrecompileError()
			require.Error(t, refusal)
			if tt.cause != nil {
				require.ErrorIs(t, refusal, tt.cause)
			}
		})
	}

	// A bind that succeeds at the plan height leaves getLayerXDid's outputs as
	// they were: the key and its did:layerx string, read through GetLayerXDid.
	stateDB := state.NewDBImpl(ctx.WithBlockHeight(planHeight), k, false)
	ret, err := call(stateDB, caller, bindInput, big.NewInt(0))
	require.NoError(t, err)
	require.Empty(t, ret)
	didInput, err := p.ABI.Pack(addr.GetLayerXDidMethod, caller)
	require.NoError(t, err)
	ret, err = call(stateDB, caller, didInput, big.NewInt(0))
	require.NoError(t, err)
	out, err := p.ABI.Methods[addr.GetLayerXDidMethod].Outputs.Unpack(ret)
	require.NoError(t, err)
	require.Equal(t, []interface{}{didPublicKey, types.LayerXDid(didPublicKey)}, out)
	stored, found := k.GetLayerXDid(stateDB.Ctx(), caller)
	require.True(t, found)
	require.Equal(t, didPublicKey, stored)
}
