package evmrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

const pendingFilterDeployGas = 200000

var pendingFilterTopic = common.HexToHash("0x00000000000000000000000000000000000000000000000000000000000f17e5")

// pendingFilterInitCode is contract creation code that emits one LOG1 with
// pendingFilterTopic and empty data, then stops: PUSH32 topic, PUSH1 0,
// PUSH1 0, LOG1, STOP.
var pendingFilterInitCode = append(append([]byte{0x7f}, pendingFilterTopic.Bytes()...), 0x60, 0x00, 0x60, 0x00, 0xa1, 0x00)

func pendingFilterTag() *big.Int {
	return big.NewInt(rpc.PendingBlockNumber.Int64())
}

func pendingFilterCrit() filters.FilterCriteria {
	return filters.FilterCriteria{ToBlock: pendingFilterTag(), Topics: [][]common.Hash{{pendingFilterTopic}}}
}

// pendingFilterAPI builds the eth filter API over the sequencer's App and a
// node client reporting latest. A nil feed builds it as every non-sequencer
// node runs it.
func pendingFilterAPI(t *testing.T, s *pendingSequencer, tm *MockClient, feed *evmrpc.ReceiptLogFeed) *evmrpc.FilterAPI {
	t.Helper()
	wm := evmrpc.NewWatermarkManager(tm, s.ctxProvider, nil, nil)
	api := evmrpc.NewFilterAPI(
		tm,
		&s.app.EvmKeeper,
		s.ctxProvider,
		func(int64) client.TxConfig { return TxConfig },
		evmrpc.NewFilterConfig(time.Minute, 0, 0),
		evmrpc.ConnectionTypeHTTP,
		"eth",
		make(chan struct{}, evmrpc.MaxDBReadConcurrency),
		evmrpc.NewBlockCache(16),
		&sync.Mutex{},
		evmrpc.NewLogSlicePool(),
		wm,
	)
	if feed != nil {
		api.SetReceiptLogFeed(feed)
	}
	t.Cleanup(api.Cleanup)
	return api
}

// pendingFilterTx signs an EVM transaction from `from` and encodes it as the
// Cosmos tx the sequencer accepts. A nil `to` creates a contract.
func pendingFilterTx(t *testing.T, from utils.TestAcct, nonce uint64, to *common.Address, value *big.Int, data []byte, gas uint64) ([]byte, common.Hash) {
	t.Helper()
	signed, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(pendingGasPriceWei),
		Gas:      gas,
		To:       to,
		Value:    value,
		Data:     data,
	}), ethtypes.LatestSignerForChainID(big.NewInt(config.DefaultChainID)), from.EvmPrivateKey)
	require.NoError(t, err)
	txData, err := ethtx.NewTxDataFromTx(signed)
	require.NoError(t, err)
	msg, err := evmtypes.NewMsgEVMTransaction(txData)
	require.NoError(t, err)
	txConfig := app.MakeEncodingConfig().TxConfig
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msg))
	builder.SetGasLimit(gas)
	bz, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return bz, signed.Hash()
}

func pendingFilterDeploy(t *testing.T, from utils.TestAcct, nonce uint64) ([]byte, common.Hash) {
	t.Helper()
	return pendingFilterTx(t, from, nonce, nil, big.NewInt(0), pendingFilterInitCode, pendingFilterDeployGas)
}

// pendingFilterReceipt accepts txBytes on the open block and feeds its
// execution receipt to the feed the way the sequencer does after Accept.
func pendingFilterReceipt(t *testing.T, a *app.App, exec sequencer.Executor, feed *evmrpc.ReceiptLogFeed, txBytes []byte, hash common.Hash) *sequencer.Accepted {
	t.Helper()
	acc, err := exec.Accept(txBytes)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code, acc.Result.Log)
	snap, err := exec.Snapshot()
	require.NoError(t, err)
	rcpt, err := a.EvmKeeper.GetTransientReceipt(snap, hash, uint64(acc.TxIndex)) //nolint:gosec // tx index is positive
	require.NoError(t, err)
	require.NoError(t, feed.Receipted(acc.Seq, acc.Height, hash, rcpt))
	return acc
}

func requirePendingFilterLog(t *testing.T, got *evmrpc.FilterLog, seq uint64, height int64, txIndex int, index uint, hash common.Hash, addr common.Address) {
	t.Helper()
	require.NotNil(t, got.Seq)
	require.Equal(t, seq, *got.Seq)
	require.Equal(t, uint64(height), got.BlockNumber) //nolint:gosec // test heights are positive
	require.Equal(t, uint(txIndex), got.TxIndex)      //nolint:gosec // tx index is positive
	require.Equal(t, index, got.Index)
	require.Equal(t, hash, got.TxHash)
	require.Equal(t, addr, got.Address)
	require.Equal(t, []common.Hash{pendingFilterTopic}, got.Topics)
	require.Empty(t, got.Data)
	require.False(t, got.Removed)
}

func requirePendingServedBySequencer(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)
	var coded *evmrpc.ErrEVMNotSupported
	require.True(t, errors.As(err, &coded))
	require.Equal(t, -32000, coded.ErrorCode())
	require.Equal(t, "pending served by sequencer", coded.Error())
}

func TestPendingFilterGetLogsIncludesReceipted(t *testing.T) {
	const committed = int64(1000)
	ctx := context.Background()
	sender := utils.NewSigner()
	s := newPendingSequencer(t, committed, sender)
	feed := evmrpc.NewReceiptLogFeed(committed)
	api := pendingFilterAPI(t, s, &MockClient{latestOverride: committed}, feed)

	// L_N: two receipted deployments at committed+1, then sealed.
	tx0, hash0 := pendingFilterDeploy(t, sender, 0)
	acc0 := pendingFilterReceipt(t, s.app, s.exec, feed, tx0, hash0)
	tx1, hash1 := pendingFilterDeploy(t, sender, 1)
	acc1 := pendingFilterReceipt(t, s.app, s.exec, feed, tx1, hash1)
	require.Equal(t, uint64(1), acc0.Seq)
	require.Equal(t, uint64(2), acc1.Seq)
	sealed, err := s.exec.Seal(committed + 1)
	require.NoError(t, err)
	require.Len(t, sealed.Txs, 2)

	// L_open: the next block continues the seq chain over a store at committed+1.
	sender2 := utils.NewSigner()
	s2 := newPendingSequencer(t, committed+1, sender2)
	empty, err := s2.exec.Seal(committed + 2)
	require.NoError(t, err)
	require.Empty(t, empty.Txs)
	exec2, err := sequencer.NewExecutor(s2.app, sequencer.Options{
		ChainID:         pendingChainID,
		ProposerAddress: empty.Ctx.BlockHeader().ProposerAddress,
		LastSeq:         acc1.Seq,
		LastSeqHash:     acc1.SeqHash,
	})
	require.NoError(t, err)
	require.NoError(t, exec2.Open(committed+2, empty.TimestampMs))
	tx2, hash2 := pendingFilterDeploy(t, sender2, 0)
	acc2 := pendingFilterReceipt(t, s2.app, exec2, feed, tx2, hash2)
	require.Equal(t, uint64(3), acc2.Seq)
	require.Equal(t, committed+2, acc2.Height)

	addr0 := crypto.CreateAddress(sender.EvmAddress, 0)
	addr1 := crypto.CreateAddress(sender.EvmAddress, 1)
	addr2 := crypto.CreateAddress(sender2.EvmAddress, 0)

	logs, err := api.GetLogs(ctx, pendingFilterCrit())
	require.NoError(t, err)
	require.Len(t, logs, 3)
	requirePendingFilterLog(t, logs[0], 1, committed+1, acc0.TxIndex, 0, hash0, addr0)
	requirePendingFilterLog(t, logs[1], 2, committed+1, acc1.TxIndex, 1, hash1, addr1)
	requirePendingFilterLog(t, logs[2], 3, committed+2, acc2.TxIndex, 0, hash2, addr2)

	// Each pending log carries its seq on the wire and has no block hash.
	enc, err := json.Marshal(logs[2])
	require.NoError(t, err)
	var wire map[string]interface{}
	require.NoError(t, json.Unmarshal(enc, &wire))
	require.Equal(t, "0x3", wire["seq"])
	require.Nil(t, wire["blockHash"])
	require.Equal(t, hash2.Hex(), wire["transactionHash"])
	require.Equal(t, "0x3ea", wire["blockNumber"])

	// fromBlock pending, and a numeric fromBlock above the committed height,
	// select receipted logs only.
	fromPending := pendingFilterCrit()
	fromPending.FromBlock = pendingFilterTag()
	logs, err = api.GetLogs(ctx, fromPending)
	require.NoError(t, err)
	require.Len(t, logs, 3)
	fromOpen := pendingFilterCrit()
	fromOpen.FromBlock = big.NewInt(committed + 2)
	logs, err = api.GetLogs(ctx, fromOpen)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	requirePendingFilterLog(t, logs[0], 3, committed+2, acc2.TxIndex, 0, hash2, addr2)

	// A criterion that matches no receipted log returns an empty list.
	other := pendingFilterCrit()
	other.Addresses = []common.Address{common.HexToAddress("0x0000000000000000000000000000000000000f17")}
	logs, err = api.GetLogs(ctx, other)
	require.NoError(t, err)
	require.NotNil(t, logs)
	require.Empty(t, logs)

	// fromBlock pending must end at pending.
	_, err = api.GetLogs(ctx, filters.FilterCriteria{FromBlock: pendingFilterTag(), ToBlock: big.NewInt(committed)})
	require.EqualError(t, err, "fromBlock pending requires toBlock pending")

	// Every other node refuses pending with -32000.
	offSequencer := pendingFilterAPI(t, s, &MockClient{latestOverride: committed}, nil)
	_, err = offSequencer.GetLogs(ctx, pendingFilterCrit())
	requirePendingServedBySequencer(t, err)
	_, err = offSequencer.GetLogs(ctx, fromPending)
	requirePendingServedBySequencer(t, err)
}

func TestPendingFilterNoRepeatAfterCommit(t *testing.T) {
	const committed = int64(500)
	ctx := context.Background()
	sender := utils.NewSigner()
	s := newPendingSequencer(t, committed, sender)
	tm := &MockClient{latestOverride: committed}
	feed := evmrpc.NewReceiptLogFeed(committed)
	api := pendingFilterAPI(t, s, tm, feed)

	id, err := api.NewFilter(ctx, pendingFilterCrit())
	require.NoError(t, err)
	changes := func() []*evmrpc.FilterLog {
		t.Helper()
		res, err := api.GetFilterChanges(ctx, id)
		require.NoError(t, err)
		logs, ok := res.([]*evmrpc.FilterLog)
		require.True(t, ok, "changes of a pending logs filter are %T", res)
		return logs
	}
	require.Empty(t, changes())

	addr0 := crypto.CreateAddress(sender.EvmAddress, 0)
	addr1 := crypto.CreateAddress(sender.EvmAddress, 1)

	// Returned once at receipt.
	tx0, hash0 := pendingFilterDeploy(t, sender, 0)
	acc0 := pendingFilterReceipt(t, s.app, s.exec, feed, tx0, hash0)
	got := changes()
	require.Len(t, got, 1)
	requirePendingFilterLog(t, got[0], acc0.Seq, committed+1, acc0.TxIndex, 0, hash0, addr0)
	require.Empty(t, changes())

	// getFilterLogs returns every receipted log once and leaves the changes
	// cursor where it was.
	tx1, hash1 := pendingFilterDeploy(t, sender, 1)
	acc1 := pendingFilterReceipt(t, s.app, s.exec, feed, tx1, hash1)
	all, err := api.GetFilterLogs(ctx, id)
	require.NoError(t, err)
	require.Len(t, all, 2)
	requirePendingFilterLog(t, all[0], acc0.Seq, committed+1, acc0.TxIndex, 0, hash0, addr0)
	requirePendingFilterLog(t, all[1], acc1.Seq, committed+1, acc1.TxIndex, 1, hash1, addr1)
	got = changes()
	require.Len(t, got, 1)
	requirePendingFilterLog(t, got[0], acc1.Seq, committed+1, acc1.TxIndex, 1, hash1, addr1)
	require.Empty(t, changes())

	// The block commits: its logs are not returned again.
	sealed, err := s.exec.Seal(committed + 1)
	require.NoError(t, err)
	require.Len(t, sealed.Txs, 2)
	require.NoError(t, feed.Committed(committed+1))
	tm.latestOverride = committed + 1
	require.Empty(t, changes())
	require.Empty(t, changes())

	// The feed refuses to go back below a committed height or to reuse a seq.
	require.EqualError(t, feed.Committed(committed), "receipt log feed: committed height 500 is below 501")
	require.EqualError(t, feed.Receipted(acc1.Seq, committed+2, hash1, &evmtypes.Receipt{}), "receipt log feed: seq 2 does not follow seq 2")
	require.EqualError(t, feed.Receipted(acc1.Seq+1, committed+1, hash1, &evmtypes.Receipt{}), "receipt log feed: height 501 is not above committed height 501")

	// Every other node refuses a pending filter with -32000.
	offSequencer := pendingFilterAPI(t, s, tm, nil)
	_, err = offSequencer.NewFilter(ctx, pendingFilterCrit())
	requirePendingServedBySequencer(t, err)
}

func TestPendingFilterPendingTransactions(t *testing.T) {
	const committed = int64(200)
	ctx := context.Background()
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, committed, sender)
	feed := evmrpc.NewReceiptLogFeed(committed)
	api := pendingFilterAPI(t, s, &MockClient{latestOverride: committed}, feed)

	id, err := api.NewPendingTransactionFilter(ctx, nil)
	require.NoError(t, err)
	hashes := func() []common.Hash {
		t.Helper()
		res, err := api.GetFilterChanges(ctx, id)
		require.NoError(t, err)
		got, ok := res.([]common.Hash)
		require.True(t, ok, "changes of a pending transaction filter are %T", res)
		return got
	}
	require.Empty(t, hashes())

	want := make([]common.Hash, 3)
	for nonce := range want {
		bz, hash := pendingFilterTx(t, sender, uint64(nonce), &recipient, big.NewInt(pendingOnePaxWei), nil, pendingTransferGas) //nolint:gosec // nonce is a small index
		acc := pendingFilterReceipt(t, s.app, s.exec, feed, bz, hash)
		require.Equal(t, uint64(nonce+1), acc.Seq) //nolint:gosec // nonce is a small index
		want[nonce] = hash
	}
	require.Equal(t, want, hashes())
	require.Empty(t, hashes())

	// Commit does not replay receipted hashes.
	_, err = s.exec.Seal(committed + 1)
	require.NoError(t, err)
	require.NoError(t, feed.Committed(committed+1))
	require.Empty(t, hashes())

	_, err = api.GetFilterLogs(ctx, id)
	require.EqualError(t, err, "pending transaction filter has no logs")
	fullTx := true
	_, err = api.NewPendingTransactionFilter(ctx, &fullTx)
	require.EqualError(t, err, "eth_newPendingTransactionFilter serves transaction hashes only")

	// A full node keeps it unsupported.
	offSequencer := pendingFilterAPI(t, s, &MockClient{latestOverride: committed}, nil)
	_, err = offSequencer.NewPendingTransactionFilter(ctx, nil)
	var coded *evmrpc.ErrEVMNotSupported
	require.True(t, errors.As(err, &coded))
	require.Equal(t, -32000, coded.ErrorCode())
	require.Equal(t, "eth_newPendingTransactionFilter is not supported on Pax EVM RPC", coded.Error())
}
