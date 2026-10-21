package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Explicit local PostgreSQL fixture, isolated by the L19 lease namespace.
func TestSnapshotStoreLocalPostgres(t *testing.T) {
	dsn := os.Getenv("L19_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("L19_POSTGRES_DSN required for real PostgreSQL qualification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	schema := fmt.Sprintf("kd_25f7eaabd03e881749ed6ae1_%d", time.Now().UnixNano())
	ddl, e := os.ReadFile("../../../../../launchpad/spec/production-system/handoffs/L19/phase1/190000_ranking_snapshots.sql")
	if e != nil {
		t.Fatal(e)
	}
	sql := strings.ReplaceAll(string(ddl), "ranking.", pgx.Identifier{schema}.Sanitize()+".")
	sql = strings.Replace(sql, "CREATE SCHEMA ranking;", "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()+";", 1)
	if _, e = pool.Exec(ctx, sql); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := pool.Exec(context.Background(), "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); e != nil {
			t.Error(e)
		}
	}()
	st, e := NewSnapshotStore(pool, schema)
	if e != nil {
		t.Fatal(e)
	}
	in := ranker.InputSnapshot{RunID: "local-empty-run", AlgorithmVersion: ranker.AlgorithmVersion, FeatureSchemaVersion: ranker.FeatureSchemaVersion, CutoffHeight: 125, CutoffTimeUTC: 1000, Complete: true}
	snap, e := ranker.BuildSnapshot(in, "trending", nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = st.Save(ctx, snap); e != nil {
		t.Fatal(e)
	}
	if e = st.Save(ctx, snap); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"snapshots", "publication_outbox"} {
		var n int
		if e = pool.QueryRow(ctx, "SELECT count(*) FROM "+st.table(table)).Scan(&n); e != nil || n != 1 {
			t.Fatalf("%s count %d: %v", table, n, e)
		}
	}
	b, e := st.Read(ctx, in.RunID, "trending")
	if e != nil || !strings.Contains(string(b), `"entries": []`) {
		t.Fatalf("empty completed artifact %s: %v", b, e)
	}
	in.CutoffHeight = 124
	conflict, _ := ranker.BuildSnapshot(in, "trending", nil)
	if e = st.Save(ctx, conflict); e == nil {
		t.Fatal("immutable identity overwritten")
	}
	// Database rejects outbox insertion; the preceding snapshot insert must roll back.
	if _, e = pool.Exec(ctx, "ALTER TABLE "+st.table("publication_outbox")+" ADD CONSTRAINT reject_test CHECK(run_id <> 'rollback-run')"); e != nil {
		t.Fatal(e)
	}
	in.RunID = "rollback-run"
	rollback, _ := ranker.BuildSnapshot(in, "trending", nil)
	if e = st.Save(ctx, rollback); e == nil {
		t.Fatal("forced failure accepted")
	}
	if _, e = st.Read(ctx, in.RunID, "trending"); e != pgx.ErrNoRows {
		t.Fatalf("partial snapshot persisted: %v", e)
	}
	canceled, stop := context.WithCancel(ctx)
	stop()
	if e = st.Save(canceled, snap); e == nil {
		t.Fatal("cancellation ignored")
	}
	// Reconnect models a fresh reader after producer/process restart.
	pool2, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer pool2.Close()
	st2, _ := NewSnapshotStore(pool2, schema)
	if _, e = st2.Read(ctx, "local-empty-run", "trending"); e != nil {
		t.Fatal(e)
	}
}
