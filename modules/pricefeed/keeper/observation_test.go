package keeper_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const submitBlockMs int64 = 1700000000000

type submitEnv struct {
	k      keeper.Keeper
	ctx    sdk.Context
	server types.MsgServer
}

// newSubmitEnv stores params on a fresh pricefeed store over the test
// application at the block time of the scenarios.
func newSubmitEnv(t *testing.T, edit func(*types.Params)) submitEnv {
	t.Helper()
	k, ctx := registryKeeper(t)
	params := types.DefaultParams(types.DefaultAuthority())
	if edit != nil {
		edit(&params)
	}
	require.NoError(t, k.SetParams(ctx, params))
	return submitEnv{k: k, ctx: ctx.WithBlockTime(time.UnixMilli(submitBlockMs)), server: keeper.NewMsgServerImpl(k)}
}

// feeder registers the feed key of a bonded validator and returns the
// operator and the bech32 feed account.
func (e submitEnv) feeder(t *testing.T, seed string) (sdk.ValAddress, string) {
	t.Helper()
	operator := setValidator(t, e.ctx, seed, stakingtypes.Bonded)
	account, _, err := e.k.RegisterFeedKey(e.ctx, operator, feedPubKey(seed))
	require.NoError(t, err)
	encoded, err := sdk.Bech32ifyAddressBytes(types.AccountAddressPrefix, account)
	require.NoError(t, err)
	return operator, encoded
}

// at returns the context of a new block at height with a fresh event manager.
func (e submitEnv) at(height int64) sdk.Context {
	return e.ctx.WithBlockHeight(height).WithEventManager(sdk.NewEventManager())
}

func (e submitEnv) submit(ctx sdk.Context, feeder string, observations ...types.Observation) (*types.MsgSubmitPricesResponse, error) {
	return e.server.SubmitPrices(sdk.WrapSDKContext(ctx), &types.MsgSubmitPrices{Feeder: feeder, Observations: observations})
}

func (e submitEnv) stored(ctx sdk.Context, marketID uint64, operator sdk.ValAddress) (types.ObservationRecord, bool) {
	var record types.ObservationRecord
	ok := e.k.Get(ctx, types.ObservationKey(marketID, operator), &record)
	return record, ok
}

func observation(marketID, sequence uint64, index, mark string, timestampMs uint64) types.Observation {
	return types.Observation{MarketId: marketID, Sequence: sequence, IndexPrice: index, MarkPrice: mark,
		SourceTimestampMs: timestampMs}
}

// valid is an observation every check accepts on the default params at the
// scenario block time.
func valid(marketID, sequence uint64) types.Observation {
	return observation(marketID, sequence, "65000500000000000000000", "65010000000000000000000", 1699999999500)
}

func refusedEvent(operator sdk.ValAddress, marketID, sequence uint64, reason string) sdk.Event {
	return sdk.NewEvent(types.EventTypeObservationRefused,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeMarketID, strconv.FormatUint(marketID, 10)),
		sdk.NewAttribute(types.AttributeSequence, strconv.FormatUint(sequence, 10)),
		sdk.NewAttribute(types.AttributeReason, reason),
	)
}

func eventsOf(ctx sdk.Context, eventType string) []sdk.Event {
	var events []sdk.Event
	for _, event := range ctx.EventManager().Events() {
		if event.Type == eventType {
			events = append(events, event)
		}
	}
	return events
}

func reasonsOf(ctx sdk.Context) []string {
	var reasons []string
	for _, event := range eventsOf(ctx, types.EventTypeObservationRefused) {
		for _, attribute := range event.Attributes {
			if string(attribute.Key) == types.AttributeReason {
				reasons = append(reasons, string(attribute.Value))
			}
		}
	}
	return reasons
}

func requireCounts(t *testing.T, response *types.MsgSubmitPricesResponse, err error, accepted, refused uint32) {
	t.Helper()
	require.NoError(t, err)
	require.Equal(t, &types.MsgSubmitPricesResponse{Accepted: accepted, Refused: refused}, response)
}

func setAggregate(t *testing.T, e submitEnv, marketID uint64, index string, timestampMs uint64) {
	t.Helper()
	price := intOf(t, index)
	e.k.Set(e.ctx, types.AggregateKey(marketID), types.Aggregate{IndexPrice: price, MarkPrice: price,
		Basis: sdk.ZeroInt(), TimestampMs: timestampMs, Height: 1, Sources: 1})
}

func TestSubmitPricesStructuralRefusals(t *testing.T) {
	e := newSubmitEnv(t, func(p *types.Params) { p.MaxObservationsPerSubmission = 32 })
	operator, feeder := e.feeder(t, "s1")
	unbonding, unbondingFeeder := e.feeder(t, "s2")
	setValidator(t, e.ctx, "s2", stakingtypes.Unbonding)
	stranger, err := sdk.Bech32ifyAddressBytes(types.AccountAddressPrefix,
		secp256k1.GenPrivKeyFromSecret([]byte("feed stranger")).PubKey().Address())
	require.NoError(t, err)

	many := make([]types.Observation, 33)
	for i := range many {
		many[i] = valid(uint64(i), 1)
	}
	oversized := make([]byte, types.DefaultMaxSubmissionBytes+1)

	cases := []struct {
		name         string
		ctx          sdk.Context
		feeder       string
		observations []types.Observation
		want         error
		lxpName      string
	}{
		{"unregistered", e.at(100), stranger, []types.Observation{valid(0, 1)},
			types.ErrUnregisteredFeeder, "LXP_PRICEFEED_UNREGISTERED_FEEDER"},
		{"unbonding", e.at(100), unbondingFeeder, []types.Observation{valid(0, 1)},
			types.ErrNotValidator, "LXP_PRICEFEED_NOT_VALIDATOR"},
		{"too many", e.at(100), feeder, many,
			types.ErrTooManyObservations, "LXP_PRICEFEED_TOO_MANY_OBSERVATIONS"},
		{"too large", e.at(100).WithTxBytes(oversized), feeder, []types.Observation{valid(0, 1)},
			types.ErrSubmissionTooLarge, "LXP_PRICEFEED_SUBMISSION_TOO_LARGE"},
		{"size before feeder", e.at(100).WithTxBytes(oversized), stranger, []types.Observation{valid(0, 1)},
			types.ErrSubmissionTooLarge, "LXP_PRICEFEED_SUBMISSION_TOO_LARGE"},
		{"feeder before count", e.at(100), stranger, many,
			types.ErrUnregisteredFeeder, "LXP_PRICEFEED_UNREGISTERED_FEEDER"},
		{"bonding before count", e.at(100), unbondingFeeder, many,
			types.ErrNotValidator, "LXP_PRICEFEED_NOT_VALIDATOR"},
	}
	for _, c := range cases {
		response, err := e.submit(c.ctx, c.feeder, c.observations...)
		require.Nil(t, response, c.name)
		requireRefusal(t, err, c.want, c.lxpName)
		require.Empty(t, c.ctx.EventManager().Events(), c.name)
	}
	for _, op := range []sdk.ValAddress{operator, unbonding} {
		for marketID := uint64(0); marketID < 33; marketID++ {
			_, ok := e.stored(e.ctx, marketID, op)
			require.False(t, ok)
		}
		_, ok := e.k.GetLastSubmissionHeight(e.ctx, op)
		require.False(t, ok)
	}

	exact := e.at(100).WithTxBytes(oversized[:types.DefaultMaxSubmissionBytes])
	response, err := e.submit(exact, feeder, many[:32]...)
	requireCounts(t, response, err, 18, 14)
}

func TestSubmitPricesAccepts(t *testing.T) {
	e := newSubmitEnv(t, nil)
	operator, feeder := e.feeder(t, "a1")
	ctx := e.at(100)

	response, err := e.submit(ctx, feeder, valid(0, 7))
	requireCounts(t, response, err, 1, 0)

	record, ok := e.stored(ctx, 0, operator)
	require.True(t, ok)
	require.Equal(t, uint64(7), record.Sequence)
	require.Equal(t, "65000500000000000000000", record.IndexPrice.String())
	require.Equal(t, "65010000000000000000000", record.MarkPrice.String())
	require.Equal(t, uint64(1699999999500), record.SourceTimestampMs)
	require.Equal(t, int64(100), record.Height)

	require.Equal(t, sdk.Events{sdk.NewEvent(types.EventTypeObservation,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeMarketID, "0"),
		sdk.NewAttribute(types.AttributeSequence, "7"),
		sdk.NewAttribute(types.AttributeIndexPrice, "65000500000000000000000"),
		sdk.NewAttribute(types.AttributeMarkPrice, "65010000000000000000000"),
		sdk.NewAttribute(types.AttributeSourceTimestampMs, "1699999999500"),
	)}, ctx.EventManager().Events())

	height, ok := e.k.GetLastSubmissionHeight(ctx, operator)
	require.True(t, ok)
	require.Equal(t, int64(100), height)
	require.Equal(t, []byte{0, 0, 0, 0, 0, 0, 0, 100}, ctx.KVStore(e.k.StoreKey()).Get(types.LastSubmitKey(operator)))

	// A later accepted observation replaces the stored one with its height.
	later := e.at(101)
	response, err = e.submit(later, feeder,
		observation(0, 8, "65000600000000000000000", "65010100000000000000000", 1699999999900))
	requireCounts(t, response, err, 1, 0)
	record, ok = e.stored(later, 0, operator)
	require.True(t, ok)
	require.Equal(t, uint64(8), record.Sequence)
	require.Equal(t, "65000600000000000000000", record.IndexPrice.String())
	require.Equal(t, "65010100000000000000000", record.MarkPrice.String())
	require.Equal(t, uint64(1699999999900), record.SourceTimestampMs)
	require.Equal(t, int64(101), record.Height)
}

func TestSubmitPricesRefusesUnlistedAndDisabled(t *testing.T) {
	e := newSubmitEnv(t, func(p *types.Params) { p.Markets[4].Enabled = false })
	operator, feeder := e.feeder(t, "u1")
	ctx := e.at(100)

	response, err := e.submit(ctx, feeder, valid(4, 1), valid(99, 1), valid(0, 1))
	requireCounts(t, response, err, 1, 2)

	_, ok := e.stored(ctx, 0, operator)
	require.True(t, ok)
	for _, marketID := range []uint64{4, 99} {
		_, ok := e.stored(ctx, marketID, operator)
		require.False(t, ok)
	}
	require.Equal(t, []sdk.Event{
		refusedEvent(operator, 4, 1, types.RefusalMarketDisabled),
		refusedEvent(operator, 99, 1, types.RefusalUnknownMarket),
	}, eventsOf(ctx, types.EventTypeObservationRefused))
	require.Len(t, eventsOf(ctx, types.EventTypeObservation), 1)
}

func TestSubmitPricesRefusesSequence(t *testing.T) {
	e := newSubmitEnv(t, nil)
	operator, feeder := e.feeder(t, "q1")

	response, err := e.submit(e.at(100), feeder, valid(1, 7))
	requireCounts(t, response, err, 1, 0)

	for i, sequence := range []uint64{7, 6} {
		ctx := e.at(101 + int64(i))
		response, err := e.submit(ctx, feeder, valid(1, sequence))
		requireCounts(t, response, err, 0, 1)
		require.Equal(t, []sdk.Event{refusedEvent(operator, 1, sequence, types.RefusalSequence)},
			ctx.EventManager().Events())
		record, ok := e.stored(ctx, 1, operator)
		require.True(t, ok)
		require.Equal(t, uint64(7), record.Sequence)
		require.Equal(t, int64(100), record.Height)
	}

	// The sequence is per validator and market.
	ctx := e.at(103)
	response, err = e.submit(ctx, feeder, valid(1, 8), valid(2, 1))
	requireCounts(t, response, err, 2, 0)
	other, otherFeeder := e.feeder(t, "q2")
	response, err = e.submit(ctx, otherFeeder, valid(1, 1))
	requireCounts(t, response, err, 1, 0)
	record, ok := e.stored(ctx, 1, other)
	require.True(t, ok)
	require.Equal(t, uint64(1), record.Sequence)
}

func TestSubmitPricesRefusesTimestamps(t *testing.T) {
	e := newSubmitEnv(t, nil)
	require.Equal(t, uint64(2000), e.k.GetParams(e.ctx).MaxClockSkewMs)
	require.Equal(t, uint64(10000), e.k.GetParams(e.ctx).StalenessWindowMs)
	operator, feeder := e.feeder(t, "t1")
	price := "65000500000000000000000"

	ctx := e.at(100)
	response, err := e.submit(ctx, feeder,
		observation(2, 1, price, price, 1700000002001), observation(3, 1, price, price, 1699999989999))
	requireCounts(t, response, err, 0, 2)
	require.Equal(t, []sdk.Event{
		refusedEvent(operator, 2, 1, types.RefusalFuture),
		refusedEvent(operator, 3, 1, types.RefusalStale),
	}, ctx.EventManager().Events())

	ctx = e.at(101)
	response, err = e.submit(ctx, feeder,
		observation(2, 1, price, price, 1700000002000), observation(3, 1, price, price, 1699999990000))
	requireCounts(t, response, err, 2, 0)
	for marketID, timestampMs := range map[uint64]uint64{2: 1700000002000, 3: 1699999990000} {
		record, ok := e.stored(ctx, marketID, operator)
		require.True(t, ok)
		require.Equal(t, timestampMs, record.SourceTimestampMs)
	}
}

func TestSubmitPricesRefusesBounds(t *testing.T) {
	minPrice, maxPrice := "1000000000000000000", "100000000000000000000000"
	e := newSubmitEnv(t, func(p *types.Params) {
		p.Markets[8].MinPrice = intOf(t, minPrice)
		p.Markets[8].MaxPrice = intOf(t, maxPrice)
	})
	operator, feeder := e.feeder(t, "b1")
	mark := "2000000000000000000000"

	refusals := []types.Observation{
		observation(8, 1, "999999999999999999", mark, 1699999999500),
		observation(8, 2, "100000000000000000000001", mark, 1699999999500),
		observation(8, 3, mark, "999999999999999999", 1699999999500),
		observation(8, 4, mark, "100000000000000000000001", 1699999999500),
	}
	for i, refused := range refusals {
		ctx := e.at(100 + int64(i))
		response, err := e.submit(ctx, feeder, refused)
		requireCounts(t, response, err, 0, 1)
		require.Equal(t, []sdk.Event{refusedEvent(operator, 8, refused.Sequence, types.RefusalBounds)},
			ctx.EventManager().Events())
		_, ok := e.stored(ctx, 8, operator)
		require.False(t, ok)
	}

	ctx := e.at(110)
	response, err := e.submit(ctx, feeder, observation(8, 5, minPrice, maxPrice, 1699999999500))
	requireCounts(t, response, err, 1, 0)
}

func TestSubmitPricesDeviationAndRecovery(t *testing.T) {
	e := newSubmitEnv(t, func(p *types.Params) { p.MaxDeviationBps = 1000 })
	operator, feeder := e.feeder(t, "d1")
	price := "2200000000000000000001"

	setAggregate(t, e, 1, "2000000000000000000000", uint64(submitBlockMs))
	ctx := e.at(100)
	response, err := e.submit(ctx, feeder, observation(1, 1, price, price, 1699999999500))
	requireCounts(t, response, err, 0, 1)
	require.Equal(t, []sdk.Event{refusedEvent(operator, 1, 1, types.RefusalDeviation)}, ctx.EventManager().Events())
	_, ok := e.stored(ctx, 1, operator)
	require.False(t, ok)

	// At exactly max_deviation_bps the index is accepted, on either side.
	ctx = e.at(101)
	response, err = e.submit(ctx, feeder,
		observation(1, 2, "2200000000000000000000", price, 1699999999500))
	requireCounts(t, response, err, 1, 0)
	ctx = e.at(102)
	response, err = e.submit(ctx, feeder,
		observation(1, 3, "1799999999999999999999", price, 1699999999500))
	requireCounts(t, response, err, 0, 1)
	require.Equal(t, []string{types.RefusalDeviation}, reasonsOf(ctx))

	// Aged past max_price_age_ms the aggregate is not fresh and the check is skipped.
	setAggregate(t, e, 1, "2000000000000000000000", uint64(submitBlockMs)-types.DefaultMaxPriceAgeMs-1)
	ctx = e.at(103)
	response, err = e.submit(ctx, feeder, observation(1, 4, price, price, 1699999999500))
	requireCounts(t, response, err, 1, 0)
	record, ok := e.stored(ctx, 1, operator)
	require.True(t, ok)
	require.Equal(t, price, record.IndexPrice.String())
	require.Equal(t, uint64(4), record.Sequence)
}

func TestSubmitPricesRefusalReasons(t *testing.T) {
	e := newSubmitEnv(t, func(p *types.Params) {
		p.Markets[4].Enabled = false
		p.Markets[8].MaxPrice = intOf(t, "100000000000000000000000")
	})
	operator, feeder := e.feeder(t, "r1")
	price := "65000500000000000000000"

	response, err := e.submit(e.at(99), feeder, valid(1, 7))
	requireCounts(t, response, err, 1, 0)
	setAggregate(t, e, 5, "2000000000000000000000", uint64(submitBlockMs))

	ctx := e.at(100)
	response, err = e.submit(ctx, feeder,
		valid(99, 1),
		valid(4, 1),
		valid(1, 7),
		observation(2, 1, price, price, 1700000002001),
		observation(3, 1, price, price, 1699999989999),
		observation(8, 1, "100000000000000000000001", price, 1699999999500),
		observation(5, 1, "2200000000000000000001", "2200000000000000000001", 1699999999500),
		valid(0, 1),
	)
	requireCounts(t, response, err, 1, 7)
	require.Equal(t, []sdk.Event{
		refusedEvent(operator, 99, 1, types.RefusalUnknownMarket),
		refusedEvent(operator, 4, 1, types.RefusalMarketDisabled),
		refusedEvent(operator, 1, 7, types.RefusalSequence),
		refusedEvent(operator, 2, 1, types.RefusalFuture),
		refusedEvent(operator, 3, 1, types.RefusalStale),
		refusedEvent(operator, 8, 1, types.RefusalBounds),
		refusedEvent(operator, 5, 1, types.RefusalDeviation),
	}, eventsOf(ctx, types.EventTypeObservationRefused))
	require.Len(t, eventsOf(ctx, types.EventTypeObservation), 1)

	// An observation failing several checks carries the reason of the first.
	over := "1000000000000000000000000000001"
	ctx = e.at(101)
	response, err = e.submit(ctx, feeder,
		observation(4, 1, over, price, 1700000002001),
		observation(1, 6, over, price, 1700000002001),
		observation(2, 1, over, price, 1700000002001),
		observation(3, 1, over, price, 1699999989999),
		observation(5, 1, over, over, 1699999999500),
	)
	requireCounts(t, response, err, 0, 5)
	require.Equal(t, []string{types.RefusalMarketDisabled, types.RefusalSequence, types.RefusalFuture,
		types.RefusalStale, types.RefusalBounds}, reasonsOf(ctx))
	record, ok := e.stored(ctx, 1, operator)
	require.True(t, ok)
	require.Equal(t, uint64(7), record.Sequence)
}

func TestSubmitPricesOncePerHeight(t *testing.T) {
	e := newSubmitEnv(t, nil)
	operator, feeder := e.feeder(t, "h1")
	ctx := e.at(60)

	response, err := e.submit(ctx, feeder, valid(0, 1), valid(1, 1))
	requireCounts(t, response, err, 2, 0)
	response, err = e.submit(ctx, feeder, valid(0, 2), valid(2, 1))
	require.Nil(t, response)
	requireRefusal(t, err, types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")

	record, ok := e.stored(ctx, 0, operator)
	require.True(t, ok)
	require.Equal(t, uint64(1), record.Sequence)
	_, ok = e.stored(ctx, 2, operator)
	require.False(t, ok)
	require.Len(t, eventsOf(ctx, types.EventTypeObservation), 2)
	height, ok := e.k.GetLastSubmissionHeight(ctx, operator)
	require.True(t, ok)
	require.Equal(t, int64(60), height)

	// A message whose observations are all refused still uses the height.
	other, otherFeeder := e.feeder(t, "h2")
	response, err = e.submit(ctx, otherFeeder, valid(99, 1))
	requireCounts(t, response, err, 0, 1)
	_, err = e.submit(ctx, otherFeeder, valid(0, 1))
	requireRefusal(t, err, types.ErrRateLimited, "LXP_PRICEFEED_RATE_LIMITED")
	height, ok = e.k.GetLastSubmissionHeight(ctx, other)
	require.True(t, ok)
	require.Equal(t, int64(60), height)

	next := e.at(61)
	response, err = e.submit(next, feeder, valid(0, 2), valid(2, 1))
	requireCounts(t, response, err, 2, 0)
	height, ok = e.k.GetLastSubmissionHeight(next, operator)
	require.True(t, ok)
	require.Equal(t, int64(61), height)
}
