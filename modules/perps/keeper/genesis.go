package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/perps/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

const (
	wordLength   = 32
	uint64Length = 8
)

// indexEntry is one key and value of a derived index prefix.
type indexEntry struct {
	key   []byte
	value []byte
}

// InitGenesis imports every stored prefix of gs. The book, cloid, trigger,
// expiry, MarginRatioIndex and AdlRankIndex entries are rebuilt from the
// orders, accounts and positions; entries gs carries for them must equal the
// rebuilt ones. It panics with ErrInvalidGenesis when ValidateGenesis refuses
// the state.
func (k *Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.initGenesis(ctx, gs); err != nil {
		panic(err)
	}
}

func (k *Keeper) initGenesis(ctx sdk.Context, gs types.GenesisState) error {
	full, err := rebuildIndexes(gs)
	if err != nil {
		return err
	}
	if err := types.ValidateGenesis(full); err != nil {
		return err
	}
	if err := matchSuppliedIndexes(gs, full); err != nil {
		return err
	}
	if err := k.setParams(ctx, gs.Params); err != nil {
		return fmt.Errorf("%w: %w", types.ErrInvalidGenesis, err)
	}
	store := k.store(ctx)
	for _, market := range gs.Markets {
		bz, err := json.Marshal(market)
		if err != nil {
			return fmt.Errorf("%w: market %d: %v", types.ErrInvalidGenesis, market.ID, err)
		}
		store.Set(types.MarketKey(market.ID), bz)
	}
	for _, state := range gs.MarketStates {
		k.SetMarketState(ctx, state)
	}
	for _, account := range gs.Accounts {
		store.Set(types.AccountKey(account.Address), account.Marshal())
	}
	for _, order := range gs.Orders {
		store.Set(types.OrderKey(order.ID), order.Marshal())
	}
	for _, entry := range gs.AccountOrders {
		store.Set(types.AccountOrderKey(entry.Account, entry.OrderID), []byte{})
	}
	for _, position := range gs.Positions {
		store.Set(types.PositionKey(position.ID), position.Marshal())
	}
	for _, entry := range gs.AccountMarketPositions {
		store.Set(types.AccountMarketPositionKey(entry.Account, entry.MarketID), wordBytes(entry.PositionID))
	}
	for _, bucket := range gs.VolumeBuckets {
		store.Set(types.VolumeKey(bucket.Account, bucket.Day),
			append(wordBytes(bucket.TakerVolumeUsd), wordBytes(bucket.MakerVolumeUsd)...))
	}
	for _, bucket := range gs.VenueVolumeBuckets {
		store.Set(types.VenueVolumeKey(bucket.Day), wordBytes(bucket.VolumeUsd))
	}
	for _, cooldown := range gs.LiquidationCooldowns {
		store.Set(types.LiquidationCooldownKey(cooldown.PositionID), binary.BigEndian.AppendUint64(nil, cooldown.LastSliceTime))
	}
	for _, attached := range gs.AttachedTpSls {
		store.Set(types.AttachedTpSlKey(attached.PositionID),
			append(wordBytes(attached.TakeProfitOrderID), wordBytes(attached.StopLossOrderID)...))
	}
	store.Set(types.SequenceKey(types.SequenceOrderID), wordBytes(gs.Counters.NextOrderID))
	store.Set(types.SequenceKey(types.SequencePositionID), wordBytes(gs.Counters.NextPositionID))
	store.Set(types.SequenceKey(types.SequencePlacement), binary.BigEndian.AppendUint64(nil, gs.Counters.NextSequence))
	store.Set(types.SweepCursorKey, wordBytes(gs.SweepCursor))
	store.Set(types.VenueBooksKey, gs.VenueBooks.Marshal())
	for _, entry := range derivedEntries(full) {
		store.Set(entry.key, entry.value)
	}
	return nil
}

// rebuildIndexes returns gs with its derived index entries replaced by the
// entries its orders, accounts and positions imply: a book entry for every
// RESTING order, a trigger entry for every UNTRIGGERED order, a cloid and an
// expiry entry for every open order carrying a cloid or a deadline, a
// MarginRatioIndex entry for every account with a position and an
// AdlRankIndex entry for every position.
func rebuildIndexes(gs types.GenesisState) (types.GenesisState, error) {
	full := gs
	full.BookEntries, full.TriggerEntries, full.CloidEntries, full.ExpiryEntries = nil, nil, nil, nil
	full.MarginRatioEntries, full.AdlRankEntries = nil, nil
	for _, order := range gs.Orders {
		switch order.Status {
		case types.OrderStatusResting:
			full.BookEntries = append(full.BookEntries, types.BookEntry{
				MarketID: order.MarketID, Side: types.SideOf(order.IsLong), Price: order.LimitPrice,
				Sequence: order.Sequence, OrderID: order.ID,
			})
		case types.OrderStatusUntriggered:
			direction, err := TriggerDirection(order)
			if err != nil {
				return types.GenesisState{}, fmt.Errorf("%w: untriggered order %s: %w", types.ErrInvalidGenesis,
					order.ID.Dec(), err)
			}
			full.TriggerEntries = append(full.TriggerEntries, types.TriggerEntry{
				MarketID: order.MarketID, Direction: direction, TriggerPrice: order.TriggerPrice,
				Sequence: order.Sequence, OrderID: order.ID,
			})
		default:
			continue
		}
		if order.Cloid != ([16]byte{}) {
			full.CloidEntries = append(full.CloidEntries, types.CloidEntry{
				Account: order.Account, MarketID: order.MarketID, Cloid: order.Cloid, OrderID: order.ID,
			})
		}
		if order.Deadline != 0 {
			full.ExpiryEntries = append(full.ExpiryEntries, types.ExpiryEntry{Deadline: order.Deadline, OrderID: order.ID})
		}
	}
	holders := map[common.Address]bool{}
	for _, position := range gs.Positions {
		holders[position.Account] = true
		full.AdlRankEntries = append(full.AdlRankEntries, types.AdlRankEntry{
			MarketID: position.MarketID, Side: types.SideOf(position.IsLong), RankScore: position.RankScore,
			PositionID: position.ID,
		})
	}
	for _, account := range gs.Accounts {
		if holders[account.Address] {
			full.MarginRatioEntries = append(full.MarginRatioEntries,
				types.MarginRatioEntry{Bucket: account.MarginRatioBucket, Account: account.Address})
		}
	}
	return full, nil
}

// matchSuppliedIndexes refuses derived entries in gs that differ from the
// entries of full rebuilt from the records; an empty list is rebuilt.
func matchSuppliedIndexes(gs, full types.GenesisState) error {
	for _, check := range []struct {
		name             string
		supplied, stored []indexEntry
	}{
		{"book", encode(gs.BookEntries, bookEntry), encode(full.BookEntries, bookEntry)},
		{"cloid", encode(gs.CloidEntries, cloidEntry), encode(full.CloidEntries, cloidEntry)},
		{"trigger", encode(gs.TriggerEntries, triggerEntry), encode(full.TriggerEntries, triggerEntry)},
		{"expiry", encode(gs.ExpiryEntries, expiryEntry), encode(full.ExpiryEntries, expiryEntry)},
		{"margin ratio", encode(gs.MarginRatioEntries, marginRatioEntry), encode(full.MarginRatioEntries, marginRatioEntry)},
		{"ADL rank", encode(gs.AdlRankEntries, adlRankEntry), encode(full.AdlRankEntries, adlRankEntry)},
	} {
		if len(check.supplied) == 0 {
			continue
		}
		if len(check.supplied) != len(check.stored) {
			return fmt.Errorf("%w: %d %s entries where the records give %d", types.ErrInvalidGenesis,
				len(check.supplied), check.name, len(check.stored))
		}
		want := make(map[string]string, len(check.stored))
		for _, entry := range check.stored {
			want[string(entry.key)] = string(entry.value)
		}
		for _, entry := range check.supplied {
			value, ok := want[string(entry.key)]
			if !ok || value != string(entry.value) {
				return fmt.Errorf("%w: %s entry %x disagrees with the records", types.ErrInvalidGenesis, check.name, entry.key)
			}
			delete(want, string(entry.key))
		}
	}
	return nil
}

func derivedEntries(full types.GenesisState) []indexEntry {
	var out []indexEntry
	out = append(out, encode(full.BookEntries, bookEntry)...)
	out = append(out, encode(full.CloidEntries, cloidEntry)...)
	out = append(out, encode(full.TriggerEntries, triggerEntry)...)
	out = append(out, encode(full.ExpiryEntries, expiryEntry)...)
	out = append(out, encode(full.MarginRatioEntries, marginRatioEntry)...)
	return append(out, encode(full.AdlRankEntries, adlRankEntry)...)
}

func encode[T any](entries []T, entry func(T) indexEntry) []indexEntry {
	out := make([]indexEntry, 0, len(entries))
	for _, value := range entries {
		out = append(out, entry(value))
	}
	return out
}

func bookEntry(e types.BookEntry) indexEntry {
	return indexEntry{
		key:   types.BookKey(e.MarketID, e.Side, types.BookPriceKey(e.Side == types.SideBuy, e.Price), e.Sequence),
		value: wordBytes(e.OrderID),
	}
}

func cloidEntry(e types.CloidEntry) indexEntry {
	return indexEntry{key: types.CloidKey(e.Account, e.MarketID, e.Cloid), value: wordBytes(e.OrderID)}
}

// triggerEntry keys a rising trigger by its price and a falling trigger by
// 2^256-1 minus its price, the inversion BookPriceKey applies to bids.
func triggerEntry(e types.TriggerEntry) indexEntry {
	priceKey := types.BookPriceKey(e.Direction == types.TriggerFalling, e.TriggerPrice)
	return indexEntry{key: types.TriggerKey(e.MarketID, e.Direction, priceKey, e.Sequence), value: wordBytes(e.OrderID)}
}

func expiryEntry(e types.ExpiryEntry) indexEntry {
	return indexEntry{key: types.ExpiryKey(e.Deadline, e.OrderID), value: []byte{}}
}

func marginRatioEntry(e types.MarginRatioEntry) indexEntry {
	return indexEntry{key: types.MarginRatioIndexKey(e.Bucket, e.Account), value: []byte{}}
}

func adlRankEntry(e types.AdlRankEntry) indexEntry {
	return indexEntry{key: types.AdlRankIndexKey(e.MarketID, e.Side, e.RankScore, e.PositionID), value: []byte{}}
}

// ExportGenesis exports every stored prefix except the derived book, cloid,
// trigger, expiry, MarginRatioIndex and AdlRankIndex entries, which
// InitGenesis rebuilds. It panics with ErrInvalidGenesis on a corrupt record.
func (k *Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs, err := k.exportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return gs
}

func (k *Keeper) exportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	gs := types.DefaultGenesis()
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", types.ErrInvalidGenesis, err)
	}
	gs.Params = params
	if err := k.IterateMarkets(ctx, func(market types.Market) bool {
		gs.Markets = append(gs.Markets, market)
		return false
	}); err != nil {
		return nil, fmt.Errorf("%w: %w", types.ErrInvalidGenesis, err)
	}
	store := k.store(ctx)
	for _, export := range []struct {
		name        string
		prefix      []byte
		keyLength   int
		decode      func(key, value []byte) error
		valueLength int // -1 leaves the value length to decode
	}{
		{"market state", types.MarketStatePrefix, uint64Length, func(key, value []byte) error {
			var state types.MarketState
			if err := state.Unmarshal(value); err != nil || state.MarketID != binary.BigEndian.Uint64(key) {
				return corruptRecord("market state", key, err)
			}
			gs.MarketStates = append(gs.MarketStates, state)
			return nil
		}, -1},
		{"account", types.AccountPrefix, common.AddressLength, func(key, value []byte) error {
			var account types.Account
			if err := account.Unmarshal(value); err != nil || account.Address != common.BytesToAddress(key) {
				return corruptRecord("account", key, err)
			}
			gs.Accounts = append(gs.Accounts, account)
			return nil
		}, -1},
		{"order", types.OrderPrefix, wordLength, func(key, value []byte) error {
			var order types.Order
			if err := order.Unmarshal(value); err != nil || !order.ID.Eq(wordOf(key)) {
				return corruptRecord("order", key, err)
			}
			gs.Orders = append(gs.Orders, order)
			return nil
		}, -1},
		{"account order", types.AccountOrderPrefix, common.AddressLength + wordLength, func(key, _ []byte) error {
			gs.AccountOrders = append(gs.AccountOrders, types.AccountOrderEntry{
				Account: common.BytesToAddress(key[:common.AddressLength]), OrderID: *wordOf(key[common.AddressLength:]),
			})
			return nil
		}, 0},
		{"position", types.PositionPrefix, wordLength, func(key, value []byte) error {
			var position types.Position
			if err := position.Unmarshal(value); err != nil || !position.ID.Eq(wordOf(key)) {
				return corruptRecord("position", key, err)
			}
			gs.Positions = append(gs.Positions, position)
			return nil
		}, -1},
		{"account market position", types.AccountMarketPositionPrefix, common.AddressLength + uint64Length, func(key, value []byte) error {
			gs.AccountMarketPositions = append(gs.AccountMarketPositions, types.AccountMarketPositionEntry{
				Account:    common.BytesToAddress(key[:common.AddressLength]),
				MarketID:   binary.BigEndian.Uint64(key[common.AddressLength:]),
				PositionID: *wordOf(value),
			})
			return nil
		}, wordLength},
		{"volume", types.VolumePrefix, common.AddressLength + uint64Length, func(key, value []byte) error {
			gs.VolumeBuckets = append(gs.VolumeBuckets, types.VolumeBucket{
				Account:        common.BytesToAddress(key[:common.AddressLength]),
				Day:            binary.BigEndian.Uint64(key[common.AddressLength:]),
				TakerVolumeUsd: *wordOf(value[:wordLength]),
				MakerVolumeUsd: *wordOf(value[wordLength:]),
			})
			return nil
		}, 2 * wordLength},
		{"venue volume", types.VenueVolumePrefix, uint64Length, func(key, value []byte) error {
			gs.VenueVolumeBuckets = append(gs.VenueVolumeBuckets, types.VenueVolumeBucket{
				Day: binary.BigEndian.Uint64(key), VolumeUsd: *wordOf(value),
			})
			return nil
		}, wordLength},
		{"liquidation cooldown", types.LiquidationCooldownPrefix, wordLength, func(key, value []byte) error {
			gs.LiquidationCooldowns = append(gs.LiquidationCooldowns, types.LiquidationCooldown{
				PositionID: *wordOf(key), LastSliceTime: binary.BigEndian.Uint64(value),
			})
			return nil
		}, uint64Length},
		{"attached TP/SL", types.AttachedTpSlPrefix, wordLength, func(key, value []byte) error {
			gs.AttachedTpSls = append(gs.AttachedTpSls, types.AttachedTpSl{
				PositionID: *wordOf(key), TakeProfitOrderID: *wordOf(value[:wordLength]),
				StopLossOrderID: *wordOf(value[wordLength:]),
			})
			return nil
		}, 2 * wordLength},
	} {
		iterator := sdk.KVStorePrefixIterator(store, export.prefix)
		for ; iterator.Valid(); iterator.Next() {
			key, value := iterator.Key()[len(export.prefix):], iterator.Value()
			if len(key) != export.keyLength {
				iterator.Close()
				return nil, corruptRecord(export.name, iterator.Key(), nil)
			}
			if export.valueLength >= 0 && len(value) != export.valueLength {
				iterator.Close()
				return nil, corruptRecord(export.name, iterator.Key(), fmt.Errorf("value of %d bytes", len(value)))
			}
			if err := export.decode(key, value); err != nil {
				iterator.Close()
				return nil, err
			}
		}
		iterator.Close()
	}
	if err := k.exportSingletons(ctx, gs); err != nil {
		return nil, err
	}
	return gs, nil
}

// exportSingletons reads the counters, the sweep cursor and the venue books;
// an absent counter is 1 and an absent cursor or venue books zero, as the
// keeper reads them.
func (k *Keeper) exportSingletons(ctx sdk.Context, gs *types.GenesisState) error {
	store := k.store(ctx)
	for _, word := range []struct {
		key    []byte
		target *uint256.Int
	}{
		{types.SequenceKey(types.SequenceOrderID), &gs.Counters.NextOrderID},
		{types.SequenceKey(types.SequencePositionID), &gs.Counters.NextPositionID},
		{types.SweepCursorKey, &gs.SweepCursor},
	} {
		bz := store.Get(word.key)
		if bz == nil {
			continue
		}
		if len(bz) != wordLength {
			return corruptRecord("counter", word.key, fmt.Errorf("value of %d bytes", len(bz)))
		}
		word.target.SetBytes32(bz)
	}
	if bz := store.Get(types.SequenceKey(types.SequencePlacement)); bz != nil {
		if len(bz) != uint64Length {
			return corruptRecord("placement sequence", types.SequenceKey(types.SequencePlacement),
				fmt.Errorf("value of %d bytes", len(bz)))
		}
		gs.Counters.NextSequence = binary.BigEndian.Uint64(bz)
	}
	if bz := store.Get(types.VenueBooksKey); bz != nil {
		if err := gs.VenueBooks.Unmarshal(bz); err != nil {
			return corruptRecord("venue books", types.VenueBooksKey, err)
		}
	}
	return nil
}

func wordOf(bz []byte) *uint256.Int {
	var value uint256.Int
	value.SetBytes32(bz)
	return &value
}

func corruptRecord(name string, key []byte, cause error) error {
	if cause == nil {
		return fmt.Errorf("%w: corrupt %s record %x", types.ErrInvalidGenesis, name, key)
	}
	return fmt.Errorf("%w: corrupt %s record %x: %v", types.ErrInvalidGenesis, name, key, cause)
}
