package migrate_test

import (
	"context"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/migrate"
)

func TestUpIsIdempotent(t *testing.T) {
	ctx := context.Background()
	// NewPostgres already applied migrate.Up once; a second Up must be a no-op.
	dsn, pool := internaltest.NewPostgres(t)

	if err := migrate.Up(ctx, dsn); err != nil {
		t.Fatalf("second migrate.Up should be idempotent: %v", err)
	}

	// The candles schema + tables must exist after migration.
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'candles' AND table_name = 'candles'
		)`).Scan(&exists)
	if err != nil || !exists {
		t.Fatalf("candles.candles table missing after migration (exists=%v, err=%v)", exists, err)
	}
}

func TestUpBadDSN(t *testing.T) {
	ctx := context.Background()
	if err := migrate.Up(ctx, "postgres://bad:bad@127.0.0.1:1/none?sslmode=disable&connect_timeout=1"); err == nil {
		t.Fatal("migrate.Up with an unreachable DSN should error")
	}
}
