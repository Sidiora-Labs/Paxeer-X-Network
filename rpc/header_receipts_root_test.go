package evmrpc_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/example/kvstore"
	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/merkle"
	rpclocal "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/client/local"
	rpctest "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/test"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	evmrpcconfig "github.com/Sidiora-Labs/Paxeer-X-Network/rpc/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
)

func TestHeaderReceiptsRootIsSameBlockResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	conf, err := rpctest.CreateConfig(t, t.Name())
	require.NoError(t, err)
	// A loaded host commits slower than the default ten-second broadcast wait.
	conf.RPC.TimeoutBroadcastTxCommit = 2 * time.Minute
	kv := kvstore.NewApplication()
	nodeService, closer, err := rpctest.StartTendermint(ctx, conf, kv, rpctest.SuppressStdout)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, closer(context.Background()))
		require.NoError(t, kv.Close())
	})
	nodeWithEnv, ok := nodeService.(rpclocal.NodeService)
	require.True(t, ok)
	tm, err := rpclocal.New(nodeWithEnv)
	require.NoError(t, err)

	waitForHeight := func(target int64) {
		for {
			status, err := tm.Status(ctx)
			require.NoError(t, err)
			if status.SyncInfo.LatestBlockHeight >= target {
				return
			}
			select {
			case <-ctx.Done():
				require.FailNow(t, "chain did not reach height", "target %d: %v", target, ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
	}
	waitForHeight(2)

	application := app.Setup(t, false, true, false)
	k := &application.EvmKeeper
	sdkCtx := application.GetContextForDeliverTx(nil).
		WithBlockTime(time.Now()).
		WithClosestUpgradeName("v6.0.0")
	ctxProvider := func(h int64) sdk.Context {
		if h == evmrpc.LatestCtxHeight {
			return sdkCtx
		}
		return sdkCtx.WithBlockHeight(h)
	}
	txConfigProvider := func(int64) client.TxConfig { return TxConfig }
	watermarks := evmrpc.NewWatermarkManager(tm, nil, nil, nil)
	blockAPI := evmrpc.NewBlockAPI(tm, k, ctxProvider, txConfigProvider, evmrpc.ConnectionTypeHTTP, watermarks, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	backend := evmrpc.NewBackend(ctxProvider, k, application.BeginBlockKeepers, txConfigProvider, tm, &SConfig,
		application.BaseApp, application.TracerAnteHandler, evmrpc.NewBlockCache(3000), &sync.Mutex{}, watermarks)

	freePort := func() int {
		listener, err := net.Listen("tcp", TestAddr+":0")
		require.NoError(t, err)
		port := listener.Addr().(*net.TCPAddr).Port
		require.NoError(t, listener.Close())
		return port
	}
	startWS := func(notifier *evmrpc.BlockHeaderNotifier) chan map[string]interface{} {
		cfg := evmrpcconfig.DefaultConfig
		cfg.WSPort = freePort()
		server, err := evmrpc.NewEVMWebSocketServer(cfg, tm, k, application.BeginBlockKeepers, application.BaseApp,
			application.TracerAnteHandler, ctxProvider, txConfigProvider, "", nil, notifier)
		require.NoError(t, err)
		require.NoError(t, server.Start())
		t.Cleanup(server.Stop)
		wsClient, err := rpc.DialWebsocket(ctx, fmt.Sprintf("ws://%s:%d", TestAddr, cfg.WSPort), "localhost")
		require.NoError(t, err)
		t.Cleanup(wsClient.Close)
		heads := make(chan map[string]interface{}, 64)
		sub, err := wsClient.EthSubscribe(ctx, heads, "newHeads")
		require.NoError(t, err)
		t.Cleanup(sub.Unsubscribe)
		return heads
	}
	// The event-bus server encodes every committed head through encodeTmHeader.
	tmHeads := startWS(nil)
	notifier := evmrpc.NewBlockHeaderNotifier(4)
	committedHeads := startWS(notifier)

	txHeights := make([]int64, 0, 3)
	for i := 0; i < 3; i++ {
		fromPaxAddr, _ := testkeeper.MockAddressPair()
		toPaxAddr, _ := testkeeper.MockAddressPair()
		builder := TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(banktypes.NewMsgSend(fromPaxAddr, toPaxAddr, sdk.NewCoins(sdk.NewCoin("uhpx", sdk.NewInt(int64(10+i)))))))
		txBytes, err := Encoder(builder.GetTx())
		require.NoError(t, err)
		committed, err := tm.BroadcastTxCommit(ctx, txBytes)
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, committed.CheckTx.Code)
		require.Equal(t, abci.CodeTypeOK, committed.TxResult.Code)
		if len(txHeights) > 0 {
			require.Greater(t, committed.Height, txHeights[len(txHeights)-1])
		}
		txHeights = append(txHeights, committed.Height)
	}
	first, last := txHeights[0], txHeights[2]
	require.Greater(t, first, int64(1))
	waitForHeight(last + 1)

	resultsOf := func(height int64) *abci.ResponseFinalizeBlock {
		h := height
		results, err := tm.BlockResults(ctx, &h)
		require.NoError(t, err)
		require.Equal(t, height, results.Height)
		return &abci.ResponseFinalizeBlock{TxResults: results.TxsResults}
	}
	resultsRoot := func(height int64) common.Hash {
		leaves, err := abci.MarshalTxResults(resultsOf(height).TxResults)
		require.NoError(t, err)
		return common.BytesToHash(merkle.HashFromByteSlices(leaves))
	}
	headerResultsHash := func(height int64) common.Hash {
		h := height
		header, err := tm.Header(ctx, &h)
		require.NoError(t, err)
		require.NotNil(t, header.Header)
		require.Equal(t, height, header.Header.Height)
		return common.BytesToHash(header.Header.LastResultsHash)
	}
	awaitHead := func(heads chan map[string]interface{}, height int64) map[string]interface{} {
		want := fmt.Sprintf("0x%x", height)
		for {
			select {
			case head := <-heads:
				if head["number"] == want {
					return head
				}
			case <-ctx.Done():
				require.FailNow(t, "no newHeads payload", "height %d: %v", height, ctx.Err())
			}
		}
	}

	// Commit hands the notifier block H's header and finalize response.
	publishCommitted := func(height int64) {
		h := height
		block, err := tm.Block(ctx, &h)
		require.NoError(t, err)
		require.NotNil(t, block.Block)
		next := height + 1
		nextBlock, err := tm.Block(ctx, &next)
		require.NoError(t, err)
		require.NotNil(t, nextBlock.Block)
		response := resultsOf(height)
		// The kvstore app hash is shorter than a state root; the head carries it as a 32-byte word.
		response.AppHash = common.BytesToHash(nextBlock.Block.AppHash).Bytes()
		notifier.OnBlockCommitted(block.BlockID.Hash, block.Block.Header.ToProto(), response)
	}

	require.NotEqual(t, resultsRoot(first-1), resultsRoot(first))
	for height := first; height <= last; height++ {
		sameBlockRoot := resultsRoot(height)
		require.Equal(t, headerResultsHash(height+1), sameBlockRoot, "height %d", height)

		served, err := blockAPI.GetBlockByNumber(ctx, rpc.BlockNumber(height), false)
		require.NoError(t, err)
		require.Equal(t, sameBlockRoot, served["receiptsRoot"], "height %d", height)

		tmHead := awaitHead(tmHeads, height)
		require.Equal(t, sameBlockRoot.Hex(), tmHead["receiptsRoot"], "event-bus height %d", height)
		publishCommitted(height)
		committedHead := awaitHead(committedHeads, height)
		require.Equal(t, sameBlockRoot.Hex(), committedHead["receiptsRoot"], "committed height %d", height)

		header, err := backend.HeaderByNumber(ctx, rpc.BlockNumber(height))
		require.NoError(t, err)
		require.Equal(t, height, header.Number.Int64())
		require.Equal(t, sameBlockRoot, header.ReceiptHash, "height %d", height)

		ownHeaderRoot := headerResultsHash(height)
		require.Equal(t, resultsRoot(height-1), ownHeaderRoot, "height %d", height)
		if height == first {
			require.NotEqual(t, ownHeaderRoot, sameBlockRoot)
		}
		if ownHeaderRoot != sameBlockRoot {
			require.NotEqual(t, ownHeaderRoot.Hex(), tmHead["receiptsRoot"], "height %d", height)
			require.NotEqual(t, ownHeaderRoot.Hex(), committedHead["receiptsRoot"], "height %d", height)
			require.NotEqual(t, ownHeaderRoot, header.ReceiptHash, "height %d", height)
		}
	}
}
