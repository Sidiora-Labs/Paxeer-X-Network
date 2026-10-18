package broker

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newBroker() *Broker {
	return New(Options{Logger: discardLogger()})
}

func swapPayload(pool string, ts int64) []byte {
	b, _ := json.Marshal(map[string]any{
		"poolAddress":    pool,
		"sender":         "0x1111111111111111111111111111111111111111",
		"blockTimestamp": ts,
	})
	return b
}

func candleUpdatePayload(pool, tf string, start int64, closePrice string) []byte {
	b, _ := json.Marshal(map[string]any{
		"poolAddress": pool, "timeframe": tf, "candleStart": start,
		"open": "1000000", "high": "2000000", "low": "500000", "close": closePrice,
		"volumeUsdl": "0", "volumeToken": "0", "buyVolumeUsdl": "0", "sellVolumeUsdl": "0",
		"tradeCount": 1, "uniqueTraders": 1, "largeTradeCount": 0,
		"mcapOpen": "0", "mcapHigh": "0", "mcapLow": "0", "mcapClose": "0",
	})
	return b
}

func poolStatePayload(pool string, n int64) []byte {
	b, _ := json.Marshal(map[string]any{"poolAddress": pool, "reserve": n})
	return b
}

func chanSet(chs ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(chs))
	for _, c := range chs {
		m[c] = struct{}{}
	}
	return m
}

func TestDefaultTransform_CandleFrameShape(t *testing.T) {
	m, ok := DefaultTransform(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 60, "1500000"))
	if !ok {
		t.Fatal("transform candle: ok=false")
	}
	if m.pool != "0xaaa" {
		t.Errorf("pool = %q, want 0xaaa", m.pool)
	}
	if m.coalesceKey != constants.ChannelCandleUpdate+":0xaaa:1m" {
		t.Errorf("coalesceKey = %q", m.coalesceKey)
	}
	var frame struct {
		Type string         `json:"type"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(m.bytes, &frame); err != nil {
		t.Fatalf("frame unmarshal: %v", err)
	}
	if frame.Type != "candle_update" {
		t.Errorf("type = %q, want candle_update", frame.Type)
	}
	if frame.Data["poolAddress"] != "0xAAA" {
		t.Errorf("data.poolAddress = %v", frame.Data["poolAddress"])
	}
	// OHLC must be numeric (float), not the raw text bigints (parity with charts).
	if _, isNum := frame.Data["open"].(float64); !isNum {
		t.Errorf("data.open should be numeric, got %T", frame.Data["open"])
	}
}

func TestDefaultTransform_GenericEnvelope(t *testing.T) {
	m, ok := DefaultTransform(constants.ChannelSwap, swapPayload("0xBBB", 123))
	if !ok {
		t.Fatal("transform swap: ok=false")
	}
	if m.coalesceKey != "" {
		t.Errorf("swap should be must-deliver (no coalesceKey), got %q", m.coalesceKey)
	}
	var frame struct {
		Type    string          `json:"type"`
		Channel string          `json:"channel"`
		Pool    string          `json:"pool"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(m.bytes, &frame); err != nil {
		t.Fatalf("frame unmarshal: %v", err)
	}
	if frame.Type != "swap" || frame.Channel != constants.ChannelSwap || frame.Pool != "0xbbb" {
		t.Errorf("unexpected frame: %+v", frame)
	}
}

func TestDefaultTransform_InvalidJSON(t *testing.T) {
	if _, ok := DefaultTransform(constants.ChannelSwap, []byte("{not json")); ok {
		t.Error("invalid JSON should yield ok=false")
	}
}

// argsNestedSwap is the indexer webhook envelope shape (the dual-delivery Redis
// payload): poolAddress lives under args, not at the top level.
func argsNestedSwap(pool string) []byte {
	b, _ := json.Marshal(map[string]any{
		"eventName": "Swap",
		"txHash":    "0xfeed",
		"logIndex":  0,
		"args":      map[string]any{"poolAddress": pool, "sender": "0x1", "isBuy": true},
	})
	return b
}

func TestDefaultTransform_ResolvesArgsNestedPool(t *testing.T) {
	m, ok := DefaultTransform(constants.ChannelSwap, argsNestedSwap("0xNESTED"))
	if !ok {
		t.Fatal("transform args-nested swap: ok=false")
	}
	if m.pool != "0xnested" {
		t.Errorf("pool = %q, want 0xnested (resolved from args)", m.pool)
	}
	var frame struct {
		Pool string `json:"pool"`
	}
	if err := json.Unmarshal(m.bytes, &frame); err != nil {
		t.Fatalf("frame unmarshal: %v", err)
	}
	if frame.Pool != "0xnested" {
		t.Errorf("frame.pool = %q, want 0xnested", frame.Pool)
	}
}

// TestDispatch_ArgsNestedSwapDoesNotFanOutGlobally is the regression for the
// global-firehose bug: a swap carrying only an args-nested poolAddress must be
// routed to its pool, so a pool-filtered subscriber only sees its own pool.
func TestDispatch_ArgsNestedSwapDoesNotFanOutGlobally(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{
		Channels: chanSet(constants.ChannelSwap),
		Pools:    map[string]struct{}{"0xAAA": {}},
	}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelSwap, argsNestedSwap("0xAAA")) // match
	b.Dispatch(constants.ChannelSwap, argsNestedSwap("0xBBB")) // must be filtered out

	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("Drain len = %d, want 1 (args-nested swap for 0xBBB must not leak)", len(got))
	}
}

func TestDispatch_RoutesByChannelAndPool(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{
		Channels: chanSet(constants.ChannelSwap),
		Pools:    map[string]struct{}{"0xAAA": {}},
	}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 1))                             // match
	b.Dispatch(constants.ChannelSwap, swapPayload("0xBBB", 2))                             // wrong pool
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 60, "1")) // wrong channel

	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("Drain len = %d, want 1 (only the matching swap)", len(got))
	}
}

func TestDispatch_WildcardReceivesAllChannels(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{All: true}, 16) // wildcard
	defer sub.Close()

	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 1))
	b.Dispatch(constants.ChannelMarketCreated, swapPayload("0xCCC", 2))

	if got := sub.Drain(); len(got) != 2 {
		t.Fatalf("wildcard Drain len = %d, want 2", len(got))
	}
}

func TestDispatch_GlobalEventReachesPoolFilteredSubscriber(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{
		Channels: chanSet(constants.ChannelConfigUpdated),
		Pools:    map[string]struct{}{"0xAAA": {}},
	}, 16)
	defer sub.Close()

	// config_updated carries no poolAddress -> global -> delivered despite the
	// pool filter.
	b.Dispatch(constants.ChannelConfigUpdated, []byte(`{"setting":"x"}`))
	if got := sub.Drain(); len(got) != 1 {
		t.Fatalf("global event Drain len = %d, want 1", len(got))
	}
}

func TestDispatch_CoalescesLatestCandlePerKey(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelCandleUpdate)}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 60, "100"))
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 61, "200"))
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 62, "300"))

	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("coalesced Drain len = %d, want 1 (latest only)", len(got))
	}
	var frame struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("frame unmarshal: %v", err)
	}
	if frame.Data["candleStart"].(float64) != 62 {
		t.Errorf("coalesced candleStart = %v, want 62 (latest)", frame.Data["candleStart"])
	}
}

func TestDispatch_DistinctCoalesceKeysKept(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelCandleUpdate)}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 60, "1"))
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "5m", 60, "1"))
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xBBB", "1m", 60, "1"))

	if got := sub.Drain(); len(got) != 3 {
		t.Fatalf("distinct keys Drain len = %d, want 3", len(got))
	}
}

func TestDispatch_MustDeliverPreservesOrder(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 16)
	defer sub.Close()

	for i := int64(1); i <= 3; i++ {
		b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", i))
	}
	got := sub.Drain()
	if len(got) != 3 {
		t.Fatalf("swaps Drain len = %d, want 3 (no drops)", len(got))
	}
	for i, raw := range got {
		var frame struct {
			Data struct {
				BlockTimestamp int64 `json:"blockTimestamp"`
			} `json:"data"`
		}
		_ = json.Unmarshal(raw, &frame)
		if frame.Data.BlockTimestamp != int64(i+1) {
			t.Errorf("frame %d ts = %d, want %d", i, frame.Data.BlockTimestamp, i+1)
		}
	}
}

func TestDeliver_EvictsSlowConsumerOnOverflow(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 2)
	defer sub.Close()

	for i := int64(0); i < 5; i++ {
		b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", i))
	}
	if !sub.Evicted() {
		t.Fatal("subscriber exceeding buffer should be evicted")
	}
}

func TestDeliver_EvictsOnDistinctCoalesceKeyOverflow(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelPoolStateUpdated)}, 2)
	defer sub.Close()

	for i := 0; i < 5; i++ {
		b.Dispatch(constants.ChannelPoolStateUpdated, poolStatePayload(fmt.Sprintf("0x%040d", i), int64(i)))
	}
	if !sub.Evicted() {
		t.Fatal("distinct coalesce keys exceeding buffer should evict")
	}
}

func TestSignal_FiresForMustDeliverNotCoalesced(t *testing.T) {
	b := newBroker()

	coalesced := b.Subscribe(Filter{Channels: chanSet(constants.ChannelCandleUpdate)}, 16)
	defer coalesced.Close()
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xAAA", "1m", 60, "1"))
	select {
	case <-coalesced.Signal():
		t.Error("coalesced delivery should NOT signal (waits for flush tick)")
	default:
	}

	must := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 16)
	defer must.Close()
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 1))
	select {
	case <-must.Signal():
	default:
		t.Error("must-deliver delivery should signal the writer")
	}
}

func TestDispatch_DropsInvalidPayload(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{All: true}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelSwap, []byte("{broken"))
	if got := sub.Drain(); got != nil {
		t.Errorf("invalid payload should deliver nothing, got %d frames", len(got))
	}
	if _, dropped := b.Stats(); dropped != 1 {
		t.Errorf("dropped = %d, want 1", dropped)
	}
}

func TestResubscribe_SwitchesChannelRouting(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 16)
	defer sub.Close()

	// Initially only swaps reach the subscriber.
	b.Dispatch(constants.ChannelMarketCreated, swapPayload("0xAAA", 1))
	if got := sub.Drain(); got != nil {
		t.Fatalf("pre-resubscribe market_created leaked %d frames", len(got))
	}

	// Switch the subscription to market_created only.
	b.Resubscribe(sub, Filter{Channels: chanSet(constants.ChannelMarketCreated)})
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 2))          // no longer wanted
	b.Dispatch(constants.ChannelMarketCreated, swapPayload("0xAAA", 3)) // now wanted
	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("post-resubscribe Drain len = %d, want 1", len(got))
	}
}

// TestDefaultTransform_DerivedStateChannels verifies the push-first channels
// expose their de-prefixed event type, route by pool where applicable, and set
// the coalesce key only for the snapshot (latest-state) channels.
func TestDefaultTransform_DerivedStateChannels(t *testing.T) {
	cases := []struct {
		channel      string
		payload      []byte
		wantType     string
		wantPool     string
		wantCoalesce string
	}{
		{
			channel:      constants.ChannelStatsUpdate,
			payload:      []byte(`{"poolAddress":"0xAAA","priceUsdl":"100"}`),
			wantType:     "stats_update",
			wantPool:     "0xaaa",
			wantCoalesce: constants.ChannelStatsUpdate + ":0xaaa",
		},
		{
			channel:      constants.ChannelPressureUpdate,
			payload:      []byte(`{"poolAddress":"0xBBB","buy":"1","sell":"2"}`),
			wantType:     "pressure_update",
			wantPool:     "0xbbb",
			wantCoalesce: constants.ChannelPressureUpdate + ":0xbbb",
		},
		{
			channel:      constants.ChannelHoldersUpdate,
			payload:      []byte(`{"poolAddress":"0xCCC","holderCount":5}`),
			wantType:     "holders_update",
			wantPool:     "0xccc",
			wantCoalesce: "", // must-deliver
		},
		{
			channel:      constants.ChannelReactionsUpdate,
			payload:      []byte(`{"poolAddress":"0xDDD","rocket":3}`),
			wantType:     "reactions_update",
			wantPool:     "0xddd",
			wantCoalesce: "", // must-deliver
		},
		{
			channel:      constants.ChannelPlatformUpdate,
			payload:      []byte(`{"totalPools":42}`),
			wantType:     "platform_update",
			wantPool:     "", // global
			wantCoalesce: constants.ChannelPlatformUpdate + ":",
		},
		{
			channel:      constants.ChannelRankingsUpdate,
			payload:      []byte(`{"category":"trending","items":[]}`),
			wantType:     "rankings_update",
			wantPool:     "", // global, client filters on category
			wantCoalesce: "",
		},
		{
			channel:      constants.ChannelPnlUpdate,
			payload:      []byte(`{"userAddress":"0xUSER","totalValueUsdl":"500"}`),
			wantType:     "pnl_update",
			wantPool:     "", // global, client filters on userAddress
			wantCoalesce: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.wantType, func(t *testing.T) {
			m, ok := DefaultTransform(tc.channel, tc.payload)
			if !ok {
				t.Fatalf("transform %s: ok=false", tc.channel)
			}
			if m.pool != tc.wantPool {
				t.Errorf("pool = %q, want %q", m.pool, tc.wantPool)
			}
			if m.coalesceKey != tc.wantCoalesce {
				t.Errorf("coalesceKey = %q, want %q", m.coalesceKey, tc.wantCoalesce)
			}
			var frame struct {
				Type    string          `json:"type"`
				Channel string          `json:"channel"`
				Pool    string          `json:"pool"`
				Data    json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(m.bytes, &frame); err != nil {
				t.Fatalf("frame unmarshal: %v", err)
			}
			if frame.Type != tc.wantType || frame.Channel != tc.channel || frame.Pool != tc.wantPool {
				t.Errorf("frame = %+v, want type=%s channel=%s pool=%s", frame, tc.wantType, tc.channel, tc.wantPool)
			}
		})
	}
}

// TestDispatch_StatsUpdateRoutesByPool ensures a stats snapshot only reaches the
// subscriber filtering on its pool (no global firehose) and coalesces.
func TestDispatch_StatsUpdateRoutesByPool(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{
		Channels: chanSet(constants.ChannelStatsUpdate),
		Pools:    map[string]struct{}{"0xAAA": {}},
	}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelStatsUpdate, []byte(`{"poolAddress":"0xAAA","priceUsdl":"1"}`)) // match
	b.Dispatch(constants.ChannelStatsUpdate, []byte(`{"poolAddress":"0xAAA","priceUsdl":"2"}`)) // coalesces with prev
	b.Dispatch(constants.ChannelStatsUpdate, []byte(`{"poolAddress":"0xBBB","priceUsdl":"9"}`)) // wrong pool

	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("Drain len = %d, want 1 (latest snapshot for 0xAAA only)", len(got))
	}
	var frame struct {
		Data struct {
			PriceUsdl string `json:"priceUsdl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatalf("frame unmarshal: %v", err)
	}
	if frame.Data.PriceUsdl != "2" {
		t.Errorf("coalesced priceUsdl = %q, want 2 (latest)", frame.Data.PriceUsdl)
	}
}

// TestDispatch_GlobalRankingsReachesPoolFilteredSubscriber confirms rankings/pnl
// (poolless, global) are delivered to a pool-filtered subscriber for client-side
// filtering on category/userAddress.
func TestDispatch_GlobalRankingsReachesPoolFilteredSubscriber(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{
		Channels: chanSet(constants.ChannelRankingsUpdate),
		Pools:    map[string]struct{}{"0xAAA": {}},
	}, 16)
	defer sub.Close()

	b.Dispatch(constants.ChannelRankingsUpdate, []byte(`{"category":"trending","items":[]}`))
	if got := sub.Drain(); len(got) != 1 {
		t.Fatalf("global rankings Drain len = %d, want 1", len(got))
	}
}

func TestSubscribe_UnregisterReleasesIndex(t *testing.T) {
	b := newBroker()
	sub := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 16)
	if b.Subscribers() != 1 {
		t.Fatalf("Subscribers = %d, want 1", b.Subscribers())
	}
	sub.Close()
	if b.Subscribers() != 0 {
		t.Fatalf("Subscribers after close = %d, want 0", b.Subscribers())
	}
	// After close, dispatch must not panic and must not buffer.
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 1))
	if got := sub.Drain(); got != nil {
		t.Errorf("closed subscription received %d frames", len(got))
	}
}

func TestSubscribe_SnapshotsFilterAndNormalizesPools(t *testing.T) {
	b := newBroker()
	channels := chanSet(constants.ChannelSwap)
	pools := map[string]struct{}{"0xAaA": {}}
	sub := b.Subscribe(Filter{Channels: channels, Pools: pools}, 16)
	defer sub.Close()
	delete(channels, constants.ChannelSwap)
	delete(pools, "0xAaA")
	pools["0xbbb"] = struct{}{}
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 1))
	b.Dispatch(constants.ChannelSwap, swapPayload("0xBBB", 2))
	got := sub.Drain()
	if len(got) != 1 {
		t.Fatalf("initial filter snapshot received %d frames, want only original pool", len(got))
	}
	updated := Filter{Channels: chanSet(constants.ChannelSwap), Pools: map[string]struct{}{"0xBbB": {}}}
	b.Resubscribe(sub, updated)
	delete(updated.Pools, "0xBbB")
	b.Dispatch(constants.ChannelSwap, swapPayload("0xAAA", 3))
	b.Dispatch(constants.ChannelSwap, swapPayload("0xBBB", 4))
	got = sub.Drain()
	if len(got) != 1 {
		t.Fatalf("replacement filter snapshot received %d frames, want only replacement pool", len(got))
	}
	var frame struct {
		Pool string `json:"pool"`
	}
	if err := json.Unmarshal(got[0], &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Pool != "0xbbb" {
		t.Fatalf("replacement pool = %q, want 0xbbb", frame.Pool)
	}
}

func TestDispatchContendedClientCannotBlockHealthyClient(t *testing.T) {
	b := newBroker()
	slow := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 128)
	healthy := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 128)
	defer slow.Close()
	defer healthy.Close()
	slow.mu.Lock()
	finished := make(chan struct{})
	go func() {
		b.Dispatch(constants.ChannelSwap, swapPayload("0xaaa", 1))
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		slow.mu.Unlock()
		t.Fatal("client queue contention blocked dispatch")
	}
	slow.mu.Unlock()
	if !slow.Evicted() || len(healthy.Drain()) != 1 || healthy.Evicted() {
		t.Fatal("contended client was not isolated")
	}
}

func TestResubscribeRacingCloseCannotReinsertSubscription(t *testing.T) {
	b := newBroker()
	for i := 0; i < 1000; i++ {
		s := b.Subscribe(Filter{Channels: chanSet(constants.ChannelSwap)}, 128)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 8; j++ {
				b.Resubscribe(s, Filter{All: j%2 == 0, Channels: chanSet(constants.ChannelMarketCreated)})
			}
		}()
		go func() { defer wg.Done(); s.Close() }()
		wg.Wait()
		b.Resubscribe(s, Filter{All: true})
		if frames, size := s.Occupancy(); frames != 0 || size != 0 {
			t.Fatal("closed queue retained allocation", frames, size)
		}
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.Subscribers() != 0 || len(b.byChannel) != 0 || len(b.wildcard) != 0 {
		t.Fatal("closed subscriptions leaked into routing index")
	}
}

func TestClosedSubscriptionReleasesQueuedPayloads(t *testing.T) {
	b := newBroker()
	s := b.Subscribe(Filter{All: true}, 128)
	b.Dispatch(constants.ChannelSwap, swapPayload("0xaaa", 1))
	b.Dispatch(constants.ChannelCandleUpdate, candleUpdatePayload("0xaaa", "1m", 60, "1"))
	s.Close()
	s.Close()
	if frames, size := s.Occupancy(); frames != 0 || size != 0 || len(s.Drain()) != 0 || b.Subscribers() != 0 {
		t.Fatal("closed subscription retained queue or routing allocation")
	}
}
