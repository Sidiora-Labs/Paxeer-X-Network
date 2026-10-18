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

// FeeAccumulatorMetaData contains all meta data concerning the FeeAccumulator contract.
var FeeAccumulatorMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[],\"name\":\"AirdropNotTriggered\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"AlreadyClaimed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"AlreadyInitialized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ApproveFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"InvalidImplementation\",\"type\":\"error\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"MissingRole\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NoFeesAccumulated\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"NotInitializing\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ReentrancyGuardReentrantCall\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"TransferFailed\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughActiveProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UUPSNotThroughProxy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"Unauthorized\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"UnauthorizedUpgrade\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"WrongStrategy\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAddress\",\"type\":\"error\"},{\"inputs\":[],\"name\":\"ZeroAmount\",\"type\":\"error\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"holder\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"AirdropClaimed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"totalAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"AirdropTriggered\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"feeAmount\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"protocolCut\",\"type\":\"uint256\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"poolCut\",\"type\":\"uint256\"}],\"name\":\"FeeRecorded\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"FeesBurned\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"FeesClaimed\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint64\",\"name\":\"version\",\"type\":\"uint64\"}],\"name\":\"Initialized\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"LpRewardsSent\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":false,\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"ProtocolFeeSwept\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"previousAdminRole\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"newAdminRole\",\"type\":\"bytes32\"}],\"name\":\"RoleAdminChanged\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleGranted\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"},{\"indexed\":true,\"internalType\":\"address\",\"name\":\"sender\",\"type\":\"address\"}],\"name\":\"RoleRevoked\",\"type\":\"event\"},{\"anonymous\":false,\"inputs\":[{\"indexed\":true,\"internalType\":\"address\",\"name\":\"implementation\",\"type\":\"address\"}],\"name\":\"Upgraded\",\"type\":\"event\"},{\"inputs\":[],\"name\":\"DEAD_ADDRESS\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"DEFAULT_ADMIN_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"FEES_ROUTER_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"POOL_ROLE\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"burn\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"recipient\",\"type\":\"address\"}],\"name\":\"claim\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"holder\",\"type\":\"address\"}],\"name\":\"claimAirdrop\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"eventEmitter\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"getAccumulatedFees\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"getAirdropBalance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"getAirdropEpoch\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"getLpRewardsBalance\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"getProtocolFeesPending\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"}],\"name\":\"getRoleAdmin\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"grantRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"holder\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"epoch\",\"type\":\"uint256\"}],\"name\":\"hasClaimedAirdrop\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"hasRole\",\"outputs\":[{\"internalType\":\"bool\",\"name\":\"\",\"type\":\"bool\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"_protocolConfig\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_treasury\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_poolRegistry\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_eventEmitter\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_usdlAddress\",\"type\":\"address\"},{\"internalType\":\"address\",\"name\":\"_admin\",\"type\":\"address\"}],\"name\":\"initialize\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"poolRegistry\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"protocolConfig\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"proxiableUUID\",\"outputs\":[{\"internalType\":\"bytes32\",\"name\":\"\",\"type\":\"bytes32\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"},{\"internalType\":\"uint256\",\"name\":\"feeAmount\",\"type\":\"uint256\"}],\"name\":\"recordFee\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"callerConfirmation\",\"type\":\"address\"}],\"name\":\"renounceRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"bytes32\",\"name\":\"role\",\"type\":\"bytes32\"},{\"internalType\":\"address\",\"name\":\"account\",\"type\":\"address\"}],\"name\":\"revokeRole\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"sendLpRewards\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"treasury\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"pool\",\"type\":\"address\"}],\"name\":\"triggerAirdrop\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"totalAmount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newImplementation\",\"type\":\"address\"},{\"internalType\":\"bytes\",\"name\":\"data\",\"type\":\"bytes\"}],\"name\":\"upgradeToAndCall\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"usdlAddress\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"}]",
}

// FeeAccumulatorABI is the input ABI used to generate the binding from.
// Deprecated: Use FeeAccumulatorMetaData.ABI instead.
var FeeAccumulatorABI = FeeAccumulatorMetaData.ABI

// FeeAccumulator is an auto generated Go binding around an Ethereum contract.
type FeeAccumulator struct {
	FeeAccumulatorCaller     // Read-only binding to the contract
	FeeAccumulatorTransactor // Write-only binding to the contract
	FeeAccumulatorFilterer   // Log filterer for contract events
}

// FeeAccumulatorCaller is an auto generated read-only Go binding around an Ethereum contract.
type FeeAccumulatorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeeAccumulatorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type FeeAccumulatorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeeAccumulatorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type FeeAccumulatorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// FeeAccumulatorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type FeeAccumulatorSession struct {
	Contract     *FeeAccumulator   // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// FeeAccumulatorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type FeeAccumulatorCallerSession struct {
	Contract *FeeAccumulatorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts         // Call options to use throughout this session
}

// FeeAccumulatorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type FeeAccumulatorTransactorSession struct {
	Contract     *FeeAccumulatorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts         // Transaction auth options to use throughout this session
}

// FeeAccumulatorRaw is an auto generated low-level Go binding around an Ethereum contract.
type FeeAccumulatorRaw struct {
	Contract *FeeAccumulator // Generic contract binding to access the raw methods on
}

// FeeAccumulatorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type FeeAccumulatorCallerRaw struct {
	Contract *FeeAccumulatorCaller // Generic read-only contract binding to access the raw methods on
}

// FeeAccumulatorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type FeeAccumulatorTransactorRaw struct {
	Contract *FeeAccumulatorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewFeeAccumulator creates a new instance of FeeAccumulator, bound to a specific deployed contract.
func NewFeeAccumulator(address common.Address, backend bind.ContractBackend) (*FeeAccumulator, error) {
	contract, err := bindFeeAccumulator(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulator{FeeAccumulatorCaller: FeeAccumulatorCaller{contract: contract}, FeeAccumulatorTransactor: FeeAccumulatorTransactor{contract: contract}, FeeAccumulatorFilterer: FeeAccumulatorFilterer{contract: contract}}, nil
}

// NewFeeAccumulatorCaller creates a new read-only instance of FeeAccumulator, bound to a specific deployed contract.
func NewFeeAccumulatorCaller(address common.Address, caller bind.ContractCaller) (*FeeAccumulatorCaller, error) {
	contract, err := bindFeeAccumulator(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorCaller{contract: contract}, nil
}

// NewFeeAccumulatorTransactor creates a new write-only instance of FeeAccumulator, bound to a specific deployed contract.
func NewFeeAccumulatorTransactor(address common.Address, transactor bind.ContractTransactor) (*FeeAccumulatorTransactor, error) {
	contract, err := bindFeeAccumulator(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorTransactor{contract: contract}, nil
}

// NewFeeAccumulatorFilterer creates a new log filterer instance of FeeAccumulator, bound to a specific deployed contract.
func NewFeeAccumulatorFilterer(address common.Address, filterer bind.ContractFilterer) (*FeeAccumulatorFilterer, error) {
	contract, err := bindFeeAccumulator(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorFilterer{contract: contract}, nil
}

// bindFeeAccumulator binds a generic wrapper to an already deployed contract.
func bindFeeAccumulator(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := FeeAccumulatorMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FeeAccumulator *FeeAccumulatorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FeeAccumulator.Contract.FeeAccumulatorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FeeAccumulator *FeeAccumulatorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.FeeAccumulatorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FeeAccumulator *FeeAccumulatorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.FeeAccumulatorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_FeeAccumulator *FeeAccumulatorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _FeeAccumulator.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_FeeAccumulator *FeeAccumulatorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_FeeAccumulator *FeeAccumulatorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.contract.Transact(opts, method, params...)
}

// DEADADDRESS is a free data retrieval call binding the contract method 0x4e6fd6c4.
//
// Solidity: function DEAD_ADDRESS() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCaller) DEADADDRESS(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "DEAD_ADDRESS")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// DEADADDRESS is a free data retrieval call binding the contract method 0x4e6fd6c4.
//
// Solidity: function DEAD_ADDRESS() view returns(address)
func (_FeeAccumulator *FeeAccumulatorSession) DEADADDRESS() (common.Address, error) {
	return _FeeAccumulator.Contract.DEADADDRESS(&_FeeAccumulator.CallOpts)
}

// DEADADDRESS is a free data retrieval call binding the contract method 0x4e6fd6c4.
//
// Solidity: function DEAD_ADDRESS() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCallerSession) DEADADDRESS() (common.Address, error) {
	return _FeeAccumulator.Contract.DEADADDRESS(&_FeeAccumulator.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCaller) DEFAULTADMINROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "DEFAULT_ADMIN_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _FeeAccumulator.Contract.DEFAULTADMINROLE(&_FeeAccumulator.CallOpts)
}

// DEFAULTADMINROLE is a free data retrieval call binding the contract method 0xa217fddf.
//
// Solidity: function DEFAULT_ADMIN_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCallerSession) DEFAULTADMINROLE() ([32]byte, error) {
	return _FeeAccumulator.Contract.DEFAULTADMINROLE(&_FeeAccumulator.CallOpts)
}

// FEESROUTERROLE is a free data retrieval call binding the contract method 0xdfb76d70.
//
// Solidity: function FEES_ROUTER_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCaller) FEESROUTERROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "FEES_ROUTER_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// FEESROUTERROLE is a free data retrieval call binding the contract method 0xdfb76d70.
//
// Solidity: function FEES_ROUTER_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorSession) FEESROUTERROLE() ([32]byte, error) {
	return _FeeAccumulator.Contract.FEESROUTERROLE(&_FeeAccumulator.CallOpts)
}

// FEESROUTERROLE is a free data retrieval call binding the contract method 0xdfb76d70.
//
// Solidity: function FEES_ROUTER_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCallerSession) FEESROUTERROLE() ([32]byte, error) {
	return _FeeAccumulator.Contract.FEESROUTERROLE(&_FeeAccumulator.CallOpts)
}

// POOLROLE is a free data retrieval call binding the contract method 0x404ccd07.
//
// Solidity: function POOL_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCaller) POOLROLE(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "POOL_ROLE")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// POOLROLE is a free data retrieval call binding the contract method 0x404ccd07.
//
// Solidity: function POOL_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorSession) POOLROLE() ([32]byte, error) {
	return _FeeAccumulator.Contract.POOLROLE(&_FeeAccumulator.CallOpts)
}

// POOLROLE is a free data retrieval call binding the contract method 0x404ccd07.
//
// Solidity: function POOL_ROLE() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCallerSession) POOLROLE() ([32]byte, error) {
	return _FeeAccumulator.Contract.POOLROLE(&_FeeAccumulator.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCaller) EventEmitter(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "eventEmitter")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_FeeAccumulator *FeeAccumulatorSession) EventEmitter() (common.Address, error) {
	return _FeeAccumulator.Contract.EventEmitter(&_FeeAccumulator.CallOpts)
}

// EventEmitter is a free data retrieval call binding the contract method 0x9ff78c30.
//
// Solidity: function eventEmitter() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCallerSession) EventEmitter() (common.Address, error) {
	return _FeeAccumulator.Contract.EventEmitter(&_FeeAccumulator.CallOpts)
}

// GetAccumulatedFees is a free data retrieval call binding the contract method 0x564201f9.
//
// Solidity: function getAccumulatedFees(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCaller) GetAccumulatedFees(opts *bind.CallOpts, pool common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "getAccumulatedFees", pool)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetAccumulatedFees is a free data retrieval call binding the contract method 0x564201f9.
//
// Solidity: function getAccumulatedFees(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorSession) GetAccumulatedFees(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetAccumulatedFees(&_FeeAccumulator.CallOpts, pool)
}

// GetAccumulatedFees is a free data retrieval call binding the contract method 0x564201f9.
//
// Solidity: function getAccumulatedFees(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCallerSession) GetAccumulatedFees(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetAccumulatedFees(&_FeeAccumulator.CallOpts, pool)
}

// GetAirdropBalance is a free data retrieval call binding the contract method 0xefa94d49.
//
// Solidity: function getAirdropBalance(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCaller) GetAirdropBalance(opts *bind.CallOpts, pool common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "getAirdropBalance", pool)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetAirdropBalance is a free data retrieval call binding the contract method 0xefa94d49.
//
// Solidity: function getAirdropBalance(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorSession) GetAirdropBalance(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetAirdropBalance(&_FeeAccumulator.CallOpts, pool)
}

// GetAirdropBalance is a free data retrieval call binding the contract method 0xefa94d49.
//
// Solidity: function getAirdropBalance(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCallerSession) GetAirdropBalance(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetAirdropBalance(&_FeeAccumulator.CallOpts, pool)
}

// GetAirdropEpoch is a free data retrieval call binding the contract method 0x9eac5b39.
//
// Solidity: function getAirdropEpoch(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCaller) GetAirdropEpoch(opts *bind.CallOpts, pool common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "getAirdropEpoch", pool)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetAirdropEpoch is a free data retrieval call binding the contract method 0x9eac5b39.
//
// Solidity: function getAirdropEpoch(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorSession) GetAirdropEpoch(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetAirdropEpoch(&_FeeAccumulator.CallOpts, pool)
}

// GetAirdropEpoch is a free data retrieval call binding the contract method 0x9eac5b39.
//
// Solidity: function getAirdropEpoch(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCallerSession) GetAirdropEpoch(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetAirdropEpoch(&_FeeAccumulator.CallOpts, pool)
}

// GetLpRewardsBalance is a free data retrieval call binding the contract method 0x81fa4ef7.
//
// Solidity: function getLpRewardsBalance(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCaller) GetLpRewardsBalance(opts *bind.CallOpts, pool common.Address) (*big.Int, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "getLpRewardsBalance", pool)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetLpRewardsBalance is a free data retrieval call binding the contract method 0x81fa4ef7.
//
// Solidity: function getLpRewardsBalance(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorSession) GetLpRewardsBalance(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetLpRewardsBalance(&_FeeAccumulator.CallOpts, pool)
}

// GetLpRewardsBalance is a free data retrieval call binding the contract method 0x81fa4ef7.
//
// Solidity: function getLpRewardsBalance(address pool) view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCallerSession) GetLpRewardsBalance(pool common.Address) (*big.Int, error) {
	return _FeeAccumulator.Contract.GetLpRewardsBalance(&_FeeAccumulator.CallOpts, pool)
}

// GetProtocolFeesPending is a free data retrieval call binding the contract method 0x52b2a385.
//
// Solidity: function getProtocolFeesPending() view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCaller) GetProtocolFeesPending(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "getProtocolFeesPending")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetProtocolFeesPending is a free data retrieval call binding the contract method 0x52b2a385.
//
// Solidity: function getProtocolFeesPending() view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorSession) GetProtocolFeesPending() (*big.Int, error) {
	return _FeeAccumulator.Contract.GetProtocolFeesPending(&_FeeAccumulator.CallOpts)
}

// GetProtocolFeesPending is a free data retrieval call binding the contract method 0x52b2a385.
//
// Solidity: function getProtocolFeesPending() view returns(uint256)
func (_FeeAccumulator *FeeAccumulatorCallerSession) GetProtocolFeesPending() (*big.Int, error) {
	return _FeeAccumulator.Contract.GetProtocolFeesPending(&_FeeAccumulator.CallOpts)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCaller) GetRoleAdmin(opts *bind.CallOpts, role [32]byte) ([32]byte, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "getRoleAdmin", role)

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _FeeAccumulator.Contract.GetRoleAdmin(&_FeeAccumulator.CallOpts, role)
}

// GetRoleAdmin is a free data retrieval call binding the contract method 0x248a9ca3.
//
// Solidity: function getRoleAdmin(bytes32 role) view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCallerSession) GetRoleAdmin(role [32]byte) ([32]byte, error) {
	return _FeeAccumulator.Contract.GetRoleAdmin(&_FeeAccumulator.CallOpts, role)
}

// HasClaimedAirdrop is a free data retrieval call binding the contract method 0x2c959e18.
//
// Solidity: function hasClaimedAirdrop(address pool, address holder, uint256 epoch) view returns(bool)
func (_FeeAccumulator *FeeAccumulatorCaller) HasClaimedAirdrop(opts *bind.CallOpts, pool common.Address, holder common.Address, epoch *big.Int) (bool, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "hasClaimedAirdrop", pool, holder, epoch)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasClaimedAirdrop is a free data retrieval call binding the contract method 0x2c959e18.
//
// Solidity: function hasClaimedAirdrop(address pool, address holder, uint256 epoch) view returns(bool)
func (_FeeAccumulator *FeeAccumulatorSession) HasClaimedAirdrop(pool common.Address, holder common.Address, epoch *big.Int) (bool, error) {
	return _FeeAccumulator.Contract.HasClaimedAirdrop(&_FeeAccumulator.CallOpts, pool, holder, epoch)
}

// HasClaimedAirdrop is a free data retrieval call binding the contract method 0x2c959e18.
//
// Solidity: function hasClaimedAirdrop(address pool, address holder, uint256 epoch) view returns(bool)
func (_FeeAccumulator *FeeAccumulatorCallerSession) HasClaimedAirdrop(pool common.Address, holder common.Address, epoch *big.Int) (bool, error) {
	return _FeeAccumulator.Contract.HasClaimedAirdrop(&_FeeAccumulator.CallOpts, pool, holder, epoch)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_FeeAccumulator *FeeAccumulatorCaller) HasRole(opts *bind.CallOpts, role [32]byte, account common.Address) (bool, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "hasRole", role, account)

	if err != nil {
		return *new(bool), err
	}

	out0 := *abi.ConvertType(out[0], new(bool)).(*bool)

	return out0, err

}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_FeeAccumulator *FeeAccumulatorSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _FeeAccumulator.Contract.HasRole(&_FeeAccumulator.CallOpts, role, account)
}

// HasRole is a free data retrieval call binding the contract method 0x91d14854.
//
// Solidity: function hasRole(bytes32 role, address account) view returns(bool)
func (_FeeAccumulator *FeeAccumulatorCallerSession) HasRole(role [32]byte, account common.Address) (bool, error) {
	return _FeeAccumulator.Contract.HasRole(&_FeeAccumulator.CallOpts, role, account)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCaller) PoolRegistry(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "poolRegistry")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_FeeAccumulator *FeeAccumulatorSession) PoolRegistry() (common.Address, error) {
	return _FeeAccumulator.Contract.PoolRegistry(&_FeeAccumulator.CallOpts)
}

// PoolRegistry is a free data retrieval call binding the contract method 0xafcff50f.
//
// Solidity: function poolRegistry() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCallerSession) PoolRegistry() (common.Address, error) {
	return _FeeAccumulator.Contract.PoolRegistry(&_FeeAccumulator.CallOpts)
}

// ProtocolConfig is a free data retrieval call binding the contract method 0xf5efbb4f.
//
// Solidity: function protocolConfig() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCaller) ProtocolConfig(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "protocolConfig")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// ProtocolConfig is a free data retrieval call binding the contract method 0xf5efbb4f.
//
// Solidity: function protocolConfig() view returns(address)
func (_FeeAccumulator *FeeAccumulatorSession) ProtocolConfig() (common.Address, error) {
	return _FeeAccumulator.Contract.ProtocolConfig(&_FeeAccumulator.CallOpts)
}

// ProtocolConfig is a free data retrieval call binding the contract method 0xf5efbb4f.
//
// Solidity: function protocolConfig() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCallerSession) ProtocolConfig() (common.Address, error) {
	return _FeeAccumulator.Contract.ProtocolConfig(&_FeeAccumulator.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCaller) ProxiableUUID(opts *bind.CallOpts) ([32]byte, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "proxiableUUID")

	if err != nil {
		return *new([32]byte), err
	}

	out0 := *abi.ConvertType(out[0], new([32]byte)).(*[32]byte)

	return out0, err

}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorSession) ProxiableUUID() ([32]byte, error) {
	return _FeeAccumulator.Contract.ProxiableUUID(&_FeeAccumulator.CallOpts)
}

// ProxiableUUID is a free data retrieval call binding the contract method 0x52d1902d.
//
// Solidity: function proxiableUUID() view returns(bytes32)
func (_FeeAccumulator *FeeAccumulatorCallerSession) ProxiableUUID() ([32]byte, error) {
	return _FeeAccumulator.Contract.ProxiableUUID(&_FeeAccumulator.CallOpts)
}

// Treasury is a free data retrieval call binding the contract method 0x61d027b3.
//
// Solidity: function treasury() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCaller) Treasury(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "treasury")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Treasury is a free data retrieval call binding the contract method 0x61d027b3.
//
// Solidity: function treasury() view returns(address)
func (_FeeAccumulator *FeeAccumulatorSession) Treasury() (common.Address, error) {
	return _FeeAccumulator.Contract.Treasury(&_FeeAccumulator.CallOpts)
}

// Treasury is a free data retrieval call binding the contract method 0x61d027b3.
//
// Solidity: function treasury() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCallerSession) Treasury() (common.Address, error) {
	return _FeeAccumulator.Contract.Treasury(&_FeeAccumulator.CallOpts)
}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCaller) UsdlAddress(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _FeeAccumulator.contract.Call(opts, &out, "usdlAddress")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_FeeAccumulator *FeeAccumulatorSession) UsdlAddress() (common.Address, error) {
	return _FeeAccumulator.Contract.UsdlAddress(&_FeeAccumulator.CallOpts)
}

// UsdlAddress is a free data retrieval call binding the contract method 0x2e10f976.
//
// Solidity: function usdlAddress() view returns(address)
func (_FeeAccumulator *FeeAccumulatorCallerSession) UsdlAddress() (common.Address, error) {
	return _FeeAccumulator.Contract.UsdlAddress(&_FeeAccumulator.CallOpts)
}

// Burn is a paid mutator transaction binding the contract method 0x89afcb44.
//
// Solidity: function burn(address pool) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactor) Burn(opts *bind.TransactOpts, pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "burn", pool)
}

// Burn is a paid mutator transaction binding the contract method 0x89afcb44.
//
// Solidity: function burn(address pool) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorSession) Burn(pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.Burn(&_FeeAccumulator.TransactOpts, pool)
}

// Burn is a paid mutator transaction binding the contract method 0x89afcb44.
//
// Solidity: function burn(address pool) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactorSession) Burn(pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.Burn(&_FeeAccumulator.TransactOpts, pool)
}

// Claim is a paid mutator transaction binding the contract method 0x21c0b342.
//
// Solidity: function claim(address pool, address recipient) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactor) Claim(opts *bind.TransactOpts, pool common.Address, recipient common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "claim", pool, recipient)
}

// Claim is a paid mutator transaction binding the contract method 0x21c0b342.
//
// Solidity: function claim(address pool, address recipient) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorSession) Claim(pool common.Address, recipient common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.Claim(&_FeeAccumulator.TransactOpts, pool, recipient)
}

// Claim is a paid mutator transaction binding the contract method 0x21c0b342.
//
// Solidity: function claim(address pool, address recipient) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactorSession) Claim(pool common.Address, recipient common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.Claim(&_FeeAccumulator.TransactOpts, pool, recipient)
}

// ClaimAirdrop is a paid mutator transaction binding the contract method 0x66e1b755.
//
// Solidity: function claimAirdrop(address pool, address holder) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactor) ClaimAirdrop(opts *bind.TransactOpts, pool common.Address, holder common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "claimAirdrop", pool, holder)
}

// ClaimAirdrop is a paid mutator transaction binding the contract method 0x66e1b755.
//
// Solidity: function claimAirdrop(address pool, address holder) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorSession) ClaimAirdrop(pool common.Address, holder common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.ClaimAirdrop(&_FeeAccumulator.TransactOpts, pool, holder)
}

// ClaimAirdrop is a paid mutator transaction binding the contract method 0x66e1b755.
//
// Solidity: function claimAirdrop(address pool, address holder) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactorSession) ClaimAirdrop(pool common.Address, holder common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.ClaimAirdrop(&_FeeAccumulator.TransactOpts, pool, holder)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_FeeAccumulator *FeeAccumulatorTransactor) GrantRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "grantRole", role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_FeeAccumulator *FeeAccumulatorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.GrantRole(&_FeeAccumulator.TransactOpts, role, account)
}

// GrantRole is a paid mutator transaction binding the contract method 0x2f2ff15d.
//
// Solidity: function grantRole(bytes32 role, address account) returns()
func (_FeeAccumulator *FeeAccumulatorTransactorSession) GrantRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.GrantRole(&_FeeAccumulator.TransactOpts, role, account)
}

// Initialize is a paid mutator transaction binding the contract method 0xcc2a9a5b.
//
// Solidity: function initialize(address _protocolConfig, address _treasury, address _poolRegistry, address _eventEmitter, address _usdlAddress, address _admin) returns()
func (_FeeAccumulator *FeeAccumulatorTransactor) Initialize(opts *bind.TransactOpts, _protocolConfig common.Address, _treasury common.Address, _poolRegistry common.Address, _eventEmitter common.Address, _usdlAddress common.Address, _admin common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "initialize", _protocolConfig, _treasury, _poolRegistry, _eventEmitter, _usdlAddress, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xcc2a9a5b.
//
// Solidity: function initialize(address _protocolConfig, address _treasury, address _poolRegistry, address _eventEmitter, address _usdlAddress, address _admin) returns()
func (_FeeAccumulator *FeeAccumulatorSession) Initialize(_protocolConfig common.Address, _treasury common.Address, _poolRegistry common.Address, _eventEmitter common.Address, _usdlAddress common.Address, _admin common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.Initialize(&_FeeAccumulator.TransactOpts, _protocolConfig, _treasury, _poolRegistry, _eventEmitter, _usdlAddress, _admin)
}

// Initialize is a paid mutator transaction binding the contract method 0xcc2a9a5b.
//
// Solidity: function initialize(address _protocolConfig, address _treasury, address _poolRegistry, address _eventEmitter, address _usdlAddress, address _admin) returns()
func (_FeeAccumulator *FeeAccumulatorTransactorSession) Initialize(_protocolConfig common.Address, _treasury common.Address, _poolRegistry common.Address, _eventEmitter common.Address, _usdlAddress common.Address, _admin common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.Initialize(&_FeeAccumulator.TransactOpts, _protocolConfig, _treasury, _poolRegistry, _eventEmitter, _usdlAddress, _admin)
}

// RecordFee is a paid mutator transaction binding the contract method 0x0a6e0fd4.
//
// Solidity: function recordFee(address pool, uint256 feeAmount) returns()
func (_FeeAccumulator *FeeAccumulatorTransactor) RecordFee(opts *bind.TransactOpts, pool common.Address, feeAmount *big.Int) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "recordFee", pool, feeAmount)
}

// RecordFee is a paid mutator transaction binding the contract method 0x0a6e0fd4.
//
// Solidity: function recordFee(address pool, uint256 feeAmount) returns()
func (_FeeAccumulator *FeeAccumulatorSession) RecordFee(pool common.Address, feeAmount *big.Int) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.RecordFee(&_FeeAccumulator.TransactOpts, pool, feeAmount)
}

// RecordFee is a paid mutator transaction binding the contract method 0x0a6e0fd4.
//
// Solidity: function recordFee(address pool, uint256 feeAmount) returns()
func (_FeeAccumulator *FeeAccumulatorTransactorSession) RecordFee(pool common.Address, feeAmount *big.Int) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.RecordFee(&_FeeAccumulator.TransactOpts, pool, feeAmount)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_FeeAccumulator *FeeAccumulatorTransactor) RenounceRole(opts *bind.TransactOpts, role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "renounceRole", role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_FeeAccumulator *FeeAccumulatorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.RenounceRole(&_FeeAccumulator.TransactOpts, role, callerConfirmation)
}

// RenounceRole is a paid mutator transaction binding the contract method 0x36568abe.
//
// Solidity: function renounceRole(bytes32 role, address callerConfirmation) returns()
func (_FeeAccumulator *FeeAccumulatorTransactorSession) RenounceRole(role [32]byte, callerConfirmation common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.RenounceRole(&_FeeAccumulator.TransactOpts, role, callerConfirmation)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_FeeAccumulator *FeeAccumulatorTransactor) RevokeRole(opts *bind.TransactOpts, role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "revokeRole", role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_FeeAccumulator *FeeAccumulatorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.RevokeRole(&_FeeAccumulator.TransactOpts, role, account)
}

// RevokeRole is a paid mutator transaction binding the contract method 0xd547741f.
//
// Solidity: function revokeRole(bytes32 role, address account) returns()
func (_FeeAccumulator *FeeAccumulatorTransactorSession) RevokeRole(role [32]byte, account common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.RevokeRole(&_FeeAccumulator.TransactOpts, role, account)
}

// SendLpRewards is a paid mutator transaction binding the contract method 0xa48d7d07.
//
// Solidity: function sendLpRewards(address pool) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactor) SendLpRewards(opts *bind.TransactOpts, pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "sendLpRewards", pool)
}

// SendLpRewards is a paid mutator transaction binding the contract method 0xa48d7d07.
//
// Solidity: function sendLpRewards(address pool) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorSession) SendLpRewards(pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.SendLpRewards(&_FeeAccumulator.TransactOpts, pool)
}

// SendLpRewards is a paid mutator transaction binding the contract method 0xa48d7d07.
//
// Solidity: function sendLpRewards(address pool) returns(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorTransactorSession) SendLpRewards(pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.SendLpRewards(&_FeeAccumulator.TransactOpts, pool)
}

// TriggerAirdrop is a paid mutator transaction binding the contract method 0xed374d4a.
//
// Solidity: function triggerAirdrop(address pool) returns(uint256 totalAmount)
func (_FeeAccumulator *FeeAccumulatorTransactor) TriggerAirdrop(opts *bind.TransactOpts, pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "triggerAirdrop", pool)
}

// TriggerAirdrop is a paid mutator transaction binding the contract method 0xed374d4a.
//
// Solidity: function triggerAirdrop(address pool) returns(uint256 totalAmount)
func (_FeeAccumulator *FeeAccumulatorSession) TriggerAirdrop(pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.TriggerAirdrop(&_FeeAccumulator.TransactOpts, pool)
}

// TriggerAirdrop is a paid mutator transaction binding the contract method 0xed374d4a.
//
// Solidity: function triggerAirdrop(address pool) returns(uint256 totalAmount)
func (_FeeAccumulator *FeeAccumulatorTransactorSession) TriggerAirdrop(pool common.Address) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.TriggerAirdrop(&_FeeAccumulator.TransactOpts, pool)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FeeAccumulator *FeeAccumulatorTransactor) UpgradeToAndCall(opts *bind.TransactOpts, newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FeeAccumulator.contract.Transact(opts, "upgradeToAndCall", newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FeeAccumulator *FeeAccumulatorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.UpgradeToAndCall(&_FeeAccumulator.TransactOpts, newImplementation, data)
}

// UpgradeToAndCall is a paid mutator transaction binding the contract method 0x4f1ef286.
//
// Solidity: function upgradeToAndCall(address newImplementation, bytes data) payable returns()
func (_FeeAccumulator *FeeAccumulatorTransactorSession) UpgradeToAndCall(newImplementation common.Address, data []byte) (*types.Transaction, error) {
	return _FeeAccumulator.Contract.UpgradeToAndCall(&_FeeAccumulator.TransactOpts, newImplementation, data)
}

// FeeAccumulatorAirdropClaimedIterator is returned from FilterAirdropClaimed and is used to iterate over the raw logs and unpacked data for AirdropClaimed events raised by the FeeAccumulator contract.
type FeeAccumulatorAirdropClaimedIterator struct {
	Event *FeeAccumulatorAirdropClaimed // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorAirdropClaimedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorAirdropClaimed)
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
		it.Event = new(FeeAccumulatorAirdropClaimed)
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
func (it *FeeAccumulatorAirdropClaimedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorAirdropClaimedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorAirdropClaimed represents a AirdropClaimed event raised by the FeeAccumulator contract.
type FeeAccumulatorAirdropClaimed struct {
	Pool   common.Address
	Holder common.Address
	Amount *big.Int
	Epoch  *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterAirdropClaimed is a free log retrieval operation binding the contract event 0xd399c6e7fad358fc300beda3f056717c94a04c7233ce92683de6500ba509022e.
//
// Solidity: event AirdropClaimed(address indexed pool, address indexed holder, uint256 amount, uint256 epoch)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterAirdropClaimed(opts *bind.FilterOpts, pool []common.Address, holder []common.Address) (*FeeAccumulatorAirdropClaimedIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var holderRule []interface{}
	for _, holderItem := range holder {
		holderRule = append(holderRule, holderItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "AirdropClaimed", poolRule, holderRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorAirdropClaimedIterator{contract: _FeeAccumulator.contract, event: "AirdropClaimed", logs: logs, sub: sub}, nil
}

// WatchAirdropClaimed is a free log subscription operation binding the contract event 0xd399c6e7fad358fc300beda3f056717c94a04c7233ce92683de6500ba509022e.
//
// Solidity: event AirdropClaimed(address indexed pool, address indexed holder, uint256 amount, uint256 epoch)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchAirdropClaimed(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorAirdropClaimed, pool []common.Address, holder []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var holderRule []interface{}
	for _, holderItem := range holder {
		holderRule = append(holderRule, holderItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "AirdropClaimed", poolRule, holderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorAirdropClaimed)
				if err := _FeeAccumulator.contract.UnpackLog(event, "AirdropClaimed", log); err != nil {
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

// ParseAirdropClaimed is a log parse operation binding the contract event 0xd399c6e7fad358fc300beda3f056717c94a04c7233ce92683de6500ba509022e.
//
// Solidity: event AirdropClaimed(address indexed pool, address indexed holder, uint256 amount, uint256 epoch)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseAirdropClaimed(log types.Log) (*FeeAccumulatorAirdropClaimed, error) {
	event := new(FeeAccumulatorAirdropClaimed)
	if err := _FeeAccumulator.contract.UnpackLog(event, "AirdropClaimed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorAirdropTriggeredIterator is returned from FilterAirdropTriggered and is used to iterate over the raw logs and unpacked data for AirdropTriggered events raised by the FeeAccumulator contract.
type FeeAccumulatorAirdropTriggeredIterator struct {
	Event *FeeAccumulatorAirdropTriggered // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorAirdropTriggeredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorAirdropTriggered)
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
		it.Event = new(FeeAccumulatorAirdropTriggered)
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
func (it *FeeAccumulatorAirdropTriggeredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorAirdropTriggeredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorAirdropTriggered represents a AirdropTriggered event raised by the FeeAccumulator contract.
type FeeAccumulatorAirdropTriggered struct {
	Pool        common.Address
	TotalAmount *big.Int
	Epoch       *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterAirdropTriggered is a free log retrieval operation binding the contract event 0x8c39ea0d52c00d579184e8070640104228f1a618d77e55429e09cfbb049c2dc1.
//
// Solidity: event AirdropTriggered(address indexed pool, uint256 totalAmount, uint256 epoch)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterAirdropTriggered(opts *bind.FilterOpts, pool []common.Address) (*FeeAccumulatorAirdropTriggeredIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "AirdropTriggered", poolRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorAirdropTriggeredIterator{contract: _FeeAccumulator.contract, event: "AirdropTriggered", logs: logs, sub: sub}, nil
}

// WatchAirdropTriggered is a free log subscription operation binding the contract event 0x8c39ea0d52c00d579184e8070640104228f1a618d77e55429e09cfbb049c2dc1.
//
// Solidity: event AirdropTriggered(address indexed pool, uint256 totalAmount, uint256 epoch)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchAirdropTriggered(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorAirdropTriggered, pool []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "AirdropTriggered", poolRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorAirdropTriggered)
				if err := _FeeAccumulator.contract.UnpackLog(event, "AirdropTriggered", log); err != nil {
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

// ParseAirdropTriggered is a log parse operation binding the contract event 0x8c39ea0d52c00d579184e8070640104228f1a618d77e55429e09cfbb049c2dc1.
//
// Solidity: event AirdropTriggered(address indexed pool, uint256 totalAmount, uint256 epoch)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseAirdropTriggered(log types.Log) (*FeeAccumulatorAirdropTriggered, error) {
	event := new(FeeAccumulatorAirdropTriggered)
	if err := _FeeAccumulator.contract.UnpackLog(event, "AirdropTriggered", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorFeeRecordedIterator is returned from FilterFeeRecorded and is used to iterate over the raw logs and unpacked data for FeeRecorded events raised by the FeeAccumulator contract.
type FeeAccumulatorFeeRecordedIterator struct {
	Event *FeeAccumulatorFeeRecorded // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorFeeRecordedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorFeeRecorded)
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
		it.Event = new(FeeAccumulatorFeeRecorded)
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
func (it *FeeAccumulatorFeeRecordedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorFeeRecordedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorFeeRecorded represents a FeeRecorded event raised by the FeeAccumulator contract.
type FeeAccumulatorFeeRecorded struct {
	Pool        common.Address
	FeeAmount   *big.Int
	ProtocolCut *big.Int
	PoolCut     *big.Int
	Raw         types.Log // Blockchain specific contextual infos
}

// FilterFeeRecorded is a free log retrieval operation binding the contract event 0xb4d4d3bd2f97a7d6f1657ee69f7191d7aa7dbd5b6864a2d7a9d14efc1322552f.
//
// Solidity: event FeeRecorded(address indexed pool, uint256 feeAmount, uint256 protocolCut, uint256 poolCut)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterFeeRecorded(opts *bind.FilterOpts, pool []common.Address) (*FeeAccumulatorFeeRecordedIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "FeeRecorded", poolRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorFeeRecordedIterator{contract: _FeeAccumulator.contract, event: "FeeRecorded", logs: logs, sub: sub}, nil
}

// WatchFeeRecorded is a free log subscription operation binding the contract event 0xb4d4d3bd2f97a7d6f1657ee69f7191d7aa7dbd5b6864a2d7a9d14efc1322552f.
//
// Solidity: event FeeRecorded(address indexed pool, uint256 feeAmount, uint256 protocolCut, uint256 poolCut)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchFeeRecorded(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorFeeRecorded, pool []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "FeeRecorded", poolRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorFeeRecorded)
				if err := _FeeAccumulator.contract.UnpackLog(event, "FeeRecorded", log); err != nil {
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

// ParseFeeRecorded is a log parse operation binding the contract event 0xb4d4d3bd2f97a7d6f1657ee69f7191d7aa7dbd5b6864a2d7a9d14efc1322552f.
//
// Solidity: event FeeRecorded(address indexed pool, uint256 feeAmount, uint256 protocolCut, uint256 poolCut)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseFeeRecorded(log types.Log) (*FeeAccumulatorFeeRecorded, error) {
	event := new(FeeAccumulatorFeeRecorded)
	if err := _FeeAccumulator.contract.UnpackLog(event, "FeeRecorded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorFeesBurnedIterator is returned from FilterFeesBurned and is used to iterate over the raw logs and unpacked data for FeesBurned events raised by the FeeAccumulator contract.
type FeeAccumulatorFeesBurnedIterator struct {
	Event *FeeAccumulatorFeesBurned // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorFeesBurnedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorFeesBurned)
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
		it.Event = new(FeeAccumulatorFeesBurned)
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
func (it *FeeAccumulatorFeesBurnedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorFeesBurnedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorFeesBurned represents a FeesBurned event raised by the FeeAccumulator contract.
type FeeAccumulatorFeesBurned struct {
	Pool   common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterFeesBurned is a free log retrieval operation binding the contract event 0x0d9575a73e2a7da16cfde907df749d23d901528ff2e7c832b731babdecca000b.
//
// Solidity: event FeesBurned(address indexed pool, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterFeesBurned(opts *bind.FilterOpts, pool []common.Address) (*FeeAccumulatorFeesBurnedIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "FeesBurned", poolRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorFeesBurnedIterator{contract: _FeeAccumulator.contract, event: "FeesBurned", logs: logs, sub: sub}, nil
}

// WatchFeesBurned is a free log subscription operation binding the contract event 0x0d9575a73e2a7da16cfde907df749d23d901528ff2e7c832b731babdecca000b.
//
// Solidity: event FeesBurned(address indexed pool, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchFeesBurned(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorFeesBurned, pool []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "FeesBurned", poolRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorFeesBurned)
				if err := _FeeAccumulator.contract.UnpackLog(event, "FeesBurned", log); err != nil {
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

// ParseFeesBurned is a log parse operation binding the contract event 0x0d9575a73e2a7da16cfde907df749d23d901528ff2e7c832b731babdecca000b.
//
// Solidity: event FeesBurned(address indexed pool, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseFeesBurned(log types.Log) (*FeeAccumulatorFeesBurned, error) {
	event := new(FeeAccumulatorFeesBurned)
	if err := _FeeAccumulator.contract.UnpackLog(event, "FeesBurned", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorFeesClaimedIterator is returned from FilterFeesClaimed and is used to iterate over the raw logs and unpacked data for FeesClaimed events raised by the FeeAccumulator contract.
type FeeAccumulatorFeesClaimedIterator struct {
	Event *FeeAccumulatorFeesClaimed // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorFeesClaimedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorFeesClaimed)
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
		it.Event = new(FeeAccumulatorFeesClaimed)
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
func (it *FeeAccumulatorFeesClaimedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorFeesClaimedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorFeesClaimed represents a FeesClaimed event raised by the FeeAccumulator contract.
type FeeAccumulatorFeesClaimed struct {
	Pool      common.Address
	Recipient common.Address
	Amount    *big.Int
	Raw       types.Log // Blockchain specific contextual infos
}

// FilterFeesClaimed is a free log retrieval operation binding the contract event 0xfe3464cd748424446c37877c28ce5b700222c5bc9f90d908afcc4e5cb22707ff.
//
// Solidity: event FeesClaimed(address indexed pool, address indexed recipient, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterFeesClaimed(opts *bind.FilterOpts, pool []common.Address, recipient []common.Address) (*FeeAccumulatorFeesClaimedIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var recipientRule []interface{}
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "FeesClaimed", poolRule, recipientRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorFeesClaimedIterator{contract: _FeeAccumulator.contract, event: "FeesClaimed", logs: logs, sub: sub}, nil
}

// WatchFeesClaimed is a free log subscription operation binding the contract event 0xfe3464cd748424446c37877c28ce5b700222c5bc9f90d908afcc4e5cb22707ff.
//
// Solidity: event FeesClaimed(address indexed pool, address indexed recipient, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchFeesClaimed(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorFeesClaimed, pool []common.Address, recipient []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}
	var recipientRule []interface{}
	for _, recipientItem := range recipient {
		recipientRule = append(recipientRule, recipientItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "FeesClaimed", poolRule, recipientRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorFeesClaimed)
				if err := _FeeAccumulator.contract.UnpackLog(event, "FeesClaimed", log); err != nil {
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

// ParseFeesClaimed is a log parse operation binding the contract event 0xfe3464cd748424446c37877c28ce5b700222c5bc9f90d908afcc4e5cb22707ff.
//
// Solidity: event FeesClaimed(address indexed pool, address indexed recipient, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseFeesClaimed(log types.Log) (*FeeAccumulatorFeesClaimed, error) {
	event := new(FeeAccumulatorFeesClaimed)
	if err := _FeeAccumulator.contract.UnpackLog(event, "FeesClaimed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorInitializedIterator is returned from FilterInitialized and is used to iterate over the raw logs and unpacked data for Initialized events raised by the FeeAccumulator contract.
type FeeAccumulatorInitializedIterator struct {
	Event *FeeAccumulatorInitialized // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorInitializedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorInitialized)
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
		it.Event = new(FeeAccumulatorInitialized)
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
func (it *FeeAccumulatorInitializedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorInitializedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorInitialized represents a Initialized event raised by the FeeAccumulator contract.
type FeeAccumulatorInitialized struct {
	Version uint64
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterInitialized is a free log retrieval operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterInitialized(opts *bind.FilterOpts) (*FeeAccumulatorInitializedIterator, error) {

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorInitializedIterator{contract: _FeeAccumulator.contract, event: "Initialized", logs: logs, sub: sub}, nil
}

// WatchInitialized is a free log subscription operation binding the contract event 0xc7f505b2f371ae2175ee4913f4499e1f2633a7b5936321eed1cdaeb6115181d2.
//
// Solidity: event Initialized(uint64 version)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchInitialized(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorInitialized) (event.Subscription, error) {

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "Initialized")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorInitialized)
				if err := _FeeAccumulator.contract.UnpackLog(event, "Initialized", log); err != nil {
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
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseInitialized(log types.Log) (*FeeAccumulatorInitialized, error) {
	event := new(FeeAccumulatorInitialized)
	if err := _FeeAccumulator.contract.UnpackLog(event, "Initialized", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorLpRewardsSentIterator is returned from FilterLpRewardsSent and is used to iterate over the raw logs and unpacked data for LpRewardsSent events raised by the FeeAccumulator contract.
type FeeAccumulatorLpRewardsSentIterator struct {
	Event *FeeAccumulatorLpRewardsSent // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorLpRewardsSentIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorLpRewardsSent)
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
		it.Event = new(FeeAccumulatorLpRewardsSent)
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
func (it *FeeAccumulatorLpRewardsSentIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorLpRewardsSentIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorLpRewardsSent represents a LpRewardsSent event raised by the FeeAccumulator contract.
type FeeAccumulatorLpRewardsSent struct {
	Pool   common.Address
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterLpRewardsSent is a free log retrieval operation binding the contract event 0x86b634da567bed9efe3e74597f5b2f7c9a880ed2e05e28f4c7c2fae1f0a9cfdb.
//
// Solidity: event LpRewardsSent(address indexed pool, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterLpRewardsSent(opts *bind.FilterOpts, pool []common.Address) (*FeeAccumulatorLpRewardsSentIterator, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "LpRewardsSent", poolRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorLpRewardsSentIterator{contract: _FeeAccumulator.contract, event: "LpRewardsSent", logs: logs, sub: sub}, nil
}

// WatchLpRewardsSent is a free log subscription operation binding the contract event 0x86b634da567bed9efe3e74597f5b2f7c9a880ed2e05e28f4c7c2fae1f0a9cfdb.
//
// Solidity: event LpRewardsSent(address indexed pool, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchLpRewardsSent(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorLpRewardsSent, pool []common.Address) (event.Subscription, error) {

	var poolRule []interface{}
	for _, poolItem := range pool {
		poolRule = append(poolRule, poolItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "LpRewardsSent", poolRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorLpRewardsSent)
				if err := _FeeAccumulator.contract.UnpackLog(event, "LpRewardsSent", log); err != nil {
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

// ParseLpRewardsSent is a log parse operation binding the contract event 0x86b634da567bed9efe3e74597f5b2f7c9a880ed2e05e28f4c7c2fae1f0a9cfdb.
//
// Solidity: event LpRewardsSent(address indexed pool, uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseLpRewardsSent(log types.Log) (*FeeAccumulatorLpRewardsSent, error) {
	event := new(FeeAccumulatorLpRewardsSent)
	if err := _FeeAccumulator.contract.UnpackLog(event, "LpRewardsSent", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorProtocolFeeSweptIterator is returned from FilterProtocolFeeSwept and is used to iterate over the raw logs and unpacked data for ProtocolFeeSwept events raised by the FeeAccumulator contract.
type FeeAccumulatorProtocolFeeSweptIterator struct {
	Event *FeeAccumulatorProtocolFeeSwept // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorProtocolFeeSweptIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorProtocolFeeSwept)
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
		it.Event = new(FeeAccumulatorProtocolFeeSwept)
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
func (it *FeeAccumulatorProtocolFeeSweptIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorProtocolFeeSweptIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorProtocolFeeSwept represents a ProtocolFeeSwept event raised by the FeeAccumulator contract.
type FeeAccumulatorProtocolFeeSwept struct {
	Amount *big.Int
	Raw    types.Log // Blockchain specific contextual infos
}

// FilterProtocolFeeSwept is a free log retrieval operation binding the contract event 0xe50aaa1965bcc0030c0dedb0b575635e1da17cf4989a55fa0f691ab35ff7ff13.
//
// Solidity: event ProtocolFeeSwept(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterProtocolFeeSwept(opts *bind.FilterOpts) (*FeeAccumulatorProtocolFeeSweptIterator, error) {

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "ProtocolFeeSwept")
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorProtocolFeeSweptIterator{contract: _FeeAccumulator.contract, event: "ProtocolFeeSwept", logs: logs, sub: sub}, nil
}

// WatchProtocolFeeSwept is a free log subscription operation binding the contract event 0xe50aaa1965bcc0030c0dedb0b575635e1da17cf4989a55fa0f691ab35ff7ff13.
//
// Solidity: event ProtocolFeeSwept(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchProtocolFeeSwept(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorProtocolFeeSwept) (event.Subscription, error) {

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "ProtocolFeeSwept")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorProtocolFeeSwept)
				if err := _FeeAccumulator.contract.UnpackLog(event, "ProtocolFeeSwept", log); err != nil {
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

// ParseProtocolFeeSwept is a log parse operation binding the contract event 0xe50aaa1965bcc0030c0dedb0b575635e1da17cf4989a55fa0f691ab35ff7ff13.
//
// Solidity: event ProtocolFeeSwept(uint256 amount)
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseProtocolFeeSwept(log types.Log) (*FeeAccumulatorProtocolFeeSwept, error) {
	event := new(FeeAccumulatorProtocolFeeSwept)
	if err := _FeeAccumulator.contract.UnpackLog(event, "ProtocolFeeSwept", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorRoleAdminChangedIterator is returned from FilterRoleAdminChanged and is used to iterate over the raw logs and unpacked data for RoleAdminChanged events raised by the FeeAccumulator contract.
type FeeAccumulatorRoleAdminChangedIterator struct {
	Event *FeeAccumulatorRoleAdminChanged // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorRoleAdminChangedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorRoleAdminChanged)
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
		it.Event = new(FeeAccumulatorRoleAdminChanged)
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
func (it *FeeAccumulatorRoleAdminChangedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorRoleAdminChangedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorRoleAdminChanged represents a RoleAdminChanged event raised by the FeeAccumulator contract.
type FeeAccumulatorRoleAdminChanged struct {
	Role              [32]byte
	PreviousAdminRole [32]byte
	NewAdminRole      [32]byte
	Raw               types.Log // Blockchain specific contextual infos
}

// FilterRoleAdminChanged is a free log retrieval operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterRoleAdminChanged(opts *bind.FilterOpts, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (*FeeAccumulatorRoleAdminChangedIterator, error) {

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

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorRoleAdminChangedIterator{contract: _FeeAccumulator.contract, event: "RoleAdminChanged", logs: logs, sub: sub}, nil
}

// WatchRoleAdminChanged is a free log subscription operation binding the contract event 0xbd79b86ffe0ab8e8776151514217cd7cacd52c909f66475c3af44e129f0b00ff.
//
// Solidity: event RoleAdminChanged(bytes32 indexed role, bytes32 indexed previousAdminRole, bytes32 indexed newAdminRole)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchRoleAdminChanged(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorRoleAdminChanged, role [][32]byte, previousAdminRole [][32]byte, newAdminRole [][32]byte) (event.Subscription, error) {

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

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "RoleAdminChanged", roleRule, previousAdminRoleRule, newAdminRoleRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorRoleAdminChanged)
				if err := _FeeAccumulator.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
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
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseRoleAdminChanged(log types.Log) (*FeeAccumulatorRoleAdminChanged, error) {
	event := new(FeeAccumulatorRoleAdminChanged)
	if err := _FeeAccumulator.contract.UnpackLog(event, "RoleAdminChanged", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorRoleGrantedIterator is returned from FilterRoleGranted and is used to iterate over the raw logs and unpacked data for RoleGranted events raised by the FeeAccumulator contract.
type FeeAccumulatorRoleGrantedIterator struct {
	Event *FeeAccumulatorRoleGranted // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorRoleGrantedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorRoleGranted)
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
		it.Event = new(FeeAccumulatorRoleGranted)
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
func (it *FeeAccumulatorRoleGrantedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorRoleGrantedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorRoleGranted represents a RoleGranted event raised by the FeeAccumulator contract.
type FeeAccumulatorRoleGranted struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleGranted is a free log retrieval operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterRoleGranted(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*FeeAccumulatorRoleGrantedIterator, error) {

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

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorRoleGrantedIterator{contract: _FeeAccumulator.contract, event: "RoleGranted", logs: logs, sub: sub}, nil
}

// WatchRoleGranted is a free log subscription operation binding the contract event 0x2f8788117e7eff1d82e926ec794901d17c78024a50270940304540a733656f0d.
//
// Solidity: event RoleGranted(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchRoleGranted(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorRoleGranted, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "RoleGranted", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorRoleGranted)
				if err := _FeeAccumulator.contract.UnpackLog(event, "RoleGranted", log); err != nil {
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
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseRoleGranted(log types.Log) (*FeeAccumulatorRoleGranted, error) {
	event := new(FeeAccumulatorRoleGranted)
	if err := _FeeAccumulator.contract.UnpackLog(event, "RoleGranted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorRoleRevokedIterator is returned from FilterRoleRevoked and is used to iterate over the raw logs and unpacked data for RoleRevoked events raised by the FeeAccumulator contract.
type FeeAccumulatorRoleRevokedIterator struct {
	Event *FeeAccumulatorRoleRevoked // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorRoleRevokedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorRoleRevoked)
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
		it.Event = new(FeeAccumulatorRoleRevoked)
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
func (it *FeeAccumulatorRoleRevokedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorRoleRevokedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorRoleRevoked represents a RoleRevoked event raised by the FeeAccumulator contract.
type FeeAccumulatorRoleRevoked struct {
	Role    [32]byte
	Account common.Address
	Sender  common.Address
	Raw     types.Log // Blockchain specific contextual infos
}

// FilterRoleRevoked is a free log retrieval operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterRoleRevoked(opts *bind.FilterOpts, role [][32]byte, account []common.Address, sender []common.Address) (*FeeAccumulatorRoleRevokedIterator, error) {

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

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorRoleRevokedIterator{contract: _FeeAccumulator.contract, event: "RoleRevoked", logs: logs, sub: sub}, nil
}

// WatchRoleRevoked is a free log subscription operation binding the contract event 0xf6391f5c32d9c69d2a47ea670b442974b53935d1edc7fd64eb21e047a839171b.
//
// Solidity: event RoleRevoked(bytes32 indexed role, address indexed account, address indexed sender)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchRoleRevoked(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorRoleRevoked, role [][32]byte, account []common.Address, sender []common.Address) (event.Subscription, error) {

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

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "RoleRevoked", roleRule, accountRule, senderRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorRoleRevoked)
				if err := _FeeAccumulator.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
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
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseRoleRevoked(log types.Log) (*FeeAccumulatorRoleRevoked, error) {
	event := new(FeeAccumulatorRoleRevoked)
	if err := _FeeAccumulator.contract.UnpackLog(event, "RoleRevoked", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// FeeAccumulatorUpgradedIterator is returned from FilterUpgraded and is used to iterate over the raw logs and unpacked data for Upgraded events raised by the FeeAccumulator contract.
type FeeAccumulatorUpgradedIterator struct {
	Event *FeeAccumulatorUpgraded // Event containing the contract specifics and raw log

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
func (it *FeeAccumulatorUpgradedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(FeeAccumulatorUpgraded)
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
		it.Event = new(FeeAccumulatorUpgraded)
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
func (it *FeeAccumulatorUpgradedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *FeeAccumulatorUpgradedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// FeeAccumulatorUpgraded represents a Upgraded event raised by the FeeAccumulator contract.
type FeeAccumulatorUpgraded struct {
	Implementation common.Address
	Raw            types.Log // Blockchain specific contextual infos
}

// FilterUpgraded is a free log retrieval operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FeeAccumulator *FeeAccumulatorFilterer) FilterUpgraded(opts *bind.FilterOpts, implementation []common.Address) (*FeeAccumulatorUpgradedIterator, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _FeeAccumulator.contract.FilterLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return &FeeAccumulatorUpgradedIterator{contract: _FeeAccumulator.contract, event: "Upgraded", logs: logs, sub: sub}, nil
}

// WatchUpgraded is a free log subscription operation binding the contract event 0xbc7cd75a20ee27fd9adebab32041f755214dbc6bffa90cc0225b39da2e5c2d3b.
//
// Solidity: event Upgraded(address indexed implementation)
func (_FeeAccumulator *FeeAccumulatorFilterer) WatchUpgraded(opts *bind.WatchOpts, sink chan<- *FeeAccumulatorUpgraded, implementation []common.Address) (event.Subscription, error) {

	var implementationRule []interface{}
	for _, implementationItem := range implementation {
		implementationRule = append(implementationRule, implementationItem)
	}

	logs, sub, err := _FeeAccumulator.contract.WatchLogs(opts, "Upgraded", implementationRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(FeeAccumulatorUpgraded)
				if err := _FeeAccumulator.contract.UnpackLog(event, "Upgraded", log); err != nil {
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
func (_FeeAccumulator *FeeAccumulatorFilterer) ParseUpgraded(log types.Log) (*FeeAccumulatorUpgraded, error) {
	event := new(FeeAccumulatorUpgraded)
	if err := _FeeAccumulator.contract.UnpackLog(event, "Upgraded", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
