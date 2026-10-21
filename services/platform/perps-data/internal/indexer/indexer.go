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
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

// Bounds of contracts/data-services.md sections 5 and 19.
const (
	// IdleInterval separates two follower steps of a caught-up indexer, give
	// or take IdleJitter (Launch-x core/indexer/internal/source/committed.go:830).
	IdleInterval = 100 * time.Millisecond
	IdleJitter   = 10 * time.Millisecond
	// MaxLagBlocks is the largest lag to the node head of a ready indexer.
	MaxLagBlocks = 30
	// MaxOutboxAge is the largest age of the oldest open outbox row of a
	// ready indexer, the MaxHeadAge of its publisher.
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
	releaseSQL = `DELETE FROM perps.writer_lease WHERE name = $1 AND holder = $2`
	outboxSQL  = `SELECT count(*), COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)) * 1000, 0)::bigint
FROM perps.delivery_outbox WHERE state = 'pending'`
)

// Deps wires an Indexer. Every field except Logger is required.
type Deps struct {
	DB        *sql.DB
	RPC       *source.RPC
	Comet     *source.Comet
	Outbox    *delivery.Store
	ChainID   uint64
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
	rpc       *source.RPC
	follower  *source.Follower
	registry  *decode.Registry
	publisher *publish.Publisher
	holder    string
	start     uint64
	logger    *slog.Logger

	// written holds the rows by table of each block written in the current
	// step, in block order; the follower commits them in that order.
	written []map[string]int
	// lookedUp holds the orders and positions whose account lookup through
	// the node is logged, so each is logged once.
	lookedUp map[string]bool

	held   atomic.Bool
	head   atomic.Uint64
	cursor atomic.Int64

	headHeight    prometheus.Gauge
	cursorHeight  prometheus.Gauge
	lagBlocks     prometheus.Gauge
	logs          *prometheus.CounterVec
	rewinds       prometheus.Counter
	outboxPending prometheus.Gauge
}

// New builds an Indexer over a follower of d.RPC and d.Comet whose block
// write and rewind rebuild are the indexer's, and registers its metrics on
// d.Metrics. The decoder counters perps_indexer_unknown_logs_total and
// perps_indexer_enum_unknown_total are registered by decode.NewRegistry on the
// same Metrics.
func New(ctx context.Context, d Deps) (*Indexer, error) {
	if d.DB == nil || d.RPC == nil || d.Comet == nil || d.Outbox == nil || d.Registry == nil || d.Publisher == nil || d.Metrics == nil {
		return nil, errors.New("indexer: database, rpc, comet, outbox, registry, publisher and metrics are required")
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
		rpc:       d.RPC,
		registry:  d.Registry,
		publisher: d.Publisher,
		holder:    d.Holder,
		start:     uint64(d.StartHeight),
		logger:    logger,
		lookedUp:  map[string]bool{},
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
	for _, c := range append([]prometheus.Collector{ix.headHeight, ix.cursorHeight, ix.lagBlocks, ix.logs, ix.rewinds, ix.outboxPending}, d.Publisher.Collectors()...) {
		if err := d.Metrics.Register(c); err != nil {
			return nil, fmt.Errorf("indexer: register metric: %w", err)
		}
	}
	follower, err := source.NewFollower(ctx, source.FollowerOptions{
		DB: d.DB, RPC: d.RPC, Comet: d.Comet, Outbox: d.Outbox, ChainID: d.ChainID, StartHeight: uint64(d.StartHeight),
		Logger: logger, Write: ix.write, Rebuild: ix.rebuild,
	})
	if err != nil {
		return nil, fmt.Errorf("indexer: follower: %w", err)
	}
	ix.follower = follower
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
	runCtx, cancel := context.WithCancelCause(ctx)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := ix.publisher.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			ix.logger.Error("perps publisher stopped", slog.Any("err", err))
		}
	}()
	go func() {
		defer wg.Done()
		if err := ix.keepLease(runCtx); errors.Is(err, ErrLeaseLost) {
			cancel(err)
		}
	}()
	err = ix.follow(runCtx, next)
	if cause := context.Cause(runCtx); errors.Is(cause, ErrLeaseLost) {
		err = cause
	}
	cancel(nil)
	wg.Wait()
	return err
}

// keepLease renews the held lease every RenewInterval until ctx ends, so a
// long catch-up step never outlives the lease, and returns ErrLeaseLost when
// a renewal finds it expired or taken.
func (ix *Indexer) keepLease(ctx context.Context) error {
	t := time.NewTicker(RenewInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
		if err := ix.renew(ctx); err != nil {
			return err
		}
	}
}

// Checks returns the readiness checks of the indexer for httpx.HealthDeps:
// lease (the writer lease is held), lag (the cursor is at most MaxLagBlocks
// behind the node head) and outbox (the publisher is ready: no open outbox
// row older than its MaxHeadAge).
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
	if _, _, err := ix.observeOutbox(ctx); err != nil {
		return err
	}
	return ix.publisher.Ready(ctx)
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

// resume reads the cursor into the cursor gauge; the follower indexes from
// the cursor plus one, or from the start height on an empty cursor.
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

// follow steps the follower from next until ctx ends, the lease is lost or a
// synthetic receipt mismatch stops the follower. A failed step is logged and
// retried after StepRetryDelay; nothing of a failed block is committed.
func (ix *Indexer) follow(ctx context.Context, next uint64) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := ix.step(ctx, next)
		if err != nil {
			var mismatch *source.SyntheticMismatchError
			switch {
			case ctx.Err() != nil:
				return ctx.Err()
			case errors.Is(err, ErrLeaseLost), errors.As(err, &mismatch):
				return err
			}
			ix.logger.Error("perps indexer step failed, retrying", slog.Uint64("height", next), slog.Any("err", err))
			if !sleep(ctx, StepRetryDelay) {
				return ctx.Err()
			}
			continue
		}
		if n == next && !sleep(ctx, IdleInterval+rand.N(2*IdleJitter+time.Millisecond)-IdleJitter) {
			return ctx.Err()
		}
		next = n
	}
}

// step runs one follower step, which writes its blocks through write and
// rewinds through rebuild, and returns the next height to index read back
// from the cursor; a cursor below next-1 after the step is a rewind.
func (ix *Indexer) step(ctx context.Context, next uint64) (uint64, error) {
	ix.written = ix.written[:0]
	stored, err := ix.follower.Step(ctx)
	for _, counts := range ix.written[:min(stored, len(ix.written))] {
		for table, n := range counts {
			ix.logs.WithLabelValues(table).Add(float64(n))
		}
	}
	if err != nil {
		return next, fmt.Errorf("follower step at %d: %w", next, err)
	}
	height, ok, err := store.Cursor(ctx, ix.db)
	if err != nil {
		return next, fmt.Errorf("indexer: read cursor: %w", err)
	}
	after := ix.start
	if ok {
		after = height + 1
	}
	if after < next {
		ix.rewinds.Inc()
		ix.logger.Warn("perps indexer rewound", slog.Uint64("fork_height", after))
	}
	ix.setCursor(int64(after) - 1)
	if head, err := ix.rpc.Head(ctx); err == nil {
		ix.setHead(head)
	} else {
		ix.logger.Warn("perps indexer head read failed", slog.Any("err", err))
	}
	if stored > 0 {
		if _, _, err := ix.observeOutbox(ctx); err != nil {
			ix.logger.Warn("perps indexer outbox read failed", slog.Any("err", err))
		}
	}
	return after, nil
}

// fence holds the writer lease for the rest of tx and reports ErrLeaseLost
// when this instance no longer holds it.
func (ix *Indexer) fence(ctx context.Context, tx *sql.Tx) error {
	err := store.Fence(ctx, tx, leaseName, ix.holder)
	if errors.Is(err, store.ErrLeaseHeld) {
		ix.held.Store(false)
		return fmt.Errorf("%w: %w", ErrLeaseLost, err)
	}
	return err
}

// write is the follower's block write: under the lease fence it decodes b and
// writes its facts, raw logs, projections and outbox rows in tx, after the
// follower's perps.blocks row and before its cursor.
func (ix *Indexer) write(ctx context.Context, tx *sql.Tx, b *source.Block) error {
	if err := ix.fence(ctx, tx); err != nil {
		return err
	}
	rows, err := ix.registry.DecodeBlock(ctx, tx, decode.BlockInput{Number: b.Number, Hash: b.Hash, Time: b.Time, Logs: b.Logs, Events: b.FinalizeBlockEvents})
	if err != nil {
		return err
	}
	header := store.Block{Number: b.Number, Hash: b.Hash, ParentHash: b.ParentHash, Time: b.Time, Proposer: b.Proposer, TxCount: b.TxCount}
	if err := store.WriteBlock(ctx, tx, header, rows); err != nil {
		return err
	}
	ix.logLookups(rows.Lookups)
	counts := map[string]int{}
	for _, d := range rows.Decoded {
		for _, r := range d.Rows {
			counts[r.Table()]++
		}
	}
	if len(rows.Raw) > 0 {
		counts[rawLogsTable] += len(rows.Raw)
	}
	ix.written = append(ix.written, counts)
	return nil
}

// logLookups logs each order or position read through the node once: the
// height and path that resolved its account and projection seed, or that the
// precompile does not know it and its events are account_lookup or
// projection_lookup raw logs.
func (ix *Indexer) logLookups(lookups []decode.AccountLookup) {
	for _, l := range lookups {
		k := l.What + " " + l.ID
		if ix.lookedUp[k] {
			continue
		}
		ix.lookedUp[k] = true
		attrs := []any{slog.String(l.What, l.ID), slog.Uint64("height", l.Height), slog.String("path", l.Path)}
		if l.Missing {
			ix.logger.Warn("perps indexer state lookup: unknown to the perps precompile, events written as raw logs", attrs...)
			continue
		}
		ix.logger.Info("perps indexer state lookup resolved through the node, projection seeded", append(attrs, slog.String("account", l.Account.Hex()))...)
	}
}

// rebuild is the follower's rewind rebuild: under the lease fence it
// re-projects the stored facts after the rewound blocks are deleted.
func (ix *Indexer) rebuild(ctx context.Context, tx *sql.Tx, a source.Affected) error {
	if err := ix.fence(ctx, tx); err != nil {
		return err
	}
	return store.Rebuild(ctx, tx, int64(a.From), int64(a.From))
}

// acquire takes the writer lease, waiting AcquireInterval between attempts
// while another live holder has it.
func (ix *Indexer) acquire(ctx context.Context) error {
	for {
		var holder string
		err := ix.db.QueryRowContext(ctx, acquireSQL, leaseName, ix.holder, LeaseTTL.Milliseconds()).Scan(&holder)
		if err == nil {
			ix.held.Store(true)
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
