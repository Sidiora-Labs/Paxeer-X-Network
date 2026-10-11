package eip712

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	Eip712Address            = "0x0000000000000000000000000000000000000908"
	PrecompileName           = "eip712"
	HashTypedDataMethod      = "hashTypedData"
	DomainSeparatorMethod    = "domainSeparator"
	RecoverTypedSignerMethod = "recoverTypedSigner"
	abiJSON                  = `[
		{"type":"function","name":"hashTypedData","stateMutability":"pure","inputs":[{"name":"domainSeparator","type":"bytes32"},{"name":"structHash","type":"bytes32"}],"outputs":[{"name":"digest","type":"bytes32"}]},
		{"type":"function","name":"domainSeparator","stateMutability":"pure","inputs":[{"name":"name","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"verifyingContract","type":"address"}],"outputs":[{"name":"separator","type":"bytes32"}]},
		{"type":"function","name":"recoverTypedSigner","stateMutability":"view","inputs":[{"name":"domainSeparator","type":"bytes32"},{"name":"structHash","type":"bytes32"},{"name":"signature","type":"bytes"}],"outputs":[{"name":"signer","type":"address"}]}
	]`
)

type Eip712Precompile struct {
	abi             abi.ABI
	maxNameBytes    int
	maxVersionBytes int
}

var _ pcommon.PrecompileExecutor = (*Eip712Precompile)(nil)

func NewPrecompile(upgrade *upgradekeeper.Keeper, planName string, maxNameBytes, maxVersionBytes int) (*agentcommon.Precompile, error) {
	if upgrade == nil || planName == "" {
		return nil, fmt.Errorf("%w: no merge upgrade activation reader bound", agentcommon.ErrNotActive)
	}
	if maxNameBytes <= 0 || maxVersionBytes <= 0 {
		return nil, fmt.Errorf("%w: name and version caps must be positive", agentcommon.ErrABIDecode)
	}
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	p := &Eip712Precompile{abi: a, maxNameBytes: maxNameBytes, maxVersionBytes: maxVersionBytes}
	return agentcommon.NewPrecompile(a, p, common.HexToAddress(Eip712Address), PrecompileName, upgrade, planName), nil
}

func (p *Eip712Precompile) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil {
		return pcommon.UnknownMethodCallGas
	}
	if len(input) > math.MaxInt-4 {
		return math.MaxUint64
	}
	return agentcommon.MethodGas(method.ID, len(input)+4)
}

func (p *Eip712Precompile) Execute(ctx sdk.Context, method *abi.Method, _ common.Address, _ common.Address, args []interface{}, value *big.Int, readOnly bool, _ *vm.EVM, _ *tracing.Hooks) ([]byte, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if method == nil {
		return nil, fmt.Errorf("%w: missing method", agentcommon.ErrUnknownMethod)
	}
	method, err := p.abi.MethodById(method.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrUnknownMethod, err)
	}
	if !method.IsConstant() {
		if err := agentcommon.GuardWrite(ctx, readOnly, ctx.EVMPrecompileCalledFromDelegateCall()); err != nil {
			return nil, err
		}
	}
	if err := agentcommon.ValidateNonPayable(value); err != nil {
		return nil, err
	}
	if err := agentcommon.ValidateArgsLength(args, len(method.Inputs)); err != nil {
		return nil, err
	}
	switch method.Name {
	case HashTypedDataMethod, RecoverTypedSignerMethod:
		domainSeparator, domainOK := args[0].([32]byte)
		structHash, structOK := args[1].([32]byte)
		if !domainOK || !structOK {
			return nil, fmt.Errorf("%w: domain separator and struct hash must be bytes32", agentcommon.ErrABIDecode)
		}
		if method.Name == HashTypedDataMethod {
			return method.Outputs.Pack(HashTypedData(domainSeparator, structHash))
		}
		signature, ok := args[2].([]byte)
		if !ok {
			return nil, fmt.Errorf("%w: signature must be bytes", agentcommon.ErrABIDecode)
		}
		return method.Outputs.Pack(RecoverTypedSigner(domainSeparator, structHash, signature))
	case DomainSeparatorMethod:
		name, nameOK := args[0].(string)
		version, versionOK := args[1].(string)
		chainID, chainOK := args[2].(*big.Int)
		verifyingContract, addressOK := args[3].(common.Address)
		if !nameOK || !versionOK || !chainOK || !addressOK {
			return nil, fmt.Errorf("%w: invalid domain arguments", agentcommon.ErrABIDecode)
		}
		separator, err := p.DomainSeparator(name, version, chainID, verifyingContract)
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(separator)
	default:
		return nil, fmt.Errorf("%w: %s", agentcommon.ErrUnknownMethod, method.Name)
	}
}

func HashTypedData(domainSeparator, structHash [32]byte) [32]byte {
	var input [66]byte
	input[0], input[1] = 0x19, 0x01
	copy(input[2:34], domainSeparator[:])
	copy(input[34:], structHash[:])
	return crypto.Keccak256Hash(input[:])
}

func (p *Eip712Precompile) DomainSeparator(name, version string, chainID *big.Int, verifyingContract common.Address) ([32]byte, error) {
	if len(name) > p.maxNameBytes || len(version) > p.maxVersionBytes {
		return [32]byte{}, fmt.Errorf("%w: domain name or version exceeds its byte cap", agentcommon.ErrBatchTooLarge)
	}
	if chainID == nil || chainID.Sign() < 0 || chainID.BitLen() > 256 {
		return [32]byte{}, fmt.Errorf("%w: chainId must be uint256", agentcommon.ErrABIDecode)
	}
	var input [160]byte
	copy(input[:32], crypto.Keccak256([]byte("EIP712Domain(string name,string version,uint256 chainId,address verifyingContract)")))
	copy(input[32:64], crypto.Keccak256([]byte(name)))
	copy(input[64:96], crypto.Keccak256([]byte(version)))
	chainID.FillBytes(input[96:128])
	copy(input[140:], verifyingContract[:])
	return crypto.Keccak256Hash(input[:]), nil
}

func RecoverTypedSigner(domainSeparator, structHash [32]byte, signature []byte) common.Address {
	if len(signature) != crypto.SignatureLength || (signature[64] != 27 && signature[64] != 28) {
		return common.Address{}
	}
	var normalized [crypto.SignatureLength]byte
	copy(normalized[:], signature)
	normalized[64] -= 27
	r := new(big.Int).SetBytes(normalized[:32])
	s := new(big.Int).SetBytes(normalized[32:64])
	if !crypto.ValidateSignatureValues(normalized[64], r, s, true) {
		return common.Address{}
	}
	digest := HashTypedData(domainSeparator, structHash)
	key, err := crypto.SigToPub(digest[:], normalized[:])
	if err != nil || key == nil {
		return common.Address{}
	}
	return crypto.PubkeyToAddress(*key)
}
