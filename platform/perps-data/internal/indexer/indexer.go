// Package indexer runs the perps indexer: under the single-writer lease it
// steps the head follower from the stored cursor, decodes every block, writes
// the facts and projections of each block in one transaction and drains the
// outbox onto JetStream through the publisher.
//
// Origin: Launch-x core/indexer/internal/processor (Processor.Run and
// ProcessBatch), with the follower of internal/source in place of the head and
// log sources and the outbox publisher in place of the webhook fan-out.
package indexer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

// Bounds of contracts/data-services.md sections 5 and 19.
const (
	// MaxBatch is the largest range of one follower step; a caught-up
	// follower steps one height at the head cadence.
	MaxBatch = 500
	// MaxLagBlocks is the largest lag to the node head of a ready indexer.
	MaxLagBlocks = 30
	// MaxOutboxAge is the largest age of the oldest pending outbox row of a
	// ready indexer.
	MaxOutboxAge = 30 * time.Second
	// LeaseTTL is the validity of one acquisition or renewal of the lease.
	LeaseTTL = 30 * time.Second
	// RenewInterval separates two renewals of a held lease.
	RenewInterval = 10 * time.Second
	// AcquireInterval separates two attempts while another instance holds
	// the lease.
	AcquireInterval = time.Second
	// StepRetryDelay separates a failed step or block write from the next
	// attempt at the same height.
	StepRetryDelay = time.Second
)

// leaseName is the perps.writer_lease row of the indexer.
const leaseName = "indexer"

// rawLogsTable labels the raw rows in perps_indexer_logs_total.
const rawLogsTable = "raw_logs"

// ErrLeaseLost reports that the writer lease expired or was taken by another
// instance; the process must exit.
var ErrLeaseLost = errors.New("indexer: writer lease lost")

const (
	acquireSQL = `INSERT INTO perps.writer_lease (name, holder, expires_at)
VALUES ($1, $2, clock_timestamp() + $3::bigint * interval '1 millisecond')
ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at
WHERE perps.writer_lease.holder = EXCLUDED.holder OR perps.writer_lease.expires_at <= clock_timestamp()
RETURNING holder`
	renewSQL = `UPDATE perps.writer_lease SET expires_at = clock_timestamp() + $3::bigint * interval '1 millisecond'
WHERE name = $1 AND holder = $2 AND expires_at > clock_timestamp()`
	fenceSQL   = `SELECT 1 FROM perps.writer_lease WHERE name = $1 AND holder = $2 AND expires_at > clock_timestamp() FOR SHARE`
	releaseSQL = `DELETE FROM perps.writer_lease WHERE name = $1 AND holder = $2`
	outboxSQL  = `SELECT count(*), COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)) * 1000, 0)::bigint
FROM perps.delivery_outbox WHERE state = 'pending'`
)

// Deps wires an Indexer. Every field except Logger is required.
type Deps struct {
	DB        *sql.DB
	Follower  *source.Follower
	Registry  *decode.Registry
	Publisher *publish.Publisher
	Metrics   *telemetry.Metrics
	// Holder names this process in perps.writer_lease.
	Holder string
	// StartHeight is the first height on an empty cursor (PERPS_START_HEIGHT).
	StartHeight int64
	Logger      *slog.Logger
}

// Indexer is the perps indexer process loop.
type Indexer struct {
	db        *sql.DB
	follower  *source.Follower
	registry  *decode.Registry
	publisher *publish.Publisher
	holder    string
	start     uint64
	logger    *slog.Logger

	held    atomic.Bool
	head    atomic.Uint64
	cursor  atomic.Int64
	renewed time.Time

	headHeight    prometheus.Gauge
	cursorHeight  prometheus.Gauge
	lagBlocks     prometheus.Gauge
	logs          *prometheus.CounterVec
	rewinds       prometheus.Counter
	outboxPending prometheus.Gauge
}

// New builds an Indexer and registers its metrics on d.Metrics. The decoder
// counters perps_indexer_unknown_logs_total and perps_indexer_enum_unknown_total
// are registered by decode.NewRegistry on the same Metrics.
func New(d Deps) (*Indexer, error) {
	if d.DB == nil || d.Follower == nil || d.Registry == nil || d.Publisher == nil || d.Metrics == nil {
		return nil, errors.New("indexer: database, follower, registry, publisher and metrics are required")
	}
	if d.Holder == "" || len(d.Holder) > 128 {
		return nil, errors.New("indexer: holder must be 1 to 128 bytes")
	}
	if d.StartHeight < 0 {
		return nil, errors.New("indexer: start height must not be below zero")
	}
	logger := d.Logger
	if logger == nil {
		logger = slog.Default()
	}
	ix := &Indexer{
		db:        d.DB,
		follower:  d.Follower,
		registry:  d.Registry,
		publisher: d.Publisher,
		holder:    d.Holder,
		start:     uint64(d.StartHeight),
		logger:    logger,
		headHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "perps_indexer_head_height",
			Help: "Latest node head height seen by the follower.",
		}),
		cursorHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "perps_indexer_cursor_height",
			Help: "Highest height written to Postgres.",
		}),
		lagBlocks: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "perps_indexer_lag_blocks",
			Help: "Node head height minus cursor height.",
		}),
		logs: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "perps_indexer_logs_total",
			Help: "Rows written from logs and block events, by table.",
		}, []string{"table"}),
		rewinds: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "perps_indexer_rewinds_total",
			Help: "Fork rewinds performed by the follower.",
		}),
		outboxPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "perps_outbox_pending",
			Help: "Outbox rows not yet published.",
		}),
	}
	ix.cursor.Store(-1)
	for _, c := range []prometheus.Collector{ix.headHeight, ix.cursorHeight, ix.lagBlocks, ix.logs, ix.rewinds, ix.outboxPending} {
		if err := d.Metrics.Register(c); err != nil {
			return nil, fmt.Errorf("indexer: register metric: %w", err)
		}
	}
	return ix, nil
}

// Run acquires the writer lease, waiting while another instance holds it,
// starts the publisher and indexes from the cursor plus one, or from the start
// height on an empty cursor, until ctx ends or the lease is lost. It returns
// ctx.Err() on cancellation and ErrLeaseLost when the lease is lost.
func (ix *Indexer) Run(ctx context.Context) error {
	if err := ix.acquire(ctx); err != nil {
		return err
	}
	defer ix.release(ctx)
	next, err := ix.resume(ctx)
	if err != nil {
		return err
	}
	pubCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := ix.publisher.Run(pubCtx); err != nil && !errors.Is(err, context.Canceled) {
			ix.logger.Error("perps publisher stopped", slog.Any("err", err))
		}
	}()
	err = ix.follow(ctx, next)
	cancel()
	wg.Wait()
	return err
}

// Checks returns the readiness checks of the indexer for httpx.HealthDeps:
// lease (the writer lease is held), lag (the cursor is at most MaxLagBlocks
// behind the node head) and outbox (no pending row older than MaxOutboxAge).
func (ix *Indexer) Checks() map[string]httpx.Check {
	return map[string]httpx.Check{
		"lease":  ix.checkLease,
		"lag":    ix.checkLag,
		"outbox": ix.checkOutbox,
	}
}

func (ix *Indexer) checkLease(ctx context.Context) error {
	if !ix.held.Load() {
		return errors.New("lease not held")
	}
	var one int
	err := ix.db.QueryRowContext(ctx, `SELECT 1 FROM perps.writer_lease WHERE name = $1 AND holder = $2 AND expires_at > clock_timestamp()`, leaseName, ix.holder).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("lease expired")
	}
	return err
}

func (ix *Indexer) checkLag(context.Context) error {
	head := ix.head.Load()
	if head == 0 {
		return errors.New("head unknown")
	}
	if lag := lag(head, ix.cursor.Load()); lag > MaxLagBlocks {
		return fmt.Errorf("lag %d blocks", lag)
	}
	return nil
}

func (ix *Indexer) checkOutbox(ctx context.Context) error {
	_, oldest, err := ix.observeOutbox(ctx)
	if err != nil {
		return err
	}
	if oldest > MaxOutboxAge {
		return fmt.Errorf("oldest pending outbox row %s old", oldest)
	}
	return nil
}

// observeOutbox reads the pending outbox rows and their oldest age and sets
// perps_outbox_pending.
func (ix *Indexer) observeOutbox(ctx context.Context) (int64, time.Duration, error) {
	var pending, oldestMs int64
	if err := ix.db.QueryRowContext(ctx, outboxSQL).Scan(&pending, &oldestMs); err != nil {
		return 0, 0, fmt.Errorf("indexer: read outbox: %w", err)
	}
	ix.outboxPending.Set(float64(pending))
	return pending, time.Duration(oldestMs) * time.Millisecond, nil
}

// resume returns the first height to index: the cursor plus one, or the start
// height on an empty cursor.
func (ix *Indexer) resume(ctx context.Context) (uint64, error) {
	height, ok, err := store.Cursor(ctx, ix.db)
	if err != nil {
		return 0, fmt.Errorf("indexer: read cursor: %w", err)
	}
	next := ix.start
	if ok {
		next = height + 1
	}
	ix.setCursor(int64(next) - 1)
	return next, nil
}

// follow steps the follower from next until ctx ends or the lease is lost. A
// failed step or block write is logged and retried at the same height after
// StepRetryDelay; nothing of a failed block is committed.
func (ix *Indexer) follow(ctx context.Context, next uint64) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if time.Since(ix.renewed) >= RenewInterval {
			if err := ix.renew(ctx); err != nil {
				return err
			}
		}
		n, err := ix.step(ctx, next)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, ErrLeaseLost) {
				return err
			}
			ix.logger.Error("perps indexer step failed, retrying", slog.Uint64("height", next), slog.Any("err", err))
			if !sleep(ctx, StepRetryDelay) {
				return ctx.Err()
			}
		}
		next = n
	}
}

// step runs one follower step from next and writes its blocks, returning the
// next height to index. After a rewind the next height is the fork height.
func (ix *Indexer) step(ctx context.Context, next uint64) (uint64, error) {
	res, err := ix.follower.Step(ctx, next, MaxBatch)
	if err != nil {
		return next, fmt.Errorf("follower step at %d: %w", next, err)
	}
	ix.setHead(res.Head)
	if res.Rewind != nil {
		fork := *res.Rewind
		if fork > next {
			return next, fmt.Errorf("indexer: rewind fork height %d above next height %d", fork, next)
		}
		ix.rewinds.Inc()
		ix.setCursor(int64(fork) - 1)
		ix.logger.Warn("perps indexer rewound", slog.Uint64("fork_height", fork))
		return fork, nil
	}
	if len(res.Blocks) > MaxBatch {
		return next, fmt.Errorf("indexer: follower returned %d blocks, above %d", len(res.Blocks), MaxBatch)
	}
	for _, b := range res.Blocks {
		if b.Header.Number != next {
			return next, fmt.Errorf("indexer: follower returned height %d, want %d", b.Header.Number, next)
		}
		counts, err := ix.writeBlock(ctx, b)
		if err != nil {
			return next, fmt.Errorf("write block %d: %w", next, err)
		}
		for table, n := range counts {
			ix.logs.WithLabelValues(table).Add(float64(n))
		}
		ix.setCursor(int64(next))
		next++
	}
	if len(res.Blocks) > 0 {
		if _, _, err := ix.observeOutbox(ctx); err != nil {
			ix.logger.Warn("perps indexer outbox read failed", slog.Any("err", err))
		}
	}
	return next, nil
}

// writeBlock decodes b and writes its block row, facts, raw logs, projections,
// outbox rows and cursor in one transaction fenced by the writer lease, and
// returns the written rows by table.
func (ix *Indexer) writeBlock(ctx context.Context, b source.Block) (map[string]int, error) {
	tx, err := ix.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	var one int
	if err := tx.QueryRowContext(ctx, fenceSQL, leaseName, ix.holder).Scan(&one); err != nil {
		rollback(tx)
		if errors.Is(err, sql.ErrNoRows) {
			ix.held.Store(false)
			return nil, ErrLeaseLost
		}
		return nil, err
	}
	rows, err := ix.registry.Decode(ctx, tx, b.Logs)
	if err != nil {
		rollback(tx)
		return nil, err
	}
	if err := store.WriteBlock(ctx, tx, b.Header, rows); err != nil {
		rollback(tx)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, d := range rows.Decoded {
		for _, r := range d.Rows {
			counts[r.Table()]++
		}
	}
	if len(rows.Raw) > 0 {
		counts[rawLogsTable] += len(rows.Raw)
	}
	return counts, nil
}

// acquire takes the writer lease, waiting AcquireInterval between attempts
// while another live holder has it.
func (ix *Indexer) acquire(ctx context.Context) error {
	for {
		var holder string
		err := ix.db.QueryRowContext(ctx, acquireSQL, leaseName, ix.holder, LeaseTTL.Milliseconds()).Scan(&holder)
		if err == nil {
			ix.held.Store(true)
			ix.renewed = time.Now()
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			ix.logger.Error("perps indexer lease acquisition failed", slog.Any("err", err))
		}
		if !sleep(ctx, AcquireInterval) {
			return ctx.Err()
		}
	}
}

// renew extends the held lease and reports ErrLeaseLost when it expired or
// another instance took it.
func (ix *Indexer) renew(ctx context.Context) error {
	res, err := ix.db.ExecContext(ctx, renewSQL, leaseName, ix.holder, LeaseTTL.Milliseconds())
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ix.logger.Error("perps indexer lease renewal failed", slog.Any("err", err))
		return nil
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		ix.held.Store(false)
		return ErrLeaseLost
	}
	ix.renewed = time.Now()
	return nil
}

// release gives the lease up so a waiting instance takes it at once.
func (ix *Indexer) release(ctx context.Context) {
	ix.held.Store(false)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := ix.db.ExecContext(cleanup, releaseSQL, leaseName, ix.holder); err != nil {
		ix.logger.Warn("perps indexer lease release failed", slog.Any("err", err))
	}
}

func (ix *Indexer) setHead(head uint64) {
	ix.head.Store(head)
	ix.headHeight.Set(float64(head))
	ix.lagBlocks.Set(float64(lag(head, ix.cursor.Load())))
}

func (ix *Indexer) setCursor(cursor int64) {
	ix.cursor.Store(cursor)
	ix.cursorHeight.Set(float64(max(cursor, 0)))
	ix.lagBlocks.Set(float64(lag(ix.head.Load(), cursor)))
}

// lag is head minus cursor, zero when the cursor is at or above the head; a
// cursor of -1 means nothing is written yet.
func lag(head uint64, cursor int64) uint64 {
	if cursor >= 0 && uint64(cursor) >= head {
		return 0
	}
	if cursor < 0 {
		return head + 1
	}
	return head - uint64(cursor)
}

// database/sql rolls a transaction back when its context ends, and the
// rollback of a failed write carries nothing the caller can act on.
func rollback(tx *sql.Tx) { _ = tx.Rollback() }

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
