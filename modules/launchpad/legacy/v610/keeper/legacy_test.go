package keeperv610_test

import (
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/launchpadtest"
	keeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/keeper"
	types "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/legacy/v610/types"
	launchpadtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	tokenfactorykeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/tokenfactory/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/stretchr/testify/require"
)

// legacyKeepers carries the v6.10 keeper the way the application's
// precompile keepers expose it.
type legacyKeepers struct{ launchpad *keeper.Keeper }

func (k legacyKeepers) LaunchpadV610K() *keeper.Keeper { return k.launchpad }

var _ utils.LaunchpadV610Keepers = legacyKeepers{}

// blockResult is what one execution of the v6.10 block leaves behind: the
// gas of each call, its results, the launchpad store, the balances it moves
// and its events.
type blockResult struct {
	Gas          []uint64
	Created      types.Market
	Buy          keeper.SwapResult
	Claimed      sdk.Int
	Market       types.Market
	ProtocolFees sdk.Int
	Store        [][2][]byte
	Balances     []sdk.Coins
	Events       sdk.Events
}

// TestLegacyKeeperReplay records a v6.10 block of createMarket, buy and
// claimFees with one legacy keeper over the launchpad store key, replays it
// on the same pre-state with a second legacy keeper reached through the
// legacy accessor, and requires equal gas and state, both at the values the
// v6.10 code produced.
func TestLegacyKeeperReplay(t *testing.T) {
	require.Equal(t, launchpadtypes.StoreKey, types.StoreKey)
	app := testkeeper.EVMTestApp
	key := sdk.NewKVStoreKey(launchpadtypes.StoreKey)
	newLegacy := func() *keeper.Keeper {
		return keeper.NewKeeper(key, app.BankKeeper, tokenfactorykeeper.NewMsgServerImpl(app.TokenFactoryKeeper), &app.EvmKeeper)
	}
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = launchpadtest.WithStore(ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx)), key)
	genesisKeeper := newLegacy()
	genesisKeeper.InitGenesis(ctx, *types.DefaultGenesis())
	e := &env{t: t, ctx: ctx, k: genesisKeeper}
	creator := e.account(250_000_000)
	trader := e.account(2_000_000_000)

	run := func(k *keeper.Keeper) blockResult {
		t.Helper()
		block, _ := ctx.CacheContext()
		var out blockResult
		step := func(call func(sdk.Context) error) {
			t.Helper()
			stepCtx := block.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(block))
			require.NoError(t, call(stepCtx))
			out.Gas = append(out.Gas, stepCtx.GasMeter().GasConsumed())
		}
		step(func(c sdk.Context) (err error) {
			out.Created, err = k.CreateMarket(c, nil, creator, "Kindle Token", "KNDL", types.FeeStrategyClaim)
			return err
		})
		denom := out.Created.Denom
		step(func(c sdk.Context) (err error) {
			out.Buy, err = k.Swap(c, trader, denom, true, b(100_000_000), b(0), trader, farDeadline)
			return err
		})
		step(func(c sdk.Context) (err error) {
			out.Claimed, err = k.ClaimFees(c, creator, denom, creator)
			return err
		})
		var found bool
		out.Market, found = k.GetMarket(block, denom)
		require.True(t, found)
		var err error
		out.ProtocolFees, err = k.GetProtocolFees(block, denom)
		require.NoError(t, err)
		iterator := block.KVStore(key).Iterator(nil, nil)
		for ; iterator.Valid(); iterator.Next() {
			out.Store = append(out.Store, [2][]byte{iterator.Key(), iterator.Value()})
		}
		require.NoError(t, iterator.Close())
		for _, account := range []sdk.AccAddress{creator, trader, k.ModuleAddress(), k.TreasuryAddress()} {
			out.Balances = append(out.Balances, app.BankKeeper.GetAllBalances(block, account))
		}
		out.Events = block.EventManager().Events()
		return out
	}

	recorded := run(newLegacy())
	accessor := utils.LaunchpadV610Keepers(legacyKeepers{launchpad: newLegacy()})
	replayed := run(accessor.LaunchpadV610K())

	sameJSON(t, recorded, replayed)
	require.Len(t, recorded.Gas, 3)
	for _, gas := range recorded.Gas {
		require.Positive(t, gas)
	}
	require.NotEmpty(t, recorded.Store)
	require.NotEmpty(t, recorded.Events)

	denom := "factory/" + genesisKeeper.ModuleAddress().String() + "/lp1"
	require.Equal(t, denom, recorded.Created.Denom)
	require.Equal(t, uint64(1), recorded.Created.Index)
	require.Equal(t, b(300), recorded.Buy.FeeBps)
	require.Equal(t, b(3_000_000), recorded.Buy.FeeAmount)
	require.Equal(t, b(9_606_813_905_120), recorded.Buy.AmountOut)
	require.Equal(t, b(300_000), recorded.Buy.ProtocolCut)
	require.Equal(t, b(2_700_000), recorded.Buy.PoolCut)
	require.Equal(t, i(2_700_000), recorded.Claimed)
	require.Equal(t, i(97_000_000), recorded.Market.RealQuoteBalance)
	require.Equal(t, i(990_393_186_094_880), recorded.Market.TokenReserve)
	require.Equal(t, i(100_000_000), recorded.Market.CumulativeVolume)
	require.Equal(t, i(3_000_000), recorded.Market.AccumulatedQuoteFees)
	require.True(t, recorded.Market.AccumulatedFees.IsZero())
	require.Equal(t, i(300_000), recorded.ProtocolFees)
	sameJSON(t, []sdk.Coins{
		sdk.NewCoins(sdk.NewCoin(quote, i(152_700_000))),
		sdk.NewCoins(sdk.NewCoin(quote, i(1_900_000_000)), sdk.NewCoin(denom, i(9_606_813_905_120))),
		sdk.NewCoins(sdk.NewCoin(quote, i(97_000_000)), sdk.NewCoin(denom, i(990_393_186_094_880))),
		sdk.NewCoins(sdk.NewCoin(quote, i(100_300_000))),
	}, recorded.Balances)

	_, found := genesisKeeper.GetMarket(ctx, denom)
	require.False(t, found, "the recorded and replayed blocks run on branches of the pre-state")
}
