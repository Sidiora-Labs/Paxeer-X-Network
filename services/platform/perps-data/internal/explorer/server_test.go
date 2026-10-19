package explorer

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/bech32"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/migrations"
)

const srvLinkBase = "https://scan.example"

var (
	srvAlice    = srvBytes(0xa1, 20)
	srvBob      = srvBytes(0xb2, 20)
	srvEmitter  = append(srvBytes(0, 18), 0x10, 0x15)
	srvTxPlace  = srvBytes(0x01, 32)
	srvTxFill   = srvBytes(0x02, 32)
	srvTxCancel = srvBytes(0x03, 32)
	srvReceipt  = srvBytes(0x04, 32)
)

func srvBytes(b byte, n int) []byte { return bytes.Repeat([]byte{b}, n) }

func srvHex(b []byte) string { return "0x" + hex.EncodeToString(b) }

func srvBlockHash(n int64) []byte { return srvBytes(byte(0x80+n-100), 32) }

func srvUser(t *testing.T, account []byte) string {
	t.Helper()
	s, err := bech32.ConvertAndEncode("pax", account)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func srvOpenDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(files)
	for _, name := range files {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	return db
}

type srvAction struct {
	block             int64
	txIndex, logIndex int
	txHash            []byte
	receipt           string
	account           []byte
	kind, action      string
	symbol, side      any
	price, size       any
}

func srvSeed(t *testing.T, db *sql.DB, base time.Time) {
	t.Helper()
	for n := int64(100); n <= 102; n++ {
		if _, err := db.Exec(`INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, $6)`,
			n, srvBlockHash(n), srvBlockHash(n-1), base.Add(time.Duration(n-100)*time.Second), "AB12CD34EF56AB12CD34EF56AB12CD34EF56AB12", n-98); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO perps.cursors (name, height, hash, updated_at) VALUES ('head', 102, $1, now())`, srvBlockHash(102)); err != nil {
		t.Fatal(err)
	}
	actions := []srvAction{
		{100, 0, 0, srvTxPlace, "tx", srvAlice, "OrderPlaced", "Place Order", "BTC", "buy", "77175200000000000000000", "420000000000000000"},
		{101, 0, 0, srvTxFill, "tx", srvAlice, "Fill", "OROB Fill", "BTC", "buy", "77176000000000000000000", "420000000000000000"},
		{102, -1, 0, srvReceipt, "perps_endblock", srvBob, "Fill", "OROB Fill", "BTC", "sell", "77180000000000000000000", "1000000000000000000"},
		{102, -1, 1, srvReceipt, "perps_endblock", srvBob, "perps_endblock", "Sealed Batch Executed", nil, nil, nil, "2"},
		{102, 1, 3, srvTxCancel, "tx", srvAlice, "OrderCancelled", "Cancel Order", "ETH", "sell", nil, nil},
	}
	for _, a := range actions {
		var from any
		if a.receipt == "tx" {
			from = a.account
		}
		if _, err := db.Exec(`INSERT INTO perps.tx_actions (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account, tx_from, action, symbol, side, price, size)
VALUES ($1, $2, $3, $4, 0, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
			a.block, srvBlockHash(a.block), a.txIndex, a.logIndex, a.txHash, a.receipt, base.Add(time.Duration(a.block-100)*time.Second),
			srvEmitter, a.kind, a.account, from, a.action, a.symbol, a.side, a.price, a.size); err != nil {
			t.Fatal(err)
		}
		if a.account[0] != srvAlice[0] {
			continue
		}
		if _, err := db.Exec(`INSERT INTO perps.order_events (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time, emitter, kind, account, tx_from, order_id)
VALUES ($1, $2, $3, $4, 0, $5, 'tx', $6, $7, 'placed', $8, $8, 1)`,
			a.block, srvBlockHash(a.block), a.txIndex, a.logIndex, a.txHash, base.Add(time.Duration(a.block-100)*time.Second), srvEmitter, a.account); err != nil {
			t.Fatal(err)
		}
	}
}

func srvNew(t *testing.T, db *sql.DB, requests int) *Server {
	t.Helper()
	metrics, err := telemetry.New(config.Explorer)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewServer(db, Options{LinkBase: srvLinkBase, Requests: requests, Window: time.Minute, Metrics: metrics})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func srvGet(t *testing.T, s *Server, path string, header http.Header) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: %v: %s", path, err, rec.Body.String())
	}
	return rec, body
}

func srvOK(t *testing.T, s *Server, path string) map[string]any {
	t.Helper()
	rec, body := srvGet(t, s, path, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "max-age=2" {
		t.Fatalf("%s: Cache-Control %q", path, got)
	}
	return body
}

func srvError(t *testing.T, s *Server, path string, code int, msg string) {
	t.Helper()
	rec, body := srvGet(t, s, path, nil)
	if rec.Code != code || body["error"] != msg {
		t.Fatalf("%s: got %d %v, want %d %q", path, rec.Code, body, code, msg)
	}
}

func srvList(t *testing.T, body map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := body[key].([]any)
	if !ok {
		t.Fatalf("%s is not a list: %v", key, body)
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

// srvRows checks the hash and transaction link of every row in order.
func srvRows(t *testing.T, rows []map[string]any, hashes ...[]byte) {
	t.Helper()
	if len(rows) != len(hashes) {
		t.Fatalf("got %d rows, want %d: %v", len(rows), len(hashes), rows)
	}
	for i, h := range hashes {
		if rows[i]["hash"] != srvHex(h) || rows[i]["link"] != srvLinkBase+"/tx/"+srvHex(h) {
			t.Fatalf("row %d: got hash %v link %v, want %s", i, rows[i]["hash"], rows[i]["link"], srvHex(h))
		}
	}
}

func srvCursor(t *testing.T, body map[string]any) string {
	t.Helper()
	c, _ := body["next_cursor"].(string)
	return c
}

func TestExplorerServer(t *testing.T) {
	db := srvOpenDB(t)
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	srvSeed(t, db, base)
	s := srvNew(t, db, 1000)

	t.Run("route_blocks", func(t *testing.T) {
		blocks := srvList(t, srvOK(t, s, "/api/chain/blocks"), "blocks")
		if len(blocks) != 3 {
			t.Fatalf("got %d blocks: %v", len(blocks), blocks)
		}
		for i, n := range []int64{102, 101, 100} {
			b := blocks[i]
			if b["block_number"] != float64(n) || b["transactions"] != float64(n-98) || b["proposer"] != "AB12CD34EF56AB12CD34EF56AB12CD34EF56AB12" ||
				b["hash"] != srvHex(srvBlockHash(n)) || b["link"] != srvLinkBase+"/block/"+strconv.FormatInt(n, 10) ||
				b["time"] != base.Add(time.Duration(n-100)*time.Second).Format("2006-01-02T15:04:05.000Z") {
				t.Fatalf("block %d: %v", i, b)
			}
		}
		if got := srvList(t, srvOK(t, s, "/api/chain/blocks?limit=1"), "blocks"); len(got) != 1 || got[0]["block_number"] != float64(102) {
			t.Fatalf("limit=1 blocks %v", got)
		}
		srvError(t, s, "/api/chain/blocks?limit=x", http.StatusBadRequest, "invalid limit")
	})

	t.Run("route_transactions", func(t *testing.T) {
		txs := srvList(t, srvOK(t, s, "/api/chain/transactions"), "transactions")
		srvRows(t, txs, srvTxCancel, srvReceipt, srvReceipt, srvTxFill, srvTxPlace)
		if txs[0]["action"] != "Cancel Order" || txs[0]["user"] != srvUser(t, srvAlice) || txs[0]["symbol"] != "ETH" || txs[0]["block"] != float64(102) ||
			txs[1]["action"] != "Sealed Batch Executed" || txs[1]["size"] != float64(2) || txs[3]["action"] != "OROB Fill" || txs[3]["price"] != 77176.0 {
			t.Fatalf("first transaction %v", txs[0])
		}
		srvRows(t, srvList(t, srvOK(t, s, "/api/chain/transactions?limit=2"), "transactions"), srvTxCancel, srvReceipt)
	})

	t.Run("route_health", func(t *testing.T) {
		before := time.Now().UnixMilli()
		body := srvOK(t, s, "/api/chain/health")
		ts, _ := body["ts"].(float64)
		if body["status"] != "ok" || body["chain_connected"] != true || body["blocks_seen"] != float64(3) || body["txs_seen"] != float64(5) ||
			int64(ts) < before || int64(ts) > time.Now().UnixMilli() {
			t.Fatalf("chain health %v", body)
		}
	})

	t.Run("explorer_health", func(t *testing.T) {
		body := srvOK(t, s, "/api/explorer/health")
		if len(body) != 2 || body["latest_block"] != float64(102) || body["chain_connected"] != true {
			t.Fatalf("explorer health %v", body)
		}
		if _, err := db.Exec(`INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count)
SELECT 900, $1, hash, time + interval '1 second', proposer, 0 FROM perps.blocks WHERE number = 102`, srvBlockHash(900)); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.Exec(`DELETE FROM perps.blocks WHERE number = 900`); err != nil {
				t.Fatal(err)
			}
		})
		if body := srvOK(t, s, "/api/explorer/health"); len(body) != 2 || body["latest_block"] != float64(900) || body["chain_connected"] != true {
			t.Fatalf("advanced explorer health %v", body)
		}
		if _, err := db.Exec(`UPDATE perps.cursors SET updated_at = now() - interval '1 minute' WHERE name = 'head'`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := db.Exec(`UPDATE perps.cursors SET updated_at = now() WHERE name = 'head'`); err != nil {
				t.Fatal(err)
			}
		})
		if body := srvOK(t, s, "/api/explorer/health"); body["latest_block"] != float64(900) || body["chain_connected"] != false {
			t.Fatalf("stale explorer health %v", body)
		}
		if body := srvOK(t, s, "/api/chain/health"); body["status"] != "degraded" || body["chain_connected"] != false {
			t.Fatalf("stale chain health %v", body)
		}
	})

	t.Run("route_account_history", func(t *testing.T) {
		for _, account := range []string{srvHex(srvAlice), srvUser(t, srvAlice)} {
			first := srvOK(t, s, "/api/chain/accounts/"+account+"/history?limit=2")
			srvRows(t, srvList(t, first, "items"), srvTxCancel, srvTxFill)
			cursor := srvCursor(t, first)
			if cursor == "" {
				t.Fatalf("first page has no next_cursor: %v", first)
			}
			last := srvOK(t, s, "/api/chain/accounts/"+account+"/history?limit=2&cursor="+url.QueryEscape(cursor))
			srvRows(t, srvList(t, last, "items"), srvTxPlace)
			if c := srvCursor(t, last); c != "" {
				t.Fatalf("last page next_cursor %q", c)
			}
		}
		srvError(t, s, "/api/chain/accounts/0x12/history", http.StatusBadRequest, "invalid address")
		srvError(t, s, "/api/chain/accounts/"+srvHex(srvAlice)+"/history?cursor=bogus", http.StatusBadRequest, "invalid cursor")
	})

	t.Run("route_market_history", func(t *testing.T) {
		first := srvOK(t, s, "/api/chain/markets/btc/history?limit=2")
		srvRows(t, srvList(t, first, "items"), srvReceipt, srvTxFill)
		cursor := srvCursor(t, first)
		if cursor == "" {
			t.Fatalf("first page has no next_cursor: %v", first)
		}
		last := srvOK(t, s, "/api/chain/markets/BTC/history?limit=2&cursor="+url.QueryEscape(cursor))
		srvRows(t, srvList(t, last, "items"), srvTxPlace)
		if c := srvCursor(t, last); c != "" {
			t.Fatalf("last page next_cursor %q", c)
		}
		srvError(t, s, "/api/chain/markets/b-tc/history", http.StatusBadRequest, "invalid symbol")
	})

	t.Run("route_tx", func(t *testing.T) {
		srvEqual(t, srvOK(t, s, "/api/chain/tx/"+srvHex(srvTxPlace)), map[string]any{
			"hash": srvHex(srvTxPlace),
			"link": srvLinkBase + "/tx/" + srvHex(srvTxPlace),
			"transactions": []any{map[string]any{
				"hash": srvHex(srvTxPlace), "action": "Place Order", "block": 100, "time": "2030-01-02T03:04:05.000Z",
				"user": srvUser(t, srvAlice), "symbol": "BTC", "side": "buy", "price": 77175.2, "size": 0.42,
				"link": srvLinkBase + "/tx/" + srvHex(srvTxPlace),
			}},
		})
	})

	t.Run("synthetic_tx_from_rows", func(t *testing.T) {
		link := srvLinkBase + "/tx/" + srvHex(srvReceipt)
		srvEqual(t, srvOK(t, s, "/api/chain/tx/"+srvHex(srvReceipt)), map[string]any{
			"hash": srvHex(srvReceipt),
			"link": link,
			"transactions": []any{
				map[string]any{
					"hash": srvHex(srvReceipt), "action": "OROB Fill", "block": 102, "time": "2030-01-02T03:04:07.000Z",
					"user": srvUser(t, srvBob), "symbol": "BTC", "side": "sell", "price": 77180.0, "size": 1.0, "link": link,
				},
				map[string]any{
					"hash": srvHex(srvReceipt), "action": "Sealed Batch Executed", "block": 102, "time": "2030-01-02T03:04:07.000Z",
					"user": srvUser(t, srvBob), "symbol": nil, "side": nil, "price": nil, "size": 2.0, "link": link,
				},
			},
		})
	})

	t.Run("unknown_tx_404", func(t *testing.T) {
		srvError(t, s, "/api/chain/tx/"+srvHex(srvBytes(0xee, 32)), http.StatusNotFound, "unknown transaction")
		srvError(t, s, "/api/chain/tx/0x1234", http.StatusBadRequest, "invalid transaction hash")
		srvError(t, s, "/api/chain/tx/0x"+hex.EncodeToString(srvBytes(0xee, 31))+"zz", http.StatusBadRequest, "invalid transaction hash")
	})

	t.Run("rate_limited_429", func(t *testing.T) {
		limited := srvNew(t, db, 2)
		for i := range 2 {
			if rec, _ := srvGet(t, limited, "/api/explorer/health", nil); rec.Code != http.StatusOK {
				t.Fatalf("request %d: status %d", i, rec.Code)
			}
		}
		rec, body := srvGet(t, limited, "/api/explorer/health", nil)
		if rec.Code != http.StatusTooManyRequests || body["error"] != "rate limit exceeded" || rec.Header().Get("Retry-After") != "60" {
			t.Fatalf("third request: %d %v Retry-After %q", rec.Code, body, rec.Header().Get("Retry-After"))
		}
		if rec, _ := srvGet(t, limited, "/api/explorer/health", http.Header{"X-Api-Key": {"client-b"}}); rec.Code != http.StatusOK {
			t.Fatalf("other client key: status %d", rec.Code)
		}
	})
}

func srvEqual(t *testing.T, got, want map[string]any) {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, w) {
		t.Fatalf("got  %s\nwant %s", g, w)
	}
}
