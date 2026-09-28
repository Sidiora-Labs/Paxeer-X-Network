package evm

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	ErrUndecodable      = errors.New("undecodable transaction")
	ErrUnsupportedType  = errors.New("unsupported transaction type")
	ErrChainID          = errors.New("chain id does not match the configured chain")
	ErrUnprotected      = errors.New("transaction carries no chain id")
	ErrUnknownMethod    = errors.New("calldata selector is not a method of the destination")
	ErrMalformedCall    = errors.New("calldata cannot be decoded against the destination ABI")
	ErrMissingChainID   = errors.New("configured chain id is missing")
	ErrMissingSignature = errors.New("transaction signature fields are malformed")
)

//go:embed abi/*.json
var abiFiles embed.FS

var precompileAddresses = map[string]common.Address{
	"addr":           common.HexToAddress("0x0000000000000000000000000000000000001004"),
	"layerxcustody":  common.HexToAddress("0x0000000000000000000000000000000000001013"),
	"layerxanchor":   common.HexToAddress("0x0000000000000000000000000000000000001014"),
	"layerxexchange": common.HexToAddress("0x0000000000000000000000000000000000001015"),
	"layerxbridge":   common.HexToAddress("0x0000000000000000000000000000000000001016"),
	"launchpad":      common.HexToAddress("0x0000000000000000000000000000000000001017"),
	"feetoken":       common.HexToAddress("0x0000000000000000000000000000000000001018"),
	"xweb":           common.HexToAddress("0x0000000000000000000000000000000000001019"),
}

const erc20Name = "erc20"

type abiRegistry struct {
	byName    map[string]abi.ABI
	byAddress map[common.Address]string
}

var loadRegistry = sync.OnceValues(func() (*abiRegistry, error) {
	reg := &abiRegistry{byName: map[string]abi.ABI{}, byAddress: map[common.Address]string{}}
	names := append(PrecompileNames(), erc20Name)
	for _, name := range names {
		raw, err := abiFiles.ReadFile("abi/" + name + ".json")
		if err != nil {
			return nil, fmt.Errorf("read %s abi: %w", name, err)
		}
		parsed, err := abi.JSON(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("parse %s abi: %w", name, err)
		}
		reg.byName[name] = parsed
	}
	for name, address := range precompileAddresses {
		reg.byAddress[address] = name
	}
	return reg, nil
})

func PrecompileNames() []string {
	names := make([]string, 0, len(precompileAddresses))
	for name := range precompileAddresses {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func PrecompileAddress(name string) (common.Address, bool) {
	address, ok := precompileAddresses[name]
	return address, ok
}

func IsPrecompile(address common.Address) bool {
	for _, known := range precompileAddresses {
		if known == address {
			return true
		}
	}
	return false
}

func PrecompileABI(name string) (abi.ABI, error) {
	reg, err := loadRegistry()
	if err != nil {
		return abi.ABI{}, err
	}
	parsed, ok := reg.byName[name]
	if !ok {
		return abi.ABI{}, fmt.Errorf("no abi named %q", name)
	}
	return parsed, nil
}

func EmbeddedABI(name string) ([]byte, error) {
	return abiFiles.ReadFile("abi/" + name + ".json")
}

type Transaction struct {
	Type           uint8
	ChainID        *big.Int
	Nonce          uint64
	Gas            uint64
	GasPrice       *big.Int
	GasTipCap      *big.Int
	GasFeeCap      *big.Int
	To             *common.Address
	Value          *big.Int
	Data           []byte
	AccessList     types.AccessList
	Authorizations []types.SetCodeAuthorization
	SigningDigest  common.Hash
}

func DecodeTransaction(raw []byte, chainID *big.Int) (*Transaction, error) {
	if chainID == nil || chainID.Sign() <= 0 {
		return nil, ErrMissingChainID
	}
	if len(raw) == 0 {
		return nil, ErrUndecodable
	}
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUndecodable, err)
	}
	var txChainID *big.Int
	switch tx.Type() {
	case types.LegacyTxType:
		v, r, s := tx.RawSignatureValues()
		switch {
		case r.Sign() == 0 && s.Sign() == 0:
			if v.Sign() == 0 {
				return nil, ErrUnprotected
			}
			txChainID = new(big.Int).Set(v)
		case r.Sign() == 0 || s.Sign() == 0:
			return nil, ErrMissingSignature
		default:
			if !tx.Protected() {
				return nil, ErrUnprotected
			}
			txChainID = tx.ChainId()
		}
	case types.AccessListTxType, types.DynamicFeeTxType, types.SetCodeTxType:
		txChainID = tx.ChainId()
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedType, tx.Type())
	}
	if txChainID == nil || txChainID.Cmp(chainID) != 0 {
		return nil, fmt.Errorf("%w: got %v want %v", ErrChainID, txChainID, chainID)
	}
	signer := types.LatestSignerForChainID(chainID)
	view := &Transaction{
		Type:          tx.Type(),
		ChainID:       new(big.Int).Set(chainID),
		Nonce:         tx.Nonce(),
		Gas:           tx.Gas(),
		GasPrice:      tx.GasPrice(),
		GasTipCap:     tx.GasTipCap(),
		GasFeeCap:     tx.GasFeeCap(),
		To:            tx.To(),
		Value:         tx.Value(),
		Data:          tx.Data(),
		AccessList:    tx.AccessList(),
		SigningDigest: signer.Hash(tx),
	}
	if tx.Type() == types.SetCodeTxType {
		view.Authorizations = tx.SetCodeAuthorizations()
	}
	return view, nil
}

type CallKind string

const (
	CallNative     CallKind = "native"
	CallPrecompile CallKind = "precompile"
	CallERC20      CallKind = "erc20"
	CallUnknown    CallKind = "unknown_destination"
)

type Call struct {
	Kind        CallKind
	To          common.Address
	Destination string
	Selector    []byte
	Method      string
	Args        map[string]any
}

func DecodeCalldata(to common.Address, data []byte) (*Call, error) {
	reg, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	call := &Call{To: to}
	name, precompile := reg.byAddress[to]
	if len(data) == 0 {
		call.Kind = CallNative
		if precompile {
			call.Destination = name
		}
		return call, nil
	}
	if precompile {
		call.Kind = CallPrecompile
		call.Destination = name
		if err := decodeMethod(call, reg.byName[name], data); err != nil {
			return nil, err
		}
		return call, nil
	}
	if len(data) >= 4 {
		erc20 := reg.byName[erc20Name]
		if method, err := erc20.MethodById(data[:4]); err == nil && !method.IsConstant() {
			call.Kind = CallERC20
			call.Destination = erc20Name
			if err := decodeMethod(call, erc20, data); err != nil {
				return nil, err
			}
			return call, nil
		}
		call.Selector = common.CopyBytes(data[:4])
	}
	call.Kind = CallUnknown
	return call, nil
}

func decodeMethod(call *Call, contract abi.ABI, data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("%w: calldata shorter than a selector", ErrMalformedCall)
	}
	method, err := contract.MethodById(data[:4])
	if err != nil {
		return fmt.Errorf("%w: %x", ErrUnknownMethod, data[:4])
	}
	args := map[string]any{}
	if err := method.Inputs.UnpackIntoMap(args, data[4:]); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrMalformedCall, method.Name, err)
	}
	packed, err := method.Inputs.Pack(orderedArgs(method.Inputs, args)...)
	if err != nil || !bytes.Equal(packed, data[4:]) {
		return fmt.Errorf("%w: %s does not round-trip", ErrMalformedCall, method.Name)
	}
	call.Selector = common.CopyBytes(data[:4])
	call.Method = method.Name
	call.Args = args
	return nil
}

func orderedArgs(inputs abi.Arguments, args map[string]any) []any {
	values := make([]any, len(inputs))
	for i, input := range inputs {
		values[i] = args[input.Name]
	}
	return values
}
