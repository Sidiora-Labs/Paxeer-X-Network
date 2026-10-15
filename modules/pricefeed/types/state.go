package types

import (
	"encoding/binary"

	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

const StoreKey = ModuleName

var (
	ParamsKey         = []byte{0x01}
	FeedKeyPrefix     = []byte{0x02}
	FeedAccountPrefix = []byte{0x03}
	ObservationPrefix = []byte{0x04}
	AggregatePrefix   = []byte{0x05}
	LastSubmitPrefix  = []byte{0x06}
)

func u64(value uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, value)
	return out
}

func join(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

// FeedKeyKey holds the FeedKey of a validator operator.
func FeedKeyKey(operator sdk.ValAddress) []byte { return join(FeedKeyPrefix, operator) }

// FeedAccountKey holds the operator a feed account belongs to.
func FeedAccountKey(feedAccount sdk.AccAddress) []byte { return join(FeedAccountPrefix, feedAccount) }

// ObservationKey holds the latest accepted observation of a validator for a market.
func ObservationKey(marketID uint64, operator sdk.ValAddress) []byte {
	return join(ObservationPrefix, u64(marketID), operator)
}

// AggregateKey holds the aggregate of a market.
func AggregateKey(marketID uint64) []byte { return join(AggregatePrefix, u64(marketID)) }

// LastSubmitKey holds the last submission height of a validator, 8 bytes big-endian.
func LastSubmitKey(operator sdk.ValAddress) []byte { return join(LastSubmitPrefix, operator) }

// FeedKey is the registered feed key of a validator: the hex compressed
// secp256k1 public key and the bech32 account it derives.
type FeedKey struct {
	PubKey      string `json:"pub_key"`
	FeedAccount string `json:"feed_account"`
}

// ObservationRecord is the latest accepted observation of a validator and
// market and the height that accepted it.
type ObservationRecord struct {
	Sequence          uint64  `json:"sequence"`
	IndexPrice        sdk.Int `json:"index_price"`
	MarkPrice         sdk.Int `json:"mark_price"`
	SourceTimestampMs uint64  `json:"source_timestamp_ms"`
	Height            int64   `json:"height"`
}

// Aggregate is the price of a market at 18 decimals. Basis is the signed
// basis average the mark price carries to the next block.
type Aggregate struct {
	IndexPrice  sdk.Int `json:"index_price"`
	MarkPrice   sdk.Int `json:"mark_price"`
	Basis       sdk.Int `json:"basis"`
	TimestampMs uint64  `json:"timestamp_ms"`
	Height      int64   `json:"height"`
	Sources     uint32  `json:"sources"`
}

type GenesisFeedKey struct {
	Operator sdk.ValAddress `json:"operator"`
	FeedKey  FeedKey        `json:"feed_key"`
}

type GenesisObservation struct {
	MarketID    uint64            `json:"market_id"`
	Operator    sdk.ValAddress    `json:"operator"`
	Observation ObservationRecord `json:"observation"`
}

type GenesisAggregate struct {
	MarketID  uint64    `json:"market_id"`
	Aggregate Aggregate `json:"aggregate"`
}

// GenesisState carries params, feed keys, observations and aggregates; last
// submission heights are not exported.
type GenesisState struct {
	Params       Params               `json:"params"`
	FeedKeys     []GenesisFeedKey     `json:"feed_keys"`
	Observations []GenesisObservation `json:"observations"`
	Aggregates   []GenesisAggregate   `json:"aggregates"`
}

func DefaultGenesis() *GenesisState {
	return &GenesisState{Params: DefaultParams(DefaultAuthority())}
}
