package keeper_test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxagent/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/layerxagent/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles"
	agentcommon "github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/layerxagent/common"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/cachemulti"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/dbadapter"
	storetypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/store/types"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	authtypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/auth/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/tests/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	tmdb "github.com/tendermint/tm-db"
)

const (
	planHeight = int64(900)
	height     = int64(1000)
	dueAt      = uint64(1010)
	later      = uint64(1020)
	gasLimit   = uint64(100_000)
	baseFee    = int64(10)
	// intrinsic is the gas of a call message with empty call data.
	intrinsic = uint64(21_000)
)

var deposit = big.NewInt(2_000_000)

func mounted(parent storetypes.MultiStore, key storetypes.StoreKey) (store storetypes.KVStore, ok bool) {
	defer func() {
		if recover() != nil {
			store, ok = nil, false
		}
	}()
	return parent.GetKVStore(key), true
}

// withStore adds the scheduler store to ctx's multistore next to the app's.
func withStore(ctx sdk.Context, key storetypes.StoreKey) sdk.Context {
	parent := ctx.MultiStore()
	stores := map[storetypes.StoreKey]storetypes.CacheWrapper{}
	keys := map[string]storetypes.StoreKey{}
	for _, existing := range parent.StoreKeys() {
		if store, ok := mounted(parent, existing); ok {
			stores[existing] = store
			keys[existing.Name()] = existing
		}
	}
	stores[key] = dbadapter.Store{DB: tmdb.NewMemDB()}
	keys[key.Name()] = key
	return ctx.WithMultiStore(cachemulti.NewStore(tmdb.NewMemDB(), stores, keys, nil, nil, nil, 0))
}

type harness struct {
	t          *testing.T
	ctx        sdk.Context
	key        storetypes.StoreKey
	keeper     keeper.Keeper
	creatorAcc sdk.AccAddress
	creator    common.Address
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	base, _ := app.NewContext(false, tmtypes.Header{}).WithBlockTime(time.Unix(1_800_000_000, 0).UTC()).CacheContext()
	key := sdk.NewKVStoreKey(keeper.StoreKey)
	ctx := withStore(base, key)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	app.EvmKeeper.UpgradeKeeper().SetDone(ctx.WithBlockHeight(planHeight), precompiles.MergeUpgrade)
	ctx = ctx.WithBlockHeight(height)
	app.EvmKeeper.SetNextBaseFeePerGas(ctx, sdk.NewDec(baseFee))
	h := &harness{t: t, ctx: ctx, key: key, keeper: keeper.NewKeeper(key, &app.EvmKeeper, precompiles.MergeUpgrade)}
	h.creatorAcc, h.creator = testkeeper.MockAddressPair()
	app.EvmKeeper.SetAddressMapping(ctx, h.creatorAcc, h.creator)
	coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(10_000)))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, "evm", coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, "evm", h.creatorAcc, coins))
	return h
}

func (h *harness) balance(ctx sdk.Context, addr sdk.AccAddress) *big.Int {
	return testkeeper.EVMTestApp.EvmKeeper.GetBalance(ctx, addr)
}

func (h *harness) schedule(target common.Address, at uint64) uint64 {
	h.t.Helper()
	id, err := h.keeper.Schedule(h.ctx, h.creatorAcc, h.creator, target, nil, at, gasLimit, deposit)
	require.NoError(h.t, err)
	return id
}

// deploy installs code at a fresh address.
func (h *harness) deploy(code []byte) common.Address {
	_, addr := testkeeper.MockAddressPair()
	testkeeper.EVMTestApp.EvmKeeper.SetCode(h.ctx, addr, code)
	return addr
}

// burner is runtime code spending exactly gas before tail runs.
func burner(gas uint64, tail []byte) []byte {
	var code []byte
	for ; gas >= 5; gas -= 5 {
		code = append(code, byte(vm.PUSH1), 0, byte(vm.POP))
	}
	for ; gas > 0; gas-- {
		code = append(code, byte(vm.JUMPDEST))
	}
	return append(code, tail...)
}

// reverter is runtime code that spends burn gas, then reverts with
// Error(reason); its revert tail costs 42 gas for a 100-byte payload.
func reverter(t *testing.T, burn uint64, reason string) []byte {
	t.Helper()
	str, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	encoded, err := abi.Arguments{{Type: str}}.Pack(reason)
	require.NoError(t, err)
	payload := append([]byte{0x08, 0xc3, 0x79, 0xa0}, encoded...)
	require.Len(t, payload, 100)
	head := burner(burn, nil)
	offset := len(head) + 13
	code := append(head,
		byte(vm.PUSH1), byte(len(payload)), byte(vm.PUSH2), byte(offset>>8), byte(offset), byte(vm.PUSH1), 0,
		byte(vm.CODECOPY), byte(vm.PUSH1), byte(len(payload)), byte(vm.PUSH1), 0, byte(vm.REVERT))
	require.Len(t, code, offset)
	return append(code, payload...)
}

// outcomes parses every typed event of kind's message type in ctx.
func outcomes[T proto.Message](t *testing.T, ctx sdk.Context, kind T) []T {
	t.Helper()
	var out []T
	for _, event := range ctx.EventManager().Events() {
		if event.Type != proto.MessageName(kind) {
			continue
		}
		parsed, err := sdk.ParseTypedEvent(abci.Event(event))
		require.NoError(t, err)
		typed, ok := parsed.(T)
		require.True(t, ok, event.Type)
		out = append(out, typed)
	}
	return out
}

func executedEvents(t *testing.T, ctx sdk.Context) []*types.EventJobExecuted {
	return outcomes(t, ctx, &types.EventJobExecuted{})
}

func failedEvents(t *testing.T, ctx sdk.Context) []*types.EventJobFailed {
	return outcomes(t, ctx, &types.EventJobFailed{})
}

func executed(id uint64, creator common.Address, gasUsed uint64, refund int64) *types.EventJobExecuted {
	return &types.EventJobExecuted{JobId: id, Creator: creator.Hex(), GasUsed: gasUsed, Refund: fmt.Sprint(refund)}
}

func failed(id uint64, creator common.Address, reason string) *types.EventJobFailed {
	return &types.EventJobFailed{JobId: id, Creator: creator.Hex(), Reason: reason}
}

func TestMaxJobsPerCreator(t *testing.T) {
	h := newHarness(t)
	target := h.deploy(burner(0, []byte{byte(vm.STOP)}))
	for i := uint64(1); i <= keeper.DefaultMaxJobsPerCreator; i++ {
		require.Equal(t, i, h.schedule(target, later))
	}
	module := h.keeper.ModuleAddress()
	escrow := h.balance(h.ctx, module)
	require.Equal(t, new(big.Int).Mul(deposit, big.NewInt(1024)), escrow)
	require.Equal(t, uint64(1025), h.keeper.NextJobID(h.ctx))

	_, err := h.keeper.Schedule(h.ctx, h.creatorAcc, h.creator, target, nil, later, gasLimit, deposit)
	require.ErrorIs(t, err, agentcommon.ErrSchedulerMaxJobs)
	require.EqualError(t, err, "LXP_AGENT_SCHEDULER_MAX_JOBS: "+h.creator.Hex()+" has 1024 active jobs")
	require.Equal(t, uint64(1025), h.keeper.NextJobID(h.ctx))
	require.Equal(t, escrow, h.balance(h.ctx, module))
	pending := h.keeper.Pending(h.ctx, h.creator)
	require.Len(t, pending, 1024)
	require.Equal(t, uint64(1), pending[0])
	require.Equal(t, uint64(1024), pending[1023])

	// Another creator is not bound by A's cap.
	otherAcc, other := testkeeper.MockAddressPair()
	testkeeper.EVMTestApp.EvmKeeper.SetAddressMapping(h.ctx, otherAcc, other)
	id, err := h.keeper.Schedule(h.ctx, h.creatorAcc, other, target, nil, later, gasLimit, deposit)
	require.NoError(t, err)
	require.Equal(t, uint64(1025), id)
}

func TestScheduleRefusals(t *testing.T) {
	h := newHarness(t)
	target := h.deploy([]byte{byte(vm.STOP)})
	low := new(big.Int).Sub(deposit, big.NewInt(1))
	for _, c := range []struct {
		target  common.Address
		at, gas uint64
		deposit *big.Int
		code    agentcommon.ErrorCode
		message string
	}{
		{common.Address{}, dueAt, gasLimit, deposit, agentcommon.ErrSchedulerInvalidTarget, "zero target"},
		{target, dueAt, 0, deposit, agentcommon.ErrSchedulerInvalidGasLimit, "gas limit 0, cap 10000000"},
		{target, dueAt, keeper.DefaultMaxJobGasLimit + 1, deposit, agentcommon.ErrSchedulerInvalidGasLimit, "gas limit 10000001, cap 10000000"},
		{target, uint64(height), gasLimit, deposit, agentcommon.ErrSchedulerPastBlock, "block 1000 at height 1000"},
		{target, uint64(height) + keeper.DefaultMaxScheduleHorizon + 1, gasLimit, deposit, agentcommon.ErrSchedulerHorizonExceeded, "block 2593001 beyond height 1000 + 2592000"},
		{target, dueAt, gasLimit, low, agentcommon.ErrSchedulerDepositTooLow, "deposit 1999999 below 2000000"},
	} {
		_, err := h.keeper.Schedule(h.ctx, h.creatorAcc, h.creator, c.target, nil, c.at, c.gas, c.deposit)
		require.ErrorIs(t, err, c.code)
		require.EqualError(t, err, string(c.code)+": "+c.message)
	}
	require.Equal(t, uint64(1), h.keeper.NextJobID(h.ctx))
	require.Empty(t, h.keeper.Pending(h.ctx, h.creator))
	require.Equal(t, uint64(1), h.schedule(target, uint64(height)+keeper.DefaultMaxScheduleHorizon))
}

func TestCancelRefundsDeposit(t *testing.T) {
	h := newHarness(t)
	target := h.deploy([]byte{byte(vm.STOP)})
	id := h.schedule(target, dueAt)
	module := h.keeper.ModuleAddress()
	before := h.balance(h.ctx, h.creatorAcc)
	_, intruder := testkeeper.MockAddressPair()

	_, err := h.keeper.Cancel(h.ctx, intruder, id)
	require.ErrorIs(t, err, agentcommon.ErrSchedulerUnauthorized)
	_, err = h.keeper.Cancel(h.ctx, h.creator, id+1)
	require.ErrorIs(t, err, agentcommon.ErrSchedulerJobNotFound)
	require.ErrorIs(t, h.keeper.Reschedule(h.ctx, h.creator, id, uint64(height)-4), agentcommon.ErrSchedulerPastBlock)
	require.NoError(t, h.keeper.Reschedule(h.ctx, h.creator, id, later))
	job, found := h.keeper.GetJob(h.ctx, id)
	require.True(t, found)
	require.Equal(t, later, job.ExecuteAtBlock)

	refund, err := h.keeper.Cancel(h.ctx, h.creator, id)
	require.NoError(t, err)
	require.Equal(t, sdk.NewIntFromBigInt(deposit), refund)
	require.Equal(t, new(big.Int).Add(before, deposit), h.balance(h.ctx, h.creatorAcc))
	require.Zero(t, h.balance(h.ctx, module).Sign())
	_, found = h.keeper.GetJob(h.ctx, id)
	require.False(t, found)
	require.Empty(t, h.keeper.Pending(h.ctx, h.creator))
	_, err = h.keeper.Cancel(h.ctx, h.creator, id)
	require.ErrorIs(t, err, agentcommon.ErrSchedulerJobNotFound)

	// The job no longer runs once cancelled.
	end := h.ctx.WithBlockHeight(int64(later)).WithEventManager(sdk.NewEventManager())
	require.NoError(t, h.keeper.EndBlocker(end))
	require.Empty(t, executedEvents(t, end))
	require.Empty(t, failedEvents(t, end))
}

func TestEndBlockRunsDueJobsInIdOrder(t *testing.T) {
	h := newHarness(t)
	idle := h.deploy([]byte{byte(vm.STOP)})
	require.Equal(t, uint64(1), h.schedule(idle, later))
	require.Equal(t, uint64(2), h.schedule(idle, later))
	spender := h.deploy(burner(40_000-intrinsic, []byte{byte(vm.STOP)}))
	require.Equal(t, uint64(3), h.schedule(spender, dueAt))
	require.Equal(t, uint64(4), h.schedule(idle, dueAt))

	module := h.keeper.ModuleAddress()
	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	end := h.ctx.WithBlockHeight(int64(dueAt)).WithEventManager(sdk.NewEventManager())

	// Without an active plan nothing runs.
	require.NoError(t, keeper.NewKeeper(h.key, &testkeeper.EVMTestApp.EvmKeeper, "absent-plan").EndBlocker(end))
	require.Empty(t, end.EventManager().Events())
	require.Equal(t, []uint64{1, 2, 3, 4}, h.keeper.Pending(end, h.creator))

	collectorBefore := h.balance(end, collector)
	creatorBefore := h.balance(end, h.creatorAcc)
	require.NoError(t, h.keeper.EndBlocker(end))

	require.Equal(t, []*types.EventJobExecuted{
		executed(3, h.creator, 40_000, 1_600_000),
		executed(4, h.creator, intrinsic, 2_000_000-210_000),
	}, executedEvents(t, end))
	require.Empty(t, failedEvents(t, end))
	require.Equal(t, new(big.Int).Add(collectorBefore, big.NewInt(400_000+210_000)), h.balance(end, collector))
	require.Equal(t, new(big.Int).Add(creatorBefore, big.NewInt(1_600_000+1_790_000)), h.balance(end, h.creatorAcc))
	require.Equal(t, new(big.Int).Mul(deposit, big.NewInt(2)), h.balance(end, module))
	for _, id := range []uint64{3, 4} {
		_, found := h.keeper.GetJob(end, id)
		require.False(t, found, id)
	}
	require.Equal(t, []uint64{1, 2}, h.keeper.Pending(end, h.creator))
}

func TestEndBlockFailureAndCap(t *testing.T) {
	h := newHarness(t)
	idle := h.deploy([]byte{byte(vm.STOP)})
	for i := uint64(1); i <= 4; i++ {
		require.Equal(t, i, h.schedule(idle, later))
	}
	failing := h.deploy(reverter(t, 30_000-intrinsic-42, "boom"))
	require.Equal(t, uint64(5), h.schedule(failing, dueAt))

	module := h.keeper.ModuleAddress()
	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	end := h.ctx.WithBlockHeight(int64(dueAt)).WithEventManager(sdk.NewEventManager())
	collectorBefore := h.balance(end, collector)
	creatorBefore := h.balance(end, h.creatorAcc)
	require.NoError(t, h.keeper.EndBlocker(end))
	require.Equal(t, []*types.EventJobFailed{failed(5, h.creator, "boom")}, failedEvents(t, end))
	require.Empty(t, executedEvents(t, end))
	require.Equal(t, new(big.Int).Add(collectorBefore, big.NewInt(300_000)), h.balance(end, collector))
	require.Equal(t, new(big.Int).Add(creatorBefore, big.NewInt(1_700_000)), h.balance(end, h.creatorAcc))
	_, found := h.keeper.GetJob(end, 5)
	require.False(t, found)
	require.Equal(t, []uint64{1, 2, 3, 4}, h.keeper.Pending(end, h.creator))
	require.Equal(t, new(big.Int).Mul(deposit, big.NewInt(4)), h.balance(end, module))

	// The reason is truncated to the cap.
	params := keeper.DefaultParams()
	params.MaxReasonBytes = 3
	params.MaxJobsPerBlock = 2
	require.NoError(t, h.keeper.SetParams(end, params))
	h.ctx = end.WithBlockHeight(int64(dueAt))
	require.Equal(t, uint64(6), h.schedule(failing, dueAt+1))
	next := end.WithBlockHeight(int64(dueAt) + 1).WithEventManager(sdk.NewEventManager())
	require.NoError(t, h.keeper.EndBlocker(next))
	require.Equal(t, []*types.EventJobFailed{failed(6, h.creator, "boo")}, failedEvents(t, next))

	// With a cap of two, the third due job rolls to the next height.
	h.ctx = next
	for _, want := range []uint64{7, 8, 9} {
		require.Equal(t, want, h.schedule(idle, dueAt+2))
	}
	capped := next.WithBlockHeight(int64(dueAt) + 2).WithEventManager(sdk.NewEventManager())
	require.NoError(t, h.keeper.EndBlocker(capped))
	require.Equal(t, []*types.EventJobExecuted{
		executed(7, h.creator, intrinsic, 2_000_000-210_000),
		executed(8, h.creator, intrinsic, 2_000_000-210_000),
	}, executedEvents(t, capped))
	job, found := h.keeper.GetJob(capped, 9)
	require.True(t, found)
	require.Equal(t, dueAt+2, job.ExecuteAtBlock)

	rolled := capped.WithBlockHeight(int64(dueAt) + 3).WithEventManager(sdk.NewEventManager())
	require.NoError(t, h.keeper.EndBlocker(rolled))
	require.Equal(t, []*types.EventJobExecuted{executed(9, h.creator, intrinsic, 2_000_000-210_000)},
		executedEvents(t, rolled))
	_, found = h.keeper.GetJob(rolled, 9)
	require.False(t, found)
	require.Equal(t, []uint64{1, 2, 3, 4}, h.keeper.Pending(rolled, h.creator))
}

// forwarded is the wei a coinbase forwarder sends: one uhpx and half of one.
var forwarded = big.NewInt(1_500_000_000_000)

// forwarder is runtime code that sends forwarded wei to block.coinbase, then
// runs tail.
func forwarder(tail ...byte) []byte {
	value := make([]byte, 8)
	forwarded.FillBytes(value)
	code := []byte{byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.PUSH8)}
	code = append(code, value...)
	code = append(code, byte(vm.COINBASE), byte(vm.GAS), byte(vm.CALL), byte(vm.POP))
	return append(code, tail...)
}

// fund mints uhpx to the account of the EVM address addr.
func (h *harness) fund(addr common.Address, uhpx int64) sdk.AccAddress {
	h.t.Helper()
	app := testkeeper.EVMTestApp
	acc := app.EvmKeeper.GetPaxAddressOrDefault(h.ctx, addr)
	coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(uhpx)))
	require.NoError(h.t, app.BankKeeper.MintCoins(h.ctx, "evm", coins))
	require.NoError(h.t, app.BankKeeper.SendCoinsFromModuleToAccount(h.ctx, "evm", acc, coins))
	return acc
}

func TestEndBlockSweepsCoinbaseTransfer(t *testing.T) {
	h := newHarness(t)
	target := h.deploy(forwarder(byte(vm.STOP)))
	targetAcc := h.fund(target, 2)
	require.Equal(t, uint64(1), h.schedule(target, dueAt))

	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	coinbase := state.GetCoinbaseAddress(0)
	end := h.ctx.WithBlockHeight(int64(dueAt)).WithEventManager(sdk.NewEventManager())
	require.Equal(t, 0, end.TxIndex())
	collectorBefore := h.balance(end, collector)
	creatorBefore := h.balance(end, h.creatorAcc)
	targetBefore := h.balance(end, targetAcc)
	require.Zero(t, h.balance(end, coinbase).Sign())
	require.NoError(t, h.keeper.EndBlocker(end))

	require.Empty(t, failedEvents(t, end))
	events := executedEvents(t, end)
	require.Len(t, events, 1)
	gasUsed := events[0].GasUsed
	require.Greater(t, gasUsed, intrinsic)
	require.LessOrEqual(t, gasUsed, gasLimit)
	fee := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), big.NewInt(baseFee))
	refund := new(big.Int).Sub(deposit, fee)
	require.Equal(t, executed(1, h.creator, gasUsed, refund.Int64()), events[0])

	require.Equal(t, new(big.Int).Sub(targetBefore, forwarded), h.balance(end, targetAcc))
	bank := testkeeper.EVMTestApp.BankKeeper
	require.True(t, bank.GetBalance(end, coinbase, sdk.MustGetBaseDenom()).Amount.IsZero())
	require.True(t, bank.GetWeiBalance(end, coinbase).IsZero())
	require.Equal(t, new(big.Int).Add(collectorBefore, new(big.Int).Add(fee, forwarded)), h.balance(end, collector))
	require.Equal(t, new(big.Int).Add(creatorBefore, refund), h.balance(end, h.creatorAcc))
	require.Zero(t, h.balance(end, h.keeper.ModuleAddress()).Sign())
}

func TestEndBlockRevertedJobLeavesCoinbaseEmpty(t *testing.T) {
	h := newHarness(t)
	target := h.deploy(forwarder(byte(vm.PUSH1), 0, byte(vm.PUSH1), 0, byte(vm.REVERT)))
	targetAcc := h.fund(target, 2)
	require.Equal(t, uint64(1), h.schedule(target, dueAt))

	collector := authtypes.NewModuleAddress(authtypes.FeeCollectorName)
	coinbase := state.GetCoinbaseAddress(0)
	end := h.ctx.WithBlockHeight(int64(dueAt)).WithEventManager(sdk.NewEventManager())
	require.Equal(t, 0, end.TxIndex())
	collectorBefore := h.balance(end, collector)
	creatorBefore := h.balance(end, h.creatorAcc)
	targetBefore := h.balance(end, targetAcc)
	coinbaseBefore := h.balance(end, coinbase)
	require.NoError(t, h.keeper.EndBlocker(end))

	require.Empty(t, executedEvents(t, end))
	require.Equal(t, []*types.EventJobFailed{failed(1, h.creator, "execution reverted")}, failedEvents(t, end))
	require.Equal(t, targetBefore, h.balance(end, targetAcc))
	require.Equal(t, coinbaseBefore, h.balance(end, coinbase))

	// Only the fee leg reaches the collector: what it gains is exactly what
	// the creator's refund falls short of the deposit.
	fee := new(big.Int).Sub(h.balance(end, collector), collectorBefore)
	require.Equal(t, new(big.Int).Sub(deposit, fee), new(big.Int).Sub(h.balance(end, h.creatorAcc), creatorBefore))
	gas, rem := new(big.Int).QuoRem(fee, big.NewInt(baseFee), new(big.Int))
	require.Zero(t, rem.Sign())
	require.Greater(t, gas.Uint64(), intrinsic)
	require.LessOrEqual(t, gas.Uint64(), gasLimit)
	require.Zero(t, h.balance(end, h.keeper.ModuleAddress()).Sign())
}
