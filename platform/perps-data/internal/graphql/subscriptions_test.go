package graphql

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/delivery"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
)

const (
	subChain       = 7
	subDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"
	subWait        = 10 * time.Second
	subQuiet       = 300 * time.Millisecond
)

var (
	subBase    = time.Unix(1_700_000_000, 0).UTC()
	subTrader  = common.HexToAddress("0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed")
	subOther   = common.HexToAddress("0x00000000000000000000000000000000000004b2")
	subKeeper  = common.HexToAddress("0x00000000000000000000000000000000000004b3")
	subEmitter = common.HexToAddress("0x0000000000000000000000000000000000001015")
	subToken   = common.HexToAddress("0x00000000000000000000000000000000000001a1")
	subTables  = []string{"mark_prices", "index_prices", "funding_rates", "trade_settlements", "liquidations", "fills", "order_events",
		"position_events", "positions_current", "orders", "blocks", "cursors"}
)

func subHash(block int64, txIndex, logIndex int) []byte {
	return []byte(fmt.Sprintf("%016d%08d%08d", block, txIndex+1, logIndex))
}

func subBlockHash(block int64) []byte { return subHash(block, -1, 0) }

func subHex(b []byte) string { return "0x" + hex.EncodeToString(b) }

func subAt(block int64) time.Time { return subBase.Add(time.Duration(block) * time.Second) }

func subStamp(block int64) string { return subAt(block).Format(time.RFC3339Nano) }

func subLower(a common.Address) string { return strings.ToLower(a.Hex()) }

func TestSubscriptions(t *testing.T) {
	url := os.Getenv(subDatabaseEnv)
	if url == "" {
		t.Fatal("TestSubscriptions requires " + subDatabaseEnv)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	db, err := store.Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close db: %v", err)
		}
	})
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	feed, err := NewFeed(subChain, nil)
	if err != nil {
		t.Fatalf("NewFeed: %v", err)
	}
	schema, err := NewSchema(db)
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	transport := &TransportWS{Schema: schema, Feed: feed}
	mux := http.NewServeMux()
	mux.Handle("/graphql-ws", transport)
	mux.Handle("/graphql", transport.Upgrade(http.NotFoundHandler()))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	reset := func(t *testing.T) {
		t.Helper()
		if _, err := db.Exec("TRUNCATE " + "perps." + strings.Join(subTables, ", perps.")); err != nil {
			t.Fatalf("truncate: %v", err)
		}
	}
	exec := func(t *testing.T, q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// fact inserts one fact row with the common columns of block, txIndex and
	// logIndex and returns its row_id.
	fact := func(t *testing.T, table, kind string, block int64, txIndex, logIndex int, account common.Address, cols []string, vals ...any) int64 {
		t.Helper()
		all := append([]string{"block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}, cols...)
		args := append([]any{block, subBlockHash(block), txIndex, logIndex, 0, subHash(block, txIndex, logIndex), "tx", subAt(block), subEmitter.Bytes(), kind,
			account.Bytes(), subTrader.Bytes()}, vals...)
		marks := make([]string, len(all))
		for i := range all {
			marks[i] = "$" + strconv.Itoa(i+1)
			all[i] = `"` + all[i] + `"`
		}
		var id int64
		q := fmt.Sprintf("INSERT INTO perps.%s (%s) VALUES (%s) RETURNING row_id", table, strings.Join(all, ", "), strings.Join(marks, ", "))
		if err := db.QueryRow(q, args...).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", table, err)
		}
		return id
	}
	head := func(t *testing.T, height int64) {
		t.Helper()
		exec(t, `INSERT INTO perps.cursors (name, height, hash) VALUES ('head', $1, $2) ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height, hash = EXCLUDED.hash`,
			height, subBlockHash(height))
	}
	block := func(t *testing.T, n int64) {
		t.Helper()
		exec(t, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, 1)`,
			n, subBlockHash(n), subBlockHash(n-1), subAt(n), strings.Repeat("A", 40))
	}
	markPrice := func(t *testing.T, market, block int64, logIndex int, index string) int64 {
		t.Helper()
		return fact(t, "mark_prices", "mark", block, 0, logIndex, subEmitter, []string{"market_id", "mark_price", "index_price", "mid_price"}, market, index, index, index)
	}
	indexPrice := func(t *testing.T, market, block int64, logIndex int, kind string, index any) int64 {
		t.Helper()
		return fact(t, "index_prices", kind, block, 0, logIndex, subEmitter, []string{"market_id", "index_price", "timestamp_ms", "sources", "required"},
			market, index, subAt(block).UnixMilli()+500, 3, 2)
	}
	settlement := func(t *testing.T, position, market, block int64, txIndex, logIndex int, tradeType int, size, price string) int64 {
		t.Helper()
		return fact(t, "trade_settlements", "settled", block, txIndex, logIndex, subTrader, []string{"position_id", "user", "market_id", "trade_type", "size_usd",
			"execution_price", "gross_pnl", "total_fees_usd", "funding_paid_usd", "net_payout_tokens"},
			position, subTrader.Bytes(), market, tradeType, size, price, "-15", "4", "1", "90")
	}
	publish := func(t *testing.T, subjects ...string) {
		t.Helper()
		for _, s := range subjects {
			feed.Deliver("perps.7."+s, delivery.Envelope{})
		}
	}
	listeners := func(t *testing.T, n int) {
		t.Helper()
		deadline := time.Now().Add(subWait)
		for {
			feed.mu.Lock()
			got := len(feed.listeners)
			feed.mu.Unlock()
			if got == n {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("feed listeners %d, want %d", got, n)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	open := func(t *testing.T, path string) *subClient {
		t.Helper()
		c := dialSub(t, srv.URL, path)
		t.Cleanup(func() {
			c.conn.Close()
			listeners(t, 0)
		})
		c.init(t)
		return c
	}
	trader := subLower(subTrader)

	t.Run("indexerStatusUpdated", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", `subscription { indexerStatusUpdated { lastIndexedBlock chainHead blocksScanned eventsProcessed isSynced } }`, nil)
		listeners(t, 1)
		head(t, 42)
		publish(t, "chain.block")
		c.wantNext(t, "s", `{"indexerStatusUpdated":{"lastIndexedBlock":42,"chainHead":null,"blocksScanned":null,"eventsProcessed":null,"isSynced":null}}`)
		head(t, 43)
		publish(t, "0.mark")
		c.quiet(t)
		publish(t, "chain.block")
		c.wantNext(t, "s", `{"indexerStatusUpdated":{"lastIndexedBlock":43,"chainHead":null,"blocksScanned":null,"eventsProcessed":null,"isSynced":null}}`)
	})

	t.Run("latestPricesUpdated", func(t *testing.T) {
		reset(t)
		markPrice(t, 0, 9, 0, "90")
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", `subscription { latestPricesUpdated { marketId price onchainTimestamp blockNumber updatedAt } }`, nil)
		listeners(t, 1)
		markPrice(t, 0, 10, 0, "100")
		markPrice(t, 1, 10, 1, "200")
		publish(t, "0.mark", "1.mark")
		c.wantNext(t, "s", fmt.Sprintf(`{"latestPricesUpdated":[
			{"marketId":0,"price":"100","onchainTimestamp":%[1]d,"blockNumber":10,"updatedAt":%[2]q},
			{"marketId":1,"price":"200","onchainTimestamp":%[1]d,"blockNumber":10,"updatedAt":%[2]q}]}`, subAt(10).Unix(), subStamp(10)))
		c.quiet(t)
	})

	priceQuery := `subscription ($m: Int) { priceUpdated(marketId: $m) { id marketId price onchainTimestamp timestamp blockNumber txHash blockTimestamp updatedAt } }`
	priceJSON := func(id, market, block int64, price string) string {
		return fmt.Sprintf(`{"priceUpdated":{"id":%d,"marketId":%d,"price":%q,"onchainTimestamp":%d,"timestamp":%[4]d,"blockNumber":%d,"txHash":%q,"blockTimestamp":%q,"updatedAt":%[7]q}}`,
			id, market, price, subAt(block).Unix(), block, subHex(subBlockHash(block)), subStamp(block))
	}

	t.Run("priceUpdated", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", priceQuery, map[string]any{"m": 1})
		listeners(t, 1)
		indexPrice(t, 1, 11, 0, "skipped", nil)
		indexPrice(t, 0, 11, 1, "aggregate", "250")
		id := indexPrice(t, 1, 11, 2, "aggregate", "300")
		publish(t, "0.index", "1.index")
		c.wantNext(t, "s", priceJSON(id, 1, 11, "300"))
		c.quiet(t)
	})

	t.Run("price_updated_all_markets", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", priceQuery, nil)
		listeners(t, 1)
		first := indexPrice(t, 0, 12, 0, "aggregate", "400")
		second := indexPrice(t, 1, 12, 1, "aggregate", "500")
		publish(t, "0.index", "1.index")
		c.wantNext(t, "s", priceJSON(first, 0, 12, "400"))
		c.wantNext(t, "s", priceJSON(second, 1, 12, "500"))
		c.quiet(t)
	})

	t.Run("positionChanged", func(t *testing.T) {
		reset(t)
		block(t, 20)
		block(t, 25)
		head(t, 19)
		position := func(id int64, status string, closed any) {
			exec(t, `INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd, collateral_usd, collateral_amount, collateral_token,
				entry_price, leverage, realized_pnl, status, opened_block, closed_block) VALUES ($1, $2, 1, true, 1000, 100, 100, $3, 2000, 10, -5, $4, 20, $5)`,
				id, subTrader.Bytes(), subToken.Bytes(), status, closed)
		}
		position(1, "open", nil)
		position(2, "closed", 25)
		position(3, "closed", 5)
		exec(t, `INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd, collateral_usd, collateral_amount, collateral_token,
			entry_price, leverage, realized_pnl, status, opened_block) VALUES (4, $1, 1, true, 1, 1, 1, $2, 1, 1, 0, 'open', 20)`, subOther.Bytes(), subToken.Bytes())
		fact(t, "position_events", "opened", 20, 0, 0, subTrader, []string{"position_id"}, 1)
		fact(t, "position_events", "opened", 20, 1, 0, subTrader, []string{"position_id"}, 2)
		fact(t, "position_events", "closed", 25, 0, 1, subTrader, []string{"position_id"}, 2)
		settlement(t, 2, 1, 25, 0, 0, 1, "1000", "2500")

		c := open(t, "/graphql-ws")
		c.subscribe(t, "bad", `subscription { positionChanged(userAddress: "nope") { positionId } }`, nil)
		c.wantError(t, "bad", `invalid userAddress "nope"`)
		c.subscribe(t, "s", `subscription ($u: String!) { positionChanged(userAddress: $u) { positionId userAddress marketId isLong sizeUsd collateralToken
			status realizedPnl exitPrice openedAt closedAt openBlock closeBlock openTxHash closeTxHash market { marketId symbol name latestPrice { price } } } }`,
			map[string]any{"u": subTrader.Hex()})
		listeners(t, 1)
		head(t, 25)
		publish(t, "user."+subLower(subOther)+".position")
		c.quiet(t)
		publish(t, "user."+trader+".position")
		open1 := fmt.Sprintf(`{"positionId":"1","userAddress":%q,"marketId":1,"isLong":true,"sizeUsd":"1000","collateralToken":%q,"status":"open",
			"realizedPnl":"-5","exitPrice":null,"openedAt":%q,"closedAt":null,"openBlock":20,"closeBlock":null,"openTxHash":%q,"closeTxHash":null,
			"market":{"marketId":1,"symbol":"ETH","name":"Ethereum","latestPrice":null}}`, trader, subLower(subToken), subStamp(20), subHex(subHash(20, 0, 0)))
		closed2 := fmt.Sprintf(`{"positionId":"2","userAddress":%q,"marketId":1,"isLong":true,"sizeUsd":"1000","collateralToken":%q,"status":"closed",
			"realizedPnl":"-5","exitPrice":"2500","openedAt":%q,"closedAt":%q,"openBlock":20,"closeBlock":25,"openTxHash":%q,"closeTxHash":%q,
			"market":{"marketId":1,"symbol":"ETH","name":"Ethereum","latestPrice":null}}`, trader, subLower(subToken), subStamp(20), subStamp(25),
			subHex(subHash(20, 1, 0)), subHex(subHash(25, 0, 1)))
		c.wantNext(t, "s", `{"positionChanged":[`+open1+`,`+closed2+`]}`)
		publish(t, "user."+trader+".position")
		c.wantNext(t, "s", `{"positionChanged":[`+open1+`]}`)
	})

	t.Run("orderChanged", func(t *testing.T) {
		reset(t)
		block(t, 30)
		block(t, 31)
		exec(t, `INSERT INTO perps.orders (order_id, account, market_id, order_type, is_long, trigger_price, size_usd, remaining_size_usd, status, placed_block)
			VALUES (7, $1, 2, 4, false, 0, 600, 600, 'active', 30)`, subTrader.Bytes())
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", `subscription ($u: String!) { orderChanged(userAddress: $u) { orderId userAddress marketId orderType orderTypeName isLong
			triggerPrice sizeUsd remainingSize status positionId executionPrice failureReason placedAt resolvedAt placedBlock resolvedBlock placedTxHash
			resolvedTxHash blockTimestamp } }`, map[string]any{"u": trader})
		listeners(t, 1)
		fact(t, "order_events", "placed", 30, 0, 0, subTrader, []string{"order_id", "user", "market_id"}, 7, subTrader.Bytes(), 2)
		publish(t, "user."+trader+".order")
		c.wantNext(t, "s", fmt.Sprintf(`{"orderChanged":{"orderId":"7","userAddress":%q,"marketId":2,"orderType":4,"orderTypeName":"Market","isLong":false,
			"triggerPrice":"0","sizeUsd":"600","remainingSize":"600","status":"active","positionId":null,"executionPrice":null,"failureReason":null,
			"placedAt":%q,"resolvedAt":null,"placedBlock":30,"resolvedBlock":null,"placedTxHash":%q,"resolvedTxHash":null,"blockTimestamp":%[2]q}}`,
			trader, subStamp(30), subHex(subHash(30, 0, 0))))
		exec(t, `UPDATE perps.orders SET status = 'filled', remaining_size_usd = 0, position_id = 12, execution_price = 2100, closed_block = 31 WHERE order_id = 7`)
		fact(t, "fills", "fill", 31, 0, 3, subEmitter, []string{"market_id", "maker_order_id", "taker_order_id", "maker", "taker", "taker_is_long", "price",
			"size_usd", "maker_fee_usd", "taker_fee_usd"}, 2, 8, 7, subOther.Bytes(), subTrader.Bytes(), false, 2100, 600, 0, 1)
		publish(t, "user."+trader+".fill")
		c.wantNext(t, "s", fmt.Sprintf(`{"orderChanged":{"orderId":"7","userAddress":%q,"marketId":2,"orderType":4,"orderTypeName":"Market","isLong":false,
			"triggerPrice":"0","sizeUsd":"600","remainingSize":"0","status":"filled","positionId":"12","executionPrice":"2100","failureReason":null,
			"placedAt":%q,"resolvedAt":%q,"placedBlock":30,"resolvedBlock":31,"placedTxHash":%q,"resolvedTxHash":%q,"blockTimestamp":%[3]q}}`,
			trader, subStamp(30), subStamp(31), subHex(subHash(30, 0, 0)), subHex(subHash(31, 0, 3))))
		c.quiet(t)
	})

	tradeJSON := func(id, block int64, txIndex, logIndex int, tradeType, size, price string) string {
		return fmt.Sprintf(`{"tradeCreated":{"id":%d,"positionId":"9","userAddress":%q,"marketId":2,"tradeType":%q,"isLong":true,"sizeUsd":%q,"price":%q,
			"executionPrice":%[5]q,"realizedPnl":"-15","feeUsd":"4","blockNumber":%d,"txHash":%q,"blockTimestamp":%q}}`,
			id, trader, tradeType, size, price, block, subHex(subHash(block, txIndex, logIndex)), subStamp(block))
	}

	t.Run("tradeCreated", func(t *testing.T) {
		reset(t)
		exec(t, `INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd, collateral_usd, collateral_amount, collateral_token,
			entry_price, leverage, realized_pnl, status, opened_block) VALUES (9, $1, 2, true, 1000, 100, 100, $2, 2000, 10, 0, 'open', 40)`,
			subTrader.Bytes(), subToken.Bytes())
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", `subscription { tradeCreated(marketId: 2) { id positionId userAddress marketId tradeType isLong sizeUsd price executionPrice
			realizedPnl feeUsd blockNumber txHash blockTimestamp } }`, nil)
		listeners(t, 1)
		settlement(t, 9, 3, 41, 0, 0, 0, "1", "1")
		reduce := settlement(t, 9, 2, 41, 0, 1, 3, "400", "2050")
		unknown := settlement(t, 9, 2, 41, 1, 0, 9, "100", "2060")
		publish(t, "2.fill")
		c.wantNext(t, "s", tradeJSON(reduce, 41, 0, 1, "reduce", "400", "2050"))
		c.wantNext(t, "s", tradeJSON(unknown, 41, 1, 0, "9", "100", "2060"))
		c.quiet(t)
	})

	t.Run("liquidationCreated", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", `subscription { liquidationCreated(marketId: 2) { id positionId userAddress marketId price executionPrice sizeUsd penalty keeper
			blockNumber txHash blockTimestamp } }`, nil)
		listeners(t, 1)
		settlement(t, 9, 2, 50, 0, 0, 2, "500", "1800")
		liquidation := func(market int64, logIndex int) int64 {
			return fact(t, "liquidations", "liquidated", 50, 0, logIndex, subEmitter, []string{"position_id", "user", "market_id", "liquidation_price", "penalty", "keeper"},
				9, subTrader.Bytes(), market, 1800, 25, subKeeper.Bytes())
		}
		liquidation(3, 1)
		id := liquidation(2, 2)
		publish(t, "3.liq", "2.liq")
		c.wantNext(t, "s", fmt.Sprintf(`{"liquidationCreated":{"id":%d,"positionId":"9","userAddress":%q,"marketId":2,"price":"1800","executionPrice":"1800",
			"sizeUsd":"500","penalty":"25","keeper":%q,"blockNumber":50,"txHash":%q,"blockTimestamp":%q}}`,
			id, trader, subLower(subKeeper), subHex(subHash(50, 0, 2)), subStamp(50)))
		c.quiet(t)
	})

	fill := func(t *testing.T, market, block int64, logIndex int, isLong bool, size, price string) int64 {
		t.Helper()
		return fact(t, "fills", "fill", block, 0, logIndex, subEmitter, []string{"market_id", "maker_order_id", "taker_order_id", "maker", "taker", "taker_is_long",
			"price", "size_usd", "maker_fee_usd", "taker_fee_usd"}, market, 8, 7, subOther.Bytes(), subTrader.Bytes(), isLong, price, size, -2, 3)
	}
	executedQuery := `subscription ($m: Int) { tradeExecuted(marketId: $m) { tradeId marketId isLong sizeUsd price blockTimestamp } }`
	executedJSON := func(id, market, block int64, isLong bool, size, price string) string {
		return fmt.Sprintf(`{"tradeExecuted":{"tradeId":"%d","marketId":%d,"isLong":%t,"sizeUsd":%q,"price":%q,"blockTimestamp":%d}}`,
			id, market, isLong, size, price, subAt(block).Unix())
	}

	t.Run("tradeExecuted", func(t *testing.T) {
		reset(t)
		fill(t, 2, 54, 0, true, "1", "1")
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", executedQuery, map[string]any{"m": 2})
		listeners(t, 1)
		fill(t, 3, 55, 0, true, "100", "2000")
		first := fill(t, 2, 55, 1, true, "9007199254740993", "12345678901234567890")
		second := fill(t, 2, 55, 2, false, "300", "2010")
		publish(t, "3.fill")
		c.quiet(t)
		publish(t, "2.fill")
		c.wantNext(t, "s", executedJSON(first, 2, 55, true, "9007199254740993", "12345678901234567890"))
		c.wantNext(t, "s", executedJSON(second, 2, 55, false, "300", "2010"))
		c.quiet(t)
	})

	t.Run("trade_executed_all_markets", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		c.subscribe(t, "s", executedQuery, nil)
		listeners(t, 1)
		first := fill(t, 3, 56, 0, true, "100", "2000")
		second := fill(t, 2, 56, 1, false, "300", "2010")
		publish(t, "user."+trader+".fill")
		c.quiet(t)
		publish(t, "3.fill", "2.fill")
		c.wantNext(t, "s", executedJSON(first, 3, 56, true, "100", "2000"))
		c.wantNext(t, "s", executedJSON(second, 2, 56, false, "300", "2010"))
		c.quiet(t)
	})

	t.Run("rewind", func(t *testing.T) {
		reset(t)
		markPrice(t, 1, 60, 0, "700")
		indexPrice(t, 1, 59, 0, "aggregate", "690")
		latest := indexPrice(t, 1, 60, 1, "aggregate", "700")
		c := open(t, "/graphql-ws")
		c.subscribe(t, "p", priceQuery, map[string]any{"m": 1})
		c.subscribe(t, "l", `subscription { latestPricesUpdated { marketId price } }`, nil)
		c.subscribe(t, "t", `subscription { tradeCreated(marketId: 5) { id } }`, nil)
		listeners(t, 3)
		publish(t, "chain.rewind")
		got := map[string]json.RawMessage{}
		for range 2 {
			f := c.read(t)
			if f.Type != "next" {
				t.Fatalf("frame %+v, want next", f)
			}
			got[f.ID] = f.data(t)
		}
		wantJSON(t, got["p"], priceJSON(latest, 1, 60, "700"))
		wantJSON(t, got["l"], `{"latestPricesUpdated":[{"marketId":1,"price":"700"}]}`)
		c.quiet(t)
	})

	t.Run("init_timeout", func(t *testing.T) {
		c := dialSub(t, srv.URL, "/graphql-ws")
		defer c.conn.Close()
		start := time.Now()
		if code := c.closed(t, initTimeout+subWait); code != closeInitTimeout {
			t.Fatalf("close code %d, want %d", code, closeInitTimeout)
		}
		if elapsed := time.Since(start); elapsed < initTimeout-time.Second {
			t.Fatalf("closed after %s, want %s", elapsed, initTimeout)
		}
	})

	t.Run("multiplex", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		c.subscribe(t, "1", priceQuery, map[string]any{"m": 1})
		c.subscribe(t, "2", priceQuery, map[string]any{"m": 1})
		listeners(t, 2)
		c.send(t, map[string]any{"type": "ping"})
		if f := c.read(t); f.Type != "pong" {
			t.Fatalf("frame %+v, want pong", f)
		}
		c.send(t, map[string]any{"id": "1", "type": "complete"})
		listeners(t, 1)
		id := indexPrice(t, 1, 70, 0, "aggregate", "800")
		publish(t, "1.index")
		c.wantNext(t, "2", priceJSON(id, 1, 70, "800"))
		c.quiet(t)
		c.subscribe(t, "2", priceQuery, map[string]any{"m": 1})
		if code := c.closed(t, subWait); code != closeDuplicateID {
			t.Fatalf("close code %d, want %d", code, closeDuplicateID)
		}
	})

	t.Run("subscription_cap", func(t *testing.T) {
		reset(t)
		c := open(t, "/graphql-ws")
		for i := 1; i <= maxSubscriptions; i++ {
			c.subscribe(t, strconv.Itoa(i), `subscription { indexerStatusUpdated { lastIndexedBlock } }`, nil)
		}
		listeners(t, maxSubscriptions)
		c.subscribe(t, "101", `subscription { indexerStatusUpdated { lastIndexedBlock } }`, nil)
		c.wantError(t, "101", "too many subscriptions")
		head(t, 80)
		publish(t, "chain.block")
		seen := map[string]bool{}
		for range maxSubscriptions {
			f := c.read(t)
			if f.Type != "next" || seen[f.ID] {
				t.Fatalf("frame %+v after %d", f, len(seen))
			}
			wantJSON(t, f.data(t), `{"indexerStatusUpdated":{"lastIndexedBlock":80}}`)
			seen[f.ID] = true
		}
		if seen["101"] {
			t.Fatal("refused subscription 101 received data")
		}
		listeners(t, maxSubscriptions)
	})

	t.Run("transport_on_graphql", func(t *testing.T) {
		open(t, "/graphql")
		resp, err := http.Get(srv.URL + "/graphql")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("plain GET /graphql status %d, want %d", resp.StatusCode, http.StatusNotFound)
		}
	})
}

type subFrame struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func (f subFrame) data(t *testing.T) json.RawMessage {
	t.Helper()
	var p struct {
		Data   json.RawMessage `json:"data"`
		Errors json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("next payload %s: %v", f.Payload, err)
	}
	if len(p.Errors) > 0 {
		t.Fatalf("next %s carries errors %s", f.ID, p.Errors)
	}
	return p.Data
}

// subClient reads its socket in a goroutine: a gorilla read that times out
// leaves the connection unusable, so waits select on the frame channel.
type subClient struct {
	conn   *websocket.Conn
	frames chan subFrame
	code   chan int
}

func dialSub(t *testing.T, base, path string) *subClient {
	t.Helper()
	dialer := websocket.Dialer{Subprotocols: []string{Subprotocol}, HandshakeTimeout: subWait}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(base, "http")+path, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", path, err)
	}
	resp.Body.Close()
	if got := conn.Subprotocol(); got != Subprotocol {
		t.Fatalf("subprotocol %q", got)
	}
	c := &subClient{conn: conn, frames: make(chan subFrame, 4*maxSubscriptions), code: make(chan int, 1)}
	go func() {
		defer close(c.frames)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				var ce *websocket.CloseError
				if errors.As(err, &ce) {
					c.code <- ce.Code
				} else {
					c.code <- -1
				}
				return
			}
			var f subFrame
			if err := json.Unmarshal(raw, &f); err != nil {
				c.code <- -2
				return
			}
			c.frames <- f
		}
	}()
	return c
}

func (c *subClient) send(t *testing.T, v any) {
	t.Helper()
	if err := c.conn.WriteJSON(v); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func (c *subClient) read(t *testing.T) subFrame {
	t.Helper()
	select {
	case f, ok := <-c.frames:
		if !ok {
			t.Fatalf("socket closed with code %d", <-c.code)
		}
		return f
	case <-time.After(subWait):
		t.Fatal("no frame")
	}
	return subFrame{}
}

func (c *subClient) quiet(t *testing.T) {
	t.Helper()
	select {
	case f, ok := <-c.frames:
		if ok {
			t.Fatalf("unexpected frame %+v", f)
		}
		t.Fatalf("socket closed with code %d", <-c.code)
	case <-time.After(subQuiet):
	}
}

func (c *subClient) closed(t *testing.T, within time.Duration) int {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				return <-c.code
			}
			t.Fatalf("frame %+v before close", f)
		case <-deadline:
			t.Fatal("socket not closed")
		}
	}
}

func (c *subClient) init(t *testing.T) {
	t.Helper()
	c.send(t, map[string]any{"type": "connection_init", "payload": map[string]any{}})
	if f := c.read(t); f.Type != "connection_ack" {
		t.Fatalf("frame %+v, want connection_ack", f)
	}
}

func (c *subClient) subscribe(t *testing.T, id, query string, vars map[string]any) {
	t.Helper()
	c.send(t, map[string]any{"id": id, "type": "subscribe", "payload": map[string]any{"query": query, "variables": vars}})
}

func (c *subClient) wantNext(t *testing.T, id, want string) {
	t.Helper()
	f := c.read(t)
	if f.Type != "next" || f.ID != id {
		t.Fatalf("frame %+v, want next %s", f, id)
	}
	wantJSON(t, f.data(t), want)
}

func (c *subClient) wantError(t *testing.T, id, message string) {
	t.Helper()
	f := c.read(t)
	if f.Type != "error" || f.ID != id {
		t.Fatalf("frame %+v, want error %s", f, id)
	}
	var errs []struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(f.Payload, &errs); err != nil {
		t.Fatalf("error payload %s: %v", f.Payload, err)
	}
	if len(errs) != 1 || errs[0].Message != message {
		t.Fatalf("error payload %s, want one message %q", f.Payload, message)
	}
}

func wantJSON(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("got %s: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want %s: %v", want, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
