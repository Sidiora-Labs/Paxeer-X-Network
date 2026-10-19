// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package bindings

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// ERC20NoopMetaData contains all meta data concerning the ERC20Noop contract.
var ERC20NoopMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"string\",\"name\":\"_name\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"_symbol\",\"type\":\"string\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_BALANCE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b506040516106c93803806106c983398101604081905261002f91610118565b600061003b8382610204565b5060016100488282610204565b50506002805460ff19166012179055506102c3565b634e487b7160e01b600052604160045260246000fd5b600082601f83011261008457600080fd5b81516001600160401b038082111561009e5761009e61005d565b604051601f8301601f19908116603f011681019082821181831017156100c6576100c661005d565b816040528381526020925086838588010111156100e257600080fd5b600091505b8382101561010457858201830151818301840152908201906100e7565b600093810190920192909252949350505050565b6000806040838503121561012b57600080fd5b82516001600160401b038082111561014257600080fd5b61014e86838701610073565b9350602085015191508082111561016457600080fd5b5061017185828601610073565b9150509250929050565b600181811c9082168061018f57607f821691505b6020821081036101af57634e487b7160e01b600052602260045260246000fd5b50919050565b601f8211156101ff57600081815260208120601f850160051c810160208610156101dc5750805b601f850160051c820191505b818110156101fb578281556001016101e8565b5050505b505050565b81516001600160401b0381111561021d5761021d61005d565b6102318161022b845461017b565b846101b5565b602080601f831160018114610266576000841561024e5750858301515b600019600386901b1c1916600185901b1785556101fb565b600085815260208120601f198616915b8281101561029557888601518255948401946001909101908401610276565b50858210156102b35787850151600019600388901b60f8161c191681555b5050505050600190811b01905550565b6103f7806102d26000396000f3fe608060405234801561001057600080fd5b506004361061009e5760003560e01c806370a082311161006657806370a082311461013457806370f357351461014757806395d89b4114610157578063a9059cbb146100c1578063dd62ed3e1461015f57600080fd5b806306fdde03146100a3578063095ea7b3146100c157806318160ddd146100e757806323b872dd146100fe578063313ce56714610115575b600080fd5b6100ab610198565b6040516100b89190610269565b60405180910390f35b6100d76100cf3660046102d3565b600192915050565b60405190151581526020016100b8565b6100f060035481565b6040519081526020016100b8565b6100d761010c3660046102fd565b60019392505050565b6002546101229060ff1681565b60405160ff90911681526020016100b8565b6100f0610142366004610339565b610226565b6100f0683635c9adc5dea0000081565b6100ab61025c565b6100f061016d366004610354565b6001600160a01b03918216600090815260056020908152604080832093909416825291909152205490565b600080546101a590610387565b80601f01602080910402602001604051908101604052809291908181526020018280546101d190610387565b801561021e5780601f106101f35761010080835404028352916020019161021e565b820191906000526020600020905b81548152906001019060200180831161020157829003601f168201915b505050505081565b6001600160a01b0381166000908152600460205260408120548061025357683635c9adc5dea00000610255565b805b9392505050565b600180546101a590610387565b600060208083528351808285015260005b818110156102965785810183015185820160400152820161027a565b506000604082860101526040601f19601f8301168501019250505092915050565b80356001600160a01b03811681146102ce57600080fd5b919050565b600080604083850312156102e657600080fd5b6102ef836102b7565b946020939093013593505050565b60008060006060848603121561031257600080fd5b61031b846102b7565b9250610329602085016102b7565b9150604084013590509250925092565b60006020828403121561034b57600080fd5b610255826102b7565b6000806040838503121561036757600080fd5b610370836102b7565b915061037e602084016102b7565b90509250929050565b600181811c9082168061039b57607f821691505b6020821081036103bb57634e487b7160e01b600052602260045260246000fd5b5091905056fea2646970667358221220248fdb8cf878ae10ee5f0361548cc0d7d239e9f6660c26ad00076c1e7c42bf6864736f6c63430008130033",
}

// ERC20NoopABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20NoopMetaData.ABI instead.
var ERC20NoopABI = ERC20NoopMetaData.ABI

// ERC20NoopBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ERC20NoopMetaData.Bin instead.
var ERC20NoopBin = ERC20NoopMetaData.Bin

// DeployERC20Noop deploys a new Ethereum contract, binding an instance of ERC20Noop to it.
func DeployERC20Noop(auth *bind.TransactOpts, backend bind.ContractBackend, _name string, _symbol string) (common.Address, *types.Transaction, *ERC20Noop, error) {
	parsed, err := ERC20NoopMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ERC20NoopBin), backend, _name, _symbol)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ERC20Noop{ERC20NoopCaller: ERC20NoopCaller{contract: contract}, ERC20NoopTransactor: ERC20NoopTransactor{contract: contract}, ERC20NoopFilterer: ERC20NoopFilterer{contract: contract}}, nil
}

// ERC20Noop is an auto generated Go binding around an Ethereum contract.
type ERC20Noop struct {
	ERC20NoopCaller     // Read-only binding to the contract
	ERC20NoopTransactor // Write-only binding to the contract
	ERC20NoopFilterer   // Log filterer for contract events
}

// ERC20NoopCaller is an auto generated read-only Go binding around an Ethereum contract.
type ERC20NoopCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20NoopTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ERC20NoopTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20NoopFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ERC20NoopFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20NoopSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ERC20NoopSession struct {
	Contract     *ERC20Noop        // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ERC20NoopCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ERC20NoopCallerSession struct {
	Contract *ERC20NoopCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts    // Call options to use throughout this session
}

// ERC20NoopTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ERC20NoopTransactorSession struct {
	Contract     *ERC20NoopTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts    // Transaction auth options to use throughout this session
}

// ERC20NoopRaw is an auto generated low-level Go binding around an Ethereum contract.
type ERC20NoopRaw struct {
	Contract *ERC20Noop // Generic contract binding to access the raw methods on
}

// ERC20NoopCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ERC20NoopCallerRaw struct {
	Contract *ERC20NoopCaller // Generic read-only contract binding to access the raw methods on
}

// ERC20NoopTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ERC20NoopTransactorRaw struct {
	Contract *ERC20NoopTransactor // Generic write-only contract binding to access the raw methods on
}

// NewERC20Noop creates a new instance of ERC20Noop, bound to a specific deployed contract.
func NewERC20Noop(address common.Address, backend bind.ContractBackend) (*ERC20Noop, error) {
	contract, err := bindERC20Noop(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ERC20Noop{ERC20NoopCaller: ERC20NoopCaller{contract: contract}, ERC20NoopTransactor: ERC20NoopTransactor{contract: contract}, ERC20NoopFilterer: ERC20NoopFilterer{contract: contract}}, nil
}

// NewERC20NoopCaller creates a new read-only instance of ERC20Noop, bound to a specific deployed contract.
func NewERC20NoopCaller(address common.Address, caller bind.ContractCaller) (*ERC20NoopCaller, error) {
	contract, err := bindERC20Noop(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20NoopCaller{contract: contract}, nil
}

// NewERC20NoopTransactor creates a new write-only instance of ERC20Noop, bound to a specific deployed contract.
func NewERC20NoopTransactor(address common.Address, transactor bind.ContractTransactor) (*ERC20NoopTransactor, error) {
	contract, err := bindERC20Noop(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20NoopTransactor{contract: contract}, nil
}

// NewERC20NoopFilterer creates a new log filterer instance of ERC20Noop, bound to a specific deployed contract.
func NewERC20NoopFilterer(address common.Address, filterer bind.ContractFilterer) (*ERC20NoopFilterer, error) {
	contract, err := bindERC20Noop(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ERC20NoopFilterer{contract: contract}, nil
}

// bindERC20Noop binds a generic wrapper to an already deployed contract.
func bindERC20Noop(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ERC20NoopMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Noop *ERC20NoopRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Noop.Contract.ERC20NoopCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Noop *ERC20NoopRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Noop.Contract.ERC20NoopTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Noop *ERC20NoopRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Noop.Contract.ERC20NoopTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Noop *ERC20NoopCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Noop.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Noop *ERC20NoopTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Noop.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Noop *ERC20NoopTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Noop.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTBALANCE is a free data retrieval call binding the contract method 0x70f35735.
//
// Solidity: function DEFAULT_BALANCE() view returns(uint256)
func (_ERC20Noop *ERC20NoopCaller) DEFAULTBALANCE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "DEFAULT_BALANCE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// DEFAULTBALANCE is a free data retrieval call binding the contract method 0x70f35735.
//
// Solidity: function DEFAULT_BALANCE() view returns(uint256)
func (_ERC20Noop *ERC20NoopSession) DEFAULTBALANCE() (*big.Int, error) {
	return _ERC20Noop.Contract.DEFAULTBALANCE(&_ERC20Noop.CallOpts)
}

// DEFAULTBALANCE is a free data retrieval call binding the contract method 0x70f35735.
//
// Solidity: function DEFAULT_BALANCE() view returns(uint256)
func (_ERC20Noop *ERC20NoopCallerSession) DEFAULTBALANCE() (*big.Int, error) {
	return _ERC20Noop.Contract.DEFAULTBALANCE(&_ERC20Noop.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20Noop *ERC20NoopCaller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20Noop *ERC20NoopSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ERC20Noop.Contract.Allowance(&_ERC20Noop.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20Noop *ERC20NoopCallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ERC20Noop.Contract.Allowance(&_ERC20Noop.CallOpts, owner, spender)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20Noop *ERC20NoopCaller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20Noop *ERC20NoopSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ERC20Noop.Contract.BalanceOf(&_ERC20Noop.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20Noop *ERC20NoopCallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ERC20Noop.Contract.BalanceOf(&_ERC20Noop.CallOpts, account)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20Noop *ERC20NoopCaller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20Noop *ERC20NoopSession) Decimals() (uint8, error) {
	return _ERC20Noop.Contract.Decimals(&_ERC20Noop.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20Noop *ERC20NoopCallerSession) Decimals() (uint8, error) {
	return _ERC20Noop.Contract.Decimals(&_ERC20Noop.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20Noop *ERC20NoopCaller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20Noop *ERC20NoopSession) Name() (string, error) {
	return _ERC20Noop.Contract.Name(&_ERC20Noop.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20Noop *ERC20NoopCallerSession) Name() (string, error) {
	return _ERC20Noop.Contract.Name(&_ERC20Noop.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20Noop *ERC20NoopCaller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20Noop *ERC20NoopSession) Symbol() (string, error) {
	return _ERC20Noop.Contract.Symbol(&_ERC20Noop.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20Noop *ERC20NoopCallerSession) Symbol() (string, error) {
	return _ERC20Noop.Contract.Symbol(&_ERC20Noop.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20Noop *ERC20NoopCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Noop.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20Noop *ERC20NoopSession) TotalSupply() (*big.Int, error) {
	return _ERC20Noop.Contract.TotalSupply(&_ERC20Noop.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20Noop *ERC20NoopCallerSession) TotalSupply() (*big.Int, error) {
	return _ERC20Noop.Contract.TotalSupply(&_ERC20Noop.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopTransactor) Approve(opts *bind.TransactOpts, spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.contract.Transact(opts, "approve", spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.Contract.Approve(&_ERC20Noop.TransactOpts, spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopTransactorSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.Contract.Approve(&_ERC20Noop.TransactOpts, spender, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address recipient, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopTransactor) Transfer(opts *bind.TransactOpts, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.contract.Transact(opts, "transfer", recipient, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address recipient, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopSession) Transfer(recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.Contract.Transfer(&_ERC20Noop.TransactOpts, recipient, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address recipient, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopTransactorSession) Transfer(recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.Contract.Transfer(&_ERC20Noop.TransactOpts, recipient, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address sender, address recipient, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopTransactor) TransferFrom(opts *bind.TransactOpts, sender common.Address, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.contract.Transact(opts, "transferFrom", sender, recipient, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address sender, address recipient, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopSession) TransferFrom(sender common.Address, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.Contract.TransferFrom(&_ERC20Noop.TransactOpts, sender, recipient, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address sender, address recipient, uint256 amount) returns(bool)
func (_ERC20Noop *ERC20NoopTransactorSession) TransferFrom(sender common.Address, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Noop.Contract.TransferFrom(&_ERC20Noop.TransactOpts, sender, recipient, amount)
}

// ERC20NoopApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the ERC20Noop contract.
type ERC20NoopApprovalIterator struct {
	Event *ERC20NoopApproval // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ERC20NoopApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NoopApproval)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ERC20NoopApproval)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ERC20NoopApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NoopApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NoopApproval represents a Approval event raised by the ERC20Noop contract.
type ERC20NoopApproval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20Noop *ERC20NoopFilterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*ERC20NoopApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ERC20Noop.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NoopApprovalIterator{contract: _ERC20Noop.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20Noop *ERC20NoopFilterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *ERC20NoopApproval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ERC20Noop.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NoopApproval)
				if err := _ERC20Noop.contract.UnpackLog(event, "Approval", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseApproval is a log parse operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20Noop *ERC20NoopFilterer) ParseApproval(log types.Log) (*ERC20NoopApproval, error) {
	event := new(ERC20NoopApproval)
	if err := _ERC20Noop.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20NoopTransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the ERC20Noop contract.
type ERC20NoopTransferIterator struct {
	Event *ERC20NoopTransfer // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *ERC20NoopTransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20NoopTransfer)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(ERC20NoopTransfer)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *ERC20NoopTransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20NoopTransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20NoopTransfer represents a Transfer event raised by the ERC20Noop contract.
type ERC20NoopTransfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20Noop *ERC20NoopFilterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*ERC20NoopTransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ERC20Noop.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &ERC20NoopTransferIterator{contract: _ERC20Noop.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20Noop *ERC20NoopFilterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *ERC20NoopTransfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ERC20Noop.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20NoopTransfer)
				if err := _ERC20Noop.contract.UnpackLog(event, "Transfer", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTransfer is a log parse operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20Noop *ERC20NoopFilterer) ParseTransfer(log types.Log) (*ERC20NoopTransfer, error) {
	event := new(ERC20NoopTransfer)
	if err := _ERC20Noop.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
