package evmrpc_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"

	abci "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/abci/types"
	tmed25519 "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/ed25519"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/crypto/merkle"
	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/privval"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/sequencer"
	occutils "github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	evmrpcconfig "github.com/Sidiora-Labs/Paxeer-X-Network/rpc/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/gorilla/websocket"
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
	parentHash := common.HexToHash("0x5252525252525252525252525252525252525252525252525252525252525252").Bytes()
	dataHash := common.HexToHash("0x6363636363636363636363636363636363636363636363636363636363636363").Bytes()
	resultsHash := common.HexToHash("0x7474747474747474747474747474747474747474747474747474747474747474").Bytes()
	proposer := common.HexToAddress("0x9999999999999999999999999999999999999999").Bytes()
	ts := time.Unix(1_700_000_500, 0).UTC()
	// The served receiptsRoot is the results root of block H, the value
	// header H+1 commits as LastResultsHash, not header H's own field.
	blockHeight := int64(MockHeight8)
	blockResults, err := (&MockClient{}).BlockResults(context.Background(), &blockHeight)
	require.NoError(t, err)
	resultLeaves, err := abci.MarshalTxResults(blockResults.TxsResults)
	require.NoError(t, err)
	sameBlockRoot := common.BytesToHash(merkle.HashFromByteSlices(resultLeaves))
	require.NotEqual(t, common.BytesToHash(resultsHash), sameBlockRoot)

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
					LastBlockId:     tmproto.BlockID{Hash: parentHash},
					DataHash:        dataHash,
					LastResultsHash: resultsHash,
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
			require.Equal(t, common.BytesToHash(parentHash).Hex(), resultMap["parentHash"])
			require.Equal(t, sameBlockRoot.Hex(), resultMap["receiptsRoot"])
			require.Equal(t, common.BytesToHash(dataHash).Hex(), resultMap["transactionsRoot"])
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

// subscribeTwoLogsInitCode is contract-creation code that emits LOG1 with topic 1, then LOG1 with
// topic 2, and deploys no code: PUSH1 1, PUSH1 0, PUSH1 0, LOG1, PUSH1 2, PUSH1 0, PUSH1 0, LOG1,
// STOP.
var subscribeTwoLogsInitCode = []byte{
	0x60, 0x01, 0x60, 0x00, 0x60, 0x00, 0xa1,
	0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xa1,
	0x00,
}

// subscribeManyLogsInitCode is contract-creation code that emits n LOG1 with topics n, n-1, ..., 1
// and deploys no code: PUSH2 n; loop: JUMPDEST, DUP1, PUSH1 0, PUSH1 0, LOG1, PUSH1 1, SWAP1, SUB,
// DUP1, PUSH1 loop, JUMPI; STOP.
func subscribeManyLogsInitCode(n uint16) []byte {
	return []byte{
		0x61, byte(n >> 8), byte(n),
		0x5b, 0x80, 0x60, 0x00, 0x60, 0x00, 0xa1,
		0x60, 0x01, 0x90, 0x03,
		0x80, 0x60, 0x03, 0x57,
		0x00,
	}
}

const (
	subscribeTwoLogsGas  = 100_000
	subscribeManyLogsGas = 2_000_000
)

// subscribeRawTx signs an EVM transaction from `from` and returns its raw encoding for
// eth_sendRawTransaction. A nil `to` creates a contract.
func subscribeRawTx(t *testing.T, from occutils.TestAcct, nonce uint64, to *common.Address, value *big.Int, gas uint64, data []byte) (hexutil.Bytes, common.Hash) {
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
	raw, err := signed.MarshalBinary()
	require.NoError(t, err)
	return raw, signed.Hash()
}

// subscribeSequencer is the open block of s served by a real Receipter through the HTTP and
// WebSocket servers of one SequencerOption.
type subscribeSequencer struct {
	*pendingSequencer
	feed     *evmrpc.ReceiptLogFeed
	accepter *sendSeqReceipter
	option   evmrpc.SequencerOption
	httpPort int
}

// newSubscribeSequencer opens height 2 over committed height 1 with sender funded, signs receipts
// with a receipt key pinned for heights 1..1000 and starts the sequencer's HTTP server.
func newSubscribeSequencer(t *testing.T, sender occutils.TestAcct) *subscribeSequencer {
	t.Helper()
	s := newPendingSequencer(t, 1, sender)
	dir := t.TempDir()
	consensusPriv, err := tmed25519.SecretKeyFromSecretBytes(ed25519.NewKeyFromSeed(sendSeqLabel("subscribe consensus key")))
	require.NoError(t, err)
	consensusFile := filepath.Join(dir, "priv_validator_key.json")
	require.NoError(t, privval.NewFilePV(consensusPriv, consensusFile, filepath.Join(dir, "priv_validator_state.json")).Key.Save())
	seed := sendSeqLabel("subscribe receipt key")
	keyFile := filepath.Join(dir, "receipt_key")
	require.NoError(t, os.WriteFile(keyFile, []byte(hex.EncodeToString(seed)+"\n"), 0o600))
	key, err := sequencer.LoadReceiptKey(keyFile, consensusFile, []verify.SequencerAuthorization{{
		SequencerID:      [32]byte(sendSeqLabel("subscribe key id")),
		PublicKey:        [32]byte(ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)),
		FirstBatchNumber: 1,
		LastBatchNumber:  1000,
	}}, s.committed+1)
	require.NoError(t, err)
	r, err := sequencer.NewReceipter(s.exec, s.app, &s.app.EvmKeeper, key, sendSeqModuleID)
	require.NoError(t, err)

	q := &subscribeSequencer{
		pendingSequencer: s,
		feed:             evmrpc.NewReceiptLogFeed(s.committed),
		accepter:         &sendSeqReceipter{r: r},
	}
	q.option = evmrpc.NewSequencerOption(q.accepter, q.feed)
	cfg := evmrpcconfig.DefaultConfig
	cfg.HTTPAddress = TestAddr
	cfg.HTTPPort = seqReceiptFreePort9005(t)
	server, err := evmrpc.NewEVMHTTPServer(cfg, &MockClient{latestOverride: s.committed}, &s.app.EvmKeeper, s.app.BeginBlockKeepers, s.app.BaseApp,
		s.app.TracerAnteHandler, s.ctxProvider, q.txConfig, "", nil, q.option)
	require.NoError(t, err)
	require.NoError(t, server.Start())
	t.Cleanup(server.Stop)
	q.httpPort = cfg.HTTPPort
	return q
}

func (q *subscribeSequencer) txConfig(int64) client.TxConfig { return q.app.GetTxConfig() }

// wsServer builds the sequencer's WebSocket server on wsPort with the same SequencerOption.
func (q *subscribeSequencer) wsServer(t *testing.T, wsPort int) evmrpc.EVMServer {
	t.Helper()
	cfg := evmrpcconfig.DefaultConfig
	cfg.WSPort = wsPort
	server, err := evmrpc.NewEVMWebSocketServer(cfg, &MockClient{latestOverride: q.committed}, &q.app.EvmKeeper, q.app.BeginBlockKeepers, q.app.BaseApp,
		q.app.TracerAnteHandler, q.ctxProvider, q.txConfig, "", nil, nil, q.option)
	require.NoError(t, err)
	return server
}

// send submits raw through eth_sendRawTransaction of the HTTP server and returns its Accept
// outcome.
func (q *subscribeSequencer) send(t *testing.T, raw hexutil.Bytes, hash common.Hash) *sequencer.Accepted {
	t.Helper()
	res, rpcErr := callSeqReceipt9005(t, q.httpPort, "eth_sendRawTransaction", raw)
	require.Nil(t, rpcErr)
	var got common.Hash
	require.NoError(t, json.Unmarshal(res, &got))
	require.Equal(t, hash, got)
	acc := q.accepter.accepted[len(q.accepter.accepted)-1]
	require.Equal(t, [32]byte(hash), acc.ActivityID)
	return acc
}

// subscribePipeListener hands the HTTP server the server ends of in-memory net.Pipe connections, on
// which a write blocks until the client reads it.
type subscribePipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

type subscribePipeAddr struct{}

func (subscribePipeAddr) Network() string { return "pipe" }
func (subscribePipeAddr) String() string  { return "pipe" }

func (l *subscribePipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *subscribePipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *subscribePipeListener) Addr() net.Addr { return subscribePipeAddr{} }

// subscribePeekConn is the client end of a pipe that can take one byte of a pending server write
// ahead of the WebSocket reader and hand it back on the next Read.
type subscribePeekConn struct {
	net.Conn
	peeked []byte
}

func (c *subscribePeekConn) Read(p []byte) (int, error) {
	if len(c.peeked) > 0 {
		n := copy(p, c.peeked)
		c.peeked = c.peeked[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// writing reports whether the server writes to the connection within wait. A byte it reads is kept
// for the WebSocket reader.
func (c *subscribePeekConn) writing(t *testing.T, wait time.Duration) bool {
	t.Helper()
	require.Empty(t, c.peeked)
	require.NoError(t, c.Conn.SetReadDeadline(time.Now().Add(wait)))
	defer func() { require.NoError(t, c.Conn.SetReadDeadline(time.Time{})) }()
	b := make([]byte, 1)
	n, err := c.Conn.Read(b)
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return false
	}
	require.NoError(t, err)
	c.peeked = append(c.peeked, b[:n]...)
	return n == 1
}

// dialWSPipe serves handler over an in-memory pipe and opens a WebSocket client on it.
func dialWSPipe(t *testing.T, handler http.Handler) (*websocket.Conn, *subscribePeekConn) {
	t.Helper()
	l := &subscribePipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close() })
	peek := &subscribePeekConn{}
	dialer := websocket.Dialer{
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			serverEnd, clientEnd := net.Pipe()
			select {
			case l.conns <- serverEnd:
				peek.Conn = clientEnd
				return peek, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		HandshakeTimeout: 10 * time.Second,
	}
	conn, res, err := dialer.Dial("ws://pipe/", http.Header{"Origin": []string{"localhost"}})
	require.NoError(t, err)
	require.NoError(t, res.Body.Close())
	t.Cleanup(func() { _ = conn.Close() })
	return conn, peek
}

// wsSubscribe sends eth_subscribe with params and returns the subscription id.
func wsSubscribe(t *testing.T, conn *websocket.Conn, params ...any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "eth_subscribe", "params": params})
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, body))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	var answer struct {
		Result string          `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	require.NoError(t, json.Unmarshal(msg, &answer), string(msg))
	require.Empty(t, answer.Error, string(msg))
	require.NotEmpty(t, answer.Result, string(msg))
	return answer.Result
}

// wsNotification reads the next notification of subscription id and returns its result.
func wsNotification(t *testing.T, conn *websocket.Conn, id string) map[string]any {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	var notification struct {
		Method string `json:"method"`
		Params struct {
			Subscription string `json:"subscription"`
			Result       any    `json:"result"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(msg, &notification), string(msg))
	require.Equal(t, "eth_subscription", notification.Method, string(msg))
	require.Equal(t, id, notification.Params.Subscription)
	result, ok := notification.Params.Result.(map[string]any)
	require.True(t, ok, "notification result is %T: %s", notification.Params.Result, msg)
	return result
}

// wsHashNotification reads the next notification of subscription id and returns its hash result.
func wsHashNotification(t *testing.T, conn *websocket.Conn, id string) string {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	var notification struct {
		Method string `json:"method"`
		Params struct {
			Subscription string `json:"subscription"`
			Result       string `json:"result"`
		} `json:"params"`
	}
	require.NoError(t, json.Unmarshal(msg, &notification), string(msg))
	require.Equal(t, "eth_subscription", notification.Method, string(msg))
	require.Equal(t, id, notification.Params.Subscription)
	return notification.Params.Result
}

// receiptedLogJSON is a receipted log as a logs subscriber receives it: the log fields, its seq and
// no block hash.
func receiptedLogJSON(addr common.Address, topic uint64, height int64, hash common.Hash, txIndex int, index uint, seq uint64) map[string]any {
	return map[string]any{
		"address":          hexutil.Encode(addr.Bytes()),
		"topics":           []any{common.BigToHash(new(big.Int).SetUint64(topic)).Hex()},
		"data":             "0x",
		"blockNumber":      hexutil.EncodeUint64(uint64(height)), //nolint:gosec // test heights are positive
		"transactionHash":  hash.Hex(),
		"transactionIndex": hexutil.EncodeUint64(uint64(txIndex)), //nolint:gosec // tx index is positive
		"blockHash":        nil,
		"logIndex":         hexutil.EncodeUint64(uint64(index)),
		"removed":          false,
		"seq":              hexutil.EncodeUint64(seq),
	}
}

func TestSubscribeLogsFromReceipts(t *testing.T) {
	sender := occutils.NewSigner()
	q := newSubscribeSequencer(t, sender)
	ws, peek := dialWSPipe(t, q.wsServer(t, 0).(http.Handler))
	id := wsSubscribe(t, ws, "logs", map[string]any{})
	open := q.committed + 1

	// A transaction emitting two logs reaches the subscriber once, at receipt, with its seq.
	rawA, hashA := subscribeRawTx(t, sender, 0, nil, big.NewInt(0), subscribeTwoLogsGas, subscribeTwoLogsInitCode)
	accA := q.send(t, rawA, hashA)
	require.Equal(t, uint64(1), accA.Seq)
	require.Equal(t, open, accA.Height)
	addrA := crypto.CreateAddress(sender.EvmAddress, 0)
	require.Equal(t, receiptedLogJSON(addrA, 1, open, hashA, accA.TxIndex, 0, 1), wsNotification(t, ws, id))
	require.Equal(t, receiptedLogJSON(addrA, 2, open, hashA, accA.TxIndex, 1, 1), wsNotification(t, ws, id))
	require.False(t, peek.writing(t, 500*time.Millisecond))

	// The subscriber stalls: the server is held writing the first log of B, B's second log waits in
	// the buffer, and the logs of C fill the rest of it; the logs of C past the buffer are dropped.
	rawB, hashB := subscribeRawTx(t, sender, 1, nil, big.NewInt(0), subscribeTwoLogsGas, subscribeTwoLogsInitCode)
	accB := q.send(t, rawB, hashB)
	require.True(t, peek.writing(t, 10*time.Second))
	const manyLogs = 1100
	const buffered = 1024 - 1
	rawC, hashC := subscribeRawTx(t, sender, 2, nil, big.NewInt(0), subscribeManyLogsGas, subscribeManyLogsInitCode(manyLogs))
	accC := q.send(t, rawC, hashC)
	require.Equal(t, uint64(2), accB.Seq)
	require.Equal(t, uint64(3), accC.Seq)

	addrB := crypto.CreateAddress(sender.EvmAddress, 1)
	addrC := crypto.CreateAddress(sender.EvmAddress, 2)
	require.Equal(t, receiptedLogJSON(addrB, 1, open, hashB, accB.TxIndex, 2, 2), wsNotification(t, ws, id))
	require.Equal(t, receiptedLogJSON(addrB, 2, open, hashB, accB.TxIndex, 3, 2), wsNotification(t, ws, id))
	for i := 0; i < buffered; i++ {
		require.Equal(t, receiptedLogJSON(addrC, uint64(manyLogs-i), open, hashC, accC.TxIndex, uint(4+i), 3), wsNotification(t, ws, id)) //nolint:gosec // i is below manyLogs
	}
	require.False(t, peek.writing(t, 500*time.Millisecond))

	// Ahead of the next log the subscriber receives exactly one lag notification with the number of
	// logs dropped, then later logs in order.
	rawD, hashD := subscribeRawTx(t, sender, 3, nil, big.NewInt(0), subscribeTwoLogsGas, subscribeTwoLogsInitCode)
	accD := q.send(t, rawD, hashD)
	require.Equal(t, uint64(4), accD.Seq)
	addrD := crypto.CreateAddress(sender.EvmAddress, 3)
	require.Equal(t, map[string]any{"lagged": hexutil.EncodeUint64(manyLogs - buffered)}, wsNotification(t, ws, id))
	require.Equal(t, receiptedLogJSON(addrD, 1, open, hashD, accD.TxIndex, 4+manyLogs, 4), wsNotification(t, ws, id))
	require.Equal(t, receiptedLogJSON(addrD, 2, open, hashD, accD.TxIndex, 5+manyLogs, 4), wsNotification(t, ws, id))

	// The block commits: neither the commit nor the committed copy of its logs sends anything more.
	sealed, err := q.exec.Seal(open)
	require.NoError(t, err)
	require.Len(t, sealed.Txs, 4)
	require.NoError(t, q.feed.Committed(open))
	require.False(t, peek.writing(t, evmrpc.SleepInterval+time.Second))
}

func TestSubscribeNewPendingTransactions(t *testing.T) {
	sender := occutils.NewSigner()
	recipient := occutils.NewSigner().EvmAddress
	q := newSubscribeSequencer(t, sender)
	wsPort := seqReceiptFreePort9005(t)
	server := q.wsServer(t, wsPort)
	require.NoError(t, server.Start())
	t.Cleanup(server.Stop)

	ws, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s:%d", TestAddr, wsPort), http.Header{"Origin": []string{"localhost"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close() })
	id := wsSubscribe(t, ws, "newPendingTransactions")

	// Two transactions sent through the HTTP server reach the WebSocket subscriber in seq order.
	value := big.NewInt(pendingOnePaxWei)
	raw0, hash0 := subscribeRawTx(t, sender, 0, &recipient, value, pendingTransferGas, nil)
	raw1, hash1 := subscribeRawTx(t, sender, 1, &recipient, value, pendingTransferGas, nil)
	acc0 := q.send(t, raw0, hash0)
	acc1 := q.send(t, raw1, hash1)
	require.Equal(t, uint64(1), acc0.Seq)
	require.Equal(t, uint64(2), acc1.Seq)
	require.Equal(t, hash0.Hex(), wsHashNotification(t, ws, id))
	require.Equal(t, hash1.Hex(), wsHashNotification(t, ws, id))

	// A node without the sequencer option refuses the subscription.
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
