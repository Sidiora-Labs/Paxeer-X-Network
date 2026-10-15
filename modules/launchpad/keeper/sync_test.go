package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/stretchr/testify/require"
)

func TestSyncReserves(t *testing.T) {
	s := newSwapEnv(t, nil)
	pool := s.pool.Hex()

	// USDL sent straight to the pool is swept into custody and booked as the
	// real balance: effective quote 20000000000 over 10^15 tokens.
	s.transfer(s.holder, s.pool, 10_000_000_000)
	ctx, market, err := s.k.SyncReserves(s.ctx, s.caller, s.locks, s.denom)
	require.NoError(t, err)
	s.ctx = ctx
	require.Equal(t, "10000000000", market.RealQuoteBalance.String())
	require.Equal(t, "1000000000000000", market.TokenReserve.String())
	sameJSON(t, market, s.market(s.denom))
	require.Equal(t, "10000000000", s.quoteLedger(keeper.HeldQuoteLedger(s.pool)).String())
	require.Equal(t, int64(0), s.balanceOf(s.pool))
	require.Equal(t, int64(10_000_000_000), s.balanceOf(s.custody))
	price, err := market.Price()
	require.NoError(t, err)
	require.Equal(t, "20000000000000", price.String())

	// Tokens sent to the pool are booked as the token reserve; no USDL is
	// left to sweep, so the real balance stays.
	s.tokenTransfer(s.token, s.holder, s.pool, b(100_000_000_000))
	s.ctx, market, err = s.k.SyncReserves(s.ctx, s.caller, s.locks, s.denom)
	require.NoError(t, err)
	require.Equal(t, "10000000000", market.RealQuoteBalance.String())
	require.Equal(t, "1000100000000000", market.TokenReserve.String())
	sameJSON(t, market, s.market(s.denom))
	require.Equal(t, []map[string]string{
		{types.AttributeKeyDenom: s.denom, keeper.AttributeKeyPool: pool,
			types.AttributeKeyVirtualQuoteReserve: "10000000000", types.AttributeKeyRealQuoteBalance: "10000000000",
			types.AttributeKeyTokenReserve: "1000000000000000", types.AttributeKeyPrice: "20000000000000"},
		{types.AttributeKeyDenom: s.denom, keeper.AttributeKeyPool: pool,
			types.AttributeKeyVirtualQuoteReserve: "10000000000", types.AttributeKeyRealQuoteBalance: "10000000000",
			types.AttributeKeyTokenReserve: "1000100000000000", types.AttributeKeyPrice: "19998000199980"},
	}, eventsOf(s.ctx, types.EventTypePoolStateUpdated))

	// The pool lock is released on return and refuses a second entry while
	// held; syncReserves has no pause check.
	require.False(t, s.k.LockHeld(s.locks, keeper.PoolLock(s.pool)))
	release, err := s.k.EnterLock(s.locks, keeper.PoolLock(s.pool))
	require.NoError(t, err)
	_, _, err = s.k.SyncReserves(s.ctx, s.caller, s.locks, s.denom)
	require.ErrorIs(t, err, types.ErrReentrancyGuardReentrantCall)
	requireRevert(t, err, "0x3ee5aeb5")
	release()
	require.NoError(t, s.k.Pause(s.ctx, sdk.MustAccAddressFromBech32(market.Guardian), s.denom))
	_, market, err = s.k.SyncReserves(s.ctx, s.caller, s.locks, s.denom)
	require.NoError(t, err)
	require.True(t, market.Paused)

	_, _, err = s.k.SyncReserves(s.ctx, s.caller, s.locks, "factory/launchpad/none")
	require.ErrorIs(t, err, types.ErrUnknownMarket)
}
