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

const ERC20Noop = "erc20noop"

// ERC20NoopScenario implements the TxGenerator interface for ERC20Noop contract operations
type ERC20NoopScenario struct {
	*ContractScenarioBase[bindings.ERC20Noop]
	contract *bindings.ERC20Noop
}

// Name returns the name of the scenario.
func (s *ERC20NoopScenario) Name() string {
	return ERC20Noop
}

// NewERC20NoopScenario creates a new ERC20Noop scenario
func NewERC20NoopScenario(cfg config.Scenario) TxGenerator {
	scenario := &ERC20NoopScenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.ERC20Noop](scenario, cfg)
	return scenario
}

// DeployContract implements ContractDeployer interface - deploys ERC20Noop with specific constructor args
func (s *ERC20NoopScenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.DeployERC20Noop(opts, client, "NoopToken", "NT")
	return address, tx, err
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *ERC20NoopScenario) GetBindFunc() ContractBindFunc[bindings.ERC20Noop] {
	return bindings.NewERC20Noop
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *ERC20NoopScenario) SetContract(contract *bindings.ERC20Noop) {
	s.contract = contract
}

// Attach implements TxGenerator interface - attaches to an existing contract
func (s *ERC20NoopScenario) Attach(config *config.LoadConfig, address common.Address) error {
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

	s.contract, err = bindings.NewERC20Noop(address, client)
	return err
}

// CreateContractTransaction implements ContractDeployer interface - creates ERC20Noop transaction
func (s *ERC20NoopScenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	auth.GasLimit = 22460
	return s.contract.Transfer(auth, scenario.Receiver, bigOne)
}
