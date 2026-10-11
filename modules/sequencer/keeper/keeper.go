package keeper

import (
	"bytes"
	"encoding/binary"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

var (
	ErrBatchPosition     = sdkerrors.Register(types.ModuleName, 5, "sequence batch is not tx 0")
	ErrBatchRepeated     = sdkerrors.Register(types.ModuleName, 6, "sequence batch already delivered in this block")
	ErrSeqDiscontinuity  = sdkerrors.Register(types.ModuleName, 7, "sequence batch does not chain from the stored sequence")
	ErrBatchBlockOnly    = sdkerrors.Register(types.ModuleName, 8, "sequence batch is block-only and refused in CheckTx")
	ErrUnknownReceiptKey = sdkerrors.Register(types.ModuleName, 9, "no pinned receipt key for keyId at height")
	ErrCorruptStore      = sdkerrors.Register(types.ModuleName, 10, "corrupt sequencer store entry")
)

// lastBatchHeightKey holds the height of the last delivered batch, u64
// big-endian, so a second batch in one block is refused.
var lastBatchHeightKey = []byte{0x06}

const receiptKeyLength = types.KeyIDLength + 32 + 8 + 8

// Keeper owns the sequencer store: lastSeq, lastSeqHash, the pinned receipt
// keys and leader list, and the height of the last delivered batch.
type Keeper struct {
	storeKey sdk.StoreKey
}

func NewKeeper(storeKey sdk.StoreKey) Keeper { return Keeper{storeKey: storeKey} }

// store reads and writes the module store without charging gas: the batch is
// fee-free and its bookkeeping never draws on a transaction gas meter.
func (k Keeper) store(ctx sdk.Context) sdk.KVStore {
	return ctx.WithGasMeter(storetypes.NewNoConsumptionInfiniteGasMeter()).KVStore(k.storeKey)
}

// RefuseCheckTx refuses the batch outside block delivery. The batch enters a
// block only as tx 0 injected by the proposer, never through the mempool.
func RefuseCheckTx(ctx sdk.Context) error {
	if ctx.IsCheckTx() || ctx.IsReCheckTx() {
		return ErrBatchBlockOnly.Wrapf("height %d", ctx.BlockHeight())
	}
	return nil
}

// DeliverSequenceBatch applies the batch carried by tx 0 of the block: it
// refuses it at any other position, a second time in one block, for another
// height, with a range or prevSeqHash that does not chain from the stored
// lastSeq and lastSeqHash, or under a receipt key not pinned for the height;
// then it stores toSeq and lastSeqHash.
func (k Keeper) DeliverSequenceBatch(ctx sdk.Context, msg types.MsgSequenceBatch) error {
	if err := RefuseCheckTx(ctx); err != nil {
		return err
	}
	if ctx.TxIndex() != 0 {
		return ErrBatchPosition.Wrapf("tx index %d", ctx.TxIndex())
	}
	if ctx.BlockHeight() < 1 {
		return types.ErrInvalidSequenceBatch.Wrapf("block height %d", ctx.BlockHeight())
	}
	height := uint64(ctx.BlockHeight())
	if msg.Height != height {
		return types.ErrInvalidSequenceBatch.Wrapf("height field %d differs from block height %d", msg.Height, height)
	}
	lastBatchHeight, err := k.readU64(ctx, lastBatchHeightKey)
	if err != nil {
		return err
	}
	if lastBatchHeight >= height {
		return ErrBatchRepeated.Wrapf("last batch at height %d, block height %d", lastBatchHeight, height)
	}
	if err := msg.ValidateBasic(); err != nil {
		return err
	}
	lastSeq, err := k.LastSeq(ctx)
	if err != nil {
		return err
	}
	lastSeqHash, err := k.LastSeqHash(ctx)
	if err != nil {
		return err
	}
	if msg.FromSeq != lastSeq+1 {
		return ErrSeqDiscontinuity.Wrapf("fromSeq %d, stored lastSeq %d", msg.FromSeq, lastSeq)
	}
	if !bytes.Equal(msg.PrevSeqHash, lastSeqHash[:]) {
		return ErrSeqDiscontinuity.Wrapf("prevSeqHash %X differs from stored lastSeqHash %X", msg.PrevSeqHash, lastSeqHash)
	}
	key, err := k.receiptKeyAt(ctx, msg.KeyId, height)
	if err != nil {
		return err
	}
	if err := msg.VerifySignature(ctx.ChainID(), key.PublicKey); err != nil {
		return err
	}
	store := k.store(ctx)
	store.Set(types.LastSeqKey, binary.BigEndian.AppendUint64(nil, msg.ToSeq))
	store.Set(types.LastSeqHashKey, bytes.Clone(msg.LastSeqHash))
	store.Set(lastBatchHeightKey, binary.BigEndian.AppendUint64(nil, height))
	return nil
}

// LastSeq returns the last committed sequence position; 0 before the first
// batch.
func (k Keeper) LastSeq(ctx sdk.Context) (uint64, error) {
	return k.readU64(ctx, types.LastSeqKey)
}

// LastSeqHash returns the seqHash of LastSeq; 32 zero bytes before the first
// batch.
func (k Keeper) LastSeqHash(ctx sdk.Context) ([types.HashLength]byte, error) {
	var hash [types.HashLength]byte
	bz := k.store(ctx).Get(types.LastSeqHashKey)
	if bz == nil {
		return hash, nil
	}
	if len(bz) != types.HashLength {
		return hash, ErrCorruptStore.Wrapf("lastSeqHash is %d bytes", len(bz))
	}
	copy(hash[:], bz)
	return hash, nil
}

// ReceiptKeys returns the pinned receipt keys in pinned order.
func (k Keeper) ReceiptKeys(ctx sdk.Context) ([]types.ReceiptKey, error) {
	bz := k.store(ctx).Get(types.ReceiptKeysKey)
	if len(bz)%receiptKeyLength != 0 {
		return nil, ErrCorruptStore.Wrapf("receipt keys entry is %d bytes", len(bz))
	}
	keys := make([]types.ReceiptKey, 0, len(bz)/receiptKeyLength)
	for offset := 0; offset < len(bz); offset += receiptKeyLength {
		entry := bz[offset : offset+receiptKeyLength]
		var key types.ReceiptKey
		copy(key.KeyId[:], entry[:types.KeyIDLength])
		copy(key.PublicKey[:], entry[types.KeyIDLength:types.KeyIDLength+32])
		key.FirstHeight = binary.BigEndian.Uint64(entry[types.KeyIDLength+32:])
		key.LastHeight = binary.BigEndian.Uint64(entry[types.KeyIDLength+40:])
		keys = append(keys, key)
	}
	return keys, nil
}

// Leaders returns the pinned leader list L, primary first.
func (k Keeper) Leaders(ctx sdk.Context) ([][32]byte, error) {
	bz := k.store(ctx).Get(types.LeaderListKey)
	if len(bz)%32 != 0 {
		return nil, ErrCorruptStore.Wrapf("leader list entry is %d bytes", len(bz))
	}
	leaders := make([][32]byte, len(bz)/32)
	for i := range leaders {
		copy(leaders[i][:], bz[i*32:])
	}
	return leaders, nil
}

// InitGenesis writes the genesis: L, the receipt keys, lastSeq and
// lastSeqHash.
func (k Keeper) InitGenesis(ctx sdk.Context, gs types.GenesisState) error {
	if err := gs.Validate(); err != nil {
		return err
	}
	leaders := make([]byte, 0, len(gs.Leaders)*32)
	for _, leader := range gs.Leaders {
		leaders = append(leaders, leader[:]...)
	}
	store := k.store(ctx)
	store.Set(types.LeaderListKey, leaders)
	store.Set(types.ReceiptKeysKey, encodeReceiptKeys(gs.ReceiptKeys))
	store.Set(types.LastSeqKey, binary.BigEndian.AppendUint64(nil, gs.LastSeq))
	store.Set(types.LastSeqHashKey, bytes.Clone(gs.LastSeqHash[:]))
	return nil
}

// SetReceiptKeys replaces the pinned receipt keys with the governed list, so
// the batch signature check reads the key set the ReceiptKeys param holds
// after a rotation.
func (k Keeper) SetReceiptKeys(ctx sdk.Context, keys ReceiptKeys) error {
	if err := keys.Validate(); err != nil {
		return err
	}
	pinned := make([]types.ReceiptKey, len(keys))
	for i, entry := range keys {
		copy(pinned[i].KeyId[:], entry.KeyId)
		copy(pinned[i].PublicKey[:], entry.PublicKey)
		pinned[i].FirstHeight = entry.FirstHeight
		pinned[i].LastHeight = entry.LastHeight
	}
	k.store(ctx).Set(types.ReceiptKeysKey, encodeReceiptKeys(pinned))
	return nil
}

// ExportGenesis returns the leader list, the receipt keys, lastSeq and
// lastSeqHash.
func (k Keeper) ExportGenesis(ctx sdk.Context) (types.GenesisState, error) {
	leaders, err := k.Leaders(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	keys, err := k.ReceiptKeys(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	lastSeq, err := k.LastSeq(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	lastSeqHash, err := k.LastSeqHash(ctx)
	if err != nil {
		return types.GenesisState{}, err
	}
	return types.GenesisState{Leaders: leaders, ReceiptKeys: keys, LastSeq: lastSeq, LastSeqHash: lastSeqHash}, nil
}

func encodeReceiptKeys(keys []types.ReceiptKey) []byte {
	bz := make([]byte, 0, len(keys)*receiptKeyLength)
	for _, key := range keys {
		bz = append(bz, key.KeyId[:]...)
		bz = append(bz, key.PublicKey[:]...)
		bz = binary.BigEndian.AppendUint64(bz, key.FirstHeight)
		bz = binary.BigEndian.AppendUint64(bz, key.LastHeight)
	}
	return bz
}

func (k Keeper) receiptKeyAt(ctx sdk.Context, keyID []byte, height uint64) (types.ReceiptKey, error) {
	keys, err := k.ReceiptKeys(ctx)
	if err != nil {
		return types.ReceiptKey{}, err
	}
	for _, key := range keys {
		if bytes.Equal(key.KeyId[:], keyID) && key.Covers(height) {
			return key, nil
		}
	}
	return types.ReceiptKey{}, ErrUnknownReceiptKey.Wrapf("keyId %X at height %d", keyID, height)
}

func (k Keeper) readU64(ctx sdk.Context, key []byte) (uint64, error) {
	bz := k.store(ctx).Get(key)
	if bz == nil {
		return 0, nil
	}
	if len(bz) != 8 {
		return 0, ErrCorruptStore.Wrapf("key %X holds %d bytes, want 8", key, len(bz))
	}
	return binary.BigEndian.Uint64(bz), nil
}
