package pricefeed_test

import (
	"encoding/hex"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	minttypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/mint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/keeper"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

const moduleNowMs int64 = 1_700_000_010_000

// newModule builds the module on the test application's keepers with the
// pricefeed store layered into a context at height.
func newModule(t *testing.T, height int64) (*app.App, pricefeed.AppModule, keeper.Keeper, sdk.StoreKey, sdk.Context) {
	t.Helper()
	testApp := app.Setup(t, false, false, false)
	ctx := testApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(height).
		WithBlockTime(time.UnixMilli(moduleNowMs).UTC())
	key := sdk.NewKVStoreKey(types.StoreKey)
	k := keeper.NewKeeper(key, testApp.StakingKeeper, testApp.AccountKeeper, &testApp.EvmKeeper)
	ctx = pricefeedtestutil.WithPriceFeedStore(ctx, key).WithEventManager(sdk.NewEventManager())
	return testApp, pricefeed.NewAppModule(k), k, key, ctx
}

func eventsOf(ctx sdk.Context, eventType string) []sdk.Event {
	var out []sdk.Event
	for _, event := range ctx.EventManager().Events() {
		if event.Type == eventType {
			out = append(out, event)
		}
	}
	return out
}

func attributes(event sdk.Event) map[string]string {
	out := make(map[string]string, len(event.Attributes))
	for _, attribute := range event.Attributes {
		out[string(attribute.Key)] = string(attribute.Value)
	}
	return out
}

func TestAppModule(t *testing.T) {
	testApp, am, k, key, ctx := newModule(t, 10)
	require.Equal(t, "pricefeed", am.Name())
	require.Equal(t, uint64(1), am.ConsensusVersion())

	params := types.DefaultParams(types.DefaultAuthority())
	require.NoError(t, k.SetParams(ctx, params))

	pubkey := ed25519.GenPrivKeyFromSecret([]byte("pricefeed-module-validator")).PubKey()
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
	_, err = testApp.StakingKeeper.ApplyAndReturnValidatorSetUpdates(ctx)
	require.NoError(t, err)
	ctx = ctx.WithEventManager(sdk.NewEventManager())

	var operators []sdk.ValAddress
	for _, validator := range testApp.StakingKeeper.GetBondedValidatorsByPower(ctx) {
		operators = append(operators, validator.GetOperator())
	}
	require.NotEmpty(t, operators)

	index, ok := sdk.NewIntFromString("64250000000000000000000")
	require.True(t, ok)
	mark := index.Add(sdk.NewIntWithDecimal(4, 18))
	const sourceMs = uint64(moduleNowMs - 1_000)
	for _, operator := range operators {
		encoded, err := json.Marshal(types.ObservationRecord{
			Sequence: 1, IndexPrice: index, MarkPrice: mark, SourceTimestampMs: sourceMs, Height: 10,
		})
		require.NoError(t, err)
		ctx.KVStore(key).Set(types.ObservationKey(0, operator), encoded)
	}

	am.MidBlock(ctx, 10)

	var aggregate types.Aggregate
	encoded := ctx.KVStore(key).Get(types.AggregateKey(0))
	require.NotNil(t, encoded)
	require.NoError(t, json.Unmarshal(encoded, &aggregate))
	require.Equal(t, "64250000000000000000000", aggregate.IndexPrice.String())
	require.Equal(t, "64254000000000000000000", aggregate.MarkPrice.String())
	require.Equal(t, "4000000000000000000", aggregate.Basis.String())
	require.Equal(t, sourceMs, aggregate.TimestampMs)
	require.Equal(t, int64(10), aggregate.Height)
	require.Equal(t, uint32(len(operators)), aggregate.Sources)
	for _, market := range params.Markets[1:] {
		require.Nil(t, ctx.KVStore(key).Get(types.AggregateKey(market.ID)))
	}

	sources := strconv.Itoa(len(operators))
	required := strconv.Itoa(len(operators)/2 + 1)
	written := eventsOf(ctx, types.EventTypeAggregate)
	require.Len(t, written, 1)
	require.Equal(t, map[string]string{
		types.AttributeMarketID:    "0",
		types.AttributeIndexPrice:  "64250000000000000000000",
		types.AttributeMarkPrice:   "64254000000000000000000",
		types.AttributeBasis:       "4000000000000000000",
		types.AttributeTimestampMs: strconv.FormatUint(sourceMs, 10),
		types.AttributeSources:     sources,
		types.AttributeRequired:    required,
		types.AttributeHeight:      "10",
	}, attributes(written[0]))

	skippedEvents := eventsOf(ctx, types.EventTypeAggregateSkipped)
	require.Len(t, skippedEvents, len(params.Markets)-1)
	for i, event := range skippedEvents {
		require.Equal(t, map[string]string{
			types.AttributeMarketID: strconv.FormatUint(params.Markets[i+1].ID, 10),
			types.AttributeSources:  "0",
			types.AttributeRequired: required,
		}, attributes(event))
	}
}

func TestAppModuleNoParams(t *testing.T) {
	_, am, _, key, ctx := newModule(t, 5)

	am.MidBlock(ctx, 5)

	iterator := ctx.KVStore(key).Iterator(nil, nil)
	defer func() { _ = iterator.Close() }()
	require.False(t, iterator.Valid())
	require.Empty(t, ctx.EventManager().Events())
}

func TestAppModuleInitGenesis(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		_, am, k, _, ctx := newModule(t, 1)
		genesis := types.DefaultGenesis()
		encoded, err := json.Marshal(genesis)
		require.NoError(t, err)
		require.Empty(t, am.InitGenesis(ctx, nil, encoded))
		require.Equal(t, genesis.Params, k.GetParams(ctx))
	})

	t.Run("duplicate feed account", func(t *testing.T) {
		_, am, _, key, ctx := newModule(t, 1)
		pubKey := secp256k1.GenPrivKeyFromSecret([]byte("pricefeed-module-genesis")).PubKey()
		feedKey := types.FeedKey{
			PubKey:      hex.EncodeToString(pubKey.Bytes()),
			FeedAccount: sdk.AccAddress(pubKey.Address()).String(),
		}
		genesis := types.DefaultGenesis()
		genesis.FeedKeys = []types.GenesisFeedKey{
			{Operator: sdk.ValAddress([]byte("pricefeed-operator-a")), FeedKey: feedKey},
			{Operator: sdk.ValAddress([]byte("pricefeed-operator-b")), FeedKey: feedKey},
		}
		encoded, err := json.Marshal(genesis)
		require.NoError(t, err)
		genesisErr := keeper.ValidateGenesis(*genesis)
		require.ErrorIs(t, genesisErr, types.ErrInvalidGenesis)
		require.Contains(t, genesisErr.Error(), "is mapped to validators")
		require.PanicsWithError(t, genesisErr.Error(), func() {
			am.InitGenesis(ctx, nil, encoded)
		})
		iterator := ctx.KVStore(key).Iterator(nil, nil)
		defer func() { _ = iterator.Close() }()
		require.False(t, iterator.Valid())
		require.Empty(t, ctx.EventManager().Events())
	})
}

func TestAppModuleValidateGenesis(t *testing.T) {
	basic := pricefeed.AppModuleBasic{}
	require.NoError(t, basic.ValidateGenesis(nil, nil, basic.DefaultGenesis(nil)))

	require.ErrorIs(t, basic.ValidateGenesis(nil, nil, json.RawMessage(`{"params":`)), types.ErrInvalidGenesis)

	invalid := types.DefaultGenesis()
	invalid.Params.StalenessWindowMs = 0
	encoded, err := json.Marshal(invalid)
	require.NoError(t, err)
	require.ErrorIs(t, basic.ValidateGenesis(nil, nil, encoded), types.ErrInvalidParams)

	feedAccount := sdk.AccAddress([]byte("pricefeed-feed-acct-")).String()
	duplicateAccount := types.DefaultGenesis()
	duplicateAccount.FeedKeys = []types.GenesisFeedKey{
		{Operator: sdk.ValAddress([]byte("pricefeed-operator-a")), FeedKey: types.FeedKey{FeedAccount: feedAccount}},
		{Operator: sdk.ValAddress([]byte("pricefeed-operator-b")), FeedKey: types.FeedKey{FeedAccount: feedAccount}},
	}
	encoded, err = json.Marshal(duplicateAccount)
	require.NoError(t, err)
	require.ErrorIs(t, basic.ValidateGenesis(nil, nil, encoded), types.ErrInvalidGenesis)

	duplicateOperator := types.DefaultGenesis()
	operator := sdk.ValAddress([]byte("pricefeed-operator-a"))
	duplicateOperator.FeedKeys = []types.GenesisFeedKey{
		{Operator: operator, FeedKey: types.FeedKey{FeedAccount: feedAccount}},
		{Operator: operator, FeedKey: types.FeedKey{FeedAccount: sdk.AccAddress([]byte("pricefeed-feed-acct2")).String()}},
	}
	encoded, err = json.Marshal(duplicateOperator)
	require.NoError(t, err)
	require.ErrorIs(t, basic.ValidateGenesis(nil, nil, encoded), types.ErrInvalidGenesis)
}
