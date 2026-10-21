package store

import (
	"context"
	"errors"
	"time"
)

// ErrIndexerCursorUnavailable reports that the development indexer has not
// recorded a processed block for the requested chain.
var ErrIndexerCursorUnavailable = errors.New("development indexer cursor unavailable")

// DevelopmentIndexerCursor returns the last block the development indexer
// processed for chainID (indexer.cursors, written by core/indexer UpsertCursor).
// It is the source position of the non-production network authority; the
// production authority reads indexer.indexer_committed_heads_v2 instead.
func (s *Store) DevelopmentIndexerCursor(ctx context.Context, chainID int32) (uint64, error) {
	if s == nil || s.pool == nil || ctx == nil {
		return 0, ErrIndexerCursorUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	var height int64
	if err := s.pool.QueryRow(bounded, "SELECT last_processed_block FROM indexer.cursors WHERE chain_id=$1", chainID).Scan(&height); err != nil {
		return 0, ErrIndexerCursorUnavailable
	}
	if height < 0 || height > 9007199254740991 {
		return 0, ErrIndexerCursorUnavailable
	}
	return uint64(height), nil
}
