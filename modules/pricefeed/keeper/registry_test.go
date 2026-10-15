package keeper_test

import (
	"encoding/hex"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/keeper"
	pricefeedtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	stakingtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/staking/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func registryKeeper(t *testing.T) (keeper.Keeper, sdk.Context) {
	t.Helper()
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockHeight(8).WithEventManager(sdk.NewEventManager())
	return pricefeedtestutil.NewKeeper(testkeeper.EVMTestApp, ctx)
}

func setValidator(t *testing.T, ctx sdk.Context, seed string, status stakingtypes.BondStatus) sdk.ValAddress {
	t.Helper()
	operator := sdk.ValAddress(secp256k1.GenPrivKeyFromSecret([]byte("operator " + seed)).PubKey().Address())
	val, err := stakingtypes.NewValidator(operator, ed25519.GenPrivKeyFromSecret([]byte("consensus "+seed)).PubKey(), stakingtypes.Description{})
	require.NoError(t, err)
	testkeeper.EVMTestApp.StakingKeeper.SetValidator(ctx, val.UpdateStatus(status))
	return operator
}

func feedPubKey(seed string) []byte {
	return secp256k1.GenPrivKeyFromSecret([]byte("feed " + seed)).PubKey().Bytes()
}

func feedAccount(pubKey []byte) sdk.AccAddress {
	return sdk.AccAddress((&secp256k1.PubKey{Key: pubKey}).Address())
}

func requireRefusal(t *testing.T, err error, want error, lxpName string) {
	t.Helper()
	require.ErrorIs(t, err, want)
	name, ok := types.LXPName(err)
	require.True(t, ok)
	require.Equal(t, lxpName, name)
}

func TestRegistryCreatesFeedAccount(t *testing.T) {
	k, ctx := registryKeeper(t)
	accounts := testkeeper.EVMTestApp.AccountKeeper
	operator := setValidator(t, ctx, "a", stakingtypes.Bonded)
	pubKey := feedPubKey("a")
	want := feedAccount(pubKey)

	require.False(t, accounts.HasAccount(ctx, want))
	peek, _ := ctx.CacheContext()
	nextNumber := accounts.GetNextAccountNumber(peek)

	got, previous, err := k.RegisterFeedKey(ctx, operator, pubKey)
	require.NoError(t, err)
	require.Equal(t, want, got)
	require.Nil(t, previous)

	require.True(t, accounts.HasAccount(ctx, want))
	account := accounts.GetAccount(ctx, want)
	require.NotNil(t, account)
	require.Equal(t, want, account.GetAddress())
	require.Equal(t, nextNumber, account.GetAccountNumber())
	require.Equal(t, uint64(0), account.GetSequence())

	feedKey, ok := k.GetFeedKey(ctx, operator)
	require.True(t, ok)
	require.Equal(t, types.FeedKey{PubKey: hex.EncodeToString(pubKey), FeedAccount: want.String()}, feedKey)
	holder, ok := k.GetValidatorByFeedAccount(ctx, want)
	require.True(t, ok)
	require.Equal(t, operator, holder)

	// A second registration of the same key keeps the existing account.
	again, previous, err := k.RegisterFeedKey(ctx, operator, pubKey)
	require.NoError(t, err)
	require.Equal(t, want, again)
	require.Equal(t, want, previous)
	require.Equal(t, nextNumber, accounts.GetAccount(ctx, want).GetAccountNumber())
}

func TestRegistryEvents(t *testing.T) {
	k, ctx := registryKeeper(t)
	operator := setValidator(t, ctx, "b", stakingtypes.Bonded)
	pubKey := feedPubKey("b")
	account := feedAccount(pubKey)

	_, _, err := k.RegisterFeedKey(ctx, operator, pubKey)
	require.NoError(t, err)
	revoked, err := k.RevokeFeedKey(ctx, operator)
	require.NoError(t, err)
	require.Equal(t, account, revoked)

	require.Equal(t, sdk.Events{
		sdk.NewEvent(types.EventTypeFeedKey,
			sdk.NewAttribute(types.AttributeValidator, operator.String()),
			sdk.NewAttribute(types.AttributeFeedAccount, account.String()),
			sdk.NewAttribute(types.AttributePreviousFeedAccount, ""),
		),
		sdk.NewEvent(types.EventTypeFeedKeyRevoked,
			sdk.NewAttribute(types.AttributeValidator, operator.String()),
			sdk.NewAttribute(types.AttributeFeedAccount, account.String()),
		),
	}, ctx.EventManager().Events())

	feedKey, ok := k.GetFeedKey(ctx, operator)
	require.False(t, ok)
	require.Equal(t, types.FeedKey{}, feedKey)
	holder, ok := k.GetValidatorByFeedAccount(ctx, account)
	require.False(t, ok)
	require.Nil(t, holder)

	_, err = k.RevokeFeedKey(ctx, operator)
	requireRefusal(t, err, types.ErrNoKey, "LXP_PRICEFEED_NO_KEY")
	require.Len(t, ctx.EventManager().Events(), 2)
}

func TestRegistryReplacesKey(t *testing.T) {
	k, ctx := registryKeeper(t)
	operator := setValidator(t, ctx, "c", stakingtypes.Bonded)
	first, second := feedPubKey("c1"), feedPubKey("c2")

	_, _, err := k.RegisterFeedKey(ctx, operator, first)
	require.NoError(t, err)
	got, previous, err := k.RegisterFeedKey(ctx, operator, second)
	require.NoError(t, err)
	require.Equal(t, feedAccount(second), got)
	require.Equal(t, feedAccount(first), previous)

	feedKey, ok := k.GetFeedKey(ctx, operator)
	require.True(t, ok)
	require.Equal(t, types.FeedKey{PubKey: hex.EncodeToString(second), FeedAccount: feedAccount(second).String()}, feedKey)
	_, ok = k.GetValidatorByFeedAccount(ctx, feedAccount(first))
	require.False(t, ok)
	holder, ok := k.GetValidatorByFeedAccount(ctx, feedAccount(second))
	require.True(t, ok)
	require.Equal(t, operator, holder)

	events := ctx.EventManager().Events()
	require.Len(t, events, 2)
	require.Equal(t, sdk.NewEvent(types.EventTypeFeedKey,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeFeedAccount, feedAccount(second).String()),
		sdk.NewAttribute(types.AttributePreviousFeedAccount, feedAccount(first).String()),
	), events[1])
}

func TestRegistryRefusals(t *testing.T) {
	k, ctx := registryKeeper(t)
	bonded := setValidator(t, ctx, "d", stakingtypes.Bonded)
	other := setValidator(t, ctx, "e", stakingtypes.Bonded)
	unbonding := setValidator(t, ctx, "f", stakingtypes.Unbonding)
	unknown := sdk.ValAddress(secp256k1.GenPrivKeyFromSecret([]byte("operator g")).PubKey().Address())
	pubKey := feedPubKey("d")

	for _, operator := range []sdk.ValAddress{unknown, unbonding} {
		_, _, err := k.RegisterFeedKey(ctx, operator, pubKey)
		requireRefusal(t, err, types.ErrNotValidator, "LXP_PRICEFEED_NOT_VALIDATOR")
		_, ok := k.GetFeedKey(ctx, operator)
		require.False(t, ok)
		_, err = k.RevokeFeedKey(ctx, operator)
		requireRefusal(t, err, types.ErrNotValidator, "LXP_PRICEFEED_NOT_VALIDATOR")
	}
	_, ok := k.GetValidatorByFeedAccount(ctx, feedAccount(pubKey))
	require.False(t, ok)
	require.False(t, testkeeper.EVMTestApp.AccountKeeper.HasAccount(ctx, feedAccount(pubKey)))

	compressed := secp256k1.GenPrivKeyFromSecret([]byte("feed d")).PubKey().Bytes()
	badPrefix := append([]byte{0x05}, compressed[1:]...)
	notOnCurve := append([]byte{0x02}, make([]byte, 32)...)
	for _, invalid := range [][]byte{make([]byte, 65), badPrefix, notOnCurve, {}, nil} {
		_, _, err := k.RegisterFeedKey(ctx, bonded, invalid)
		requireRefusal(t, err, types.ErrInvalidKey, "LXP_PRICEFEED_INVALID_KEY")
	}
	_, ok = k.GetFeedKey(ctx, bonded)
	require.False(t, ok)

	_, _, err := k.RegisterFeedKey(ctx, bonded, pubKey)
	require.NoError(t, err)
	_, _, err = k.RegisterFeedKey(ctx, other, pubKey)
	requireRefusal(t, err, types.ErrKeyInUse, "LXP_PRICEFEED_KEY_IN_USE")
	holder, ok := k.GetValidatorByFeedAccount(ctx, feedAccount(pubKey))
	require.True(t, ok)
	require.Equal(t, bonded, holder)
	_, ok = k.GetFeedKey(ctx, other)
	require.False(t, ok)
	require.Len(t, ctx.EventManager().Events(), 1)
}
