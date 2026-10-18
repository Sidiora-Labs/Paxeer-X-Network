package protocol

import (
	"fmt"
	"sort"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/bindings"
)

// EventDef describes one decoded launchpad event: its topic0 (the keccak256 of
// the canonical signature, i.e. log.Topics[0]), the event name and signature,
// the ABI event used to unpack log data, and the set of contracts that declare
// it (the same signature can appear on several contracts, e.g. OwnershipTransferred).
type EventDef struct {
	Topic0    common.Hash
	Name      string
	Sig       string
	Event     abi.Event
	Contracts []string
}

// abiSources are the contract metadata whose events seed the registry — the ten
// contracts that chain.CreateContracts binds for the indexer and processors.
var abiSources = []struct {
	Name string
	Meta *bind.MetaData
}{
	{"EventEmitter", bindings.EventEmitterMetaData},
	{"PoolRegistry", bindings.PoolRegistryMetaData},
	{"SidioraFactory", bindings.SidioraFactoryMetaData},
	{"Router", bindings.RouterMetaData},
	{"Quoter", bindings.QuoterMetaData},
	{"ProtocolConfig", bindings.ProtocolConfigMetaData},
	{"FeeAccumulator", bindings.FeeAccumulatorMetaData},
	{"SidioraNFT", bindings.SidioraNFTMetaData},
	{"FeesRouter", bindings.FeesRouterMetaData},
	{"PoolBeacon", bindings.PoolBeaconMetaData},
}

// eventRegistry maps topic0 -> event definition, built once at package init from
// the generated ABIs (so it always tracks the bindings; no hand-maintained list).
var eventRegistry = buildEventRegistry()

func buildEventRegistry() map[common.Hash]*EventDef {
	reg := make(map[common.Hash]*EventDef)
	for _, src := range abiSources {
		parsed, err := src.Meta.GetAbi()
		if err != nil {
			panic(fmt.Sprintf("protocol: parse %s ABI: %v", src.Name, err))
		}
		for name := range parsed.Events {
			ev := parsed.Events[name]
			if d, ok := reg[ev.ID]; ok {
				d.Contracts = appendUnique(d.Contracts, src.Name)
				continue
			}
			reg[ev.ID] = &EventDef{
				Topic0:    ev.ID,
				Name:      ev.Name,
				Sig:       ev.Sig,
				Event:     ev,
				Contracts: []string{src.Name},
			}
		}
	}
	return reg
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// LookupEvent returns the event definition for a log's first topic (topic0), and
// whether it is a known launchpad event.
func LookupEvent(topic0 common.Hash) (*EventDef, bool) {
	d, ok := eventRegistry[topic0]
	return d, ok
}

// Events returns every registered event definition, sorted by name then topic0
// for deterministic output.
func Events() []*EventDef {
	out := make([]*EventDef, 0, len(eventRegistry))
	for _, d := range eventRegistry {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Topic0.Hex() < out[j].Topic0.Hex()
	})
	return out
}

// Topic0Count returns the number of distinct event topics in the registry.
func Topic0Count() int { return len(eventRegistry) }
