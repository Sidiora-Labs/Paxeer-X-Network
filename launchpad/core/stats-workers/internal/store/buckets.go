package store

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/jackc/pgx/v5"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"
)

// priceChangeWindow describes one price-change look-back window and the two
// pool_stats columns it populates (parity with the TS computePriceChanges).
type priceChangeWindow struct {
	pctCol    string
	dollarCol string
	seconds   int64
}

// priceChangeWindows is the ordered window list (declaration order matches the
// TS source so dynamic UPDATE column order is deterministic).
var priceChangeWindows = []priceChangeWindow{
	{"price_change_1m", "price_change_dollar_1m", 60},
	{"price_change_5m", "price_change_dollar_5m", 300},
	{"price_change_15m", "price_change_dollar_15m", 900},
	{"price_change_1h", "price_change_dollar_1h", 3600},
	{"price_change_24h", "price_change_dollar_24h", 86400},
}

// rollingWindow describes one rolling-volume window (5m/1h/24h).
type rollingWindow struct {
	col     string
	seconds int64
}

var rollingWindows = []rollingWindow{
	{"volume_5m", 300},
	{"volume_1h", 3600},
	{"volume_24h", 86400},
}

// supplyWadFactor is TOTAL_SUPPLY_RAW (1e15) used as the multiplier in the $-delta
// computation; WAD (1e18) is the divisor (parity with the TS BigInt literals).
var (
	supplyWadFactor = big.NewInt(1_000_000_000_000_000) // 1e15
	wad             = func() *big.Int { v, _ := new(big.Int).SetString(shareddb.WAD, 10); return v }()
	tenThousand     = big.NewInt(10000)
)

// ApplyVolumeBucket upserts the 1-minute volume bucket for (poolAddress,
// bucketStart) and records the trader, under the per-bucket advisory lock
// (parity with the TS volbucket-<pool>-<bucketStart> lock). volumeUsdl is the
// USDL leg of the swap (amountIn for buys, amountOut for sells).
func (s *Store) ApplyVolumeBucket(ctx context.Context, poolAddress string, bucketStart int64, volumeUsdl, price, trader string, isBuy bool) error {
	key := fmt.Sprintf("volbucket-%s-%d", poolAddress, bucketStart)
	return s.withXactLock(ctx, key, func(tx pgx.Tx) error {
		var (
			existingVol string
			buyCount    int
			sellCount   int
		)
		err := tx.QueryRow(ctx, `
			SELECT volume_usdl, buy_count, sell_count
			FROM stats.volume_buckets
			WHERE pool_address = $1 AND bucket_start = $2`, poolAddress, bucketStart).
			Scan(&existingVol, &buyCount, &sellCount)

		switch {
		case err == nil:
			newVol, addErr := shareddb.BigintAdd(existingVol, volumeUsdl)
			if addErr != nil {
				return fmt.Errorf("store: bucket volume add: %w", addErr)
			}
			if isBuy {
				buyCount++
			} else {
				sellCount++
			}
			if _, err := tx.Exec(ctx, `
				UPDATE stats.volume_buckets
				SET volume_usdl = $3, buy_count = $4, sell_count = $5
				WHERE pool_address = $1 AND bucket_start = $2`,
				poolAddress, bucketStart, newVol, buyCount, sellCount); err != nil {
				return fmt.Errorf("store: update volume bucket: %w", err)
			}
		case errors.Is(err, pgx.ErrNoRows):
			buy, sell := 0, 1
			if isBuy {
				buy, sell = 1, 0
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO stats.volume_buckets (pool_address, bucket_start, volume_usdl, buy_count, sell_count, high_price, low_price)
				VALUES ($1,$2,$3,$4,$5,$6,$6)
				ON CONFLICT (pool_address, bucket_start) DO NOTHING`,
				poolAddress, bucketStart, volumeUsdl, buy, sell, price); err != nil {
				return fmt.Errorf("store: insert volume bucket: %w", err)
			}
		default:
			return fmt.Errorf("store: read volume bucket: %w", err)
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO stats.bucket_traders (pool_address, bucket_start, bucket_size, trader)
			VALUES ($1,$2,60,$3)
			ON CONFLICT (pool_address, bucket_start, bucket_size, trader) DO NOTHING`,
			poolAddress, bucketStart, trader); err != nil {
			return fmt.Errorf("store: insert bucket trader: %w", err)
		}
		return nil
	})
}

// RecalculateRollingStats recomputes the 5m/1h/24h rolling volumes plus the 24h
// buy/sell counts and unique-trader count from the bucket tables, and writes them
// to pool_stats. Ports StatsSwapConsumer.recalculateRollingStats.
func (s *Store) RecalculateRollingStats(ctx context.Context, poolAddress string, now int64) error {
	vols := make(map[string]string, len(rollingWindows))
	var buys24h, sells24h, uniqueTraders24h int

	for _, w := range rollingWindows {
		cutoff := now - w.seconds
		rows, err := s.db.Query(ctx, `
			SELECT volume_usdl, buy_count, sell_count
			FROM stats.volume_buckets
			WHERE pool_address = $1 AND bucket_start >= $2`, poolAddress, cutoff)
		if err != nil {
			return fmt.Errorf("store: read rolling buckets: %w", err)
		}

		total := "0"
		var buys, sells int
		for rows.Next() {
			var vol string
			var b, sl int
			if err := rows.Scan(&vol, &b, &sl); err != nil {
				rows.Close()
				return fmt.Errorf("store: scan rolling bucket: %w", err)
			}
			total, err = shareddb.BigintAdd(total, vol)
			if err != nil {
				rows.Close()
				return fmt.Errorf("store: rolling volume add: %w", err)
			}
			buys += b
			sells += sl
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("store: rolling buckets rows: %w", err)
		}

		vols[w.col] = total
		if w.col == "volume_24h" {
			buys24h, sells24h = buys, sells
			if err := s.db.QueryRow(ctx, `
				SELECT COUNT(DISTINCT trader)
				FROM stats.bucket_traders
				WHERE pool_address = $1 AND bucket_start >= $2`, poolAddress, cutoff).
				Scan(&uniqueTraders24h); err != nil {
				return fmt.Errorf("store: count unique traders: %w", err)
			}
		}
	}

	_, err := s.db.Exec(ctx, `
		UPDATE stats.pool_stats
		SET volume_5m = $2, volume_1h = $3, volume_24h = $4,
		    buy_count_24h = $5, sell_count_24h = $6, unique_traders_24h = $7, updated_at = $8
		WHERE pool_address = $1`,
		poolAddress, vols["volume_5m"], vols["volume_1h"], vols["volume_24h"],
		buys24h, sells24h, uniqueTraders24h, now)
	if err != nil {
		return fmt.Errorf("store: update rolling stats: %w", err)
	}
	return nil
}

// UpsertPriceSnapshot stores the 1-minute price snapshot (parity with the TS
// onConflictDoUpdate keyed on (pool_address, minute_ts)).
func (s *Store) UpsertPriceSnapshot(ctx context.Context, poolAddress string, minuteTs int64, price string) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO stats.price_snapshots (pool_address, minute_ts, price)
		VALUES ($1,$2,$3)
		ON CONFLICT (pool_address, minute_ts) DO UPDATE SET price = excluded.price`,
		poolAddress, minuteTs, price)
	if err != nil {
		return fmt.Errorf("store: upsert price snapshot: %w", err)
	}
	return nil
}

// ComputePriceChanges returns the basis-point and $-delta price changes for every
// window that has a prior snapshot, keyed by pool_stats column name. Ports
// StatsSwapConsumer.computePriceChanges (integer math via math/big, truncating
// division — byte-identical to the TS BigInt arithmetic, negatives included).
func (s *Store) ComputePriceChanges(ctx context.Context, poolAddress, currentPrice string, now int64) (map[string]string, error) {
	current, ok := new(big.Int).SetString(currentPrice, 10)
	if !ok {
		return nil, fmt.Errorf("store: invalid current price %q", currentPrice)
	}

	out := map[string]string{}
	for _, w := range priceChangeWindows {
		targetTs := ((now - w.seconds) / 60) * 60

		var oldPriceStr string
		err := s.db.QueryRow(ctx, `
			SELECT price FROM stats.price_snapshots
			WHERE pool_address = $1 AND minute_ts >= $2
			ORDER BY minute_ts ASC LIMIT 1`, poolAddress, targetTs-60).Scan(&oldPriceStr)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("store: read snapshot: %w", err)
		}
		if oldPriceStr == "0" {
			continue
		}
		old, ok := new(big.Int).SetString(oldPriceStr, 10)
		if !ok || old.Sign() <= 0 {
			continue
		}

		delta := new(big.Int).Sub(current, old)

		pct := new(big.Int).Mul(delta, tenThousand)
		pct.Quo(pct, old)
		out[w.pctCol] = pct.String()

		dollar := new(big.Int).Mul(delta, supplyWadFactor)
		dollar.Quo(dollar, wad)
		out[w.dollarCol] = dollar.String()
	}
	return out, nil
}

// UpdatePoolStatsPriceAndChanges applies the post-swap price, market cap, 24h
// high/low and the computed price changes to an existing pool_stats row, under
// the per-pool advisory lock (parity with the TS poolstats-<pool> lock). A no-op
// when the pool row does not yet exist (parity: TS only updates if existing).
func (s *Store) UpdatePoolStatsPriceAndChanges(ctx context.Context, poolAddress, price, marketCap string, now int64, changes map[string]string) error {
	return s.withXactLock(ctx, "poolstats-"+poolAddress, func(tx pgx.Tx) error {
		var high, low string
		err := tx.QueryRow(ctx, `
			SELECT high_24h, low_24h FROM stats.pool_stats WHERE pool_address = $1 LIMIT 1`,
			poolAddress).Scan(&high, &low)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("store: read pool stats high/low: %w", err)
		}

		newHigh := price
		if high != "0" {
			newHigh, err = shareddb.BigintMax(high, price)
			if err != nil {
				return fmt.Errorf("store: high max: %w", err)
			}
		}
		newLow := price
		if low != "0" {
			newLow, err = shareddb.BigintMin(low, price)
			if err != nil {
				return fmt.Errorf("store: low min: %w", err)
			}
		}

		// Base SET columns ($2..$6); dynamic price-change columns follow.
		query := `UPDATE stats.pool_stats SET price = $2, market_cap = $3, high_24h = $4, low_24h = $5, updated_at = $6`
		args := []any{poolAddress, price, marketCap, newHigh, newLow, now}
		n := len(args)
		for _, w := range priceChangeWindows {
			if v, present := changes[w.pctCol]; present {
				n++
				query += fmt.Sprintf(", %s = $%d", w.pctCol, n)
				args = append(args, v)
			}
			if v, present := changes[w.dollarCol]; present {
				n++
				query += fmt.Sprintf(", %s = $%d", w.dollarCol, n)
				args = append(args, v)
			}
		}
		query += ` WHERE pool_address = $1`

		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("store: update pool stats price/changes: %w", err)
		}
		return nil
	})
}
