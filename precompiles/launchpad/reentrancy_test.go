package launchpad_test

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"math/big"
	"testing"

	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/state"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/launchpad/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/launchpad"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"
)

const nestedGas = uint64(1_000_000)

var (
	// copier stores slot 0 into slot 1 and returns slot 0.
	copierCode = common.FromHex("60005460015560005460005260206000f3")
	// reader returns slot 0.
	readerCode = common.FromHex("60005460005260206000f3")
	// dataReverter reverts with the four bytes 0x12345678.
	dataReverterCode = common.FromHex("631234567860e01b60005260046000fd")
	// emptyReverter reverts with no data.
	emptyReverterCode = common.FromHex("60006000fd")
	// writingReverter stores 1 in slot 0, then reverts with no data.
	writingReverterCode = common.FromHex("600160005560006000fd")
	// writer stores 1 in slot 0, then stops.
	writerCode = common.FromHex("600160005500")
	// accepter returns the onERC721Received selector as a bytes4 word.
	accepterCode = common.FromHex("63150b7a0260e01b60005260206000f3")
	// wrongReturner returns 0xdeadbeef as a bytes4 word.
	wrongReturnerCode = common.FromHex("63deadbeef60e01b60005260206000f3")
	// shortReturner returns the four selector bytes only.
	shortReturnerCode = common.FromHex("63150b7a0260e01b60005260046000f3")
	// burner loops until it runs out of gas.
	nestedBurnerCode = common.FromHex("5b600056")

	copier          = common.HexToAddress("0x00000000000000000000000000000000ca110001")
	reader          = common.HexToAddress("0x00000000000000000000000000000000ca110002")
	dataReverter    = common.HexToAddress("0x00000000000000000000000000000000ca110003")
	emptyReverter   = common.HexToAddress("0x00000000000000000000000000000000ca110004")
	writingReverter = common.HexToAddress("0x00000000000000000000000000000000ca110005")
	writer          = common.HexToAddress("0x00000000000000000000000000000000ca110006")
	accepter        = common.HexToAddress("0x00000000000000000000000000000000ca110007")
	wrongReturner   = common.HexToAddress("0x00000000000000000000000000000000ca110008")
	shortReturner   = common.HexToAddress("0x00000000000000000000000000000000ca110009")
	nestedBurner    = common.HexToAddress("0x00000000000000000000000000000000ca11000a")
	codeless        = common.HexToAddress("0x00000000000000000000000000000000ca11000b")
	nestedCaller    = common.HexToAddress("0x00000000000000000000000000000000ca11000c")

	slot0 = common.Hash{}
	slot1 = common.BigToHash(big.NewInt(1))
	slot2 = common.BigToHash(big.NewInt(2))
)

type nestedRun struct {
	stateDB *state.DBImpl
	evm     *vm.EVM
	gas     uint64
	caller  *launchpad.RunCaller
}

func newNestedRun(t *testing.T) *nestedRun {
	t.Helper()
	app := testkeeper.EVMTestApp
	ctx := app.GetContextForDeliverTx([]byte{}).WithBlockTime(genesisTime)
	ctx = ctx.WithGasMeter(sdk.NewInfiniteGasMeterWithMultiplier(ctx))
	r := &nestedRun{stateDB: state.NewDBImpl(ctx, &app.EvmKeeper, false), gas: nestedGas}
	blockCtx, err := app.EvmKeeper.GetVMBlockContext(ctx, core.GasPool(math.MaxUint64))
	require.NoError(t, err)
	cfg := evmtypes.DefaultChainConfig().EthereumConfig(app.EvmKeeper.ChainID(ctx))
	r.evm = vm.NewEVM(*blockCtx, r.stateDB, cfg, vm.Config{}, maps.Clone(app.EvmKeeper.CustomPrecompiles(ctx)))
	for address, code := range map[common.Address][]byte{copier: copierCode, reader: readerCode,
		dataReverter: dataReverterCode, emptyReverter: emptyReverterCode, writingReverter: writingReverterCode,
		writer: writerCode, accepter: accepterCode, wrongReturner: wrongReturnerCode,
		shortReturner: shortReturnerCode, nestedBurner: nestedBurnerCode} {
		r.stateDB.SetCode(address, code)
	}
	// The run starts on a branch of its own, as a precompile run inside a
	// transaction does.
	r.stateDB.Snapshot()
	r.caller, err = launchpad.NewRunCaller(r.evm, &r.gas)
	require.NoError(t, err)
	return r
}

func (r *nestedRun) ctx() sdk.Context { return r.stateDB.Ctx().WithEventManager(sdk.NewEventManager()) }

func TestNestedCallsSeeCommittedState(t *testing.T) {
	r := newNestedRun(t)
	evmKeeper := &testkeeper.EVMTestApp.EvmKeeper
	ctx := r.ctx()
	events := ctx.EventManager()

	// A keeper change of the run is seen by the nested call and by a view.
	first := common.BigToHash(big.NewInt(0x1017_0001))
	evmKeeper.SetState(ctx, copier, slot0, first)
	ctx, ret, err := r.caller.Call(ctx, nestedCaller, copier, nil)
	require.NoError(t, err)
	require.Equal(t, first.Bytes(), ret)
	require.Equal(t, first, evmKeeper.GetState(ctx, copier, slot1))
	require.Same(t, events, ctx.EventManager())
	require.Equal(t, first, r.stateDB.GetState(copier, slot1))

	evmKeeper.SetState(ctx, reader, slot0, first)
	ret, err = r.caller.StaticCall(ctx, nestedCaller, reader, nil)
	require.NoError(t, err)
	require.Equal(t, first.Bytes(), ret)

	// A change made after a nested call, on the context it returned, is seen
	// by the next nested call, which writes on top of it.
	second := common.BigToHash(big.NewInt(0x1017_0002))
	evmKeeper.SetState(ctx, copier, slot0, second)
	evmKeeper.SetState(ctx, copier, slot2, second)
	ctx, ret, err = r.caller.Call(ctx, nestedCaller, copier, nil)
	require.NoError(t, err)
	require.Equal(t, second.Bytes(), ret)
	require.Equal(t, second, evmKeeper.GetState(ctx, copier, slot1))
	require.Equal(t, second, evmKeeper.GetState(ctx, copier, slot2))

	// A change made after a staticcall on the same context is seen as well.
	third := common.BigToHash(big.NewInt(0x1017_0003))
	evmKeeper.SetState(ctx, reader, slot0, third)
	ret, err = r.caller.StaticCall(ctx, nestedCaller, reader, nil)
	require.NoError(t, err)
	require.Equal(t, third.Bytes(), ret)
	require.Less(t, r.gas, nestedGas)
	require.Positive(t, r.gas)
}

func TestNestedRevertPropagation(t *testing.T) {
	t.Run("revert data is returned unchanged", func(t *testing.T) {
		r := newNestedRun(t)
		ctx := r.ctx()
		_, ret, err := r.caller.Call(ctx, nestedCaller, dataReverter, nil)
		require.Nil(t, ret)
		var revert *launchpad.NestedRevertError
		require.True(t, errors.As(err, &revert))
		require.Equal(t, dataReverter, revert.Target)
		require.Equal(t, common.FromHex("0x12345678"), revert.Data)
		require.ErrorIs(t, err, vm.ErrExecutionReverted)

		// A keeper wrapping keeps the nested revert data reachable.
		wrapped := fmt.Errorf("%w: optical hook: %w", types.ErrInvalidHookResponse, err)
		require.True(t, errors.As(wrapped, &revert))
		data, packErr := revert.Revert()
		require.NoError(t, packErr)
		require.Equal(t, common.FromHex("0x12345678"), data)

		_, err = r.caller.StaticCall(ctx, nestedCaller, emptyReverter, nil)
		require.True(t, errors.As(err, &revert))
		data, packErr = revert.Revert()
		require.NoError(t, packErr)
		require.Equal(t, []byte{}, data)
	})

	t.Run("a nested call out of gas reverts with empty data", func(t *testing.T) {
		r := newNestedRun(t)
		_, _, err := r.caller.Call(r.ctx(), nestedCaller, nestedBurner, nil)
		var revert *launchpad.NestedRevertError
		require.True(t, errors.As(err, &revert))
		require.Equal(t, []byte{}, revert.Data)
		require.ErrorIs(t, err, vm.ErrOutOfGas)
		require.Zero(t, r.gas)
	})

	t.Run("a failed permit is ignored and leaves no state", func(t *testing.T) {
		r := newNestedRun(t)
		ctx := r.ctx()
		var zero [32]byte
		outer := common.BigToHash(big.NewInt(42))
		testkeeper.EVMTestApp.EvmKeeper.SetState(ctx, writingReverter, slot0, outer)
		ctx, err := r.caller.Permit(ctx, nestedCaller, writingReverter, nestedCaller, accepter, big.NewInt(5),
			big.NewInt(6), 27, zero, zero)
		require.NoError(t, err)
		require.Equal(t, outer, testkeeper.EVMTestApp.EvmKeeper.GetState(ctx, writingReverter, slot0))
		require.Equal(t, outer, r.stateDB.GetState(writingReverter, slot0))

		testkeeper.EVMTestApp.EvmKeeper.SetState(ctx, copier, slot0, outer)
		ctx, ret, err := r.caller.Call(ctx, nestedCaller, copier, nil)
		require.NoError(t, err)
		require.Equal(t, outer.Bytes(), ret)
		require.Equal(t, outer, r.stateDB.GetState(copier, slot1))

		ctx, err = r.caller.Permit(ctx, nestedCaller, writer, nestedCaller, accepter, big.NewInt(5), big.NewInt(6), 27,
			zero, zero)
		require.NoError(t, err)
		require.Equal(t, common.BigToHash(big.NewInt(1)), testkeeper.EVMTestApp.EvmKeeper.GetState(ctx, writer, slot0))
	})

	t.Run("an outer revert rolls back committed and nested writes", func(t *testing.T) {
		r := newNestedRun(t)
		revision := r.stateDB.Snapshot()
		ctx := r.ctx()
		outer := common.BigToHash(big.NewInt(42))
		testkeeper.EVMTestApp.EvmKeeper.SetState(ctx, copier, slot0, outer)
		ctx, ret, err := r.caller.Call(ctx, nestedCaller, copier, nil)
		require.NoError(t, err)
		require.Equal(t, outer.Bytes(), ret)
		require.Equal(t, outer, r.stateDB.GetState(copier, slot1))

		_, _, err = r.caller.Call(ctx, nestedCaller, dataReverter, nil)
		var revert *launchpad.NestedRevertError
		require.ErrorAs(t, err, &revert)
		require.Equal(t, common.FromHex("0x12345678"), revert.Data)
		r.stateDB.RevertToSnapshot(revision)
		require.Equal(t, common.Hash{}, r.stateDB.GetState(copier, slot0))
		require.Equal(t, common.Hash{}, r.stateDB.GetState(copier, slot1))
	})

	t.Run("the fee-right receiver hook follows the contract rule", func(t *testing.T) {
		r := newNestedRun(t)
		ctx := r.ctx()
		tokenID := big.NewInt(7)
		receive := func(to common.Address) error {
			_, err := r.caller.OnERC721Received(ctx, nestedCaller, nestedCaller, common.Address{}, to, tokenID, nil)
			return err
		}
		require.NoError(t, receive(accepter))
		require.NoError(t, receive(codeless))

		nonReceiver, ok := types.ContractErrorOf(types.ErrNonERC721Receiver)
		require.True(t, ok)
		require.Equal(t, [4]byte{0x80, 0x52, 0x6d, 0x0c}, nonReceiver.Selector)
		for _, to := range []common.Address{emptyReverter, wrongReturner} {
			err := receive(to)
			require.ErrorIs(t, err, types.ErrNonERC721Receiver)
			var revert *launchpad.NestedRevertError
			require.False(t, errors.As(err, &revert))
		}

		var revert *launchpad.NestedRevertError
		err := receive(dataReverter)
		require.True(t, errors.As(err, &revert))
		require.Equal(t, common.FromHex("0x12345678"), revert.Data)
		require.NotErrorIs(t, err, types.ErrNonERC721Receiver)

		err = receive(shortReturner)
		require.True(t, errors.As(err, &revert))
		require.Equal(t, []byte{}, revert.Data)
		require.NotErrorIs(t, err, types.ErrNonERC721Receiver)
	})
}
