package portal

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/pnl"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

const explorerBase = "https://explorer.invalid/tx/"

var (
	pageTraderA = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	pageTraderB = common.HexToAddress("0x00000000000000000000000000000000000000b2")
	pageTraderC = common.HexToAddress("0x00000000000000000000000000000000000000e5")
	pageKeeper  = common.HexToAddress("0x00000000000000000000000000000000000000c3")
	pageVault   = common.HexToAddress("0x00000000000000000000000000000000000000d4")
)

const (
	pUSD1    = "1000000000000000000"
	pUSD5    = "5000000000000000000"
	pUSD7    = "7000000000000000000"
	pUSD98   = "98000000000000000000"
	pUSD500  = "500000000000000000000"
	pUSD1000 = "1000000000000000000000"
	pLoss    = "-100000000000000000000"
	pFunding = "-2000000000000000000"
	pLev10   = "10000000000000000000"
	pPx49000 = "49000000000000000000000"
	pPx50000 = "50000000000000000000000"
	pPx50350 = "50350000000000000000000"
	pPx51000 = "51000000000000000000000"
	pRate    = "1000000000"
	pRate24h = "86400000000000"
	pUSDL100 = "100000000"
	pUSDL1k  = "1000000000"
)

func pageAddr(a common.Address) string { return strings.ToLower(a.Hex()) }

func pageBlockHash(n uint64) common.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("portal/block/%d", n)))
}

func pageTxHash(n uint64, txIndex int64) common.Hash {
	return crypto.Keccak256Hash([]byte(fmt.Sprintf("portal/tx/%d/%d", n, txIndex)))
}

// explorerLink is the anchor the pages render for the transaction of
// transaction index txIndex in block n.
func explorerLink(n uint64, txIndex int64) string {
	h := pageTxHash(n, txIndex).Hex()
	return `<a href="` + explorerBase + h + `">` + h + `</a>`
}

// pageBlock collects the decoded logs of one block in log order.
type pageBlock struct {
	n   uint64
	at  time.Time
	log int64
	res decode.Result
}

// fact is the common part of the next log of transaction txIndex; endBlock
// marks the perps EndBlock receipt of the block.
func (b *pageBlock) fact(txIndex int64, endBlock bool, kind string, account common.Address) decode.Fact {
	f := decode.Fact{Block: b.n, BlockHash: pageBlockHash(b.n), TxIndex: txIndex, LogIndex: b.log, TxHash: pageTxHash(b.n, txIndex),
		ReceiptKind: decode.ReceiptTx, BlockTime: b.at, Emitter: decode.PerpsAddress, Kind: kind, Account: account}
	if endBlock {
		f.ReceiptKind = decode.ReceiptPerpsEndBlock
	} else {
		from := account
		f.TxFrom = &from
	}
	b.log++
	return f
}

func (b *pageBlock) add(name string, args map[string]any, row decode.Row) {
	f := row.Common()
	b.res.Decoded = append(b.res.Decoded, decode.Decoded{
		Event: decode.Event{EventName: name, Args: args, BlockNumber: f.Block, TxHash: f.TxHash.Hex(), LogIndex: uint(f.LogIndex)},
		Rows:  []decode.Row{row},
	})
}

// commitPageBlock commits b the way the follower does: the perps.blocks row,
// store.WriteBlock and the head cursor.
func commitPageBlock(t *testing.T, db *sql.DB, b *pageBlock, parent common.Hash) common.Hash {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	header := store.Block{Number: b.n, Hash: pageBlockHash(b.n), ParentHash: parent, Time: b.at, Proposer: strings.Repeat("AB", 20), TxCount: 3}
	if _, err := tx.ExecContext(ctx, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, $6)`,
		int64(header.Number), header.Hash.Bytes(), header.ParentHash.Bytes(), header.Time, header.Proposer, header.TxCount); err != nil {
		t.Fatalf("block row %d: %v", b.n, err)
	}
	if err := store.WriteBlock(ctx, tx, header, &b.res); err != nil {
		t.Fatalf("WriteBlock %d: %v", b.n, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO perps.cursors (name, height, hash, updated_at) VALUES ('head', $1, $2, now())
ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height, hash = EXCLUDED.hash, updated_at = now()`,
		int64(header.Number), header.Hash.Bytes()); err != nil {
		t.Fatalf("head cursor %d: %v", b.n, err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return header.Hash
}

// seedPages writes blocks 90, 100, 101 and 102 on BTC (market 0). Block 90,
// nine days ago, carries trader C's position 3 opened and closed at a 7 USD
// profit and the first BTC mark price. Block 100, 20 hours ago, carries trader
// A's vault, a 1000 USDL deposit, market order 7 filled at 50000 into long
// position 1, trader B's short position 2, and in its EndBlock the 50000 mark,
// the funding rate and the funding applied to position 1. Block 101, 2 hours
// ago, carries trader A's resting limit order 8 and in its EndBlock the 51000
// mark and the liquidation of position 2 at a 100 USD loss through a 500 USD
// buy fill at 51000, the last BTC fill. Block 102, a
// minute ago, is empty. The leaderboard is rebuilt as of now.
func seedPages(t *testing.T, db *sql.DB, now time.Time) (b100, b101, b102 time.Time) {
	t.Helper()
	b90, b100, b101, b102 := now.Add(-9*24*time.Hour), now.Add(-20*time.Hour), now.Add(-2*time.Hour), now.Add(-time.Minute)

	old := &pageBlock{n: 90, at: b90}
	f := old.fact(0, false, "opened", pageTraderC)
	old.add("PositionOpened", map[string]any{"positionId": "3"}, decode.PositionOpenedRow{Fact: f, PositionID: "3", User: pageTraderC, MarketID: "0",
		IsLong: true, SizeUSD: pUSD1000, Leverage: pLev10, EntryPrice: pPx50000, CollateralToken: store.USDL, CollateralAmount: pUSDL100})
	f = old.fact(0, false, "settled", pageTraderC)
	old.add("TradeSettled", map[string]any{"positionId": "3"}, decode.TradeSettledRow{Fact: f, PositionID: "3", User: pageTraderC, MarketID: "0",
		TradeType: 0, SizeUSD: pUSD1000, ExecutionPrice: pPx50000, GrossPnl: "0", TotalFeesUSD: "0", FundingPaidUSD: "0", NetPayoutTokens: "0"})
	f = old.fact(1, false, "closed", pageTraderC)
	old.add("PositionClosed", map[string]any{"positionId": "3"}, decode.PositionClosedRow{Fact: f, PositionID: "3", User: pageTraderC, MarketID: "0",
		ClosedSizeUSD: pUSD1000, ExitPrice: pPx50350, RealizedPnl: pUSD7, IsFullClose: true})
	f = old.fact(1, false, "settled", pageTraderC)
	old.add("TradeSettled", map[string]any{"positionId": "3"}, decode.TradeSettledRow{Fact: f, PositionID: "3", User: pageTraderC, MarketID: "0",
		TradeType: 1, SizeUSD: pUSD1000, ExecutionPrice: pPx50350, GrossPnl: pUSD7, TotalFeesUSD: "0", FundingPaidUSD: "0", NetPayoutTokens: "107000000"})
	f = old.fact(2, true, "mark", decode.PerpsAddress)
	old.add("MarkPriceUpdated", map[string]any{"marketId": "0"}, decode.MarkPriceUpdatedRow{Fact: f, MarketID: "0", MarkPrice: pPx50000,
		IndexPrice: pPx50000, MidPrice: pPx50000})

	one := &pageBlock{n: 100, at: b100}
	f = one.fact(0, false, "created", pageTraderA)
	one.add("VaultCreated", map[string]any{"user": pageAddr(pageTraderA), "vault": pageAddr(pageVault)}, decode.VaultCreatedRow{Fact: f, User: pageTraderA, Vault: pageVault})
	f = one.fact(0, false, "deposited", pageTraderA)
	one.add("CollateralDeposited", map[string]any{"amount": pUSDL1k}, decode.CollateralDepositedRow{Fact: f, AccountArg: pageTraderA, Amount: pUSDL1k, NewBalance: pUSDL1k})
	f = one.fact(0, false, "placed", pageTraderA)
	one.add("OrderPlaced", map[string]any{"orderId": "7"}, decode.OrderPlacedRow{Fact: f, OrderID: "7", User: pageTraderA, MarketID: "0", OrderType: 4,
		IsLong: true, TriggerPrice: "0", SizeUSD: pUSD1000})
	f = one.fact(0, false, "fill", pageTraderA)
	one.add("Fill", map[string]any{"takerOrderId": "7"}, decode.FillRow{Fact: f, MarketID: "0", MakerOrderID: "0", TakerOrderID: "7", Maker: pageTraderB,
		Taker: pageTraderA, TakerIsLong: true, Price: pPx50000, SizeUSD: pUSD1000, MakerFeeUSD: "0", TakerFeeUSD: "0"})
	f = one.fact(0, false, "opened", pageTraderA)
	one.add("PositionOpened", map[string]any{"positionId": "1"}, decode.PositionOpenedRow{Fact: f, PositionID: "1", User: pageTraderA, MarketID: "0",
		IsLong: true, SizeUSD: pUSD1000, Leverage: pLev10, EntryPrice: pPx50000, CollateralToken: store.USDL, CollateralAmount: pUSDL100})
	f = one.fact(0, false, "settled", pageTraderA)
	one.add("TradeSettled", map[string]any{"positionId": "1"}, decode.TradeSettledRow{Fact: f, PositionID: "1", User: pageTraderA, MarketID: "0",
		TradeType: 0, SizeUSD: pUSD1000, ExecutionPrice: pPx50000, GrossPnl: "0", TotalFeesUSD: pUSD1, FundingPaidUSD: "0", NetPayoutTokens: "0"})
	f = one.fact(1, false, "opened", pageTraderB)
	one.add("PositionOpened", map[string]any{"positionId": "2"}, decode.PositionOpenedRow{Fact: f, PositionID: "2", User: pageTraderB, MarketID: "0",
		IsLong: false, SizeUSD: pUSD500, Leverage: pLev10, EntryPrice: pPx50000, CollateralToken: store.USDL, CollateralAmount: "50000000"})
	f = one.fact(2, true, "mark", decode.PerpsAddress)
	one.add("MarkPriceUpdated", map[string]any{"marketId": "0"}, decode.MarkPriceUpdatedRow{Fact: f, MarketID: "0", MarkPrice: pPx50000,
		IndexPrice: pPx50000, MidPrice: pPx50000})
	f = one.fact(2, true, "rate", decode.PerpsAddress)
	one.add("FundingRateUpdated", map[string]any{"marketId": "0"}, decode.FundingRateUpdatedRow{Fact: f, MarketID: "0", NewRatePerSecond: pRate,
		FundingRate24h: pRate24h})
	f = one.fact(2, true, "applied", pageTraderA)
	one.add("PositionFundingApplied", map[string]any{"positionId": "1"}, decode.PositionFundingAppliedRow{Fact: f, PositionID: "1", User: pageTraderA,
		MarketID: "0", FundingPaymentUSD: pFunding, NewCollateralUSD: pUSD98, NewCollateralAmount: "98000000"})

	two := &pageBlock{n: 101, at: b101}
	f = two.fact(0, false, "placed", pageTraderA)
	two.add("OrderPlaced", map[string]any{"orderId": "8"}, decode.OrderPlacedRow{Fact: f, OrderID: "8", User: pageTraderA, MarketID: "0", OrderType: 0,
		IsLong: true, TriggerPrice: pPx49000, SizeUSD: pUSD500})
	f = two.fact(1, true, "mark", decode.PerpsAddress)
	two.add("MarkPriceUpdated", map[string]any{"marketId": "0"}, decode.MarkPriceUpdatedRow{Fact: f, MarketID: "0", MarkPrice: pPx51000,
		IndexPrice: pPx51000, MidPrice: pPx51000})
	f = two.fact(1, true, "fill", pageTraderB)
	two.add("Fill", map[string]any{"takerOrderId": "0"}, decode.FillRow{Fact: f, MarketID: "0", MakerOrderID: "0", TakerOrderID: "0",
		Maker: decode.PerpsAddress, Taker: pageTraderB, TakerIsLong: true, Price: pPx51000, SizeUSD: pUSD500, MakerFeeUSD: "0", TakerFeeUSD: "0"})
	f = two.fact(1, true, "closed", pageTraderB)
	two.add("PositionClosed", map[string]any{"positionId": "2"}, decode.PositionClosedRow{Fact: f, PositionID: "2", User: pageTraderB, MarketID: "0",
		ClosedSizeUSD: pUSD500, ExitPrice: pPx51000, RealizedPnl: pLoss, IsFullClose: true})
	f = two.fact(1, true, "liquidated", pageTraderB)
	two.add("Liquidation", map[string]any{"positionId": "2"}, decode.LiquidationRow{Fact: f, PositionID: "2", User: pageTraderB, MarketID: "0",
		LiquidationPrice: pPx51000, Penalty: pUSD5, Keeper: pageKeeper})

	three := &pageBlock{n: 102, at: b102}

	parent := common.Hash{}
	for _, b := range []*pageBlock{old, one, two, three} {
		parent = commitPageBlock(t, db, b, parent)
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := (pnl.Leaderboard{}).Rebuild(ctx, tx, now); err != nil {
		t.Fatalf("leaderboard: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return b100, b101, b102
}

// recorder forwards every request to the gateway and keeps its request URI,
// so a subtest can name exactly what a page read.
type recorder struct {
	mu   sync.Mutex
	uris []string
}

func (r *recorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.uris
	r.uris = nil
	return out
}

func recordingGateway(t *testing.T, gatewayURL string) (*httptest.Server, *recorder) {
	t.Helper()
	target, err := url.Parse(gatewayURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	rec := &recorder{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.uris = append(rec.uris, r.Method+" "+r.URL.RequestURI())
		rec.mu.Unlock()
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts, rec
}

// section is the text of the section element with id, or "" when the page has
// none.
func section(body, id string) string {
	start := strings.Index(body, `<section id="`+id+`">`)
	if start < 0 {
		return ""
	}
	end := strings.Index(body[start:], "</section>")
	if end < 0 {
		return ""
	}
	return body[start : start+end]
}

func page(t *testing.T, url string) string {
	t.Helper()
	a := get(t, url)
	if a.status != http.StatusOK {
		t.Fatalf("%s answered %d: %s", url, a.status, a.body)
	}
	if got := a.header.Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("%s Content-Type %q", url, got)
	}
	return a.body
}

func contains(t *testing.T, where, text string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("%s lacks %q:\n%s", where, want, text)
		}
	}
}

func hour(at time.Time) string { return at.UTC().Truncate(time.Hour).Format(time.RFC3339) }

func TestPortalPages(t *testing.T) {
	db := serverDB(t)
	now := time.Now()
	b100, b101, b102 := seedPages(t, db, now)
	gw := gatewayServer(t, db)
	proxy, rec := recordingGateway(t, gw.URL)
	portal := portalServer(t, proxy.URL)

	t.Run("page_home", func(t *testing.T) {
		rec.take()
		body := page(t, portal.URL+"/")
		markets := section(body, "markets")
		row := markets[strings.Index(markets, `<tr><td><a href="/markets/btc">BTC</a></td>`):]
		row = row[:strings.Index(row, "</tr>")]
		// price and mark 51000, change (51000 - 50000) / 50000 = 0.02 from
		// the first to the last fill, volume 1000 + 500 = 1500, open interest
		// 0, funding 8h 1e9 x 28800 / 1e18, two fills.
		contains(t, "BTC row", row, "<td>51000</td><td>51000</td><td>0.02</td><td>1500</td><td>0</td><td>0.0000288</td><td>2</td>")
		contains(t, "markets", markets, `<a href="/markets/eth">ETH</a>`)
		contains(t, "totals", section(body, "totals"),
			"<tr><th>Trades</th><td>2</td></tr>", "<tr><th>Volume 24h (USD)</th><td>1500</td></tr>",
			"<tr><th>Volume 7d (USD)</th><td>1500</td></tr>", "<tr><th>Volume 30d (USD)</th><td>1500</td></tr>",
			"<tr><th>Open interest (USD)</th><td>0</td></tr>")
		if strings.Contains(body, explorerBase) {
			t.Errorf("home page links a transaction although no market row carries one")
		}
		if got := strings.Join(rec.take(), ","); got != "GET /api/markets,GET /api/totals" {
			t.Errorf("home page read %s, want GET /api/markets,GET /api/totals", got)
		}
	})

	t.Run("page_market", func(t *testing.T) {
		rec.take()
		body := page(t, portal.URL+"/markets/btc")
		contains(t, "record", section(body, "record"), "<tr><th>Symbol</th><td>BTC</td></tr>",
			"<tr><th>Mark price</th><td>51000</td></tr>", "<tr><th>Index price</th><td>51000</td></tr>",
			"<tr><th>Volume 24h (USD)</th><td>1500</td></tr>", "<tr><th>Last settled funding 1h</th><td>-</td></tr>",
			"<tr><th>Trades</th><td>2</td></tr>")
		fill := fmt.Sprintf("<tr><td>100-3</td><td>BUY</td><td>50000</td><td>0.02</td><td>1000</td><td>no</td><td>%s</td></tr>",
			b100.UTC().Format(time.RFC3339))
		contains(t, "fills", section(body, "fills"), fill)
		contains(t, "funding", section(body, "funding"), fmt.Sprintf("<tr><td>100</td><td>0.000000001</td><td>0.0000864</td><td>%s</td><td>%s</td></tr>",
			b100.UTC().Format(time.RFC3339), explorerLink(100, 2)))
		snapshots := section(body, "snapshots")
		// The hour a minute ago or earlier follows the 51000 mark of block
		// 101 and sees both fills, 1000 + 500 USD, in its 24 hours; the hour
		// after block 100 follows its 50000 mark and sees only the 1000 USD
		// fill.
		contains(t, "snapshots", snapshots,
			"<tr><td>"+hour(b102)+"</td><td>51000</td><td>51000</td><td>1500</td></tr>",
			"<tr><td>"+hour(b100.Add(time.Hour))+"</td><td>50000</td><td>50000</td><td>1000</td></tr>")
		if eight := hour(now.Add(-8 * 24 * time.Hour)); strings.Contains(snapshots, "<tr><td>"+eight+"</td>") {
			t.Errorf("snapshots show hour %s, older than 7 days", eight)
		}
		if rows := strings.Count(snapshots, "<tr><td>"); rows < 167 || rows > 168 {
			t.Errorf("snapshots show %d hourly rows, want the 167 or 168 hours of the last 7 days", rows)
		}
		want := "GET /api/markets/BTC,GET /api/btc/perp_trades?limit=100,POST /graphql,POST /graphql"
		if got := strings.Join(rec.take(), ","); got != want {
			t.Errorf("market page read %s, want %s and no candle route", got, want)
		}
	})

	t.Run("page_account", func(t *testing.T) {
		body := page(t, portal.URL+"/accounts/"+pageAddr(pageTraderA))
		contains(t, "positions", section(body, "positions"),
			`<tr><td>1</td><td><a href="/markets/btc">BTC</a></td><td>long</td><td>1000</td><td>10</td><td>50000</td>`,
			explorerLink(100, 0))
		contains(t, "orders", section(body, "orders"), fmt.Sprintf(
			"<tr><td>8</td><td>0</td><td>limit</td><td>long</td><td>49000</td><td>500</td><td>active</td><td>%s</td><td>%s</td></tr>",
			b101.UTC().Format(time.RFC3339), explorerLink(101, 0)))
		contains(t, "fills", section(body, "fills"), fmt.Sprintf(
			"<tr><td>1</td><td>0</td><td>open</td><td>1000</td><td>50000</td><td>0</td><td>1</td><td>0</td><td>%s</td><td>%s</td></tr>",
			b100.UTC().Format(time.RFC3339), explorerLink(100, 0)))
		contains(t, "funding", section(body, "funding"), fmt.Sprintf("<tr><td>1</td><td>0</td><td>-2</td><td>98</td><td>%s</td><td>%s</td></tr>",
			b100.UTC().Format(time.RFC3339), explorerLink(100, 2)))
		contains(t, "collateral", section(body, "collateral"), fmt.Sprintf("<tr><td>deposited</td><td>%s</td><td>1000</td><td>100</td><td>%s</td><td>%s</td></tr>",
			pageAddr(store.USDL), b100.UTC().Format(time.RFC3339), explorerLink(100, 0)))
		contains(t, "liquidations", section(body, "liquidations"), "No liquidations.")

		body = page(t, portal.URL+"/accounts/"+pageAddr(pageTraderB))
		contains(t, "liquidations of B", section(body, "liquidations"), fmt.Sprintf(
			"<tr><td>2</td><td>0</td><td>51000</td><td>5</td><td>%s</td><td>%s</td><td>%s</td></tr>",
			pageAddr(pageKeeper), b101.UTC().Format(time.RFC3339), explorerLink(101, 1)))
		contains(t, "positions of B", section(body, "positions"), "No open positions.")

		body = page(t, portal.URL+"/accounts/"+seededAccount)
		for id, empty := range map[string]string{"positions": "No open positions.", "orders": "No active orders.", "fills": "No fills.",
			"funding": "No funding payments.", "liquidations": "No liquidations.", "collateral": "No collateral events."} {
			contains(t, "empty "+id, section(body, id), empty)
		}
	})

	t.Run("page_leaderboard", func(t *testing.T) {
		rec.take()
		body := page(t, portal.URL+"/leaderboard")
		rowOf := func(rank int, a common.Address, pnl, volume string, trades int, winRate string) string {
			return fmt.Sprintf(`<tr><td>%d</td><td><a href="/accounts/%s">%s</a></td><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>`,
				rank, pageAddr(a), pageAddr(a), pnl, volume, trades, winRate)
		}
		c := rowOf(1, pageTraderC, "7", "2000", 2, "100")
		for _, window := range []string{"all", "30d"} {
			contains(t, "window "+window, section(body, "window-"+window), c, rowOf(2, pageTraderA, "0", "1000", 1, "0"),
				rowOf(3, pageTraderB, "-100", "0", 0, "0"))
		}
		seven := section(body, "window-7d")
		contains(t, "window 7d", seven, rowOf(1, pageTraderA, "0", "1000", 1, "0"), rowOf(2, pageTraderB, "-100", "0", 0, "0"))
		if strings.Contains(seven, pageAddr(pageTraderC)) {
			t.Errorf("window 7d ranks trader C, whose only close is nine days old:\n%s", seven)
		}
		want := "POST /graphql,GET /v2/leaderboard?limit=100&period=30d,GET /v2/leaderboard?limit=100&period=7d"
		if got := strings.Join(rec.take(), ","); got != want {
			t.Errorf("leaderboard read %s, want %s", got, want)
		}
	})

	t.Run("page_stats", func(t *testing.T) {
		var markets, events int64
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM perps.markets), (SELECT count(*) FROM perps.protocol_events)`).Scan(&markets, &events); err != nil {
			t.Fatal(err)
		}
		body := page(t, portal.URL+"/stats")
		contains(t, "global", section(body, "global"), fmt.Sprintf("<tr><th>Markets</th><td>%d</td></tr>", markets),
			"<tr><th>Positions</th><td>3</td></tr>", "<tr><th>Open positions</th><td>1</td></tr>",
			"<tr><th>Trades</th><td>2</td></tr>", "<tr><th>Liquidations</th><td>1</td></tr>",
			"<tr><th>Volume (USD)</th><td>1500</td></tr>", "<tr><th>Traders</th><td>3</td></tr>")
		contains(t, "protocol snapshots", section(body, "protocol-snapshots"),
			fmt.Sprintf("<tr><td>%s</td><td>102</td><td>3</td><td>1</td><td>%d</td><td>0</td><td>0</td></tr>", hour(b102), markets))
		contains(t, "indexer", section(body, "indexer"), "<tr><th>Last indexed block</th><td>102</td></tr>",
			"<tr><th>Chain head</th><td>unknown</td></tr>", "<tr><th>Blocks scanned</th><td>4</td></tr>",
			fmt.Sprintf("<tr><th>Events processed</th><td>%d</td></tr>", events), "<tr><th>Synced</th><td>unknown</td></tr>")
	})
}
