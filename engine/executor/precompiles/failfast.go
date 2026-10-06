package precompiles

import (
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/addr"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/bank"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/distribution"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/feetoken"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/gov"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/ibc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/json"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxanchor"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxbridge"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxcustody"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxexchange"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxverify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/oracle"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/p256"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pointer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/pointerview"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/solo"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/staking"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/wasmd"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

var FailFastPrecompileAddresses = []common.Address{
	common.HexToAddress(bank.BankAddress),
	common.HexToAddress(wasmd.WasmdAddress),
	common.HexToAddress(json.JSONAddress),
	common.HexToAddress(addr.AddrAddress),
	common.HexToAddress(staking.StakingAddress),
	common.HexToAddress(gov.GovAddress),
	common.HexToAddress(distribution.DistrAddress),
	common.HexToAddress(oracle.OracleAddress),
	common.HexToAddress(ibc.IBCAddress),
	common.HexToAddress(pointerview.PointerViewAddress),
	common.HexToAddress(pointer.PointerAddress),
	common.HexToAddress(solo.SoloAddress),
	common.HexToAddress(p256.P256VerifyAddress),
	common.HexToAddress(layerxverify.LayerXVerifyAddress),
	common.HexToAddress(layerxcustody.LayerXCustodyAddress),
	common.HexToAddress(layerxanchor.LayerXAnchorAddress),
}

// InvalidPrecompileCallError is an error type that implements vm.AbortError,
// signaling that execution should abort and this error should propagate
// through the entire call stack.
type InvalidPrecompileCallError struct{}

func (e *InvalidPrecompileCallError) Error() string {
	return "invalid precompile call"
}

// IsAbortError implements vm.AbortError interface, signaling that this error
// should propagate through the EVM call stack instead of being swallowed.
func (e *InvalidPrecompileCallError) IsAbortError() bool {
	return true
}

// ErrInvalidPrecompileCall is the singleton error instance for invalid precompile calls.
// It implements vm.AbortError to ensure it propagates through the call stack.
var ErrInvalidPrecompileCall error = &InvalidPrecompileCallError{}

// BalanceMigrationAbortError signals that the transaction requires balance
// migration (unassociated address), which giga cannot handle. The caller
// should fall back to v2.
type BalanceMigrationAbortError struct{}

func (e *BalanceMigrationAbortError) Error() string {
	return "balance migration required for unassociated address"
}

func (e *BalanceMigrationAbortError) IsAbortError() bool {
	return true
}

var ErrBalanceMigrationRequired error = &BalanceMigrationAbortError{}

// SelfDestructAbortError signals a self-destruct, whose storage clearing needs
// store iteration giga can't do; the caller should fall back to v2.
type SelfDestructAbortError struct{}

func (e *SelfDestructAbortError) Error() string {
	return "self-destruct storage clearing requires store iteration unsupported by giga"
}

func (e *SelfDestructAbortError) IsAbortError() bool {
	return true
}

var ErrSelfDestructUnsupported error = &SelfDestructAbortError{}

type FailFastPrecompile struct{}

var FailFastSingleton vm.PrecompiledContract = &FailFastPrecompile{}

func (p *FailFastPrecompile) RequiredGas(input []byte) uint64 {
	return 0
}

func (p *FailFastPrecompile) Run(evm *vm.EVM, caller common.Address, callingContract common.Address, input []byte, value *big.Int, readOnly bool, isFromDelegateCall bool, hooks *tracing.Hooks) ([]byte, error) {
	return nil, ErrInvalidPrecompileCall
}

var AllCustomPrecompilesFailFast = map[common.Address]vm.PrecompiledContract{}

// LateFailFastPrecompileAddresses lists the custom precompiles the giga
// executor hands to the ordinary execution path from the height the
// application names, in addition to FailFastPrecompileAddresses.
var LateFailFastPrecompileAddresses = []common.Address{
	common.HexToAddress(layerxexchange.ExchangeAddress),
	common.HexToAddress(layerxbridge.BridgeAddress),
	common.HexToAddress(launchpad.LaunchpadAddress),
	common.HexToAddress(feetoken.FeeTokenAddress),
	common.HexToAddress(xweb.XWebAddress),
}

// AllCustomPrecompilesFailFastLate maps every address of
// FailFastPrecompileAddresses and of LateFailFastPrecompileAddresses to the
// fail-fast singleton.
var AllCustomPrecompilesFailFastLate = map[common.Address]vm.PrecompiledContract{}

func init() {
	for _, addr := range FailFastPrecompileAddresses {
		AllCustomPrecompilesFailFast[addr] = FailFastSingleton
		AllCustomPrecompilesFailFastLate[addr] = FailFastSingleton
	}
	for _, addr := range LateFailFastPrecompileAddresses {
		AllCustomPrecompilesFailFastLate[addr] = FailFastSingleton
	}
}
