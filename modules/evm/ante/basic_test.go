package ante_test

import (
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/ante"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	ethcore "github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/params"
	"github.com/stretchr/testify/require"
)

func TestBasicDecorator(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	a := ante.NewBasicDecorator(k)
	msg, _ := types.NewMsgEVMTransaction(&ethtx.LegacyTx{})
	ctx, err := a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err) // expect out of gas err
	dataTooLarge := make([]byte, params.MaxInitCodeSize+1)
	for i := 0; i <= params.MaxInitCodeSize; i++ {
		dataTooLarge[i] = 1
	}
	msg, _ = types.NewMsgEVMTransaction(&ethtx.LegacyTx{Data: dataTooLarge})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	require.Contains(t, err.Error(), "code size")
	negAmount := sdk.NewInt(-1)
	msg, _ = types.NewMsgEVMTransaction(&ethtx.LegacyTx{Amount: &negAmount})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Equal(t, sdkerrors.ErrInvalidCoins, err)
	data := make([]byte, 10)
	for i := 0; i < 10; i++ {
		dataTooLarge[i] = 1
	}
	msg, _ = types.NewMsgEVMTransaction(&ethtx.LegacyTx{Data: data})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.Equal(t, ethcore.ErrIntrinsicGas, err)

	msg, _ = types.NewMsgEVMTransaction(&ethtx.BlobTx{GasLimit: 21000})
	ctx, err = a.AnteHandle(ctx, &mockTx{msgs: []sdk.Msg{msg}}, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		return ctx, nil
	})
	require.NotNil(t, err)
	require.Error(t, err, sdkerrors.ErrUnsupportedTxType)
}
