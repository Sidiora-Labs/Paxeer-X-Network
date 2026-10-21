package decode_test

import (
	"context"
	"database/sql"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

// writeSeedBlock decodes logs as block number and writes it with
// store.WriteBlock after its perps.blocks row, as the indexer does.
func writeSeedBlock(t *testing.T, tx *sql.Tx, r *decode.Registry, number uint64, logs ...decode.Log) *decode.Result {
	t.Helper()
	ctx := context.Background()
	hash := common.BigToHash(new(big.Int).SetUint64(0xb10c0000 + number))
	txHash := common.BigToHash(new(big.Int).SetUint64(0x7a0000 + number))
	at := xBlockTime
	for i := range logs {
		logs[i] = inBlock(logs[i], number, hash, txHash)
	}
	res, err := r.Decode(ctx, tx, logs)
	if err != nil {
		t.Fatalf("decode block %d: %v", number, err)
	}
	parent := common.BigToHash(new(big.Int).SetUint64(0xb10c0000 + number - 1))
	if _, err := tx.ExecContext(ctx, `INSERT INTO perps.blocks (number, hash, parent_hash, time, proposer, tx_count) VALUES ($1, $2, $3, $4, $5, 1)`,
		int64(number), hash.Bytes(), parent.Bytes(), at, strings.Repeat("AB", 20)); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteBlock(ctx, tx, store.Block{Number: number, Hash: hash, ParentHash: parent, Time: at, TxCount: 1}, res); err != nil {
		t.Fatalf("write block %d: %v", number, err)
	}
	return res
}

// row reads one row as text, NULL as "null".
func row(t *testing.T, tx *sql.Tx, query string, args ...any) []string {
	t.Helper()
	rows, err := tx.QueryContext(context.Background(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !rows.Next() {
		t.Fatalf("%s %v: no row", query, args)
	}
	cells := make([]sql.NullString, len(cols))
	ptrs := make([]any, len(cols))
	for i := range cells {
		ptrs[i] = &cells[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = "null"
		if c.Valid {
			out[i] = c.String
		}
	}
	return out
}

func TestProjectionSeeds(t *testing.T) {
	const (
		orderSQL = `SELECT account = $2, market_id, order_type, is_long, trigger_price, size_usd, limit_price, time_in_force, reduce_only,
    remaining_size_usd, status, placed_block, closed_block, projected_from, projected_height FROM perps.orders WHERE order_id = $1`
		positionSQL = `SELECT account = $2, market_id, is_long, size_usd, collateral_usd, collateral_amount, entry_price, leverage, realized_pnl,
    status, opened_block, take_profit_price, stop_loss_price, projected_from, projected_height FROM perps.positions_current WHERE position_id = $1`
	)
	tx := lookupDB(t)
	state := newRecordedState(t)
	state.orders["105"] = orderRecord{OrderId: big.NewInt(105), User: xAlice, MarketId: big.NewInt(1), IsLong: true, OrderType: 0, TimeInForce: 0,
		Status: 1, LimitPrice: big.NewInt(2000), TriggerPrice: new(big.Int), AcceptablePrice: new(big.Int), SizeUsd: big.NewInt(8_000_000),
		SizeBase: big.NewInt(4_000), FilledBase: big.NewInt(1_000), Leverage: big.NewInt(5), Cloid: [16]byte{7}}
	state.positions["77"] = positionRecord{User: xBob, MarketId: big.NewInt(1), IsLong: false, SizeUsd: big.NewInt(9_000_000),
		CollateralUsd: big.NewInt(3_000_000), CollateralToken: store.USDL, CollateralAmount: big.NewInt(3), EntryPrice: big.NewInt(2100),
		Leverage: big.NewInt(3), TakeProfitPrice: big.NewInt(1900), StopLossPrice: new(big.Int)}
	r := registry(t, state)

	// Block 200: a fill of resting order 105, an ADL reduce of position 77,
	// and a fill of order 106, which the precompile does not know.
	res := writeSeedBlock(t, tx, r, xBlock,
		perpsLog(t, "Fill", 0, n(1), n(105), n(0), xAlice, xBob, false, n(2000), n(2_000_000), n(0), n(0)),
		perpsLog(t, "ADLExecuted", 1, n(77), n(4_000_000)),
		perpsLog(t, "Fill", 2, n(1), n(106), n(0), xCarolSeed, xBob, false, n(2000), n(1_000_000), n(0), n(0)),
	)
	wantCalls := []stateCall{
		{decode.PerpsAddress, state.callData("getOrderDetails", 105), xBlock - 1, false},
		{decode.PerpsAddress, state.callData("getPosition", 77), xBlock - 1, false},
		{decode.PerpsAddress, state.callData("getPositionDetails", 77), xBlock - 1, false},
		{decode.PerpsAddress, state.callData("getOrderDetails", 106), xBlock - 1, false},
	}
	if !reflect.DeepEqual(state.calls, wantCalls) {
		t.Fatalf("calls %+v, want %+v", state.calls, wantCalls)
	}
	if len(res.Seeds) != 2 || len(res.Raw) != 1 || res.Raw[0].FailureClass != decode.FailureProjectionLookup {
		t.Fatalf("seeds %+v raw %+v, want two seeds and one %s raw row", res.Seeds, res.Raw, decode.FailureProjectionLookup)
	}

	t.Run("fill_on_an_order_never_placed", func(t *testing.T) {
		// remaining 8,000,000 x 3,000 / 4,000 at the height before, less the fill.
		want := []string{"true", "1", "0", "true", "0", "8000000", "2000", "0", "false", "4000000", "active", "null", "null", "state", "199"}
		if got := row(t, tx, orderSQL, "105", xAlice.Bytes()); !reflect.DeepEqual(got, want) {
			t.Fatalf("order 105 %v, want %v", got, want)
		}
	})

	t.Run("reduce_on_a_position_never_opened", func(t *testing.T) {
		want := []string{"true", "1", "false", "5000000", "3000000", "3", "2100", "3", "null", "open", "null", "1900", "null", "state", "199"}
		if got := row(t, tx, positionSQL, "77", xBob.Bytes()); !reflect.DeepEqual(got, want) {
			t.Fatalf("position 77 %v, want %v", got, want)
		}
	})

	t.Run("unknown_id_is_a_projection_lookup_raw_row", func(t *testing.T) {
		got := row(t, tx, `SELECT (SELECT count(*) FROM perps.raw_logs WHERE failure_class = $1 AND block = $2),
    (SELECT count(*) FROM perps.orders WHERE order_id = 106), (SELECT count(*) FROM perps.fills WHERE maker_order_id = 106),
    (SELECT count(*) FROM perps.projection_seeds WHERE id = 106)`, decode.FailureProjectionLookup, xBlock)
		if want := []string{"1", "0", "0", "0"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("raw, order, fill, seed counts %v, want %v", got, want)
		}
		want := decode.AccountLookup{What: "order", ID: "106", Height: xBlock - 1, Path: decode.LookupAtHeight, Missing: true}
		if len(res.Lookups) != 3 || res.Lookups[2] != want {
			t.Fatalf("lookups %+v, want the third %+v", res.Lookups, want)
		}
	})

	t.Run("later_batch_reads_the_stored_row", func(t *testing.T) {
		before := len(state.calls)
		next := writeSeedBlock(t, tx, r, xBlock+1,
			perpsLog(t, "OrderCancelled", 0, n(105), xAlice),
			perpsLog(t, "ADLExecuted", 1, n(77), n(1_000_000)),
		)
		if len(state.calls) != before || len(next.Seeds) != 0 || len(next.Lookups) != 0 {
			t.Fatalf("calls %+v seeds %+v lookups %+v, the stored rows must answer without a state call", state.calls[before:], next.Seeds, next.Lookups)
		}
		if got := row(t, tx, `SELECT remaining_size_usd, status, closed_block FROM perps.orders WHERE order_id = 105`); !reflect.DeepEqual(got, []string{"4000000", "cancelled", "201"}) {
			t.Fatalf("order 105 %v, want cancelled at 201 with 4000000 left", got)
		}
		if got := row(t, tx, `SELECT size_usd, account FROM perps.positions_current WHERE position_id = 77`); got[0] != "4000000" {
			t.Fatalf("position 77 %v, want size 4000000", got)
		}
	})

	t.Run("rebuild_projects_from_the_stored_seeds", func(t *testing.T) {
		if err := store.Rebuild(context.Background(), tx, xBlock, xBlock+1); err != nil {
			t.Fatal(err)
		}
		if got := row(t, tx, `SELECT remaining_size_usd, status, projected_from, projected_height FROM perps.orders WHERE order_id = 105`); !reflect.DeepEqual(got, []string{"4000000", "cancelled", "state", "199"}) {
			t.Fatalf("rebuilt order 105 %v", got)
		}
		if got := row(t, tx, `SELECT size_usd, projected_from FROM perps.positions_current WHERE position_id = 77`); !reflect.DeepEqual(got, []string{"4000000", "state"}) {
			t.Fatalf("rebuilt position 77 %v", got)
		}
	})
}

var xCarolSeed = common.HexToAddress("0xca201000000000000000000000000000000ca202")
