package app_test

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/app"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/constants"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// The development v2 stream test boots app.New with NODE_ENV=development over
// real Postgres and Redis (external test servers), an in-process go-ethereum node
// served over HTTP JSON-RPC, and an httptest server speaking the gateway's
// identity introspection protocol (/internal/v2/identity/ticket:consume and
// session:check, service bearer, strict JSON) that the real
// sharedauth.HTTPStreamAuthority calls. Pools, swaps and state rows are
// written the way the development indexer writes them, for real transactions
// whose Swap logs carry the indexed amounts.

const (
	dstOrigin       = "https://web.example"
	dstServiceToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	dstPool         = "0x7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a7a"
	dstCreator      = "0xcececececececececececececececececececece"
	dstTrader       = "0x1111111111111111111111111111111111111111"
)

var (
	dstSwapTopic          = crypto.Keccak256Hash([]byte("Swap(bytes32,address,bool,uint256,uint256,uint256,uint256,uint256,uint256)"))
	dstMarketCreatedTopic = crypto.Keccak256Hash([]byte("MarketCreated(bytes32,address,address,address,address,uint256,uint256)"))
	dstPoolID             = common.HexToHash("0x" + strings.Repeat("7a", 32))
)

// dstChain is the in-process chain: an emitter contract whose runtime logs
// LOG3(calldata[0:32], calldata[32:64], calldata[64:96]) over calldata[96:],
// and a token whose runtime answers 18 to decimals().
type dstChain struct {
	backend *simulated.Backend
	url     string
	key     *ecdsa.PrivateKey
	nonce   uint64
	emitter common.Address
	token   common.Address
}

func newDstChain(t *testing.T) *dstChain {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	funds := new(big.Int).Exp(big.NewInt(10), big.NewInt(21), nil)
	backend := simulated.NewBackend(types.GenesisAlloc{crypto.PubkeyToAddress(key.PublicKey): {Balance: funds}}, func(n *node.Config, _ *ethconfig.Config) {
		n.HTTPHost = "127.0.0.1"
		n.HTTPPort = port
		n.HTTPModules = []string{"eth"}
	})
	t.Cleanup(func() { _ = backend.Close() })
	backend.Commit()
	c := &dstChain{backend: backend, url: "http://127.0.0.1:" + strconv.Itoa(port), key: key}
	emitterRuntime := []byte{0x60, 0x60, 0x36, 0x03, 0x80, 0x60, 0x60, 0x60, 0x00, 0x37, 0x60, 0x40, 0x35, 0x60, 0x20, 0x35, 0x60, 0x00, 0x35, 0x83, 0x60, 0x00, 0xa3, 0x00}
	tokenRuntime := []byte{0x60, 0x12, 0x60, 0x00, 0x52, 0x60, 0x20, 0x60, 0x00, 0xf3}
	c.emitter = c.deploy(t, emitterRuntime)
	c.token = c.deploy(t, tokenRuntime)
	return c
}

func (c *dstChain) send(t *testing.T, to *common.Address, data []byte) *types.Receipt {
	t.Helper()
	ctx := context.Background()
	client := c.backend.Client()
	tip, err := client.SuggestGasTipCap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := client.HeaderByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	feeCap := new(big.Int).Add(tip, new(big.Int).Mul(head.BaseFee, big.NewInt(2)))
	tx := types.NewTx(&types.DynamicFeeTx{ChainID: big.NewInt(1337), Nonce: c.nonce, GasTipCap: tip, GasFeeCap: feeCap, Gas: 300000, To: to, Data: data})
	signed, err := types.SignTx(tx, types.LatestSignerForChainID(big.NewInt(1337)), c.key)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.SendTransaction(ctx, signed); err != nil {
		t.Fatal(err)
	}
	c.nonce++
	c.backend.Commit()
	receipt, err := client.TransactionReceipt(ctx, signed.Hash())
	if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatalf("receipt: %v %+v", err, receipt)
	}
	return receipt
}

func (c *dstChain) deploy(t *testing.T, runtime []byte) common.Address {
	t.Helper()
	n := byte(len(runtime))
	init := append([]byte{0x60, n, 0x60, 0x0c, 0x60, 0x00, 0x39, 0x60, n, 0x60, 0x00, 0xf3}, runtime...)
	receipt := c.send(t, nil, init)
	return receipt.ContractAddress
}

func word(v *big.Int) []byte { return common.LeftPadBytes(v.Bytes(), 32) }

func (c *dstChain) emit(t *testing.T, topics [3]common.Hash, data []byte) *types.Receipt {
	t.Helper()
	calldata := append(append(append([]byte{}, topics[0].Bytes()...), topics[1].Bytes()...), topics[2].Bytes()...)
	receipt := c.send(t, &c.emitter, append(calldata, data...))
	if len(receipt.Logs) != 1 {
		t.Fatalf("emitter logs: %+v", receipt.Logs)
	}
	return receipt
}

func (c *dstChain) blockTime(t *testing.T, receipt *types.Receipt) int64 {
	t.Helper()
	header, err := c.backend.Client().HeaderByNumber(context.Background(), receipt.BlockNumber)
	if err != nil {
		t.Fatal(err)
	}
	return int64(header.Time)
}

// dstGateway speaks the gateway's internal identity protocol for stream
// tickets: one-time 30 s tickets stored by sha256 digest and bound to the
// exact StreamTicketBinding, consumed under the service bearer.
type dstGateway struct {
	mu       sync.Mutex
	tickets  map[string]dstTicket
	sessions map[string]bool
	consumed int
	checked  int
}

type dstTicket struct {
	binding   []byte
	principal sharedauth.StreamPrincipal
	expires   time.Time
}

func (g *dstGateway) issue(t *testing.T, p sharedauth.StreamPrincipal, topics []string, origin string) string {
	t.Helper()
	sorted := append([]string(nil), topics...)
	sort.Strings(sorted)
	raw, err := json.Marshal(sharedauth.StreamTicketBinding{SchemaMajor: 2, Topics: sorted, FilterDigest: sharedauth.StreamFilterDigest(sorted), Origin: origin, Endpoint: "/v2/stream"})
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	ticket := hex.EncodeToString(secret)
	digest := sha256.Sum256([]byte(ticket))
	g.mu.Lock()
	g.tickets[hex.EncodeToString(digest[:])] = dstTicket{binding: raw, principal: p, expires: time.Now().Add(30 * time.Second)}
	g.sessions[p.SessionID] = true
	g.mu.Unlock()
	return ticket
}

func strictDecode(r *http.Request, v any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 16385))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func (g *dstGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer "+dstServiceToken {
		http.Error(w, `{"code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
		return
	}
	switch r.URL.Path {
	case "/internal/v2/identity/ticket:consume":
		var req struct {
			Ticket  string                         `json:"ticket"`
			Binding sharedauth.StreamTicketBinding `json:"binding"`
		}
		if strictDecode(r, &req) != nil {
			http.Error(w, `{"code":"FIELD_INVALID"}`, http.StatusBadRequest)
			return
		}
		sort.Strings(req.Binding.Topics)
		raw, _ := json.Marshal(req.Binding)
		digest := sha256.Sum256([]byte(req.Ticket))
		g.mu.Lock()
		ticket, ok := g.tickets[hex.EncodeToString(digest[:])]
		delete(g.tickets, hex.EncodeToString(digest[:]))
		g.consumed++
		g.mu.Unlock()
		if !ok || string(ticket.binding) != string(raw) || time.Now().After(ticket.expires) {
			http.Error(w, `{"code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ticket.principal)
	case "/internal/v2/identity/session:check":
		var p sharedauth.StreamPrincipal
		if strictDecode(r, &p) != nil {
			http.Error(w, `{"code":"FIELD_INVALID"}`, http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		active := g.sessions[p.SessionID]
		g.checked++
		g.mu.Unlock()
		if !active {
			http.Error(w, `{"code":"AUTH_REQUIRED"}`, http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	default:
		http.NotFound(w, r)
	}
}

// dstApplyIndexerMigration runs core/indexer's goose statements for the
// development indexer tables the internaltest read subset does not create.
func dstApplyIndexerMigration(t *testing.T, pool *pgxpool.Pool, tables ...string) {
	t.Helper()
	raw, err := os.ReadFile("../../../indexer/migrations/00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(raw), "-- +goose Down", 2)[0]
	applied := 0
	for _, block := range strings.Split(up, "-- +goose StatementBegin") {
		statement := strings.SplitN(block, "-- +goose StatementEnd", 2)[0]
		for _, table := range tables {
			if strings.Contains(statement, `CREATE TABLE "indexer"."`+table+`"`) {
				if _, err = pool.Exec(context.Background(), statement); err != nil {
					t.Fatalf("indexer migration %s: %v", table, err)
				}
				applied++
			}
		}
	}
	if applied != len(tables) {
		t.Fatalf("indexer migration applied %d of %v", applied, tables)
	}
}

type dstFixture struct {
	srv     *httptest.Server
	pool    *pgxpool.Pool
	pub     *goredis.Client
	chain   *dstChain
	gateway *dstGateway
	topics  []string
}

func newDstFixture(t *testing.T) dstFixture {
	t.Helper()
	dsn, pool := internaltest.NewPostgresWithDSN(t)
	redisURL := internaltest.NewRedisURL(t)
	dstApplyIndexerMigration(t, pool, "cursors", "swaps")
	chain := newDstChain(t)
	gateway := &dstGateway{tickets: map[string]dstTicket{}, sessions: map[string]bool{}}
	identity := httptest.NewServer(gateway)
	t.Cleanup(identity.Close)
	cfg := config.Config{
		DatabaseURL: dsn, RedisURL: redisURL, Port: 3000, LogLevel: "info", NodeEnv: "development",
		RateLimitMax: 1000, RateLimitWindowSec: 60, MaxInFlightRequests: 1000,
		WSMaxConnections: 1000, WSMaxPerIP: 100, SSEMaxConnections: 1000, SSEMaxPerIP: 100,
		ClientSendBuffer: 256, CoalesceFlushMS: 20,
		CORSAllowedOrigins: dstOrigin, NetworkRPCURL: chain.url,
		IdentityURL: identity.URL, IdentityServiceToken: dstServiceToken,
		StreamSigningKey: strings.Repeat("k", 32),
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, &cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		cancel()
		t.Fatalf("app.New: %v", err)
	}
	go func() { _ = a.Broker.Run(ctx) }()
	srv := httptest.NewServer(a.Router)
	opt, _ := goredis.ParseURL(redisURL)
	pub := goredis.NewClient(opt)
	t.Cleanup(func() {
		srv.Close()
		cancel()
		a.Close()
		_ = pub.Close()
	})
	topics := []string{"market.price." + dstPool, "market.stats." + dstPool, "market.trades." + dstPool}
	return dstFixture{srv: srv, pool: pool, pub: pub, chain: chain, gateway: gateway, topics: topics}
}

func (f dstFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// swap emits a real Swap log and indexes it the way core/indexer's
// development processor does (id = txHash-logIndex, sender = event sender).
func (f dstFixture) swap(t *testing.T, isBuy bool, amountIn, amountOut, fee int64) *types.Receipt {
	t.Helper()
	buy := big.NewInt(0)
	if isBuy {
		buy = big.NewInt(1)
	}
	data := []byte{}
	for _, v := range []*big.Int{buy, big.NewInt(amountIn), big.NewInt(amountOut), big.NewInt(fee), big.NewInt(1), big.NewInt(0), big.NewInt(0)} {
		data = append(data, word(v)...)
	}
	receipt := f.chain.emit(t, [3]common.Hash{dstSwapTopic, dstPoolID, common.BytesToHash(common.HexToAddress(dstTrader).Bytes())}, data)
	tx := strings.ToLower(receipt.TxHash.Hex())
	f.exec(t, `INSERT INTO indexer.swaps(id,pool_id,pool_address,sender,router,is_buy,amount_in,amount_out,fee,price,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,$3,$4,$4,$5,$6,$7,$8,'1',$9,$10,$11,$12)`,
		tx+"-"+strconv.Itoa(int(receipt.Logs[0].Index)), dstPoolID.Hex(), dstPool, dstTrader, isBuy, strconv.FormatInt(amountIn, 10), strconv.FormatInt(amountOut, 10), strconv.FormatInt(fee, 10), receipt.BlockNumber.Int64(), f.chain.blockTime(t, receipt), tx, int(receipt.Logs[0].Index))
	f.exec(t, `INSERT INTO indexer.pool_state_snapshots(id,pool_id,virtual_reserve,real_reserve,token_reserve,price,block_number,block_timestamp,tx_hash,log_index) VALUES($1,$2,'1000000000','5000000','800000000000000000000000000','1',$3,$4,$5,1)`,
		tx+"-1", dstPoolID.Hex(), receipt.BlockNumber.Int64(), f.chain.blockTime(t, receipt), tx)
	f.exec(t, `INSERT INTO stats.pool_stats(pool_address,token_address,price,price_change_24h,volume_24h,market_cap,buy_count_24h,sell_count_24h,holder_count,created_at,updated_at) VALUES($1,$2,'1','125','1000000','5000',1,0,2,1760000000,$3)
		ON CONFLICT(pool_address) DO UPDATE SET volume_24h=(stats.pool_stats.volume_24h::numeric+1)::text, sell_count_24h=stats.pool_stats.sell_count_24h+1, updated_at=EXCLUDED.updated_at`, dstPool, strings.ToLower(f.chain.token.Hex()), f.chain.blockTime(t, receipt))
	// The development indexer advances its cursor after the block's rows.
	f.exec(t, `INSERT INTO indexer.cursors(chain_id,last_processed_block,updated_at) VALUES(125,$1,now()) ON CONFLICT(chain_id) DO UPDATE SET last_processed_block=EXCLUDED.last_processed_block, updated_at=EXCLUDED.updated_at`, receipt.BlockNumber.Int64())
	if err := f.pub.Publish(context.Background(), constants.ChannelSwap, `{"eventName":"Swap"}`).Err(); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func (f dstFixture) createPool(t *testing.T) {
	t.Helper()
	receipt := f.chain.emit(t, [3]common.Hash{dstMarketCreatedTopic, dstPoolID, {}}, word(big.NewInt(1)))
	f.exec(t, `INSERT INTO indexer.pools(pool_address,token_address,creator,optical,pool_id,nft_id,created_at,created_block,tx_hash) VALUES($1,$2,$3,$4,$5,1,1760000000,$6,$7)`,
		dstPool, strings.ToLower(f.chain.token.Hex()), dstCreator, "0x0000000000000000000000000000000000000000", dstPoolID.Hex(), receipt.BlockNumber.Int64(), strings.ToLower(receipt.TxHash.Hex()))
}

func (f dstFixture) snapshot(t *testing.T, origin string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.srv.URL+"/v2/snapshots?topics="+url.QueryEscape(strings.Join(f.topics, ",")), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", origin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func (f dstFixture) dial(t *testing.T, origin string, protocols []string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	header.Set("Origin", origin)
	dialer := websocket.Dialer{Subprotocols: protocols, HandshakeTimeout: 5 * time.Second}
	return dialer.Dial("ws"+strings.TrimPrefix(f.srv.URL, "http")+"/v2/stream", header)
}

type dstTopicEntry struct {
	Topic     string          `json:"topic"`
	Sequence  string          `json:"sequence"`
	Revision  string          `json:"revision"`
	Watermark json.RawMessage `json:"watermark"`
	Payload   json.RawMessage `json:"payload"`
}

type dstSnapshot struct {
	SnapshotID   string          `json:"snapshotId"`
	ResumeCursor string          `json:"resumeCursor"`
	Topics       []dstTopicEntry `json:"topics"`
}

func readControl(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err = interfaces.ValidateJSON("StreamControl", raw); err != nil {
		t.Fatalf("not a StreamControl: %v %s", err, raw)
	}
	var control map[string]any
	_ = json.Unmarshal(raw, &control)
	return control
}

func TestDevelopmentStreamSnapshotWebSocketAndSSE(t *testing.T) {
	f := newDstFixture(t)
	f.createPool(t)
	first := f.swap(t, true, 1000000, 400000000000000000, 10000)

	// Snapshot: the projector appends the trade, price and stats topics from
	// the indexed rows; the public snapshot becomes a valid document.
	var status int
	var body []byte
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, body = f.snapshot(t, dstOrigin)
		if status == 200 || time.Now().After(deadline) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if status != 200 {
		t.Fatalf("snapshot = %d %s", status, body)
	}
	if err := interfaces.ValidateJSON("TopicSnapshotReadResponse", body); err != nil {
		t.Fatalf("snapshot document: %v\n%s", err, body)
	}
	var envelope struct {
		Data struct {
			Snapshot dstSnapshot `json:"snapshot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	snap := envelope.Data.Snapshot
	if len(snap.Topics) != 3 || snap.ResumeCursor == "" {
		t.Fatalf("snapshot topics %+v", snap)
	}
	firstHash := strings.ToLower(first.BlockHash.Hex())
	for _, entry := range snap.Topics {
		var wm map[string]any
		_ = json.Unmarshal(entry.Watermark, &wm)
		switch entry.Topic {
		case "market.trades." + dstPool:
			if err := interfaces.ValidateJSON("Trade", entry.Payload); err != nil {
				t.Fatalf("trade payload: %v", err)
			}
			var trade map[string]any
			_ = json.Unmarshal(entry.Payload, &trade)
			position := trade["position"].(map[string]any)
			if entry.Revision != "1" || wm["blockHash"] != firstHash || wm["height"] != first.BlockNumber.String() || wm["projectionRevision"] != "1" ||
				position["blockHash"] != firstHash || position["transactionIndex"] != strconv.FormatUint(uint64(first.TransactionIndex), 10) ||
				trade["side"] != "buy" || trade["usdlAmountRaw"] != "1000000" || trade["tokenAmountRaw"] != "400000000000000000" ||
				trade["emittingContract"] != strings.ToLower(f.chain.emitter.Hex()) || trade["feeDisposition"] != "distributed_usdl" ||
				trade["inputDerivationQuality"] != "exact" || trade["ammInputAfterFeeRaw"] != "990000" || trade["sender"] != dstTrader {
				t.Fatalf("trade entry %s %s %s", entry.Revision, entry.Watermark, entry.Payload)
			}
			code, err := f.chain.backend.Client().CodeAt(context.Background(), f.chain.emitter, nil)
			if err != nil || trade["codeEpoch"] != hex.EncodeToString(crypto.Keccak256(code)) {
				t.Fatalf("code epoch %v %v", trade["codeEpoch"], err)
			}
		case "market.price." + dstPool:
			if err := interfaces.ValidateJSON("Price", entry.Payload); err != nil {
				t.Fatalf("price payload: %v", err)
			}
			var price map[string]any
			_ = json.Unmarshal(entry.Payload, &price)
			// 1 USDL (1e6 raw) for 0.4 token (4e17 raw, 18 decimals) = 2.5 USDL per token.
			if price["numerator"] != "5" || price["denominator"] != "2" || entry.Revision != "1" {
				t.Fatalf("price entry %s", entry.Payload)
			}
		case "market.stats." + dstPool:
			if err := interfaces.ValidateJSON("PoolStats", entry.Payload); err != nil {
				t.Fatalf("stats payload: %v", err)
			}
			head, err := f.chain.backend.Client().HeaderByNumber(context.Background(), first.BlockNumber)
			if err != nil || wm["blockHash"] != strings.ToLower(head.Hash().Hex()) || entry.Revision != first.BlockNumber.String() {
				t.Fatalf("stats watermark %s revision %s", entry.Watermark, entry.Revision)
			}
		default:
			t.Fatalf("unexpected topic %s", entry.Topic)
		}
	}

	// Origin outside CORS_ALLOWED_ORIGINS: 403 on snapshot and stream.
	if status, body := f.snapshot(t, "https://evil.example"); status != 403 || interfaces.ValidateJSON("Error", body) != nil {
		t.Fatalf("foreign-origin snapshot = %d %s", status, body)
	}
	if _, resp, err := f.dial(t, "https://evil.example", []string{"sidiora.v2"}); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("foreign-origin stream: %v %v", err, resp)
	}
	// Malformed ticket subprotocol: 401 before the upgrade.
	if _, resp, err := f.dial(t, dstOrigin, []string{"sidiora.v2", "ticket.not-a-ticket"}); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("malformed ticket: %v %v", err, resp)
	}
	subscribe := func(conn *websocket.Conn) {
		raw, _ := json.Marshal(map[string]any{"op": "subscribe", "subscriptionId": "token-page", "topics": f.topics, "afterCursor": snap.ResumeCursor})
		if err := interfaces.ValidateJSON("Subscribe", raw); err != nil {
			t.Fatal(err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
	}
	// A well-formed ticket the identity authority does not know: AUTH_REQUIRED.
	unknown := strings.Repeat("ab", 32)
	conn, _, err := f.dial(t, dstOrigin, []string{"sidiora.v2", "ticket." + unknown})
	if err != nil {
		t.Fatal(err)
	}
	subscribe(conn)
	if c := readControl(t, conn); c["op"] != "error" || c["code"] != "AUTH_REQUIRED" {
		t.Fatalf("unknown ticket control %v", c)
	}
	_ = conn.Close()

	// A valid ticket, issued for the browser's exact binding.
	principal := sharedauth.StreamPrincipal{AccountID: uuid.NewString(), SessionID: uuid.NewString(), Wallet: dstTrader, AuthEpoch: 1}
	ticket := f.gateway.issue(t, principal, f.topics, dstOrigin)
	conn, resp, err := f.dial(t, dstOrigin, []string{"sidiora.v2", "ticket." + ticket})
	if err != nil {
		t.Fatalf("ticketed dial: %v %v", err, resp)
	}
	defer conn.Close()
	if conn.Subprotocol() != "sidiora.v2" {
		t.Fatalf("subprotocol %q", conn.Subprotocol())
	}
	subscribe(conn)
	if c := readControl(t, conn); c["op"] != "subscribed" || c["subscriptionId"] != "token-page" {
		t.Fatalf("subscribed control %v", c)
	}
	if c := readControl(t, conn); c["op"] != "checkpoint" || c["code"] != "REPLAY_COMPLETE" {
		t.Fatalf("replay checkpoint %v", c)
	}
	if err = conn.WriteMessage(websocket.TextMessage, []byte(`{"op":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if c := readControl(t, conn); c["op"] != "heartbeat" {
		t.Fatalf("heartbeat %v", c)
	}
	f.gateway.mu.Lock()
	consumed, checked := f.gateway.consumed, f.gateway.checked
	f.gateway.mu.Unlock()
	if consumed != 2 || checked < 1 {
		t.Fatalf("identity authority consumed=%d checked=%d", consumed, checked)
	}
	// Reusing the consumed ticket fails.
	again, _, err := f.dial(t, dstOrigin, []string{"sidiora.v2", "ticket." + ticket})
	if err != nil {
		t.Fatal(err)
	}
	subscribe(again)
	if c := readControl(t, again); c["op"] != "error" || c["code"] != "AUTH_REQUIRED" {
		t.Fatalf("replayed ticket control %v", c)
	}
	_ = again.Close()

	// A sell fill indexed after the snapshot arrives live as a contract
	// MarketTradesEvent with the next revision.
	second := f.swap(t, false, 200000000000000000, 480000, 2000000000000000)
	secondID := "125:" + strings.ToLower(second.TxHash.Hex()) + ":" + strconv.FormatUint(uint64(second.Logs[0].Index), 10)
	seen := map[string]bool{}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for !seen["trade"] {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("live read: %v (seen %v)", err, seen)
		}
		if interfaces.ValidateJSON("StreamControl", raw) == nil {
			continue
		}
		if err = interfaces.ValidateJSON("StreamEvent", raw); err != nil {
			t.Fatalf("live frame: %v %s", err, raw)
		}
		var frame struct {
			EventID   string         `json:"eventId"`
			Topic     string         `json:"topic"`
			Revision  string         `json:"revision"`
			Watermark map[string]any `json:"watermark"`
			Payload   map[string]any `json:"payload"`
		}
		_ = json.Unmarshal(raw, &frame)
		if frame.Topic == "market.trades."+dstPool {
			if frame.EventID != secondID || frame.Revision != "2" || frame.Payload["side"] != "sell" || frame.Payload["feeDisposition"] != "retained_token_reserve" ||
				frame.Payload["feeAssetAddress"] != strings.ToLower(f.chain.token.Hex()) || frame.Watermark["blockHash"] != strings.ToLower(second.BlockHash.Hex()) || frame.Watermark["projectionRevision"] != "2" {
				t.Fatalf("live trade %s", raw)
			}
			seen["trade"] = true
		}
	}

	// SSE fallback: resume from the same snapshot cursor replays the fill.
	req, err := http.NewRequest(http.MethodGet, f.srv.URL+"/v2/stream/sse?topics="+url.QueryEscape(strings.Join(f.topics, ",")), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", dstOrigin)
	req.Header.Set("Last-Event-ID", snap.ResumeCursor)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sse, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer sse.Body.Close()
	if sse.StatusCode != 200 || sse.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("sse = %d %s", sse.StatusCode, sse.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(sse.Body)
	var ops []string
	replayedTrade := false
	for !(replayedTrade && len(ops) > 0 && ops[len(ops)-1] == "checkpoint") {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("sse read: %v (ops %v)", err, ops)
		}
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := []byte(strings.TrimSpace(strings.TrimPrefix(line, "data: ")))
		if interfaces.ValidateJSON("StreamControl", raw) == nil {
			var c map[string]any
			_ = json.Unmarshal(raw, &c)
			ops = append(ops, c["op"].(string))
			continue
		}
		if err = interfaces.ValidateJSON("StreamEvent", raw); err != nil {
			t.Fatalf("sse frame: %v %s", err, raw)
		}
		if strings.Contains(string(raw), secondID) && strings.Contains(string(raw), `"topic":"market.trades.`) {
			replayedTrade = true
		}
	}
	if ops[0] != "subscribed" {
		t.Fatalf("sse controls %v", ops)
	}
	// SSE without a resume cursor asks for a snapshot.
	req2, _ := http.NewRequest(http.MethodGet, f.srv.URL+"/v2/stream/sse?topics="+url.QueryEscape(strings.Join(f.topics, ",")), nil)
	req2.Header.Set("Origin", "https://evil.example")
	if resp, err := http.DefaultClient.Do(req2); err != nil || resp.StatusCode != 403 {
		t.Fatalf("foreign-origin sse: %v %v", err, resp)
	}
}

// dstErrorBody is the contract Error document fields the snapshot error
// cases assert on.
type dstErrorBody struct {
	Code         string          `json:"code"`
	Retryable    bool            `json:"retryable"`
	RetryAfterMs json.RawMessage `json:"retryAfterMs"`
}

// snapshotError requests the fixture topics' snapshot and returns the status,
// the Retry-After header and the decoded Error body (validated against the
// contract Error schema).
func (f dstFixture) snapshotError(t *testing.T) (int, string, dstErrorBody) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, f.srv.URL+"/v2/snapshots?topics="+url.QueryEscape(strings.Join(f.topics, ",")), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", dstOrigin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if err = interfaces.ValidateJSON("Error", raw); err != nil {
		t.Fatalf("snapshot %d body is not a contract Error: %v %s", resp.StatusCode, err, raw)
	}
	var body dstErrorBody
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	t.Logf("GET /v2/snapshots -> %d Retry-After=%q %s", resp.StatusCode, resp.Header.Get("Retry-After"), strings.TrimSpace(string(raw)))
	return resp.StatusCode, resp.Header.Get("Retry-After"), body
}

// A pool absent from indexer.pools can never be projected: the snapshot
// answers 404 NOT_FOUND retryable=false with no Retry-After, identically on
// every call, and the legacy development surface is unchanged.
func TestDevelopmentStreamUnknownPoolAndLegacy(t *testing.T) {
	f := newDstFixture(t)
	for i := 0; i < 3; i++ {
		status, retryAfter, body := f.snapshotError(t)
		if status != 404 || body.Code != "NOT_FOUND" || body.Retryable || string(body.RetryAfterMs) != "null" || retryAfter != "" {
			t.Fatalf("unknown-pool snapshot call %d = %d %+v Retry-After=%q", i, status, body, retryAfter)
		}
	}
	header := http.Header{}
	header.Set("Origin", dstOrigin)
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.srv.URL, "http")+"/ws", header)
	if err != nil {
		t.Fatalf("legacy /ws: %v", err)
	}
	_ = conn.Close()
}

// A registered pool with no swap or stats row has no projected topic and the
// contract has no empty TopicSnapshotEntry (payload is required and non-null,
// topics has minItems 1), so the snapshot stays 503 DEPENDENCY_UNAVAILABLE
// retryable=true until the projector appends the first frames.
func TestDevelopmentStreamSnapshotKnownPoolWithoutData(t *testing.T) {
	f := newDstFixture(t)
	f.createPool(t)
	for i := 0; i < 2; i++ {
		status, _, body := f.snapshotError(t)
		if status != 503 || body.Code != "DEPENDENCY_UNAVAILABLE" || !body.Retryable {
			t.Fatalf("known-pool-without-data snapshot call %d = %d %+v", i, status, body)
		}
	}
}

// A registry read failure for an unprojected topic is a dependency failure,
// not a missing pool: 503 DEPENDENCY_UNAVAILABLE retryable=true.
func TestDevelopmentStreamSnapshotRegistryFailureStaysRetryable(t *testing.T) {
	f := newDstFixture(t)
	f.exec(t, `ALTER TABLE indexer.pools RENAME TO pools_unavailable`)
	status, _, body := f.snapshotError(t)
	if status != 503 || body.Code != "DEPENDENCY_UNAVAILABLE" || !body.Retryable {
		t.Fatalf("registry-failure snapshot = %d %+v", status, body)
	}
}

// Subscribing /v2/stream to an unregistered pool ends with error NOT_FOUND and
// a 1008 close frame (no subscribed / resnapshot_required loop).
func TestDevelopmentStreamUnknownPoolSubscribeCloses(t *testing.T) {
	f := newDstFixture(t)
	for i := 0; i < 2; i++ {
		conn, _, err := f.dial(t, dstOrigin, []string{"sidiora.v2"})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"op": "subscribe", "subscriptionId": "token-page", "topics": f.topics, "afterCursor": nil})
		if err = interfaces.ValidateJSON("Subscribe", raw); err != nil {
			t.Fatal(err)
		}
		if err = conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatal(err)
		}
		c := readControl(t, conn)
		if c["op"] != "error" || c["code"] != "NOT_FOUND" || c["subscriptionId"] != "token-page" {
			t.Fatalf("unknown-pool control %v", c)
		}
		closed := readClose(t, conn)
		t.Logf("WS /v2/stream unknown pool -> control %v, close %d %q", c, closed.Code, closed.Text)
		if closed.Code != websocket.ClosePolicyViolation || closed.Text != "NOT_FOUND" {
			t.Fatalf("close = %d %q, want 1008 NOT_FOUND", closed.Code, closed.Text)
		}
		_ = conn.Close()
	}
}
