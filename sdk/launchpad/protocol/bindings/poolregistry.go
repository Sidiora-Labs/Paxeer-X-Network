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

// IPoolRegistryPoolMetadata is an auto generated low-level Go binding around an user-defined struct.
type IPoolRegistryPoolMetadata struct {
	Creator      common.Address
	Token        common.Address
	Optical      common.Address
	NftId        *big.Int
	CreatedAt    *big.Int
	CreatedBlock *big.Int
}

// PoolRegistryMetaData contains all meta data concerning the PoolRegistry contract.
var PoolRegistryMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"DuplicateToken\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"MissingRole\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"PoolNotFound\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughActiveProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Unauthorized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnauthorizedUpgrade\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddress\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"}],\"name\":\"PoolRegistered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"FACTORY_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"eventEmitter\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"offset\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"limit\",\"type\":\"uint256\"}],\"name\":\"getAllPools\",\"outputs\":[{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"getNftIdByPool\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"}],\"name\":\"getPoolByToken\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getPoolCount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"getPoolMetadata\",\"outputs\":[{\"components\":[{\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"createdAt\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"createdBlock\",\"type\":\"uint256\"}],\"internalType\":\"structIPoolRegistry.PoolMetadata\",\"name\":\"\",\"type\":\"tuple\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"}],\"name\":\"getPoolsByCreator\",\"outputs\":[{\"internalType\":\"address[]\",\"name\":\"\",\"type\":\"address[]\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_eventEmitter\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_admin\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"isRegisteredPool\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"name\":\"register\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"callerConfirmation\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"}]",
}

// PoolRegistryABI is the input ABI used to generate the binding from.
// Deprecated: Use PoolRegistryMetaData.ABI instead.
var PoolRegistryABI = PoolRegistryMetaData.ABI

// PoolRegistry is an auto generated Go binding around an Ethereum contract.
type PoolRegistry struct {
	PoolRegistryCaller     // Read-only binding to the contract
	PoolRegistryTransactor // Write-only binding to the contract
	PoolRegistryFilterer   // Log filterer for contract events
}

// PoolRegistryCaller is an auto generated read-only Go binding around an Ethereum contract.
type PoolRegistryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PoolRegistryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type PoolRegistryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PoolRegistryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type PoolRegistryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// PoolRegistrySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type PoolRegistrySession struct {
	Contract     *PoolRegistry     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// PoolRegistryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type PoolRegistryCallerSession struct {
	Contract *PoolRegistryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// PoolRegistryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type PoolRegistryTransactorSession struct {
	Contract     *PoolRegistryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// PoolRegistryRaw is an auto generated low-level Go binding around an Ethereum contract.
type PoolRegistryRaw struct {
	Contract *PoolRegistry // Generic contract binding to access the raw methods on
}

// PoolRegistryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type PoolRegistryCallerRaw struct {
	Contract *PoolRegistryCaller // Generic read-only contract binding to access the raw methods on
}

// PoolRegistryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type PoolRegistryTransactorRaw struct {
	Contract *PoolRegistryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewPoolRegistry creates a new instance of PoolRegistry, bound to a specific deployed contract.
func NewPoolRegistry(address common.Address, backend bind.ContractBackend) (*PoolRegistry, error) {
	contract, err := bindPoolRegistry(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &PoolRegistry{PoolRegistryCaller: PoolRegistryCaller{contract: contract}, PoolRegistryTransactor: PoolRegistryTransactor{contract: contract}, PoolRegistryFilterer: PoolRegistryFilterer{contract: contract}}, nil
}

// NewPoolRegistryCaller creates a new read-only instance of PoolRegistry, bound to a specific deployed contract.
func NewPoolRegistryCaller(address common.Address, caller bind.ContractCaller) (*PoolRegistryCaller, error) {
	contract, err := bindPoolRegistry(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryCaller{contract: contract}, nil
}

// NewPoolRegistryTransactor creates a new write-only instance of PoolRegistry, bound to a specific deployed contract.
func NewPoolRegistryTransactor(address common.Address, transactor bind.ContractTransactor) (*PoolRegistryTransactor, error) {
	contract, err := bindPoolRegistry(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryTransactor{contract: contract}, nil
}

// NewPoolRegistryFilterer creates a new log filterer instance of PoolRegistry, bound to a specific deployed contract.
func NewPoolRegistryFilterer(address common.Address, filterer bind.ContractFilterer) (*PoolRegistryFilterer, error) {
	contract, err := bindPoolRegistry(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryFilterer{contract: contract}, nil
}

// bindPoolRegistry binds a generic wrapper to an already deployed contract.
func bindPoolRegistry(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := PoolRegistryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PoolRegistry *PoolRegistryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PoolRegistry.Contract.PoolRegistryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PoolRegistry *PoolRegistryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PoolRegistry.Contract.PoolRegistryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PoolRegistry *PoolRegistryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PoolRegistry.Contract.PoolRegistryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_PoolRegistry *PoolRegistryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _PoolRegistry.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_PoolRegistry *PoolRegistryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _PoolRegistry.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_PoolRegistry *PoolRegistryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _PoolRegistry.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_PoolRegistry *PoolRegistryCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_PoolRegistry *PoolRegistrySession) DEFAULTADMINROLE() ([32]byte, error) {
	return _PoolRegistry.Contract.DEFAULTADMINROLE(&_PoolRegistry.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_PoolRegistry *PoolRegistryCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _PoolRegistry.Contract.DEFAULTADMINROLE(&_PoolRegistry.CallOpts)
}

// FACTORYROLE is a free data retrieval call binding the contract method 0x04a0fb17.
//
// Solidity: function FACTORY_ROLE() view returns(bytes32)
func (_PoolRegistry *PoolRegistryCaller) FACTORYROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "FACTORY_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FACTORYROLE is a free data retrieval call binding the contract method 0x04a0fb17.
//
// Solidity: function FACTORY_ROLE() view returns(bytes32)
func (_PoolRegistry *PoolRegistrySession) FACTORYROLE() ([32]byte, error) {
	return _PoolRegistry.Contract.FACTORYROLE(&_PoolRegistry.CallOpts)
}

// FACTORYROLE is a free data retrieval call binding the contract method 0x04a0fb17.
//
// Solidity: function FACTORY_ROLE() view returns(bytes32)
func (_PoolRegistry *PoolRegistryCallerSession) FACTORYROLE() ([32]byte, error) {
	return _PoolRegistry.Contract.FACTORYROLE(&_PoolRegistry.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_PoolRegistry *PoolRegistryCaller) EventEmitter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "eventEmitter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_PoolRegistry *PoolRegistrySession) EventEmitter() (common.Address, error) {
	return _PoolRegistry.Contract.EventEmitter(&_PoolRegistry.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_PoolRegistry *PoolRegistryCallerSession) EventEmitter() (common.Address, error) {
	return _PoolRegistry.Contract.EventEmitter(&_PoolRegistry.CallOpts)
}

// GetAllPools is a free data retrieval call binding the contract method 0xa7753f09.
//
// Solidity: function getAllPools(uint256 offset, uint256 limit) view returns(address[])
func (_PoolRegistry *PoolRegistryCaller) GetAllPools(opts *bind.CallOpts, offset *big.Int, limit *big.Int) ([]common.Address, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getAllPools", offset, limit)

	if err != nil {
		return *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)

	return out0, err

}

// GetAllPools is a free data retrieval call binding the contract method 0xa7753f09.
//
// Solidity: function getAllPools(uint256 offset, uint256 limit) view returns(address[])
func (_PoolRegistry *PoolRegistrySession) GetAllPools(offset *big.Int, limit *big.Int) ([]common.Address, error) {
	return _PoolRegistry.Contract.GetAllPools(&_PoolRegistry.CallOpts, offset, limit)
}

// GetAllPools is a free data retrieval call binding the contract method 0xa7753f09.
//
// Solidity: function getAllPools(uint256 offset, uint256 limit) view returns(address[])
func (_PoolRegistry *PoolRegistryCallerSession) GetAllPools(offset *big.Int, limit *big.Int) ([]common.Address, error) {
	return _PoolRegistry.Contract.GetAllPools(&_PoolRegistry.CallOpts, offset, limit)
}

// GetNftIdByPool is a free data retrieval call binding the contract method 0xd56ed6c4.
//
// Solidity: function getNftIdByPool(address pool) view returns(uint256)
func (_PoolRegistry *PoolRegistryCaller) GetNftIdByPool(opts *bind.CallOpts, pool common.Address) (*big.Int, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getNftIdByPool", pool)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNftIdByPool is a free data retrieval call binding the contract method 0xd56ed6c4.
//
// Solidity: function getNftIdByPool(address pool) view returns(uint256)
func (_PoolRegistry *PoolRegistrySession) GetNftIdByPool(pool common.Address) (*big.Int, error) {
	return _PoolRegistry.Contract.GetNftIdByPool(&_PoolRegistry.CallOpts, pool)
}

// GetNftIdByPool is a free data retrieval call binding the contract method 0xd56ed6c4.
//
// Solidity: function getNftIdByPool(address pool) view returns(uint256)
func (_PoolRegistry *PoolRegistryCallerSession) GetNftIdByPool(pool common.Address) (*big.Int, error) {
	return _PoolRegistry.Contract.GetNftIdByPool(&_PoolRegistry.CallOpts, pool)
}

// GetPoolByToken is a free data retrieval call binding the contract method 0xbbadc03b.
//
// Solidity: function getPoolByToken(address token) view returns(address)
func (_PoolRegistry *PoolRegistryCaller) GetPoolByToken(opts *bind.CallOpts, token common.Address) (common.Address, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getPoolByToken", token)

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// GetPoolByToken is a free data retrieval call binding the contract method 0xbbadc03b.
//
// Solidity: function getPoolByToken(address token) view returns(address)
func (_PoolRegistry *PoolRegistrySession) GetPoolByToken(token common.Address) (common.Address, error) {
	return _PoolRegistry.Contract.GetPoolByToken(&_PoolRegistry.CallOpts, token)
}

// GetPoolByToken is a free data retrieval call binding the contract method 0xbbadc03b.
//
// Solidity: function getPoolByToken(address token) view returns(address)
func (_PoolRegistry *PoolRegistryCallerSession) GetPoolByToken(token common.Address) (common.Address, error) {
	return _PoolRegistry.Contract.GetPoolByToken(&_PoolRegistry.CallOpts, token)
}

// GetPoolCount is a free data retrieval call binding the contract method 0x8eec5d70.
//
// Solidity: function getPoolCount() view returns(uint256)
func (_PoolRegistry *PoolRegistryCaller) GetPoolCount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getPoolCount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetPoolCount is a free data retrieval call binding the contract method 0x8eec5d70.
//
// Solidity: function getPoolCount() view returns(uint256)
func (_PoolRegistry *PoolRegistrySession) GetPoolCount() (*big.Int, error) {
	return _PoolRegistry.Contract.GetPoolCount(&_PoolRegistry.CallOpts)
}

// GetPoolCount is a free data retrieval call binding the contract method 0x8eec5d70.
//
// Solidity: function getPoolCount() view returns(uint256)
func (_PoolRegistry *PoolRegistryCallerSession) GetPoolCount() (*big.Int, error) {
	return _PoolRegistry.Contract.GetPoolCount(&_PoolRegistry.CallOpts)
}

// GetPoolMetadata is a free data retrieval call binding the contract method 0x51de9d26.
//
// Solidity: function getPoolMetadata(address pool) view returns((address,address,address,uint256,uint256,uint256))
func (_PoolRegistry *PoolRegistryCaller) GetPoolMetadata(opts *bind.CallOpts, pool common.Address) (IPoolRegistryPoolMetadata, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getPoolMetadata", pool)

	if err != nil {
		return *new(IPoolRegistryPoolMetadata), err
	}

	out0 := *abi.ConvertType(out[0], new(IPoolRegistryPoolMetadata)).(*IPoolRegistryPoolMetadata)

	return out0, err

}

// GetPoolMetadata is a free data retrieval call binding the contract method 0x51de9d26.
//
// Solidity: function getPoolMetadata(address pool) view returns((address,address,address,uint256,uint256,uint256))
func (_PoolRegistry *PoolRegistrySession) GetPoolMetadata(pool common.Address) (IPoolRegistryPoolMetadata, error) {
	return _PoolRegistry.Contract.GetPoolMetadata(&_PoolRegistry.CallOpts, pool)
}

// GetPoolMetadata is a free data retrieval call binding the contract method 0x51de9d26.
//
// Solidity: function getPoolMetadata(address pool) view returns((address,address,address,uint256,uint256,uint256))
func (_PoolRegistry *PoolRegistryCallerSession) GetPoolMetadata(pool common.Address) (IPoolRegistryPoolMetadata, error) {
	return _PoolRegistry.Contract.GetPoolMetadata(&_PoolRegistry.CallOpts, pool)
}

// GetPoolsByCreator is a free data retrieval call binding the contract method 0xc84728ef.
//
// Solidity: function getPoolsByCreator(address creator) view returns(address[])
func (_PoolRegistry *PoolRegistryCaller) GetPoolsByCreator(opts *bind.CallOpts, creator common.Address) ([]common.Address, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getPoolsByCreator", creator)

	if err != nil {
		return *new([]common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new([]common.Address)).(*[]common.Address)

	return out0, err

}

// GetPoolsByCreator is a free data retrieval call binding the contract method 0xc84728ef.
//
// Solidity: function getPoolsByCreator(address creator) view returns(address[])
func (_PoolRegistry *PoolRegistrySession) GetPoolsByCreator(creator common.Address) ([]common.Address, error) {
	return _PoolRegistry.Contract.GetPoolsByCreator(&_PoolRegistry.CallOpts, creator)
}

// GetPoolsByCreator is a free data retrieval call binding the contract method 0xc84728ef.
//
// Solidity: function getPoolsByCreator(address creator) view returns(address[])
func (_PoolRegistry *PoolRegistryCallerSession) GetPoolsByCreator(creator common.Address) ([]common.Address, error) {
	return _PoolRegistry.Contract.GetPoolsByCreator(&_PoolRegistry.CallOpts, creator)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_PoolRegistry *PoolRegistryCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_PoolRegistry *PoolRegistrySession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _PoolRegistry.Contract.GetRoleAdmin(&_PoolRegistry.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_PoolRegistry *PoolRegistryCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _PoolRegistry.Contract.GetRoleAdmin(&_PoolRegistry.CallOpts, role)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_PoolRegistry *PoolRegistryCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_PoolRegistry *PoolRegistrySession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _PoolRegistry.Contract.HasRole(&_PoolRegistry.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_PoolRegistry *PoolRegistryCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _PoolRegistry.Contract.HasRole(&_PoolRegistry.CallOpts, role, account)
}

// IsRegisteredPool is a free data retrieval call binding the contract method 0xc699e633.
//
// Solidity: function isRegisteredPool(address pool) view returns(bool)
func (_PoolRegistry *PoolRegistryCaller) IsRegisteredPool(opts *bind.CallOpts, pool common.Address) (bool, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "isRegisteredPool", pool)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsRegisteredPool is a free data retrieval call binding the contract method 0xc699e633.
//
// Solidity: function isRegisteredPool(address pool) view returns(bool)
func (_PoolRegistry *PoolRegistrySession) IsRegisteredPool(pool common.Address) (bool, error) {
	return _PoolRegistry.Contract.IsRegisteredPool(&_PoolRegistry.CallOpts, pool)
}

// IsRegisteredPool is a free data retrieval call binding the contract method 0xc699e633.
//
// Solidity: function isRegisteredPool(address pool) view returns(bool)
func (_PoolRegistry *PoolRegistryCallerSession) IsRegisteredPool(pool common.Address) (bool, error) {
	return _PoolRegistry.Contract.IsRegisteredPool(&_PoolRegistry.CallOpts, pool)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_PoolRegistry *PoolRegistryCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _PoolRegistry.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_PoolRegistry *PoolRegistrySession) ProxiableUUID() ([32]byte, error) {
	return _PoolRegistry.Contract.ProxiableUUID(&_PoolRegistry.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_PoolRegistry *PoolRegistryCallerSession) ProxiableUUID() ([32]byte, error) {
	return _PoolRegistry.Contract.ProxiableUUID(&_PoolRegistry.CallOpts)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_PoolRegistry *PoolRegistryTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _PoolRegistry.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_PoolRegistry *PoolRegistrySession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.GrantRole(&_PoolRegistry.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_PoolRegistry *PoolRegistryTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.GrantRole(&_PoolRegistry.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _eventEmitter, address _admin) returns()
func (_PoolRegistry *PoolRegistryTransactor) Initialize(opts *bind.TransactOpts, _eventEmitter common.Address, _admin common.Address) (*types.Transaction, error) {
	return _PoolRegistry.contract.Transact(opts, "initialize", _eventEmitter, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _eventEmitter, address _admin) returns()
func (_PoolRegistry *PoolRegistrySession) Initialize(_eventEmitter common.Address, _admin common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.Initialize(&_PoolRegistry.TransactOpts, _eventEmitter, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0x485cc955.
//
// Solidity: function initialize(address _eventEmitter, address _admin) returns()
func (_PoolRegistry *PoolRegistryTransactorSession) Initialize(_eventEmitter common.Address, _admin common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.Initialize(&_PoolRegistry.TransactOpts, _eventEmitter, _admin)
}

// Register is a paid mutator transaction binding the contract method 0x43a6d1c3.
//
// Solidity: function register(address pool, address token, address creator, address optical, uint256 nftId) returns()
func (_PoolRegistry *PoolRegistryTransactor) Register(opts *bind.TransactOpts, pool common.Address, token common.Address, creator common.Address, optical common.Address, nftId *big.Int) (*types.Transaction, error) {
	return _PoolRegistry.contract.Transact(opts, "register", pool, token, creator, optical, nftId)
}

// Register is a paid mutator transaction binding the contract method 0x43a6d1c3.
//
// Solidity: function register(address pool, address token, address creator, address optical, uint256 nftId) returns()
func (_PoolRegistry *PoolRegistrySession) Register(pool common.Address, token common.Address, creator common.Address, optical common.Address, nftId *big.Int) (*types.Transaction, error) {
	return _PoolRegistry.Contract.Register(&_PoolRegistry.TransactOpts, pool, token, creator, optical, nftId)
}

// Register is a paid mutator transaction binding the contract method 0x43a6d1c3.
//
// Solidity: function register(address pool, address token, address creator, address optical, uint256 nftId) returns()
func (_PoolRegistry *PoolRegistryTransactorSession) Register(pool common.Address, token common.Address, creator common.Address, optical common.Address, nftId *big.Int) (*types.Transaction, error) {
	return _PoolRegistry.Contract.Register(&_PoolRegistry.TransactOpts, pool, token, creator, optical, nftId)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_PoolRegistry *PoolRegistryTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _PoolRegistry.contract.Transact(opts, "renounceRole", role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_PoolRegistry *PoolRegistrySession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.RenounceRole(&_PoolRegistry.TransactOpts, role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_PoolRegistry *PoolRegistryTransactorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.RenounceRole(&_PoolRegistry.TransactOpts, role, callerConfirmation)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_PoolRegistry *PoolRegistryTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _PoolRegistry.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_PoolRegistry *PoolRegistrySession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.RevokeRole(&_PoolRegistry.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_PoolRegistry *PoolRegistryTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _PoolRegistry.Contract.RevokeRole(&_PoolRegistry.TransactOpts, role, account)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_PoolRegistry *PoolRegistryTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _PoolRegistry.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_PoolRegistry *PoolRegistrySession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _PoolRegistry.Contract.UpgradeToAndCall(&_PoolRegistry.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_PoolRegistry *PoolRegistryTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _PoolRegistry.Contract.UpgradeToAndCall(&_PoolRegistry.TransactOpts, newImplementation, data)
}

// PoolRegistryInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the PoolRegistry contract.
type PoolRegistryInitializedIterator struct {
	Event *PoolRegistryInitialized // Event containing the contract specifics and raw log

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
func (it *PoolRegistryInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PoolRegistryInitialized)
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
		it.Event = new(PoolRegistryInitialized)
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
func (it *PoolRegistryInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PoolRegistryInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PoolRegistryInitialized represents a Initialized event raised by the PoolRegistry contract.
type PoolRegistryInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_PoolRegistry *PoolRegistryFilterer) FilterInitialized(opts *bind.FilterOpts) (*PoolRegistryInitializedIterator, error) {

	logs, sub, err := _PoolRegistry.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &PoolRegistryInitializedIterator{contract: _PoolRegistry.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_PoolRegistry *PoolRegistryFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *PoolRegistryInitialized) (event.Subscription, error) {

	logs, sub, err := _PoolRegistry.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PoolRegistryInitialized)
				if err := _PoolRegistry.contract.UnpackLog(event, "Initialized", log); err != nil {
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

// ParseInitialized is a log parse operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_PoolRegistry *PoolRegistryFilterer) ParseInitialized(log types.Log) (*PoolRegistryInitialized, error) {
	event := new(PoolRegistryInitialized)
	if err := _PoolRegistry.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PoolRegistryPoolRegisteredIterator is returned from FilterPoolRegistered and is used to iterate over the raw logs and unpacked data for PoolRegistered events raised by the PoolRegistry contract.
type PoolRegistryPoolRegisteredIterator struct {
	Event *PoolRegistryPoolRegistered // Event containing the contract specifics and raw log

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
func (it *PoolRegistryPoolRegisteredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PoolRegistryPoolRegistered)
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
		it.Event = new(PoolRegistryPoolRegistered)
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
func (it *PoolRegistryPoolRegisteredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PoolRegistryPoolRegisteredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PoolRegistryPoolRegistered represents a PoolRegistered event raised by the PoolRegistry contract.
type PoolRegistryPoolRegistered struct {
	Pool      common.Address
	Token     common.Address
	Creator   common.Address
	Optical   common.Address
	NftId     *big.Int
	Timestamp *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterPoolRegistered is a free log retrieval operation binding the contract event 0xdd85b374a142757b7745e1d5957bddced32d0fae0d5a038c41e7f79dc162b5ce.
//
// Solidity: event PoolRegistered(address indexed pool, address indexed token, address indexed creator, address optical, uint256 nftId, uint256 timestamp)
func (_PoolRegistry *PoolRegistryFilterer) FilterPoolRegistered(opts *bind.FilterOpts, pool []common.Address, token []common.Address, creator []common.Address) (*PoolRegistryPoolRegisteredIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var creatorRule []interface{}
	for _, creatorItem := range creator {
		creatorRule = append(creatorRule, creatorItem)
	}

	logs, sub, err := _PoolRegistry.contract.FilterLogs(opts, "PoolRegistered", poolRule, tokenRule, creatorRule)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryPoolRegisteredIterator{contract: _PoolRegistry.contract, event: "PoolRegistered", logs: logs, sub: sub}, nil
}

// WatchPoolRegistered is a free log subscription operation binding the contract event 0xdd85b374a142757b7745e1d5957bddced32d0fae0d5a038c41e7f79dc162b5ce.
//
// Solidity: event PoolRegistered(address indexed pool, address indexed token, address indexed creator, address optical, uint256 nftId, uint256 timestamp)
func (_PoolRegistry *PoolRegistryFilterer) WatchPoolRegistered(opts *bind.WatchOpts, sink chan<- *PoolRegistryPoolRegistered, pool []common.Address, token []common.Address, creator []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var creatorRule []interface{}
	for _, creatorItem := range creator {
		creatorRule = append(creatorRule, creatorItem)
	}

	logs, sub, err := _PoolRegistry.contract.WatchLogs(opts, "PoolRegistered", poolRule, tokenRule, creatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PoolRegistryPoolRegistered)
				if err := _PoolRegistry.contract.UnpackLog(event, "PoolRegistered", log); err != nil {
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

// ParsePoolRegistered is a log parse operation binding the contract event 0xdd85b374a142757b7745e1d5957bddced32d0fae0d5a038c41e7f79dc162b5ce.
//
// Solidity: event PoolRegistered(address indexed pool, address indexed token, address indexed creator, address optical, uint256 nftId, uint256 timestamp)
func (_PoolRegistry *PoolRegistryFilterer) ParsePoolRegistered(log types.Log) (*PoolRegistryPoolRegistered, error) {
	event := new(PoolRegistryPoolRegistered)
	if err := _PoolRegistry.contract.UnpackLog(event, "PoolRegistered", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PoolRegistryRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the PoolRegistry contract.
type PoolRegistryRoleAdminChangedIterator struct {
	Event *PoolRegistryRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *PoolRegistryRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PoolRegistryRoleAdminChanged)
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
		it.Event = new(PoolRegistryRoleAdminChanged)
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
func (it *PoolRegistryRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PoolRegistryRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PoolRegistryRoleAdminChanged represents a RoleAdminChanged event raised by the PoolRegistry contract.
type PoolRegistryRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_PoolRegistry *PoolRegistryFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*PoolRegistryRoleAdminChangedIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var previousAdminRoleRule []interface{}
	for _, previousAdminRoleItem := range previousAdminRole {
		previousAdminRoleRule = append(previousAdminRoleRule, previousAdminRoleItem)
	}
	var newAdminRoleRule []interface{}
	for _, newAdminRoleItem := range newAdminRole {
		newAdminRoleRule = append(newAdminRoleRule, newAdminRoleItem)
	}

	logs, sub, err := _PoolRegistry.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryRoleAdminChangedIterator{contract: _PoolRegistry.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_PoolRegistry *PoolRegistryFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *PoolRegistryRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var previousAdminRoleRule []interface{}
	for _, previousAdminRoleItem := range previousAdminRole {
		previousAdminRoleRule = append(previousAdminRoleRule, previousAdminRoleItem)
	}
	var newAdminRoleRule []interface{}
	for _, newAdminRoleItem := range newAdminRole {
		newAdminRoleRule = append(newAdminRoleRule, newAdminRoleItem)
	}

	logs, sub, err := _PoolRegistry.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PoolRegistryRoleAdminChanged)
				if err := _PoolRegistry.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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

// ParseRoleAdminChanged is a log parse operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_PoolRegistry *PoolRegistryFilterer) ParseRoleAdminChanged(log types.Log) (*PoolRegistryRoleAdminChanged, error) {
	event := new(PoolRegistryRoleAdminChanged)
	if err := _PoolRegistry.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PoolRegistryRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the PoolRegistry contract.
type PoolRegistryRoleGrantedIterator struct {
	Event *PoolRegistryRoleGranted // Event containing the contract specifics and raw log

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
func (it *PoolRegistryRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PoolRegistryRoleGranted)
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
		it.Event = new(PoolRegistryRoleGranted)
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
func (it *PoolRegistryRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PoolRegistryRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PoolRegistryRoleGranted represents a RoleGranted event raised by the PoolRegistry contract.
type PoolRegistryRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_PoolRegistry *PoolRegistryFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*PoolRegistryRoleGrantedIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _PoolRegistry.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryRoleGrantedIterator{contract: _PoolRegistry.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_PoolRegistry *PoolRegistryFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *PoolRegistryRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _PoolRegistry.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PoolRegistryRoleGranted)
				if err := _PoolRegistry.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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

// ParseRoleGranted is a log parse operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_PoolRegistry *PoolRegistryFilterer) ParseRoleGranted(log types.Log) (*PoolRegistryRoleGranted, error) {
	event := new(PoolRegistryRoleGranted)
	if err := _PoolRegistry.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PoolRegistryRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the PoolRegistry contract.
type PoolRegistryRoleRevokedIterator struct {
	Event *PoolRegistryRoleRevoked // Event containing the contract specifics and raw log

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
func (it *PoolRegistryRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PoolRegistryRoleRevoked)
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
		it.Event = new(PoolRegistryRoleRevoked)
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
func (it *PoolRegistryRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PoolRegistryRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PoolRegistryRoleRevoked represents a RoleRevoked event raised by the PoolRegistry contract.
type PoolRegistryRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_PoolRegistry *PoolRegistryFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*PoolRegistryRoleRevokedIterator, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _PoolRegistry.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryRoleRevokedIterator{contract: _PoolRegistry.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_PoolRegistry *PoolRegistryFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *PoolRegistryRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

	var roleRule []interface{}
	for _, roleItem := range role {
		roleRule = append(roleRule, roleItem)
	}
	var accountRule []interface{}
	for _, accountItem := range account {
		accountRule = append(accountRule, accountItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _PoolRegistry.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PoolRegistryRoleRevoked)
				if err := _PoolRegistry.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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

// ParseRoleRevoked is a log parse operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_PoolRegistry *PoolRegistryFilterer) ParseRoleRevoked(log types.Log) (*PoolRegistryRoleRevoked, error) {
	event := new(PoolRegistryRoleRevoked)
	if err := _PoolRegistry.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// PoolRegistryUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the PoolRegistry contract.
type PoolRegistryUpgradedIterator struct {
	Event *PoolRegistryUpgraded // Event containing the contract specifics and raw log

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
func (it *PoolRegistryUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(PoolRegistryUpgraded)
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
		it.Event = new(PoolRegistryUpgraded)
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
func (it *PoolRegistryUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *PoolRegistryUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// PoolRegistryUpgraded represents a Upgraded event raised by the PoolRegistry contract.
type PoolRegistryUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_PoolRegistry *PoolRegistryFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*PoolRegistryUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _PoolRegistry.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &PoolRegistryUpgradedIterator{contract: _PoolRegistry.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_PoolRegistry *PoolRegistryFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *PoolRegistryUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _PoolRegistry.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(PoolRegistryUpgraded)
				if err := _PoolRegistry.contract.UnpackLog(event, "Upgraded", log); err != nil {
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

// ParseUpgraded is a log parse operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_PoolRegistry *PoolRegistryFilterer) ParseUpgraded(log types.Log) (*PoolRegistryUpgraded, error) {
	event := new(PoolRegistryUpgraded)
	if err := _PoolRegistry.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
