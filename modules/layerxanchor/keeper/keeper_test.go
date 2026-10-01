package keeper_test

import (
	"bytes"
	"github.com/gogo/protobuf/jsonpb"
	"github.com/gogo/protobuf/proto"
	moduleimpl "github.com/sidiora-labs/paxeer-network/modules/layerxanchor"
	"github.com/sidiora-labs/paxeer-network/sdk/baseapp"
	"github.com/sidiora-labs/paxeer-network/sdk/codec"
	cdctypes "github.com/sidiora-labs/paxeer-network/sdk/codec/types"
	"github.com/sidiora-labs/paxeer-network/sdk/types/module"

	"encoding/json"
	"testing"
	"time"

	"github.com/sidiora-labs/paxeer-network/layerxproof/testvectors"
	"github.com/sidiora-labs/paxeer-network/modules/layerxanchor/keeper"
	"github.com/sidiora-labs/paxeer-network/modules/layerxanchor/types"
	app "github.com/sidiora-labs/paxeer-network/node"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	bond           = int64(5_000_000)
	unbondingDelay = uint64(1000)
)

type suite struct {
	t         *testing.T
	app       *app.App
	ctx       sdk.Context
	k         keeper.Keeper
	authority sdk.AccAddress
	reporter  sdk.AccAddress
	operators []sdk.AccAddress
	ids       [][32]byte
	signers   [][20]byte
	fixture   testvectors.Fixture
}

func account(tag byte) sdk.AccAddress {
	raw := make([]byte, 20)
	for i := range raw {
		raw[i] = tag
	}
	return sdk.AccAddress(raw)
}

func (s *suite) fund(to sdk.AccAddress, amount int64) {
	s.t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(amount)))
	require.NoError(s.t, s.app.BankKeeper.MintCoins(s.ctx, "evm", coins))
	require.NoError(s.t, s.app.BankKeeper.SendCoinsFromModuleToAccount(s.ctx, "evm", to, coins))
}

func (s *suite) balance(of sdk.AccAddress) sdk.Int {
	return s.app.BankKeeper.GetBalance(s.ctx, of, "uhpx").Amount
}

func newSuite(t *testing.T, mutate func(*types.Params)) *suite {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	s := &suite{t: t, app: testApp, k: testApp.LayerXAnchorKeeper, authority: account(0xa1), reporter: account(0xa2)}
	s.ctx = testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithBlockTime(time.Unix(1_800_000_000, 0))
	fixture, err := testvectors.LoadAnchor()
	require.NoError(t, err)
	s.fixture = fixture
	context := fixture["context"][0]

	params := types.DefaultParams(s.authority.String())
	params.PaxeerChainID, err = context.Uint64("paxeer_chain_id")
	require.NoError(t, err)
	network, err := context.Uint64("network_id")
	require.NoError(t, err)
	params.NetworkID = uint32(network)
	contract, err := context.Bytes("settlement_contract")
	require.NoError(t, err)
	copy(params.SettlementContract[:], contract)
	params.Threshold = 2
	params.UnbondingDelaySeconds = unbondingDelay
	if mutate != nil {
		mutate(&params)
	}
	require.NoError(t, s.k.SetParams(s.ctx, params))

	sequencerID, err := context.Array32("sequencer_id")
	require.NoError(t, err)
	sequencerKey, err := context.Array32("sequencer_public_key")
	require.NoError(t, err)
	authorization := types.SequencerAuthorization{SequencerID: sequencerID, PublicKey: sequencerKey, FirstBatchNumber: 1, LastBatchNumber: 8}
	require.ErrorIs(t, s.k.SetSequencerAuthorization(s.ctx, s.reporter, authorization), types.ErrUnauthorized)
	require.NoError(t, s.k.SetSequencerAuthorization(s.ctx, s.authority, authorization))

	// Guarantor 3 of the fixture signs validly but never bonds.
	for index, guarantor := range fixture["guarantors"][:3] {
		id, err := guarantor.Array32("guarantor_id")
		require.NoError(t, err)
		raw, err := guarantor.Bytes("signer")
		require.NoError(t, err)
		var signer [20]byte
		copy(signer[:], raw)
		operator := account(byte(0xb0 + index))
		s.fund(operator, 2*bond)
		s.ids, s.signers, s.operators = append(s.ids, id), append(s.signers, signer), append(s.operators, operator)
	}
	return s
}

func (s *suite) bondAll() {
	s.t.Helper()
	for index := range s.ids {
		registered, err := s.k.RegisterGuarantor(s.ctx, s.operators[index], s.ids[index], s.signers[index], sdk.NewInt(bond))
		require.NoError(s.t, err)
		require.Equal(s.t, types.GuarantorPending, registered.Status)
		require.NoError(s.t, s.k.ActivateGuarantor(s.ctx, s.authority, s.ids[index]))
	}
	s.invariant()
}

func (s *suite) invariant() {
	s.t.Helper()
	message, broken := keeper.BalanceInvariant(s.k)(s.ctx)
	require.False(s.t, broken, message)
}

func (s *suite) vector(section, name string) testvectors.Vector {
	s.t.Helper()
	for _, v := range s.fixture[section] {
		if v.Name == name {
			return v
		}
	}
	s.t.Fatalf("no vector %s/%s", section, name)
	return testvectors.Vector{}
}

func (s *suite) submit(name string) (types.Checkpoint, error) {
	s.t.Helper()
	v := s.vector("checkpoints", name)
	header, err := v.Bytes("header")
	require.NoError(s.t, err)
	raw, err := v.Bytes("header_signature")
	require.NoError(s.t, err)
	var signature [64]byte
	copy(signature[:], raw)
	certificate, err := v.Bytes("certificate")
	require.NoError(s.t, err)
	return s.k.SubmitCheckpoint(s.ctx, s.reporter, header, signature, certificate)
}

func (s *suite) attestation(name string) []byte {
	s.t.Helper()
	raw, err := s.vector("attestations", name).Bytes("attestation")
	require.NoError(s.t, err)
	return raw
}

func (s *suite) advance(seconds uint64) {
	s.ctx = s.ctx.WithBlockTime(s.ctx.BlockTime().Add(time.Duration(seconds) * time.Second)).WithBlockHeight(s.ctx.BlockHeight() + 1)
}

func TestQuorumCheckpointFinalizes(t *testing.T) {
	s := newSuite(t, nil)
	s.bondAll()
	_, none := s.k.LatestFinalizedBatch(s.ctx)
	require.False(t, none)

	checkpoint, err := s.submit("batch_1_quorum")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointFinal, checkpoint.Status)
	require.Equal(t, s.ctx.BlockHeight(), checkpoint.FinalizedHeight)
	require.Equal(t, uint8(0x1f), checkpoint.AvailabilityMask)

	v := s.vector("checkpoints", "batch_1_quorum")
	expectedID, _ := v.Array32("checkpoint_id")
	expectedState, _ := v.Array32("resulting_state_root")
	expectedReceipts, _ := v.Array32("receipt_root")
	require.Equal(t, types.Hash32(expectedID), checkpoint.CheckpointID)
	state, ok := s.k.FinalizedStateRoot(s.ctx, 1)
	require.True(t, ok)
	require.Equal(t, expectedState, state)
	receipts, ok := s.k.FinalizedReceiptRoot(s.ctx, 1)
	require.True(t, ok)
	require.Equal(t, expectedReceipts, receipts)
	latest, ok := s.k.LatestFinalizedBatch(s.ctx)
	require.True(t, ok)
	require.Equal(t, uint64(1), latest)
	require.Equal(t, types.CheckpointFinal, s.k.StatusOf(s.ctx, 1))
	require.Equal(t, types.CheckpointUnknown, s.k.StatusOf(s.ctx, 2))

	authorization, ok := s.k.SequencerAuthorization(s.ctx, checkpoint.SequencerID, 1)
	require.True(t, ok)
	require.Equal(t, uint64(8), authorization.LastBatchNumber)
	_, ok = s.k.SequencerAuthorization(s.ctx, checkpoint.SequencerID, 9)
	require.False(t, ok)

	_, err = s.submit("batch_1_quorum")
	require.ErrorIs(t, err, types.ErrCheckpointFinal)

	for _, name := range []string{"batch_2_sequence_gap", "batch_2_wrong_previous_root"} {
		_, err = s.submit(name)
		require.ErrorIs(t, err, types.ErrContinuity, name)
		require.Equal(t, types.CheckpointUnknown, s.k.StatusOf(s.ctx, 2))
	}
	second, err := s.submit("batch_2_quorum")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointFinal, second.Status)
	latest, _ = s.k.LatestFinalizedBatch(s.ctx)
	require.Equal(t, uint64(2), latest)
	s.invariant()
}

func TestBelowThresholdStaysSubmitted(t *testing.T) {
	s := newSuite(t, nil)
	s.bondAll()
	checkpoint, err := s.submit("batch_1_below_threshold")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointSubmitted, checkpoint.Status)
	require.Equal(t, uint8(0), checkpoint.AvailabilityMask)
	_, ok := s.k.FinalizedStateRoot(s.ctx, 1)
	require.False(t, ok)
	_, ok = s.k.FinalizedReceiptRoot(s.ctx, 1)
	require.False(t, ok)
	_, ok = s.k.LatestFinalizedBatch(s.ctx)
	require.False(t, ok)
	_, err = s.k.Finalize(s.ctx, 1)
	require.ErrorIs(t, err, types.ErrNotFinalizable)

	// A second guarantor's possession statement reaches the availability
	// threshold without making the one-signature certificate final.
	after, record, err := s.k.SubmitAvailabilityAttestation(s.ctx, s.attestation("batch_1_attestation_1"))
	require.NoError(t, err)
	require.Equal(t, uint8(0x1f), record.ClassMask)
	require.Equal(t, uint8(0x1f), after.AvailabilityMask)
	_, _, err = s.k.SubmitAvailabilityAttestation(s.ctx, s.attestation("batch_1_attestation_1"))
	require.ErrorIs(t, err, types.ErrAvailability)
	_, _, err = s.k.SubmitAvailabilityAttestation(s.ctx, s.attestation("batch_1_conflicting_attestation_2"))
	require.ErrorIs(t, err, types.ErrAvailability)
	_, err = s.k.Finalize(s.ctx, 1)
	require.ErrorIs(t, err, types.ErrNotFinalizable)

	_, err = s.submit("batch_2_quorum")
	require.ErrorIs(t, err, types.ErrContinuity)

	final, err := s.submit("batch_1_quorum")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointFinal, final.Status)
}

func TestPendingGuarantorsDoNotCount(t *testing.T) {
	s := newSuite(t, nil)
	for index := range s.ids {
		_, err := s.k.RegisterGuarantor(s.ctx, s.operators[index], s.ids[index], s.signers[index], sdk.NewInt(bond))
		require.NoError(t, err)
	}
	_, err := s.submit("batch_1_pair")
	require.ErrorIs(t, err, types.ErrCertificate)
	require.ErrorIs(t, s.k.ActivateGuarantor(s.ctx, s.reporter, s.ids[0]), types.ErrUnauthorized)
	for _, id := range s.ids {
		require.NoError(t, s.k.ActivateGuarantor(s.ctx, s.authority, id))
	}
	checkpoint, err := s.submit("batch_1_pair")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointFinal, checkpoint.Status)
}

func TestRefusedCheckpoints(t *testing.T) {
	s := newSuite(t, nil)
	s.bondAll()
	expected := map[string]error{
		"unsorted":      types.ErrCertificate,
		"duplicate":     types.ErrCertificate,
		"signature":     types.ErrCertificate,
		"guarantor":     types.ErrCertificate,
		"freshness":     types.ErrCertificate,
		"sequencer":     types.ErrSequencerUnauthorized,
		"authorization": types.ErrSequencerUnauthorized,
		"continuity":    types.ErrContinuity,
	}
	seen := map[string]bool{}
	for _, v := range s.fixture["checkpoints"] {
		refusal := v.Fields["refusal"]
		if refusal == "" {
			continue
		}
		seen[refusal] = true
		_, err := s.submit(v.Name)
		require.ErrorIs(t, err, expected[refusal], v.Name)
		batch, _ := v.Uint64("batch_number")
		require.Equal(t, types.CheckpointUnknown, s.k.StatusOf(s.ctx, batch), v.Name)
	}
	require.Len(t, seen, len(expected))
	require.Empty(t, s.k.GetCheckpoints(s.ctx))

	v := s.vector("checkpoints", "batch_1_quorum")
	header, _ := v.Bytes("header")
	certificate, _ := v.Bytes("certificate")
	other, _ := s.vector("checkpoints", "batch_2_quorum").Bytes("header")
	raw, _ := v.Bytes("header_signature")
	var signature [64]byte
	copy(signature[:], raw)
	_, err := s.k.SubmitCheckpoint(s.ctx, s.reporter, other, signature, certificate)
	require.ErrorIs(t, err, types.ErrCertificate)
	signature[5] ^= 1
	_, err = s.k.SubmitCheckpoint(s.ctx, s.reporter, header, signature, certificate)
	require.ErrorIs(t, err, types.ErrSequencerUnauthorized)
}

func TestEquivocationSlashesOnce(t *testing.T) {
	s := newSuite(t, nil)
	s.bondAll()
	supply := s.app.BankKeeper.GetSupply(s.ctx, "uhpx").Amount
	first, second := s.attestation("batch_1_attestation_0"), s.attestation("batch_1_conflicting_attestation_0")

	_, err := s.k.SubmitEquivocation(s.ctx, s.reporter, first, first)
	require.ErrorIs(t, err, types.ErrEvidence)
	_, err = s.k.SubmitEquivocation(s.ctx, s.reporter, first, s.attestation("batch_1_conflicting_attestation_1"))
	require.ErrorIs(t, err, types.ErrEvidence)
	forged := append([]byte(nil), second...)
	forged[220] ^= 1
	_, err = s.k.SubmitEquivocation(s.ctx, s.reporter, first, forged)
	require.ErrorIs(t, err, types.ErrEvidence)

	record, err := s.k.SubmitEquivocation(s.ctx, s.reporter, first, second)
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(bond), record.Amount)
	require.Equal(t, sdk.NewInt(bond/10), record.ReporterReward)
	require.Equal(t, sdk.NewInt(bond/10), s.balance(s.reporter))
	require.Equal(t, supply.Sub(sdk.NewInt(bond-bond/10)), s.app.BankKeeper.GetSupply(s.ctx, "uhpx").Amount)
	guarantor, _ := s.k.GetGuarantor(s.ctx, s.ids[0])
	require.Equal(t, types.GuarantorEjected, guarantor.Status)
	require.True(t, guarantor.Bond.IsZero())
	s.invariant()

	for _, pair := range [][2][]byte{{first, second}, {second, first}} {
		_, err = s.k.SubmitEquivocation(s.ctx, s.reporter, pair[0], pair[1])
		require.ErrorIs(t, err, types.ErrAlreadySlashed)
	}
	require.Len(t, s.k.GetSlashRecords(s.ctx), 1)
	require.Equal(t, sdk.NewInt(bond/10), s.balance(s.reporter))
	s.invariant()

	// The ejected guarantor no longer counts towards any certificate.
	_, err = s.submit("batch_1_quorum")
	require.ErrorIs(t, err, types.ErrCertificate)
	_, err = s.k.IncreaseBond(s.ctx, s.operators[0], s.ids[0], sdk.NewInt(bond))
	require.ErrorIs(t, err, types.ErrBond)
}

func TestEquivocatorRemovedFromSubmittedQuorum(t *testing.T) {
	s := newSuite(t, func(p *types.Params) { p.ChallengeWindowSeconds = 50 })
	s.bondAll()
	checkpoint, err := s.submit("batch_1_pair")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointSubmitted, checkpoint.Status)
	_, err = s.k.SubmitEquivocation(s.ctx, s.reporter, s.attestation("batch_1_attestation_0"),
		s.attestation("batch_1_conflicting_attestation_0"))
	require.NoError(t, err)
	s.advance(60)
	_, err = s.k.Finalize(s.ctx, 1)
	require.ErrorIs(t, err, types.ErrNotFinalizable)
}

func TestUnbondingDelayAndSlashability(t *testing.T) {
	s := newSuite(t, nil)
	s.bondAll()
	half := sdk.NewInt(bond / 2)

	_, err := s.k.BeginUnbond(s.ctx, s.operators[1], s.ids[0], half)
	require.ErrorIs(t, err, types.ErrUnauthorized)
	_, err = s.k.BeginUnbond(s.ctx, s.operators[0], s.ids[0], sdk.NewInt(bond+1))
	require.ErrorIs(t, err, types.ErrBond)
	for index := range s.ids {
		entry, err := s.k.BeginUnbond(s.ctx, s.operators[index], s.ids[index], half)
		require.NoError(t, err)
		require.Equal(t, s.ctx.BlockTime().Unix()+int64(unbondingDelay), entry.CompletionTime)
	}
	s.invariant()
	before := s.balance(s.operators[0])
	_, err = s.k.CompleteUnbond(s.ctx, s.operators[0], s.ids[0])
	require.ErrorIs(t, err, types.ErrUnbondingImmature)
	s.advance(unbondingDelay - 1)
	_, err = s.k.CompleteUnbond(s.ctx, s.operators[0], s.ids[0])
	require.ErrorIs(t, err, types.ErrUnbondingImmature)
	require.Equal(t, before, s.balance(s.operators[0]))

	// Inside the delay the unbonding half is slashed with the bonded half.
	record, err := s.k.SubmitEquivocation(s.ctx, s.reporter, s.attestation("batch_1_attestation_0"),
		s.attestation("batch_1_conflicting_attestation_0"))
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(bond), record.Amount)
	s.invariant()

	s.advance(1)
	_, err = s.k.CompleteUnbond(s.ctx, s.operators[0], s.ids[0])
	require.ErrorIs(t, err, types.ErrBond)
	require.Equal(t, before, s.balance(s.operators[0]))

	// Past the delay the entry is the operator's again and is not slashed.
	record, err = s.k.SubmitEquivocation(s.ctx, s.reporter, s.attestation("batch_1_attestation_1"),
		s.attestation("batch_1_conflicting_attestation_1"))
	require.NoError(t, err)
	require.Equal(t, half, record.Amount)
	paid, err := s.k.CompleteUnbond(s.ctx, s.operators[1], s.ids[1])
	require.NoError(t, err)
	require.Equal(t, half, paid)
	require.Equal(t, sdk.NewInt(2*bond).Sub(sdk.NewInt(bond)).Add(half), s.balance(s.operators[1]))
	s.invariant()

	guarantor, _ := s.k.GetGuarantor(s.ctx, s.ids[2])
	require.Equal(t, half, guarantor.Bond)
	require.Empty(t, filter(s.k.GetUnbondings(s.ctx), s.ids[1]))
}

func filter(entries []types.UnbondingEntry, id [32]byte) []types.UnbondingEntry {
	var out []types.UnbondingEntry
	for _, entry := range entries {
		if entry.GuarantorID == types.Hash32(id) {
			out = append(out, entry)
		}
	}
	return out
}

func TestUnbondingBelowMinimumLeavesQuorum(t *testing.T) {
	s := newSuite(t, func(p *types.Params) { p.MinBond = sdk.NewInt(bond) })
	s.bondAll()
	_, err := s.k.BeginUnbond(s.ctx, s.operators[0], s.ids[0], sdk.NewInt(1))
	require.NoError(t, err)
	_, err = s.k.BeginUnbond(s.ctx, s.operators[1], s.ids[1], sdk.NewInt(1))
	require.NoError(t, err)
	_, err = s.submit("batch_1_quorum")
	require.ErrorIs(t, err, types.ErrCertificate)
}

func TestChallengesAreAuthorityArbitrated(t *testing.T) {
	s := newSuite(t, func(p *types.Params) { p.ChallengeWindowSeconds = 50 })
	s.bondAll()
	challenger := account(0xc1)
	s.fund(challenger, 3_000_000)
	evidence := [32]byte{1}

	_, err := s.k.OpenChallenge(s.ctx, challenger, 1, types.ChallengeFraud, evidence, sdk.NewInt(1_000_000))
	require.ErrorIs(t, err, types.ErrCheckpointUnknown)
	checkpoint, err := s.submit("batch_1_quorum")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointSubmitted, checkpoint.Status)
	_, err = s.k.OpenChallenge(s.ctx, challenger, 1, types.ChallengeFraud, evidence, sdk.NewInt(999_999))
	require.ErrorIs(t, err, types.ErrChallenge)
	_, err = s.k.OpenChallenge(s.ctx, challenger, 1, 2, evidence, sdk.NewInt(1_000_000))
	require.ErrorIs(t, err, types.ErrChallenge)

	rejected, err := s.k.OpenChallenge(s.ctx, challenger, 1, types.ChallengeDataAvailability, evidence, sdk.NewInt(1_000_000))
	require.NoError(t, err)
	require.Equal(t, sdk.NewInt(2_000_000), s.balance(challenger))
	s.invariant()
	s.advance(60)
	_, err = s.k.Finalize(s.ctx, 1)
	require.ErrorIs(t, err, types.ErrNotFinalizable)

	for _, stranger := range []sdk.AccAddress{challenger, s.reporter, s.operators[0]} {
		_, _, err = s.k.ResolveChallenge(s.ctx, stranger, rejected.ID, true)
		require.ErrorIs(t, err, types.ErrUnauthorized)
	}
	supply := s.app.BankKeeper.GetSupply(s.ctx, "uhpx").Amount
	resolved, slashed, err := s.k.ResolveChallenge(s.ctx, s.authority, rejected.ID, false)
	require.NoError(t, err)
	require.Empty(t, slashed)
	require.Equal(t, types.ChallengeRejected, resolved.Status)
	require.Equal(t, supply.Sub(sdk.NewInt(1_000_000)), s.app.BankKeeper.GetSupply(s.ctx, "uhpx").Amount)
	_, _, err = s.k.ResolveChallenge(s.ctx, s.authority, rejected.ID, false)
	require.ErrorIs(t, err, types.ErrChallenge)
	s.invariant()

	upheld, err := s.k.OpenChallenge(s.ctx, challenger, 1, types.ChallengeFraud, evidence, sdk.NewInt(1_000_000))
	require.NoError(t, err)
	_, slashed, err = s.k.ResolveChallenge(s.ctx, s.authority, upheld.ID, true)
	require.NoError(t, err)
	require.Len(t, slashed, 3)
	require.Equal(t, types.CheckpointUnknown, s.k.StatusOf(s.ctx, 1))
	require.Empty(t, s.k.GetAvailability(s.ctx))
	// Bond returned, plus a tenth of three full bonds.
	require.Equal(t, sdk.NewInt(2_000_000+3*bond/10), s.balance(challenger))
	for _, id := range s.ids {
		guarantor, _ := s.k.GetGuarantor(s.ctx, id)
		require.Equal(t, types.GuarantorEjected, guarantor.Status)
	}
	s.invariant()
}

func TestChallengeWindowDelaysFinality(t *testing.T) {
	s := newSuite(t, func(p *types.Params) { p.ChallengeWindowSeconds = 50 })
	s.bondAll()
	checkpoint, err := s.submit("batch_1_quorum")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointSubmitted, checkpoint.Status)
	s.advance(49)
	_, err = s.k.Finalize(s.ctx, 1)
	require.ErrorIs(t, err, types.ErrNotFinalizable)
	s.advance(1)
	final, err := s.k.Finalize(s.ctx, 1)
	require.NoError(t, err)
	require.Equal(t, types.CheckpointFinal, final.Status)
	_, err = s.k.Finalize(s.ctx, 1)
	require.ErrorIs(t, err, types.ErrNotFinalizable)
	_, err = s.k.Finalize(s.ctx, 2)
	require.ErrorIs(t, err, types.ErrCheckpointUnknown)
}

func TestGenesisRoundTrip(t *testing.T) {
	s := newSuite(t, func(p *types.Params) { p.ChallengeWindowSeconds = 50 })
	s.bondAll()
	challenger := account(0xc1)
	s.fund(challenger, 1_000_000)
	_, err := s.submit("batch_1_quorum")
	require.NoError(t, err)
	s.advance(50)
	_, err = s.k.Finalize(s.ctx, 1)
	require.NoError(t, err)
	_, err = s.submit("batch_2_quorum")
	require.NoError(t, err)
	_, err = s.k.OpenChallenge(s.ctx, challenger, 2, types.ChallengeFraud, [32]byte{9}, sdk.NewInt(1_000_000))
	require.NoError(t, err)
	_, err = s.k.BeginUnbond(s.ctx, s.operators[2], s.ids[2], sdk.NewInt(7))
	require.NoError(t, err)
	_, err = s.k.SubmitEquivocation(s.ctx, s.reporter, s.attestation("batch_1_attestation_1"),
		s.attestation("batch_1_conflicting_attestation_1"))
	require.NoError(t, err)
	s.invariant()

	exported := s.k.ExportGenesis(s.ctx)
	require.NoError(t, exported.Validate())
	require.True(t, exported.HasLatestFinalized)
	require.Len(t, exported.Checkpoints, 2)
	require.Len(t, exported.Guarantors, 3)
	require.Len(t, exported.Sequencers, 1)
	require.Len(t, exported.Unbondings, 1)
	require.Len(t, exported.Challenges, 1)
	require.Len(t, exported.SlashRecords, 1)
	require.Len(t, exported.Availability, 6)
	encoded, err := json.Marshal(exported)
	require.NoError(t, err)
	var decoded types.GenesisState
	require.NoError(t, json.Unmarshal(encoded, &decoded))

	fresh := newSuite(t, nil)
	require.Panics(t, func() { fresh.k.InitGenesis(fresh.ctx, decoded) })

	fresh = newSuite(t, nil)
	escrow := sdk.NewCoins(sdk.NewCoin("uhpx", s.k.Escrowed(s.ctx)))
	require.NoError(t, fresh.app.BankKeeper.MintCoins(fresh.ctx, "evm", escrow))
	require.NoError(t, fresh.app.BankKeeper.SendCoinsFromModuleToModule(fresh.ctx, "evm", types.ModuleName, escrow))
	fresh.k.InitGenesis(fresh.ctx, decoded)
	again, err := json.Marshal(fresh.k.ExportGenesis(fresh.ctx))
	require.NoError(t, err)
	require.JSONEq(t, string(encoded), string(again))
	fresh.invariant()

	state, ok := fresh.k.FinalizedStateRoot(fresh.ctx, 1)
	require.True(t, ok)
	expected, _ := s.k.FinalizedStateRoot(s.ctx, 1)
	require.Equal(t, expected, state)
	_, ok = fresh.k.FinalizedStateRoot(fresh.ctx, 2)
	require.False(t, ok)

	// A genesis anchor and guarantor set bootstrap a chain that starts later.
	anchored := newSuite(t, nil)
	genesis := types.DefaultGenesis()
	genesis.Params = s.k.GetParams(s.ctx)
	genesis.Sequencers = exported.Sequencers
	first, _ := s.k.GetCheckpoint(s.ctx, 1)
	genesis.Anchor = types.Anchor{Set: true, BatchNumber: 1, LastSequence: first.LastSequence, StateRoot: first.StateRoot}
	for index := range s.ids {
		genesis.Guarantors = append(genesis.Guarantors, types.Guarantor{ID: s.ids[index], Signer: s.signers[index],
			Operator: s.operators[index].String(), Bond: sdk.NewInt(bond), Status: types.GuarantorActive})
	}
	total := sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(3*bond)))
	require.NoError(t, anchored.app.BankKeeper.MintCoins(anchored.ctx, "evm", total))
	require.NoError(t, anchored.app.BankKeeper.SendCoinsFromModuleToModule(anchored.ctx, "evm", types.ModuleName, total))
	anchored.k.InitGenesis(anchored.ctx, *genesis)
	_, err = anchored.submit("batch_1_quorum")
	require.ErrorIs(t, err, types.ErrContinuity)
	second, err := anchored.submit("batch_2_quorum")
	require.NoError(t, err)
	require.Equal(t, types.CheckpointSubmitted, second.Status)
	anchored.invariant()
}

func TestDefaultGenesisIsValid(t *testing.T) {
	require.NoError(t, types.DefaultGenesis().Validate())
	bad := types.DefaultGenesis()
	bad.Params.Threshold = 0
	require.Error(t, bad.Validate())
	bad = types.DefaultGenesis()
	bad.HasLatestFinalized = true
	require.Error(t, bad.Validate())
}

func parameterRouter(t *testing.T, k keeper.Keeper) (*baseapp.MsgServiceRouter, *codec.ProtoCodec) {
	t.Helper()
	registry := cdctypes.NewInterfaceRegistry()
	sdk.RegisterInterfaces(registry)
	moduleimpl.AppModuleBasic{}.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(registry)
	moduleimpl.NewAppModule(k).RegisterServices(module.NewConfigurator(cdc, router, baseapp.NewGRPCQueryRouter()))
	return router, cdc
}
func parameterRoute(t *testing.T, router *baseapp.MsgServiceRouter, cdc *codec.ProtoCodec, ctx sdk.Context, msg *types.MsgUpdateParams) error {
	t.Helper()
	raw, err := cdc.MarshalInterface(msg)
	require.NoError(t, err)
	var decoded sdk.Msg
	require.NoError(t, cdc.UnmarshalInterface(raw, &decoded))
	require.Equal(t, "/paxprotocol.paxchain.layerxanchor.MsgUpdateParams", sdk.MsgTypeURL(decoded))
	raw2, err := cdc.MarshalInterface(decoded)
	require.NoError(t, err)
	require.Equal(t, raw, raw2)
	handler := router.Handler(decoded)
	require.NotNil(t, handler)
	_, err = handler(ctx, decoded)
	return err
}
func TestMsgUpdateParamsGovernanceRoute(t *testing.T) {
	s := newSuite(t, nil)
	k, ctx := s.k, s.ctx
	authority := s.authority.String()
	router, cdc := parameterRouter(t, k)
	params := k.GetParams(ctx)
	params.ReporterShare = sdk.NewDecWithPrec(2, 1)
	params.PermissionlessRegistration = !params.PermissionlessRegistration
	require.NoError(t, parameterRoute(t, router, cdc, ctx, &types.MsgUpdateParams{Authority: authority, Params: params}))
	require.Equal(t, params, k.GetParams(ctx))
	eventResult, eventErr := router.Handler(&types.MsgUpdateParams{})(ctx, &types.MsgUpdateParams{Authority: authority, Params: params})
	require.NoError(t, eventErr)
	events := eventResult.Events
	require.NotEmpty(t, events)
	require.Equal(t, types.EventParamsUpdated, events[len(events)-1].Type)
	response, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), &types.MsgUpdateParams{Authority: authority, Params: params})
	require.NoError(t, err)
	require.NotNil(t, response)
}
func TestMsgUpdateParamsCodecAndSigners(t *testing.T) {
	s := newSuite(t, nil)
	k, ctx := s.k, s.ctx
	authority := s.authority.String()
	_, cdc := parameterRouter(t, k)
	msg := &types.MsgUpdateParams{Authority: authority, Params: k.GetParams(ctx)}
	require.NoError(t, msg.ValidateBasic())
	signer, err := sdk.AccAddressFromBech32(authority)
	require.NoError(t, err)
	require.Equal(t, []sdk.AccAddress{signer}, msg.GetSigners())
	require.Equal(t, types.RouterKey, msg.Route())
	require.Equal(t, types.TypeMsgUpdateParams, msg.Type())
	require.Equal(t, sdk.MustSortJSON(types.ModuleCdc.MustMarshalJSON(msg)), msg.GetSignBytes())
	raw, err := proto.Marshal(msg)
	require.NoError(t, err)
	var decoded types.MsgUpdateParams
	require.NoError(t, proto.Unmarshal(raw, &decoded))
	require.Equal(t, *msg, decoded)
	raw, err = cdc.MarshalJSON(msg)
	require.NoError(t, err)
	require.NoError(t, cdc.UnmarshalJSON(raw, &decoded))
	require.Equal(t, *msg, decoded)
	amino := codec.NewLegacyAmino()
	sdk.RegisterLegacyAminoCodec(amino)
	moduleimpl.AppModuleBasic{}.RegisterLegacyAminoCodec(amino)
	raw, err = amino.MarshalJSON(msg)
	require.NoError(t, err)
	require.NoError(t, amino.UnmarshalJSON(raw, &decoded))
	require.Equal(t, *msg, decoded)
	invalid := *msg
	invalid.Params.SlashFractionAvailability = sdk.Dec{}
	require.Error(t, invalid.ValidateBasic())
	_, err = keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), &invalid)
	require.Error(t, err)
	require.Equal(t, msg.Params, k.GetParams(ctx))
	preserved := msg.Params
	require.Error(t, preserved.UnmarshalJSONPB(&jsonpb.Unmarshaler{}, []byte(`{"unknown_field":true}`)))
	require.Equal(t, msg.Params, preserved)
	raw, err = json.Marshal(msg.Params)
	require.NoError(t, err)
	require.Error(t, preserved.UnmarshalJSONPB(nil, append(raw, []byte(` {}`)...)))
	require.Equal(t, msg.Params, preserved)
	address := types.AnchorPrecompileAddress
	saved := address
	for _, length := range []int{0, 19, 21} {
		require.Error(t, address.Unmarshal(make([]byte, length)))
		require.Equal(t, saved, address)
	}
	_, err = address.MarshalTo(make([]byte, 19))
	require.Error(t, err)
	encoded, err := address.Marshal()
	require.NoError(t, err)
	require.Len(t, encoded, 20)
	var other types.Address20
	require.NoError(t, other.Unmarshal(encoded))
	require.Equal(t, address, other)
}
func TestMsgUpdateParamsRefusals(t *testing.T) {
	s := newSuite(t, nil)
	k, ctx := s.k, s.ctx
	authority := s.authority.String()
	router, cdc := parameterRouter(t, k)
	before := k.GetParams(ctx)
	outsider := sdk.AccAddress(bytes.Repeat([]byte{0xfe}, 20)).String()
	for _, caller := range []string{outsider, "invalid-bech32"} {
		msg := &types.MsgUpdateParams{Authority: caller, Params: before}
		require.Error(t, parameterRoute(t, router, cdc, ctx, msg))
		_, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), msg)
		require.Error(t, err)
		require.Equal(t, before, k.GetParams(ctx))
		if caller == "invalid-bech32" {
			require.Empty(t, msg.GetSigners())
			require.Error(t, msg.ValidateBasic())
		}
	}
	cases := []struct {
		name   string
		mutate func(*types.Params)
	}{
		{"authority", func(p *types.Params) { p.Authority = "invalid" }},
		{"denom", func(p *types.Params) { p.BondDenom = "!" }},
		{"threshold-zero", func(p *types.Params) { p.Threshold = 0 }},
		{"threshold-high", func(p *types.Params) { p.Threshold = 33 }},
		{"min-bond-nil", func(p *types.Params) { p.MinBond = sdk.Int{} }},
		{"min-bond-zero", func(p *types.Params) { p.MinBond = sdk.ZeroInt() }},
		{"challenge-bond-negative", func(p *types.Params) { p.ChallengeBond = sdk.NewInt(-1) }},
		{"attestation-delay", func(p *types.Params) { p.MaxAttestationDelayMs = 0 }},
		{"settlement", func(p *types.Params) { p.SettlementContract = types.Address20{} }},
		{"equivocation", func(p *types.Params) { p.SlashFractionEquivocation = sdk.NewDec(2) }},
		{"fraud", func(p *types.Params) { p.SlashFractionFraud = sdk.NewDec(-1) }},
		{"availability", func(p *types.Params) { p.SlashFractionAvailability = sdk.NewDec(-1) }},
		{"reporter", func(p *types.Params) { p.ReporterShare = sdk.NewDec(2) }},
		{"destination", func(p *types.Params) { p.SlashDestination = "invalid" }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			params := before
			test.mutate(&params)
			msg := &types.MsgUpdateParams{Authority: authority, Params: params}
			require.Error(t, msg.ValidateBasic())
			require.Error(t, parameterRoute(t, router, cdc, ctx, msg))
			_, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), msg)
			require.Error(t, err)
			require.Equal(t, before, k.GetParams(ctx))
		})
	}
	_, err := keeper.NewMsgServerImpl(k).UpdateParams(sdk.WrapSDKContext(ctx), nil)
	require.Error(t, err)
	require.Equal(t, before, k.GetParams(ctx))
}
