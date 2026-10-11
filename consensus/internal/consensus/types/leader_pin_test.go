package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
)

const leaderPinActivation = int64(5000)

func leaderPinKey(seed string) crypto.PubKey {
	return ed25519.TestSecretKey([]byte(seed)).Public()
}

// leaderPinValidators returns a four-validator set whose primary P and standby S
// are members, with unequal powers so the weighted rule differs from the pin.
func leaderPinValidators() (p, s crypto.PubKey, vals []*tmtypes.Validator) {
	p, s = leaderPinKey("leader-pin-primary"), leaderPinKey("leader-pin-standby")
	vals = []*tmtypes.Validator{
		tmtypes.NewValidator(p, 10),
		tmtypes.NewValidator(s, 20),
		tmtypes.NewValidator(leaderPinKey("leader-pin-other-1"), 30),
		tmtypes.NewValidator(leaderPinKey("leader-pin-other-2"), 40),
	}
	return p, s, vals
}

func mustLeaderPin(t *testing.T, h int64, leaders ...crypto.PubKey) LeaderPin {
	t.Helper()
	pin, err := NewLeaderPin(h, leaders)
	require.NoError(t, err)
	return pin
}

func weightedLeader(vs *tmtypes.ValidatorSet, h int64, r int32) crypto.PubKey {
	return (&RoundState{Validators: vs, HRS: HRS{Height: h, Round: r}}).Leader()
}

func TestLeaderPinRoundRobinFromActivation(t *testing.T) {
	p, s, vals := leaderPinValidators()
	vs := tmtypes.NewValidatorSet(vals)
	rs := RoundState{Validators: vs, LeaderPin: mustLeaderPin(t, leaderPinActivation, p, s)}

	// Below H_act the sha256(height, round) power-weighted rule is unchanged.
	for h := leaderPinActivation - 200; h < leaderPinActivation; h++ {
		for r := range int32(3) {
			rs.Height, rs.Round = h, r
			require.Equal(t, weightedLeader(vs, h, r), rs.Leader(), "height=%d round=%d", h, r)
		}
	}

	for _, tc := range []struct {
		height int64
		round  int32
		want   crypto.PubKey
	}{
		{leaderPinActivation, 0, p},
		{leaderPinActivation, 1, s},
		{leaderPinActivation, 2, p},
	} {
		rs.Height, rs.Round = tc.height, tc.round
		require.Equal(t, tc.want, rs.Leader(), "height=%d round=%d", tc.height, tc.round)
	}

	// The pin is not the weighted rule in disguise: over these heights the
	// weighted rule picks a validator outside L at round 0 at least once.
	differs := false
	for h := leaderPinActivation; h < leaderPinActivation+100; h++ {
		if w := weightedLeader(vs, h, 0); w != p {
			differs = true
			break
		}
	}
	require.True(t, differs)

	rs.Height, rs.Round = leaderPinActivation-1, 0
	key, ok := rs.LeaderPin.Leader(rs.Height, rs.Round)
	require.False(t, ok)
	require.Equal(t, crypto.PubKey{}, key)

	var unpinned LeaderPin
	key, ok = unpinned.Leader(leaderPinActivation, 0)
	require.False(t, ok)
	require.Equal(t, crypto.PubKey{}, key)
}

func TestLeaderPinRoundRobin(t *testing.T) {
	a, b, vals := leaderPinValidators()
	pin := mustLeaderPin(t, leaderPinActivation, a, b)
	rs := RoundState{Validators: tmtypes.NewValidatorSet(vals), LeaderPin: pin}

	rs.Height = leaderPinActivation
	for r, want := range []crypto.PubKey{a, b, a, b, a} {
		rs.Round = int32(r) //nolint:gosec
		require.Equal(t, want, rs.Leader(), "round=%d", r)
	}

	for _, h := range []int64{leaderPinActivation + 1, leaderPinActivation + 77, 1 << 40} {
		for r := range int32(64) {
			rs.Height, rs.Round = h, r
			want := a
			if r%2 == 1 {
				want = b
			}
			require.Equal(t, want, rs.Leader(), "height=%d round=%d", h, r)
		}
	}

	c := leaderPinKey("leader-pin-third")
	three := mustLeaderPin(t, leaderPinActivation, a, b, c)
	for r, want := range []crypto.PubKey{a, b, c, a, b, c, a} {
		got, ok := three.Leader(leaderPinActivation, int32(r)) //nolint:gosec
		require.True(t, ok)
		require.Equal(t, want, got, "round=%d", r)
	}
	got, ok := three.Leader(leaderPinActivation, 1<<31-1)
	require.True(t, ok)
	require.Equal(t, []crypto.PubKey{a, b, c}[(1<<31-1)%3], got)

	// The pin keeps its own copy of L.
	leaders := []crypto.PubKey{a, b}
	owned := mustLeaderPin(t, leaderPinActivation, leaders...)
	leaders[0] = c
	got, ok = owned.Leader(leaderPinActivation, 0)
	require.True(t, ok)
	require.Equal(t, a, got)
}

func TestLeaderPinRefusesInvalidList(t *testing.T) {
	a, b := leaderPinKey("leader-pin-primary"), leaderPinKey("leader-pin-standby")
	for _, tc := range []struct {
		name    string
		height  int64
		leaders []crypto.PubKey
		err     string
	}{
		{"zero activation height", 0, []crypto.PubKey{a, b}, "leader pin: activation height 0 must be at least 1"},
		{"negative activation height", -1, []crypto.PubKey{a, b}, "leader pin: activation height -1 must be at least 1"},
		{"nil list", leaderPinActivation, nil, "leader pin: leader list is empty"},
		{"empty list", leaderPinActivation, []crypto.PubKey{}, "leader pin: leader list is empty"},
		{"zero key", leaderPinActivation, []crypto.PubKey{a, {}}, "leader pin: leader 1 is the zero key"},
		{"duplicate key", leaderPinActivation, []crypto.PubKey{a, b, a}, "leader pin: duplicate leader " + a.String() + " at index 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pin, err := NewLeaderPin(tc.height, tc.leaders)
			require.EqualError(t, err, tc.err)
			_, ok := pin.Leader(leaderPinActivation, 0)
			require.False(t, ok)
		})
	}
}

func TestLeaderPinSafeRoundStateAgrees(t *testing.T) {
	p, s, vals := leaderPinValidators()
	vs := tmtypes.NewValidatorSet(vals)

	srs := NewSafeRoundState()
	srs.SetValidators(vs)
	srs.SetHeight(leaderPinActivation)
	srs.SetRound(1)
	require.Equal(t, weightedLeader(vs, leaderPinActivation, 1), srs.Leader())

	srs.SetLeaderPin(mustLeaderPin(t, leaderPinActivation, p, s))

	// enterPropose reads SafeRoundState.Leader; the proposal path reads the
	// copied RoundState and the round events; all of them see S.
	require.Equal(t, s, srs.Leader())
	require.Equal(t, s, srs.CopyInternal().Leader())
	require.Equal(t, s, srs.GetInternalPointer().Leader())

	sIdx, _, ok := vs.GetByAddress(s.Address())
	require.True(t, ok)
	ev := srs.NewRoundEvent()
	require.Equal(t, s.Address(), ev.Proposer.Address)
	require.Equal(t, sIdx, ev.Proposer.Index)

	srs.SetRound(2)
	require.Equal(t, p, srs.Leader())
	require.Equal(t, p, srs.CopyInternal().Leader())
	pIdx, _, ok := vs.GetByAddress(p.Address())
	require.True(t, ok)
	ev = srs.NewRoundEvent()
	require.Equal(t, p.Address(), ev.Proposer.Address)
	require.Equal(t, pIdx, ev.Proposer.Index)

	// Clearing the pin returns the weighted rule.
	srs.SetLeaderPin(LeaderPin{})
	require.Equal(t, weightedLeader(vs, leaderPinActivation, 2), srs.Leader())
}

func TestLeaderPinIndependentOfRestartOrder(t *testing.T) {
	p, s, vals := leaderPinValidators()
	const nodes = 4

	// Every node builds its validator set from a different insertion order and
	// carries different proposer priorities, as nodes restarted at different
	// heights do; restartAt[i] is the height from which node i runs with the pin.
	restartAt := [nodes]int64{4990, 4900, 4960, 4930}
	states := make([]*RoundState, nodes)
	for i := range states {
		order := make([]*tmtypes.Validator, len(vals))
		for j := range vals {
			order[j] = vals[(i+j)%len(vals)].Copy()
		}
		vs := tmtypes.NewValidatorSet(order)
		vs.IncrementProposerPriority(int32(i*7 + 1)) //nolint:gosec
		states[i] = &RoundState{Validators: vs}
	}

	for h := int64(4880); h <= 5010; h++ {
		for i, rs := range states {
			if h == restartAt[i] {
				rs.LeaderPin = mustLeaderPin(t, leaderPinActivation, p, s)
			}
		}
		for r := range int32(3) {
			var want crypto.PubKey
			for i, rs := range states {
				rs.Height, rs.Round = h, r
				got := rs.Leader()
				if i == 0 {
					want = got
					continue
				}
				require.Equal(t, want, got, "node=%d height=%d round=%d", i, h, r)
			}
			if h >= leaderPinActivation {
				require.Equal(t, []crypto.PubKey{p, s}[r%2], want, "height=%d round=%d", h, r)
			}
		}
	}
}
