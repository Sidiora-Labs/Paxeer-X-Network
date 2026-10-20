package perpsdata

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gorilla/websocket"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/explorer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/gateway"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/indexer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/portal"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/stream"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/ws"
)

const (
	e2eDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	e2eRPCEnv      = "PERPS_DATA_TEST_RPC"
	e2eCometEnv    = "PERPS_DATA_TEST_COMET"
	e2eSeedKeyEnv  = "PERPS_DATA_TEST_SEED_KEY_FILE"
	e2eSecret      = "end-to-end-test-secret-of-at-least-32-bytes"
	e2eSymbol      = "PAX"
	e2eMarket      = 11
	// perpTopic is the guarded trade topic of acceptance 7.12.20 in place of
	// BTC@trade: the gateway serves spot topics for PAX and SID only, and
	// only once the indexer has seen the activation registry.
	perpTopic   = e2eSymbol + "@perp_trade"
	scanWindow  = 500
	catchUp     = 6 * time.Minute
	waitShort   = 2 * time.Minute
	frameWait   = 90 * time.Second
	receiptWait = time.Minute
)

var (
	perpsPrecompile = common.HexToAddress("0x1015")
	pecorPrecompile = common.HexToAddress("0x1021")
	vaultPrecompile = common.HexToAddress("0x1025")
	replayLine      = regexp.MustCompile(`^(\S+) rows=(\d+) changed=(\d+)$`)
	erc20JSON       = `[{"type":"function","name":"approve","stateMutability":"nonpayable","inputs":[{"type":"address","name":"spender"},{"type":"uint256","name":"amount"}],"outputs":[{"type":"bool","name":""}]},
{"type":"function","name":"transfer","stateMutability":"nonpayable","inputs":[{"type":"address","name":"to"},{"type":"uint256","name":"amount"}],"outputs":[{"type":"bool","name":""}]}]`
)

// e2e is the in-process data plane of one test: Postgres schema perps, an
// in-process JetStream holding PERPS_V1, the loopback chain and its clients.
type e2e struct {
	dbURL, rpcURL, cometURL string
	db                      *sql.DB
	client                  *ethclient.Client
	nc                      *nats.Conn
	js                      jetstream.JetStream
	chain                   uint64
	chainID                 *big.Int
}

// orderParams is the PlaceOrderParams tuple of 0x1015 placeOrder.
type orderParams struct {
	MarketId         *big.Int
	IsLong           bool
	OrderType        uint8
	TimeInForce      uint8
	SizeUsd          *big.Int
	Leverage         *big.Int
	LimitPrice       *big.Int
	TriggerPrice     *big.Int
	AcceptablePrice  *big.Int
	CollateralAmount *big.Int
	ReduceOnly       bool
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
	Deadline         uint64
	Cloid            [16]byte
}

type sent struct {
	hash  common.Hash
	block uint64
}

func TestEndToEnd(t *testing.T) {
	if os.Getenv(e2eDatabaseEnv) == "" && os.Getenv(e2eRPCEnv) == "" && os.Getenv(e2eCometEnv) == "" {
		t.Skip("TestEndToEnd needs " + e2eDatabaseEnv + ", " + e2eRPCEnv + " and " + e2eCometEnv)
	}
	t.Run("module_builds", testModuleBuilds)

	e := setupE2E(t)
	ctx := context.Background()
	opKey := seedKey(t)
	op := crypto.PubkeyToAddress(opKey.PublicKey)
	bKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b := crypto.PubkeyToAddress(bKey.PublicKey)
	cKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	c := crypto.PubkeyToAddress(cKey.PublicKey)

	head, err := e.client.BlockNumber(ctx)
	if err != nil {
		t.Fatalf("head: %v", err)
	}
	// The position projections hold only what the indexer saw, so it follows
	// the chain from its first block, past at least one hourly settlement.
	e.latestFunding(t, head)
	start := uint64(1)
	ix := e.startIndexer(t, "e2e-a", start)
	e.waitIndexed(t, ix, head, catchUp)

	explorerURL := e.serveExplorer(t)
	gwAddr := freeAddr(t)
	gwURL := "http://" + gwAddr
	gwA := e.startGateway(t, gwAddr)

	connA, welcomeA := dialData(t, gwURL)
	readerA := readFrames(connA)
	guard := sequenceGuard()
	densityA := map[string]uint64{}
	subscribe(t, connA, perpTopic)
	expectSnapshots(t, readerA, guard, densityA, welcomeA, perpTopic)

	perps := mustABI(t, perpsabi.ABI)
	pecor := mustABI(t, spotabi.PECOR)
	vault := mustABI(t, spotabi.Vault)
	erc20, err := abi.JSON(strings.NewReader(erc20JSON))
	if err != nil {
		t.Fatal(err)
	}
	usdl := store.USDL
	pax := callAddress(t, e.client, pecor, pecorPrecompile, "WETH")
	price := markPrice(t, e.client, perps)
	usd := func(n int64) *big.Int { return big.NewInt(n * 1_000_000) }
	e18 := func(n int64) *big.Int { return new(big.Int).Mul(big.NewInt(n), big.NewInt(1e18)) }
	zero := big.NewInt(0)
	order := func(long bool) orderParams {
		return orderParams{big.NewInt(e2eMarket), long, 0, 0, e18(25), e18(2), price, zero, zero, zero, false, zero, zero, 0, [16]byte{}}
	}

	withdraw := e.send(t, opKey, perpsPrecompile, nil, perps, "withdraw", usdl, usd(120))
	e.send(t, opKey, usdl, nil, erc20, "approve", perpsPrecompile, usd(20))
	depositOp := e.send(t, opKey, perpsPrecompile, nil, perps, "deposit", usdl, usd(20))
	// Self-trade prevention cancels a resting maker of the taker, so two
	// fresh accounts trade with each other.
	deposits := map[common.Address]sent{}
	for _, k := range []*ecdsa.PrivateKey{bKey, cKey} {
		a := crypto.PubkeyToAddress(k.PublicKey)
		e.send(t, opKey, a, e18(1), abi.ABI{}, "")
		e.send(t, opKey, usdl, nil, erc20, "transfer", a, usd(40))
		e.send(t, k, usdl, nil, erc20, "approve", perpsPrecompile, usd(40))
		deposits[a] = e.send(t, k, perpsPrecompile, nil, perps, "deposit", usdl, usd(40))
	}
	maker := e.send(t, bKey, perpsPrecompile, nil, perps, "placeOrder", order(true))
	fill := e.send(t, cKey, perpsPrecompile, nil, perps, "placeOrder", order(false))
	nativeDeposit := e.send(t, opKey, vaultPrecompile, e18(30), vault, "depositNative")
	e.send(t, opKey, usdl, nil, erc20, "approve", pecorPrecompile, usd(12))

	nextLive(t, readerA, guard, densityA, perpTopic)

	var welcomeB string
	var readerB <-chan frameOrErr
	densityB := map[string]uint64{}
	t.Run("restart_new_snapshot_id", func(t *testing.T) {
		// The process stops: its server, its hub and every socket it held.
		gwA.stop(t)
		if err := connA.Close(); err != nil {
			t.Fatalf("close socket: %v", err)
		}
		for range readerA {
		}
		e.startGateway(t, gwAddr)
		var connB *websocket.Conn
		connB, welcomeB = dialData(t, gwURL)
		if welcomeB == welcomeA {
			t.Fatalf("welcome snapshotId %q after the restart, want a new one", welcomeB)
		}
		readerB = readFrames(connB)
		subscribe(t, connB, perpTopic)
		expectSnapshots(t, readerB, guard, densityB, welcomeB, perpTopic)
	})
	if readerB == nil {
		t.FailNow()
	}

	maker2 := e.send(t, bKey, perpsPrecompile, nil, perps, "placeOrder", order(true))
	fill2 := e.send(t, cKey, perpsPrecompile, nil, perps, "placeOrder", order(false))
	buy := e.send(t, opKey, pecorPrecompile, nil, pecor, "marketBuy", usdl, pax, usd(12), zero, big.NewInt(time.Now().Add(time.Hour).Unix()))
	withdrawLast := e.send(t, opKey, perpsPrecompile, nil, perps, "withdraw", usdl, usd(5))
	t.Run("websocket_dense_seq_after_restart", func(t *testing.T) {
		f := nextLive(t, readerB, guard, densityB, perpTopic)
		if f.SnapshotID != welcomeB || f.Seq != 1 {
			t.Fatalf("first live %s frame snapshotId %q seq %d, want %q seq 1", perpTopic, f.SnapshotID, f.Seq, welcomeB)
		}
	})

	e.waitIndexed(t, ix, withdrawLast.block, waitShort)
	first := withdraw.block

	t.Run("postgres_rows", func(t *testing.T) {
		for _, c := range []struct {
			table, kind string
			tx          sent
			account     common.Address
		}{
			{"collateral_events", "withdrawn", withdraw, op},
			{"collateral_events", "deposited", depositOp, op},
			{"collateral_events", "deposited", deposits[b], b},
			{"collateral_events", "deposited", deposits[c], c},
			{"order_events", "placed", maker, b},
			{"order_events", "options", maker, b},
			{"order_events", "placed", fill, c},
			{"order_events", "executed", fill, c},
			{"fills", "fill", fill, common.Address{}},
			{"order_events", "placed", maker2, b},
			{"fills", "fill", fill2, common.Address{}},
			{"spot_vault_events", "native_deposit", nativeDeposit, common.Address{}},
			{"spot_swaps", "market_order", buy, common.Address{}},
			{"collateral_events", "withdrawn", withdrawLast, op},
		} {
			q := `SELECT count(*) FROM perps.` + c.table + ` WHERE kind = $1 AND tx_hash = $2 AND block = $3
  AND receipt_kind = 'tx' AND tx_index >= 0`
			args := []any{c.kind, c.tx.hash.Bytes(), int64(c.tx.block)}
			if c.account != (common.Address{}) {
				q += ` AND account = $4`
				args = append(args, c.account.Bytes())
			}
			if n := e.count(t, q, args...); n == 0 {
				t.Errorf("no perps.%s %s row for tx %s in block %d", c.table, c.kind, c.tx.hash.Hex(), c.tx.block)
			}
		}
		for _, table := range []string{"funding_settlements", "funding_rates"} {
			rows := e.endBlockRows(t, table)
			if len(rows) == 0 {
				t.Errorf("no perps.%s row with receipt_kind perps_endblock", table)
			}
			for block, hash := range rows {
				if want := source.SyntheticHash("perps-endblock", block); hash != want {
					t.Errorf("perps.%s block %d tx_hash %s, want the synthetic receipt %s", table, block, hash.Hex(), want.Hex())
				}
			}
		}
		if n := e.count(t, `SELECT count(*) FROM perps.index_prices WHERE kind = 'aggregate' AND market_id = $1::numeric
  AND receipt_kind = 'block_event' AND tx_index = -1 AND tx_hash = block_hash AND block BETWEEN $2 AND $3`,
			int64(e2eMarket), int64(first), int64(withdrawLast.block)); n == 0 {
			t.Errorf("no perps.index_prices aggregate row of market %d between blocks %d and %d", e2eMarket, first, withdrawLast.block)
		}
	})

	t.Run("jetstream_perps_seq", func(t *testing.T) {
		msgs := e.streamMessages(t)
		if len(msgs) == 0 {
			t.Fatal("PERPS_V1 holds no message")
		}
		ids := map[string]bool{}
		for i, m := range msgs {
			if i > 0 && m.perpsSeq <= msgs[i-1].perpsSeq {
				t.Fatalf("stream sequence %d Perps-Seq %d after %d", m.seq, m.perpsSeq, msgs[i-1].perpsSeq)
			}
			if ids[m.msgID] {
				t.Fatalf("stream sequence %d repeats Nats-Msg-Id %q", m.seq, m.msgID)
			}
			ids[m.msgID] = true
		}
		if last := msgs[len(msgs)-1].block; last < withdrawLast.block {
			t.Fatalf("last PERPS_V1 message is of block %d, want at least %d", last, withdrawLast.block)
		}
	})

	t.Run("rest_graphql", func(t *testing.T) {
		var markets []map[string]any
		getJSON(t, gwURL+"/api/markets", &markets)
		if len(markets) == 0 {
			t.Error("/api/markets is empty")
		}
		var one map[string]any
		getJSON(t, gwURL+"/api/markets/"+e2eSymbol, &one)
		var trades struct {
			Symbol string           `json:"symbol"`
			Count  int              `json:"count"`
			Trades []map[string]any `json:"trades"`
		}
		for _, route := range []string{"perp_trades"} {
			getJSON(t, gwURL+"/api/"+strings.ToLower(e2eSymbol)+"/"+route+"?limit=10", &trades)
			if trades.Symbol != e2eSymbol || trades.Count == 0 || len(trades.Trades) != trades.Count {
				t.Errorf("/api/%s/%s = symbol %q count %d with %d trades", strings.ToLower(e2eSymbol), route, trades.Symbol, trades.Count, len(trades.Trades))
			}
		}
		var gql struct {
			VaultEvents []struct {
				EventType string `json:"eventType"`
				TxHash    string `json:"txHash"`
			} `json:"vaultEvents"`
			FundingRates []struct {
				BlockNumber int64  `json:"blockNumber"`
				TxHash      string `json:"txHash"`
			} `json:"fundingRates"`
			PriceHistory []struct {
				BlockNumber int64 `json:"blockNumber"`
			} `json:"priceHistory"`
			TradeSettlements []map[string]any `json:"tradeSettlements"`
			IndexerStatus    struct {
				LastIndexedBlock int64 `json:"lastIndexedBlock"`
			} `json:"indexerStatus"`
		}
		graphQL(t, gwURL, `query($u: String!, $b: String!) {
  vaultEvents(userAddress: $u, eventType: "deposited", limit: 50) { eventType txHash }
  fundingRates(marketId: 11, limit: 5) { blockNumber txHash }
  priceHistory(marketId: 11, limit: 5) { blockNumber }
  tradeSettlements(userAddress: $b, marketId: 11) { id }
  indexerStatus { lastIndexedBlock }
}`, map[string]any{"u": strings.ToLower(op.Hex()), "b": strings.ToLower(c.Hex())}, &gql)
		found := false
		for _, v := range gql.VaultEvents {
			found = found || strings.EqualFold(v.TxHash, depositOp.hash.Hex())
		}
		if !found {
			t.Errorf("vaultEvents of %s lacks deposit %s: %+v", op.Hex(), depositOp.hash.Hex(), gql.VaultEvents)
		}
		if len(gql.FundingRates) == 0 {
			t.Error("fundingRates(11) is empty")
		}
		for _, f := range gql.FundingRates {
			if want := source.SyntheticHash("perps-endblock", uint64(f.BlockNumber)); !strings.EqualFold(f.TxHash, want.Hex()) {
				t.Errorf("fundingRates block %d txHash %s, want %s", f.BlockNumber, f.TxHash, want.Hex())
			}
		}
		if len(gql.PriceHistory) == 0 {
			t.Error("priceHistory(11) is empty")
		}
		if len(gql.TradeSettlements) == 0 {
			t.Errorf("tradeSettlements of %s on market 11 is empty", c.Hex())
		}
		if gql.IndexerStatus.LastIndexedBlock < int64(withdrawLast.block) {
			t.Errorf("indexerStatus lastIndexedBlock %d, want at least %d", gql.IndexerStatus.LastIndexedBlock, withdrawLast.block)
		}
	})

	t.Run("explorer_portal", func(t *testing.T) {
		var history struct {
			Items []explorer.HistoryItem `json:"items"`
		}
		getJSON(t, explorerURL+"/api/chain/accounts/"+strings.ToLower(op.Hex())+"/history", &history)
		var link string
		for _, it := range history.Items {
			if strings.EqualFold(it.Hash, depositOp.hash.Hex()) {
				link = it.Link
			}
		}
		if link == "" {
			t.Fatalf("explorer history of %s lacks deposit %s with a link", op.Hex(), depositOp.hash.Hex())
		}
		var tx struct {
			Hash         string            `json:"hash"`
			Transactions []json.RawMessage `json:"transactions"`
		}
		getJSON(t, link, &tx)
		if !strings.EqualFold(tx.Hash, depositOp.hash.Hex()) || len(tx.Transactions) == 0 {
			t.Fatalf("explorer link %s answered hash %q with %d transactions", link, tx.Hash, len(tx.Transactions))
		}
		portalURL := servePortal(t, gwURL, explorerURL+"/api/chain/tx")
		page := get(t, portalURL+"/accounts/"+strings.ToLower(op.Hex()))
		href := explorerURL + "/api/chain/tx/" + strings.ToLower(depositOp.hash.Hex())
		if !strings.Contains(page, `href="`+href+`"`) {
			t.Fatalf("portal account page lacks the explorer link %s", href)
		}
		get(t, href)
	})

	t.Run("replay_identical", func(t *testing.T) {
		ix.stop(t)
		cursor, ok, err := store.Cursor(ctx, e.db)
		if err != nil || !ok {
			t.Fatalf("cursor %v %v", ok, err)
		}
		before := e.dumpTables(t)
		msgsBefore := e.streamMessages(t)
		bin := buildReplay(t)
		funding := e.firstEndBlockFunding(t)
		for _, args := range [][]string{
			{"--from", strconv.FormatUint(funding, 10), "--to", strconv.FormatUint(funding, 10)},
			{"--from", strconv.FormatUint(first, 10)},
		} {
			e.runReplay(t, bin, args...)
		}
		after := e.dumpTables(t)
		for table, sum := range before {
			if after[table] != sum {
				t.Errorf("replay changed perps.%s", table)
			}
		}
		if len(after) != len(before) {
			t.Errorf("replay changed the table set: %d tables, want %d", len(after), len(before))
		}
		ix2 := e.startIndexer(t, "e2e-b", start)
		e.waitIndexed(t, ix2, cursor+2, waitShort)
		ix2.stop(t)
		msgsAfter := e.streamMessages(t)
		count := func(msgs []streamMsg) int {
			n := 0
			for _, m := range msgs {
				if m.block <= cursor {
					n++
				}
			}
			return n
		}
		if got, want := count(msgsAfter), count(msgsBefore); got != want {
			t.Fatalf("PERPS_V1 holds %d messages through block %d after the replay, want %d", got, cursor, want)
		}
		ids := map[string]bool{}
		for i, m := range msgsAfter {
			if ids[m.msgID] {
				t.Fatalf("stream sequence %d repeats Nats-Msg-Id %q after the replay", m.seq, m.msgID)
			}
			ids[m.msgID] = true
			if i > 0 && m.perpsSeq <= msgsAfter[i-1].perpsSeq {
				t.Fatalf("stream sequence %d Perps-Seq %d after %d", m.seq, m.perpsSeq, msgsAfter[i-1].perpsSeq)
			}
		}
	})
}

// testModuleBuilds is acceptance 7.1.1: the tree builds and vets as packages
// of the root module, with no second module under it.
func testModuleBuilds(t *testing.T) {
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		cmd := exec.Command(gobin, args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	out, err := exec.Command(gobin, "list", "-m").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	if mods := strings.Fields(string(out)); len(mods) != 1 || mods[0] != "github.com/Sidiora-Labs/Paxeer-X-Network" {
		t.Fatalf("go list -m = %q, want the root module only", mods)
	}
	err = filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "go.mod" {
			t.Errorf("second module at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func setupE2E(t *testing.T) *e2e {
	t.Helper()
	ctx := context.Background()
	e := &e2e{dbURL: mustEnv(t, e2eDatabaseEnv), rpcURL: mustEnv(t, e2eRPCEnv), cometURL: mustEnv(t, e2eCometEnv)}
	db, err := store.Open(e.dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	e.db = db
	if e.client, err = ethclient.DialContext(ctx, e.rpcURL); err != nil {
		t.Fatalf("dial %s: %v", e2eRPCEnv, err)
	}
	t.Cleanup(e.client.Close)
	if e.chainID, err = e.client.ChainID(ctx); err != nil || !e.chainID.IsUint64() || e.chainID.Sign() == 0 {
		t.Fatalf("chain id %v: %v", e.chainID, err)
	}
	e.chain = e.chainID.Uint64()
	ns, err := server.NewServer(&server.Options{Host: "127.0.0.1", Port: server.RANDOM_PORT, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true})
	if err != nil {
		t.Fatalf("nats server: %v", err)
	}
	go ns.Start()
	t.Cleanup(ns.Shutdown)
	if !ns.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	if e.nc, err = nats.Connect(ns.ClientURL()); err != nil {
		t.Fatalf("nats connect: %v", err)
	}
	t.Cleanup(e.nc.Close)
	if e.js, err = jetstream.New(e.nc); err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	// One in-process server cannot hold three replicas; every other setting
	// is the PERPS_V1 configuration.
	cfg := stream.Config(e.chain)
	cfg.Replicas = 1
	if _, err := e.js.CreateStream(ctx, cfg); err != nil {
		t.Fatalf("create stream: %v", err)
	}
	return e
}

func mustEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatal("TestEndToEnd requires " + name)
	}
	return v
}

func seedKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	raw, err := os.ReadFile(mustEnv(t, e2eSeedKeyEnv))
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(strings.TrimSpace(string(raw)), "0x"))
	if err != nil {
		t.Fatalf("%s: %v", e2eSeedKeyEnv, err)
	}
	return key
}

func mustABI(t *testing.T, load func() (abi.ABI, error)) abi.ABI {
	t.Helper()
	a, err := load()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func moduleAddress(name string) common.Address {
	sum := sha256.Sum256([]byte(name))
	return common.BytesToAddress(sum[:common.AddressLength])
}

// runningIndexer is one indexer process: Run in a goroutine until stop.
type runningIndexer struct {
	cancel context.CancelFunc
	done   chan error
}

func (e *e2e) startIndexer(t *testing.T, holder string, start uint64) *runningIndexer {
	t.Helper()
	ctx := context.Background()
	metrics, err := telemetry.New(config.Indexer)
	if err != nil {
		t.Fatalf("telemetry: %v", err)
	}
	rpc, err := source.NewRPC(ctx, source.RPCOptions{URLs: []string{e.rpcURL}})
	if err != nil {
		t.Fatalf("NewRPC: %v", err)
	}
	t.Cleanup(rpc.Close)
	registry, err := decode.NewRegistry(metrics, decode.Options{PerpsModule: moduleAddress("perps"), SpotModule: moduleAddress("spot"),
		PricefeedModule: moduleAddress("pricefeed"), State: rpc})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	comet, err := source.NewComet([]string{e.cometURL}, 2*time.Second)
	if err != nil {
		t.Fatalf("NewComet: %v", err)
	}
	outbox, err := delivery.NewStore(e.db, "perps", store.Policy(e.chain))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	publisher, err := publish.New(e.db, e.nc, store.Policy(e.chain), publish.Config{
		Owner: holder, Batch: 100, MaxAttempts: 20, LeaseDuration: 30 * time.Second,
		PublishTimeout: 5 * time.Second, RetryDelay: time.Second, IdleInterval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("publish.New: %v", err)
	}
	ix, err := indexer.New(ctx, indexer.Deps{DB: e.db, RPC: rpc, Comet: comet, Outbox: outbox, ChainID: e.chain, Registry: registry,
		Publisher: publisher, Metrics: metrics, Holder: holder, StartHeight: int64(start)})
	if err != nil {
		t.Fatalf("indexer.New: %v", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	r := &runningIndexer{cancel: cancel, done: make(chan error, 1)}
	go func() { r.done <- ix.Run(runCtx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-r.done:
		case <-time.After(30 * time.Second):
		}
	})
	return r
}

// stop cancels the indexer and checks that Run returned the cancellation,
// which releases its writer lease.
func (r *runningIndexer) stop(t *testing.T) {
	t.Helper()
	r.cancel()
	select {
	case err := <-r.done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("indexer Run = %v, want context.Canceled", err)
		}
		r.done <- err
	case <-time.After(30 * time.Second):
		t.Fatal("indexer did not stop")
	}
}

// waitIndexed waits until the cursor reached height and every outbox row is
// published.
func (e *e2e) waitIndexed(t *testing.T, r *runningIndexer, height uint64, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for {
		h, ok, err := store.Cursor(context.Background(), e.db)
		if err != nil {
			t.Fatalf("Cursor: %v", err)
		}
		if ok && h >= height && e.count(t, `SELECT count(*) FROM perps.delivery_outbox WHERE state <> 'published'`) == 0 {
			return
		}
		select {
		case err := <-r.done:
			r.done <- err
			t.Fatalf("indexer Run returned before height %d: %v", height, err)
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatalf("indexer cursor %d after %s, want %d", h, limit, height)
		}
	}
}

func (e *e2e) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// endBlockRows returns block -> tx_hash of the perps_endblock rows of table.
func (e *e2e) endBlockRows(t *testing.T, table string) map[uint64]common.Hash {
	t.Helper()
	rows, err := e.db.Query(`SELECT block, tx_hash FROM perps.` + table + ` WHERE receipt_kind = 'perps_endblock' AND tx_index = -1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[uint64]common.Hash{}
	for rows.Next() {
		var block int64
		var hash []byte
		if err := rows.Scan(&block, &hash); err != nil {
			t.Fatal(err)
		}
		out[uint64(block)] = common.BytesToHash(hash)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// latestFunding is the height of the newest FundingSettled log of 0x1015 on
// the node at or below head.
func (e *e2e) latestFunding(t *testing.T, head uint64) uint64 {
	t.Helper()
	perps := mustABI(t, perpsabi.ABI)
	topic := perps.Events["FundingSettled"].ID
	for to := head; ; to -= scanWindow {
		from := uint64(1)
		if to > scanWindow {
			from = to - scanWindow + 1
		}
		logs, err := e.client.FilterLogs(context.Background(), ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(from),
			ToBlock: new(big.Int).SetUint64(to), Addresses: []common.Address{perpsPrecompile}, Topics: [][]common.Hash{{topic}}})
		if err != nil {
			t.Fatalf("FundingSettled logs %d-%d: %v", from, to, err)
		}
		if len(logs) > 0 {
			return logs[len(logs)-1].BlockNumber
		}
		if from == 1 {
			t.Fatal("the node holds no FundingSettled log")
		}
	}
}

func (e *e2e) firstEndBlockFunding(t *testing.T) uint64 {
	t.Helper()
	var block int64
	if err := e.db.QueryRow(`SELECT min(block) FROM perps.funding_settlements WHERE receipt_kind = 'perps_endblock'`).Scan(&block); err != nil {
		t.Fatalf("funding settlement block: %v", err)
	}
	return uint64(block)
}

// markPrice is the fresh mark of the market on the node rounded down to its
// 1e13 tick.
func markPrice(t *testing.T, c *ethclient.Client, perps abi.ABI) *big.Int {
	t.Helper()
	data, err := perps.Pack("getMarkPriceDetails", big.NewInt(e2eMarket))
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.CallContract(context.Background(), ethereum.CallMsg{To: &perpsPrecompile, Data: data}, nil)
	if err != nil {
		t.Fatalf("getMarkPriceDetails: %v", err)
	}
	v, err := perps.Unpack("getMarkPriceDetails", out)
	if err != nil || len(v) != 4 {
		t.Fatalf("getMarkPriceDetails: %v", err)
	}
	p, ok := v[0].(*big.Int)
	if fresh, _ := v[3].(bool); !ok || p.Sign() <= 0 || !fresh {
		t.Fatalf("market %d mark %v fresh %v", e2eMarket, v[0], v[3])
	}
	tick := big.NewInt(1e13)
	return new(big.Int).Mul(new(big.Int).Div(p, tick), tick)
}

func callAddress(t *testing.T, c *ethclient.Client, a abi.ABI, to common.Address, method string) common.Address {
	t.Helper()
	data, err := a.Pack(method)
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.CallContract(context.Background(), ethereum.CallMsg{To: &to, Data: data}, nil)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	v, err := a.Unpack(method, out)
	if err != nil || len(v) != 1 {
		t.Fatalf("%s: %v", method, err)
	}
	addr, ok := v[0].(common.Address)
	if !ok {
		t.Fatalf("%s returned %T", method, v[0])
	}
	return addr
}

// send signs and sends one call and waits for its successful receipt. An
// empty method sends value only.
func (e *e2e) send(t *testing.T, key *ecdsa.PrivateKey, to common.Address, value *big.Int, a abi.ABI, method string, args ...any) sent {
	t.Helper()
	ctx := context.Background()
	from := crypto.PubkeyToAddress(key.PublicKey)
	var data []byte
	if method != "" {
		var err error
		if data, err = a.Pack(method, args...); err != nil {
			t.Fatalf("pack %s: %v", method, err)
		}
	}
	// The node answers pending reads only through its sequencer.
	nonce, err := e.client.NonceAt(ctx, from, nil)
	if err != nil {
		t.Fatalf("nonce: %v", err)
	}
	gasPrice, err := e.client.SuggestGasPrice(ctx)
	if err != nil {
		t.Fatalf("gas price: %v", err)
	}
	gas, err := e.client.EstimateGas(ctx, ethereum.CallMsg{From: from, To: &to, GasPrice: gasPrice, Value: value, Data: data})
	if err != nil {
		t.Fatalf("estimate %s: %v", method, err)
	}
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: nonce, GasPrice: gasPrice, Gas: gas * 2, To: &to, Value: value, Data: data}),
		ethtypes.LatestSignerForChainID(e.chainID), key)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.client.SendTransaction(ctx, tx); err != nil {
		t.Fatalf("send %s: %v", method, err)
	}
	deadline := time.Now().Add(receiptWait)
	for {
		r, err := e.client.TransactionReceipt(ctx, tx.Hash())
		if err == nil {
			if r.Status != ethtypes.ReceiptStatusSuccessful {
				t.Fatalf("%s %s reverted in block %d", method, tx.Hash().Hex(), r.BlockNumber)
			}
			return sent{hash: tx.Hash(), block: r.BlockNumber.Uint64()}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no receipt for %s %s: %v", method, tx.Hash().Hex(), err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// serve runs h on a new loopback listener and returns its base URL.
func serve(t *testing.T, h http.Handler) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.Serve(l) }()
	t.Cleanup(func() { _ = s.Close() })
	return "http://" + l.Addr().String()
}

// lateHandler serves through the handler set after its URL is known.
type lateHandler struct{ h http.Handler }

func (l *lateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) { l.h.ServeHTTP(w, r) }

// serveExplorer runs the explorer with its links pointing at its own routes.
func (e *e2e) serveExplorer(t *testing.T) string {
	t.Helper()
	metrics, err := telemetry.New(config.Explorer)
	if err != nil {
		t.Fatal(err)
	}
	late := &lateHandler{}
	base := serve(t, late)
	srv, err := explorer.NewServer(e.db, explorer.Options{LinkBase: base + "/api/chain", Requests: 120, Window: time.Minute, Metrics: metrics})
	if err != nil {
		t.Fatalf("explorer.NewServer: %v", err)
	}
	late.h = srv
	return base
}

func servePortal(t *testing.T, gatewayURL, explorerTxBase string) string {
	t.Helper()
	client := &http.Client{Timeout: 15 * time.Second}
	pages, err := portal.NewPages(gatewayURL, explorerTxBase, client)
	if err != nil {
		t.Fatalf("portal.NewPages: %v", err)
	}
	metrics, err := telemetry.New(config.Portal)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := portal.NewServer(pages, gatewayURL, metrics, client)
	if err != nil {
		t.Fatalf("portal.NewServer: %v", err)
	}
	return serve(t, srv)
}

// runningGateway is one gateway process serving at a fixed address.
type runningGateway struct {
	http   *http.Server
	cancel context.CancelFunc
	done   chan error
}

func (e *e2e) startGateway(t *testing.T, addr string) *runningGateway {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	metrics, err := telemetry.New(config.Gateway)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := gateway.NewServer(ctx, gateway.Options{DB: e.db, NATS: e.nc, Chain: e.chain, AuthSecret: e2eSecret, Node: e.client, Metrics: metrics})
	if err != nil {
		cancel()
		t.Fatalf("gateway.NewServer: %v", err)
	}
	var l net.Listener
	for deadline := time.Now().Add(10 * time.Second); ; {
		if l, err = net.Listen("tcp", addr); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("listen %s: %v", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	g := &runningGateway{http: &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}, cancel: cancel, done: make(chan error, 1)}
	go func() { _ = g.http.Serve(l) }()
	go func() { g.done <- srv.Run(ctx) }()
	t.Cleanup(func() {
		_ = g.http.Close()
		cancel()
	})
	deadline := time.Now().Add(waitShort)
	for !srv.Ready() {
		if time.Now().After(deadline) {
			t.Fatal("gateway not ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return g
}

func (g *runningGateway) stop(t *testing.T) {
	t.Helper()
	if err := g.http.Close(); err != nil {
		t.Fatalf("close gateway server: %v", err)
	}
	g.cancel()
	select {
	case err := <-g.done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("gateway Run = %v, want context.Canceled", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("gateway did not stop")
	}
}

func dialData(t *testing.T, baseURL string) (*websocket.Conn, string) {
	t.Helper()
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := d.Dial("ws"+strings.TrimPrefix(baseURL, "http")+"/ws", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var w struct {
		Event      string `json:"event"`
		SnapshotID string `json:"snapshotId"`
		TS         *int64 `json:"ts"`
	}
	if err := conn.ReadJSON(&w); err != nil {
		t.Fatalf("welcome: %v", err)
	}
	if w.Event != "welcome" || w.SnapshotID == "" || w.TS == nil {
		t.Fatalf("first frame %+v, want a welcome with snapshotId and ts", w)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	return conn, w.SnapshotID
}

func subscribe(t *testing.T, conn *websocket.Conn, topics ...string) {
	t.Helper()
	for _, topic := range topics {
		if err := conn.WriteJSON(ws.ClientFrame{Event: ws.EventSubscribe, Topic: topic}); err != nil {
			t.Fatalf("subscribe %s: %v", topic, err)
		}
	}
}

type frameOrErr struct {
	frame ws.DataFrame
	err   error
}

// readFrames reads the data frames of conn until it fails.
func readFrames(conn *websocket.Conn) <-chan frameOrErr {
	out := make(chan frameOrErr, 256)
	go func() {
		defer close(out)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				out <- frameOrErr{err: err}
				return
			}
			var probe map[string]json.RawMessage
			if err := json.Unmarshal(raw, &probe); err != nil {
				out <- frameOrErr{err: err}
				return
			}
			if _, ok := probe["data"]; !ok {
				if ev, ok := probe["event"]; ok && strings.Contains(string(ev), "error") {
					out <- frameOrErr{err: errors.New(string(raw))}
					return
				}
				continue
			}
			var f ws.DataFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				out <- frameOrErr{err: err}
				return
			}
			out <- frameOrErr{frame: f}
		}
	}()
	return out
}

// next returns the next data frame, checked by the client sequence guard and
// for a dense seq on its topic.
func next(t *testing.T, frames <-chan frameOrErr, guard func(ws.DataFrame) bool, last map[string]uint64) ws.DataFrame {
	t.Helper()
	select {
	case f, ok := <-frames:
		if !ok || f.err != nil {
			t.Fatalf("socket: %v", f.err)
		}
		if !guard(f.frame) {
			t.Fatalf("sequence guard reported a gap: %s seq %d snapshotId %q", f.frame.Topic, f.frame.Seq, f.frame.SnapshotID)
		}
		if f.frame.Snapshot {
			last[f.frame.Topic] = f.frame.Seq
		} else if prev, ok := last[f.frame.Topic]; !ok || f.frame.Seq != prev+1 {
			t.Fatalf("%s live seq %d after %d (snapshot seen %v)", f.frame.Topic, f.frame.Seq, prev, ok)
		} else {
			last[f.frame.Topic] = f.frame.Seq
		}
		return f.frame
	case <-time.After(frameWait):
		t.Fatalf("no frame in %s", frameWait)
	}
	return ws.DataFrame{}
}

func expectSnapshots(t *testing.T, frames <-chan frameOrErr, guard func(ws.DataFrame) bool, last map[string]uint64, snapshotID string, topics ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, topic := range topics {
		want[topic] = true
	}
	for len(want) > 0 {
		f := next(t, frames, guard, last)
		if !f.Snapshot {
			continue
		}
		if !want[f.Topic] || f.SnapshotID != snapshotID {
			t.Fatalf("snapshot of %s with snapshotId %q, want one of %v with %q", f.Topic, f.SnapshotID, want, snapshotID)
		}
		delete(want, f.Topic)
	}
}

func nextLive(t *testing.T, frames <-chan frameOrErr, guard func(ws.DataFrame) bool, last map[string]uint64, topic string) ws.DataFrame {
	t.Helper()
	for {
		if f := next(t, frames, guard, last); !f.Snapshot && f.Topic == topic {
			return f
		}
	}
}

// sequenceGuard is the client gap guard of loadtest: it refuses a live frame
// of another snapshotId or beyond the next seq, and a snapshot never rolls
// the baseline of the same snapshotId backwards.
func sequenceGuard() func(ws.DataFrame) bool {
	type mark struct {
		seq   uint64
		epoch string
	}
	topics := map[string]mark{}
	return func(f ws.DataFrame) bool {
		old, ok := topics[f.Topic]
		if f.Snapshot || !ok {
			if !ok || old.epoch != f.SnapshotID || f.Seq >= old.seq {
				topics[f.Topic] = mark{f.Seq, f.SnapshotID}
			}
			return true
		}
		if old.epoch != f.SnapshotID || f.Seq > old.seq+1 {
			return false
		}
		if f.Seq <= old.seq {
			return true
		}
		topics[f.Topic] = mark{f.Seq, f.SnapshotID}
		return true
	}
}

func get(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d: %s", url, resp.StatusCode, raw)
	}
	return string(raw)
}

func getJSON(t *testing.T, url string, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(get(t, url)), v); err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
}

func graphQL(t *testing.T, baseURL, query string, vars map[string]any, v any) {
	t.Helper()
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(baseURL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data   json.RawMessage   `json:"data"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || len(out.Errors) > 0 {
		t.Fatalf("graphql status %d errors %s", resp.StatusCode, out.Errors)
	}
	if err := json.Unmarshal(out.Data, v); err != nil {
		t.Fatalf("graphql data %s: %v", out.Data, err)
	}
}

type streamMsg struct {
	seq      uint64
	perpsSeq int64
	block    uint64
	msgID    string
}

func (e *e2e) streamMessages(t *testing.T) []streamMsg {
	t.Helper()
	ctx := context.Background()
	s, err := e.js.Stream(ctx, stream.Name)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatalf("stream info: %v", err)
	}
	var out []streamMsg
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq && info.State.Msgs > 0; seq++ {
		raw, err := s.GetMsg(ctx, seq)
		if err != nil {
			t.Fatalf("get message %d: %v", seq, err)
		}
		m := streamMsg{seq: raw.Sequence, msgID: raw.Header.Get(nats.MsgIdHdr)}
		if m.perpsSeq, err = strconv.ParseInt(raw.Header.Get(publish.HeaderSeq), 10, 64); err != nil {
			t.Fatalf("message %d Perps-Seq: %v", seq, err)
		}
		if m.block, err = strconv.ParseUint(raw.Header.Get(publish.HeaderBlock), 10, 64); err != nil {
			t.Fatalf("message %d Perps-Block: %v", seq, err)
		}
		if m.msgID == "" {
			t.Fatalf("message %d without Nats-Msg-Id", seq)
		}
		out = append(out, m)
	}
	return out
}

// dumpTables digests every table of schema perps but the cursor, the writer
// lease and the delivery tables.
func (e *e2e) dumpTables(t *testing.T) map[string]string {
	t.Helper()
	rows, err := e.db.Query(`SELECT table_name FROM information_schema.tables WHERE table_schema = 'perps' AND table_type = 'BASE TABLE'
  AND table_name NOT IN ('cursors', 'writer_lease', 'delivery_outbox', 'delivery_inbox', 'delivery_quarantine') ORDER BY table_name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, name := range tables {
		var sum sql.NullString
		if err := e.db.QueryRow(`SELECT md5(string_agg(t::text, '|' ORDER BY t::text)) FROM perps.` + name + ` t`).Scan(&sum); err != nil {
			t.Fatalf("digest perps.%s: %v", name, err)
		}
		out[name] = sum.String
	}
	return out
}

func buildReplay(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "perps-replay")
	cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-o", bin, "./cmd/perps-replay")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build perps-replay: %v\n%s", err, out)
	}
	return bin
}

// runReplay runs perps-replay over args and requires that it rewrote every
// row unchanged and added no outbox message.
func (e *e2e) runReplay(t *testing.T, bin string, args ...string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "PERPS_CHAIN_ID="+strconv.FormatUint(e.chain, 10), "PERPS_RPC_URLS="+e.rpcURL,
		"PERPS_COMET_URLS="+e.cometURL, "DATABASE_URL="+e.dbURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("perps-replay %v: %v\n%s", args, err, stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "replayed heights ") {
		t.Fatalf("perps-replay %v printed %q", args, stdout.String())
	}
	for _, line := range lines[1:] {
		m := replayLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("perps-replay %v line %q", args, line)
		}
		if m[3] != "0" {
			t.Errorf("perps-replay %v: %s", args, line)
		}
		if m[1] == "perps.delivery_outbox" && m[2] != "0" {
			t.Errorf("perps-replay %v added outbox messages: %s", args, line)
		}
	}
}
