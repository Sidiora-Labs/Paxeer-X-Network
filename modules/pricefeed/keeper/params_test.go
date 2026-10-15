package keeper_test

import (
	"bytes"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"
)

func newParamsKeeper(t *testing.T) (keeper.Keeper, sdk.Context, string) {
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8)
	k, ctx := pricefeedtestutil.NewKeeper(testApp, ctx)
	authority := types.DefaultAuthority()
	require.NoError(t, k.SetParams(ctx, types.DefaultParams(authority)))
	return k, ctx.WithEventManager(sdk.NewEventManager()), authority
}

func requireCode(t *testing.T, err error, sentinel error, name string) {
	require.ErrorIs(t, err, sentinel)
	got, ok := types.LXPName(err)
	require.True(t, ok)
	require.Equal(t, name, got)
}

func paramsEvents(ctx sdk.Context) []sdk.Event {
	var events []sdk.Event
	for _, event := range ctx.EventManager().Events() {
		if event.Type == types.EventTypeParams {
			events = append(events, event)
		}
	}
	return events
}

func TestSetParamsAuthority(t *testing.T) {
	k, ctx, authority := newParamsKeeper(t)
	stored := k.GetParams(ctx)
	require.Equal(t, types.DefaultParams(authority), stored)

	changed := types.DefaultParams(authority)
	changed.MaxPriceAgeMs = 20000
	stranger := sdk.AccAddress(bytes.Repeat([]byte{0xC3}, 20)).String()
	for _, signer := range []string{stranger, ""} {
		err := k.UpdateParams(ctx, signer, changed)
		requireCode(t, err, types.ErrUnauthorized, "LXP_PRICEFEED_UNAUTHORIZED")
		require.ErrorContains(t, err, signer+" is not the pricefeed authority")
		require.Equal(t, stored, k.GetParams(ctx))
	}
	require.Empty(t, paramsEvents(ctx))
}

func TestSetParamsStores(t *testing.T) {
	testApp := app.Setup(t, false, false, false)
	k, ctx := pricefeedtestutil.NewKeeper(testApp, testApp.GetContextForDeliverTx([]byte{}))
	authority := types.DefaultAuthority()
	require.Equal(t, types.DefaultParams(authority), k.GetParams(ctx))
	require.Nil(t, ctx.KVStore(k.StoreKey()).Get(types.ParamsKey))

	invalid := types.DefaultParams(authority)
	invalid.MaxPriceAgeMs = invalid.StalenessWindowMs - 1
	requireCode(t, k.SetParams(ctx, invalid), types.ErrInvalidParams, "LXP_PRICEFEED_INVALID_PARAMS")
	require.Nil(t, ctx.KVStore(k.StoreKey()).Get(types.ParamsKey))

	k, ctx, authority = newParamsKeeper(t)
	changed := types.DefaultParams(authority)
	changed.MaxPriceAgeMs = 20000
	require.NoError(t, k.UpdateParams(ctx, authority, changed))
	got := k.GetParams(ctx)
	require.Equal(t, uint64(20000), got.MaxPriceAgeMs)
	require.Equal(t, changed, got)
	events := paramsEvents(ctx)
	require.Len(t, events, 1)
	require.Len(t, events[0].Attributes, 1)
	require.Equal(t, types.AttributeAuthority, string(events[0].Attributes[0].Key))
	require.Equal(t, authority, string(events[0].Attributes[0].Value))

	invalid = changed
	invalid.MaxDivergenceBps = types.BpsDenominator + 1
	requireCode(t, k.UpdateParams(ctx, authority, invalid), types.ErrInvalidParams, "LXP_PRICEFEED_INVALID_PARAMS")
	require.Equal(t, changed, k.GetParams(ctx))
	require.Len(t, paramsEvents(ctx), 1)
}

func TestSetParamsListingKeepsIds(t *testing.T) {
	k, ctx, authority := newParamsKeeper(t)
	listing := types.DefaultParams(authority)
	listing.Markets = append(listing.Markets, types.Market{
		ID:              18,
		Symbol:          "DOGE",
		ChainflowSymbol: "DOGE",
		MinPrice:        sdk.OneInt(),
		MaxPrice:        sdk.NewIntWithDecimal(1, 30),
		Enabled:         true,
	})
	require.NoError(t, k.UpdateParams(ctx, authority, listing))
	stored := k.GetParams(ctx)
	require.Len(t, stored.Markets, 19)
	require.Equal(t, uint64(18), stored.Markets[18].ID)
	require.Equal(t, "DOGE", stored.Markets[18].Symbol)

	renamed := k.GetParams(ctx)
	renamed.Markets = append([]types.Market(nil), renamed.Markets...)
	renamed.Markets[3].Symbol = "AVAX2"
	renamed.Markets[3].ChainflowSymbol = "AVAX2"
	err := k.UpdateParams(ctx, authority, renamed)
	requireCode(t, err, types.ErrInvalidParams, "LXP_PRICEFEED_INVALID_PARAMS")
	require.ErrorContains(t, err, "market 3 AVAX is listed, its id and symbol never change")
	require.Equal(t, "AVAX", k.GetParams(ctx).Markets[3].Symbol)

	removed := k.GetParams(ctx)
	removed.Markets = removed.Markets[:18]
	err = k.UpdateParams(ctx, authority, removed)
	requireCode(t, err, types.ErrInvalidParams, "LXP_PRICEFEED_INVALID_PARAMS")
	require.ErrorContains(t, err, "18 markets for 19 listed, a listed market is never removed")

	disabled := k.GetParams(ctx)
	disabled.Markets = append([]types.Market(nil), disabled.Markets...)
	disabled.Markets[3].Enabled = false
	require.NoError(t, k.UpdateParams(ctx, authority, disabled))
	require.Equal(t, disabled, k.GetParams(ctx))
	require.Len(t, paramsEvents(ctx), 2)
}
