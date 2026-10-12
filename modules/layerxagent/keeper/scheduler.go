package keeper

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode/utf8"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxagent/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
)

// EndBlocker runs the jobs due at the block height in (block, id) order, at
// most MaxJobsPerBlock of them; the rest stay due and roll to the next
// height. Each job runs as its creator in a cached context under its own gas
// limit; gasUsed x baseFee of its deposit goes to the fee collector whether
// it succeeds or fails and the rest returns to the creator. Nothing a job
// does halts the block; the returned error joins the encoding failures of
// outcome events, reported after every due job has run and settled.
func (k Keeper) EndBlocker(ctx sdk.Context) error {
	if ctx.BlockHeight() < 0 || k.Active(ctx) != nil {
		return nil
	}
	params := k.GetParams(ctx)
	var due []Job
	k.IterateDue(ctx, uint64(ctx.BlockHeight()), func(job Job) bool {
		due = append(due, job)
		return uint64(len(due)) >= params.MaxJobsPerBlock
	})
	baseFee := k.baseFee(ctx)
	var errs []error
	for _, job := range due {
		if err := k.runJob(ctx, job, baseFee, params.MaxReasonBytes); err != nil {
			errs = append(errs, fmt.Errorf("job %d: %w", job.ID, err))
		}
	}
	return errors.Join(errs...)
}

func (k Keeper) runJob(ctx sdk.Context, job Job, baseFee *big.Int, reasonCap uint64) error {
	k.DeleteJob(ctx, job)
	gasUsed, reasons := k.execute(ctx, job, baseFee)

	fee := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), baseFee)
	if deposit := job.Deposit.BigInt(); fee.Cmp(deposit) > 0 {
		fee = deposit
	}
	feeAmount := sdk.NewIntFromBigInt(fee)
	refund := job.Deposit.Sub(feeAmount)
	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	if err := k.settle(ctx, collector, feeAmount); err != nil {
		reasons = append(reasons, "fee: "+err.Error())
	}
	if err := k.settle(ctx, k.evm.GetPaxAddressOrDefault(ctx, job.Creator), refund); err != nil {
		reasons = append(reasons, "refund: "+err.Error())
	}

	// No transaction receipt exists at end block, so job outcomes are Cosmos
	// typed events rather than EVM logs.
	if len(reasons) == 0 {
		return ctx.EventManager().EmitTypedEvent(&types.EventJobExecuted{
			JobId:   job.ID,
			Creator: job.Creator.Hex(),
			GasUsed: gasUsed,
			Refund:  refund.String(),
		})
	}
	return ctx.EventManager().EmitTypedEvent(&types.EventJobFailed{
		JobId:   job.ID,
		Creator: job.Creator.Hex(),
		Reason:  truncate(strings.Join(reasons, "; "), reasonCap),
	})
}

// settle pays amount out of escrow in its own cached context so a failed
// transfer leaves no partial state.
func (k Keeper) settle(ctx sdk.Context, to sdk.AccAddress, amount sdk.Int) error {
	cached, write := ctx.CacheContext()
	if err := k.transfer(cached, k.ModuleAddress(), to, amount); err != nil {
		return err
	}
	write()
	ctx.EventManager().EmitEvents(cached.EventManager().Events())
	return nil
}

// execute applies the job as a message from its creator to its target with
// the job's gas limit, priced at baseFee for GASPRICE. State changes are written only on success. It returns
// the gas used and, on failure, the deterministic reason.
func (k Keeper) execute(ctx sdk.Context, job Job, baseFee *big.Int) (gasUsed uint64, reasons []string) {
	cached, write := ctx.CacheContext()
	cached = cached.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(cached))
	defer func() {
		if recovered := recover(); recovered != nil {
			gasUsed, reasons = job.GasLimit, []string{fmt.Sprintf("execution panicked: %v", recovered)}
		}
	}()
	stateDB := state.NewDBImpl(cached, k.evm, false)
	gasPool := k.evm.GetGasPool()
	blockCtx, err := k.evm.GetVMBlockContext(cached, gasPool)
	if err != nil {
		return 0, []string{err.Error()}
	}
	sstore := k.evm.GetSstoreSetGasEIP2200(cached)
	cfg := evmtypes.DefaultChainConfig().EthereumConfigWithSstore(k.evm.ChainID(cached), &sstore)
	target := job.Target
	msg := &core.Message{
		From:      job.Creator,
		To:        &target,
		Value:     new(big.Int),
		GasLimit:  job.GasLimit,
		GasPrice:  new(big.Int),
		GasFeeCap: new(big.Int),
		GasTipCap: new(big.Int),
		Data:      job.CallData,
	}
	txCtx := core.NewEVMTxContext(msg)
	txCtx.GasPrice = new(big.Int).Set(baseFee)
	evm := vm.NewEVM(*blockCtx, stateDB, cfg, vm.Config{}, k.evm.CustomPrecompiles(cached))
	evm.SetTxContext(txCtx)
	result, err := core.NewStateTransition(evm, msg, &gasPool, true, false).Execute()
	if err != nil {
		return 0, []string{err.Error()}
	}
	if result.Err != nil {
		return result.UsedGas, []string{revertReason(result)}
	}
	if _, err := stateDB.Finalize(); err != nil {
		return result.UsedGas, []string{err.Error()}
	}
	write()
	ctx.EventManager().EmitEvents(cached.EventManager().Events())
	return result.UsedGas, nil
}

// revertReason is the Error(string) message of a reverted job, else the
// EVM error.
func revertReason(result *core.ExecutionResult) string {
	if data := result.Revert(); data != nil {
		if reason, err := abi.UnpackRevert(data); err == nil {
			return reason
		}
	}
	return result.Err.Error()
}

// truncate cuts reason to at most limit bytes without splitting a rune.
func truncate(reason string, limit uint64) string {
	if uint64(len(reason)) <= limit {
		return reason
	}
	cut := int(limit)
	for cut > 0 && !utf8.RuneStart(reason[cut]) {
		cut--
	}
	return reason[:cut]
}
