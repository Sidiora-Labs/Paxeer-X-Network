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

const ERC20 = "erc20"

// ERC20Scenario implements the TxGenerator interface for ERC20 contract operations
type ERC20Scenario struct {
	*ContractScenarioBase[bindings.ERC20]
	contract *bindings.ERC20
}

// Name returns the name of the scenario.
func (s *ERC20Scenario) Name() string {
	return ERC20
}

// NewERC20Scenario creates a new ERC20 scenario
func NewERC20Scenario(cfg config.Scenario) TxGenerator {
	scenario := &ERC20Scenario{}
	scenario.ContractScenarioBase = NewContractScenarioBase[bindings.ERC20](scenario, cfg)
	return scenario
}

// GetBindFunc implements ContractDeployer interface - returns the binding function
func (s *ERC20Scenario) GetBindFunc() ContractBindFunc[bindings.ERC20] {
	return bindings.NewERC20
}

// SetContract implements ContractDeployer interface - stores the contract instance
func (s *ERC20Scenario) SetContract(contract *bindings.ERC20) {
	s.contract = contract
}

// DeployContract implements ContractDeployer interface - deploys ERC20 with specific constructor args
func (s *ERC20Scenario) DeployContract(opts *bind.TransactOpts, client *ethclient.Client) (common.Address, *ethtypes.Transaction, error) {
	// TODO: Update with actual constructor arguments
	address, tx, _, err := bindings.DeployERC20(opts, client, "LoadToken", "LT")
	return address, tx, err
}

// Attach implements TxGenerator interface - attaches to an existing contract
func (s *ERC20Scenario) Attach(config *config.LoadConfig, address common.Address) error {
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

	s.contract, err = bindings.NewERC20(address, client)
	return err
}

// CreateContractTransaction implements ContractDeployer interface - creates ERC20 transaction
func (s *ERC20Scenario) CreateContractTransaction(auth *bind.TransactOpts, scenario *types.TxScenario) (*ethtypes.Transaction, error) {
	auth.GasLimit = 72156
	return s.contract.Transfer(auth, scenario.Receiver, bigOne)
}
