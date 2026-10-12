// Package keeper holds the layerxagent scheduler state: the parameters, the
// next job id, every active job with its due and creator indexes, and the
// deposits escrowed by the module account until a job runs or is cancelled.
package keeper

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"

	evmkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/prefix"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	"github.com/ethereum/go-ethereum/common"
)

const (
	ModuleName = "layerxagent"
	StoreKey   = ModuleName
)

// Store prefixes: due (block BE, id BE), creator index (creator, id BE), job
// by id (id BE), params and next id.
var (
	DueKeyPrefix     = []byte{0x01}
	CreatorKeyPrefix = []byte{0x02}
	JobKeyPrefix     = []byte{0x03}
	ParamsKey        = []byte{0x04}
	NextJobIDKey     = []byte{0x05}
)

const (
	DefaultMaxJobsPerCreator  uint64 = 1024
	DefaultMaxScheduleHorizon uint64 = 2_592_000
	DefaultMinDepositFactor   uint64 = 2
	DefaultMaxJobsPerBlock    uint64 = 64
	DefaultMaxJobGasLimit     uint64 = 10_000_000
	DefaultMaxReasonBytes     uint64 = 256
)

// Params bound the scheduler. The per-block job cap, the per-job gas limit
// cap and the JobFailed reason cap are set at activation rehearsal.
type Params struct {
	MaxJobsPerCreator  uint64 `json:"max_jobs_per_creator"`
	MaxScheduleHorizon uint64 `json:"max_schedule_horizon"`
	MinDepositFactor   uint64 `json:"min_deposit_factor"`
	MaxJobsPerBlock    uint64 `json:"max_jobs_per_block"`
	MaxJobGasLimit     uint64 `json:"max_job_gas_limit"`
	MaxReasonBytes     uint64 `json:"max_reason_bytes"`
}

func DefaultParams() Params {
	return Params{
		MaxJobsPerCreator:  DefaultMaxJobsPerCreator,
		MaxScheduleHorizon: DefaultMaxScheduleHorizon,
		MinDepositFactor:   DefaultMinDepositFactor,
		MaxJobsPerBlock:    DefaultMaxJobsPerBlock,
		MaxJobGasLimit:     DefaultMaxJobGasLimit,
		MaxReasonBytes:     DefaultMaxReasonBytes,
	}
}

func (p Params) Validate() error {
	for _, field := range []struct {
		name  string
		value uint64
	}{
		{"max jobs per creator", p.MaxJobsPerCreator}, {"max schedule horizon", p.MaxScheduleHorizon},
		{"min deposit factor", p.MinDepositFactor}, {"max jobs per block", p.MaxJobsPerBlock},
		{"max job gas limit", p.MaxJobGasLimit}, {"max reason bytes", p.MaxReasonBytes},
	} {
		if field.value == 0 {
			return fmt.Errorf("layerxagent params: %s must be positive", field.name)
		}
	}
	return nil
}

// Job is one scheduled call. Deposit is in wei and is held by the module
// account while the job is stored.
type Job struct {
	ID             uint64         `json:"id"`
	Creator        common.Address `json:"creator"`
	Target         common.Address `json:"target"`
	CallData       []byte         `json:"call_data"`
	ExecuteAtBlock uint64         `json:"execute_at_block"`
	GasLimit       uint64         `json:"gas_limit"`
	Deposit        sdk.Int        `json:"deposit"`
	Active         bool           `json:"active"`
}

type Keeper struct {
	storeKey sdk.StoreKey
	evm      *evmkeeper.Keeper
	planName string
}

// NewKeeper builds the scheduler keeper. The end-block hook runs only once
// planName is active at the block height.
func NewKeeper(storeKey sdk.StoreKey, evm *evmkeeper.Keeper, planName string) Keeper {
	return Keeper{storeKey: storeKey, evm: evm, planName: planName}
}

// ModuleAddress is the account that escrows every stored job's deposit.
func (k Keeper) ModuleAddress() sdk.AccAddress { return authtypes.NewModuleAddress(ModuleName) }

// Active reports whether the scheduler runs at ctx.BlockHeight(). It fails
// closed without a bound plan.
func (k Keeper) Active(ctx sdk.Context) error {
	if k.evm == nil || k.planName == "" {
		return fmt.Errorf("%w: no merge upgrade activation reader bound", agentcommon.ErrNotActive)
	}
	readCtx := ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	if !k.evm.UpgradeKeeper().IsUpgradeActiveAtHeight(readCtx, k.planName, ctx.BlockHeight()) {
		return fmt.Errorf("%w: %s not active at height %d", agentcommon.ErrNotActive, k.planName, ctx.BlockHeight())
	}
	return nil
}

func (k Keeper) store(ctx sdk.Context) sdk.KVStore { return ctx.KVStore(k.storeKey) }

func (k Keeper) GetParams(ctx sdk.Context) Params {
	encoded := k.store(ctx).Get(ParamsKey)
	if encoded == nil {
		return DefaultParams()
	}
	var params Params
	if err := json.Unmarshal(encoded, &params); err != nil {
		panic(fmt.Errorf("layerxagent params: %w", err))
	}
	return params
}

func (k Keeper) SetParams(ctx sdk.Context, params Params) error {
	if err := params.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	k.store(ctx).Set(ParamsKey, encoded)
	return nil
}

// NextJobID is the id the next scheduled job receives; ids start at 1.
func (k Keeper) NextJobID(ctx sdk.Context) uint64 {
	encoded := k.store(ctx).Get(NextJobIDKey)
	if encoded == nil {
		return 1
	}
	return binary.BigEndian.Uint64(encoded)
}

func (k Keeper) setNextJobID(ctx sdk.Context, id uint64) {
	k.store(ctx).Set(NextJobIDKey, binary.BigEndian.AppendUint64(nil, id))
}

func jobKey(id uint64) []byte {
	return binary.BigEndian.AppendUint64(append([]byte{}, JobKeyPrefix...), id)
}

func dueKey(block, id uint64) []byte {
	return binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(append([]byte{}, DueKeyPrefix...), block), id)
}

func creatorPrefix(creator common.Address) []byte {
	return append(append([]byte{}, CreatorKeyPrefix...), creator.Bytes()...)
}

func creatorKey(creator common.Address, id uint64) []byte {
	return binary.BigEndian.AppendUint64(creatorPrefix(creator), id)
}

func (k Keeper) GetJob(ctx sdk.Context, id uint64) (Job, bool) {
	encoded := k.store(ctx).Get(jobKey(id))
	if encoded == nil {
		return Job{}, false
	}
	var job Job
	if err := json.Unmarshal(encoded, &job); err != nil {
		panic(fmt.Errorf("layerxagent job %d: %w", id, err))
	}
	return job, true
}

// SetJob stores job with its due and creator indexes, moving the due entry
// when the job already exists under another block.
func (k Keeper) SetJob(ctx sdk.Context, job Job) {
	store := k.store(ctx)
	if previous, found := k.GetJob(ctx, job.ID); found {
		store.Delete(dueKey(previous.ExecuteAtBlock, previous.ID))
		store.Delete(creatorKey(previous.Creator, previous.ID))
	}
	encoded, err := json.Marshal(job)
	if err != nil {
		panic(fmt.Errorf("layerxagent job %d: %w", job.ID, err))
	}
	store.Set(jobKey(job.ID), encoded)
	store.Set(dueKey(job.ExecuteAtBlock, job.ID), []byte{1})
	store.Set(creatorKey(job.Creator, job.ID), []byte{1})
}

// DeleteJob removes job and its indexes.
func (k Keeper) DeleteJob(ctx sdk.Context, job Job) {
	store := k.store(ctx)
	store.Delete(jobKey(job.ID))
	store.Delete(dueKey(job.ExecuteAtBlock, job.ID))
	store.Delete(creatorKey(job.Creator, job.ID))
}

// IterateDue visits the jobs due at or before height in (block, id)
// big-endian order until visit returns true.
func (k Keeper) IterateDue(ctx sdk.Context, height uint64, visit func(Job) (stop bool)) {
	iterator := prefix.NewStore(k.store(ctx), DueKeyPrefix).Iterator(nil, binary.BigEndian.AppendUint64(nil, height+1))
	defer func() { _ = iterator.Close() }()
	for ; iterator.Valid(); iterator.Next() {
		id := binary.BigEndian.Uint64(iterator.Key()[8:])
		job, found := k.GetJob(ctx, id)
		if !found {
			panic(fmt.Errorf("layerxagent due index names missing job %d", id))
		}
		if visit(job) {
			return
		}
	}
}

// Pending returns the creator's active job ids in id order; the creator cap
// bounds its length.
func (k Keeper) Pending(ctx sdk.Context, creator common.Address) []uint64 {
	iterator := prefix.NewStore(k.store(ctx), creatorPrefix(creator)).Iterator(nil, nil)
	defer func() { _ = iterator.Close() }()
	ids := []uint64{}
	for ; iterator.Valid(); iterator.Next() {
		ids = append(ids, binary.BigEndian.Uint64(iterator.Key()))
	}
	return ids
}

func (k Keeper) baseFee(ctx sdk.Context) *big.Int {
	if fee := k.evm.GetBaseFee(ctx); fee != nil {
		return fee
	}
	return new(big.Int)
}

// transfer moves wei between accounts through the bank's coin and wei
// balances.
func (k Keeper) transfer(ctx sdk.Context, from, to sdk.AccAddress, amount sdk.Int) error {
	if amount.IsZero() {
		return nil
	}
	coins, wei := state.SplitUhpxWeiAmount(amount.BigInt())
	return k.evm.BankKeeper().SendCoinsAndWei(ctx, from, to, coins, wei)
}

// Schedule validates a job for creator in the order target, gas limit, past
// block, horizon, creator cap and deposit, escrows deposit from payer into
// the module account and stores the job under the next id.
func (k Keeper) Schedule(ctx sdk.Context, payer sdk.AccAddress, creator, target common.Address, callData []byte,
	executeAtBlock, gasLimit uint64, deposit *big.Int) (uint64, error) {
	params := k.GetParams(ctx)
	if target == (common.Address{}) {
		return 0, fmt.Errorf("%w: zero target", agentcommon.ErrSchedulerInvalidTarget)
	}
	if gasLimit == 0 || gasLimit > params.MaxJobGasLimit {
		return 0, fmt.Errorf("%w: gas limit %d, cap %d", agentcommon.ErrSchedulerInvalidGasLimit, gasLimit, params.MaxJobGasLimit)
	}
	height := uint64(max(ctx.BlockHeight(), 0))
	if executeAtBlock <= height {
		return 0, fmt.Errorf("%w: block %d at height %d", agentcommon.ErrSchedulerPastBlock, executeAtBlock, height)
	}
	if executeAtBlock-height > params.MaxScheduleHorizon {
		return 0, fmt.Errorf("%w: block %d beyond height %d + %d", agentcommon.ErrSchedulerHorizonExceeded,
			executeAtBlock, height, params.MaxScheduleHorizon)
	}
	if active := uint64(len(k.Pending(ctx, creator))); active >= params.MaxJobsPerCreator {
		return 0, fmt.Errorf("%w: %s has %d active jobs", agentcommon.ErrSchedulerMaxJobs, creator.Hex(), active)
	}
	minimum := new(big.Int).SetUint64(gasLimit)
	minimum.Mul(minimum, k.baseFee(ctx))
	minimum.Mul(minimum, new(big.Int).SetUint64(params.MinDepositFactor))
	if deposit == nil || deposit.Cmp(minimum) < 0 {
		return 0, fmt.Errorf("%w: deposit %v below %s", agentcommon.ErrSchedulerDepositTooLow, deposit, minimum)
	}
	amount := sdk.NewIntFromBigInt(deposit)
	if err := k.transfer(ctx, payer, k.ModuleAddress(), amount); err != nil {
		return 0, fmt.Errorf("%w: %v", agentcommon.ErrSchedulerEscrow, err)
	}
	id := k.NextJobID(ctx)
	k.setNextJobID(ctx, id+1)
	k.SetJob(ctx, Job{ID: id, Creator: creator, Target: target, CallData: append([]byte(nil), callData...),
		ExecuteAtBlock: executeAtBlock, GasLimit: gasLimit, Deposit: amount, Active: true})
	return id, nil
}

func (k Keeper) ownedJob(ctx sdk.Context, caller common.Address, id uint64) (Job, error) {
	job, found := k.GetJob(ctx, id)
	if !found {
		return Job{}, fmt.Errorf("%w: job %d", agentcommon.ErrSchedulerJobNotFound, id)
	}
	if !job.Active {
		return Job{}, fmt.Errorf("%w: job %d", agentcommon.ErrSchedulerJobInactive, id)
	}
	if job.Creator != caller {
		return Job{}, fmt.Errorf("%w: %s did not create job %d", agentcommon.ErrSchedulerUnauthorized, caller.Hex(), id)
	}
	return job, nil
}

// Cancel deletes the caller's job and refunds its whole remaining deposit to
// the creator.
func (k Keeper) Cancel(ctx sdk.Context, caller common.Address, id uint64) (sdk.Int, error) {
	job, err := k.ownedJob(ctx, caller, id)
	if err != nil {
		return sdk.Int{}, err
	}
	if err := k.transfer(ctx, k.ModuleAddress(), k.evm.GetPaxAddressOrDefault(ctx, job.Creator), job.Deposit); err != nil {
		return sdk.Int{}, err
	}
	k.DeleteJob(ctx, job)
	return job.Deposit, nil
}

// Reschedule moves the caller's job to newBlock under the schedule bounds.
func (k Keeper) Reschedule(ctx sdk.Context, caller common.Address, id, newBlock uint64) error {
	job, err := k.ownedJob(ctx, caller, id)
	if err != nil {
		return err
	}
	height := uint64(max(ctx.BlockHeight(), 0))
	if newBlock <= height {
		return fmt.Errorf("%w: block %d at height %d", agentcommon.ErrSchedulerPastBlock, newBlock, height)
	}
	if horizon := k.GetParams(ctx).MaxScheduleHorizon; newBlock-height > horizon {
		return fmt.Errorf("%w: block %d beyond height %d + %d", agentcommon.ErrSchedulerHorizonExceeded, newBlock, height, horizon)
	}
	job.ExecuteAtBlock = newBlock
	k.SetJob(ctx, job)
	return nil
}
