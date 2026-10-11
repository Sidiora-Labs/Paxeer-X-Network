package xweb_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	xwebkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/keeper"
	xwebtestutil "github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/testutil"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/xweb/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/xweb"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

const (
	fee         = int64(1_000_003)
	callbackGas = uint64(200_000)
	timeout     = uint64(100)
	startHeight = int64(8)
	payloadCap  = uint32(256)
	callbackCap = uint64(300_000)
	supplied    = uint64(2_000_000)
)

var (
	precompile = common.HexToAddress(xweb.XWebAddress)
	authority  = types.DefaultAuthority()
	payload    = []byte("https://paxeer.app/")
	digest     = [32]byte{0x22, 0x22, 0x22}
	response   = []byte("Paxeer X Network")
	fullLength = uint32(4096)

	// recorder stores the request id in slot 0, its caller in slot 1 and the
	// content digest in slot 2, then stops.
	recorderCode = common.FromHex("6004356000553360015560243560025500")
	// reverter reverts with no data.
	reverterCode = common.FromHex("60006000fd")
	// burner loops until it runs out of gas.
	burnerCode = common.FromHex("5b600056")

	recorder = common.HexToAddress("0x00000000000000000000000000000000c0de0001")
	reverter = common.HexToAddress("0x00000000000000000000000000000000c0de0002")
	burner   = common.HexToAddress("0x00000000000000000000000000000000c0de0003")
	stranger = common.HexToAddress("0x00000000000000000000000000000000c0de0004")
)

type keepers struct {
	utils.Keepers
	xweb *xwebkeeper.Keeper
}

func (k keepers) XWebK() *xwebkeeper.Keeper { return k.xweb }

type harness struct {
	t          *testing.T
	stateDB    *state.DBImpl
	evm        *vm.EVM
	precompile *xweb.Precompile
	keeper     *xwebkeeper.Keeper
	attestors  []xwebtestutil.Attestor
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockHeight(startHeight).WithBlockTime(time.Unix(1_800_000_000, 0))
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	k, ctx := xwebtestutil.NewKeeper(app, ctx)
	k.InitGenesis(ctx, *types.DefaultGenesis())
	h := &harness{t: t, keeper: &k, attestors: xwebtestutil.Attestors(3)}
	for _, attestor := range h.attestors {
		require.NoError(t, k.RegisterAttestor(ctx, types.MsgRegisterAttestor{Authority: authority,
			Attestor: attestor.Registration()}))
	}
	require.NoError(t, k.UpdateParams(ctx, types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout}))
	require.NoError(t, k.Unpause(ctx, types.MsgUnpause{Authority: authority}))
	var err error
	h.precompile, err = xweb.NewPrecompile(keepers{Keepers: app.GetPrecompileKeepers(), xweb: &k})
	require.NoError(t, err)
	h.stateDB = state.NewDBImpl(ctx, &app.EvmKeeper, false)
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	contracts := maps.Clone(app.EvmKeeper.CustomPrecompiles(ctx))
	contracts[precompile] = h.precompile
	h.evm = vm.NewEVM(*blockCtx, h.stateDB, cfg, vm.Config{}, contracts)
	for address, code := range map[common.Address][]byte{recorder: recorderCode, reverter: reverterCode, burner: burnerCode} {
		h.stateDB.SetCode(address, code)
		h.fund(address, 10*fee)
	}
	h.fund(stranger, 10*fee)
	return h
}

func (h *harness) ctx() sdk.Context { return h.stateDB.Ctx() }

func (h *harness) account(address common.Address) sdk.AccAddress {
	return testkeeper.EVMTestApp.EvmKeeper.GetPaxAddressOrDefault(h.ctx(), address)
}

func (h *harness) fund(address common.Address, amount int64) {
	h.t.Helper()
	coins := sdk.NewCoins(sdk.NewCoin(sdk.MustGetBaseDenom(), sdk.NewInt(amount)))
	require.NoError(h.t, testkeeper.EVMTestApp.BankKeeper.MintCoins(h.ctx(), "evm", coins))
	require.NoError(h.t, testkeeper.EVMTestApp.BankKeeper.SendCoinsFromModuleToAccount(h.ctx(), "evm", h.account(address), coins))
}

func (h *harness) balance(of sdk.AccAddress) sdk.Int {
	return testkeeper.EVMTestApp.BankKeeper.GetBalance(h.ctx(), of, sdk.MustGetBaseDenom()).Amount
}

func (h *harness) method(name string) abi.Method {
	h.t.Helper()
	m, ok := h.precompile.GetABI().Methods[name]
	require.True(h.t, ok, name)
	return m
}

func (h *harness) input(name string, args ...interface{}) []byte {
	h.t.Helper()
	m := h.method(name)
	packed, err := m.Inputs.Pack(args...)
	require.NoError(h.t, err)
	return append(append([]byte(nil), m.ID...), packed...)
}

// cost is the documented charge taken before a call runs.
func cost(input []byte, base uint64, signatures int) uint64 {
	return base + xweb.GasPerByte*uint64(len(input)-4) + xweb.GasPerSignature*uint64(signatures)
}

// call runs input against the precompile through the EVM and returns the
// decoded outputs, the gas left and the revert reason.
func (h *harness) call(from common.Address, name string, value *big.Int, gas uint64, args ...interface{}) ([]interface{}, uint64, string) {
	h.t.Helper()
	amount := uint256.NewInt(0)
	if value != nil {
		amount = uint256.MustFromBig(value)
	}
	ret, left, err := h.evm.Call(from, precompile, h.input(name, args...), gas, amount)
	if err != nil {
		require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
		reason, unpackErr := abi.UnpackRevert(ret)
		require.NoError(h.t, unpackErr)
		require.NotEmpty(h.t, reason)
		return nil, left, reason
	}
	out, err := h.method(name).Outputs.Unpack(ret)
	require.NoError(h.t, err)
	return out, left, ""
}

func (h *harness) view(name string, args ...interface{}) []interface{} {
	h.t.Helper()
	input := h.input(name, args...)
	ret, left, err := h.evm.StaticCall(stranger, precompile, input, supplied)
	require.NoError(h.t, err, name)
	require.Equal(h.t, supplied-cost(input, xweb.ViewBaseGas, 0), left, name)
	out, err := h.method(name).Outputs.Unpack(ret)
	require.NoError(h.t, err)
	return out
}

func (h *harness) viewReverts(name string, args ...interface{}) string {
	h.t.Helper()
	ret, _, err := h.evm.StaticCall(stranger, precompile, h.input(name, args...), supplied)
	require.ErrorIs(h.t, err, vm.ErrExecutionReverted)
	reason, err := abi.UnpackRevert(ret)
	require.NoError(h.t, err)
	return reason
}

func weiOf(amount int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(amount), state.UhpxToSweiMultiplier)
}

func (h *harness) request(from common.Address, gas uint64) uint64 {
	h.t.Helper()
	input := h.input(xweb.RequestMethod, types.KindFetch, payload, gas)
	out, left, reason := h.call(from, xweb.RequestMethod, weiOf(fee), supplied, types.KindFetch, payload, gas)
	require.Empty(h.t, reason)
	require.Equal(h.t, supplied-cost(input, xweb.WriteBaseGas, 0), left)
	return out[0].(uint64)
}

func (h *harness) signatures(id uint64, resp []byte, contentDigest [32]byte, length uint32) [][]byte {
	h.t.Helper()
	request, found := h.keeper.GetRequest(h.ctx(), id)
	require.True(h.t, found)
	attestation := h.keeper.Attestation(h.ctx(), request, resp, types.Hash32(contentDigest), length)
	return xwebtestutil.Sign(types.Digest(attestation), h.attestors[0], h.attestors[1])
}

// fulfil submits the two-of-three attested answer to id with gas and returns
// the callback outcome, the gas it used and the gas left.
func (h *harness) fulfil(id uint64, gas uint64) (types.CallbackOutcome, uint64, uint64) {
	h.t.Helper()
	signatures := h.signatures(id, response, digest, fullLength)
	input := h.input(xweb.FulfilMethod, id, response, digest, fullLength, signatures)
	out, left, reason := h.call(stranger, xweb.FulfilMethod, nil, gas, id, response, digest, fullLength, signatures)
	require.Empty(h.t, reason)
	outcome, used := types.CallbackOutcome(out[0].(uint8)), out[1].(uint64)
	require.Equal(h.t, gas-cost(input, xweb.WriteBaseGas, 2)-used-xweb.CallbackRecordGas, left)
	return outcome, used, left
}

func (h *harness) logs(event string) []*ethtypes.Log {
	h.t.Helper()
	topic := h.precompile.GetABI().Events[event].ID
	var out []*ethtypes.Log
	for _, log := range h.stateDB.GetAllLogs() {
		if log.Topics[0] == topic {
			require.Equal(h.t, precompile, log.Address)
			out = append(out, log)
		}
	}
	return out
}

func (h *harness) result(id uint64) xweb.ResultView {
	h.t.Helper()
	return *abi.ConvertType(h.view(xweb.GetResultMethod, id)[0], new(xweb.ResultView)).(*xweb.ResultView)
}

func (h *harness) requestView(id uint64) xweb.RequestView {
	h.t.Helper()
	return *abi.ConvertType(h.view(xweb.GetRequestMethod, id)[0], new(xweb.RequestView)).(*xweb.RequestView)
}

func topicOf(value uint64) common.Hash { return common.BigToHash(new(big.Int).SetUint64(value)) }

func TestRequestTakesTheFeeAndEmits(t *testing.T) {
	h := newHarness(t)
	before := h.balance(h.account(recorder))
	moduleBefore := h.balance(h.keeper.ModuleAddress())
	id := h.request(recorder, callbackGas)
	require.Equal(t, uint64(1), id)
	requireAmount(t, before.SubRaw(fee), h.balance(h.account(recorder)))
	requireAmount(t, moduleBefore.AddRaw(fee), h.balance(h.keeper.ModuleAddress()))
	require.True(t, h.balance(h.account(precompile)).IsZero())

	view := h.requestView(id)
	require.Equal(t, id, view.Id)
	require.Equal(t, recorder, view.Requester)
	require.Equal(t, types.KindFetch, view.Kind)
	require.Equal(t, [32]byte(types.Keccak(payload)), view.PayloadHash)
	require.Equal(t, callbackGas, view.CallbackGas)
	require.Zero(t, weiOf(fee).Cmp(view.Fee))
	require.Equal(t, uint64(startHeight), view.Height)
	require.Equal(t, uint64(startHeight)+timeout, view.TimeoutHeight)
	require.Equal(t, uint8(types.StatusPending), view.Status)
	require.Equal(t, types.LevelMajority, view.Level)
	require.Equal(t, common.Address{}, view.Attestor)

	logs := h.logs("XWebRequested")
	require.Len(t, logs, 1)
	require.Equal(t, []common.Hash{logs[0].Topics[0], topicOf(id), common.BytesToHash(recorder.Bytes())}, logs[0].Topics)
	data, err := h.precompile.GetABI().Events["XWebRequested"].Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, types.KindFetch, data[0].(uint8))
	require.Equal(t, payload, data[1].([]byte))
	require.Equal(t, callbackGas, data[2].(uint64))
	require.Zero(t, weiOf(fee).Cmp(data[3].(*big.Int)))
	require.Equal(t, uint64(startHeight)+timeout, data[4].(uint64))

	require.Equal(t, uint64(2), h.request(recorder, callbackGas))
}

func TestRequestRefusesWrongPayment(t *testing.T) {
	h := newHarness(t)
	before := h.balance(h.account(recorder))
	moduleBefore := h.balance(h.keeper.ModuleAddress())
	_, left, reason := h.call(recorder, xweb.RequestMethod, weiOf(fee-1), supplied, types.KindFetch, payload, callbackGas)
	require.Equal(t, "LXP_XWEB_WRONG_FEE: paid 1000002, fee 1000003: paid amount is not the current fee", reason)
	input := h.input(xweb.RequestMethod, types.KindFetch, payload, callbackGas)
	require.Equal(t, supplied-cost(input, xweb.WriteBaseGas, 0), left)

	withRemainder := new(big.Int).Add(weiOf(fee), big.NewInt(1))
	_, _, reason = h.call(recorder, xweb.RequestMethod, withRemainder, supplied, types.KindFetch, payload, callbackGas)
	require.Equal(t, "LXP_PRECOMPILE_WEI_REMAINDER: selected precompile function does not allow payment with non-zero wei remainder: received "+withRemainder.String(), reason)

	_, _, reason = h.call(recorder, xweb.RequestMethod, nil, supplied, types.KindFetch, payload, callbackGas)
	require.Equal(t, "LXP_XWEB_WRONG_FEE: paid 0, fee 1000003: paid amount is not the current fee", reason)

	_, _, reason = h.call(recorder, xweb.RequestMethod, weiOf(fee), supplied, types.KindFetch, payload, callbackCap+1)
	require.Equal(t, "LXP_XWEB_CALLBACK_GAS: 300001, cap 300000: callback gas zero or over the callback cap", reason)

	_, _, reason = h.call(recorder, xweb.RefundMethod, big.NewInt(1), supplied, uint64(1))
	require.Equal(t, "LXP_PRECOMPILE_NON_PAYABLE: sending funds to a non-payable function", reason)

	requireAmount(t, before, h.balance(h.account(recorder)))
	requireAmount(t, moduleBefore, h.balance(h.keeper.ModuleAddress()))
	require.True(t, h.balance(h.account(precompile)).IsZero())
	require.Equal(t, uint64(0), h.keeper.Nonce(h.ctx()))
	require.Empty(t, h.logs("XWebRequested"))

	input = h.input(xweb.RequestMethod, types.KindFetch, payload, callbackGas)
	ret, left, err := h.evm.StaticCall(recorder, precompile, input, supplied)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, supplied-cost(input, xweb.WriteBaseGas, 0), left)
	reason, err = abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_XWEB_STATICCALL: cannot call xweb request from staticcall", reason)

	_, left, err = h.evm.Call(recorder, precompile, input, cost(input, xweb.WriteBaseGas, 0)-1, uint256.NewInt(0))
	require.ErrorIs(t, err, vm.ErrOutOfGas)
	require.Zero(t, left)
}

func TestLXPNames(t *testing.T) {
	errors := []struct {
		err  *sdkerrors.Error
		code uint32
		name string
		text string
	}{
		{types.ErrUnauthorized, 2, "LXP_XWEB_UNAUTHORIZED", "unauthorized"},
		{types.ErrInvalidGenesis, 3, "LXP_XWEB_INVALID_GENESIS", "invalid genesis"},
		{types.ErrInvalidParams, 4, "LXP_XWEB_INVALID_PARAMS", "invalid params"},
		{types.ErrInvalidAttestors, 5, "LXP_XWEB_INVALID_ATTESTORS", "invalid attestor set"},
		{types.ErrInvalidThreshold, 6, "LXP_XWEB_INVALID_THRESHOLD", "threshold is not a majority of the attestor set"},
		{types.ErrPaused, 7, "LXP_XWEB_PAUSED", "xweb paused"},
		{types.ErrUnknownKind, 8, "LXP_XWEB_UNKNOWN_KIND", "unknown request kind"},
		{types.ErrPayloadSize, 9, "LXP_XWEB_PAYLOAD_SIZE", "payload empty or over the payload cap"},
		{types.ErrCallbackGas, 10, "LXP_XWEB_CALLBACK_GAS", "callback gas zero or over the callback cap"},
		{types.ErrWrongFee, 11, "LXP_XWEB_WRONG_FEE", "paid amount is not the current fee"},
		{types.ErrUnknownRequest, 12, "LXP_XWEB_UNKNOWN_REQUEST", "request not found"},
		{types.ErrAlreadyFulfilled, 13, "LXP_XWEB_ALREADY_FULFILLED", "request already fulfilled"},
		{types.ErrRefunded, 14, "LXP_XWEB_REFUNDED", "request already refunded"},
		{types.ErrResponseTooLarge, 15, "LXP_XWEB_RESPONSE_TOO_LARGE", "response over the stored response bound"},
		{types.ErrInvalidLength, 16, "LXP_XWEB_INVALID_LENGTH", "full length shorter than the response"},
		{types.ErrBadSignature, 17, "LXP_XWEB_BAD_SIGNATURE", "invalid attestor signature"},
		{types.ErrBelowThreshold, 18, "LXP_XWEB_BELOW_THRESHOLD", "attestor signatures below threshold"},
		{types.ErrNotExpired, 19, "LXP_XWEB_NOT_EXPIRED", "request not yet past its timeout"},
		{types.ErrUnknownAttestor, 20, "LXP_XWEB_UNKNOWN_ATTESTOR", "attestor not registered"},
		{types.ErrCallbackRecorded, 21, "LXP_XWEB_CALLBACK_RECORDED", "callback outcome already recorded"},
		{types.ErrInvalidRequest, 22, "LXP_XWEB_INVALID_REQUEST", "invalid request"},
		{types.ErrInvalidApi, 23, "LXP_XWEB_INVALID_API", "invalid api payload"},
		{types.ErrInvalidEnvelope, 24, "LXP_XWEB_INVALID_ENVELOPE", "invalid credential envelope"},
		{types.ErrInvalidLevel, 25, "LXP_XWEB_INVALID_LEVEL", "invalid attestation level"},
	}
	for _, tc := range errors {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.code, tc.err.ABCICode())
			require.Equal(t, types.ModuleName, tc.err.Codespace())
			require.Equal(t, tc.text, tc.err.Error())
			for _, err := range []error{tc.err, tc.err.Wrap("context"), fmt.Errorf("outer: %w", tc.err.Wrap("context"))} {
				name, mapped := types.LXPName(err)
				require.True(t, mapped)
				require.Equal(t, tc.name, name)
				require.ErrorIs(t, err, tc.err)
			}
		})
	}
	t.Run("unmapped", func(t *testing.T) {
		var nilError *sdkerrors.Error
		for _, err := range []error{nil, nilError, sdkerrors.ErrUnauthorized, sdkerrors.New(types.ModuleName, 1, "unused")} {
			name, mapped := types.LXPName(err)
			require.False(t, mapped)
			require.Empty(t, name)
		}
	})
	t.Run("paused_request", func(t *testing.T) {
		h := newHarness(t)
		before := h.balance(h.account(recorder))
		moduleBefore := h.balance(h.keeper.ModuleAddress())
		require.NoError(t, h.keeper.Pause(h.ctx(), types.MsgPause{Authority: authority}))
		_, left, reason := h.call(recorder, xweb.RequestMethod, weiOf(fee), supplied, types.KindFetch, payload, callbackGas)
		require.Equal(t, "LXP_XWEB_PAUSED: xweb paused", reason)
		require.Equal(t, supplied-cost(h.input(xweb.RequestMethod, types.KindFetch, payload, callbackGas), xweb.WriteBaseGas, 0), left)
		requireAmount(t, before, h.balance(h.account(recorder)))
		requireAmount(t, moduleBefore, h.balance(h.keeper.ModuleAddress()))
		require.Equal(t, uint64(0), h.keeper.Nonce(h.ctx()))
		require.Empty(t, h.logs("XWebRequested"))
	})
	t.Run("wrapped_errors_and_fulfil_pipeline", func(t *testing.T) {
		h := newHarness(t)
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: request 7: request not found", h.viewReverts(xweb.GetRequestMethod, uint64(7)))
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: no result for request 7: request not found", h.viewReverts(xweb.GetResultMethod, uint64(7)))
		input := h.input(xweb.FulfilMethod, uint64(7), response, digest, fullLength, [][]byte{})
		_, left, reason := h.call(stranger, xweb.FulfilMethod, nil, supplied, uint64(7), response, digest, fullLength, [][]byte{})
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: request 7: request not found", reason)
		require.Equal(t, supplied-cost(input, xweb.WriteBaseGas, 0), left)
		require.Empty(t, h.logs("XWebFulfilled"))
		method := h.method(xweb.FulfilMethod)
		ret, err := h.precompile.GetExecutor().Execute(h.ctx(), &method, stranger, stranger, nil, nil, false, h.evm, nil)
		require.Nil(t, ret)
		require.EqualError(t, err, "LXP_XWEB_FULFIL_EVM_ONLY: xweb: fulfil runs only through the EVM, which meters its callback")
		method = h.method(xweb.GetAttestorsMethod)
		ret, err = h.precompile.GetExecutor().Execute(h.ctx(), &method, stranger, stranger, []interface{}{uint64(1)}, nil, false, h.evm, nil)
		require.Nil(t, ret)
		require.EqualError(t, err, "LXP_PRECOMPILE_ARG_COUNT: expected 0 arguments but got 1")
	})
	t.Run("callback_budget", func(t *testing.T) {
		h := newHarness(t)
		id := h.request(recorder, callbackGas)
		signatures := h.signatures(id, response, digest, fullLength)
		input := h.input(xweb.FulfilMethod, id, response, digest, fullLength, signatures)
		gas := cost(input, xweb.WriteBaseGas, 2) + callbackGas + xweb.CallbackRecordGas - 1
		_, left, reason := h.call(stranger, xweb.FulfilMethod, nil, gas, id, response, digest, fullLength, signatures)
		require.Equal(t, "LXP_XWEB_CALLBACK_BUDGET: xweb: fulfil of request 1 needs 210000 gas for its callback and its record, 209999 left", reason)
		require.Equal(t, uint64(209999), left)
		require.Equal(t, uint8(types.StatusPending), h.requestView(id).Status)
		_, found := h.keeper.GetResult(h.ctx(), id)
		require.False(t, found)
		require.Empty(t, h.logs("XWebFulfilled"))
	})
	t.Run("wrong_payment", TestRequestRefusesWrongPayment)
}

func TestFulfilDeliversTheCallback(t *testing.T) {
	h := newHarness(t)
	id := h.request(recorder, callbackGas)
	payouts := []sdk.Int{h.balance(h.attestors[0].Payout), h.balance(h.attestors[1].Payout), h.balance(h.attestors[2].Payout)}
	moduleBefore := h.balance(h.keeper.ModuleAddress())

	outcome, used, _ := h.fulfil(id, supplied)
	require.Equal(t, types.CallbackDelivered, outcome)
	require.Positive(t, used)
	require.LessOrEqual(t, used, callbackGas)

	require.Equal(t, topicOf(id), h.stateDB.GetState(recorder, common.Hash{}))
	require.Equal(t, common.BytesToHash(precompile.Bytes()), h.stateDB.GetState(recorder, common.BigToHash(big.NewInt(1))))
	require.Equal(t, common.Hash(digest), h.stateDB.GetState(recorder, common.BigToHash(big.NewInt(2))))

	requireAmount(t, payouts[0].AddRaw(500_002), h.balance(h.attestors[0].Payout))
	requireAmount(t, payouts[1].AddRaw(500_001), h.balance(h.attestors[1].Payout))
	requireAmount(t, payouts[2], h.balance(h.attestors[2].Payout))
	requireAmount(t, moduleBefore.SubRaw(fee), h.balance(h.keeper.ModuleAddress()))

	result := h.result(id)
	require.Equal(t, id, result.RequestId)
	require.Equal(t, response, result.Response)
	require.Equal(t, digest, result.ContentDigest)
	require.Equal(t, fullLength, result.FullLength)
	require.Equal(t, []common.Address{common.Address(h.attestors[0].Signer), common.Address(h.attestors[1].Signer)}, result.Signers)
	require.Equal(t, uint64(startHeight), result.Height)
	require.Equal(t, uint8(types.CallbackDelivered), result.Callback)
	require.Equal(t, used, result.CallbackGasUsed)
	require.Equal(t, types.LevelMajority, result.Level)
	require.Equal(t, uint8(types.StatusFulfilled), h.requestView(id).Status)

	logs := h.logs("XWebFulfilled")
	require.Len(t, logs, 1)
	require.Equal(t, []common.Hash{logs[0].Topics[0], topicOf(id), common.BytesToHash(recorder.Bytes())}, logs[0].Topics)
	data, err := h.precompile.GetABI().Events["XWebFulfilled"].Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, digest, data[0].([32]byte))
	require.Equal(t, fullLength, data[1].(uint32))
	require.Equal(t, types.LevelMajority, data[2].(uint8))
	require.Equal(t, uint8(types.CallbackDelivered), data[3].(uint8))
	require.Equal(t, used, data[4].(uint64))

	signatures := h.signatures(id, response, digest, fullLength)
	_, _, reason := h.call(stranger, xweb.FulfilMethod, nil, supplied, id, response, digest, fullLength, signatures)
	require.Contains(t, reason, "fulfilled")
	_, _, reason = h.call(stranger, xweb.RefundMethod, nil, supplied, id)
	require.Contains(t, reason, "fulfilled")
}

func TestFulfilRecordsARevertedCallback(t *testing.T) {
	h := newHarness(t)
	id := h.request(reverter, callbackGas)
	outcome, used, _ := h.fulfil(id, supplied)
	require.Equal(t, types.CallbackReverted, outcome)
	// PUSH1, PUSH1 and a zero-length REVERT.
	require.Equal(t, uint64(6), used)
	result := h.result(id)
	require.Equal(t, uint8(types.CallbackReverted), result.Callback)
	require.Equal(t, uint64(6), result.CallbackGasUsed)
	require.Equal(t, uint8(types.StatusFulfilled), h.requestView(id).Status)
}

func TestFulfilBoundsTheCallbackByTheModuleMaximum(t *testing.T) {
	h := newHarness(t)
	id := h.request(burner, callbackGas)
	lowered := uint64(50_000)
	require.NoError(t, h.keeper.UpdateParams(h.ctx(), types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: payloadCap, MaxCallbackGas: lowered, TimeoutBlocks: timeout}))
	outcome, used, _ := h.fulfil(id, supplied)
	require.Equal(t, types.CallbackOutOfGas, outcome)
	require.Equal(t, lowered, used)
	result := h.result(id)
	require.Equal(t, uint8(types.CallbackOutOfGas), result.Callback)
	require.Equal(t, lowered, result.CallbackGasUsed)
}

func TestFulfilRefusesWithoutGasForTheCallback(t *testing.T) {
	h := newHarness(t)
	id := h.request(burner, callbackGas)
	signatures := h.signatures(id, response, digest, fullLength)
	input := h.input(xweb.FulfilMethod, id, response, digest, fullLength, signatures)
	short := cost(input, xweb.WriteBaseGas, 2) + callbackGas + xweb.CallbackRecordGas - 1
	_, left, reason := h.call(stranger, xweb.FulfilMethod, nil, short, id, response, digest, fullLength, signatures)
	require.Contains(t, reason, "needs")
	require.Equal(t, callbackGas+xweb.CallbackRecordGas-1, left)
	require.Equal(t, uint8(types.StatusPending), h.requestView(id).Status)
	require.Empty(t, h.logs("XWebFulfilled"))

	outcome, used, left := h.fulfil(id, short+1)
	require.Equal(t, types.CallbackOutOfGas, outcome)
	require.Equal(t, callbackGas, used)
	require.Zero(t, left)
}

func TestFulfilRefusesBadAttestations(t *testing.T) {
	h := newHarness(t)
	id := h.request(recorder, callbackGas)
	signatures := h.signatures(id, response, digest, fullLength)
	_, _, reason := h.call(stranger, xweb.FulfilMethod, nil, supplied, id, []byte("other"), digest, fullLength, signatures)
	require.Contains(t, reason, "attestor")
	_, _, reason = h.call(stranger, xweb.FulfilMethod, nil, supplied, id, response, digest, fullLength, signatures[:1])
	require.Contains(t, reason, "threshold")
	_, _, reason = h.call(stranger, xweb.FulfilMethod, weiOf(1), supplied, id, response, digest, fullLength, signatures)
	require.Contains(t, reason, "payable")
	_, _, reason = h.call(stranger, xweb.FulfilMethod, nil, supplied, id+1, response, digest, fullLength, signatures)
	require.Contains(t, reason, "not found")
	input := h.input(xweb.FulfilMethod, id, response, digest, fullLength, signatures)
	_, _, err := h.evm.StaticCall(stranger, precompile, input, supplied)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, uint8(types.StatusPending), h.requestView(id).Status)
	require.Equal(t, common.Hash{}, h.stateDB.GetState(recorder, common.Hash{}))
}

func TestRefundAfterTheTimeout(t *testing.T) {
	h := newHarness(t)
	id := h.request(recorder, callbackGas)
	before := h.balance(h.account(recorder))
	_, _, reason := h.call(stranger, xweb.RefundMethod, nil, supplied, id)
	require.Contains(t, reason, "refundable at height")

	h.stateDB.WithCtx(h.ctx().WithBlockHeight(startHeight + int64(timeout)))
	input := h.input(xweb.RefundMethod, id)
	_, left, reason := h.call(stranger, xweb.RefundMethod, nil, supplied, id)
	require.Empty(t, reason)
	require.Equal(t, supplied-cost(input, xweb.WriteBaseGas, 0), left)
	requireAmount(t, before.AddRaw(fee), h.balance(h.account(recorder)))
	require.Equal(t, uint8(types.StatusRefunded), h.requestView(id).Status)

	logs := h.logs("XWebRefunded")
	require.Len(t, logs, 1)
	require.Equal(t, []common.Hash{logs[0].Topics[0], topicOf(id), common.BytesToHash(recorder.Bytes())}, logs[0].Topics)
	data, err := h.precompile.GetABI().Events["XWebRefunded"].Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Zero(t, weiOf(fee).Cmp(data[0].(*big.Int)))

	_, _, reason = h.call(stranger, xweb.RefundMethod, nil, supplied, id)
	require.Contains(t, reason, "refunded")
	signatures := h.signatures(id, response, digest, fullLength)
	_, _, reason = h.call(stranger, xweb.FulfilMethod, nil, supplied, id, response, digest, fullLength, signatures)
	require.Contains(t, reason, "refunded")
	requireAmount(t, before.AddRaw(fee), h.balance(h.account(recorder)))
}

func TestViews(t *testing.T) {
	t.Run("acceptance_5_4_1", func(t *testing.T) {
		h := newHarness(t)
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: request 99: request not found",
			h.viewReverts(xweb.GetRequestMethod, uint64(99)))
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: no result for request 99: request not found",
			h.viewReverts(xweb.GetResultMethod, uint64(99)))
		for id := uint64(1); id <= 7; id++ {
			require.Equal(t, id, h.request(recorder, callbackGas))
		}
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: no result for request 7: request not found",
			h.viewReverts(xweb.GetResultMethod, uint64(7)))

		resp := bytes.Repeat([]byte{0x5a}, 1024)
		contentDigest := [32]byte(types.Keccak(resp))
		length := uint32(1024)
		signatures := h.signatures(7, resp, contentDigest, length)
		out, left, reason := h.call(stranger, xweb.FulfilMethod, nil, supplied,
			uint64(7), resp, contentDigest, length, signatures)
		require.Empty(t, reason)
		require.Equal(t, uint8(types.CallbackDelivered), out[0].(uint8))
		used := out[1].(uint64)
		require.Positive(t, used)
		require.Equal(t, supplied-cost(h.input(xweb.FulfilMethod, uint64(7), resp, contentDigest, length, signatures),
			xweb.WriteBaseGas, 2)-used-xweb.CallbackRecordGas, left)

		request := xweb.RequestView{Id: 7, Requester: recorder, Kind: types.KindFetch,
			PayloadHash: [32]byte(types.Keccak(payload)), CallbackGas: callbackGas, Fee: weiOf(fee),
			Height: uint64(startHeight), TimeoutHeight: uint64(startHeight) + timeout,
			Status: uint8(types.StatusFulfilled), Level: types.LevelMajority, Attestor: common.Address{}}
		result := xweb.ResultView{RequestId: 7, Response: resp, ContentDigest: contentDigest, FullLength: length,
			Signers: []common.Address{common.Address(h.attestors[0].Signer), common.Address(h.attestors[1].Signer)},
			Height:  uint64(startHeight), Callback: uint8(types.CallbackDelivered), CallbackGasUsed: used,
			Level: types.LevelMajority}
		require.Equal(t, request, h.requestView(7))
		require.Equal(t, result, h.result(7))
		storedRequest, found := h.keeper.GetRequest(h.ctx(), 7)
		require.True(t, found)
		require.Equal(t, types.StatusFulfilled, storedRequest.Status)
		require.Equal(t, types.Hash32(request.PayloadHash), storedRequest.PayloadHash)
		storedResult, found := h.keeper.GetResult(h.ctx(), 7)
		require.True(t, found)
		require.Equal(t, resp, storedResult.Response)
		require.Equal(t, types.Hash32(contentDigest), storedResult.ContentDigest)
		require.Equal(t, []types.Address20{h.attestors[0].Signer, h.attestors[1].Signer}, storedResult.Signers)

		require.NoError(t, h.keeper.Pause(h.ctx(), types.MsgPause{Authority: authority}))
		require.Equal(t, request, h.requestView(7))
		require.Equal(t, result, h.result(7))
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: request 99: request not found",
			h.viewReverts(xweb.GetRequestMethod, uint64(99)))
		require.Equal(t, "LXP_XWEB_UNKNOWN_REQUEST: no result for request 99: request not found",
			h.viewReverts(xweb.GetResultMethod, uint64(99)))
	})
	t.Run("acceptance_5_4_2", func(t *testing.T) {
		h := newHarness(t)
		for _, attestor := range h.attestors {
			require.NoError(t, h.keeper.RemoveAttestor(h.ctx(), types.MsgRemoveAttestor{
				Authority: authority, Signer: attestor.Signer}))
		}
		h.attestors = xwebtestutil.Attestors(4)
		want := make([]xweb.AttestorView, len(h.attestors))
		for i, attestor := range h.attestors {
			registration := attestor.Registration()
			registration.PublicKey = []byte{}
			if i%2 == 0 {
				registration.PublicKey = crypto.CompressPubkey(&attestor.Key.PublicKey)
			}
			require.NoError(t, h.keeper.RegisterAttestor(h.ctx(), types.MsgRegisterAttestor{
				Authority: authority, Attestor: registration}))
			want[i] = xweb.AttestorView{Signer: common.Address(attestor.Signer), Payout: registration.Payout,
				PublicKey: registration.PublicKey}
		}
		require.NoError(t, h.keeper.SetThreshold(h.ctx(), types.MsgSetThreshold{Authority: authority, Threshold: 3}))
		require.NoError(t, h.keeper.UpdateParams(h.ctx(), types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(75),
			MaxPayloadBytes: 8192, MaxCallbackGas: 500000, TimeoutBlocks: 3600}))
		require.Equal(t, uint32(3), h.keeper.GetAttestorSet(h.ctx()).Threshold)
		require.Equal(t, uint32(3), h.keeper.Threshold(h.ctx()))
		requireAmount(t, sdk.NewInt(75), h.keeper.Fee(h.ctx()))

		out := h.view(xweb.GetAttestorsMethod)
		var attestors []xweb.AttestorView
		attestors = *abi.ConvertType(out[0], &attestors).(*[]xweb.AttestorView)
		require.Equal(t, want, attestors)
		require.Equal(t, uint32(3), out[1].(uint32))
		require.Equal(t, uint32(3), h.view(xweb.ThresholdMethod)[0].(uint32))
		wantFee := big.NewInt(75_000_000_000_000)
		require.Equal(t, wantFee, h.view(xweb.FeeMethod)[0].(*big.Int))
		wantParams := xweb.ParamsView{Fee: wantFee, MaxPayloadBytes: 8192, MaxCallbackGas: 500000,
			TimeoutBlocks: 3600, Paused: false}
		params := *abi.ConvertType(h.view(xweb.GetParamsMethod)[0], new(xweb.ParamsView)).(*xweb.ParamsView)
		require.Equal(t, wantParams, params)

		require.NoError(t, h.keeper.Pause(h.ctx(), types.MsgPause{Authority: authority}))
		wantParams.Paused = true
		params = *abi.ConvertType(h.view(xweb.GetParamsMethod)[0], new(xweb.ParamsView)).(*xweb.ParamsView)
		require.Equal(t, wantParams, params)
		out = h.view(xweb.GetAttestorsMethod)
		attestors = *abi.ConvertType(out[0], &attestors).(*[]xweb.AttestorView)
		require.Equal(t, want, attestors)
		require.Equal(t, uint32(3), out[1].(uint32))
		require.Equal(t, uint32(3), h.view(xweb.ThresholdMethod)[0].(uint32))
		require.Equal(t, wantFee, h.view(xweb.FeeMethod)[0].(*big.Int))
		_, _, reason := h.call(recorder, xweb.RequestMethod, wantFee, supplied, types.KindFetch, payload, callbackGas)
		require.Equal(t, "LXP_XWEB_PAUSED: xweb paused", reason)
	})
}

func TestRefusesDelegateCallAndAnUnwiredKeeper(t *testing.T) {
	h := newHarness(t)
	input := h.input(xweb.FeeMethod)
	ret, left, err := h.evm.DelegateCall(stranger, stranger, precompile, input, supplied, uint256.NewInt(0))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, supplied-cost(input, xweb.ViewBaseGas, 0), left)
	reason, err := abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_XWEB_DELEGATECALL: cannot delegatecall xweb", reason)

	appKeepers, wires := testkeeper.EVMTestApp.GetPrecompileKeepers().(utils.XWebKeepers)
	require.True(t, wires)
	require.NotNil(t, appKeepers.XWebK())

	unwired := xweb.NewPrecompileWithKeeper(nil, testkeeper.EVMTestApp.BankKeeper, &testkeeper.EVMTestApp.EvmKeeper)
	ret, _, err = unwired.RunAndCalculateGas(h.evm, stranger, stranger, input, supplied, big.NewInt(0), nil, true, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err = abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_XWEB_KEEPER_MISSING: xweb: the xweb keeper is not wired into this application", reason)

	signatures := h.signatures(h.request(recorder, callbackGas), response, digest, fullLength)
	fulfil := h.input(xweb.FulfilMethod, uint64(1), response, digest, fullLength, signatures)
	ret, _, err = unwired.RunAndCalculateGas(h.evm, stranger, stranger, fulfil, supplied, big.NewInt(0), nil, false, false)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	reason, err = abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_XWEB_KEEPER_MISSING: xweb: the xweb keeper is not wired into this application", reason)

	ret, left, err = h.evm.DelegateCall(stranger, stranger, precompile, fulfil, supplied, uint256.NewInt(0))
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, supplied-cost(fulfil, xweb.WriteBaseGas, 2), left)
	reason, err = abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_XWEB_DELEGATECALL: cannot delegatecall xweb", reason)

	ret, left, err = h.evm.StaticCall(stranger, precompile, fulfil, supplied)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	require.Equal(t, supplied-cost(fulfil, xweb.WriteBaseGas, 2), left)
	reason, err = abi.UnpackRevert(ret)
	require.NoError(t, err)
	require.Equal(t, "LXP_XWEB_STATICCALL: cannot call xweb fulfil from staticcall", reason)
}

// TestABIMatchesTheSolidityInterface pins abi.json to the signatures the
// Foundry suite checks against the Solidity interface, and the interface the
// contracts import to the precompile's copy.
func TestABIMatchesTheSolidityInterface(t *testing.T) {
	h := newHarness(t)
	suite, err := os.ReadFile("../../contracts/test/XWebConsumer.t.sol")
	require.NoError(t, err)
	parsed := h.precompile.GetABI()
	require.Len(t, parsed.Methods, 12)
	require.Len(t, parsed.Events, 3)
	local, err := os.ReadFile("XWeb.sol")
	require.NoError(t, err)
	t.Run("acceptance_5_4_3", func(t *testing.T) {
		source := regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`).ReplaceAllString(string(local), "")
		source, _, found := strings.Cut(source, "interface IXWebConsumer")
		require.True(t, found)
		structs := make(map[string][]abi.ArgumentMarshaling)
		for _, declaration := range regexp.MustCompile(`(?s)\bstruct\s+(\w+)\s*\{([^}]*)\}`).FindAllStringSubmatch(source, -1) {
			for _, field := range strings.Split(declaration[2], ";") {
				if strings.TrimSpace(field) == "" {
					continue
				}
				parts := strings.Fields(field)
				require.Len(t, parts, 2)
				structs[declaration[1]] = append(structs[declaration[1]], abi.ArgumentMarshaling{
					Name: parts[1], Type: parts[0], InternalType: parts[0]})
			}
		}
		require.Len(t, structs, 4)
		declarations := regexp.MustCompile(`(?s)\bfunction\s+(\w+)\s*\(([^)]*)\)\s+external(?:\s+(view|pure|payable))?(?:\s+returns\s*\(([^)]*)\))?\s*;`).FindAllStringSubmatch(source, -1)
		require.Len(t, declarations, 12)
		require.Equal(t, strings.Count(source, "function "), len(declarations))
		seen := make(map[string]bool)
		for _, declaration := range declarations {
			name := declaration[1]
			require.False(t, seen[name], name)
			seen[name] = true
			t.Run(name, func(t *testing.T) {
				mutability := declaration[3]
				if mutability == "" {
					mutability = "nonpayable"
				}
				inputs := solidityArguments(t, declaration[2], structs)
				outputs := solidityArguments(t, declaration[4], structs)
				want := abi.NewMethod(name, name, abi.Function, mutability, false, false, inputs, outputs)
				got, exists := parsed.Methods[name]
				require.True(t, exists, name)
				require.Equal(t, want.Sig, got.Sig)
				require.Equal(t, want.ID, got.ID)
				require.Equal(t, want.StateMutability, got.StateMutability)
				require.Equal(t, want.Inputs, got.Inputs)
				require.Equal(t, want.Outputs, got.Outputs)
				resolved, err := parsed.MethodById(want.ID)
				require.NoError(t, err)
				require.Equal(t, name, resolved.Name)
			})
		}
		require.Len(t, seen, len(parsed.Methods))
		for name, selector := range map[string]string{
			xweb.GetRequestMethod: "cbbf67ec", xweb.GetResultMethod: "edff42d6",
			xweb.GetAttestorsMethod: "1a15922d", xweb.ThresholdMethod: "42cde4e8",
			xweb.FeeMethod: "ddca3f43", xweb.GetParamsMethod: "5e615a6b",
			"fulfilProgram": "2d4bf7ad", "getProgramResult": "2b2e13fc", "getKernelNetworkId": "fc0d26f4",
		} {
			t.Run(name+"_selector", func(t *testing.T) {
				require.Equal(t, common.FromHex(selector), parsed.Methods[name].ID)
			})
		}
		params := parsed.Methods[xweb.GetParamsMethod].Outputs
		require.Len(t, params, 1)
		require.Equal(t, "(uint256,uint32,uint64,uint64,bool)", params[0].Type.String())
		require.Equal(t, []string{"fee", "maxPayloadBytes", "maxCallbackGas", "timeoutBlocks", "paused"},
			params[0].Type.TupleRawNames)
	})
	t.Run("consumer_selectors", func(t *testing.T) {
		for _, method := range parsed.Methods {
			t.Run(method.Name, func(t *testing.T) {
				require.True(t, strings.Contains(string(suite), `"`+method.Sig+`"`), method.Sig)
			})
		}
		for _, event := range parsed.Events {
			require.True(t, strings.Contains(string(suite), `"`+event.Sig+`"`), event.Sig)
		}
		require.True(t, strings.Contains(string(suite), `"`+xweb.CallbackSignature+`"`))
		require.Equal(t, crypto.Keccak256([]byte(xweb.CallbackSignature))[:4],
			abi.NewMethod(xweb.CallbackMethod, xweb.CallbackMethod, abi.Function, "nonpayable", false, false,
				abi.Arguments{{Type: mustType(t, "uint64")}, {Type: mustType(t, "bytes32")}, {Type: mustType(t, "uint32")},
					{Type: mustType(t, "bytes")}}, nil).ID)
	})
	t.Run("consumer_interface", func(t *testing.T) {
		imported, err := os.ReadFile("../../contracts/src/precompiles/IXWeb.sol")
		require.NoError(t, err)
		require.True(t, bytes.Equal(local, imported))
	})
}

func solidityArguments(t *testing.T, declaration string, structs map[string][]abi.ArgumentMarshaling) abi.Arguments {
	t.Helper()
	args := make(abi.Arguments, 0)
	if strings.TrimSpace(declaration) == "" {
		return args
	}
	for _, parameter := range strings.Split(declaration, ",") {
		parts := strings.Fields(parameter)
		require.NotEmpty(t, parts)
		typeName := parts[0]
		name := ""
		for _, part := range parts[1:] {
			switch part {
			case "calldata", "memory", "storage":
			default:
				require.Empty(t, name, parameter)
				name = part
			}
		}
		internalType := typeName
		components, tuple := structs[strings.TrimSuffix(typeName, "[]")]
		if tuple {
			internalType = "struct IXWeb." + typeName
			if strings.HasSuffix(typeName, "[]") {
				typeName = "tuple[]"
			} else {
				typeName = "tuple"
			}
		}
		typ, err := abi.NewType(typeName, internalType, components)
		require.NoError(t, err)
		args = append(args, abi.Argument{Name: name, Type: typ})
	}
	return args
}

// TestApiRequestAtTheSingleLevel registers an attestor with its public key,
// requests an api call naming it with a credential sealed to it, and fulfils
// with its one signature.
func TestApiRequestAtTheSingleLevel(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.keeper.UpdateParams(h.ctx(), types.MsgSetParams{Authority: authority, Fee: sdk.NewInt(fee),
		MaxPayloadBytes: types.DefaultMaxPayloadBytes, MaxCallbackGas: callbackCap, TimeoutBlocks: timeout}))
	var named xwebtestutil.Attestor
	for _, candidate := range xwebtestutil.Attestors(4) {
		if !h.keeper.GetAttestorSet(h.ctx()).Has(candidate.Signer) {
			named = candidate
		}
	}
	require.NotNil(t, named.Key)
	registration := named.Registration()
	registration.PublicKey = crypto.CompressPubkey(&named.Key.PublicKey)
	require.NoError(t, h.keeper.RegisterAttestor(h.ctx(), types.MsgRegisterAttestor{Authority: authority,
		Attestor: registration}))

	var attestors []xweb.AttestorView
	attestors = *abi.ConvertType(h.view(xweb.GetAttestorsMethod)[0], &attestors).(*[]xweb.AttestorView)
	require.Len(t, attestors, 4)
	var published []byte
	for _, attestor := range attestors {
		if attestor.Signer == common.Address(named.Signer) {
			published = attestor.PublicKey
		} else {
			require.Empty(t, attestor.PublicKey)
		}
	}
	require.Equal(t, registration.PublicKey, published)
	recipient, err := crypto.DecompressPubkey(published)
	require.NoError(t, err)

	call := types.ApiPayload{Method: types.MethodPost, Level: types.LevelSingle, Attestor: named.Signer,
		URL: "https://paxeer.app/api/v1/quote", Headers: []types.ApiHeader{{Name: "Content-Type", Value: "application/json"}},
		Body: []byte(`{"asset":"SID","amount":"1"}`), Pointers: []string{"/quote/amount"}}
	origin, err := call.Origin()
	require.NoError(t, err)
	credential, err := types.EncodeCredential([]types.ApiHeader{{Name: "X-Api-Key", Value: "paxeer-test-credential"}})
	require.NoError(t, err)
	sealed, err := types.SealEnvelope(recipient, origin, credential)
	require.NoError(t, err)
	envelope, err := types.ParseEnvelope(sealed)
	require.NoError(t, err)
	call.Envelopes = []types.Envelope{envelope}
	body, err := call.Encode()
	require.NoError(t, err)

	out, _, reason := h.call(recorder, xweb.RequestMethod, weiOf(fee), supplied, types.KindApi, body, callbackGas)
	require.Empty(t, reason)
	id := out[0].(uint64)
	view := h.requestView(id)
	require.Equal(t, types.KindApi, view.Kind)
	require.Equal(t, [32]byte(types.Keccak(body)), view.PayloadHash)
	require.Equal(t, types.LevelSingle, view.Level)
	require.Equal(t, common.Address(named.Signer), view.Attestor)
	opened, err := types.OpenEnvelope(sealed, named.Key, origin)
	require.NoError(t, err)
	require.Equal(t, credential, opened)

	unregistered := call
	unregistered.Attestor = types.Address20{0x0b}
	unregistered.Envelopes = nil
	refused, err := unregistered.Encode()
	require.NoError(t, err)
	_, _, reason = h.call(recorder, xweb.RequestMethod, weiOf(fee), supplied, types.KindApi, refused, callbackGas)
	require.Contains(t, reason, "the single level names")

	request, found := h.keeper.GetRequest(h.ctx(), id)
	require.True(t, found)
	attested := types.Digest(h.keeper.Attestation(h.ctx(), request, response, types.Hash32(digest), fullLength))
	majority := xwebtestutil.Sign(attested, h.attestors[0], h.attestors[1])
	_, _, reason = h.call(stranger, xweb.FulfilMethod, nil, supplied, id, response, digest, fullLength, majority)
	require.Contains(t, reason, "exactly one signature")

	signatures := xwebtestutil.Sign(attested, named)
	input := h.input(xweb.FulfilMethod, id, response, digest, fullLength, signatures)
	payout := h.balance(named.Payout)
	out, left, reason := h.call(stranger, xweb.FulfilMethod, nil, supplied, id, response, digest, fullLength, signatures)
	require.Empty(t, reason)
	used := out[1].(uint64)
	require.Equal(t, supplied-cost(input, xweb.WriteBaseGas, 1)-used-xweb.CallbackRecordGas, left)
	requireAmount(t, payout.AddRaw(fee), h.balance(named.Payout))

	result := h.result(id)
	require.Equal(t, types.LevelSingle, result.Level)
	require.Equal(t, []common.Address{common.Address(named.Signer)}, result.Signers)
	logs := h.logs("XWebFulfilled")
	require.Len(t, logs, 1)
	data, err := h.precompile.GetABI().Events["XWebFulfilled"].Inputs.NonIndexed().Unpack(logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, types.LevelSingle, data[2].(uint8))
}

// TestFoundrySuitePinsTheApiVectors checks the Foundry suite carries every api
// vector payload the XWebApi library must reproduce.
func TestFoundrySuitePinsTheApiVectors(t *testing.T) {
	suite, err := os.ReadFile("../../contracts/test/XWebConsumer.t.sol")
	require.NoError(t, err)
	raw, err := os.ReadFile("../../modules/xweb/types/testdata/api-vectors.json")
	require.NoError(t, err)
	var file struct {
		Vectors []struct {
			Name    string `json:"name"`
			Payload string `json:"payload"`
		} `json:"vectors"`
	}
	require.NoError(t, json.Unmarshal(raw, &file))
	require.Len(t, file.Vectors, 4)
	for _, v := range file.Vectors {
		require.True(t, strings.Contains(string(suite), `hex"`+strings.TrimPrefix(v.Payload, "0x")+`"`), v.Name)
	}
}

func mustType(t *testing.T, name string) abi.Type {
	t.Helper()
	typ, err := abi.NewType(name, "", nil)
	require.NoError(t, err)
	return typ
}

func requireAmount(t *testing.T, want, got sdk.Int) {
	t.Helper()
	require.True(t, want.Equal(got), "want %s, got %s", want, got)
}
