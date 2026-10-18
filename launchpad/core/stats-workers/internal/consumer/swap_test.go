package consumer_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	shareddb "github.com/Sidiora-Technologies/KindleLaunch/shared/db"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// TestSwapConsumer exercises the full StatsSwapConsumer.processEvent pipeline
// against real Postgres + Redis: transaction insert, volume bucket + trader,
// rolling-stats recompute, price snapshot, price-change computation, the
// pool_stats price/mcap/high-low update and the read-through cache — proving the
// seven steps run in order and the cached row mirrors the final persisted state.
func TestSwapConsumer(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	sc := consumer.NewSwapConsumer(st, rdb, discardLogger())

	t.Run("end-to-end ordering and final consistency", func(t *testing.T) {
		const addr = "0xswap_e2e"
		const initial = "10000000000000" // 1e13 seed price (= high/low baseline)
		seedPoolStats(t, ctx, st, addr, nil, initial)

		ts := time.Now().Unix()
		buyPrice := "20000000000000" // 2e13 > seed -> drives high_24h up
		buy := consumer.SwapEvent{
			PoolAddress: addr, Sender: "0xtrader1", IsBuy: true,
			AmountIn: "1000000", AmountOut: "5000", // buy: USDL leg = amountIn
			Price: buyPrice, Fee: "10", BlockTimestamp: ts, TxHash: "0xbuy", LogIndex: 0,
		}
		if err := sc.ProcessEvent(ctx, buy); err != nil {
			t.Fatalf("process buy: %v", err)
		}

		// 1. Transaction recorded under txHash-logIndex.
		var txCount int
		if err := st.Pool().QueryRow(ctx,
			`SELECT COUNT(*) FROM stats.pool_transactions WHERE id='0xbuy-0' AND pool_address=$1`, addr).
			Scan(&txCount); err != nil {
			t.Fatalf("count tx: %v", err)
		}
		if txCount != 1 {
			t.Fatalf("transaction rows = %d, want 1", txCount)
		}

		// 2. Volume bucket at (ts/60)*60: USDL leg = amountIn, buy counted, price seeded.
		bucketStart := (ts / 60) * 60
		var vol, high, low string
		var bc, sCount int
		if err := st.Pool().QueryRow(ctx, `
			SELECT volume_usdl, buy_count, sell_count, high_price, low_price
			FROM stats.volume_buckets WHERE pool_address=$1 AND bucket_start=$2`, addr, bucketStart).
			Scan(&vol, &bc, &sCount, &high, &low); err != nil {
			t.Fatalf("read bucket: %v", err)
		}
		if vol != "1000000" || bc != 1 || sCount != 0 || high != buyPrice || low != buyPrice {
			t.Fatalf("bucket = vol:%s buy:%d sell:%d high:%s low:%s", vol, bc, sCount, high, low)
		}
		var traderCount int
		if err := st.Pool().QueryRow(ctx, `
			SELECT COUNT(*) FROM stats.bucket_traders
			WHERE pool_address=$1 AND bucket_start=$2 AND trader='0xtrader1'`, addr, bucketStart).
			Scan(&traderCount); err != nil {
			t.Fatalf("count trader: %v", err)
		}
		if traderCount != 1 {
			t.Fatalf("bucket_traders for 0xtrader1 = %d, want 1", traderCount)
		}

		// 3/6. Rolling stats + price/mcap/high-low written to pool_stats.
		row, err := st.GetPoolStats(ctx, addr)
		if err != nil || row == nil {
			t.Fatalf("get stats: row=%v err=%v", row, err)
		}
		if row.Price != buyPrice {
			t.Errorf("price = %s, want %s", row.Price, buyPrice)
		}
		wantMcap, _ := shareddb.ComputeMarketCap(buyPrice)
		if row.MarketCap != wantMcap {
			t.Errorf("market_cap = %s, want %s", row.MarketCap, wantMcap)
		}
		if row.Volume24h != "1000000" || row.Volume1h != "1000000" || row.Volume5m != "1000000" {
			t.Errorf("rolling volume not within all windows: 24h=%s 1h=%s 5m=%s", row.Volume24h, row.Volume1h, row.Volume5m)
		}
		if row.BuyCount24h != 1 || row.SellCount24h != 0 || row.UniqueTraders24h != 1 {
			t.Errorf("counts buy=%d sell=%d traders=%d", row.BuyCount24h, row.SellCount24h, row.UniqueTraders24h)
		}
		if row.High24h != buyPrice {
			t.Errorf("high_24h = %s, want %s (max of seed,buy)", row.High24h, buyPrice)
		}
		if row.Low24h != initial {
			t.Errorf("low_24h = %s, want %s (min of seed,buy)", row.Low24h, initial)
		}

		// 4. Price snapshot at the current wall-clock minute.
		minuteTs := (shareddb.NowSeconds() / 60) * 60
		var snapCount int
		if err := st.Pool().QueryRow(ctx, `
			SELECT COUNT(*) FROM stats.price_snapshots WHERE pool_address=$1 AND minute_ts <= $2`, addr, minuteTs).
			Scan(&snapCount); err != nil {
			t.Fatalf("count snapshot: %v", err)
		}
		if snapCount < 1 {
			t.Fatalf("expected a price snapshot, got %d", snapCount)
		}

		// 7. The Redis cache mirrors the final persisted row (step 7 ran last).
		cached, err := rdb.Get(ctx, "stats:"+addr).Result()
		if err != nil {
			t.Fatalf("read cache: %v", err)
		}
		var cachedRow store.PoolStatsRow
		if err := json.Unmarshal([]byte(cached), &cachedRow); err != nil {
			t.Fatalf("unmarshal cache: %v", err)
		}
		if cachedRow.Price != row.Price || cachedRow.MarketCap != row.MarketCap ||
			cachedRow.Volume24h != row.Volume24h || cachedRow.UpdatedAt != row.UpdatedAt {
			t.Fatalf("cache row diverged from DB row:\ncache=%+v\ndb=%+v", cachedRow, *row)
		}

		// A subsequent sell by a different trader accumulates into the same bucket
		// and flips price to the latest swap (ordering: each event recomputes).
		sellPrice := "15000000000000" // 1.5e13: between seed and buy -> high stays, low stays
		sell := consumer.SwapEvent{
			PoolAddress: addr, Sender: "0xtrader2", IsBuy: false,
			AmountIn: "3000", AmountOut: "2000000", // sell: USDL leg = amountOut
			Price: sellPrice, Fee: "5", BlockTimestamp: ts, TxHash: "0xsell", LogIndex: 0,
		}
		if err := sc.ProcessEvent(ctx, sell); err != nil {
			t.Fatalf("process sell: %v", err)
		}
		row2, _ := st.GetPoolStats(ctx, addr)
		if row2.Volume24h != "3000000" { // 1000000 (buy) + 2000000 (sell)
			t.Errorf("volume_24h after sell = %s, want 3000000", row2.Volume24h)
		}
		if row2.BuyCount24h != 1 || row2.SellCount24h != 1 || row2.UniqueTraders24h != 2 {
			t.Errorf("after sell: buy=%d sell=%d traders=%d", row2.BuyCount24h, row2.SellCount24h, row2.UniqueTraders24h)
		}
		if row2.Price != sellPrice {
			t.Errorf("price after sell = %s, want %s (latest swap)", row2.Price, sellPrice)
		}
		if row2.High24h != buyPrice || row2.Low24h != initial {
			t.Errorf("high/low after sell: high=%s low=%s, want %s/%s", row2.High24h, row2.Low24h, buyPrice, initial)
		}
	})

	t.Run("price-change propagation from a prior snapshot", func(t *testing.T) {
		const addr = "0xswap_pc"
		seedPoolStats(t, ctx, st, addr, nil, "1000000")

		// Seed an older snapshot ~5 minutes ago so the 5m/15m/1h/24h windows have a
		// prior price to diff against; the 1m window has none (other than the one the
		// consumer writes for the current minute).
		oldMinute := ((time.Now().Unix() - 300) / 60) * 60
		if _, err := st.Pool().Exec(ctx, `
			INSERT INTO stats.price_snapshots (pool_address, minute_ts, price)
			VALUES ($1,$2,'1000000')`, addr, oldMinute); err != nil {
			t.Fatalf("seed old snapshot: %v", err)
		}

		ev := consumer.SwapEvent{
			PoolAddress: addr, Sender: "0xpc", IsBuy: true,
			AmountIn: "500", AmountOut: "10", Price: "1100000", Fee: "1",
			BlockTimestamp: time.Now().Unix(), TxHash: "0xpc", LogIndex: 0,
		}
		if err := sc.ProcessEvent(ctx, ev); err != nil {
			t.Fatalf("process: %v", err)
		}

		row, _ := st.GetPoolStats(ctx, addr)
		// delta = 1100000-1000000 = 100000.
		// pct  = 100000*10000/1000000 = 1000 bps.
		// $    = 100000*1e15/1e18 = 100 (truncating).
		if row.PriceChange5m != "1000" {
			t.Errorf("price_change_5m = %s, want 1000", row.PriceChange5m)
		}
		if row.PriceChangeDollar5m != "100" {
			t.Errorf("price_change_dollar_5m = %s, want 100", row.PriceChangeDollar5m)
		}
		// No prior snapshot inside the 1m window -> change stays at the column default.
		if row.PriceChange1m != "0" {
			t.Errorf("price_change_1m = %s, want 0 (no prior 1m snapshot)", row.PriceChange1m)
		}
	})
}
