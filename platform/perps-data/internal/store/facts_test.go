package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
)

const outboxAtSQL = `SELECT count(*)::text FROM perps.delivery_outbox
WHERE convert_from(envelope, 'UTF8')::jsonb #>> '{source_order,height}' = $1`

func lower(a common.Address) string { return strings.ToLower(a.Hex()) }

func header(n uint64, at time.Time) Block {
	return Block{
		Number:     n,
		Hash:       crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n))),
		ParentHash: crypto.Keccak256Hash([]byte(fmt.Sprintf("block/%d", n-1))),
		Time:       at.UTC(),
		Proposer:   strings.Repeat("AB", 20),
		TxCount:    1,
	}
}

// placeOrders stores block n-1 with the OrderPlaced rows of maker order 7 of
// trader A and taker order 8 of trader B, which the Fill of block n names.
func (c *chain) placeOrders(n uint64, at time.Time) {
	c.t.Helper()
	before := at.Add(-time.Second)
	c.next(n-1, before)
	c.blockRow(n-1, before)
	c.placed("7", traderA, "300")
	c.placed("8", traderB, "300")
	c.project()
}

// block400 is block 400 with three logs: a CollateralTransferred with rows
// sub 0 and sub 1 and a CollateralDeposited in the transaction receipt, and a
// Fill in the perps EndBlock receipt.
func block400(c *chain, at time.Time) (Block, *decode.Result) {
	c.placeOrders(400, at)
	c.next(400, at)
	in := c.fact("transferred_in", traderB, decode.PerpsAddress, 1)
	out := c.fact("transferred_out", traderA, decode.PerpsAddress, 0)
	dep := c.fact("deposited", traderA, decode.PerpsAddress, 0)
	c.endBlock()
	fill := c.fact("fill", traderB, decode.PerpsAddress, 0)
	return header(400, at), &decode.Result{Decoded: []decode.Decoded{
		{
			Event: decode.Event{EventName: "CollateralTransferred", Args: map[string]any{"from": lower(traderA), "to": lower(traderB), "amount": "5"}},
			Rows: []decode.Row{
				decode.CollateralTransferredRow{Fact: out, FromAccount: traderA, ToAccount: traderB, Amount: "5"},
				decode.CollateralTransferredRow{Fact: in, FromAccount: traderA, ToAccount: traderB, Amount: "5"},
			},
		},
		{
			Event: decode.Event{EventName: "CollateralDeposited", Args: map[string]any{"account": lower(traderA), "amount": "100", "newBalance": "100"}},
			Rows:  []decode.Row{decode.CollateralDepositedRow{Fact: dep, AccountArg: traderA, Amount: "100", NewBalance: "100"}},
		},
		{
			Event: decode.Event{EventName: "Fill", Args: map[string]any{"marketId": "1", "price": "60000", "sizeUsd": "300"}},
			Rows: []decode.Row{decode.FillRow{Fact: fill, MarketID: "1", MakerOrderID: "7", TakerOrderID: "8", Maker: traderA, Taker: traderB,
				TakerIsLong: true, Price: "60000", SizeUSD: "300", MakerFeeUSD: "0", TakerFeeUSD: "0"}},
		},
	}}
}

func (c *chain) writeBlock(b Block, rows *decode.Result) {
	c.t.Helper()
	if err := WriteBlock(c.ctx, c.tx, b, rows); err != nil {
		c.t.Fatalf("WriteBlock %d: %v", b.Number, err)
	}
}

func (c *chain) counts(block string) []string {
	c.t.Helper()
	var out []string
	for _, table := range []string{"collateral_events", "fills", "raw_logs", "protocol_events", "tx_actions"} {
		out = append(out, table+"="+c.values(`SELECT count(*)::text FROM perps.`+table+` WHERE block = $1`, block)[0])
	}
	return append(out, "outbox="+c.values(outboxAtSQL, block)[0])
}

func TestFactWriters(t *testing.T) {
	db := testDB(t)
	migrate(t, db)
	t0 := time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC)
	wantCounts := []string{"collateral_events=3", "fills=1", "raw_logs=0", "protocol_events=3", "tx_actions=2", "outbox=7"}

	t.Run("one_row_per_log", func(t *testing.T) {
		c := newChain(t, db)
		b, rows := block400(c, t0)
		c.blockRow(400, t0)
		c.writeBlock(b, rows)
		if got := c.counts("400"); strings.Join(got, " ") != strings.Join(wantCounts, " ") {
			t.Fatalf("counts %q, want %q", got, wantCounts)
		}
		c.want(`SELECT string_agg(kind || ':' || log_index || ':' || sub, ',' ORDER BY log_index, sub) FROM perps.collateral_events WHERE block = 400`,
			[]string{"transferred_out:0:0,transferred_in:0:1,deposited:1:0"})
		c.want(`SELECT string_agg(event_name || ':' || log_index || ':' || sub || ':' || coalesce(market_id::text, 'null'), ',' ORDER BY log_index)
FROM perps.protocol_events WHERE block = 400`,
			[]string{"CollateralTransferred:0:0:null,CollateralDeposited:1:0:null,Fill:2:0:1"})
		c.want(`SELECT args->>'amount', account = $1 FROM perps.protocol_events WHERE block = 400 AND log_index = 0`, []string{"5", "true"}, traderA.Bytes())
	})

	t.Run("duplicate_block_noop", func(t *testing.T) {
		c := newChain(t, db)
		b, rows := block400(c, t0)
		c.blockRow(400, t0)
		c.writeBlock(b, rows)
		margin := c.values(`SELECT count(*)::text, coalesce(string_agg(balance::text || '/' || locked::text, ',' ORDER BY account, token), '') FROM perps.account_margin WHERE account IN ($1, $2)`, traderA.Bytes(), traderB.Bytes())
		c.writeBlock(b, rows)
		if got := c.counts("400"); strings.Join(got, " ") != strings.Join(wantCounts, " ") {
			t.Fatalf("counts after the second write %q, want %q", got, wantCounts)
		}
		c.want(`SELECT count(*)::text FROM perps.collateral_events WHERE block = 400 AND log_index = 0 AND kind IN ('transferred_out', 'transferred_in')`,
			[]string{"2"})
		c.want(`SELECT count(*)::text, coalesce(string_agg(balance::text || '/' || locked::text, ',' ORDER BY account, token), '') FROM perps.account_margin WHERE account IN ($1, $2)`, margin, traderA.Bytes(), traderB.Bytes())
	})

	t.Run("outbox_in_same_tx", func(t *testing.T) {
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		t.Cleanup(func() { _ = tx.Rollback() })
		c := &chain{t: t, ctx: context.Background(), tx: tx}
		b, rows := block400(c, t0)
		c.blockRow(400, t0)
		c.writeBlock(b, rows)
		c.want(outboxAtSQL, []string{"7"}, "400")
		c.want(`SELECT string_agg(subject, ',' ORDER BY perps_seq) FROM perps.delivery_outbox
WHERE convert_from(envelope, 'UTF8')::jsonb #>> '{source_order,height}' = '400'`,
			[]string{strings.Join([]string{
				"perps.125.user." + lower(traderA) + ".margin",
				"perps.125.user." + lower(traderB) + ".margin",
				"perps.125.user." + lower(traderA) + ".margin",
				"perps.125.1.fill",
				"perps.125.chain.block",
				"perps.125.chain.tx",
				"perps.125.chain.tx",
			}, ",")})
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		var facts, outbox int
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM perps.collateral_events WHERE block = 400) + (SELECT count(*) FROM perps.fills WHERE block = 400)`).Scan(&facts); err != nil {
			t.Fatalf("count facts: %v", err)
		}
		if err := db.QueryRow(outboxAtSQL, "400").Scan(&outbox); err != nil {
			t.Fatalf("count outbox: %v", err)
		}
		if facts != 0 || outbox != 0 {
			t.Fatalf("after rollback %d fact rows and %d outbox rows, want 0 and 0", facts, outbox)
		}
	})

	t.Run("cursor_advances", func(t *testing.T) {
		ctx := context.Background()
		at := t0.Add(30 * time.Second)
		b, before := header(400, at), header(399, at.Add(-time.Second))
		cleanup := func() {
			for _, q := range []string{
				`DELETE FROM perps.delivery_outbox WHERE convert_from(envelope, 'UTF8')::jsonb #>> '{source_order,height}' IN ('399', '400')`,
				`DELETE FROM perps.blocks WHERE number IN (399, 400)`, `DELETE FROM perps.cursors WHERE name = 'head'`,
			} {
				if _, err := db.ExecContext(ctx, q); err != nil {
					t.Errorf("%s: %v", q, err)
				}
			}
		}
		cleanup()
		t.Cleanup(cleanup)
		commit := func(b Block, rows *decode.Result) {
			t.Helper()
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin: %v", err)
			}
			defer func() { _ = tx.Rollback() }()
			if _, err := tx.ExecContext(ctx, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, $6)`,
				int64(b.Number), b.Hash.Bytes(), b.ParentHash.Bytes(), b.Time, b.Proposer, b.TxCount); err != nil {
				t.Fatalf("insert block %d: %v", b.Number, err)
			}
			if err := WriteBlock(ctx, tx, b, rows); err != nil {
				t.Fatalf("WriteBlock %d: %v", b.Number, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO perps.cursors (name, height, hash) VALUES ('head', $1, $2)
ON CONFLICT (name) DO UPDATE SET height = EXCLUDED.height, hash = EXCLUDED.hash, updated_at = now()`, int64(b.Number), b.Hash.Bytes()); err != nil {
				t.Fatalf("cursor %d: %v", b.Number, err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatalf("commit %d: %v", b.Number, err)
			}
		}
		if _, ok, err := Cursor(ctx, db); err != nil || ok {
			t.Fatalf("Cursor on an empty head = ok %v, err %v; want no cursor", ok, err)
		}
		commit(before, &decode.Result{})
		if h, ok, err := Cursor(ctx, db); err != nil || !ok || h != 399 {
			t.Fatalf("Cursor = %d, %v, %v; want 399", h, ok, err)
		}
		commit(b, &decode.Result{})
		h, ok, err := Cursor(ctx, db)
		if err != nil || !ok || h != 400 {
			t.Fatalf("Cursor = %d, %v, %v; want 400", h, ok, err)
		}
		var hash []byte
		if err := db.QueryRowContext(ctx, `SELECT hash FROM perps.cursors WHERE name = 'head'`).Scan(&hash); err != nil {
			t.Fatalf("cursor hash: %v", err)
		}
		if common.BytesToHash(hash) != b.Hash {
			t.Fatalf("cursor hash %x, want %x", hash, b.Hash)
		}
	})

	t.Run("lease_refuses_second_writer", func(t *testing.T) {
		c := newChain(t, db)
		if _, err := c.tx.ExecContext(c.ctx, `INSERT INTO perps.writer_lease (name, holder, expires_at)
VALUES ('indexer', 'indexer-a', clock_timestamp() + interval '30 seconds')
ON CONFLICT (name) DO UPDATE SET holder = EXCLUDED.holder, expires_at = EXCLUDED.expires_at`); err != nil {
			t.Fatalf("lease: %v", err)
		}
		err := Fence(c.ctx, c.tx, "indexer", "indexer-b")
		if !errors.Is(err, ErrLeaseHeld) || err.Error() != "store: writer lease held by another instance: indexer" {
			t.Fatalf("Fence of indexer-b = %v, want ErrLeaseHeld", err)
		}
		if got := c.counts("400"); strings.Join(got, " ") != "collateral_events=0 fills=0 raw_logs=0 protocol_events=0 tx_actions=0 outbox=0" {
			t.Fatalf("counts %q after a refused fence, want none", got)
		}
		if err := Fence(c.ctx, c.tx, "indexer", "indexer-a"); err != nil {
			t.Fatalf("Fence of indexer-a = %v, want nil", err)
		}
	})

	t.Run("protocol_events_and_actions", func(t *testing.T) {
		c := newChain(t, db)
		c.placeOrders(410, t0)
		c.next(410, t0)
		b := header(410, t0)
		aggregate := func(i int64, market string) decode.Decoded {
			f := decode.Fact{Block: 410, BlockHash: b.Hash, TxIndex: -1, LogIndex: i, TxHash: b.Hash, ReceiptKind: decode.ReceiptBlockEvent,
				BlockTime: b.Time, Emitter: decode.PricefeedAddress, Kind: "aggregate", Account: decode.ModuleAddress("pricefeed")}
			return decode.Decoded{
				Event: decode.Event{EventName: decode.EventAggregate, Args: map[string]any{"market_id": market}},
				Rows: []decode.Row{decode.IndexPriceRow{Fact: f, MarketID: market, IndexPrice: "60000", MarkPrice: "60010", Basis: "-10",
					TimestampMs: "1772445600000", Sources: "3", Required: "2"}},
			}
		}
		c.endBlock()
		fill := c.fact("fill", traderB, decode.PerpsAddress, 0)
		rows := &decode.Result{Decoded: []decode.Decoded{
			aggregate(0, "1"), aggregate(1, "2"),
			{
				Event: decode.Event{EventName: "Fill", Args: map[string]any{"marketId": "1"}},
				Rows: []decode.Row{decode.FillRow{Fact: fill, MarketID: "1", MakerOrderID: "7", TakerOrderID: "8", Maker: traderA, Taker: traderB,
					TakerIsLong: true, Price: "60000", SizeUSD: "300", MakerFeeUSD: "0", TakerFeeUSD: "0"}},
			},
		}}
		c.blockRow(410, t0)
		c.writeBlock(b, rows)
		c.want(`SELECT string_agg(event_name || ':' || tx_index || ':' || log_index || ':' || market_id::text, ',' ORDER BY tx_index, log_index)
FROM perps.protocol_events WHERE block = 410`,
			[]string{"pricefeed_aggregate:-1:0:1,pricefeed_aggregate:-1:1:2,Fill:1:0:1"})
		c.want(`SELECT string_agg(action || '|' || kind || '|' || tx_index || '|' || log_index || '|' || sub || '|' || coalesce(side, 'null') || '|' ||
    coalesce(price::text, 'null') || '|' || coalesce(size::text, 'null'), ',' ORDER BY tx_index, log_index, sub) FROM perps.tx_actions WHERE block = 410`,
			[]string{"Oracle Updated|pricefeed_aggregate|-1|0|0|null|null|2,OROB Fill|Fill|1|0|0|buy|60000|300,Sealed Batch Executed|perps_endblock|1|0|1|null|null|1"})
		c.want(`SELECT account = $1, tx_hash = $2 FROM perps.tx_actions WHERE block = 410 AND kind = 'Fill'`, []string{"true", "true"},
			traderB.Bytes(), fill.TxHash.Bytes())
	})
}

func TestTokenOfSpotRows(t *testing.T) {
	usdl := common.HexToAddress("0x00000000000000000000000000000000000001a1")
	pax := common.HexToAddress("0x00000000000000000000000000000000000002a2")
	sid := common.HexToAddress("0x00000000000000000000000000000000000003a3")
	stable := map[common.Address]bool{usdl: true, pax: false, sid: false}
	for _, c := range []struct {
		name string
		row  decode.Row
		want []common.Address
	}{
		{"sale_into_stablecoin", decode.SpotSimpleSwapRow{TokenIn: sid, TokenOut: usdl}, []common.Address{sid}},
		{"buy_with_stablecoin", decode.SpotSimpleSwapRow{TokenIn: usdl, TokenOut: sid}, []common.Address{sid}},
		{"token_to_token", decode.SpotSimpleSwapRow{TokenIn: pax, TokenOut: sid}, []common.Address{sid}},
		{"native_swap_of_token", decode.SpotNativeSwapRow{TokenOther: sid}, []common.Address{sid, pax}},
		{"native_swap_of_stablecoin", decode.SpotNativeSwapRow{TokenOther: usdl}, []common.Address{pax}},
		{"market_order", decode.SpotMarketOrderRow{Stablecoin: usdl, Token: sid}, []common.Address{sid}},
		{"tiered_fee", decode.SpotTieredFeeRow{TokenOut: usdl}, []common.Address{usdl}},
	} {
		if got := tokensOf(c.row, pax, stable); !slices.Equal(got, c.want) {
			t.Errorf("%s: tokensOf = %v, want %v", c.name, got, c.want)
		}
	}
}
