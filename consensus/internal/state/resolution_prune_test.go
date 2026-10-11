package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// resolutionPruneApplyUnpruned applies blocks up to height with no retain
// height requested.
func resolutionPruneApplyUnpruned(t *testing.T, c *pruneTestChain, height int64) {
	t.Helper()
	for c.state.LastBlockHeight < height {
		c.apply(t, 0)
	}
}

func TestResolutionPruneOffApplyBlock(t *testing.T) {
	c := newPruneTestChain(t)
	resolutionPruneApplyUnpruned(t, c, 199)
	pruner := c.blockExec.Pruner()

	// Block 200 makes height 10 prunable; ApplyBlock returns while the prune
	// it requested is still held.
	c.apply(t, 11)
	require.Equal(t, int64(200), c.state.LastBlockHeight)
	require.Equal(t, int64(11), c.awaitPrune(t))
	require.Equal(t, int64(1), c.blocks.Base())
	require.NotNil(t, c.blocks.LoadBlock(10))
	require.Equal(t, int64(0), pruner.RetainedBase())

	// Proposal construction does not wait for the held prune.
	proposal, err := c.blockExec.CreateProposalBlock(t.Context(), 201, c.state, c.lastCommit, c.state.Validators.Validators[0].Address)
	require.NoError(t, err)
	require.Equal(t, int64(201), proposal.Height)

	c.blocks.release <- struct{}{}
	require.Eventually(t, func() bool { return pruner.RetainedBase() == 11 }, 10*time.Second, time.Millisecond)
	require.NoError(t, pruner.Err())
	c.requirePruned(t, 11)
	c.blockExec.Stop()
	require.Empty(t, c.blocks.entered)
}

func TestResolutionPruneOrdered(t *testing.T) {
	c := newPruneTestChain(t)
	resolutionPruneApplyUnpruned(t, c, 12)
	pruner := c.blockExec.Pruner()

	// Height 10 becomes prunable, then height 11 while the first prune runs.
	c.apply(t, 11)
	require.Equal(t, int64(11), c.awaitPrune(t))
	c.apply(t, 12)
	require.Empty(t, c.blocks.entered, "a second prune started while the first was running")
	require.NotNil(t, c.blocks.LoadBlock(10))
	require.Equal(t, int64(0), pruner.RetainedBase())

	// Height 10 is pruned and the retained base advances to 11 before the
	// prune of height 11 starts.
	c.blocks.release <- struct{}{}
	require.Equal(t, int64(12), c.awaitPrune(t))
	require.Nil(t, c.blocks.LoadBlock(10))
	require.NotNil(t, c.blocks.LoadBlock(11))
	require.Equal(t, int64(11), c.blocks.Base())
	require.Equal(t, int64(11), pruner.RetainedBase())

	c.blocks.release <- struct{}{}
	require.Eventually(t, func() bool { return pruner.RetainedBase() == 12 }, 10*time.Second, time.Millisecond)
	require.NoError(t, pruner.Err())
	c.requirePruned(t, 12)
	c.blockExec.Stop()
	require.Empty(t, c.blocks.entered)
}

func TestResolutionPruneErrorIsolated(t *testing.T) {
	c := newPruneTestChain(t)
	resolutionPruneApplyUnpruned(t, c, 200)
	pruner := c.blockExec.Pruner()

	// Block 201 requests a retain height above the stored head; the block
	// store refuses the prune, and block application still succeeds.
	c.apply(t, 202)
	require.Equal(t, int64(201), c.state.LastBlockHeight)
	require.Equal(t, int64(202), c.awaitPrune(t))
	c.blocks.release <- struct{}{}
	require.Eventually(t, func() bool { return pruner.Err() != nil }, 10*time.Second, time.Millisecond)
	require.EqualError(t, pruner.Err(), "prune below height 202: failed to prune block store: height must be equal to or less than the latest height 201")
	require.Equal(t, int64(0), pruner.RetainedBase())
	require.Equal(t, int64(1), c.blocks.Base())
	require.NotNil(t, c.blocks.LoadBlock(1))

	// The pruner keeps serving later blocks after the failure.
	c.apply(t, 11)
	require.Equal(t, int64(202), c.state.LastBlockHeight)
	require.Equal(t, int64(11), c.awaitPrune(t))
	c.blocks.release <- struct{}{}
	require.Eventually(t, func() bool { return pruner.RetainedBase() == 11 }, 10*time.Second, time.Millisecond)
	require.NoError(t, pruner.Err())
	c.requirePruned(t, 11)
	c.blockExec.Stop()
	require.Empty(t, c.blocks.entered)
}
