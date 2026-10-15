package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery/natsdriver"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/stream"
)

const spotUserTestAddress = "0x00000000000000000000000000000000000000a1"
const spotUserTestToken = "0x00000000000000000000000000000000000000b1"

func spotUserRead(t *testing.T, conn *websocket.Conn, wait time.Duration) map[string]json.RawMessage {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
		t.Fatal(err)
	}
	kind, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if kind != websocket.TextMessage {
		t.Fatalf("frame kind = %d, want text", kind)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func spotUserExpect(t *testing.T, got map[string]json.RawMessage, want string) {
	t.Helper()
	var expected map[string]json.RawMessage
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Fatalf("frame = %s, want %s", mustSpotUserJSON(t, got), want)
	}
}

func mustSpotUserJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func spotUserSend(t *testing.T, conn *websocket.Conn, op, stream, address string) {
	t.Helper()
	if err := conn.WriteJSON(map[string]string{"op": op, "stream": stream, "address": address}); err != nil {
		t.Fatal(err)
	}
}

func spotUserControlWant(kind, stream, address string) string {
	return fmt.Sprintf(`{"type":%q,"stream":%q,"address":%q}`, kind, stream, address)
}

func spotUserBroker(t *testing.T) (*nats.Conn, jetstream.Stream) {
	t.Helper()
	ns, err := server.NewServer(&server.Options{
		Host: net.IPv4(127, 0, 0, 1).String(), Port: server.RANDOM_PORT,
		JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go ns.Start()
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("broker did not become ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	cfg := stream.Config(125)
	cfg.Replicas = 1
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	brokerStream, err := js.CreateStream(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return nc, brokerStream
}

func spotUserBrokerItem(t *testing.T, nc *nats.Conn, brokerStream jetstream.Stream, socket *SpotUser, streamName, subject, address string, item json.RawMessage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := natsdriver.ConsumerConfig{
		Stream: stream.Name, Durable: "spot_" + strings.ReplaceAll(streamName, "-", "_"), Subject: subject,
		Batch: 1, MaxAckPending: 1, MaxWaiting: 1, MaxDeliver: 2,
		FetchWait: time.Second, AckWait: 5 * time.Second,
	}
	brokerCfg, err := cfg.BrokerConsumerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = brokerStream.CreateConsumer(ctx, brokerCfg); err != nil {
		t.Fatal(err)
	}
	consumer, err := natsdriver.NewConsumer(ctx, nc, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer consumer.Close()
	publisher, err := natsdriver.NewPublisher(nc, stream.Name)
	if err != nil {
		t.Fatal(err)
	}
	id := "spot_" + strings.ReplaceAll(streamName, "-", "_")
	parts := strings.Split(subject, ".")
	kind := parts[len(parts)-1]
	policy := delivery.Policy{Subjects: map[string]bool{subject: true}, EventTypes: map[string]bool{kind: true}}
	envelope := delivery.Envelope{
		EventID: id, EventType: kind, SchemaVersion: 1, ChainID: 125,
		SourceOrder: delivery.SourceOrder{Height: "1", TransactionIndex: "0", LogIndex: "0"},
		EmittedAt:   time.Now().UTC(), TraceID: id, PayloadHash: delivery.Hash(item), Payload: item,
	}
	wire, err := envelope.Encode(policy, subject)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := publisher.Publish(ctx, subject, id, wire)
	if err != nil {
		t.Fatal(err)
	}
	messages, err := consumer.Fetch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(messages))
	}
	msg := messages[0]
	if msg.Subject != subject || msg.EventID != id || msg.Stream != stream.Name || msg.Sequence != ack.Sequence || msg.Deliveries != 1 {
		t.Fatalf("unexpected delivery: %+v", msg)
	}
	decoded, err := delivery.Decode(msg.Wire, policy, msg.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if err = socket.Deliver(streamName, address, decoded.Payload); err != nil {
		t.Fatal(err)
	}
	if err = msg.Ack(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSpotUserSocket(t *testing.T) {
	nc, brokerStream := spotUserBroker(t)
	socket := &SpotUser{}
	mux := http.NewServeMux()
	mux.Handle("/v1/ws", socket)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	connect := func(t *testing.T) *websocket.Conn {
		t.Helper()
		conn, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(httpServer.URL, "http")+"/v1/ws", nil)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusSwitchingProtocols {
			t.Fatalf("upgrade status = %d", response.StatusCode)
		}
		t.Cleanup(func() { _ = conn.Close() })
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"hello","service":"websocket","streams":["trade","position","pnl","order","closed-leg"],"protocolVersion":1}`)
		return conn
	}
	subscribe := func(t *testing.T, conn *websocket.Conn, streamName, address string) {
		t.Helper()
		spotUserSend(t, conn, "subscribe", streamName, address)
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), spotUserControlWant("subscribed", streamName, strings.ToLower(address)))
	}

	t.Run("hello", func(t *testing.T) { connect(t) })
	t.Run("subscribe_unsubscribe", func(t *testing.T) {
		conn := connect(t)
		subscribe(t, conn, "trade", strings.ToUpper(spotUserTestAddress))
		subscribe(t, conn, "trade", spotUserTestAddress)
		spotUserSend(t, conn, "unsubscribe", "trade", strings.ToUpper(spotUserTestAddress))
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), spotUserControlWant("unsubscribed", "trade", spotUserTestAddress))
		if err := socket.Deliver("trade", spotUserTestAddress, json.RawMessage(`{"id":"removed"}`)); err != nil {
			t.Fatal(err)
		}
		spotUserSend(t, conn, "ping", "", "")
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"pong"}`)
		subscribe(t, conn, "trade", spotUserTestAddress)
		if err := socket.Deliver("trade", spotUserTestAddress, json.RawMessage(`{"id":"restored"}`)); err != nil {
			t.Fatal(err)
		}
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"stream":"trade","address":"`+spotUserTestAddress+`","data":{"id":"restored"}}`)
	})
	t.Run("ping", func(t *testing.T) {
		conn := connect(t)
		spotUserSend(t, conn, "ping", "", "")
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"pong"}`)
		start := time.Now()
		var pingAt time.Time
		conn.SetPingHandler(func(data string) error {
			pingAt = time.Now()
			if err := conn.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(time.Second)); err != nil {
				return err
			}
			return conn.WriteJSON(map[string]string{"op": "ping"})
		})
		spotUserExpect(t, spotUserRead(t, conn, 35*time.Second), `{"type":"pong"}`)
		elapsed := pingAt.Sub(start)
		if elapsed < 29*time.Second || elapsed > 35*time.Second {
			t.Fatalf("server ping after %s, want 30 seconds", elapsed)
		}
	})

	items := []struct {
		stream, subtest, family, kind, entity, item string
	}{
		{"trade", "stream_trade", "spot", "swap", spotUserTestToken,
			`{"id":"1","txHash":"0x` + strings.Repeat("ab", 32) + `","logIndex":0,"blockNumber":"1","blockTimestamp":"1","user":"` + spotUserTestAddress + `","tokenIn":"` + spotUserTestToken + `","tokenOut":"0x00000000000000000000000000000000000000c1","amountIn":"9007199254740993123456789","amountOut":"2000000","executionPrice":"2","volumeUsd":"1000000","source":"market_order","contract":"0x0000000000000000000000000000000000001021","createdAt":"1","feeAmount":"1","feeBps":1,"hops":1,"priceImpactAmount":null,"priceImpactBps":null,"routeAdapterId":null,"twapPrice":null}`},
		{"position", "stream_position", "user", "position", spotUserTestAddress,
			`{"token":"` + spotUserTestToken + `","symbol":"TOK","name":"Token","decimals":6,"isStablecoin":false,"derived":{"qty":"2000000","avgEntryPrice":"2","avgExitPrice":null,"totalBought":"2000000","totalSold":"0","totalCostBasis":"1000000","totalProceeds":"0","realizedPnl":"0","realizedPnlUsd":"0","tradeCount":1,"firstTradeAt":"1","lastTradeAt":"1"},"paxscan":{"qty":"2000000"},"externalActivity":false,"externalQtyDelta":"0"}`},
		{"pnl", "stream_pnl", "user", "ledger", spotUserTestAddress,
			`{"address":"` + spotUserTestAddress + `","realizedUsd":"-1000000","unrealizedUsd":"2000000","totalUsd":"1000000","tradeCount":2,"closedLegCount":1}`},
		{"order", "stream_order", "spot", "order", spotUserTestToken,
			`{"id":"1","orderId":"1","type":"limit","status":"open","token":"` + spotUserTestToken + `","stablecoin":"0x00000000000000000000000000000000000000c1","amount":"9007199254740993123456789","targetPrice":"2","stopPrice":null,"limitPrice":"2","triggerPrice":null,"executionPrice":null,"isBuy":true,"placedAt":"1","executedAt":null,"cancelledAt":null,"placedTxHash":"0x` + strings.Repeat("ab", 32) + `","executedTxHash":null,"symbol":"TOK","decimals":6}`},
		{"closed-leg", "stream_closed_leg", "user", "fill", spotUserTestAddress,
			`{"id":1,"token":"` + spotUserTestToken + `","qty":"2000000","entryPrice":"2","exitPrice":"3","pnl":"1000000","pnlUsd":"1000000","pnlPct":"50","holdingSeconds":"1","openedAt":"1","closedAt":"2","closeTxHash":"0x` + strings.Repeat("ab", 32) + `","fullyClosed":true,"symbol":"TOK","name":"Token","decimals":6}`},
	}
	for _, item := range items {
		t.Run(item.subtest, func(t *testing.T) {
			conn := connect(t)
			subscribe(t, conn, item.stream, strings.ToUpper(spotUserTestAddress))
			subject, err := stream.Subject(125, stream.Family(item.family), item.entity, item.kind)
			if err != nil {
				t.Fatal(err)
			}
			spotUserBrokerItem(t, nc, brokerStream, socket, item.stream, subject, strings.ToUpper(spotUserTestAddress), json.RawMessage(item.item))
			spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"stream":"`+item.stream+`","address":"`+spotUserTestAddress+`","data":`+item.item+`}`)
			spotUserSend(t, conn, "ping", "", "")
			spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"pong"}`)
		})
	}
	t.Run("subscription_cap", func(t *testing.T) {
		conn := connect(t)
		for i := 1; i <= 50; i++ {
			subscribe(t, conn, "trade", fmt.Sprintf("0x%040x", i))
		}
		subscribe(t, conn, "trade", fmt.Sprintf("0x%040x", 1))
		address := fmt.Sprintf("0x%040x", 51)
		spotUserSend(t, conn, "subscribe", "trade", address)
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"error","message":"too_many_subscriptions"}`)
		if err := socket.Deliver("trade", address, json.RawMessage(`{"id":"refused"}`)); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 50; i++ {
			address := fmt.Sprintf("0x%040x", i)
			item := fmt.Sprintf(`{"id":"%d"}`, i)
			if err := socket.Deliver("trade", address, json.RawMessage(item)); err != nil {
				t.Fatal(err)
			}
			spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"stream":"trade","address":"`+address+`","data":`+item+`}`)
		}
		removed := fmt.Sprintf("0x%040x", 1)
		spotUserSend(t, conn, "unsubscribe", "trade", removed)
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), spotUserControlWant("unsubscribed", "trade", removed))
		subscribe(t, conn, "trade", address)
	})
	t.Run("routing_isolation", func(t *testing.T) {
		conn := connect(t)
		other := connect(t)
		subscribe(t, conn, "trade", spotUserTestAddress)
		subscribe(t, other, "position", spotUserTestAddress)
		subscribe(t, other, "trade", spotUserTestToken)
		if err := socket.Deliver("trade", spotUserTestAddress, json.RawMessage(items[0].item)); err != nil {
			t.Fatal(err)
		}
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"stream":"trade","address":"`+spotUserTestAddress+`","data":`+items[0].item+`}`)
		spotUserSend(t, other, "ping", "", "")
		spotUserExpect(t, spotUserRead(t, other, 5*time.Second), `{"type":"pong"}`)
	})
	t.Run("invalid_frames", func(t *testing.T) {
		conn := connect(t)
		for _, test := range []struct{ raw, code string }{
			{`{`, "invalid_json"},
			{`[]`, "invalid_json"},
			{`{"op":"unknown"}`, "unknown_event"},
			{`{"op":"subscribe","stream":"unknown","address":"` + spotUserTestAddress + `"}`, "unknown_stream"},
			{`{"op":"subscribe","stream":"trade","address":"bad"}`, "invalid_address"},
			{`{"op":"subscribe","stream":"trade","address":"0x` + strings.Repeat("z", 40) + `"}`, "invalid_address"},
		} {
			if err := conn.WriteMessage(websocket.TextMessage, []byte(test.raw)); err != nil {
				t.Fatal(err)
			}
			spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"error","message":"`+test.code+`"}`)
		}
		spotUserSend(t, conn, "ping", "", "")
		spotUserExpect(t, spotUserRead(t, conn, 5*time.Second), `{"type":"pong"}`)
	})
	t.Run("invalid_items", func(t *testing.T) {
		for _, test := range []struct{ stream, address, data, message string }{
			{"unknown", spotUserTestAddress, `{}`, "ws: unknown spot stream"},
			{"trade", "invalid", `{}`, "ws: invalid spot address"},
			{"trade", spotUserTestAddress, `null`, "ws: spot item must be a bounded JSON object"},
			{"trade", spotUserTestAddress, `[]`, "ws: spot item must be a bounded JSON object"},
			{"trade", spotUserTestAddress, `{`, "ws: spot item must be a bounded JSON object"},
			{"trade", spotUserTestAddress, `{"data":"` + strings.Repeat("x", delivery.MaxEnvelopeBytes) + `"}`, "ws: spot item must be a bounded JSON object"},
		} {
			err := socket.Deliver(test.stream, test.address, json.RawMessage(test.data))
			if err == nil || err.Error() != test.message {
				t.Fatalf("delivery error = %v, want %q", err, test.message)
			}
		}
	})
}
