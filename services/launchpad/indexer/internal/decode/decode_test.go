package decode

import (
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol"
)

// ── fixtures / helpers ─────────────────────────────────────────────

func eventByName(t *testing.T, name string) *protocol.EventDef {
	t.Helper()
	for _, d := range protocol.Events() {
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("event %q not in protocol registry", name)
	return nil
}

func word(bi *big.Int) []byte { return common.LeftPadBytes(bi.Bytes(), 32) }

// indexedTopic renders one indexed argument value into its 32-byte log topic.
func indexedTopic(t *testing.T, arg abi.Argument, v any) common.Hash {
	t.Helper()
	switch arg.Type.T {
	case abi.AddressTy:
		return common.BytesToHash(common.LeftPadBytes(v.(common.Address).Bytes(), 32))
	case abi.FixedBytesTy:
		b := v.([32]byte)
		return common.BytesToHash(b[:])
	case abi.UintTy:
		return common.BytesToHash(word(v.(*big.Int)))
	default:
		t.Fatalf("unsupported indexed type %v", arg.Type)
		return common.Hash{}
	}
}

// buildLog assembles a log for ev from ordered indexed values + packed
// non-indexed data.
func buildLog(t *testing.T, ev *protocol.EventDef, indexedVals []any, data []byte, blockNumber uint64, txHash string, idx uint) ethtypes.Log {
	t.Helper()
	topics := []common.Hash{ev.Topic0}
	indexed := indexedSubset(ev.Event.Inputs)
	if len(indexed) != len(indexedVals) {
		t.Fatalf("%s: %d indexed args, got %d values", ev.Name, len(indexed), len(indexedVals))
	}
	for i, arg := range indexed {
		topics = append(topics, indexedTopic(t, arg, indexedVals[i]))
	}
	return ethtypes.Log{
		Topics:      topics,
		Data:        data,
		BlockNumber: blockNumber,
		TxHash:      common.HexToHash(txHash),
		Index:       idx,
	}
}

func packNonIndexed(t *testing.T, ev *protocol.EventDef, vals ...any) []byte {
	t.Helper()
	b, err := ev.Event.Inputs.NonIndexed().Pack(vals...)
	if err != nil {
		t.Fatalf("pack %s non-indexed: %v", ev.Name, err)
	}
	return b
}

func bytes32(b byte) [32]byte {
	var out [32]byte
	for i := range out {
		out[i] = b
	}
	return out
}

const (
	tokenAddr   = "0x00000000000000000000000000000000000000aa"
	creatorAddr = "0x00000000000000000000000000000000000000bb"
	poolAddr    = "0x00000000000000000000000000000000000000cc"
	opticalAddr = "0x00000000000000000000000000000000000000dd"
)

// ── tests ──────────────────────────────────────────────────────────

func TestDecodeEmptyTopics(t *testing.T) {
	t.Parallel()
	ev, err := NewDecoder().Decode(ethtypes.Log{})
	if ev != nil || err != nil {
		t.Fatalf("empty topics: got (%v, %v), want (nil, nil)", ev, err)
	}
}

func TestDecodeUnknownTopic(t *testing.T) {
	t.Parallel()
	log := ethtypes.Log{Topics: []common.Hash{common.HexToHash("0xdead")}}
	ev, err := NewDecoder().Decode(log)
	if ev != nil || err != nil {
		t.Fatalf("unknown topic: got (%v, %v), want (nil, nil)", ev, err)
	}
}

func TestDecodeMarketCreated(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "MarketCreated")
	poolID := bytes32(0xab)
	data := packNonIndexed(t, ev,
		common.HexToAddress(poolAddr),
		common.HexToAddress(opticalAddr),
		big.NewInt(1700000000),
		big.NewInt(999),
	)
	log := buildLog(t, ev,
		[]any{poolID, common.HexToAddress(tokenAddr), common.HexToAddress(creatorAddr)},
		data, 999, "0xFEED", 3)

	got, err := NewDecoder().Decode(log)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.EventName != "MarketCreated" || got.BlockNumber != 999 || got.LogIndex != 3 {
		t.Fatalf("envelope wrong: %+v", got)
	}
	if got.TxHash != strings.ToLower(common.HexToHash("0xFEED").Hex()) {
		t.Errorf("txHash = %q", got.TxHash)
	}
	wantPoolID := "0x" + strings.Repeat("ab", 32)
	if got.Args["poolId"] != wantPoolID {
		t.Errorf("poolId = %v, want %s", got.Args["poolId"], wantPoolID)
	}
	if got.Args["token"] != tokenAddr || got.Args["creator"] != creatorAddr {
		t.Errorf("indexed addrs = %v / %v", got.Args["token"], got.Args["creator"])
	}
	if got.Args["pool"] != poolAddr || got.Args["optical"] != opticalAddr {
		t.Errorf("non-indexed addrs = %v / %v", got.Args["pool"], got.Args["optical"])
	}
	if got.Args["timestamp"] != "1700000000" || got.Args["blockNumber"] != "999" {
		t.Errorf("uint args = %v / %v", got.Args["timestamp"], got.Args["blockNumber"])
	}
}

func TestDecodeSwapHappy(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "Swap")
	poolID := bytes32(0x11)
	data := packNonIndexed(t, ev,
		true, big.NewInt(100), big.NewInt(95), big.NewInt(5), big.NewInt(2), big.NewInt(1700000000), big.NewInt(42))
	log := buildLog(t, ev,
		[]any{poolID, common.HexToAddress(tokenAddr)}, data, 42, "0xabc", 0)

	got, err := NewDecoder().Decode(log)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Args["isBuy"] != true {
		t.Errorf("isBuy = %v, want true", got.Args["isBuy"])
	}
	if got.Args["amountIn"] != "100" || got.Args["amountOut"] != "95" || got.Args["fee"] != "5" {
		t.Errorf("amounts wrong: %+v", got.Args)
	}
	if got.Args["sender"] != tokenAddr {
		t.Errorf("indexed sender = %v", got.Args["sender"])
	}
}

// TestDecodeSwapEvmosBoolQuirk feeds the non-strict full-word bool encoding the
// Paxeer EVM actually emits (a uint256 word, value != 0/1) which go-ethereum's
// strict decoder rejects — the exact reason the loose substitution exists.
func TestDecodeSwapEvmosBoolQuirk(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "Swap")
	poolID := bytes32(0x22)

	build := func(boolWord *big.Int) ethtypes.Log {
		var data []byte
		data = append(data, word(boolWord)...)               // isBuy (full word)
		data = append(data, word(big.NewInt(7))...)          // amountIn
		data = append(data, word(big.NewInt(8))...)          // amountOut
		data = append(data, word(big.NewInt(1))...)          // fee
		data = append(data, word(big.NewInt(3))...)          // price
		data = append(data, word(big.NewInt(1700000000))...) // timestamp
		data = append(data, word(big.NewInt(9))...)          // blockNumber
		return buildLog(t, ev, []any{poolID, common.HexToAddress(tokenAddr)}, data, 9, "0xdef", 1)
	}

	// A strict go-ethereum unpack of this word would fail; ours must accept it.
	got, err := NewDecoder().Decode(build(big.NewInt(2)))
	if err != nil {
		t.Fatalf("decode non-strict bool=2: %v", err)
	}
	if got.Args["isBuy"] != true {
		t.Errorf("isBuy(word=2) = %v, want true", got.Args["isBuy"])
	}

	got0, err := NewDecoder().Decode(build(big.NewInt(0)))
	if err != nil {
		t.Fatalf("decode bool=0: %v", err)
	}
	if got0.Args["isBuy"] != false {
		t.Errorf("isBuy(word=0) = %v, want false", got0.Args["isBuy"])
	}
}

func TestDecodePoolStateUpdated(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "PoolStateUpdated")
	data := packNonIndexed(t, ev,
		big.NewInt(1000), big.NewInt(900), big.NewInt(800), big.NewInt(7), big.NewInt(1700000000), big.NewInt(11))
	log := buildLog(t, ev, []any{bytes32(0x33)}, data, 11, "0x111", 2)
	got, err := NewDecoder().Decode(log)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Args["virtualReserve"] != "1000" || got.Args["tokenReserve"] != "800" || got.Args["price"] != "7" {
		t.Errorf("reserves wrong: %+v", got.Args)
	}
}

func TestDecodeOpticalExecuted(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "OpticalExecuted")
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	data := packNonIndexed(t, ev, "myHook", payload, big.NewInt(1700000000), big.NewInt(5))
	log := buildLog(t, ev, []any{bytes32(0x44), common.HexToAddress(opticalAddr)}, data, 5, "0x222", 0)
	got, err := NewDecoder().Decode(log)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Args["hookName"] != "myHook" {
		t.Errorf("hookName = %v", got.Args["hookName"])
	}
	if got.Args["data"] != "0xdeadbeef" {
		t.Errorf("data = %v, want 0xdeadbeef", got.Args["data"])
	}
	if got.Args["optical"] != opticalAddr {
		t.Errorf("optical = %v", got.Args["optical"])
	}
}

func TestDecodeFeeDistributedUint8(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "FeeDistributed")
	data := packNonIndexed(t, ev,
		big.NewInt(77), uint8(3), big.NewInt(500), common.HexToAddress(creatorAddr), big.NewInt(1700000000), big.NewInt(8))
	log := buildLog(t, ev, []any{bytes32(0x55)}, data, 8, "0x333", 4)
	got, err := NewDecoder().Decode(log)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Args["nftId"] != "77" || got.Args["amount"] != "500" {
		t.Errorf("uint256 args wrong: %+v", got.Args)
	}
	// strategy is uint8 (not substituted) so humanize returns it as-is.
	if got.Args["strategy"] != uint8(3) {
		t.Errorf("strategy = %#v, want uint8(3)", got.Args["strategy"])
	}
	if got.Args["recipient"] != creatorAddr {
		t.Errorf("recipient = %v", got.Args["recipient"])
	}
}

func TestDecodeConfigUpdated(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "ConfigUpdated")
	data := packNonIndexed(t, ev, big.NewInt(10), big.NewInt(20), big.NewInt(1700000000), big.NewInt(6))
	log := buildLog(t, ev, []any{bytes32(0x66)}, data, 6, "0x444", 0)
	got, err := NewDecoder().Decode(log)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Args["oldValue"] != "10" || got.Args["newValue"] != "20" {
		t.Errorf("config values wrong: %+v", got.Args)
	}
	if got.Args["key"] != "0x"+strings.Repeat("66", 32) {
		t.Errorf("key = %v", got.Args["key"])
	}
}

func TestDecodeTopicsUnderflow(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "Swap")                          // needs 2 indexed topics
	log := ethtypes.Log{Topics: []common.Hash{ev.Topic0}} // only topic0
	_, err := NewDecoder().Decode(log)
	if err == nil || !strings.Contains(err.Error(), "topics underflow") {
		t.Fatalf("want topics underflow error, got %v", err)
	}
}

func TestDecodeDataUnpackError(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "MarketCreated")
	// Valid topics but truncated (1-byte) data -> non-indexed unpack must fail.
	log := buildLog(t, ev,
		[]any{bytes32(0x77), common.HexToAddress(tokenAddr), common.HexToAddress(creatorAddr)},
		[]byte{0x01}, 1, "0x555", 0)
	_, err := NewDecoder().Decode(log)
	if err == nil || !strings.Contains(err.Error(), "MarketCreated") {
		t.Fatalf("want data unpack error, got %v", err)
	}
}

func TestHumanize(t *testing.T) {
	t.Parallel()
	uint256 := mustType("uint256")
	addrT := mustType("address")
	bytes4 := mustType("bytes4")
	dynBytes := mustType("bytes")

	if humanize(nil, uint256) != nil {
		t.Error("nil")
	}
	if humanize(big.NewInt(42), uint256) != "42" {
		t.Error("big.Int -> decimal string")
	}
	if humanize(common.HexToAddress("0xAaBb"), addrT) != strings.ToLower(common.HexToAddress("0xAaBb").Hex()) {
		t.Error("address -> lowercased hex")
	}
	if humanize(common.HexToHash("0x01"), mustType("bytes32")) != common.HexToHash("0x01").Hex() {
		t.Error("hash -> hex")
	}
	if humanize(true, mustType("bool")) != true {
		t.Error("bool passthrough")
	}
	if humanize("hi", mustType("string")) != "hi" {
		t.Error("string passthrough")
	}
	if humanize([]byte{0x0a, 0x0b}, dynBytes) != "0x0a0b" {
		t.Error("[]byte -> 0xhex")
	}
	if humanize(bytes32(0x01), mustType("bytes32")) != "0x"+strings.Repeat("01", 32) {
		t.Error("[32]byte -> 0xhex")
	}
	// FixedBytes fallback (non-[32]byte fixed-size array hits the type-tag branch).
	if got := humanize([4]byte{0xde, 0xad, 0xbe, 0xef}, bytes4); got != "0xdeadbeef" {
		t.Errorf("bytes4 fallback = %v", got)
	}
	// Unhandled type falls through unchanged.
	if humanize(int64(5), uint256) != int64(5) {
		t.Error("passthrough fallthrough")
	}
}

func TestDecodeConcurrent(t *testing.T) {
	t.Parallel()
	ev := eventByName(t, "Swap")
	data := packNonIndexed(t, ev,
		true, big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4), big.NewInt(5), big.NewInt(6))
	log := buildLog(t, ev, []any{bytes32(0x88), common.HexToAddress(tokenAddr)}, data, 1, "0x666", 0)

	d := NewDecoder() // shared decoder exercises the looseCache RWMutex under -race
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := d.Decode(log)
			if err != nil || got == nil || got.Args["isBuy"] != true {
				t.Errorf("concurrent decode: %v / %v", got, err)
			}
		}()
	}
	wg.Wait()
}

func FuzzDecode(f *testing.F) {
	ev := eventByName(&testing.T{}, "Swap")
	f.Add(ev.Topic0.Bytes(), []byte{0x01, 0x02})
	f.Add([]byte("not a topic"), []byte{})
	d := NewDecoder()
	f.Fuzz(func(t *testing.T, topic, data []byte) {
		log := ethtypes.Log{
			Topics: []common.Hash{common.BytesToHash(topic)},
			Data:   data,
		}
		// Must never panic regardless of input; error is acceptable.
		_, _ = d.Decode(log)
	})
}
