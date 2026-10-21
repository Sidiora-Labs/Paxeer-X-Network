package decode

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	pricefeedabi "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pricefeed/abi"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/bigint"
)

// PricefeedAddress is the pricefeed precompile, the emitter of the feed key
// events and of the index_prices rows of the pricefeed block events.
var PricefeedAddress = common.HexToAddress("0x0000000000000000000000000000000000001020")

// The pricefeed block events of finalize_block_events decoded into
// perps.index_prices (modules/pricefeed/types/events.go).
const (
	EventAggregate        = "pricefeed_aggregate"
	EventAggregateSkipped = "pricefeed_aggregate_skipped"
)

var (
	aggregateAttributes = []string{"market_id", "index_price", "mark_price", "basis", "timestamp_ms", "sources", "required", "height"}
	skippedAttributes   = []string{"market_id", "sources", "required"}
)

func registerPricefeed(r *Registry) error {
	a, err := abi.JSON(strings.NewReader(pricefeedabi.JSON))
	if err != nil {
		return fmt.Errorf("decode: pricefeed ABI: %w", err)
	}
	return r.register(PricefeedAddress, r.opts.PricefeedModule, a, pricefeedEvents)
}

// pricefeedEvents is the decoding table of contracts/data-services.md
// section 6 for the logs of 0x1020.
var pricefeedEvents = []eventDecoder{
	{"FeedKeyRegistered", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "registered"
		f.Account = r.addr("validator")
		prev := r.addr("previousFeedAccount")
		var previous *common.Address
		if prev != (common.Address{}) {
			previous = &prev
		}
		return []Row{FeedKeyRegisteredRow{f, f.Account, r.addr("feedAccount"), r.bytes("pubKey"), previous}}
	}},
	{"FeedKeyRevoked", func(_ *batch, f Fact, r *reader) []Row {
		f.Kind = "revoked"
		f.Account = r.addr("validator")
		return []Row{FeedKeyRevokedRow{f, f.Account, r.addr("feedAccount")}}
	}},
}

// BlockInput is one block for DecodeBlock: its identity, its logs and its
// finalize_block_events.
type BlockInput struct {
	Number uint64
	Hash   common.Hash
	Time   time.Time
	Logs   []Log
	Events []abci.Event
}

// DecodeBlock decodes the pricefeed block events of one block into
// index_prices rows, then its logs as Decode does. A block event row has
// receipt_kind block_event, tx_index -1, log_index its position in
// finalize_block_events, tx_hash the block hash and emitter 0x1020; the other
// block events are not decoded. The logs are decoded in (tx index, log index)
// order, so the second-pass share pointer logs of the follower take their
// place in the block.
func (r *Registry) DecodeBlock(ctx context.Context, q Querier, in BlockInput) (*Result, error) {
	res := &Result{}
	for i, ev := range in.Events {
		var names []string
		switch ev.Type {
		case EventAggregate:
			names = aggregateAttributes
		case EventAggregateSkipped:
			names = skippedAttributes
		default:
			continue
		}
		attrs, err := blockEventAttributes(ev, names)
		if err != nil {
			return nil, fmt.Errorf("decode block %d event %d: %w", in.Number, i, err)
		}
		f := Fact{
			Block:       in.Number,
			BlockHash:   in.Hash,
			TxIndex:     -1,
			LogIndex:    int64(i),
			TxHash:      in.Hash,
			ReceiptKind: ReceiptBlockEvent,
			BlockTime:   in.Time,
			Emitter:     PricefeedAddress,
			Account:     r.opts.PricefeedModule,
		}
		var row Row
		if ev.Type == EventAggregate {
			f.Kind = "aggregate"
			row = IndexPriceRow{f, attrs["market_id"], attrs["index_price"], attrs["mark_price"], attrs["basis"], attrs["timestamp_ms"], attrs["sources"], attrs["required"]}
		} else {
			f.Kind = "skipped"
			row = IndexPriceSkippedRow{f, attrs["market_id"], attrs["sources"], attrs["required"]}
		}
		args := make(map[string]any, len(attrs))
		for k, v := range attrs {
			args[k] = v
		}
		res.Decoded = append(res.Decoded, Decoded{
			Event: Event{ev.Type, ev.Type, args, in.Number, strings.ToLower(in.Hash.Hex()), uint(i)},
			Rows:  []Row{row},
		})
	}
	logs := slices.Clone(in.Logs)
	slices.SortStableFunc(logs, func(x, y Log) int {
		return cmp.Or(cmp.Compare(x.TxIndex, y.TxIndex), cmp.Compare(x.Index, y.Index))
	})
	b := r.newBatch(ctx, q)
	if err := r.decodeLogs(b, res, logs); err != nil {
		return nil, err
	}
	r.unknownLogs.Add(float64(b.unknownLogs))
	r.enumUnknown.Add(float64(b.enumUnknown))
	res.Lookups = b.lookups
	return res, nil
}

// blockEventAttributes reads each named attribute exactly once as a decimal;
// basis is signed, height a uint64, the rest unsigned. Other attributes, such
// as the mode the SDK appends, are ignored.
func blockEventAttributes(ev abci.Event, names []string) (map[string]string, error) {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	out := make(map[string]string, len(names))
	for _, a := range ev.Attributes {
		k, v := string(a.Key), string(a.Value)
		if !want[k] {
			continue
		}
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%w: %s repeats %s", ErrInvalidBlockEvent, ev.Type, k)
		}
		var err error
		switch k {
		case "basis":
			err = bigint.ValidateRawInt256(v)
		case "height":
			_, err = strconv.ParseUint(v, 10, 64)
		default:
			err = bigint.ValidateRaw(v)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %s %s: %v", ErrInvalidBlockEvent, ev.Type, k, err)
		}
		out[k] = v
	}
	for _, n := range names {
		if _, ok := out[n]; !ok {
			return nil, fmt.Errorf("%w: %s lacks %s", ErrInvalidBlockEvent, ev.Type, n)
		}
	}
	return out, nil
}

// FeedKeyRegisteredRow is a perps.feed_keys row of kind registered;
// PreviousFeedAccount is nil when no key was registered before.
type FeedKeyRegisteredRow struct {
	Fact
	Validator           common.Address
	FeedAccount         common.Address
	PubKey              []byte
	PreviousFeedAccount *common.Address
}

// FeedKeyRevokedRow is a perps.feed_keys row of kind revoked.
type FeedKeyRevokedRow struct {
	Fact
	Validator   common.Address
	FeedAccount common.Address
}

// IndexPriceRow is a perps.index_prices row of kind aggregate; Basis is
// signed.
type IndexPriceRow struct {
	Fact
	MarketID    string
	IndexPrice  string
	MarkPrice   string
	Basis       string
	TimestampMs string
	Sources     string
	Required    string
}

// IndexPriceSkippedRow is a perps.index_prices row of kind skipped.
type IndexPriceSkippedRow struct {
	Fact
	MarketID string
	Sources  string
	Required string
}

func (FeedKeyRegisteredRow) Table() string { return "feed_keys" }
func (FeedKeyRevokedRow) Table() string    { return "feed_keys" }
func (IndexPriceRow) Table() string        { return "index_prices" }
func (IndexPriceSkippedRow) Table() string { return "index_prices" }
