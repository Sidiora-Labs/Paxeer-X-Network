package decode_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/store"
)

// stateCall is one eth_call a recordedState answered.
type stateCall struct {
	to     common.Address
	data   []byte
	height uint64
	latest bool
}

// recordedState is a decode.StateReader over the perps getters
// getOrderDetails, getPosition and getPositionDetails of the leaf ABI: an id
// in orders or positions answers its record packed through the ABI, any other
// id reverts with the getter's unknown error, or with revert when set.
// pruned refuses every CallAt with ErrStatePruned.
type recordedState struct {
	t         *testing.T
	abi       abi.ABI
	orders    map[string]orderRecord
	positions map[string]positionRecord
	pruned    bool
	revert    []byte
	calls     []stateCall
}

// orderRecord is the getOrderDetails tuple of the leaf ABI.
type orderRecord struct {
	OrderId         *big.Int
	User            common.Address
	MarketId        *big.Int
	IsLong          bool
	OrderType       uint8
	TimeInForce     uint8
	Status          uint8
	ReduceOnly      bool
	LimitPrice      *big.Int
	TriggerPrice    *big.Int
	AcceptablePrice *big.Int
	SizeUsd         *big.Int
	SizeBase        *big.Int
	FilledBase      *big.Int
	Leverage        *big.Int
	Deadline        uint64
	Cloid           [16]byte
	Sequence        uint64
}

// positionRecord holds the getPosition outputs and the getPositionDetails
// tuple of one position.
type positionRecord struct {
	User             common.Address
	MarketId         *big.Int
	IsLong           bool
	SizeUsd          *big.Int
	CollateralUsd    *big.Int
	CollateralToken  common.Address
	CollateralAmount *big.Int
	EntryPrice       *big.Int
	Leverage         *big.Int
	TakeProfitPrice  *big.Int
	StopLossPrice    *big.Int
}

func newRecordedState(t *testing.T) *recordedState {
	t.Helper()
	a, err := perpsabi.ABI()
	if err != nil {
		t.Fatal(err)
	}
	return &recordedState{t: t, abi: a, orders: map[string]orderRecord{}, positions: map[string]positionRecord{}}
}

func (s *recordedState) callData(method string, id int64) []byte {
	s.t.Helper()
	data, err := s.abi.Pack(method, big.NewInt(id))
	if err != nil {
		s.t.Fatal(err)
	}
	return data
}

// ownOrder and ownPosition record an open order or position of owner with
// one unit of every amount.
func (s *recordedState) ownOrder(id int64, owner common.Address) {
	one := big.NewInt(1)
	s.orders[big.NewInt(id).String()] = orderRecord{OrderId: big.NewInt(id), User: owner, MarketId: one, LimitPrice: one, TriggerPrice: new(big.Int),
		AcceptablePrice: new(big.Int), SizeUsd: one, SizeBase: one, FilledBase: new(big.Int), Leverage: one, Status: 1}
}

func (s *recordedState) ownPosition(id int64, owner common.Address) {
	one := big.NewInt(1)
	s.positions[big.NewInt(id).String()] = positionRecord{User: owner, MarketId: one, SizeUsd: one, CollateralUsd: one, CollateralAmount: one,
		EntryPrice: one, Leverage: one, TakeProfitPrice: new(big.Int), StopLossPrice: new(big.Int)}
}

func (s *recordedState) CallAt(_ context.Context, to common.Address, data []byte, height uint64) ([]byte, error) {
	s.calls = append(s.calls, stateCall{to, bytes.Clone(data), height, false})
	if s.pruned {
		return nil, fmt.Errorf("%w: height %d", decode.ErrStatePruned, height)
	}
	return s.answer(data)
}

func (s *recordedState) Call(_ context.Context, to common.Address, data []byte) ([]byte, error) {
	s.calls = append(s.calls, stateCall{to, bytes.Clone(data), 0, true})
	return s.answer(data)
}

func (s *recordedState) answer(data []byte) ([]byte, error) {
	m, err := s.abi.MethodById(data)
	if err != nil {
		return nil, err
	}
	args, err := m.Inputs.Unpack(data[4:])
	if err != nil {
		return nil, err
	}
	id := args[0].(*big.Int)
	o, isOrder := s.orders[id.String()]
	p, isPosition := s.positions[id.String()]
	unknown := "PerpsUnknownPosition"
	switch m.Name {
	case "getOrderDetails":
		unknown = "PerpsUnknownOrder"
		if isOrder {
			return m.Outputs.Pack(o)
		}
	case "getPosition":
		if isPosition {
			return m.Outputs.Pack(p.User, p.MarketId, p.IsLong, p.SizeUsd, p.CollateralUsd, p.CollateralToken, p.CollateralAmount, p.EntryPrice,
				new(big.Int), true)
		}
	case "getPositionDetails":
		if isPosition {
			return m.Outputs.Pack(struct {
				PositionId       *big.Int
				User             common.Address
				MarketId         *big.Int
				IsLong           bool
				SizeBase         *big.Int
				SizeUsdAtMark    *big.Int
				EntryPrice       *big.Int
				Leverage         *big.Int
				UnrealizedPnlUsd *big.Int
				FundingOwedUsd   *big.Int
				LiquidationPrice *big.Int
				TakeProfitPrice  *big.Int
				StopLossPrice    *big.Int
				OpenedAt         *big.Int
			}{id, p.User, p.MarketId, p.IsLong, new(big.Int), p.SizeUsd, p.EntryPrice, p.Leverage, new(big.Int), new(big.Int), new(big.Int),
				p.TakeProfitPrice, p.StopLossPrice, new(big.Int)})
		}
	default:
		return nil, fmt.Errorf("recorded state answers no %s", m.Name)
	}
	if s.revert != nil {
		return nil, &decode.RevertError{Data: s.revert}
	}
	return nil, &decode.RevertError{Data: s.abi.Errors[unknown].ID.Bytes()[:4]}
}

// lookupDB empties schema perps of PERPS_DATA_TEST_DATABASE_URL, applies the
// migrations and returns a transaction rolled back at cleanup.
func lookupDB(t *testing.T) *sql.Tx {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(requireEnv(t, "PERPS_DATA_TEST_DATABASE_URL", "the account lookups read and write the stored facts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, `DROP SCHEMA IF EXISTS perps CASCADE`); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func perpsLog(t *testing.T, name string, index uint, values ...any) decode.Log {
	t.Helper()
	return pack(t, event(t, perpsabi.ABI, name), decode.PerpsAddress, index, values...)
}

func inBlock(l decode.Log, block uint64, hash, txHash common.Hash) decode.Log {
	l.BlockNumber, l.BlockHash, l.TxHash = block, hash, txHash
	return l
}

func accounts(res *decode.Result) []common.Address {
	var out []common.Address
	for _, d := range res.Decoded {
		for _, r := range d.Rows {
			out = append(out, r.Common().Account)
		}
	}
	return out
}

func TestPerpsAccountLookup(t *testing.T) {
	ctx := context.Background()
	xCarol := common.HexToAddress("0xca201000000000000000000000000000000ca201")
	noCloid := [16]byte{1}

	t.Run("unstored_ids_resolve_at_the_height_before_and_persist", func(t *testing.T) {
		tx := lookupDB(t)
		state := newRecordedState(t)
		state.ownPosition(900001, xAlice)
		state.ownOrder(900002, xBob)
		r := registry(t, state)
		res, err := r.Decode(ctx, tx, []decode.Log{
			perpsLog(t, "PositionModified", 0, n(900001), n(10), n(5), n(5)),
			perpsLog(t, "OrderTriggered", 1, n(900002), n(1), n(7)),
			perpsLog(t, "PositionTpSlSet", 2, n(900001), n(9), n(3)),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := accounts(res); !reflect.DeepEqual(got, []common.Address{xAlice, xBob, xAlice}) || len(res.Raw) != 0 {
			t.Fatalf("accounts %v raw %+v, want alice bob alice from the getters", got, res.Raw)
		}
		wantCalls := []stateCall{
			{decode.PerpsAddress, state.callData("getPosition", 900001), xBlock - 1, false},
			{decode.PerpsAddress, state.callData("getPositionDetails", 900001), xBlock - 1, false},
			{decode.PerpsAddress, state.callData("getOrderDetails", 900002), xBlock - 1, false},
		}
		if !reflect.DeepEqual(state.calls, wantCalls) {
			t.Fatalf("calls %+v, want the getters of each id once at the height before the block", state.calls)
		}
		wantLookups := []decode.AccountLookup{
			{What: "position", ID: "900001", Height: xBlock - 1, Path: decode.LookupAtHeight, Account: xAlice},
			{What: "order", ID: "900002", Height: xBlock - 1, Path: decode.LookupAtHeight, Account: xBob},
		}
		if !reflect.DeepEqual(res.Lookups, wantLookups) {
			t.Fatalf("lookups %+v, want %+v", res.Lookups, wantLookups)
		}
		if _, err := store.WriteFacts(ctx, tx, store.Block{Number: xBlock, Hash: xBlockHash, Time: xBlockTime}, res); err != nil {
			t.Fatal(err)
		}

		nextHash, nextTx := common.HexToHash("0xb10c3"), common.HexToHash("0xa3")
		next, err := r.Decode(ctx, tx, []decode.Log{
			inBlock(perpsLog(t, "PositionEntryPriceChanged", 0, n(900001), n(1), n(2)), xBlock+1, nextHash, nextTx),
			inBlock(perpsLog(t, "OrderExecuted", 1, n(900002), n(900001), n(2)), xBlock+1, nextHash, nextTx),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := accounts(next); !reflect.DeepEqual(got, []common.Address{xAlice, xBob}) {
			t.Fatalf("accounts %v, want the accounts of the stored rows", got)
		}
		if len(state.calls) != len(wantCalls) || len(next.Lookups) != 0 {
			t.Fatalf("calls %d lookups %+v, the stored rows must answer without a call", len(state.calls), next.Lookups)
		}
	})

	t.Run("pruned_height_falls_back_to_latest", func(t *testing.T) {
		tx := lookupDB(t)
		state := newRecordedState(t)
		state.pruned = true
		state.ownPosition(900003, xCarol)
		res, err := registry(t, state).Decode(ctx, tx, []decode.Log{perpsLog(t, "PositionModified", 0, n(900003), n(10), n(5), n(5))})
		if err != nil {
			t.Fatal(err)
		}
		if got := accounts(res); !reflect.DeepEqual(got, []common.Address{xCarol}) {
			t.Fatalf("accounts %v, want carol from the latest state", got)
		}
		data := state.callData("getPosition", 900003)
		want := []stateCall{{decode.PerpsAddress, data, xBlock - 1, false}, {decode.PerpsAddress, data, 0, true},
			{decode.PerpsAddress, state.callData("getPositionDetails", 900003), 0, true}}
		if !reflect.DeepEqual(state.calls, want) {
			t.Fatalf("calls %+v, want %+v", state.calls, want)
		}
		if want := []decode.AccountLookup{{What: "position", ID: "900003", Height: xBlock - 1, Path: decode.LookupLatest, Account: xCarol}}; !reflect.DeepEqual(res.Lookups, want) {
			t.Fatalf("lookups %+v, want %+v", res.Lookups, want)
		}
	})

	t.Run("unknown_id_is_an_account_lookup_raw_row", func(t *testing.T) {
		tx := lookupDB(t)
		state := newRecordedState(t)
		res, err := registry(t, state).Decode(ctx, tx, []decode.Log{
			perpsLog(t, "PositionModified", 0, n(900004), n(10), n(5), n(5)),
			perpsLog(t, "OrderOptions", 1, n(900005), uint8(0), false, n(0), n(0), uint64(0), noCloid),
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Decoded) != 0 || len(res.Raw) != 2 {
			t.Fatalf("decoded %+v raw %+v, want two raw rows", res.Decoded, res.Raw)
		}
		for _, raw := range res.Raw {
			if raw.FailureClass != decode.FailureAccountLookup || raw.Account != perpsModule {
				t.Fatalf("raw %+v, want class %s on the module account", raw, decode.FailureAccountLookup)
			}
		}
		want := []decode.AccountLookup{
			{What: "position", ID: "900004", Height: xBlock - 1, Path: decode.LookupAtHeight, Missing: true},
			{What: "order", ID: "900005", Height: xBlock - 1, Path: decode.LookupAtHeight, Missing: true},
		}
		if !reflect.DeepEqual(res.Lookups, want) {
			t.Fatalf("lookups %+v, want %+v", res.Lookups, want)
		}
		if _, err := store.WriteFacts(ctx, tx, store.Block{Number: xBlock, Hash: xBlockHash, Time: xBlockTime}, res); err != nil {
			t.Fatal(err)
		}
		var stored int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM perps.raw_logs WHERE failure_class = $1 AND block = $2`, decode.FailureAccountLookup, xBlock).Scan(&stored); err != nil || stored != 2 {
			t.Fatalf("stored account_lookup raw logs %d, %v, want 2", stored, err)
		}
	})

	t.Run("other_revert_is_an_error", func(t *testing.T) {
		tx := lookupDB(t)
		state := newRecordedState(t)
		state.revert = state.abi.Errors["PerpsPriceStale"].ID.Bytes()[:4]
		_, err := registry(t, state).Decode(ctx, tx, []decode.Log{perpsLog(t, "PositionModified", 0, n(900006), n(10), n(5), n(5))})
		var revert *decode.RevertError
		if !errors.As(err, &revert) || !bytes.Equal(revert.Data, state.revert) {
			t.Fatalf("decode %v, want the PerpsPriceStale revert as an error", err)
		}
	})
}
