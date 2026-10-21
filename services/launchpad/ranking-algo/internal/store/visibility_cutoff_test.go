package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
)

// TestVisibilityObservedAfterCutoffFailsTheFreeze pins the cutoff/observation
// contract: a fresh, complete visibility response whose acquisition crossed into
// the next UTC second is refused before any canonical read, instead of being
// copied onto every candidate that BuildSnapshot would then exclude as
// moderation-stale. A response at or before the cutoff passes that gate and
// reaches the canonical reads (absent on this empty database).
func TestVisibilityObservedAfterCutoffFailsTheFreeze(t *testing.T) {
	admin := os.Getenv(internaltest.PostgresDSNEnv)
	if admin == "" {
		t.Fatalf("%s is not set: point it at a running PostgreSQL whose role may CREATE DATABASE", internaltest.PostgresDSNEnv)
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, internaltest.NewExternalDatabase(t, admin))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	cutoff := time.Now().UTC().Truncate(time.Second).Add(-time.Second)
	read := func(observed time.Time) error {
		t.Helper()
		reader, err := NewCanonicalInputReader("indexer", "stats", universeStream, universeProjection, newFixtureVisibility(t, observed, map[string]bool{poolA: true}))
		if err != nil {
			t.Fatal(err)
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		_, err = reader.ReadRankingUniverseTx(ctx, tx, "run", cutoff)
		return err
	}
	if err := read(cutoff.Add(time.Second)); !errors.Is(err, ranker.ErrUnavailable) {
		t.Fatalf("visibility observed after the cutoff: err = %v, want ErrUnavailable", err)
	}
	if err := read(cutoff.Add(-61 * time.Second)); !errors.Is(err, ranker.ErrUnavailable) {
		t.Fatalf("visibility observed more than 60 s before the cutoff: err = %v, want ErrUnavailable", err)
	}
	for _, observed := range []time.Time{cutoff, cutoff.Add(-time.Second)} {
		if err := read(observed); err == nil || errors.Is(err, ranker.ErrUnavailable) {
			t.Fatalf("visibility observed %s before the cutoff was refused: %v", cutoff.Sub(observed), err)
		}
	}
}
