// Package position folds indexer Swap events into per-(user, pool) PnL positions.
// It is the realtime consumer (driven by the HMAC webhook) and shares the exact
// idempotent fold the reconciler uses (store.FoldTrade), so realtime delivery and
// backfill converge to the same state. The Swap webhook args carry only
// poolAddress, so the token address is resolved cross-schema via indexer.pools.
// All money math lives in internal/pnlmath (math/big — invariant i1).
package position

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	sharedredis "github.com/Sidiora-Technologies/KindleLaunch/shared/redis"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/pnlcache"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

// SwapEvent is a decoded Swap webhook event (the fields the PnL fold needs). A
// buy spends amountIn USDL for amountOut tokens; a sell sends amountIn tokens for
// amountOut USDL — the same leg convention the stats swap consumer uses.
type SwapEvent struct {
	PoolAddress    string
	Sender         string
	IsBuy          bool
	AmountIn       string
	AmountOut      string
	Price          string
	Fee            string
	BlockNumber    int64
	BlockTimestamp int64
	TxHash         string
	LogIndex       int
}

// Consumer folds swaps into positions and busts the trader's read cache.
type Consumer struct {
	store  *store.Store
	redis  *goredis.Client
	logger *slog.Logger
}

// NewConsumer builds a swap Consumer.
func NewConsumer(st *store.Store, rdb *goredis.Client, logger *slog.Logger) *Consumer {
	return &Consumer{store: st, redis: rdb, logger: logger}
}

// Legs splits a swap into its USDL and token amounts by direction.
func legs(ev SwapEvent) (usdl, token string) {
	if ev.IsBuy {
		return ev.AmountIn, ev.AmountOut
	}
	return ev.AmountOut, ev.AmountIn
}

// ProcessEvent folds one Swap into the sender's position, idempotently, then
// invalidates the sender's cached reads when a new trade was recorded. Ports the
// TS PnL swap consumer.
func (c *Consumer) ProcessEvent(ctx context.Context, ev SwapEvent) error {
	usdl, token := legs(ev)

	tokenAddr, err := c.store.GetPoolToken(ctx, strings.ToLower(ev.PoolAddress))
	if err != nil {
		return err
	}

	inserted, err := c.store.FoldTrade(ctx, store.TradeInput{
		ID:             fmt.Sprintf("%s-%d", ev.TxHash, ev.LogIndex),
		UserAddress:    ev.Sender,
		PoolAddress:    ev.PoolAddress,
		TokenAddress:   tokenAddr,
		IsBuy:          ev.IsBuy,
		UsdlAmount:     usdl,
		TokenAmount:    token,
		Price:          ev.Price,
		Fee:            ev.Fee,
		BlockNumber:    ev.BlockNumber,
		BlockTimestamp: ev.BlockTimestamp,
		TxHash:         ev.TxHash,
	})
	if err != nil {
		return err
	}
	if !inserted {
		return nil // redelivery — position already reflects this trade
	}
	if err := pnlcache.InvalidateUser(ctx, c.redis, ev.Sender); err != nil {
		// Cache busting is best-effort; the TTL bounds staleness. Log and proceed.
		c.logger.Warn("failed to invalidate pnl cache",
			slog.String("user", strings.ToLower(ev.Sender)), slog.Any("err", err))
	}
	// Push-first: signal pnl:update for this user so the client refetches the
	// (now-busted) portfolio ONCE instead of polling. A signal (not the full
	// summary) keeps the hot fold path cheap; the broker treats it as a global
	// event and the client filters on userAddress. Best-effort.
	if payload, err := json.Marshal(map[string]string{"userAddress": strings.ToLower(ev.Sender)}); err == nil {
		_ = sharedredis.PublishJSON(ctx, c.redis, constants.ChannelPnlUpdate, payload)
	}
	return nil
}
