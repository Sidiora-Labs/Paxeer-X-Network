package layerxcustody_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/testvectors"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/client/cli"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
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

func pendingGovernanceClaim(t *testing.T) (sdk.Context, *keeper.Keeper, types.Claim) {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := newCtx().WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	k := keeper.NewKeeper(app.AppCodec(), app.GetKey(types.StoreKey),
		app.AccountKeeper, app.BankKeeper, &app.EvmKeeper, app.UpgradeKeeper)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	vectors, err := testvectors.Load()
	require.NoError(t, err)
	require.NotEmpty(t, vectors["withdrawal"])
	vector := vectors["withdrawal"][0]
	batch, err := vector.Uint64("batch_number")
	require.NoError(t, err)
	network, err := vector.Uint64("network_id")
	require.NoError(t, err)
	require.LessOrEqual(t, network, uint64(1<<32-1))
	params := k.GetParams(ctx)
	params.NetworkId = uint32(network)
	params.WithdrawalDelaySeconds = 600
	params.SequencerAuthorizations = []types.SequencerAuthorization{{
		SequencerId: vector.Fields["sequencer_id"], PublicKey: vector.Fields["public_key"],
		FirstBatchNumber: batch, LastBatchNumber: batch}}
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.SetAsset(ctx, types.AssetMapping{AssetId: vector.Fields["asset"],
		Denom: sdk.MustGetBaseDenom(), Enabled: true}))
	stateRoot, err := vector.Array32("header_state_root")
	require.NoError(t, err)
	receiptRoot, err := vector.Array32("header_receipt_root")
	require.NoError(t, err)
	require.NoError(t, k.RegisterCheckpoint(ctx, batch, stateRoot, receiptRoot))

	payer := common.HexToAddress("0x4242424242424242424242424242424242424242")
	payerAcc := sdk.AccAddress(payer.Bytes())
	app.EvmKeeper.SetAddressMapping(ctx, payerAcc, payer)
	coins := sdk.NewCoins(sdk.NewInt64Coin(sdk.MustGetBaseDenom(), 1000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", payerAcc, coins))
	withdrawalAsset, err := types.ParseHash32(vector.Fields["asset"])
	require.NoError(t, err)
	_, err = k.Deposit(ctx, payer, payerAcc, withdrawalAsset, [32]byte{0xbe, 0xef}, sdk.NewInt(1000))
	require.NoError(t, err)
	decode := func(name string) []byte {
		value, decodeErr := vector.Bytes(name)
		require.NoError(t, decodeErr)
		return value
	}
	response, err := keeper.NewMsgServerImpl(k).RequestWithdrawal(sdk.WrapSDKContext(ctx), &types.MsgRequestWithdrawal{
		Receipt: decode("receipt"), Proof: decode("proof"), Header: decode("header"),
		HeaderSignature: decode("header_signature")})
	require.NoError(t, err)
	claimID, err := types.ParseHash32(response.ClaimId)
	require.NoError(t, err)
	claim, found := k.GetClaim(ctx, claimID)
	require.True(t, found)
	require.Equal(t, types.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status)
	require.Equal(t, ctx.BlockTime().Unix()+600, claim.AvailableAt)
	nullifierID, err := types.ParseHash32(claim.Nullifier)
	require.NoError(t, err)
	nullifier, found := k.GetNullifier(ctx, nullifierID)
	require.True(t, found)
	require.Equal(t, types.Nullifier{Nullifier: claim.Nullifier, Status: types.NullifierStatus_NULLIFIER_STATUS_RESERVED,
		ClaimId: claim.ClaimId, WithdrawalId: claim.WithdrawalId}, nullifier)
	require.Equal(t, claim.Amount, k.GetTotals(ctx, withdrawalAsset).Pending)
	message, broken := keeper.SolvencyInvariant(k)(ctx)
	require.False(t, broken, message)
	return ctx, k, claim
}

func governanceMessages(t *testing.T, ctx sdk.Context, k *keeper.Keeper, claim types.Claim) ([]sdk.Msg, types.Params, *types.MsgRegisterCheckpoint) {
	t.Helper()
	governance := types.GovernanceAuthority()
	params := k.GetParams(ctx)
	params.DepositRootAuthority = depositRootAuthority()
	checkpoint := &types.MsgRegisterCheckpoint{Authority: governance, BatchNumber: claim.BatchNumber + 1,
		StateRoot: assetID("governed state"), ReceiptRoot: assetID("governed receipt")}
	return []sdk.Msg{
		&types.MsgUpdateParams{Authority: governance, Params: params},
		&types.MsgSetAsset{Authority: governance, Asset: sidAsset()},
		checkpoint,
		&types.MsgSetEmergency{Authority: governance, Enabled: true},
		&types.MsgCancelClaim{Authority: governance, ClaimId: claim.ClaimId},
	}, params, checkpoint
}

func custodyStoreSnapshot(t *testing.T, ctx sdk.Context) map[string][]byte {
	t.Helper()
	iterator := ctx.KVStore(testkeeper.EVMTestApp.GetKey(types.StoreKey)).Iterator(nil, nil)
	defer func() { require.NoError(t, iterator.Close()) }()
	state := make(map[string][]byte)
	for ; iterator.Valid(); iterator.Next() {
		state[string(iterator.Key())] = append([]byte(nil), iterator.Value()...)
	}
	return state
}

func TestCustodyProposalDispatchesAllGovernanceMessages(t *testing.T) {
	ctx, k, pending := pendingGovernanceClaim(t)
	messages, params, checkpoint := governanceMessages(t, ctx, k, pending)
	proposal, err := types.NewCustodyProposal("All custody operations", "Execute every governed custody message", messages...)
	require.NoError(t, err)
	require.NoError(t, proposal.ValidateBasic())
	eventsBefore := append(sdk.Events(nil), ctx.EventManager().Events()...)
	withdrawalAsset, err := types.ParseHash32(pending.AssetId)
	require.NoError(t, err)
	totalsBefore := k.GetTotals(ctx, withdrawalAsset)

	require.NoError(t, layerxcustody.NewProposalHandler(k)(ctx, proposal))

	require.Equal(t, params, k.GetParams(ctx))
	id, err := types.ParseHash32(assetID("SID"))
	require.NoError(t, err)
	asset, found := k.GetAsset(ctx, id)
	require.True(t, found)
	expectedAsset := sidAsset()
	expectedAsset.Pointer = types.Address(mustAddress(t, sidPointer))
	require.Equal(t, expectedAsset, asset)
	byPointer, found := k.GetAssetByPointer(ctx, mustAddress(t, sidPointer))
	require.True(t, found)
	require.Equal(t, expectedAsset, byPointer)
	byDenom, found := k.GetAssetByDenom(ctx, "usid")
	require.True(t, found)
	require.Equal(t, expectedAsset, byDenom)
	registered, found := k.GetCheckpoint(ctx, checkpoint.BatchNumber)
	require.True(t, found)
	require.Equal(t, types.Checkpoint{BatchNumber: checkpoint.BatchNumber, StateRoot: checkpoint.StateRoot,
		ReceiptRoot: checkpoint.ReceiptRoot, FinalizedAt: ctx.BlockTime().Unix()}, registered)
	latest, finalizedAt, found := k.Anchor().LatestFinalizedBatch(ctx)
	require.True(t, found)
	require.Equal(t, checkpoint.BatchNumber, latest)
	require.Equal(t, ctx.BlockTime().Unix(), finalizedAt)
	require.True(t, k.GetEmergency(ctx))
	claimID, err := types.ParseHash32(pending.ClaimId)
	require.NoError(t, err)
	cancelled, found := k.GetClaim(ctx, claimID)
	require.True(t, found)
	expectedClaim := pending
	expectedClaim.Status = types.ClaimStatus_CLAIM_STATUS_CANCELLED
	require.Equal(t, expectedClaim, cancelled)
	nullifierID, err := types.ParseHash32(pending.Nullifier)
	require.NoError(t, err)
	nullifier, found := k.GetNullifier(ctx, nullifierID)
	require.True(t, found)
	require.Equal(t, types.Nullifier{Nullifier: pending.Nullifier, Status: types.NullifierStatus_NULLIFIER_STATUS_CANCELLED,
		ClaimId: pending.ClaimId, WithdrawalId: pending.WithdrawalId}, nullifier)
	expectedTotals := totalsBefore
	expectedTotals.Pending = "0"
	require.Equal(t, expectedTotals, k.GetTotals(ctx, withdrawalAsset))
	message, broken := keeper.SolvencyInvariant(k)(ctx)
	require.False(t, broken, message)

	expectedEvents := sdk.NewEventManager()
	expectedEvents.EmitEvents(eventsBefore)
	require.NoError(t, expectedEvents.EmitTypedEvents(
		&types.EventCheckpointRegistered{BatchNumber: checkpoint.BatchNumber, StateRoot: checkpoint.StateRoot, ReceiptRoot: checkpoint.ReceiptRoot},
		&types.EventEmergencySet{Enabled: true},
		&types.EventClaimCancelled{ClaimId: pending.ClaimId, Nullifier: pending.Nullifier}))
	require.Equal(t, expectedEvents.Events(), ctx.EventManager().Events())
}

func TestCustodyProposalWithoutUpgradeReaderPreservesStateAndEvents(t *testing.T) {
	ctx, k, pending := pendingGovernanceClaim(t)
	app := testkeeper.EVMTestApp
	withoutReader := keeper.NewKeeper(app.AppCodec(), app.GetKey(types.StoreKey),
		app.AccountKeeper, app.BankKeeper, &app.EvmKeeper, nil)
	messages, _, _ := governanceMessages(t, ctx, k, pending)
	proposal, err := types.NewCustodyProposal("Missing activation reader", "Every governed message must be refused", messages...)
	require.NoError(t, err)
	require.NoError(t, proposal.ValidateBasic())
	require.NoError(t, k.GovernanceExecutionActive(ctx))
	before := custodyStoreSnapshot(t, ctx)
	genesisBefore := k.ExportGenesis(ctx)
	eventsBefore := append(sdk.Events(nil), ctx.EventManager().Events()...)

	require.ErrorIs(t, layerxcustody.NewProposalHandler(withoutReader)(ctx, proposal), types.ErrGovernanceNotActive)

	require.Equal(t, before, custodyStoreSnapshot(t, ctx))
	require.Equal(t, genesisBefore, k.ExportGenesis(ctx))
	require.Equal(t, eventsBefore, ctx.EventManager().Events())
}

func TestCustodyProposalRollsBackAllGovernanceStateAndEvents(t *testing.T) {
	for _, failure := range []string{"missing claim", "immutable checkpoint", "unauthorized"} {
		t.Run(failure, func(t *testing.T) {
			ctx, k, pending := pendingGovernanceClaim(t)
			messages, _, checkpoint := governanceMessages(t, ctx, k, pending)
			var expectedError error
			switch failure {
			case "missing claim":
				messages = append(messages, &types.MsgCancelClaim{Authority: types.GovernanceAuthority(), ClaimId: assetID("missing claim")})
				expectedError = types.ErrClaimNotPending
			case "immutable checkpoint":
				messages = append(messages, &types.MsgRegisterCheckpoint{Authority: types.GovernanceAuthority(),
					BatchNumber: pending.BatchNumber, StateRoot: checkpoint.StateRoot, ReceiptRoot: checkpoint.ReceiptRoot})
				expectedError = types.ErrInvalidCheckpoint
			case "unauthorized":
				messages = append(messages, &types.MsgSetEmergency{Authority: sdk.AccAddress(make([]byte, 20)).String(), Enabled: false})
				expectedError = sdkerrors.ErrUnauthorized
			}
			proposal, err := types.NewCustodyProposal("Atomic custody", "Refuse without preserving partial mutations", messages...)
			require.NoError(t, err)
			before := custodyStoreSnapshot(t, ctx)
			genesisBefore := k.ExportGenesis(ctx)
			eventsBefore := append(sdk.Events(nil), ctx.EventManager().Events()...)

			require.ErrorIs(t, layerxcustody.NewProposalHandler(k)(ctx, proposal), expectedError)

			require.Equal(t, before, custodyStoreSnapshot(t, ctx))
			require.Equal(t, genesisBefore, k.ExportGenesis(ctx))
			require.Equal(t, eventsBefore, ctx.EventManager().Events())
			require.Equal(t, genesisBefore.Params, k.GetParams(ctx))
			require.Equal(t, genesisBefore.Emergency, k.GetEmergency(ctx))
			_, found := k.GetCheckpoint(ctx, checkpoint.BatchNumber)
			require.False(t, found)
			for _, previous := range genesisBefore.Checkpoints {
				got, exists := k.GetCheckpoint(ctx, previous.BatchNumber)
				require.True(t, exists)
				require.Equal(t, previous, got)
			}
			claimID, err := types.ParseHash32(pending.ClaimId)
			require.NoError(t, err)
			claim, found := k.GetClaim(ctx, claimID)
			require.True(t, found)
			require.Equal(t, pending, claim)
			nullifierID, err := types.ParseHash32(pending.Nullifier)
			require.NoError(t, err)
			nullifier, found := k.GetNullifier(ctx, nullifierID)
			require.True(t, found)
			require.Equal(t, types.Nullifier{Nullifier: pending.Nullifier, Status: types.NullifierStatus_NULLIFIER_STATUS_RESERVED,
				ClaimId: pending.ClaimId, WithdrawalId: pending.WithdrawalId}, nullifier)
			withdrawalAsset, err := types.ParseHash32(pending.AssetId)
			require.NoError(t, err)
			require.Equal(t, pending.Amount, k.GetTotals(ctx, withdrawalAsset).Pending)
			_, found = k.GetAssetByPointer(ctx, mustAddress(t, sidPointer))
			require.False(t, found)
			_, found = k.GetAssetByDenom(ctx, "usid")
			require.False(t, found)
		})
	}
}
