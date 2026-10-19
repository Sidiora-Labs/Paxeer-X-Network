package scenarios

import (
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/paxeer-network/pax-load/config"
	"github.com/paxeer-network/pax-load/generator/bindings"
	"github.com/paxeer-network/pax-load/types"
)

const Disperse = "disperse"

// DisperseScenario implements the TxGenerator interface for Disperse contract operations
type DisperseScenario struct {
	*ContractScenarioBase[bindings.Disperse]
	contract *bindings.Disperse
	pool     types.AccountPool
}

// NewDisperseScenario creates a new Disperse scenario
func NewDisperseScenario(cfg config.Scenario) TxGenerator {
	scenario := &DisperseScenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.Disperse](scenario, cfg)
	scenario.pool = types.NewAccountPool(&types.AccountConfig{
		NewAccountRate: 1.0,
	})
	return scenario
}

// Name returns the name of the scenario.
func (s *DisperseScenario) Name() string {
	return Disperse
}

// DeployContract implements ContractDeployer interface - deploys Disperse with specific constructor args
func (s *DisperseScenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	address, tx, _, err := bindings.DeployDisperse(opts, client, bigOne, bigOne)
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *DisperseScenario) GetBindFunc() ContractBindFunc[bindings.Disperse] {
	return bindings.NewDisperse
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *DisperseScenario) SetContract(contract *bindings.Disperse) {
	s.contract = contract
}

// Attach implements TxGenerator interface - attaches to an existing contract
func (s *DisperseScenario) Attach(config *config.LoadConfig, address common.Address) error {
	// Call base Attach to set deployed flag and config
	if err := s.ContractScenarioBase.Attach(config, address); err != nil {
		return err
	}

	var client *ethclient.Client
	var err error
	if !config.MockDeploy {
		client, err = ethclient.Dial(config.Endpoints[0])
		if err != nil {
			return err
		}
	}

	s.contract, err = bindings.NewDisperse(address, client)
	return err
}

// CreateContractTransaction implements ContractDeployer interface - creates Disperse transaction
func (s *DisperseScenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	// create new accounts so that it auto-creates the accounts.
	targets := make([]common.Address, 0, 100)
	for range 100 {
		targets = append(targets, s.pool.NextAccount().Address)
	}
	return s.contract.DisperseEtherFixed(auth, targets)
}
