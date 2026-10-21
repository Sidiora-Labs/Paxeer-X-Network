package consumer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	goredis "github.com/redis/go-redis/v9"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// SwapEvent is a decoded Swap webhook event (the fields the stats consumers use).
type SwapEvent struct {
	PoolAddress    string
	Sender         string
	IsBuy          bool
	AmountIn       string
	AmountOut      string
	Price          string
	Fee            string
	BlockTimestamp int64
	TxHash         string
	LogIndex       int
}

// SwapConsumer maintains transactions, volume buckets, price snapshots/changes
// and the pool_stats price/mcap/high-low for each swap. Ports StatsSwapConsumer.
type SwapConsumer struct {
	store  *store.Store
	redis  *goredis.Client
	logger *slog.Logger
}

// NewSwapConsumer builds a SwapConsumer.
func NewSwapConsumer(st *store.Store, rdb *goredis.Client, logger *slog.Logger) *SwapConsumer {
	return &SwapConsumer{store: st, redis: rdb, logger: logger}
}

// ProcessEvent applies one Swap event end to end, in the same ordered sequence as
// the TS StatsSwapConsumer.processEvent: (1) record the transaction, (2) update
// the 1-minute volume bucket + trader, (3) recompute rolling stats, (4) snapshot
// the price, (5) compute price changes, (6) update price/mcap/high-low, (7) cache.
func (c *SwapConsumer) ProcessEvent(ctx context.Context, ev SwapEvent) error {
	if _, err := project(ctx, c.store, ev, false); err != nil {
		return err
	}
	// 7. Cache the fresh row in Redis.
	return cachePoolStats(ctx, c.store, c.redis, ev.PoolAddress)
}

// ProjectTx applies steps 1-6 of ProcessEvent inside the caller's transaction:
// the pool_transactions row (the "<txHash>-<logIndex>" inbox), volume bucket and
// bucket trader, rolling stats, price snapshot and pool_stats price/changes all
// become durable only when the caller commits tx. It reports whether the
// pool_transactions row was newly inserted; when false the swap was already
// projected and nothing else is written, so the caller must not apply any other
// per-fill effect (holder delta) either. The Redis cache is not touched: call
// CachePoolStats after the commit.
func (c *SwapConsumer) ProjectTx(ctx context.Context, tx pgx.Tx, ev SwapEvent) (bool, error) {
	st := c.store.WithTx(tx)
	if st == nil {
		return false, fmt.Errorf("consumer: swap projection transaction required")
	}
	return project(ctx, st, ev, true)
}

// CachePoolStats refreshes the Redis pool_stats cache and publishes stats:update
// (step 7 of ProcessEvent) from committed state.
func (c *SwapConsumer) CachePoolStats(ctx context.Context, poolAddress string) error {
	return cachePoolStats(ctx, c.store, c.redis, poolAddress)
}

// project runs steps 1-6 against st. When st is bound to a transaction the
// inbox insert and every derived write share that transaction. With
// stopOnDuplicate an already-recorded id ends the projection (inbox
// semantics); without it steps 2-6 run regardless, preserving the webhook's
// historical ProcessEvent behaviour.
func project(ctx context.Context, st *store.Store, ev SwapEvent, stopOnDuplicate bool) (bool, error) {
	now := shareddb.NowSeconds()

	// 1. Record transaction (idempotent on txHash-logIndex).
	inserted, err := st.RecordTransaction(ctx, store.TransactionRow{
		ID:             fmt.Sprintf("%s-%d", ev.TxHash, ev.LogIndex),
		PoolAddress:    ev.PoolAddress,
		Sender:         ev.Sender,
		IsBuy:          ev.IsBuy,
		AmountIn:       ev.AmountIn,
		AmountOut:      ev.AmountOut,
		Price:          ev.Price,
		Fee:            ev.Fee,
		BlockTimestamp: ev.BlockTimestamp,
		TxHash:         ev.TxHash,
	})
	if err != nil || (stopOnDuplicate && !inserted) {
		return false, err
	}

	// 2. Update the 1-minute volume bucket (USDL leg = amountIn for buys).
	bucketStart := (ev.BlockTimestamp / 60) * 60
	volumeUsdl := ev.AmountOut
	if ev.IsBuy {
		volumeUsdl = ev.AmountIn
	}
	if err := st.ApplyVolumeBucket(ctx, ev.PoolAddress, bucketStart, volumeUsdl, ev.Price, ev.Sender, ev.IsBuy); err != nil {
		return false, err
	}

	// 3. Recompute rolling 5m/1h/24h stats.
	if err := st.RecalculateRollingStats(ctx, ev.PoolAddress, now); err != nil {
		return false, err
	}

	// 4. Store the 1-minute price snapshot.
	minuteTs := (now / 60) * 60
	if err := st.UpsertPriceSnapshot(ctx, ev.PoolAddress, minuteTs, ev.Price); err != nil {
		return false, err
	}

	// 5. Compute price changes across all windows.
	changes, err := st.ComputePriceChanges(ctx, ev.PoolAddress, ev.Price, now)
	if err != nil {
		return false, err
	}

	// 6. Update price, market cap, high/low and price changes.
	mcap, err := shareddb.ComputeMarketCap(ev.Price)
	if err != nil {
		return false, fmt.Errorf("consumer: compute market cap: %w", err)
	}
	if err := st.UpdatePoolStatsPriceAndChanges(ctx, ev.PoolAddress, ev.Price, mcap, now, changes); err != nil {
		return false, err
	}

	return inserted, nil
}
