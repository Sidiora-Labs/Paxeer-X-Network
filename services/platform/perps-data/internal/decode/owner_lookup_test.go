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

// recordedState is a decode.StateReader over the perps getters getPosition and
// getOrder of the leaf ABI: an id in owners answers its packed outputs with
// the owner first, any other id reverts with the getter's unknown error, or
// with revert when set. pruned refuses every CallAt with ErrStatePruned.
type recordedState struct {
	t      *testing.T
	abi    abi.ABI
	owners map[string]common.Address
	pruned bool
	revert []byte
	calls  []stateCall
}

func newRecordedState(t *testing.T) *recordedState {
	t.Helper()
	a, err := perpsabi.ABI()
	if err != nil {
		t.Fatal(err)
	}
	return &recordedState{t: t, abi: a, owners: map[string]common.Address{}}
}

func (s *recordedState) callData(method string, id int64) []byte {
	s.t.Helper()
	data, err := s.abi.Pack(method, big.NewInt(id))
	if err != nil {
		s.t.Fatal(err)
	}
	return data
}

func (s *recordedState) own(method string, id int64, owner common.Address) {
	s.owners[string(s.callData(method, id))] = owner
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
	unknown := map[string]string{"getPosition": "PerpsUnknownPosition", "getOrder": "PerpsUnknownOrder"}[m.Name]
	if unknown == "" {
		return nil, fmt.Errorf("recorded state answers no %s", m.Name)
	}
	owner, ok := s.owners[string(data)]
	if !ok {
		if s.revert != nil {
			return nil, &decode.RevertError{Data: s.revert}
		}
		return nil, &decode.RevertError{Data: s.abi.Errors[unknown].ID.Bytes()[:4]}
	}
	values := make([]any, len(m.Outputs))
	values[0] = owner
	for i, out := range m.Outputs[1:] {
		values[i+1] = zeroOf(s.t, out.Type)
	}
	return m.Outputs.Pack(values...)
}

func zeroOf(t *testing.T, typ abi.Type) any {
	t.Helper()
	switch typ.T {
	case abi.AddressTy:
		return common.Address{}
	case abi.BoolTy:
		return false
	case abi.UintTy, abi.IntTy:
		if typ.Size == 8 {
			return uint8(0)
		}
		return new(big.Int)
	}
	t.Fatalf("no zero value of %s", typ)
	return nil
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
		state.own("getPosition", 900001, xAlice)
		state.own("getOrder", 900002, xBob)
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
			{decode.PerpsAddress, state.callData("getOrder", 900002), xBlock - 1, false},
		}
		if !reflect.DeepEqual(state.calls, wantCalls) {
			t.Fatalf("calls %+v, want one getter call per id at the height before the block", state.calls)
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
		if len(state.calls) != 2 || len(next.Lookups) != 0 {
			t.Fatalf("calls %d lookups %+v, the stored rows must answer without a call", len(state.calls), next.Lookups)
		}
	})

	t.Run("pruned_height_falls_back_to_latest", func(t *testing.T) {
		tx := lookupDB(t)
		state := newRecordedState(t)
		state.pruned = true
		state.own("getPosition", 900003, xCarol)
		res, err := registry(t, state).Decode(ctx, tx, []decode.Log{perpsLog(t, "PositionModified", 0, n(900003), n(10), n(5), n(5))})
		if err != nil {
			t.Fatal(err)
		}
		if got := accounts(res); !reflect.DeepEqual(got, []common.Address{xCarol}) {
			t.Fatalf("accounts %v, want carol from the latest state", got)
		}
		data := state.callData("getPosition", 900003)
		if want := []stateCall{{decode.PerpsAddress, data, xBlock - 1, false}, {decode.PerpsAddress, data, 0, true}}; !reflect.DeepEqual(state.calls, want) {
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
