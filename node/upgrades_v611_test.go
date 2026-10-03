package app

import (
	"testing"

	layerxcustodytypes "github.com/sidiora-labs/paxeer-network/modules/layerxcustody/types"
	"github.com/stretchr/testify/require"
)

func emergencyProposal(t *testing.T) *layerxcustodytypes.CustodyProposal {
	t.Helper()
	proposal, err := layerxcustodytypes.NewCustodyProposal("Emergency", "Switch custody to emergency",
		&layerxcustodytypes.MsgSetEmergency{Authority: layerxcustodytypes.GovernanceAuthority(), Enabled: true})
	require.NoError(t, err)
	return proposal
}

func TestCustodyGovernanceV611ActivationBoundaries(t *testing.T) {
	require.Equal(t, layerxcustodytypes.GovernanceActivationUpgrade, V611Upgrade)
	g := newCustodyGovernance(t)
	h := g.ctx.BlockHeight()

	before, _ := g.ctx.CacheContext()
	require.ErrorIs(t, g.route()(before, emergencyProposal(t)), layerxcustodytypes.ErrGovernanceNotActive)
	require.False(t, g.k.GetEmergency(before))

	g.activate(t)
	require.Equal(t, h, g.app.UpgradeKeeper.GetDoneHeight(g.ctx, V611Upgrade))

	below, _ := g.ctx.WithBlockHeight(h - 1).CacheContext()
	require.ErrorIs(t, g.route()(below, emergencyProposal(t)), layerxcustodytypes.ErrGovernanceNotActive)
	require.False(t, g.k.GetEmergency(below))

	for _, height := range []int64{h, h + 1} {
		at, _ := g.ctx.WithBlockHeight(height).CacheContext()
		require.NoError(t, g.k.GovernanceExecutionActive(at))
		require.NoError(t, g.route()(at, emergencyProposal(t)), "height %d", height)
		require.True(t, g.k.GetEmergency(at), "height %d", height)
	}
	require.False(t, g.k.GetEmergency(g.ctx), "each height ran on its own branch")
}

func TestCustodyGovernanceHistoricalContextRefusal(t *testing.T) {
	g := newCustodyGovernance(t)
	g.activate(t)
	h := g.ctx.BlockHeight()
	state := g.k.ExportGenesis(g.ctx)

	historical := g.ctx.WithBlockHeight(h - 5)
	events := len(historical.EventManager().Events())
	require.ErrorIs(t, g.k.GovernanceExecutionActive(historical), layerxcustodytypes.ErrGovernanceNotActive)
	require.ErrorIs(t, g.route()(historical, g.allMessages(t)), layerxcustodytypes.ErrGovernanceNotActive)
	g.requireUnchanged(t, historical, state, events)
	require.Equal(t, state, g.k.ExportGenesis(g.ctx))
}
