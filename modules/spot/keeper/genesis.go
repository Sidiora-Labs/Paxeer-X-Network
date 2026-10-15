package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/spot/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// Cursor bytes of the EndBlock cursors under CursorPrefix.
const (
	activationCursor byte = 0x00
	fillCursor       byte = 0x01
)

// recordKey is prefix followed by each part.
func recordKey(prefix []byte, parts ...[]byte) []byte {
	key := append([]byte{}, prefix...)
	for _, part := range parts {
		key = append(key, part...)
	}
	return key
}

func orderIDBytes(id uint64) []byte { return binary.BigEndian.AppendUint64(nil, id) }

func setRecord(store sdk.KVStore, key []byte, record any) error {
	bz, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("%w: record %x: %v", types.ErrInvalidGenesis, key, err)
	}
	store.Set(key, bz)
	return nil
}

// getRecord decodes the record at key into out and reports whether one is
// stored.
func getRecord(store sdk.KVStore, key []byte, out any) (bool, error) {
	bz := store.Get(key)
	if bz == nil {
		return false, nil
	}
	if err := json.Unmarshal(bz, out); err != nil {
		return false, fmt.Errorf("spot: corrupt record %x: %w", key, err)
	}
	return true, nil
}

// InitGenesis imports every prefix of gs. It panics when gs fails
// validation or a record cannot be written.
func (k *Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) {
	if err := k.initGenesis(ctx, gs); err != nil {
		panic(fmt.Errorf("spot: invalid genesis: %w", err))
	}
}

func (k *Keeper) initGenesis(ctx sdk.Context, gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	store := ctx.KVStore(k.storeKey)
	if err := setRecord(store, types.ParamsKey, gs.Params); err != nil {
		return err
	}
	store.Set(types.NextOrderIDKey, orderIDBytes(gs.NextOrderID))
	store.Set(recordKey(types.CursorPrefix, []byte{activationCursor}), orderIDBytes(gs.Cursors.Activation))
	store.Set(recordKey(types.CursorPrefix, []byte{fillCursor}),
		append([]byte{byte(gs.Cursors.FillIndex)}, orderIDBytes(gs.Cursors.Fill)...))
	for _, index := range []struct {
		prefix []byte
		ids    []uint64
	}{
		{types.ActiveLimitPrefix, gs.ActiveLimitOrders},
		{types.ActiveStopLimitPrefix, gs.ActiveStopLimitOrders},
		{types.ActiveStopLossPrefix, gs.ActiveStopLossOrders},
	} {
		for _, id := range index.ids {
			store.Set(recordKey(index.prefix, orderIDBytes(id)), []byte{})
		}
	}
	for _, entry := range gs.Expiries {
		store.Set(recordKey(types.ExpiryIndexPrefix, orderIDBytes(entry.ExpiresAt), orderIDBytes(entry.OrderID)), []byte{})
	}

	type record struct {
		key   []byte
		value any
	}
	var records []record
	for _, book := range gs.Books {
		records = append(records, record{recordKey(types.BookPrefix, book.Token.Bytes()), book})
	}
	for _, escrow := range gs.Escrows {
		records = append(records, record{recordKey(types.EscrowPrefix, escrow.Token.Bytes()), escrow})
	}
	for _, accrual := range gs.FeeAccruals {
		records = append(records, record{recordKey(types.FeeAccrualPrefix, []byte{byte(accrual.Rail)}, accrual.Token.Bytes()),
			accrual})
	}
	for _, order := range gs.LimitOrders {
		records = append(records, record{recordKey(types.LimitOrderPrefix, orderIDBytes(order.ID)), order})
	}
	for _, order := range gs.StopLimitOrders {
		records = append(records, record{recordKey(types.StopLimitOrderPrefix, orderIDBytes(order.ID)), order})
	}
	for _, order := range gs.StopLossOrders {
		records = append(records, record{recordKey(types.StopLossOrderPrefix, orderIDBytes(order.ID)), order})
	}
	for _, entry := range gs.UserOrders {
		records = append(records, record{recordKey(types.UserOrderPrefix, entry.User.Bytes(), orderIDBytes(entry.OrderID)),
			entry})
	}
	for _, sample := range gs.TwapSamples {
		records = append(records, record{recordKey(types.TwapSamplePrefix, sample.Token.Bytes(),
			binary.BigEndian.AppendUint32(nil, sample.Slot)), sample})
	}
	for _, ring := range gs.TwapRings {
		records = append(records, record{recordKey(types.TwapRingPrefix, ring.Token.Bytes()), ring})
	}
	for _, volume := range gs.Volumes {
		records = append(records, record{recordKey(types.VolumePrefix, volume.Token.Bytes()), volume})
	}
	for _, entry := range gs.OrderMinOutputs {
		records = append(records, record{recordKey(types.OrderMinOutputPrefix, orderIDBytes(entry.OrderID)), entry})
	}
	for _, entry := range gs.FillBackoffs {
		records = append(records, record{recordKey(types.FillBackoffPrefix, orderIDBytes(entry.OrderID)), entry})
	}
	for _, entry := range gs.ActiveCounts {
		records = append(records, record{recordKey(types.ActiveCountPrefix, entry.User.Bytes()), entry})
	}
	for _, r := range records {
		if err := setRecord(store, r.key, r.value); err != nil {
			return err
		}
	}
	return nil
}

// ExportGenesis returns every prefix of the spot store. It panics on a
// corrupt record.
func (k *Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	gs, err := k.exportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return gs
}

// exportRecords decodes every record under prefix in key order.
func exportRecords[T any](store sdk.KVStore, prefix []byte) ([]T, error) {
	iterator := sdk.KVStorePrefixIterator(store, prefix)
	defer iterator.Close()
	out := []T{}
	for ; iterator.Valid(); iterator.Next() {
		var record T
		if err := json.Unmarshal(iterator.Value(), &record); err != nil {
			return nil, fmt.Errorf("spot: corrupt record %x: %w", iterator.Key(), err)
		}
		out = append(out, record)
	}
	return out, nil
}

// exportKeys returns the key suffix after prefix of every entry under it,
// each of exactly length bytes.
func exportKeys(store sdk.KVStore, prefix []byte, length int) ([][]byte, error) {
	iterator := sdk.KVStorePrefixIterator(store, prefix)
	defer iterator.Close()
	out := [][]byte{}
	for ; iterator.Valid(); iterator.Next() {
		suffix := iterator.Key()[len(prefix):]
		if len(suffix) != length {
			return nil, fmt.Errorf("spot: corrupt index key %x", iterator.Key())
		}
		out = append(out, suffix)
	}
	return out, nil
}

func exportIDs(store sdk.KVStore, prefix []byte) ([]uint64, error) {
	keys, err := exportKeys(store, prefix, 8)
	if err != nil {
		return nil, err
	}
	ids := make([]uint64, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, binary.BigEndian.Uint64(key))
	}
	return ids, nil
}

func (k *Keeper) exportGenesis(ctx sdk.Context) (*types.GenesisState, error) {
	store := ctx.KVStore(k.storeKey)
	gs := types.DefaultGenesis()
	params, err := k.GetParams(ctx)
	if err != nil {
		return nil, err
	}
	gs.Params = params
	if bz := store.Get(types.NextOrderIDKey); bz != nil {
		if len(bz) != 8 {
			return nil, fmt.Errorf("spot: corrupt next order id %x", bz)
		}
		gs.NextOrderID = binary.BigEndian.Uint64(bz)
	}
	if bz := store.Get(recordKey(types.CursorPrefix, []byte{activationCursor})); bz != nil {
		if len(bz) != 8 {
			return nil, fmt.Errorf("spot: corrupt activation cursor %x", bz)
		}
		gs.Cursors.Activation = binary.BigEndian.Uint64(bz)
	}
	if bz := store.Get(recordKey(types.CursorPrefix, []byte{fillCursor})); bz != nil {
		if len(bz) != 9 {
			return nil, fmt.Errorf("spot: corrupt fill cursor %x", bz)
		}
		gs.Cursors.FillIndex, gs.Cursors.Fill = types.FillIndex(bz[0]), binary.BigEndian.Uint64(bz[1:])
	}

	if gs.Books, err = exportRecords[types.TokenBook](store, types.BookPrefix); err != nil {
		return nil, err
	}
	if gs.Escrows, err = exportRecords[types.TokenEscrow](store, types.EscrowPrefix); err != nil {
		return nil, err
	}
	if gs.FeeAccruals, err = exportRecords[types.FeeAccrual](store, types.FeeAccrualPrefix); err != nil {
		return nil, err
	}
	if gs.LimitOrders, err = exportRecords[types.LimitOrder](store, types.LimitOrderPrefix); err != nil {
		return nil, err
	}
	if gs.StopLimitOrders, err = exportRecords[types.StopLimitOrder](store, types.StopLimitOrderPrefix); err != nil {
		return nil, err
	}
	if gs.StopLossOrders, err = exportRecords[types.StopLossOrder](store, types.StopLossOrderPrefix); err != nil {
		return nil, err
	}
	if gs.UserOrders, err = exportRecords[types.UserOrder](store, types.UserOrderPrefix); err != nil {
		return nil, err
	}
	if gs.TwapSamples, err = exportRecords[types.TwapSample](store, types.TwapSamplePrefix); err != nil {
		return nil, err
	}
	if gs.TwapRings, err = exportRecords[types.TwapRing](store, types.TwapRingPrefix); err != nil {
		return nil, err
	}
	if gs.Volumes, err = exportRecords[types.TokenVolume](store, types.VolumePrefix); err != nil {
		return nil, err
	}
	if gs.OrderMinOutputs, err = exportRecords[types.OrderMinOutput](store, types.OrderMinOutputPrefix); err != nil {
		return nil, err
	}
	if gs.FillBackoffs, err = exportRecords[types.FillBackoff](store, types.FillBackoffPrefix); err != nil {
		return nil, err
	}
	if gs.ActiveCounts, err = exportRecords[types.ActiveOrderCount](store, types.ActiveCountPrefix); err != nil {
		return nil, err
	}
	if gs.ActiveLimitOrders, err = exportIDs(store, types.ActiveLimitPrefix); err != nil {
		return nil, err
	}
	if gs.ActiveStopLimitOrders, err = exportIDs(store, types.ActiveStopLimitPrefix); err != nil {
		return nil, err
	}
	if gs.ActiveStopLossOrders, err = exportIDs(store, types.ActiveStopLossPrefix); err != nil {
		return nil, err
	}
	expiries, err := exportKeys(store, types.ExpiryIndexPrefix, 16)
	if err != nil {
		return nil, err
	}
	for _, key := range expiries {
		gs.Expiries = append(gs.Expiries, types.OrderExpiry{ExpiresAt: binary.BigEndian.Uint64(key[:8]),
			OrderID: binary.BigEndian.Uint64(key[8:])})
	}
	return gs, nil
}

// tokenBook returns the book of token and whether the vault holds one.
func (k *Keeper) tokenBook(ctx sdk.Context, token common.Address) (types.TokenBook, bool, error) {
	var book types.TokenBook
	found, err := getRecord(ctx.KVStore(k.storeKey), recordKey(types.BookPrefix, token.Bytes()), &book)
	return book, found, err
}

// tokenEscrow returns the escrow reserved by the active orders whose input
// is token, zero when none is recorded.
func (k *Keeper) tokenEscrow(ctx sdk.Context, token common.Address) (sdk.Int, error) {
	var escrow types.TokenEscrow
	found, err := getRecord(ctx.KVStore(k.storeKey), recordKey(types.EscrowPrefix, token.Bytes()), &escrow)
	if err != nil || !found {
		return sdk.ZeroInt(), err
	}
	return escrow.Amount, nil
}
