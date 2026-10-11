package consensus

import (
	"context"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto"
	cstypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/consensus/types"
	tmpubsub "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/internal/pubsub"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils/require"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

// sequencerSourceRounds is the number of consecutive rounds each case drives,
// enough to see every host of the two-host pin lead twice.
const sequencerSourceRounds = 4

// sequencerSourcePin pins L = [remote, local] from the state's current height
// and returns both keys, so even rounds belong to the remote host and odd
// rounds to the local one.
func sequencerSourcePin(ctx context.Context, t *testing.T, cs *testState, vss []*validatorStub) (remote, local crypto.PubKey) {
	t.Helper()
	local, err := vss[0].GetPubKey(ctx)
	require.NoError(t, err)
	remote, err = vss[1].GetPubKey(ctx)
	require.NoError(t, err)
	pin, err := cstypes.NewLeaderPin(cs.roundState.Height(), []crypto.PubKey{remote, local})
	require.NoError(t, err)
	cs.SetLeaderPin(pin)
	return remote, local
}

// sequencerSourceAwaitProposeTimeout waits for the propose timeout of round,
// skipping the timeout a leader round may still emit after its own proposal.
func sequencerSourceAwaitProposeTimeout(t *testing.T, ch <-chan tmpubsub.Message, height int64, round int32) {
	t.Helper()
	for {
		ev, ok := ensureMessageBeforeTimeout(t, ch, ensureTimeout).Data().(types.EventDataRoundState)
		require.True(t, ok, "expected a EventDataRoundState")
		require.Equal(t, height, ev.Height)
		if ev.Round == round {
			return
		}
		require.Less(t, ev.Round, round)
	}
}

func TestSequencerSourceLeaderAlternation(t *testing.T) {
	ctx := t.Context()
	cfg := configSetup(t)

	cs1, vss := makeState(ctx, t, makeStateArgs{config: cfg})
	remote, local := sequencerSourcePin(ctx, t, cs1, vss)
	height := cs1.roundState.Height()

	newRoundCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryNewRound)
	proposalCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryCompleteProposal)
	timeoutProposeCh := subscribe(ctx, t, cs1.eventBus, types.EventQueryTimeoutPropose)

	cs1.startTestRound(ctx, height, 0)

	for round := int32(0); round < sequencerSourceRounds; round++ {
		ensureNewRound(t, newRoundCh, height, round)

		localLeads := round%2 == 1
		want := remote
		if localLeads {
			want = local
		}
		require.Equal(t, want, cs1.GetRoundState().Leader())
		require.Equal(t, localLeads, cs1.isProposer(local))
		require.Equal(t, !localLeads, cs1.isProposer(remote))

		if localLeads {
			ensureNewProposal(t, proposalCh, height, round)
			proposal := cs1.GetRoundState().Proposal
			require.NotNil(t, proposal)
			require.Equal(t, local.Address(), proposal.ProposerAddress)
			require.Equal(t, round, proposal.Round)
			require.NoError(t, local.Verify(types.ProposalSignBytes(cfg.ChainID(), proposal.ToProto()), proposal.Signature))
		} else {
			sequencerSourceAwaitProposeTimeout(t, timeoutProposeCh, height, round)
			require.Nil(t, cs1.GetRoundState().Proposal)
		}

		cs1.signAddVotes(ctx, t, tmproto.PrecommitType, cfg.ChainID(), types.BlockID{}, vss[1:]...)
		incrementRound(vss[1:]...)
	}
	ensureNewRound(t, newRoundCh, height, sequencerSourceRounds)
}

func TestSequencerSourceProposalFromOtherHostIgnored(t *testing.T) {
	ctx := t.Context()
	cfg := configSetup(t)

	cs1, vss := makeState(ctx, t, makeStateArgs{config: cfg})
	remote, local := sequencerSourcePin(ctx, t, cs1, vss)
	height := cs1.roundState.Height()

	for round := int32(0); round < sequencerSourceRounds; round++ {
		fromRemote, _ := cs1.decideProposal(ctx, t, vss[1], height, round)
		fromLocal, _ := cs1.decideProposal(ctx, t, vss[0], height, round)

		localLeads := round%2 == 1
		accepted, ignored := fromRemote, fromLocal
		leader, other := remote, local
		if localLeads {
			accepted, ignored = fromLocal, fromRemote
			leader, other = local, remote
		}

		cs1.mtx.Lock()
		cs1.updateRoundStep(round, cstypes.RoundStepPropose)
		cs1.roundState.SetProposal(nil)
		ignoredErr := cs1.defaultSetProposal(ignored, time.Now())
		ignoredLeft := cs1.roundState.Proposal()
		acceptedErr := cs1.defaultSetProposal(accepted, time.Now())
		acceptedSet := cs1.roundState.Proposal()
		cs1.mtx.Unlock()

		require.ErrorIs(t, ignoredErr, ErrInvalidProposer)
		require.Nil(t, ignoredLeft)
		require.NoError(t, acceptedErr)
		require.Equal(t, accepted, acceptedSet)
		require.Equal(t, leader.Address(), acceptedSet.ProposerAddress)
		require.NotEqual(t, other.Address(), acceptedSet.ProposerAddress)
	}
}
