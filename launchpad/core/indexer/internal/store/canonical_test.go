package store

import (
	"context"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store/canonicaltest"
	"strings"
	"testing"
	"time"
)

func TestCanonicalPostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, isolated := canonicaltest.New(t)
	var err error
	st := New(isolated)
	fence, err := st.AcquireLease(ctx, 125, "live", "A", 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.AcquireLease(ctx, 125, "live", "B", 30*time.Second); !errors.Is(err, ErrFenced) {
		t.Fatalf("competing: %v", err)
	}
	if _, err = isolated.Exec(ctx, `INSERT INTO indexer.indexer_ingest_watermarks VALUES(125,'live',100,100)`); err != nil {
		t.Fatal(err)
	}
	hash := "0x" + strings.Repeat("1", 64)
	txhash := "0x" + strings.Repeat("2", 64)
	b := CanonicalBatch{ChainID: 125, StreamID: "live", Owner: "A", BatchID: "batch101", Fence: fence, PreviousHeight: 100, Blocks: []BlockCompleteness{{Height: 101, Hash: hash, RawCount: 1, FetchComplete: true, RequiredDecodeComplete: true}}, Records: []RawRecord{{TransactionHash: txhash, BlockHash: hash, Emitter: "0x" + strings.Repeat("3", 40), Height: 101, Topics: []string{}, Data: []byte{1}}}, Outbox: []OutboxIntent{{TransactionHash: txhash, Kind: "Swap", Payload: []byte(`{"amount":"1"}`)}}}
	inspect := func(want int64) {
		t.Helper()
		var height, n int64
		if err := isolated.QueryRow(ctx, `SELECT raw_complete_height FROM indexer.indexer_ingest_watermarks`).Scan(&height); err != nil {
			t.Fatal(err)
		}
		if err := isolated.QueryRow(ctx, `SELECT count(*) FROM indexer.indexer_raw_logs`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		var complete, outbox, commits int64
		if err := isolated.QueryRow(ctx, `SELECT (SELECT count(*) FROM indexer.indexer_block_completeness),(SELECT count(*) FROM indexer.indexer_ingest_outbox),(SELECT count(*) FROM indexer.indexer_ingest_commits)`).Scan(&complete, &outbox, &commits); err != nil {
			t.Fatal(err)
		}
		if complete != want-100 || outbox != want-100 || commits != want-100 {
			t.Fatalf("atomic rows completeness=%d outbox=%d commits=%d", complete, outbox, commits)
		}
		if height != want || n != want-100 {
			t.Fatalf("height=%d rows=%d", height, n)
		}
	}
	if _, err = isolated.Exec(ctx, `CREATE TABLE indexer.typed_fixture(value integer)`); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("typed boundary failure")
	if err = st.CommitCanonical(ctx, b, func(ctx context.Context, txStore *Store) error {
		if _, err := txStore.db.Exec(ctx, `INSERT INTO indexer.typed_fixture VALUES(1)`); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	inspect(100)
	var typedCount int
	if err = isolated.QueryRow(ctx, `SELECT count(*) FROM indexer.typed_fixture`).Scan(&typedCount); err != nil || typedCount != 0 {
		t.Fatalf("typed rollback: %d %v", typedCount, err)
	}
	if _, err = isolated.Exec(ctx, `ALTER TABLE indexer.indexer_ingest_outbox ADD CONSTRAINT reject_fixture CHECK(event_kind<>'Swap')`); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitCanonical(ctx, b, nil); err == nil {
		t.Fatal("outbox failure expected")
	}
	inspect(100)
	if _, err = isolated.Exec(ctx, `ALTER TABLE indexer.indexer_ingest_outbox DROP CONSTRAINT reject_fixture`); err != nil {
		t.Fatal(err)
	}
	if err = st.CommitCanonical(ctx, b, func(ctx context.Context, txStore *Store) error {
		_, e := txStore.db.Exec(ctx, `UPDATE indexer.indexer_ingest_leases SET expires_at=clock_timestamp()-interval '1 second'`)
		return e
	}); !errors.Is(err, ErrFenced) {
		t.Fatalf("expired before cursor: %v", err)
	}
	inspect(100)
	if err = st.CommitCanonical(ctx, b, nil); err != nil {
		t.Fatal(err)
	}
	inspect(101)
	if err = st.CommitCanonical(ctx, b, nil); !errors.Is(err, ErrWatermark) {
		t.Fatalf("retry: %v", err)
	}
	next := b
	next.PreviousHeight = 101
	next.BatchID = "conflict"
	next.Blocks = []BlockCompleteness{{Height: 102, Hash: hash, RawCount: 1, FetchComplete: true, RequiredDecodeComplete: true}}
	next.Records = append([]RawRecord(nil), b.Records...)
	next.Records[0].Height = 102
	if err = st.CommitCanonical(ctx, next, nil); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("conflict: %v", err)
	}
	inspect(101)
	if err = st.ReleaseLease(ctx, 125, "live", "A", fence); err != nil {
		t.Fatal(err)
	}
	replacement, err := st.AcquireLease(ctx, 125, "live", "B", 30*time.Second)
	if err != nil || replacement <= fence {
		t.Fatalf("replacement: %d %v", replacement, err)
	}
	if err = st.CommitCanonical(ctx, next, nil); !errors.Is(err, ErrFenced) {
		t.Fatalf("stale: %v", err)
	}
	next.Owner = "B"
	next.Fence = replacement
	next.Records = nil
	next.Outbox = nil
	next.Blocks[0].RawCount = 0
	if err = st.CommitCanonical(ctx, next, nil); err != nil {
		t.Fatalf("empty: %v", err)
	}
	next.PreviousHeight = 102
	next.Blocks[0].Height = 104
	if err = st.CommitCanonical(ctx, next, nil); err == nil {
		t.Fatal("gap accepted")
	}
}
