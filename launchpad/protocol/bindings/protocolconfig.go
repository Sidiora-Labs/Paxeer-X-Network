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

// ProtocolConfigMetaData contains all meta data concerning the ProtocolConfig contract.
var ProtocolConfigMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"FeeOutOfRange\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"MissingRole\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughActiveProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnauthorizedUpgrade\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddress\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"key\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldValue\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newValue\",\"type\":\"uint256\"}],\"name\":\"ConfigUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"baseFeeBps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"concentrationWeight\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"creationFee\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"eventEmitter\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"feeDecayRate\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_usdlAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_eventEmitter\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_admin\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"maxFeeBps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"minFeeBps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"protocolFeeBps\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"callerConfirmation\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newBaseFeeBps\",\"type\":\"uint256\"}],\"name\":\"setBaseFeeBps\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newCreationFee\",\"type\":\"uint256\"}],\"name\":\"setCreationFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newDecayRate\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"newVolWeight\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"newConcWeight\",\"type\":\"uint256\"}],\"name\":\"setFeeWeights\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newProtocolFeeBps\",\"type\":\"uint256\"}],\"name\":\"setProtocolFeeBps\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"newVirtualUsdl\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"newVirtualToken\",\"type\":\"uint256\"}],\"name\":\"setVirtualDefaults\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"usdlAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"virtualTokenDefault\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"virtualUsdlDefault\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"volatilityWeight\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// ProtocolConfigABI is the input ABI used to generate the binding from.
// Deprecated: Use ProtocolConfigMetaData.ABI instead.
var ProtocolConfigABI = ProtocolConfigMetaData.ABI

// ProtocolConfig is an auto generated Go binding around an Ethereum contract.
type ProtocolConfig struct {
	ProtocolConfigCaller     // Read-only binding to the contract
	ProtocolConfigTransactor // Write-only binding to the contract
	ProtocolConfigFilterer   // Log filterer for contract events
}

// ProtocolConfigCaller is an auto generated read-only Go binding around an Ethereum contract.
type ProtocolConfigCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ProtocolConfigTransactor is an auto generated write-only Go binding around an Ethereum contract.
type ProtocolConfigTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ProtocolConfigFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type ProtocolConfigFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// ProtocolConfigSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type ProtocolConfigSession struct {
	Contract     *ProtocolConfig   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// ProtocolConfigCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type ProtocolConfigCallerSession struct {
	Contract *ProtocolConfigCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// ProtocolConfigTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type ProtocolConfigTransactorSession struct {
	Contract     *ProtocolConfigTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// ProtocolConfigRaw is an auto generated low-level Go binding around an Ethereum contract.
type ProtocolConfigRaw struct {
	Contract *ProtocolConfig // Generic contract binding to access the raw methods on
}

// ProtocolConfigCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type ProtocolConfigCallerRaw struct {
	Contract *ProtocolConfigCaller // Generic read-only contract binding to access the raw methods on
}

// ProtocolConfigTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type ProtocolConfigTransactorRaw struct {
	Contract *ProtocolConfigTransactor // Generic write-only contract binding to access the raw methods on
}

// NewProtocolConfig creates a new instance of ProtocolConfig, bound to a specific deployed contract.
func NewProtocolConfig(address common.Address, backend bind.ContractBackend) (*ProtocolConfig, error) {
	contract, err := bindProtocolConfig(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfig{ProtocolConfigCaller: ProtocolConfigCaller{contract: contract}, ProtocolConfigTransactor: ProtocolConfigTransactor{contract: contract}, ProtocolConfigFilterer: ProtocolConfigFilterer{contract: contract}}, nil
}

// NewProtocolConfigCaller creates a new read-only instance of ProtocolConfig, bound to a specific deployed contract.
func NewProtocolConfigCaller(address common.Address, caller bind.ContractCaller) (*ProtocolConfigCaller, error) {
	contract, err := bindProtocolConfig(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigCaller{contract: contract}, nil
}

// NewProtocolConfigTransactor creates a new write-only instance of ProtocolConfig, bound to a specific deployed contract.
func NewProtocolConfigTransactor(address common.Address, transactor bind.ContractTransactor) (*ProtocolConfigTransactor, error) {
	contract, err := bindProtocolConfig(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigTransactor{contract: contract}, nil
}

// NewProtocolConfigFilterer creates a new log filterer instance of ProtocolConfig, bound to a specific deployed contract.
func NewProtocolConfigFilterer(address common.Address, filterer bind.ContractFilterer) (*ProtocolConfigFilterer, error) {
	contract, err := bindProtocolConfig(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigFilterer{contract: contract}, nil
}

// bindProtocolConfig binds a generic wrapper to an already deployed contract.
func bindProtocolConfig(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := ProtocolConfigMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ProtocolConfig *ProtocolConfigRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ProtocolConfig.Contract.ProtocolConfigCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ProtocolConfig *ProtocolConfigRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.ProtocolConfigTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ProtocolConfig *ProtocolConfigRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.ProtocolConfigTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_ProtocolConfig *ProtocolConfigCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _ProtocolConfig.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_ProtocolConfig *ProtocolConfigTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_ProtocolConfig *ProtocolConfigTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _ProtocolConfig.Contract.DEFAULTADMINROLE(&_ProtocolConfig.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _ProtocolConfig.Contract.DEFAULTADMINROLE(&_ProtocolConfig.CallOpts)
}

// BaseFeeBps is a free data retrieval call binding the contract method 0xbf5a5940.
//
// Solidity: function baseFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) BaseFeeBps(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "baseFeeBps")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// BaseFeeBps is a free data retrieval call binding the contract method 0xbf5a5940.
//
// Solidity: function baseFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) BaseFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.BaseFeeBps(&_ProtocolConfig.CallOpts)
}

// BaseFeeBps is a free data retrieval call binding the contract method 0xbf5a5940.
//
// Solidity: function baseFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) BaseFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.BaseFeeBps(&_ProtocolConfig.CallOpts)
}

// ConcentrationWeight is a free data retrieval call binding the contract method 0x2006e14c.
//
// Solidity: function concentrationWeight() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) ConcentrationWeight(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "concentrationWeight")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ConcentrationWeight is a free data retrieval call binding the contract method 0x2006e14c.
//
// Solidity: function concentrationWeight() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) ConcentrationWeight() (*big.Int, error) {
	return _ProtocolConfig.Contract.ConcentrationWeight(&_ProtocolConfig.CallOpts)
}

// ConcentrationWeight is a free data retrieval call binding the contract method 0x2006e14c.
//
// Solidity: function concentrationWeight() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) ConcentrationWeight() (*big.Int, error) {
	return _ProtocolConfig.Contract.ConcentrationWeight(&_ProtocolConfig.CallOpts)
}

// CreationFee is a free data retrieval call binding the contract method 0xdce0b4e4.
//
// Solidity: function creationFee() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) CreationFee(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "creationFee")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// CreationFee is a free data retrieval call binding the contract method 0xdce0b4e4.
//
// Solidity: function creationFee() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) CreationFee() (*big.Int, error) {
	return _ProtocolConfig.Contract.CreationFee(&_ProtocolConfig.CallOpts)
}

// CreationFee is a free data retrieval call binding the contract method 0xdce0b4e4.
//
// Solidity: function creationFee() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) CreationFee() (*big.Int, error) {
	return _ProtocolConfig.Contract.CreationFee(&_ProtocolConfig.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_ProtocolConfig *ProtocolConfigCaller) EventEmitter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "eventEmitter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_ProtocolConfig *ProtocolConfigSession) EventEmitter() (common.Address, error) {
	return _ProtocolConfig.Contract.EventEmitter(&_ProtocolConfig.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_ProtocolConfig *ProtocolConfigCallerSession) EventEmitter() (common.Address, error) {
	return _ProtocolConfig.Contract.EventEmitter(&_ProtocolConfig.CallOpts)
}

// FeeDecayRate is a free data retrieval call binding the contract method 0x155d94c4.
//
// Solidity: function feeDecayRate() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) FeeDecayRate(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "feeDecayRate")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FeeDecayRate is a free data retrieval call binding the contract method 0x155d94c4.
//
// Solidity: function feeDecayRate() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) FeeDecayRate() (*big.Int, error) {
	return _ProtocolConfig.Contract.FeeDecayRate(&_ProtocolConfig.CallOpts)
}

// FeeDecayRate is a free data retrieval call binding the contract method 0x155d94c4.
//
// Solidity: function feeDecayRate() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) FeeDecayRate() (*big.Int, error) {
	return _ProtocolConfig.Contract.FeeDecayRate(&_ProtocolConfig.CallOpts)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _ProtocolConfig.Contract.GetRoleAdmin(&_ProtocolConfig.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _ProtocolConfig.Contract.GetRoleAdmin(&_ProtocolConfig.CallOpts, role)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_ProtocolConfig *ProtocolConfigCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_ProtocolConfig *ProtocolConfigSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _ProtocolConfig.Contract.HasRole(&_ProtocolConfig.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_ProtocolConfig *ProtocolConfigCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _ProtocolConfig.Contract.HasRole(&_ProtocolConfig.CallOpts, role, account)
}

// MaxFeeBps is a free data retrieval call binding the contract method 0xbf769a3f.
//
// Solidity: function maxFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) MaxFeeBps(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "maxFeeBps")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MaxFeeBps is a free data retrieval call binding the contract method 0xbf769a3f.
//
// Solidity: function maxFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) MaxFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.MaxFeeBps(&_ProtocolConfig.CallOpts)
}

// MaxFeeBps is a free data retrieval call binding the contract method 0xbf769a3f.
//
// Solidity: function maxFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) MaxFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.MaxFeeBps(&_ProtocolConfig.CallOpts)
}

// MinFeeBps is a free data retrieval call binding the contract method 0x071de20d.
//
// Solidity: function minFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) MinFeeBps(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "minFeeBps")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MinFeeBps is a free data retrieval call binding the contract method 0x071de20d.
//
// Solidity: function minFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) MinFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.MinFeeBps(&_ProtocolConfig.CallOpts)
}

// MinFeeBps is a free data retrieval call binding the contract method 0x071de20d.
//
// Solidity: function minFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) MinFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.MinFeeBps(&_ProtocolConfig.CallOpts)
}

// ProtocolFeeBps is a free data retrieval call binding the contract method 0x35659fb8.
//
// Solidity: function protocolFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) ProtocolFeeBps(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "protocolFeeBps")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProtocolFeeBps is a free data retrieval call binding the contract method 0x35659fb8.
//
// Solidity: function protocolFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) ProtocolFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.ProtocolFeeBps(&_ProtocolConfig.CallOpts)
}

// ProtocolFeeBps is a free data retrieval call binding the contract method 0x35659fb8.
//
// Solidity: function protocolFeeBps() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) ProtocolFeeBps() (*big.Int, error) {
	return _ProtocolConfig.Contract.ProtocolFeeBps(&_ProtocolConfig.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigSession) ProxiableUUID() ([32]byte, error) {
	return _ProtocolConfig.Contract.ProxiableUUID(&_ProtocolConfig.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_ProtocolConfig *ProtocolConfigCallerSession) ProxiableUUID() ([32]byte, error) {
	return _ProtocolConfig.Contract.ProxiableUUID(&_ProtocolConfig.CallOpts)
}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_ProtocolConfig *ProtocolConfigCaller) UsdlAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "usdlAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_ProtocolConfig *ProtocolConfigSession) UsdlAddress() (common.Address, error) {
	return _ProtocolConfig.Contract.UsdlAddress(&_ProtocolConfig.CallOpts)
}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_ProtocolConfig *ProtocolConfigCallerSession) UsdlAddress() (common.Address, error) {
	return _ProtocolConfig.Contract.UsdlAddress(&_ProtocolConfig.CallOpts)
}

// VirtualTokenDefault is a free data retrieval call binding the contract method 0xd9fce324.
//
// Solidity: function virtualTokenDefault() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) VirtualTokenDefault(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "virtualTokenDefault")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// VirtualTokenDefault is a free data retrieval call binding the contract method 0xd9fce324.
//
// Solidity: function virtualTokenDefault() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) VirtualTokenDefault() (*big.Int, error) {
	return _ProtocolConfig.Contract.VirtualTokenDefault(&_ProtocolConfig.CallOpts)
}

// VirtualTokenDefault is a free data retrieval call binding the contract method 0xd9fce324.
//
// Solidity: function virtualTokenDefault() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) VirtualTokenDefault() (*big.Int, error) {
	return _ProtocolConfig.Contract.VirtualTokenDefault(&_ProtocolConfig.CallOpts)
}

// VirtualUsdlDefault is a free data retrieval call binding the contract method 0x071ed6b5.
//
// Solidity: function virtualUsdlDefault() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) VirtualUsdlDefault(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "virtualUsdlDefault")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// VirtualUsdlDefault is a free data retrieval call binding the contract method 0x071ed6b5.
//
// Solidity: function virtualUsdlDefault() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) VirtualUsdlDefault() (*big.Int, error) {
	return _ProtocolConfig.Contract.VirtualUsdlDefault(&_ProtocolConfig.CallOpts)
}

// VirtualUsdlDefault is a free data retrieval call binding the contract method 0x071ed6b5.
//
// Solidity: function virtualUsdlDefault() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) VirtualUsdlDefault() (*big.Int, error) {
	return _ProtocolConfig.Contract.VirtualUsdlDefault(&_ProtocolConfig.CallOpts)
}

// VolatilityWeight is a free data retrieval call binding the contract method 0x294ae7f9.
//
// Solidity: function volatilityWeight() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCaller) VolatilityWeight(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _ProtocolConfig.contract.Call(opts, &out, "volatilityWeight")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// VolatilityWeight is a free data retrieval call binding the contract method 0x294ae7f9.
//
// Solidity: function volatilityWeight() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigSession) VolatilityWeight() (*big.Int, error) {
	return _ProtocolConfig.Contract.VolatilityWeight(&_ProtocolConfig.CallOpts)
}

// VolatilityWeight is a free data retrieval call binding the contract method 0x294ae7f9.
//
// Solidity: function volatilityWeight() view returns(uint256)
func (_ProtocolConfig *ProtocolConfigCallerSession) VolatilityWeight() (*big.Int, error) {
	return _ProtocolConfig.Contract.VolatilityWeight(&_ProtocolConfig.CallOpts)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_ProtocolConfig *ProtocolConfigSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.GrantRole(&_ProtocolConfig.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.GrantRole(&_ProtocolConfig.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0xc0c53b8b.
//
// Solidity: function initialize(address _usdlAddress, address _eventEmitter, address _admin) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) Initialize(opts *bind.TransactOpts, _usdlAddress common.Address, _eventEmitter common.Address, _admin common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "initialize", _usdlAddress, _eventEmitter, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xc0c53b8b.
//
// Solidity: function initialize(address _usdlAddress, address _eventEmitter, address _admin) returns()
func (_ProtocolConfig *ProtocolConfigSession) Initialize(_usdlAddress common.Address, _eventEmitter common.Address, _admin common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.Initialize(&_ProtocolConfig.TransactOpts, _usdlAddress, _eventEmitter, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xc0c53b8b.
//
// Solidity: function initialize(address _usdlAddress, address _eventEmitter, address _admin) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) Initialize(_usdlAddress common.Address, _eventEmitter common.Address, _admin common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.Initialize(&_ProtocolConfig.TransactOpts, _usdlAddress, _eventEmitter, _admin)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "renounceRole", role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_ProtocolConfig *ProtocolConfigSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.RenounceRole(&_ProtocolConfig.TransactOpts, role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.RenounceRole(&_ProtocolConfig.TransactOpts, role, callerConfirmation)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_ProtocolConfig *ProtocolConfigSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.RevokeRole(&_ProtocolConfig.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.RevokeRole(&_ProtocolConfig.TransactOpts, role, account)
}

// SetBaseFeeBps is a paid mutator transaction binding the contract method 0xa389301a.
//
// Solidity: function setBaseFeeBps(uint256 newBaseFeeBps) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) SetBaseFeeBps(opts *bind.TransactOpts, newBaseFeeBps *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "setBaseFeeBps", newBaseFeeBps)
}

// SetBaseFeeBps is a paid mutator transaction binding the contract method 0xa389301a.
//
// Solidity: function setBaseFeeBps(uint256 newBaseFeeBps) returns()
func (_ProtocolConfig *ProtocolConfigSession) SetBaseFeeBps(newBaseFeeBps *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetBaseFeeBps(&_ProtocolConfig.TransactOpts, newBaseFeeBps)
}

// SetBaseFeeBps is a paid mutator transaction binding the contract method 0xa389301a.
//
// Solidity: function setBaseFeeBps(uint256 newBaseFeeBps) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) SetBaseFeeBps(newBaseFeeBps *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetBaseFeeBps(&_ProtocolConfig.TransactOpts, newBaseFeeBps)
}

// SetCreationFee is a paid mutator transaction binding the contract method 0xb7d86225.
//
// Solidity: function setCreationFee(uint256 newCreationFee) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) SetCreationFee(opts *bind.TransactOpts, newCreationFee *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "setCreationFee", newCreationFee)
}

// SetCreationFee is a paid mutator transaction binding the contract method 0xb7d86225.
//
// Solidity: function setCreationFee(uint256 newCreationFee) returns()
func (_ProtocolConfig *ProtocolConfigSession) SetCreationFee(newCreationFee *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetCreationFee(&_ProtocolConfig.TransactOpts, newCreationFee)
}

// SetCreationFee is a paid mutator transaction binding the contract method 0xb7d86225.
//
// Solidity: function setCreationFee(uint256 newCreationFee) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) SetCreationFee(newCreationFee *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetCreationFee(&_ProtocolConfig.TransactOpts, newCreationFee)
}

// SetFeeWeights is a paid mutator transaction binding the contract method 0xf1726fe7.
//
// Solidity: function setFeeWeights(uint256 newDecayRate, uint256 newVolWeight, uint256 newConcWeight) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) SetFeeWeights(opts *bind.TransactOpts, newDecayRate *big.Int, newVolWeight *big.Int, newConcWeight *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "setFeeWeights", newDecayRate, newVolWeight, newConcWeight)
}

// SetFeeWeights is a paid mutator transaction binding the contract method 0xf1726fe7.
//
// Solidity: function setFeeWeights(uint256 newDecayRate, uint256 newVolWeight, uint256 newConcWeight) returns()
func (_ProtocolConfig *ProtocolConfigSession) SetFeeWeights(newDecayRate *big.Int, newVolWeight *big.Int, newConcWeight *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetFeeWeights(&_ProtocolConfig.TransactOpts, newDecayRate, newVolWeight, newConcWeight)
}

// SetFeeWeights is a paid mutator transaction binding the contract method 0xf1726fe7.
//
// Solidity: function setFeeWeights(uint256 newDecayRate, uint256 newVolWeight, uint256 newConcWeight) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) SetFeeWeights(newDecayRate *big.Int, newVolWeight *big.Int, newConcWeight *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetFeeWeights(&_ProtocolConfig.TransactOpts, newDecayRate, newVolWeight, newConcWeight)
}

// SetProtocolFeeBps is a paid mutator transaction binding the contract method 0xc0417e58.
//
// Solidity: function setProtocolFeeBps(uint256 newProtocolFeeBps) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) SetProtocolFeeBps(opts *bind.TransactOpts, newProtocolFeeBps *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "setProtocolFeeBps", newProtocolFeeBps)
}

// SetProtocolFeeBps is a paid mutator transaction binding the contract method 0xc0417e58.
//
// Solidity: function setProtocolFeeBps(uint256 newProtocolFeeBps) returns()
func (_ProtocolConfig *ProtocolConfigSession) SetProtocolFeeBps(newProtocolFeeBps *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetProtocolFeeBps(&_ProtocolConfig.TransactOpts, newProtocolFeeBps)
}

// SetProtocolFeeBps is a paid mutator transaction binding the contract method 0xc0417e58.
//
// Solidity: function setProtocolFeeBps(uint256 newProtocolFeeBps) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) SetProtocolFeeBps(newProtocolFeeBps *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetProtocolFeeBps(&_ProtocolConfig.TransactOpts, newProtocolFeeBps)
}

// SetVirtualDefaults is a paid mutator transaction binding the contract method 0xa83f1b4a.
//
// Solidity: function setVirtualDefaults(uint256 newVirtualUsdl, uint256 newVirtualToken) returns()
func (_ProtocolConfig *ProtocolConfigTransactor) SetVirtualDefaults(opts *bind.TransactOpts, newVirtualUsdl *big.Int, newVirtualToken *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "setVirtualDefaults", newVirtualUsdl, newVirtualToken)
}

// SetVirtualDefaults is a paid mutator transaction binding the contract method 0xa83f1b4a.
//
// Solidity: function setVirtualDefaults(uint256 newVirtualUsdl, uint256 newVirtualToken) returns()
func (_ProtocolConfig *ProtocolConfigSession) SetVirtualDefaults(newVirtualUsdl *big.Int, newVirtualToken *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetVirtualDefaults(&_ProtocolConfig.TransactOpts, newVirtualUsdl, newVirtualToken)
}

// SetVirtualDefaults is a paid mutator transaction binding the contract method 0xa83f1b4a.
//
// Solidity: function setVirtualDefaults(uint256 newVirtualUsdl, uint256 newVirtualToken) returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) SetVirtualDefaults(newVirtualUsdl *big.Int, newVirtualToken *big.Int) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.SetVirtualDefaults(&_ProtocolConfig.TransactOpts, newVirtualUsdl, newVirtualToken)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_ProtocolConfig *ProtocolConfigTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _ProtocolConfig.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_ProtocolConfig *ProtocolConfigSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.UpgradeToAndCall(&_ProtocolConfig.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_ProtocolConfig *ProtocolConfigTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _ProtocolConfig.Contract.UpgradeToAndCall(&_ProtocolConfig.TransactOpts, newImplementation, data)
}

// ProtocolConfigConfigUpdatedIterator is returned from FilterConfigUpdated and is used to iterate over the raw logs and unpacked data for ConfigUpdated events raised by the ProtocolConfig contract.
type ProtocolConfigConfigUpdatedIterator struct {
	Event *ProtocolConfigConfigUpdated // Event containing the contract specifics and raw log

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
func (it *ProtocolConfigConfigUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ProtocolConfigConfigUpdated)
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
		it.Event = new(ProtocolConfigConfigUpdated)
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
func (it *ProtocolConfigConfigUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ProtocolConfigConfigUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ProtocolConfigConfigUpdated represents a ConfigUpdated event raised by the ProtocolConfig contract.
type ProtocolConfigConfigUpdated struct {
	Key      [32]byte
	OldValue *big.Int
	NewValue *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterConfigUpdated is a free log retrieval operation binding the contract event 0xac2ccce3de9c0816ae772598f7f65fe69f9893b637f7c490497378cbb3ea043e.
//
// Solidity: event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue)
func (_ProtocolConfig *ProtocolConfigFilterer) FilterConfigUpdated(opts *bind.FilterOpts, key [][32]byte) (*ProtocolConfigConfigUpdatedIterator, error) {

	var keyRule []interface{}
	for _, keyItem := range key {
		keyRule = append(keyRule, keyItem)
	}

	logs, sub, err := _ProtocolConfig.contract.FilterLogs(opts, "ConfigUpdated", keyRule)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigConfigUpdatedIterator{contract: _ProtocolConfig.contract, event: "ConfigUpdated", logs: logs, sub: sub}, nil
}

// WatchConfigUpdated is a free log subscription operation binding the contract event 0xac2ccce3de9c0816ae772598f7f65fe69f9893b637f7c490497378cbb3ea043e.
//
// Solidity: event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue)
func (_ProtocolConfig *ProtocolConfigFilterer) WatchConfigUpdated(opts *bind.WatchOpts, sink chan<- *ProtocolConfigConfigUpdated, key [][32]byte) (event.Subscription, error) {

	var keyRule []interface{}
	for _, keyItem := range key {
		keyRule = append(keyRule, keyItem)
	}

	logs, sub, err := _ProtocolConfig.contract.WatchLogs(opts, "ConfigUpdated", keyRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ProtocolConfigConfigUpdated)
				if err := _ProtocolConfig.contract.UnpackLog(event, "ConfigUpdated", log); err != nil {
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

// ParseConfigUpdated is a log parse operation binding the contract event 0xac2ccce3de9c0816ae772598f7f65fe69f9893b637f7c490497378cbb3ea043e.
//
// Solidity: event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue)
func (_ProtocolConfig *ProtocolConfigFilterer) ParseConfigUpdated(log types.Log) (*ProtocolConfigConfigUpdated, error) {
	event := new(ProtocolConfigConfigUpdated)
	if err := _ProtocolConfig.contract.UnpackLog(event, "ConfigUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ProtocolConfigInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the ProtocolConfig contract.
type ProtocolConfigInitializedIterator struct {
	Event *ProtocolConfigInitialized // Event containing the contract specifics and raw log

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
func (it *ProtocolConfigInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ProtocolConfigInitialized)
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
		it.Event = new(ProtocolConfigInitialized)
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
func (it *ProtocolConfigInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ProtocolConfigInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ProtocolConfigInitialized represents a Initialized event raised by the ProtocolConfig contract.
type ProtocolConfigInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_ProtocolConfig *ProtocolConfigFilterer) FilterInitialized(opts *bind.FilterOpts) (*ProtocolConfigInitializedIterator, error) {

	logs, sub, err := _ProtocolConfig.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigInitializedIterator{contract: _ProtocolConfig.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_ProtocolConfig *ProtocolConfigFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *ProtocolConfigInitialized) (event.Subscription, error) {

	logs, sub, err := _ProtocolConfig.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ProtocolConfigInitialized)
				if err := _ProtocolConfig.contract.UnpackLog(event, "Initialized", log); err != nil {
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
func (_ProtocolConfig *ProtocolConfigFilterer) ParseInitialized(log types.Log) (*ProtocolConfigInitialized, error) {
	event := new(ProtocolConfigInitialized)
	if err := _ProtocolConfig.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ProtocolConfigRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the ProtocolConfig contract.
type ProtocolConfigRoleAdminChangedIterator struct {
	Event *ProtocolConfigRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *ProtocolConfigRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ProtocolConfigRoleAdminChanged)
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
		it.Event = new(ProtocolConfigRoleAdminChanged)
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
func (it *ProtocolConfigRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ProtocolConfigRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ProtocolConfigRoleAdminChanged represents a RoleAdminChanged event raised by the ProtocolConfig contract.
type ProtocolConfigRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_ProtocolConfig *ProtocolConfigFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*ProtocolConfigRoleAdminChangedIterator, error) {

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

	logs, sub, err := _ProtocolConfig.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigRoleAdminChangedIterator{contract: _ProtocolConfig.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_ProtocolConfig *ProtocolConfigFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *ProtocolConfigRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

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

	logs, sub, err := _ProtocolConfig.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ProtocolConfigRoleAdminChanged)
				if err := _ProtocolConfig.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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
func (_ProtocolConfig *ProtocolConfigFilterer) ParseRoleAdminChanged(log types.Log) (*ProtocolConfigRoleAdminChanged, error) {
	event := new(ProtocolConfigRoleAdminChanged)
	if err := _ProtocolConfig.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ProtocolConfigRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the ProtocolConfig contract.
type ProtocolConfigRoleGrantedIterator struct {
	Event *ProtocolConfigRoleGranted // Event containing the contract specifics and raw log

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
func (it *ProtocolConfigRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ProtocolConfigRoleGranted)
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
		it.Event = new(ProtocolConfigRoleGranted)
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
func (it *ProtocolConfigRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ProtocolConfigRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ProtocolConfigRoleGranted represents a RoleGranted event raised by the ProtocolConfig contract.
type ProtocolConfigRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_ProtocolConfig *ProtocolConfigFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*ProtocolConfigRoleGrantedIterator, error) {

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

	logs, sub, err := _ProtocolConfig.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigRoleGrantedIterator{contract: _ProtocolConfig.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_ProtocolConfig *ProtocolConfigFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *ProtocolConfigRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _ProtocolConfig.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ProtocolConfigRoleGranted)
				if err := _ProtocolConfig.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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
func (_ProtocolConfig *ProtocolConfigFilterer) ParseRoleGranted(log types.Log) (*ProtocolConfigRoleGranted, error) {
	event := new(ProtocolConfigRoleGranted)
	if err := _ProtocolConfig.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ProtocolConfigRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the ProtocolConfig contract.
type ProtocolConfigRoleRevokedIterator struct {
	Event *ProtocolConfigRoleRevoked // Event containing the contract specifics and raw log

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
func (it *ProtocolConfigRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ProtocolConfigRoleRevoked)
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
		it.Event = new(ProtocolConfigRoleRevoked)
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
func (it *ProtocolConfigRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ProtocolConfigRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ProtocolConfigRoleRevoked represents a RoleRevoked event raised by the ProtocolConfig contract.
type ProtocolConfigRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_ProtocolConfig *ProtocolConfigFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*ProtocolConfigRoleRevokedIterator, error) {

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

	logs, sub, err := _ProtocolConfig.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigRoleRevokedIterator{contract: _ProtocolConfig.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_ProtocolConfig *ProtocolConfigFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *ProtocolConfigRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _ProtocolConfig.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ProtocolConfigRoleRevoked)
				if err := _ProtocolConfig.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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
func (_ProtocolConfig *ProtocolConfigFilterer) ParseRoleRevoked(log types.Log) (*ProtocolConfigRoleRevoked, error) {
	event := new(ProtocolConfigRoleRevoked)
	if err := _ProtocolConfig.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// ProtocolConfigUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the ProtocolConfig contract.
type ProtocolConfigUpgradedIterator struct {
	Event *ProtocolConfigUpgraded // Event containing the contract specifics and raw log

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
func (it *ProtocolConfigUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(ProtocolConfigUpgraded)
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
		it.Event = new(ProtocolConfigUpgraded)
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
func (it *ProtocolConfigUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *ProtocolConfigUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// ProtocolConfigUpgraded represents a Upgraded event raised by the ProtocolConfig contract.
type ProtocolConfigUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_ProtocolConfig *ProtocolConfigFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*ProtocolConfigUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _ProtocolConfig.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &ProtocolConfigUpgradedIterator{contract: _ProtocolConfig.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_ProtocolConfig *ProtocolConfigFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *ProtocolConfigUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _ProtocolConfig.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(ProtocolConfigUpgraded)
				if err := _ProtocolConfig.contract.UnpackLog(event, "Upgraded", log); err != nil {
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
func (_ProtocolConfig *ProtocolConfigFilterer) ParseUpgraded(log types.Log) (*ProtocolConfigUpgraded, error) {
	event := new(ProtocolConfigUpgraded)
	if err := _ProtocolConfig.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
