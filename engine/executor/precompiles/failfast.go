package precompiles

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/sidiora-labs/paxeer-network/precompiles/addr"
	"github.com/sidiora-labs/paxeer-network/precompiles/bank"
	"github.com/sidiora-labs/paxeer-network/precompiles/distribution"
	"github.com/sidiora-labs/paxeer-network/precompiles/feetoken"
	"github.com/sidiora-labs/paxeer-network/precompiles/gov"
	"github.com/sidiora-labs/paxeer-network/precompiles/ibc"
	"github.com/sidiora-labs/paxeer-network/precompiles/json"
	"github.com/sidiora-labs/paxeer-network/precompiles/launchpad"
	"github.com/sidiora-labs/paxeer-network/precompiles/layerxanchor"
	"github.com/sidiora-labs/paxeer-network/precompiles/layerxbridge"
	"github.com/sidiora-labs/paxeer-network/precompiles/layerxcustody"
	"github.com/sidiora-labs/paxeer-network/precompiles/layerxexchange"
	"github.com/sidiora-labs/paxeer-network/precompiles/layerxverify"
	"github.com/sidiora-labs/paxeer-network/precompiles/oracle"
	"github.com/sidiora-labs/paxeer-network/precompiles/p256"
	"github.com/sidiora-labs/paxeer-network/precompiles/pointer"
	"github.com/sidiora-labs/paxeer-network/precompiles/pointerview"
	"github.com/sidiora-labs/paxeer-network/precompiles/solo"
	"github.com/sidiora-labs/paxeer-network/precompiles/staking"
	"github.com/sidiora-labs/paxeer-network/precompiles/wasmd"
	"github.com/sidiora-labs/paxeer-network/precompiles/xweb"
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
