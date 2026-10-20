package keeper_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	bankkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/keeper"
	govtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/gov/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

// historyEnv is an airdrop market whose fees open successive epochs through
// FeeAccumulator.triggerAirdrop, sent by the FeesRouter that holds
// FEES_ROUTER_ROLE.
type historyEnv struct {
	*env
	creator, trader sdk.AccAddress
	denom           string
}

func newHistoryEnv(t *testing.T) *historyEnv {
	e := newEnv(t)
	gov := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	params := e.k.GetParams(e.ctx)
	params.VirtualQuoteDefault = i(1_000_000_000)
	params.VirtualTokenDefault = i(1_000_000_000)
	require.NoError(t, e.k.UpdateParams(e.ctx, gov, params))
	roles := e.k.Generation2()
	granted, err := e.k.GovernanceGrant(e.ctx, e.k.Authority(), roles.FeeAccumulator, keeper.FeesRouterRole, roles.FeesRouter)
	require.NoError(t, err)
	require.True(t, granted)
	h := &historyEnv{env: e, creator: e.account(100_000_000), trader: e.account(40_000_000_000)}
	h.denom = e.create(h.creator, types.FeeStrategyAirdrop).Denom
	e.at(mature)
	return h
}

// next moves to the next block, so the pool-token checkpoints written so far
// lie before it.
func (h *historyEnv) next() { h.ctx = h.ctx.WithBlockHeight(h.ctx.BlockHeight() + 1) }

// trigger opens the next epoch through triggerAirdrop in a new block: the
// epoch snapshots the block before it, whose balances are the current ones.
func (h *historyEnv) trigger() types.PoolAirdropEpoch {
	h.t.Helper()
	h.next()
	token, pool := h.token(h.denom), h.pool(h.denom)
	eligible := h.tokenSupply(token).Sub(h.tokenBalance(token, pool)).
		Sub(h.tokenBalance(token, common.HexToAddress(types.DeadAddress)))
	before := h.market(h.denom)
	amount, err := h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), h.k.Generation2().FeesRouter, pool)
	require.NoError(h.t, err)
	require.Equal(h.t, before.AccumulatedFees, amount)
	after := h.market(h.denom)
	require.Equal(h.t, before.AirdropEpoch+1, after.AirdropEpoch)
	require.True(h.t, after.AccumulatedFees.IsZero())
	require.Equal(h.t, before.AirdropBalance.Add(amount), after.AirdropBalance)
	record := h.epochRecord(after.AirdropEpoch)
	sameJSON(h.t, types.PoolAirdropEpoch{Pool: pool.Hex(), Epoch: after.AirdropEpoch, Amount: amount,
		SnapshotBlock: uint64(h.ctx.BlockHeight() - 1), EligibleSupply: eligible, Remaining: amount,
		ClaimedSupply: i(0), LegacyRemaining: i(0)}, record)
	return record
}

// nextEpoch earns fees with a buy and opens the next epoch on them.
func (h *historyEnv) nextEpoch() types.PoolAirdropEpoch {
	h.t.Helper()
	h.swap(h.trader, h.denom, true, 100_000_000)
	return h.trigger()
}

func (h *historyEnv) epochRecord(epoch uint64) types.PoolAirdropEpoch {
	h.t.Helper()
	record, found, err := h.k.GetPoolAirdropEpoch(h.ctx, h.pool(h.denom), epoch)
	require.NoError(h.t, err)
	require.True(h.t, found)
	return record
}

// transfer moves amount of the market token from one holder to another.
func (h *historyEnv) transfer(from, to sdk.AccAddress, amount int64) {
	h.t.Helper()
	h.sendToken(h.token(h.denom), h.evmAddress(from), h.evmAddress(to), b(amount))
}

// pastBalance is the pool-token checkpoint of holder at the epoch's snapshot.
func (h *historyEnv) pastBalance(holder common.Address, record types.PoolAirdropEpoch) sdk.Int {
	h.t.Helper()
	return h.viewInt(h.token(h.denom), "getPastBalance(address,uint256)", addressWord(holder),
		word(b(int64(record.SnapshotBlock))))
}

// claim is claimAirdropEpoch on a branch written only when it succeeds, as
// the reverting precompile call leaves the state.
func (h *historyEnv) claim(holder common.Address, epoch uint64) (sdk.Int, error) {
	h.t.Helper()
	ctx, write := h.ctx.CacheContext()
	_, amount, err := h.k.ClaimAirdropEpoch(ctx, h.k.ModuleCaller(), h.pool(h.denom), holder, epoch)
	if err != nil {
		return sdk.Int{}, err
	}
	write()
	return amount, nil
}

// share is the entitlement of balance in an epoch: amount*balance/eligible.
func share(record types.PoolAirdropEpoch, balance sdk.Int) sdk.Int {
	return record.Amount.Mul(balance).Quo(record.EligibleSupply)
}

func TestAirdropHoldingHistory(t *testing.T) {
	app := testkeeper.EVMTestApp
	bank := app.BankKeeper
	giga := app.GigaBankKeeper

	t.Run("inert", func(t *testing.T) {
		h := newHistoryEnv(t)
		h.swap(h.trader, h.denom, true, 100_000_000)
		holder := h.account(0)
		h.transfer(h.trader, holder, 1_000)
		h.transfer(holder, h.trader, 500)
		h.next()
		require.Zero(t, h.market(h.denom).AirdropEpoch)
		_, found, err := h.k.GetPoolAirdropEpoch(h.ctx, h.pool(h.denom), 1)
		require.NoError(t, err)
		require.False(t, found)
		_, err = h.claim(h.evmAddress(holder), 1)
		require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		require.Equal(t, i(500), h.balance(holder, h.denom))
	})

	t.Run("boundary", func(t *testing.T) {
		h := newHistoryEnv(t)
		a, b2, c, d := h.account(0), h.account(0), h.account(0), h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, a, 700_000)
		h.transfer(h.trader, b2, 300_000)
		h.transfer(h.trader, c, 50_000)
		record := h.trigger()
		require.Equal(t, uint64(1), record.Epoch)
		holders := []sdk.AccAddress{a, b2, c, d, h.trader}
		boundary := map[string]sdk.Int{}
		eligible := sdk.ZeroInt()
		for _, holder := range holders {
			boundary[holder.String()] = h.balance(holder, h.denom)
			eligible = eligible.Add(boundary[holder.String()])
		}
		require.Equal(t, eligible, record.EligibleSupply)

		// A empties itself into fresh D, B pays C, and the trader sells back
		// into the pool after the boundary.
		h.transfer(a, d, 700_000)
		h.transfer(b2, c, 100_000)
		h.swap(h.trader, h.denom, false, 50_000)

		require.True(t, h.balance(a, h.denom).IsZero())
		for _, holder := range holders {
			require.Equal(t, boundary[holder.String()], h.pastBalance(h.evmAddress(holder), record), holder.String())
		}
		require.True(t, h.pastBalance(h.evmAddress(d), record).IsZero())
		require.Equal(t, i(700_000), h.pastBalance(h.evmAddress(a), record))

		_, err := h.claim(h.evmAddress(d), 1)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		for _, holder := range []sdk.AccAddress{a, b2, c} {
			before := h.usdlOf(holder)
			amount, err := h.claim(h.evmAddress(holder), 1)
			require.NoError(t, err)
			require.Equal(t, share(record, boundary[holder.String()]), amount)
			require.Equal(t, before.Add(amount), h.usdlOf(holder))
		}
		require.Equal(t, i(700_000).Add(i(300_000)).Add(i(50_000)), h.epochRecord(1).ClaimedSupply)
	})

	t.Run("multi-epoch", func(t *testing.T) {
		h := newHistoryEnv(t)
		x, y := h.account(0), h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		h.transfer(h.trader, x, 10_000)
		first := h.nextEpoch()
		h.transfer(h.trader, x, 5_000)
		// Epochs 2 and 3 pass without any activity of x.
		second := h.nextEpoch()
		third := h.nextEpoch()
		require.Equal(t, []uint64{1, 2, 3}, []uint64{first.Epoch, second.Epoch, third.Epoch})
		for n := 0; n < 1_000; n++ {
			h.transfer(x, y, 1)
		}
		records := []types.PoolAirdropEpoch{first, second, third}
		forward := []sdk.Int{}
		for _, record := range records {
			forward = append(forward, h.pastBalance(h.evmAddress(x), record))
		}
		reverse := []sdk.Int{}
		for index := len(records) - 1; index >= 0; index-- {
			reverse = append(reverse, h.pastBalance(h.evmAddress(x), records[index]))
		}
		require.Equal(t, []sdk.Int{i(10_000), i(15_000), i(15_000)}, forward)
		require.Equal(t, []sdk.Int{forward[2], forward[1], forward[0]}, reverse)
		require.True(t, h.pastBalance(h.evmAddress(y), first).IsZero())
		require.True(t, h.pastBalance(h.evmAddress(y), third).IsZero())
		require.Equal(t, i(14_000), h.balance(x, h.denom))
		require.Equal(t, i(1_000), h.balance(y, h.denom))

		for index := len(records) - 1; index >= 0; index-- {
			amount, err := h.claim(h.evmAddress(x), records[index].Epoch)
			require.NoError(t, err)
			require.Equal(t, share(records[index], forward[index]), amount)
		}
		_, err := h.claim(h.evmAddress(y), third.Epoch)
		require.ErrorIs(t, err, types.ErrZeroAmount)
	})

	t.Run("refusals", func(t *testing.T) {
		h := newHistoryEnv(t)
		x := h.account(0)
		roles := h.k.Generation2()
		pool := h.pool(h.denom)
		dead := common.HexToAddress(types.DeadAddress)
		_, err := h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), roles.FeesRouter, pool)
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		_, err = h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), h.evmAddress(h.creator), pool)
		require.ErrorIs(t, err, types.ErrMissingRole)
		_, err = h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), roles.FeesRouter, address("unregistered pool"))
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
		// The block that earned the fees is not before the snapshot of an
		// epoch opened in that same block, so nothing is eligible yet.
		before := h.market(h.denom)
		_, err = h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), roles.FeesRouter, pool)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		sameJSON(t, before, h.market(h.denom))

		record := h.trigger()
		_, err = h.k.TriggerAirdrop(h.ctx, h.k.ModuleCaller(), roles.FeesRouter, pool)
		require.ErrorIs(t, err, types.ErrNoFeesAccumulated)
		for _, epoch := range []uint64{0, 2} {
			_, err = h.claim(h.evmAddress(h.trader), epoch)
			require.ErrorIs(t, err, types.ErrAirdropNotTriggered)
		}
		for _, refused := range []common.Address{pool, dead, {}} {
			_, err = h.claim(refused, 1)
			require.ErrorIs(t, err, types.ErrUnauthorized)
		}

		// A rejected transfer and a discarded cache branch record nothing.
		token := h.token(h.denom)
		_, err = app.EvmKeeper.CallEVM(h.ctx, h.evmAddress(x), &token, nil,
			calldata("transfer(address,uint256)", addressWord(h.evmAddress(h.trader)), word(b(1))))
		require.Error(t, err)
		require.True(t, h.balance(x, h.denom).IsZero())
		cacheCtx, _ := h.ctx.CacheContext()
		_, amount, err := h.k.ClaimAirdropEpoch(cacheCtx, h.k.ModuleCaller(), pool, h.evmAddress(h.trader), 1)
		require.NoError(t, err)
		require.Equal(t, record.Amount, amount)
		require.True(t, h.k.HasClaimedPoolAirdrop(cacheCtx, pool, h.evmAddress(h.trader), 1))
		require.False(t, h.k.HasClaimedPoolAirdrop(h.ctx, pool, h.evmAddress(h.trader), 1))
		sameJSON(t, record, h.epochRecord(1))

		// A transfer after the boundary leaves the entitlement where it was.
		h.transfer(h.trader, x, 100)
		_, err = h.claim(h.evmAddress(x), 1)
		require.ErrorIs(t, err, types.ErrZeroAmount)
		amount, err = h.claim(h.evmAddress(h.trader), 1)
		require.NoError(t, err)
		require.Equal(t, record.Amount, amount)
		_, err = h.claim(h.evmAddress(h.trader), 1)
		require.ErrorIs(t, err, types.ErrAlreadyClaimed)
		require.Equal(t, i(100), h.balance(x, h.denom))
	})

	t.Run("bounds", func(t *testing.T) {
		h := newHistoryEnv(t)
		x := h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		const epochs = 16
		records := make([]types.PoolAirdropEpoch, 0, epochs)
		for epoch := uint64(1); epoch <= epochs; epoch++ {
			record := h.nextEpoch()
			require.Equal(t, epoch, record.Epoch)
			records = append(records, record)
			h.transfer(h.trader, x, int64(epoch))
		}
		for _, record := range records {
			want := i(int64((record.Epoch - 1) * record.Epoch / 2))
			require.Equal(t, want.String(), h.pastBalance(h.evmAddress(x), record).String())
			amount, err := h.claim(h.evmAddress(x), record.Epoch)
			if want.IsZero() {
				require.ErrorIs(t, err, types.ErrZeroAmount)
				continue
			}
			require.NoError(t, err)
			require.Equal(t, share(record, want), amount)
		}
	})

	t.Run("registration", func(t *testing.T) {
		hook := func(sdk.Context, sdk.AccAddress, string, func() sdk.Int) error { return nil }
		regular := bank.(bankkeeper.BaseKeeper)
		require.Panics(t, func() { regular.RegisterBalanceChangeHook(types.ModuleName, hook) })
		require.Panics(t, func() { regular.RegisterBalanceChangeHook("", hook) })
		require.Panics(t, func() { regular.RegisterBalanceChangeHook("other", nil) })
		require.Panics(t, func() { giga.RegisterBalanceChangeHook(types.ModuleName, hook) })
		require.Panics(t, func() { giga.RegisterBalanceChangeHook("", hook) })
		require.Panics(t, func() { giga.RegisterBalanceChangeHook("other", nil) })
	})
}
