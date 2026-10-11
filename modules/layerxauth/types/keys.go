package types

import "encoding/binary"

const (
	// ModuleName defines the module name.
	ModuleName = "layerxauth"

	// StoreKey defines the primary module store key.
	StoreKey = ModuleName

	// RouterKey is the message route.
	RouterKey = ModuleName

	// QuerierRoute defines the module's query routing key.
	QuerierRoute = ModuleName
)

var (
	ParamsKey                   = []byte{0x01}
	IdempotencyPrefix           = []byte{0x02}
	IdempotencyBySequencePrefix = []byte{0x03}
)

// IdempotencyKey indexes one idempotency entry by the 32-byte context hash of
// u32be(len(actor_did)) || actor_did || idempotency_key.
func IdempotencyKey(keyHash [32]byte) []byte {
	out := make([]byte, 0, len(IdempotencyPrefix)+32)
	out = append(out, IdempotencyPrefix...)
	return append(out, keyHash[:]...)
}

// IdempotencyBySequenceKey orders idempotency entries by the global sequence
// they were recorded at, so pruning walks the oldest entries first.
func IdempotencyBySequenceKey(recordedSequence uint64, keyHash [32]byte) []byte {
	out := make([]byte, 0, len(IdempotencyBySequencePrefix)+8+32)
	out = append(out, IdempotencyBySequencePrefix...)
	out = binary.BigEndian.AppendUint64(out, recordedSequence)
	return append(out, keyHash[:]...)
}
