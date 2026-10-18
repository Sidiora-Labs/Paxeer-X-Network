package consumer_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// discardLogger returns a slog.Logger that drops all output, so consumer tests
// exercise the real logging code paths without noise.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newStore spins up a migrated Postgres container and returns a real Store.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	return store.New(internaltest.NewPostgres(t))
}

// seedPoolStats inserts a baseline stats.pool_stats row for addr via the real
// InsertInitialPoolStats path so the price/holder consumers have a row to mutate.
func seedPoolStats(t *testing.T, ctx context.Context, st *store.Store, addr string, creator *string, price string) {
	t.Helper()
	if _, err := st.InsertInitialPoolStats(ctx, store.InitialPoolStats{
		PoolAddress:    addr,
		TokenAddress:   addr + "-tok",
		CreatorAddress: creator,
		Price:          price,
		MarketCap:      "0",
		High24h:        price,
		Low24h:         price,
		CreatedAt:      100,
		UpdatedAt:      100,
	}); err != nil {
		t.Fatalf("seed pool stats: %v", err)
	}
}

// strptr is a small helper for optional string fields.
func strptr(s string) *string { return &s }

// poolHolderCount reads stats.pool_stats.holder_count via the real store (used by
// the holder-tracker tests to observe whether the debounced refresh has run).
func poolHolderCount(t *testing.T, ctx context.Context, st *store.Store, addr string) int {
	t.Helper()
	row, err := st.GetPoolStats(ctx, addr)
	if err != nil {
		t.Fatalf("get pool stats: %v", err)
	}
	if row == nil {
		t.Fatalf("pool stats row %s missing", addr)
	}
	return row.HolderCount
}
