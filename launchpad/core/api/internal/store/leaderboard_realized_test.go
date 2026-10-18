package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
)

const (
	realizedPool  = "0x00000000000000000000000000000000000000a1"
	realizedAlice = "0xa11ce00000000000000000000000000000000001"
	realizedBob   = "0xb0b0000000000000000000000000000000000002"
	realizedCarol = "0xca201000000000000000000000000000000000c3"
)

func TestRealizedTraderPnLProportionalBasis(t *testing.T) {
	const from, end = 1000, 2000
	fills := []traderFill{
		// Alice buys 100 for 110 (fee included) before the window, buys 100
		// more for 90 inside it, then sells 50 for 80 and the remaining 150
		// for 150: basis floor(200*50/200)=50, then the full remaining 150.
		{Wallet: realizedAlice, Pool: realizedPool, Side: "buy", TokenRaw: "100", PaidRaw: "110", USDLRaw: "110", Timestamp: 900, AttributionAvailable: true},
		{Wallet: realizedAlice, Pool: realizedPool, Side: "buy", TokenRaw: "100", PaidRaw: "90", USDLRaw: "90", Timestamp: 1100, AttributionAvailable: true},
		{Wallet: realizedAlice, Pool: realizedPool, Side: "sell", TokenRaw: "50", PaidRaw: "50", USDLRaw: "80", Timestamp: 1200, AttributionAvailable: true},
		{Wallet: realizedAlice, Pool: realizedPool, Side: "sell", TokenRaw: "150", PaidRaw: "150", USDLRaw: "150", Timestamp: 1300, AttributionAvailable: true},
		// Bob sells tokens he never bought through the pool (transferred in):
		// basis unknown, so he is excluded rather than ranked on proceeds.
		{Wallet: realizedBob, Pool: realizedPool, Side: "buy", TokenRaw: "10", PaidRaw: "10", USDLRaw: "10", Timestamp: 1100, AttributionAvailable: true},
		{Wallet: realizedBob, Pool: realizedPool, Side: "sell", TokenRaw: "1000", PaidRaw: "1000", USDLRaw: "5000", Timestamp: 1500, AttributionAvailable: true},
		// Carol takes a loss: buys 3 for 10, sells 1 for 2 (basis floor(10/3)=3).
		{Wallet: realizedCarol, Pool: realizedPool, Side: "buy", TokenRaw: "3", PaidRaw: "10", USDLRaw: "10", Timestamp: 1100, AttributionAvailable: true},
		{Wallet: realizedCarol, Pool: realizedPool, Side: "sell", TokenRaw: "1", PaidRaw: "1", USDLRaw: "2", Timestamp: 1600, AttributionAvailable: false},
	}
	entries, excluded, err := RealizedTraderPnL(fills, from, end)
	if err != nil {
		t.Fatal(err)
	}
	if excluded != 1 || len(entries) != 2 {
		t.Fatalf("entries=%+v excluded=%d", entries, excluded)
	}
	byWallet := map[string]LeaderboardEntry{}
	for _, e := range entries {
		byWallet[e.Wallet] = e
	}
	// Alice: (80-50) + (150-150) = 30. Gross proceeds 230 would have been the
	// net-USDL-flow metric's 230-200 = 30 too, so also check Carol, where the
	// unsold inventory makes net flow (2-10=-8) differ from realised (2-3=-1).
	if a := byWallet[realizedAlice]; a.MetricSigned != "30" || a.MetricRaw != "30" || a.Quality != "complete" {
		t.Fatalf("alice: %+v", a)
	}
	if c := byWallet[realizedCarol]; c.MetricSigned != "-1" || c.MetricRaw != "1" || c.Quality != "partial" {
		t.Fatalf("carol: %+v", c)
	}
	if _, ok := byWallet[realizedBob]; ok {
		t.Fatal("unknown-basis disposal ranked")
	}
	// Sells outside the window realise nothing for the window.
	entries, excluded, err = RealizedTraderPnL(fills, 1600, 3000)
	if err != nil || excluded != 0 || len(entries) != 0 {
		t.Fatalf("late window: %+v %d %v", entries, excluded, err)
	}
	if _, _, err = RealizedTraderPnL([]traderFill{{Wallet: "nope", Side: "buy", TokenRaw: "1", PaidRaw: "1", USDLRaw: "1"}}, from, end); err != ErrDataIntegrity {
		t.Fatalf("invalid wallet accepted: %v", err)
	}
}

// TestTraderRealizedRowsSelectsHistoryForWindowSellers runs the production
// query on a real Postgres: only wallet/pool pairs with an in-window sell are
// loaded, with their earlier buys, in chain order, seen from the inventory
// owner (buy recipient, sell payer).
func TestTraderRealizedRowsSelectsHistoryForWindowSellers(t *testing.T) {
	if os.Getenv(internaltest.ExternalPostgresEnv) == "" {
		t.Skip(internaltest.ExternalPostgresEnv + " not set; this test never starts containers")
	}
	pool := internaltest.NewPostgres(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE SCHEMA realized; CREATE TABLE realized.accounting_market_commits_v1(source_fill_id text PRIMARY KEY,source_fact_digest text NOT NULL,projection_version text NOT NULL,pool text NOT NULL,fill jsonb NOT NULL,sequence bigint NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	insert := func(sequence int, side, sender, owner string, height, ts int64, token, paid, usdl string) {
		t.Helper()
		attribution := map[string]any{"transactionSender": sender, "trader": sender, "quality": "exact_receipt"}
		if side == "buy" {
			attribution["recipient"] = owner
		} else {
			attribution["payer"] = owner
		}
		fill, _ := json.Marshal(map[string]any{"side": side, "tokenAmountRaw": token, "paidInputRaw": paid, "usdlAmountRaw": usdl, "blockTimestampSeconds": strconv.FormatInt(ts, 10), "position": map[string]string{"height": strconv.FormatInt(height, 10), "transactionIndex": "0", "logIndex": "0"}, "attribution": attribution})
		if _, err := pool.Exec(ctx, `INSERT INTO realized.accounting_market_commits_v1 VALUES($1,$2,'accounting-v2',$3,$4,$5)`, uuid.NewString(), fmt.Sprintf("%064d", sequence), realizedPool, fill, sequence); err != nil {
			t.Fatal(err)
		}
	}
	router := "0x7000000000000000000000000000000000000007"
	insert(1, "buy", router, realizedAlice, 10, 900, "100", "110", "110")
	insert(2, "sell", router, realizedAlice, 12, 1200, "50", "50", "80")
	insert(3, "buy", realizedCarol, realizedCarol, 11, 1100, "3", "10", "10")
	insert(4, "sell", realizedBob, realizedBob, 13, 2500, "1", "1", "1")
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	rows, err := traderRealizedRows(ctx, tx, `realized.accounting_market_commits_v1`, "accounting-v2", 1000, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Wallet != realizedAlice || rows[0].Side != "buy" || rows[1].Side != "sell" || rows[1].USDLRaw != "80" || !rows[1].AttributionAvailable {
		t.Fatalf("rows: %+v", rows)
	}
	entries, excluded, err := RealizedTraderPnL(rows, 1000, 2000)
	if err != nil || excluded != 0 || len(entries) != 1 || entries[0].MetricSigned != "25" {
		t.Fatalf("entries: %+v %d %v", entries, excluded, err)
	}
}
