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

// ERC20ConflictMetaData contains all meta data concerning the ERC20Conflict contract.
var ERC20ConflictMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"string\",\"name\":\"_name\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"_symbol\",\"type\":\"string\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Approval\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"from\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"to\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"value\",\"type\":\"uint256\"}],\"name\":\"Transfer\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_BALANCE\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"}],\"name\":\"allowance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"spender\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"approve\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"balanceOf\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"counter\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"decimals\",\"outputs\":[{\"internalType\":\"uint8\",\"name\":\"\",\"type\":\"uint8\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"name\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"symbol\",\"outputs\":[{\"internalType\":\"string\",\"name\":\"\",\"type\":\"string\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"totalSupply\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"transfer\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"transferFrom\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x60806040523480156200001157600080fd5b50604051620009e2380380620009e283398101604081905262000034916200012c565b600062000042838262000225565b50600162000051828262000225565b50506002805460ff1916601217905550620002f1565b634e487b7160e01b600052604160045260246000fd5b600082601f8301126200008f57600080fd5b81516001600160401b0380821115620000ac57620000ac62000067565b604051601f8301601f19908116603f01168101908282118183101715620000d757620000d762000067565b81604052838152602092508683858801011115620000f457600080fd5b600091505b83821015620001185785820183015181830184015290820190620000f9565b600093810190920192909252949350505050565b600080604083850312156200014057600080fd5b82516001600160401b03808211156200015857600080fd5b62000166868387016200007d565b935060208501519150808211156200017d57600080fd5b506200018c858286016200007d565b9150509250929050565b600181811c90821680620001ab57607f821691505b602082108103620001cc57634e487b7160e01b600052602260045260246000fd5b50919050565b601f8211156200022057600081815260208120601f850160051c81016020861015620001fb5750805b601f850160051c820191505b818110156200021c5782815560010162000207565b5050505b505050565b81516001600160401b0381111562000241576200024162000067565b620002598162000252845462000196565b84620001d2565b602080601f831160018114620002915760008415620002785750858301515b600019600386901b1c1916600185901b1785556200021c565b600085815260208120601f198616915b82811015620002c257888601518255948401946001909101908401620002a1565b5085821015620002e15787850151600019600388901b60f8161c191681555b5050505050600190811b01905550565b6106e180620003016000396000f3fe608060405234801561001057600080fd5b50600436106100a95760003560e01c806361bc221a1161007157806361bc221a1461013857806370a082311461014157806370f357351461015457806395d89b4114610164578063a9059cbb1461016c578063dd62ed3e1461017f57600080fd5b806306fdde03146100ae578063095ea7b3146100cc57806318160ddd146100ef57806323b872dd14610106578063313ce56714610119575b600080fd5b6100b66101b8565b6040516100c391906104fe565b60405180910390f35b6100df6100da366004610568565b610246565b60405190151581526020016100c3565b6100f860035481565b6040519081526020016100c3565b6100df610114366004610592565b6102b3565b6002546101269060ff1681565b60405160ff90911681526020016100c3565b6100f860045481565b6100f861014f3660046105ce565b6103cc565b6100f8683635c9adc5dea0000081565b6100b6610402565b6100df61017a366004610568565b61040f565b6100f861018d3660046105e9565b6001600160a01b03918216600090815260066020908152604080832093909416825291909152205490565b600080546101c59061061c565b80601f01602080910402602001604051908101604052809291908181526020018280546101f19061061c565b801561023e5780601f106102135761010080835404028352916020019161023e565b820191906000526020600020905b81548152906001019060200180831161022157829003601f168201915b505050505081565b3360008181526006602090815260408083206001600160a01b038716808552925280832085905551919290917f8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925906102a19086815260200190565b60405180910390a35060015b92915050565b6001600160a01b038316600090815260056020526040812054821115610300576102e6683635c9adc5dea000008361066c565b6001600160a01b0385166000908152600560205260409020555b600480549060006103108361067f565b90915550506001600160a01b0384166000908152600560205260408120805484929061033d908490610698565b90915550506001600160a01b03831660009081526005602052604090205461036690839061066c565b6001600160a01b0380851660008181526005602052604090819020939093559151908616907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef906103ba9086815260200190565b60405180910390a35060019392505050565b6001600160a01b038116600090815260056020526040812054806103f957683635c9adc5dea000006103fb565b805b9392505050565b600180546101c59061061c565b3360009081526005602052604081205482111561044a57610439683635c9adc5dea000008361066c565b336000908152600560205260409020555b3360009081526005602052604081208054849290610469908490610698565b90915550506001600160a01b03831660009081526005602052604090205461049290839061066c565b6001600160a01b03841660009081526005602052604081209190915560048054916104bc8361067f565b90915550506040518281526001600160a01b0384169033907fddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef906020016102a1565b600060208083528351808285015260005b8181101561052b5785810183015185820160400152820161050f565b506000604082860101526040601f19601f8301168501019250505092915050565b80356001600160a01b038116811461056357600080fd5b919050565b6000806040838503121561057b57600080fd5b6105848361054c565b946020939093013593505050565b6000806000606084860312156105a757600080fd5b6105b08461054c565b92506105be6020850161054c565b9150604084013590509250925092565b6000602082840312156105e057600080fd5b6103fb8261054c565b600080604083850312156105fc57600080fd5b6106058361054c565b91506106136020840161054c565b90509250929050565b600181811c9082168061063057607f821691505b60208210810361065057634e487b7160e01b600052602260045260246000fd5b50919050565b634e487b7160e01b600052601160045260246000fd5b808201808211156102ad576102ad610656565b60006001820161069157610691610656565b5060010190565b818103818111156102ad576102ad61065656fea2646970667358221220d1331fb89fb53f0ac7387c589b9b06322e67be63aff729921866871363f0ce8464736f6c63430008130033",
}

// ERC20ConflictABI is the input ABI used to generate the binding from.
// Deprecated: Use ERC20ConflictMetaData.ABI instead.
var ERC20ConflictABI = ERC20ConflictMetaData.ABI

// ERC20ConflictBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use ERC20ConflictMetaData.Bin instead.
var ERC20ConflictBin = ERC20ConflictMetaData.Bin

// DeployERC20Conflict deploys a new Ethereum contract, binding an instance of ERC20Conflict to it.
func DeployERC20Conflict(auth *bind.TransactOpts, backend bind.ContractBackend, _name string, _symbol string) (common.Address, *types.Transaction, *ERC20Conflict, error) {
	parsed, err := ERC20ConflictMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(ERC20ConflictBin), backend, _name, _symbol)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &ERC20Conflict{ERC20ConflictCaller: ERC20ConflictCaller{contract: contract}, ERC20ConflictTransactor: ERC20ConflictTransactor{contract: contract}, ERC20ConflictFilterer: ERC20ConflictFilterer{contract: contract}}, nil
}

// ERC20Conflict is an auto generated Go binding around an Ethereum contract.
type ERC20Conflict struct {
	ERC20ConflictCaller     // Read-only binding to the contract
	ERC20ConflictTransactor // Write-only binding to the contract
	ERC20ConflictFilterer   // Log filterer for contract events
}

// ERC20ConflictCaller is an auto generated read-only Go binding around an Ethereum contract.
type ERC20ConflictCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20ConflictTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ERC20ConflictTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20ConflictFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ERC20ConflictFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ERC20ConflictSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ERC20ConflictSession struct {
	Contract     *ERC20Conflict    // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ERC20ConflictCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ERC20ConflictCallerSession struct {
	Contract *ERC20ConflictCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts        // Call options to use throughout this session
}

// ERC20ConflictTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ERC20ConflictTransactorSession struct {
	Contract     *ERC20ConflictTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts        // Transaction auth options to use throughout this session
}

// ERC20ConflictRaw is an auto generated low-level Go binding around an Ethereum contract.
type ERC20ConflictRaw struct {
	Contract *ERC20Conflict // Generic contract binding to access the raw methods on
}

// ERC20ConflictCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ERC20ConflictCallerRaw struct {
	Contract *ERC20ConflictCaller // Generic read-only contract binding to access the raw methods on
}

// ERC20ConflictTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ERC20ConflictTransactorRaw struct {
	Contract *ERC20ConflictTransactor // Generic write-only contract binding to access the raw methods on
}

// NewERC20Conflict creates a new instance of ERC20Conflict, bound to a specific deployed contract.
func NewERC20Conflict(address common.Address, backend bind.ContractBackend) (*ERC20Conflict, error) {
	contract, err := bindERC20Conflict(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ERC20Conflict{ERC20ConflictCaller: ERC20ConflictCaller{contract: contract}, ERC20ConflictTransactor: ERC20ConflictTransactor{contract: contract}, ERC20ConflictFilterer: ERC20ConflictFilterer{contract: contract}}, nil
}

// NewERC20ConflictCaller creates a new read-only instance of ERC20Conflict, bound to a specific deployed contract.
func NewERC20ConflictCaller(address common.Address, caller bind.ContractCaller) (*ERC20ConflictCaller, error) {
	contract, err := bindERC20Conflict(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20ConflictCaller{contract: contract}, nil
}

// NewERC20ConflictTransactor creates a new write-only instance of ERC20Conflict, bound to a specific deployed contract.
func NewERC20ConflictTransactor(address common.Address, transactor bind.ContractTransactor) (*ERC20ConflictTransactor, error) {
	contract, err := bindERC20Conflict(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ERC20ConflictTransactor{contract: contract}, nil
}

// NewERC20ConflictFilterer creates a new log filterer instance of ERC20Conflict, bound to a specific deployed contract.
func NewERC20ConflictFilterer(address common.Address, filterer bind.ContractFilterer) (*ERC20ConflictFilterer, error) {
	contract, err := bindERC20Conflict(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ERC20ConflictFilterer{contract: contract}, nil
}

// bindERC20Conflict binds a generic wrapper to an already deployed contract.
func bindERC20Conflict(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ERC20ConflictMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Conflict *ERC20ConflictRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Conflict.Contract.ERC20ConflictCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Conflict *ERC20ConflictRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.ERC20ConflictTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Conflict *ERC20ConflictRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.ERC20ConflictTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ERC20Conflict *ERC20ConflictCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ERC20Conflict.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ERC20Conflict *ERC20ConflictTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ERC20Conflict *ERC20ConflictTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTBALANCE is a free data retrieval call binding the contract method 0x70f35735.
//
// Solidity: function DEFAULT_BALANCE() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCaller) DEFAULTBALANCE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "DEFAULT_BALANCE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// DEFAULTBALANCE is a free data retrieval call binding the contract method 0x70f35735.
//
// Solidity: function DEFAULT_BALANCE() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictSession) DEFAULTBALANCE() (*big.Int, error) {
	return _ERC20Conflict.Contract.DEFAULTBALANCE(&_ERC20Conflict.CallOpts)
}

// DEFAULTBALANCE is a free data retrieval call binding the contract method 0x70f35735.
//
// Solidity: function DEFAULT_BALANCE() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCallerSession) DEFAULTBALANCE() (*big.Int, error) {
	return _ERC20Conflict.Contract.DEFAULTBALANCE(&_ERC20Conflict.CallOpts)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCaller) Allowance(opts *bind.CallOpts, owner common.Address, spender common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "allowance", owner, spender)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20Conflict *ERC20ConflictSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ERC20Conflict.Contract.Allowance(&_ERC20Conflict.CallOpts, owner, spender)
}

// Allowance is a free data retrieval call binding the contract method 0xdd62ed3e.
//
// Solidity: function allowance(address owner, address spender) view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCallerSession) Allowance(owner common.Address, spender common.Address) (*big.Int, error) {
	return _ERC20Conflict.Contract.Allowance(&_ERC20Conflict.CallOpts, owner, spender)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCaller) BalanceOf(opts *bind.CallOpts, account common.Address) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "balanceOf", account)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20Conflict *ERC20ConflictSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ERC20Conflict.Contract.BalanceOf(&_ERC20Conflict.CallOpts, account)
}

// BalanceOf is a free data retrieval call binding the contract method 0x70a08231.
//
// Solidity: function balanceOf(address account) view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCallerSession) BalanceOf(account common.Address) (*big.Int, error) {
	return _ERC20Conflict.Contract.BalanceOf(&_ERC20Conflict.CallOpts, account)
}

// Counter is a free data retrieval call binding the contract method 0x61bc221a.
//
// Solidity: function counter() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCaller) Counter(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "counter")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// Counter is a free data retrieval call binding the contract method 0x61bc221a.
//
// Solidity: function counter() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictSession) Counter() (*big.Int, error) {
	return _ERC20Conflict.Contract.Counter(&_ERC20Conflict.CallOpts)
}

// Counter is a free data retrieval call binding the contract method 0x61bc221a.
//
// Solidity: function counter() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCallerSession) Counter() (*big.Int, error) {
	return _ERC20Conflict.Contract.Counter(&_ERC20Conflict.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20Conflict *ERC20ConflictCaller) Decimals(opts *bind.CallOpts) (uint8, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "decimals")

	if err != nil {
		return *new(uint8), err
	}

	out0 := *abi.ConvertType(out[0], new(uint8)).(*uint8)

	return out0, err

}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20Conflict *ERC20ConflictSession) Decimals() (uint8, error) {
	return _ERC20Conflict.Contract.Decimals(&_ERC20Conflict.CallOpts)
}

// Decimals is a free data retrieval call binding the contract method 0x313ce567.
//
// Solidity: function decimals() view returns(uint8)
func (_ERC20Conflict *ERC20ConflictCallerSession) Decimals() (uint8, error) {
	return _ERC20Conflict.Contract.Decimals(&_ERC20Conflict.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20Conflict *ERC20ConflictCaller) Name(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "name")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20Conflict *ERC20ConflictSession) Name() (string, error) {
	return _ERC20Conflict.Contract.Name(&_ERC20Conflict.CallOpts)
}

// Name is a free data retrieval call binding the contract method 0x06fdde03.
//
// Solidity: function name() view returns(string)
func (_ERC20Conflict *ERC20ConflictCallerSession) Name() (string, error) {
	return _ERC20Conflict.Contract.Name(&_ERC20Conflict.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20Conflict *ERC20ConflictCaller) Symbol(opts *bind.CallOpts) (string, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "symbol")

	if err != nil {
		return *new(string), err
	}

	out0 := *abi.ConvertType(out[0], new(string)).(*string)

	return out0, err

}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20Conflict *ERC20ConflictSession) Symbol() (string, error) {
	return _ERC20Conflict.Contract.Symbol(&_ERC20Conflict.CallOpts)
}

// Symbol is a free data retrieval call binding the contract method 0x95d89b41.
//
// Solidity: function symbol() view returns(string)
func (_ERC20Conflict *ERC20ConflictCallerSession) Symbol() (string, error) {
	return _ERC20Conflict.Contract.Symbol(&_ERC20Conflict.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCaller) TotalSupply(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ERC20Conflict.contract.Call(opts, &out, "totalSupply")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictSession) TotalSupply() (*big.Int, error) {
	return _ERC20Conflict.Contract.TotalSupply(&_ERC20Conflict.CallOpts)
}

// TotalSupply is a free data retrieval call binding the contract method 0x18160ddd.
//
// Solidity: function totalSupply() view returns(uint256)
func (_ERC20Conflict *ERC20ConflictCallerSession) TotalSupply() (*big.Int, error) {
	return _ERC20Conflict.Contract.TotalSupply(&_ERC20Conflict.CallOpts)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictTransactor) Approve(opts *bind.TransactOpts, spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.contract.Transact(opts, "approve", spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.Approve(&_ERC20Conflict.TransactOpts, spender, amount)
}

// Approve is a paid mutator transaction binding the contract method 0x095ea7b3.
//
// Solidity: function approve(address spender, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictTransactorSession) Approve(spender common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.Approve(&_ERC20Conflict.TransactOpts, spender, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address recipient, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictTransactor) Transfer(opts *bind.TransactOpts, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.contract.Transact(opts, "transfer", recipient, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address recipient, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictSession) Transfer(recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.Transfer(&_ERC20Conflict.TransactOpts, recipient, amount)
}

// Transfer is a paid mutator transaction binding the contract method 0xa9059cbb.
//
// Solidity: function transfer(address recipient, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictTransactorSession) Transfer(recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.Transfer(&_ERC20Conflict.TransactOpts, recipient, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address sender, address recipient, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictTransactor) TransferFrom(opts *bind.TransactOpts, sender common.Address, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.contract.Transact(opts, "transferFrom", sender, recipient, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address sender, address recipient, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictSession) TransferFrom(sender common.Address, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.TransferFrom(&_ERC20Conflict.TransactOpts, sender, recipient, amount)
}

// TransferFrom is a paid mutator transaction binding the contract method 0x23b872dd.
//
// Solidity: function transferFrom(address sender, address recipient, uint256 amount) returns(bool)
func (_ERC20Conflict *ERC20ConflictTransactorSession) TransferFrom(sender common.Address, recipient common.Address, amount *big.Int) (*types.Transaction, error) {
	return _ERC20Conflict.Contract.TransferFrom(&_ERC20Conflict.TransactOpts, sender, recipient, amount)
}

// ERC20ConflictApprovalIterator is returned from FilterApproval and is used to iterate over the raw logs and unpacked data for Approval events raised by the ERC20Conflict contract.
type ERC20ConflictApprovalIterator struct {
	Event *ERC20ConflictApproval // Event containing the contract specifics and raw log

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
func (it *ERC20ConflictApprovalIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20ConflictApproval)
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
		it.Event = new(ERC20ConflictApproval)
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
func (it *ERC20ConflictApprovalIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20ConflictApprovalIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20ConflictApproval represents a Approval event raised by the ERC20Conflict contract.
type ERC20ConflictApproval struct {
	Owner   common.Address
	Spender common.Address
	Value   *big.Int
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterApproval is a free log retrieval operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20Conflict *ERC20ConflictFilterer) FilterApproval(opts *bind.FilterOpts, owner []common.Address, spender []common.Address) (*ERC20ConflictApprovalIterator, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ERC20Conflict.contract.FilterLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return &ERC20ConflictApprovalIterator{contract: _ERC20Conflict.contract, event: "Approval", logs: logs, sub: sub}, nil
}

// WatchApproval is a free log subscription operation binding the contract event 0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925.
//
// Solidity: event Approval(address indexed owner, address indexed spender, uint256 value)
func (_ERC20Conflict *ERC20ConflictFilterer) WatchApproval(opts *bind.WatchOpts, sink chan<- *ERC20ConflictApproval, owner []common.Address, spender []common.Address) (event.Subscription, error) {

	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}
	var spenderRule []interface{}
	for _, spenderItem := range spender {
		spenderRule = append(spenderRule, spenderItem)
	}

	logs, sub, err := _ERC20Conflict.contract.WatchLogs(opts, "Approval", ownerRule, spenderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20ConflictApproval)
				if err := _ERC20Conflict.contract.UnpackLog(event, "Approval", log); err != nil {
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
func (_ERC20Conflict *ERC20ConflictFilterer) ParseApproval(log types.Log) (*ERC20ConflictApproval, error) {
	event := new(ERC20ConflictApproval)
	if err := _ERC20Conflict.contract.UnpackLog(event, "Approval", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ERC20ConflictTransferIterator is returned from FilterTransfer and is used to iterate over the raw logs and unpacked data for Transfer events raised by the ERC20Conflict contract.
type ERC20ConflictTransferIterator struct {
	Event *ERC20ConflictTransfer // Event containing the contract specifics and raw log

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
func (it *ERC20ConflictTransferIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ERC20ConflictTransfer)
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
		it.Event = new(ERC20ConflictTransfer)
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
func (it *ERC20ConflictTransferIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ERC20ConflictTransferIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ERC20ConflictTransfer represents a Transfer event raised by the ERC20Conflict contract.
type ERC20ConflictTransfer struct {
	From  common.Address
	To    common.Address
	Value *big.Int
	Raw   types.Log // Blockchain specific contextual infos
}

// FilterTransfer is a free log retrieval operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20Conflict *ERC20ConflictFilterer) FilterTransfer(opts *bind.FilterOpts, from []common.Address, to []common.Address) (*ERC20ConflictTransferIterator, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ERC20Conflict.contract.FilterLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return &ERC20ConflictTransferIterator{contract: _ERC20Conflict.contract, event: "Transfer", logs: logs, sub: sub}, nil
}

// WatchTransfer is a free log subscription operation binding the contract event 0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef.
//
// Solidity: event Transfer(address indexed from, address indexed to, uint256 value)
func (_ERC20Conflict *ERC20ConflictFilterer) WatchTransfer(opts *bind.WatchOpts, sink chan<- *ERC20ConflictTransfer, from []common.Address, to []common.Address) (event.Subscription, error) {

	var fromRule []interface{}
	for _, fromItem := range from {
		fromRule = append(fromRule, fromItem)
	}
	var toRule []interface{}
	for _, toItem := range to {
		toRule = append(toRule, toItem)
	}

	logs, sub, err := _ERC20Conflict.contract.WatchLogs(opts, "Transfer", fromRule, toRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ERC20ConflictTransfer)
				if err := _ERC20Conflict.contract.UnpackLog(event, "Transfer", log); err != nil {
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
func (_ERC20Conflict *ERC20ConflictFilterer) ParseTransfer(log types.Log) (*ERC20ConflictTransfer, error) {
	event := new(ERC20ConflictTransfer)
	if err := _ERC20Conflict.contract.UnpackLog(event, "Transfer", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
