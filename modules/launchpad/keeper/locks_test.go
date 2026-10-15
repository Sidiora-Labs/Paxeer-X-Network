package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/stretchr/testify/require"
)

func (e *env) stateDB() *state.DBImpl {
	return state.NewDBImpl(e.ctx, &testkeeper.EVMTestApp.EvmKeeper, false)
}

func requireRevert(t *testing.T, err error, selector string) {
	t.Helper()
	contractError, ok := types.ContractErrorOf(err)
	require.True(t, ok, err)
	revert, packErr := contractError.Revert()
	require.NoError(t, packErr)
	require.Equal(t, selector, hexutil.Encode(revert))
}

func TestPoolLockRefusesReentry(t *testing.T) {
	e := newEnv(t)
	db := e.stateDB()
	poolA := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	poolB := common.HexToAddress("0x00000000000000000000000000000000000a0002")

	release, err := e.k.EnterLock(db, keeper.PoolLock(poolA))
	require.NoError(t, err)
	require.True(t, e.k.LockHeld(db, keeper.PoolLock(poolA)))

	_, err = e.k.EnterLock(db, keeper.PoolLock(poolA))
	require.ErrorIs(t, err, types.ErrReentrancyGuardReentrantCall)
	requireRevert(t, err, "0x3ee5aeb5")
	require.True(t, e.k.LockHeld(db, keeper.PoolLock(poolA)))

	releaseB, err := e.k.EnterLock(db, keeper.PoolLock(poolB))
	require.NoError(t, err)
	releaseB()
	require.False(t, e.k.LockHeld(db, keeper.PoolLock(poolB)))

	release()
	require.False(t, e.k.LockHeld(db, keeper.PoolLock(poolA)))
	release, err = e.k.EnterLock(db, keeper.PoolLock(poolA))
	require.NoError(t, err)
	release()

	snapshot := db.Snapshot()
	_, err = e.k.EnterLock(db, keeper.PoolLock(poolA))
	require.NoError(t, err)
	require.True(t, e.k.LockHeld(db, keeper.PoolLock(poolA)))
	db.RevertToSnapshot(snapshot)
	require.False(t, e.k.LockHeld(db, keeper.PoolLock(poolA)))

	_, err = e.k.EnterLock(db, keeper.Lock{})
	require.ErrorIs(t, err, types.ErrInvalidConfiguration)
}

func TestFeeAccumulatorAndTreasuryLocks(t *testing.T) {
	e := newEnv(t)
	db := e.stateDB()
	pool := common.HexToAddress("0x00000000000000000000000000000000000a0001")
	roles := []struct {
		lock     keeper.Lock
		err      error
		selector string
	}{
		{keeper.FeeAccumulatorLock, types.ErrReentrancyGuardReentrantCall, "0x3ee5aeb5"},
		{keeper.TreasuryLock, types.ErrReentrantCall, "0x37ed32e8"},
		{keeper.RouterLock, types.ErrReentrancyGuardReentrantCall, "0x3ee5aeb5"},
		{keeper.FeesRouterLock, types.ErrReentrancyGuardReentrantCall, "0x3ee5aeb5"},
	}

	releasePool, err := e.k.EnterLock(db, keeper.PoolLock(pool))
	require.NoError(t, err)
	releases := make([]func(), 0, len(roles))
	for _, role := range roles {
		release, enterErr := e.k.EnterLock(db, role.lock)
		require.NoError(t, enterErr)
		releases = append(releases, release)
	}
	for _, role := range roles {
		require.True(t, e.k.LockHeld(db, role.lock))
		_, enterErr := e.k.EnterLock(db, role.lock)
		require.ErrorIs(t, enterErr, role.err)
		requireRevert(t, enterErr, role.selector)
	}

	releases[0]()
	require.False(t, e.k.LockHeld(db, keeper.FeeAccumulatorLock))
	for _, role := range roles[1:] {
		require.True(t, e.k.LockHeld(db, role.lock))
	}
	require.True(t, e.k.LockHeld(db, keeper.PoolLock(pool)))
	for _, release := range releases[1:] {
		release()
	}
	releasePool()
	for _, role := range roles {
		require.False(t, e.k.LockHeld(db, role.lock))
	}
	require.False(t, e.k.LockHeld(db, keeper.PoolLock(pool)))
}

func TestViewsAnswerUnderLock(t *testing.T) {
	e := newEnv(t)
	creator := e.account(250_000_000)
	market := e.create(creator, types.FeeStrategyClaim)
	pool := common.HexToAddress(market.Pointer)
	quoteIn := b(1_000_000)

	wantQuote, err := e.k.QuoteBuy(e.ctx, market.Denom, quoteIn)
	require.NoError(t, err)
	wantFee, err := e.k.FeeBps(e.ctx, market)
	require.NoError(t, err)

	db := e.stateDB()
	_, err = e.k.EnterLock(db, keeper.PoolLock(pool))
	require.NoError(t, err)
	_, err = e.k.EnterLock(db, keeper.FeeAccumulatorLock)
	require.NoError(t, err)

	gotQuote, err := e.k.QuoteBuy(db.Ctx(), market.Denom, quoteIn)
	require.NoError(t, err)
	require.Equal(t, wantQuote, gotQuote)
	gotMarket, found := e.k.GetMarket(db.Ctx(), market.Denom)
	require.True(t, found)
	sameJSON(t, market, gotMarket)
	gotFee, err := e.k.FeeBps(db.Ctx(), gotMarket)
	require.NoError(t, err)
	require.Equal(t, wantFee, gotFee)
	require.True(t, e.k.LockHeld(db, keeper.PoolLock(pool)))

	next := e.stateDB()
	require.False(t, e.k.LockHeld(next, keeper.PoolLock(pool)))
	require.False(t, e.k.LockHeld(next, keeper.FeeAccumulatorLock))
	release, err := e.k.EnterLock(next, keeper.PoolLock(pool))
	require.NoError(t, err)
	release()
}
