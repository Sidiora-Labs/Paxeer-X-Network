package engine

// TxMarker is the pre-broadcast idempotency record for a sign-tx task —
// engine-owned metadata read during crash recovery, distinct from a handler's
// TaskResult.Result. It carries the signed tx bytes so a re-run re-broadcasts
// the identical tx rather than re-signing (which risks a double submit).
type TxMarker struct {
	TaskID        string
	TxHash        string
	TxBytes       []byte
	AccountNumber uint64
	Sequence      uint64
	ChainID       string
}

// Checkpointer persists a TxMarker durably before broadcast and retrieves it on
// re-execution, so a crash between broadcast and result-persist re-adopts the
// in-flight tx instead of signing a second one. Nil when no durable store is
// configured (handlers then broadcast without the guard, and log it).
type Checkpointer interface {
	SaveTxMarker(m *TxMarker) error
	// GetTxMarker returns (nil, nil) when no marker exists for taskID.
	GetTxMarker(taskID string) (*TxMarker, error)
}
