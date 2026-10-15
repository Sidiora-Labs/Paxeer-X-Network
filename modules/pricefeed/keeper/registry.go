package keeper

import (
	"encoding/hex"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// requireBonded refuses an operator whose validator is unknown or outside the
// bonded set, the rule ValidateFeeder applies to oracle feeders.
func (k Keeper) requireBonded(ctx sdk.Context, operator sdk.ValAddress) error {
	if val := k.stakingKeeper.Validator(ctx, operator); val == nil || !val.IsBonded() {
		return types.ErrNotValidator.Wrapf("validator %s is not in the bonded set", operator)
	}
	return nil
}

// feedAccountOf decodes a 33-byte compressed secp256k1 public key and returns
// the account the auth module derives for it.
func feedAccountOf(pubKey []byte) (sdk.AccAddress, error) {
	if len(pubKey) != secp256k1.PubKeySize {
		return nil, types.ErrInvalidKey.Wrapf("%d bytes", len(pubKey))
	}
	if _, err := crypto.DecompressPubkey(pubKey); err != nil {
		return nil, types.ErrInvalidKey.Wrap(err.Error())
	}
	return sdk.AccAddress((&secp256k1.PubKey{Key: pubKey}).Address()), nil
}

// RegisterFeedKey stores pubKey as the feed key of a bonded validator and the
// reverse entry of its feed account, replacing any previous key of the
// validator, and creates the feed account in the auth module when absent. It
// returns the feed account and the previous feed account, nil on a first key.
func (k Keeper) RegisterFeedKey(ctx sdk.Context, operator sdk.ValAddress, pubKey []byte) (sdk.AccAddress, sdk.AccAddress, error) {
	if err := k.requireBonded(ctx, operator); err != nil {
		return nil, nil, err
	}
	feedAccount, err := feedAccountOf(pubKey)
	if err != nil {
		return nil, nil, err
	}
	if holder, ok := k.GetValidatorByFeedAccount(ctx, feedAccount); ok && !holder.Equals(operator) {
		return nil, nil, types.ErrKeyInUse.Wrapf("feed account %s is registered to validator %s", feedAccount, holder)
	}

	var previous sdk.AccAddress
	if current, ok := k.GetFeedKey(ctx, operator); ok {
		previous, err = sdk.AccAddressFromBech32(current.FeedAccount)
		if err != nil {
			return nil, nil, types.ErrInvalidKey.Wrapf("stored feed account %q: %v", current.FeedAccount, err)
		}
		ctx.KVStore(k.storeKey).Delete(types.FeedAccountKey(previous))
	}

	k.set(ctx, types.FeedKeyKey(operator), types.FeedKey{PubKey: hex.EncodeToString(pubKey), FeedAccount: feedAccount.String()})
	k.set(ctx, types.FeedAccountKey(feedAccount), operator)
	if !k.accountKeeper.HasAccount(ctx, feedAccount) {
		k.accountKeeper.SetAccount(ctx, k.accountKeeper.NewAccountWithAddress(ctx, feedAccount))
	}

	previousAttribute := ""
	if previous != nil {
		previousAttribute = previous.String()
	}
	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeedKey,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeFeedAccount, feedAccount.String()),
		sdk.NewAttribute(types.AttributePreviousFeedAccount, previousAttribute),
	))
	return feedAccount, previous, nil
}

// RevokeFeedKey removes the feed key of a bonded validator and its reverse
// entry and returns the feed account that held it.
func (k Keeper) RevokeFeedKey(ctx sdk.Context, operator sdk.ValAddress) (sdk.AccAddress, error) {
	if err := k.requireBonded(ctx, operator); err != nil {
		return nil, err
	}
	current, ok := k.GetFeedKey(ctx, operator)
	if !ok {
		return nil, types.ErrNoKey.Wrapf("validator %s", operator)
	}
	feedAccount, err := sdk.AccAddressFromBech32(current.FeedAccount)
	if err != nil {
		return nil, types.ErrInvalidKey.Wrapf("stored feed account %q: %v", current.FeedAccount, err)
	}

	store := ctx.KVStore(k.storeKey)
	store.Delete(types.FeedKeyKey(operator))
	store.Delete(types.FeedAccountKey(feedAccount))

	ctx.EventManager().EmitEvent(sdk.NewEvent(types.EventTypeFeedKeyRevoked,
		sdk.NewAttribute(types.AttributeValidator, operator.String()),
		sdk.NewAttribute(types.AttributeFeedAccount, feedAccount.String()),
	))
	return feedAccount, nil
}

// GetFeedKey returns the feed key of a validator and whether one is stored.
func (k Keeper) GetFeedKey(ctx sdk.Context, operator sdk.ValAddress) (types.FeedKey, bool) {
	var feedKey types.FeedKey
	if !k.get(ctx, types.FeedKeyKey(operator), &feedKey) {
		return types.FeedKey{}, false
	}
	return feedKey, true
}

// GetValidatorByFeedAccount returns the validator a feed account belongs to
// and whether one is stored.
func (k Keeper) GetValidatorByFeedAccount(ctx sdk.Context, feedAccount sdk.AccAddress) (sdk.ValAddress, bool) {
	var operator sdk.ValAddress
	if !k.get(ctx, types.FeedAccountKey(feedAccount), &operator) {
		return nil, false
	}
	return operator, true
}
