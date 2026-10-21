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

// FeesRouterMetaData contains all meta data concerning the FeesRouter contract.
var FeesRouterMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"MissingRole\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotNftOwner\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ReentrancyGuardReentrantCall\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughActiveProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnauthorizedUpgrade\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"WrongStrategy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddress\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"holder\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"AirdropClaimed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"AirdropExecuted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"oldStrategy\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"newStrategy\",\"type\":\"uint8\"}],\"name\":\"FeeStrategyChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"FeesBurned\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"owner\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"FeesClaimed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"LpRewardsExecuted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"name\":\"claimAirdrop\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"name\":\"claimFees\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"name\":\"executeAirdrop\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"name\":\"executeBurn\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"name\":\"executeLpRewards\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"feeAccumulator\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_nftContract\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_feeAccumulator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_poolRegistry\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_admin\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"nftContract\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"poolRegistry\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"callerConfirmation\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"internalType\":\"uint8\",\"name\":\"newStrategy\",\"type\":\"uint8\"}],\"name\":\"setFeeStrategy\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"}]",
}

// FeesRouterABI is the input ABI used to generate the binding from.
// Deprecated: Use FeesRouterMetaData.ABI instead.
var FeesRouterABI = FeesRouterMetaData.ABI

// FeesRouter is an auto generated Go binding around an Ethereum contract.
type FeesRouter struct {
	FeesRouterCaller     // Read-only binding to the contract
	FeesRouterTransactor // Write-only binding to the contract
	FeesRouterFilterer   // Log filterer for contract events
}

// FeesRouterCaller is an auto generated read-only Go binding around an Ethereum contract.
type FeesRouterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeesRouterTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FeesRouterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeesRouterFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FeesRouterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeesRouterSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FeesRouterSession struct {
	Contract     *FeesRouter       // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// FeesRouterCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FeesRouterCallerSession struct {
	Contract *FeesRouterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts     // Call options to use throughout this session
}

// FeesRouterTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FeesRouterTransactorSession struct {
	Contract     *FeesRouterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts     // Transaction auth options to use throughout this session
}

// FeesRouterRaw is an auto generated low-level Go binding around an Ethereum contract.
type FeesRouterRaw struct {
	Contract *FeesRouter // Generic contract binding to access the raw methods on
}

// FeesRouterCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FeesRouterCallerRaw struct {
	Contract *FeesRouterCaller // Generic read-only contract binding to access the raw methods on
}

// FeesRouterTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FeesRouterTransactorRaw struct {
	Contract *FeesRouterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFeesRouter creates a new instance of FeesRouter, bound to a specific deployed contract.
func NewFeesRouter(address common.Address, backend bind.ContractBackend) (*FeesRouter, error) {
	contract, err := bindFeesRouter(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FeesRouter{FeesRouterCaller: FeesRouterCaller{contract: contract}, FeesRouterTransactor: FeesRouterTransactor{contract: contract}, FeesRouterFilterer: FeesRouterFilterer{contract: contract}}, nil
}

// NewFeesRouterCaller creates a new read-only instance of FeesRouter, bound to a specific deployed contract.
func NewFeesRouterCaller(address common.Address, caller bind.ContractCaller) (*FeesRouterCaller, error) {
	contract, err := bindFeesRouter(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FeesRouterCaller{contract: contract}, nil
}

// NewFeesRouterTransactor creates a new write-only instance of FeesRouter, bound to a specific deployed contract.
func NewFeesRouterTransactor(address common.Address, transactor bind.ContractTransactor) (*FeesRouterTransactor, error) {
	contract, err := bindFeesRouter(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FeesRouterTransactor{contract: contract}, nil
}

// NewFeesRouterFilterer creates a new log filterer instance of FeesRouter, bound to a specific deployed contract.
func NewFeesRouterFilterer(address common.Address, filterer bind.ContractFilterer) (*FeesRouterFilterer, error) {
	contract, err := bindFeesRouter(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FeesRouterFilterer{contract: contract}, nil
}

// bindFeesRouter binds a generic wrapper to an already deployed contract.
func bindFeesRouter(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FeesRouterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FeesRouter *FeesRouterRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FeesRouter.Contract.FeesRouterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FeesRouter *FeesRouterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FeesRouter.Contract.FeesRouterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FeesRouter *FeesRouterRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FeesRouter.Contract.FeesRouterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FeesRouter *FeesRouterCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FeesRouter.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FeesRouter *FeesRouterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FeesRouter.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FeesRouter *FeesRouterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FeesRouter.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_FeesRouter *FeesRouterCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_FeesRouter *FeesRouterSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _FeesRouter.Contract.DEFAULTADMINROLE(&_FeesRouter.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_FeesRouter *FeesRouterCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _FeesRouter.Contract.DEFAULTADMINROLE(&_FeesRouter.CallOpts)
}

// FeeAccumulator is a free data retrieval call binding the contract method 0x5eaea35f.
//
// Solidity: function feeAccumulator() view returns(address)
func (_FeesRouter *FeesRouterCaller) FeeAccumulator(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "feeAccumulator")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// FeeAccumulator is a free data retrieval call binding the contract method 0x5eaea35f.
//
// Solidity: function feeAccumulator() view returns(address)
func (_FeesRouter *FeesRouterSession) FeeAccumulator() (common.Address, error) {
	return _FeesRouter.Contract.FeeAccumulator(&_FeesRouter.CallOpts)
}

// FeeAccumulator is a free data retrieval call binding the contract method 0x5eaea35f.
//
// Solidity: function feeAccumulator() view returns(address)
func (_FeesRouter *FeesRouterCallerSession) FeeAccumulator() (common.Address, error) {
	return _FeesRouter.Contract.FeeAccumulator(&_FeesRouter.CallOpts)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_FeesRouter *FeesRouterCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_FeesRouter *FeesRouterSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _FeesRouter.Contract.GetRoleAdmin(&_FeesRouter.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_FeesRouter *FeesRouterCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _FeesRouter.Contract.GetRoleAdmin(&_FeesRouter.CallOpts, role)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_FeesRouter *FeesRouterCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_FeesRouter *FeesRouterSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _FeesRouter.Contract.HasRole(&_FeesRouter.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_FeesRouter *FeesRouterCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _FeesRouter.Contract.HasRole(&_FeesRouter.CallOpts, role, account)
}

// NftContract is a free data retrieval call binding the contract method 0xd56d229d.
//
// Solidity: function nftContract() view returns(address)
func (_FeesRouter *FeesRouterCaller) NftContract(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "nftContract")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// NftContract is a free data retrieval call binding the contract method 0xd56d229d.
//
// Solidity: function nftContract() view returns(address)
func (_FeesRouter *FeesRouterSession) NftContract() (common.Address, error) {
	return _FeesRouter.Contract.NftContract(&_FeesRouter.CallOpts)
}

// NftContract is a free data retrieval call binding the contract method 0xd56d229d.
//
// Solidity: function nftContract() view returns(address)
func (_FeesRouter *FeesRouterCallerSession) NftContract() (common.Address, error) {
	return _FeesRouter.Contract.NftContract(&_FeesRouter.CallOpts)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_FeesRouter *FeesRouterCaller) PoolRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "poolRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_FeesRouter *FeesRouterSession) PoolRegistry() (common.Address, error) {
	return _FeesRouter.Contract.PoolRegistry(&_FeesRouter.CallOpts)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_FeesRouter *FeesRouterCallerSession) PoolRegistry() (common.Address, error) {
	return _FeesRouter.Contract.PoolRegistry(&_FeesRouter.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FeesRouter *FeesRouterCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FeesRouter.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FeesRouter *FeesRouterSession) ProxiableUUID() ([32]byte, error) {
	return _FeesRouter.Contract.ProxiableUUID(&_FeesRouter.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FeesRouter *FeesRouterCallerSession) ProxiableUUID() ([32]byte, error) {
	return _FeesRouter.Contract.ProxiableUUID(&_FeesRouter.CallOpts)
}

// ClaimAirdrop is a paid mutator transaction binding the contract method 0xe30d4440.
//
// Solidity: function claimAirdrop(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactor) ClaimAirdrop(opts *bind.TransactOpts, nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "claimAirdrop", nftId)
}

// ClaimAirdrop is a paid mutator transaction binding the contract method 0xe30d4440.
//
// Solidity: function claimAirdrop(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterSession) ClaimAirdrop(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ClaimAirdrop(&_FeesRouter.TransactOpts, nftId)
}

// ClaimAirdrop is a paid mutator transaction binding the contract method 0xe30d4440.
//
// Solidity: function claimAirdrop(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactorSession) ClaimAirdrop(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ClaimAirdrop(&_FeesRouter.TransactOpts, nftId)
}

// ClaimFees is a paid mutator transaction binding the contract method 0xac68a748.
//
// Solidity: function claimFees(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactor) ClaimFees(opts *bind.TransactOpts, nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "claimFees", nftId)
}

// ClaimFees is a paid mutator transaction binding the contract method 0xac68a748.
//
// Solidity: function claimFees(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterSession) ClaimFees(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ClaimFees(&_FeesRouter.TransactOpts, nftId)
}

// ClaimFees is a paid mutator transaction binding the contract method 0xac68a748.
//
// Solidity: function claimFees(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactorSession) ClaimFees(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ClaimFees(&_FeesRouter.TransactOpts, nftId)
}

// ExecuteAirdrop is a paid mutator transaction binding the contract method 0x9d4be5b2.
//
// Solidity: function executeAirdrop(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactor) ExecuteAirdrop(opts *bind.TransactOpts, nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "executeAirdrop", nftId)
}

// ExecuteAirdrop is a paid mutator transaction binding the contract method 0x9d4be5b2.
//
// Solidity: function executeAirdrop(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterSession) ExecuteAirdrop(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ExecuteAirdrop(&_FeesRouter.TransactOpts, nftId)
}

// ExecuteAirdrop is a paid mutator transaction binding the contract method 0x9d4be5b2.
//
// Solidity: function executeAirdrop(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactorSession) ExecuteAirdrop(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ExecuteAirdrop(&_FeesRouter.TransactOpts, nftId)
}

// ExecuteBurn is a paid mutator transaction binding the contract method 0x15dec3cf.
//
// Solidity: function executeBurn(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactor) ExecuteBurn(opts *bind.TransactOpts, nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "executeBurn", nftId)
}

// ExecuteBurn is a paid mutator transaction binding the contract method 0x15dec3cf.
//
// Solidity: function executeBurn(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterSession) ExecuteBurn(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ExecuteBurn(&_FeesRouter.TransactOpts, nftId)
}

// ExecuteBurn is a paid mutator transaction binding the contract method 0x15dec3cf.
//
// Solidity: function executeBurn(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactorSession) ExecuteBurn(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ExecuteBurn(&_FeesRouter.TransactOpts, nftId)
}

// ExecuteLpRewards is a paid mutator transaction binding the contract method 0x90d862a1.
//
// Solidity: function executeLpRewards(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactor) ExecuteLpRewards(opts *bind.TransactOpts, nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "executeLpRewards", nftId)
}

// ExecuteLpRewards is a paid mutator transaction binding the contract method 0x90d862a1.
//
// Solidity: function executeLpRewards(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterSession) ExecuteLpRewards(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ExecuteLpRewards(&_FeesRouter.TransactOpts, nftId)
}

// ExecuteLpRewards is a paid mutator transaction binding the contract method 0x90d862a1.
//
// Solidity: function executeLpRewards(uint256 nftId) returns(uint256 amount)
func (_FeesRouter *FeesRouterTransactorSession) ExecuteLpRewards(nftId *big.Int) (*types.Transaction, error) {
	return _FeesRouter.Contract.ExecuteLpRewards(&_FeesRouter.TransactOpts, nftId)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_FeesRouter *FeesRouterTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_FeesRouter *FeesRouterSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.GrantRole(&_FeesRouter.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_FeesRouter *FeesRouterTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.GrantRole(&_FeesRouter.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0xf8c8765e.
//
// Solidity: function initialize(address _nftContract, address _feeAccumulator, address _poolRegistry, address _admin) returns()
func (_FeesRouter *FeesRouterTransactor) Initialize(opts *bind.TransactOpts, _nftContract common.Address, _feeAccumulator common.Address, _poolRegistry common.Address, _admin common.Address) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "initialize", _nftContract, _feeAccumulator, _poolRegistry, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xf8c8765e.
//
// Solidity: function initialize(address _nftContract, address _feeAccumulator, address _poolRegistry, address _admin) returns()
func (_FeesRouter *FeesRouterSession) Initialize(_nftContract common.Address, _feeAccumulator common.Address, _poolRegistry common.Address, _admin common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.Initialize(&_FeesRouter.TransactOpts, _nftContract, _feeAccumulator, _poolRegistry, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xf8c8765e.
//
// Solidity: function initialize(address _nftContract, address _feeAccumulator, address _poolRegistry, address _admin) returns()
func (_FeesRouter *FeesRouterTransactorSession) Initialize(_nftContract common.Address, _feeAccumulator common.Address, _poolRegistry common.Address, _admin common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.Initialize(&_FeesRouter.TransactOpts, _nftContract, _feeAccumulator, _poolRegistry, _admin)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_FeesRouter *FeesRouterTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "renounceRole", role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_FeesRouter *FeesRouterSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.RenounceRole(&_FeesRouter.TransactOpts, role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_FeesRouter *FeesRouterTransactorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.RenounceRole(&_FeesRouter.TransactOpts, role, callerConfirmation)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_FeesRouter *FeesRouterTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_FeesRouter *FeesRouterSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.RevokeRole(&_FeesRouter.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_FeesRouter *FeesRouterTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeesRouter.Contract.RevokeRole(&_FeesRouter.TransactOpts, role, account)
}

// SetFeeStrategy is a paid mutator transaction binding the contract method 0x9fcf8f62.
//
// Solidity: function setFeeStrategy(uint256 nftId, uint8 newStrategy) returns()
func (_FeesRouter *FeesRouterTransactor) SetFeeStrategy(opts *bind.TransactOpts, nftId *big.Int, newStrategy uint8) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "setFeeStrategy", nftId, newStrategy)
}

// SetFeeStrategy is a paid mutator transaction binding the contract method 0x9fcf8f62.
//
// Solidity: function setFeeStrategy(uint256 nftId, uint8 newStrategy) returns()
func (_FeesRouter *FeesRouterSession) SetFeeStrategy(nftId *big.Int, newStrategy uint8) (*types.Transaction, error) {
	return _FeesRouter.Contract.SetFeeStrategy(&_FeesRouter.TransactOpts, nftId, newStrategy)
}

// SetFeeStrategy is a paid mutator transaction binding the contract method 0x9fcf8f62.
//
// Solidity: function setFeeStrategy(uint256 nftId, uint8 newStrategy) returns()
func (_FeesRouter *FeesRouterTransactorSession) SetFeeStrategy(nftId *big.Int, newStrategy uint8) (*types.Transaction, error) {
	return _FeesRouter.Contract.SetFeeStrategy(&_FeesRouter.TransactOpts, nftId, newStrategy)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FeesRouter *FeesRouterTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FeesRouter.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FeesRouter *FeesRouterSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FeesRouter.Contract.UpgradeToAndCall(&_FeesRouter.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FeesRouter *FeesRouterTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FeesRouter.Contract.UpgradeToAndCall(&_FeesRouter.TransactOpts, newImplementation, data)
}

// FeesRouterAirdropClaimedIterator is returned from FilterAirdropClaimed and is used to iterate over the raw logs and unpacked data for AirdropClaimed events raised by the FeesRouter contract.
type FeesRouterAirdropClaimedIterator struct {
	Event *FeesRouterAirdropClaimed // Event containing the contract specifics and raw log

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
func (it *FeesRouterAirdropClaimedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterAirdropClaimed)
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
		it.Event = new(FeesRouterAirdropClaimed)
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
func (it *FeesRouterAirdropClaimedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterAirdropClaimedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterAirdropClaimed represents a AirdropClaimed event raised by the FeesRouter contract.
type FeesRouterAirdropClaimed struct {
	NftId  *big.Int
	Holder common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterAirdropClaimed is a free log retrieval operation binding the contract event 0x752d3a5de250652e9111e24fea2886c837dadfe50c8479ad3b5511f4c36f20a2.
//
// Solidity: event AirdropClaimed(uint256 indexed nftId, address indexed holder, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) FilterAirdropClaimed(opts *bind.FilterOpts, nftId []*big.Int, holder []common.Address) (*FeesRouterAirdropClaimedIterator, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}
	var holderRule []interface{}
	for _, holderItem := range holder {
		holderRule = append(holderRule, holderItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "AirdropClaimed", nftIdRule, holderRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterAirdropClaimedIterator{contract: _FeesRouter.contract, event: "AirdropClaimed", logs: logs, sub: sub}, nil
}

// WatchAirdropClaimed is a free log subscription operation binding the contract event 0x752d3a5de250652e9111e24fea2886c837dadfe50c8479ad3b5511f4c36f20a2.
//
// Solidity: event AirdropClaimed(uint256 indexed nftId, address indexed holder, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) WatchAirdropClaimed(opts *bind.WatchOpts, sink chan<- *FeesRouterAirdropClaimed, nftId []*big.Int, holder []common.Address) (event.Subscription, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}
	var holderRule []interface{}
	for _, holderItem := range holder {
		holderRule = append(holderRule, holderItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "AirdropClaimed", nftIdRule, holderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterAirdropClaimed)
				if err := _FeesRouter.contract.UnpackLog(event, "AirdropClaimed", log); err != nil {
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

// ParseAirdropClaimed is a log parse operation binding the contract event 0x752d3a5de250652e9111e24fea2886c837dadfe50c8479ad3b5511f4c36f20a2.
//
// Solidity: event AirdropClaimed(uint256 indexed nftId, address indexed holder, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) ParseAirdropClaimed(log types.Log) (*FeesRouterAirdropClaimed, error) {
	event := new(FeesRouterAirdropClaimed)
	if err := _FeesRouter.contract.UnpackLog(event, "AirdropClaimed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterAirdropExecutedIterator is returned from FilterAirdropExecuted and is used to iterate over the raw logs and unpacked data for AirdropExecuted events raised by the FeesRouter contract.
type FeesRouterAirdropExecutedIterator struct {
	Event *FeesRouterAirdropExecuted // Event containing the contract specifics and raw log

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
func (it *FeesRouterAirdropExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterAirdropExecuted)
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
		it.Event = new(FeesRouterAirdropExecuted)
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
func (it *FeesRouterAirdropExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterAirdropExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterAirdropExecuted represents a AirdropExecuted event raised by the FeesRouter contract.
type FeesRouterAirdropExecuted struct {
	NftId  *big.Int
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterAirdropExecuted is a free log retrieval operation binding the contract event 0x0fa3c377438bb545a4c9ecaa3e574d96bb1b2c6416976df85919d2b83a57bd9f.
//
// Solidity: event AirdropExecuted(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) FilterAirdropExecuted(opts *bind.FilterOpts, nftId []*big.Int) (*FeesRouterAirdropExecutedIterator, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "AirdropExecuted", nftIdRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterAirdropExecutedIterator{contract: _FeesRouter.contract, event: "AirdropExecuted", logs: logs, sub: sub}, nil
}

// WatchAirdropExecuted is a free log subscription operation binding the contract event 0x0fa3c377438bb545a4c9ecaa3e574d96bb1b2c6416976df85919d2b83a57bd9f.
//
// Solidity: event AirdropExecuted(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) WatchAirdropExecuted(opts *bind.WatchOpts, sink chan<- *FeesRouterAirdropExecuted, nftId []*big.Int) (event.Subscription, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "AirdropExecuted", nftIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterAirdropExecuted)
				if err := _FeesRouter.contract.UnpackLog(event, "AirdropExecuted", log); err != nil {
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

// ParseAirdropExecuted is a log parse operation binding the contract event 0x0fa3c377438bb545a4c9ecaa3e574d96bb1b2c6416976df85919d2b83a57bd9f.
//
// Solidity: event AirdropExecuted(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) ParseAirdropExecuted(log types.Log) (*FeesRouterAirdropExecuted, error) {
	event := new(FeesRouterAirdropExecuted)
	if err := _FeesRouter.contract.UnpackLog(event, "AirdropExecuted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterFeeStrategyChangedIterator is returned from FilterFeeStrategyChanged and is used to iterate over the raw logs and unpacked data for FeeStrategyChanged events raised by the FeesRouter contract.
type FeesRouterFeeStrategyChangedIterator struct {
	Event *FeesRouterFeeStrategyChanged // Event containing the contract specifics and raw log

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
func (it *FeesRouterFeeStrategyChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterFeeStrategyChanged)
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
		it.Event = new(FeesRouterFeeStrategyChanged)
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
func (it *FeesRouterFeeStrategyChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterFeeStrategyChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterFeeStrategyChanged represents a FeeStrategyChanged event raised by the FeesRouter contract.
type FeesRouterFeeStrategyChanged struct {
	NftId       *big.Int
	OldStrategy uint8
	NewStrategy uint8
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterFeeStrategyChanged is a free log retrieval operation binding the contract event 0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568.
//
// Solidity: event FeeStrategyChanged(uint256 indexed nftId, uint8 oldStrategy, uint8 newStrategy)
func (_FeesRouter *FeesRouterFilterer) FilterFeeStrategyChanged(opts *bind.FilterOpts, nftId []*big.Int) (*FeesRouterFeeStrategyChangedIterator, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "FeeStrategyChanged", nftIdRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterFeeStrategyChangedIterator{contract: _FeesRouter.contract, event: "FeeStrategyChanged", logs: logs, sub: sub}, nil
}

// WatchFeeStrategyChanged is a free log subscription operation binding the contract event 0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568.
//
// Solidity: event FeeStrategyChanged(uint256 indexed nftId, uint8 oldStrategy, uint8 newStrategy)
func (_FeesRouter *FeesRouterFilterer) WatchFeeStrategyChanged(opts *bind.WatchOpts, sink chan<- *FeesRouterFeeStrategyChanged, nftId []*big.Int) (event.Subscription, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "FeeStrategyChanged", nftIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterFeeStrategyChanged)
				if err := _FeesRouter.contract.UnpackLog(event, "FeeStrategyChanged", log); err != nil {
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

// ParseFeeStrategyChanged is a log parse operation binding the contract event 0x773a9b32cf9f6ecdff8ee5e6f78159f15beb042f9da471ba934182412ea69568.
//
// Solidity: event FeeStrategyChanged(uint256 indexed nftId, uint8 oldStrategy, uint8 newStrategy)
func (_FeesRouter *FeesRouterFilterer) ParseFeeStrategyChanged(log types.Log) (*FeesRouterFeeStrategyChanged, error) {
	event := new(FeesRouterFeeStrategyChanged)
	if err := _FeesRouter.contract.UnpackLog(event, "FeeStrategyChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterFeesBurnedIterator is returned from FilterFeesBurned and is used to iterate over the raw logs and unpacked data for FeesBurned events raised by the FeesRouter contract.
type FeesRouterFeesBurnedIterator struct {
	Event *FeesRouterFeesBurned // Event containing the contract specifics and raw log

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
func (it *FeesRouterFeesBurnedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterFeesBurned)
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
		it.Event = new(FeesRouterFeesBurned)
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
func (it *FeesRouterFeesBurnedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterFeesBurnedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterFeesBurned represents a FeesBurned event raised by the FeesRouter contract.
type FeesRouterFeesBurned struct {
	NftId  *big.Int
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterFeesBurned is a free log retrieval operation binding the contract event 0x9768e1fe058fe58a8022c0c5a88689f80a64c3a69cc5ba498c518407fc04ced5.
//
// Solidity: event FeesBurned(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) FilterFeesBurned(opts *bind.FilterOpts, nftId []*big.Int) (*FeesRouterFeesBurnedIterator, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "FeesBurned", nftIdRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterFeesBurnedIterator{contract: _FeesRouter.contract, event: "FeesBurned", logs: logs, sub: sub}, nil
}

// WatchFeesBurned is a free log subscription operation binding the contract event 0x9768e1fe058fe58a8022c0c5a88689f80a64c3a69cc5ba498c518407fc04ced5.
//
// Solidity: event FeesBurned(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) WatchFeesBurned(opts *bind.WatchOpts, sink chan<- *FeesRouterFeesBurned, nftId []*big.Int) (event.Subscription, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "FeesBurned", nftIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterFeesBurned)
				if err := _FeesRouter.contract.UnpackLog(event, "FeesBurned", log); err != nil {
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

// ParseFeesBurned is a log parse operation binding the contract event 0x9768e1fe058fe58a8022c0c5a88689f80a64c3a69cc5ba498c518407fc04ced5.
//
// Solidity: event FeesBurned(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) ParseFeesBurned(log types.Log) (*FeesRouterFeesBurned, error) {
	event := new(FeesRouterFeesBurned)
	if err := _FeesRouter.contract.UnpackLog(event, "FeesBurned", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterFeesClaimedIterator is returned from FilterFeesClaimed and is used to iterate over the raw logs and unpacked data for FeesClaimed events raised by the FeesRouter contract.
type FeesRouterFeesClaimedIterator struct {
	Event *FeesRouterFeesClaimed // Event containing the contract specifics and raw log

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
func (it *FeesRouterFeesClaimedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterFeesClaimed)
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
		it.Event = new(FeesRouterFeesClaimed)
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
func (it *FeesRouterFeesClaimedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterFeesClaimedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterFeesClaimed represents a FeesClaimed event raised by the FeesRouter contract.
type FeesRouterFeesClaimed struct {
	NftId  *big.Int
	Owner  common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterFeesClaimed is a free log retrieval operation binding the contract event 0x2227733fc4c8a9034cb58087dcf6995128b9c0233b038b03366aaf30c92b92d6.
//
// Solidity: event FeesClaimed(uint256 indexed nftId, address indexed owner, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) FilterFeesClaimed(opts *bind.FilterOpts, nftId []*big.Int, owner []common.Address) (*FeesRouterFeesClaimedIterator, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "FeesClaimed", nftIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterFeesClaimedIterator{contract: _FeesRouter.contract, event: "FeesClaimed", logs: logs, sub: sub}, nil
}

// WatchFeesClaimed is a free log subscription operation binding the contract event 0x2227733fc4c8a9034cb58087dcf6995128b9c0233b038b03366aaf30c92b92d6.
//
// Solidity: event FeesClaimed(uint256 indexed nftId, address indexed owner, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) WatchFeesClaimed(opts *bind.WatchOpts, sink chan<- *FeesRouterFeesClaimed, nftId []*big.Int, owner []common.Address) (event.Subscription, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}
	var ownerRule []interface{}
	for _, ownerItem := range owner {
		ownerRule = append(ownerRule, ownerItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "FeesClaimed", nftIdRule, ownerRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterFeesClaimed)
				if err := _FeesRouter.contract.UnpackLog(event, "FeesClaimed", log); err != nil {
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

// ParseFeesClaimed is a log parse operation binding the contract event 0x2227733fc4c8a9034cb58087dcf6995128b9c0233b038b03366aaf30c92b92d6.
//
// Solidity: event FeesClaimed(uint256 indexed nftId, address indexed owner, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) ParseFeesClaimed(log types.Log) (*FeesRouterFeesClaimed, error) {
	event := new(FeesRouterFeesClaimed)
	if err := _FeesRouter.contract.UnpackLog(event, "FeesClaimed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the FeesRouter contract.
type FeesRouterInitializedIterator struct {
	Event *FeesRouterInitialized // Event containing the contract specifics and raw log

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
func (it *FeesRouterInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterInitialized)
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
		it.Event = new(FeesRouterInitialized)
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
func (it *FeesRouterInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterInitialized represents a Initialized event raised by the FeesRouter contract.
type FeesRouterInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FeesRouter *FeesRouterFilterer) FilterInitialized(opts *bind.FilterOpts) (*FeesRouterInitializedIterator, error) {

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &FeesRouterInitializedIterator{contract: _FeesRouter.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FeesRouter *FeesRouterFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *FeesRouterInitialized) (event.Subscription, error) {

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterInitialized)
				if err := _FeesRouter.contract.UnpackLog(event, "Initialized", log); err != nil {
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
func (_FeesRouter *FeesRouterFilterer) ParseInitialized(log types.Log) (*FeesRouterInitialized, error) {
	event := new(FeesRouterInitialized)
	if err := _FeesRouter.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterLpRewardsExecutedIterator is returned from FilterLpRewardsExecuted and is used to iterate over the raw logs and unpacked data for LpRewardsExecuted events raised by the FeesRouter contract.
type FeesRouterLpRewardsExecutedIterator struct {
	Event *FeesRouterLpRewardsExecuted // Event containing the contract specifics and raw log

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
func (it *FeesRouterLpRewardsExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterLpRewardsExecuted)
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
		it.Event = new(FeesRouterLpRewardsExecuted)
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
func (it *FeesRouterLpRewardsExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterLpRewardsExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterLpRewardsExecuted represents a LpRewardsExecuted event raised by the FeesRouter contract.
type FeesRouterLpRewardsExecuted struct {
	NftId  *big.Int
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterLpRewardsExecuted is a free log retrieval operation binding the contract event 0xf831aaeb2aae759a8843e359900e1b8a3fe4cdefa90fc390825acb66a4374408.
//
// Solidity: event LpRewardsExecuted(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) FilterLpRewardsExecuted(opts *bind.FilterOpts, nftId []*big.Int) (*FeesRouterLpRewardsExecutedIterator, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "LpRewardsExecuted", nftIdRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterLpRewardsExecutedIterator{contract: _FeesRouter.contract, event: "LpRewardsExecuted", logs: logs, sub: sub}, nil
}

// WatchLpRewardsExecuted is a free log subscription operation binding the contract event 0xf831aaeb2aae759a8843e359900e1b8a3fe4cdefa90fc390825acb66a4374408.
//
// Solidity: event LpRewardsExecuted(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) WatchLpRewardsExecuted(opts *bind.WatchOpts, sink chan<- *FeesRouterLpRewardsExecuted, nftId []*big.Int) (event.Subscription, error) {

	var nftIdRule []interface{}
	for _, nftIdItem := range nftId {
		nftIdRule = append(nftIdRule, nftIdItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "LpRewardsExecuted", nftIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterLpRewardsExecuted)
				if err := _FeesRouter.contract.UnpackLog(event, "LpRewardsExecuted", log); err != nil {
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

// ParseLpRewardsExecuted is a log parse operation binding the contract event 0xf831aaeb2aae759a8843e359900e1b8a3fe4cdefa90fc390825acb66a4374408.
//
// Solidity: event LpRewardsExecuted(uint256 indexed nftId, uint256 amount)
func (_FeesRouter *FeesRouterFilterer) ParseLpRewardsExecuted(log types.Log) (*FeesRouterLpRewardsExecuted, error) {
	event := new(FeesRouterLpRewardsExecuted)
	if err := _FeesRouter.contract.UnpackLog(event, "LpRewardsExecuted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the FeesRouter contract.
type FeesRouterRoleAdminChangedIterator struct {
	Event *FeesRouterRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *FeesRouterRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterRoleAdminChanged)
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
		it.Event = new(FeesRouterRoleAdminChanged)
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
func (it *FeesRouterRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterRoleAdminChanged represents a RoleAdminChanged event raised by the FeesRouter contract.
type FeesRouterRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_FeesRouter *FeesRouterFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*FeesRouterRoleAdminChangedIterator, error) {

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

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterRoleAdminChangedIterator{contract: _FeesRouter.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_FeesRouter *FeesRouterFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *FeesRouterRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

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

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterRoleAdminChanged)
				if err := _FeesRouter.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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
func (_FeesRouter *FeesRouterFilterer) ParseRoleAdminChanged(log types.Log) (*FeesRouterRoleAdminChanged, error) {
	event := new(FeesRouterRoleAdminChanged)
	if err := _FeesRouter.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the FeesRouter contract.
type FeesRouterRoleGrantedIterator struct {
	Event *FeesRouterRoleGranted // Event containing the contract specifics and raw log

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
func (it *FeesRouterRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterRoleGranted)
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
		it.Event = new(FeesRouterRoleGranted)
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
func (it *FeesRouterRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterRoleGranted represents a RoleGranted event raised by the FeesRouter contract.
type FeesRouterRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeesRouter *FeesRouterFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*FeesRouterRoleGrantedIterator, error) {

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

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterRoleGrantedIterator{contract: _FeesRouter.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeesRouter *FeesRouterFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *FeesRouterRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterRoleGranted)
				if err := _FeesRouter.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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
func (_FeesRouter *FeesRouterFilterer) ParseRoleGranted(log types.Log) (*FeesRouterRoleGranted, error) {
	event := new(FeesRouterRoleGranted)
	if err := _FeesRouter.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the FeesRouter contract.
type FeesRouterRoleRevokedIterator struct {
	Event *FeesRouterRoleRevoked // Event containing the contract specifics and raw log

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
func (it *FeesRouterRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterRoleRevoked)
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
		it.Event = new(FeesRouterRoleRevoked)
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
func (it *FeesRouterRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterRoleRevoked represents a RoleRevoked event raised by the FeesRouter contract.
type FeesRouterRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeesRouter *FeesRouterFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*FeesRouterRoleRevokedIterator, error) {

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

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterRoleRevokedIterator{contract: _FeesRouter.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeesRouter *FeesRouterFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *FeesRouterRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterRoleRevoked)
				if err := _FeesRouter.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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
func (_FeesRouter *FeesRouterFilterer) ParseRoleRevoked(log types.Log) (*FeesRouterRoleRevoked, error) {
	event := new(FeesRouterRoleRevoked)
	if err := _FeesRouter.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeesRouterUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the FeesRouter contract.
type FeesRouterUpgradedIterator struct {
	Event *FeesRouterUpgraded // Event containing the contract specifics and raw log

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
func (it *FeesRouterUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeesRouterUpgraded)
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
		it.Event = new(FeesRouterUpgraded)
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
func (it *FeesRouterUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeesRouterUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeesRouterUpgraded represents a Upgraded event raised by the FeesRouter contract.
type FeesRouterUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FeesRouter *FeesRouterFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*FeesRouterUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _FeesRouter.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &FeesRouterUpgradedIterator{contract: _FeesRouter.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FeesRouter *FeesRouterFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *FeesRouterUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _FeesRouter.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeesRouterUpgraded)
				if err := _FeesRouter.contract.UnpackLog(event, "Upgraded", log); err != nil {
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
func (_FeesRouter *FeesRouterFilterer) ParseUpgraded(log types.Log) (*FeesRouterUpgraded, error) {
	event := new(FeesRouterUpgraded)
	if err := _FeesRouter.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
