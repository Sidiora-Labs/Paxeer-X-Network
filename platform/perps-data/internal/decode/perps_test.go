package decode

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"math/big"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/bigint"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
	perpsabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/perps/abi"
)

var (
	testModule = moduleAddress("perps")
	blockHash  = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000b10c1")
	txHash     = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000a1")
	endTxHash  = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000000e1")
	sender     = common.HexToAddress("0x5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e5e")
	alice      = common.HexToAddress("0xa11ce0000000000000000000000000000000a11c")
	bob        = common.HexToAddress("0xb0b0000000000000000000000000000000000b0b")
	carol      = common.HexToAddress("0xca201000000000000000000000000000000ca201")
	usdl       = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	blockTime  = time.Unix(1800000000, 0).UTC()
	cloid      = [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
)

// moduleAddress is authtypes.NewModuleAddress: the first 20 bytes of the
// sha256 of the module name.
func moduleAddress(name string) common.Address {
	sum := sha256.Sum256([]byte(name))
	return common.BytesToAddress(sum[:common.AddressLength])
}

func u(t *testing.T, s string) *big.Int {
	t.Helper()
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("bad integer %q", s)
	}
	return v
}

func newTestRegistry(t *testing.T) (*Registry, *telemetry.Metrics) {
	t.Helper()
	m, err := telemetry.New(config.Indexer)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewRegistry(m, Options{PerpsModule: testModule, SpotModule: ModuleAddress("spot"), PricefeedModule: ModuleAddress("pricefeed")})
	if err != nil {
		t.Fatal(err)
	}
	return r, m
}

func perpsEvent(t *testing.T, name string) abi.Event {
	t.Helper()
	a, err := perpsabi.ABI()
	if err != nil {
		t.Fatal(err)
	}
	ev, ok := a.Events[name]
	if !ok {
		t.Fatalf("leaf ABI declares no event %s", name)
	}
	return ev
}

// packLog packs values, in signature order, into a transaction log of 0x1015
// with the leaf ABI.
func packLog(t *testing.T, name string, index uint, values ...any) Log {
	t.Helper()
	ev := perpsEvent(t, name)
	if len(values) != len(ev.Inputs) {
		t.Fatalf("%s takes %d values, got %d", name, len(ev.Inputs), len(values))
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
			t.Fatalf("%s topic %s: %v", name, in.Name, err)
		}
		topics = append(topics, hashes[0][0])
	}
	packed, err := ev.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		t.Fatalf("%s pack: %v", name, err)
	}
	from := sender
	return Log{
		Log: ethtypes.Log{
			Address:     PerpsAddress,
			Topics:      topics,
			Data:        packed,
			BlockNumber: 100,
			TxHash:      txHash,
			TxIndex:     2,
			BlockHash:   blockHash,
			Index:       index,
		},
		ReceiptKind: ReceiptTx,
		TxFrom:      &from,
		BlockTime:   blockTime,
	}
}

// endBlock moves l into the perps end-block receipt of the block.
func endBlock(l Log) Log {
	l.TxHash = endTxHash
	l.TxIndex = 3
	l.ReceiptKind = ReceiptPerpsEndBlock
	l.TxFrom = nil
	return l
}

func txFact(index int64, kind string, account common.Address) Fact {
	from := sender
	return Fact{Block: 100, BlockHash: blockHash, TxIndex: 2, LogIndex: index, TxHash: txHash, ReceiptKind: ReceiptTx,
		BlockTime: blockTime, Emitter: PerpsAddress, Kind: kind, Account: account, TxFrom: &from}
}

func endFact(index int64, kind string, account common.Address) Fact {
	return Fact{Block: 100, BlockHash: blockHash, TxIndex: 3, LogIndex: index, TxHash: endTxHash, ReceiptKind: ReceiptPerpsEndBlock,
		BlockTime: blockTime, Emitter: PerpsAddress, Kind: kind, Account: account}
}

func counter(t *testing.T, m *telemetry.Metrics, name string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := m.WritePrometheus(&buf); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(buf.String(), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[0] == name {
			return f[1]
		}
	}
	t.Fatalf("metric %s not exported", name)
	return ""
}

func decodeOne(t *testing.T, r *Registry, logs ...Log) Decoded {
	t.Helper()
	res, err := r.Decode(context.Background(), nil, logs)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Raw) != 0 {
		t.Fatalf("raw rows %+v", res.Raw)
	}
	if len(res.Decoded) != len(logs) {
		t.Fatalf("decoded %d logs, want %d", len(res.Decoded), len(logs))
	}
	return res.Decoded[len(res.Decoded)-1]
}

func TestPerpsDecoders(t *testing.T) {
	type eventCase struct {
		name    string
		prelude []Log
		log     Log
		want    []Row
	}
	placed := func(id int64, user common.Address) Log {
		return packLog(t, "OrderPlaced", 0, big.NewInt(id), user, big.NewInt(0), uint8(1), true, u(t, "59000000000000000000000"), u(t, "6000000000000000000000"))
	}
	opened := func(id int64, user common.Address) Log {
		return packLog(t, "PositionOpened", 0, big.NewInt(id), user, big.NewInt(0), true, u(t, "990000000000000000000"),
			u(t, "10000000000000000000"), u(t, "60000000000000000000000"), usdl, big.NewInt(100000000))
	}
	cases := []eventCase{
		{name: "VaultCreated", log: packLog(t, "VaultCreated", 4, alice, PerpsAddress),
			want: []Row{VaultCreatedRow{txFact(4, "created", alice), alice, PerpsAddress}}},
		{name: "CollateralDeposited", log: packLog(t, "CollateralDeposited", 4, alice, big.NewInt(250000000), big.NewInt(250000000)),
			want: []Row{CollateralDepositedRow{txFact(4, "deposited", alice), alice, "250000000", "250000000"}}},
		{name: "CollateralWithdrawn", log: packLog(t, "CollateralWithdrawn", 4, alice, big.NewInt(150000000), big.NewInt(100000000)),
			want: []Row{CollateralWithdrawnRow{txFact(4, "withdrawn", alice), alice, "150000000", "100000000"}}},
		{name: "CollateralTransferred", log: packLog(t, "CollateralTransferred", 4, alice, bob, big.NewInt(60000000)),
			want: func() []Row {
				out, in := txFact(4, "transferred_out", alice), txFact(4, "transferred_in", bob)
				in.Sub = 1
				return []Row{CollateralTransferredRow{out, alice, bob, "60000000"}, CollateralTransferredRow{in, alice, bob, "60000000"}}
			}()},
		{name: "OrderPlaced", log: packLog(t, "OrderPlaced", 4, big.NewInt(1), alice, big.NewInt(0), uint8(0), true,
			u(t, "59000000000000000000000"), u(t, "5999710000000000000000")),
			want: []Row{OrderPlacedRow{txFact(4, "placed", alice), "1", alice, "0", 0, true, "59000000000000000000000", "5999710000000000000000"}}},
		{name: "OrderOptions", prelude: []Log{placed(1, alice)},
			log: packLog(t, "OrderOptions", 4, big.NewInt(1), uint8(2), true, u(t, "59000000000000000000000"), u(t, "59500000000000000000000"),
				uint64(1800000100), cloid),
			want: []Row{OrderOptionsRow{txFact(4, "options", alice), "1", 2, true, "59000000000000000000000", "59500000000000000000000", 1800000100, cloid}}},
		{name: "OrderCancelled", log: packLog(t, "OrderCancelled", 4, big.NewInt(1), alice),
			want: []Row{OrderCancelledRow{txFact(4, "cancelled", alice), "1", alice}}},
		{name: "OrderExpired", log: endBlock(packLog(t, "OrderExpired", 4, big.NewInt(4), alice, big.NewInt(0))),
			want: []Row{OrderExpiredRow{endFact(4, "expired", alice), "4", alice, "0"}}},
		{name: "OrderTriggered", prelude: []Log{placed(3, alice)},
			log:  endBlock(packLog(t, "OrderTriggered", 4, big.NewInt(3), big.NewInt(0), u(t, "61050000000000000000000"))),
			want: []Row{OrderTriggeredRow{endFact(4, "triggered", alice), "3", "0", "61050000000000000000000"}}},
		{name: "OrderExecuted", prelude: []Log{placed(5, bob)},
			log:  packLog(t, "OrderExecuted", 4, big.NewInt(5), big.NewInt(7), u(t, "60050000000000000000000")),
			want: []Row{OrderExecutedRow{txFact(4, "executed", bob), "5", "7", "60050000000000000000000"}}},
		{name: "Fill", log: packLog(t, "Fill", 4, big.NewInt(0), big.NewInt(1), big.NewInt(5), alice, bob, true,
			u(t, "60000000000000000000000"), u(t, "600000000000000000000"), u(t, "-12000000000000000"), u(t, "270000000000000000")),
			want: []Row{FillRow{txFact(4, "fill", bob), "0", "1", "5", alice, bob, true, "60000000000000000000000", "600000000000000000000",
				"-12000000000000000", "270000000000000000"}}},
		{name: "PositionOpened", log: packLog(t, "PositionOpened", 4, big.NewInt(7), alice, big.NewInt(0), true, u(t, "990000000000000000000"),
			u(t, "10000000000000000000"), u(t, "60000000000000000000000"), usdl, big.NewInt(100000000)),
			want: []Row{PositionOpenedRow{txFact(4, "opened", alice), "7", alice, "0", true, "990000000000000000000", "10000000000000000000",
				"60000000000000000000000", usdl, "100000000"}}},
		{name: "PositionModified", prelude: []Log{opened(7, alice)},
			log:  packLog(t, "PositionModified", 4, big.NewInt(7), u(t, "1517760000000000000000"), u(t, "1000000000000000000000"), big.NewInt(100000000)),
			want: []Row{PositionModifiedRow{txFact(4, "modified", alice), "7", "1517760000000000000000", "1000000000000000000000", "100000000"}}},
		{name: "PositionEntryPriceChanged", prelude: []Log{opened(7, alice)},
			log:  packLog(t, "PositionEntryPriceChanged", 4, big.NewInt(7), u(t, "60000000000000000000000"), u(t, "60651960784313725490196")),
			want: []Row{PositionEntryPriceChangedRow{txFact(4, "entry_price_changed", alice), "7", "60000000000000000000000", "60651960784313725490196"}}},
		{name: "PositionClosed", log: packLog(t, "PositionClosed", 4, big.NewInt(7), alice, big.NewInt(0), u(t, "1006500000000000000000"),
			u(t, "59000000000000000000000"), u(t, "-16500000000000000000"), true),
			want: []Row{PositionClosedRow{txFact(4, "closed", alice), "7", alice, "0", "1006500000000000000000", "59000000000000000000000",
				"-16500000000000000000", true}}},
		{name: "PositionTpSlSet", prelude: []Log{opened(7, alice)},
			log:  packLog(t, "PositionTpSlSet", 4, big.NewInt(7), u(t, "66000000000000000000000"), u(t, "57000000000000000000000")),
			want: []Row{PositionTpSlSetRow{txFact(4, "tpsl_set", alice), "7", "66000000000000000000000", "57000000000000000000000"}}},
		{name: "TradeSettled", log: packLog(t, "TradeSettled", 4, big.NewInt(7), alice, big.NewInt(0), TradeClose, u(t, "1006500000000000000000"),
			u(t, "61000000000000000000000"), u(t, "16500000000000000000"), u(t, "452925000000000000"), u(t, "-50000000000000000"), big.NewInt(15997075)),
			want: []Row{TradeSettledRow{txFact(4, "settled", alice), "7", alice, "0", 1, "1006500000000000000000", "61000000000000000000000",
				"16500000000000000000", "452925000000000000", "-50000000000000000", "15997075"}}},
		{name: "FeeCollected", log: packLog(t, "FeeCollected", 4, big.NewInt(7), alice, big.NewInt(0), FeeMakerRebate, u(t, "12000000000000000"),
			big.NewInt(12000), usdl),
			want: []Row{FeeCollectedRow{txFact(4, "collected", alice), "7", alice, "0", 4, "12000000000000000", "12000", usdl}}},
		{name: "MarkPriceUpdated", log: endBlock(packLog(t, "MarkPriceUpdated", 4, big.NewInt(0), u(t, "60095000000000000000000"),
			u(t, "60100000000000000000000"), u(t, "60090000000000000000000"))),
			want: []Row{MarkPriceUpdatedRow{endFact(4, "mark", testModule), "0", "60095000000000000000000", "60100000000000000000000", "60090000000000000000000"}}},
		{name: "FundingRateUpdated", log: endBlock(packLog(t, "FundingRateUpdated", 4, big.NewInt(0), big.NewInt(-3472222222), u(t, "-300000000000000"))),
			want: []Row{FundingRateUpdatedRow{endFact(4, "rate", testModule), "0", "-3472222222", "-300000000000000"}}},
		{name: "FundingSettled", log: endBlock(packLog(t, "FundingSettled", 4, big.NewInt(0), big.NewInt(12500000000000),
			u(t, "-1500000000000000000"), u(t, "1500000000000000000"))),
			want: []Row{FundingSettledRow{endFact(4, "settled", testModule), "0", "12500000000000", "-1500000000000000000", "1500000000000000000"}}},
		{name: "PositionFundingApplied", log: endBlock(packLog(t, "PositionFundingApplied", 4, big.NewInt(7), alice, big.NewInt(0),
			u(t, "-750000000000000000"), u(t, "999250000000000000000"), big.NewInt(99250000))),
			want: []Row{PositionFundingAppliedRow{endFact(4, "applied", alice), "7", alice, "0", "-750000000000000000", "999250000000000000000", "99250000"}}},
		{name: "OpenInterestChanged", log: packLog(t, "OpenInterestChanged", 4, big.NewInt(0), u(t, "600000000000000000000"),
			u(t, "600000000000000000000"), u(t, "600000000000000000000"), true),
			want: []Row{OpenInterestChangedRow{txFact(4, "oi", testModule), "0", "600000000000000000000", "600000000000000000000", "600000000000000000000", true}}},
		{name: "Liquidation", log: endBlock(packLog(t, "Liquidation", 4, big.NewInt(7), alice, big.NewInt(0), u(t, "59000000000000000000000"),
			big.NewInt(0), common.Address{})),
			want: []Row{LiquidationRow{endFact(4, "liquidated", alice), "7", alice, "0", "59000000000000000000000", "0", common.Address{}}}},
		{name: "BackstopLiquidation", log: endBlock(packLog(t, "BackstopLiquidation", 4, alice, big.NewInt(0), u(t, "60000000000000000000000"),
			u(t, "59990000000000000000000"), u(t, "18000000000000000000"))),
			want: []Row{BackstopLiquidationRow{endFact(4, "backstop", alice), alice, "0", "60000000000000000000000", "59990000000000000000000", "18000000000000000000"}}},
		{name: "InsuranceContribution", log: endBlock(packLog(t, "InsuranceContribution", 4, usdl, big.NewInt(18000000), uint8(1))),
			want: []Row{InsuranceContributionRow{endFact(4, "contribution", testModule), usdl, "18000000", 1}}},
		{name: "InsuranceDraw", log: endBlock(packLog(t, "InsuranceDraw", 4, big.NewInt(0), big.NewInt(400000000), big.NewInt(4600000000))),
			want: []Row{InsuranceDrawRow{endFact(4, "draw", testModule), "0", "400000000", "4600000000"}}},
		{name: "VaultDeficit", log: endBlock(packLog(t, "VaultDeficit", 4, usdl, big.NewInt(1000000000))),
			want: []Row{VaultDeficitRow{endFact(4, "vault_deficit", testModule), usdl, "1000000000"}}},
		{name: "ADLExecuted", prelude: []Log{opened(8, carol)},
			log:  endBlock(packLog(t, "ADLExecuted", 4, big.NewInt(8), u(t, "6000000000000000000000"))),
			want: []Row{ADLExecutedRow{endFact(4, "adl", carol), "8", "6000000000000000000000"}}},
		{name: "ADLMatched", log: endBlock(packLog(t, "ADLMatched", 4, big.NewInt(0), big.NewInt(9), carol, u(t, "6000000000000000000000"),
			u(t, "60000000000000000000000"), u(t, "5500000000000000000"))),
			want: []Row{ADLMatchedRow{endFact(4, "matched", carol), "0", "9", carol, "6000000000000000000000", "60000000000000000000000", "5500000000000000000"}}},
	}
	if len(cases) != 30 {
		t.Fatalf("%d event cases, the decoding table has 30 rows at 0x1015", len(cases))
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, m := newTestRegistry(t)
			got := decodeOne(t, r, append(append([]Log(nil), c.prelude...), c.log)...)
			if got.Event.EventName != c.name || got.Event.Sig != perpsEvent(t, c.name).Sig {
				t.Fatalf("event %s %s", got.Event.EventName, got.Event.Sig)
			}
			if got.Event.BlockNumber != 100 || got.Event.LogIndex != 4 || got.Event.TxHash != strings.ToLower(c.log.TxHash.Hex()) {
				t.Fatalf("event position %+v", got.Event)
			}
			if !reflect.DeepEqual(got.Rows, c.want) {
				t.Fatalf("rows\n got %+v\nwant %+v", got.Rows, c.want)
			}
			if v := counter(t, m, "perps_indexer_unknown_logs_total"); v != "0" {
				t.Fatalf("unknown logs %s", v)
			}
			if v := counter(t, m, "perps_indexer_enum_unknown_total"); v != "0" {
				t.Fatalf("enum unknown %s", v)
			}
		})
	}

	t.Run("unknown_topic", func(t *testing.T) {
		r, m := newTestRegistry(t)
		unknown := packLog(t, "VaultDeficit", 1, usdl, big.NewInt(1))
		unknown.Topics = []common.Hash{crypto.Keccak256Hash([]byte("VaultDeficitChanged(address,uint256)")), unknown.Topics[1]}
		bare := endBlock(packLog(t, "VaultDeficit", 2, usdl, big.NewInt(2)))
		bare.Topics = nil
		res, err := r.Decode(context.Background(), nil, []Log{unknown, bare})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Decoded) != 0 {
			t.Fatalf("decoded %+v", res.Decoded)
		}
		want := []RawLogRow{
			{Fact: txFact(1, "raw", testModule), Topics: unknown.Topics, Data: unknown.Data, FailureClass: FailureUnknownTopic},
			{Fact: endFact(2, "raw", testModule), Data: bare.Data, FailureClass: FailureUnknownTopic},
		}
		if !reflect.DeepEqual(res.Raw, want) {
			t.Fatalf("raw\n got %+v\nwant %+v", res.Raw, want)
		}
		if v := counter(t, m, "perps_indexer_unknown_logs_total"); v != "2" {
			t.Fatalf("unknown logs %s, want 2", v)
		}
	})

	t.Run("abi_decode", func(t *testing.T) {
		r, m := newTestRegistry(t)
		short := packLog(t, "VaultDeficit", 1, usdl, big.NewInt(1))
		short.Data = short.Data[:31]
		wideBool := packLog(t, "OpenInterestChanged", 2, big.NewInt(0), big.NewInt(1), big.NewInt(1), big.NewInt(1), true)
		wideBool.Data = append([]byte(nil), wideBool.Data...)
		wideBool.Data[len(wideBool.Data)-1] = 2
		extraTopic := packLog(t, "VaultDeficit", 3, usdl, big.NewInt(1))
		extraTopic.Topics = append(extraTopic.Topics, common.Hash{})
		res, err := r.Decode(context.Background(), nil, []Log{short, wideBool, extraTopic})
		if err != nil {
			t.Fatal(err)
		}
		want := []RawLogRow{
			{Fact: txFact(1, "raw", testModule), Topics: short.Topics, Data: short.Data, FailureClass: FailureABIDecode},
			{Fact: txFact(2, "raw", testModule), Topics: wideBool.Topics, Data: wideBool.Data, FailureClass: FailureABIDecode},
			{Fact: txFact(3, "raw", testModule), Topics: extraTopic.Topics, Data: extraTopic.Data, FailureClass: FailureABIDecode},
		}
		if len(res.Decoded) != 0 || !reflect.DeepEqual(res.Raw, want) {
			t.Fatalf("decoded %+v raw %+v", res.Decoded, res.Raw)
		}
		if v := counter(t, m, "perps_indexer_unknown_logs_total"); v != "0" {
			t.Fatalf("unknown logs %s, want 0", v)
		}
	})

	t.Run("duplicate_log", func(t *testing.T) {
		r, _ := newTestRegistry(t)
		l := packLog(t, "CollateralDeposited", 1, alice, big.NewInt(5), big.NewInt(5))
		res, err := r.Decode(context.Background(), nil, []Log{l, l, l})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Decoded) != 1 || len(res.Raw) != 0 {
			t.Fatalf("decoded %d raw %d, want 1 and 0", len(res.Decoded), len(res.Raw))
		}
		unique, err := Dedup([]Log{l, l})
		if err != nil || len(unique) != 1 || !reflect.DeepEqual(unique[0], l) {
			t.Fatalf("dedup %v %v", unique, err)
		}
		conflict := packLog(t, "CollateralDeposited", 1, alice, big.NewInt(6), big.NewInt(6))
		if _, err := r.Decode(context.Background(), nil, []Log{l, conflict}); !errors.Is(err, ErrInvalidLog) {
			t.Fatalf("conflicting identity: %v", err)
		}
		removed := l
		removed.Removed = true
		if _, err := r.Decode(context.Background(), nil, []Log{removed}); !errors.Is(err, ErrInvalidLog) {
			t.Fatalf("removed log: %v", err)
		}
		noHash := l
		noHash.BlockHash = common.Hash{}
		if _, err := Dedup([]Log{noHash}); !errors.Is(err, ErrInvalidLog) {
			t.Fatalf("zero block hash: %v", err)
		}
	})

	t.Run("account_rule", func(t *testing.T) {
		r, _ := newTestRegistry(t)
		dep := decodeOne(t, r, packLog(t, "CollateralDeposited", 1, alice, big.NewInt(5), big.NewInt(5)))
		f := dep.Rows[0].Common()
		if f.Account != alice || f.TxFrom == nil || *f.TxFrom != sender {
			t.Fatalf("account %s tx_from %v, want the account argument and the sender", f.Account, f.TxFrom)
		}
		mark := decodeOne(t, r, endBlock(packLog(t, "MarkPriceUpdated", 1, big.NewInt(0), big.NewInt(1), big.NewInt(1), big.NewInt(1))))
		f = mark.Rows[0].Common()
		if f.Account != testModule || f.Account == PerpsAddress || f.TxFrom != nil {
			t.Fatalf("account %s tx_from %v, want the module address and no sender", f.Account, f.TxFrom)
		}
		txMark := decodeOne(t, r, packLog(t, "OpenInterestChanged", 1, big.NewInt(0), big.NewInt(1), big.NewInt(1), big.NewInt(1), false))
		if f = txMark.Rows[0].Common(); f.Account != testModule || *f.TxFrom != sender {
			t.Fatalf("account %s, the sender never supplies the account", f.Account)
		}
		options := packLog(t, "OrderOptions", 1, big.NewInt(42), uint8(0), false, big.NewInt(0), big.NewInt(0), uint64(0), cloid)
		if _, err := r.Decode(context.Background(), nil, []Log{options}); !errors.Is(err, ErrAccountUnresolved) {
			t.Fatalf("options without a placed row: %v", err)
		}
		late := packLog(t, "OrderPlaced", 2, big.NewInt(42), bob, big.NewInt(0), uint8(0), true, big.NewInt(0), big.NewInt(1))
		if _, err := r.Decode(context.Background(), nil, []Log{options, late}); !errors.Is(err, ErrAccountUnresolved) {
			t.Fatalf("a later placed row must not supply the account: %v", err)
		}
		late.Index = 0
		got := decodeOne(t, r, late, options)
		if f = got.Rows[0].Common(); f.Account != bob || *f.TxFrom != sender {
			t.Fatalf("options account %s, want the placed user", f.Account)
		}
		modified := packLog(t, "PositionModified", 1, big.NewInt(77), big.NewInt(1), big.NewInt(1), big.NewInt(1))
		if _, err := r.Decode(context.Background(), nil, []Log{modified}); !errors.Is(err, ErrAccountUnresolved) {
			t.Fatalf("modified without an opened row: %v", err)
		}
		noSender := packLog(t, "VaultDeficit", 1, usdl, big.NewInt(1))
		noSender.TxFrom = nil
		if _, err := r.Decode(context.Background(), nil, []Log{noSender}); !errors.Is(err, ErrInvalidLog) {
			t.Fatalf("tx receipt without sender: %v", err)
		}
		endSender := endBlock(packLog(t, "VaultDeficit", 1, usdl, big.NewInt(1)))
		endSender.TxFrom = &sender
		if _, err := r.Decode(context.Background(), nil, []Log{endSender}); !errors.Is(err, ErrInvalidLog) {
			t.Fatalf("end-block receipt with sender: %v", err)
		}
		blockEvent := endBlock(packLog(t, "VaultDeficit", 1, usdl, big.NewInt(1)))
		blockEvent.ReceiptKind = ReceiptBlockEvent
		if _, err := r.Decode(context.Background(), nil, []Log{blockEvent}); !errors.Is(err, ErrInvalidLog) {
			t.Fatalf("block_event receipt kind on a log: %v", err)
		}
		foreign := packLog(t, "VaultDeficit", 1, usdl, big.NewInt(1))
		foreign.Address = common.HexToAddress("0x0000000000000000000000000000000000001026")
		if _, err := r.Decode(context.Background(), nil, []Log{foreign}); !errors.Is(err, ErrForeignEmitter) {
			t.Fatalf("foreign emitter: %v", err)
		}
	})

	t.Run("signed_columns", func(t *testing.T) {
		r, _ := newTestRegistry(t)
		got := decodeOne(t, r, packLog(t, "TradeSettled", 1, u(t, bigint.Uint256Max), alice, big.NewInt(0), TradeLiquidation,
			u(t, bigint.Uint256Max), big.NewInt(1), u(t, bigint.Int256Min), big.NewInt(0), u(t, bigint.Int256Max), big.NewInt(0)))
		row := got.Rows[0].(TradeSettledRow)
		if row.PositionID != bigint.Uint256Max || row.SizeUSD != bigint.Uint256Max || row.GrossPnl != bigint.Int256Min ||
			row.FundingPaidUSD != bigint.Int256Max || row.TotalFeesUSD != "0" {
			t.Fatalf("row %+v", row)
		}
		if got.Event.Args["grossPnl"] != bigint.Int256Min || got.Event.Args["user"] != strings.ToLower(alice.Hex()) {
			t.Fatalf("args %+v", got.Event.Args)
		}
		funding := decodeOne(t, r, endBlock(packLog(t, "FundingSettled", 1, big.NewInt(0), big.NewInt(-1), u(t, bigint.Int256Min), u(t, bigint.Int256Max))))
		fs := funding.Rows[0].(FundingSettledRow)
		for _, s := range []string{fs.FundingRate, fs.LongPayment, fs.ShortPayment} {
			if err := bigint.ValidateRawInt256(s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		if fs.FundingRate != "-1" || fs.LongPayment != bigint.Int256Min || fs.ShortPayment != bigint.Int256Max {
			t.Fatalf("row %+v", fs)
		}
		if err := bigint.ValidateRaw(fs.LongPayment); err == nil {
			t.Fatal("a negative signed column must not pass the unsigned check")
		}
		fill := decodeOne(t, r, packLog(t, "Fill", 1, big.NewInt(0), big.NewInt(1), big.NewInt(2), alice, bob, false, big.NewInt(1), big.NewInt(1),
			big.NewInt(-1), big.NewInt(0)))
		if fr := fill.Rows[0].(FillRow); fr.MakerFeeUSD != "-1" || fr.TakerFeeUSD != "0" || fr.TakerIsLong {
			t.Fatalf("row %+v", fr)
		}
	})

	t.Run("enum_unknown", func(t *testing.T) {
		r, m := newTestRegistry(t)
		trade := func(index uint, tradeType uint8) Log {
			return packLog(t, "TradeSettled", index, big.NewInt(1), alice, big.NewInt(0), tradeType, big.NewInt(1), big.NewInt(1),
				big.NewInt(0), big.NewInt(0), big.NewInt(0), big.NewInt(0))
		}
		fee := func(index uint, feeType uint8) Log {
			return packLog(t, "FeeCollected", index, big.NewInt(1), alice, big.NewInt(0), feeType, big.NewInt(1), big.NewInt(1), usdl)
		}
		var logs []Log
		for i := uint8(0); i <= TradeBackstop; i++ {
			logs = append(logs, trade(uint(i), i))
		}
		logs = append(logs, fee(10, FeeTrading), fee(11, FeeMakerRebate))
		res, err := r.Decode(context.Background(), nil, logs)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Decoded) != len(logs) {
			t.Fatalf("decoded %d", len(res.Decoded))
		}
		if v := counter(t, m, "perps_indexer_enum_unknown_total"); v != "0" {
			t.Fatalf("known values counted: %s", v)
		}
		res, err = r.Decode(context.Background(), nil, []Log{trade(20, 7), fee(21, 1), fee(22, 2), fee(23, 3), fee(24, 255)})
		if err != nil {
			t.Fatal(err)
		}
		gotTypes := []uint8{
			res.Decoded[0].Rows[0].(TradeSettledRow).TradeType,
			res.Decoded[1].Rows[0].(FeeCollectedRow).FeeType,
			res.Decoded[2].Rows[0].(FeeCollectedRow).FeeType,
			res.Decoded[3].Rows[0].(FeeCollectedRow).FeeType,
			res.Decoded[4].Rows[0].(FeeCollectedRow).FeeType,
		}
		if !reflect.DeepEqual(gotTypes, []uint8{7, 1, 2, 3, 255}) {
			t.Fatalf("stored values %v, want the emitted values", gotTypes)
		}
		if v := counter(t, m, "perps_indexer_enum_unknown_total"); v != "5" {
			t.Fatalf("enum unknown %s, want 5", v)
		}
	})

	t.Run("not_emitted_events", func(t *testing.T) {
		r, m := newTestRegistry(t)
		signatures := []string{
			"MarginLocked(uint256,address,uint256)",
			"MarginReleased(uint256,address,uint256)",
			"MarginTransferred(uint256,uint256,uint256)",
			"MarginModeChanged(uint8,uint8)",
			"LedgerEntryRecorded(uint256,uint8,uint256,uint256)",
			"VaultBalanceChanged(address,uint8,uint256,uint256,bool)",
			"MarkPriceChanged(uint256,uint256,uint256,uint256,uint256)",
			"MarketCreated(uint256,string,string,uint256)",
			"MarketUpdated(uint256)",
			"FeesUpdated(uint256,uint256,uint256,uint256)",
			"DelegateAdded(address,bool,bool,bool,uint256)",
			"BorrowingFeeAccrued(uint256,address,uint256,uint256)",
		}
		var logs []Log
		for i, sig := range signatures {
			l := packLog(t, "VaultDeficit", uint(i), usdl, big.NewInt(1))
			l.Topics = []common.Hash{crypto.Keccak256Hash([]byte(sig))}
			logs = append(logs, l)
		}
		res, err := r.Decode(context.Background(), nil, logs)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Decoded) != 0 || len(res.Raw) != len(signatures) {
			t.Fatalf("decoded %d raw %d", len(res.Decoded), len(res.Raw))
		}
		for i, raw := range res.Raw {
			if raw.FailureClass != FailureUnknownTopic || raw.Decoded || raw.Topics[0] != logs[i].Topics[0] {
				t.Fatalf("%s: %+v", signatures[i], raw)
			}
		}
		if v := counter(t, m, "perps_indexer_unknown_logs_total"); v != "12" {
			t.Fatalf("unknown logs %s, want 12", v)
		}
	})
}
