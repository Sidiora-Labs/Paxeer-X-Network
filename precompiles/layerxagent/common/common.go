package common

import (
	"context"
	"fmt"
	"math"
	"math/big"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	pcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/common"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	upgradekeeper "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/upgrade/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
)

type ErrorCode string

func (code ErrorCode) Error() string { return string(code) }

const (
	ErrInputTooShort            ErrorCode = "LXP_PRECOMPILE_INPUT_TOO_SHORT"
	ErrUnknownMethod            ErrorCode = "LXP_PRECOMPILE_UNKNOWN_METHOD"
	ErrArgCount                 ErrorCode = "LXP_PRECOMPILE_ARG_COUNT"
	ErrNonPayable               ErrorCode = "LXP_PRECOMPILE_NON_PAYABLE"
	ErrNoContext                ErrorCode = "LXP_PRECOMPILE_NO_CONTEXT"
	ErrABIDecode                ErrorCode = "LXP_AGENT_ABI_DECODE"
	ErrReadOnly                 ErrorCode = "LXP_AGENT_READ_ONLY"
	ErrDelegatecall             ErrorCode = "LXP_AGENT_DELEGATECALL"
	ErrOverflow                 ErrorCode = "LXP_AGENT_OVERFLOW"
	ErrBatchTooLarge            ErrorCode = "LXP_AGENT_BATCH_TOO_LARGE"
	ErrOrobOraclePrice          ErrorCode = "LXP_AGENT_OROB_ORACLE_PRICE"
	ErrOrobOffsetRange          ErrorCode = "LXP_AGENT_OROB_OFFSET_RANGE"
	ErrClearingLengthMismatch   ErrorCode = "LXP_AGENT_CLEARING_LENGTH_MISMATCH"
	ErrOracleNoFeeds            ErrorCode = "LXP_AGENT_ORACLE_NO_FEEDS"
	ErrOracleNoConfidentFeeds   ErrorCode = "LXP_AGENT_ORACLE_NO_CONFIDENT_FEEDS"
	ErrOracleNotSigner          ErrorCode = "LXP_AGENT_ORACLE_NOT_SIGNER"
	ErrOraclePrice              ErrorCode = "LXP_AGENT_ORACLE_PRICE"
	ErrOracleConfidence         ErrorCode = "LXP_AGENT_ORACLE_CONFIDENCE"
	ErrPofqLengthMismatch       ErrorCode = "LXP_AGENT_POFQ_LENGTH_MISMATCH"
	ErrPofqDecayRange           ErrorCode = "LXP_AGENT_POFQ_DECAY_RANGE"
	ErrSchedulerJobNotFound     ErrorCode = "LXP_AGENT_SCHEDULER_JOB_NOT_FOUND"
	ErrSchedulerUnauthorized    ErrorCode = "LXP_AGENT_SCHEDULER_UNAUTHORIZED"
	ErrSchedulerHorizonExceeded ErrorCode = "LXP_AGENT_SCHEDULER_HORIZON_EXCEEDED"
	ErrSchedulerPastBlock       ErrorCode = "LXP_AGENT_SCHEDULER_PAST_BLOCK"
	ErrSchedulerMaxJobs         ErrorCode = "LXP_AGENT_SCHEDULER_MAX_JOBS"
	ErrSchedulerDepositTooLow   ErrorCode = "LXP_AGENT_SCHEDULER_DEPOSIT_TOO_LOW"
	ErrSchedulerInvalidGasLimit ErrorCode = "LXP_AGENT_SCHEDULER_INVALID_GAS_LIMIT"
	ErrSchedulerInvalidTarget   ErrorCode = "LXP_AGENT_SCHEDULER_INVALID_TARGET"
	ErrSchedulerJobInactive     ErrorCode = "LXP_AGENT_SCHEDULER_JOB_INACTIVE"
	ErrSchedulerEscrow          ErrorCode = "LXP_AGENT_SCHEDULER_ESCROW"
	ErrStreamsInvalidPayee      ErrorCode = "LXP_AGENT_STREAMS_INVALID_PAYEE"
	ErrStreamsSelfPayment       ErrorCode = "LXP_AGENT_STREAMS_SELF_PAYMENT"
	ErrStreamsUnknownToken      ErrorCode = "LXP_AGENT_STREAMS_UNKNOWN_TOKEN"
	ErrTEEUnknownFamily         ErrorCode = "LXP_AGENT_TEE_UNKNOWN_FAMILY"
	ErrTEENoRoots               ErrorCode = "LXP_AGENT_TEE_NO_ROOTS"
	ErrTEEInvalidQuote          ErrorCode = "LXP_AGENT_TEE_INVALID_QUOTE"
	ErrTEEUntrustedChain        ErrorCode = "LXP_AGENT_TEE_UNTRUSTED_CHAIN"
	ErrTEEStale                 ErrorCode = "LXP_AGENT_TEE_STALE"
	ErrTEEDebugRefused          ErrorCode = "LXP_AGENT_TEE_DEBUG_REFUSED"
	ErrTEEReportDataMismatch    ErrorCode = "LXP_AGENT_TEE_REPORT_DATA_MISMATCH"
	ErrTEEUnauthorizedUpdate    ErrorCode = "LXP_AGENT_TEE_UNAUTHORIZED_UPDATE"
	ErrNotActive                ErrorCode = "LXP_AGENT_NOT_ACTIVE"
)

func GuardWrite(ctx sdk.Context, readOnly, isDelegate bool) error {
	if isDelegate || ctx.EVMPrecompileCalledFromDelegateCall() {
		return fmt.Errorf("%w: cannot delegatecall an agent function", ErrDelegatecall)
	}
	if readOnly {
		return fmt.Errorf("%w: write under staticcall", ErrReadOnly)
	}
	return nil
}

type heightGateKey struct{}

type heightGate struct {
	upgrade  *upgradekeeper.Keeper
	planName string
}

func WithHeightGate(ctx sdk.Context, upgrade *upgradekeeper.Keeper, planName string) sdk.Context {
	if ctx.Context() == nil {
		return ctx
	}
	return ctx.WithContext(context.WithValue(ctx.Context(), heightGateKey{}, heightGate{upgrade, planName}))
}

func HeightGate(ctx sdk.Context) error {
	if ctx.Context() == nil || ctx.MultiStore() == nil || ctx.GasMeter() == nil {
		return fmt.Errorf("%w: cannot get context from EVM", ErrNoContext)
	}
	gate, ok := ctx.Context().Value(heightGateKey{}).(heightGate)
	if !ok || gate.upgrade == nil || gate.planName == "" {
		return fmt.Errorf("%w: no merge upgrade activation reader bound", ErrNotActive)
	}
	readCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	if !gate.upgrade.IsUpgradeActiveAtHeight(readCtx, gate.planName, ctx.BlockHeight()) {
		return fmt.Errorf("%w: %s not active at height %d", ErrNotActive, gate.planName, ctx.BlockHeight())
	}
	return nil
}

func ValidateArgsLength(args []interface{}, length int) error {
	if err := pcommon.ValidateArgsLength(args, length); err != nil {
		return fmt.Errorf("%w: %v", ErrArgCount, err)
	}
	return nil
}

func ValidateNonPayable(value *big.Int) error {
	if err := pcommon.ValidateNonPayable(value); err != nil {
		return fmt.Errorf("%w: %v", ErrNonPayable, err)
	}
	return nil
}

type Precompile struct {
	*pcommon.Precompile
	executor pcommon.PrecompileExecutor
	upgrade  *upgradekeeper.Keeper
	planName string
}

type boundExecutor struct {
	pcommon.PrecompileExecutor
	upgrade  *upgradekeeper.Keeper
	planName string
}

func (p boundExecutor) RequiredGas(input []byte, method *abi.Method) uint64 {
	if method == nil || len(input) > math.MaxInt-4 {
		return math.MaxUint64
	}
	return MethodGas(method.ID, len(input)+4)
}

func (p boundExecutor) Execute(ctx sdk.Context, method *abi.Method, caller ethcommon.Address, callingContract ethcommon.Address, args []interface{}, value *big.Int, readOnly bool, evm *vm.EVM, hooks *tracing.Hooks) ([]byte, error) {
	return p.PrecompileExecutor.Execute(WithHeightGate(ctx, p.upgrade, p.planName), method, caller, callingContract, args, value, readOnly, evm, hooks)
}

var _ vm.PrecompiledContract = (*Precompile)(nil)

func NewPrecompile(a abi.ABI, executor pcommon.PrecompileExecutor, address ethcommon.Address, name string, upgrade *upgradekeeper.Keeper, planName string) *Precompile {
	return &Precompile{
		Precompile: pcommon.NewPrecompile(a, boundExecutor{executor, upgrade, planName}, address, name).WithRevertReasons(),
		executor:   executor,
		upgrade:    upgrade,
		planName:   planName,
	}
}

func (p *Precompile) RequiredGas(input []byte) uint64 {
	selector, err := pcommon.ExtractMethodID(input)
	if err != nil || p == nil || p.Precompile == nil {
		return pcommon.UnknownMethodCallGas
	}
	if _, err = p.MethodById(selector); err != nil {
		return pcommon.UnknownMethodCallGas
	}
	return MethodGas(selector, len(input))
}

func (p *Precompile) Run(evm *vm.EVM, caller ethcommon.Address, callingContract ethcommon.Address, input []byte, value *big.Int, readOnly bool, isFromDelegateCall bool, hooks *tracing.Hooks) ([]byte, error) {
	if err := p.validateCall(evm, input, value, readOnly, isFromDelegateCall); err != nil {
		if evm != nil {
			pcommon.HandlePrecompileError(err, evm, "layerxagent")
		}
		return pcommon.RevertReason(err), vm.ErrExecutionReverted
	}
	return p.Precompile.Run(evm, caller, callingContract, input, value, readOnly, isFromDelegateCall, hooks)
}

func (p *Precompile) validateCall(evm *vm.EVM, input []byte, value *big.Int, readOnly, isDelegate bool) error {
	if p == nil || p.Precompile == nil || evm == nil {
		return fmt.Errorf("%w: cannot get context from EVM", ErrNoContext)
	}
	db := state.GetDBImpl(evm.StateDB)
	if db == nil {
		return fmt.Errorf("%w: cannot get context from EVM", ErrNoContext)
	}
	ctx := WithHeightGate(db.Ctx(), p.upgrade, p.planName)
	if err := HeightGate(ctx); err != nil {
		return err
	}
	selector, err := pcommon.ExtractMethodID(input)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInputTooShort, err)
	}
	method, err := p.MethodById(selector)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnknownMethod, err)
	}
	_, perByte, known := methodGas(selector)
	if !known {
		return fmt.Errorf("%w: no method with id: %#x", ErrUnknownMethod, selector)
	}
	if perByte == 30 {
		if err := GuardWrite(ctx, readOnly, isDelegate); err != nil {
			return err
		}
	}
	if !method.IsPayable() {
		if err := ValidateNonPayable(value); err != nil {
			return err
		}
	}
	if _, err := method.Inputs.Unpack(input[4:]); err != nil {
		return fmt.Errorf("%w: %v", ErrABIDecode, err)
	}
	if p.executor == nil {
		return fmt.Errorf("%w: precompile executor is unavailable", ErrNoContext)
	}
	return nil
}
