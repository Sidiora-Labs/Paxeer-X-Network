package evmrpc_test

import (
	"context"
	"encoding/json"
	"math/big"
	"sync"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/crypto/keys/secp256k1"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/tests/occ/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

const pendingBlockBaseFeeWei = 70_000_000

func pendingBlockAPI(s *pendingSequencer) *evmrpc.BlockAPI {
	txConfig := s.app.GetTxConfig()
	return evmrpc.NewBlockAPI(&MockClient{latestOverride: s.committed}, &s.app.EvmKeeper, s.ctxProvider,
		func(int64) client.TxConfig { return txConfig }, evmrpc.ConnectionTypeHTTP, s.wm, nil, &sync.Mutex{})
}

// pendingBlockEthHash decodes txBytes as the node does and returns its EVM hash.
func pendingBlockEthHash(t *testing.T, s *pendingSequencer, txBytes []byte) common.Hash {
	t.Helper()
	tx, err := s.app.GetTxConfig().TxDecoder()(txBytes)
	require.NoError(t, err)
	msg := s.app.GetEVMMsg(tx)
	require.NotNil(t, msg)
	etx, _ := msg.AsTransaction()
	require.NotNil(t, etx)
	return etx.Hash()
}

// pendingBlockFeeSequencer commits a real App to committed with baseFeeWei as
// its next base fee, then opens committed+1 through the fee floor, so the open
// block fixes baseFeeWei at Open.
func pendingBlockFeeSequencer(t *testing.T, committed int64, baseFeeWei int64) (*sequencer.FeeFloor, *evmrpc.WatermarkManager) {
	t.Helper()
	require.GreaterOrEqual(t, committed, int64(2))
	genesis := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	a := app.NewTestWrapper(t, genesis, valPub, false).App
	_, err := a.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Header: &tmproto.Header{ChainID: pendingChainID, Height: 1},
	})
	require.NoError(t, err)
	a.SetDeliverStateToCommit()
	_, err = a.Commit(context.Background())
	require.NoError(t, err)
	for height := int64(2); height <= committed; height++ {
		if height == committed {
			ctx := sdk.NewContext(a.CommitMultiStore(), tmproto.Header{ChainID: pendingChainID, Height: height}, false)
			a.EvmKeeper.SetNextBaseFeePerGas(ctx, sdk.NewDec(baseFeeWei))
		}
		require.Equal(t, height, a.CommitMultiStore().Commit(true).Version)
	}
	require.Equal(t, committed, a.LastBlockHeight())

	exec, err := sequencer.NewExecutor(a, sequencer.Options{
		ChainID:         pendingChainID,
		ProposerAddress: valPub.Address().Bytes(),
	})
	require.NoError(t, err)
	floor, err := sequencer.NewFeeFloor(exec, a, "", a.ParamsKeeper, a.OracleKeeper, a.PricefeedKeeper, &a.EvmKeeper)
	require.NoError(t, err)
	require.NoError(t, floor.Open(committed+1, genesis.Add(time.Second).UnixMilli()))

	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.PendingCtxHeight {
			ctx, err := floor.Snapshot()
			if err != nil {
				panic(err)
			}
			return ctx
		}
		return sdk.NewContext(a.CommitMultiStore().CacheMultiStore(), tmproto.Header{ChainID: pendingChainID, Height: committed}, false)
	}
	wm := evmrpc.NewWatermarkManager(&MockClient{latestOverride: committed}, ctxProvider, nil, nil)
	wm.SetPendingSnapshot(floor)
	return floor, wm
}

func TestPendingBlockOpenBlock(t *testing.T) {
	const committed = int64(1000)
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, committed, sender)
	api := pendingBlockAPI(s)
	ctx := context.Background()

	empty, err := api.GetBlockByNumber(ctx, rpc.PendingBlockNumber, false)
	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(committed+1)), empty["number"])
	require.Equal(t, []common.Hash{}, empty["transactions"])

	var want []common.Hash
	var openTimeMs int64
	for nonce := uint64(0); nonce < 3; nonce++ {
		tx := pendingTransfer(t, sender, nonce, recipient, big.NewInt(pendingOnePaxWei))
		accepted, err := s.exec.Accept(tx)
		require.NoError(t, err)
		require.Equal(t, committed+1, accepted.Height)
		require.Equal(t, abci.CodeTypeOK, accepted.Result.Code)
		openTimeMs = accepted.TimestampMs
		want = append(want, pendingBlockEthHash(t, s, tx))
	}

	block, err := api.GetBlockByNumber(ctx, rpc.PendingBlockNumber, false)
	require.NoError(t, err)
	require.Equal(t, (*hexutil.Big)(big.NewInt(committed+1)), block["number"])
	require.Equal(t, hexutil.Uint64(time.UnixMilli(openTimeMs).Unix()), block["timestamp"])
	require.Equal(t, want, block["transactions"])
	encoded, err := json.Marshal(block)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.JSONEq(t, "null", string(fields["hash"]))
	require.JSONEq(t, "null", string(fields["stateRoot"]))
	require.JSONEq(t, `"0x3e9"`, string(fields["number"]))

	_, err = api.GetBlockByNumber(ctx, rpc.PendingBlockNumber, true)
	require.EqualError(t, err, "pending block lists transaction hashes only")

	_, err = s.exec.Seal(committed + 1)
	require.NoError(t, err)
	_, err = api.GetBlockByNumber(ctx, rpc.PendingBlockNumber, false)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)

	// Every other node refuses pending.
	pending := sendRequestGood(t, "getBlockByNumber", "pending", false)
	errObj, ok := pending["error"].(map[string]interface{})
	require.True(t, ok)
	require.Equal(t, float64(-32000), errObj["code"])
	require.Equal(t, "pending served by sequencer", errObj["message"])
	require.NotContains(t, pending, "result")
}

func TestPendingBlockNumberStaysCommitted(t *testing.T) {
	const committed = int64(1000)
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, committed, sender)
	api := evmrpc.NewInfoAPI(&MockClient{latestOverride: committed}, &s.app.EvmKeeper, s.ctxProvider, nil, "", 1024, evmrpc.ConnectionTypeHTTP, nil, s.wm)
	ctx := context.Background()

	_, err := s.exec.Accept(pendingTransfer(t, sender, 0, recipient, big.NewInt(pendingOnePaxWei)))
	require.NoError(t, err)
	pendingHeight, err := evmrpc.GetBlockNumberByNrOrHash(ctx, nil, s.wm, rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber))
	require.NoError(t, err)
	require.Equal(t, committed+1, *pendingHeight)

	number, err := api.BlockNumber(ctx)
	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(committed), number)
}

func TestPendingBlockGasPriceFromOpenBlock(t *testing.T) {
	floor, wm := pendingBlockFeeSequencer(t, MockHeight8, pendingBlockBaseFeeWei)
	fixed, err := floor.BaseFeePerGas()
	require.NoError(t, err)
	require.Equal(t, sdk.NewDec(pendingBlockBaseFeeWei), fixed)

	ctxProvider := func(height int64) sdk.Context {
		if height == evmrpc.LatestCtxHeight {
			height = MockHeight8
		}
		return Ctx.WithBlockHeight(height).WithIsTracing(true).WithClosestUpgradeName(LatestCtxUpgradeName)
	}
	txConfigProvider := func(int64) client.TxConfig { return TxConfig }
	committedWM := evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, EVMKeeper.ReceiptStore())
	committedAPI := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, ctxProvider, txConfigProvider, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder, committedWM)
	sequencerAPI := evmrpc.NewInfoAPI(&MockClient{}, EVMKeeper, ctxProvider, txConfigProvider, "", 1024, evmrpc.ConnectionTypeHTTP, Decoder, wm)
	ctx := context.Background()

	// Not congested: the price is the open block's base fee plus 10%.
	price, err := sequencerAPI.GasPrice(ctx)
	require.NoError(t, err)
	require.Equal(t, big.NewInt(77_000_000), price.ToInt())

	committedFee := EVMKeeper.GetNextBaseFeePerGas(ctxProvider(evmrpc.LatestCtxHeight)).TruncateInt().BigInt()
	require.NotEqual(t, big.NewInt(pendingBlockBaseFeeWei), committedFee)
	committedPrice, err := committedAPI.GasPrice(ctx)
	require.NoError(t, err)
	wantCommitted := new(big.Int).Div(new(big.Int).Mul(committedFee, big.NewInt(110)), big.NewInt(100))
	require.Equal(t, wantCommitted, committedPrice.ToInt())

	// The child base fee after the latest committed block is the open block's.
	history, err := sequencerAPI.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{50})
	require.NoError(t, err)
	require.Len(t, history.BaseFee, 2)
	require.Equal(t, big.NewInt(pendingBlockBaseFeeWei), history.BaseFee[1].ToInt())
	committedHistory, err := committedAPI.FeeHistory(ctx, 1, rpc.LatestBlockNumber, []float64{50})
	require.NoError(t, err)
	require.Equal(t, committedHistory.BaseFee[0], history.BaseFee[0])
	require.Equal(t, committedHistory.Reward, history.Reward)

	tip, err := sequencerAPI.MaxPriorityFeePerGas(ctx)
	require.NoError(t, err)
	committedTip, err := committedAPI.MaxPriorityFeePerGas(ctx)
	require.NoError(t, err)
	require.Equal(t, committedTip, tip)

	// Without an open block the sequencer refuses rather than report a stale fee.
	_, err = floor.Seal(MockHeight8 + 1)
	require.NoError(t, err)
	_, err = sequencerAPI.GasPrice(ctx)
	require.ErrorIs(t, err, sequencer.ErrLayerNotOpen)
}
