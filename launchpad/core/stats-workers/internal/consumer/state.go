package consumer

import (
	"context"
	"fmt"
	"log/slog"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// StateEvent is a decoded PoolStateUpdated webhook event.
type StateEvent struct {
	PoolAddress string
	Price       string
}

// StateConsumer refreshes a pool's price + market cap on a state update. Ports
// StateConsumer.
type StateConsumer struct {
	store  *store.Store
	logger *slog.Logger
}

// NewStateConsumer builds a StateConsumer.
func NewStateConsumer(st *store.Store, logger *slog.Logger) *StateConsumer {
	return &StateConsumer{store: st, logger: logger}
}

// ProcessEvent updates price and market cap for the pool (a no-op when unknown).
// Ports StateConsumer.processEvent.
func (c *StateConsumer) ProcessEvent(ctx context.Context, ev StateEvent) error {
	now := shareddb.NowSeconds()
	mcap, err := shareddb.ComputeMarketCap(ev.Price)
	if err != nil {
		return fmt.Errorf("consumer: compute market cap: %w", err)
	}
	return c.store.UpdatePoolStatsPrice(ctx, ev.PoolAddress, ev.Price, mcap, now)
}
