// Package engine implements the OHLCV candle builder and gap-fill logic, porting
// candles/src/engine. All money math uses math/big via shared/db helpers — never
// float (invariant i1). Candle updates are published to Redis channel
// constants.ChannelCandleUpdate for downstream WS consumers.
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

// largeTradeThreshold is $500 USDL in raw 6-decimal units — swaps above this
// count as "large" (parity with candle-builder.ts).
var largeTradeThreshold = new(big.Int).SetInt64(500_000_000)

// minValidTimestamp is Jan 1 2024 — any timestamp before this is clearly bad data.
const minValidTimestamp int64 = 1704067200

// dedupCacheTTL bounds how long a folded swap key is remembered in-process and
// dedupCacheMax bounds its size. The durable candles.processed_swaps table is
// the correctness floor, so the in-memory cache is a pure hot-path optimisation
// and these values are not correctness-critical.
const (
	dedupCacheTTL = 2 * time.Minute
	dedupCacheMax = 50_000
)

// SwapEvent is the input swap event from the indexer webhook or Redis pub/sub.
type SwapEvent struct {
	PoolID         string `json:"poolId"`
	PoolAddress    string `json:"poolAddress"`
	Sender         string `json:"sender"`
	IsBuy          bool   `json:"isBuy"`
	AmountIn       string `json:"amountIn"`
	AmountOut      string `json:"amountOut"`
	Fee            string `json:"fee"`
	Price          string `json:"price"`
	BlockTimestamp int64  `json:"blockTimestamp"`
	TxHash         string `json:"txHash"`
	LogIndex       int    `json:"logIndex"`
}

// CandleUpdateEvent is published to Redis on every candle change.
type CandleUpdateEvent struct {
	PoolAddress     string `json:"poolAddress"`
	Timeframe       string `json:"timeframe"`
	CandleStart     int64  `json:"candleStart"`
	Open            string `json:"open"`
	High            string `json:"high"`
	Low             string `json:"low"`
	Close           string `json:"close"`
	VolumeUsdl      string `json:"volumeUsdl"`
	VolumeToken     string `json:"volumeToken"`
	BuyVolumeUsdl   string `json:"buyVolumeUsdl"`
	SellVolumeUsdl  string `json:"sellVolumeUsdl"`
	TradeCount      int    `json:"tradeCount"`
	UniqueTraders   int    `json:"uniqueTraders"`
	LargeTradeCount int    `json:"largeTradeCount"`
	McapOpen        string `json:"mcapOpen"`
	McapHigh        string `json:"mcapHigh"`
	McapLow         string `json:"mcapLow"`
	McapClose       string `json:"mcapClose"`
}

// Builder is the OHLCV candle builder. It processes swaps and upserts candles
// across all timeframes in a single transaction (C-1), tracks unique traders via
// Redis SET (C-2), and publishes candle updates to Redis pub/sub.
type Builder struct {
	pool   *pgxpool.Pool
	redis  *goredis.Client
	store  *store.Store
	logger *slog.Logger
	seen   *dedupCache
}

// New creates a Builder.
func New(pool *pgxpool.Pool, rdb *goredis.Client, st *store.Store, logger *slog.Logger) *Builder {
	return &Builder{pool: pool, redis: rdb, store: st, logger: logger, seen: newDedupCache(dedupCacheTTL, dedupCacheMax)}
}

// ProcessSwap processes a single swap event across all timeframes, exactly once.
//
// The indexer dual-delivers each swap over both the Redis stream and the webhook
// for redundancy, so the same swap can reach this method twice. Folding it twice
// would double-count candle volume/trades (a money invariant breach), so the
// fold is guarded by an idempotency claim on (txHash, logIndex): an in-memory
// TTL fast-path short-circuits the common near-simultaneous duplicate, and a
// durable claim in candles.processed_swaps (inside the candle transaction) is
// the correctness floor that survives restart and multi-replica fan-out.
func (b *Builder) ProcessSwap(ctx context.Context, swap SwapEvent) error {
	if swap.BlockTimestamp == 0 || swap.BlockTimestamp < minValidTimestamp {
		return nil
	}

	dedupKey := swap.TxHash + ":" + strconv.Itoa(swap.LogIndex)
	if swap.TxHash != "" && b.seen.has(dedupKey) {
		return nil
	}

	volumeUsdl := swap.AmountIn
	volumeToken := swap.AmountOut
	if !swap.IsBuy {
		volumeUsdl = swap.AmountOut
		volumeToken = swap.AmountIn
	}

	// Batch all timeframe updates in a single transaction (C-1).
	tx, err := b.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("engine: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() //nolint:errcheck // no-op after commit; rollback is best-effort cleanup

	// Durable idempotency claim (i9): a zero-row insert means another path/replica
	// already folded this swap, so skip the fold entirely (the tx rolls back).
	if swap.TxHash != "" {
		ct, err := tx.Exec(ctx, `
			INSERT INTO candles.processed_swaps (tx_hash, log_index)
			VALUES ($1, $2) ON CONFLICT DO NOTHING
		`, swap.TxHash, swap.LogIndex)
		if err != nil {
			return fmt.Errorf("engine: claim swap: %w", err)
		}
		if ct.RowsAffected() == 0 {
			b.seen.add(dedupKey)
			return nil
		}
	}

	for _, tf := range constants.TimeframeKeys {
		if err := b.upsertCandleInTx(ctx, tx, swap.PoolAddress, tf, swap.BlockTimestamp, swap.Price,
			volumeUsdl, volumeToken, swap.IsBuy, swap.Sender); err != nil {
			return fmt.Errorf("engine: upsert %s: %w", tf, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("engine: commit: %w", err)
	}
	if swap.TxHash != "" {
		b.seen.add(dedupKey)
	}
	return nil
}

func (b *Builder) upsertCandleInTx(ctx context.Context, tx pgx.Tx, poolAddr, timeframe string,
	timestamp int64, price, volumeUsdl, volumeToken string, isBuy bool, sender string) error {

	volUsdlInt, ok := new(big.Int).SetString(volumeUsdl, 10)
	if !ok {
		return fmt.Errorf("engine: invalid volumeUsdl %q", volumeUsdl)
	}
	large := volUsdlInt.Cmp(largeTradeThreshold) >= 0

	mcap, err := shareddb.ComputeMarketCap(price)
	if err != nil {
		return fmt.Errorf("engine: mcap: %w", err)
	}

	interval := constants.Timeframes[timeframe]
	candleStart := (timestamp / interval) * interval

	// Check if candle exists.
	row := tx.QueryRow(ctx, `
		SELECT open, high, low, close, volume_usdl, volume_token, buy_volume_usdl, sell_volume_usdl,
		       trade_count, unique_traders, large_trade_count, sequence_num, mcap_open, mcap_high, mcap_low, mcap_close
		FROM candles.candles
		WHERE pool_address = $1 AND timeframe = $2 AND candle_start = $3
	`, poolAddr, timeframe, candleStart)

	var existing struct {
		Open, High, Low, Close, VolumeUsdl, VolumeToken, BuyVol, SellVol string
		TradeCount, UniqueTraders, LargeTrades                           int
		SequenceNum                                                      int64
		McapOpen, McapHigh, McapLow, McapClose                           string
	}
	err = row.Scan(
		&existing.Open, &existing.High, &existing.Low, &existing.Close,
		&existing.VolumeUsdl, &existing.VolumeToken, &existing.BuyVol, &existing.SellVol,
		&existing.TradeCount, &existing.UniqueTraders, &existing.LargeTrades,
		&existing.SequenceNum,
		&existing.McapOpen, &existing.McapHigh, &existing.McapLow, &existing.McapClose,
	)

	if err == nil {
		// Candle exists — update it.
		newHigh, err := shareddb.BigintMax(existing.High, price)
		if err != nil {
			return err
		}
		newLow, err := shareddb.BigintMin(existing.Low, price)
		if err != nil {
			return err
		}
		newVolUsdl, err := shareddb.BigintAdd(existing.VolumeUsdl, volumeUsdl)
		if err != nil {
			return err
		}
		newVolToken, err := shareddb.BigintAdd(existing.VolumeToken, volumeToken)
		if err != nil {
			return err
		}
		newBuyVol := existing.BuyVol
		if isBuy {
			newBuyVol, err = shareddb.BigintAdd(existing.BuyVol, volumeUsdl)
			if err != nil {
				return err
			}
		}
		newSellVol := existing.SellVol
		if !isBuy {
			newSellVol, err = shareddb.BigintAdd(existing.SellVol, volumeUsdl)
			if err != nil {
				return err
			}
		}
		newTradeCount := existing.TradeCount + 1
		newMcapHigh, err := shareddb.BigintMax(existing.McapHigh, mcap)
		if err != nil {
			return err
		}
		newMcapLow, err := shareddb.BigintMin(existing.McapLow, mcap)
		if err != nil {
			return err
		}

		// Track unique traders via Redis SET (C-2).
		traderSetKey := fmt.Sprintf("candle:traders:%s:%s:%d", poolAddr, timeframe, candleStart)
		added, err := b.redis.SAdd(ctx, traderSetKey, sender).Result()
		if err != nil {
			b.logger.Warn("engine: redis sadd", slog.String("err", err.Error()))
		}
		if added > 0 {
			b.redis.Expire(ctx, traderSetKey, time.Duration(interval+3600)*time.Second)
		}
		newUniqueTraders := existing.UniqueTraders
		if added > 0 {
			newUniqueTraders++
		}
		newLargeTradeCount := existing.LargeTrades
		if large {
			newLargeTradeCount++
		}

		_, err = tx.Exec(ctx, `
			UPDATE candles.candles SET
				high = $4, low = $5, close = $6,
				volume_usdl = $7, volume_token = $8, buy_volume_usdl = $9, sell_volume_usdl = $10,
				trade_count = $11, unique_traders = $12, large_trade_count = $13,
				last_trade_ts = $14, mcap_high = $15, mcap_low = $16, mcap_close = $17
			WHERE pool_address = $1 AND timeframe = $2 AND candle_start = $3
		`,
			poolAddr, timeframe, candleStart,
			newHigh, newLow, price,
			newVolUsdl, newVolToken, newBuyVol, newSellVol,
			newTradeCount, newUniqueTraders, newLargeTradeCount,
			timestamp, newMcapHigh, newMcapLow, mcap,
		)
		if err != nil {
			return fmt.Errorf("engine: update candle: %w", err)
		}

		_, err = tx.Exec(ctx, `
			UPDATE candles.candle_cursors SET last_close = $3
			WHERE pool_address = $1 AND timeframe = $2 AND last_candle_start <= $4
		`, poolAddr, timeframe, price, candleStart)
		if err != nil {
			return fmt.Errorf("engine: update cursor close: %w", err)
		}

		return b.publishCandleUpdate(ctx, CandleUpdateEvent{
			PoolAddress: poolAddr, Timeframe: timeframe, CandleStart: candleStart,
			Open: existing.Open, High: newHigh, Low: newLow, Close: price,
			VolumeUsdl: newVolUsdl, VolumeToken: newVolToken, TradeCount: newTradeCount,
			UniqueTraders: newUniqueTraders, LargeTradeCount: newLargeTradeCount,
			BuyVolumeUsdl: newBuyVol, SellVolumeUsdl: newSellVol,
			McapOpen: existing.McapOpen, McapHigh: newMcapHigh, McapLow: newMcapLow, McapClose: mcap,
		})
	}

	// Candle doesn't exist — insert new.
	var openPrice string
	var sequenceNum int64

	cursor, err := b.store.GetCursor(ctx, poolAddr, timeframe)
	if err != nil {
		return fmt.Errorf("engine: get cursor: %w", err)
	}
	if cursor != nil {
		openPrice = cursor.LastClose
		sequenceNum = cursor.LastSequenceNum + 1
	} else {
		openPrice = price
		sequenceNum = 0
	}

	mcapOpen, err := shareddb.ComputeMarketCap(openPrice)
	if err != nil {
		return err
	}
	newHigh, err := shareddb.BigintMax(openPrice, price)
	if err != nil {
		return err
	}
	newLow, err := shareddb.BigintMin(openPrice, price)
	if err != nil {
		return err
	}
	mcapHigh, err := shareddb.BigintMax(mcapOpen, mcap)
	if err != nil {
		return err
	}
	mcapLow, err := shareddb.BigintMin(mcapOpen, mcap)
	if err != nil {
		return err
	}

	// Track first trader via Redis SET (C-2).
	traderSetKey := fmt.Sprintf("candle:traders:%s:%s:%d", poolAddr, timeframe, candleStart)
	b.redis.SAdd(ctx, traderSetKey, sender)
	b.redis.Expire(ctx, traderSetKey, time.Duration(interval+3600)*time.Second)

	buyVol := "0"
	sellVol := "0"
	if isBuy {
		buyVol = volumeUsdl
	} else {
		sellVol = volumeUsdl
	}
	largeCount := 0
	if large {
		largeCount = 1
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO candles.candles (
			pool_address, timeframe, candle_start, open, high, low, close,
			volume_usdl, volume_token, buy_volume_usdl, sell_volume_usdl,
			trade_count, unique_traders, large_trade_count, last_trade_ts,
			sequence_num, mcap_open, mcap_high, mcap_low, mcap_close
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)
		ON CONFLICT DO NOTHING
	`,
		poolAddr, timeframe, candleStart, openPrice, newHigh, newLow, price,
		volumeUsdl, volumeToken, buyVol, sellVol,
		1, 1, largeCount, timestamp,
		sequenceNum, mcapOpen, mcapHigh, mcapLow, mcap,
	)
	if err != nil {
		return fmt.Errorf("engine: insert candle: %w", err)
	}

	if perr := b.publishCandleUpdate(ctx, CandleUpdateEvent{
		PoolAddress: poolAddr, Timeframe: timeframe, CandleStart: candleStart,
		Open: openPrice, High: newHigh, Low: newLow, Close: price,
		VolumeUsdl: volumeUsdl, VolumeToken: volumeToken, TradeCount: 1,
		UniqueTraders: 1, LargeTradeCount: largeCount,
		BuyVolumeUsdl: buyVol, SellVolumeUsdl: sellVol,
		McapOpen: mcapOpen, McapHigh: mcapHigh, McapLow: mcapLow, McapClose: mcap,
	}); perr != nil {
		return perr
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO candles.candle_cursors (pool_address, timeframe, last_close, last_candle_start, last_sequence_num)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (pool_address, timeframe) DO UPDATE SET
			last_close = EXCLUDED.last_close,
			last_candle_start = EXCLUDED.last_candle_start,
			last_sequence_num = EXCLUDED.last_sequence_num
	`, poolAddr, timeframe, price, candleStart, sequenceNum)
	if err != nil {
		return fmt.Errorf("engine: upsert cursor: %w", err)
	}

	return nil
}

func (b *Builder) publishCandleUpdate(ctx context.Context, event CandleUpdateEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("engine: marshal candle update: %w", err)
	}
	if err := b.redis.Publish(ctx, constants.ChannelCandleUpdate, payload).Err(); err != nil {
		b.logger.Warn("engine: publish candle update", slog.String("err", err.Error()))
	}
	return nil
}

// dedupCache is a small, bounded, TTL'd set of recently-folded (txHash:logIndex)
// keys. It short-circuits the common near-simultaneous dual-delivery duplicate
// before a DB round-trip; correctness never depends on it (the
// candles.processed_swaps table is the durable floor). Concurrency-safe.
type dedupCache struct {
	mu      sync.Mutex
	entries map[string]int64 // key -> expiry (unix nanos)
	ttl     time.Duration
	max     int
	now     func() time.Time
}

func newDedupCache(ttl time.Duration, maxEntries int) *dedupCache {
	return &dedupCache{entries: make(map[string]int64), ttl: ttl, max: maxEntries, now: time.Now}
}

// has reports whether key is present and unexpired, pruning it if expired.
func (c *dedupCache) has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	exp, ok := c.entries[key]
	if !ok {
		return false
	}
	if c.now().UnixNano() > exp {
		delete(c.entries, key)
		return false
	}
	return true
}

// add records key with a fresh TTL, pruning expired entries (and, if still at
// capacity, evicting arbitrary entries) to keep memory bounded.
func (c *dedupCache) add(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now().UnixNano()
	if len(c.entries) >= c.max {
		for k, exp := range c.entries {
			if now > exp {
				delete(c.entries, k)
			}
		}
		for k := range c.entries {
			if len(c.entries) < c.max {
				break
			}
			delete(c.entries, k)
		}
	}
	c.entries[key] = now + c.ttl.Nanoseconds()
}
