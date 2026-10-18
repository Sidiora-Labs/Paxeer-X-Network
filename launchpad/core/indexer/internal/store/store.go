// Package store wraps the generated sqlc queries with ergonomic, not-found-aware
// methods used by the block processor and backfill processor. All inserts are
// idempotent (ON CONFLICT DO NOTHING / cursor upsert) so redelivery and restart
// are safe (invariant i9). Money/amount fields are persisted verbatim as text
// (no float — invariant i1).
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/db/sqlcdb"
)

// Store is the indexer's persistence layer over a pgx pool.
type Store struct {
	lifecycle bool
	q         *sqlcdb.Queries
	db        sqlcdb.DBTX
}

// New builds a Store from a pgx pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{q: sqlcdb.New(pool), db: pool}
}

// NewFromQuerier builds a Store from any sqlc DBTX (pool, conn, or tx). Useful
// for tests that share a single connection.
func NewFromDBTX(db sqlcdb.DBTX) *Store {
	return &Store{q: sqlcdb.New(db), db: db}
}

// GetCursor returns the last processed block for chainID, or nil when no cursor
// row exists yet.
func (s *Store) GetCursor(ctx context.Context, chainID int32) (*int64, error) {
	c, err := s.q.GetCursor(ctx, chainID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	v := c.LastProcessedBlock
	return &v, nil
}

// UpsertCursor advances (or creates) the cursor for chainID.
func (s *Store) UpsertCursor(ctx context.Context, chainID int32, lastProcessedBlock int64) error {
	return s.q.UpsertCursor(ctx, sqlcdb.UpsertCursorParams{
		ChainID:            chainID,
		LastProcessedBlock: lastProcessedBlock,
	})
}

// GetPoolByPoolID returns the pool for a 0x-hex poolId, or nil when unknown.
func (s *Store) GetPoolByPoolID(ctx context.Context, poolID string) (*sqlcdb.IndexerPool, error) {
	p, err := s.q.GetPoolByPoolID(ctx, poolID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetPoolByAddress returns the pool registered at a 0x-hex address (case
// insensitive), or nil when unknown.
func (s *Store) GetPoolByAddress(ctx context.Context, poolAddress string) (*sqlcdb.IndexerPool, error) {
	p, err := s.q.GetPoolByAddress(ctx, poolAddress)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetPoolByNftID returns the pool bound to a SidioraNFT id, or nil when no
// registered pool carries that id.
func (s *Store) GetPoolByNftID(ctx context.Context, nftID int64) (*sqlcdb.IndexerPool, error) {
	p, err := s.q.GetPoolByNftID(ctx, nftID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// PoolCount / SwapCount back the /status endpoint.
func (s *Store) PoolCount(ctx context.Context) (int64, error) { return s.q.GetPoolCount(ctx) }
func (s *Store) SwapCount(ctx context.Context) (int64, error) { return s.q.GetSwapCount(ctx) }

// InsertPool persists a MarketCreated pool row.
func (s *Store) InsertPool(ctx context.Context, p sqlcdb.InsertPoolParams) error {
	return s.q.InsertPool(ctx, p)
}

// InsertSwap persists a Swap row.
func (s *Store) InsertSwap(ctx context.Context, p sqlcdb.InsertSwapParams) error {
	return s.q.InsertSwap(ctx, p)
}

// InsertPoolStateSnapshot persists a PoolStateUpdated snapshot.
func (s *Store) InsertPoolStateSnapshot(ctx context.Context, p sqlcdb.InsertPoolStateSnapshotParams) error {
	return s.q.InsertPoolStateSnapshot(ctx, p)
}

// InsertFeeEvent persists a FeeRecorded row.
func (s *Store) InsertFeeEvent(ctx context.Context, p sqlcdb.InsertFeeEventParams) error {
	return s.q.InsertFeeEvent(ctx, p)
}

// InsertFeeDistribution persists a FeeDistributed row.
func (s *Store) InsertFeeDistribution(ctx context.Context, p sqlcdb.InsertFeeDistributionParams) error {
	return s.q.InsertFeeDistribution(ctx, p)
}

// InsertFeeStrategyChange persists a FeeStrategyChanged row.
func (s *Store) InsertFeeStrategyChange(ctx context.Context, p sqlcdb.InsertFeeStrategyChangeParams) error {
	return s.q.InsertFeeStrategyChange(ctx, p)
}

// InsertAirdropClaim persists a holder airdrop claim (native AirdropClaimed or
// the EventEmitter FeeFlow kind=4 mirror).
func (s *Store) InsertAirdropClaim(ctx context.Context, p sqlcdb.InsertAirdropClaimParams) error {
	return s.q.InsertAirdropClaim(ctx, p)
}

// InsertOpticalExecution persists an OpticalExecuted row.
func (s *Store) InsertOpticalExecution(ctx context.Context, p sqlcdb.InsertOpticalExecutionParams) error {
	return s.q.InsertOpticalExecution(ctx, p)
}

// InsertTokenForTokenSwap persists a TokenForTokenSwap row.
func (s *Store) InsertTokenForTokenSwap(ctx context.Context, p sqlcdb.InsertTokenForTokenSwapParams) error {
	return s.q.InsertTokenForTokenSwap(ctx, p)
}

// InsertConfigUpdate persists a ConfigUpdated row.
func (s *Store) InsertConfigUpdate(ctx context.Context, p sqlcdb.InsertConfigUpdateParams) error {
	return s.q.InsertConfigUpdate(ctx, p)
}

// ── Backfill job tracking ──────────────────────────────────────────

// ActiveBackfillJob returns the running backfill job, or nil when none is
// active (so a fresh run starts a new job).
func (s *Store) ActiveBackfillJob(ctx context.Context) (*sqlcdb.IndexerBackfillJob, error) {
	j, err := s.q.GetActiveBackfillJob(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &j, nil
}

// InsertBackfillJob records a new running backfill job.
func (s *Store) InsertBackfillJob(ctx context.Context, p sqlcdb.InsertBackfillJobParams) error {
	return s.q.InsertBackfillJob(ctx, p)
}

// UpdateBackfillProgress advances a job's last processed block.
func (s *Store) UpdateBackfillProgress(ctx context.Context, id string, lastProcessedBlock int64) error {
	return s.q.UpdateBackfillProgress(ctx, sqlcdb.UpdateBackfillProgressParams{
		ID:                 id,
		LastProcessedBlock: lastProcessedBlock,
	})
}

// CompleteBackfillJob marks a job completed.
func (s *Store) CompleteBackfillJob(ctx context.Context, id string) error {
	return s.q.CompleteBackfillJob(ctx, id)
}

// FailBackfillJob marks a job failed with an error message.
func (s *Store) FailBackfillJob(ctx context.Context, id, errorMessage string) error {
	return s.q.FailBackfillJob(ctx, sqlcdb.FailBackfillJobParams{
		ID:           id,
		ErrorMessage: &errorMessage,
	})
}
