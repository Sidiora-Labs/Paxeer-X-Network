package keeper

import (
	"encoding/binary"
	"math"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// SyntheticReceiptKind names one chain-generated receipt written once per block
// outside every transaction: the perps and spot end-blockers and the v6.12.0 activation.
type SyntheticReceiptKind uint8

const (
	SyntheticReceiptPerps      SyntheticReceiptKind = 1
	SyntheticReceiptSpot       SyntheticReceiptKind = 2
	SyntheticReceiptActivation SyntheticReceiptKind = 3
)

// SyntheticReceiptKinds lists every kind in index order.
var SyntheticReceiptKinds = [...]SyntheticReceiptKind{SyntheticReceiptPerps, SyntheticReceiptSpot, SyntheticReceiptActivation}

func (kind SyntheticReceiptKind) tag() (string, bool) {
	switch kind {
	case SyntheticReceiptPerps:
		return "perps-endblock", true
	case SyntheticReceiptSpot:
		return "spot-endblock", true
	case SyntheticReceiptActivation:
		return "v6120-activation", true
	default:
		return "", false
	}
}

// SyntheticReceiptHash returns keccak256(tag || height as 8 bytes big-endian), or the zero
// hash for an unknown kind.
func SyntheticReceiptHash(kind SyntheticReceiptKind, height int64) common.Hash {
	tag, ok := kind.tag()
	if !ok {
		return common.Hash{}
	}
	buf := make([]byte, len(tag)+8)
	copy(buf, tag)
	binary.BigEndian.PutUint64(buf[len(tag):], uint64(height)) //nolint:gosec
	return crypto.Keccak256Hash(buf)
}

// SyntheticReceiptProvisionalIndex is the transient index a kind writes at during the block:
// math.MaxUint32 - 3 + kind, outside every real transaction index.
func SyntheticReceiptProvisionalIndex(kind SyntheticReceiptKind) uint32 {
	return math.MaxUint32 - 3 + uint32(kind)
}

// SyntheticReceiptContext returns ctx carrying the kind's hash and provisional index, so every
// CallEVM of one kind in one block merges its logs into a single receipt.
func SyntheticReceiptContext(ctx sdk.Context, kind SyntheticReceiptKind) sdk.Context {
	return ctx.WithTxSum(SyntheticReceiptHash(kind, ctx.BlockHeight())).WithTxIndex(int(SyntheticReceiptProvisionalIndex(kind)))
}

// SyntheticReceiptKindOf reports the kind whose hash at height equals hash.
func SyntheticReceiptKindOf(hash common.Hash, height int64) (SyntheticReceiptKind, bool) {
	for _, kind := range SyntheticReceiptKinds {
		if SyntheticReceiptHash(kind, height) == hash {
			return kind, true
		}
	}
	return 0, false
}
