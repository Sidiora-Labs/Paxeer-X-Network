package teeattestor

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"strings"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	bridgetypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxbridge/types"
	xwebkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	xwebtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	TeeAttestorAddress            = "0x0000000000000000000000000000000000000907"
	PrecompileName                = "tee attestor"
	VerifyMethod                  = "verify"
	VerifyAndExpectMethod         = "verifyAndExpect"
	RootOfMethod                  = "rootOf"
	RootCountMethod               = "rootCount"
	MaxAttestationAge      uint64 = 600
	envelopeHeaderLength          = 96
	envelopePreimageLength        = 95
	attestorCount                 = 5
	minimumThreshold              = 3
)

const abiJSON = `[
    {"type":"function","name":"verify","stateMutability":"view","inputs":[{"name":"family","type":"uint8"},{"name":"quote","type":"bytes"}],"outputs":[{"name":"att","type":"tuple","components":[{"name":"family","type":"uint8"},{"name":"mrtd","type":"bytes32"},{"name":"reportData","type":"bytes32"},{"name":"timestamp","type":"uint64"},{"name":"debug","type":"bool"}]}]},
    {"type":"function","name":"verifyAndExpect","stateMutability":"view","inputs":[{"name":"family","type":"uint8"},{"name":"quote","type":"bytes"},{"name":"expectedReportData","type":"bytes32"}],"outputs":[{"name":"att","type":"tuple","components":[{"name":"family","type":"uint8"},{"name":"mrtd","type":"bytes32"},{"name":"reportData","type":"bytes32"},{"name":"timestamp","type":"uint64"},{"name":"debug","type":"bool"}]}]},
    {"type":"function","name":"rootOf","stateMutability":"view","inputs":[{"name":"family","type":"uint8"},{"name":"index","type":"uint256"}],"outputs":[{"name":"root","type":"bytes"}]},
    {"type":"function","name":"rootCount","stateMutability":"view","inputs":[{"name":"family","type":"uint8"}],"outputs":[{"name":"count","type":"uint256"}]},
    {"type":"event","name":"AttestationVerified","anonymous":false,"inputs":[{"name":"family","type":"uint8","indexed":true},{"name":"mrtd","type":"bytes32","indexed":true},{"name":"reportData","type":"bytes32","indexed":false},{"name":"timestamp","type":"uint64","indexed":false},{"name":"debug","type":"bool","indexed":false}]}
]`

var (
	errUnknownFamily      = fmt.Errorf("%w: unknown family", agentcommon.ErrTEEUnknownFamily)
	errNoRoots            = fmt.Errorf("%w: no roots loaded", agentcommon.ErrTEENoRoots)
	errInvalidQuote       = fmt.Errorf("%w: invalid quote", agentcommon.ErrTEEInvalidQuote)
	errUntrustedChain     = fmt.Errorf("%w: untrusted chain", agentcommon.ErrTEEUntrustedChain)
	errStale              = fmt.Errorf("%w: attestation stale", agentcommon.ErrTEEStale)
	errDebugRefused       = fmt.Errorf("%w: debug enclave refused", agentcommon.ErrTEEDebugRefused)
	errReportDataMismatch = fmt.Errorf("%w: report data mismatch", agentcommon.ErrTEEReportDataMismatch)
)

type Attestation struct {
	Family     uint8
	Mrtd       [32]byte
	ReportData [32]byte
	Timestamp  uint64
	Debug      bool
}

type devEnvelope struct {
	attestation Attestation
	chainID     uint64
	nonce       uint64
	signatures  []byte
}

type TeeAttestorPrecompile struct {
	*agentcommon.Precompile
}

type PrecompileExecutor struct {
	abi       abi.ABI
	keeper    *xwebkeeper.Keeper
	evmKeeper *evmkeeper.Keeper
}

var _ vm.PrecompiledContract = (*TeeAttestorPrecompile)(nil)
var _ pcommon.PrecompileExecutor = (*PrecompileExecutor)(nil)

func NewPrecompile(keeper *xwebkeeper.Keeper, evmKeeper *evmkeeper.Keeper, upgrade *upgradekeeper.Keeper, planName string) (*TeeAttestorPrecompile, error) {
	if keeper == nil || evmKeeper == nil {
		return nil, fmt.Errorf("%w: tee attestor keepers are unavailable", agentcommon.ErrNoContext)
	}
	if upgrade == nil || planName == "" {
		return nil, fmt.Errorf("%w: no merge upgrade activation reader bound", agentcommon.ErrNotActive)
	}
	a, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrABIDecode, err)
	}
	executor := &PrecompileExecutor{abi: a, keeper: keeper, evmKeeper: evmKeeper}
	return &TeeAttestorPrecompile{Precompile: agentcommon.NewPrecompile(a, executor, ethcommon.HexToAddress(TeeAttestorAddress), PrecompileName, upgrade, planName)}, nil
}

func init() {
	if err := precompiles.RegisterMergeConstructor(ethcommon.HexToAddress(TeeAttestorAddress), NewMergePrecompile); err != nil {
		panic(err)
	}
}

// NewMergePrecompile builds the precompile from the application's precompile
// keepers; it is the constructor registered for the merge address table.
func NewMergePrecompile(keepers utils.Keepers, planName string) (precompiles.IPrecompile, error) {
	if keepers == nil {
		return nil, fmt.Errorf("%w: precompile keepers are unavailable", agentcommon.ErrNoContext)
	}
	wired, ok := keepers.(utils.XWebKeepers)
	if !ok {
		return nil, fmt.Errorf("%w: xweb keeper is unavailable", agentcommon.ErrNoContext)
	}
	evmKeeper, ok := keepers.EVMK().(*evmkeeper.Keeper)
	if !ok || evmKeeper == nil {
		return nil, fmt.Errorf("%w: evm keeper is unavailable", agentcommon.ErrNoContext)
	}
	p, err := NewPrecompile(wired.XWebK(), evmKeeper, evmKeeper.UpgradeKeeper(), planName)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PrecompileExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil || len(input) > math.MaxInt-4 {
		return math.MaxUint64
	}
	return agentcommon.MethodGas(method.ID, len(input)+4)
}

func (p *PrecompileExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if p == nil || p.keeper == nil || p.evmKeeper == nil {
		return nil, fmt.Errorf("%w: tee attestor keepers are unavailable", agentcommon.ErrNoContext)
	}
	if method == nil {
		return nil, agentcommon.ErrUnknownMethod
	}
	canonical, err := p.abi.MethodById(method.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", agentcommon.ErrUnknownMethod, err)
	}
	method = canonical
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
	family, ok := args[0].(uint8)
	if !ok {
		return nil, fmt.Errorf("%w: family must be uint8", agentcommon.ErrABIDecode)
	}
	switch method.Name {
	case VerifyMethod, VerifyAndExpectMethod:
		quote, ok := args[1].([]byte)
		if !ok {
			return nil, fmt.Errorf("%w: quote must be bytes", agentcommon.ErrABIDecode)
		}
		var attestation Attestation
		if method.Name == VerifyAndExpectMethod {
			expected, ok := args[2].([32]byte)
			if !ok {
				return nil, fmt.Errorf("%w: expected report data must be bytes32", agentcommon.ErrABIDecode)
			}
			attestation, err = p.VerifyAndExpect(ctx, family, quote, expected)
		} else {
			attestation, err = p.Verify(ctx, family, quote)
		}
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(attestation)
	case RootOfMethod:
		index, ok := args[1].(*big.Int)
		if !ok || index == nil || index.Sign() < 0 || index.BitLen() > 256 {
			return nil, fmt.Errorf("%w: index must be uint256", agentcommon.ErrABIDecode)
		}
		root, err := p.RootOf(ctx, family, index)
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(root)
	case RootCountMethod:
		count, err := p.RootCount(ctx, family)
		if err != nil {
			return nil, err
		}
		return method.Outputs.Pack(count)
	default:
		return nil, agentcommon.ErrUnknownMethod
	}
}

func parseDevEnvelope(family uint8, data []byte) (devEnvelope, error) {
	if family > 3 {
		return devEnvelope{}, errUnknownFamily
	}
	if len(data) < envelopeHeaderLength || !bytes.Equal(data[:4], []byte("PXVE")) || data[4] != 1 {
		return devEnvelope{}, errInvalidQuote
	}
	if data[5] > 3 {
		return devEnvelope{}, errUnknownFamily
	}
	if data[5] != family || data[6] > 1 || len(data) != envelopeHeaderLength+bridgetypes.SignatureLength*int(data[95]) {
		return devEnvelope{}, errInvalidQuote
	}
	envelope := devEnvelope{
		attestation: Attestation{Family: data[5], Timestamp: binary.BigEndian.Uint64(data[71:79]), Debug: data[6] == 1},
		chainID:     binary.BigEndian.Uint64(data[79:87]),
		nonce:       binary.BigEndian.Uint64(data[87:95]),
		signatures:  data[envelopeHeaderLength:],
	}
	copy(envelope.attestation.Mrtd[:], data[7:39])
	copy(envelope.attestation.ReportData[:], data[39:71])
	return envelope, nil
}

func (p *PrecompileExecutor) Verify(ctx sdk.Context, family uint8, quote []byte) (Attestation, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return Attestation{}, err
	}
	if p == nil || p.keeper == nil || p.evmKeeper == nil {
		return Attestation{}, fmt.Errorf("%w: tee attestor keepers are unavailable", agentcommon.ErrNoContext)
	}
	envelope, err := parseDevEnvelope(family, quote)
	if err != nil {
		return Attestation{}, err
	}
	chainID := p.evmKeeper.ChainID(ctx)
	if chainID == nil || !chainID.IsUint64() || envelope.chainID != chainID.Uint64() {
		return Attestation{}, errInvalidQuote
	}
	set := p.keeper.GetAttestorSet(ctx)
	if len(set.Attestors) == 0 {
		return Attestation{}, errNoRoots
	}
	if len(set.Attestors) != attestorCount || set.Threshold < minimumThreshold || set.Threshold > attestorCount {
		return Attestation{}, errUntrustedChain
	}
	digest := bridgetypes.Hash32(crypto.Keccak256Hash(quote[:envelopePreimageLength]))
	count := len(envelope.signatures) / bridgetypes.SignatureLength
	var previous xwebtypes.Address20
	for index := 0; index < count; index++ {
		signature := envelope.signatures[index*bridgetypes.SignatureLength : (index+1)*bridgetypes.SignatureLength]
		recovered, err := bridgetypes.RecoverSigner(digest, signature)
		if err != nil {
			return Attestation{}, errInvalidQuote
		}
		signer := xwebtypes.Address20(recovered)
		if !set.Has(signer) || (index > 0 && bytes.Compare(previous[:], signer[:]) >= 0) {
			return Attestation{}, errUntrustedChain
		}
		previous = signer
	}
	if count < int(set.Threshold) {
		return Attestation{}, errUntrustedChain
	}
	now := ctx.BlockTime().Unix()
	if now < 0 || envelope.attestation.Timestamp > uint64(now) || uint64(now)-envelope.attestation.Timestamp > MaxAttestationAge {
		return Attestation{}, errStale
	}
	if envelope.attestation.Debug {
		return Attestation{}, errDebugRefused
	}
	return envelope.attestation, nil
}

func (p *PrecompileExecutor) VerifyAndExpect(ctx sdk.Context, family uint8, quote []byte, expectedReportData [32]byte) (Attestation, error) {
	attestation, err := p.Verify(ctx, family, quote)
	if err != nil {
		return Attestation{}, err
	}
	if attestation.ReportData != expectedReportData {
		return Attestation{}, errReportDataMismatch
	}
	return attestation, nil
}

func (p *PrecompileExecutor) RootOf(ctx sdk.Context, family uint8, index *big.Int) ([]byte, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if p == nil || p.keeper == nil {
		return nil, fmt.Errorf("%w: tee attestor keeper is unavailable", agentcommon.ErrNoContext)
	}
	if index == nil || index.Sign() < 0 || index.BitLen() > 256 {
		return nil, fmt.Errorf("%w: index must be uint256", agentcommon.ErrABIDecode)
	}
	if family > 3 || !index.IsUint64() {
		return []byte{}, nil
	}
	set := p.keeper.GetAttestorSet(ctx)
	if index.Uint64() >= uint64(len(set.Attestors)) {
		return []byte{}, nil
	}
	signer := set.Attestors[int(index.Uint64())].Signer
	return append([]byte(nil), signer[:]...), nil
}

func (p *PrecompileExecutor) RootCount(ctx sdk.Context, family uint8) (*big.Int, error) {
	if err := agentcommon.HeightGate(ctx); err != nil {
		return nil, err
	}
	if p == nil || p.keeper == nil {
		return nil, fmt.Errorf("%w: tee attestor keeper is unavailable", agentcommon.ErrNoContext)
	}
	if family > 3 {
		return new(big.Int), nil
	}
	return new(big.Int).SetUint64(uint64(len(p.keeper.GetAttestorSet(ctx).Attestors))), nil
}
