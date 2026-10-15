package decode_test

import (
	"context"
	"errors"
	"math/big"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
	spotabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/spot/abi"
)

var (
	perpsModule     = decode.ModuleAddress("perps")
	spotModule      = decode.ModuleAddress("spot")
	pricefeedModule = decode.ModuleAddress("pricefeed")
	xBlockHash      = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000b10c2")
	xTxHash         = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a2")
	xSender         = common.HexToAddress("0x5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e")
	xAlice          = common.HexToAddress("0xa11ce0000000000000000000000000000000a11c")
	xBob            = common.HexToAddress("0xb0b0000000000000000000000000000000000b0b")
	xToken          = common.HexToAddress("0x70ce000000000000000000000000000000000070")
	xStable         = common.HexToAddress("0x5ab1e00000000000000000000000000000005ab1")
	xPointer        = common.HexToAddress("0x5ba2e00000000000000000000000000000005ba2")
	xBlockTime      = time.Unix(1800000000, 0).UTC()
)

const xBlock = 200

func registry(t *testing.T, state decode.StateReader) *decode.Registry {
	t.Helper()
	m, err := telemetry.New(config.Indexer)
	if err != nil {
		t.Fatal(err)
	}
	r, err := decode.NewRegistry(m, decode.Options{PerpsModule: perpsModule, SpotModule: spotModule, PricefeedModule: pricefeedModule, State: state})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// node returns the RPC pool of PERPS_DATA_TEST_RPC and refuses without it.
func node(t *testing.T) *source.RPC {
	t.Helper()
	url := os.Getenv("PERPS_DATA_TEST_RPC")
	if url == "" {
		t.Fatal("PERPS_DATA_TEST_RPC is required: the spot order lookups run eth_call against a node")
	}
	rpc, err := source.NewRPC(context.Background(), source.RPCOptions{URLs: []string{url}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rpc.Close)
	return rpc
}

func event(t *testing.T, load func() (abi.ABI, error), name string) abi.Event {
	t.Helper()
	a, err := load()
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := a.Events[name]
	if !ok {
		t.Fatalf("leaf ABI declares no event %s", name)
	}
	return ev
}

// pack packs values, in signature order, into a transaction log of address.
func pack(t *testing.T, ev abi.Event, address common.Address, index uint, values ...any) decode.Log {
	t.Helper()
	if len(values) != len(ev.Inputs) {
		t.Fatalf("%s takes %d values, got %d", ev.Name, len(ev.Inputs), len(values))
	}
	topics := []common.Hash{ev.ID}
	var data []any
	for i, in := range ev.Inputs {
		if !in.Indexed {
			data = append(data, values[i])
			continue
		}
		hashes, err := abi.MakeTopics([]any{values[i]})
		if err != nil {
			t.Fatalf("%s topic %s: %v", ev.Name, in.Name, err)
		}
		topics = append(topics, hashes[0][0])
	}
	packed, err := ev.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		t.Fatalf("%s pack: %v", ev.Name, err)
	}
	from := xSender
	return decode.Log{
		Log: ethtypes.Log{Address: address, Topics: topics, Data: packed, BlockNumber: xBlock, TxHash: xTxHash, TxIndex: 1,
			BlockHash: xBlockHash, Index: index},
		ReceiptKind: decode.ReceiptTx,
		TxFrom:      &from,
		BlockTime:   xBlockTime,
	}
}

func fact(emitter common.Address, index int64, kind string, account common.Address) decode.Fact {
	from := xSender
	return decode.Fact{Block: xBlock, BlockHash: xBlockHash, TxIndex: 1, LogIndex: index, TxHash: xTxHash, ReceiptKind: decode.ReceiptTx,
		BlockTime: xBlockTime, Emitter: emitter, Kind: kind, Account: account, TxFrom: &from}
}

func decodeRows(t *testing.T, r *decode.Registry, logs ...decode.Log) *decode.Result {
	t.Helper()
	res, err := r.Decode(context.Background(), nil, logs)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func wantRows(t *testing.T, res *decode.Result, want ...decode.Row) {
	t.Helper()
	if len(res.Raw) != 0 {
		t.Fatalf("raw rows %+v", res.Raw)
	}
	var got []decode.Row
	for _, d := range res.Decoded {
		got = append(got, d.Rows...)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rows\n got %+v\nwant %+v", got, want)
	}
}

func n(v int64) *big.Int { return big.NewInt(v) }

func TestSpotDecoders(t *testing.T) {
	pecor := func(name string) abi.Event { return event(t, spotabi.PECOR, name) }
	orders := func(name string) abi.Event { return event(t, spotabi.Orders, name) }
	stops := func(name string) abi.Event { return event(t, spotabi.StopOrders, name) }
	vault := func(name string) abi.Event { return event(t, spotabi.Vault, name) }
	share := func(name string) abi.Event { return event(t, spotabi.Share, name) }
	pool, vaultAddr := decode.SpotPoolAddress, decode.SpotVaultAddress

	offline := []struct {
		name string
		log  decode.Log
		want []decode.Row
	}{
		{"MarketOrderExecuted", pack(t, pecor("MarketOrderExecuted"), pool, 0, xAlice, xStable, xToken, n(1000), n(7), true),
			[]decode.Row{decode.SpotMarketOrderRow{fact(pool, 0, "market_order", xAlice), xAlice, xStable, xToken, "1000", "7", true}}},
		{"NativeSwap", pack(t, pecor("NativeSwap"), pool, 0, xAlice, xToken, n(50), n(9), false),
			[]decode.Row{decode.SpotNativeSwapRow{fact(pool, 0, "native_swap", xAlice), xAlice, xToken, "50", "9", false}}},
		{"SimpleSwap", pack(t, pecor("SimpleSwap"), pool, 0, xAlice, xToken, xStable, n(3), n(4), n(5)),
			[]decode.Row{decode.SpotSimpleSwapRow{fact(pool, 0, "simple_swap", xAlice), xAlice, xToken, xStable, "3", "4", "5"}}},
		{"TwapSanityUpdated", pack(t, pecor("TwapSanityUpdated"), pool, 0, true, n(600), n(250)),
			[]decode.Row{decode.SpotTwapSanityRow{fact(pool, 0, "twap_sanity_updated", spotModule), true, "600", "250"}}},
		{"PriceImpactApplied", pack(t, pecor("PriceImpactApplied"), pool, 0, xAlice, xToken, n(12), n(34)),
			[]decode.Row{decode.SpotPriceImpactRow{fact(pool, 0, "price_impact", xAlice), xAlice, xToken, "12", "34"}}},
		{"TieredFeeApplied", pack(t, pecor("TieredFeeApplied"), pool, 0, xAlice, xToken, n(100), n(30), n(3)),
			[]decode.Row{decode.SpotTieredFeeRow{fact(pool, 0, "tiered_fee", xAlice), xAlice, xToken, "100", "30", "3"}}},
		{"OwnershipTransferred", pack(t, pecor("OwnershipTransferred"), pool, 0, xAlice, xBob),
			[]decode.Row{decode.SpotOwnershipRow{fact(pool, 0, "ownership_transferred", spotModule), xAlice, xBob}}},
		{"Paused", pack(t, orders("Paused"), decode.SpotOrdersAddress, 0, xAlice),
			[]decode.Row{decode.SpotPauseRow{fact(decode.SpotOrdersAddress, 0, "paused", spotModule), xAlice}}},
		{"Unpaused", pack(t, stops("Unpaused"), decode.SpotStopAddress, 0, xAlice),
			[]decode.Row{decode.SpotPauseRow{fact(decode.SpotStopAddress, 0, "unpaused", spotModule), xAlice}}},
		{"Deposit", pack(t, vault("Deposit"), vaultAddr, 0, xToken, xAlice, n(10), n(110)),
			[]decode.Row{decode.SpotDepositRow{fact(vaultAddr, 0, "deposit", xAlice), xToken, xAlice, "10", "110"}}},
		{"LPWithdrawal", pack(t, vault("LPWithdrawal"), vaultAddr, 0, xToken, xAlice, n(2), n(20)),
			[]decode.Row{decode.SpotLPWithdrawalRow{fact(vaultAddr, 0, "lp_withdrawal", xAlice), xToken, xAlice, "2", "20"}}},
		{"NativeDeposit", pack(t, vault("NativeDeposit"), vaultAddr, 0, xAlice, n(10), n(110)),
			[]decode.Row{decode.SpotNativeDepositRow{fact(vaultAddr, 0, "native_deposit", xAlice), xAlice, "10", "110"}}},
		{"NativeWithdrawal", pack(t, vault("NativeWithdrawal"), vaultAddr, 0, xBob, n(10), n(100)),
			[]decode.Row{decode.SpotNativeWithdrawalRow{fact(vaultAddr, 0, "native_withdrawal", xBob), xBob, "10", "100"}}},
		{"Withdrawal", pack(t, vault("Withdrawal"), vaultAddr, 0, xToken, xBob, n(10), n(100)),
			[]decode.Row{decode.SpotWithdrawalRow{fact(vaultAddr, 0, "withdrawal", xBob), xToken, xBob, "10", "100"}}},
		{"ReservesSync", pack(t, vault("ReservesSync"), vaultAddr, 0, xToken, n(1), n(3), n(2)),
			[]decode.Row{decode.SpotReservesSyncRow{fact(vaultAddr, 0, "reserves_sync", spotModule), xToken, "1", "3", "2"}}},
		{"ReservesUpdated", pack(t, vault("ReservesUpdated"), vaultAddr, 0, xToken, n(1), n(3)),
			[]decode.Row{decode.SpotReservesUpdatedRow{fact(vaultAddr, 0, "reserves_updated", spotModule), xToken, "1", "3"}}},
		{"TokenRegistered", pack(t, vault("TokenRegistered"), vaultAddr, 0, xToken, uint8(18), false),
			[]decode.Row{decode.SpotTokenRegisteredRow{fact(vaultAddr, 0, "token_registered", spotModule), xToken, 18, false}}},
		{"ShareTokenCreated", pack(t, vault("ShareTokenCreated"), vaultAddr, 0, xToken, xPointer),
			[]decode.Row{decode.SpotShareTokenCreatedRow{fact(vaultAddr, 0, "share_token_created", spotModule), xToken, xPointer}}},
		{"OperatorUpdated", pack(t, vault("OperatorUpdated"), vaultAddr, 0, xBob, true),
			[]decode.Row{decode.SpotOperatorRow{fact(vaultAddr, 0, "operator_updated", spotModule), xBob, true}}},
		{"TokenPauseUpdated", pack(t, vault("TokenPauseUpdated"), vaultAddr, 0, xToken, true),
			[]decode.Row{decode.SpotTokenPauseRow{fact(vaultAddr, 0, "token_pause_updated", spotModule), xToken, true}}},
		{"TransactionTrackerUpdated", pack(t, vault("TransactionTrackerUpdated"), vaultAddr, 0, xBob),
			[]decode.Row{decode.SpotTrackerRow{fact(vaultAddr, 0, "tracker_updated", spotModule), xBob}}},
		{"UtilizationCapUpdated", pack(t, vault("UtilizationCapUpdated"), vaultAddr, 0, xToken, n(8000)),
			[]decode.Row{decode.SpotUtilizationCapRow{fact(vaultAddr, 0, "utilization_cap_updated", spotModule), xToken, "8000"}}},
	}
	for _, c := range offline {
		t.Run(c.name, func(t *testing.T) {
			wantRows(t, decodeRows(t, registry(t, nil), c.log), c.want...)
		})
	}

	created := pack(t, vault("ShareTokenCreated"), vaultAddr, 0, xToken, xPointer)
	createdRow := decode.SpotShareTokenCreatedRow{fact(vaultAddr, 0, "share_token_created", spotModule), xToken, xPointer}

	t.Run("Transfer", func(t *testing.T) {
		res := decodeRows(t, registry(t, nil), created, pack(t, share("Transfer"), xPointer, 1, xAlice, xBob, n(5)))
		wantRows(t, res, createdRow, decode.SpotShareTransferRow{fact(xPointer, 1, "transfer", xAlice), xPointer, xToken, xAlice, xBob, "5"})
	})
	t.Run("Approval", func(t *testing.T) {
		res := decodeRows(t, registry(t, nil), created, pack(t, share("Approval"), xPointer, 1, xAlice, xBob, n(5)))
		wantRows(t, res, createdRow, decode.SpotShareApprovalRow{fact(xPointer, 1, "approval", xAlice), xPointer, xToken, xAlice, xBob, "5"})
	})
	t.Run("share_OwnershipTransferred", func(t *testing.T) {
		res := decodeRows(t, registry(t, nil), created, pack(t, share("OwnershipTransferred"), xPointer, 1, xAlice, xBob))
		wantRows(t, res, createdRow, decode.SpotOwnershipRow{fact(xPointer, 1, "ownership_transferred", spotModule), xAlice, xBob})
	})

	t.Run("share_pointer_passes", func(t *testing.T) {
		// Pass two of the follower appends the pointer log after pass one;
		// the created row earlier in the batch resolves it.
		res := decodeRows(t, registry(t, nil), created, pack(t, share("Transfer"), xPointer, 1, common.Address{}, xBob, n(5)))
		wantRows(t, res, createdRow, decode.SpotShareTransferRow{fact(xPointer, 1, "transfer", xBob), xPointer, xToken, common.Address{}, xBob, "5"})
		// Without the created row and without state the pointer stays
		// unresolved and the log goes to raw_logs as share_lookup.
		lone := pack(t, share("Transfer"), xPointer, 1, xAlice, xBob, n(5))
		res = decodeRows(t, registry(t, nil), lone)
		if len(res.Decoded) != 0 || len(res.Raw) != 1 || res.Raw[0].FailureClass != decode.FailureShareLookup || res.Raw[0].Kind != "raw" ||
			res.Raw[0].Account != spotModule || res.Raw[0].Emitter != xPointer {
			t.Fatalf("unresolved pointer: %+v", res)
		}
		// A resolved pointer with a topic of no share event is unknown_topic.
		other := pack(t, vault("TokenRegistered"), xPointer, 1, xToken, uint8(6), true)
		res = decodeRows(t, registry(t, nil), created, other)
		if len(res.Raw) != 1 || res.Raw[0].FailureClass != decode.FailureUnknownTopic {
			t.Fatalf("pointer unknown topic: %+v", res.Raw)
		}
	})

	t.Run("share_mint_burn", func(t *testing.T) {
		mint := pack(t, share("Transfer"), xPointer, 1, common.Address{}, xBob, n(5))
		burn := pack(t, share("Transfer"), xPointer, 2, xAlice, common.Address{}, n(4))
		res := decodeRows(t, registry(t, nil), created, mint, burn)
		wantRows(t, res, createdRow,
			decode.SpotShareTransferRow{fact(xPointer, 1, "transfer", xBob), xPointer, xToken, common.Address{}, xBob, "5"},
			decode.SpotShareTransferRow{fact(xPointer, 2, "transfer", xAlice), xPointer, xToken, xAlice, common.Address{}, "4"})
	})

	t.Run("fee_events_by_topic", func(t *testing.T) {
		for i, address := range []common.Address{decode.SpotPoolAddress, decode.SpotOrdersAddress, decode.SpotStopAddress, decode.SpotQuoterAddress, decode.SpotVaultAddress} {
			impact := pack(t, pecor("PriceImpactApplied"), address, uint(2*i), xAlice, xToken, n(12), n(34))
			tiered := pack(t, pecor("TieredFeeApplied"), address, uint(2*i+1), xAlice, xToken, n(100), n(30), n(3))
			wantRows(t, decodeRows(t, registry(t, nil), impact, tiered),
				decode.SpotPriceImpactRow{fact(address, int64(2*i), "price_impact", xAlice), xAlice, xToken, "12", "34"},
				decode.SpotTieredFeeRow{fact(address, int64(2*i+1), "tiered_fee", xAlice), xAlice, xToken, "100", "30", "3"})
		}
	})

	t.Run("address_set", func(t *testing.T) {
		r := registry(t, nil)
		// 0x1024 is view-only: any log there but the fee events is unknown_topic.
		quoter := pack(t, pecor("SimpleSwap"), decode.SpotQuoterAddress, 0, xAlice, xToken, xStable, n(3), n(4), n(5))
		res := decodeRows(t, r, quoter)
		if len(res.Raw) != 1 || res.Raw[0].FailureClass != decode.FailureUnknownTopic {
			t.Fatalf("0x1024 log: %+v", res)
		}
		for _, foreign := range []string{"0x0000000000000000000000000000000000001026", "0x0000000000000000000000000000000000001027"} {
			l := pack(t, pecor("SimpleSwap"), common.HexToAddress(foreign), 0, xAlice, xToken, xStable, n(3), n(4), n(5))
			if _, err := r.Decode(context.Background(), nil, []decode.Log{l}); !errors.Is(err, decode.ErrForeignEmitter) {
				t.Fatalf("%s: %v", foreign, err)
			}
		}
	})

	// The order events resolve their token on the node of PERPS_DATA_TEST_RPC.
	type orderCase struct {
		name  string
		ev    abi.Event
		rail  common.Address
		view  string
		words int
	}
	orderCases := []orderCase{
		{"LimitOrderCreated", orders("LimitOrderCreated"), decode.SpotOrdersAddress, "getLimitOrder", 10},
		{"LimitOrderCancelled", orders("LimitOrderCancelled"), decode.SpotOrdersAddress, "getLimitOrder", 10},
		{"LimitOrderExecuted", orders("LimitOrderExecuted"), decode.SpotOrdersAddress, "getLimitOrder", 10},
		{"LimitOrderExpired", orders("LimitOrderExpired"), decode.SpotOrdersAddress, "getLimitOrder", 10},
		{"LimitOrderModified", orders("LimitOrderModified"), decode.SpotOrdersAddress, "getLimitOrder", 10},
		{"StopLimitOrderCreated", stops("StopLimitOrderCreated"), decode.SpotStopAddress, "getStopLimitOrder", 11},
		{"StopLimitActivated", stops("StopLimitActivated"), decode.SpotStopAddress, "getStopLimitOrder", 11},
		{"StopLimitCancelled", stops("StopLimitCancelled"), decode.SpotStopAddress, "getStopLimitOrder", 11},
		{"StopLimitExecuted", stops("StopLimitExecuted"), decode.SpotStopAddress, "getStopLimitOrder", 11},
		{"StopLossOrderCreated", stops("StopLossOrderCreated"), decode.SpotStopAddress, "getStopLossOrder", 9},
		{"StopLossOrderCancelled", stops("StopLossOrderCancelled"), decode.SpotStopAddress, "getStopLossOrder", 9},
		{"StopLossOrderExecuted", stops("StopLossOrderExecuted"), decode.SpotStopAddress, "getStopLossOrder", 9},
	}
	for _, c := range orderCases {
		t.Run(c.name, func(t *testing.T) {
			rpc := node(t)
			o := liveOrder(t, rpc, c.rail, c.view, c.words)
			values := []any{o.id}
			for _, in := range c.ev.Inputs[1:] {
				switch {
				case in.Name == "user":
					values = append(values, o.user)
				case in.Type.T == abi.UintTy && in.Type.Size == 8:
					values = append(values, o.orderType)
				default:
					values = append(values, n(77))
				}
			}
			res := decodeRows(t, registry(t, rpc), pack(t, c.ev, c.rail, 0, values...))
			if len(res.Raw) != 0 || len(res.Decoded) != 1 || len(res.Decoded[0].Rows) != 1 {
				t.Fatalf("result %+v", res)
			}
			row := reflect.ValueOf(res.Decoded[0].Rows[0])
			if f := row.FieldByName("Fact").Interface().(decode.Fact); f.Account != o.user || f.Emitter != c.rail {
				t.Fatalf("fact %+v, want account %s", f, o.user.Hex())
			}
			if got := row.FieldByName("Token").Interface().(common.Address); got != o.token {
				t.Fatalf("token %s, want %s", got.Hex(), o.token.Hex())
			}
			if got := row.FieldByName("Stablecoin").Interface().(common.Address); got != o.stablecoin {
				t.Fatalf("stablecoin %s, want %s", got.Hex(), o.stablecoin.Hex())
			}
			if got := row.FieldByName("OrderID").String(); got != o.id.String() {
				t.Fatalf("order id %s, want %s", got, o.id)
			}
		})
	}

	t.Run("order_token_lookup", func(t *testing.T) {
		rpc := node(t)
		o := liveOrder(t, rpc, decode.SpotOrdersAddress, "getLimitOrder", 10)
		head, err := rpc.Head(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		l := pack(t, orders("LimitOrderCancelled"), decode.SpotOrdersAddress, 0, o.id)
		l.BlockNumber = head
		res := decodeRows(t, registry(t, rpc), l)
		if len(res.Decoded) != 1 {
			t.Fatalf("result %+v", res)
		}
		row, ok := res.Decoded[0].Rows[0].(decode.SpotOrderIDRow)
		if !ok || row.Token != o.token || row.Stablecoin != o.stablecoin || row.Account != o.user || row.Kind != "limit_cancelled" {
			t.Fatalf("row %+v, want token %s stablecoin %s user %s", res.Decoded[0].Rows[0], o.token.Hex(), o.stablecoin.Hex(), o.user.Hex())
		}
	})

	t.Run("order_token_latest_fallback", func(t *testing.T) {
		rpc := node(t)
		o := liveOrder(t, rpc, decode.SpotOrdersAddress, "getLimitOrder", 10)
		data := append(common.FromHex("0x53061026"), common.LeftPadBytes(o.id.Bytes(), 32)...)
		if _, err := rpc.CallAt(context.Background(), decode.SpotOrdersAddress, data, 1); !errors.Is(err, decode.ErrStatePruned) {
			t.Fatalf("height 1 must be pruned on every node of PERPS_DATA_TEST_RPC for the fallback, got %v", err)
		}
		l := pack(t, orders("LimitOrderCancelled"), decode.SpotOrdersAddress, 0, o.id)
		l.BlockNumber = 1
		res := decodeRows(t, registry(t, rpc), l)
		row, ok := res.Decoded[0].Rows[0].(decode.SpotOrderIDRow)
		if !ok || row.Token != o.token || row.Stablecoin != o.stablecoin {
			t.Fatalf("row %+v", res.Decoded[0].Rows[0])
		}
	})

	t.Run("order_lookup_failure", func(t *testing.T) {
		rpc := node(t)
		unknown := new(big.Int).Lsh(big.NewInt(1), 255)
		res := decodeRows(t, registry(t, rpc), pack(t, orders("LimitOrderCancelled"), decode.SpotOrdersAddress, 0, unknown))
		if len(res.Decoded) != 0 || len(res.Raw) != 1 || res.Raw[0].FailureClass != decode.FailureOrderLookup || res.Raw[0].Account != spotModule {
			t.Fatalf("result %+v", res)
		}
	})

	t.Run("order_side", func(t *testing.T) {
		rpc := node(t)
		o := liveOrder(t, rpc, decode.SpotOrdersAddress, "getLimitOrder", 10)
		cases := []struct {
			ev        abi.Event
			rail      common.Address
			orderType uint8
			side      string
		}{
			{orders("LimitOrderCreated"), decode.SpotOrdersAddress, decode.SpotLimitBuy, "buy"},
			{orders("LimitOrderCreated"), decode.SpotOrdersAddress, decode.SpotLimitSell, "sell"},
		}
		for _, c := range cases {
			res := decodeRows(t, registry(t, rpc), pack(t, c.ev, c.rail, 0, o.id, o.user, c.orderType, n(1), n(2)))
			row, ok := res.Decoded[0].Rows[0].(decode.SpotLimitCreatedRow)
			if side, known := row.Side(); !ok || !known || side != c.side {
				t.Fatalf("orderType %d side %q, want %q", c.orderType, side, c.side)
			}
		}
		if side, known := (decode.SpotStopLossCreatedRow{}).Side(); !known || side != "sell" {
			t.Fatalf("stop-loss side %q", side)
		}
	})
}

type order struct {
	id                      *big.Int
	user, stablecoin, token common.Address
	orderType               uint8
}

// liveOrder finds the newest order the rail of the node knows.
func liveOrder(t *testing.T, rpc *source.RPC, rail common.Address, view string, words int) order {
	t.Helper()
	ctx := context.Background()
	load := spotabi.Orders
	if rail == decode.SpotStopAddress {
		load = spotabi.StopOrders
	}
	a, err := load()
	if err != nil {
		t.Fatal(err)
	}
	out, err := rpc.Call(ctx, rail, a.Methods["nextOrderId"].ID)
	if err != nil || len(out) != 32 {
		t.Fatalf("nextOrderId: %x %v", out, err)
	}
	m := a.Methods[view]
	for id := new(big.Int).SetBytes(out); id.Sign() > 0; id.Sub(id, big.NewInt(1)) {
		out, err := rpc.Call(ctx, rail, append(append([]byte(nil), m.ID...), common.LeftPadBytes(id.Bytes(), 32)...))
		if err != nil {
			t.Fatalf("%s(%s): %v", view, id, err)
		}
		if len(out) != 32*words {
			t.Fatalf("%s(%s) returned %d bytes", view, id, len(out))
		}
		if new(big.Int).SetBytes(out[:32]).Sign() == 0 {
			continue
		}
		o := order{id: new(big.Int).Set(id), user: common.BytesToAddress(out[32:64]), stablecoin: common.BytesToAddress(out[64:96]),
			token: common.BytesToAddress(out[96:128])}
		if view != "getStopLossOrder" {
			typeWord := 6
			if view == "getStopLimitOrder" {
				typeWord = 7
			}
			o.orderType = uint8(new(big.Int).SetBytes(out[32*typeWord : 32*typeWord+32]).Uint64())
		}
		return o
	}
	t.Fatalf("the node holds no order on %s", view)
	return order{}
}
