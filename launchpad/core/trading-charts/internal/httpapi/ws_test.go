package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	goredis "github.com/redis/go-redis/v9"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture() CandleSnapshot {
	return CandleSnapshot{Version: 1, Chain: 125, PoolAddress: "0x" + strings.Repeat("a", 40), Timeframe: "1m", Start: 60, Revision: "1", AlgorithmVersion: "v1", SourceWatermark: "125:4", Cursor: "opaque", Open: "1", High: "2", Low: "1", Close: "2", VolumeUsdl: "3", VolumeToken: "0", BuyVolumeUsdl: "3", SellVolumeUsdl: "0", McapOpen: "1", McapHigh: "2", McapLow: "1", McapClose: "2"}
}
func TestSnapshotExactAndCoalesced(t *testing.T) {
	c := NewCandleCoalescer(2)
	a := fixture()
	for _, r := range []string{"1", "3", "2", "3"} {
		a.Revision = r
		if e := c.Add(a); e != nil {
			t.Fatal(e)
		}
	}
	b := a
	b.PoolAddress = "0x" + strings.Repeat("b", 40)
	if e := c.Add(b); e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	out := c.Drain(now)
	if len(out) != 2 {
		t.Fatal(out)
	}
	for _, v := range out {
		if v.Revision != "3" {
			t.Fatal(v)
		}
	}
	a.Revision = "4"
	_ = c.Add(a)
	if len(c.Drain(now.Add(499*time.Millisecond))) != 0 {
		t.Fatal("over 2Hz")
	}
	if len(c.Drain(now.Add(500*time.Millisecond))) != 1 {
		t.Fatal("missing flush")
	}
	a.Open = "1.0"
	if _, e := EncodeCandle(a); e == nil {
		t.Fatal("noncanonical accepted")
	}
}
func TestConflictAndBound(t *testing.T) {
	c := NewCandleCoalescer(1)
	a := fixture()
	_ = c.Add(a)
	a.Open = "3"
	if c.Add(a) == nil {
		t.Fatal("conflict accepted")
	}
	a.PoolAddress = "0x" + strings.Repeat("b", 40)
	if c.Add(a) == nil {
		t.Fatal("capacity exceeded")
	}
}
func TestSlowConsumerAndHealthy(t *testing.T) {
	h := &wsHub{}
	slow := &clientSub{send: make(chan []byte, 256), quit: make(chan struct{})}
	fast := &clientSub{send: make(chan []byte, 1), quit: make(chan struct{})}
	for i := 0; i < 257; i++ {
		h.enqueue(slow, make([]byte, 1024))
	}
	select {
	case <-slow.quit:
	default:
		t.Fatal("not evicted")
	}
	if slow.queued.Load() > 256*1024 {
		t.Fatal("byte bound")
	}
	h.enqueue(fast, []byte("healthy"))
	if string(<-fast.send) != "healthy" {
		t.Fatal("blocked")
	}
}
func TestSocketValidationAndOrigins(t *testing.T) {
	h := &wsHub{clients: map[*clientSub]struct{}{}, subscriptionIndex: map[string]map[*clientSub]struct{}{}, wildcardSubs: map[*clientSub]struct{}{}, ipCounts: map[string]int{}, maxConns: 10, maxPerIP: 10}
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	defer srv.Close()
	u := "ws" + strings.TrimPrefix(srv.URL, "http")
	header := http.Header{"Origin": []string{"https://evil.invalid"}}
	if c, _, e := websocket.DefaultDialer.Dial(u, header); e == nil {
		c.Close()
		t.Fatal("origin accepted")
	}
	conn, _, e := websocket.DefaultDialer.Dial(u, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close()
	read := func() map[string]interface{} {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(time.Second))
		_, raw, e := conn.ReadMessage()
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]interface{}
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	read()
	for _, pools := range [][]string{{}, make([]string, 21), {"invalid"}} {
		conn.WriteJSON(map[string]interface{}{"type": "subscribe", "pools": pools, "timeframes": []string{"1m"}})
		if read()["type"] != "error" {
			t.Fatal("invalid accepted")
		}
	}
	pool := fixture().PoolAddress
	conn.WriteJSON(map[string]interface{}{"type": "subscribe", "pools": []string{pool, strings.ToUpper(pool[:2]) + pool[2:]}, "timeframes": []string{"1m"}})
	if read()["type"] != "subscribed" {
		t.Fatal("alias rejected")
	}
	uppercase := fixture()
	uppercase.PoolAddress = strings.ToUpper(uppercase.PoolAddress)
	h.broadcastCandleUpdate(string(mustEncode(t, uppercase)))
	snapshot := read()
	if snapshot["type"] != "candle.updated" || snapshot["data"].(map[string]interface{})["poolAddress"] != pool {
		t.Fatal("uppercase snapshot missed canonical subscription", snapshot)
	}
	conn.WriteJSON(map[string]interface{}{"type": "unsubscribe", "pools": []string{pool}, "timeframes": []string{"1m"}})
	if read()["type"] != "unsubscribed" {
		t.Fatal("unsubscribe")
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.subscriptionIndex) != 0 {
		t.Fatal("leaked index")
	}
}
func mustEncode(t *testing.T, c CandleSnapshot) []byte {
	t.Helper()
	b, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestRedisPartitionToRealSocket(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	channel := "kd-25af2ac44985146cf57fae0c:L12:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	url := "redis://:kindle_fixture_password@127.0.0.1:26379/0"
	rdb := goredis.NewClient(parseRedisOpts(url))
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	h := &wsHub{clients: map[*clientSub]struct{}{}, subscriptionIndex: map[string]map[*clientSub]struct{}{}, wildcardSubs: map[*clientSub]struct{}{}, ipCounts: map[string]int{}, maxConns: 10, maxPerIP: 10, channel: channel, retentionCapacity: 2}
	go h.runRedisSubscriber(ctx, url)
	deadline := time.Now().Add(3 * time.Second)
	for {
		n, e := rdb.PubSubNumSub(ctx, channel).Result()
		if e != nil {
			t.Fatal(e)
		}
		if n[channel] == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscriber not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	read := func() map[string]interface{} {
		t.Helper()
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, raw, e := conn.ReadMessage()
		if e != nil {
			t.Fatal(e)
		}
		var v map[string]interface{}
		if e = json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	read()
	a := fixture()
	b := a
	b.PoolAddress = "0x" + strings.Repeat("b", 40)
	conn.WriteJSON(map[string]interface{}{"type": "subscribe", "pools": []string{a.PoolAddress, b.PoolAddress}, "timeframes": []string{"1m"}})
	if read()["type"] != "subscribed" {
		t.Fatal("subscribe")
	}
	for _, r := range []string{"1", "3", "2", "3"} {
		a.Revision = r
		if err = rdb.Publish(ctx, channel, mustEncode(t, a)).Err(); err != nil {
			t.Fatal(err)
		}
	}
	if err = rdb.Publish(ctx, channel, mustEncode(t, b)).Err(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for len(seen) < 2 {
		msg := read()
		data, ok := msg["data"].(map[string]interface{})
		if !ok {
			t.Fatal(msg)
		}
		seen[data["poolAddress"].(string)] = data["revision"].(string)
	}
	if seen[a.PoolAddress] != "3" || seen[b.PoolAddress] != "1" {
		t.Fatal(seen)
	}
	a.Revision = "2"
	b.Revision = "2"
	if err = rdb.Publish(ctx, channel, mustEncode(t, a)).Err(); err != nil {
		t.Fatal(err)
	}
	if err = rdb.Publish(ctx, channel, mustEncode(t, b)).Err(); err != nil {
		t.Fatal(err)
	}
	next := read()
	nextData := next["data"].(map[string]interface{})
	if nextData["poolAddress"] != b.PoolAddress || nextData["revision"] != "2" {
		t.Fatal("stale state reemitted after drain", next)
	}
	a.Revision = "3"
	a.Open = "3"
	if err = rdb.Publish(ctx, channel, mustEncode(t, a)).Err(); err != nil {
		t.Fatal(err)
	}
	if conflict := read(); conflict["type"] != "resync_required" {
		t.Fatal("cross-window integrity error did not trigger resync", conflict)
	}
	a.Open = "1"
	a.Revision = "1"
	a.Start = 120
	if err = rdb.Publish(ctx, channel, mustEncode(t, a)).Err(); err != nil {
		t.Fatal(err)
	}
	if saturated := read(); saturated["type"] != "resync_required" {
		t.Fatal("retention saturation silently lost live state", saturated)
	}

}
func TestQuotaAtomicAndForgedIdentity(t *testing.T) {
	h := &wsHub{clients: map[*clientSub]struct{}{}, subscriptionIndex: map[string]map[*clientSub]struct{}{}, wildcardSubs: map[*clientSub]struct{}{}, ipCounts: map[string]int{}, maxConns: 10, maxPerIP: 10}
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	read := func() string {
		conn.SetReadDeadline(time.Now().Add(time.Second))
		_, raw, e := conn.ReadMessage()
		if e != nil {
			t.Fatal(e)
		}
		var msg struct{ Type string }
		json.Unmarshal(raw, &msg)
		return msg.Type
	}
	read()
	for i := 0; i < 21; i++ {
		conn.WriteJSON(map[string]interface{}{"type": "subscribe", "pools": []string{fmt.Sprintf("0x%040x", i+1)}, "timeframes": []string{"1m"}, "requestId": "quota"})
		got := read()
		want := "subscribed"
		if i == 20 {
			want = "error"
		}
		if got != want {
			t.Fatalf("topic %d: %s", i, got)
		}
	}
	h.mu.RLock()
	n := len(h.subscriptionIndex)
	h.mu.RUnlock()
	if n != 20 {
		t.Fatal(n)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "8.8.8.8")
	if clientIPFromRequest(req) != "127.0.0.1" {
		t.Fatal("forged identity")
	}
	conn.WriteMessage(websocket.TextMessage, make([]byte, 16*1024+1))
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, e := conn.ReadMessage(); e == nil {
		t.Fatal("oversize accepted")
	}
}

func TestExplicitPairsDoNotCreateCrossProducts(t *testing.T) {
	a := fixture().PoolAddress
	b := "0x" + strings.Repeat("b", 40)
	topics, err := validateTopics([]seriesSubscription{{a, "1m"}, {b, "5m"}, {strings.ToUpper(a), "1m"}}, nil, nil)
	if err != nil || len(topics) != 2 {
		t.Fatalf("topics=%v err=%v", topics, err)
	}
	if _, ok := topics[a+":5m"]; ok {
		t.Fatal("cross product allocated")
	}
	if _, err = validateTopics([]seriesSubscription{{a, "1m"}}, []string{a}, []string{"1m"}); err == nil {
		t.Fatal("ambiguous format")
	}
}

func TestSlowSocketCloseIncludesCursor(t *testing.T) {
	h := &wsHub{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sub := &clientSub{conn: conn, send: make(chan []byte, 256), quit: make(chan struct{}), cursor: "accepted-stream-cursor"}
		h.enqueue(sub, make([]byte, 256*1024))
		h.enqueue(sub, []byte("overflow"))
		h.writePump(sub)
	}))
	defer srv.Close()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err = conn.ReadMessage()
		if err != nil {
			break
		}
	}
	closeErr, ok := err.(*websocket.CloseError)
	if !ok || closeErr.Code != 1013 || !strings.Contains(closeErr.Text, "accepted-stream-cursor") {
		t.Fatalf("close: %v", err)
	}
}

func TestRevisionKnowledgeSurvivesDrain(t *testing.T) {
	c := NewCandleCoalescer(1)
	v := fixture()
	v.Revision = "3"
	v.PoolAddress = strings.ToUpper(v.PoolAddress)
	if err := c.Add(v); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	first := c.Drain(now)
	if len(first) != 1 || first[0].PoolAddress != fixture().PoolAddress {
		t.Fatalf("not normalized: %v", first)
	}
	v.PoolAddress = strings.ToLower(v.PoolAddress)
	v.Revision = "2"
	if err := c.Add(v); err != nil {
		t.Fatal(err)
	}
	if got := c.Drain(now.Add(time.Second)); len(got) != 0 {
		t.Fatalf("stale revision reemitted: %v", got)
	}
	v.Revision = "3"
	v.Open = "3"
	if err := c.Add(v); err == nil {
		t.Fatal("cross-window conflict accepted")
	}
	v.Open = "1"
	v.Cursor = "new-transport-cursor"
	if err := c.Add(v); err != nil {
		t.Fatal(err)
	}
	if got := c.Drain(now.Add(2 * time.Second)); len(got) != 0 {
		t.Fatal("same revision reemitted")
	}
	v.Revision = "4"
	if err := c.Add(v); err != nil {
		t.Fatal(err)
	}
	if got := c.Drain(now.Add(3 * time.Second)); len(got) != 1 || got[0].Revision != "4" {
		t.Fatal(got)
	}
	v.PoolAddress = "0x" + strings.Repeat("b", 40)
	if err := c.Add(v); err == nil {
		t.Fatal("retention capacity evicted revision knowledge")
	}
}

func TestSocketAdversarialFloodAndRelease(t *testing.T) {
	h := &wsHub{clients: map[*clientSub]struct{}{}, subscriptionIndex: map[string]map[*clientSub]struct{}{}, wildcardSubs: map[*clientSub]struct{}{}, ipCounts: map[string]int{}, maxConns: 8, maxPerIP: 1}
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	defer srv.Close()
	endpoint := "ws" + strings.TrimPrefix(srv.URL, "http")
	headers := http.Header{"X-Forwarded-For": {"8.8.8.8"}, "Forwarded": {"for=8.8.8.8"}, "X-Real-IP": {"8.8.8.8"}}
	first, _, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	read := func(conn *websocket.Conn) string {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var frame struct{ Type string }
		if json.Unmarshal(raw, &frame) != nil {
			t.Fatal("invalid server JSON")
		}
		return frame.Type
	}
	if read(first) != "connected" {
		t.Fatal("welcome missing")
	}
	headers.Set("X-Forwarded-For", "9.9.9.9")
	second, _, err := websocket.DefaultDialer.Dial(endpoint, headers)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if read(second) != "error" {
		t.Fatal("forged forwarding headers bypassed socket quota")
	}
	_ = first.WriteJSON(map[string]any{"type": "subscribe", "series": []seriesSubscription{{fixture().PoolAddress, "1m"}}})
	if read(first) != "subscribed" {
		t.Fatal("subscription missing")
	}
	for i := 0; i < 100; i++ {
		if first.WriteJSON(map[string]any{"type": "subscribe", "series": []seriesSubscription{{fixture().PoolAddress, "1m"}}, "requestId": fmt.Sprint(i)}) != nil {
			break
		}
	}
	_ = first.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, _, err := first.ReadMessage(); err != nil {
			break
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.mu.RLock()
		released := len(h.clients) == 0 && len(h.subscriptionIndex) == 0 && len(h.ipCounts) == 0
		h.mu.RUnlock()
		if released {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("flood teardown retained subscriptions or IP quota")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestSocketRejectsMalformedOriginAndBinaryFrames(t *testing.T) {
	h := &wsHub{clients: map[*clientSub]struct{}{}, subscriptionIndex: map[string]map[*clientSub]struct{}{}, wildcardSubs: map[*clientSub]struct{}{}, ipCounts: map[string]int{}, maxConns: 4, maxPerIP: 4}
	srv := httptest.NewServer(http.HandlerFunc(h.handleWS))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")
	for _, origin := range []string{"http://user@" + host, "http://" + host + "/path", "http://" + host + "?forged=1", "http://" + host + "#fragment", "https://evil.invalid"} {
		conn, _, err := websocket.DefaultDialer.Dial("ws://"+host, http.Header{"Origin": {origin}})
		if err == nil {
			_ = conn.Close()
			t.Fatalf("malformed origin admitted: %s", origin)
		}
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err = conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err = conn.WriteMessage(websocket.BinaryMessage, []byte(`{"type":"subscribe"}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.ReadMessage(); err == nil {
		t.Fatal("binary subscription frame admitted")
	}
}
