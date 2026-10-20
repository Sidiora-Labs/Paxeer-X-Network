package pnl_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/pnl"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

const testDatabaseEnv = "PERPS_DATA_TEST_DATABASE_URL"

var (
	perpsAddress = common.HexToAddress("0x0000000000000000000000000000000000001015")
	usdlAddress  = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	tokenAddress = common.HexToAddress("0x00000000000000000000000000000000000000c1")
	accountA1    = common.HexToAddress("0x00000000000000000000000000000000000000a1")
	accountA2    = common.HexToAddress("0x00000000000000000000000000000000000000a2")
	accountA3    = common.HexToAddress("0x00000000000000000000000000000000000000a3")
	baseTime     = time.Date(2026, time.March, 2, 12, 0, 0, 0, time.UTC)
)

// e18 returns n x 1e18 as a decimal string.
func e18(n int64) string {
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("%d000000000000000000", n)
}

type factKey struct {
	block    int64
	txIndex  int
	logIndex int
	sub      int
}

func (k factKey) txHash() []byte {
	var b [16]byte
	binary.BigEndian.PutUint64(b[:8], uint64(k.block))
	binary.BigEndian.PutUint64(b[8:], uint64(k.txIndex))
	return crypto.Keccak256(b[:])
}

func blockHash(block int64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], uint64(block))
	return crypto.Keccak256([]byte("block"), b[:])
}

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv(testDatabaseEnv)
	if url == "" {
		t.Fatal("TestPnl requires " + testDatabaseEnv)
	}
	db, err := store.Open(url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func begin(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			t.Errorf("rollback: %v", err)
		}
	})
	return tx
}

// insertFact writes one fact row with the common columns of
// contracts/data-services.md section 2 and the given argument columns.
func insertFact(t *testing.T, tx *sql.Tx, table string, k factKey, at time.Time, kind string, account common.Address, args map[string]any) {
	t.Helper()
	cols := []string{"block", "block_hash", "tx_index", "log_index", "sub", "tx_hash", "receipt_kind", "block_time", "emitter", "kind", "account", "tx_from"}
	vals := []any{k.block, blockHash(k.block), k.txIndex, k.logIndex, k.sub, k.txHash(), "tx", at, perpsAddress.Bytes(), kind, account.Bytes(), account.Bytes()}
	names := make([]string, 0, len(args))
	for name := range args {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		cols = append(cols, `"`+name+`"`)
		vals = append(vals, args[name])
	}
	marks := make([]string, len(cols))
	for i := range marks {
		marks[i] = fmt.Sprintf("$%d", i+1)
	}
	query := fmt.Sprintf("INSERT INTO perps.%s (%s) VALUES (%s)", table, strings.Join(cols, ", "), strings.Join(marks, ", "))
	if _, err := tx.Exec(query, vals...); err != nil {
		t.Fatalf("insert %s %+v: %v", table, k, err)
	}
}

func opened(t *testing.T, tx *sql.Tx, k factKey, at time.Time, account common.Address, positionID int64, isLong bool, sizeUsd, entryPrice string) {
	t.Helper()
	insertFact(t, tx, "position_events", k, at, "opened", account, map[string]any{
		"position_id": positionID, "user": account.Bytes(), "market_id": 0, "is_long": isLong, "size_usd": sizeUsd,
		"leverage": e18(10), "entry_price": entryPrice, "collateral_token": usdlAddress.Bytes(), "collateral_amount": "100000000",
	})
}

func closed(t *testing.T, tx *sql.Tx, k factKey, at time.Time, account common.Address, positionID int64, closedSizeUsd, exitPrice, realizedPnl string, full bool) {
	t.Helper()
	insertFact(t, tx, "position_events", k, at, "closed", account, map[string]any{
		"position_id": positionID, "user": account.Bytes(), "market_id": 0, "closed_size_usd": closedSizeUsd,
		"exit_price": exitPrice, "realized_pnl": realizedPnl, "is_full_close": full,
	})
}

func settled(t *testing.T, tx *sql.Tx, k factKey, at time.Time, account common.Address, positionID int64, tradeType int, sizeUsd, grossPnl, fees, funding string) {
	t.Helper()
	insertFact(t, tx, "trade_settlements", k, at, "settled", account, map[string]any{
		"position_id": positionID, "user": account.Bytes(), "market_id": 0, "trade_type": tradeType, "size_usd": sizeUsd,
		"execution_price": e18(100), "gross_pnl": grossPnl, "total_fees_usd": fees, "funding_paid_usd": funding, "net_payout_tokens": "0",
	})
}

type leaderboardRow struct {
	rank          int
	account       common.Address
	realizedPnl   string
	volume        string
	trades        int64
	winRate       string
	avgHold       int64
	openPositions int
}

func leaderboard(t *testing.T, tx *sql.Tx, window pnl.Window) []leaderboardRow {
	t.Helper()
	rows, err := tx.Query(`SELECT rank, account, trunc(realized_pnl)::text, trunc(volume_usd)::text, trades_count,
        win_rate::text, avg_hold_time_seconds, open_positions FROM perps.leaderboard WHERE "window" = $1 ORDER BY rank`, string(window))
	if err != nil {
		t.Fatalf("read leaderboard: %v", err)
	}
	defer rows.Close()
	var out []leaderboardRow
	for rows.Next() {
		var r leaderboardRow
		var account []byte
		if err := rows.Scan(&r.rank, &account, &r.realizedPnl, &r.volume, &r.trades, &r.winRate, &r.avgHold, &r.openPositions); err != nil {
			t.Fatalf("scan leaderboard: %v", err)
		}
		r.account = common.BytesToAddress(account)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read leaderboard: %v", err)
	}
	return out
}

func rebuild(t *testing.T, tx *sql.Tx, asOf time.Time) {
	t.Helper()
	if err := (pnl.Leaderboard{}).Rebuild(context.Background(), tx, asOf); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
}

func TestPnl(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()

	t.Run("realised_from_close", func(t *testing.T) {
		tx := begin(t, db)
		// Long 2 at 100 is 200 USD notional; closing all 2 at 110 is 220 USD.
		opened(t, tx, factKey{1, 0, 0, 0}, baseTime, accountA1, 7, true, e18(200), e18(100))
		closed(t, tx, factKey{2, 0, 0, 0}, baseTime.Add(90*time.Second), accountA1, 7, e18(220), e18(110), e18(20), true)

		realized, err := pnl.RealizedOnClose(true, e18(100), e18(110), e18(220))
		if err != nil || realized != e18(20) {
			t.Fatalf("RealizedOnClose long = %q, %v; want %s", realized, err, e18(20))
		}
		var stored string
		if err := tx.QueryRow(`SELECT trunc(realized_pnl)::text FROM perps.position_events WHERE kind = 'closed' AND position_id = 7`).Scan(&stored); err != nil {
			t.Fatalf("read close: %v", err)
		}
		if stored != realized {
			t.Fatalf("PositionClosed realizedPnl %s, computed %s", stored, realized)
		}
		short, err := pnl.RealizedOnClose(false, e18(100), e18(110), e18(220))
		if err != nil || short != "-"+e18(20) {
			t.Fatalf("RealizedOnClose short = %q, %v; want -%s", short, err, e18(20))
		}
		// The contract example: 1006.5 USD closed at 61,000 from 60,000 realises 16.5 USD.
		example, err := pnl.RealizedOnClose(true, e18(60000), e18(61000), "1006500000000000000000")
		if err != nil || example != "16500000000000000000" {
			t.Fatalf("RealizedOnClose example = %q, %v; want 16500000000000000000", example, err)
		}
		if _, err := pnl.RealizedOnClose(true, e18(100), "0", e18(220)); !errors.Is(err, pnl.ErrZeroExitPrice) {
			t.Fatalf("zero exit price error %v, want ErrZeroExitPrice", err)
		}
		if _, err := pnl.RealizedOnClose(true, "-1", e18(110), e18(220)); !errors.Is(err, bigint.ErrInvalidRaw) {
			t.Fatalf("signed entry price error %v, want bigint.ErrInvalidRaw", err)
		}

		rebuild(t, tx, baseTime.Add(time.Hour))
		got := leaderboard(t, tx, pnl.WindowAll)
		want := []leaderboardRow{{1, accountA1, e18(20), "0", 0, "100.0000", 90, 0}}
		if !slices.Equal(got, want) {
			t.Fatalf("leaderboard all = %+v, want %+v", got, want)
		}
	})

	t.Run("settlement_components", func(t *testing.T) {
		tx := begin(t, db)
		day := baseTime.Truncate(24 * time.Hour)
		settled(t, tx, factKey{10, 0, 0, 0}, day.Add(time.Hour), accountA1, 7, 0, "1000", "10", "2", "-1")
		settled(t, tx, factKey{11, 0, 0, 0}, day.Add(5*time.Hour), accountA1, 7, 3, "500", "-4", "1", "0")
		settled(t, tx, factKey{12, 0, 0, 0}, day.Add(25*time.Hour), accountA1, 7, 1, "700", "9", "1", "1")
		settled(t, tx, factKey{12, 1, 0, 0}, day.Add(25*time.Hour), accountA2, 8, 0, "900", "0", "1", "0")

		d, err := pnl.RebuildDaily(ctx, tx, accountA1, day.Add(3*time.Hour))
		if err != nil {
			t.Fatalf("RebuildDaily: %v", err)
		}
		want := pnl.Daily{TradingPnl: "6", Fees: "3", Funding: "-1", RealizedPnl: "2", Volume: "1500", Trades: 2}
		if d != want {
			t.Fatalf("RebuildDaily = %+v, want %+v", d, want)
		}
		var got pnl.Daily
		var date string
		if err := tx.QueryRow(`SELECT to_char(day, 'YYYY-MM-DD'), trunc(trading_pnl)::text, trunc(fees)::text, trunc(funding)::text,
            trunc(realized_pnl)::text, trunc(volume)::text, trades FROM perps.user_pnl_daily WHERE account = $1`, accountA1.Bytes()).
			Scan(&date, &got.TradingPnl, &got.Fees, &got.Funding, &got.RealizedPnl, &got.Volume, &got.Trades); err != nil {
			t.Fatalf("read user_pnl_daily: %v", err)
		}
		if got != want || date != day.Format(time.DateOnly) {
			t.Fatalf("user_pnl_daily %s %+v, want %s %+v", date, got, day.Format(time.DateOnly), want)
		}

		if _, err := tx.Exec(`DELETE FROM perps.trade_settlements WHERE account = $1 AND block IN (10, 11)`, accountA1.Bytes()); err != nil {
			t.Fatalf("delete settlements: %v", err)
		}
		if d, err := pnl.RebuildDaily(ctx, tx, accountA1, day); err != nil || d.Trades != 0 {
			t.Fatalf("RebuildDaily after rewind = %+v, %v; want no trades", d, err)
		}
		var rows int
		if err := tx.QueryRow(`SELECT count(*) FROM perps.user_pnl_daily WHERE account = $1`, accountA1.Bytes()).Scan(&rows); err != nil || rows != 0 {
			t.Fatalf("user_pnl_daily rows after rewind = %d, %v; want 0", rows, err)
		}

		if _, err := pnl.FoldSettlement(pnl.Daily{}, pnl.Settlement{SizeUsd: bigint.Uint256Max, GrossPnl: "0", TotalFeesUsd: "0", FundingPaidUsd: "0"}); err != nil {
			t.Fatalf("FoldSettlement at the ceiling: %v", err)
		}
		if _, err := pnl.FoldSettlement(pnl.Daily{Volume: bigint.Uint256Max}, pnl.Settlement{SizeUsd: "1", GrossPnl: "0", TotalFeesUsd: "0", FundingPaidUsd: "0"}); !errors.Is(err, bigint.ErrOverflow) {
			t.Fatalf("volume overflow error %v, want bigint.ErrOverflow", err)
		}
		if _, err := pnl.FoldSettlement(pnl.Daily{TradingPnl: bigint.Int256Max}, pnl.Settlement{SizeUsd: "1", GrossPnl: "1", TotalFeesUsd: "0", FundingPaidUsd: "0"}); !errors.Is(err, bigint.ErrOverflow) {
			t.Fatalf("trading overflow error %v, want bigint.ErrOverflow", err)
		}
		if _, err := pnl.FoldSettlement(pnl.Daily{}, pnl.Settlement{SizeUsd: "1", GrossPnl: "1.5", TotalFeesUsd: "0", FundingPaidUsd: "0"}); !errors.Is(err, bigint.ErrInvalidRaw) {
			t.Fatalf("fractional gross error %v, want bigint.ErrInvalidRaw", err)
		}
	})

	t.Run("spot_average_cost", func(t *testing.T) {
		tx := begin(t, db)
		buy := func(k factKey, usdl, tok string) {
			insertFact(t, tx, "spot_swaps", k, baseTime.Add(time.Duration(k.block)*time.Second), "market_order", accountA1, map[string]any{
				"user": accountA1.Bytes(), "stablecoin": usdlAddress.Bytes(), "token": tokenAddress.Bytes(),
				"stablecoin_amount": usdl, "token_amount": tok, "is_buy": true,
			})
		}
		// Buys of 1 at 10 and 1 at 20, then a sell of 1 at 30 through SimpleSwap.
		buy(factKey{20, 0, 0, 0}, "10000000", e18(1))
		buy(factKey{21, 0, 0, 0}, "20000000", e18(1))
		insertFact(t, tx, "spot_swaps", factKey{22, 0, 0, 0}, baseTime.Add(22*time.Second), "simple_swap", accountA1, map[string]any{
			"user": accountA1.Bytes(), "token_in": tokenAddress.Bytes(), "token_out": usdlAddress.Bytes(),
			"amount_in": e18(1), "amount_out": "30000000", "execution_price": e18(30),
		})
		// Another account's swap is not folded.
		insertFact(t, tx, "spot_swaps", factKey{22, 1, 0, 0}, baseTime.Add(22*time.Second), "market_order", accountA2, map[string]any{
			"user": accountA2.Bytes(), "stablecoin": usdlAddress.Bytes(), "token": tokenAddress.Bytes(),
			"stablecoin_amount": "5000000", "token_amount": e18(1), "is_buy": true,
		})

		pos, err := pnl.SpotPosition(ctx, tx, accountA1, tokenAddress, usdlAddress)
		if err != nil {
			t.Fatalf("SpotPosition: %v", err)
		}
		first := baseTime.Add(20 * time.Second).Unix()
		want := pnl.Position{
			TotalUsdlSpent: "30000000", TotalTokensBought: e18(2), TotalUsdlReceived: "30000000", TotalTokensSold: e18(1),
			AvgCostBasis: "15000000", CurrentHoldings: e18(1), RealizedPnlUsdl: "15000000",
			FirstBuyTs: &first, LastTradeTs: baseTime.Add(22 * time.Second).Unix(), TradeCount: 3,
		}
		if pos.FirstBuyTs == nil || *pos.FirstBuyTs != first {
			t.Fatalf("FirstBuyTs = %v, want %d", pos.FirstBuyTs, first)
		}
		pos.FirstBuyTs = want.FirstBuyTs
		if pos != want {
			t.Fatalf("SpotPosition = %+v, want %+v", pos, want)
		}
		total, err := pnl.TotalPnl(pos.RealizedPnlUsdl, pos.AvgCostBasis, pos.CurrentHoldings, "40000000")
		if err != nil || total != "40000000" {
			t.Fatalf("TotalPnl = %q, %v; want 40000000", total, err)
		}
		if _, err := pnl.TotalPnl(pos.RealizedPnlUsdl, pos.AvgCostBasis, pos.CurrentHoldings, ""); err == nil {
			t.Fatal("TotalPnl without a mark succeeded for a non-empty inventory")
		}
		if _, err := pnl.Fold(pos, pnl.Trade{IsBuy: false, UsdlAmount: "1", TokenAmount: e18(2)}); err == nil || !strings.Contains(err.Error(), "oversell") {
			t.Fatalf("oversell error %v, want the oversell refusal", err)
		}
	})

	t.Run("win_rate", func(t *testing.T) {
		tx := begin(t, db)
		opened(t, tx, factKey{30, 0, 0, 0}, baseTime, accountA1, 1, true, e18(100), e18(100))
		closed(t, tx, factKey{31, 0, 0, 0}, baseTime.Add(10*time.Second), accountA1, 1, e18(10), e18(100), e18(10), false)
		closed(t, tx, factKey{32, 0, 0, 0}, baseTime.Add(20*time.Second), accountA1, 1, e18(10), e18(100), "-"+e18(5), false)
		closed(t, tx, factKey{33, 0, 0, 0}, baseTime.Add(30*time.Second), accountA1, 1, e18(10), e18(100), e18(7), false)
		// A close at exactly 0 is neither a win nor a loss.
		closed(t, tx, factKey{34, 0, 0, 0}, baseTime.Add(40*time.Second), accountA1, 1, e18(70), e18(100), "0", true)
		settled(t, tx, factKey{30, 0, 1, 0}, baseTime, accountA1, 1, 0, e18(100), "0", "1", "0")

		rebuild(t, tx, baseTime.Add(time.Hour))
		got := leaderboard(t, tx, pnl.WindowAll)
		want := []leaderboardRow{{1, accountA1, e18(12), e18(100), 1, "66.6667", 40, 0}}
		if !slices.Equal(got, want) {
			t.Fatalf("leaderboard all = %+v, want %+v", got, want)
		}
	})

	t.Run("leaderboard_rank_ties", func(t *testing.T) {
		tx := begin(t, db)
		asOf := baseTime.Add(40 * 24 * time.Hour)
		// a2 is written first; equal profit ranks the lower address a1 first.
		closed(t, tx, factKey{40, 0, 0, 0}, asOf.Add(-time.Hour), accountA2, 2, e18(10), e18(100), e18(5), false)
		closed(t, tx, factKey{40, 1, 0, 0}, asOf.Add(-time.Hour), accountA1, 1, e18(10), e18(100), e18(5), false)
		settled(t, tx, factKey{40, 1, 1, 0}, asOf.Add(-time.Hour), accountA1, 1, 3, e18(10), e18(5), "0", "0")
		// a3 earned more, but ten days ago: inside all and 30d, outside 7d.
		closed(t, tx, factKey{41, 0, 0, 0}, asOf.Add(-10*24*time.Hour), accountA3, 3, e18(10), e18(100), e18(9), false)
		// a3 also lost 50 days ago, which only window all counts.
		closed(t, tx, factKey{42, 0, 0, 0}, asOf.Add(-50*24*time.Hour), accountA3, 3, e18(10), e18(100), "-"+e18(1), false)
		// A close after asOf is not counted in any window.
		closed(t, tx, factKey{43, 0, 0, 0}, asOf.Add(time.Hour), accountA2, 2, e18(10), e18(100), e18(100), false)
		if _, err := tx.Exec(`INSERT INTO perps.positions_current (position_id, account, market_id, is_long, size_usd, collateral_usd,
            collateral_amount, collateral_token, entry_price, leverage, realized_pnl, status, opened_block)
            VALUES (1, $1, 0, true, $2, $2, 100000000, $3, $2, $2, 0, 'open', 39)`, accountA1.Bytes(), e18(100), usdlAddress.Bytes()); err != nil {
			t.Fatalf("insert positions_current: %v", err)
		}

		rebuild(t, tx, asOf)
		all := []leaderboardRow{
			{1, accountA3, e18(8), "0", 0, "50.0000", 0, 0},
			{2, accountA1, e18(5), e18(10), 1, "100.0000", 0, 1},
			{3, accountA2, e18(5), "0", 0, "100.0000", 0, 0},
		}
		if got := leaderboard(t, tx, pnl.WindowAll); !slices.Equal(got, all) {
			t.Fatalf("leaderboard all = %+v, want %+v", got, all)
		}
		month := []leaderboardRow{
			{1, accountA3, e18(9), "0", 0, "100.0000", 0, 0},
			{2, accountA1, e18(5), e18(10), 1, "100.0000", 0, 1},
			{3, accountA2, e18(5), "0", 0, "100.0000", 0, 0},
		}
		if got := leaderboard(t, tx, pnl.Window30d); !slices.Equal(got, month) {
			t.Fatalf("leaderboard 30d = %+v, want %+v", got, month)
		}
		week := []leaderboardRow{
			{1, accountA1, e18(5), e18(10), 1, "100.0000", 0, 1},
			{2, accountA2, e18(5), "0", 0, "100.0000", 0, 0},
		}
		if got := leaderboard(t, tx, pnl.Window7d); !slices.Equal(got, week) {
			t.Fatalf("leaderboard 7d = %+v, want %+v", got, week)
		}

		// A second rebuild replaces the rows instead of adding to them.
		rebuild(t, tx, asOf)
		var rows int
		if err := tx.QueryRow(`SELECT count(*) FROM perps.leaderboard`).Scan(&rows); err != nil || rows != 8 {
			t.Fatalf("leaderboard rows after second rebuild = %d, %v; want 8", rows, err)
		}
	})

	t.Run("history_order", func(t *testing.T) {
		tx := begin(t, db)
		at := func(block int64) time.Time { return baseTime.Add(time.Duration(block) * time.Second) }
		insertFact(t, tx, "collateral_events", factKey{4, 9, 9, 0}, at(4), "deposited", accountA1, map[string]any{
			"account_arg": accountA1.Bytes(), "amount": "250000000", "new_balance": "250000000",
		})
		opened(t, tx, factKey{5, 0, 0, 0}, at(5), accountA1, 7, true, e18(200), e18(100))
		insertFact(t, tx, "fills", factKey{5, 1, 0, 0}, at(5), "fill", accountA2, map[string]any{
			"market_id": 0, "maker_order_id": 1, "taker_order_id": 5, "maker": accountA1.Bytes(), "taker": accountA2.Bytes(),
			"taker_is_long": true, "price": e18(100), "size_usd": e18(200), "maker_fee_usd": "0", "taker_fee_usd": "0",
		})
		// Rows of another account and of an excluded kind table are not returned.
		opened(t, tx, factKey{5, 2, 0, 0}, at(5), accountA3, 8, false, e18(200), e18(100))
		settled(t, tx, factKey{5, 0, 1, 0}, at(5), accountA1, 7, 0, e18(200), "0", "0", "0")

		entries, err := pnl.History(ctx, tx, accountA1, 0, pnl.Through(10), nil, pnl.MaxHistoryRows)
		if err != nil {
			t.Fatalf("History: %v", err)
		}
		type got struct {
			kind  pnl.Kind
			event string
			key   factKey
			hash  common.Hash
		}
		var order []got
		for _, e := range entries {
			order = append(order, got{e.Kind, e.Event, factKey{e.Block, e.TxIndex, e.LogIndex, e.Sub}, e.TxHash})
		}
		want := []got{
			{pnl.KindFill, "fill", factKey{5, 1, 0, 0}, common.BytesToHash(factKey{5, 1, 0, 0}.txHash())},
			{pnl.KindPosition, "opened", factKey{5, 0, 0, 0}, common.BytesToHash(factKey{5, 0, 0, 0}.txHash())},
			{pnl.KindMargin, "deposited", factKey{4, 9, 9, 0}, common.BytesToHash(factKey{4, 9, 9, 0}.txHash())},
		}
		if !slices.Equal(order, want) {
			t.Fatalf("History order = %+v, want %+v", order, want)
		}
		var args map[string]any
		dec := json.NewDecoder(bytes.NewReader(entries[2].Args))
		dec.UseNumber()
		if err := dec.Decode(&args); err != nil {
			t.Fatalf("decode margin args: %v", err)
		}
		if args["amount"] != json.Number("250000000") || args["block"] != nil || args["account"] != nil {
			t.Fatalf("margin args = %v, want amount 250000000 without common columns", args)
		}
		if entries[2].ReceiptKind != "tx" || !entries[2].BlockTime.Equal(at(4)) {
			t.Fatalf("margin entry receipt %q time %v, want tx %v", entries[2].ReceiptKind, entries[2].BlockTime, at(4))
		}

		margin, err := pnl.History(ctx, tx, accountA1, 0, pnl.Through(10), []pnl.Kind{pnl.KindMargin}, pnl.MaxHistoryRows)
		if err != nil || len(margin) != 1 || margin[0].Kind != pnl.KindMargin {
			t.Fatalf("History margin = %+v, %v; want the deposit only", margin, err)
		}
		recent, err := pnl.History(ctx, tx, accountA1, 5, pnl.Through(5), nil, pnl.MaxHistoryRows)
		if err != nil || len(recent) != 2 || recent[0].Kind != pnl.KindFill || recent[1].Kind != pnl.KindPosition {
			t.Fatalf("History block 5 = %+v, %v; want the fill then the open", recent, err)
		}
		if _, err := pnl.History(ctx, tx, accountA1, 0, pnl.Through(10), []pnl.Kind{"settlement"}, pnl.MaxHistoryRows); !errors.Is(err, pnl.ErrUnknownKind) {
			t.Fatalf("unknown kind error %v, want ErrUnknownKind", err)
		}
		if _, err := pnl.History(ctx, tx, accountA1, 6, pnl.Through(5), nil, pnl.MaxHistoryRows); !errors.Is(err, pnl.ErrRange) {
			t.Fatalf("reversed range error %v, want ErrRange", err)
		}
		for _, bad := range []int{0, pnl.MaxHistoryRows + 1} {
			if _, err := pnl.History(ctx, tx, accountA1, 0, pnl.Through(10), nil, bad); !errors.Is(err, pnl.ErrLimit) {
				t.Fatalf("limit %d error %v, want ErrLimit", bad, err)
			}
		}

		// A limited read keeps a key's sub rows together and pages by key.
		insertFact(t, tx, "fills", factKey{5, 1, 0, 1}, at(5), "fill", accountA2, map[string]any{
			"market_id": 0, "maker_order_id": 2, "taker_order_id": 6, "maker": accountA1.Bytes(), "taker": accountA2.Bytes(),
			"taker_is_long": true, "price": e18(100), "size_usd": e18(100), "maker_fee_usd": "0", "taker_fee_usd": "0",
		})
		keys := func(entries []pnl.Entry) []factKey {
			out := []factKey{}
			for _, e := range entries {
				out = append(out, factKey{e.Block, e.TxIndex, e.LogIndex, e.Sub})
			}
			return out
		}
		for _, c := range []struct {
			below pnl.Key
			limit int
			want  []factKey
		}{
			{pnl.Through(10), 1, []factKey{{5, 1, 0, 1}, {5, 1, 0, 0}}},
			{pnl.Through(10), 2, []factKey{{5, 1, 0, 1}, {5, 1, 0, 0}}},
			{pnl.Through(10), 3, []factKey{{5, 1, 0, 1}, {5, 1, 0, 0}, {5, 0, 0, 0}}},
			{pnl.Key{Block: 5, TxIndex: 1, LogIndex: 0}, 1, []factKey{{5, 0, 0, 0}}},
			{pnl.Key{Block: 5, TxIndex: 0, LogIndex: 0}, 5, []factKey{{4, 9, 9, 0}}},
		} {
			page, err := pnl.History(ctx, tx, accountA1, 0, c.below, nil, c.limit)
			if err != nil || !slices.Equal(keys(page), c.want) {
				t.Fatalf("History below %+v limit %d = %v, %v; want %v", c.below, c.limit, keys(page), err, c.want)
			}
		}
	})
}
