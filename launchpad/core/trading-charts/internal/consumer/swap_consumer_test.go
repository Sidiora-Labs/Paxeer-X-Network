package consumer

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

const cPool = "0xpool00000000000000000000000000000000dddd"

func TestAsStringCoercions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   interface{}
		want string
	}{
		{nil, ""},
		{"hello", "hello"},
		{float64(42), "42"},
		{json.Number("12345678901234567890"), "12345678901234567890"},
		{true, "true"},
	}
	for _, tc := range cases {
		if got := asString(tc.in); got != tc.want {
			t.Errorf("asString(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAsBoolCoercions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   interface{}
		want bool
	}{
		{nil, false},
		{true, true},
		{false, false},
		{"true", true},
		{"false", false},
		{float64(1), false},
	}
	for _, tc := range cases {
		if got := asBool(tc.in); got != tc.want {
			t.Errorf("asBool(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestAsInt64Coercions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   interface{}
		want int64
	}{
		{nil, 0},
		{float64(1704067260), 1704067260},
		{json.Number("99"), 99},
		{"123", 123},
		{"not-a-number", 0},
		{true, 0},
	}
	for _, tc := range cases {
		if got := asInt64(tc.in); got != tc.want {
			t.Errorf("asInt64(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestNewBadRedisURL(t *testing.T) {
	t.Parallel()
	if _, err := New(nil, "not-a-valid-url", slog.Default()); err == nil {
		t.Fatal("New with invalid redis URL should error")
	}
}

func TestConsumerProcessesSwapFromChannel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)

	opt, err := goredis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("parse redis url: %v", err)
	}
	rdb := goredis.NewClient(opt)
	defer rdb.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)
	builder := engine.New(pool, rdb, st, logger)

	sc, err := New(builder, redisURL, logger)
	if err != nil {
		t.Fatalf("New consumer: %v", err)
	}
	defer sc.Close()

	if err := sc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// A malformed message must be tolerated (logged, skipped).
	if err := rdb.Publish(ctx, constants.ChannelSwap, []byte("{not json")).Err(); err != nil {
		t.Fatalf("publish bad: %v", err)
	}

	// A well-formed swap must fold a candle.
	msg, _ := json.Marshal(map[string]interface{}{
		"eventName":   "Swap",
		"blockNumber": 100,
		"txHash":      "0xfeed",
		"logIndex":    0,
		"args": map[string]interface{}{
			"poolId":      "0xpid",
			"poolAddress": cPool,
			"sender":      "0xtrader",
			"isBuy":       true,
			"amountIn":    "100",
			"amountOut":   "200",
			"fee":         "1",
			"price":       "1000000000000000000",
			"timestamp":   1704067260,
		},
	})
	if err := rdb.Publish(ctx, constants.ChannelSwap, msg).Err(); err != nil {
		t.Fatalf("publish good: %v", err)
	}

	// Poll until the consumer folds the candle (async receive loop).
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := st.GetCandle(ctx, cPool, "1m", 1704067260)
		if err == nil && got != nil {
			if got.VolumeUsdl != "100" || got.BuyVolumeUsdl != "100" {
				t.Errorf("folded candle wrong: %+v", got)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("consumer did not fold candle within deadline")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestConsumerFallsBackToBlockNumber(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)

	opt, _ := goredis.ParseURL(redisURL)
	rdb := goredis.NewClient(opt)
	defer rdb.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)
	builder := engine.New(pool, rdb, st, logger)

	sc, err := New(builder, redisURL, logger)
	if err != nil {
		t.Fatalf("New consumer: %v", err)
	}
	defer sc.Close()
	if err := sc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// No args.timestamp -> blockNumber is used as the block timestamp.
	msg, _ := json.Marshal(map[string]interface{}{
		"eventName":   "Swap",
		"blockNumber": 1704067320,
		"txHash":      "0xfeed2",
		"logIndex":    0,
		"args": map[string]interface{}{
			"poolAddress": cPool,
			"sender":      "0xtrader",
			"isBuy":       true,
			"amountIn":    "100",
			"amountOut":   "200",
			"fee":         "1",
			"price":       "1000000000000000000",
		},
	})
	if err := rdb.Publish(ctx, constants.ChannelSwap, msg).Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, err := st.GetCandle(ctx, cPool, "1m", 1704067320); err == nil && got != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("consumer did not fold candle from blockNumber fallback")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestConsumerUsesTopLevelBlockTimestamp asserts the consumer reads the timestamp
// from the indexer envelope's top-level blockTimestamp (the real dual-delivery
// shape) in preference to blockNumber — otherwise real Redis-path swaps would
// resolve to a block number far below minValidTimestamp and be silently skipped.
func TestConsumerUsesTopLevelBlockTimestamp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, pool := internaltest.NewPostgres(t)
	redisURL := internaltest.NewRedisURL(t)
	opt, _ := goredis.ParseURL(redisURL)
	rdb := goredis.NewClient(opt)
	defer rdb.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(pool)
	builder := engine.New(pool, rdb, st, logger)

	sc, err := New(builder, redisURL, logger)
	if err != nil {
		t.Fatalf("New consumer: %v", err)
	}
	defer sc.Close()
	if err := sc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// blockNumber is a real (small) block height; the candle MUST bucket by the
	// top-level blockTimestamp, not blockNumber.
	msg, _ := json.Marshal(map[string]interface{}{
		"eventName":      "Swap",
		"blockNumber":    12345,
		"blockTimestamp": 1704067380,
		"txHash":         "0xfeed3",
		"logIndex":       0,
		"args": map[string]interface{}{
			"poolAddress": cPool,
			"sender":      "0xtrader",
			"isBuy":       true,
			"amountIn":    "100",
			"amountOut":   "200",
			"fee":         "1",
			"price":       "1000000000000000000",
		},
	})
	if err := rdb.Publish(ctx, constants.ChannelSwap, msg).Err(); err != nil {
		t.Fatalf("publish: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, err := st.GetCandle(ctx, cPool, "1m", 1704067380); err == nil && got != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("consumer did not fold candle bucketed by top-level blockTimestamp")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
