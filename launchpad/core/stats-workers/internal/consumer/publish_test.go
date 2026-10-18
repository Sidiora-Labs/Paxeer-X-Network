package consumer_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/consumer"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/store"
)

// subscribeConfirmed subscribes to channel and blocks until the subscription is
// confirmed, so a publish that follows can never race ahead of registration.
func subscribeConfirmed(t *testing.T, ctx context.Context, rdb *goredis.Client, channel string) *goredis.PubSub {
	t.Helper()
	sub := rdb.Subscribe(ctx, channel)
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe %s: %v", channel, err)
	}
	t.Cleanup(func() { _ = sub.Close() })
	return sub
}

// awaitPublish waits up to 3s for one message on sub, returning its raw payload.
func awaitPublish(t *testing.T, sub *goredis.PubSub) []byte {
	t.Helper()
	select {
	case msg := <-sub.Channel():
		return []byte(msg.Payload)
	case <-time.After(3 * time.Second):
		t.Fatal("expected a published message, got none")
		return nil
	}
}

// TestSwapPublishesStatsUpdate proves the push-first contract: folding a swap
// caches the pool_stats row AND publishes the same snapshot on stats:update so
// the broker can fan it out (no polling). The published payload must be the
// pool_stats row carrying poolAddress for broker routing.
func TestSwapPublishesStatsUpdate(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	sc := consumer.NewSwapConsumer(st, rdb, discardLogger())

	const addr = "0xpublish_stats"
	seedPoolStats(t, ctx, st, addr, nil, "10000000000000")

	sub := subscribeConfirmed(t, ctx, rdb, constants.ChannelStatsUpdate)

	if err := sc.ProcessEvent(ctx, consumer.SwapEvent{
		PoolAddress: addr, Sender: "0xt1", IsBuy: true,
		AmountIn: "1000000", AmountOut: "5000", Price: "20000000000000",
		Fee: "10", BlockTimestamp: time.Now().Unix(), TxHash: "0xpub", LogIndex: 0,
	}); err != nil {
		t.Fatalf("process swap: %v", err)
	}

	var row store.PoolStatsRow
	if err := json.Unmarshal(awaitPublish(t, sub), &row); err != nil {
		t.Fatalf("unmarshal stats:update payload: %v", err)
	}
	if row.PoolAddress != addr {
		t.Errorf("published poolAddress = %q, want %q (broker routes on it)", row.PoolAddress, addr)
	}
	if row.Price != "20000000000000" {
		t.Errorf("published price = %q, want the freshly-folded price", row.Price)
	}
}

// TestHolderRefreshPublishesHoldersUpdate proves a holder-stats refresh pushes a
// holders:update snapshot ({poolAddress, holderCount, topHolders}).
func TestHolderRefreshPublishesHoldersUpdate(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	tracker := consumer.NewHolderTracker(st, rdb, discardLogger(), time.Hour) // long debounce; we call RefreshNow directly
	t.Cleanup(tracker.Close)

	const addr = "0xpublish_holders"
	seedPoolStats(t, ctx, st, addr, nil, "10000000000000")

	// Apply a buy so the pool has a holder row to summarise.
	if _, err := st.ApplyHolderDelta(ctx, addr, "0xholder1", true, "0", "1000000", time.Now().Unix()); err != nil {
		t.Fatalf("apply holder delta: %v", err)
	}

	sub := subscribeConfirmed(t, ctx, rdb, constants.ChannelHoldersUpdate)

	if err := tracker.RefreshNow(ctx, addr); err != nil {
		t.Fatalf("refresh holders: %v", err)
	}

	var snap struct {
		PoolAddress string            `json:"poolAddress"`
		HolderCount int               `json:"holderCount"`
		TopHolders  []store.HolderRow `json:"topHolders"`
	}
	if err := json.Unmarshal(awaitPublish(t, sub), &snap); err != nil {
		t.Fatalf("unmarshal holders:update payload: %v", err)
	}
	if snap.PoolAddress != addr {
		t.Errorf("published poolAddress = %q, want %q", snap.PoolAddress, addr)
	}
	if snap.HolderCount != 1 {
		t.Errorf("published holderCount = %d, want 1", snap.HolderCount)
	}
	if len(snap.TopHolders) != 1 || snap.TopHolders[0].HolderAddress != "0xholder1" {
		t.Errorf("published topHolders = %+v, want the single seeded holder", snap.TopHolders)
	}
}
