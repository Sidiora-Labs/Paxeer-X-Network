package replay

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	testRPCEnv      = "PERPS_DATA_TEST_RPC"
	testCometEnv    = "PERPS_DATA_TEST_COMET"
	indexerHolder   = "indexer-test"
	replayHolder    = "replay-test"
	testSpan        = 20
)

func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatal("TestReplay requires " + name)
	}
	return v
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(requireEnv(t, testDatabaseEnv))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	ctx := context.Background()
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	tables, err := blockTables(ctx, db)
	if err != nil {
		t.Fatalf("blockTables: %v", err)
	}
	reset := []string{"perps.blocks", "perps.cursors", "perps.writer_lease", "perps.delivery_outbox", "perps.delivery_inbox"}
	for _, tb := range tables {
		reset = append(reset, "perps."+tb.name)
	}
	for _, p := range projectionTables {
		reset = append(reset, "perps."+p)
	}
	for _, name := range reset {
		if _, err := db.ExecContext(ctx, "TRUNCATE "+name); err != nil {
			t.Fatalf("truncate %s: %v", name, err)
		}
	}
	return db
}

func moduleAddress(name string) common.Address {
	sum := sha256.Sum256([]byte(name))
	return common.BytesToAddress(sum[:20])
}

func testSource(t *testing.T, db *sql.DB) (*source.Follower, *source.RPC) {
	t.Helper()
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	metrics, err := telemetry.New(config.Replay)
	if err != nil {
		t.Fatalf("telemetry.New: %v", err)
	}
	registry, err := decode.NewRegistry(metrics, moduleAddress("perps"))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	rpc, err := source.NewRPC(ctx, source.RPCOptions{URLs: []string{requireEnv(t, testRPCEnv)}, Logger: logger})
	if err != nil {
		t.Fatalf("NewRPC: %v", err)
	}
	t.Cleanup(func() {
		if err := rpc.Close(); err != nil {
			t.Errorf("close rpc: %v", err)
		}
	})
	comet, err := source.NewComet([]string{requireEnv(t, testCometEnv)})
	if err != nil {
		t.Fatalf("NewComet: %v", err)
	}
	follower, err := source.NewFollower(source.FollowerOptions{RPC: rpc, Comet: comet, Registry: registry, DB: db, ChainID: 1, Logger: logger})
	if err != nil {
		t.Fatalf("NewFollower: %v", err)
	}
	return follower, rpc
}

// index is the indexer's write path: one transaction per height through
// store.WriteBlock under the writer lease of indexerHolder.
func index(t *testing.T, db *sql.DB, src Source, from, to int64) {
	t.Helper()
	ctx := context.Background()
	if err := acquireLease(ctx, db, indexerHolder, MaxLeaseTTL); err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	for h := from; h <= to; h++ {
		if err := writeHeight(ctx, db, src, h); err != nil {
			t.Fatalf("index height %d: %v", h, err)
		}
	}
	if err := releaseLease(ctx, db, indexerHolder); err != nil {
		t.Fatalf("release lease: %v", err)
	}
}

func writeHeight(ctx context.Context, db *sql.DB, src Source, h int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	block, rows, err := src.Read(ctx, tx, uint64(h))
	if err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := store.WriteBlock(ctx, tx, block, rows); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func runner(db *sql.DB, src Source) *Runner {
	return &Runner{DB: db, Source: src, Holder: replayHolder, LeaseTTL: MaxLeaseTTL, LeaseRetry: 10 * time.Millisecond, BatchSize: 7}
}

func cursorHeight(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var h int64
	if err := db.QueryRow(`SELECT height FROM perps.cursors WHERE name = 'head'`).Scan(&h); err != nil {
		t.Fatalf("read cursor: %v", err)
	}
	return h
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// dump returns every row of the replayed scope, the cursor, the lease and the
// outbox as JSON text, keyed by table and sorted.
func dump(t *testing.T, db *sql.DB, from, to int64) map[string][]string {
	t.Helper()
	ctx := context.Background()
	tables, err := blockTables(ctx, db)
	if err != nil {
		t.Fatalf("blockTables: %v", err)
	}
	queries := map[string]string{
		"perps.blocks":          fmt.Sprintf(`SELECT * FROM perps.blocks WHERE number BETWEEN %d AND %d`, from, to),
		"perps.cursors":         `SELECT * FROM perps.cursors`,
		"perps.writer_lease":    `SELECT * FROM perps.writer_lease`,
		"perps.delivery_outbox": `SELECT * FROM perps.delivery_outbox`,
	}
	for _, tb := range tables {
		queries["perps."+tb.name] = fmt.Sprintf(`SELECT * FROM perps.%s WHERE block BETWEEN %d AND %d`, tb.name, from, to)
	}
	for _, p := range projectionTables {
		queries["perps."+p] = `SELECT * FROM perps.` + p
	}
	out := map[string][]string{}
	for name, q := range queries {
		rows, err := db.QueryContext(ctx, `SELECT row_to_json(x)::text FROM (`+q+`) x ORDER BY 1`)
		if err != nil {
			t.Fatalf("dump %s: %v", name, err)
		}
		var lines []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatalf("dump %s: %v", name, err)
			}
			lines = append(lines, s)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("dump %s: %v", name, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("dump %s: %v", name, err)
		}
		out[name] = lines
	}
	return out
}

func sameDump(t *testing.T, before, after map[string][]string) {
	t.Helper()
	for name, rows := range before {
		if !slices.Equal(rows, after[name]) {
			t.Errorf("%s changed: %d rows before, %d after\nbefore %v\nafter  %v", name, len(rows), len(after[name]), rows, after[name])
		}
	}
}

func wantRefusal(t *testing.T, err error, want string) {
	t.Helper()
	var refusal Refusal
	if !errors.As(err, &refusal) || string(refusal) != want {
		t.Fatalf("error %v, want refusal %q", err, want)
	}
	if code := Exit(err); code != 2 {
		t.Fatalf("Exit(%v) = %d, want 2", err, code)
	}
}

// observed is the follower read path with a hook run before each read.
type observed struct {
	Source
	before func(n int)
	mu     sync.Mutex
	reads  int
}

func (o *observed) Read(ctx context.Context, q decode.Querier, h uint64) (store.Block, *decode.Result, error) {
	o.mu.Lock()
	o.reads++
	n := o.reads
	o.mu.Unlock()
	o.before(n)
	return o.Source.Read(ctx, q, h)
}

func TestReplay(t *testing.T) {
	ctx := context.Background()
	db := testDB(t)
	src, rpc := testSource(t, db)
	head, err := rpc.Head(ctx)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	to := head - 1
	from := to - testSpan
	if from < 1 {
		t.Fatalf("node head %d is below %d", head, testSpan+2)
	}
	index(t, db, src, from, to)
	if c := cursorHeight(t, db); c != to {
		t.Fatalf("cursor %d after the first index, want %d", c, to)
	}
	if n := count(t, db, `SELECT count(*) FROM perps.blocks WHERE number BETWEEN $1 AND $2`, from, to); n != testSpan+1 {
		t.Fatalf("%d blocks indexed, want %d", n, testSpan+1)
	}

	t.Run("validates_range", func(t *testing.T) {
		wantRefusal(t, Validate(Request{From: 500, To: 400}, 1000), "from must not exceed to")
		wantRefusal(t, Validate(Request{From: -1, To: 400}, 1000), "from must not be negative")
		wantRefusal(t, Validate(Request{From: 400, To: 1001}, 1000), "to is above the cursor 1000")
		wantRefusal(t, Validate(Request{From: 0, To: MaxSpan}, MaxSpan+5), fmt.Sprintf("range exceeds %d heights", MaxSpan))
		for _, ok := range []Request{{From: 400, To: 420}, {From: 400, To: ToCursor}, {From: 1000, To: 1000}, {From: 0, To: MaxSpan - 1}} {
			if err := Validate(ok, MaxSpan); err != nil {
				t.Fatalf("Validate(%+v): %v", ok, err)
			}
		}
		before := dump(t, db, from, to)
		_, err := runner(db, src).Run(ctx, Request{From: to, To: from})
		wantRefusal(t, err, "from must not exceed to")
		sameDump(t, before, dump(t, db, from, to))
		if code := Exit(nil); code != 0 {
			t.Fatalf("Exit(nil) = %d, want 0", code)
		}
		if code := Exit(ErrLeaseLost); code != 1 {
			t.Fatalf("Exit(ErrLeaseLost) = %d, want 1", code)
		}
	})

	t.Run("refuses_from_above_cursor", func(t *testing.T) {
		_, err := Admit(ctx, db, Request{From: to + 1, To: ToCursor})
		wantRefusal(t, err, fmt.Sprintf("from is above the cursor %d", to))
		before := dump(t, db, from, to)
		_, err = runner(db, src).Run(ctx, Request{From: to + 1, To: ToCursor})
		wantRefusal(t, err, fmt.Sprintf("from is above the cursor %d", to))
		sameDump(t, before, dump(t, db, from, to))
		got, err := Admit(ctx, db, Request{From: from, To: ToCursor})
		if err != nil || got != (Request{From: from, To: to}) {
			t.Fatalf("Admit(to the cursor) = %+v, %v; want to %d", got, err, to)
		}
	})

	t.Run("rewrites_identical_rows", func(t *testing.T) {
		if n := count(t, db, `SELECT count(*) FROM perps.index_prices WHERE block BETWEEN $1 AND $2`, from, to); n == 0 {
			t.Fatalf("no pricefeed rows between %d and %d on the test node", from, to)
		}
		before := dump(t, db, from, to)
		rep, err := runner(db, src).Run(ctx, Request{From: from, To: to})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if rep.Request != (Request{From: from, To: to}) {
			t.Fatalf("report request %+v", rep.Request)
		}
		sameDump(t, before, dump(t, db, from, to))
		for _, c := range rep.Tables {
			if c.Changed != 0 {
				t.Errorf("%s changed %d rows", c.Table, c.Changed)
			}
			if rows, ok := before[c.Table]; ok && c.Table != "perps.delivery_outbox" && c.Rows != int64(len(rows)) {
				t.Errorf("%s reports %d rows, %d stored", c.Table, c.Rows, len(rows))
			}
		}
	})

	t.Run("dry_run_counts", func(t *testing.T) {
		before := dump(t, db, from, to)
		rep, err := runner(db, src).Run(ctx, Request{From: from, To: to, DryRun: true})
		if err != nil {
			t.Fatalf("Run dry: %v", err)
		}
		sameDump(t, before, dump(t, db, from, to))
		reported := map[string]TableCount{}
		for _, c := range rep.Tables {
			reported[c.Table] = c
		}
		for name, rows := range before {
			switch name {
			case "perps.cursors", "perps.writer_lease", "perps.delivery_outbox":
				continue
			}
			c, ok := reported[name]
			if !ok {
				t.Errorf("dry run reports no count for %s", name)
				continue
			}
			if c.Rows != int64(len(rows)) || c.Changed != 0 {
				t.Errorf("%s dry run rows=%d changed=%d, want rows=%d changed=0", name, c.Rows, c.Changed, len(rows))
			}
		}
		if c := reported["perps.blocks"]; c.Rows != testSpan+1 {
			t.Errorf("perps.blocks dry run rows=%d, want %d", c.Rows, testSpan+1)
		}
	})

	t.Run("no_duplicate_messages", func(t *testing.T) {
		outbox := count(t, db, `SELECT count(*) FROM perps.delivery_outbox`)
		maxSeq := count(t, db, `SELECT COALESCE(max(perps_seq), 0) FROM perps.delivery_outbox`)
		rep, err := runner(db, src).Run(ctx, Request{From: from, To: to})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.delivery_outbox`); n != outbox {
			t.Fatalf("outbox holds %d messages after the replay, %d before", n, outbox)
		}
		if n := count(t, db, `SELECT COALESCE(max(perps_seq), 0) FROM perps.delivery_outbox`); n != maxSeq {
			t.Fatalf("outbox sequence %d after the replay, %d before", n, maxSeq)
		}
		for _, c := range rep.Tables {
			if c.Table == "perps.delivery_outbox" && (c.Rows != 0 || c.Changed != 0) {
				t.Fatalf("replay added %d outbox messages", c.Rows)
			}
		}
	})

	t.Run("holds_lease", func(t *testing.T) {
		acquired := make(chan error, 1)
		written := make(chan error, 1)
		var start sync.Once
		obs := &observed{Source: src}
		obs.before = func(n int) {
			if n == 1 {
				if err := acquireLease(ctx, db, indexerHolder, MaxLeaseTTL); !errors.Is(err, errLeaseHeld) {
					t.Errorf("indexer lease during replay: %v, want held", err)
				}
				start.Do(func() {
					go func() {
						err := waitLease(ctx, db, indexerHolder, MaxLeaseTTL, 10*time.Millisecond)
						acquired <- err
						if err != nil {
							written <- err
							return
						}
						err = writeHeight(ctx, db, src, to+1)
						written <- errors.Join(err, releaseLease(ctx, db, indexerHolder))
					}()
				})
				return
			}
			select {
			case err := <-acquired:
				t.Errorf("indexer took the lease during the replay at read %d: %v", n, err)
			default:
			}
		}
		if _, err := runner(db, obs).Run(ctx, Request{From: from, To: to}); err != nil {
			t.Fatalf("Run: %v", err)
		}
		if obs.reads != testSpan+1 {
			t.Fatalf("replay read %d heights, want %d", obs.reads, testSpan+1)
		}
		select {
		case err := <-written:
			if err != nil {
				t.Fatalf("indexer write after the replay: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Fatal("indexer did not write after the replay released the lease")
		}
		if c := cursorHeight(t, db); c != to+1 {
			t.Fatalf("cursor %d after the indexer write, want %d", c, to+1)
		}
		if n := count(t, db, `SELECT count(*) FROM perps.writer_lease`); n != 0 {
			t.Fatalf("%d writer lease rows left, want 0", n)
		}
	})
}
