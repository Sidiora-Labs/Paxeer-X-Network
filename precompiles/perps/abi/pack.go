package abi

import (
	"fmt"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

var parsedABI = sync.OnceValues(ABI)

// PackEvent returns the topics and data of the named event of ABI(): topic0
// is the event id, each indexed argument becomes a topic in declaration order
// and the non-indexed arguments are ABI-encoded as the data. Args are every
// argument of the event, indexed ones included, in signature order.
func PackEvent(name string, args ...interface{}) ([]common.Hash, []byte, error) {
	parsed, err := parsedABI()
	if err != nil {
		return nil, nil, err
	}
	event, ok := parsed.Events[name]
	if !ok {
		return nil, nil, fmt.Errorf("perps: unknown event %s", name)
	}
	if len(args) != len(event.Inputs) {
		return nil, nil, fmt.Errorf("perps: event %s takes %d arguments, got %d", name, len(event.Inputs), len(args))
	}
	topics := []common.Hash{event.ID}
	data := make([]interface{}, 0, len(args))
	for i, input := range event.Inputs {
		if !input.Indexed {
			data = append(data, args[i])
			continue
		}
		topic, err := indexedTopic(input, args[i])
		if err != nil {
			return nil, nil, fmt.Errorf("perps: event %s argument %s: %w", name, input.Name, err)
		}
		topics = append(topics, topic)
	}
	packed, err := event.Inputs.NonIndexed().Pack(data...)
	if err != nil {
		return nil, nil, fmt.Errorf("perps: event %s: %w", name, err)
	}
	return topics, packed, nil
}

// indexedTopic encodes one indexed argument of a static type as its topic:
// an address is left-padded and any other static value is its 32-byte ABI
// word, which Pack checks against the declared type.
func indexedTopic(input abi.Argument, value interface{}) (common.Hash, error) {
	if input.Type.T == abi.AddressTy {
		address, ok := value.(common.Address)
		if !ok {
			return common.Hash{}, fmt.Errorf("want common.Address, got %T", value)
		}
		return addressTopic(address), nil
	}
	word, err := abi.Arguments{{Type: input.Type}}.Pack(value)
	if err != nil {
		return common.Hash{}, err
	}
	if len(word) != common.HashLength {
		return common.Hash{}, fmt.Errorf("indexed type %s is not a static word", input.Type)
	}
	return common.BytesToHash(word), nil
}

func addressTopic(value common.Address) common.Hash { return common.BytesToHash(value.Bytes()) }
