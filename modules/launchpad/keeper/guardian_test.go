package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/stretchr/testify/require"
)

func TestTransferGuardian(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(0)
	bob := e.account(0)
	denom := e.create(creator, types.FeeStrategyClaim).Denom

	err := e.k.TransferGuardian(e.ctx, alice, denom, bob)
	require.ErrorIs(t, err, types.ErrNotGuardian)
	requireRevert(t, err, "0x82b42900")
	err = e.k.TransferGuardian(e.ctx, nil, denom, bob)
	require.ErrorIs(t, err, types.ErrNotGuardian)
	err = e.k.TransferGuardian(e.ctx, creator, denom, nil)
	require.ErrorIs(t, err, types.ErrZeroAddress)
	requireRevert(t, err, "0xd92e233d")
	require.ErrorIs(t, e.k.TransferGuardian(e.ctx, creator, "factory/launchpad/none", bob), types.ErrUnknownMarket)
	require.Equal(t, "", e.market(denom).PendingGuardian)
	require.Empty(t, eventsOf(e.ctx, types.EventTypeGuardianTransferStarted))

	require.NoError(t, e.k.TransferGuardian(e.ctx, creator, denom, alice))
	require.NoError(t, e.k.TransferGuardian(e.ctx, creator, denom, bob))
	market := e.market(denom)
	require.Equal(t, creator.String(), market.Guardian)
	require.Equal(t, bob.String(), market.PendingGuardian)
	require.Equal(t, []map[string]string{
		{types.AttributeKeyDenom: denom, types.AttributeKeyGuardian: creator.String(),
			types.AttributeKeyPendingGuardian: alice.String()},
		{types.AttributeKeyDenom: denom, types.AttributeKeyGuardian: creator.String(),
			types.AttributeKeyPendingGuardian: bob.String()},
	}, eventsOf(e.ctx, types.EventTypeGuardianTransferStarted))

	// The proposal grants nothing: the guardian still pauses, the pending
	// guardian does not.
	require.ErrorIs(t, e.k.Pause(e.ctx, bob, denom), types.ErrNotGuardian)
	require.NoError(t, e.k.Pause(e.ctx, creator, denom))
}

func TestAcceptGuardian(t *testing.T) {
	e := newEnv(t)
	creator := e.account(100_000_000)
	alice := e.account(0)
	bob := e.account(0)
	denom := e.create(creator, types.FeeStrategyClaim).Denom

	// No transfer pending: every caller is refused, the guardian included.
	err := e.k.AcceptGuardian(e.ctx, creator, denom)
	require.ErrorIs(t, err, types.ErrNotGuardian)
	requireRevert(t, err, "0x82b42900")
	require.ErrorIs(t, e.k.AcceptGuardian(e.ctx, nil, denom), types.ErrNotGuardian)

	require.NoError(t, e.k.TransferGuardian(e.ctx, creator, denom, bob))
	require.ErrorIs(t, e.k.AcceptGuardian(e.ctx, alice, denom), types.ErrNotGuardian)
	require.ErrorIs(t, e.k.AcceptGuardian(e.ctx, creator, denom), types.ErrNotGuardian)
	require.ErrorIs(t, e.k.AcceptGuardian(e.ctx, bob, "factory/launchpad/none"), types.ErrUnknownMarket)
	require.Empty(t, eventsOf(e.ctx, types.EventTypeGuardianTransferred))

	require.NoError(t, e.k.AcceptGuardian(e.ctx, bob, denom))
	market := e.market(denom)
	require.Equal(t, bob.String(), market.Guardian)
	require.Equal(t, "", market.PendingGuardian)
	require.NoError(t, market.Validate())
	require.Equal(t, []map[string]string{{types.AttributeKeyDenom: denom,
		types.AttributeKeyPreviousGuardian: creator.String(), types.AttributeKeyGuardian: bob.String()}},
		eventsOf(e.ctx, types.EventTypeGuardianTransferred))

	// The accepted guardian holds the role alone, and the cleared pending
	// guardian cannot accept twice.
	require.ErrorIs(t, e.k.AcceptGuardian(e.ctx, bob, denom), types.ErrNotGuardian)
	require.ErrorIs(t, e.k.Pause(e.ctx, creator, denom), types.ErrNotGuardian)
	require.ErrorIs(t, e.k.TransferGuardian(e.ctx, creator, denom, alice), types.ErrNotGuardian)
	require.NoError(t, e.k.Pause(e.ctx, bob, denom))
	require.NoError(t, e.k.TransferGuardian(e.ctx, bob, denom, creator))
	require.NoError(t, e.k.AcceptGuardian(e.ctx, creator, denom))
	require.Equal(t, creator.String(), e.market(denom).Guardian)
}
