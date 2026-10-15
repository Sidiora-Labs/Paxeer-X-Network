package keeper

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// Migration log events: every record MigrateV6120 stores is also emitted as
// one event of the upgrade block.
const (
	EventTypeMigrationBookHeldDelta = "launchpad_migration_book_held_delta"
	EventTypeMigrationFeeRightID    = "launchpad_migration_fee_right_id"
	EventTypeMigrationRetired       = "launchpad_migration_retired"

	AttributeKeyPool          = "pool"
	AttributeKeyAsset         = "asset"
	AttributeKeyBook          = "book"
	AttributeKeyHeld          = "held"
	AttributeKeyDelta         = "delta"
	AttributeKeyGeneration1ID = "generation_1_id"
	AttributeKeyGeneration2ID = "generation_2_id"
	AttributeKeyAddress       = "address"
	AttributeKeyRole          = "role"
	AttributeKeyReplacement   = "replacement"
)

var (
	// ErrNativeMarketsExist refuses the v6.12.0 migration over a store that
	// already holds native markets.
	ErrNativeMarketsExist = errors.New("native markets exist")
	// ErrUnlistedBookHeldDelta refuses a book-held delta above the fixture
	// tolerance at a pool and asset the fixture does not list.
	ErrUnlistedBookHeldDelta = errors.New("unlisted book-held delta")
	// ErrInvalidMigrationRecord refuses a malformed or repeated migration
	// record.
	ErrInvalidMigrationRecord = errors.New("invalid migration record")
)

// retiredRoles are the generation 1 roles whose addresses are retired
// (contracts/launchpad.md section 21).
var retiredRoles = map[string]bool{types.RoleFactory: true, types.RoleRouter: true, types.RoleQuoter: true,
	types.RoleFeesRouter: true, types.RoleFeeAccumulator: true, types.RolePoolRegistry: true,
	types.RoleProtocolConfig: true, types.RoleFeeRights: true, types.RoleTreasury: true,
	types.RoleOpticalRegistry: true}

func invalidRecord(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrInvalidMigrationRecord, fmt.Sprintf(format, args...))
}

func canonicalAddress(value string) bool {
	return common.IsHexAddress(value) && common.HexToAddress(value).Hex() == value &&
		common.HexToAddress(value) != (common.Address{})
}

func uint256(value sdk.Int) bool {
	return !value.IsNil() && !value.IsNegative() && value.BigInt().Cmp(types.MaxUint256()) <= 0
}

func validBookHeldDelta(delta types.BookHeldDelta) error {
	if !canonicalAddress(delta.Pool) || !canonicalAddress(delta.Asset) || !uint256(delta.Book) ||
		!uint256(delta.Held) || delta.Delta.IsNil() || delta.Delta.IsZero() ||
		!delta.Delta.Equal(delta.Held.Sub(delta.Book)) {
		return invalidRecord("book-held delta %s %s", delta.Pool, delta.Asset)
	}
	return nil
}

// CheckNoNativeMarkets refuses when any native market exists, so no market
// created before the upgrade collides with the imported indexes.
func (k *Keeper) CheckNoNativeMarkets(ctx sdk.Context) error {
	if k.GetMarketCount(ctx) > 0 {
		return ErrNativeMarketsExist
	}
	return nil
}

// CheckDeltaTolerance refuses when a delta of deltas sits at a pool and asset
// not listed in known and its magnitude exceeds tolerance. MigrateV6120 runs
// it over every pool before recording any delta.
func CheckDeltaTolerance(deltas, known []types.BookHeldDelta, tolerance sdk.Int) error {
	if !uint256(tolerance) {
		return invalidRecord("book-held delta tolerance")
	}
	type poolAsset struct{ pool, asset string }
	listed := make(map[poolAsset]bool, len(known))
	for _, delta := range known {
		if err := validBookHeldDelta(delta); err != nil {
			return err
		}
		listed[poolAsset{delta.Pool, delta.Asset}] = true
	}
	for _, delta := range deltas {
		if err := validBookHeldDelta(delta); err != nil {
			return err
		}
		if !listed[poolAsset{delta.Pool, delta.Asset}] && delta.Delta.Abs().GT(tolerance) {
			return ErrUnlistedBookHeldDelta
		}
	}
	return nil
}

// RecordBookHeldDelta stores the book-held delta of a migrated pool and asset
// and writes it to the migration log.
func (k *Keeper) RecordBookHeldDelta(ctx sdk.Context, delta types.BookHeldDelta) error {
	if err := validBookHeldDelta(delta); err != nil {
		return err
	}
	key := types.BookHeldDeltaKey(common.HexToAddress(delta.Pool), common.HexToAddress(delta.Asset))
	store := k.store(ctx)
	if store.Has(key) {
		return invalidRecord("book-held delta %s %s recorded twice", delta.Pool, delta.Asset)
	}
	bz, err := json.Marshal(delta)
	if err != nil {
		return err
	}
	store.Set(key, bz)
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeMigrationBookHeldDelta,
		sdk.NewAttribute(AttributeKeyPool, delta.Pool),
		sdk.NewAttribute(AttributeKeyAsset, delta.Asset),
		sdk.NewAttribute(AttributeKeyBook, delta.Book.String()),
		sdk.NewAttribute(AttributeKeyHeld, delta.Held.String()),
		sdk.NewAttribute(AttributeKeyDelta, delta.Delta.String())))
	return nil
}

func decodeBookHeldDelta(key, bz []byte) (types.BookHeldDelta, error) {
	var delta types.BookHeldDelta
	if err := json.Unmarshal(bz, &delta); err != nil {
		return types.BookHeldDelta{}, invalidRecord("corrupt book-held delta %x: %v", key, err)
	}
	if err := validBookHeldDelta(delta); err != nil {
		return types.BookHeldDelta{}, err
	}
	return delta, nil
}

// GetBookHeldDelta returns the recorded book-held delta of pool and asset.
func (k *Keeper) GetBookHeldDelta(ctx sdk.Context, pool, asset common.Address) (types.BookHeldDelta, bool, error) {
	key := types.BookHeldDeltaKey(pool, asset)
	bz := k.store(ctx).Get(key)
	if bz == nil {
		return types.BookHeldDelta{}, false, nil
	}
	delta, err := decodeBookHeldDelta(key, bz)
	if err != nil {
		return types.BookHeldDelta{}, false, err
	}
	return delta, true, nil
}

// BookHeldDeltas returns every recorded book-held delta in pool and asset
// order.
func (k *Keeper) BookHeldDeltas(ctx sdk.Context) ([]types.BookHeldDelta, error) {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), types.BookHeldDeltaPrefix)
	defer iterator.Close()
	out := []types.BookHeldDelta{}
	for ; iterator.Valid(); iterator.Next() {
		delta, err := decodeBookHeldDelta(iterator.Key(), iterator.Value())
		if err != nil {
			return nil, err
		}
		out = append(out, delta)
	}
	return out, nil
}

// RecordFeeRightID stores that the generation 1 fee right generation1ID was
// minted again as generation2ID in the generation 2 collection and writes it
// to the migration log.
func (k *Keeper) RecordFeeRightID(ctx sdk.Context, generation1ID, generation2ID uint64) error {
	if generation1ID == 0 || generation2ID == 0 {
		return invalidRecord("fee-right id map %d to %d", generation1ID, generation2ID)
	}
	store := k.store(ctx)
	key := types.FeeRightIDMapKey(generation1ID)
	if store.Has(key) {
		return invalidRecord("fee-right id %d mapped twice", generation1ID)
	}
	mappings, err := k.FeeRightIDMap(ctx)
	if err != nil {
		return err
	}
	for _, mapping := range mappings {
		if mapping.Generation2ID == generation2ID {
			return invalidRecord("generation 2 fee-right id %d mapped twice", generation2ID)
		}
	}
	store.Set(key, binary.BigEndian.AppendUint64(nil, generation2ID))
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeMigrationFeeRightID,
		sdk.NewAttribute(AttributeKeyGeneration1ID, strconv.FormatUint(generation1ID, 10)),
		sdk.NewAttribute(AttributeKeyGeneration2ID, strconv.FormatUint(generation2ID, 10))))
	return nil
}

func decodeFeeRightID(key, bz []byte) (uint64, error) {
	if len(bz) != 8 || binary.BigEndian.Uint64(bz) == 0 {
		return 0, invalidRecord("corrupt fee-right id map entry %x", key)
	}
	return binary.BigEndian.Uint64(bz), nil
}

// GetFeeRightID returns the generation 2 id of the generation 1 fee right
// generation1ID.
func (k *Keeper) GetFeeRightID(ctx sdk.Context, generation1ID uint64) (uint64, bool, error) {
	key := types.FeeRightIDMapKey(generation1ID)
	bz := k.store(ctx).Get(key)
	if bz == nil {
		return 0, false, nil
	}
	id, err := decodeFeeRightID(key, bz)
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// FeeRightIDMap returns the generation 1 to generation 2 fee-right id map in
// generation 1 id order.
func (k *Keeper) FeeRightIDMap(ctx sdk.Context) ([]types.FeeRightIDMapping, error) {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), types.FeeRightIDMapPrefix)
	defer iterator.Close()
	out := []types.FeeRightIDMapping{}
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != len(types.FeeRightIDMapPrefix)+8 {
			return nil, invalidRecord("corrupt fee-right id map key %x", key)
		}
		generation2ID, err := decodeFeeRightID(key, iterator.Value())
		if err != nil {
			return nil, err
		}
		out = append(out, types.FeeRightIDMapping{
			Generation1ID: binary.BigEndian.Uint64(key[len(types.FeeRightIDMapPrefix):]),
			Generation2ID: generation2ID})
	}
	return out, nil
}

func validRetired(entry types.RetiredAddress) error {
	if !canonicalAddress(entry.Address) || !canonicalAddress(entry.Replacement) ||
		entry.Address == entry.Replacement || !retiredRoles[entry.Role] {
		return invalidRecord("retired %s %s", entry.Address, entry.Role)
	}
	return nil
}

// RecordRetired stores a retired generation 1 address with its role and
// generation 2 replacement and writes it to the migration log.
func (k *Keeper) RecordRetired(ctx sdk.Context, entry types.RetiredAddress) error {
	if err := validRetired(entry); err != nil {
		return err
	}
	store := k.store(ctx)
	key := types.RetiredKey(common.HexToAddress(entry.Address))
	if store.Has(key) {
		return invalidRecord("retired %s recorded twice", entry.Address)
	}
	retired, err := k.RetiredAddresses(ctx)
	if err != nil {
		return err
	}
	for _, other := range retired {
		if other.Role == entry.Role {
			return invalidRecord("role %s retired twice", entry.Role)
		}
	}
	bz, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	store.Set(key, bz)
	ctx.EventManager().EmitEvent(sdk.NewEvent(EventTypeMigrationRetired,
		sdk.NewAttribute(AttributeKeyAddress, entry.Address),
		sdk.NewAttribute(AttributeKeyRole, entry.Role),
		sdk.NewAttribute(AttributeKeyReplacement, entry.Replacement)))
	return nil
}

func decodeRetired(key, bz []byte) (types.RetiredAddress, error) {
	var entry types.RetiredAddress
	if err := json.Unmarshal(bz, &entry); err != nil {
		return types.RetiredAddress{}, invalidRecord("corrupt retired entry %x: %v", key, err)
	}
	if err := validRetired(entry); err != nil {
		return types.RetiredAddress{}, err
	}
	return entry, nil
}

// GetRetired returns the retired entry of a generation 1 address.
func (k *Keeper) GetRetired(ctx sdk.Context, address common.Address) (types.RetiredAddress, bool, error) {
	key := types.RetiredKey(address)
	bz := k.store(ctx).Get(key)
	if bz == nil {
		return types.RetiredAddress{}, false, nil
	}
	entry, err := decodeRetired(key, bz)
	if err != nil {
		return types.RetiredAddress{}, false, err
	}
	return entry, true, nil
}

// RetiredAddresses returns the retired list in address order.
func (k *Keeper) RetiredAddresses(ctx sdk.Context) ([]types.RetiredAddress, error) {
	iterator := sdk.KVStorePrefixIterator(k.store(ctx), types.RetiredPrefix)
	defer iterator.Close()
	out := []types.RetiredAddress{}
	for ; iterator.Valid(); iterator.Next() {
		entry, err := decodeRetired(iterator.Key(), iterator.Value())
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}
