// Package decode turns raw EVM logs into launchpad events with JSON-friendly
// argument maps, parity with the TS event-decoder.ts (viem parseEventLogs,
// strict:false). It resolves topic0 against the protocol/ ABI registry (built
// from the abigen bindings) so it always tracks the deployed contracts.
//
// Evmos/Ethermint quirk (carried over from the prior Go indexer): the Paxeer
// EVM encodes `bool` event args as a full uint256 word with non-strict
// semantics (any non-zero == true), which go-ethereum's strict decoder rejects.
// We substitute every non-indexed bool with uint256 before unpacking and coerce
// back via Sign() != 0 — without this, Swap (isBuy bool) fails to decode.
package decode

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol"
)

// Event is a decoded launchpad event. Args holds the decoded inputs humanized
// to JSON-friendly Go types (uint256 -> decimal string, address -> lowercased
// 0x-hex, bytes32 -> 0x-hex, bool -> bool, string -> string), matching the TS
// webhook payload arg shapes.
type Event struct {
	EventName   string
	Sig         string
	Args        map[string]any
	BlockNumber uint64
	TxHash      string
	LogIndex    uint
}

// Decoder resolves logs against the protocol registry. It is safe for
// concurrent use: the only mutable state is the per-topic0 loose-args cache,
// guarded by an RWMutex.
type Decoder struct {
	looseMu    sync.RWMutex
	looseCache map[common.Hash]abi.Arguments
}

// NewDecoder returns a ready Decoder.
func NewDecoder() *Decoder {
	return &Decoder{looseCache: make(map[common.Hash]abi.Arguments, 32)}
}

// Decode resolves a single EVM log into an Event.
//
// Returns (nil, nil) when topic0 is not a known launchpad event (parity with
// viem strict:false, which drops unmatched logs). Returns (nil, err) on a hard
// decode failure (malformed data, topic underflow, arity mismatch).
func (d *Decoder) Decode(l ethtypes.Log) (*Event, error) {
	if len(l.Topics) == 0 {
		return nil, nil
	}
	def, ok := protocol.LookupEvent(l.Topics[0])
	if !ok {
		return nil, nil
	}
	args, err := d.unpackArgs(l.Topics[0], def.Event, l.Topics, l.Data)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", def.Name, err)
	}
	return &Event{
		EventName:   def.Name,
		Sig:         def.Sig,
		Args:        args,
		BlockNumber: l.BlockNumber,
		TxHash:      strings.ToLower(l.TxHash.Hex()),
		LogIndex:    l.Index,
	}, nil
}

// unpackArgs decodes indexed (topics) + non-indexed (data) inputs into a
// humanized map keyed by input name.
func (d *Decoder) unpackArgs(topic0 common.Hash, ev abi.Event, topics []common.Hash, data []byte) (map[string]any, error) {
	out := make(map[string]any, len(ev.Inputs))

	indexed := indexedSubset(ev.Inputs)
	nonIndexed := ev.Inputs.NonIndexed()

	if len(topics) < 1+len(indexed) {
		return nil, fmt.Errorf("topics underflow: have %d, need %d for %d indexed inputs",
			len(topics), 1+len(indexed), len(indexed))
	}

	if len(indexed) > 0 {
		raw := make(map[string]any, len(indexed))
		if err := abi.ParseTopicsIntoMap(raw, indexed, topics[1:1+len(indexed)]); err != nil {
			return nil, fmt.Errorf("parse topics: %w", err)
		}
		for _, in := range indexed {
			out[in.Name] = humanize(raw[in.Name], in.Type)
		}
	}

	if len(nonIndexed) > 0 {
		loose := d.looseInputs(topic0, nonIndexed)
		vals, err := loose.Unpack(data)
		if err != nil {
			return nil, fmt.Errorf("data unpack: %w", err)
		}
		if len(vals) != len(nonIndexed) {
			return nil, fmt.Errorf("data arity mismatch: got %d, expected %d", len(vals), len(nonIndexed))
		}
		for i, in := range nonIndexed {
			v := vals[i]
			if in.Type.T == abi.BoolTy {
				if bi, ok := v.(*big.Int); ok {
					out[in.Name] = bi.Sign() != 0
					continue
				}
			}
			out[in.Name] = humanize(v, in.Type)
		}
	}

	return out, nil
}

// looseInputs returns (and lazily caches per topic0) a copy of nonIndexed where
// every bool is replaced with uint256 (the Evmos non-strict bool workaround).
func (d *Decoder) looseInputs(topic0 common.Hash, nonIndexed abi.Arguments) abi.Arguments {
	d.looseMu.RLock()
	if cached, ok := d.looseCache[topic0]; ok {
		d.looseMu.RUnlock()
		return cached
	}
	d.looseMu.RUnlock()

	out := make(abi.Arguments, len(nonIndexed))
	for i, in := range nonIndexed {
		if in.Type.T == abi.BoolTy {
			out[i] = abi.Argument{Name: in.Name, Type: abiUint256, Indexed: false}
			continue
		}
		out[i] = in
	}

	d.looseMu.Lock()
	d.looseCache[topic0] = out
	d.looseMu.Unlock()
	return out
}

// indexedSubset returns the Indexed entries of args, preserving order.
func indexedSubset(args abi.Arguments) abi.Arguments {
	out := make(abi.Arguments, 0, len(args))
	for _, a := range args {
		if a.Indexed {
			out = append(out, a)
		}
	}
	return out
}

// humanize converts an ABI-decoded Go value into a JSON-friendly shape matching
// the TS webhook arg contract.
func humanize(v any, t abi.Type) any {
	switch x := v.(type) {
	case nil:
		return nil
	case *big.Int:
		return x.String()
	case common.Address:
		return strings.ToLower(x.Hex())
	case common.Hash:
		return x.Hex()
	case bool:
		return x
	case string:
		return x
	case []byte:
		return "0x" + hex.EncodeToString(x)
	case [32]byte:
		return "0x" + hex.EncodeToString(x[:])
	}
	if t.T == abi.FixedBytesTy {
		return fmt.Sprintf("0x%x", v)
	}
	return v
}

// abiUint256 is the static type used to substitute bools in the loose cache.
var abiUint256 = mustType("uint256")

func mustType(name string) abi.Type {
	t, err := abi.NewType(name, "", nil)
	if err != nil {
		panic(fmt.Sprintf("decode: abi.NewType(%q): %v", name, err))
	}
	return t
}
