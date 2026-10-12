package types

import (
	"context"
	"time"
)

// SequencerSource supplies the sealed pending block a leader proposes in
// sequencer mode.
type SequencerSource interface {
	// Sequenced reports whether the block at height is built from the sealed
	// slice; it is false below the activation height.
	Sequenced(height int64) bool
	// Seal closes the pending block for height and returns it.
	Seal(ctx context.Context, height int64) (SealedBlock, error)
}

// SealedBlock is a sealed pending block: the encoded MsgSequenceBatch, the
// executed transactions in seq order and the time Open fixed for the height.
type SealedBlock struct {
	Batch    Tx
	Txs      Txs
	OpenTime time.Time
}
