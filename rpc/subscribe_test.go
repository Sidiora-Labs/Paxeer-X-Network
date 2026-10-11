package evmrpc_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	app "github.com/Sidiora-Labs/Paxeer-X-Network/node"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	gethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

func TestSubscribeNewHeads(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	expectedKeys := []string{
		"parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot",
		"receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit",
		"gasUsed", "timestamp", "extraData", "mixHash", "nonce",
		"baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas",
		"parentBeaconBlockRoot", "hash",
	}
	inapplicableKeys := make(map[string]struct{})
	for _, key := range []string{
		"difficulty", "extraData", "logsBloom", "nonce", "sha3Uncles", "mixHash",
		"excessBlobGas", "parentBeaconBlockRoot",
		"withdrawalsRoot", "blobGasUsed",
	} {
		inapplicableKeys[key] = struct{}{}
	}
	var subscriptionId string
	allZerosRE := regexp.MustCompile(`^0+$`)
	for t.Context().Err() == nil {
		select {
		case resObj := <-recvCh:
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			if subscriptionId == "" {
				// get subscriptionId from first message
				subscriptionId = resObj["result"].(string)
				// Reset timer now that subscription is confirmed, then trigger new heads
				timer.Reset(5 * time.Second)
				NewHeadsCalled <- struct{}{}
				continue
			}
			method := resObj["method"].(string)
			if method != "eth_subscription" {
				t.Fatal("Method is not eth_subscription")
			}
			paramMap := resObj["params"].(map[string]interface{})
			if paramMap["subscription"] != subscriptionId {
				t.Fatal("Subscription ID does not match")
			}
			resultMap := paramMap["result"].(map[string]interface{})
			// check all fields
			for _, key := range expectedKeys {
				if _, ok := resultMap[key]; !ok {
					t.Fatalf("%s is nil", key)
				}
				// check that applicable keys aren't all 0's
				if _, inapplicable := inapplicableKeys[key]; !inapplicable {
					got := fmt.Sprintf("%v", resultMap[key])
					if allZerosRE.MatchString(got) {
						t.Fatalf("%s must be non-zero (got %v)", key, resultMap[key])
					}
				}
			}
			// Event validated successfully, no need to wait further
			return
		case <-timer.C:
			t.Fatal("No event received within 5 seconds")
		}
	}
}

// TestSubscribeNewHeadsAutobahn exercises the in-process notifier path
// (used under Autobahn) end-to-end: a WS client subscribes to newHeads
// against the notifier-backed server, the test pushes an event via
// NotifierForTest.OnBlockCommitted, and the WS subscriber must receive an
// eth_subscription notification carrying the encoded header.
func TestSubscribeNewHeadsAutobahn(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestAndListen(t, TestNotifierWSPort, "subscribe", "newHeads")
	defer func() { done <- struct{}{} }()

	hash := common.HexToHash("0x4242424242424242424242424242424242424242424242424242424242424242").Bytes()
	appHash := common.HexToHash("0x3131313131313131313131313131313131313131313131313131313131313131").Bytes()
	proposer := common.HexToAddress("0x9999999999999999999999999999999999999999").Bytes()
	ts := time.Unix(1_700_000_500, 0).UTC()

	expectedKeys := []string{
		"parentHash", "sha3Uncles", "miner", "stateRoot", "transactionsRoot",
		"receiptsRoot", "logsBloom", "difficulty", "number", "gasLimit",
		"gasUsed", "timestamp", "extraData", "mixHash", "nonce",
		"baseFeePerGas", "withdrawalsRoot", "blobGasUsed", "excessBlobGas",
		"parentBeaconBlockRoot", "hash",
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	var subscriptionId string
	for t.Context().Err() == nil {
		select {
		case resObj := <-recvCh:
			if errVal, ok := resObj["error"]; ok {
				t.Fatal("Received error:", errVal)
			}
			if subscriptionId == "" {
				subscriptionId = resObj["result"].(string)
				timer.Reset(5 * time.Second)
				NotifierForTest.OnBlockCommitted(hash, &tmproto.Header{
					Height:          MockHeight8,
					Time:            ts,
					ProposerAddress: proposer,
				}, &abci.ResponseFinalizeBlock{
					AppHash: appHash,
					TxResults: []*abci.ExecTxResult{
						{GasUsed: 21000},
						{GasUsed: 50000},
					},
					// gasLimit is sourced from the SDK ConsensusParams in
					// the consumer goroutine, not from the response, so
					// ConsensusParamUpdates is intentionally omitted here.
				})
				continue
			}
			require.Equal(t, "eth_subscription", resObj["method"])
			paramMap := resObj["params"].(map[string]interface{})
			require.Equal(t, subscriptionId, paramMap["subscription"])
			resultMap := paramMap["result"].(map[string]interface{})
			for _, k := range expectedKeys {
				_, ok := resultMap[k]
				require.Truef(t, ok, "missing key %q in header", k)
			}
			require.Equal(t, common.BytesToHash(hash).Hex(), resultMap["hash"])
			require.Equal(t, fmt.Sprintf("0x%x", MockHeight8), resultMap["number"])
			require.Equal(t, common.BytesToAddress(proposer).Hex(), resultMap["miner"])
			require.Equal(t, common.BytesToHash(appHash).Hex(), resultMap["stateRoot"])
			require.Equal(t, fmt.Sprintf("0x%x", ts.Unix()), resultMap["timestamp"])
			require.Equal(t, fmt.Sprintf("0x%x", 21000+50000), resultMap["gasUsed"])
			// gasLimit comes from the SDK ConsensusParams that the test
			// runtime sets; just assert it's a non-zero hex string.
			gasLimitStr, _ := resultMap["gasLimit"].(string)
			require.NotEmpty(t, gasLimitStr)
			require.NotEqual(t, "0x0", gasLimitStr)
			// Fields the Autobahn path does not surface must serialize to
			// the zero hash.
			zeroHash := (common.Hash{}).Hex()
			require.Equal(t, zeroHash, resultMap["parentHash"])
			require.Equal(t, zeroHash, resultMap["receiptsRoot"])
			require.Equal(t, zeroHash, resultMap["transactionsRoot"])
			return
		case <-timer.C:
			t.Fatal("No event received within 5 seconds")
		}
	}
}

func TestSubscribeEmptyLogs(t *testing.T) {
	t.Parallel()
	recvCh, done := sendWSRequestGood(t, "subscribe", "logs")
	defer func() { done <- struct{}{} }()

	timer := time.NewTimer(2 * time.Second)

	// just testing to see that we don't crash when no params are provided
	for {
		select {
		case <-recvCh:
			return
		case <-timer.C:
			t.Fatal("No message received within 5 seconds")
		}
	}
}

func TestSubscribeNewLogs(t *testing.T) {
	t.Skip()
	data := map[string]interface{}{
		"fromBlock": "0x0",
		"toBlock":   "latest",
		"address": []common.Address{
			common.HexToAddress("0x1111111111111111111111111111111111111111"),
		},
		"topics": [][]common.Hash{
			{
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111112"),
			},
		},
	}
	recvCh, done := sendWSRequestGood(t, "subscribe", "logs", data)
	defer func() { done <- struct{}{} }()

	receivedSubMsg := false
	receivedEvents := false
	timer := time.NewTimer(2 * time.Second)

	var subscriptionId string

	for {
		select {
		case resObj := <-recvCh:
			_, ok := resObj["error"]
			if ok {
				t.Fatal("Received error:", resObj["error"])
			}
			if !receivedSubMsg {
				// get subscriptionId from first message
				subscriptionId = resObj["result"].(string)
				receivedSubMsg = true
				continue
			}
			receivedEvents = true
			method := resObj["method"].(string)
			if method != "eth_subscription" {
				t.Fatal("Method is not eth_subscription")
			}
			paramMap := resObj["params"].(map[string]interface{})
			if paramMap["subscription"] != subscriptionId {
				t.Fatal("Subscription ID does not match")
			}
			resultMap := paramMap["result"].(map[string]interface{})
			if resultMap["address"] != "0x1111111111111111111111111111111111111111" && resultMap["address"] != "0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48" {
				t.Fatalf("Unexpected address, got %v", resultMap["address"])
			}
			firstTopic := resultMap["topics"].([]interface{})[0].(string)
			if firstTopic != "0x1111111111111111111111111111111111111111111111111111111111111111" {
				t.Fatalf("Unexpected topic, got %v", firstTopic)
			}
		case <-timer.C:
			if !receivedSubMsg || !receivedEvents {
				t.Fatal("No message received within 5 seconds")
			}
			return
		}
	}
}

func TestSubscriptionManager(t *testing.T) {
	manager := evmrpc.NewSubscriptionManager(&MockClient{})
	res, subCh, err := manager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 1, int(res))

	res, subCh, err = manager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.Nil(t, err)
	require.NotNil(t, subCh)
	require.Equal(t, 2, int(res))

	badManager := evmrpc.NewSubscriptionManager(&MockBadClient{})
	_, subCh, err = badManager.Subscribe(context.Background(), evmrpc.NewHeadQueryBuilder(), 10)
	require.NotNil(t, err)
	require.Nil(t, subCh)
}

// pendingSubscribeTwoLogs is contract-creation code that emits LOG1 with topic
// 1 and then LOG1 with topic 2, and deploys no code.
var pendingSubscribeTwoLogs = []byte{
	0x60, 0x01, 0x60, 0x00, 0x60, 0x00, 0xa1,
	0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xa1,
	0x00,
}

const pendingSubscribeDeployGas = 100_000

type pendingSubscribeLog struct {
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	BlockNumber hexutil.Uint64 `json:"blockNumber"`
	TxHash      common.Hash    `json:"transactionHash"`
	TxIndex     hexutil.Uint   `json:"transactionIndex"`
	Index       hexutil.Uint   `json:"logIndex"`
	Seq         hexutil.Uint64 `json:"seq"`
}

// pendingSubscribeTx signs an EVM transaction from `from` and wraps it as the
// Cosmos transaction Accept takes. A nil `to` creates a contract.
func pendingSubscribeTx(t *testing.T, from utils.TestAcct, nonce uint64, to *common.Address, value *big.Int, gas uint64, data []byte) ([]byte, common.Hash) {
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

// pendingSubscribeAccept accepts txBytes on the open block and publishes the
// receipted transaction with its execution logs, as the sequencer's Accept
// path does.
func pendingSubscribeAccept(t *testing.T, s *pendingSequencer, stream *evmrpc.ReceiptStream, txBytes []byte) *sequencer.Accepted {
	t.Helper()
	acc, err := s.exec.Accept(txBytes)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, acc.Result.Code)
	snap, err := s.exec.Snapshot()
	require.NoError(t, err)
	rcpt, err := s.app.EvmKeeper.GetTransientReceipt(snap, common.Hash(acc.ActivityID), uint64(acc.TxIndex)) //nolint:gosec // TxIndex is positive
	require.NoError(t, err)
	require.NoError(t, stream.Publish(evmrpc.SubscriptionReceipt{
		Seq:     acc.Seq,
		Height:  acc.Height,
		TxIndex: uint32(acc.TxIndex), //nolint:gosec // TxIndex is positive
		Hash:    acc.ActivityID,
		Logs:    rcpt.Logs,
	}))
	return acc
}

// pendingSubscribeClient serves a receipt-fed SubscriptionAPI for s in process.
func pendingSubscribeClient(t *testing.T, s *pendingSequencer, stream *evmrpc.ReceiptStream) *gethrpc.Client {
	t.Helper()
	api, err := evmrpc.NewSubscriptionAPI(nil, &s.app.EvmKeeper, s.ctxProvider, &evmrpc.LogFetcher{}, &evmrpc.SubscriptionConfig{}, &evmrpc.FilterConfig{}, evmrpc.ConnectionTypeWS, evmrpc.NewBlockHeaderNotifier(1), stream)
	require.NoError(t, err)
	server := gethrpc.NewServer()
	require.NoError(t, server.RegisterName("eth", api))
	client := gethrpc.DialInProc(server)
	t.Cleanup(func() {
		client.Close()
		server.Stop()
	})
	return client
}

func pendingSubscribeNext[T any](t *testing.T, ch chan T, sub *gethrpc.ClientSubscription) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case err := <-sub.Err():
		require.FailNow(t, "subscription ended", "%v", err)
	case <-time.After(10 * time.Second):
		require.FailNow(t, "no notification within 10 seconds")
	}
	var zero T
	return zero
}

func pendingSubscribeQuiet[T any](t *testing.T, ch chan T, wait time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		require.FailNow(t, "unexpected notification", "%v", v)
	case <-time.After(wait):
	}
}

func pendingSubscribeLogOf(t *testing.T, raw json.RawMessage) pendingSubscribeLog {
	t.Helper()
	var l pendingSubscribeLog
	require.NoError(t, json.Unmarshal(raw, &l))
	return l
}

func TestSubscribeLogsFromReceipts(t *testing.T) {
	sender := utils.NewSigner()
	s := newPendingSequencer(t, 1, sender)
	stream := evmrpc.NewReceiptStream()
	client := pendingSubscribeClient(t, s, stream)
	ctx := context.Background()
	contractA := crypto.CreateAddress(sender.EvmAddress, 0)
	contractB := crypto.CreateAddress(sender.EvmAddress, 1)

	all := make(chan json.RawMessage, 8)
	allSub, err := client.EthSubscribe(ctx, all, "logs", map[string]interface{}{})
	require.NoError(t, err)
	defer allSub.Unsubscribe()
	onlyB := make(chan json.RawMessage, 8)
	bSub, err := client.EthSubscribe(ctx, onlyB, "logs", map[string]interface{}{"address": contractB})
	require.NoError(t, err)
	defer bSub.Unsubscribe()

	txA, hashA := pendingSubscribeTx(t, sender, 0, nil, big.NewInt(0), pendingSubscribeDeployGas, pendingSubscribeTwoLogs)
	first := pendingSubscribeAccept(t, s, stream, txA)
	txB, hashB := pendingSubscribeTx(t, sender, 1, nil, big.NewInt(0), pendingSubscribeDeployGas, pendingSubscribeTwoLogs)
	second := pendingSubscribeAccept(t, s, stream, txB)
	require.Equal(t, uint64(1), first.Seq)
	require.Equal(t, uint64(2), second.Seq)
	require.Equal(t, hashA, common.Hash(first.ActivityID))
	require.Equal(t, hashB, common.Hash(second.ActivityID))

	topics := []common.Hash{common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(2))}
	want := []pendingSubscribeLog{
		{Address: contractA, Topics: topics[:1], BlockNumber: 2, TxHash: hashA, TxIndex: hexutil.Uint(first.TxIndex), Index: 0, Seq: 1},  //nolint:gosec // TxIndex is positive
		{Address: contractA, Topics: topics[1:], BlockNumber: 2, TxHash: hashA, TxIndex: hexutil.Uint(first.TxIndex), Index: 1, Seq: 1},  //nolint:gosec // TxIndex is positive
		{Address: contractB, Topics: topics[:1], BlockNumber: 2, TxHash: hashB, TxIndex: hexutil.Uint(second.TxIndex), Index: 2, Seq: 2}, //nolint:gosec // TxIndex is positive
		{Address: contractB, Topics: topics[1:], BlockNumber: 2, TxHash: hashB, TxIndex: hexutil.Uint(second.TxIndex), Index: 3, Seq: 2}, //nolint:gosec // TxIndex is positive
	}
	for _, w := range want {
		require.Equal(t, w, pendingSubscribeLogOf(t, pendingSubscribeNext(t, all, allSub)))
	}
	for _, w := range want[2:] {
		require.Equal(t, w, pendingSubscribeLogOf(t, pendingSubscribeNext(t, onlyB, bSub)))
	}

	// The block leaves the open layer for commit; its logs are not notified
	// again, neither from a replayed receipt nor from the committed-log poll.
	_, err = s.exec.Seal(2)
	require.NoError(t, err)
	require.ErrorIs(t, stream.Publish(evmrpc.SubscriptionReceipt{Seq: first.Seq, Height: first.Height, Hash: hashA}), evmrpc.ErrReceiptOrder)
	pendingSubscribeQuiet(t, all, evmrpc.SleepInterval+time.Second)
	require.Empty(t, onlyB)
}

func TestSubscribeNewPendingTransactions(t *testing.T) {
	sender := utils.NewSigner()
	recipient := utils.NewSigner().EvmAddress
	s := newPendingSequencer(t, 1, sender)
	stream := evmrpc.NewReceiptStream()
	client := pendingSubscribeClient(t, s, stream)

	hashes := make(chan common.Hash, 4)
	sub, err := client.EthSubscribe(context.Background(), hashes, "newPendingTransactions")
	require.NoError(t, err)
	defer sub.Unsubscribe()

	value := big.NewInt(pendingOnePaxWei)
	tx0, hash0 := pendingSubscribeTx(t, sender, 0, &recipient, value, pendingTransferGas, nil)
	tx1, hash1 := pendingSubscribeTx(t, sender, 1, &recipient, value, pendingTransferGas, nil)
	first := pendingSubscribeAccept(t, s, stream, tx0)
	second := pendingSubscribeAccept(t, s, stream, tx1)
	require.Equal(t, uint64(1), first.Seq)
	require.Equal(t, uint64(2), second.Seq)
	require.Equal(t, hash0, pendingSubscribeNext(t, hashes, sub))
	require.Equal(t, hash1, pendingSubscribeNext(t, hashes, sub))

	// A receipt published out of order is refused and notifies nothing.
	require.ErrorIs(t, stream.Publish(evmrpc.SubscriptionReceipt{Seq: second.Seq, Height: second.Height, Hash: hash1}), evmrpc.ErrReceiptOrder)
	require.ErrorIs(t, stream.Publish(evmrpc.SubscriptionReceipt{Seq: second.Seq + 1, Height: second.Height - 1, Hash: hash1}), evmrpc.ErrReceiptOrder)
	pendingSubscribeQuiet(t, hashes, 500*time.Millisecond)

	// A node without the receipt stream refuses the subscription.
	recvCh, done := sendWSRequestGood(t, "subscribe", "newPendingTransactions")
	defer func() { done <- struct{}{} }()
	select {
	case resObj := <-recvCh:
		errObj, ok := resObj["error"].(map[string]interface{})
		require.True(t, ok, "want an error, got %v", resObj)
		require.Equal(t, float64(-32000), errObj["code"])
		require.Equal(t, "pending served by sequencer", errObj["message"])
	case <-time.After(5 * time.Second):
		require.FailNow(t, "no reply within 5 seconds")
	}
}
