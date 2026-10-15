package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

const (
	testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	testRPCEnv      = "PERPS_DATA_TEST_RPC"
	testChain       = 125
	testSecret      = "gateway-test-secret-of-at-least-32-bytes"
	testAccount     = "0x00000000000000000000000000000000000000a1"
	frameDeadline   = 15 * time.Second
	// unmountedBody is the answer of the ServeMux to a path no route matches.
	unmountedBody = "404 page not found\n"
)

func openDB(t *testing.T, url string) *sql.DB {
	t.Helper()
	db, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// startCluster runs three in-process JetStream servers, the replica count of
// the PERPS_V1 stream.
func startCluster(t *testing.T) *nats.Conn {
	t.Helper()
	routes := make([]string, 3)
	ports := make([]int, 3)
	for i := range ports {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ports[i] = l.Addr().(*net.TCPAddr).Port
		if err = l.Close(); err != nil {
			t.Fatal(err)
		}
		routes[i] = "nats://127.0.0.1:" + strconv.Itoa(ports[i])
	}
	servers := make([]*server.Server, 3)
	urls := make([]string, 3)
	for i := range servers {
		ns, err := server.NewServer(&server.Options{
			ServerName: "gateway-" + strconv.Itoa(i), Host: "127.0.0.1", Port: server.RANDOM_PORT,
			JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
			Cluster: server.ClusterOpts{Name: "gateway-test", Host: "127.0.0.1", Port: ports[i]},
			Routes:  server.RoutesFromStr(strings.Join(routes, ",")),
		})
		if err != nil {
			t.Fatal(err)
		}
		go ns.Start()
		t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
		servers[i] = ns
	}
	for i, ns := range servers {
		if !ns.ReadyForConnections(10 * time.Second) {
			t.Fatalf("server %d not ready", i)
		}
		urls[i] = ns.ClientURL()
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		formed := false
		for _, ns := range servers {
			if ns.JetStreamIsLeader() && len(ns.JetStreamClusterPeers()) == 3 {
				formed = true
			}
		}
		if formed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("JetStream cluster has no meta leader with three peers")
		}
		time.Sleep(50 * time.Millisecond)
	}
	nc, err := nats.Connect(strings.Join(urls, ","))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func newGateway(t *testing.T, ctx context.Context, db *sql.DB, nc *nats.Conn, node *ethclient.Client) (*Server, *httptest.Server) {
	t.Helper()
	metrics, err := telemetry.New(config.Gateway)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(ctx, Options{DB: db, NATS: nc, Chain: testChain, AuthSecret: testSecret, Node: node, Metrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func request(t *testing.T, ts *httptest.Server, method, path, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

func decodeObject(t *testing.T, raw []byte) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
	return out
}

func metricsText(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, raw := request(t, ts, http.MethodGet, "/metrics", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status %d", resp.StatusCode)
	}
	return string(raw)
}

func dial(t *testing.T, ts *httptest.Server, path string, subprotocols ...string) (*websocket.Conn, *http.Response) {
	t.Helper()
	d := websocket.Dialer{Subprotocols: subprotocols, HandshakeTimeout: 10 * time.Second}
	conn, resp, err := d.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, resp
}

func readFrame(t *testing.T, conn *websocket.Conn) map[string]json.RawMessage {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(frameDeadline)); err != nil {
		t.Fatal(err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return decodeObject(t, raw)
}

func text(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return s
}

// welcome reads the first frame of a data socket and returns its snapshotId.
func welcome(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	f := readFrame(t, conn)
	if got := text(t, f["event"]); got != "welcome" {
		t.Fatalf("first frame event %q, want welcome", got)
	}
	id := text(t, f["snapshotId"])
	if id == "" {
		t.Fatal("welcome without snapshotId")
	}
	if _, ok := f["ts"]; !ok {
		t.Fatal("welcome without ts")
	}
	return id
}

// snapshots reads frames until every topic has sent its snapshot frame and
// fails on a snapshot of another topic.
func snapshots(t *testing.T, conn *websocket.Conn, snapshotID string, topics ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, topic := range topics {
		want[topic] = true
	}
	for len(want) > 0 {
		f := readFrame(t, conn)
		if _, data := f["data"]; !data {
			continue
		}
		var snapshot bool
		if raw, ok := f["snapshot"]; ok {
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatal(err)
			}
		}
		topic := text(t, f["topic"])
		if !snapshot {
			continue
		}
		if !want[topic] {
			t.Fatalf("unexpected snapshot of %q, waiting for %v", topic, want)
		}
		if got := text(t, f["snapshotId"]); got != snapshotID {
			t.Fatalf("%s snapshotId %q, want %q", topic, got, snapshotID)
		}
		if _, ok := f["seq"]; !ok {
			t.Fatalf("%s snapshot without seq", topic)
		}
		delete(want, topic)
	}
}

func expectRetryAfter(t *testing.T, ts *httptest.Server, path string) {
	t.Helper()
	conn, _ := dial(t, ts, path)
	if err := conn.SetReadDeadline(time.Now().Add(frameDeadline)); err != nil {
		t.Fatal(err)
	}
	_, _, err := conn.ReadMessage()
	var closeErr *websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("%s: read error %v, want a close frame", path, err)
	}
	if closeErr.Code != websocket.CloseTryAgainLater {
		t.Fatalf("%s: close code %d, want 1013", path, closeErr.Code)
	}
	secs, ok := strings.CutPrefix(closeErr.Text, "retry-after=")
	n, err := strconv.Atoi(secs)
	if !ok || err != nil || n < 5 || n > 30 {
		t.Fatalf("%s: close reason %q, want retry-after=<5..30>", path, closeErr.Text)
	}
}

func TestGatewayServer(t *testing.T) {
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal(testDatabaseEnv + " is unset")
	}
	rpcURL := os.Getenv(testRPCEnv)
	if rpcURL == "" {
		t.Fatal(testRPCEnv + " is unset")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	db := openDB(t, url)
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	nc := startCluster(t)
	if err := publish.EnsureStream(ctx, nc, testChain); err != nil {
		t.Fatal(err)
	}
	node, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(node.Close)

	srv, ts := newGateway(t, ctx, db, nc, node)
	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()
	deadline := time.Now().Add(30 * time.Second)
	for !srv.Ready() {
		select {
		case err := <-runErr:
			t.Fatalf("gateway run stopped before ready: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("gateway not ready within 30 seconds")
		}
		time.Sleep(50 * time.Millisecond)
	}

	// idle is a second gateway that is never run, so it is never ready, over
	// its own database handle so that readiness_gate can close it.
	idleDB := openDB(t, url)
	idle, idleTS := newGateway(t, ctx, idleDB, nc, node)

	t.Run("rest_mounted", func(t *testing.T) {
		resp, raw := request(t, ts, http.MethodGet, "/api/markets", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/api/markets status %d: %s", resp.StatusCode, raw)
		}
		var markets []map[string]json.RawMessage
		if err := json.Unmarshal(decodeObject(t, raw)["markets"], &markets); err != nil {
			t.Fatal(err)
		}
		if len(markets) != 18 {
			t.Fatalf("/api/markets has %d records, want 18", len(markets))
		}
		resp, raw = request(t, ts, http.MethodGet, "/api/totals", "")
		if resp.StatusCode != http.StatusOK || decodeObject(t, raw)["totals"] == nil {
			t.Fatalf("/api/totals status %d: %s", resp.StatusCode, raw)
		}
		resp, raw = request(t, ts, http.MethodGet, "/api/btc/price", "")
		if resp.StatusCode != http.StatusOK || text(t, decodeObject(t, raw)["symbol"]) != "BTC" {
			t.Fatalf("/api/btc/price status %d: %s", resp.StatusCode, raw)
		}
		if got := resp.Header.Get("Cache-Control"); got != "max-age=1" {
			t.Fatalf("/api/btc/price Cache-Control %q, want max-age=1", got)
		}
		resp, raw = request(t, ts, http.MethodGet, "/api/markets/health", "")
		if resp.StatusCode != http.StatusOK || decodeObject(t, raw)["ok"] == nil {
			t.Fatalf("/api/markets/health status %d: %s", resp.StatusCode, raw)
		}
		resp, raw = request(t, ts, http.MethodGet, "/api/markets/btc", "")
		if resp.StatusCode != http.StatusOK || text(t, decodeObject(t, decodeObject(t, raw)["market"])["symbol"]) != "BTC" {
			t.Fatalf("/api/markets/btc status %d: %s", resp.StatusCode, raw)
		}
		resp, raw = request(t, ts, http.MethodGet, "/api/btc/config", "")
		if resp.StatusCode != http.StatusNotFound || string(raw) != `{"error":"Not found"}` {
			t.Fatalf("/api/btc/config status %d: %s, want the symbol route 404", resp.StatusCode, raw)
		}
		resp, _ = request(t, ts, http.MethodGet, "/healthz/live", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("/healthz/live status %d", resp.StatusCode)
		}
		m := metricsText(t, ts)
		for _, line := range []string{
			`perps_http_requests_total{route="GET /api/markets",status="200"} 1`,
			`perps_http_requests_total{route="GET /api/{symbol}/{route...}",status="404"} 1`,
			"# TYPE perps_gateway_sockets gauge",
			"# TYPE perps_gateway_subscriptions gauge",
		} {
			if !strings.Contains(m, line) {
				t.Fatalf("/metrics lacks %q", line)
			}
		}
	})

	t.Run("venue_account_routes_mounted", func(t *testing.T) {
		for _, path := range []string{
			"/api/orderbook/BTC", "/api/markets/orderbook/BTC",
			"/api/open-interest", "/api/markets/open-interest",
			"/api/ticker/BTC", "/api/markets/ticker/BTC",
			"/api/markets/tickers",
		} {
			resp, raw := request(t, ts, http.MethodGet, path, "")
			if resp.StatusCode != http.StatusOK || string(decodeObject(t, raw)["success"]) != "true" {
				t.Fatalf("%s status %d: %s", path, resp.StatusCode, raw)
			}
		}
		cases := []struct {
			method, path, body string
			status             int
			private            bool
		}{
			{http.MethodGet, "/v1/users/" + testAccount + "/orders", "", http.StatusOK, true},
			{http.MethodGet, "/v1/leaderboards/pnl", "", http.StatusOK, false},
			{http.MethodPost, "/v2/auth/challenge", `{"address":"` + testAccount + `"}`, http.StatusOK, true},
			{http.MethodPost, "/v2/auth/verify", `{"address":"` + testAccount + `","signature":"0x00","message":"m"}`, http.StatusBadRequest, true},
			{http.MethodPost, "/v2/auth/refresh", "", http.StatusUnauthorized, true},
			{http.MethodGet, "/v2/user/" + testAccount + "/daily-pnl", "", http.StatusOK, true},
			{http.MethodGet, "/v2/leaderboard", "", http.StatusOK, false},
			{http.MethodPost, "/v2/alerts", `{"alertType":"price"}`, http.StatusUnauthorized, true},
			{http.MethodDelete, "/v2/alerts/1", "", http.StatusUnauthorized, true},
			{http.MethodPost, "/v2/referrals/generate", "", http.StatusUnauthorized, true},
			{http.MethodPost, "/v2/referrals/apply", `{"code":"x"}`, http.StatusUnauthorized, true},
			{http.MethodPost, "/v2/competitions/c1/join", "", http.StatusUnauthorized, true},
		}
		for _, c := range cases {
			resp, raw := request(t, ts, c.method, c.path, c.body)
			if string(raw) == unmountedBody {
				t.Fatalf("%s %s is not mounted", c.method, c.path)
			}
			if resp.StatusCode != c.status {
				t.Fatalf("%s %s status %d, want %d: %s", c.method, c.path, resp.StatusCode, c.status, raw)
			}
			if got := resp.Header.Get("Cache-Control") == privateCache; got != c.private {
				t.Fatalf("%s %s Cache-Control %q, private %v", c.method, c.path, resp.Header.Get("Cache-Control"), c.private)
			}
		}
		resp, raw := request(t, ts, http.MethodGet, "/v1/leaderboards/bogus", "")
		if resp.StatusCode != http.StatusNotFound || string(raw) == unmountedBody {
			t.Fatalf("/v1/leaderboards/bogus status %d: %s, want the route's 404", resp.StatusCode, raw)
		}
		conn, _ := dial(t, ts, "/v1/ws")
		hello := readFrame(t, conn)
		if text(t, hello["type"]) != "hello" || text(t, hello["service"]) != "websocket" || string(hello["protocolVersion"]) != "1" {
			t.Fatalf("/v1/ws first frame %v", hello)
		}
	})

	t.Run("graphql_mounted", func(t *testing.T) {
		for _, path := range []string{"/graphql", "/api/subgraph"} {
			resp, raw := request(t, ts, http.MethodPost, path, `{"query":"query Probe { __typename }","operationName":"Probe"}`)
			if resp.StatusCode != http.StatusOK || string(raw) != `{"data":{"__typename":"Query"}}` {
				t.Fatalf("%s status %d: %s", path, resp.StatusCode, raw)
			}
		}
		resp, raw := request(t, ts, http.MethodPost, "/graphql", `{`)
		if resp.StatusCode != http.StatusOK || string(raw) != `{"errors":[{"message":"invalid request body"}]}` {
			t.Fatalf("malformed body status %d: %s", resp.StatusCode, raw)
		}
		resp, raw = request(t, ts, http.MethodPost, "/graphql", `{"query":"{ noSuchRoot }"}`)
		body := decodeObject(t, raw)
		if resp.StatusCode != http.StatusOK || body["errors"] == nil || body["data"] != nil {
			t.Fatalf("invalid query status %d: %s", resp.StatusCode, raw)
		}
		resp, _ = request(t, ts, http.MethodGet, "/graphql", "")
		if resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != http.MethodPost {
			t.Fatalf("GET /graphql without upgrade status %d", resp.StatusCode)
		}
		m := metricsText(t, ts)
		for _, line := range []string{
			`perps_gateway_graphql_duration_seconds_count{operation="Probe"} 2`,
			`perps_gateway_graphql_duration_seconds_count{operation="anonymous"} 1`,
		} {
			if !strings.Contains(m, line) {
				t.Fatalf("/metrics lacks %q", line)
			}
		}
	})

	t.Run("ws_mounted", func(t *testing.T) {
		var first string
		for _, path := range []string{"/ws", "/", "/ws/btc", "/ws/markets", "/ws/explorer"} {
			conn, _ := dial(t, ts, path)
			id := welcome(t, conn)
			if first == "" {
				first = id
			}
			if id != first {
				t.Fatalf("%s snapshotId %q, want the process id %q", path, id, first)
			}
		}
		d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
		_, resp, err := d.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/ws/nosuch", nil)
		if err == nil || resp == nil || resp.StatusCode != http.StatusNotFound {
			t.Fatalf("/ws/nosuch: %v, want 404 before the upgrade", err)
		}
	})

	t.Run("socket_paths_autosubscribe", func(t *testing.T) {
		for path, topics := range map[string][]string{
			"/ws/markets":  {"markets@all", "markets@totals"},
			"/ws/explorer": {"chain@blocks", "chain@transactions"},
			"/ws/btc":      {"BTC@perp_orderbook", "BTC@perp_trade", "BTC@perp_stats"},
		} {
			conn, _ := dial(t, ts, path)
			snapshots(t, conn, welcome(t, conn), topics...)
		}
	})

	t.Run("graphql_ws_mounted", func(t *testing.T) {
		for _, path := range []string{"/graphql-ws", "/graphql"} {
			conn, resp := dial(t, ts, path, "graphql-transport-ws")
			if got := resp.Header.Get("Sec-WebSocket-Protocol"); got != "graphql-transport-ws" {
				t.Fatalf("%s subprotocol %q", path, got)
			}
			for _, step := range []struct{ send, want string }{{"connection_init", "connection_ack"}, {"ping", "pong"}} {
				if err := conn.WriteJSON(map[string]string{"type": step.send}); err != nil {
					t.Fatal(err)
				}
				if got := text(t, readFrame(t, conn)["type"]); got != step.want {
					t.Fatalf("%s answered %s with %q, want %q", path, step.send, got, step.want)
				}
			}
		}
	})

	t.Run("readiness_gate", func(t *testing.T) {
		resp, raw := request(t, ts, http.MethodGet, "/healthz/ready", "")
		checks := decodeObject(t, decodeObject(t, raw)["checks"])
		if resp.StatusCode != http.StatusOK || text(t, checks["postgres"]) != "ok" || text(t, checks["consumer"]) != "ok" {
			t.Fatalf("running gateway readiness %d: %s", resp.StatusCode, raw)
		}
		resp, raw = request(t, idleTS, http.MethodGet, "/healthz/ready", "")
		checks = decodeObject(t, decodeObject(t, raw)["checks"])
		if resp.StatusCode != http.StatusServiceUnavailable || text(t, checks["postgres"]) != "ok" || text(t, checks["consumer"]) != "failed" {
			t.Fatalf("unattached consumer readiness %d: %s", resp.StatusCode, raw)
		}
		if idle.Ready() {
			t.Fatal("a gateway that never ran reports ready")
		}
		if err := idleDB.Close(); err != nil {
			t.Fatal(err)
		}
		resp, raw = request(t, idleTS, http.MethodGet, "/healthz/ready", "")
		checks = decodeObject(t, decodeObject(t, raw)["checks"])
		if resp.StatusCode != http.StatusServiceUnavailable || text(t, checks["postgres"]) != "failed" {
			t.Fatalf("closed database readiness %d: %s", resp.StatusCode, raw)
		}
	})

	t.Run("close_1013_when_not_ready", func(t *testing.T) {
		for _, path := range []string{"/ws", "/", "/ws/btc", "/ws/markets", "/ws/explorer", "/v1/ws"} {
			expectRetryAfter(t, idleTS, path)
		}
	})

	cancel()
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("gateway run: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("gateway run did not stop after cancel")
	}
	if srv.Ready() {
		t.Fatal("a stopped gateway reports ready")
	}
}
