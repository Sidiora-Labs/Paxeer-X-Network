package types_test

import (
	"bytes"
	"encoding/json"
	"testing"

	launchpad "github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	anchor "github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	bridge "github.com/sidiora-labs/paxeer-network/modules/layerxbridge/types"
	custody "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	exchange "github.com/sidiora-labs/paxeer-network/modules/layerxexchange/types"
	"github.com/sidiora-labs/paxeer-network/modules/layerxgov/types"
	web "github.com/sidiora-labs/paxeer-network/modules/xweb/types"
	"github.com/sidiora-labs/paxeer-network/sdk/codec"
	cdctypes "github.com/sidiora-labs/paxeer-network/sdk/codec/types"
	"github.com/sidiora-labs/paxeer-network/sdk/std"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	banktypes "github.com/sidiora-labs/paxeer-network/sdk/x/bank/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func proposalCodec() *codec.ProtoCodec {
	r := cdctypes.NewInterfaceRegistry()
	std.RegisterInterfaces(r)
	govtypes.RegisterInterfaces(r)
	for _, register := range []func(cdctypes.InterfaceRegistry){anchor.RegisterInterfaces, bridge.RegisterInterfaces, custody.RegisterInterfaces, exchange.RegisterInterfaces, launchpad.RegisterInterfaces, web.RegisterInterfaces, types.RegisterInterfaces} {
		register(r)
	}
	return codec.NewProtoCodec(r)
}
func sixMessages() []sdk.Msg {
	a := types.GovernanceAuthority()
	return []sdk.Msg{&custody.MsgSetEmergency{Authority: a}, &anchor.MsgUpdateParams{Authority: a, Params: anchor.DefaultParams(a)}, &exchange.MsgUpdateParams{Authority: a, Params: exchange.DefaultParams()}, &bridge.MsgPause{Authority: a}, &launchpad.MsgUpdateParams{Authority: a, Params: launchpad.DefaultParams()}, &web.MsgPause{Authority: a}}
}
func TestLayerXProposalSixModulesRoundTrip(t *testing.T) {
	msgs := sixMessages()
	p, err := types.NewLayerXProposal("Configure fork", "Set module operating values", msgs...)
	require.NoError(t, err)
	require.NoError(t, p.ValidateBasic())
	require.Equal(t, types.RouterKey, p.ProposalRoute())
	require.Equal(t, types.ProposalTypeLayerX, p.ProposalType())
	cdc := proposalCodec()
	raw, err := cdc.MarshalInterface(p)
	require.NoError(t, err)
	var content govtypes.Content
	require.NoError(t, cdc.UnmarshalInterface(raw, &content))
	require.NoError(t, content.ValidateBasic())
	decoded := content.(*types.LayerXProposal)
	carried, err := decoded.GetMessages()
	require.NoError(t, err)
	require.Len(t, carried, len(msgs))
	for i, msg := range msgs {
		require.Equal(t, sdk.MsgTypeURL(msg), sdk.MsgTypeURL(carried[i]))
		require.Equal(t, msg.String(), carried[i].String())
	}
	raw, err = cdc.MarshalInterfaceJSON(p)
	require.NoError(t, err)
	require.NoError(t, cdc.UnmarshalInterfaceJSON(raw, &content))
	require.NoError(t, content.ValidateBasic())
	submit, err := govtypes.NewMsgSubmitProposal(p, sdk.NewCoins(), sdk.AccAddress(bytes.Repeat([]byte{1}, 20)))
	require.NoError(t, err)
	require.NotEmpty(t, submit.GetSignBytes())
	amino := codec.NewLegacyAmino()
	std.RegisterLegacyAminoCodec(amino)
	govtypes.RegisterLegacyAminoCodec(amino)
	for _, register := range []func(*codec.LegacyAmino){anchor.RegisterCodec, bridge.RegisterCodec, custody.RegisterCodec, exchange.RegisterCodec, launchpad.RegisterCodec, web.RegisterCodec, types.RegisterCodec} {
		register(amino)
	}
	raw, err = amino.MarshalAsJSON(p)
	require.NoError(t, err)
	var round types.LayerXProposal
	require.NoError(t, amino.UnmarshalAsJSON(raw, &round))
	require.NoError(t, round.ValidateBasic())
	require.Contains(t, p.String(), p.Title)
}
func TestLayerXProposalRefusals(t *testing.T) {
	a := types.GovernanceAuthority()
	for _, test := range []struct {
		name, title, description string
		msgs                     []sdk.Msg
	}{
		{"title", "", "description", sixMessages()}, {"description", "title", "", sixMessages()}, {"empty", "title", "description", nil},
		{"authority", "title", "description", []sdk.Msg{&bridge.MsgPause{Authority: sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String()}}},
		{"invalid", "title", "description", []sdk.Msg{&web.MsgSetThreshold{Authority: a, Threshold: 0}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, err := types.NewLayerXProposal(test.title, test.description, test.msgs...)
			require.NoError(t, err)
			require.Error(t, p.ValidateBasic())
		})
	}
	for _, msg := range []sdk.Msg{&banktypes.MsgSend{}, &custody.MsgRequestWithdrawal{}, nil, (*bridge.MsgPause)(nil)} {
		_, err := types.NewLayerXProposal("title", "description", msg)
		require.Error(t, err)
	}
	for _, packed := range []*cdctypes.Any{nil, {TypeUrl: "/unknown.Type", Value: []byte{1}}} {
		p := &types.LayerXProposal{Title: "title", Description: "description", Messages: []*cdctypes.Any{packed}}
		require.Error(t, p.ValidateBasic())
	}
	p, err := types.NewLayerXProposal("title", "description", &bridge.MsgPause{Authority: a})
	require.NoError(t, err)
	p.Messages[0].TypeUrl = "/unknown.Type"
	require.Error(t, p.ValidateBasic())
	var nilProposal *types.LayerXProposal
	require.Error(t, nilProposal.ValidateBasic())
	cdc := proposalCodec()
	p, err = types.NewLayerXProposal("title", "description", &bridge.MsgPause{Authority: a})
	require.NoError(t, err)
	raw, err := cdc.MarshalInterfaceJSON(p)
	require.NoError(t, err)
	var obj map[string]any
	require.NoError(t, json.Unmarshal(raw, &obj))
	obj["unexpected"] = true
	raw, err = json.Marshal(obj)
	require.NoError(t, err)
	var content govtypes.Content
	require.Error(t, cdc.UnmarshalInterfaceJSON(raw, &content))
}
func TestLayerXProposalAuthorityWhitelist(t *testing.T) {
	a := types.GovernanceAuthority()
	messages := []sdk.Msg{
		&custody.MsgUpdateParams{Authority: a}, &custody.MsgSetAsset{Authority: a}, &custody.MsgRegisterCheckpoint{Authority: a}, &custody.MsgSetEmergency{Authority: a}, &custody.MsgCancelClaim{Authority: a},
		&anchor.MsgUpdateParams{Authority: a}, &exchange.MsgUpdateParams{Authority: a}, &exchange.MsgSetMarket{Authority: a},
		&bridge.MsgRegisterChain{Authority: a}, &bridge.MsgSetAttestors{Authority: a}, &bridge.MsgSetCap{Authority: a}, &bridge.MsgPause{Authority: a}, &bridge.MsgUnpause{Authority: a}, &bridge.MsgRegisterSidioraPair{Authority: a},
		&launchpad.MsgUpdateParams{Authority: a}, &web.MsgRegisterAttestor{Authority: a}, &web.MsgRemoveAttestor{Authority: a}, &web.MsgSetThreshold{Authority: a}, &web.MsgSetParams{Authority: a}, &web.MsgPause{Authority: a}, &web.MsgUnpause{Authority: a},
	}
	for _, msg := range messages {
		t.Run(sdk.MsgTypeURL(msg), func(t *testing.T) {
			p, err := types.NewLayerXProposal("title", "description", msg)
			require.NoError(t, err)
			got, err := p.GetMessages()
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, sdk.MsgTypeURL(msg), sdk.MsgTypeURL(got[0]))
		})
	}
}
