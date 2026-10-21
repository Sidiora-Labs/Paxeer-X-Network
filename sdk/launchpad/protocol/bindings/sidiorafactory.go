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

// SidioraFactoryMetaData contains all meta data concerning the SidioraFactory contract.
var SidioraFactoryMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"DuplicateToken\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InsufficientCreationFee\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"MissingRole\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TransferFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TransferFromFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughActiveProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnauthorizedUpgrade\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddress\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"}],\"name\":\"MarketCreated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"ROUTER_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"string\",\"name\":\"name\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"symbol\",\"type\":\"string\"},{\"internalType\":\"uint8\",\"name\":\"feeStrategy\",\"type\":\"uint8\"},{\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"}],\"name\":\"createMarket\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"tokenAddr\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"poolAddr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"internalType\":\"string\",\"name\":\"name\",\"type\":\"string\"},{\"internalType\":\"string\",\"name\":\"symbol\",\"type\":\"string\"},{\"internalType\":\"uint8\",\"name\":\"feeStrategy\",\"type\":\"uint8\"},{\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"}],\"name\":\"createMarketFor\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"tokenAddr\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"poolAddr\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"eventEmitter\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"feeAccumulator\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"}],\"name\":\"getNonce\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_poolBeacon\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_nftContract\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_poolRegistry\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_eventEmitter\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_protocolConfig\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_treasury\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_feeAccumulator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_usdlAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_admin\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"nftContract\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"poolBeacon\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"poolRegistry\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"protocolConfig\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"callerConfirmation\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"treasury\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"usdlAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// SidioraFactoryABI is the input ABI used to generate the binding from.
// Deprecated: Use SidioraFactoryMetaData.ABI instead.
var SidioraFactoryABI = SidioraFactoryMetaData.ABI

// SidioraFactory is an auto generated Go binding around an Ethereum contract.
type SidioraFactory struct {
	SidioraFactoryCaller     // Read-only binding to the contract
	SidioraFactoryTransactor // Write-only binding to the contract
	SidioraFactoryFilterer   // Log filterer for contract events
}

// SidioraFactoryCaller is an auto generated read-only Go binding around an Ethereum contract.
type SidioraFactoryCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SidioraFactoryTransactor is an auto generated write-only Go binding around an Ethereum contract.
type SidioraFactoryTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SidioraFactoryFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type SidioraFactoryFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// SidioraFactorySession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type SidioraFactorySession struct {
	Contract     *SidioraFactory   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// SidioraFactoryCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type SidioraFactoryCallerSession struct {
	Contract *SidioraFactoryCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// SidioraFactoryTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type SidioraFactoryTransactorSession struct {
	Contract     *SidioraFactoryTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// SidioraFactoryRaw is an auto generated low-level Go binding around an Ethereum contract.
type SidioraFactoryRaw struct {
	Contract *SidioraFactory // Generic contract binding to access the raw methods on
}

// SidioraFactoryCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type SidioraFactoryCallerRaw struct {
	Contract *SidioraFactoryCaller // Generic read-only contract binding to access the raw methods on
}

// SidioraFactoryTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type SidioraFactoryTransactorRaw struct {
	Contract *SidioraFactoryTransactor // Generic write-only contract binding to access the raw methods on
}

// NewSidioraFactory creates a new instance of SidioraFactory, bound to a specific deployed contract.
func NewSidioraFactory(address common.Address, backend bind.ContractBackend) (*SidioraFactory, error) {
	contract, err := bindSidioraFactory(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &SidioraFactory{SidioraFactoryCaller: SidioraFactoryCaller{contract: contract}, SidioraFactoryTransactor: SidioraFactoryTransactor{contract: contract}, SidioraFactoryFilterer: SidioraFactoryFilterer{contract: contract}}, nil
}

// NewSidioraFactoryCaller creates a new read-only instance of SidioraFactory, bound to a specific deployed contract.
func NewSidioraFactoryCaller(address common.Address, caller bind.ContractCaller) (*SidioraFactoryCaller, error) {
	contract, err := bindSidioraFactory(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryCaller{contract: contract}, nil
}

// NewSidioraFactoryTransactor creates a new write-only instance of SidioraFactory, bound to a specific deployed contract.
func NewSidioraFactoryTransactor(address common.Address, transactor bind.ContractTransactor) (*SidioraFactoryTransactor, error) {
	contract, err := bindSidioraFactory(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryTransactor{contract: contract}, nil
}

// NewSidioraFactoryFilterer creates a new log filterer instance of SidioraFactory, bound to a specific deployed contract.
func NewSidioraFactoryFilterer(address common.Address, filterer bind.ContractFilterer) (*SidioraFactoryFilterer, error) {
	contract, err := bindSidioraFactory(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryFilterer{contract: contract}, nil
}

// bindSidioraFactory binds a generic wrapper to an already deployed contract.
func bindSidioraFactory(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := SidioraFactoryMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SidioraFactory *SidioraFactoryRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SidioraFactory.Contract.SidioraFactoryCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SidioraFactory *SidioraFactoryRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SidioraFactory.Contract.SidioraFactoryTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SidioraFactory *SidioraFactoryRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SidioraFactory.Contract.SidioraFactoryTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_SidioraFactory *SidioraFactoryCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _SidioraFactory.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_SidioraFactory *SidioraFactoryTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _SidioraFactory.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_SidioraFactory *SidioraFactoryTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _SidioraFactory.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_SidioraFactory *SidioraFactorySession) DEFAULTADMINROLE() ([32]byte, error) {
	return _SidioraFactory.Contract.DEFAULTADMINROLE(&_SidioraFactory.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _SidioraFactory.Contract.DEFAULTADMINROLE(&_SidioraFactory.CallOpts)
}

// ROUTERROLE is a free data retrieval call binding the contract method 0x30d643b5.
//
// Solidity: function ROUTER_ROLE() view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCaller) ROUTERROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "ROUTER_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ROUTERROLE is a free data retrieval call binding the contract method 0x30d643b5.
//
// Solidity: function ROUTER_ROLE() view returns(bytes32)
func (_SidioraFactory *SidioraFactorySession) ROUTERROLE() ([32]byte, error) {
	return _SidioraFactory.Contract.ROUTERROLE(&_SidioraFactory.CallOpts)
}

// ROUTERROLE is a free data retrieval call binding the contract method 0x30d643b5.
//
// Solidity: function ROUTER_ROLE() view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCallerSession) ROUTERROLE() ([32]byte, error) {
	return _SidioraFactory.Contract.ROUTERROLE(&_SidioraFactory.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) EventEmitter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "eventEmitter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_SidioraFactory *SidioraFactorySession) EventEmitter() (common.Address, error) {
	return _SidioraFactory.Contract.EventEmitter(&_SidioraFactory.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) EventEmitter() (common.Address, error) {
	return _SidioraFactory.Contract.EventEmitter(&_SidioraFactory.CallOpts)
}

// FeeAccumulator is a free data retrieval call binding the contract method 0x5eaea35f.
//
// Solidity: function feeAccumulator() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) FeeAccumulator(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "feeAccumulator")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// FeeAccumulator is a free data retrieval call binding the contract method 0x5eaea35f.
//
// Solidity: function feeAccumulator() view returns(address)
func (_SidioraFactory *SidioraFactorySession) FeeAccumulator() (common.Address, error) {
	return _SidioraFactory.Contract.FeeAccumulator(&_SidioraFactory.CallOpts)
}

// FeeAccumulator is a free data retrieval call binding the contract method 0x5eaea35f.
//
// Solidity: function feeAccumulator() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) FeeAccumulator() (common.Address, error) {
	return _SidioraFactory.Contract.FeeAccumulator(&_SidioraFactory.CallOpts)
}

// GetNonce is a free data retrieval call binding the contract method 0x2d0335ab.
//
// Solidity: function getNonce(address creator) view returns(uint256)
func (_SidioraFactory *SidioraFactoryCaller) GetNonce(opts *bind.CallOpts, creator common.Address) (*big.Int, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "getNonce", creator)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetNonce is a free data retrieval call binding the contract method 0x2d0335ab.
//
// Solidity: function getNonce(address creator) view returns(uint256)
func (_SidioraFactory *SidioraFactorySession) GetNonce(creator common.Address) (*big.Int, error) {
	return _SidioraFactory.Contract.GetNonce(&_SidioraFactory.CallOpts, creator)
}

// GetNonce is a free data retrieval call binding the contract method 0x2d0335ab.
//
// Solidity: function getNonce(address creator) view returns(uint256)
func (_SidioraFactory *SidioraFactoryCallerSession) GetNonce(creator common.Address) (*big.Int, error) {
	return _SidioraFactory.Contract.GetNonce(&_SidioraFactory.CallOpts, creator)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_SidioraFactory *SidioraFactorySession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _SidioraFactory.Contract.GetRoleAdmin(&_SidioraFactory.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _SidioraFactory.Contract.GetRoleAdmin(&_SidioraFactory.CallOpts, role)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_SidioraFactory *SidioraFactoryCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_SidioraFactory *SidioraFactorySession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _SidioraFactory.Contract.HasRole(&_SidioraFactory.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_SidioraFactory *SidioraFactoryCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _SidioraFactory.Contract.HasRole(&_SidioraFactory.CallOpts, role, account)
}

// NftContract is a free data retrieval call binding the contract method 0xd56d229d.
//
// Solidity: function nftContract() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) NftContract(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "nftContract")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// NftContract is a free data retrieval call binding the contract method 0xd56d229d.
//
// Solidity: function nftContract() view returns(address)
func (_SidioraFactory *SidioraFactorySession) NftContract() (common.Address, error) {
	return _SidioraFactory.Contract.NftContract(&_SidioraFactory.CallOpts)
}

// NftContract is a free data retrieval call binding the contract method 0xd56d229d.
//
// Solidity: function nftContract() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) NftContract() (common.Address, error) {
	return _SidioraFactory.Contract.NftContract(&_SidioraFactory.CallOpts)
}

// PoolBeacon is a free data retrieval call binding the contract method 0xc5a7b2ea.
//
// Solidity: function poolBeacon() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) PoolBeacon(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "poolBeacon")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PoolBeacon is a free data retrieval call binding the contract method 0xc5a7b2ea.
//
// Solidity: function poolBeacon() view returns(address)
func (_SidioraFactory *SidioraFactorySession) PoolBeacon() (common.Address, error) {
	return _SidioraFactory.Contract.PoolBeacon(&_SidioraFactory.CallOpts)
}

// PoolBeacon is a free data retrieval call binding the contract method 0xc5a7b2ea.
//
// Solidity: function poolBeacon() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) PoolBeacon() (common.Address, error) {
	return _SidioraFactory.Contract.PoolBeacon(&_SidioraFactory.CallOpts)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) PoolRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "poolRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_SidioraFactory *SidioraFactorySession) PoolRegistry() (common.Address, error) {
	return _SidioraFactory.Contract.PoolRegistry(&_SidioraFactory.CallOpts)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) PoolRegistry() (common.Address, error) {
	return _SidioraFactory.Contract.PoolRegistry(&_SidioraFactory.CallOpts)
}

// ProtocolConfig is a free data retrieval call binding the contract method 0xf5efbb4f.
//
// Solidity: function protocolConfig() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) ProtocolConfig(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "protocolConfig")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ProtocolConfig is a free data retrieval call binding the contract method 0xf5efbb4f.
//
// Solidity: function protocolConfig() view returns(address)
func (_SidioraFactory *SidioraFactorySession) ProtocolConfig() (common.Address, error) {
	return _SidioraFactory.Contract.ProtocolConfig(&_SidioraFactory.CallOpts)
}

// ProtocolConfig is a free data retrieval call binding the contract method 0xf5efbb4f.
//
// Solidity: function protocolConfig() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) ProtocolConfig() (common.Address, error) {
	return _SidioraFactory.Contract.ProtocolConfig(&_SidioraFactory.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_SidioraFactory *SidioraFactorySession) ProxiableUUID() ([32]byte, error) {
	return _SidioraFactory.Contract.ProxiableUUID(&_SidioraFactory.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_SidioraFactory *SidioraFactoryCallerSession) ProxiableUUID() ([32]byte, error) {
	return _SidioraFactory.Contract.ProxiableUUID(&_SidioraFactory.CallOpts)
}

// Treasury is a free data retrieval call binding the contract method 0x61d027b3.
//
// Solidity: function treasury() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) Treasury(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "treasury")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Treasury is a free data retrieval call binding the contract method 0x61d027b3.
//
// Solidity: function treasury() view returns(address)
func (_SidioraFactory *SidioraFactorySession) Treasury() (common.Address, error) {
	return _SidioraFactory.Contract.Treasury(&_SidioraFactory.CallOpts)
}

// Treasury is a free data retrieval call binding the contract method 0x61d027b3.
//
// Solidity: function treasury() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) Treasury() (common.Address, error) {
	return _SidioraFactory.Contract.Treasury(&_SidioraFactory.CallOpts)
}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_SidioraFactory *SidioraFactoryCaller) UsdlAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _SidioraFactory.contract.Call(opts, &out, "usdlAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_SidioraFactory *SidioraFactorySession) UsdlAddress() (common.Address, error) {
	return _SidioraFactory.Contract.UsdlAddress(&_SidioraFactory.CallOpts)
}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_SidioraFactory *SidioraFactoryCallerSession) UsdlAddress() (common.Address, error) {
	return _SidioraFactory.Contract.UsdlAddress(&_SidioraFactory.CallOpts)
}

// CreateMarket is a paid mutator transaction binding the contract method 0x57c0d7fd.
//
// Solidity: function createMarket(string name, string symbol, uint8 feeStrategy, address optical) returns(address tokenAddr, address poolAddr, uint256 nftId)
func (_SidioraFactory *SidioraFactoryTransactor) CreateMarket(opts *bind.TransactOpts, name string, symbol string, feeStrategy uint8, optical common.Address) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "createMarket", name, symbol, feeStrategy, optical)
}

// CreateMarket is a paid mutator transaction binding the contract method 0x57c0d7fd.
//
// Solidity: function createMarket(string name, string symbol, uint8 feeStrategy, address optical) returns(address tokenAddr, address poolAddr, uint256 nftId)
func (_SidioraFactory *SidioraFactorySession) CreateMarket(name string, symbol string, feeStrategy uint8, optical common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.CreateMarket(&_SidioraFactory.TransactOpts, name, symbol, feeStrategy, optical)
}

// CreateMarket is a paid mutator transaction binding the contract method 0x57c0d7fd.
//
// Solidity: function createMarket(string name, string symbol, uint8 feeStrategy, address optical) returns(address tokenAddr, address poolAddr, uint256 nftId)
func (_SidioraFactory *SidioraFactoryTransactorSession) CreateMarket(name string, symbol string, feeStrategy uint8, optical common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.CreateMarket(&_SidioraFactory.TransactOpts, name, symbol, feeStrategy, optical)
}

// CreateMarketFor is a paid mutator transaction binding the contract method 0x5db85915.
//
// Solidity: function createMarketFor(address creator, string name, string symbol, uint8 feeStrategy, address optical) returns(address tokenAddr, address poolAddr, uint256 nftId)
func (_SidioraFactory *SidioraFactoryTransactor) CreateMarketFor(opts *bind.TransactOpts, creator common.Address, name string, symbol string, feeStrategy uint8, optical common.Address) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "createMarketFor", creator, name, symbol, feeStrategy, optical)
}

// CreateMarketFor is a paid mutator transaction binding the contract method 0x5db85915.
//
// Solidity: function createMarketFor(address creator, string name, string symbol, uint8 feeStrategy, address optical) returns(address tokenAddr, address poolAddr, uint256 nftId)
func (_SidioraFactory *SidioraFactorySession) CreateMarketFor(creator common.Address, name string, symbol string, feeStrategy uint8, optical common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.CreateMarketFor(&_SidioraFactory.TransactOpts, creator, name, symbol, feeStrategy, optical)
}

// CreateMarketFor is a paid mutator transaction binding the contract method 0x5db85915.
//
// Solidity: function createMarketFor(address creator, string name, string symbol, uint8 feeStrategy, address optical) returns(address tokenAddr, address poolAddr, uint256 nftId)
func (_SidioraFactory *SidioraFactoryTransactorSession) CreateMarketFor(creator common.Address, name string, symbol string, feeStrategy uint8, optical common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.CreateMarketFor(&_SidioraFactory.TransactOpts, creator, name, symbol, feeStrategy, optical)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_SidioraFactory *SidioraFactoryTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_SidioraFactory *SidioraFactorySession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.GrantRole(&_SidioraFactory.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_SidioraFactory *SidioraFactoryTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.GrantRole(&_SidioraFactory.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0xf5e95acb.
//
// Solidity: function initialize(address _poolBeacon, address _nftContract, address _poolRegistry, address _eventEmitter, address _protocolConfig, address _treasury, address _feeAccumulator, address _usdlAddress, address _admin) returns()
func (_SidioraFactory *SidioraFactoryTransactor) Initialize(opts *bind.TransactOpts, _poolBeacon common.Address, _nftContract common.Address, _poolRegistry common.Address, _eventEmitter common.Address, _protocolConfig common.Address, _treasury common.Address, _feeAccumulator common.Address, _usdlAddress common.Address, _admin common.Address) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "initialize", _poolBeacon, _nftContract, _poolRegistry, _eventEmitter, _protocolConfig, _treasury, _feeAccumulator, _usdlAddress, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xf5e95acb.
//
// Solidity: function initialize(address _poolBeacon, address _nftContract, address _poolRegistry, address _eventEmitter, address _protocolConfig, address _treasury, address _feeAccumulator, address _usdlAddress, address _admin) returns()
func (_SidioraFactory *SidioraFactorySession) Initialize(_poolBeacon common.Address, _nftContract common.Address, _poolRegistry common.Address, _eventEmitter common.Address, _protocolConfig common.Address, _treasury common.Address, _feeAccumulator common.Address, _usdlAddress common.Address, _admin common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.Initialize(&_SidioraFactory.TransactOpts, _poolBeacon, _nftContract, _poolRegistry, _eventEmitter, _protocolConfig, _treasury, _feeAccumulator, _usdlAddress, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xf5e95acb.
//
// Solidity: function initialize(address _poolBeacon, address _nftContract, address _poolRegistry, address _eventEmitter, address _protocolConfig, address _treasury, address _feeAccumulator, address _usdlAddress, address _admin) returns()
func (_SidioraFactory *SidioraFactoryTransactorSession) Initialize(_poolBeacon common.Address, _nftContract common.Address, _poolRegistry common.Address, _eventEmitter common.Address, _protocolConfig common.Address, _treasury common.Address, _feeAccumulator common.Address, _usdlAddress common.Address, _admin common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.Initialize(&_SidioraFactory.TransactOpts, _poolBeacon, _nftContract, _poolRegistry, _eventEmitter, _protocolConfig, _treasury, _feeAccumulator, _usdlAddress, _admin)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_SidioraFactory *SidioraFactoryTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "renounceRole", role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_SidioraFactory *SidioraFactorySession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.RenounceRole(&_SidioraFactory.TransactOpts, role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_SidioraFactory *SidioraFactoryTransactorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.RenounceRole(&_SidioraFactory.TransactOpts, role, callerConfirmation)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_SidioraFactory *SidioraFactoryTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_SidioraFactory *SidioraFactorySession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.RevokeRole(&_SidioraFactory.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_SidioraFactory *SidioraFactoryTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _SidioraFactory.Contract.RevokeRole(&_SidioraFactory.TransactOpts, role, account)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_SidioraFactory *SidioraFactoryTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _SidioraFactory.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_SidioraFactory *SidioraFactorySession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _SidioraFactory.Contract.UpgradeToAndCall(&_SidioraFactory.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_SidioraFactory *SidioraFactoryTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _SidioraFactory.Contract.UpgradeToAndCall(&_SidioraFactory.TransactOpts, newImplementation, data)
}

// SidioraFactoryInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the SidioraFactory contract.
type SidioraFactoryInitializedIterator struct {
	Event *SidioraFactoryInitialized // Event containing the contract specifics and raw log

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
func (it *SidioraFactoryInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SidioraFactoryInitialized)
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
		it.Event = new(SidioraFactoryInitialized)
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
func (it *SidioraFactoryInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SidioraFactoryInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SidioraFactoryInitialized represents a Initialized event raised by the SidioraFactory contract.
type SidioraFactoryInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_SidioraFactory *SidioraFactoryFilterer) FilterInitialized(opts *bind.FilterOpts) (*SidioraFactoryInitializedIterator, error) {

	logs, sub, err := _SidioraFactory.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryInitializedIterator{contract: _SidioraFactory.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_SidioraFactory *SidioraFactoryFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *SidioraFactoryInitialized) (event.Subscription, error) {

	logs, sub, err := _SidioraFactory.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SidioraFactoryInitialized)
				if err := _SidioraFactory.contract.UnpackLog(event, "Initialized", log); err != nil {
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
func (_SidioraFactory *SidioraFactoryFilterer) ParseInitialized(log types.Log) (*SidioraFactoryInitialized, error) {
	event := new(SidioraFactoryInitialized)
	if err := _SidioraFactory.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SidioraFactoryMarketCreatedIterator is returned from FilterMarketCreated and is used to iterate over the raw logs and unpacked data for MarketCreated events raised by the SidioraFactory contract.
type SidioraFactoryMarketCreatedIterator struct {
	Event *SidioraFactoryMarketCreated // Event containing the contract specifics and raw log

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
func (it *SidioraFactoryMarketCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SidioraFactoryMarketCreated)
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
		it.Event = new(SidioraFactoryMarketCreated)
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
func (it *SidioraFactoryMarketCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SidioraFactoryMarketCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SidioraFactoryMarketCreated represents a MarketCreated event raised by the SidioraFactory contract.
type SidioraFactoryMarketCreated struct {
	Token   common.Address
	Pool    common.Address
	Creator common.Address
	NftId   *big.Int
	Optical common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterMarketCreated is a free log retrieval operation binding the contract event 0xde3c5cd27ce87044b7bc6c8182270f302e537f55f8c943271a34e5163ef2b284.
//
// Solidity: event MarketCreated(address indexed token, address indexed pool, address indexed creator, uint256 nftId, address optical)
func (_SidioraFactory *SidioraFactoryFilterer) FilterMarketCreated(opts *bind.FilterOpts, token []common.Address, pool []common.Address, creator []common.Address) (*SidioraFactoryMarketCreatedIterator, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var creatorRule []interface{}
	for _, creatorItem := range creator {
		creatorRule = append(creatorRule, creatorItem)
	}

	logs, sub, err := _SidioraFactory.contract.FilterLogs(opts, "MarketCreated", tokenRule, poolRule, creatorRule)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryMarketCreatedIterator{contract: _SidioraFactory.contract, event: "MarketCreated", logs: logs, sub: sub}, nil
}

// WatchMarketCreated is a free log subscription operation binding the contract event 0xde3c5cd27ce87044b7bc6c8182270f302e537f55f8c943271a34e5163ef2b284.
//
// Solidity: event MarketCreated(address indexed token, address indexed pool, address indexed creator, uint256 nftId, address optical)
func (_SidioraFactory *SidioraFactoryFilterer) WatchMarketCreated(opts *bind.WatchOpts, sink chan<- *SidioraFactoryMarketCreated, token []common.Address, pool []common.Address, creator []common.Address) (event.Subscription, error) {

	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var creatorRule []interface{}
	for _, creatorItem := range creator {
		creatorRule = append(creatorRule, creatorItem)
	}

	logs, sub, err := _SidioraFactory.contract.WatchLogs(opts, "MarketCreated", tokenRule, poolRule, creatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SidioraFactoryMarketCreated)
				if err := _SidioraFactory.contract.UnpackLog(event, "MarketCreated", log); err != nil {
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

// ParseMarketCreated is a log parse operation binding the contract event 0xde3c5cd27ce87044b7bc6c8182270f302e537f55f8c943271a34e5163ef2b284.
//
// Solidity: event MarketCreated(address indexed token, address indexed pool, address indexed creator, uint256 nftId, address optical)
func (_SidioraFactory *SidioraFactoryFilterer) ParseMarketCreated(log types.Log) (*SidioraFactoryMarketCreated, error) {
	event := new(SidioraFactoryMarketCreated)
	if err := _SidioraFactory.contract.UnpackLog(event, "MarketCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SidioraFactoryRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the SidioraFactory contract.
type SidioraFactoryRoleAdminChangedIterator struct {
	Event *SidioraFactoryRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *SidioraFactoryRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SidioraFactoryRoleAdminChanged)
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
		it.Event = new(SidioraFactoryRoleAdminChanged)
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
func (it *SidioraFactoryRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SidioraFactoryRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SidioraFactoryRoleAdminChanged represents a RoleAdminChanged event raised by the SidioraFactory contract.
type SidioraFactoryRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_SidioraFactory *SidioraFactoryFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*SidioraFactoryRoleAdminChangedIterator, error) {

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

	logs, sub, err := _SidioraFactory.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryRoleAdminChangedIterator{contract: _SidioraFactory.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_SidioraFactory *SidioraFactoryFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *SidioraFactoryRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

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

	logs, sub, err := _SidioraFactory.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SidioraFactoryRoleAdminChanged)
				if err := _SidioraFactory.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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
func (_SidioraFactory *SidioraFactoryFilterer) ParseRoleAdminChanged(log types.Log) (*SidioraFactoryRoleAdminChanged, error) {
	event := new(SidioraFactoryRoleAdminChanged)
	if err := _SidioraFactory.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SidioraFactoryRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the SidioraFactory contract.
type SidioraFactoryRoleGrantedIterator struct {
	Event *SidioraFactoryRoleGranted // Event containing the contract specifics and raw log

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
func (it *SidioraFactoryRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SidioraFactoryRoleGranted)
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
		it.Event = new(SidioraFactoryRoleGranted)
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
func (it *SidioraFactoryRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SidioraFactoryRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SidioraFactoryRoleGranted represents a RoleGranted event raised by the SidioraFactory contract.
type SidioraFactoryRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_SidioraFactory *SidioraFactoryFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*SidioraFactoryRoleGrantedIterator, error) {

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

	logs, sub, err := _SidioraFactory.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryRoleGrantedIterator{contract: _SidioraFactory.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_SidioraFactory *SidioraFactoryFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *SidioraFactoryRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _SidioraFactory.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SidioraFactoryRoleGranted)
				if err := _SidioraFactory.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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
func (_SidioraFactory *SidioraFactoryFilterer) ParseRoleGranted(log types.Log) (*SidioraFactoryRoleGranted, error) {
	event := new(SidioraFactoryRoleGranted)
	if err := _SidioraFactory.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SidioraFactoryRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the SidioraFactory contract.
type SidioraFactoryRoleRevokedIterator struct {
	Event *SidioraFactoryRoleRevoked // Event containing the contract specifics and raw log

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
func (it *SidioraFactoryRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SidioraFactoryRoleRevoked)
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
		it.Event = new(SidioraFactoryRoleRevoked)
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
func (it *SidioraFactoryRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SidioraFactoryRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SidioraFactoryRoleRevoked represents a RoleRevoked event raised by the SidioraFactory contract.
type SidioraFactoryRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_SidioraFactory *SidioraFactoryFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*SidioraFactoryRoleRevokedIterator, error) {

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

	logs, sub, err := _SidioraFactory.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryRoleRevokedIterator{contract: _SidioraFactory.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_SidioraFactory *SidioraFactoryFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *SidioraFactoryRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _SidioraFactory.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SidioraFactoryRoleRevoked)
				if err := _SidioraFactory.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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
func (_SidioraFactory *SidioraFactoryFilterer) ParseRoleRevoked(log types.Log) (*SidioraFactoryRoleRevoked, error) {
	event := new(SidioraFactoryRoleRevoked)
	if err := _SidioraFactory.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// SidioraFactoryUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the SidioraFactory contract.
type SidioraFactoryUpgradedIterator struct {
	Event *SidioraFactoryUpgraded // Event containing the contract specifics and raw log

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
func (it *SidioraFactoryUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(SidioraFactoryUpgraded)
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
		it.Event = new(SidioraFactoryUpgraded)
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
func (it *SidioraFactoryUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *SidioraFactoryUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// SidioraFactoryUpgraded represents a Upgraded event raised by the SidioraFactory contract.
type SidioraFactoryUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_SidioraFactory *SidioraFactoryFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*SidioraFactoryUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _SidioraFactory.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &SidioraFactoryUpgradedIterator{contract: _SidioraFactory.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_SidioraFactory *SidioraFactoryFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *SidioraFactoryUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _SidioraFactory.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(SidioraFactoryUpgraded)
				if err := _SidioraFactory.contract.UnpackLog(event, "Upgraded", log); err != nil {
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
func (_SidioraFactory *SidioraFactoryFilterer) ParseUpgraded(log types.Log) (*SidioraFactoryUpgraded, error) {
	event := new(SidioraFactoryUpgraded)
	if err := _SidioraFactory.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
