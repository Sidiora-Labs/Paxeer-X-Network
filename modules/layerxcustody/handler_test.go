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
	ctx, _ := testkeeper.EVMTestApp.NewContext(false, tmtypes.Header{}).WithBlockHeight(7).CacheContext()
	return ctx
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
