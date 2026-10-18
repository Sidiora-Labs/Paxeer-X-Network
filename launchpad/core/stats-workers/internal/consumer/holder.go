package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/holdings"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
	"github.com/jackc/pgx/v5"
)

// holderSnapshot is the holders:update push payload: the routing key plus the
// fields a client needs to update the holder panel in place without refetching.
type holderSnapshot struct {
	PoolAddress string            `json:"poolAddress"`
	HolderCount int               `json:"holderCount"`
	TopHolders  []store.HolderRow `json:"topHolders"`
}

// topHoldersLimit bounds the top-holder slice carried in a holders:update push.
const topHoldersLimit = 10

// defaultHolderDebounce bounds holder-stats refresh to at most once per pool per
// 10s (parity with the TS HOLDER_STATS_DEBOUNCE_MS constant; S-1).
const defaultHolderDebounce = 10 * time.Second

// HolderSwap is the subset of a Swap event the holder tracker consumes.
type HolderSwap struct {
	PoolAddress string
	Sender      string
	IsBuy       bool
	AmountIn    string
	AmountOut   string
}

// HolderTracker maintains per-holder balances and debounces the (expensive)
// holder-stats + risk enrichment. Ports HolderTracker. It is safe for concurrent
// use; pending refresh timers are stopped on Close so no goroutine leaks.
type HolderTracker struct {
	store    *store.Store
	redis    *goredis.Client
	logger   *slog.Logger
	debounce time.Duration

	mu     sync.Mutex
	timers map[string]*time.Timer
	closed bool
}

// NewHolderTracker builds a HolderTracker. A non-positive debounce falls back to
// the 10s parity default.
func NewHolderTracker(st *store.Store, rdb *goredis.Client, logger *slog.Logger, debounce time.Duration) *HolderTracker {
	if debounce <= 0 {
		debounce = defaultHolderDebounce
	}
	return &HolderTracker{
		store:    st,
		redis:    rdb,
		logger:   logger,
		debounce: debounce,
		timers:   make(map[string]*time.Timer),
	}
}

// ProcessSwap applies the sender's balance delta and schedules a debounced
// holder-stats refresh. A "new holder selling" swap is a no-op and skips the
// refresh (parity with the TS early return). Ports HolderTracker.processSwap.
func (h *HolderTracker) ProcessSwap(ctx context.Context, sw HolderSwap) error {
	now := shareddb.NowSeconds()
	applied, err := h.store.ApplyHolderDelta(ctx, sw.PoolAddress, sw.Sender, sw.IsBuy, sw.AmountIn, sw.AmountOut, now)
	if err != nil {
		return err
	}
	if !applied {
		return nil
	}
	h.debouncedRefresh(sw.PoolAddress) //nolint:contextcheck // refresh is debounced and fires after the request returns, so it intentionally uses a background context
	return nil
}

// ApplySwapTx applies the sender's balance delta inside the caller's
// transaction (the per-holder advisory lock is taken on a savepoint and held to
// the caller's commit). It reports whether a holder mutation was applied; the
// caller schedules the refresh with ScheduleRefresh only after committing, so a
// refresh never reads an uncommitted or rolled-back delta.
func (h *HolderTracker) ApplySwapTx(ctx context.Context, tx pgx.Tx, sw HolderSwap) (bool, error) {
	st := h.store.WithTx(tx)
	if st == nil {
		return false, errors.New("consumer: holder transaction required")
	}
	return st.ApplyHolderDelta(ctx, sw.PoolAddress, sw.Sender, sw.IsBuy, sw.AmountIn, sw.AmountOut, shareddb.NowSeconds())
}

// ScheduleRefresh schedules the debounced holder-stats refresh for a pool whose
// holder delta has been committed.
func (h *HolderTracker) ScheduleRefresh(poolAddress string) {
	h.debouncedRefresh(poolAddress)
}

// debouncedRefresh schedules at most one refresh per pool within the debounce
// window (parity with the TS refreshTimers map + setTimeout/unref).
func (h *HolderTracker) debouncedRefresh(poolAddress string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	if _, pending := h.timers[poolAddress]; pending {
		return
	}
	h.timers[poolAddress] = time.AfterFunc(h.debounce, func() {
		h.mu.Lock()
		delete(h.timers, poolAddress)
		h.mu.Unlock()
		if err := h.RefreshNow(context.Background(), poolAddress); err != nil {
			h.logger.Error("failed to refresh holder stats",
				slog.String("pool", poolAddress), slog.Any("err", err))
		}
	})
}

// RefreshNow recomputes the holder stats + risk for a pool and invalidates the
// Redis cache, synchronously. Ports HolderTracker.refreshPoolHolderStats.
func (h *HolderTracker) RefreshNow(ctx context.Context, poolAddress string) error {
	now := shareddb.NowSeconds()
	if err := h.store.RefreshPoolHolderStats(ctx, poolAddress, now); err != nil {
		return err
	}
	if err := h.redis.Del(ctx, cacheKey(poolAddress)).Err(); err != nil {
		return err
	}
	h.publishHolders(ctx, poolAddress)
	return nil
}

// publishHolders pushes the fresh holder snapshot on the holders:update channel
// so subscribers refresh the holder panel without polling. Best-effort: a read
// or publish failure is logged but never fails the refresh (the DB row and the
// invalidated cache remain consistent regardless).
func (h *HolderTracker) publishHolders(ctx context.Context, poolAddress string) {
	count, err := h.store.CountHolders(ctx, poolAddress)
	if err != nil {
		h.logger.Warn("holders:update count failed", slog.String("pool", poolAddress), slog.Any("err", err))
		return
	}
	top, err := h.store.ListHolders(ctx, poolAddress, topHoldersLimit, 0)
	if err != nil {
		h.logger.Warn("holders:update top holders failed", slog.String("pool", poolAddress), slog.Any("err", err))
		return
	}
	payload, err := json.Marshal(holderSnapshot{
		PoolAddress: poolAddress,
		HolderCount: count,
		TopHolders:  top,
	})
	if err != nil {
		h.logger.Warn("holders:update marshal failed", slog.String("pool", poolAddress), slog.Any("err", err))
		return
	}
	if err := sharedredis.PublishJSON(ctx, h.redis, constants.ChannelHoldersUpdate, payload); err != nil {
		h.logger.Warn("holders:update publish failed", slog.String("pool", poolAddress), slog.Any("err", err))
	}
}

// Close stops all pending debounce timers.
func (h *HolderTracker) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for k, t := range h.timers {
		t.Stop()
		delete(h.timers, k)
	}
}

type TransferHolderTracker struct {
	Repository   *holdings.Repository
	SourceStream string
}

func (h TransferHolderTracker) ProcessTransferTx(ctx context.Context, tx pgx.Tx, e holdings.Transfer) (holdings.ApplyResult, error) {
	if h.Repository == nil {
		return holdings.ApplyResult{}, holdings.ErrUnknownToken
	}
	return h.Repository.ApplyCanonicalTransferTx(ctx, tx, e, h.SourceStream)
}

func (h TransferHolderTracker) CatchUp(ctx context.Context, token string, limit int) (holdings.CanonicalBatch, error) {
	if h.Repository == nil {
		return holdings.CanonicalBatch{}, holdings.ErrUnknownToken
	}
	return h.Repository.CatchUpCanonical(ctx, token, h.SourceStream, limit)
}

func (h TransferHolderTracker) ProcessTransfer(ctx context.Context, e holdings.Transfer) (holdings.ApplyResult, error) {
	if h.Repository == nil {
		return holdings.ApplyResult{}, holdings.ErrUnknownToken
	}
	return h.Repository.ApplyCanonicalTransfer(ctx, e, h.SourceStream)
}
