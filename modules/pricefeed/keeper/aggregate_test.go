package keeper_test

import (
	"testing"
	"time"

	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const aggregateNowMs int64 = 1_700_000_010_000

type aggregateEnv struct {
	app       *app.App
	k         keeper.Keeper
	ctx       sdk.Context
	operators []sdk.ValAddress
}

// newAggregateEnv builds the keeper on the test application with exactly
// bonded bonded validators, the block at height and aggregateNowMs, and the
// given params stored.
func newAggregateEnv(t *testing.T, bonded int, height int64, params types.Params) aggregateEnv {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(height).
		WithBlockTime(time.UnixMilli(aggregateNowMs).UTC())
	k, ctx := pricefeedtestutil.NewKeeper(testApp, ctx)

	existing := len(testApp.StakingKeeper.GetBondedValidatorsByPower(ctx))
	require.LessOrEqual(t, existing, bonded)
	for i := existing; i < bonded; i++ {
		createValidator(t, testApp, ctx, byte(i+1))
	}
	_, err := testApp.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
	require.NoError(t, err)
	var operators []sdk.ValAddress
	for _, validator := range testApp.StakingKeeper.GetBondedValidatorsByPower(ctx) {
		operators = append(operators, validator.GetOperator())
	}
	require.Len(t, operators, bonded)

	k.Set(ctx, types.ParamsKey, params)
	return aggregateEnv{app: testApp, k: k, ctx: ctx.WithEventManager(sdk.NewEventManager()), operators: operators}
}

func createValidator(t *testing.T, testApp *app.App, ctx sdk.Context, seed byte) sdk.ValAddress {
	t.Helper()
	pubkey := ed25519.GenPrivKeyFromSecret([]byte{'p', 'f', seed}).PubKey()
	operator := sdk.ValAddress(pubkey.Address())
	bond := sdk.NewCoin(testApp.StakingKeeper.BondDenom(ctx), testApp.StakingKeeper.TokensFromConsensusPower(ctx, 1))
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, sdk.NewCoins(bond)))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName,
		sdk.AccAddress(operator), sdk.NewCoins(bond)))
	commission := testApp.StakingKeeper.MinCommissionRate(ctx)
	msg, err := stakingtypes.NewMsgCreateValidator(operator, pubkey, bond, stakingtypes.Description{},
		stakingtypes.NewCommissionRates(commission, commission, sdk.ZeroDec()), sdk.OneInt())
	require.NoError(t, err)
	_, err = stakingkeeper.NewMsgServerImpl(testApp.StakingKeeper).CreateValidator(sdk.WrapSDKContext(ctx), msg)
	require.NoError(t, err)
	return operator
}

func aggregateParams(minSources, maxDivergenceBps uint32) types.Params {
	params := types.DefaultParams(types.DefaultAuthority())
	params.MinSources = minSources
	params.MaxDivergenceBps = maxDivergenceBps
	return params
}

func observe(env aggregateEnv, ctx sdk.Context, marketID uint64, operator sdk.ValAddress, index, mark sdk.Int, ts uint64) {
	env.k.Set(ctx, types.ObservationKey(marketID, operator), types.ObservationRecord{
		Sequence: 1, IndexPrice: index, MarkPrice: mark, SourceTimestampMs: ts, Height: ctx.BlockHeight(),
	})
}

func storedAggregate(t *testing.T, env aggregateEnv, ctx sdk.Context, marketID uint64) (types.Aggregate, bool) {
	t.Helper()
	var aggregate types.Aggregate
	found := env.k.Get(ctx, types.AggregateKey(marketID), &aggregate)
	return aggregate, found
}

// marketEvents returns the attributes of every event of eventType for marketID.
func marketEvents(ctx sdk.Context, eventType string, marketID string) [][]sdk.Attribute {
	var out [][]sdk.Attribute
	for _, event := range ctx.EventManager().Events() {
		if event.Type != eventType {
			continue
		}
		var attributes []sdk.Attribute
		matches := false
		for _, attribute := range event.Attributes {
			attributes = append(attributes, sdk.NewAttribute(string(attribute.Key), string(attribute.Value)))
			if string(attribute.Key) == types.AttributeMarketID && string(attribute.Value) == marketID {
				matches = true
			}
		}
		if matches {
			out = append(out, attributes)
		}
	}
	return out
}

func skipped(marketID, sources, required string) []sdk.Attribute {
	return []sdk.Attribute{
		sdk.NewAttribute(types.AttributeMarketID, marketID),
		sdk.NewAttribute(types.AttributeSources, sources),
		sdk.NewAttribute(types.AttributeRequired, required),
	}
}

func intOf(t *testing.T, value string) sdk.Int {
	t.Helper()
	parsed, ok := sdk.NewIntFromString(value)
	require.True(t, ok)
	return parsed
}

func TestAggregateBondedSetOnly(t *testing.T) {
	env := newAggregateEnv(t, 4, 200, aggregateParams(1, types.BpsDenominator))
	unbonded := createValidator(t, env.app, env.ctx, 99)
	validator, found := env.app.StakingKeeper.GetValidator(env.ctx, unbonded)
	require.True(t, found)
	require.Equal(t, stakingtypes.Unbonded, validator.Status)

	ts := uint64(aggregateNowMs - 1000)
	for i, operator := range env.operators {
		price := sdk.NewInt(int64(100 * (i + 1)))
		observe(env, env.ctx, 0, operator, price, price, ts)
	}
	observe(env, env.ctx, 0, unbonded, sdk.NewInt(1000), sdk.NewInt(1000), ts)
	env.k.Aggregate(env.ctx)

	aggregate, found := storedAggregate(t, env, env.ctx, 0)
	require.True(t, found)
	require.Equal(t, uint32(4), aggregate.Sources)
	require.Equal(t, "250", aggregate.IndexPrice.String())
	require.Equal(t, "250", aggregate.MarkPrice.String())
}

func TestAggregateStalenessFilter(t *testing.T) {
	env := newAggregateEnv(t, 4, 200, aggregateParams(1, types.BpsDenominator))
	require.Equal(t, uint64(10000), types.DefaultStalenessWindowMs)
	for i, ts := range []uint64{1_700_000_000_000, 1_699_999_999_999, 1_700_000_005_000, 1_700_000_009_000} {
		observe(env, env.ctx, 0, env.operators[i], sdk.NewInt(5000), sdk.NewInt(5000), ts)
	}
	env.k.Aggregate(env.ctx)

	aggregate, found := storedAggregate(t, env, env.ctx, 0)
	require.True(t, found)
	require.Equal(t, uint32(3), aggregate.Sources)
	require.Equal(t, uint64(1_700_000_000_000), aggregate.TimestampMs)
}

func TestAggregateQuorum(t *testing.T) {
	params := aggregateParams(1, types.DefaultMaxDivergenceBps)
	params.Markets[17].Enabled = false
	env := newAggregateEnv(t, 4, 200, params)
	ts := uint64(aggregateNowMs - 500)
	price := price18(64000)

	twoCtx, _ := env.ctx.CacheContext()
	twoCtx = twoCtx.WithEventManager(sdk.NewEventManager())
	for _, operator := range env.operators[:2] {
		observe(env, twoCtx, 0, operator, price, price, ts)
	}
	env.k.Aggregate(twoCtx)
	_, found := storedAggregate(t, env, twoCtx, 0)
	require.False(t, found)
	require.Equal(t, [][]sdk.Attribute{skipped("0", "2", "3")}, marketEvents(twoCtx, types.EventTypeAggregateSkipped, "0"))
	require.Empty(t, marketEvents(twoCtx, types.EventTypeAggregate, "0"))
	require.Equal(t, [][]sdk.Attribute{skipped("5", "0", "3")}, marketEvents(twoCtx, types.EventTypeAggregateSkipped, "5"))
	require.Empty(t, marketEvents(twoCtx, types.EventTypeAggregateSkipped, "17"))
	require.Len(t, twoCtx.EventManager().Events(), 17)

	threeCtx, _ := env.ctx.CacheContext()
	threeCtx = threeCtx.WithEventManager(sdk.NewEventManager())
	for _, operator := range env.operators[:3] {
		observe(env, threeCtx, 0, operator, price, price, ts)
	}
	env.k.Aggregate(threeCtx)
	aggregate, found := storedAggregate(t, env, threeCtx, 0)
	require.True(t, found)
	require.Equal(t, uint32(3), aggregate.Sources)
	require.Equal(t, price, aggregate.IndexPrice)
	require.Empty(t, marketEvents(threeCtx, types.EventTypeAggregateSkipped, "0"))
	events := marketEvents(threeCtx, types.EventTypeAggregate, "0")
	require.Len(t, events, 1)
	require.Equal(t, sdk.NewAttribute(types.AttributeRequired, "3"), events[0][6])

	params.MinSources = 4
	minCtx, _ := env.ctx.CacheContext()
	minCtx = minCtx.WithEventManager(sdk.NewEventManager())
	env.k.Set(minCtx, types.ParamsKey, params)
	for _, operator := range env.operators[:3] {
		observe(env, minCtx, 0, operator, price, price, ts)
	}
	env.k.Aggregate(minCtx)
	_, found = storedAggregate(t, env, minCtx, 0)
	require.False(t, found)
	require.Equal(t, [][]sdk.Attribute{skipped("0", "3", "4")}, marketEvents(minCtx, types.EventTypeAggregateSkipped, "0"))
}

func TestAggregateMedianOddEven(t *testing.T) {
	env := newAggregateEnv(t, 4, 200, aggregateParams(1, types.BpsDenominator))
	ts := uint64(aggregateNowMs - 500)

	oddCtx, _ := env.ctx.CacheContext()
	for i, price := range []int64{100, 300, 200} {
		observe(env, oddCtx, 0, env.operators[i], sdk.NewInt(price), sdk.NewInt(price), ts)
	}
	env.k.Aggregate(oddCtx)
	odd, found := storedAggregate(t, env, oddCtx, 0)
	require.True(t, found)
	require.Equal(t, "200", odd.IndexPrice.String())
	require.Equal(t, uint32(3), odd.Sources)

	evenCtx, _ := env.ctx.CacheContext()
	for i, price := range []int64{100, 200, 301, 400} {
		observe(env, evenCtx, 0, env.operators[i], sdk.NewInt(price), sdk.NewInt(price), ts)
	}
	env.k.Aggregate(evenCtx)
	even, found := storedAggregate(t, env, evenCtx, 0)
	require.True(t, found)
	require.Equal(t, "250", even.IndexPrice.String())
	require.Equal(t, "250", even.MarkPrice.String())
	require.Equal(t, uint32(4), even.Sources)
}

func TestAggregateDivergenceFilter(t *testing.T) {
	env := newAggregateEnv(t, 4, 200, aggregateParams(1, 50))
	ts := uint64(aggregateNowMs - 500)

	dropCtx, _ := env.ctx.CacheContext()
	for i, price := range []int64{10000, 10010, 10020, 11000} {
		observe(env, dropCtx, 0, env.operators[i], price18(price), price18(price), ts)
	}
	env.k.Aggregate(dropCtx)
	aggregate, found := storedAggregate(t, env, dropCtx, 0)
	require.True(t, found)
	require.Equal(t, price18(10010), aggregate.IndexPrice)
	require.Equal(t, uint32(3), aggregate.Sources)

	skipCtx, _ := env.ctx.CacheContext()
	skipCtx = skipCtx.WithEventManager(sdk.NewEventManager())
	for i, price := range []int64{10000, 10010, 11000} {
		observe(env, skipCtx, 0, env.operators[i], price18(price), price18(price), ts)
	}
	env.k.Aggregate(skipCtx)
	_, found = storedAggregate(t, env, skipCtx, 0)
	require.False(t, found)
	require.Equal(t, [][]sdk.Attribute{skipped("0", "2", "3")}, marketEvents(skipCtx, types.EventTypeAggregateSkipped, "0"))
}

func TestAggregateRecord(t *testing.T) {
	env := newAggregateEnv(t, 4, 210, aggregateParams(1, types.DefaultMaxDivergenceBps))
	marks := []int64{1003, 1001, 1002}
	for i, ts := range []uint64{1_700_000_002_000, 1_700_000_001_000, 1_700_000_003_000} {
		observe(env, env.ctx, 0, env.operators[i], price18(1000), price18(marks[i]), ts)
	}
	env.k.Aggregate(env.ctx)

	aggregate, found := storedAggregate(t, env, env.ctx, 0)
	require.True(t, found)
	require.Equal(t, types.Aggregate{
		IndexPrice: price18(1000), MarkPrice: price18(1002), Basis: price18(2),
		TimestampMs: 1_700_000_001_000, Height: 210, Sources: 3,
	}, aggregate)
	require.Equal(t, [][]sdk.Attribute{{
		sdk.NewAttribute(types.AttributeMarketID, "0"),
		sdk.NewAttribute(types.AttributeIndexPrice, "1000000000000000000000"),
		sdk.NewAttribute(types.AttributeMarkPrice, "1002000000000000000000"),
		sdk.NewAttribute(types.AttributeBasis, "2000000000000000000"),
		sdk.NewAttribute(types.AttributeTimestampMs, "1700000001000"),
		sdk.NewAttribute(types.AttributeSources, "3"),
		sdk.NewAttribute(types.AttributeRequired, "3"),
		sdk.NewAttribute(types.AttributeHeight, "210"),
	}}, marketEvents(env.ctx, types.EventTypeAggregate, "0"))
}

func TestAggregateOrderIndependent(t *testing.T) {
	env := newAggregateEnv(t, 4, 200, aggregateParams(1, types.DefaultMaxDivergenceBps))
	env.k.Set(env.ctx, types.AggregateKey(1), types.Aggregate{
		IndexPrice: price18(3000), MarkPrice: price18(3001), Basis: price18(1), TimestampMs: 1_700_000_000_000, Height: 199, Sources: 4,
	})
	type observation struct {
		market      uint64
		index, mark sdk.Int
		ts          uint64
	}
	submissions := make([][]observation, len(env.operators))
	for i := range env.operators {
		offset := int64(i)
		submissions[i] = []observation{
			{0, price18(64000 + offset), price18(64010 + offset), uint64(aggregateNowMs - 900 + 100*offset)},
			{1, price18(3000 + offset), price18(3004 - offset), uint64(aggregateNowMs - 400 - 100*offset)},
			{2, price18(150).AddRaw(offset), price18(151), uint64(aggregateNowMs - 1000)},
		}
	}
	deliver := func(order []int) (sdk.Context, [][]byte) {
		branch, _ := env.ctx.CacheContext()
		branch = branch.WithEventManager(sdk.NewEventManager())
		for _, i := range order {
			for _, o := range submissions[i] {
				observe(env, branch, o.market, env.operators[i], o.index, o.mark, o.ts)
			}
		}
		env.k.Aggregate(branch)
		store := branch.KVStore(env.k.StoreKey())
		records := make([][]byte, len(types.Markets))
		for m := range types.Markets {
			records[m] = store.Get(types.AggregateKey(uint64(m)))
		}
		return branch, records
	}
	forward, forwardRecords := deliver([]int{0, 1, 2, 3})
	backward, backwardRecords := deliver([]int{3, 2, 1, 0})

	require.Equal(t, forwardRecords, backwardRecords)
	require.Equal(t, forward.EventManager().Events(), backward.EventManager().Events())
	for m := 0; m < 3; m++ {
		require.NotNil(t, forwardRecords[m])
	}
	require.Nil(t, forwardRecords[3])
	aggregate, found := storedAggregate(t, env, forward, 1)
	require.True(t, found)
	require.Equal(t, price18(3001).Add(price18(3002)).QuoRaw(2), aggregate.IndexPrice)
	require.Equal(t, uint64(aggregateNowMs-700), aggregate.TimestampMs)
}

func TestAggregateEventShape(t *testing.T) {
	env := newAggregateEnv(t, 4, 210, aggregateParams(1, types.DefaultMaxDivergenceBps))
	index := intOf(t, "65000500000000000000000")
	mark := intOf(t, "64998500000000000000000")
	for i, ts := range []uint64{1_700_000_001_000, 1_700_000_002_000, 1_700_000_003_000} {
		observe(env, env.ctx, 0, env.operators[i], index, mark, ts)
	}
	env.k.Aggregate(env.ctx)

	require.Equal(t, [][]sdk.Attribute{{
		sdk.NewAttribute(types.AttributeMarketID, "0"),
		sdk.NewAttribute(types.AttributeIndexPrice, "65000500000000000000000"),
		sdk.NewAttribute(types.AttributeMarkPrice, "64998500000000000000000"),
		sdk.NewAttribute(types.AttributeBasis, "-2000000000000000000"),
		sdk.NewAttribute(types.AttributeTimestampMs, "1700000001000"),
		sdk.NewAttribute(types.AttributeSources, "3"),
		sdk.NewAttribute(types.AttributeRequired, "3"),
		sdk.NewAttribute(types.AttributeHeight, "210"),
	}}, marketEvents(env.ctx, types.EventTypeAggregate, "0"))
	require.Equal(t,
		`{"index_price":"65000500000000000000000","mark_price":"64998500000000000000000","basis":"-2000000000000000000","timestamp_ms":1700000001000,"height":210,"sources":3}`,
		string(env.ctx.KVStore(env.k.StoreKey()).Get(types.AggregateKey(0))))
}

func price18(n int64) sdk.Int { return sdk.NewIntWithDecimal(n, 18) }
