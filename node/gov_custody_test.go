package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/testvectors"
	layerxcustodykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/keeper"
	layerxcustodytypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxcustody/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	upgradetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

const custodyGovernanceHeight = int64(20)

// custodyGovernance is a fresh application with a custody asset, a finalized
// checkpoint, a deposit and a pending withdrawal claim proven by the signed
// withdrawal vector.
type custodyGovernance struct {
	app        *App
	ctx        sdk.Context
	k          *layerxcustodykeeper.Keeper
	withdrawal testvectors.Vector
	batch      uint64
	claimID    string
}

func custodyVectorBytes(t *testing.T, v testvectors.Vector, key string) []byte {
	t.Helper()
	out, err := v.Bytes(key)
	require.NoError(t, err)
	return out
}

func custodyVectorArray(t *testing.T, v testvectors.Vector, key string) [32]byte {
	t.Helper()
	out, err := v.Array32(key)
	require.NoError(t, err)
	return out
}

func newCustodyGovernance(t *testing.T) *custodyGovernance {
	t.Helper()
	testApp := Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(custodyGovernanceHeight).
		WithBlockTime(time.Unix(1_800_000_000, 0).UTC())
	k := testApp.LayerXCustodyKeeper
	// This fresh application verifies against custody's own registered
	// checkpoints, so the signed withdrawal vector's checkpoint is the one it
	// trusts.
	k.SetAnchorReader(nil)
	fixture, err := testvectors.Load()
	require.NoError(t, err)
	withdrawal := fixture["withdrawal"][0]
	batch, err := withdrawal.Uint64("batch_number")
	require.NoError(t, err)
	networkID, err := withdrawal.Uint64("network_id")
	require.NoError(t, err)

	payerAcc := sdk.AccAddress(bytes.Repeat([]byte{0x0c}, 20))
	payer := common.BytesToAddress(payerAcc)
	testApp.EvmKeeper.SetAddressMapping(ctx, payerAcc, payer)
	coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(50_000_000)))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", payerAcc, coins))

	params := k.GetParams(ctx)
	params.NetworkId = uint32(networkID) //nolint:gosec
	params.WithdrawalDelaySeconds = 600
	params.SequencerAuthorizations = []layerxcustodytypes.SequencerAuthorization{{
		SequencerId: withdrawal.Fields["sequencer_id"], PublicKey: withdrawal.Fields["public_key"],
		FirstBatchNumber: batch, LastBatchNumber: batch}}
	require.NoError(t, k.SetParams(ctx, params))
	require.NoError(t, k.SetAsset(ctx, layerxcustodytypes.AssetMapping{AssetId: withdrawal.Fields["asset"],
		Denom: sdk.MustGetBaseDenom(), Enabled: true}))
	require.NoError(t, k.RegisterCheckpoint(ctx, batch, custodyVectorArray(t, withdrawal, "header_state_root"),
		custodyVectorArray(t, withdrawal, "header_receipt_root")))
	asset, err := layerxcustodytypes.ParseHash32(withdrawal.Fields["asset"])
	require.NoError(t, err)
	_, err = k.Deposit(ctx, payer, payerAcc, asset, [32]byte{0xbe, 0xef}, sdk.NewInt(1_000))
	require.NoError(t, err)
	claim, err := k.RequestWithdrawal(ctx, layerxcustodykeeper.WithdrawalEvidence{
		Receipt: custodyVectorBytes(t, withdrawal, "receipt"), Proof: custodyVectorBytes(t, withdrawal, "proof"),
		Header: custodyVectorBytes(t, withdrawal, "header"), HeaderSignature: custodyVectorBytes(t, withdrawal, "header_signature")})
	require.NoError(t, err)
	require.Equal(t, layerxcustodytypes.ClaimStatus_CLAIM_STATUS_PENDING, claim.Status)
	return &custodyGovernance{app: testApp, ctx: ctx, k: k, withdrawal: withdrawal, batch: batch, claimID: claim.ClaimId}
}

// activate schedules and applies the v6.11 plan at the context height through
// the application's upgrade keeper and its registered handler.
func (g *custodyGovernance) activate(t *testing.T) {
	t.Helper()
	if !g.app.UpgradeKeeper.HasHandler(V611Upgrade) {
		g.app.RegisterUpgradeHandlers()
	}
	require.True(t, g.app.UpgradeKeeper.HasHandler(V611Upgrade))
	plan := upgradetypes.Plan{Name: V611Upgrade, Height: g.ctx.BlockHeight()}
	require.NoError(t, g.app.UpgradeKeeper.ScheduleUpgrade(g.ctx, plan))
	g.app.UpgradeKeeper.ApplyUpgrade(g.ctx, plan)
	require.Equal(t, g.ctx.BlockHeight(), g.app.UpgradeKeeper.GetDoneHeight(g.ctx, V611Upgrade))
}

func (g *custodyGovernance) route() govtypes.Handler {
	return g.app.GovKeeper.Router().GetRoute(layerxcustodytypes.RouterKey)
}

func custodyGovernanceAsset() layerxcustodytypes.AssetMapping {
	sum := sha256.Sum256([]byte("layerx-asset:125:SID"))
	return layerxcustodytypes.AssetMapping{AssetId: hex.EncodeToString(sum[:]), Denom: "usid",
		Pointer: "0x21f7b20a555199fa73A238B1a91FD0f549068fEe", Enabled: true, MinimumDeposit: "1"}
}

func custodyGovernanceRootAuthority() string {
	seed := sha256.Sum256([]byte("custody governance router test"))
	return hex.EncodeToString(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
}

// allMessages is one proposal carrying every custody governance message.
func (g *custodyGovernance) allMessages(t *testing.T) *layerxcustodytypes.CustodyProposal {
	t.Helper()
	governance := layerxcustodytypes.GovernanceAuthority()
	params := g.k.GetParams(g.ctx)
	params.DepositRootAuthority = custodyGovernanceRootAuthority()
	proposal, err := layerxcustodytypes.NewCustodyProposal("Custody governance", "Every custody governance message",
		&layerxcustodytypes.MsgUpdateParams{Authority: governance, Params: params},
		&layerxcustodytypes.MsgSetAsset{Authority: governance, Asset: custodyGovernanceAsset()},
		&layerxcustodytypes.MsgRegisterCheckpoint{Authority: governance, BatchNumber: g.batch + 1,
			StateRoot: layerxcustodytypes.Hash32([32]byte{1}), ReceiptRoot: layerxcustodytypes.Hash32([32]byte{2})},
		&layerxcustodytypes.MsgCancelClaim{Authority: governance, ClaimId: g.claimID},
		&layerxcustodytypes.MsgSetEmergency{Authority: governance, Enabled: true})
	require.NoError(t, err)
	require.NoError(t, proposal.ValidateBasic())
	return proposal
}

// submit carries the proposal through a submit-proposal message and the codec
// into governance and returns the stored proposal.
func (g *custodyGovernance) submit(t *testing.T, content govtypes.Content) govtypes.Proposal {
	t.Helper()
	msg, err := govtypes.NewMsgSubmitProposal(content, sdk.NewCoins(), sdk.AccAddress(bytes.Repeat([]byte{0x0b}, 20)))
	require.NoError(t, err)
	encoded, err := g.app.AppCodec().MarshalInterface(msg)
	require.NoError(t, err)
	var decoded sdk.Msg
	require.NoError(t, g.app.AppCodec().UnmarshalInterface(encoded, &decoded))
	carried, ok := decoded.(*govtypes.MsgSubmitProposal)
	require.True(t, ok, "decoded a %T", decoded)
	submitted, err := g.app.GovKeeper.SubmitProposal(g.ctx, carried.GetContent())
	require.NoError(t, err)
	stored, found := g.app.GovKeeper.GetProposal(g.ctx, submitted.ProposalId)
	require.True(t, found)
	return stored
}

func (g *custodyGovernance) requireUnchanged(t *testing.T, ctx sdk.Context, state *layerxcustodytypes.GenesisState, events int) {
	t.Helper()
	require.Equal(t, state, g.k.ExportGenesis(ctx))
	require.Len(t, ctx.EventManager().Events(), events)
}

func TestCustodyGovernanceRouterDispatchesAllMessages(t *testing.T) {
	g := newCustodyGovernance(t)
	g.activate(t)
	require.True(t, g.app.GovKeeper.Router().HasRoute(layerxcustodytypes.RouterKey))
	stored := g.submit(t, g.allMessages(t))
	require.Equal(t, layerxcustodytypes.RouterKey, stored.ProposalRoute())
	content, ok := stored.GetContent().(*layerxcustodytypes.CustodyProposal)
	require.True(t, ok, "governance stored a %T", stored.GetContent())
	carried, err := content.GetMessages()
	require.NoError(t, err)
	require.Len(t, carried, 5)

	events := len(g.ctx.EventManager().Events())
	require.NoError(t, g.route()(g.ctx, stored.GetContent()))

	require.Equal(t, custodyGovernanceRootAuthority(), g.k.GetParams(g.ctx).DepositRootAuthority)
	assetID, err := layerxcustodytypes.ParseHash32(custodyGovernanceAsset().AssetId)
	require.NoError(t, err)
	asset, found := g.k.GetAsset(g.ctx, assetID)
	require.True(t, found)
	require.Equal(t, custodyGovernanceAsset().Denom, asset.Denom)
	pointer, err := layerxcustodytypes.ParseAddress(custodyGovernanceAsset().Pointer)
	require.NoError(t, err)
	byPointer, found := g.k.GetAssetByPointer(g.ctx, pointer)
	require.True(t, found)
	require.Equal(t, asset.AssetId, byPointer.AssetId)
	checkpoint, found := g.k.GetCheckpoint(g.ctx, g.batch+1)
	require.True(t, found)
	require.Equal(t, layerxcustodytypes.Hash32([32]byte{1}), checkpoint.StateRoot)
	require.Equal(t, layerxcustodytypes.Hash32([32]byte{2}), checkpoint.ReceiptRoot)
	claimID, err := layerxcustodytypes.ParseHash32(g.claimID)
	require.NoError(t, err)
	claim, found := g.k.GetClaim(g.ctx, claimID)
	require.True(t, found)
	require.Equal(t, layerxcustodytypes.ClaimStatus_CLAIM_STATUS_CANCELLED, claim.Status)
	require.True(t, g.k.GetEmergency(g.ctx))
	emitted := g.ctx.EventManager().Events()
	require.Greater(t, len(emitted), events)
	emergency, err := sdk.TypedEventToEvent(&layerxcustodytypes.EventEmergencySet{Enabled: true})
	require.NoError(t, err)
	require.Contains(t, emitted[events:], emergency)
}

func TestCustodyGovernanceRouterRefusalsAreAtomic(t *testing.T) {
	g := newCustodyGovernance(t)
	g.activate(t)
	governance := layerxcustodytypes.GovernanceAuthority()
	state := g.k.ExportGenesis(g.ctx)
	events := len(g.ctx.EventManager().Events())

	wrongContent := govtypes.NewTextProposal("Text", "Not custody content", false)
	require.ErrorIs(t, g.route()(g.ctx, wrongContent), sdkerrors.ErrUnknownRequest)
	g.requireUnchanged(t, g.ctx, state, events)

	outsider := sdk.AccAddress(make([]byte, 20)).String()
	foreign, err := layerxcustodytypes.NewCustodyProposal("Foreign authority", "Not governance",
		&layerxcustodytypes.MsgSetAsset{Authority: governance, Asset: custodyGovernanceAsset()},
		&layerxcustodytypes.MsgSetEmergency{Authority: outsider, Enabled: true})
	require.NoError(t, err)
	require.ErrorIs(t, g.route()(g.ctx, foreign), sdkerrors.ErrUnauthorized)
	g.requireUnchanged(t, g.ctx, state, events)

	// A lawful first message followed by a rewrite of the finalized checkpoint.
	checkpoint, err := layerxcustodytypes.NewCustodyProposal("Checkpoint rewrite", "Second message fails in state",
		&layerxcustodytypes.MsgSetAsset{Authority: governance, Asset: custodyGovernanceAsset()},
		&layerxcustodytypes.MsgRegisterCheckpoint{Authority: governance, BatchNumber: g.batch,
			StateRoot: layerxcustodytypes.Hash32([32]byte{1}), ReceiptRoot: layerxcustodytypes.Hash32([32]byte{2})})
	require.NoError(t, err)
	require.NoError(t, checkpoint.ValidateBasic())
	require.ErrorIs(t, g.route()(g.ctx, checkpoint), layerxcustodytypes.ErrInvalidCheckpoint)
	g.requireUnchanged(t, g.ctx, state, events)

	// A lawful emergency switch followed by the cancellation of a claim that
	// does not exist.
	missing, err := layerxcustodytypes.NewCustodyProposal("Missing claim", "Second message fails in state",
		&layerxcustodytypes.MsgSetEmergency{Authority: governance, Enabled: true},
		&layerxcustodytypes.MsgCancelClaim{Authority: governance, ClaimId: layerxcustodytypes.Hash32([32]byte{0xee})})
	require.NoError(t, err)
	require.NoError(t, missing.ValidateBasic())
	require.ErrorIs(t, g.route()(g.ctx, missing), layerxcustodytypes.ErrClaimNotPending)
	g.requireUnchanged(t, g.ctx, state, events)
	require.False(t, g.k.GetEmergency(g.ctx))
}

func TestCustodyGovernanceSubmissionDoesNotExecute(t *testing.T) {
	g := newCustodyGovernance(t)
	require.Zero(t, g.app.UpgradeKeeper.GetDoneHeight(g.ctx, V611Upgrade))
	state := g.k.ExportGenesis(g.ctx)

	// A statically valid proposal is admitted before activation and changes
	// nothing in custody.
	stored := g.submit(t, g.allMessages(t))
	require.Equal(t, state, g.k.ExportGenesis(g.ctx))

	// Executing it before activation is refused and changes nothing.
	events := len(g.ctx.EventManager().Events())
	require.ErrorIs(t, g.route()(g.ctx, stored.GetContent()), layerxcustodytypes.ErrGovernanceNotActive)
	g.requireUnchanged(t, g.ctx, state, events)
}

func TestCustodyGovernanceMsgServiceAuthority(t *testing.T) {
	g := newCustodyGovernance(t)
	require.ErrorIs(t, g.k.GovernanceExecutionActive(g.ctx), layerxcustodytypes.ErrGovernanceNotActive)
	server := layerxcustodykeeper.NewMsgServerImpl(g.k)
	goCtx := sdk.WrapSDKContext(g.ctx)
	outsider := sdk.AccAddress(bytes.Repeat([]byte{0x0d}, 20)).String()

	// The direct Msg service keeps its authority rule and needs no activation.
	_, err := server.SetEmergency(goCtx, &layerxcustodytypes.MsgSetEmergency{Authority: outsider, Enabled: true})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	require.False(t, g.k.GetEmergency(g.ctx))
	_, err = server.SetAsset(goCtx, &layerxcustodytypes.MsgSetAsset{Authority: outsider, Asset: custodyGovernanceAsset()})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)
	_, err = server.CancelClaim(goCtx, &layerxcustodytypes.MsgCancelClaim{Authority: outsider, ClaimId: g.claimID})
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	_, err = server.SetEmergency(goCtx, &layerxcustodytypes.MsgSetEmergency{Authority: g.k.Authority(g.ctx), Enabled: true})
	require.NoError(t, err)
	require.True(t, g.k.GetEmergency(g.ctx))

	// Genesis export and import are unchanged by the activation reader.
	exported := g.k.ExportGenesis(g.ctx)
	imported, _ := g.ctx.CacheContext()
	g.k.InitGenesis(imported, *exported)
	require.Equal(t, exported, g.k.ExportGenesis(imported))
}
