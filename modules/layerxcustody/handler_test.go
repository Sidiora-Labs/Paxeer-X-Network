package layerxcustody_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	tmtypes "github.com/sidiora-labs/paxeer-network/consensus/proto/tendermint/types"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/client/cli"
	"github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	sdkerrors "github.com/sidiora-labs/paxeer-network/sdk/types/errors"
	banktypes "github.com/sidiora-labs/paxeer-network/sdk/x/bank/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	upgradetypes "github.com/sidiora-labs/paxeer-network/sdk/x/upgrade/types"
	testkeeper "github.com/sidiora-labs/paxeer-network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

const sidPointer = "0x21f7b20a555199fa73A238B1a91FD0f549068fEe"

func assetID(symbol string) string {
	sum := sha256.Sum256([]byte("layerx-asset:125:" + symbol))
	return hex.EncodeToString(sum[:])
}

func depositRootAuthority() string {
	seed := sha256.Sum256([]byte("custody proposal test"))
	key := ed25519.NewKeyFromSeed(seed[:])
	return hex.EncodeToString(key.Public().(ed25519.PublicKey))
}

func newCtx() sdk.Context {
	return activate(inactiveCtx(), 7)
}

func inactiveCtx() sdk.Context {
	ctx, _ := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(7).CacheContext()
	return ctx
}

// activate schedules and applies the custody governance activation plan at
// height through the real upgrade keeper and returns ctx at that height.
func activate(ctx sdk.Context, height int64) sdk.Context {
	plan := upgradetypes.Plan{Name: types.GovernanceActivationUpgrade, Height: height}
	if err := testkeeper.EVMTestApp.UpgradeKeeper.ScheduleUpgrade(ctx.WithBlockHeight(height-1), plan); err != nil {
		panic(err)
	}
	testkeeper.EVMTestApp.UpgradeKeeper.ApplyUpgrade(ctx.WithBlockHeight(height), plan)
	return ctx.WithBlockHeight(height)
}

func sidAsset() types.AssetMapping {
	return types.AssetMapping{AssetId: assetID("SID"), Denom: "usid", Pointer: sidPointer, Enabled: true, MinimumDeposit: "1"}
}

func TestCustodyProposalSetsAssetAndDepositRootAuthority(t *testing.T) {
	ctx := newCtx()
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	governance := types.GovernanceAuthority()
	params := k.GetParams(ctx)
	params.DepositRootAuthority = depositRootAuthority()
	proposal, err := types.NewCustodyProposal("Custody assets", "Map SID and set the deposit root authority",
		&types.MsgSetAsset{Authority: governance, Asset: sidAsset()},
		&types.MsgUpdateParams{Authority: governance, Params: params})
	require.NoError(t, err)
	require.NoError(t, proposal.ValidateBasic())

	require.NoError(t, layerxcustody.NewProposalHandler(k)(ctx, proposal))

	id, err := types.ParseHash32(assetID("SID"))
	require.NoError(t, err)
	got, found := k.GetAsset(ctx, id)
	require.True(t, found)
	require.Equal(t, "usid", got.Denom)
	byPointer, found := k.GetAssetByPointer(ctx, mustAddress(t, sidPointer))
	require.True(t, found)
	require.Equal(t, got.AssetId, byPointer.AssetId)
	require.Equal(t, depositRootAuthority(), k.GetParams(ctx).DepositRootAuthority)
}

func mustAddress(t *testing.T, text string) common.Address {
	t.Helper()
	address, err := types.ParseAddress(text)
	require.NoError(t, err)
	return address
}

func TestCustodyProposalRefusals(t *testing.T) {
	ctx := newCtx()
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	handler := layerxcustody.NewProposalHandler(k)
	governance := types.GovernanceAuthority()

	empty := &types.CustodyProposal{Title: "Empty", Description: "No message"}
	require.ErrorIs(t, empty.ValidateBasic(), govtypes.ErrInvalidProposalContent)
	require.ErrorIs(t, handler(ctx, empty), govtypes.ErrInvalidProposalContent)

	foreign := &banktypes.MsgSend{FromAddress: governance, ToAddress: governance, Amount: sdk.NewCoins(sdk.NewInt64Coin(sdk.MustGetBaseDenom(), 1))}
	_, err := types.NewCustodyProposal("Foreign", "A bank message", foreign)
	require.ErrorIs(t, err, govtypes.ErrInvalidProposalContent)

	outsider := sdk.AccAddress(make([]byte, 20)).String()
	wrong, err := types.NewCustodyProposal("Wrong authority", "Not governance",
		&types.MsgSetAsset{Authority: outsider, Asset: sidAsset()})
	require.NoError(t, err)
	require.ErrorIs(t, wrong.ValidateBasic(), sdkerrors.ErrUnauthorized)
	require.ErrorIs(t, handler(ctx, wrong), sdkerrors.ErrUnauthorized)

	id, err := types.ParseHash32(assetID("SID"))
	require.NoError(t, err)
	_, found := k.GetAsset(ctx, id)
	require.False(t, found, "a refused proposal must not map the asset")
}

func TestSubmitCustodyProposalFile(t *testing.T) {
	governance := types.GovernanceAuthority()
	file := fmt.Sprintf(`{"title":"Custody assets","description":"Map SID","deposit":"1000usei","messages":[
		{"@type":"/paxprotocol.paxchain.layerxcustody.MsgSetAsset","authority":%q,
		 "asset":{"asset_id":%q,"denom":"usid","pointer":%q,"enabled":true}}]}`,
		governance, assetID("SID"), sidPointer)
	proposer := sdk.AccAddress(make([]byte, 20))
	cdc := testkeeper.EVMTestApp.AppCodec()

	msg, err := cli.NewSubmitCustodyProposalMsg(cdc, []byte(file), proposer)
	require.NoError(t, err)
	content, ok := msg.GetContent().(*types.CustodyProposal)
	require.True(t, ok)
	require.Len(t, content.Messages, 1)

	var unknown map[string]any
	require.NoError(t, json.Unmarshal([]byte(file), &unknown))
	unknown["extra"] = true
	withUnknown, err := json.Marshal(unknown)
	require.NoError(t, err)
	_, err = cli.NewSubmitCustodyProposalMsg(cdc, withUnknown, proposer)
	require.Error(t, err)
}

func authorityProposal(t *testing.T, authority string) *types.CustodyProposal {
	t.Helper()
	params := testkeeper.EVMTestApp.LayerXCustodyKeeper.GetParams(newCtx())
	params.DepositRootAuthority = authority
	proposal, err := types.NewCustodyProposal("Deposit root authority", "Set the deposit root authority",
		&types.MsgUpdateParams{Authority: types.GovernanceAuthority(), Params: params})
	require.NoError(t, err)
	return proposal
}

func TestCustodyProposalExecutesOnlyFromActivationHeight(t *testing.T) {
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	handler := layerxcustody.NewProposalHandler(k)
	proposal := authorityProposal(t, depositRootAuthority())

	never := inactiveCtx()
	before := k.GetParams(never)
	require.ErrorIs(t, handler(never, proposal), types.ErrGovernanceNotActive)
	require.Equal(t, before, k.GetParams(never))
	require.Empty(t, never.EventManager().Events())

	ctx := activate(inactiveCtx(), 20)
	require.Equal(t, int64(20), testkeeper.EVMTestApp.UpgradeKeeper.GetDoneHeight(ctx, types.GovernanceActivationUpgrade))
	early := ctx.WithBlockHeight(19)
	require.ErrorIs(t, handler(early, proposal), types.ErrGovernanceNotActive)
	require.Equal(t, before, k.GetParams(early))
	for _, height := range []int64{20, 21} {
		at, _ := ctx.WithBlockHeight(height).CacheContext()
		require.NoError(t, handler(at, proposal))
		require.Equal(t, depositRootAuthority(), k.GetParams(at).DepositRootAuthority)
	}
}

func TestCustodyProposalRejectsWrongContent(t *testing.T) {
	ctx := newCtx()
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	before := k.GetParams(ctx)
	err := layerxcustody.NewProposalHandler(k)(ctx, govtypes.NewTextProposal("Text", "Not custody content", false))
	require.ErrorIs(t, err, sdkerrors.ErrUnknownRequest)
	require.Equal(t, before, k.GetParams(ctx))
	require.Empty(t, ctx.EventManager().Events())
}

func TestCustodyProposalRejectsUnauthorizedMessages(t *testing.T) {
	ctx := newCtx()
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	handler := layerxcustody.NewProposalHandler(k)
	outsider := sdk.AccAddress(make([]byte, 20)).String()
	params := k.GetParams(ctx)
	before := params
	params.DepositRootAuthority = depositRootAuthority()
	for _, msg := range []sdk.Msg{
		&types.MsgUpdateParams{Authority: outsider, Params: params},
		&types.MsgSetAsset{Authority: outsider, Asset: sidAsset()},
		&types.MsgRegisterCheckpoint{Authority: outsider, BatchNumber: 1, StateRoot: assetID("state"), ReceiptRoot: assetID("receipt")},
		&types.MsgSetEmergency{Authority: outsider, Enabled: true},
		&types.MsgCancelClaim{Authority: outsider, ClaimId: assetID("claim")},
	} {
		proposal, err := types.NewCustodyProposal("Outsider", "Not governance", msg)
		require.NoError(t, err)
		require.ErrorIs(t, handler(ctx, proposal), sdkerrors.ErrUnauthorized, "%T", msg)
	}
	require.Equal(t, before, k.GetParams(ctx))
	require.Empty(t, ctx.EventManager().Events())
}

func TestCustodyProposalPreservesMessageOrder(t *testing.T) {
	ctx := newCtx()
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	governance := types.GovernanceAuthority()
	first, second := k.GetParams(ctx), k.GetParams(ctx)
	first.DepositRootAuthority = depositRootAuthority()
	seed := sha256.Sum256([]byte("second authority"))
	second.DepositRootAuthority = hex.EncodeToString(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
	proposal, err := types.NewCustodyProposal("Ordered", "The last update wins",
		&types.MsgUpdateParams{Authority: governance, Params: first},
		&types.MsgUpdateParams{Authority: governance, Params: second})
	require.NoError(t, err)
	require.NoError(t, layerxcustody.NewProposalHandler(k)(ctx, proposal))
	require.Equal(t, second.DepositRootAuthority, k.GetParams(ctx).DepositRootAuthority)
}

func TestCustodyProposalRollsBackStateIndexesAndEvents(t *testing.T) {
	ctx := newCtx()
	k := testkeeper.EVMTestApp.LayerXCustodyKeeper
	governance := types.GovernanceAuthority()
	params := k.GetParams(ctx)
	before := params
	params.DepositRootAuthority = depositRootAuthority()
	proposal, err := types.NewCustodyProposal("Partial", "A lawful update then a missing pending claim",
		&types.MsgSetAsset{Authority: governance, Asset: sidAsset()},
		&types.MsgUpdateParams{Authority: governance, Params: params},
		&types.MsgCancelClaim{Authority: governance, ClaimId: assetID("missing claim")})
	require.NoError(t, err)
	require.NoError(t, proposal.ValidateBasic())
	eventsBefore := len(ctx.EventManager().Events())

	require.Error(t, layerxcustody.NewProposalHandler(k)(ctx, proposal))

	require.Equal(t, before, k.GetParams(ctx))
	id, err := types.ParseHash32(assetID("SID"))
	require.NoError(t, err)
	_, found := k.GetAsset(ctx, id)
	require.False(t, found)
	_, found = k.GetAssetByPointer(ctx, mustAddress(t, sidPointer))
	require.False(t, found)
	require.Len(t, ctx.EventManager().Events(), eventsBefore)
}
