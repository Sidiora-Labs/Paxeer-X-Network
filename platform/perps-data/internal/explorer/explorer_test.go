package explorer

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	_ "github.com/lib/pq"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/migrations"
)

var (
	spotAddress = common.HexToAddress("0x0000000000000000000000000000000000001021")
	trader      = common.HexToAddress("0x1111111111111111111111111111111111111111")
	other       = common.HexToAddress("0x00000000000000000000000000000000000000ff")
	baseTime    = time.Date(2026, 5, 18, 0, 0, 0, 123_000_000, time.UTC)

	zero = common.Address{}
)

// traderUser and zeroUser are the bech32 forms with prefix pax of trader and
// the zero address.
const (
	traderUser = "pax1zyg3zyg3zyg3zyg3zyg3zyg3zyg3zyg3aj26p8"
	zeroUser   = "pax1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq0va854"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("PERPS_DATA_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_DATABASE_URL is unset")
	}
	db, err := sql.Open("postgres", url)
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

func reset(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`TRUNCATE perps.blocks, perps.tx_actions, perps.collateral_events RESTART IDENTITY`); err != nil {
		t.Fatal(err)
	}
}

func blockHash(block int64) common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("block-%026d", block)))
}

func txHash(block int64, txIndex int) common.Hash {
	return common.BytesToHash([]byte(fmt.Sprintf("tx-%019d-%08d", block, txIndex+1)))
}

func proposer(block int64) string {
	return fmt.Sprintf("AB12CD34%032X", block)
}

func blockTime(block int64) time.Time {
	return baseTime.Add(time.Duration(block) * time.Second)
}

func insertBlock(t *testing.T, db *sql.DB, number int64, txCount int) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, $6)`,
		number, blockHash(number).Bytes(), blockHash(number-1).Bytes(), blockTime(number), proposer(number), txCount); err != nil {
		t.Fatalf("insert block %d: %v", number, err)
	}
}

// action is one perps.tx_actions row.
type action struct {
	block             int64
	txIndex, logIndex int
	sub               int
	hash              common.Hash
	receiptKind       string
	emitter           common.Address
	kind, action      string
	account           *common.Address
	symbol, side      any
	price, size       any
}

func insertAction(t *testing.T, db *sql.DB, a action) {
	t.Helper()
	if a.hash == (common.Hash{}) {
		a.hash = txHash(a.block, a.txIndex)
	}
	if a.receiptKind == "" {
		a.receiptKind = "tx"
	}
	if a.emitter == (common.Address{}) {
		a.emitter = perpsAddress
	}
	account := trader
	if a.account != nil {
		account = *a.account
	}
	var from any
	if a.receiptKind == "tx" {
		from = account.Bytes()
	}
	if _, err := db.Exec(`INSERT INTO perps.tx_actions (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time,
        emitter, kind, account, tx_from, action, symbol, side, price, size)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		a.block, blockHash(a.block).Bytes(), a.txIndex, a.logIndex, a.sub, a.hash.Bytes(), a.receiptKind, blockTime(a.block),
		a.emitter.Bytes(), a.kind, account.Bytes(), from, a.action, a.symbol, a.side, a.price, a.size); err != nil {
		t.Fatalf("insert action %+v: %v", a, err)
	}
}

func insertDeposit(t *testing.T, db *sql.DB, block int64, txIndex, logIndex, sub int, account common.Address, amount string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO perps.collateral_events (block, block_hash, tx_index, log_index, sub, tx_hash, receipt_kind, block_time,
        emitter, kind, account, tx_from, account_arg, amount, new_balance)
        VALUES ($1, $2, $3, $4, $5, $6, 'tx', $7, $8, 'deposited', $9, $9, $9, $10, $10)`,
		block, blockHash(block).Bytes(), txIndex, logIndex, sub, txHash(block, txIndex).Bytes(), blockTime(block),
		perpsAddress.Bytes(), account.Bytes(), amount); err != nil {
		t.Fatalf("insert deposit %d/%d/%d/%d: %v", block, txIndex, logIndex, sub, err)
	}
}

func newExplorer(t *testing.T, db *sql.DB, base string) *Explorer {
	t.Helper()
	e, err := New(db, base)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func ptr[T any](v T) *T { return &v }

func TestExplorer(t *testing.T) {
	db := openDB(t)
	ctx := context.Background()

	t.Run("blocks_clamp", func(t *testing.T) {
		reset(t, db)
		for n := int64(1); n <= 60; n++ {
			insertBlock(t, db, n, int(n%7))
		}
		e := newExplorer(t, db, "https://explorer.example")
		for _, c := range []struct {
			limit string
			want  int
		}{{"", 20}, {"0", 1}, {"-3", 1}, {"-99999999999999999999", 1}, {"1", 1}, {"37", 37}, {"50", 50}, {"51", 50}, {"1000", 50}, {"99999999999999999999", 50}} {
			blocks, err := e.Blocks(ctx, c.limit)
			if err != nil {
				t.Fatalf("Blocks(%q): %v", c.limit, err)
			}
			if len(blocks) != c.want {
				t.Fatalf("Blocks(%q) = %d rows, want %d", c.limit, len(blocks), c.want)
			}
			for i, b := range blocks {
				if b.BlockNumber != int64(60-i) {
					t.Fatalf("Blocks(%q)[%d] = block %d, want %d (newest first)", c.limit, i, b.BlockNumber, 60-i)
				}
			}
		}
		for _, bad := range []string{"abc", "1.5", " 2", "0x10"} {
			if _, err := e.Blocks(ctx, bad); !errors.Is(err, ErrLimit) {
				t.Fatalf("Blocks(%q) err = %v, want ErrLimit", bad, err)
			}
		}
		blocks, err := e.Blocks(ctx, "1")
		if err != nil {
			t.Fatal(err)
		}
		want := Block{
			BlockNumber:  60,
			Time:         "2026-05-18T00:01:00.123Z",
			Proposer:     "AB12CD340000000000000000000000000000003C",
			Hash:         blockHash(60).Hex(),
			Transactions: 4,
			Link:         "https://explorer.example/block/60",
		}
		if blocks[0] != want {
			t.Fatalf("block = %+v, want %+v", blocks[0], want)
		}
	})

	t.Run("transactions_clamp", func(t *testing.T) {
		reset(t, db)
		for i := 0; i < 210; i++ {
			insertAction(t, db, action{block: int64(1 + i/3), txIndex: i % 3, kind: "OrderPlaced", action: ActionPlaceOrder,
				symbol: "BTC", side: "buy", price: "77175200000000000000000", size: "420000000000000000"})
		}
		e := newExplorer(t, db, "")
		for _, c := range []struct {
			limit string
			want  int
		}{{"", 50}, {"0", 1}, {"-1", 1}, {"1", 1}, {"200", 200}, {"201", 200}, {"5000", 200}, {"99999999999999999999", 200}} {
			txs, err := e.Transactions(ctx, c.limit)
			if err != nil {
				t.Fatalf("Transactions(%q): %v", c.limit, err)
			}
			if len(txs) != c.want {
				t.Fatalf("Transactions(%q) = %d rows, want %d", c.limit, len(txs), c.want)
			}
			for i := 1; i < len(txs); i++ {
				if txs[i].Block > txs[i-1].Block {
					t.Fatalf("Transactions(%q) not newest first at %d: %d after %d", c.limit, i, txs[i].Block, txs[i-1].Block)
				}
			}
		}
		if _, err := e.Transactions(ctx, "ten"); !errors.Is(err, ErrLimit) {
			t.Fatalf("Transactions(ten) err = %v, want ErrLimit", err)
		}
		txs, err := e.Transactions(ctx, "1")
		if err != nil {
			t.Fatal(err)
		}
		want := Transaction{
			Hash: txHash(70, 2).Hex(), Action: ActionPlaceOrder, Block: 70, Time: "2026-05-18T00:01:10.123Z", User: traderUser,
			Symbol: ptr("BTC"), Side: ptr("buy"), Price: ptr(77175.2), Size: ptr(0.42),
		}
		got, _ := json.Marshal(txs[0])
		exp, _ := json.Marshal(want)
		if string(got) != string(exp) {
			t.Fatalf("transaction = %s, want %s", got, exp)
		}
	})

	t.Run("action_mapping", func(t *testing.T) {
		mapping := map[string]string{
			"OrderPlaced": "Place Order", "LimitOrderCreated": "Place Order", "StopLimitOrderCreated": "Place Order", "StopLossOrderCreated": "Place Order",
			"OrderCancelled": "Cancel Order", "OrderExpired": "Cancel Order", "LimitOrderCancelled": "Cancel Order", "LimitOrderExpired": "Cancel Order",
			"StopLimitCancelled": "Cancel Order", "StopLossOrderCancelled": "Cancel Order",
			"LimitOrderModified": "Modify Order", "PositionTpSlSet": "Modify Order", "OrderTriggered": "Modify Order", "StopLimitActivated": "Modify Order",
			"Fill": "OROB Fill", "LimitOrderExecuted": "OROB Fill", "StopLimitExecuted": "OROB Fill", "StopLossOrderExecuted": "OROB Fill",
			"MarketOrderExecuted": "OROB Fill", "NativeSwap": "OROB Fill", "SimpleSwap": "OROB Fill",
			"pricefeed_aggregate": "Oracle Updated", "perps_endblock": "Sealed Batch Executed", "spot_endblock": "Sealed Batch Executed",
		}
		for kind, want := range mapping {
			got, err := Action(kind)
			if err != nil || got != want {
				t.Fatalf("Action(%q) = %q, %v; want %q", kind, got, err, want)
			}
		}
		for _, kind := range []string{"", "OrderExecuted", "PositionOpened", "TradeSettled", "orderplaced", "FeedKeyRegistered", "activation"} {
			if got, err := Action(kind); !errors.Is(err, ErrUnknownKind) || got != "" {
				t.Fatalf("Action(%q) = %q, %v; want ErrUnknownKind", kind, got, err)
			}
		}

		reset(t, db)
		insertAction(t, db, action{block: 5, txIndex: 0, logIndex: 0, kind: "Fill", action: ActionOROBFill,
			symbol: "BTC", side: "sell", price: "77175200000000000000000", size: "420000000000000000"})
		insertAction(t, db, action{block: 5, txIndex: 1, logIndex: 1, emitter: spotAddress, kind: "MarketOrderExecuted", action: ActionOROBFill,
			symbol: "PAX", side: "buy", price: "250000000000000000", size: "1500000"})
		insertAction(t, db, action{block: 5, txIndex: -1, logIndex: 2, hash: common.HexToHash("0x01"), receiptKind: "block_event",
			emitter: common.HexToAddress("0x0000000000000000000000000000000000001020"), kind: "pricefeed_aggregate", action: ActionOracleUpdated,
			account: &zero, size: "18"})
		e := newExplorer(t, db, "")
		txs, err := e.Transactions(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		want := []Transaction{
			{Hash: txHash(5, 1).Hex(), Action: ActionOROBFill, Block: 5, Time: "2026-05-18T00:00:05.123Z", User: traderUser,
				Symbol: ptr("PAX"), Side: ptr("buy"), Price: ptr(0.25), Size: ptr(1.5)},
			{Hash: txHash(5, 0).Hex(), Action: ActionOROBFill, Block: 5, Time: "2026-05-18T00:00:05.123Z", User: traderUser,
				Symbol: ptr("BTC"), Side: ptr("sell"), Price: ptr(77175.2), Size: ptr(0.42)},
			{Hash: blockHash(5).Hex(), Action: ActionOracleUpdated, Block: 5, Time: "2026-05-18T00:00:05.123Z", User: zeroUser,
				Size: ptr(18.0)},
		}
		got, _ := json.Marshal(txs)
		exp, _ := json.Marshal(want)
		if string(got) != string(exp) {
			t.Fatalf("transactions = %s, want %s", got, exp)
		}

		insertAction(t, db, action{block: 6, kind: "OrderCancelled", action: ActionPlaceOrder})
		if _, err := e.Transactions(ctx, ""); !errors.Is(err, ErrActionMismatch) {
			t.Fatalf("mismatched stored action err = %v, want ErrActionMismatch", err)
		}
		reset(t, db)
		insertAction(t, db, action{block: 6, kind: "OrderExecuted", action: ActionOROBFill})
		if _, err := e.Transactions(ctx, ""); !errors.Is(err, ErrUnknownKind) {
			t.Fatalf("unmapped stored kind err = %v, want ErrUnknownKind", err)
		}
	})

	t.Run("user_proposer_synthetic", func(t *testing.T) {
		reset(t, db)
		insertBlock(t, db, 9, 2)
		height := make([]byte, 8)
		binary.BigEndian.PutUint64(height, 9)
		perpsReceipt := common.BytesToHash(crypto.Keccak256([]byte("perps-endblock"), height))
		spotReceipt := common.BytesToHash(crypto.Keccak256([]byte("spot-endblock"), height))
		insertAction(t, db, action{block: 9, txIndex: 1, logIndex: 0, account: &other, kind: "OrderPlaced", action: ActionPlaceOrder, symbol: "ETH", side: "buy"})
		insertAction(t, db, action{block: 9, txIndex: 2, logIndex: 1, hash: perpsReceipt, receiptKind: "perps_endblock",
			kind: "perps_endblock", action: ActionSealedBatchExecuted, account: &zero, size: "4"})
		insertAction(t, db, action{block: 9, txIndex: 3, logIndex: 5, hash: spotReceipt, receiptKind: "spot_endblock", emitter: spotAddress,
			kind: "spot_endblock", action: ActionSealedBatchExecuted, account: &zero, size: "2"})
		e := newExplorer(t, db, "https://explorer.example")
		txs, err := e.Transactions(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		wantHashes := []common.Hash{spotReceipt, perpsReceipt, txHash(9, 1)}
		wantUsers := []string{zeroUser, zeroUser, "pax1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqq8lppzhxv"}
		wantSizes := []float64{2, 4}
		if len(txs) != 3 {
			t.Fatalf("transactions = %+v, want 3 rows", txs)
		}
		for i, tx := range txs {
			if tx.Hash != wantHashes[i].Hex() || tx.User != wantUsers[i] || tx.Link != "https://explorer.example/tx/"+wantHashes[i].Hex() {
				t.Fatalf("row %d = %+v, want hash %s user %s", i, tx, wantHashes[i].Hex(), wantUsers[i])
			}
			if i < 2 && (tx.Action != ActionSealedBatchExecuted || tx.Size == nil || *tx.Size != wantSizes[i]) {
				t.Fatalf("row %d = %+v, want Sealed Batch Executed of size %v", i, tx, wantSizes[i])
			}
		}
		blocks, err := e.Blocks(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(blocks) != 1 || blocks[0].Proposer != "AB12CD3400000000000000000000000000000009" {
			t.Fatalf("blocks = %+v, want proposer AB12CD3400000000000000000000000000000009", blocks)
		}
	})

	t.Run("account_history_paging", func(t *testing.T) {
		reset(t, db)
		insertBlock(t, db, 10000, 1)
		for b := int64(1); b <= 5; b++ {
			insertDeposit(t, db, b, 0, 0, 0, trader, fmt.Sprint(b*100))
		}
		insertDeposit(t, db, 3, 0, 0, 1, trader, "301")
		insertDeposit(t, db, 4, 1, 0, 0, other, "999")
		insertDeposit(t, db, 10000, 0, 3, 0, trader, "10000")
		e := newExplorer(t, db, "https://explorer.example")

		type key struct {
			block           int64
			txIndex, logIdx int
			amount          string
		}
		keys := func(p Page[HistoryItem]) []key {
			out := []key{}
			for _, it := range p.Items {
				var args struct {
					Amount json.Number `json:"amount"`
				}
				if err := json.Unmarshal(it.Args, &args); err != nil {
					t.Fatalf("args %s: %v", it.Args, err)
				}
				if it.Kind != "margin" || it.Event != "deposited" || it.ReceiptKind != "tx" || it.Hash != txHash(it.Block, it.TxIndex).Hex() ||
					it.Link != "https://explorer.example/tx/"+it.Hash || it.Time != blockTime(it.Block).Format(isoMillis) {
					t.Fatalf("item = %+v", it)
				}
				out = append(out, key{it.Block, it.TxIndex, it.LogIndex, args.Amount.String()})
			}
			return out
		}
		pages := []struct {
			cursor string
			want   []key
			next   string
		}{
			{"", []key{{10000, 0, 3, "10000"}, {5, 0, 0, "500"}}, "5:0:0"},
			{"5:0:0", []key{{4, 0, 0, "400"}, {3, 0, 0, "301"}, {3, 0, 0, "300"}}, "3:0:0"},
			{"3:0:0", []key{{2, 0, 0, "200"}, {1, 0, 0, "100"}}, ""},
		}
		for _, p := range pages {
			page, err := e.AccountHistory(ctx, trader, p.cursor, "2")
			if err != nil {
				t.Fatalf("AccountHistory(%q): %v", p.cursor, err)
			}
			if got := keys(page); !slices.Equal(got, p.want) || page.NextCursor != p.next {
				t.Fatalf("AccountHistory(%q) = %v next %q, want %v next %q", p.cursor, got, page.NextCursor, p.want, p.next)
			}
		}
		all, err := e.AccountHistory(ctx, trader, "", "")
		if err != nil || len(all.Items) != 7 || all.NextCursor != "" {
			t.Fatalf("AccountHistory default = %d items next %q, %v; want 7 and no cursor", len(all.Items), all.NextCursor, err)
		}
		empty, err := e.AccountHistory(ctx, trader, "1:0:0", "")
		if err != nil || len(empty.Items) != 0 || empty.NextCursor != "" {
			t.Fatalf("AccountHistory below the oldest = %+v, %v; want empty", empty, err)
		}
		for _, bad := range []string{"5", "5:0", "a:0:0", "-1:0:0", "5:-2:0", "5:0:-1", "5:0:0:0"} {
			if _, err := e.AccountHistory(ctx, trader, bad, ""); !errors.Is(err, ErrCursor) {
				t.Fatalf("AccountHistory(%q) err = %v, want ErrCursor", bad, err)
			}
		}
		if _, err := e.AccountHistory(ctx, trader, "", "x"); !errors.Is(err, ErrLimit) {
			t.Fatalf("AccountHistory limit x err = %v, want ErrLimit", err)
		}
	})

	t.Run("market_history_paging", func(t *testing.T) {
		reset(t, db)
		fill := func(block int64, txIndex, logIndex, sub int, symbol, size string) {
			insertAction(t, db, action{block: block, txIndex: txIndex, logIndex: logIndex, sub: sub, kind: "Fill", action: ActionOROBFill,
				symbol: symbol, side: "buy", price: "1000000000000000000", size: size})
		}
		fill(1, 0, 0, 0, "BTC", "1000000000000000000")
		fill(1, 0, 0, 1, "BTC", "7000000000000000000")
		fill(2, 0, 0, 0, "BTC", "2000000000000000000")
		fill(2, 0, 0, 1, "BTC", "3000000000000000000")
		fill(2, 0, 0, 2, "BTC", "4000000000000000000")
		fill(3, 0, 1, 0, "ETH", "9000000000000000000")
		fill(4, 0, 0, 0, "BTC", "5000000000000000000")
		fill(4, 1, 0, 0, "BTC", "6000000000000000000")
		e := newExplorer(t, db, "")
		sizes := func(p Page[Transaction]) []float64 {
			out := []float64{}
			for _, tx := range p.Items {
				if tx.Symbol == nil || *tx.Symbol != "BTC" {
					t.Fatalf("market row %+v is not BTC", tx)
				}
				out = append(out, *tx.Size)
			}
			return out
		}
		pages := []struct {
			cursor, limit string
			want          []float64
			next          string
		}{
			{"", "3", []float64{6, 5, 4, 3, 2}, "2:0:0"},
			{"2:0:0", "3", []float64{7, 1}, ""},
			{"2:0:0", "1", []float64{7, 1}, ""},
			{"", "2", []float64{6, 5}, "4:0:0"},
			{"4:0:0", "1", []float64{4, 3, 2}, "2:0:0"},
			{"4:0:0", "2", []float64{4, 3, 2}, "2:0:0"},
			{"", "4", []float64{6, 5, 4, 3, 2}, "2:0:0"},
			{"", "5", []float64{6, 5, 4, 3, 2}, "2:0:0"},
			{"", "6", []float64{6, 5, 4, 3, 2, 7, 1}, ""},
			{"", "", []float64{6, 5, 4, 3, 2, 7, 1}, ""},
		}
		for _, p := range pages {
			page, err := e.MarketHistory(ctx, "BTC", p.cursor, p.limit)
			if err != nil {
				t.Fatalf("MarketHistory(%q, %q): %v", p.cursor, p.limit, err)
			}
			if got := sizes(page); !slices.Equal(got, p.want) || page.NextCursor != p.next {
				t.Fatalf("MarketHistory(%q, %q) = %v next %q, want %v next %q", p.cursor, p.limit, got, page.NextCursor, p.want, p.next)
			}
		}
		eth, err := e.MarketHistory(ctx, "ETH", "", "")
		if err != nil || len(eth.Items) != 1 || *eth.Items[0].Size != 9 || eth.NextCursor != "" {
			t.Fatalf("MarketHistory(ETH) = %+v, %v", eth, err)
		}
		none, err := e.MarketHistory(ctx, "SOL", "", "")
		if err != nil || len(none.Items) != 0 || none.NextCursor != "" {
			t.Fatalf("MarketHistory(SOL) = %+v, %v; want empty", none, err)
		}
		if _, err := e.MarketHistory(ctx, "BTC", "2:0", ""); !errors.Is(err, ErrCursor) {
			t.Fatalf("MarketHistory bad cursor err = %v, want ErrCursor", err)
		}
	})

	t.Run("tx_links", func(t *testing.T) {
		hash := common.HexToHash("0xabcdef")
		linked := newExplorer(t, db, "https://explorer.example/")
		if got, want := linked.TxLink(hash), "https://explorer.example/tx/0x0000000000000000000000000000000000000000000000000000000000abcdef"; got != want {
			t.Fatalf("TxLink = %q, want %q", got, want)
		}
		if got, want := linked.AddressLink(trader), "https://explorer.example/address/0x1111111111111111111111111111111111111111"; got != want {
			t.Fatalf("AddressLink = %q, want %q", got, want)
		}
		if got, want := linked.BlockLink(42), "https://explorer.example/block/42"; got != want {
			t.Fatalf("BlockLink = %q, want %q", got, want)
		}
		bare := newExplorer(t, db, "")
		if bare.TxLink(hash) != "" || bare.AddressLink(trader) != "" || bare.BlockLink(42) != "" {
			t.Fatal("links with an empty PERPS_EXPLORER_TX_BASE are not omitted")
		}

		reset(t, db)
		insertBlock(t, db, 7, 1)
		insertAction(t, db, action{block: 7, kind: "OrderPlaced", action: ActionPlaceOrder, symbol: "BTC", side: "buy"})
		for _, c := range []struct {
			e       *Explorer
			txLink  string
			blkLink string
		}{
			{linked, "https://explorer.example/tx/" + txHash(7, 0).Hex(), "https://explorer.example/block/7"},
			{bare, "", ""},
		} {
			txs, err := c.e.Transactions(ctx, "")
			if err != nil || len(txs) != 1 || txs[0].Link != c.txLink {
				t.Fatalf("transactions = %+v, %v; want link %q", txs, err, c.txLink)
			}
			blocks, err := c.e.Blocks(ctx, "")
			if err != nil || len(blocks) != 1 || blocks[0].Link != c.blkLink {
				t.Fatalf("blocks = %+v, %v; want link %q", blocks, err, c.blkLink)
			}
			for _, v := range []any{txs[0], blocks[0]} {
				raw, err := json.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				if has := strings.Contains(string(raw), `"link"`); has != (c.txLink != "") {
					t.Fatalf("%s: link field present = %v, want %v", raw, has, c.txLink != "")
				}
			}
		}
		if _, err := New(nil, ""); err == nil {
			t.Fatal("New(nil) succeeded")
		}
	})
}
