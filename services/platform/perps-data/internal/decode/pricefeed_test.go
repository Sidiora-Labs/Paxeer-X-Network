package decode_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	pricefeedabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
)

func blockEvent(typ string, kv ...string) abci.Event {
	ev := abci.Event{Type: typ}
	for i := 0; i+1 < len(kv); i += 2 {
		ev.Attributes = append(ev.Attributes, abci.EventAttribute{Key: []byte(kv[i]), Value: []byte(kv[i+1])})
	}
	return ev
}

func blockFact(index int64, kind string) decode.Fact {
	return decode.Fact{Block: xBlock, BlockHash: xBlockHash, TxIndex: -1, LogIndex: index, TxHash: xBlockHash, ReceiptKind: decode.ReceiptBlockEvent,
		BlockTime: xBlockTime, Emitter: decode.PricefeedAddress, Kind: kind, Account: pricefeedModule}
}

func decodeBlock(t *testing.T, events []abci.Event, logs ...decode.Log) (*decode.Result, error) {
	t.Helper()
	return registry(t, nil).DecodeBlock(context.Background(), nil, decode.BlockInput{Number: xBlock, Hash: xBlockHash, Time: xBlockTime, Logs: logs, Events: events})
}

func TestPricefeedDecoders(t *testing.T) {
	load := func() (abi.ABI, error) { return abi.JSON(strings.NewReader(pricefeedabi.JSON)) }
	feed := common.HexToAddress("0xfeed00000000000000000000000000000000feed")
	prev := common.HexToAddress("0x0bed00000000000000000000000000000000bed0")
	key := []byte{0x02, 0xaa, 0xbb}

	t.Run("FeedKeyRegistered", func(t *testing.T) {
		ev := event(t, load, "FeedKeyRegistered")
		res := decodeRows(t, registry(t, nil), pack(t, ev, decode.PricefeedAddress, 0, xAlice, feed, key, prev))
		wantRows(t, res, decode.FeedKeyRegisteredRow{fact(decode.PricefeedAddress, 0, "registered", xAlice), xAlice, feed, key, &prev})
		res = decodeRows(t, registry(t, nil), pack(t, ev, decode.PricefeedAddress, 0, xAlice, feed, key, common.Address{}))
		wantRows(t, res, decode.FeedKeyRegisteredRow{fact(decode.PricefeedAddress, 0, "registered", xAlice), xAlice, feed, key, nil})
	})
	t.Run("FeedKeyRevoked", func(t *testing.T) {
		res := decodeRows(t, registry(t, nil), pack(t, event(t, load, "FeedKeyRevoked"), decode.PricefeedAddress, 0, xAlice, feed))
		wantRows(t, res, decode.FeedKeyRevokedRow{fact(decode.PricefeedAddress, 0, "revoked", xAlice), xAlice, feed})
	})
	aggregate := blockEvent(decode.EventAggregate, "market_id", "11", "index_price", "1500000000000000000", "mark_price", "1490000000000000000",
		"basis", "-10000000000000000", "timestamp_ms", "1800000000000", "sources", "4", "required", "3", "height", "200", "mode", "EndBlock")
	t.Run("pricefeed_aggregate", func(t *testing.T) {
		res, err := decodeBlock(t, []abci.Event{aggregate})
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, decode.IndexPriceRow{blockFact(0, "aggregate"), "11", "1500000000000000000", "1490000000000000000", "-10000000000000000", "1800000000000", "4", "3"})
		if e := res.Decoded[0].Event; e.EventName != decode.EventAggregate || e.TxHash != strings.ToLower(xBlockHash.Hex()) || e.LogIndex != 0 || e.Args["basis"] != "-10000000000000000" {
			t.Fatalf("event %+v", e)
		}
	})
	t.Run("pricefeed_aggregate_skipped", func(t *testing.T) {
		res, err := decodeBlock(t, []abci.Event{blockEvent("transfer"), blockEvent(decode.EventAggregateSkipped, "market_id", "12", "sources", "1", "required", "3")})
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res, decode.IndexPriceSkippedRow{blockFact(1, "skipped"), "12", "1", "3"})
	})
	t.Run("invalid_block_events", func(t *testing.T) {
		for _, ev := range []abci.Event{
			blockEvent(decode.EventAggregateSkipped, "market_id", "12", "sources", "1"),
			blockEvent(decode.EventAggregateSkipped, "market_id", "12", "market_id", "13", "sources", "1", "required", "3"),
			blockEvent(decode.EventAggregateSkipped, "market_id", "-1", "sources", "1", "required", "3"),
		} {
			if _, err := decodeBlock(t, []abci.Event{ev}); !errors.Is(err, decode.ErrInvalidBlockEvent) {
				t.Fatalf("%+v: %v", ev, err)
			}
		}
	})
	t.Run("other_events", func(t *testing.T) {
		res, err := decodeBlock(t, []abci.Event{blockEvent("transfer", "amount", "1"), blockEvent("pricefeed_submission", "market_id", "11")})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Decoded) != 0 || len(res.Raw) != 0 {
			t.Fatalf("result %+v", res)
		}
	})
	t.Run("block_events_before_logs", func(t *testing.T) {
		late := pack(t, event(t, load, "FeedKeyRevoked"), decode.PricefeedAddress, 1, xAlice, feed)
		early := pack(t, event(t, load, "FeedKeyRevoked"), decode.PricefeedAddress, 0, xBob, feed)
		res, err := decodeBlock(t, []abci.Event{aggregate}, late, early)
		if err != nil {
			t.Fatal(err)
		}
		wantRows(t, res,
			decode.IndexPriceRow{blockFact(0, "aggregate"), "11", "1500000000000000000", "1490000000000000000", "-10000000000000000", "1800000000000", "4", "3"},
			decode.FeedKeyRevokedRow{fact(decode.PricefeedAddress, 0, "revoked", xBob), xBob, feed},
			decode.FeedKeyRevokedRow{fact(decode.PricefeedAddress, 1, "revoked", xAlice), xAlice, feed})
	})
}
