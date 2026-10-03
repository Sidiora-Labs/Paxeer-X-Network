package types_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	"github.com/sidiora-labs/paxeer-network/sdk/codec"
	cdctypes "github.com/sidiora-labs/paxeer-network/sdk/codec/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	sdkerrors "github.com/sidiora-labs/paxeer-network/sdk/types/errors"
	banktypes "github.com/sidiora-labs/paxeer-network/sdk/x/bank/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func proposalCodec() (cdctypes.InterfaceRegistry, *codec.ProtoCodec) {
	registry := cdctypes.NewInterfaceRegistry()
	sdk.RegisterInterfaces(registry)
	govtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	types.RegisterInterfaces(registry)
	return registry, codec.NewProtoCodec(registry)
}

func hash32(label string) string {
	sum := sha256.Sum256([]byte(label))
	return hex.EncodeToString(sum[:])
}

func ed25519Key(label string) string {
	seed := sha256.Sum256([]byte(label))
	return hex.EncodeToString(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
}

func otherAddress() string {
	return sdk.AccAddress(make([]byte, 20)).String()
}

// governanceMessages returns one fully populated message of every
// authority-gated type, in a fixed execution order.
func governanceMessages(authority string) []sdk.Msg {
	return []sdk.Msg{
		&types.MsgUpdateParams{Authority: authority, Params: types.Params{
			Authority:              authority,
			NetworkId:              125,
			WithdrawalDelaySeconds: 7200,
			ForcedExitDelaySeconds: 600,
			LivenessBoundSeconds:   43200,
			SequencerAuthorizations: []types.SequencerAuthorization{{
				SequencerId: hash32("sequencer"), PublicKey: ed25519Key("sequencer key"),
				FirstBatchNumber: 3, LastBatchNumber: 900,
			}},
			DepositRootAuthority: ed25519Key("deposit root"),
		}},
		&types.MsgSetAsset{Authority: authority, Asset: types.AssetMapping{
			AssetId: hash32("layerx-asset:125:SID"), Denom: "usid",
			Pointer: "0x21f7b20a555199fa73A238B1a91FD0f549068fEe", Enabled: true,
			MinimumDeposit: "5", CustodyCap: "1000000",
		}},
		&types.MsgRegisterCheckpoint{Authority: authority, BatchNumber: 42,
			StateRoot: hash32("state root"), ReceiptRoot: hash32("receipt root")},
		&types.MsgSetEmergency{Authority: authority, Enabled: true},
		&types.MsgCancelClaim{Authority: authority, ClaimId: hash32("claim")},
	}
}

func TestCustodyProposalTypedMessageRoundTrip(t *testing.T) {
	registry, cdc := proposalCodec()
	msgs := governanceMessages(types.GovernanceAuthority())
	proposal, err := types.NewCustodyProposal("Custody governance", "Every governed custody change", msgs...)
	require.NoError(t, err)
	require.NoError(t, proposal.ValidateBasic())
	require.Equal(t, types.RouterKey, proposal.ProposalRoute())
	require.Equal(t, types.ProposalTypeCustody, proposal.ProposalType())

	for i, msg := range msgs {
		require.Equal(t, sdk.MsgTypeURL(msg), proposal.Messages[i].TypeUrl)
		resolved, err := registry.Resolve(proposal.Messages[i].TypeUrl)
		require.NoError(t, err)
		require.IsType(t, msg, resolved)
	}
	content, err := cdctypes.NewAnyWithValue(proposal)
	require.NoError(t, err)
	resolved, err := registry.Resolve(content.TypeUrl)
	require.NoError(t, err)
	require.IsType(t, &types.CustodyProposal{}, resolved)

	check := func(decoded *types.CustodyProposal) {
		t.Helper()
		require.Equal(t, proposal.Title, decoded.Title)
		require.Equal(t, proposal.Description, decoded.Description)
		require.NoError(t, decoded.ValidateBasic())
		got, err := decoded.GetMessages()
		require.NoError(t, err)
		require.Len(t, got, len(msgs))
		for i := range msgs {
			require.Equal(t, msgs[i], got[i], "message %d", i)
		}
	}

	binary, err := cdc.Marshal(proposal)
	require.NoError(t, err)
	var fromBinary types.CustodyProposal
	require.NoError(t, cdc.Unmarshal(binary, &fromBinary))
	check(&fromBinary)

	jsonBytes, err := cdc.MarshalAsJSON(proposal)
	require.NoError(t, err)
	var fromJSON types.CustodyProposal
	require.NoError(t, cdc.UnmarshalAsJSON(jsonBytes, &fromJSON))
	check(&fromJSON)

	packed, err := cdc.MarshalInterface(proposal)
	require.NoError(t, err)
	var decodedContent govtypes.Content
	require.NoError(t, cdc.UnmarshalInterface(packed, &decodedContent))
	asProposal, ok := decodedContent.(*types.CustodyProposal)
	require.True(t, ok)
	check(asProposal)

	packedJSON, err := cdc.MarshalInterfaceJSON(proposal)
	require.NoError(t, err)
	var decodedJSONContent govtypes.Content
	require.NoError(t, cdc.UnmarshalInterfaceJSON(packedJSON, &decodedJSONContent))
	asProposal, ok = decodedJSONContent.(*types.CustodyProposal)
	require.True(t, ok)
	check(asProposal)
}

func TestCustodyProposalAuthorityRefusals(t *testing.T) {
	for _, authority := range []string{"", "not-an-address", otherAddress()} {
		for i, msg := range governanceMessages(authority) {
			proposal, err := types.NewCustodyProposal("Custody governance", "Wrong authority", msg)
			require.NoError(t, err)
			err = proposal.ValidateBasic()
			require.Error(t, err, "authority %q message %d", authority, i)
			if authority == otherAddress() {
				require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
			} else {
				require.ErrorIs(t, err, sdkerrors.ErrInvalidAddress)
			}
		}
	}
	// A governance authority later in the batch does not excuse an earlier one.
	governance := governanceMessages(types.GovernanceAuthority())
	mixed := append([]sdk.Msg{governanceMessages(otherAddress())[3]}, governance...)
	proposal, err := types.NewCustodyProposal("Custody governance", "Mixed authority", mixed...)
	require.NoError(t, err)
	require.ErrorIs(t, proposal.ValidateBasic(), sdkerrors.ErrUnauthorized)
}

func TestCustodyProposalMalformedContentRefusals(t *testing.T) {
	_, cdc := proposalCodec()
	governance := types.GovernanceAuthority()
	valid := governanceMessages(governance)

	noTitle, err := types.NewCustodyProposal("", "Description", valid...)
	require.NoError(t, err)
	require.Error(t, noTitle.ValidateBasic())
	noDescription, err := types.NewCustodyProposal("Title", "", valid...)
	require.NoError(t, err)
	require.Error(t, noDescription.ValidateBasic())

	empty, err := types.NewCustodyProposal("Title", "Description")
	require.NoError(t, err)
	require.ErrorIs(t, empty.ValidateBasic(), govtypes.ErrInvalidProposalContent)

	nilAny := &types.CustodyProposal{Title: "Title", Description: "Description", Messages: []*cdctypes.Any{nil}}
	require.ErrorIs(t, nilAny.ValidateBasic(), govtypes.ErrInvalidProposalContent)
	require.NotPanics(t, func() { _ = nilAny.String() })

	payload, err := cdc.Marshal(valid[1].(*types.MsgSetAsset))
	require.NoError(t, err)
	unresolved := &types.CustodyProposal{Title: "Title", Description: "Description",
		Messages: []*cdctypes.Any{{TypeUrl: sdk.MsgTypeURL(valid[1]), Value: payload}}}
	require.ErrorIs(t, unresolved.ValidateBasic(), govtypes.ErrInvalidProposalContent)

	unknown := &types.CustodyProposal{Title: "Title", Description: "Description",
		Messages: []*cdctypes.Any{{TypeUrl: "/layerxcustody.v1.MsgUnknown", Value: payload}}}
	require.ErrorIs(t, unknown.ValidateBasic(), govtypes.ErrInvalidProposalContent)
	bz, err := cdc.Marshal(unknown)
	require.NoError(t, err)
	require.Error(t, cdc.Unmarshal(bz, &types.CustodyProposal{}))

	malformed := &types.CustodyProposal{Title: "Title", Description: "Description",
		Messages: []*cdctypes.Any{{TypeUrl: sdk.MsgTypeURL(valid[1]), Value: []byte{0xff, 0xff, 0xff}}}}
	bz, err = cdc.Marshal(malformed)
	require.NoError(t, err)
	require.Error(t, cdc.Unmarshal(bz, &types.CustodyProposal{}))

	invalid := []sdk.Msg{
		&types.MsgUpdateParams{Authority: governance, Params: types.Params{LivenessBoundSeconds: 1}},
		&types.MsgSetAsset{Authority: governance, Asset: types.AssetMapping{Denom: "usid"}},
		&types.MsgRegisterCheckpoint{Authority: governance, BatchNumber: 1},
		&types.MsgCancelClaim{Authority: governance, ClaimId: "not-a-claim"},
	}
	for i, msg := range invalid {
		proposal, err := types.NewCustodyProposal("Title", "Description", valid[3], msg)
		require.NoError(t, err)
		require.Error(t, proposal.ValidateBasic(), "invalid payload %d", i)
	}
}

func TestCustodyProposalRejectsUnsupportedMessages(t *testing.T) {
	registry, cdc := proposalCodec()
	governance := types.GovernanceAuthority()
	unsupported := []sdk.Msg{
		&banktypes.MsgSend{FromAddress: governance, ToAddress: otherAddress(),
			Amount: sdk.NewCoins(sdk.NewInt64Coin("usid", 1))},
		&types.MsgRequestWithdrawal{Sender: governance},
		&types.MsgFinaliseWithdrawal{Sender: governance},
		&types.MsgRequestForcedExit{Sender: governance},
		&types.MsgExecuteForcedExit{Sender: governance},
	}
	for i, msg := range unsupported {
		_, err := types.NewCustodyProposal("Title", "Description", governanceMessages(governance)[3], msg)
		require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent, "message %d", i)

		packed, err := cdctypes.NewAnyWithValue(msg)
		require.NoError(t, err)
		proposal := &types.CustodyProposal{Title: "Title", Description: "Description", Messages: []*cdctypes.Any{packed}}
		require.ErrorIs(t, proposal.ValidateBasic(), govtypes.ErrInvalidProposalContent, "message %d", i)

		bz, err := cdc.Marshal(proposal)
		require.NoError(t, err)
		var decoded types.CustodyProposal
		require.NoError(t, cdc.Unmarshal(bz, &decoded))
		_, err = decoded.GetMessages()
		require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent, "message %d", i)
		require.ErrorIs(t, decoded.ValidateBasic(), govtypes.ErrInvalidProposalContent, "message %d", i)
	}
	_, err := registry.Resolve(sdk.MsgTypeURL(unsupported[0]))
	require.NoError(t, err)
}

func TestCustodyProposalNilMessageRefusals(t *testing.T) {
	governance := types.GovernanceAuthority()
	nils := []sdk.Msg{
		nil,
		(*types.MsgUpdateParams)(nil),
		(*types.MsgSetAsset)(nil),
		(*types.MsgRegisterCheckpoint)(nil),
		(*types.MsgSetEmergency)(nil),
		(*types.MsgCancelClaim)(nil),
	}
	for i, msg := range nils {
		require.NotPanics(t, func() {
			proposal, err := types.NewCustodyProposal("Title", "Description", governanceMessages(governance)[3], msg)
			require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent, "nil message %d", i)
			require.Nil(t, proposal)
		}, "nil message %d", i)
	}
}
