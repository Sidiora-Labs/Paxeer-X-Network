// Package reconcile is the idempotent backfill worker for core/pnl-tracker. On a
// schedule it scans indexer.swaps past the pnl.reconciler_cursor keyset and folds
// any swaps the realtime consumer missed (e.g. during downtime or webhook loss),
// using the SAME store.FoldTrade as the consumer — so backfill can never
// double-count (ON CONFLICT on the trade id) and realtime + backfill converge.
// Ports workers/reconciler.ts.
package reconcile

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlcache"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// Reconciler folds unreconciled indexer swaps into pnl positions.
type Reconciler struct {
	store     *store.Store
	redis     *goredis.Client
	logger    *slog.Logger
	batchSize int
}

// New builds a Reconciler. A non-positive batch size falls back to 500.
func New(st *store.Store, rdb *goredis.Client, logger *slog.Logger, batchSize int) *Reconciler {
	if batchSize <= 0 {
		batchSize = 500
	}
	return &Reconciler{store: st, redis: rdb, logger: logger, batchSize: batchSize}
}

// RunOnce scans and folds a single batch, advancing the cursor to the last swap
// processed. It returns the number of swaps examined (folded or already-present).
// Forward progress is guaranteed by the (block_number, log_index) keyset, so a
// block larger than the batch is still fully drained over successive ticks.
func (r *Reconciler) RunOnce(ctx context.Context) (int, error) {
	cur, err := r.store.GetCursor(ctx)
	if err != nil {
		return 0, err
	}
	swaps, err := r.store.ListUnreconciledSwaps(ctx, cur, r.batchSize)
	if err != nil {
		return 0, err
	}
	if len(swaps) == 0 {
		return 0, nil
	}

	touched := make(map[string]struct{})
	var last store.IndexerSwap
	processed := 0
	for i := range swaps {
		sw := swaps[i]
		usdl, token := legs(sw)
		inserted, ferr := r.store.FoldTrade(ctx, store.TradeInput{
			ID:             sw.ID,
			UserAddress:    sw.Sender,
			PoolAddress:    sw.PoolAddress,
			TokenAddress:   sw.TokenAddress,
			IsBuy:          sw.IsBuy,
			UsdlAmount:     usdl,
			TokenAmount:    token,
			Price:          sw.Price,
			Fee:            sw.Fee,
			BlockNumber:    sw.BlockNumber,
			BlockTimestamp: sw.BlockTimestamp,
			TxHash:         sw.TxHash,
		})
		if ferr != nil {
			// Advance over what we've durably folded so far before returning, so a
			// single poison row doesn't wedge the cursor on every restart.
			if processed > 0 {
				if cerr := r.store.AdvanceCursor(ctx, last.BlockNumber, last.LogIndex, last.ID, shareddb.NowSeconds()); cerr != nil {
					r.logger.Warn("reconcile: advance cursor on poison-row path failed", slog.Any("err", cerr))
				}
			}
			return processed, fmt.Errorf("reconcile: fold swap %s: %w", sw.ID, ferr)
		}
		if inserted {
			touched[strings.ToLower(sw.Sender)] = struct{}{}
		}
		last = sw
		processed++
	}

	if err := r.store.AdvanceCursor(ctx, last.BlockNumber, last.LogIndex, last.ID, shareddb.NowSeconds()); err != nil {
		return processed, err
	}
	for user := range touched {
		if err := pnlcache.InvalidateUser(ctx, r.redis, user); err != nil {
			r.logger.Warn("reconcile: cache invalidate failed", slog.String("user", user), slog.Any("err", err))
		}
	}
	if processed > 0 {
		r.logger.Info("reconciled swaps", slog.Int("count", processed),
			slog.Int64("throughBlock", last.BlockNumber))
	}
	return processed, nil
}

// legs splits an indexer swap into its USDL and token amounts by direction.
func legs(sw store.IndexerSwap) (usdl, token string) {
	if sw.IsBuy {
		return sw.AmountIn, sw.AmountOut
	}
	return sw.AmountOut, sw.AmountIn
}
