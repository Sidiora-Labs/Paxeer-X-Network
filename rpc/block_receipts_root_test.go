package evmrpc_test

import (
	"context"
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
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	banktypes "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/x/bank/types"
	testkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/testutil/keeper"
)

func TestReceiptsRootIsSameBlockResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conf, err := rpctest.CreateConfig(t, t.Name())
	require.NoError(t, err)
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
	// Transactions start after height 1 so block txHeights[0]-1 holds no results.
	waitForHeight(2)

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
		require.Positive(t, committed.Height)
		if len(txHeights) > 0 {
			require.Greater(t, committed.Height, txHeights[len(txHeights)-1])
		}
		txHeights = append(txHeights, committed.Height)
	}
	require.Len(t, txHeights, 3)
	first, last := txHeights[0], txHeights[2]
	require.Greater(t, first, int64(1))
	waitForHeight(last + 1)

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
	api := evmrpc.NewBlockAPI(tm, k, ctxProvider, txConfigProvider, evmrpc.ConnectionTypeHTTP, watermarks, evmrpc.NewBlockCache(3000), &sync.Mutex{})

	resultsRoot := func(height int64) common.Hash {
		h := height
		results, err := tm.BlockResults(ctx, &h)
		require.NoError(t, err)
		require.Equal(t, height, results.Height)
		leaves, err := abci.MarshalTxResults(results.TxsResults)
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

	require.NotEqual(t, resultsRoot(first-1), resultsRoot(first))
	for height := first; height <= last; height++ {
		served, err := api.GetBlockByNumber(ctx, rpc.BlockNumber(height), false)
		require.NoError(t, err)
		require.NotNil(t, served)
		receiptsRoot, ok := served["receiptsRoot"].(common.Hash)
		require.True(t, ok, "height %d receiptsRoot type %T", height, served["receiptsRoot"])

		sameBlockRoot := resultsRoot(height)
		require.Equal(t, headerResultsHash(height+1), receiptsRoot, "height %d", height)
		require.Equal(t, sameBlockRoot, receiptsRoot, "height %d", height)

		byHash, err := api.GetBlockByHash(ctx, served["hash"].(common.Hash), false)
		require.NoError(t, err)
		require.Equal(t, receiptsRoot, byHash["receiptsRoot"], "height %d", height)

		previousRoot := resultsRoot(height - 1)
		ownHeaderRoot := headerResultsHash(height)
		require.Equal(t, previousRoot, ownHeaderRoot, "height %d", height)
		if previousRoot != sameBlockRoot {
			require.NotEqual(t, ownHeaderRoot, receiptsRoot, "height %d", height)
		}
	}
}
