package processor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store/canonicaltest"
)

func TestCanonicalProcessorCommitReconciliationPostgres(t *testing.T) {
	_, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	ctx := context.Background()
	st := store.New(pool)
	if committed, err := st.ReconcileCanonicalCommit(ctx, b); err != nil || committed {
		t.Fatalf("uncommitted batch: committed=%v err=%v", committed, err)
	}
	assertRows(t, pool, false)
	if err := process(ctx, pool, b); err != nil {
		t.Fatal(err)
	}
	if err := st.ReleaseLease(ctx, b.ChainID, b.StreamID, b.Owner, b.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcquireLease(ctx, b.ChainID, b.StreamID, "replacement", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if committed, err := store.New(pool).ReconcileCanonicalCommit(ctx, b); err != nil || !committed {
		t.Fatalf("durable commit after restart and takeover: committed=%v err=%v", committed, err)
	}
	changed := b
	changed.Records = append([]store.RawRecord(nil), b.Records...)
	changed.Records[0].Data = append([]byte(nil), b.Records[0].Data...)
	changed.Records[0].Data[0] ^= 1
	if committed, err := st.ReconcileCanonicalCommit(ctx, changed); committed || !errors.Is(err, store.ErrIntegrity) {
		t.Fatalf("conflicting raw bytes: committed=%v err=%v", committed, err)
	}
	changed = b
	changed.Outbox = []store.OutboxIntent{}
	if committed, err := st.ReconcileCanonicalCommit(ctx, changed); committed || !errors.Is(err, store.ErrIntegrity) {
		t.Fatalf("missing asserted delivery: committed=%v err=%v", committed, err)
	}
	changed = b
	changed.BatchID = "not-committed"
	if committed, err := st.ReconcileCanonicalCommit(ctx, changed); committed || err != nil {
		t.Fatalf("unknown batch identity: committed=%v err=%v", committed, err)
	}
	assertRows(t, pool, true)
}

func TestCanonicalLostCommitResponsePostgres(t *testing.T) {
	dsn, pool := canonicaltest.New(t)
	b := prepare(t, pool)
	proxy, lost := canonicaltest.LostCommitResponse(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	uncertain, err := pgxpool.New(ctx, proxy)
	if err != nil {
		t.Fatal(err)
	}
	defer uncertain.Close()
	if err = process(ctx, uncertain, b); err == nil {
		t.Fatal("lost PostgreSQL COMMIT response acknowledged")
	}
	select {
	case <-lost:
	case <-ctx.Done():
		t.Fatal("real PostgreSQL COMMIT was not intercepted")
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	assertRows(t, fresh, true)
	if err = process(ctx, fresh, b); !errors.Is(err, store.ErrWatermark) {
		t.Fatalf("immutable retry must not write again: %v", err)
	}
	if committed, err := store.New(fresh).ReconcileCanonicalCommit(ctx, b); err != nil || !committed {
		t.Fatalf("lost response reconciliation: committed=%v err=%v", committed, err)
	}
	assertRows(t, fresh, true)
}

func TestCanonicalCommitCrashHelper(t *testing.T) {
	file := os.Getenv("L06_COMMIT_CRASH_BATCH")
	if file == "" {
		return
	}
	body, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var batch store.CanonicalBatch
	if err = json.Unmarshal(body, &batch); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, os.Getenv("L06_COMMIT_CRASH_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = process(ctx, pool, batch); err == nil {
		t.Fatal("lost commit response unexpectedly acknowledged")
	}
	select {}
}
func TestCanonicalKilledAfterCommitBeforeAcknowledgementPostgres(t *testing.T) {
	dsn, pool := canonicaltest.New(t)
	batch := prepare(t, pool)
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "batch.json")
	if err = os.WriteFile(file, body, 0600); err != nil {
		t.Fatal(err)
	}
	proxy, lost := canonicaltest.LostCommitResponse(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCanonicalCommitCrashHelper$")
	child.Env = append(os.Environ(), "L06_COMMIT_CRASH_BATCH="+file, "L06_COMMIT_CRASH_DSN="+proxy)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	})
	select {
	case <-lost:
	case <-ctx.Done():
		t.Fatal("child did not reach committed response boundary")
	}
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err = child.Wait(); err == nil {
		t.Fatal("leader process was not killed")
	}
	fresh, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	assertRows(t, fresh, true)
	if committed, err := store.New(fresh).ReconcileCanonicalCommit(ctx, batch); err != nil || !committed {
		t.Fatalf("restart commit reconciliation: %v %v", committed, err)
	}
	assertRows(t, fresh, true)
}
