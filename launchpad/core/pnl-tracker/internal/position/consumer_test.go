package position_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlcache"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/position"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// setup builds a consumer over real Postgres + Redis with indexer.pools seeded so
// the swap consumer can resolve the pool's token.
func setup(t *testing.T) (*position.Consumer, *store.Store, *goredis.Client) {
	t.Helper()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	internaltest.EnsureIndexerSchema(t, st.Pool())
	if _, err := st.Pool().Exec(context.Background(), `
		INSERT INTO indexer.pools (pool_address, token_address, creator, created_at)
		VALUES ('0xpool','0xtoken','0xc',1)`); err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	return position.NewConsumer(st, rdb, discardLogger()), st, rdb
}

func TestProcessEventFoldsAndResolvesToken(t *testing.T) {
	ctx := context.Background()
	c, st, rdb := setup(t)

	// Seed a stale positions cache to prove the consumer busts it on a new fold.
	if err := rdb.Set(ctx, pnlcache.KeyPositions("0xbuyer"), `{"stale":true}`, 0).Err(); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	buy := position.SwapEvent{
		PoolAddress: "0xPool", Sender: "0xBuyer", IsBuy: true,
		AmountIn: "1000000", AmountOut: "1000000000000000000", // buy: USDL in, tokens out
		Price: "1000000", Fee: "10", BlockNumber: 50, BlockTimestamp: 100, TxHash: "0xtx", LogIndex: 0,
	}
	if err := c.ProcessEvent(ctx, buy); err != nil {
		t.Fatalf("process buy: %v", err)
	}

	pos, err := st.GetPosition(ctx, "0xbuyer", "0xpool")
	if err != nil || pos == nil {
		t.Fatalf("position: %v %v", pos, err)
	}
	if pos.TokenAddress != "0xtoken" {
		t.Errorf("token resolved = %q, want 0xtoken", pos.TokenAddress)
	}
	if pos.TotalUsdlSpent != "1000000" || pos.CurrentHoldings != "1000000000000000000" {
		t.Errorf("position = %+v", pos)
	}
	// Cache was invalidated.
	if n, _ := rdb.Exists(ctx, pnlcache.KeyPositions("0xbuyer")).Result(); n != 0 {
		t.Error("positions cache should be invalidated after a fold")
	}
}

// TestProcessEventPublishesPnlUpdate proves a new fold signals pnl:update for the
// trader so the client refetches the (busted) portfolio ONCE instead of polling.
func TestProcessEventPublishesPnlUpdate(t *testing.T) {
	ctx := context.Background()
	c, _, rdb := setup(t)

	sub := rdb.Subscribe(ctx, constants.ChannelPnlUpdate)
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	if err := c.ProcessEvent(ctx, position.SwapEvent{
		PoolAddress: "0xPool", Sender: "0xBuyer", IsBuy: true,
		AmountIn: "1000000", AmountOut: "1000000000000000000",
		Price: "1000000", Fee: "10", BlockNumber: 50, BlockTimestamp: 100, TxHash: "0xtx", LogIndex: 0,
	}); err != nil {
		t.Fatalf("process buy: %v", err)
	}

	var payload []byte
	select {
	case msg := <-sub.Channel():
		payload = []byte(msg.Payload)
	case <-time.After(3 * time.Second):
		t.Fatal("expected pnl:update publish")
	}

	var got struct {
		UserAddress string `json:"userAddress"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal pnl:update: %v", err)
	}
	if got.UserAddress != "0xbuyer" {
		t.Errorf("pnl:update userAddress = %q, want lowercased 0xbuyer", got.UserAddress)
	}
}

func TestProcessEventIdempotentRedelivery(t *testing.T) {
	ctx := context.Background()
	c, st, _ := setup(t)

	ev := position.SwapEvent{
		PoolAddress: "0xpool", Sender: "0xb", IsBuy: true,
		AmountIn: "1000000", AmountOut: "1000000000000000000",
		Price: "1000000", Fee: "0", BlockNumber: 1, BlockTimestamp: 1, TxHash: "0xtx", LogIndex: 0,
	}
	if err := c.ProcessEvent(ctx, ev); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := c.ProcessEvent(ctx, ev); err != nil {
		t.Fatalf("redeliver: %v", err)
	}
	pos, _ := st.GetPosition(ctx, "0xb", "0xpool")
	if pos.TradeCount != 1 || pos.TotalUsdlSpent != "1000000" {
		t.Fatalf("redelivery double-counted: %+v", pos)
	}
}

func TestProcessSellLegSplit(t *testing.T) {
	ctx := context.Background()
	c, st, _ := setup(t)

	// A sell: amountIn = tokens, amountOut = USDL received.
	sell := position.SwapEvent{
		PoolAddress: "0xpool", Sender: "0xs", IsBuy: false,
		AmountIn: "1000000000000000000", AmountOut: "5000000",
		Price: "5000000", Fee: "0", BlockNumber: 1, BlockTimestamp: 1, TxHash: "0xtx", LogIndex: 0,
	}
	if err := c.ProcessEvent(ctx, sell); err != nil {
		t.Fatalf("process sell: %v", err)
	}
	pos, _ := st.GetPosition(ctx, "0xs", "0xpool")
	// No prior cost basis -> full proceeds realised; tokens sold recorded.
	if pos.RealizedPnlUsdl != "5000000" || pos.TotalTokensSold != "1000000000000000000" {
		t.Fatalf("sell fold = %+v", pos)
	}
}
