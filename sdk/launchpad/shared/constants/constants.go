// Package constants holds the cross-service wire constants (Redis channels,
// queue names, candle timeframes) ported verbatim from the TypeScript shared
// package so Go and TS services interoperate byte-for-byte.
//
// Parity sources:
//   - channels.ts   -> Channel* (invariant i5: identical Redis channel names)
//   - queues.ts     -> Queue*
//   - timeframes.ts -> Timeframes / TimeframeKeys
package constants

// Redis pub/sub channel names (shared/src/constants/channels.ts CHANNELS).
const (
	ChannelSwap               = "indexer:swap"
	ChannelMarketCreated      = "indexer:market_created"
	ChannelPoolStateUpdated   = "indexer:pool_state_updated"
	ChannelFeeRecorded        = "indexer:fee_recorded"
	ChannelFeeDistributed     = "indexer:fee_distributed"
	ChannelFeeStrategyChanged = "indexer:fee_strategy_changed"
	ChannelOpticalExecuted    = "indexer:optical_executed"
	ChannelConfigUpdated      = "indexer:config_updated"
	ChannelCandleUpdate       = "candles:update"

	// Derived-state channels (push-first). Unlike the indexer:* channels — which
	// carry raw on-chain events — these carry the FRESH SNAPSHOT a worker just
	// wrote to its Redis cache, published at the cache-write site so the browser
	// gets a "this changed" signal and never has to poll. Payload is the same
	// JSON the cache holds plus a routing key (poolAddress / category /
	// userAddress).
	ChannelStatsUpdate     = "stats:update"     // pool stats snapshot recomputed
	ChannelHoldersUpdate   = "holders:update"   // holder set / top holders changed
	ChannelPressureUpdate  = "pressure:update"  // buy/sell pressure recomputed
	ChannelReactionsUpdate = "reactions:update" // reaction tally changed
	ChannelPlatformUpdate  = "platform:update"  // platform metrics precomputed
	ChannelRankingsUpdate  = "rankings:update"  // a ranking category recomputed
	ChannelPnlUpdate       = "pnl:update"       // a user's portfolio folded
)

// Channels is the ordered set of every Redis channel the core/api broker
// subscribes to and fans out: the raw indexer:* events, the candle stream, and
// the derived-state push channels.
var Channels = []string{
	ChannelSwap,
	ChannelMarketCreated,
	ChannelPoolStateUpdated,
	ChannelFeeRecorded,
	ChannelFeeDistributed,
	ChannelFeeStrategyChanged,
	ChannelOpticalExecuted,
	ChannelConfigUpdated,
	ChannelCandleUpdate,
	ChannelStatsUpdate,
	ChannelHoldersUpdate,
	ChannelPressureUpdate,
	ChannelReactionsUpdate,
	ChannelPlatformUpdate,
	ChannelRankingsUpdate,
	ChannelPnlUpdate,
}

// Queue names (shared/src/constants/queues.ts QUEUES).
const (
	QueueHolderEnrichment = "holder-enrichment"
	QueueRankingCompute   = "ranking-compute"
	QueueImageProcessing  = "image-processing"
	QueueBackfill         = "indexer-backfill"
)

// Queues is the ordered set of every background queue name.
var Queues = []string{
	QueueHolderEnrichment,
	QueueRankingCompute,
	QueueImageProcessing,
	QueueBackfill,
}

// Timeframes maps a candle timeframe key to its duration in seconds
// (shared/src/constants/timeframes.ts TIMEFRAMES).
var Timeframes = map[string]int64{
	"1m":  60,
	"5m":  300,
	"15m": 900,
	"1h":  3600,
	"4h":  14400,
	"1d":  86400,
	"1w":  604800,
}

// TimeframeKeys lists the timeframe keys in ascending duration order
// (parity with TIMEFRAME_KEYS, which preserves object insertion order).
var TimeframeKeys = []string{"1m", "5m", "15m", "1h", "4h", "1d", "1w"}
