package keeper

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/pricefeed/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

// observationKeyLen is the prefix, the 8-byte market id and at least one
// operator byte of an observation key.
const observationKeyLen = 1 + 8 + 1

// ValidateGenesis checks the params, that every feed key derives its feed
// account and that no operator, feed account, observation or aggregate
// appears twice or names an unlisted market.
func ValidateGenesis(genesis types.GenesisState) error {
	if err := genesis.Params.Validate(); err != nil {
		return err
	}
	listed := uint64(len(genesis.Params.Markets))

	operators := make(map[string]struct{}, len(genesis.FeedKeys))
	accounts := make(map[string]string, len(genesis.FeedKeys))
	for _, entry := range genesis.FeedKeys {
		if len(entry.Operator) == 0 {
			return types.ErrInvalidGenesis.Wrap("feed key without an operator")
		}
		operator := entry.Operator.String()
		if _, duplicate := operators[operator]; duplicate {
			return types.ErrInvalidGenesis.Wrapf("validator %s has two feed keys", operator)
		}
		operators[operator] = struct{}{}
		pubKey, err := hex.DecodeString(entry.FeedKey.PubKey)
		if err != nil {
			return types.ErrInvalidGenesis.Wrapf("validator %s feed key %q: %v", operator, entry.FeedKey.PubKey, err)
		}
		derived, err := feedAccountOf(pubKey)
		if err != nil {
			return types.ErrInvalidGenesis.Wrapf("validator %s: %v", operator, err)
		}
		if derived.String() != entry.FeedKey.FeedAccount {
			return types.ErrInvalidGenesis.Wrapf("validator %s feed account %s, its key derives %s",
				operator, entry.FeedKey.FeedAccount, derived)
		}
		if holder, duplicate := accounts[entry.FeedKey.FeedAccount]; duplicate {
			return types.ErrInvalidGenesis.Wrapf("feed account %s is mapped to validators %s and %s",
				entry.FeedKey.FeedAccount, holder, operator)
		}
		accounts[entry.FeedKey.FeedAccount] = operator
	}

	observations := make(map[string]struct{}, len(genesis.Observations))
	for _, entry := range genesis.Observations {
		if entry.MarketID >= listed {
			return types.ErrInvalidGenesis.Wrapf("observation of unlisted market %d", entry.MarketID)
		}
		if len(entry.Operator) == 0 {
			return types.ErrInvalidGenesis.Wrapf("observation of market %d without an operator", entry.MarketID)
		}
		key := string(types.ObservationKey(entry.MarketID, entry.Operator))
		if _, duplicate := observations[key]; duplicate {
			return types.ErrInvalidGenesis.Wrapf("validator %s has two observations of market %d", entry.Operator, entry.MarketID)
		}
		observations[key] = struct{}{}
		record := entry.Observation
		if !positive(record.IndexPrice) || !positive(record.MarkPrice) {
			return types.ErrInvalidGenesis.Wrapf("observation of market %d by %s needs positive prices", entry.MarketID, entry.Operator)
		}
	}

	aggregates := make(map[uint64]struct{}, len(genesis.Aggregates))
	for _, entry := range genesis.Aggregates {
		if entry.MarketID >= listed {
			return types.ErrInvalidGenesis.Wrapf("aggregate of unlisted market %d", entry.MarketID)
		}
		if _, duplicate := aggregates[entry.MarketID]; duplicate {
			return types.ErrInvalidGenesis.Wrapf("market %d has two aggregates", entry.MarketID)
		}
		aggregates[entry.MarketID] = struct{}{}
		aggregate := entry.Aggregate
		if !positive(aggregate.IndexPrice) || !positive(aggregate.MarkPrice) || aggregate.Basis.IsNil() || aggregate.Sources == 0 {
			return types.ErrInvalidGenesis.Wrapf("aggregate of market %d needs positive prices, a basis and a source", entry.MarketID)
		}
	}
	return nil
}

func positive(value sdk.Int) bool { return !value.IsNil() && value.IsPositive() }

// InitGenesis validates the whole genesis before it writes anything, then
// stores the params, each feed key with its reverse entry, the observations
// and the aggregates. Last submission heights start empty.
func (k Keeper) InitGenesis(ctx sdk.Context, genesis types.GenesisState) error {
	if err := ValidateGenesis(genesis); err != nil {
		return err
	}
	if err := k.SetParams(ctx, genesis.Params); err != nil {
		return err
	}
	for _, entry := range genesis.FeedKeys {
		feedAccount, err := sdk.AccAddressFromBech32(entry.FeedKey.FeedAccount)
		if err != nil {
			return types.ErrInvalidGenesis.Wrapf("feed account %q: %v", entry.FeedKey.FeedAccount, err)
		}
		k.set(ctx, types.FeedKeyKey(entry.Operator), entry.FeedKey)
		k.set(ctx, types.FeedAccountKey(feedAccount), entry.Operator)
	}
	for _, entry := range genesis.Observations {
		k.set(ctx, types.ObservationKey(entry.MarketID, entry.Operator), entry.Observation)
	}
	for _, entry := range genesis.Aggregates {
		k.set(ctx, types.AggregateKey(entry.MarketID), entry.Aggregate)
	}
	return nil
}

// ExportGenesis reads the params (the defaults before any are stored), the feed keys, the observations and the
// aggregates in store key order, so an export of an imported export is
// byte-identical. Last submission heights are not exported.
func (k Keeper) ExportGenesis(ctx sdk.Context) (types.GenesisState, error) {
	genesis := types.GenesisState{Params: k.GetParams(ctx)}
	store := ctx.KVStore(k.storeKey)

	feedKeys := sdk.KVStorePrefixIterator(store, types.FeedKeyPrefix)
	defer func() { _ = feedKeys.Close() }()
	for ; feedKeys.Valid(); feedKeys.Next() {
		entry := types.GenesisFeedKey{Operator: sdk.ValAddress(append([]byte(nil), feedKeys.Key()[len(types.FeedKeyPrefix):]...))}
		if err := json.Unmarshal(feedKeys.Value(), &entry.FeedKey); err != nil {
			return types.GenesisState{}, types.ErrInvalidGenesis.Wrapf("feed key of %s: %v", entry.Operator, err)
		}
		genesis.FeedKeys = append(genesis.FeedKeys, entry)
	}

	observations := sdk.KVStorePrefixIterator(store, types.ObservationPrefix)
	defer func() { _ = observations.Close() }()
	for ; observations.Valid(); observations.Next() {
		key := observations.Key()
		if len(key) < observationKeyLen {
			return types.GenesisState{}, types.ErrInvalidGenesis.Wrapf("observation key %x is short", key)
		}
		prefix := len(types.ObservationPrefix)
		entry := types.GenesisObservation{
			MarketID: binary.BigEndian.Uint64(key[prefix : prefix+8]),
			Operator: sdk.ValAddress(append([]byte(nil), key[prefix+8:]...)),
		}
		if err := json.Unmarshal(observations.Value(), &entry.Observation); err != nil {
			return types.GenesisState{}, types.ErrInvalidGenesis.Wrapf("observation of market %d by %s: %v", entry.MarketID, entry.Operator, err)
		}
		genesis.Observations = append(genesis.Observations, entry)
	}

	aggregates := sdk.KVStorePrefixIterator(store, types.AggregatePrefix)
	defer func() { _ = aggregates.Close() }()
	for ; aggregates.Valid(); aggregates.Next() {
		key := aggregates.Key()
		prefix := len(types.AggregatePrefix)
		if len(key) != prefix+8 {
			return types.GenesisState{}, types.ErrInvalidGenesis.Wrapf("aggregate key %x is not 9 bytes", key)
		}
		entry := types.GenesisAggregate{MarketID: binary.BigEndian.Uint64(key[prefix:])}
		if err := json.Unmarshal(aggregates.Value(), &entry.Aggregate); err != nil {
			return types.GenesisState{}, types.ErrInvalidGenesis.Wrapf("aggregate of market %d: %v", entry.MarketID, err)
		}
		genesis.Aggregates = append(genesis.Aggregates, entry)
	}
	return genesis, nil
}
