package evmrpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	tmproto "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/proto/tendermint/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/verify"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	seqkeeper "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/keeper"
	seqtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/sequencer/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/legacyabci"
	occutils "github.com/Sidiora-Labs/Paxeer-X-Network/occ_tests/utils"
	evmrpc "github.com/Sidiora-Labs/Paxeer-X-Network/rpc"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"
)

const resolutionOnePaxWei1315 = 1_000_000_000_000_000_000

// resolutionCtxProvider1315 serves the open block of h for the pending height and the committed
// height-1 state for every other height, as the node's RPC context provider does on the sequencer.
func resolutionCtxProvider1315(h *sendSeqHarness) func(int64) sdk.Context {
	return func(height int64) sdk.Context {
		if height == evmrpc.PendingCtxHeight {
			ctx, err := h.exec.Snapshot()
			if err != nil {
				panic(err)
			}
			return ctx
		}
		return sdk.NewContext(h.app.CommitMultiStore().CacheMultiStore(), tmproto.Header{ChainID: sendSeqChainID, Height: sendSeqCommittedTip}, false).
			WithChainID(sendSeqChainID).WithBlockHeight(sendSeqCommittedTip)
	}
}

// resolutionSendAPI1315 serves eth_sendRawTransaction over the harness App with cfg.
func resolutionSendAPI1315(h *sendSeqHarness, cfg *evmrpc.SendConfig) *evmrpc.SendAPI {
	return evmrpc.NewSendAPI(
		h.client,
		func(int64) client.TxConfig { return h.app.GetTxConfig() },
		cfg,
		&h.app.EvmKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) sdk.Context { return h.app.GetContextForDeliverTx([]byte{}) },
		"",
		nil,
		nil,
		nil,
		evmrpc.ConnectionTypeHTTP,
		utils.None[time.Duration](),
		evmrpc.NewBlockCache(1),
		nil,
		nil,
	)
}

// resolutionInProc1315 serves the given eth services on an in-process JSON-RPC server.
func resolutionInProc1315(t *testing.T, services ...any) *rpc.Client {
	t.Helper()
	srv := rpc.NewServer()
	for _, service := range services {
		require.NoError(t, srv.RegisterName("eth", service))
	}
	c := rpc.DialInProc(srv)
	t.Cleanup(func() {
		c.Close()
		srv.Stop()
	})
	return c
}

func requireRPCError1315(t *testing.T, err error, code int, message string) {
	t.Helper()
	var rpcErr rpc.Error
	require.True(t, errors.As(err, &rpcErr), "error %v carries no JSON-RPC code", err)
	require.Equal(t, code, rpcErr.ErrorCode())
	require.Equal(t, message, rpcErr.Error())
}

// resolutionSends1315 is the sequencer's send record built from the Receipter's accepted
// transactions and the execution outputs their receipts bind.
type resolutionSends1315 struct {
	sends map[common.Hash]*evmrpc.SequencerSend
}

func (r *resolutionSends1315) SequencerSend(txHash common.Hash) (*evmrpc.SequencerSend, error) {
	return r.sends[txHash], nil
}

// record stores the send of etx, receipted as the i-th receipt of h.
func (r *resolutionSends1315) record(t *testing.T, h *sendSeqHarness, i int, etx *ethtypes.Transaction) {
	t.Helper()
	acc := h.accepter.accepted[i]
	var msgData sdk.TxMsgData
	require.NoError(t, msgData.Unmarshal(acc.Result.Data))
	require.Len(t, msgData.Data, 1)
	var resp evmtypes.MsgEVMTransactionResponse
	require.NoError(t, resp.Unmarshal(msgData.Data[0].Data))
	snapshot, err := h.exec.Snapshot()
	require.NoError(t, err)
	rcpt, err := h.app.EvmKeeper.GetTransientReceipt(snapshot, etx.Hash(), uint64(acc.TxIndex))
	require.NoError(t, err)
	logData := make([][]byte, len(rcpt.Logs))
	for j, log := range rcpt.Logs {
		logData[j] = log.Data
	}
	r.sends[etx.Hash()] = &evmrpc.SequencerSend{
		Nonce:      etx.Nonce(),
		Receipt:    h.accepter.receipts[i],
		ReturnData: resp.ReturnData,
		LogData:    logData,
		VMError:    rcpt.VmError,
	}
}

// startResolutionReceiptServer1315 serves pax_sequencerReceipt over HTTP from the harness record.
func startResolutionReceiptServer1315(t *testing.T, h *sendSeqHarness, sends *resolutionSends1315) int {
	t.Helper()
	keeper := seqkeeper.NewKeeper(sdk.NewKVStoreKey(seqtypes.StoreKey))
	return startSeqReceiptServer9005(t, h.app, resolutionCtxProvider1315(h), evmrpc.SequencerReceiptOption{Source: sends, Keeper: &keeper})
}

func TestResolutionPendingTag(t *testing.T) {
	sender := occutils.NewSigner()
	h := newSendSeqHarness(t, sender)
	to := common.BytesToAddress(sendSeqLabel("resolution pending recipient")[:20])
	_, raw := sendSeqTransfer(t, sender, 0, to, big.NewInt(resolutionOnePaxWei1315), sendSeqTransferGas)
	_, err := h.api.SendRawTransaction(context.Background(), raw)
	require.NoError(t, err)

	ctxProvider := resolutionCtxProvider1315(h)
	fullNode := resolutionInProc1315(t, evmrpc.NewStateAPI(&MockClient{}, &h.app.EvmKeeper, ctxProvider, evmrpc.ConnectionTypeHTTP,
		evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, nil)))
	var balance hexutil.Big
	err = fullNode.Call(&balance, "eth_getBalance", to, "pending")
	requireRPCError1315(t, err, -32000, "pending served by sequencer")
	require.Contains(t, err.Error(), "sequencer")

	_, err = evmrpc.GetBlockNumberByNrOrHash(context.Background(), &MockClient{}, evmrpc.NewWatermarkManager(&MockClient{}, ctxProvider, nil, nil),
		rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber))
	require.ErrorIs(t, err, evmrpc.ErrPendingServedBySequencer)

	seqWatermarks := evmrpc.NewWatermarkManager(&MockClient{latestOverride: sendSeqCommittedTip}, ctxProvider, nil, nil)
	seqWatermarks.SetPendingSnapshot(h.exec)
	seqNode := resolutionInProc1315(t, evmrpc.NewStateAPI(&MockClient{}, &h.app.EvmKeeper, ctxProvider, evmrpc.ConnectionTypeHTTP, seqWatermarks))
	require.NoError(t, seqNode.Call(&balance, "eth_getBalance", to, "pending"))
	require.Zero(t, big.NewInt(resolutionOnePaxWei1315).Cmp(balance.ToInt()))
}

func TestResolutionSendReturnsHash(t *testing.T) {
	sender := occutils.NewSigner()
	h := newSendSeqHarness(t, sender)
	to := common.BytesToAddress(sendSeqLabel("resolution send recipient")[:20])
	etx, raw := sendSeqTransfer(t, sender, 0, to, big.NewInt(resolutionOnePaxWei1315), sendSeqTransferGas)

	c := resolutionInProc1315(t, h.api)
	var hash common.Hash
	require.NoError(t, c.Call(&hash, "eth_sendRawTransaction", raw))
	require.Equal(t, etx.Hash(), hash)
	require.Len(t, h.accepter.receipts, 1)

	sends := &resolutionSends1315{sends: map[common.Hash]*evmrpc.SequencerSend{}}
	sends.record(t, h, 0, etx)
	port := startResolutionReceiptServer1315(t, h, sends)
	_, result, _ := seqReceiptResult9005(t, port, hash)
	require.NotNil(t, result)
	require.Equal(t, evmrpc.RungReceipted, result.Rung)
	require.Equal(t, hash, result.TxHash)
	require.NoError(t, result.Verify())
	verified, err := verify.ReceiptSignature(result.Receipt, h.key.PublicKey())
	require.NoError(t, err)
	require.Equal(t, [32]byte(hash), verified.Receipt.ActivityID)
	require.Equal(t, uint64(1), verified.Receipt.GlobalSequence)
	require.Equal(t, hexutil.Uint64(1), result.Status)
	require.Equal(t, to, result.To)
	require.Equal(t, sender.EvmAddress, result.From)
}

func TestResolutionSequencerReceiptRoute(t *testing.T) {
	sender := occutils.NewSigner()
	h := newSendSeqHarness(t, sender)
	to := common.BytesToAddress(sendSeqLabel("resolution route recipient")[:20])
	etx, raw := sendSeqTransfer(t, sender, 0, to, big.NewInt(resolutionOnePaxWei1315), sendSeqTransferGas)
	hash, err := h.api.SendRawTransaction(context.Background(), raw)
	require.NoError(t, err)
	sends := &resolutionSends1315{sends: map[common.Hash]*evmrpc.SequencerSend{}}
	sends.record(t, h, 0, etx)
	port := startResolutionReceiptServer1315(t, h, sends)

	unknown := common.BytesToHash(sendSeqLabel("resolution unknown tx"))
	rawResult, rpcErr := callSeqReceipt9005(t, port, evmrpc.SequencerReceiptMethod, unknown)
	require.Nil(t, rpcErr)
	require.JSONEq(t, "null", string(rawResult))

	_, result, _ := seqReceiptResult9005(t, port, hash)
	require.NotNil(t, result)
	require.Equal(t, evmrpc.RungReceipted, result.Rung)
	require.Equal(t, hexutil.Uint64(0), result.Nonce)
	require.Equal(t, hexutil.Bytes(h.accepter.receipts[0]), result.Receipt)
	require.Equal(t, hexutil.Uint64(sendSeqOpenHeight), result.Height)
	require.Equal(t, hexutil.Uint64(1), result.Seq)
	require.Nil(t, result.BlockNumber)
	require.Nil(t, result.BlockHash)
	require.NoError(t, result.Verify())

	fullNodePort := startSeqReceiptServer9005(t, h.app, resolutionCtxProvider1315(h), evmrpc.SequencerReceiptOption{})
	_, rpcErr = callSeqReceipt9005(t, fullNodePort, evmrpc.SequencerReceiptMethod, hash)
	require.NotNil(t, rpcErr)
	require.Equal(t, -32000, rpcErr.Code)
	require.Equal(t, evmrpc.SequencerReceiptMethod+" served by sequencer", rpcErr.Message)
}

func TestResolutionPendingSubscription(t *testing.T) {
	sender := occutils.NewSigner()
	h := newSendSeqHarness(t, sender)
	to := common.BytesToAddress(sendSeqLabel("resolution subscription recipient")[:20])

	// The WebSocket and HTTP SendAPI of the sequencer share one SendConfig: a hash receipted through
	// the HTTP API reaches the subscriber of the WebSocket API.
	cfg := &evmrpc.SendConfig{Sequencer: h.accepter}
	ws := resolutionInProc1315(t, resolutionSendAPI1315(h, cfg))
	httpAPI := resolutionSendAPI1315(h, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hashes := make(chan common.Hash, 8)
	sub, err := ws.EthSubscribe(ctx, hashes, "newPendingTransactions")
	require.NoError(t, err)
	defer sub.Unsubscribe()

	sent := make([]common.Hash, 0, 4)
	for nonce := uint64(0); nonce < 3; nonce++ {
		etx, raw := sendSeqTransfer(t, sender, nonce, to, big.NewInt(resolutionOnePaxWei1315), sendSeqTransferGas)
		hash, err := httpAPI.SendRawTransaction(ctx, raw)
		require.NoError(t, err)
		require.Equal(t, etx.Hash(), hash)
		sent = append(sent, hash)
		if nonce == 1 {
			// A refused transaction is not receipted and emits nothing.
			_, err = httpAPI.SendRawTransaction(ctx, raw)
			require.Error(t, err)
		}
	}
	require.Len(t, h.accepter.receipts, 3)

	for i, want := range sent {
		select {
		case got := <-hashes:
			require.Equal(t, want, got, "hash %d", i)
			verified, err := verify.ReceiptSignature(h.accepter.receipts[i], h.key.PublicKey())
			require.NoError(t, err)
			require.Equal(t, [32]byte(got), verified.Receipt.ActivityID)
			require.Equal(t, uint64(i+1), verified.Receipt.GlobalSequence)
		case err := <-sub.Err():
			t.Fatalf("subscription ended before hash %d: %v", i, err)
		case <-ctx.Done():
			t.Fatalf("hash %d not emitted: %v", i, ctx.Err())
		}
	}
	select {
	case extra := <-hashes:
		t.Fatalf("unexpected hash %s after the three receipted ones", extra.Hex())
	case <-time.After(200 * time.Millisecond):
	}

	// A node that is not the sequencer refuses the subscription.
	fullNode := resolutionInProc1315(t, resolutionSendAPI1315(h, &evmrpc.SendConfig{}))
	_, err = fullNode.EthSubscribe(ctx, make(chan common.Hash), "newPendingTransactions")
	requireRPCError1315(t, err, -32000, "pending served by sequencer")
}

// resolutionStallConn1315 is the server end of an in-process JSON-RPC connection whose writes block
// while stalled, as they do when the client stops reading its socket.
type resolutionStallConn1315 struct {
	in      *io.PipeReader
	out     *io.PipeWriter
	mu      sync.Mutex
	gate    chan struct{}
	blocked chan struct{}
}

func (c *resolutionStallConn1315) Read(b []byte) (int, error) { return c.in.Read(b) }

func (c *resolutionStallConn1315) Write(b []byte) (int, error) {
	c.mu.Lock()
	gate := c.gate
	c.mu.Unlock()
	if gate != nil {
		select {
		case c.blocked <- struct{}{}:
		default:
		}
		<-gate
	}
	return c.out.Write(b)
}

func (c *resolutionStallConn1315) Close() error {
	c.resume()
	_ = c.in.Close()
	return c.out.Close()
}

func (c *resolutionStallConn1315) SetWriteDeadline(time.Time) error { return nil }

func (c *resolutionStallConn1315) stall() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gate == nil {
		c.gate = make(chan struct{})
	}
}

func (c *resolutionStallConn1315) resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gate != nil {
		close(c.gate)
		c.gate = nil
	}
}

// resolutionStallable1315 serves the given eth service to a client over a connection whose server
// writes can be stalled.
func resolutionStallable1315(t *testing.T, service any) (*rpc.Client, *resolutionStallConn1315) {
	t.Helper()
	srv := rpc.NewServer()
	require.NoError(t, srv.RegisterName("eth", service))
	toServer, fromClient := io.Pipe()
	toClient, fromServer := io.Pipe()
	conn := &resolutionStallConn1315{in: toServer, out: fromServer, blocked: make(chan struct{}, 1)}
	go srv.ServeCodec(rpc.NewCodec(conn), 0)
	c, err := rpc.DialIO(context.Background(), toClient, fromClient)
	require.NoError(t, err)
	t.Cleanup(func() {
		srv.Stop()
		c.Close()
		_ = toClient.Close()
		_ = fromClient.Close()
	})
	return c, conn
}

func TestResolutionPendingSubscriptionOverflow(t *testing.T) {
	sender := occutils.NewSigner()
	h := newSendSeqHarness(t, sender)
	to := common.BytesToAddress(sendSeqLabel("resolution overflow recipient")[:20])
	cfg := &evmrpc.SendConfig{Sequencer: h.accepter}
	httpAPI := resolutionSendAPI1315(h, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ws, conn := resolutionStallable1315(t, resolutionSendAPI1315(h, cfg))
	notes := make(chan json.RawMessage, 16)
	sub, err := ws.EthSubscribe(ctx, notes, "newPendingTransactions")
	require.NoError(t, err)
	defer sub.Unsubscribe()

	nonce := uint64(0)
	send := func() common.Hash {
		etx, raw := sendSeqTransfer(t, sender, nonce, to, big.NewInt(1), sendSeqTransferGas)
		nonce++
		hash, err := httpAPI.SendRawTransaction(ctx, raw)
		require.NoError(t, err)
		require.Equal(t, etx.Hash(), hash)
		return hash
	}
	next := func(what string) json.RawMessage {
		select {
		case note := <-notes:
			return note
		case err := <-sub.Err():
			t.Fatalf("subscription ended before %s: %v", what, err)
		case <-ctx.Done():
			t.Fatalf("%s not delivered: %v", what, ctx.Err())
		}
		return nil
	}
	requireHash := func(want common.Hash, what string) {
		var got common.Hash
		require.NoError(t, json.Unmarshal(next(what), &got), what)
		require.Equal(t, want, got, what)
	}

	// The first hash reaches the client, so the subscription is active.
	requireHash(send(), "the first hash")

	// The client stops reading: the forwarder blocks writing the next hash, the buffer then fills
	// and every further hash is dropped for this subscriber.
	conn.stall()
	delivered := []common.Hash{send()}
	select {
	case <-conn.blocked:
	case <-ctx.Done():
		t.Fatalf("forwarder never wrote to the stalled connection: %v", ctx.Err())
	}
	for i := 0; i < 1024; i++ {
		delivered = append(delivered, send())
	}
	const dropped = 5
	for i := 0; i < dropped; i++ {
		send()
	}

	conn.resume()
	for i, want := range delivered {
		requireHash(want, "buffered hash "+hexutil.EncodeUint64(uint64(i)))
	}

	// Delivery continues: one lag notification with the exact dropped count, then every later hash
	// in receipt order.
	later := []common.Hash{send(), send(), send()}
	require.JSONEq(t, `{"lagged":"0x5"}`, string(next("the lag notification")))
	for i, want := range later {
		requireHash(want, "later hash "+hexutil.EncodeUint64(uint64(i)))
	}
	select {
	case extra := <-notes:
		t.Fatalf("unexpected notification %s after the later hashes", extra)
	case err := <-sub.Err():
		t.Fatalf("subscription ended after the later hashes: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	require.Len(t, h.accepter.receipts, 1+len(delivered)+dropped+len(later))

	// The feed still holds the listener: a hash receipted after the lag reaches the subscriber.
	requireHash(send(), "the hash after the lag")
}
