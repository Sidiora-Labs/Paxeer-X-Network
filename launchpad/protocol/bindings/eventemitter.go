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

// EventEmitterMetaData contains all meta data concerning the EventEmitter contract.
var EventEmitterMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"MissingRole\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughActiveProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Unauthorized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnauthorizedUpgrade\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"key\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"oldValue\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"newValue\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"ConfigUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"strategy\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"FeeDistributed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"feeAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"protocolCut\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"poolCut\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"FeeRecorded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"oldStrategy\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"uint8\",\"name\":\"newStrategy\",\"type\":\"uint8\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"FeeStrategyChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"MarketCreated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"string\",\"name\":\"hookName\",\"type\":\"string\"},{\"indexed\":false,\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"OpticalExecuted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"virtualReserve\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"realReserve\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"tokenReserve\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"price\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"PoolStateUpdated\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"bool\",\"name\":\"isBuy\",\"type\":\"bool\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amountIn\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amountOut\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"price\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"timestamp\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"blockNumber\",\"type\":\"uint256\"}],\"name\":\"Swap\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"EMITTER_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"key\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"oldValue\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"newValue\",\"type\":\"uint256\"}],\"name\":\"emitConfigUpdated\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"internalType\":\"uint8\",\"name\":\"strategy\",\"type\":\"uint8\"},{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"}],\"name\":\"emitFeeDistributed\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"feeAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"protocolCut\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"poolCut\",\"type\":\"uint256\"}],\"name\":\"emitFeeRecorded\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"nftId\",\"type\":\"uint256\"},{\"internalType\":\"uint8\",\"name\":\"oldStrategy\",\"type\":\"uint8\"},{\"internalType\":\"uint8\",\"name\":\"newStrategy\",\"type\":\"uint8\"}],\"name\":\"emitFeeStrategyChanged\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"creator\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"}],\"name\":\"emitMarketCreated\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"optical\",\"type\":\"address\"},{\"internalType\":\"string\",\"name\":\"hookName\",\"type\":\"string\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"emitOpticalExecuted\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"uint256\",\"name\":\"virtualReserve\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"realReserve\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"tokenReserve\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"price\",\"type\":\"uint256\"}],\"name\":\"emitPoolStateUpdated\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"poolId\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"},{\"internalType\":\"bool\",\"name\":\"isBuy\",\"type\":\"bool\"},{\"internalType\":\"uint256\",\"name\":\"amountIn\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"amountOut\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"fee\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"price\",\"type\":\"uint256\"}],\"name\":\"emitSwap\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_admin\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"emitter\",\"type\":\"address\"}],\"name\":\"isAuthorizedEmitter\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"poolRegistry\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"callerConfirmation\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"emitter\",\"type\":\"address\"},{\"internalType\":\"bool\",\"name\":\"authorized\",\"type\":\"bool\"}],\"name\":\"setAuthorizedEmitter\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_poolRegistry\",\"type\":\"address\"}],\"name\":\"setPoolRegistry\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"}]",
}

// EventEmitterABI is the input ABI used to generate the binding from.
// Deprecated: Use EventEmitterMetaData.ABI instead.
var EventEmitterABI = EventEmitterMetaData.ABI

// EventEmitter is an auto generated Go binding around an Ethereum contract.
type EventEmitter struct {
	EventEmitterCaller     // Read-only binding to the contract
	EventEmitterTransactor // Write-only binding to the contract
	EventEmitterFilterer   // Log filterer for contract events
}

// EventEmitterCaller is an auto generated read-only Go binding around an Ethereum contract.
type EventEmitterCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EventEmitterTransactor is an auto generated write-only Go binding around an Ethereum contract.
type EventEmitterTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EventEmitterFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type EventEmitterFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// EventEmitterSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type EventEmitterSession struct {
	Contract     *EventEmitter     // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// EventEmitterCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type EventEmitterCallerSession struct {
	Contract *EventEmitterCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts       // Call options to use throughout this session
}

// EventEmitterTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type EventEmitterTransactorSession struct {
	Contract     *EventEmitterTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts       // Transaction auth options to use throughout this session
}

// EventEmitterRaw is an auto generated low-level Go binding around an Ethereum contract.
type EventEmitterRaw struct {
	Contract *EventEmitter // Generic contract binding to access the raw methods on
}

// EventEmitterCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type EventEmitterCallerRaw struct {
	Contract *EventEmitterCaller // Generic read-only contract binding to access the raw methods on
}

// EventEmitterTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type EventEmitterTransactorRaw struct {
	Contract *EventEmitterTransactor // Generic write-only contract binding to access the raw methods on
}

// NewEventEmitter creates a new instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitter(address common.Address, backend bind.ContractBackend) (*EventEmitter, error) {
	contract, err := bindEventEmitter(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &EventEmitter{EventEmitterCaller: EventEmitterCaller{contract: contract}, EventEmitterTransactor: EventEmitterTransactor{contract: contract}, EventEmitterFilterer: EventEmitterFilterer{contract: contract}}, nil
}

// NewEventEmitterCaller creates a new read-only instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitterCaller(address common.Address, caller bind.ContractCaller) (*EventEmitterCaller, error) {
	contract, err := bindEventEmitter(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &EventEmitterCaller{contract: contract}, nil
}

// NewEventEmitterTransactor creates a new write-only instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitterTransactor(address common.Address, transactor bind.ContractTransactor) (*EventEmitterTransactor, error) {
	contract, err := bindEventEmitter(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &EventEmitterTransactor{contract: contract}, nil
}

// NewEventEmitterFilterer creates a new log filterer instance of EventEmitter, bound to a specific deployed contract.
func NewEventEmitterFilterer(address common.Address, filterer bind.ContractFilterer) (*EventEmitterFilterer, error) {
	contract, err := bindEventEmitter(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &EventEmitterFilterer{contract: contract}, nil
}

// bindEventEmitter binds a generic wrapper to an already deployed contract.
func bindEventEmitter(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := EventEmitterMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EventEmitter *EventEmitterRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _EventEmitter.Contract.EventEmitterCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EventEmitter *EventEmitterRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EventEmitter.Contract.EventEmitterTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EventEmitter *EventEmitterRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _EventEmitter.Contract.EventEmitterTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_EventEmitter *EventEmitterCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _EventEmitter.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_EventEmitter *EventEmitterTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _EventEmitter.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_EventEmitter *EventEmitterTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _EventEmitter.Contract.contract.Transact(opts, method, params...)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_EventEmitter *EventEmitterCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_EventEmitter *EventEmitterSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _EventEmitter.Contract.DEFAULTADMINROLE(&_EventEmitter.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_EventEmitter *EventEmitterCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _EventEmitter.Contract.DEFAULTADMINROLE(&_EventEmitter.CallOpts)
}

// EMITTERADMINROLE is a free data retrieval call binding the contract method 0xc2d11c38.
//
// Solidity: function EMITTER_ADMIN_ROLE() view returns(bytes32)
func (_EventEmitter *EventEmitterCaller) EMITTERADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "EMITTER_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// EMITTERADMINROLE is a free data retrieval call binding the contract method 0xc2d11c38.
//
// Solidity: function EMITTER_ADMIN_ROLE() view returns(bytes32)
func (_EventEmitter *EventEmitterSession) EMITTERADMINROLE() ([32]byte, error) {
	return _EventEmitter.Contract.EMITTERADMINROLE(&_EventEmitter.CallOpts)
}

// EMITTERADMINROLE is a free data retrieval call binding the contract method 0xc2d11c38.
//
// Solidity: function EMITTER_ADMIN_ROLE() view returns(bytes32)
func (_EventEmitter *EventEmitterCallerSession) EMITTERADMINROLE() ([32]byte, error) {
	return _EventEmitter.Contract.EMITTERADMINROLE(&_EventEmitter.CallOpts)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_EventEmitter *EventEmitterCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_EventEmitter *EventEmitterSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _EventEmitter.Contract.GetRoleAdmin(&_EventEmitter.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_EventEmitter *EventEmitterCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _EventEmitter.Contract.GetRoleAdmin(&_EventEmitter.CallOpts, role)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_EventEmitter *EventEmitterCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_EventEmitter *EventEmitterSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _EventEmitter.Contract.HasRole(&_EventEmitter.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_EventEmitter *EventEmitterCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _EventEmitter.Contract.HasRole(&_EventEmitter.CallOpts, role, account)
}

// IsAuthorizedEmitter is a free data retrieval call binding the contract method 0x66706fd2.
//
// Solidity: function isAuthorizedEmitter(address emitter) view returns(bool)
func (_EventEmitter *EventEmitterCaller) IsAuthorizedEmitter(opts *bind.CallOpts, emitter common.Address) (bool, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "isAuthorizedEmitter", emitter)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// IsAuthorizedEmitter is a free data retrieval call binding the contract method 0x66706fd2.
//
// Solidity: function isAuthorizedEmitter(address emitter) view returns(bool)
func (_EventEmitter *EventEmitterSession) IsAuthorizedEmitter(emitter common.Address) (bool, error) {
	return _EventEmitter.Contract.IsAuthorizedEmitter(&_EventEmitter.CallOpts, emitter)
}

// IsAuthorizedEmitter is a free data retrieval call binding the contract method 0x66706fd2.
//
// Solidity: function isAuthorizedEmitter(address emitter) view returns(bool)
func (_EventEmitter *EventEmitterCallerSession) IsAuthorizedEmitter(emitter common.Address) (bool, error) {
	return _EventEmitter.Contract.IsAuthorizedEmitter(&_EventEmitter.CallOpts, emitter)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_EventEmitter *EventEmitterCaller) PoolRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "poolRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_EventEmitter *EventEmitterSession) PoolRegistry() (common.Address, error) {
	return _EventEmitter.Contract.PoolRegistry(&_EventEmitter.CallOpts)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_EventEmitter *EventEmitterCallerSession) PoolRegistry() (common.Address, error) {
	return _EventEmitter.Contract.PoolRegistry(&_EventEmitter.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_EventEmitter *EventEmitterCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _EventEmitter.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_EventEmitter *EventEmitterSession) ProxiableUUID() ([32]byte, error) {
	return _EventEmitter.Contract.ProxiableUUID(&_EventEmitter.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_EventEmitter *EventEmitterCallerSession) ProxiableUUID() ([32]byte, error) {
	return _EventEmitter.Contract.ProxiableUUID(&_EventEmitter.CallOpts)
}

// EmitConfigUpdated is a paid mutator transaction binding the contract method 0x85ceceb5.
//
// Solidity: function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) returns()
func (_EventEmitter *EventEmitterTransactor) EmitConfigUpdated(opts *bind.TransactOpts, key [32]byte, oldValue *big.Int, newValue *big.Int) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitConfigUpdated", key, oldValue, newValue)
}

// EmitConfigUpdated is a paid mutator transaction binding the contract method 0x85ceceb5.
//
// Solidity: function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) returns()
func (_EventEmitter *EventEmitterSession) EmitConfigUpdated(key [32]byte, oldValue *big.Int, newValue *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitConfigUpdated(&_EventEmitter.TransactOpts, key, oldValue, newValue)
}

// EmitConfigUpdated is a paid mutator transaction binding the contract method 0x85ceceb5.
//
// Solidity: function emitConfigUpdated(bytes32 key, uint256 oldValue, uint256 newValue) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitConfigUpdated(key [32]byte, oldValue *big.Int, newValue *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitConfigUpdated(&_EventEmitter.TransactOpts, key, oldValue, newValue)
}

// EmitFeeDistributed is a paid mutator transaction binding the contract method 0x06a7cedb.
//
// Solidity: function emitFeeDistributed(bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient) returns()
func (_EventEmitter *EventEmitterTransactor) EmitFeeDistributed(opts *bind.TransactOpts, poolId [32]byte, nftId *big.Int, strategy uint8, amount *big.Int, recipient common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitFeeDistributed", poolId, nftId, strategy, amount, recipient)
}

// EmitFeeDistributed is a paid mutator transaction binding the contract method 0x06a7cedb.
//
// Solidity: function emitFeeDistributed(bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient) returns()
func (_EventEmitter *EventEmitterSession) EmitFeeDistributed(poolId [32]byte, nftId *big.Int, strategy uint8, amount *big.Int, recipient common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitFeeDistributed(&_EventEmitter.TransactOpts, poolId, nftId, strategy, amount, recipient)
}

// EmitFeeDistributed is a paid mutator transaction binding the contract method 0x06a7cedb.
//
// Solidity: function emitFeeDistributed(bytes32 poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitFeeDistributed(poolId [32]byte, nftId *big.Int, strategy uint8, amount *big.Int, recipient common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitFeeDistributed(&_EventEmitter.TransactOpts, poolId, nftId, strategy, amount, recipient)
}

// EmitFeeRecorded is a paid mutator transaction binding the contract method 0xd6d28200.
//
// Solidity: function emitFeeRecorded(bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut) returns()
func (_EventEmitter *EventEmitterTransactor) EmitFeeRecorded(opts *bind.TransactOpts, poolId [32]byte, feeAmount *big.Int, protocolCut *big.Int, poolCut *big.Int) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitFeeRecorded", poolId, feeAmount, protocolCut, poolCut)
}

// EmitFeeRecorded is a paid mutator transaction binding the contract method 0xd6d28200.
//
// Solidity: function emitFeeRecorded(bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut) returns()
func (_EventEmitter *EventEmitterSession) EmitFeeRecorded(poolId [32]byte, feeAmount *big.Int, protocolCut *big.Int, poolCut *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitFeeRecorded(&_EventEmitter.TransactOpts, poolId, feeAmount, protocolCut, poolCut)
}

// EmitFeeRecorded is a paid mutator transaction binding the contract method 0xd6d28200.
//
// Solidity: function emitFeeRecorded(bytes32 poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitFeeRecorded(poolId [32]byte, feeAmount *big.Int, protocolCut *big.Int, poolCut *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitFeeRecorded(&_EventEmitter.TransactOpts, poolId, feeAmount, protocolCut, poolCut)
}

// EmitFeeStrategyChanged is a paid mutator transaction binding the contract method 0xb2c8dc72.
//
// Solidity: function emitFeeStrategyChanged(bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy) returns()
func (_EventEmitter *EventEmitterTransactor) EmitFeeStrategyChanged(opts *bind.TransactOpts, poolId [32]byte, nftId *big.Int, oldStrategy uint8, newStrategy uint8) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitFeeStrategyChanged", poolId, nftId, oldStrategy, newStrategy)
}

// EmitFeeStrategyChanged is a paid mutator transaction binding the contract method 0xb2c8dc72.
//
// Solidity: function emitFeeStrategyChanged(bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy) returns()
func (_EventEmitter *EventEmitterSession) EmitFeeStrategyChanged(poolId [32]byte, nftId *big.Int, oldStrategy uint8, newStrategy uint8) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitFeeStrategyChanged(&_EventEmitter.TransactOpts, poolId, nftId, oldStrategy, newStrategy)
}

// EmitFeeStrategyChanged is a paid mutator transaction binding the contract method 0xb2c8dc72.
//
// Solidity: function emitFeeStrategyChanged(bytes32 poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitFeeStrategyChanged(poolId [32]byte, nftId *big.Int, oldStrategy uint8, newStrategy uint8) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitFeeStrategyChanged(&_EventEmitter.TransactOpts, poolId, nftId, oldStrategy, newStrategy)
}

// EmitMarketCreated is a paid mutator transaction binding the contract method 0xe4617ca5.
//
// Solidity: function emitMarketCreated(bytes32 poolId, address token, address creator, address pool, address optical) returns()
func (_EventEmitter *EventEmitterTransactor) EmitMarketCreated(opts *bind.TransactOpts, poolId [32]byte, token common.Address, creator common.Address, pool common.Address, optical common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitMarketCreated", poolId, token, creator, pool, optical)
}

// EmitMarketCreated is a paid mutator transaction binding the contract method 0xe4617ca5.
//
// Solidity: function emitMarketCreated(bytes32 poolId, address token, address creator, address pool, address optical) returns()
func (_EventEmitter *EventEmitterSession) EmitMarketCreated(poolId [32]byte, token common.Address, creator common.Address, pool common.Address, optical common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitMarketCreated(&_EventEmitter.TransactOpts, poolId, token, creator, pool, optical)
}

// EmitMarketCreated is a paid mutator transaction binding the contract method 0xe4617ca5.
//
// Solidity: function emitMarketCreated(bytes32 poolId, address token, address creator, address pool, address optical) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitMarketCreated(poolId [32]byte, token common.Address, creator common.Address, pool common.Address, optical common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitMarketCreated(&_EventEmitter.TransactOpts, poolId, token, creator, pool, optical)
}

// EmitOpticalExecuted is a paid mutator transaction binding the contract method 0xd4756325.
//
// Solidity: function emitOpticalExecuted(bytes32 poolId, address optical, string hookName, bytes data) returns()
func (_EventEmitter *EventEmitterTransactor) EmitOpticalExecuted(opts *bind.TransactOpts, poolId [32]byte, optical common.Address, hookName string, data []byte) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitOpticalExecuted", poolId, optical, hookName, data)
}

// EmitOpticalExecuted is a paid mutator transaction binding the contract method 0xd4756325.
//
// Solidity: function emitOpticalExecuted(bytes32 poolId, address optical, string hookName, bytes data) returns()
func (_EventEmitter *EventEmitterSession) EmitOpticalExecuted(poolId [32]byte, optical common.Address, hookName string, data []byte) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitOpticalExecuted(&_EventEmitter.TransactOpts, poolId, optical, hookName, data)
}

// EmitOpticalExecuted is a paid mutator transaction binding the contract method 0xd4756325.
//
// Solidity: function emitOpticalExecuted(bytes32 poolId, address optical, string hookName, bytes data) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitOpticalExecuted(poolId [32]byte, optical common.Address, hookName string, data []byte) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitOpticalExecuted(&_EventEmitter.TransactOpts, poolId, optical, hookName, data)
}

// EmitPoolStateUpdated is a paid mutator transaction binding the contract method 0xfa78f4ca.
//
// Solidity: function emitPoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price) returns()
func (_EventEmitter *EventEmitterTransactor) EmitPoolStateUpdated(opts *bind.TransactOpts, poolId [32]byte, virtualReserve *big.Int, realReserve *big.Int, tokenReserve *big.Int, price *big.Int) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitPoolStateUpdated", poolId, virtualReserve, realReserve, tokenReserve, price)
}

// EmitPoolStateUpdated is a paid mutator transaction binding the contract method 0xfa78f4ca.
//
// Solidity: function emitPoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price) returns()
func (_EventEmitter *EventEmitterSession) EmitPoolStateUpdated(poolId [32]byte, virtualReserve *big.Int, realReserve *big.Int, tokenReserve *big.Int, price *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitPoolStateUpdated(&_EventEmitter.TransactOpts, poolId, virtualReserve, realReserve, tokenReserve, price)
}

// EmitPoolStateUpdated is a paid mutator transaction binding the contract method 0xfa78f4ca.
//
// Solidity: function emitPoolStateUpdated(bytes32 poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitPoolStateUpdated(poolId [32]byte, virtualReserve *big.Int, realReserve *big.Int, tokenReserve *big.Int, price *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitPoolStateUpdated(&_EventEmitter.TransactOpts, poolId, virtualReserve, realReserve, tokenReserve, price)
}

// EmitSwap is a paid mutator transaction binding the contract method 0x100fca4f.
//
// Solidity: function emitSwap(bytes32 poolId, address sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price) returns()
func (_EventEmitter *EventEmitterTransactor) EmitSwap(opts *bind.TransactOpts, poolId [32]byte, sender common.Address, isBuy bool, amountIn *big.Int, amountOut *big.Int, fee *big.Int, price *big.Int) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "emitSwap", poolId, sender, isBuy, amountIn, amountOut, fee, price)
}

// EmitSwap is a paid mutator transaction binding the contract method 0x100fca4f.
//
// Solidity: function emitSwap(bytes32 poolId, address sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price) returns()
func (_EventEmitter *EventEmitterSession) EmitSwap(poolId [32]byte, sender common.Address, isBuy bool, amountIn *big.Int, amountOut *big.Int, fee *big.Int, price *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitSwap(&_EventEmitter.TransactOpts, poolId, sender, isBuy, amountIn, amountOut, fee, price)
}

// EmitSwap is a paid mutator transaction binding the contract method 0x100fca4f.
//
// Solidity: function emitSwap(bytes32 poolId, address sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price) returns()
func (_EventEmitter *EventEmitterTransactorSession) EmitSwap(poolId [32]byte, sender common.Address, isBuy bool, amountIn *big.Int, amountOut *big.Int, fee *big.Int, price *big.Int) (*types.Transaction, error) {
	return _EventEmitter.Contract.EmitSwap(&_EventEmitter.TransactOpts, poolId, sender, isBuy, amountIn, amountOut, fee, price)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_EventEmitter *EventEmitterTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_EventEmitter *EventEmitterSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.GrantRole(&_EventEmitter.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_EventEmitter *EventEmitterTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.GrantRole(&_EventEmitter.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0xc4d66de8.
//
// Solidity: function initialize(address _admin) returns()
func (_EventEmitter *EventEmitterTransactor) Initialize(opts *bind.TransactOpts, _admin common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "initialize", _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xc4d66de8.
//
// Solidity: function initialize(address _admin) returns()
func (_EventEmitter *EventEmitterSession) Initialize(_admin common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.Initialize(&_EventEmitter.TransactOpts, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xc4d66de8.
//
// Solidity: function initialize(address _admin) returns()
func (_EventEmitter *EventEmitterTransactorSession) Initialize(_admin common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.Initialize(&_EventEmitter.TransactOpts, _admin)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_EventEmitter *EventEmitterTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "renounceRole", role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_EventEmitter *EventEmitterSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.RenounceRole(&_EventEmitter.TransactOpts, role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_EventEmitter *EventEmitterTransactorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.RenounceRole(&_EventEmitter.TransactOpts, role, callerConfirmation)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_EventEmitter *EventEmitterTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_EventEmitter *EventEmitterSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.RevokeRole(&_EventEmitter.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_EventEmitter *EventEmitterTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.RevokeRole(&_EventEmitter.TransactOpts, role, account)
}

// SetAuthorizedEmitter is a paid mutator transaction binding the contract method 0x27b81718.
//
// Solidity: function setAuthorizedEmitter(address emitter, bool authorized) returns()
func (_EventEmitter *EventEmitterTransactor) SetAuthorizedEmitter(opts *bind.TransactOpts, emitter common.Address, authorized bool) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "setAuthorizedEmitter", emitter, authorized)
}

// SetAuthorizedEmitter is a paid mutator transaction binding the contract method 0x27b81718.
//
// Solidity: function setAuthorizedEmitter(address emitter, bool authorized) returns()
func (_EventEmitter *EventEmitterSession) SetAuthorizedEmitter(emitter common.Address, authorized bool) (*types.Transaction, error) {
	return _EventEmitter.Contract.SetAuthorizedEmitter(&_EventEmitter.TransactOpts, emitter, authorized)
}

// SetAuthorizedEmitter is a paid mutator transaction binding the contract method 0x27b81718.
//
// Solidity: function setAuthorizedEmitter(address emitter, bool authorized) returns()
func (_EventEmitter *EventEmitterTransactorSession) SetAuthorizedEmitter(emitter common.Address, authorized bool) (*types.Transaction, error) {
	return _EventEmitter.Contract.SetAuthorizedEmitter(&_EventEmitter.TransactOpts, emitter, authorized)
}

// SetPoolRegistry is a paid mutator transaction binding the contract method 0x7b77cd6a.
//
// Solidity: function setPoolRegistry(address _poolRegistry) returns()
func (_EventEmitter *EventEmitterTransactor) SetPoolRegistry(opts *bind.TransactOpts, _poolRegistry common.Address) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "setPoolRegistry", _poolRegistry)
}

// SetPoolRegistry is a paid mutator transaction binding the contract method 0x7b77cd6a.
//
// Solidity: function setPoolRegistry(address _poolRegistry) returns()
func (_EventEmitter *EventEmitterSession) SetPoolRegistry(_poolRegistry common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.SetPoolRegistry(&_EventEmitter.TransactOpts, _poolRegistry)
}

// SetPoolRegistry is a paid mutator transaction binding the contract method 0x7b77cd6a.
//
// Solidity: function setPoolRegistry(address _poolRegistry) returns()
func (_EventEmitter *EventEmitterTransactorSession) SetPoolRegistry(_poolRegistry common.Address) (*types.Transaction, error) {
	return _EventEmitter.Contract.SetPoolRegistry(&_EventEmitter.TransactOpts, _poolRegistry)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_EventEmitter *EventEmitterTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _EventEmitter.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_EventEmitter *EventEmitterSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _EventEmitter.Contract.UpgradeToAndCall(&_EventEmitter.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_EventEmitter *EventEmitterTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _EventEmitter.Contract.UpgradeToAndCall(&_EventEmitter.TransactOpts, newImplementation, data)
}

// EventEmitterConfigUpdatedIterator is returned from FilterConfigUpdated and is used to iterate over the raw logs and unpacked data for ConfigUpdated events raised by the EventEmitter contract.
type EventEmitterConfigUpdatedIterator struct {
	Event *EventEmitterConfigUpdated // Event containing the contract specifics and raw log

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
func (it *EventEmitterConfigUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterConfigUpdated)
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
		it.Event = new(EventEmitterConfigUpdated)
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
func (it *EventEmitterConfigUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterConfigUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterConfigUpdated represents a ConfigUpdated event raised by the EventEmitter contract.
type EventEmitterConfigUpdated struct {
	Key         [32]byte
	OldValue    *big.Int
	NewValue    *big.Int
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterConfigUpdated is a free log retrieval operation binding the contract event 0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e.
//
// Solidity: event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterConfigUpdated(opts *bind.FilterOpts, key [][32]byte) (*EventEmitterConfigUpdatedIterator, error) {

	var keyRule []interface{}
	for _, keyItem := range key {
		keyRule = append(keyRule, keyItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "ConfigUpdated", keyRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterConfigUpdatedIterator{contract: _EventEmitter.contract, event: "ConfigUpdated", logs: logs, sub: sub}, nil
}

// WatchConfigUpdated is a free log subscription operation binding the contract event 0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e.
//
// Solidity: event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchConfigUpdated(opts *bind.WatchOpts, sink chan<- *EventEmitterConfigUpdated, key [][32]byte) (event.Subscription, error) {

	var keyRule []interface{}
	for _, keyItem := range key {
		keyRule = append(keyRule, keyItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "ConfigUpdated", keyRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterConfigUpdated)
				if err := _EventEmitter.contract.UnpackLog(event, "ConfigUpdated", log); err != nil {
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

// ParseConfigUpdated is a log parse operation binding the contract event 0x2903b3642d2105b7fbd127326de888ab50e8d975bf819398337fb58407c1b10e.
//
// Solidity: event ConfigUpdated(bytes32 indexed key, uint256 oldValue, uint256 newValue, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseConfigUpdated(log types.Log) (*EventEmitterConfigUpdated, error) {
	event := new(EventEmitterConfigUpdated)
	if err := _EventEmitter.contract.UnpackLog(event, "ConfigUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterFeeDistributedIterator is returned from FilterFeeDistributed and is used to iterate over the raw logs and unpacked data for FeeDistributed events raised by the EventEmitter contract.
type EventEmitterFeeDistributedIterator struct {
	Event *EventEmitterFeeDistributed // Event containing the contract specifics and raw log

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
func (it *EventEmitterFeeDistributedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterFeeDistributed)
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
		it.Event = new(EventEmitterFeeDistributed)
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
func (it *EventEmitterFeeDistributedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterFeeDistributedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterFeeDistributed represents a FeeDistributed event raised by the EventEmitter contract.
type EventEmitterFeeDistributed struct {
	PoolId      [32]byte
	NftId       *big.Int
	Strategy    uint8
	Amount      *big.Int
	Recipient   common.Address
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterFeeDistributed is a free log retrieval operation binding the contract event 0xf7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc.
//
// Solidity: event FeeDistributed(bytes32 indexed poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterFeeDistributed(opts *bind.FilterOpts, poolId [][32]byte) (*EventEmitterFeeDistributedIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "FeeDistributed", poolIdRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterFeeDistributedIterator{contract: _EventEmitter.contract, event: "FeeDistributed", logs: logs, sub: sub}, nil
}

// WatchFeeDistributed is a free log subscription operation binding the contract event 0xf7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc.
//
// Solidity: event FeeDistributed(bytes32 indexed poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchFeeDistributed(opts *bind.WatchOpts, sink chan<- *EventEmitterFeeDistributed, poolId [][32]byte) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "FeeDistributed", poolIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterFeeDistributed)
				if err := _EventEmitter.contract.UnpackLog(event, "FeeDistributed", log); err != nil {
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

// ParseFeeDistributed is a log parse operation binding the contract event 0xf7ab826cedf9d005102eddf95e65f43597aef36a55ff09ee4f452204c83f4afc.
//
// Solidity: event FeeDistributed(bytes32 indexed poolId, uint256 nftId, uint8 strategy, uint256 amount, address recipient, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseFeeDistributed(log types.Log) (*EventEmitterFeeDistributed, error) {
	event := new(EventEmitterFeeDistributed)
	if err := _EventEmitter.contract.UnpackLog(event, "FeeDistributed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterFeeRecordedIterator is returned from FilterFeeRecorded and is used to iterate over the raw logs and unpacked data for FeeRecorded events raised by the EventEmitter contract.
type EventEmitterFeeRecordedIterator struct {
	Event *EventEmitterFeeRecorded // Event containing the contract specifics and raw log

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
func (it *EventEmitterFeeRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterFeeRecorded)
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
		it.Event = new(EventEmitterFeeRecorded)
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
func (it *EventEmitterFeeRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterFeeRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterFeeRecorded represents a FeeRecorded event raised by the EventEmitter contract.
type EventEmitterFeeRecorded struct {
	PoolId      [32]byte
	FeeAmount   *big.Int
	ProtocolCut *big.Int
	PoolCut     *big.Int
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterFeeRecorded is a free log retrieval operation binding the contract event 0xe80e9e3e09460d716260f5ce6979a59f6cf4fbc1096e20f008d78b5cac7b7179.
//
// Solidity: event FeeRecorded(bytes32 indexed poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterFeeRecorded(opts *bind.FilterOpts, poolId [][32]byte) (*EventEmitterFeeRecordedIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "FeeRecorded", poolIdRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterFeeRecordedIterator{contract: _EventEmitter.contract, event: "FeeRecorded", logs: logs, sub: sub}, nil
}

// WatchFeeRecorded is a free log subscription operation binding the contract event 0xe80e9e3e09460d716260f5ce6979a59f6cf4fbc1096e20f008d78b5cac7b7179.
//
// Solidity: event FeeRecorded(bytes32 indexed poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchFeeRecorded(opts *bind.WatchOpts, sink chan<- *EventEmitterFeeRecorded, poolId [][32]byte) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "FeeRecorded", poolIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterFeeRecorded)
				if err := _EventEmitter.contract.UnpackLog(event, "FeeRecorded", log); err != nil {
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

// ParseFeeRecorded is a log parse operation binding the contract event 0xe80e9e3e09460d716260f5ce6979a59f6cf4fbc1096e20f008d78b5cac7b7179.
//
// Solidity: event FeeRecorded(bytes32 indexed poolId, uint256 feeAmount, uint256 protocolCut, uint256 poolCut, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseFeeRecorded(log types.Log) (*EventEmitterFeeRecorded, error) {
	event := new(EventEmitterFeeRecorded)
	if err := _EventEmitter.contract.UnpackLog(event, "FeeRecorded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterFeeStrategyChangedIterator is returned from FilterFeeStrategyChanged and is used to iterate over the raw logs and unpacked data for FeeStrategyChanged events raised by the EventEmitter contract.
type EventEmitterFeeStrategyChangedIterator struct {
	Event *EventEmitterFeeStrategyChanged // Event containing the contract specifics and raw log

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
func (it *EventEmitterFeeStrategyChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterFeeStrategyChanged)
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
		it.Event = new(EventEmitterFeeStrategyChanged)
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
func (it *EventEmitterFeeStrategyChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterFeeStrategyChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterFeeStrategyChanged represents a FeeStrategyChanged event raised by the EventEmitter contract.
type EventEmitterFeeStrategyChanged struct {
	PoolId      [32]byte
	NftId       *big.Int
	OldStrategy uint8
	NewStrategy uint8
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterFeeStrategyChanged is a free log retrieval operation binding the contract event 0x337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd.
//
// Solidity: event FeeStrategyChanged(bytes32 indexed poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterFeeStrategyChanged(opts *bind.FilterOpts, poolId [][32]byte) (*EventEmitterFeeStrategyChangedIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "FeeStrategyChanged", poolIdRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterFeeStrategyChangedIterator{contract: _EventEmitter.contract, event: "FeeStrategyChanged", logs: logs, sub: sub}, nil
}

// WatchFeeStrategyChanged is a free log subscription operation binding the contract event 0x337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd.
//
// Solidity: event FeeStrategyChanged(bytes32 indexed poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchFeeStrategyChanged(opts *bind.WatchOpts, sink chan<- *EventEmitterFeeStrategyChanged, poolId [][32]byte) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "FeeStrategyChanged", poolIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterFeeStrategyChanged)
				if err := _EventEmitter.contract.UnpackLog(event, "FeeStrategyChanged", log); err != nil {
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

// ParseFeeStrategyChanged is a log parse operation binding the contract event 0x337fd9f9304e9da4c04886a5b8bf56e74032b459758212a860bc61d3544353dd.
//
// Solidity: event FeeStrategyChanged(bytes32 indexed poolId, uint256 nftId, uint8 oldStrategy, uint8 newStrategy, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseFeeStrategyChanged(log types.Log) (*EventEmitterFeeStrategyChanged, error) {
	event := new(EventEmitterFeeStrategyChanged)
	if err := _EventEmitter.contract.UnpackLog(event, "FeeStrategyChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the EventEmitter contract.
type EventEmitterInitializedIterator struct {
	Event *EventEmitterInitialized // Event containing the contract specifics and raw log

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
func (it *EventEmitterInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterInitialized)
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
		it.Event = new(EventEmitterInitialized)
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
func (it *EventEmitterInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterInitialized represents a Initialized event raised by the EventEmitter contract.
type EventEmitterInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_EventEmitter *EventEmitterFilterer) FilterInitialized(opts *bind.FilterOpts) (*EventEmitterInitializedIterator, error) {

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &EventEmitterInitializedIterator{contract: _EventEmitter.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_EventEmitter *EventEmitterFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *EventEmitterInitialized) (event.Subscription, error) {

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterInitialized)
				if err := _EventEmitter.contract.UnpackLog(event, "Initialized", log); err != nil {
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
func (_EventEmitter *EventEmitterFilterer) ParseInitialized(log types.Log) (*EventEmitterInitialized, error) {
	event := new(EventEmitterInitialized)
	if err := _EventEmitter.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterMarketCreatedIterator is returned from FilterMarketCreated and is used to iterate over the raw logs and unpacked data for MarketCreated events raised by the EventEmitter contract.
type EventEmitterMarketCreatedIterator struct {
	Event *EventEmitterMarketCreated // Event containing the contract specifics and raw log

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
func (it *EventEmitterMarketCreatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterMarketCreated)
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
		it.Event = new(EventEmitterMarketCreated)
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
func (it *EventEmitterMarketCreatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterMarketCreatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterMarketCreated represents a MarketCreated event raised by the EventEmitter contract.
type EventEmitterMarketCreated struct {
	PoolId      [32]byte
	Token       common.Address
	Creator     common.Address
	Pool        common.Address
	Optical     common.Address
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterMarketCreated is a free log retrieval operation binding the contract event 0xb0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b.
//
// Solidity: event MarketCreated(bytes32 indexed poolId, address indexed token, address indexed creator, address pool, address optical, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterMarketCreated(opts *bind.FilterOpts, poolId [][32]byte, token []common.Address, creator []common.Address) (*EventEmitterMarketCreatedIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var creatorRule []interface{}
	for _, creatorItem := range creator {
		creatorRule = append(creatorRule, creatorItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "MarketCreated", poolIdRule, tokenRule, creatorRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterMarketCreatedIterator{contract: _EventEmitter.contract, event: "MarketCreated", logs: logs, sub: sub}, nil
}

// WatchMarketCreated is a free log subscription operation binding the contract event 0xb0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b.
//
// Solidity: event MarketCreated(bytes32 indexed poolId, address indexed token, address indexed creator, address pool, address optical, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchMarketCreated(opts *bind.WatchOpts, sink chan<- *EventEmitterMarketCreated, poolId [][32]byte, token []common.Address, creator []common.Address) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}
	var tokenRule []interface{}
	for _, tokenItem := range token {
		tokenRule = append(tokenRule, tokenItem)
	}
	var creatorRule []interface{}
	for _, creatorItem := range creator {
		creatorRule = append(creatorRule, creatorItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "MarketCreated", poolIdRule, tokenRule, creatorRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterMarketCreated)
				if err := _EventEmitter.contract.UnpackLog(event, "MarketCreated", log); err != nil {
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

// ParseMarketCreated is a log parse operation binding the contract event 0xb0a9c7605389f1127c2299d4a3ed85f0b2fc19894ab5411d76b453bb29f9f47b.
//
// Solidity: event MarketCreated(bytes32 indexed poolId, address indexed token, address indexed creator, address pool, address optical, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseMarketCreated(log types.Log) (*EventEmitterMarketCreated, error) {
	event := new(EventEmitterMarketCreated)
	if err := _EventEmitter.contract.UnpackLog(event, "MarketCreated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterOpticalExecutedIterator is returned from FilterOpticalExecuted and is used to iterate over the raw logs and unpacked data for OpticalExecuted events raised by the EventEmitter contract.
type EventEmitterOpticalExecutedIterator struct {
	Event *EventEmitterOpticalExecuted // Event containing the contract specifics and raw log

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
func (it *EventEmitterOpticalExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterOpticalExecuted)
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
		it.Event = new(EventEmitterOpticalExecuted)
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
func (it *EventEmitterOpticalExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterOpticalExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterOpticalExecuted represents a OpticalExecuted event raised by the EventEmitter contract.
type EventEmitterOpticalExecuted struct {
	PoolId      [32]byte
	Optical     common.Address
	HookName    string
	Data        []byte
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterOpticalExecuted is a free log retrieval operation binding the contract event 0xfdf5ccbb26a02ea4efd5415e00b1d7782ca04cbb6b33a0d0a0d549ba284f9e57.
//
// Solidity: event OpticalExecuted(bytes32 indexed poolId, address indexed optical, string hookName, bytes data, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterOpticalExecuted(opts *bind.FilterOpts, poolId [][32]byte, optical []common.Address) (*EventEmitterOpticalExecutedIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}
	var opticalRule []interface{}
	for _, opticalItem := range optical {
		opticalRule = append(opticalRule, opticalItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "OpticalExecuted", poolIdRule, opticalRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterOpticalExecutedIterator{contract: _EventEmitter.contract, event: "OpticalExecuted", logs: logs, sub: sub}, nil
}

// WatchOpticalExecuted is a free log subscription operation binding the contract event 0xfdf5ccbb26a02ea4efd5415e00b1d7782ca04cbb6b33a0d0a0d549ba284f9e57.
//
// Solidity: event OpticalExecuted(bytes32 indexed poolId, address indexed optical, string hookName, bytes data, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchOpticalExecuted(opts *bind.WatchOpts, sink chan<- *EventEmitterOpticalExecuted, poolId [][32]byte, optical []common.Address) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}
	var opticalRule []interface{}
	for _, opticalItem := range optical {
		opticalRule = append(opticalRule, opticalItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "OpticalExecuted", poolIdRule, opticalRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterOpticalExecuted)
				if err := _EventEmitter.contract.UnpackLog(event, "OpticalExecuted", log); err != nil {
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

// ParseOpticalExecuted is a log parse operation binding the contract event 0xfdf5ccbb26a02ea4efd5415e00b1d7782ca04cbb6b33a0d0a0d549ba284f9e57.
//
// Solidity: event OpticalExecuted(bytes32 indexed poolId, address indexed optical, string hookName, bytes data, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseOpticalExecuted(log types.Log) (*EventEmitterOpticalExecuted, error) {
	event := new(EventEmitterOpticalExecuted)
	if err := _EventEmitter.contract.UnpackLog(event, "OpticalExecuted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterPoolStateUpdatedIterator is returned from FilterPoolStateUpdated and is used to iterate over the raw logs and unpacked data for PoolStateUpdated events raised by the EventEmitter contract.
type EventEmitterPoolStateUpdatedIterator struct {
	Event *EventEmitterPoolStateUpdated // Event containing the contract specifics and raw log

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
func (it *EventEmitterPoolStateUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterPoolStateUpdated)
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
		it.Event = new(EventEmitterPoolStateUpdated)
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
func (it *EventEmitterPoolStateUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterPoolStateUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterPoolStateUpdated represents a PoolStateUpdated event raised by the EventEmitter contract.
type EventEmitterPoolStateUpdated struct {
	PoolId         [32]byte
	VirtualReserve *big.Int
	RealReserve    *big.Int
	TokenReserve   *big.Int
	Price          *big.Int
	Timestamp      *big.Int
	BlockNumber    *big.Int
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterPoolStateUpdated is a free log retrieval operation binding the contract event 0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd.
//
// Solidity: event PoolStateUpdated(bytes32 indexed poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterPoolStateUpdated(opts *bind.FilterOpts, poolId [][32]byte) (*EventEmitterPoolStateUpdatedIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "PoolStateUpdated", poolIdRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterPoolStateUpdatedIterator{contract: _EventEmitter.contract, event: "PoolStateUpdated", logs: logs, sub: sub}, nil
}

// WatchPoolStateUpdated is a free log subscription operation binding the contract event 0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd.
//
// Solidity: event PoolStateUpdated(bytes32 indexed poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchPoolStateUpdated(opts *bind.WatchOpts, sink chan<- *EventEmitterPoolStateUpdated, poolId [][32]byte) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "PoolStateUpdated", poolIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterPoolStateUpdated)
				if err := _EventEmitter.contract.UnpackLog(event, "PoolStateUpdated", log); err != nil {
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

// ParsePoolStateUpdated is a log parse operation binding the contract event 0x8155a4b38a877d212b271811786986f8f7d78074c82fce4526c58defcc6d7ebd.
//
// Solidity: event PoolStateUpdated(bytes32 indexed poolId, uint256 virtualReserve, uint256 realReserve, uint256 tokenReserve, uint256 price, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParsePoolStateUpdated(log types.Log) (*EventEmitterPoolStateUpdated, error) {
	event := new(EventEmitterPoolStateUpdated)
	if err := _EventEmitter.contract.UnpackLog(event, "PoolStateUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the EventEmitter contract.
type EventEmitterRoleAdminChangedIterator struct {
	Event *EventEmitterRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *EventEmitterRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterRoleAdminChanged)
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
		it.Event = new(EventEmitterRoleAdminChanged)
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
func (it *EventEmitterRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterRoleAdminChanged represents a RoleAdminChanged event raised by the EventEmitter contract.
type EventEmitterRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_EventEmitter *EventEmitterFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*EventEmitterRoleAdminChangedIterator, error) {

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

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterRoleAdminChangedIterator{contract: _EventEmitter.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_EventEmitter *EventEmitterFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *EventEmitterRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

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

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterRoleAdminChanged)
				if err := _EventEmitter.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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
func (_EventEmitter *EventEmitterFilterer) ParseRoleAdminChanged(log types.Log) (*EventEmitterRoleAdminChanged, error) {
	event := new(EventEmitterRoleAdminChanged)
	if err := _EventEmitter.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the EventEmitter contract.
type EventEmitterRoleGrantedIterator struct {
	Event *EventEmitterRoleGranted // Event containing the contract specifics and raw log

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
func (it *EventEmitterRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterRoleGranted)
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
		it.Event = new(EventEmitterRoleGranted)
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
func (it *EventEmitterRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterRoleGranted represents a RoleGranted event raised by the EventEmitter contract.
type EventEmitterRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_EventEmitter *EventEmitterFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*EventEmitterRoleGrantedIterator, error) {

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

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterRoleGrantedIterator{contract: _EventEmitter.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_EventEmitter *EventEmitterFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *EventEmitterRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterRoleGranted)
				if err := _EventEmitter.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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
func (_EventEmitter *EventEmitterFilterer) ParseRoleGranted(log types.Log) (*EventEmitterRoleGranted, error) {
	event := new(EventEmitterRoleGranted)
	if err := _EventEmitter.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the EventEmitter contract.
type EventEmitterRoleRevokedIterator struct {
	Event *EventEmitterRoleRevoked // Event containing the contract specifics and raw log

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
func (it *EventEmitterRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterRoleRevoked)
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
		it.Event = new(EventEmitterRoleRevoked)
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
func (it *EventEmitterRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterRoleRevoked represents a RoleRevoked event raised by the EventEmitter contract.
type EventEmitterRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_EventEmitter *EventEmitterFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*EventEmitterRoleRevokedIterator, error) {

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

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterRoleRevokedIterator{contract: _EventEmitter.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_EventEmitter *EventEmitterFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *EventEmitterRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterRoleRevoked)
				if err := _EventEmitter.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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
func (_EventEmitter *EventEmitterFilterer) ParseRoleRevoked(log types.Log) (*EventEmitterRoleRevoked, error) {
	event := new(EventEmitterRoleRevoked)
	if err := _EventEmitter.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterSwapIterator is returned from FilterSwap and is used to iterate over the raw logs and unpacked data for Swap events raised by the EventEmitter contract.
type EventEmitterSwapIterator struct {
	Event *EventEmitterSwap // Event containing the contract specifics and raw log

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
func (it *EventEmitterSwapIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterSwap)
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
		it.Event = new(EventEmitterSwap)
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
func (it *EventEmitterSwapIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterSwapIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterSwap represents a Swap event raised by the EventEmitter contract.
type EventEmitterSwap struct {
	PoolId      [32]byte
	Sender      common.Address
	IsBuy       bool
	AmountIn    *big.Int
	AmountOut   *big.Int
	Fee         *big.Int
	Price       *big.Int
	Timestamp   *big.Int
	BlockNumber *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterSwap is a free log retrieval operation binding the contract event 0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc.
//
// Solidity: event Swap(bytes32 indexed poolId, address indexed sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) FilterSwap(opts *bind.FilterOpts, poolId [][32]byte, sender []common.Address) (*EventEmitterSwapIterator, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Swap", poolIdRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterSwapIterator{contract: _EventEmitter.contract, event: "Swap", logs: logs, sub: sub}, nil
}

// WatchSwap is a free log subscription operation binding the contract event 0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc.
//
// Solidity: event Swap(bytes32 indexed poolId, address indexed sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) WatchSwap(opts *bind.WatchOpts, sink chan<- *EventEmitterSwap, poolId [][32]byte, sender []common.Address) (event.Subscription, error) {

	var poolIdRule []interface{}
	for _, poolIdItem := range poolId {
		poolIdRule = append(poolIdRule, poolIdItem)
	}
	var senderRule []interface{}
	for _, senderItem := range sender {
		senderRule = append(senderRule, senderItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Swap", poolIdRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterSwap)
				if err := _EventEmitter.contract.UnpackLog(event, "Swap", log); err != nil {
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

// ParseSwap is a log parse operation binding the contract event 0xa234ab85728295548823d8a26077ecc77aa5815f9ed8f4c5e6fd390ac851d4cc.
//
// Solidity: event Swap(bytes32 indexed poolId, address indexed sender, bool isBuy, uint256 amountIn, uint256 amountOut, uint256 fee, uint256 price, uint256 timestamp, uint256 blockNumber)
func (_EventEmitter *EventEmitterFilterer) ParseSwap(log types.Log) (*EventEmitterSwap, error) {
	event := new(EventEmitterSwap)
	if err := _EventEmitter.contract.UnpackLog(event, "Swap", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// EventEmitterUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the EventEmitter contract.
type EventEmitterUpgradedIterator struct {
	Event *EventEmitterUpgraded // Event containing the contract specifics and raw log

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
func (it *EventEmitterUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(EventEmitterUpgraded)
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
		it.Event = new(EventEmitterUpgraded)
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
func (it *EventEmitterUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *EventEmitterUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// EventEmitterUpgraded represents a Upgraded event raised by the EventEmitter contract.
type EventEmitterUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_EventEmitter *EventEmitterFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*EventEmitterUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _EventEmitter.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &EventEmitterUpgradedIterator{contract: _EventEmitter.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_EventEmitter *EventEmitterFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *EventEmitterUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _EventEmitter.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(EventEmitterUpgraded)
				if err := _EventEmitter.contract.UnpackLog(event, "Upgraded", log); err != nil {
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
func (_EventEmitter *EventEmitterFilterer) ParseUpgraded(log types.Log) (*EventEmitterUpgraded, error) {
	event := new(EventEmitterUpgraded)
	if err := _EventEmitter.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
