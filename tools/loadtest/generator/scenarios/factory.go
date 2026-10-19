package scenarios

import (
	"strings"

	"github.com/paxeer-network/pax-load/config"
)

// ScenarioFactory is a function type that creates a new scenario instance
type ScenarioFactory func(s config.Scenario) TxGenerator

// scenarioFactories maps scenario names to their factory functions
var scenarioFactories = map[string]ScenarioFactory{
	// Manual entries for non-contract scenarios
	EVMTransfer:     NewEVMTransferScenario,
	EVMTransferNoop: NewEVMTransferNoopScenario,

	// Auto-generated entries will be added below this line by make generate
	// DO NOT EDIT BELOW THIS LINE - AUTO-GENERATED CONTENT
	Disperse:      NewDisperseScenario,
	ERC20Conflict: NewERC20ConflictScenario,
	ERC20Noop:     NewERC20NoopScenario,
	ERC20:         NewERC20Scenario,
	ERC721:        NewERC721Scenario,

	// DO NOT EDIT ABOVE THIS LINE - AUTO-GENERATED CONTENT
}

// CreateScenario creates a new scenario instance by name
func CreateScenario(s config.Scenario) TxGenerator {
	factory, exists := scenarioFactories[strings.ToLower(s.Name)]
	if !exists {
		panic("Unknown scenario: " + s.Name)
	}
	return factory(s)
}
