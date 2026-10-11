package evmrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"sync"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/libs/utils"
	"github.com/Sidiora-Labs/Paxeer-X-Network/layerxproof/codec"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types/ethtx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/node/legacyabci"
	"github.com/Sidiora-Labs/Paxeer-X-Network/precompiles/wasmd"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/baseapp"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	sdkerrors "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

type SendAPI struct {
	tmClient         client.LocalClient
	txConfigProvider func(int64) client.TxConfig
	sendConfig       *SendConfig
	keeper           *keeper.Keeper
	ctxProvider      func(int64) sdk.Context
	homeDir          string
	backend          *Backend
	connectionType   ConnectionType
	methodTimeout    utils.Option[time.Duration]
}

type SendConfig struct {
	slow           bool
	keyringEnabled bool
	// Sequencer, set only on the sequencer, receives every raw transaction in place of the mempool.
	Sequencer SequencerAccepter
	// SequencerURL, set on every other node, is the EVM RPC endpoint raw transactions are forwarded to.
	SequencerURL *url.URL
	// pendingTxs, created by NewSendAPI when Sequencer is set, feeds eth_subscribe
	// newPendingTransactions. The HTTP and WebSocket SendAPI of the sequencer share one SendConfig so
	// a hash receipted through either reaches every subscriber.
	pendingTxs *pendingTxFeed1315
	// receiptLogs, set with Sequencer by a SequencerOption, records the execution receipt of every
	// receipted transaction for pending logs, filters and logs subscriptions.
	receiptLogs *ReceiptLogFeed
}

// pendingTxListenerBuffer1315 is how many receipted hashes a newPendingTransactions subscriber may
// lag behind before further hashes are dropped for it and counted as lag.
const pendingTxListenerBuffer1315 = 1024

// pendingTx1315 is one receipted hash queued for a subscriber, with the number of hashes dropped for
// that subscriber between the previous queued hash and this one.
type pendingTx1315 struct {
	hash   common.Hash
	lagged uint64
}

// pendingTxListener1315 is one newPendingTransactions subscriber. lagged counts the hashes dropped
// since the last queued one; it is guarded by the feed lock.
type pendingTxListener1315 struct {
	queue  chan pendingTx1315
	lagged uint64
}

// pendingTxLag1315 is the notification a subscriber receives in place of the hashes dropped while
// its buffer was full, so the client can resynchronise through pending-block reads.
type pendingTxLag1315 struct {
	Lagged hexutil.Uint64 `json:"lagged"`
}

// pendingTxFeed1315 fans receipted transaction hashes out to newPendingTransactions subscribers.
// receiptMu is held across Accept and emit so hashes leave in receipt order.
type pendingTxFeed1315 struct {
	receiptMu sync.Mutex
	mu        sync.Mutex
	listeners map[rpc.ID]*pendingTxListener1315
}

func (f *pendingTxFeed1315) add(id rpc.ID) <-chan pendingTx1315 {
	listener := &pendingTxListener1315{queue: make(chan pendingTx1315, pendingTxListenerBuffer1315)}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listeners[id] = listener
	return listener.queue
}

func (f *pendingTxFeed1315) remove(id rpc.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.listeners, id)
}

// emit hands hash to every subscriber. A subscriber whose buffer is full stays registered: the hash
// is dropped for it and counted as lag, reported ahead of the next hash it is handed.
func (f *pendingTxFeed1315) emit(hash common.Hash) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, listener := range f.listeners {
		select {
		case listener.queue <- pendingTx1315{hash: hash, lagged: listener.lagged}:
			listener.lagged = 0
		default:
			listener.lagged++
		}
	}
}

// SequencerAccepter executes transactions at receipt on the sequencer's open block.
type SequencerAccepter interface {
	// Accept executes the encoded transaction on the open block and returns its signed receipt once
	// the receipt is written to the WAL and acknowledged by the standby. On error the transaction
	// is not receipted.
	Accept(txBytes []byte) (receipt []byte, err error)
}

func NewSendAPI(
	tmClient client.LocalClient,
	txConfigProvider func(int64) client.TxConfig,
	sendConfig *SendConfig,
	k *keeper.Keeper,
	beginBlockKeepers legacyabci.BeginBlockKeepers,
	ctxProvider func(int64) sdk.Context,
	homeDir string,
	simulateConfig *SimulateConfig,
	app *baseapp.BaseApp,
	antehandler sdk.AnteHandler,
	connectionType ConnectionType,
	methodTimeout utils.Option[time.Duration],
	globalBlockCache BlockCache,
	cacheCreationMutex *sync.Mutex,
	watermarks *WatermarkManager,
) *SendAPI {
	if sendConfig.Sequencer != nil && sendConfig.pendingTxs == nil {
		sendConfig.pendingTxs = &pendingTxFeed1315{listeners: make(map[rpc.ID]*pendingTxListener1315)}
	}
	return &SendAPI{
		tmClient:         tmClient,
		txConfigProvider: txConfigProvider,
		sendConfig:       sendConfig,
		keeper:           k,
		ctxProvider:      ctxProvider,
		homeDir:          homeDir,
		backend:          NewBackend(ctxProvider, k, beginBlockKeepers, txConfigProvider, tmClient, simulateConfig, app, antehandler, globalBlockCache, cacheCreationMutex, watermarks),
		connectionType:   connectionType,
		methodTimeout:    methodTimeout,
	}
}

func (s *SendAPI) SendRawTransaction(ctx context.Context, input hexutil.Bytes) (hash common.Hash, err error) {
	if timeout, ok := s.methodTimeout.Get(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_sendRawTransaction", s.connectionType, startTime, err, recover())
	}()
	tx := new(ethtypes.Transaction)
	if err = tx.UnmarshalBinary(input); err != nil {
		return
	}
	hash = tx.Hash()
	if s.sendConfig.Sequencer != nil {
		return hash, s.acceptOnSequencer(tx)
	}
	if s.sendConfig.SequencerURL != nil {
		return s.forwardToSequencer(ctx, hash, input)
	}
	// getSender fails for AccessListTx, in which case we are not able to proxy or simulate,
	// but we still need to handle it.
	sender, senderErr := getSender(tx, s.keeper.ChainID(s.ctxProvider(LatestCtxHeight)))
	if senderErr == nil {
		if url, ok := s.tmClient.EvmProxy(sender); ok {
			recordRedirectedRequest(ctx, "eth_sendRawTransaction", string(s.connectionType))
			return forwardRawTransaction(ctx, url, hash, input)
		}
	}

	txData, err := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return hash, err
	}
	msg, err := types.NewMsgEVMTransaction(txData)
	if err != nil {
		return hash, err
	}
	gasUsedEstimate := tx.Gas() // if issue simulating, fallback to gas limit
	if senderErr == nil {       // simulation requires sender.
		if gas, err := s.simulateTx(ctx, sender, tx); err == nil {
			gasUsedEstimate = gas
		}
	}
	txBuilder := s.txConfigProvider(LatestCtxHeight).NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return hash, err
	}
	txBuilder.SetGasEstimate(gasUsedEstimate)
	txbz, encodeErr := s.txConfigProvider(LatestCtxHeight).TxEncoder()(txBuilder.GetTx())
	if encodeErr != nil {
		return hash, encodeErr
	}

	if s.sendConfig.slow {
		res, broadcastError := s.tmClient.BroadcastTxCommit(ctx, txbz)
		if broadcastError != nil {
			err = broadcastError
		} else if res == nil {
			err = errors.New("missing broadcast response")
		} else if res.CheckTx.Code != 0 {
			err = sdkerrors.ABCIError(sdkerrors.RootCodespace, res.CheckTx.Code, "")
		}
	} else {
		res, broadcastError := s.tmClient.BroadcastTx(ctx, txbz)
		if broadcastError != nil {
			err = broadcastError
		} else if res == nil {
			err = errors.New("missing broadcast response")
		} else if res.Code != 0 {
			err = sdkerrors.ABCIError(sdkerrors.RootCodespace, res.Code, "")
		}
	}
	return
}

// forwardRawTransaction relays input unchanged to the eth_sendRawTransaction of target and returns
// the hash target answers with.
func forwardRawTransaction(ctx context.Context, target *url.URL, hash common.Hash, input hexutil.Bytes) (common.Hash, error) {
	// HTTP transport pooling already happens globally underneath net/http, so
	// creating a fresh RPC client per proxied request is fine here. If we
	// start proxying over WebSocket, we'll need explicit custom pooling since
	// the underlying TCP connection lifecycle is strictly bound to Dial -> Close calls.
	client, err := rpc.DialContext(ctx, target.String())
	if err != nil {
		return hash, fmt.Errorf("rpc.DialContext(%q): %w", target.String(), err)
	}
	defer client.Close()

	if err := client.CallContext(ctx, &hash, "eth_sendRawTransaction", input); err != nil {
		// No error wrapping, because evm server is too dumb to handle wrapped error.
		return hash, err
	}
	return hash, nil
}

// forwardToSequencer relays the raw transaction to the sequencer; it never enters the local mempool.
func (s *SendAPI) forwardToSequencer(ctx context.Context, hash common.Hash, input hexutil.Bytes) (common.Hash, error) {
	recordRedirectedRequest(ctx, "eth_sendRawTransaction", string(s.connectionType))
	return forwardRawTransaction(ctx, s.sendConfig.SequencerURL, hash, input)
}

// acceptOnSequencer hands tx to the sequencer's Accept and returns once its signed receipt is
// released. The transaction is not simulated against the latest block and carries no gas
// estimate: the gas used comes from its execution at receipt.
func (s *SendAPI) acceptOnSequencer(tx *ethtypes.Transaction) error {
	txData, err := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return err
	}
	msg, err := types.NewMsgEVMTransaction(txData)
	if err != nil {
		return err
	}
	txConfig := s.txConfigProvider(LatestCtxHeight)
	txBuilder := txConfig.NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return err
	}
	txbz, err := txConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return err
	}
	feed := s.sendConfig.pendingTxs
	feed.receiptMu.Lock()
	defer feed.receiptMu.Unlock()
	receipt, err := s.sendConfig.Sequencer.Accept(txbz)
	if err != nil {
		var coded *sdkerrors.Error
		if errors.As(err, &coded) {
			return sdkerrors.ABCIError(coded.Codespace(), coded.ABCICode(), "")
		}
		return err
	}
	if len(receipt) == 0 {
		return errors.New("missing sequencer receipt")
	}
	if s.sendConfig.receiptLogs != nil {
		if err := s.recordReceiptLogs(receipt, tx.Hash()); err != nil {
			return fmt.Errorf("transaction %s is receipted but its logs are not recorded: %w", tx.Hash().Hex(), err)
		}
	}
	feed.emit(tx.Hash())
	return nil
}

// recordReceiptLogs records the execution receipt of the transaction hash, whose signed sequencer
// receipt is signed, in the receipt log feed at the receipt's seq, batch height and transaction
// index. The caller holds receiptMu, so transactions are recorded in receipt order.
func (s *SendAPI) recordReceiptLogs(signed []byte, hash common.Hash) error {
	decoded, err := codec.DecodeReceipt(signed)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSequencerReceiptInvalid, err)
	}
	if common.Hash(decoded.ActivityID) != hash {
		return mismatch("receipt activity %x, transaction %s", decoded.ActivityID, hash.Hex())
	}
	if !bytes.Equal(decoded.BatchID[:24], make([]byte, 24)) {
		return invalidReceipt("batch_id is not a height")
	}
	height := binary.BigEndian.Uint64(decoded.BatchID[24:])
	if height == 0 || height > math.MaxInt64 {
		return invalidReceipt("batch height %d", height)
	}
	if len(decoded.Effects) == 0 {
		return invalidReceipt("no summary effect")
	}
	summary := decoded.Effects[0]
	if summary.ModuleID != decoded.ModuleID || summary.Ordinal != 0 || summary.Kind != sequencerEffectKindState {
		return invalidReceipt("effect 0 is not the execution summary")
	}
	if len(summary.Body) < sequencerSummaryFixedBytes {
		return invalidReceipt("summary is %d bytes", len(summary.Body))
	}
	txIndex := binary.BigEndian.Uint32(summary.Body[24:28])
	rcpt, err := s.keeper.GetTransientReceipt(s.ctxProvider(PendingCtxHeight), hash, uint64(txIndex))
	if err != nil {
		return fmt.Errorf("read execution receipt at index %d: %w", txIndex, err)
	}
	return s.sendConfig.receiptLogs.Receipted(decoded.GlobalSequence, int64(height), hash, rcpt) //nolint:gosec // height is checked to fit int64 above
}

// NewPendingTransactions serves eth_subscribe newPendingTransactions on the sequencer: the hash of
// every transaction receipted after the subscription, in receipt order. Every other node refuses
// it with ErrPendingServedBySequencer.
//
// A subscriber may lag pendingTxListenerBuffer1315 (1024) hashes behind; hashes receipted while its
// buffer is full are dropped for it. Ahead of the next hash it receives after a drop it receives one
// lag notification {"lagged": "0x<count>"} carrying the number of hashes dropped, then hashes
// resume in receipt order. The subscription stays live until the client unsubscribes or the
// connection closes.
func (s *SendAPI) NewPendingTransactions(ctx context.Context) (sub *rpc.Subscription, err error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_newPendingTransactions", s.connectionType, startTime, err, recover())
	}()
	feed := s.sendConfig.pendingTxs
	if feed == nil {
		return nil, ErrPendingServedBySequencer
	}
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}
	rpcSub := notifier.CreateSubscription()
	listener := feed.add(rpcSub.ID)
	go func() {
		defer recoverAndLog()
		defer feed.remove(rpcSub.ID)
		for {
			select {
			case pending := <-listener:
				if pending.lagged > 0 {
					if err := notifier.Notify(rpcSub.ID, pendingTxLag1315{Lagged: hexutil.Uint64(pending.lagged)}); err != nil {
						return
					}
				}
				if err := notifier.Notify(rpcSub.ID, pending.hash); err != nil {
					return
				}
			case <-rpcSub.Err():
				return
			}
		}
	}()
	return rpcSub, nil
}

func getSender(tx *ethtypes.Transaction, chainID *big.Int) (common.Address, error) {
	return ethtypes.LatestSignerForChainID(chainID).Sender(tx)
}

func (s *SendAPI) simulateTx(ctx context.Context, sender common.Address, tx *ethtypes.Transaction) (estimate uint64, err error) {
	input_ := (hexutil.Bytes)(tx.Data())
	gas_ := hexutil.Uint64(tx.Gas())
	nonce_ := hexutil.Uint64(tx.Nonce())
	al := tx.AccessList()
	bNrOrHash := rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	ctx = context.WithValue(ctx, CtxIsWasmdPrecompileCallKey, wasmd.IsWasmdCall(tx.To()))
	gp := tx.GasPrice()
	maxFeePerGas := tx.GasFeeCap()
	maxPriorityFeePerGas := tx.GasTipCap()
	if gp != nil {
		maxFeePerGas = nil
		maxPriorityFeePerGas = nil
	} else {
		gp = nil
	}
	txArgs := export.TransactionArgs{
		From:                 &sender,
		To:                   tx.To(),
		Gas:                  &gas_,
		GasPrice:             (*hexutil.Big)(gp),
		MaxFeePerGas:         (*hexutil.Big)(maxFeePerGas),
		MaxPriorityFeePerGas: (*hexutil.Big)(maxPriorityFeePerGas),
		Value:                (*hexutil.Big)(tx.Value()),
		Nonce:                &nonce_,
		Input:                &input_,
		AccessList:           &al,
		ChainID:              (*hexutil.Big)(tx.ChainId()),
	}
	estimate_, err := export.DoEstimateGas(ctx, s.backend, txArgs, bNrOrHash, nil, nil, s.backend.RPCGasCap())
	if err != nil {
		err = fmt.Errorf("failed to estimate gas: %w", err)
		return
	}
	return uint64(estimate_), nil
}

func (s *SendAPI) SignTransaction(ctx context.Context, args apitypes.SendTxArgs, _ *string) (result *export.SignTransactionResult, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_signTransaction", s.connectionType, startTime, returnErr, recover())
	}()
	if s.sendConfig == nil || !s.sendConfig.keyringEnabled {
		return nil, &ErrEVMNotSupported{Msg: "eth_signTransaction is disabled; submit a locally signed raw transaction"}
	}
	unsignedTx, err := args.ToTransaction()
	if err != nil {
		return nil, err
	}
	signedTx, err := s.signTransaction(unsignedTx, args.From.Address().Hex())
	if err != nil {
		return nil, err
	}
	data, err := signedTx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &export.SignTransactionResult{Raw: data, Tx: signedTx}, nil
}

func (s *SendAPI) SendTransaction(ctx context.Context, args export.TransactionArgs) (result common.Hash, returnErr error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_sendTransaction", s.connectionType, startTime, returnErr, recover())
	}()
	if s.sendConfig == nil || !s.sendConfig.keyringEnabled {
		return common.Hash{}, &ErrEVMNotSupported{Msg: "eth_sendTransaction is disabled; submit a locally signed raw transaction"}
	}
	if err := args.SetDefaults(ctx, s.backend, false); err != nil {
		return common.Hash{}, err
	}
	var unsignedTx = args.ToTransaction(ethtypes.LegacyTxType)
	signedTx, err := s.signTransaction(unsignedTx, args.From.Hex())
	if err != nil {
		return common.Hash{}, err
	}
	data, err := signedTx.MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	return s.SendRawTransaction(ctx, data)
}

func (s *SendAPI) signTransaction(unsignedTx *ethtypes.Transaction, from string) (*ethtypes.Transaction, error) {
	kb, err := getTestKeyring(s.homeDir)
	if err != nil {
		return nil, err
	}
	privKey, ok := getAddressPrivKeyMap(kb)[from]
	if !ok {
		return nil, errors.New("from address does not have hosted key")
	}
	chainId := s.keeper.ChainID(s.ctxProvider(LatestCtxHeight))
	signer := ethtypes.LatestSignerForChainID(chainId)
	return ethtypes.SignTx(unsignedTx, signer, privKey)
}
