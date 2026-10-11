package keeper_test

import (
	"testing"

	custodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	sdkgovtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"
)

func governanceAuthority() string {
	return authtypes.NewModuleAddress(sdkgovtypes.ModuleName).String()
}

func revertText(t *testing.T, data []byte) string {
	t.Helper()
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

func (f *fixture) pause(ctx sdk.Context, paused bool) {
	f.t.Helper()
	params := f.keeper.GetParams(ctx)
	params.Paused = paused
	_, err := keeper.NewMsgServerImpl(f.keeper).UpdateParams(sdk.WrapSDKContext(ctx),
		&types.MsgUpdateParams{Authority: governanceAuthority(), Params: params})
	require.NoError(f.t, err)
	require.Equal(f.t, paused, f.keeper.GetParams(ctx).Paused)
}

func TestSetMarketOnlyTogglesFlags(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	server := keeper.NewMsgServerImpl(f.keeper)
	listed := custodytypes.Hash32(marketID)
	before := f.keeper.GetParams(ctx)
	stored, ok := before.Market(listed)
	require.True(t, ok)
	require.True(t, stored.Enabled)

	unknown := custodytypes.Hash32([32]byte{0x6d, 0x09})
	_, err := server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: governanceAuthority(),
		Market: types.Market{MarketId: unknown, MarginAssetId: custodytypes.Hash32(f.native), Enabled: true}})
	require.ErrorIs(t, err, types.ErrUnknownMarket)
	_, ok = f.keeper.GetParams(ctx).Market(unknown)
	require.False(t, ok, "MsgSetMarket never creates a market")
	require.Equal(t, before, f.keeper.GetParams(ctx))

	_, err = server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: f.callerAcc.String(),
		Market: types.Market{MarketId: listed, MarginAssetId: custodytypes.Hash32(f.native), Enabled: false}})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Equal(t, before, f.keeper.GetParams(ctx))

	_, err = server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: governanceAuthority(),
		Market: types.Market{MarketId: listed, MarginAssetId: custodytypes.Hash32(f.token), Enabled: false}})
	require.NoError(t, err)
	disabled, ok := f.keeper.GetParams(ctx).Market(listed)
	require.True(t, ok)
	require.False(t, disabled.Enabled)
	require.Equal(t, custodytypes.Hash32(f.native), disabled.MarginAssetId, "only the enabled flag changes")
	require.Len(t, f.keeper.GetParams(ctx).Markets, len(before.Markets))
	require.False(t, f.keeper.GetParams(ctx).MarginAsset(custodytypes.Hash32(f.native)))
	require.False(t, f.keeper.GetParams(ctx).MarginAsset(custodytypes.Hash32(f.token)))

	_, err = server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: governanceAuthority(),
		Market: types.Market{MarketId: listed, MarginAssetId: custodytypes.Hash32(f.native), Enabled: true}})
	require.NoError(t, err)
	require.Equal(t, before, f.keeper.GetParams(ctx))
}

func TestUpdateParamsAuthority(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	server := keeper.NewMsgServerImpl(f.keeper)
	before := f.keeper.GetParams(ctx)
	changed := before
	changed.Paused, changed.TimeInForce = true, true

	_, err := server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: f.callerAcc.String(), Params: changed})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.Equal(t, f.callerAcc.String()+" is not the exchange authority: unauthorized", err.Error())
	require.Equal(t, before, f.keeper.GetParams(ctx))

	_, err = server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: governanceAuthority(), Params: changed})
	require.NoError(t, err)
	require.Equal(t, changed, f.keeper.GetParams(ctx))

	invalid := changed
	invalid.Markets = []types.Market{changed.Markets[0], changed.Markets[0]}
	_, err = server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: governanceAuthority(), Params: invalid})
	require.ErrorIs(t, err, types.ErrInvalidParams)
	require.Equal(t, changed, f.keeper.GetParams(ctx))
}

func TestPausedRefusesWrites(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	writes := []string{"placeOrder", "pushOracle", "depositMargin", "depositMarginToken", "openPosition", "placeSpotOrder", "liquidate", "createPerpsMarket"}
	for _, method := range writes {
		require.NoError(t, f.keeper.PauseCheck(ctx, method), method)
	}

	f.pause(ctx, true)
	for _, method := range writes {
		err := f.keeper.PauseCheck(ctx, method)
		require.ErrorIs(t, err, govtypes.ErrPausedScope, method)
		require.Equal(t, "LXP_PAUSED_SCOPE", revertText(t, pcommon.LXPReason(err)), method)
	}
	require.NoError(t, f.keeper.PauseCheck(ctx, "cancelOrder"))
	require.ErrorIs(t, f.keeper.PauseCheck(ctx, ""), sdkerrors.ErrInvalidRequest)

	f.pause(ctx, false)
	require.NoError(t, f.keeper.PauseCheck(ctx, "placeOrder"))
}

func TestTifCapabilityFlag(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	require.False(t, f.keeper.GetParams(ctx).TimeInForce)
	server := keeper.NewMsgServerImpl(f.keeper)
	for _, enabled := range []bool{true, false} {
		params := f.keeper.GetParams(ctx)
		params.TimeInForce = enabled
		_, err := server.UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: governanceAuthority(), Params: params})
		require.NoError(t, err)
		require.Equal(t, enabled, f.keeper.GetParams(ctx).TimeInForce)
		require.Equal(t, params, f.keeper.GetParams(ctx))
	}
}

func TestMarketErrorsMapped(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	server := keeper.NewMsgServerImpl(f.keeper)
	before := f.keeper.GetParams(ctx)

	_, err := server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: governanceAuthority(),
		Market: types.Market{MarketId: "", MarginAssetId: custodytypes.Hash32(f.native), Enabled: true}})
	require.ErrorIs(t, err, types.ErrInvalidMarket)
	require.Regexp(t, `^LXP_EXCHANGE_INVALID_MARKET: market id: .*: invalid market$`, revertText(t, pcommon.LXPReason(err)))
	require.Equal(t, before, f.keeper.GetParams(ctx))

	unknown := custodytypes.Hash32([32]byte{0x6d, 0x09})
	_, err = server.SetMarket(sdk.WrapSDKContext(ctx), &types.MsgSetMarket{Authority: governanceAuthority(),
		Market: types.Market{MarketId: unknown, MarginAssetId: custodytypes.Hash32(f.native), Enabled: false}})
	require.ErrorIs(t, err, types.ErrUnknownMarket)
	require.Equal(t, "LXP_EXCHANGE_UNKNOWN_MARKET: market "+unknown+": market is not listed or is disabled",
		revertText(t, pcommon.LXPReason(err)))
	require.Equal(t, before, f.keeper.GetParams(ctx))
}

func TestPausedAllowsClose(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	f.pause(ctx, true)
	require.NoError(t, f.keeper.PauseCheck(ctx, "closePosition"))
	require.NoError(t, f.keeper.PauseCheck(ctx, "withdrawMargin"))
	require.ErrorIs(t, f.keeper.PauseCheck(ctx, "openPosition"), govtypes.ErrPausedScope)
}

func TestPausedAllowsSpotCancel(t *testing.T) {
	f := newFixture(t)
	ctx := f.fresh()
	f.pause(ctx, true)
	require.NoError(t, f.keeper.PauseCheck(ctx, "cancelSpotOrder"))
	require.NoError(t, f.keeper.PauseCheck(ctx, "cancelOrder"))
	require.ErrorIs(t, f.keeper.PauseCheck(ctx, "placeSpotOrder"), govtypes.ErrPausedScope)
}
