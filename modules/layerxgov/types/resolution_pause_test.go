package types_test

import (
	"bytes"
	"strings"
	"testing"

	launchpad "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	anchor "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxanchor/types"
	layerxauthtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxauth/types"
	custody "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	exchange "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxexchange/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	layerxledgertypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxledger/types"
	pay "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxpay/types"
	web "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestResolutionPauseScopes(t *testing.T) {
	require.Equal(t, types.ModuleName, types.ErrPausedScope.Codespace())
	require.Equal(t, uint32(735), types.ErrPausedScope.ABCICode())

	paused := pay.Params{Paused: true}
	err := types.CheckPauseScope(pay.ModuleName, paused.Paused, "budgetSpend")
	require.ErrorIs(t, err, types.ErrPausedScope)
	require.Equal(t, "layerxpay.budgetSpend: LXP_PAUSED_SCOPE", err.Error())
	require.NoError(t, types.CheckPauseScope(pay.ModuleName, paused.Paused, "budgetClose"))

	payExits := []string{"escrowRelease", "escrowTimeout", "budgetClose", "budgetDefund", "budgetRevoke", "streamPause", "streamClose", "serviceOfferWithdraw", "serviceCommitAbandon"}
	payWrites := []string{"escrowOpen", "escrowCapture", "escrowPartialCapture", "escrowDisputeOpen", "escrowDisputeResolve", "budgetCreate", "budgetCreateNative", "budgetFund", "budgetFundNative", "budgetAmend", "budgetDelegateAdd", "budgetDelegateRemove", "budgetSpend", "budgetPull", "budgetDelegateSpend", "streamOpen", "streamTopUp", "streamMeter", "streamSettle", "streamResume"}
	for _, method := range payExits {
		require.NoError(t, types.CheckPauseScope(pay.ModuleName, true, method), method)
		require.NoError(t, types.CheckPauseScope(pay.ModuleName, false, method), method)
	}
	for _, method := range payWrites {
		require.ErrorIs(t, types.CheckPauseScope(pay.ModuleName, true, method), types.ErrPausedScope, method)
		require.NoError(t, types.CheckPauseScope(pay.ModuleName, false, method), method)
	}
	require.True(t, pay.DefaultParams().Paused)
	require.ErrorIs(t, types.CheckPauseScope(pay.ModuleName, pay.DefaultParams().Paused, "budgetSpend"), types.ErrPausedScope)

	for _, method := range []string{"cancelOrder", "cancelSpotOrder", "closePosition", "withdrawMargin"} {
		require.NoError(t, types.CheckPauseScope(exchange.ModuleName, true, method), method)
	}
	for _, method := range []string{"depositMargin", "depositMarginToken", "placeOrder", "openPosition", "placeSpotOrder", "liquidate"} {
		require.ErrorIs(t, types.CheckPauseScope(exchange.ModuleName, true, method), types.ErrPausedScope, method)
		require.NoError(t, types.CheckPauseScope(exchange.ModuleName, false, method), method)
	}

	a := types.GovernanceAuthority()
	ledgerPause := &layerxledgertypes.MsgUpdateParams{Authority: a, Params: layerxledgertypes.Params{Paused: true}}
	authPause := &layerxauthtypes.MsgUpdateParams{Authority: a, Params: layerxauthtypes.Params{Paused: true, IdempotencyRetention: 7}}
	p, err := types.NewLayerXProposal("Pause ledger and auth", "Per-module Paused params replace the kernel halt", ledgerPause, authPause)
	require.NoError(t, err)
	require.NoError(t, p.ValidateBasic())
	carried, err := p.GetMessages()
	require.NoError(t, err)
	require.Len(t, carried, 2)
	ledgerPaused := carried[0].(*layerxledgertypes.MsgUpdateParams).Params.Paused
	authPaused := carried[1].(*layerxauthtypes.MsgUpdateParams).Params.Paused
	require.ErrorIs(t, types.CheckPauseScope(layerxledgertypes.ModuleName, ledgerPaused, "send"), types.ErrPausedScope)
	require.ErrorIs(t, types.CheckPauseScope(layerxauthtypes.ModuleName, authPaused, "submit"), types.ErrPausedScope)
	require.NoError(t, types.CheckPauseScope(layerxledgertypes.ModuleName, layerxledgertypes.DefaultParams().Paused, "send"))

	err = types.CheckPauseScope(web.ModuleName, true, "request")
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	require.Equal(t, `module "xweb" has no governance pause scope: invalid request`, err.Error())
	require.ErrorIs(t, types.CheckPauseScope(pay.ModuleName, true, ""), sdkerrors.ErrInvalidRequest)
	require.Equal(t, web.ModuleName, web.ErrPaused.Codespace())
	require.Equal(t, uint32(7), web.ErrPaused.ABCICode())
	require.False(t, web.ErrPaused.Is(types.ErrPausedScope))
	xwebPause, err := types.NewLayerXProposal("Pause xweb", "xweb keeps its own paused error", &web.MsgPause{Authority: a})
	require.NoError(t, err)
	require.NoError(t, xwebPause.ValidateBasic())
}

func TestResolutionParamsRegistered(t *testing.T) {
	var listing bytes.Buffer
	require.NoError(t, govtypes.ModuleCdc.LegacyAmino.Amino.PrintTypes(&listing))
	registered := map[string]string{}
	for _, line := range strings.Split(listing.String(), "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			continue
		}
		registered[strings.TrimSpace(cells[2])] = strings.TrimSpace(cells[1])
	}
	a := types.GovernanceAuthority()
	for _, tc := range []struct {
		name string
		msg  sdk.Msg
	}{
		{"layerxanchor/MsgUpdateParams", &anchor.MsgUpdateParams{Authority: a, Params: anchor.DefaultParams(a)}},
		{"layerxauth/MsgUpdateParams", &layerxauthtypes.MsgUpdateParams{Authority: a, Params: layerxauthtypes.DefaultParams()}},
		{"layerxcustody/MsgUpdateParams", &custody.MsgUpdateParams{Authority: a, Params: custody.DefaultParams()}},
		{"layerxexchange/MsgUpdateParams", &exchange.MsgUpdateParams{Authority: a, Params: exchange.DefaultParams()}},
		{"layerxledger/MsgUpdateParams", &layerxledgertypes.MsgUpdateParams{Authority: a, Params: layerxledgertypes.DefaultParams()}},
		{"launchpad/MsgUpdateParams", &launchpad.MsgUpdateParams{Authority: a, Params: launchpad.DefaultParams()}},
	} {
		require.Equal(t, "MsgUpdateParams", registered[tc.name], tc.name)
		p, err := types.NewLayerXProposal("Update params", "Kernel-backed module params", tc.msg)
		require.NoError(t, err, tc.name)
		require.NoError(t, p.ValidateBasic(), tc.name)
		require.Equal(t, sdk.MsgTypeURL(tc.msg), p.Messages[0].TypeUrl, tc.name)
	}
}
