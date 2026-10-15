package evmrpc_test

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/coretypes"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
)

var (
	syntheticRealEmitter  = common.HexToAddress("0x00000000000000000000000000000000000000f1")
	syntheticPerpsEmitter = common.HexToAddress("0x00000000000000000000000000000000000000f2")
	syntheticRealTopic    = common.HexToHash("0x0f1f")
	syntheticPerpsTopic   = common.HexToHash("0x0f2f")
)

type syntheticReceiptFixture struct {
	k                *keeper.Keeper
	block            *coretypes.ResultBlock
	tm               *parityTxCountTMClient
	ctxProvider      func(int64) sdk.Context
	txConfigProvider func(int64) client.TxConfig
	watermarks       *evmrpc.WatermarkManager
	cache            evmrpc.BlockCache
	mu               *sync.Mutex
	realHashes       []common.Hash
	perpsHash        common.Hash
}

// newSyntheticReceiptFixture builds a block of three EVM transactions whose height also
// carries one perps end-block receipt written through SyntheticReceiptContext and CallEVM.
func newSyntheticReceiptFixture(t *testing.T) *syntheticReceiptFixture {
	application := app.Setup(t, false, true, false)
	k := &application.EvmKeeper
	ctx := application.GetContextForDeliverTx(nil).
		WithBlockHeight(parityTestHeight).
		WithBlockTime(time.Unix(1700000000, 0)).
		WithClosestUpgradeName("v6.0.0")
	k.BeginBlock(ctx)
	k.SetTxResults([]*abci.ExecTxResult{{Code: 0}, {Code: 0}, {Code: 0}})

	code := append([]byte{0x7f}, syntheticPerpsTopic.Bytes()...)
	code = append(code, 0x60, 0x00, 0x60, 0x00, 0xa1, 0x00)
	k.SetCode(ctx, syntheticPerpsEmitter, code)
	_, err := k.CallEVM(keeper.SyntheticReceiptContext(ctx, keeper.SyntheticReceiptPerps),
		common.HexToAddress("0x00000000000000000000000000000000000ca11e"), &syntheticPerpsEmitter, nil, nil)
	require.NoError(t, err)

	txs := []sdk.Tx{MultiTxBlockTx1, MultiTxBlockTx2, MultiTxBlockTx3}
	fixture := &syntheticReceiptFixture{k: k, perpsHash: keeper.SyntheticReceiptHash(keeper.SyntheticReceiptPerps, parityTestHeight)}
	encoded := make([]tmtypes.Tx, 0, len(txs))
	for i, tx := range txs {
		bz, err := Encoder(tx)
		require.NoError(t, err)
		encoded = append(encoded, bz)
		etx, _ := tx.GetMsgs()[0].(*types.MsgEVMTransaction).AsTransaction()
		require.NotNil(t, etx)
		fixture.realHashes = append(fixture.realHashes, etx.Hash())
		ethLog := &ethtypes.Log{Address: syntheticRealEmitter, Topics: []common.Hash{syntheticRealTopic}}
		bloom := ethtypes.CreateBloom(&ethtypes.Receipt{Logs: []*ethtypes.Log{ethLog}})
		require.NoError(t, k.MockReceipt(ctx, etx.Hash(), &types.Receipt{
			From:              "0x1234567890123456789012345678901234567890",
			To:                syntheticRealEmitter.Hex(),
			TransactionIndex:  uint32(i), //nolint:gosec
			BlockNumber:       uint64(parityTestHeight),
			TxType:            2,
			TxHashHex:         etx.Hash().Hex(),
			GasUsed:           21000,
			Status:            1,
			EffectiveGasPrice: 1,
			Logs:              []*types.Log{keeper.ConvertEthLog(ethLog)},
			LogsBloom:         bloom[:],
		}))
	}
	require.Eventually(t, func() bool {
		_, err := k.GetReceipt(ctx, fixture.perpsHash)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	k.EndBlock(ctx, 0, 0)

	fixture.block = &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header:     mockBlockHeader(parityTestHeight),
			Data:       tmtypes.Data{Txs: encoded},
			LastCommit: &tmtypes.Commit{Height: parityTestHeight - 1},
		},
	}
	fixture.ctxProvider = func(h int64) sdk.Context {
		if h == evmrpc.LatestCtxHeight {
			return ctx
		}
		return ctx.WithBlockHeight(h)
	}
	fixture.txConfigProvider = func(int64) client.TxConfig { return TxConfig }
	fixture.tm = &parityTxCountTMClient{block: fixture.block}
	fixture.watermarks = evmrpc.NewWatermarkManager(fixture.tm, fixture.ctxProvider, nil, nil)
	fixture.cache = evmrpc.NewBlockCache(3000)
	fixture.mu = &sync.Mutex{}
	return fixture
}

func (f *syntheticReceiptFixture) filterAPI(t *testing.T, namespace string) *evmrpc.FilterAPI {
	api := evmrpc.NewFilterAPI(f.tm, f.k, f.ctxProvider, f.txConfigProvider, evmrpc.NewFilterConfig(time.Minute, 0, 0),
		evmrpc.ConnectionTypeHTTP, namespace, make(chan struct{}, evmrpc.MaxDBReadConcurrency), f.cache, f.mu,
		evmrpc.NewLogSlicePool(), f.watermarks)
	t.Cleanup(api.Cleanup)
	return api
}

func (f *syntheticReceiptFixture) requirePerpsLog(t *testing.T, l *ethtypes.Log) {
	require.Equal(t, syntheticPerpsEmitter, l.Address)
	require.Equal(t, []common.Hash{syntheticPerpsTopic}, l.Topics)
	require.Equal(t, f.perpsHash, l.TxHash)
	require.Equal(t, uint(3), l.TxIndex)
	require.Equal(t, uint(3), l.Index)
	require.Equal(t, uint64(parityTestHeight), l.BlockNumber)
	require.Equal(t, common.BytesToHash(f.block.BlockID.Hash), l.BlockHash)
}

func TestVenueSyntheticReceiptHashes(t *testing.T) {
	f := newSyntheticReceiptFixture(t)
	blockHash := common.BytesToHash(f.block.BlockID.Hash)
	for _, namespace := range []string{"eth", "pax"} {
		logs, err := f.filterAPI(t, namespace).GetLogs(context.Background(), filters.FilterCriteria{BlockHash: &blockHash})
		require.NoError(t, err, namespace)
		require.Len(t, logs, 4, namespace)
		for i, h := range f.realHashes {
			require.Equal(t, h, logs[i].TxHash, namespace)
			require.Equal(t, uint(i), logs[i].TxIndex, namespace)
			require.Equal(t, uint(i), logs[i].Index, namespace)
			require.Equal(t, syntheticRealEmitter, logs[i].Address, namespace)
		}
		f.requirePerpsLog(t, logs[3].Log)
	}
}

func TestVenueSyntheticReceiptEndpoints(t *testing.T) {
	f := newSyntheticReceiptFixture(t)
	height := big.NewInt(parityTestHeight)

	for _, namespace := range []string{"eth", "pax"} {
		t.Run(namespace+"_getLogs", func(t *testing.T) {
			logs, err := f.filterAPI(t, namespace).GetLogs(context.Background(), filters.FilterCriteria{
				FromBlock: height,
				ToBlock:   height,
				Addresses: []common.Address{syntheticPerpsEmitter},
				Topics:    [][]common.Hash{{syntheticPerpsTopic}},
			})
			require.NoError(t, err)
			require.Len(t, logs, 1)
			f.requirePerpsLog(t, logs[0].Log)
		})
	}

	t.Run("eth_getBlockByNumber", func(t *testing.T) {
		for _, includeSynthetic := range []bool{false, true} {
			encoded, err := evmrpc.EncodeTmBlock(f.ctxProvider, f.txConfigProvider, f.block, f.k, true, false, includeSynthetic, false, f.cache, f.mu)
			require.NoError(t, err)
			list := encoded["transactions"].([]interface{})
			require.Len(t, list, len(f.realHashes))
			for i, entry := range list {
				tx, ok := entry.(*export.RPCTransaction)
				require.True(t, ok)
				require.Equal(t, f.realHashes[i], tx.Hash)
				require.NotEqual(t, f.perpsHash, tx.Hash)
			}
		}
	})

	t.Run("eth_getBlockReceipts", func(t *testing.T) {
		api := evmrpc.NewBlockAPI(f.tm, f.k, f.ctxProvider, f.txConfigProvider, evmrpc.ConnectionTypeHTTP, f.watermarks, f.cache, f.mu)
		receipts, err := api.GetBlockReceipts(context.Background(), rpc.BlockNumberOrHashWithNumber(rpc.BlockNumber(parityTestHeight)))
		require.NoError(t, err)
		require.Len(t, receipts, 4)
		for i, h := range f.realHashes {
			require.Equal(t, h, receipts[i]["transactionHash"])
			require.Equal(t, hexutil.Uint64(i), receipts[i]["transactionIndex"]) //nolint:gosec
		}
		perps := receipts[3]
		require.Equal(t, f.perpsHash, perps["transactionHash"])
		require.Equal(t, hexutil.Uint64(3), perps["transactionIndex"])
		logs := perps["logs"].([]*ethtypes.Log)
		require.Len(t, logs, 1)
		f.requirePerpsLog(t, logs[0])
	})

	t.Run("eth_getTransactionReceipt", func(t *testing.T) {
		api := evmrpc.NewTransactionAPI(f.tm, f.k, f.ctxProvider, f.txConfigProvider, "", evmrpc.ConnectionTypeHTTP,
			utils.None[time.Duration](), f.watermarks, f.cache, f.mu)
		receipt, err := api.GetTransactionReceipt(context.Background(), f.perpsHash)
		require.NoError(t, err)
		require.NotNil(t, receipt)
		require.Equal(t, f.perpsHash, receipt["transactionHash"])
		require.Equal(t, hexutil.Uint64(parityTestHeight), receipt["blockNumber"])
		require.Equal(t, hexutil.Uint64(3), receipt["transactionIndex"])
		require.Equal(t, hexutil.Uint(ethtypes.ReceiptStatusSuccessful), receipt["status"])
		logs := receipt["logs"].([]*ethtypes.Log)
		require.Len(t, logs, 1)
		f.requirePerpsLog(t, logs[0])
	})
}
