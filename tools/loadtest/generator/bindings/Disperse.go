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

// DisperseMetaData contains all meta data concerning the Disperse contract.
var DisperseMetaData = &bind.MetaData{
	ABI: "[{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"etherAmount\",\"type\":\"uint256\"},{\"internalType\":\"uint256\",\"name\":\"tokenAmount\",\"type\":\"uint256\"}],\"stateMutability\":\"nonpayable\",\"type\":\"constructor\"},{\"inputs\":[{\"internalType\":\"address[]\",\"name\":\"recipients\",\"type\":\"address[]\"},{\"internalType\":\"uint256[]\",\"name\":\"values\",\"type\":\"uint256[]\"}],\"name\":\"disperseEther\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address[]\",\"name\":\"recipients\",\"type\":\"address[]\"}],\"name\":\"disperseEtherFixed\",\"outputs\":[],\"stateMutability\":\"payable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address[]\",\"name\":\"recipients\",\"type\":\"address[]\"},{\"internalType\":\"uint256[]\",\"name\":\"values\",\"type\":\"uint256[]\"}],\"name\":\"disperseToken\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address[]\",\"name\":\"recipients\",\"type\":\"address[]\"}],\"name\":\"disperseTokenFixed\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"contractIERC20\",\"name\":\"token\",\"type\":\"address\"},{\"internalType\":\"address[]\",\"name\":\"recipients\",\"type\":\"address[]\"},{\"internalType\":\"uint256[]\",\"name\":\"values\",\"type\":\"uint256[]\"}],\"name\":\"disperseTokenSimple\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"fixedEtherAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"fixedTokenAmount\",\"outputs\":[{\"internalType\":\"uint256\",\"name\":\"\",\"type\":\"uint256\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[],\"name\":\"owner\",\"outputs\":[{\"internalType\":\"address\",\"name\":\"\",\"type\":\"address\"}],\"stateMutability\":\"view\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"setFixedEtherAmount\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"uint256\",\"name\":\"amount\",\"type\":\"uint256\"}],\"name\":\"setFixedTokenAmount\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"},{\"inputs\":[{\"internalType\":\"address\",\"name\":\"newOwner\",\"type\":\"address\"}],\"name\":\"transferOwnership\",\"outputs\":[],\"stateMutability\":\"nonpayable\",\"type\":\"function\"}]",
	Bin: "0x608060405234801561001057600080fd5b50604051610b99380380610b9983398101604081905261002f9161004f565b600080546001600160a01b03191633179055600191909155600255610073565b6000806040838503121561006257600080fd5b505080516020909101519092909150565b610b17806100826000396000f3fe60806040526004361061009c5760003560e01c80638da5cb5b116100645780638da5cb5b1461013f578063c73a2d6014610177578063e63d38ed14610197578063e904a243146101aa578063f2fde38b146101c0578063fb169f36146101e057600080fd5b806309486423146100a1578063107520fe146100ca5780631839bcf3146100ec57806351ba162c146100ff5780635fcc7d151461011f575b600080fd5b3480156100ad57600080fd5b506100b760015481565b6040519081526020015b60405180910390f35b3480156100d657600080fd5b506100ea6100e5366004610823565b610200565b005b6100ea6100fa366004610888565b61021c565b34801561010b57600080fd5b506100ea61011a3660046108e2565b6102f0565b34801561012b57600080fd5b506100ea61013a366004610823565b6103e7565b34801561014b57600080fd5b5060005461015f906001600160a01b031681565b6040516001600160a01b0390911681526020016100c1565b34801561018357600080fd5b506100ea6101923660046108e2565b610403565b6100ea6101a5366004610965565b6105b7565b3480156101b657600080fd5b506100b760025481565b3480156101cc57600080fd5b506100ea6101db3660046109d1565b610688565b3480156101ec57600080fd5b506100ea6101fb3660046109f5565b6106c1565b6000546001600160a01b0316331461021757600080fd5b600255565b60015461022a908290610a60565b341461023557600080fd5b60005b818110156102b45782828281811061025257610252610a7d565b905060200201602081019061026791906109d1565b6001600160a01b03166108fc6001549081150290604051600060405180830381858888f193505050501580156102a1573d6000803e3d6000fd5b50806102ac81610a93565b915050610238565b504780156102eb57604051339082156108fc029083906000818181858888f193505050501580156102e9573d6000803e3d6000fd5b505b505050565b60005b838110156103df57856001600160a01b03166323b872dd3387878581811061031d5761031d610a7d565b905060200201602081019061033291906109d1565b86868681811061034457610344610a7d565b6040516001600160e01b031960e088901b1681526001600160a01b039586166004820152949093166024850152506020909102013560448201526064016020604051808303816000875af11580156103a0573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906103c49190610aac565b6103cd57600080fd5b806103d781610a93565b9150506102f3565b505050505050565b6000546001600160a01b031633146103fe57600080fd5b600155565b6000805b848110156104475783838281811061042157610421610a7d565b90506020020135826104339190610ace565b91508061043f81610a93565b915050610407565b506040516323b872dd60e01b8152336004820152306024820152604481018290526001600160a01b038716906323b872dd906064016020604051808303816000875af115801561049b573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906104bf9190610aac565b6104c857600080fd5b60005b848110156105ae57866001600160a01b031663a9059cbb8787848181106104f4576104f4610a7d565b905060200201602081019061050991906109d1565b86868581811061051b5761051b610a7d565b6040516001600160e01b031960e087901b1681526001600160a01b03909416600485015260200291909101356024830152506044016020604051808303816000875af115801561056f573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906105939190610aac565b61059c57600080fd5b806105a681610a93565b9150506104cb565b50505050505050565b60005b8381101561064c578484828181106105d4576105d4610a7d565b90506020020160208101906105e991906109d1565b6001600160a01b03166108fc84848481811061060757610607610a7d565b905060200201359081150290604051600060405180830381858888f19350505050158015610639573d6000803e3d6000fd5b508061064481610a93565b9150506105ba565b5047801561068157604051339082156108fc029083906000818181858888f193505050501580156103df573d6000803e3d6000fd5b5050505050565b6000546001600160a01b0316331461069f57600080fd5b600080546001600160a01b0319166001600160a01b0392909216919091179055565b6002546000906106d2908390610a60565b6040516323b872dd60e01b8152336004820152306024820152604481018290529091506001600160a01b038516906323b872dd906064016020604051808303816000875af1158015610728573d6000803e3d6000fd5b505050506040513d601f19601f8201168201806040525081019061074c9190610aac565b61075557600080fd5b60005b8281101561068157846001600160a01b031663a9059cbb85858481811061078157610781610a7d565b905060200201602081019061079691906109d1565b6002546040516001600160e01b031960e085901b1681526001600160a01b03909216600483015260248201526044016020604051808303816000875af11580156107e4573d6000803e3d6000fd5b505050506040513d601f19601f820116820180604052508101906108089190610aac565b61081157600080fd5b8061081b81610a93565b915050610758565b60006020828403121561083557600080fd5b5035919050565b60008083601f84011261084e57600080fd5b50813567ffffffffffffffff81111561086657600080fd5b6020830191508360208260051b850101111561088157600080fd5b9250929050565b6000806020838503121561089b57600080fd5b823567ffffffffffffffff8111156108b257600080fd5b6108be8582860161083c565b90969095509350505050565b6001600160a01b03811681146108df57600080fd5b50565b6000806000806000606086880312156108fa57600080fd5b8535610905816108ca565b9450602086013567ffffffffffffffff8082111561092257600080fd5b61092e89838a0161083c565b9096509450604088013591508082111561094757600080fd5b506109548882890161083c565b969995985093965092949392505050565b6000806000806040858703121561097b57600080fd5b843567ffffffffffffffff8082111561099357600080fd5b61099f8883890161083c565b909650945060208701359150808211156109b857600080fd5b506109c58782880161083c565b95989497509550505050565b6000602082840312156109e357600080fd5b81356109ee816108ca565b9392505050565b600080600060408486031215610a0a57600080fd5b8335610a15816108ca565b9250602084013567ffffffffffffffff811115610a3157600080fd5b610a3d8682870161083c565b9497909650939450505050565b634e487b7160e01b600052601160045260246000fd5b8082028115828204841417610a7757610a77610a4a565b92915050565b634e487b7160e01b600052603260045260246000fd5b600060018201610aa557610aa5610a4a565b5060010190565b600060208284031215610abe57600080fd5b815180151581146109ee57600080fd5b80820180821115610a7757610a77610a4a56fea26469706673582212208ffcccc97a5bdf72ec557b90ab896814c212e0ff1ea1746475e09fb464ccb41d64736f6c63430008130033",
}

// DisperseABI is the input ABI used to generate the binding from.
// Deprecated: Use DisperseMetaData.ABI instead.
var DisperseABI = DisperseMetaData.ABI

// DisperseBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use DisperseMetaData.Bin instead.
var DisperseBin = DisperseMetaData.Bin

// DeployDisperse deploys a new Ethereum contract, binding an instance of Disperse to it.
func DeployDisperse(auth *bind.TransactOpts, backend bind.ContractBackend, etherAmount *big.Int, tokenAmount *big.Int) (common.Address, *types.Transaction, *Disperse, error) {
	parsed, err := DisperseMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(DisperseBin), backend, etherAmount, tokenAmount)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &Disperse{DisperseCaller: DisperseCaller{contract: contract}, DisperseTransactor: DisperseTransactor{contract: contract}, DisperseFilterer: DisperseFilterer{contract: contract}}, nil
}

// Disperse is an auto generated Go binding around an Ethereum contract.
type Disperse struct {
	DisperseCaller     // Read-only binding to the contract
	DisperseTransactor // Write-only binding to the contract
	DisperseFilterer   // Log filterer for contract events
}

// DisperseCaller is an auto generated read-only Go binding around an Ethereum contract.
type DisperseCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DisperseTransactor is an auto generated write-only Go binding around an Ethereum contract.
type DisperseTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DisperseFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type DisperseFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// DisperseSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type DisperseSession struct {
	Contract     *Disperse         // Generic contract binding to set the session for
	CallOpts     bind.CallOpts     // Call options to use throughout this session
	TransactOpts bind.TransactOpts // Transaction auth options to use throughout this session
}

// DisperseCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type DisperseCallerSession struct {
	Contract *DisperseCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts   // Call options to use throughout this session
}

// DisperseTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type DisperseTransactorSession struct {
	Contract     *DisperseTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// DisperseRaw is an auto generated low-level Go binding around an Ethereum contract.
type DisperseRaw struct {
	Contract *Disperse // Generic contract binding to access the raw methods on
}

// DisperseCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type DisperseCallerRaw struct {
	Contract *DisperseCaller // Generic read-only contract binding to access the raw methods on
}

// DisperseTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type DisperseTransactorRaw struct {
	Contract *DisperseTransactor // Generic write-only contract binding to access the raw methods on
}

// NewDisperse creates a new instance of Disperse, bound to a specific deployed contract.
func NewDisperse(address common.Address, backend bind.ContractBackend) (*Disperse, error) {
	contract, err := bindDisperse(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &Disperse{DisperseCaller: DisperseCaller{contract: contract}, DisperseTransactor: DisperseTransactor{contract: contract}, DisperseFilterer: DisperseFilterer{contract: contract}}, nil
}

// NewDisperseCaller creates a new read-only instance of Disperse, bound to a specific deployed contract.
func NewDisperseCaller(address common.Address, caller bind.ContractCaller) (*DisperseCaller, error) {
	contract, err := bindDisperse(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &DisperseCaller{contract: contract}, nil
}

// NewDisperseTransactor creates a new write-only instance of Disperse, bound to a specific deployed contract.
func NewDisperseTransactor(address common.Address, transactor bind.ContractTransactor) (*DisperseTransactor, error) {
	contract, err := bindDisperse(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &DisperseTransactor{contract: contract}, nil
}

// NewDisperseFilterer creates a new log filterer instance of Disperse, bound to a specific deployed contract.
func NewDisperseFilterer(address common.Address, filterer bind.ContractFilterer) (*DisperseFilterer, error) {
	contract, err := bindDisperse(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &DisperseFilterer{contract: contract}, nil
}

// bindDisperse binds a generic wrapper to an already deployed contract.
func bindDisperse(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := DisperseMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Disperse *DisperseRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Disperse.Contract.DisperseCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Disperse *DisperseRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Disperse *DisperseRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_Disperse *DisperseCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _Disperse.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_Disperse *DisperseTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _Disperse.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_Disperse *DisperseTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _Disperse.Contract.contract.Transact(opts, method, params...)
}

// FixedEtherAmount is a free data retrieval call binding the contract method 0x09486423.
//
// Solidity: function fixedEtherAmount() view returns(uint256)
func (_Disperse *DisperseCaller) FixedEtherAmount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Disperse.contract.Call(opts, &out, "fixedEtherAmount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FixedEtherAmount is a free data retrieval call binding the contract method 0x09486423.
//
// Solidity: function fixedEtherAmount() view returns(uint256)
func (_Disperse *DisperseSession) FixedEtherAmount() (*big.Int, error) {
	return _Disperse.Contract.FixedEtherAmount(&_Disperse.CallOpts)
}

// FixedEtherAmount is a free data retrieval call binding the contract method 0x09486423.
//
// Solidity: function fixedEtherAmount() view returns(uint256)
func (_Disperse *DisperseCallerSession) FixedEtherAmount() (*big.Int, error) {
	return _Disperse.Contract.FixedEtherAmount(&_Disperse.CallOpts)
}

// FixedTokenAmount is a free data retrieval call binding the contract method 0xe904a243.
//
// Solidity: function fixedTokenAmount() view returns(uint256)
func (_Disperse *DisperseCaller) FixedTokenAmount(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _Disperse.contract.Call(opts, &out, "fixedTokenAmount")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// FixedTokenAmount is a free data retrieval call binding the contract method 0xe904a243.
//
// Solidity: function fixedTokenAmount() view returns(uint256)
func (_Disperse *DisperseSession) FixedTokenAmount() (*big.Int, error) {
	return _Disperse.Contract.FixedTokenAmount(&_Disperse.CallOpts)
}

// FixedTokenAmount is a free data retrieval call binding the contract method 0xe904a243.
//
// Solidity: function fixedTokenAmount() view returns(uint256)
func (_Disperse *DisperseCallerSession) FixedTokenAmount() (*big.Int, error) {
	return _Disperse.Contract.FixedTokenAmount(&_Disperse.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Disperse *DisperseCaller) Owner(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _Disperse.contract.Call(opts, &out, "owner")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Disperse *DisperseSession) Owner() (common.Address, error) {
	return _Disperse.Contract.Owner(&_Disperse.CallOpts)
}

// Owner is a free data retrieval call binding the contract method 0x8da5cb5b.
//
// Solidity: function owner() view returns(address)
func (_Disperse *DisperseCallerSession) Owner() (common.Address, error) {
	return _Disperse.Contract.Owner(&_Disperse.CallOpts)
}

// DisperseEther is a paid mutator transaction binding the contract method 0xe63d38ed.
//
// Solidity: function disperseEther(address[] recipients, uint256[] values) payable returns()
func (_Disperse *DisperseTransactor) DisperseEther(opts *bind.TransactOpts, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "disperseEther", recipients, values)
}

// DisperseEther is a paid mutator transaction binding the contract method 0xe63d38ed.
//
// Solidity: function disperseEther(address[] recipients, uint256[] values) payable returns()
func (_Disperse *DisperseSession) DisperseEther(recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseEther(&_Disperse.TransactOpts, recipients, values)
}

// DisperseEther is a paid mutator transaction binding the contract method 0xe63d38ed.
//
// Solidity: function disperseEther(address[] recipients, uint256[] values) payable returns()
func (_Disperse *DisperseTransactorSession) DisperseEther(recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseEther(&_Disperse.TransactOpts, recipients, values)
}

// DisperseEtherFixed is a paid mutator transaction binding the contract method 0x1839bcf3.
//
// Solidity: function disperseEtherFixed(address[] recipients) payable returns()
func (_Disperse *DisperseTransactor) DisperseEtherFixed(opts *bind.TransactOpts, recipients []common.Address) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "disperseEtherFixed", recipients)
}

// DisperseEtherFixed is a paid mutator transaction binding the contract method 0x1839bcf3.
//
// Solidity: function disperseEtherFixed(address[] recipients) payable returns()
func (_Disperse *DisperseSession) DisperseEtherFixed(recipients []common.Address) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseEtherFixed(&_Disperse.TransactOpts, recipients)
}

// DisperseEtherFixed is a paid mutator transaction binding the contract method 0x1839bcf3.
//
// Solidity: function disperseEtherFixed(address[] recipients) payable returns()
func (_Disperse *DisperseTransactorSession) DisperseEtherFixed(recipients []common.Address) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseEtherFixed(&_Disperse.TransactOpts, recipients)
}

// DisperseToken is a paid mutator transaction binding the contract method 0xc73a2d60.
//
// Solidity: function disperseToken(address token, address[] recipients, uint256[] values) returns()
func (_Disperse *DisperseTransactor) DisperseToken(opts *bind.TransactOpts, token common.Address, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "disperseToken", token, recipients, values)
}

// DisperseToken is a paid mutator transaction binding the contract method 0xc73a2d60.
//
// Solidity: function disperseToken(address token, address[] recipients, uint256[] values) returns()
func (_Disperse *DisperseSession) DisperseToken(token common.Address, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseToken(&_Disperse.TransactOpts, token, recipients, values)
}

// DisperseToken is a paid mutator transaction binding the contract method 0xc73a2d60.
//
// Solidity: function disperseToken(address token, address[] recipients, uint256[] values) returns()
func (_Disperse *DisperseTransactorSession) DisperseToken(token common.Address, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseToken(&_Disperse.TransactOpts, token, recipients, values)
}

// DisperseTokenFixed is a paid mutator transaction binding the contract method 0xfb169f36.
//
// Solidity: function disperseTokenFixed(address token, address[] recipients) returns()
func (_Disperse *DisperseTransactor) DisperseTokenFixed(opts *bind.TransactOpts, token common.Address, recipients []common.Address) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "disperseTokenFixed", token, recipients)
}

// DisperseTokenFixed is a paid mutator transaction binding the contract method 0xfb169f36.
//
// Solidity: function disperseTokenFixed(address token, address[] recipients) returns()
func (_Disperse *DisperseSession) DisperseTokenFixed(token common.Address, recipients []common.Address) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseTokenFixed(&_Disperse.TransactOpts, token, recipients)
}

// DisperseTokenFixed is a paid mutator transaction binding the contract method 0xfb169f36.
//
// Solidity: function disperseTokenFixed(address token, address[] recipients) returns()
func (_Disperse *DisperseTransactorSession) DisperseTokenFixed(token common.Address, recipients []common.Address) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseTokenFixed(&_Disperse.TransactOpts, token, recipients)
}

// DisperseTokenSimple is a paid mutator transaction binding the contract method 0x51ba162c.
//
// Solidity: function disperseTokenSimple(address token, address[] recipients, uint256[] values) returns()
func (_Disperse *DisperseTransactor) DisperseTokenSimple(opts *bind.TransactOpts, token common.Address, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "disperseTokenSimple", token, recipients, values)
}

// DisperseTokenSimple is a paid mutator transaction binding the contract method 0x51ba162c.
//
// Solidity: function disperseTokenSimple(address token, address[] recipients, uint256[] values) returns()
func (_Disperse *DisperseSession) DisperseTokenSimple(token common.Address, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseTokenSimple(&_Disperse.TransactOpts, token, recipients, values)
}

// DisperseTokenSimple is a paid mutator transaction binding the contract method 0x51ba162c.
//
// Solidity: function disperseTokenSimple(address token, address[] recipients, uint256[] values) returns()
func (_Disperse *DisperseTransactorSession) DisperseTokenSimple(token common.Address, recipients []common.Address, values []*big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.DisperseTokenSimple(&_Disperse.TransactOpts, token, recipients, values)
}

// SetFixedEtherAmount is a paid mutator transaction binding the contract method 0x5fcc7d15.
//
// Solidity: function setFixedEtherAmount(uint256 amount) returns()
func (_Disperse *DisperseTransactor) SetFixedEtherAmount(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "setFixedEtherAmount", amount)
}

// SetFixedEtherAmount is a paid mutator transaction binding the contract method 0x5fcc7d15.
//
// Solidity: function setFixedEtherAmount(uint256 amount) returns()
func (_Disperse *DisperseSession) SetFixedEtherAmount(amount *big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.SetFixedEtherAmount(&_Disperse.TransactOpts, amount)
}

// SetFixedEtherAmount is a paid mutator transaction binding the contract method 0x5fcc7d15.
//
// Solidity: function setFixedEtherAmount(uint256 amount) returns()
func (_Disperse *DisperseTransactorSession) SetFixedEtherAmount(amount *big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.SetFixedEtherAmount(&_Disperse.TransactOpts, amount)
}

// SetFixedTokenAmount is a paid mutator transaction binding the contract method 0x107520fe.
//
// Solidity: function setFixedTokenAmount(uint256 amount) returns()
func (_Disperse *DisperseTransactor) SetFixedTokenAmount(opts *bind.TransactOpts, amount *big.Int) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "setFixedTokenAmount", amount)
}

// SetFixedTokenAmount is a paid mutator transaction binding the contract method 0x107520fe.
//
// Solidity: function setFixedTokenAmount(uint256 amount) returns()
func (_Disperse *DisperseSession) SetFixedTokenAmount(amount *big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.SetFixedTokenAmount(&_Disperse.TransactOpts, amount)
}

// SetFixedTokenAmount is a paid mutator transaction binding the contract method 0x107520fe.
//
// Solidity: function setFixedTokenAmount(uint256 amount) returns()
func (_Disperse *DisperseTransactorSession) SetFixedTokenAmount(amount *big.Int) (*types.Transaction, error) {
	return _Disperse.Contract.SetFixedTokenAmount(&_Disperse.TransactOpts, amount)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_Disperse *DisperseTransactor) TransferOwnership(opts *bind.TransactOpts, newOwner common.Address) (*types.Transaction, error) {
	return _Disperse.contract.Transact(opts, "transferOwnership", newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_Disperse *DisperseSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _Disperse.Contract.TransferOwnership(&_Disperse.TransactOpts, newOwner)
}

// TransferOwnership is a paid mutator transaction binding the contract method 0xf2fde38b.
//
// Solidity: function transferOwnership(address newOwner) returns()
func (_Disperse *DisperseTransactorSession) TransferOwnership(newOwner common.Address) (*types.Transaction, error) {
	return _Disperse.Contract.TransferOwnership(&_Disperse.TransactOpts, newOwner)
}
