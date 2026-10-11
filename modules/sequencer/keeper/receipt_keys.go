package keeper

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	paramtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/params/types"
)

// KeyReceiptKeys is the sequencer param subspace key of the pinned receipt
// key list.
var KeyReceiptKeys = []byte("ReceiptKeys")

var (
	// ErrInvalidReceiptKey refuses an entry with a malformed keyId or public
	// key, or a keyId pinned twice.
	ErrInvalidReceiptKey = errors.New("sequencer: invalid receipt key")
	// ErrReceiptKeyRange refuses an entry whose firstHeight is above its
	// lastHeight.
	ErrReceiptKeyRange = errors.New("sequencer: receipt key range has firstHeight above lastHeight")
	// ErrReceiptKeyNotPinned refuses a keyId that no entry pins at the height.
	ErrReceiptKeyNotPinned = errors.New("sequencer: receipt key not pinned at height")
	// ErrReceiptKeyRotation refuses a change that rewrites the pinned keys of
	// a height at or below the current block.
	ErrReceiptKeyRotation = errors.New("sequencer: receipt key change rewrites a past height")
	// ErrReceiptBatchID refuses a receipt whose batch_id is not 24 zero bytes
	// followed by the big-endian block height.
	ErrReceiptBatchID = errors.New("sequencer: receipt batch_id is not a block height")
)

// ReceiptKeyEntry pins one ed25519 receipt public key under keyId for the
// inclusive block height range [FirstHeight, LastHeight]. Receipts signed with
// the key carry context_hash = KeyId.
type ReceiptKeyEntry struct {
	KeyId       []byte `json:"key_id" yaml:"key_id"`
	PublicKey   []byte `json:"public_key" yaml:"public_key"`
	FirstHeight uint64 `json:"first_height" yaml:"first_height"`
	LastHeight  uint64 `json:"last_height" yaml:"last_height"`
}

// Validate applies the range rule of the proof crate's from_config: a 32-byte
// non-zero keyId, a canonical 32-byte ed25519 public key and firstHeight not
// above lastHeight.
func (e ReceiptKeyEntry) Validate() error {
	if len(e.KeyId) != types.KeyIDLength {
		return fmt.Errorf("%w: keyId is %d bytes, want %d", ErrInvalidReceiptKey, len(e.KeyId), types.KeyIDLength)
	}
	if bytes.Equal(e.KeyId, make([]byte, types.KeyIDLength)) {
		return fmt.Errorf("%w: keyId is zero", ErrInvalidReceiptKey)
	}
	if len(e.PublicKey) != 32 {
		return fmt.Errorf("%w: keyId %X public key is %d bytes, want 32", ErrInvalidReceiptKey, e.KeyId, len(e.PublicKey))
	}
	if !verify.PublicKeyIsCanonical([32]byte(e.PublicKey)) {
		return fmt.Errorf("%w: keyId %X public key is not a canonical ed25519 key", ErrInvalidReceiptKey, e.KeyId)
	}
	if e.FirstHeight > e.LastHeight {
		return fmt.Errorf("%w: keyId %X range %d-%d", ErrReceiptKeyRange, e.KeyId, e.FirstHeight, e.LastHeight)
	}
	return nil
}

// covers reports whether height lies in the entry's inclusive range.
func (e ReceiptKeyEntry) covers(height uint64) bool {
	return e.FirstHeight <= height && height <= e.LastHeight
}

// ReceiptKeys is the governed list of pinned receipt keys. Ranges of distinct
// keys may overlap: the primary and standby sequencer hosts each hold a key.
type ReceiptKeys []ReceiptKeyEntry

// Validate checks every entry and refuses a keyId pinned twice.
func (keys ReceiptKeys) Validate() error {
	seen := make(map[string]struct{}, len(keys))
	for _, entry := range keys {
		if err := entry.Validate(); err != nil {
			return err
		}
		if _, ok := seen[string(entry.KeyId)]; ok {
			return fmt.Errorf("%w: keyId %X pinned twice", ErrInvalidReceiptKey, entry.KeyId)
		}
		seen[string(entry.KeyId)] = struct{}{}
	}
	return nil
}

// KeyForHeight returns the entry pinning keyID whose range covers height: the
// key a receipt with context_hash = keyID and batch_id height = height must
// verify under.
func (keys ReceiptKeys) KeyForHeight(keyID [32]byte, height uint64) (ReceiptKeyEntry, error) {
	for _, entry := range keys {
		if bytes.Equal(entry.KeyId, keyID[:]) {
			if !entry.covers(height) {
				return ReceiptKeyEntry{}, fmt.Errorf("%w: keyId %X range %d-%d excludes height %d", ErrReceiptKeyNotPinned, keyID, entry.FirstHeight, entry.LastHeight, height)
			}
			return entry, nil
		}
	}
	return ReceiptKeyEntry{}, fmt.Errorf("%w: keyId %X is not in the list at height %d", ErrReceiptKeyNotPinned, keyID, height)
}

// VerifyReceipt authenticates canonical receipt bytes against the list: the
// batch_id names the block height, context_hash names the keyId, the entry
// for that keyId must cover the height, and the sequencer signature must
// verify under the entry's public key.
func (keys ReceiptKeys) VerifyReceipt(receiptBytes []byte) (*verify.VerifiedReceipt, error) {
	receipt, err := codec.DecodeReceipt(receiptBytes)
	if err != nil {
		return nil, err
	}
	height, err := ReceiptHeight(receipt)
	if err != nil {
		return nil, err
	}
	entry, err := keys.KeyForHeight(receipt.ContextHash, height)
	if err != nil {
		return nil, err
	}
	if err := entry.Validate(); err != nil {
		return nil, err
	}
	return verify.ReceiptSignature(receiptBytes, [32]byte(entry.PublicKey))
}

// ReceiptHeight decodes the block height a receipt's batch_id carries:
// 24 zero bytes followed by the big-endian height.
func ReceiptHeight(receipt *codec.Receipt) (uint64, error) {
	if !bytes.Equal(receipt.BatchID[:24], make([]byte, 24)) {
		return 0, fmt.Errorf("%w: %X", ErrReceiptBatchID, receipt.BatchID)
	}
	return binary.BigEndian.Uint64(receipt.BatchID[24:]), nil
}

// ValidateRotation checks a governed change from current to next at block
// height: an existing keyId keeps its public key and firstHeight, the
// coverage of every height at or below height stays as it was, and a new
// keyId starts above height. Entries may be removed.
func ValidateRotation(current, next ReceiptKeys, height int64) error {
	if height < 0 {
		return fmt.Errorf("%w: negative block height %d", ErrReceiptKeyRotation, height)
	}
	h := uint64(height)
	pinned := make(map[string]ReceiptKeyEntry, len(current))
	for _, entry := range current {
		pinned[string(entry.KeyId)] = entry
	}
	for _, entry := range next {
		old, ok := pinned[string(entry.KeyId)]
		if !ok {
			if entry.FirstHeight <= h {
				return fmt.Errorf("%w: new keyId %X starts at %d, not above height %d", ErrReceiptKeyRotation, entry.KeyId, entry.FirstHeight, h)
			}
			continue
		}
		if !bytes.Equal(old.PublicKey, entry.PublicKey) || old.FirstHeight != entry.FirstHeight {
			return fmt.Errorf("%w: keyId %X changes its public key or firstHeight", ErrReceiptKeyRotation, entry.KeyId)
		}
		if min(old.LastHeight, h) != min(entry.LastHeight, h) {
			return fmt.Errorf("%w: keyId %X lastHeight %d to %d at height %d", ErrReceiptKeyRotation, entry.KeyId, old.LastHeight, entry.LastHeight, h)
		}
	}
	return nil
}

// ParamKeyTable registers ReceiptKeys in the sequencer param subspace.
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable(paramtypes.NewParamSetPair(KeyReceiptKeys, &ReceiptKeys{}, validateReceiptKeys))
}

func validateReceiptKeys(value interface{}) error {
	keys, ok := value.(ReceiptKeys)
	if !ok {
		return fmt.Errorf("%w: invalid parameter type %T", ErrInvalidReceiptKey, value)
	}
	return keys.Validate()
}

// GetReceiptKeys reads the pinned list; an unset list is empty.
func GetReceiptKeys(ctx sdk.Context, paramSpace paramtypes.Subspace) ReceiptKeys {
	var keys ReceiptKeys
	paramSpace.GetIfExists(ctx, KeyReceiptKeys, &keys)
	return keys
}

// InitReceiptKeys writes the genesis list after validating it. Every later
// change goes through a governance param change.
func InitReceiptKeys(ctx sdk.Context, paramSpace paramtypes.Subspace, keys ReceiptKeys) error {
	if err := keys.Validate(); err != nil {
		return err
	}
	paramSpace.Set(ctx, KeyReceiptKeys, keys)
	return nil
}
