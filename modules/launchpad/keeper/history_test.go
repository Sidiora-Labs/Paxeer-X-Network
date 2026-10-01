package keeper_test

import (
	"testing"

	"github.com/sidiora-labs/paxeer-network/modules/launchpad/types"
	tokenfactorykeeper "github.com/sidiora-labs/paxeer-network/modules/tokenfactory/keeper"
	tokenfactorytypes "github.com/sidiora-labs/paxeer-network/modules/tokenfactory/types"
	storetypes "github.com/sidiora-labs/paxeer-network/sdk/store/types"
	sdk "github.com/sidiora-labs/paxeer-network/sdk/types"
	authtypes "github.com/sidiora-labs/paxeer-network/sdk/x/auth/types"
	bankkeeper "github.com/sidiora-labs/paxeer-network/sdk/x/bank/keeper"
	banktypes "github.com/sidiora-labs/paxeer-network/sdk/x/bank/types"
	govtypes "github.com/sidiora-labs/paxeer-network/sdk/x/gov/types"
	testkeeper "github.com/sidiora-labs/paxeer-network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

// historyEnv is an airdrop market whose fees can open successive epochs.
type historyEnv struct {
	*env
	creator, trader sdk.AccAddress
	denom           string
}

func newHistoryEnv(t *testing.T) *historyEnv {
	e := newEnv(t)
	gov := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	params := types.DefaultParams()
	params.VirtualQuoteDefault = i(1_000_000_000)
	params.VirtualTokenDefault = i(1_000_000_000)
	require.NoError(t, e.k.UpdateParams(e.ctx, gov, params))
	h := &historyEnv{env: e, creator: e.account(100_000_000), trader: e.account(40_000_000_000)}
	h.denom = e.create(h.creator, types.FeeStrategyAirdrop).Denom
	e.at(mature)
	return h
}

// nextEpoch accrues fees with a real swap and triggers the next airdrop.
func (h *historyEnv) nextEpoch() uint64 {
	h.t.Helper()
	h.swap(h.trader, h.denom, true, 100_000_000)
	_, err := h.k.ExecuteAirdrop(h.ctx, h.creator, h.denom)
	require.NoError(h.t, err)
	return h.market(h.denom).AirdropEpoch
}

func (h *historyEnv) send(bank interface {
	SendCoins(sdk.Context, sdk.AccAddress, sdk.AccAddress, sdk.Coins) error
}, from, to sdk.AccAddress, amount int64) error {
	return bank.SendCoins(h.ctx, from, to, sdk.NewCoins(sdk.NewCoin(h.denom, i(amount))))
}

func (h *historyEnv) count(holder sdk.AccAddress) uint64 {
	h.t.Helper()
	n, err := h.k.HoldingCheckpointCount(h.ctx, h.denom, holder)
	require.NoError(h.t, err)
	return n
}

func (h *historyEnv) epochBalance(holder sdk.AccAddress, epoch uint64) sdk.Int {
	h.t.Helper()
	amount, err := h.k.EpochBalance(h.ctx, h.denom, holder, epoch)
	require.NoError(h.t, err)
	return amount
}

func TestAirdropHoldingHistory(t *testing.T) {
	app := testkeeper.EVMTestApp
	bank := app.BankKeeper
	giga := app.GigaBankKeeper

	t.Run("inert", func(t *testing.T) {
		h := newHistoryEnv(t)
		h.swap(h.trader, h.denom, true, 100_000_000)
		holder := h.account(0)
		require.NoError(t, h.send(bank, h.trader, holder, 1_000))
		require.NoError(t, h.send(giga, holder, h.trader, 500))
		require.Zero(t, h.count(h.trader))
		require.Zero(t, h.count(holder))
		_, err := h.k.EpochBalance(h.ctx, h.denom, holder, 1)
		require.ErrorIs(t, err, types.ErrUnsupportedHoldingEpoch)
	})

	t.Run("boundary", func(t *testing.T) {
		h := newHistoryEnv(t)
		a, b2, c, d := h.account(0), h.account(0), h.account(0), h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		require.NoError(t, h.send(bank, h.trader, a, 700_000))
		require.NoError(t, h.send(bank, h.trader, b2, 300_000))
		require.NoError(t, h.send(bank, h.trader, c, 50_000))
		epoch := h.nextEpoch()
		require.Equal(t, uint64(1), epoch)
		require.NoError(t, h.k.OpenHoldingHistory(h.ctx, h.denom, epoch))
		escrow := h.k.ModuleAddress()
		boundary := map[string]sdk.Int{}
		for _, holder := range []sdk.AccAddress{a, b2, c, d, escrow, h.trader} {
			boundary[holder.String()] = h.balance(holder, h.denom)
		}

		// A empties itself into fresh D through the regular bank.
		require.NoError(t, h.send(bank, a, d, 700_000))
		// B pays through the Giga bank, then through a multi-send.
		require.NoError(t, h.send(giga, b2, c, 100_000))
		coins := sdk.NewCoins(sdk.NewCoin(h.denom, i(50_000)))
		require.NoError(t, bank.InputOutputCoins(h.ctx, []banktypes.Input{banktypes.NewInput(b2, coins)},
			[]banktypes.Output{banktypes.NewOutput(d, coins)}))
		// The launchpad module mints and burns through tokenfactory, whose
		// bank copy was made before the hook was registered.
		tf := tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper)
		admin := escrow.String()
		_, err := tf.Mint(sdk.WrapSDKContext(h.ctx), tokenfactorytypes.NewMsgMint(admin, sdk.NewCoin(h.denom, i(5_000))))
		require.NoError(t, err)
		_, err = tf.Burn(sdk.WrapSDKContext(h.ctx), tokenfactorytypes.NewMsgBurn(admin, sdk.NewCoin(h.denom, i(8_000))))
		require.NoError(t, err)
		require.NoError(t, h.send(bank, escrow, c, 1_000))

		require.True(t, h.balance(a, h.denom).IsZero())
		for _, holder := range []sdk.AccAddress{a, b2, c, d, escrow, h.trader} {
			require.Equal(t, boundary[holder.String()], h.epochBalance(holder, 1), holder.String())
		}
		require.True(t, h.epochBalance(d, 1).IsZero())
		require.Equal(t, i(700_000), h.epochBalance(a, 1))
		require.Equal(t, uint64(1), h.count(a))
		require.Equal(t, uint64(1), h.count(b2))
		require.Equal(t, uint64(1), h.count(d))
		require.Equal(t, uint64(1), h.count(escrow))
		require.Zero(t, h.count(h.trader))
	})

	t.Run("multi-epoch", func(t *testing.T) {
		h := newHistoryEnv(t)
		x, y := h.account(0), h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		require.NoError(t, h.send(bank, h.trader, x, 10_000))
		require.NoError(t, h.k.OpenHoldingHistory(h.ctx, h.denom, h.nextEpoch()))
		require.NoError(t, h.send(bank, h.trader, x, 5_000))
		// Epochs 2 and 3 pass without any activity of x.
		require.NoError(t, h.k.OpenHoldingHistory(h.ctx, h.denom, h.nextEpoch()))
		require.NoError(t, h.k.OpenHoldingHistory(h.ctx, h.denom, h.nextEpoch()))
		require.Equal(t, uint64(1), h.count(x))
		for n := 0; n < 1_000; n++ {
			require.NoError(t, h.send(giga, x, y, 1))
		}
		require.Equal(t, uint64(2), h.count(x))
		require.Equal(t, uint64(1), h.count(y))
		forward := []sdk.Int{h.epochBalance(x, 1), h.epochBalance(x, 2), h.epochBalance(x, 3)}
		reverse := []sdk.Int{h.epochBalance(x, 3), h.epochBalance(x, 2), h.epochBalance(x, 1)}
		require.Equal(t, []sdk.Int{i(10_000), i(15_000), i(15_000)}, forward)
		require.Equal(t, []sdk.Int{forward[2], forward[1], forward[0]}, reverse)
		require.True(t, h.epochBalance(y, 1).IsZero())
		require.True(t, h.epochBalance(y, 3).IsZero())
		require.Equal(t, i(14_000), h.balance(x, h.denom))
	})

	t.Run("refusals", func(t *testing.T) {
		h := newHistoryEnv(t)
		x := h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		require.ErrorIs(t, h.k.OpenHoldingHistory(h.ctx, h.denom, 0), types.ErrHoldingHistory)
		require.ErrorIs(t, h.k.OpenHoldingHistory(h.ctx, "factory/unknown/lp9", 1), types.ErrUnknownMarket)
		require.NoError(t, h.k.OpenHoldingHistory(h.ctx, h.denom, h.nextEpoch()))
		require.ErrorIs(t, h.k.OpenHoldingHistory(h.ctx, h.denom, 1), types.ErrHoldingHistory)
		require.ErrorIs(t, h.k.OpenHoldingHistory(h.ctx, h.denom, 2), types.ErrHoldingHistory)
		h.nextEpoch()
		h.nextEpoch()
		require.ErrorIs(t, h.k.OpenHoldingHistory(h.ctx, h.denom, 3), types.ErrHoldingHistory)

		for _, epoch := range []uint64{0, 2, 3} {
			_, err := h.k.EpochBalance(h.ctx, h.denom, x, epoch)
			require.ErrorIs(t, err, types.ErrUnsupportedHoldingEpoch)
		}

		// A rejected transfer and a discarded cache branch record nothing.
		require.Error(t, h.send(bank, x, h.trader, 1))
		require.Zero(t, h.count(x))
		require.NoError(t, h.send(bank, h.trader, x, 100))
		before := h.count(h.trader)
		cacheCtx, _ := h.ctx.CacheContext()
		y := h.account(0)
		require.NoError(t, bank.SendCoins(cacheCtx, x, y, sdk.NewCoins(sdk.NewCoin(h.denom, i(100)))))
		n, err := h.k.HoldingCheckpointCount(cacheCtx, h.denom, y)
		require.NoError(t, err)
		require.Equal(t, uint64(1), n)
		require.Zero(t, h.count(y))
		require.Equal(t, before, h.count(h.trader))
		require.Equal(t, i(100), h.balance(x, h.denom))

		// A corrupted checkpoint is refused by the hook and the lookup, and
		// the bank write it guards does not happen.
		store := h.ctx.KVStore(sdk.NewKVStoreKey(types.StoreKey))
		store.Set(types.HoldingEntryKey(h.denom, x, 0), []byte{0x01})
		_, err = h.k.EpochBalance(h.ctx, h.denom, x, 1)
		require.ErrorIs(t, err, types.ErrHoldingHistory)
		require.ErrorIs(t, h.send(bank, h.trader, x, 1), types.ErrHoldingHistory)
		require.Equal(t, i(100), h.balance(x, h.denom))

		// Out-of-order entries are refused by the lookup.
		w := h.account(0)
		require.NoError(t, h.send(bank, h.trader, w, 10))
		entry := store.Get(types.HoldingEntryKey(h.denom, w, 0))
		store.Set(types.HoldingEntryKey(h.denom, w, 1), entry)
		store.Set(types.HoldingCountKey(h.denom, w), []byte{0, 0, 0, 0, 0, 0, 0, 2})
		_, err = h.k.EpochBalance(h.ctx, h.denom, w, 1)
		require.ErrorIs(t, err, types.ErrHoldingHistory)
	})

	t.Run("bounds", func(t *testing.T) {
		h := newHistoryEnv(t)
		x := h.account(0)
		h.swap(h.trader, h.denom, true, 1_000_000_000)
		const epochs = 16
		for epoch := uint64(1); epoch <= epochs; epoch++ {
			require.Equal(t, epoch, h.nextEpoch())
			require.NoError(t, h.k.OpenHoldingHistory(h.ctx, h.denom, epoch))
			require.NoError(t, h.send(bank, h.trader, x, int64(epoch)))
		}
		require.Equal(t, uint64(epochs), h.count(x))
		params := storetypes.KVGasConfig()
		limit := 66 * (params.ReadCostFlat + params.ReadCostPerByte*300)
		for epoch := uint64(1); epoch <= epochs; epoch++ {
			metered := h.ctx.WithGasMeter(sdk.NewGasMeter(limit, 1, 1))
			amount, err := h.k.EpochBalance(metered, h.denom, x, epoch)
			require.NoError(t, err)
			require.Equal(t, i(int64((epoch-1)*epoch/2)), amount)
			require.LessOrEqual(t, metered.GasMeter().GasConsumed(), limit)
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
