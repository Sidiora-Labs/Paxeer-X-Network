package types_test

import (
	"bytes"
	"testing"

	layerxauthtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxauth/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxgov/types"
	layerxledgertypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxledger/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec"
	cdctypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/codec/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/std"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func pauseProposalCodec() *codec.ProtoCodec {
	r := cdctypes.NewInterfaceRegistry()
	std.RegisterInterfaces(r)
	govtypes.RegisterInterfaces(r)
	layerxledgertypes.RegisterInterfaces(r)
	layerxauthtypes.RegisterInterfaces(r)
	types.RegisterInterfaces(r)
	return codec.NewProtoCodec(r)
}

func TestPauseRoutesLedgerAndAuth(t *testing.T) {
	a := types.GovernanceAuthority()
	ledgerPause := &layerxledgertypes.MsgUpdateParams{Authority: a, Params: layerxledgertypes.Params{Paused: true}}
	authPause := &layerxauthtypes.MsgUpdateParams{Authority: a, Params: layerxauthtypes.Params{Paused: true, IdempotencyRetention: 7}}
	for _, msg := range []sdk.Msg{ledgerPause, authPause} {
		p, err := types.NewLayerXProposal("Pause module", "Set Paused through governance", msg)
		require.NoError(t, err)
		require.NoError(t, p.ValidateBasic())
	}
	p, err := types.NewLayerXProposal("Pause ledger and auth", "One proposal pauses both modules", ledgerPause, authPause)
	require.NoError(t, err)
	require.NoError(t, p.ValidateBasic())
	require.Equal(t, "/paxprotocol.paxchain.layerxledger.MsgUpdateParams", p.Messages[0].TypeUrl)
	require.Equal(t, sdk.MsgTypeURL(authPause), p.Messages[1].TypeUrl)
	cdc := pauseProposalCodec()
	raw, err := cdc.MarshalInterfaceJSON(p)
	require.NoError(t, err)
	var content govtypes.Content
	require.NoError(t, cdc.UnmarshalInterfaceJSON(raw, &content))
	require.NoError(t, content.ValidateBasic())
	carried, err := content.(*types.LayerXProposal).GetMessages()
	require.NoError(t, err)
	require.Len(t, carried, 2)
	require.Equal(t, ledgerPause, carried[0])
	require.Equal(t, authPause, carried[1])
	require.True(t, carried[0].(*layerxledgertypes.MsgUpdateParams).Params.Paused)
	require.True(t, carried[1].(*layerxauthtypes.MsgUpdateParams).Params.Paused)
	unpause := &layerxledgertypes.MsgUpdateParams{Authority: a, Params: layerxledgertypes.Params{Paused: false}}
	p, err = types.NewLayerXProposal("Resume ledger", "Clear Paused through governance", unpause)
	require.NoError(t, err)
	require.NoError(t, p.ValidateBasic())
}

func TestPauseRefusesForeignAuthority(t *testing.T) {
	a := types.GovernanceAuthority()
	outsider := sdk.AccAddress(bytes.Repeat([]byte{3}, 20)).String()
	for _, msg := range []sdk.Msg{
		&layerxledgertypes.MsgUpdateParams{Authority: outsider, Params: layerxledgertypes.Params{Paused: true}},
		&layerxauthtypes.MsgUpdateParams{Authority: outsider, Params: layerxauthtypes.Params{Paused: true}},
	} {
		p, err := types.NewLayerXProposal("Pause module", "Foreign authority", msg)
		require.NoError(t, err)
		err = p.ValidateBasic()
		require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent)
		require.Contains(t, err.Error(), "message authority is not the governance module account")
	}
	p, err := types.NewLayerXProposal("Pause both", "One foreign authority fails the proposal",
		&layerxledgertypes.MsgUpdateParams{Authority: a, Params: layerxledgertypes.Params{Paused: true}},
		&layerxauthtypes.MsgUpdateParams{Authority: outsider, Params: layerxauthtypes.Params{Paused: true}})
	require.NoError(t, err)
	require.ErrorIs(t, p.ValidateBasic(), govtypes.ErrInvalidProposalContent)
	_, err = types.NewLayerXProposal("Pause module", "Unrouted message", &banktypes.MsgSend{FromAddress: a, ToAddress: outsider})
	require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent)
	require.Contains(t, err.Error(), "is not a fork authority message")
}
