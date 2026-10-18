package migrate

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestUpCreatesLifecycleReadStore applies the embedded migrations to a real
// Postgres database (twice, proving idempotence) and checks the tables the
// non-production read surface queries.
func TestUpCreatesLifecycleReadStore(t *testing.T) {
	dsn := os.Getenv("L40_TEST_APP_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("L40_TEST_APP_POSTGRES_DSN (a dedicated database) is required; no silent skip")
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := Up(ctx, dsn); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, table := range []string{"livestream.streams", "livestream.stream_operations", "livestream.stream_inbox", "livestream.stream_audit", "livestream.stream_outbox", "livestream.stream_credentials", "livestream.stream_worker_attempts", "livestream.stream_usage", "livestream.stream_key_rotations", "livestream.stream_budget_alerts", "livestream.stream_vendor_usage", "livestream.stream_playback_privacy"} {
		var present bool
		if err = pool.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", table).Scan(&present); err != nil || !present {
			t.Fatalf("%s missing: %v", table, err)
		}
	}
	var terminal bool
	if err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='livestream' AND table_name='stream_operations' AND column_name='terminal_at')").Scan(&terminal); err != nil || !terminal {
		t.Fatal("terminal_at column missing", err)
	}
}
