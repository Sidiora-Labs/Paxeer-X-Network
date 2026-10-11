package types

import (
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
)

var ErrInvalidGenesis = sdkerrors.Register(ModuleName, 4, "invalid sequencer genesis")

// ReceiptKey pins one ed25519 receipt key to the inclusive height range
// [FirstHeight, LastHeight] in which it signs sequence batches.
type ReceiptKey struct {
	KeyId       [KeyIDLength]byte
	PublicKey   [32]byte
	FirstHeight uint64
	LastHeight  uint64
}

// Covers reports whether height lies in the key's range.
func (k ReceiptKey) Covers(height uint64) bool {
	return k.FirstHeight <= height && height <= k.LastHeight
}

// GenesisState is the sequencer module state written at the activation
// height: the pinned leader list L (validator consensus public keys, primary
// first), the pinned receipt keys and lastSeq 0. lastSeqHash starts as 32 zero
// bytes.
type GenesisState struct {
	Leaders     [][32]byte
	ReceiptKeys []ReceiptKey
	LastSeq     uint64
}

// Validate checks the activation genesis: a non-empty leader list with no zero
// or repeated key, a non-empty receipt key list whose keyIds are non-zero and
// distinct, whose public keys are canonical ed25519 encodings and whose
// ranges start at height 1 or later and are not inverted, and lastSeq 0.
func (gs GenesisState) Validate() error {
	if len(gs.Leaders) == 0 {
		return ErrInvalidGenesis.Wrap("leader list is empty")
	}
	leaders := make(map[[32]byte]struct{}, len(gs.Leaders))
	for i, leader := range gs.Leaders {
		if leader == ([32]byte{}) {
			return ErrInvalidGenesis.Wrapf("leader %d is the zero key", i)
		}
		if _, ok := leaders[leader]; ok {
			return ErrInvalidGenesis.Wrapf("leader %d repeats an earlier leader", i)
		}
		leaders[leader] = struct{}{}
	}
	if len(gs.ReceiptKeys) == 0 {
		return ErrInvalidGenesis.Wrap("receipt key list is empty")
	}
	keyIDs := make(map[[KeyIDLength]byte]struct{}, len(gs.ReceiptKeys))
	for i, key := range gs.ReceiptKeys {
		if key.KeyId == ([KeyIDLength]byte{}) {
			return ErrInvalidGenesis.Wrapf("receipt key %d has the zero keyId", i)
		}
		if _, ok := keyIDs[key.KeyId]; ok {
			return ErrInvalidGenesis.Wrapf("receipt key %d repeats an earlier keyId", i)
		}
		keyIDs[key.KeyId] = struct{}{}
		if !verify.PublicKeyIsCanonical(key.PublicKey) {
			return ErrInvalidGenesis.Wrapf("receipt key %d public key is not a canonical ed25519 key", i)
		}
		if key.FirstHeight == 0 {
			return ErrInvalidGenesis.Wrapf("receipt key %d firstHeight is zero", i)
		}
		if key.FirstHeight > key.LastHeight {
			return ErrInvalidGenesis.Wrapf("receipt key %d firstHeight %d above lastHeight %d", i, key.FirstHeight, key.LastHeight)
		}
	}
	if gs.LastSeq != 0 {
		return ErrInvalidGenesis.Wrapf("lastSeq is %d, want 0 at activation", gs.LastSeq)
	}
	return nil
}
