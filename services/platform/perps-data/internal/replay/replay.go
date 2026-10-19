// Package replay rewrites a height range of the perps data schema from the
// node: under the writer lease it deletes the facts and block-keyed
// projections of the range, re-reads every height through the indexer's read
// path, rewrites it through store.WriteFacts with the row ids and event ids of
// the first index, rebuilds the projections and restores the cursor.
//
// Origin: Launch-x core/indexer/internal/store (ReplayRequest,
// ValidateReplayRequest, AdmitReplay as Request, Validate, Admit) and
// core/indexer/internal/processor (ReplayRunner, Run and the batch loop of
// Backfill as Runner and Run), bound to schema perps instead of the launchpad
// replay job tables.
package replay

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

// ToCursor as Request.To replays up to the indexer cursor.
const ToCursor int64 = -1

// Bounds of one replay, parity with the origin.
const (
	MaxSpan      = 1000000
	MaxBatchSize = 1000
	MinLeaseTTL  = time.Second
	MaxLeaseTTL  = 30 * time.Second
)

// cursorName and leaseName name the indexer's rows of perps.cursors and
// perps.writer_lease (contracts/data-services.md section 2).
const (
	cursorName = "head"
	leaseName  = "indexer"
)

var holderToken = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,128}$`)

// projectionTables are the projection tables of contracts/data-services.md
// section 2 not keyed by block; the block-keyed ones are found with the facts.
var projectionTables = []string{
	"positions_current", "orders", "spot_orders", "account_margin", "user_stats", "user_pnl_daily",
	"market_stats_rolling", "leaderboard", "spot_token_seen", "market_snapshots", "protocol_snapshots",
}

var (
	// ErrLeaseLost reports that the writer lease expired or changed holder
	// before a batch committed; the batch is rolled back.
	ErrLeaseLost = errors.New("replay: writer lease lost")
	// ErrHashMismatch reports a height whose node hash differs from the stored
	// one; a fork is the follower's rewind, never a replay.
	ErrHashMismatch = errors.New("replay: node hash differs from the stored hash")
	errLeaseHeld    = errors.New("replay: writer lease held")
)

// Refusal is a request the tool refuses before writing; perps-replay exits 2.
type Refusal string

func (r Refusal) Error() string { return string(r) }

// Request names the inclusive height range to replay. To is ToCursor to
// replay up to the cursor. DryRun rolls every batch back after counting.
type Request struct {
	From, To int64
	DryRun   bool
}

// Validate refuses a range that starts below 0 or above the cursor, ends
// before it starts or above the cursor, or spans MaxSpan heights or more.
func Validate(r Request, cursor int64) error {
	if r.From < 0 {
		return Refusal("from must not be negative")
	}
	if r.From > cursor {
		return Refusal(fmt.Sprintf("from is above the cursor %d", cursor))
	}
	to := r.To
	if to == ToCursor {
		to = cursor
	}
	if to < 0 {
		return Refusal("to must not be negative")
	}
	if r.From > to {
		return Refusal("from must not exceed to")
	}
	if to > cursor {
		return Refusal(fmt.Sprintf("to is above the cursor %d", cursor))
	}
	if to-r.From >= MaxSpan {
		return Refusal(fmt.Sprintf("range exceeds %d heights", MaxSpan))
	}
	return nil
}

// Admit reads the indexer cursor through q, validates r against it and
// returns r with To resolved.
func Admit(ctx context.Context, q decode.Querier, r Request) (Request, error) {
	var cursor int64
	err := q.QueryRowContext(ctx, `SELECT height FROM perps.cursors WHERE name = $1`, cursorName).Scan(&cursor)
	if errors.Is(err, sql.ErrNoRows) {
		return r, Refusal("no cursor: nothing is indexed")
	}
	if err != nil {
		return r, fmt.Errorf("read cursor: %w", err)
	}
	if err := Validate(r, cursor); err != nil {
		return r, err
	}
	if r.To == ToCursor {
		r.To = cursor
	}
	return r, nil
}

// Exit maps a Run or Admit outcome to the process exit code: 0 on success, 2
// on a refusal, 1 otherwise.
func Exit(err error) int {
	var refusal Refusal
	switch {
	case err == nil:
		return 0
	case errors.As(err, &refusal):
		return 2
	default:
		return 1
	}
}

// Source reads one height from the node exactly as the indexer does: the
// block and its proposer, the logs of the address set including the
// synthetic receipts, and the block_results events, decoded through q so the
// account lookups of id-only events see the heights already rewritten.
type Source interface {
	Read(ctx context.Context, q decode.Querier, height uint64) (store.Block, *decode.Result, error)
}

// NodeSource reads a height through the follower and decodes it with the
// registry, the read path of the indexer.
type NodeSource struct {
	Follower *source.Follower
	Registry *decode.Registry
}

// Read fetches height from the node and decodes it through q.
func (n NodeSource) Read(ctx context.Context, q decode.Querier, height uint64) (store.Block, *decode.Result, error) {
	if n.Follower == nil || n.Registry == nil {
		return store.Block{}, nil, errors.New("replay: node source needs a follower and a registry")
	}
	b, err := n.Follower.Fetch(ctx, height)
	if err != nil {
		return store.Block{}, nil, err
	}
	rows, err := n.Registry.DecodeBlock(ctx, q, decode.BlockInput{Number: b.Number, Hash: b.Hash, Time: b.Time, Logs: b.Logs, Events: b.FinalizeBlockEvents})
	if err != nil {
		return store.Block{}, nil, err
	}
	return store.Block{Number: b.Number, Hash: b.Hash, ParentHash: b.ParentHash, Time: b.Time, Proposer: b.Proposer, TxCount: b.TxCount}, rows, nil
}

// Runner replays ranges under the writer lease, one transaction per batch.
type Runner struct {
	DB         *sql.DB
	Source     Source
	Holder     string
	LeaseTTL   time.Duration
	LeaseRetry time.Duration
	BatchSize  int
}

// TableCount is the outcome of a replay for one table: Rows is the number of
// rows of the replayed scope after the rewrite and Changed the number of rows
// that differ from the first index (removed plus added). For
// perps.delivery_outbox both count the messages the replay added.
type TableCount struct {
	Table         string
	Rows, Changed int64
}

// Report is the per-table outcome of one Run, in table order.
type Report struct {
	Request Request
	Tables  []TableCount
}

type table struct {
	name    string
	rowID   bool
	columns []string
}

func (r *Runner) validate() error {
	if r == nil || r.DB == nil || r.Source == nil || !holderToken.MatchString(r.Holder) ||
		r.LeaseTTL < MinLeaseTTL || r.LeaseTTL > MaxLeaseTTL || r.LeaseRetry <= 0 ||
		r.BatchSize < 1 || r.BatchSize > MaxBatchSize {
		return errors.New("replay: runner requires a database, a source, a holder token, a lease ttl of 1 to 30 seconds, a lease retry and a batch size of 1 to 1000")
	}
	return nil
}

// Run waits for the writer lease, admits req against the cursor and replays
// it in batches of BatchSize heights, each in one transaction that is
// committed, or rolled back on a dry run, and releases the lease.
func (r *Runner) Run(ctx context.Context, req Request) (rep Report, err error) {
	if err := r.validate(); err != nil {
		return rep, err
	}
	if req.From < 0 {
		return rep, Refusal("from must not be negative")
	}
	if req.To != ToCursor && req.From > req.To {
		return rep, Refusal("from must not exceed to")
	}
	if err := waitLease(ctx, r.DB, r.Holder, r.LeaseTTL, r.LeaseRetry); err != nil {
		return rep, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, releaseLease(cleanup, r.DB, r.Holder))
	}()
	if req, err = Admit(ctx, r.DB, req); err != nil {
		return rep, err
	}
	rep.Request = req
	tables, err := blockTables(ctx, r.DB)
	if err != nil {
		return rep, err
	}
	totals := map[string]*TableCount{}
	wholeTables := map[string]bool{}
	for _, name := range projectionTables {
		wholeTables["perps."+name] = true
	}
	for from := req.From; from <= req.To; {
		to := from + int64(r.BatchSize) - 1
		if to > req.To {
			to = req.To
		}
		counts, err := r.batch(ctx, tables, from, to, req.DryRun)
		if err != nil {
			return rep, fmt.Errorf("replay heights %d to %d: %w", from, to, err)
		}
		for _, c := range counts {
			if t, ok := totals[c.Table]; ok {
				if wholeTables[c.Table] {
					t.Rows = c.Rows
				} else {
					t.Rows += c.Rows
				}
				t.Changed += c.Changed
				continue
			}
			totals[c.Table] = &c
			rep.Tables = append(rep.Tables, c)
		}
		from = to + 1
	}
	for i := range rep.Tables {
		rep.Tables[i] = *totals[rep.Tables[i].Table]
	}
	return rep, nil
}

// batch rewrites [from, to] in one transaction and returns its counts.
func (r *Runner) batch(ctx context.Context, tables []table, from, to int64, dryRun bool) ([]TableCount, error) {
	if err := renewLease(ctx, r.DB, r.Holder, r.LeaseTTL); err != nil {
		return nil, err
	}
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	// database/sql rolls a transaction back when its context is cancelled, and
	// a rollback after commit returns sql.ErrTxDone, so the error carries nothing.
	defer func() { _ = tx.Rollback() }()

	var cursorHeight int64
	var cursorHash []byte
	var cursorUpdated time.Time
	if err := tx.QueryRowContext(ctx, `SELECT height, hash, updated_at FROM perps.cursors WHERE name = $1 FOR UPDATE`, cursorName).
		Scan(&cursorHeight, &cursorHash, &cursorUpdated); err != nil {
		return nil, fmt.Errorf("lock cursor: %w", err)
	}
	if to > cursorHeight {
		return nil, Refusal(fmt.Sprintf("to is above the cursor %d", cursorHeight))
	}
	stored, err := storedHashes(ctx, tx, from, to)
	if err != nil {
		return nil, err
	}
	if err := snapshot(ctx, tx, tables, from, to); err != nil {
		return nil, err
	}
	var maxSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(max(perps_seq), 0) FROM perps.delivery_outbox`).Scan(&maxSeq); err != nil {
		return nil, fmt.Errorf("read outbox sequence: %w", err)
	}
	for _, t := range tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM perps.`+pq.QuoteIdentifier(t.name)+` WHERE block BETWEEN $1 AND $2`, from, to); err != nil {
			return nil, fmt.Errorf("delete perps.%s: %w", t.name, err)
		}
	}
	for h := from; h <= to; h++ {
		block, rows, err := r.Source.Read(ctx, tx, uint64(h))
		if err != nil {
			return nil, fmt.Errorf("read height %d: %w", h, err)
		}
		if !bytes.Equal(block.Hash.Bytes(), stored[h]) {
			return nil, fmt.Errorf("%w: height %d node %x stored %x", ErrHashMismatch, h, block.Hash.Bytes(), stored[h])
		}
		if _, err := store.WriteFacts(ctx, tx, block, rows); err != nil {
			return nil, fmt.Errorf("write height %d: %w", h, err)
		}
	}
	for i, t := range tables {
		if t.rowID {
			if err := restoreRowIDs(ctx, tx, t, snapshotName(i)); err != nil {
				return nil, err
			}
		}
	}
	if err := store.Rebuild(ctx, tx, from, to); err != nil {
		return nil, fmt.Errorf("rebuild projections: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE perps.cursors SET height = $2, hash = $3, updated_at = $4 WHERE name = $1`,
		cursorName, cursorHeight, cursorHash, cursorUpdated); err != nil {
		return nil, fmt.Errorf("restore cursor: %w", err)
	}
	counts, err := diff(ctx, tx, tables, from, to, maxSeq)
	if err != nil {
		return nil, err
	}
	if err := renewLease(ctx, tx, r.Holder, r.LeaseTTL); err != nil {
		return nil, err
	}
	if dryRun {
		return counts, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return counts, nil
}

// blockTables lists the base tables of schema perps with a block column: every
// fact table and the block-keyed projections, with their columns.
func blockTables(ctx context.Context, db *sql.DB) ([]table, error) {
	rows, err := db.QueryContext(ctx, `SELECT c.table_name, array_agg(c.column_name::text ORDER BY c.ordinal_position)
FROM information_schema.columns c
JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
WHERE c.table_schema = 'perps' AND t.table_type = 'BASE TABLE'
GROUP BY c.table_name
HAVING bool_or(c.column_name = 'block')
ORDER BY c.table_name`)
	if err != nil {
		return nil, fmt.Errorf("list block tables: %w", err)
	}
	defer rows.Close()
	var out []table
	for rows.Next() {
		var t table
		if err := rows.Scan(&t.name, pq.Array(&t.columns)); err != nil {
			return nil, fmt.Errorf("scan block tables: %w", err)
		}
		for _, c := range t.columns {
			if c == "row_id" {
				t.rowID = true
			}
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list block tables: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("replay: schema perps has no block tables")
	}
	return out, nil
}

func storedHashes(ctx context.Context, tx *sql.Tx, from, to int64) (map[int64][]byte, error) {
	rows, err := tx.QueryContext(ctx, `SELECT number, hash FROM perps.blocks WHERE number BETWEEN $1 AND $2`, from, to)
	if err != nil {
		return nil, fmt.Errorf("read stored blocks: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]byte, to-from+1)
	for rows.Next() {
		var n int64
		var h []byte
		if err := rows.Scan(&n, &h); err != nil {
			return nil, fmt.Errorf("scan stored blocks: %w", err)
		}
		out[n] = h
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read stored blocks: %w", err)
	}
	for h := from; h <= to; h++ {
		if _, ok := out[h]; !ok {
			return nil, Refusal(fmt.Sprintf("height %d is not indexed", h))
		}
	}
	return out, nil
}

func snapshotName(i int) string { return fmt.Sprintf("replay_before_%d", i) }

const (
	snapshotBlocks     = "replay_before_blocks"
	snapshotProjection = "replay_before_projection_"
)

// snapshot copies the rows of the range, and the projection tables whole, into
// temporary tables dropped with the transaction.
// ponytail: projections are copied whole per batch, fine while they fit in a
// transaction; key the copy by the accounts and markets of the range if they
// outgrow it.
func snapshot(ctx context.Context, tx *sql.Tx, tables []table, from, to int64) error {
	for i, t := range tables {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+snapshotName(i)+` ON COMMIT DROP AS SELECT * FROM perps.`+pq.QuoteIdentifier(t.name)+` WHERE block BETWEEN $1 AND $2`, from, to); err != nil {
			return fmt.Errorf("snapshot perps.%s: %w", t.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+snapshotBlocks+` ON COMMIT DROP AS SELECT * FROM perps.blocks WHERE number BETWEEN $1 AND $2`, from, to); err != nil {
		return fmt.Errorf("snapshot perps.blocks: %w", err)
	}
	for _, p := range projectionTables {
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE `+snapshotProjection+p+` ON COMMIT DROP AS SELECT * FROM perps.`+pq.QuoteIdentifier(p)); err != nil {
			return fmt.Errorf("snapshot perps.%s: %w", p, err)
		}
	}
	return nil
}

// restoreRowIDs gives every rewritten row of t the row_id of its first index,
// matched on (tx_hash, log_index, sub), so the served ids stay stable.
func restoreRowIDs(ctx context.Context, tx *sql.Tx, t table, snap string) error {
	var cols, ret []string
	for _, c := range t.columns {
		if c == "row_id" {
			continue
		}
		q := pq.QuoteIdentifier(c)
		cols = append(cols, q)
		ret = append(ret, "f."+q)
	}
	name := `perps.` + pq.QuoteIdentifier(t.name)
	query := `WITH moved AS (DELETE FROM ` + name + ` f USING ` + snap + ` s
WHERE f.tx_hash = s.tx_hash AND f.log_index = s.log_index AND f.sub = s.sub AND f.row_id <> s.row_id
RETURNING s.row_id AS replay_row_id, ` + strings.Join(ret, ", ") + `)
INSERT INTO ` + name + ` (row_id, ` + strings.Join(cols, ", ") + `) OVERRIDING SYSTEM VALUE
SELECT replay_row_id, ` + strings.Join(cols, ", ") + ` FROM moved`
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("restore row ids of %s: %w", name, err)
	}
	return nil
}

// diff counts the rows of each replayed scope after the rewrite and the rows
// that differ from the snapshot, then the outbox messages added.
func diff(ctx context.Context, tx *sql.Tx, tables []table, from, to, maxSeq int64) ([]TableCount, error) {
	type scope struct {
		table, current, before string
		args                   []any
	}
	var scopes []scope
	for i, t := range tables {
		scopes = append(scopes, scope{"perps." + t.name, `SELECT * FROM perps.` + pq.QuoteIdentifier(t.name) + ` WHERE block BETWEEN $1 AND $2`, snapshotName(i), []any{from, to}})
	}
	scopes = append(scopes, scope{"perps.blocks", `SELECT * FROM perps.blocks WHERE number BETWEEN $1 AND $2`, snapshotBlocks, []any{from, to}})
	for _, p := range projectionTables {
		scopes = append(scopes, scope{"perps." + p, `SELECT * FROM perps.` + pq.QuoteIdentifier(p), snapshotProjection + p, nil})
	}
	out := make([]TableCount, 0, len(scopes)+1)
	for _, s := range scopes {
		c := TableCount{Table: s.table}
		query := `SELECT (SELECT count(*) FROM (` + s.current + `) cur),
(SELECT count(*) FROM (SELECT * FROM ` + s.before + ` EXCEPT ALL ` + s.current + `) removed)
+ (SELECT count(*) FROM (` + s.current + ` EXCEPT ALL SELECT * FROM ` + s.before + `) added)`
		if err := tx.QueryRowContext(ctx, query, s.args...).Scan(&c.Rows, &c.Changed); err != nil {
			return nil, fmt.Errorf("count %s: %w", s.table, err)
		}
		out = append(out, c)
	}
	added := TableCount{Table: "perps.delivery_outbox"}
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM perps.delivery_outbox WHERE perps_seq > $1`, maxSeq).Scan(&added.Rows); err != nil {
		return nil, fmt.Errorf("count perps.delivery_outbox: %w", err)
	}
	added.Changed = added.Rows
	return append(out, added), nil
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// acquireLease takes the writer lease for holder when it is free, expired or
// already held by holder; it reports errLeaseHeld otherwise.
func acquireLease(ctx context.Context, db decode.Querier, holder string, ttl time.Duration) error {
	var got string
	err := db.QueryRowContext(ctx, `INSERT INTO perps.writer_lease (name, holder, expires_at)
VALUES ($1, $2, clock_timestamp() + $3::bigint * interval '1 millisecond')
ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
WHERE perps.writer_lease.expires_at <= clock_timestamp() OR perps.writer_lease.holder = EXCLUDED.holder
RETURNING holder`, leaseName, holder, ttl.Milliseconds()).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return errLeaseHeld
	}
	if err != nil {
		return fmt.Errorf("acquire writer lease: %w", err)
	}
	return nil
}

// waitLease retries acquireLease every retry until it succeeds or ctx ends.
func waitLease(ctx context.Context, db *sql.DB, holder string, ttl, retry time.Duration) error {
	ticker := time.NewTicker(retry)
	defer ticker.Stop()
	for {
		err := acquireLease(ctx, db, holder, ttl)
		if !errors.Is(err, errLeaseHeld) {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for writer lease: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

// renewLease extends the unexpired lease of holder through e. Run calls it
// before each batch and again inside the batch transaction before commit, so
// the commit is fenced by the lease row lock.
func renewLease(ctx context.Context, e execer, holder string, ttl time.Duration) error {
	res, err := e.ExecContext(ctx, `UPDATE perps.writer_lease SET expires_at = clock_timestamp() + $3::bigint * interval '1 millisecond'
WHERE name = $1 AND holder = $2 AND expires_at > clock_timestamp()`, leaseName, holder, ttl.Milliseconds())
	if err != nil {
		return fmt.Errorf("renew writer lease: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("renew writer lease: %w", err)
	}
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}

// releaseLease frees the lease of holder; the indexer waiting on it resumes.
func releaseLease(ctx context.Context, db *sql.DB, holder string) error {
	if _, err := db.ExecContext(ctx, `DELETE FROM perps.writer_lease WHERE name = $1 AND holder = $2`, leaseName, holder); err != nil {
		return fmt.Errorf("release writer lease: %w", err)
	}
	return nil
}
