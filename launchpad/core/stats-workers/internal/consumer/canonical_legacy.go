package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CanonicalLegacyBridge feeds the legacy stats read model (stats.pool_stats,
// pool_transactions, volume buckets, price snapshots and the swap-ledger
// holders behind /stats/*, /bff/token and the platform metrics) from the
// production canonical pipeline instead of the development HMAC webhook.
//
// Sources are committed canonical facts only: pool creations from the indexer
// canonical creation table, and market fills this service already committed
// (accounting_market_commits) together with the authorized EventEmitter Swap
// log at the fill's position, which carries the post-trade spot price. Each
// fill is replayed through the same SwapConsumer and HolderTracker the webhook
// drives, in canonical chain order. stats.pool_transactions (keyed
// "<txHash>-<logIndex>") is the inbox: a fill is selected only while its row is
// absent, and a single advisory lock serializes bridge instances. Each fill's
// inbox row and all of its derived legacy writes (holder delta included) commit
// in one transaction (projectFill), so the inbox row exists exactly when the
// fill's projection is durable.
type CanonicalLegacyBridge struct {
	Pool              *pgxpool.Pool
	ProjectionSchema  string
	SourceSchema      string
	SourceStream      string
	ProjectionVersion string
	Market            *MarketConsumer
	Swap              *SwapConsumer
	Holder            *HolderTracker
	Batch             int
}

const (
	canonicalLegacyLock = "stats:canonical-legacy-bridge:v1"
	legacySwapTopic     = "0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc"
)

var legacySchema = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

// ErrLegacyBridgeIntegrity marks committed canonical evidence that does not
// bind (missing or mismatching EventEmitter log); the bridge stops rather than
// skip an event.
var ErrLegacyBridgeIntegrity = errors.New("stats: canonical legacy bridge evidence mismatch")

func (b *CanonicalLegacyBridge) validate() error {
	if b == nil || b.Pool == nil || !legacySchema.MatchString(b.ProjectionSchema) || !legacySchema.MatchString(b.SourceSchema) || b.SourceStream == "" || b.ProjectionVersion == "" || len(b.ProjectionVersion) > 64 || b.Market == nil || b.Swap == nil || b.Holder == nil || b.Batch < 1 || b.Batch > 1000 {
		return errors.New("stats: canonical legacy bridge dependencies required")
	}
	return nil
}

func (b *CanonicalLegacyBridge) source(name string) string {
	return pgx.Identifier{b.SourceSchema, name}.Sanitize()
}

// RunOnce bootstraps pool_stats rows for new canonical pools and replays up to
// Batch committed fills. It returns the number of events applied; zero when
// another instance holds the bridge lock.
func (b *CanonicalLegacyBridge) RunOnce(ctx context.Context) (int, error) {
	if err := b.validate(); err != nil {
		return 0, err
	}
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	var locked bool
	if err = tx.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", canonicalLegacyLock).Scan(&locked); err != nil || !locked {
		return 0, err
	}
	var raw, typed int64
	var alive bool
	err = tx.QueryRow(ctx, "SELECT w.raw_complete_height,w.typed_complete_height,l.expires_at>clock_timestamp() FROM "+b.source("indexer_ingest_watermarks_v1")+" w JOIN "+b.source("indexer_ingest_leases_v1")+" l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1", b.SourceStream).Scan(&raw, &typed, &alive)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrAccountingSourceUnavailable
	}
	if err != nil {
		return 0, err
	}
	if raw < 0 || raw != typed || !alive {
		return 0, ErrAccountingSourceUnavailable
	}
	applied, err := b.creations(ctx, tx, typed)
	if err != nil {
		return applied, err
	}
	swaps, err := b.swaps(ctx, tx, typed)
	return applied + swaps, err
}

func (b *CanonicalLegacyBridge) creations(ctx context.Context, tx pgx.Tx, typed int64) (int, error) {
	rows, err := tx.Query(ctx, "SELECT k.pool,k.token,k.creator,k.block_timestamp FROM "+b.source("indexer_canonical_creations_v1")+" k WHERE k.height<=$1 AND NOT EXISTS(SELECT 1 FROM stats.pool_stats s WHERE s.pool_address=k.pool) ORDER BY k.height,k.transaction_index,k.log_index LIMIT $2", typed, b.Batch)
	if err != nil {
		return 0, err
	}
	type creation struct {
		pool, token, creator string
		created              int64
	}
	pending := []creation{}
	for rows.Next() {
		var c creation
		if err = rows.Scan(&c.pool, &c.token, &c.creator, &c.created); err != nil {
			rows.Close()
			return 0, err
		}
		pending = append(pending, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for i, c := range pending {
		creator := strings.ToLower(c.creator)
		if err = b.Market.ProcessCanonical(ctx, MarketEvent{Pool: strings.ToLower(c.pool), Token: strings.ToLower(c.token), Creator: &creator}, c.created); err != nil {
			return i, err
		}
	}
	return len(pending), nil
}

// LegacySwapFromCanonical binds a committed fill to its EventEmitter Swap log
// and returns the webhook-equivalent swap event.
func LegacySwapFromCanonical(fill accounting.FillV1, emitter string, topics []string, data []byte) (SwapEvent, error) {
	var ev SwapEvent
	if accounting.ValidateFillV1(fill) != nil || len(topics) != 3 || len(data) != 7*32 {
		return ev, ErrLegacyBridgeIntegrity
	}
	if strings.ToLower(topics[0]) != legacySwapTopic || strings.ToLower(topics[1]) != "0x000000000000000000000000"+strings.TrimPrefix(fill.PoolAddress, "0x") || strings.ToLower(emitter) != fill.ActualEmitter || !strings.HasPrefix(strings.ToLower(topics[2]), "0x000000000000000000000000") || len(topics[2]) != 66 {
		return ev, ErrLegacyBridgeIntegrity
	}
	word := func(i int) *big.Int { return new(big.Int).SetBytes(data[i*32 : (i+1)*32]) }
	isBuy := word(0)
	output := fill.USDLAmountRaw
	if fill.Side == "buy" {
		output = fill.TokenAmountRaw
	}
	if isBuy.Cmp(big.NewInt(1)) > 0 || (isBuy.Sign() == 1) != (fill.Side == "buy") || word(1).String() != fill.PaidInputRaw || word(2).String() != output || word(3).String() != fill.FeeRaw || word(6).String() != fill.Position.Height || word(5).String() != fill.BlockTimestampSeconds {
		return ev, ErrLegacyBridgeIntegrity
	}
	logIndex, err := strconv.Atoi(fill.Position.LogIndex)
	if err != nil {
		return ev, ErrLegacyBridgeIntegrity
	}
	timestamp, err := strconv.ParseInt(fill.BlockTimestampSeconds, 10, 64)
	if err != nil {
		return ev, ErrLegacyBridgeIntegrity
	}
	return SwapEvent{PoolAddress: fill.PoolAddress, Sender: "0x" + strings.ToLower(topics[2][26:]), IsBuy: fill.Side == "buy", AmountIn: word(1).String(), AmountOut: word(2).String(), Price: word(4).String(), Fee: word(3).String(), BlockTimestamp: timestamp, TxHash: fill.Position.TransactionHash, LogIndex: logIndex}, nil
}

func (b *CanonicalLegacyBridge) swaps(ctx context.Context, tx pgx.Tx, typed int64) (int, error) {
	commits := pgx.Identifier{b.ProjectionSchema, "accounting_market_commits"}.Sanitize()
	rows, err := tx.Query(ctx, `SELECT c.fill,r.emitter,r.topics,r.data FROM `+commits+` c
 LEFT JOIN `+b.source("indexer_raw_logs_v1")+` r ON r.chain_id=125 AND r.transaction_hash=c.fill->'position'->>'transactionHash' AND r.log_index=(c.fill->'position'->>'logIndex')::bigint AND r.block_hash=c.fill->'position'->>'blockHash'
 WHERE c.projection_version=$1 AND (c.fill->'position'->>'height')::numeric<=$2::numeric
  AND EXISTS(SELECT 1 FROM stats.pool_stats s WHERE s.pool_address=c.pool)
  AND NOT EXISTS(SELECT 1 FROM stats.pool_transactions p WHERE p.id=(c.fill->'position'->>'transactionHash')||'-'||(c.fill->'position'->>'logIndex'))
 ORDER BY (c.fill->'position'->>'height')::numeric,(c.fill->'position'->>'transactionIndex')::numeric,(c.fill->'position'->>'logIndex')::numeric LIMIT $3`, b.ProjectionVersion, typed, b.Batch)
	if err != nil {
		return 0, err
	}
	events := []SwapEvent{}
	for rows.Next() {
		var fillRaw, topicsRaw []byte
		var emitter *string
		var data []byte
		if err = rows.Scan(&fillRaw, &emitter, &topicsRaw, &data); err != nil {
			rows.Close()
			return 0, err
		}
		fill, decodeErr := accounting.DecodeFillV1(fillRaw)
		var topics []string
		if decodeErr != nil || emitter == nil || json.Unmarshal(topicsRaw, &topics) != nil {
			rows.Close()
			return 0, ErrLegacyBridgeIntegrity
		}
		ev, bindErr := LegacySwapFromCanonical(fill, *emitter, topics, data)
		if bindErr != nil {
			rows.Close()
			return 0, fmt.Errorf("%w: fill %s", bindErr, fill.SourceFillID)
		}
		events = append(events, ev)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return 0, err
	}
	for i, ev := range events {
		if err = b.projectFill(ctx, ev); err != nil {
			return i, err
		}
	}
	return len(events), nil
}

// projectFill applies one fill's whole legacy projection in a single database
// transaction: the stats.pool_transactions inbox row ("<txHash>-<logIndex>"),
// the volume bucket and bucket trader, rolling pool_stats, the price snapshot,
// pool_stats price/changes and the stats.pool_holders delta. The inbox row is
// written first inside the transaction but becomes visible to other sessions
// (core/api's holders freshness probe, the next bridge pass) only in the same
// commit as every derived write, so "fresh" is never observable before the
// holder change is durable. Any failure rolls the whole fill back, leaving no
// inbox row, and the caller stops the batch so the next pass retries this same
// earliest pending fill in chain order. Redis cache and the debounced holder
// refresh run only after the commit.
func (b *CanonicalLegacyBridge) projectFill(ctx context.Context, ev SwapEvent) error {
	tx, err := b.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	inserted, err := b.Swap.ProjectTx(ctx, tx, ev)
	if err != nil {
		return err
	}
	if !inserted {
		// Already projected by a committed transaction: the inbox row only
		// exists alongside its derived writes, so there is nothing to apply.
		return nil
	}
	holderApplied, err := b.Holder.ApplySwapTx(ctx, tx, HolderSwap{PoolAddress: ev.PoolAddress, Sender: ev.Sender, IsBuy: ev.IsBuy, AmountIn: ev.AmountIn, AmountOut: ev.AmountOut})
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if err = b.Swap.CachePoolStats(ctx, ev.PoolAddress); err != nil && b.Swap.logger != nil {
		// The projection is durable; the cache is rebuilt on the next write.
		b.Swap.logger.Warn("legacy bridge pool stats cache refresh failed", slog.String("pool", ev.PoolAddress), slog.Any("err", err))
	}
	if holderApplied {
		b.Holder.ScheduleRefresh(ev.PoolAddress)
	}
	return nil
}
