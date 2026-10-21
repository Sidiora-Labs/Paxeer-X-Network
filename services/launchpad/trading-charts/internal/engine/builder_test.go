package engine_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/engine"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/trading-charts/internal/store"
)

const (
	bPool = "0xpool00000000000000000000000000000000bbbb"
	// 1704067260 is divisible by 60 (>= MIN_VALID_TIMESTAMP) so the 1m bucket
	// start equals the timestamp.
	bTS    = int64(1704067260)
	priceA = "1000000000000000000" // 1e18 WAD
	priceB = "2000000000000000000" // 2e18 WAD
)

func newBuilder(t *testing.T) (*engine.Builder, *store.Store, *goredis.Client) {
	t.Helper()
	_, pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)

	opt, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := goredis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)
	return engine.New(pool, rdb, st, logger), st, rdb
}

func TestProcessSwapNewCandle(t *testing.T) {
	ctx := context.Background()
	b, st, rdb := newBuilder(t)

	// Subscribe BEFORE processing to capture the candles:update publication.
	sub := rdb.Subscribe(ctx, constants.ChannelCandleUpdate)
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()
	ch := sub.Channel()

	swap := engine.SwapEvent{
		PoolAddress:    bPool,
		Sender:         "0xsenderAAAA",
		IsBuy:          true,
		AmountIn:       "100",
		AmountOut:      "200",
		Fee:            "1",
		Price:          priceA,
		BlockTimestamp: bTS,
		TxHash:         "0xtx1",
		LogIndex:       0,
	}
	if err := b.ProcessSwap(ctx, swap); err != nil {
		t.Fatalf("ProcessSwap: %v", err)
	}

	got, err := st.GetCandle(ctx, bPool, "1m", bTS)
	if err != nil || got == nil {
		t.Fatalf("GetCandle = %v, err %v", got, err)
	}
	if got.Open != priceA || got.Close != priceA {
		t.Errorf("open/close = %s/%s, want %s", got.Open, got.Close, priceA)
	}
	if got.VolumeUsdl != "100" || got.BuyVolumeUsdl != "100" || got.SellVolumeUsdl != "0" {
		t.Errorf("volumes wrong: usdl=%s buy=%s sell=%s", got.VolumeUsdl, got.BuyVolumeUsdl, got.SellVolumeUsdl)
	}
	if got.TradeCount != 1 || got.UniqueTraders != 1 || got.LargeTradeCount != 0 {
		t.Errorf("counts wrong: trades=%d uniq=%d large=%d", got.TradeCount, got.UniqueTraders, got.LargeTradeCount)
	}

	// A cursor must have been created with the candle close.
	cur, err := st.GetCursor(ctx, bPool, "1m")
	if err != nil || cur == nil || cur.LastClose != priceA {
		t.Fatalf("cursor = %+v, err %v", cur, err)
	}

	// At least one candle:update event was published for this pool.
	select {
	case msg := <-ch:
		var ev engine.CandleUpdateEvent
		if err := json.Unmarshal([]byte(msg.Payload), &ev); err != nil {
			t.Fatalf("unmarshal candle update: %v", err)
		}
		if ev.PoolAddress != bPool {
			t.Errorf("published pool = %q, want %q", ev.PoolAddress, bPool)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no candle:update published")
	}
}

func TestProcessSwapUpdateExisting(t *testing.T) {
	ctx := context.Background()
	b, st, _ := newBuilder(t)

	// First swap (buy) creates the candle.
	first := engine.SwapEvent{
		PoolAddress: bPool, Sender: "0xsenderAAAA", IsBuy: true,
		AmountIn: "100", AmountOut: "200", Fee: "1", Price: priceA,
		BlockTimestamp: bTS, TxHash: "0xtx1", LogIndex: 0,
	}
	if err := b.ProcessSwap(ctx, first); err != nil {
		t.Fatalf("ProcessSwap first: %v", err)
	}

	// Second swap, same 1m bucket, different sender, SELL, higher price, large.
	second := engine.SwapEvent{
		PoolAddress: bPool, Sender: "0xsenderBBBB", IsBuy: false,
		AmountIn: "50", AmountOut: "500000000", Fee: "1", Price: priceB,
		BlockTimestamp: bTS + 30, TxHash: "0xtx2", LogIndex: 1,
	}
	if err := b.ProcessSwap(ctx, second); err != nil {
		t.Fatalf("ProcessSwap second: %v", err)
	}

	got, _ := st.GetCandle(ctx, bPool, "1m", bTS)
	if got == nil {
		t.Fatal("candle missing after update")
	}
	if got.Open != priceA {
		t.Errorf("open mutated to %s, want stable %s", got.Open, priceA)
	}
	if got.High != priceB {
		t.Errorf("high = %s, want %s", got.High, priceB)
	}
	if got.Close != priceB {
		t.Errorf("close = %s, want %s", got.Close, priceB)
	}
	if got.TradeCount != 2 {
		t.Errorf("tradeCount = %d, want 2", got.TradeCount)
	}
	if got.UniqueTraders != 2 {
		t.Errorf("uniqueTraders = %d, want 2 (two distinct senders)", got.UniqueTraders)
	}
	// Sell volume == amountOut for a sell; large (>= 500_000_000) increments.
	if got.SellVolumeUsdl != "500000000" {
		t.Errorf("sellVolumeUsdl = %s, want 500000000", got.SellVolumeUsdl)
	}
	if got.LargeTradeCount != 1 {
		t.Errorf("largeTradeCount = %d, want 1", got.LargeTradeCount)
	}
	// volumeUsdl accumulates buy(100) + sell(500000000).
	if got.VolumeUsdl != "500000100" {
		t.Errorf("volumeUsdl = %s, want 500000100", got.VolumeUsdl)
	}
}

func TestProcessSwapSameTraderNoUniqueIncrement(t *testing.T) {
	ctx := context.Background()
	b, st, _ := newBuilder(t)

	swap := engine.SwapEvent{
		PoolAddress: bPool, Sender: "0xsameSender", IsBuy: true,
		AmountIn: "100", AmountOut: "200", Fee: "1", Price: priceA,
		BlockTimestamp: bTS, TxHash: "0xtx1", LogIndex: 0,
	}
	if err := b.ProcessSwap(ctx, swap); err != nil {
		t.Fatalf("ProcessSwap 1: %v", err)
	}
	swap.TxHash = "0xtx2"
	swap.LogIndex = 1
	if err := b.ProcessSwap(ctx, swap); err != nil {
		t.Fatalf("ProcessSwap 2: %v", err)
	}

	got, _ := st.GetCandle(ctx, bPool, "1m", bTS)
	if got.TradeCount != 2 {
		t.Errorf("tradeCount = %d, want 2", got.TradeCount)
	}
	if got.UniqueTraders != 1 {
		t.Errorf("uniqueTraders = %d, want 1 (same sender)", got.UniqueTraders)
	}
}

func TestProcessSwapInvalidTimestampSkipped(t *testing.T) {
	ctx := context.Background()
	b, st, _ := newBuilder(t)

	for _, ts := range []int64{0, 1700000000 - 1, 1} {
		swap := engine.SwapEvent{
			PoolAddress: bPool, Sender: "0xs", IsBuy: true,
			AmountIn: "100", AmountOut: "200", Fee: "1", Price: priceA,
			BlockTimestamp: ts, TxHash: "0xtx", LogIndex: 0,
		}
		if err := b.ProcessSwap(ctx, swap); err != nil {
			t.Fatalf("ProcessSwap ts=%d: %v", ts, err)
		}
	}
	// Nothing should have been written.
	if c, _ := st.GetCursor(ctx, bPool, "1m"); c != nil {
		t.Errorf("cursor created for invalid-timestamp swaps: %+v", c)
	}
}

func TestProcessSwapInvalidAmountErrors(t *testing.T) {
	ctx := context.Background()
	b, _, _ := newBuilder(t)

	swap := engine.SwapEvent{
		PoolAddress: bPool, Sender: "0xs", IsBuy: true,
		AmountIn: "not-a-number", AmountOut: "200", Fee: "1", Price: priceA,
		BlockTimestamp: bTS, TxHash: "0xtx", LogIndex: 0,
	}
	if err := b.ProcessSwap(ctx, swap); err == nil {
		t.Fatal("ProcessSwap with non-numeric volume should error")
	}
}

// TestProcessSwapDedupSameInstance proves the in-memory + durable idempotency
// guard: redelivering the SAME (txHash, logIndex) — as dual-delivery does — folds
// the candle exactly once (no inflated volume / trade_count).
func TestProcessSwapDedupSameInstance(t *testing.T) {
	ctx := context.Background()
	b, st, _ := newBuilder(t)

	swap := engine.SwapEvent{
		PoolAddress: bPool, Sender: "0xsenderAAAA", IsBuy: true,
		AmountIn: "100", AmountOut: "200", Fee: "1", Price: priceA,
		BlockTimestamp: bTS, TxHash: "0xdup", LogIndex: 0,
	}
	for i := 0; i < 3; i++ {
		if err := b.ProcessSwap(ctx, swap); err != nil {
			t.Fatalf("ProcessSwap #%d: %v", i, err)
		}
	}

	got, err := st.GetCandle(ctx, bPool, "1m", bTS)
	if err != nil || got == nil {
		t.Fatalf("GetCandle = %v, err %v", got, err)
	}
	if got.TradeCount != 1 {
		t.Errorf("tradeCount = %d, want 1 (duplicate folds suppressed)", got.TradeCount)
	}
	if got.VolumeUsdl != "100" {
		t.Errorf("volumeUsdl = %s, want 100 (counted once)", got.VolumeUsdl)
	}
}

// TestProcessSwapDedupDurableAcrossInstances proves the durable floor: a SECOND
// builder with an empty in-memory cache (the restart / multi-replica case) still
// refuses to re-fold a swap already claimed in candles.processed_swaps.
func TestProcessSwapDedupDurableAcrossInstances(t *testing.T) {
	ctx := context.Background()
	_, pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)
	opt, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := goredis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)

	swap := engine.SwapEvent{
		PoolAddress: bPool, Sender: "0xsenderAAAA", IsBuy: true,
		AmountIn: "100", AmountOut: "200", Fee: "1", Price: priceA,
		BlockTimestamp: bTS, TxHash: "0xdup", LogIndex: 0,
	}

	first := engine.New(pool, rdb, st, logger)
	if err := first.ProcessSwap(ctx, swap); err != nil {
		t.Fatalf("first ProcessSwap: %v", err)
	}

	// Fresh builder => empty in-memory cache, so only the durable claim can stop
	// the refold.
	second := engine.New(pool, rdb, st, logger)
	if err := second.ProcessSwap(ctx, swap); err != nil {
		t.Fatalf("second ProcessSwap: %v", err)
	}

	got, err := st.GetCandle(ctx, bPool, "1m", bTS)
	if err != nil || got == nil {
		t.Fatalf("GetCandle = %v, err %v", got, err)
	}
	if got.TradeCount != 1 {
		t.Errorf("tradeCount = %d, want 1 (durable dedup across instances)", got.TradeCount)
	}
	if got.VolumeUsdl != "100" {
		t.Errorf("volumeUsdl = %s, want 100 (counted once)", got.VolumeUsdl)
	}
}
