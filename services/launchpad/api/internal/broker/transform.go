package broker

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/util"
)

// OutMessage is a fan-out frame: the pre-marshaled client bytes plus the routing
// metadata the broker/subscription need (pool for filtering, coalesceKey for
// backpressure; an empty coalesceKey marks a must-deliver frame).
type OutMessage struct {
	pool        string
	coalesceKey string
	bytes       []byte
}

// channelType maps a Redis channel to the client-facing event "type" field. The
// candle channel keeps the trading-charts name ("candle_update") for client
// parity; the rest expose a stable, de-prefixed event name.
var channelType = map[string]string{
	constants.ChannelSwap:               "swap",
	constants.ChannelMarketCreated:      "market_created",
	constants.ChannelPoolStateUpdated:   "pool_state_updated",
	constants.ChannelFeeRecorded:        "fee_recorded",
	constants.ChannelFeeDistributed:     "fee_distributed",
	constants.ChannelFeeStrategyChanged: "fee_strategy_changed",
	constants.ChannelOpticalExecuted:    "optical_executed",
	constants.ChannelConfigUpdated:      "config_updated",
	constants.ChannelCandleUpdate:       "candle_update",
	// Derived-state push channels: de-prefixed event names the data-stream
	// client subscribes to. stats/holders/pressure/reactions route by pool;
	// platform is global; rankings/pnl are global and the client filters on the
	// payload's category/userAddress (low volume — no pool-keyed Filter needed).
	constants.ChannelStatsUpdate:     "stats_update",
	constants.ChannelHoldersUpdate:   "holders_update",
	constants.ChannelPressureUpdate:  "pressure_update",
	constants.ChannelReactionsUpdate: "reactions_update",
	constants.ChannelPlatformUpdate:  "platform_update",
	constants.ChannelRankingsUpdate:  "rankings_update",
	constants.ChannelPnlUpdate:       "pnl_update",
}

// coalescable lists the channels whose ticks represent the LATEST STATE and may
// therefore be coalesced (latest-per-key) under backpressure. Discrete events
// (swaps, market_created, fee/optical events) are NOT coalesced so none is ever
// silently dropped — they are must-deliver (parity with the client, which only
// coalesces candle ticks).
var coalescable = map[string]bool{
	constants.ChannelCandleUpdate:     true,
	constants.ChannelPoolStateUpdated: true,
	// Snapshot channels carry the LATEST recomputed state per key, so under
	// backpressure only the newest matters (latest-per-pool / latest-global).
	constants.ChannelStatsUpdate:    true,
	constants.ChannelPressureUpdate: true,
	constants.ChannelPlatformUpdate: true,
	// holders/reactions/rankings/pnl stay must-deliver: each tick reflects a
	// discrete mutation a user expects to see land, and volume is low.
}

// routing is the minimal envelope the transform parses out of every payload to
// route and (for candles) coalesce by pool+timeframe.
//
// poolAddress lives at the top level for the flat candle payload but is nested
// under args for the indexer webhook envelope (the dual-delivery Redis payload
// is one webhook event: {eventName,...,args:{poolAddress}}). We accept either so
// a swap/pool_state event routes to its pool instead of fanning out globally.
type routing struct {
	PoolAddress string `json:"poolAddress"`
	Timeframe   string `json:"timeframe"`
	Args        struct {
		PoolAddress string `json:"poolAddress"`
	} `json:"args"`
}

// pool resolves the routing pool from the top-level field, falling back to the
// args-nested field used by indexer:* envelopes. Empty means a global event.
//
// The resolved pool is lowercased so the routing/filter key is canonical: the
// indexer emits checksummed/mixed-case poolAddress while clients subscribe with
// lowercased pools, and broker.Filter.wantsPool is an exact map lookup. Without
// canonicalising both ends, equal-by-value addresses would never match and every
// swap/candle_update frame would be dropped before reaching the subscriber
// (Bug 3 / 6a). The candle data payload keeps its original-cased poolAddress;
// only the routing pool is normalised.
func (r routing) pool() string {
	if r.PoolAddress != "" {
		return strings.ToLower(r.PoolAddress)
	}
	return strings.ToLower(r.Args.PoolAddress)
}

// DefaultTransform converts a raw Redis channel payload into an OutMessage.
//
// For the candle channel it reproduces the trading-charts WS frame byte-shape
// (type "candle_update" with numeric OHLCV/mcap fields formatted from the text
// bigints) so existing /ws/candles clients are unaffected. For every other
// channel it forwards the raw payload under a uniform envelope:
//
//	{"type": <event>, "channel": <redis-channel>, "pool": <addr>, "data": <raw>}
//
// ok is false when the payload is not valid JSON (the broker then drops it).
func DefaultTransform(channel string, payload []byte) (OutMessage, bool) {
	var r routing
	if err := json.Unmarshal(payload, &r); err != nil {
		return OutMessage{}, false
	}
	pool := r.pool()

	typ, known := channelType[channel]
	if !known {
		typ = channel
	}

	coalesceKey := ""
	if coalescable[channel] {
		coalesceKey = channel + ":" + pool
		if r.Timeframe != "" {
			coalesceKey += ":" + r.Timeframe
		}
	}

	if channel == constants.ChannelCandleUpdate {
		bytes, ok := candleFrame(payload)
		if !ok {
			return OutMessage{}, false
		}
		return OutMessage{pool: pool, coalesceKey: coalesceKey, bytes: bytes}, true
	}

	frame, err := json.Marshal(map[string]any{
		"type":    typ,
		"channel": channel,
		"pool":    pool,
		"data":    json.RawMessage(payload),
	})
	if err != nil {
		return OutMessage{}, false
	}
	return OutMessage{pool: pool, coalesceKey: coalesceKey, bytes: frame}, true
}

// candlePayload mirrors the candles:update payload the indexer/charts service
// publishes (text bigints; invariant i1). All money fields are formatted to
// decimal strings then parsed to float for the TradingView-style client frame
// (identical to core/trading-charts ws.go broadcastCandleUpdate).
type candlePayload struct {
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

func candleFrame(payload []byte) ([]byte, bool) {
	var e candlePayload
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, false
	}
	frame, err := json.Marshal(map[string]any{
		"type": "candle_update",
		"data": map[string]any{
			"poolAddress":     e.PoolAddress,
			"timeframe":       e.Timeframe,
			"candleStart":     e.CandleStart,
			"open":            fprice(e.Open),
			"high":            fprice(e.High),
			"low":             fprice(e.Low),
			"close":           fprice(e.Close),
			"volumeUsdl":      fvol(e.VolumeUsdl),
			"volumeToken":     fvol(e.VolumeToken),
			"buyVolumeUsdl":   fvol(e.BuyVolumeUsdl),
			"sellVolumeUsdl":  fvol(e.SellVolumeUsdl),
			"tradeCount":      e.TradeCount,
			"uniqueTraders":   e.UniqueTraders,
			"largeTradeCount": e.LargeTradeCount,
			"mcapOpen":        fvol(e.McapOpen),
			"mcapHigh":        fvol(e.McapHigh),
			"mcapLow":         fvol(e.McapLow),
			"mcapClose":       fvol(e.McapClose),
		},
	})
	if err != nil {
		return nil, false
	}
	return frame, true
}

// fprice formats a text bigint price to a float using the shared 8-dp price
// formatter (exact decimal, no float math; invariant i1), returning 0 on error.
func fprice(raw string) float64 {
	s, err := util.FormatPrice(raw)
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// fvol formats a text bigint volume to a float using the shared 2-dp volume
// formatter, returning 0 on error.
func fvol(raw string) float64 {
	s, err := util.FormatVolume(raw)
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
