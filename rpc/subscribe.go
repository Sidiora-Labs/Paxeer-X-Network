package evmrpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sync"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/consensus/rpc/coretypes"
	tmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/consensus/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/keeper"
	evmtypes "github.com/Sidiora-Labs/Paxeer-X-Network/modules/evm/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/sdk/client"
	sdk "github.com/Sidiora-Labs/Paxeer-X-Network/sdk/types"
	"github.com/Sidiora-Labs/Paxeer-X-Network/utils"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/ethereum/go-ethereum/rpc"
)

const SleepInterval = 5 * time.Second
const NewHeadsListenerBuffer = 10

// ReceiptListenerBuffer is how many receipted transactions a receipt-fed
// subscriber may lag behind Accept before it is dropped.
const ReceiptListenerBuffer = 1024

type SubscriptionAPI struct {
	tmClient            client.LocalClient
	subscriptionManager *SubscriptionManager
	subscriptonConfig   *SubscriptionConfig

	logFetcher          *LogFetcher
	newHeadListenersMtx *sync.RWMutex
	newHeadListeners    map[rpc.ID]chan map[string]interface{}
	connectionType      ConnectionType
	// receipts is set only on the sequencer; it feeds the logs and
	// newPendingTransactions subscriptions from Accept.
	receipts *ReceiptStream
}

type SubscriptionConfig struct {
	subscriptionCapacity int
	newHeadLimit         uint64
}

// NewSubscriptionAPI serves eth_subscribe. The sequencer passes the
// ReceiptStream its Accept path publishes to; every other node passes none.
func NewSubscriptionAPI(tmClient client.LocalClient, k *keeper.Keeper, ctxProvider func(int64) sdk.Context, logFetcher *LogFetcher, subscriptionConfig *SubscriptionConfig, filterConfig *FilterConfig, connectionType ConnectionType, blockHeaderNotifier *BlockHeaderNotifier, receipts ...*ReceiptStream) (*SubscriptionAPI, error) {
	if len(receipts) > 1 {
		return nil, errors.New("subscription API takes at most one receipt stream")
	}
	if k == nil || ctxProvider == nil || logFetcher == nil || subscriptionConfig == nil || filterConfig == nil {
		return nil, errors.New("subscription API dependencies are not configured")
	}
	if blockHeaderNotifier == nil && tmClient == nil {
		return nil, errors.New("subscription API requires a consensus client or block-header notifier")
	}
	logFetcher.filterConfig = filterConfig
	api := &SubscriptionAPI{
		tmClient:            tmClient,
		subscriptonConfig:   subscriptionConfig,
		logFetcher:          logFetcher,
		newHeadListenersMtx: &sync.RWMutex{},
		newHeadListeners:    make(map[rpc.ID]chan map[string]interface{}),
		connectionType:      connectionType,
		// subscriptionManager is only constructed for the legacy
		// event-bus path below; under Autobahn the notifier feeds the
		// fan-out directly and the manager is unused.
	}
	if len(receipts) == 1 {
		api.receipts = receipts[0]
	}
	if blockHeaderNotifier != nil {
		// Autobahn (and any future direct-channel) path. The producer
		// pushes one event per committed block; there is no Tendermint
		// event-bus subscription.
		go api.runNewHeadsFromNotifier(blockHeaderNotifier, k, ctxProvider)
	} else {
		// Legacy CometBFT path: subscribe to the Tendermint event bus.
		api.subscriptionManager = NewSubscriptionManager(tmClient)
		id, subCh, err := api.subscriptionManager.Subscribe(context.Background(), NewHeadQueryBuilder(), api.subscriptonConfig.subscriptionCapacity)
		if err != nil {
			return nil, fmt.Errorf("subscribe to consensus new-head events: %w", err)
		}
		go func() {
			defer recoverAndLog()
			defer func() {
				_ = api.subscriptionManager.Unsubscribe(context.Background(), id)
			}()
			for res := range subCh {
				eventHeader, ok := res.Data.(tmtypes.EventDataNewBlockHeader)
				if !ok {
					fmt.Printf("dropping malformed newHeads event of type %T\n", res.Data)
					continue
				}
				ctx := ctxProvider(eventHeader.Header.Height)
				baseFeePerGas := k.GetNextBaseFeePerGas(ctx).TruncateInt().BigInt()
				cp := ctx.ConsensusParams()
				if cp == nil || cp.Block == nil {
					fmt.Printf("dropping newHeads event at height %d: consensus block parameters are unavailable\n", eventHeader.Header.Height)
					continue
				}
				gasLimit := cp.Block.MaxGas
				ethHeader, err := encodeTmHeader(eventHeader, baseFeePerGas, gasLimit)
				if err != nil {
					fmt.Printf("error encoding new head event %#v due to %s\n", res.Data, err)
					continue
				}
				api.broadcastNewHead(ethHeader)
			}
		}()
	}
	return api, nil
}

func (a *SubscriptionAPI) runNewHeadsFromNotifier(notifier *BlockHeaderNotifier, k *keeper.Keeper, ctxProvider func(int64) sdk.Context) {
	defer recoverAndLog()
	for evt := range notifier.recv() {
		// Defend against a misbehaving producer. OnBlockCommitted's
		// contract requires non-nil header/response, but a single bad
		// event must not kill the fan-out goroutine for all subscribers.
		if evt.header == nil || evt.response == nil {
			fmt.Printf("dropping malformed newHeads event: header=%v response=%v\n", evt.header, evt.response)
			continue
		}
		ctx := ctxProvider(evt.header.Height)
		baseFeePerGas := pickHeadBaseFee(k.GetNextBaseFeePerGas, ctxProvider, evt.header.Height)
		// Source gasLimit from the active SDK ConsensusParams rather than
		// evt.response.ConsensusParamUpdates: the latter is only populated
		// on actual updates (nil for nearly every block). See block.go's
		// GetBlockByNumber for the same pattern + rationale.
		cp := ctx.ConsensusParams()
		if cp == nil || cp.Block == nil {
			fmt.Printf("dropping newHeads event at height %d: consensus block parameters are unavailable\n", evt.header.Height)
			continue
		}
		gasLimit := cp.Block.MaxGas
		ethHeader, err := encodeCommittedBlock(evt, baseFeePerGas, gasLimit)
		if err != nil {
			fmt.Printf("dropping invalid newHeads event at height %d: %v\n", evt.header.Height, err)
			continue
		}
		a.broadcastNewHead(ethHeader)
	}
}

// pickHeadBaseFee returns the baseFeePerGas to attach to the eth_newHeads
// notification for the block at `height`. Mirrors block.go's
// GetBlockByNumber: GetNextBaseFeePerGas(ctx_at_N) is the fee for N+1, so
// we call it on the *parent* ctx (height-1). Genesis (height 1) has no
// parent; return the configured default min fee instead.
//
// `getNextBaseFee` is a function pointer rather than a *keeper.Keeper
// method so tests can inject a fake without needing a full keeper.
func pickHeadBaseFee(getNextBaseFee func(sdk.Context) sdk.Dec, ctxProvider func(int64) sdk.Context, height int64) *big.Int {
	if height > 1 {
		return getNextBaseFee(ctxProvider(height - 1)).TruncateInt().BigInt()
	}
	return evmtypes.DefaultMinFeePerGas.TruncateInt().BigInt()
}

func (a *SubscriptionAPI) broadcastNewHead(ethHeader map[string]interface{}) {
	a.newHeadListenersMtx.Lock()
	defer a.newHeadListenersMtx.Unlock()
	toDelete := []rpc.ID{}
	for id, c := range a.newHeadListeners {
		if !handleListener(c, ethHeader) {
			toDelete = append(toDelete, id)
		}
	}
	for _, id := range toDelete {
		delete(a.newHeadListeners, id)
	}
}

func handleListener[T any](c chan T, ethHeader T) bool {
	// if the channel is already closed, sending to it/closing it will panic
	defer func() { _ = recover() }()
	select {
	case c <- ethHeader:
		return true
	default:
		// this path is hit when the buffer is full, meaning that the subscriber is not consuming
		// fast enough
		close(c)
		return false
	}
}

func (a *SubscriptionAPI) NewHeads(ctx context.Context) (s *rpc.Subscription, err error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_newHeads", a.connectionType, startTime, err, recover())
	}()
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}

	rpcSub := notifier.CreateSubscription()
	listener := make(chan map[string]interface{}, NewHeadsListenerBuffer)
	a.newHeadListenersMtx.Lock()
	defer a.newHeadListenersMtx.Unlock()
	if a.subscriptonConfig.newHeadLimit > 0 && uint64(len(a.newHeadListeners)) >= a.subscriptonConfig.newHeadLimit {
		return nil, errors.New("no new subscription can be created")
	}
	a.newHeadListeners[rpcSub.ID] = listener

	go func() {
		defer recoverAndLog()
	OUTER:
		for {
			select {
			case res, ok := <-listener:
				if !ok {
					break OUTER
				}
				if err := notifier.Notify(rpcSub.ID, res); err != nil {
					break OUTER
				}
			case <-rpcSub.Err():
				break OUTER
			}
		}
		a.newHeadListenersMtx.Lock()
		defer a.newHeadListenersMtx.Unlock()
		delete(a.newHeadListeners, rpcSub.ID)
		defer func() { _ = recover() }() // might have already been closed
		close(listener)
	}()

	return rpcSub, nil
}

func (a *SubscriptionAPI) Logs(ctx context.Context, filter *filters.FilterCriteria) (s *rpc.Subscription, _err error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_logs", a.connectionType, startTime, _err, recover())
	}()
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}
	// create empty filter if filter does not exist
	if filter == nil {
		filter = &filters.FilterCriteria{}
	}
	// when fromBlock is 0 and toBlock is latest, adjust the filter
	// to unbounded filter
	if filter.FromBlock != nil && filter.FromBlock.Int64() == 0 &&
		filter.ToBlock != nil && filter.ToBlock.Int64() < 0 {
		latest := big.NewInt(a.logFetcher.ctxProvider(LatestCtxHeight).BlockHeight())
		unboundedFilter := &filters.FilterCriteria{
			FromBlock: latest, // set to latest block height
			ToBlock:   nil,    // set to nil to continue listening
			Addresses: filter.Addresses,
			Topics:    filter.Topics,
		}
		filter = unboundedFilter
	}

	rpcSub := notifier.CreateSubscription()

	// Track subscription metrics
	wpMetrics := GetGlobalMetrics()
	wpMetrics.RecordSubscriptionStart()

	if filter.BlockHash != nil {
		go func() {
			var err error
			defer recoverAndLog()
			defer wpMetrics.RecordSubscriptionEnd()
			logs, _, err := a.logFetcher.GetLogsByFilters(ctx, *filter, 0)
			if err != nil {
				wpMetrics.RecordSubscriptionError()
				_ = notifier.Notify(rpcSub.ID, err)
				return
			}
			for _, log := range logs {
				if err = notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
		}()
		return rpcSub, nil
	}

	if a.receipts != nil {
		listener := a.receipts.addLogs(rpcSub.ID)
		go a.receipts.receiptLogStream(notifier, rpcSub, *filter, listener, wpMetrics)
		return rpcSub, nil
	}

	go func() {
		var err error
		defer recoverAndLog()
		defer wpMetrics.RecordSubscriptionEnd()
		begin := int64(0)
		for {
			var logs []*ethtypes.Log
			var lastToHeight int64
			logs, lastToHeight, err = a.logFetcher.GetLogsByFilters(ctx, *filter, begin)
			if err != nil {
				wpMetrics.RecordSubscriptionError()
				_ = notifier.Notify(rpcSub.ID, err)
				return
			}
			for _, log := range logs {
				if err = notifier.Notify(rpcSub.ID, log); err != nil {
					return
				}
			}
			if filter.ToBlock != nil && lastToHeight >= filter.ToBlock.Int64() {
				return
			}
			begin = lastToHeight
			filter.FromBlock = big.NewInt(lastToHeight + 1)
			timer := time.NewTimer(SleepInterval)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
	}()

	return rpcSub, nil
}

// NewPendingTransactions streams the hashes of receipted transactions in
// receipt order. Only the sequencer serves it; other nodes refuse with
// ErrPendingServedBySequencer.
func (a *SubscriptionAPI) NewPendingTransactions(ctx context.Context) (s *rpc.Subscription, err error) {
	startTime := time.Now()
	defer func() {
		recordMetricsWithError(ctx, "eth_newPendingTransactions", a.connectionType, startTime, err, recover())
	}()
	if a.receipts == nil {
		return nil, ErrPendingServedBySequencer
	}
	notifier, supported := rpc.NotifierFromContext(ctx)
	if !supported {
		return &rpc.Subscription{}, rpc.ErrNotificationsUnsupported
	}
	rpcSub := notifier.CreateSubscription()
	wpMetrics := GetGlobalMetrics()
	wpMetrics.RecordSubscriptionStart()
	listener := a.receipts.addTxs(rpcSub.ID)
	go func() {
		defer recoverAndLog()
		defer wpMetrics.RecordSubscriptionEnd()
		defer a.receipts.removeTxs(rpcSub.ID)
		for {
			select {
			case hash, ok := <-listener:
				if !ok {
					return
				}
				if err := notifier.Notify(rpcSub.ID, hash); err != nil {
					return
				}
			case <-rpcSub.Err():
				return
			}
		}
	}()
	return rpcSub, nil
}

// ErrReceiptOrder refuses a receipted transaction published out of receipt
// order.
var ErrReceiptOrder = errors.New("receipt published out of order")

// SubscriptionReceipt is one transaction receipted by Accept on the
// sequencer.
type SubscriptionReceipt struct {
	Seq    uint64
	Height int64
	// TxIndex is the position of the transaction in the open block.
	TxIndex uint32
	Hash    common.Hash
	Logs    []*evmtypes.Log
}

type receiptedLogs struct {
	seq  uint64
	logs []*ethtypes.Log
}

// receiptLog is a receipted log as notified to a logs subscriber: the log
// fields plus the seq of the transaction that emitted it.
type receiptLog struct {
	log *ethtypes.Log
	seq uint64
}

func (l receiptLog) MarshalJSON() ([]byte, error) {
	enc, err := l.log.MarshalJSON()
	if err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(enc, &fields); err != nil {
		return nil, err
	}
	seq, err := json.Marshal(hexutil.Uint64(l.seq))
	if err != nil {
		return nil, err
	}
	fields["seq"] = seq
	return json.Marshal(fields)
}

// ReceiptStream fans transactions receipted by Accept out to the sequencer's
// logs and newPendingTransactions subscriptions. Logs are numbered within the
// open block the way committed logs are numbered, so a committed log keeps
// the index it was first notified with.
type ReceiptStream struct {
	mu           sync.Mutex
	lastSeq      uint64
	height       int64
	nextLogIndex uint
	logs         map[rpc.ID]chan receiptedLogs
	txs          map[rpc.ID]chan common.Hash
}

func NewReceiptStream() *ReceiptStream {
	return &ReceiptStream{
		logs: make(map[rpc.ID]chan receiptedLogs),
		txs:  make(map[rpc.ID]chan common.Hash),
	}
}

// Publish hands one receipted transaction to the subscribers. The Accept path
// calls it once per receipted transaction, in seq order; a seq that is not
// after the last published one, or a height below the current one, is
// refused with ErrReceiptOrder.
func (s *ReceiptStream) Publish(r SubscriptionReceipt) error {
	if r.Height <= 0 {
		return fmt.Errorf("receipted transaction %s has invalid height %d", r.Hash.Hex(), r.Height)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Seq <= s.lastSeq {
		return fmt.Errorf("%w: seq %d after seq %d", ErrReceiptOrder, r.Seq, s.lastSeq)
	}
	if r.Height < s.height {
		return fmt.Errorf("%w: height %d after height %d", ErrReceiptOrder, r.Height, s.height)
	}
	logIndex := s.nextLogIndex
	if r.Height != s.height {
		logIndex = 0
	}
	logs := make([]*ethtypes.Log, 0, len(r.Logs))
	for i, l := range r.Logs {
		if l == nil {
			return fmt.Errorf("receipted transaction %s has a nil log at %d", r.Hash.Hex(), i)
		}
		topics := make([]common.Hash, len(l.Topics))
		for j, topic := range l.Topics {
			topics[j] = common.HexToHash(topic)
		}
		logs = append(logs, &ethtypes.Log{
			Address:     common.HexToAddress(l.Address),
			Topics:      topics,
			Data:        l.Data,
			BlockNumber: uint64(r.Height), //nolint:gosec // height checked positive above
			TxHash:      r.Hash,
			TxIndex:     uint(r.TxIndex),
			Index:       logIndex,
		})
		logIndex++
	}
	s.lastSeq, s.height, s.nextLogIndex = r.Seq, r.Height, logIndex
	if len(logs) > 0 {
		fanOutReceipted(s.logs, receiptedLogs{seq: r.Seq, logs: logs})
	}
	fanOutReceipted(s.txs, r.Hash)
	return nil
}

// fanOutReceipted delivers v to every listener; a listener whose buffer is
// full is closed and dropped. The caller holds the stream lock.
func fanOutReceipted[T any](listeners map[rpc.ID]chan T, v T) {
	for id, c := range listeners {
		select {
		case c <- v:
		default:
			close(c)
			delete(listeners, id)
		}
	}
}

func (s *ReceiptStream) addLogs(id rpc.ID) chan receiptedLogs {
	c := make(chan receiptedLogs, ReceiptListenerBuffer)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs[id] = c
	return c
}

func (s *ReceiptStream) addTxs(id rpc.ID) chan common.Hash {
	c := make(chan common.Hash, ReceiptListenerBuffer)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.txs[id] = c
	return c
}

func (s *ReceiptStream) removeLogs(id rpc.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.logs[id]; ok {
		close(c)
		delete(s.logs, id)
	}
}

func (s *ReceiptStream) removeTxs(id rpc.ID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.txs[id]; ok {
		close(c)
		delete(s.txs, id)
	}
}

// receiptLogStream notifies the receipted logs matching crit, each tagged with
// its seq. It never reads committed logs, so a log is notified once, when it
// is receipted. A non-negative ToBlock ends the subscription once the open
// block passes it.
func (s *ReceiptStream) receiptLogStream(notifier *rpc.Notifier, sub *rpc.Subscription, crit filters.FilterCriteria, listener chan receiptedLogs, wpMetrics *WorkerPoolMetrics) {
	defer recoverAndLog()
	defer wpMetrics.RecordSubscriptionEnd()
	defer s.removeLogs(sub.ID)
	from, to := int64(-1), int64(-1)
	if crit.FromBlock != nil && crit.FromBlock.IsInt64() {
		from = crit.FromBlock.Int64()
	}
	if crit.ToBlock != nil && crit.ToBlock.IsInt64() {
		to = crit.ToBlock.Int64()
	}
	for {
		select {
		case batch, ok := <-listener:
			if !ok {
				return
			}
			for _, l := range batch.logs {
				height := int64(l.BlockNumber) //nolint:gosec // Publish takes positive int64 heights
				if to >= 0 && height > to {
					return
				}
				if height < from || !MatchesCriteria(l, crit) {
					continue
				}
				if err := notifier.Notify(sub.ID, receiptLog{log: l, seq: batch.seq}); err != nil {
					return
				}
			}
		case <-sub.Err():
			return
		}
	}
}

const SubscriberPrefix = "evm.rpc."

type SubscriberID uint64

type SubInfo struct {
	Query          string
	SubscriptionCh <-chan coretypes.ResultEvent
}

type SubscriptionManager struct {
	subMu            sync.Mutex
	NextID           SubscriberID
	SubscriptionInfo map[SubscriberID]SubInfo
	tmClient         client.LocalClient
}

func NewSubscriptionManager(tmClient client.LocalClient) *SubscriptionManager {
	return &SubscriptionManager{
		subMu:            sync.Mutex{},
		NextID:           1,
		SubscriptionInfo: make(map[SubscriberID]SubInfo),
		tmClient:         tmClient,
	}
}

func (s *SubscriptionManager) Subscribe(ctx context.Context, q *QueryBuilder, limit int) (SubscriberID, <-chan coretypes.ResultEvent, error) {
	if s == nil || s.tmClient == nil || q == nil {
		return 0, nil, errors.New("subscription manager is not configured")
	}
	query := q.Build()
	s.subMu.Lock()
	defer s.subMu.Unlock()
	if s.NextID == SubscriberID(math.MaxUint64) {
		return 0, nil, errors.New("subscription identifier space exhausted")
	}
	id := s.NextID
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	res, err := s.tmClient.Subscribe(ctx, fmt.Sprintf("%s%d", SubscriberPrefix, id), query, limit)
	if err != nil {
		return 0, nil, err
	}
	s.SubscriptionInfo[id] = SubInfo{Query: query, SubscriptionCh: res}
	s.NextID++
	return id, res, nil
}

func (s *SubscriptionManager) Unsubscribe(ctx context.Context, id SubscriberID) error {
	if s == nil || s.tmClient == nil {
		return errors.New("subscription manager is not configured")
	}
	s.subMu.Lock()
	defer s.subMu.Unlock()
	info, ok := s.SubscriptionInfo[id]
	if !ok {
		return fmt.Errorf("subscription %d does not exist", id)
	}
	// ignore deprecation here since the new endpoint does not support polling
	//nolint:staticcheck
	err := s.tmClient.Unsubscribe(ctx, fmt.Sprintf("%s%d", SubscriberPrefix, id), info.Query)
	if err != nil {
		return err
	}
	delete(s.SubscriptionInfo, id)
	return nil
}

// encodeCommittedBlock rejects Autobahn notifications until the producer
// supplies the canonical parent, transaction, and receipt roots required by
// an Ethereum newHeads payload. Publishing zero roots makes the stream look
// cryptographically complete when it is not.
func encodeCommittedBlock(evt blockHeaderEvent, baseFee *big.Int, gasLimit int64) (map[string]interface{}, error) {
	if evt.header == nil || evt.response == nil {
		return nil, errors.New("committed head is missing header or finalize response")
	}
	return nil, fmt.Errorf("committed Autobahn head %d lacks canonical parent, transaction, and receipt roots", evt.header.Height)
}

func encodeTmHeader(
	header tmtypes.EventDataNewBlockHeader,
	baseFee *big.Int,
	gasLimit int64,
) (map[string]interface{}, error) {
	if header.Header.Height <= 0 {
		return nil, fmt.Errorf("new head has invalid height %d", header.Header.Height)
	}
	if header.Header.Time.Unix() < 0 {
		return nil, fmt.Errorf("new head %d timestamp predates Unix epoch", header.Header.Height)
	}
	if baseFee == nil || baseFee.Sign() < 0 {
		return nil, errors.New("new head has invalid base fee")
	}
	encodedGasLimit, err := encodeHeadGasLimit(gasLimit)
	if err != nil {
		return nil, err
	}
	blockHash := common.HexToHash(header.Header.Hash().String())
	number := big.NewInt(header.Header.Height)
	miner := common.HexToAddress(header.Header.ProposerAddress.String())
	gasWanted := uint64(0)
	lastHash := common.HexToHash(header.Header.LastBlockID.Hash.String())
	resultHash := common.HexToHash(header.Header.LastResultsHash.String())
	appHash := common.HexToHash(header.Header.AppHash.String())
	txHash := common.HexToHash(header.Header.DataHash.String())
	for _, txRes := range header.ResultFinalizeBlock.TxResults {
		if txRes == nil || txRes.GasUsed < 0 {
			return nil, errors.New("new head contains invalid transaction gas usage")
		}
		gasUsed := uint64(txRes.GasUsed)
		if math.MaxUint64-gasWanted < gasUsed {
			return nil, errors.New("new head transaction gas usage overflows uint64")
		}
		gasWanted += gasUsed
	}
	result := map[string]interface{}{
		"difficulty":            (*hexutil.Big)(utils.Big0), // inapplicable to Pax
		"extraData":             hexutil.Bytes{},            // inapplicable to Pax
		"gasLimit":              encodedGasLimit,
		"gasUsed":               hexutil.Uint64(gasWanted),
		"logsBloom":             ethtypes.Bloom{}, // inapplicable to Pax
		"miner":                 miner,
		"nonce":                 ethtypes.BlockNonce{}, // inapplicable to Pax
		"number":                (*hexutil.Big)(number),
		"parentHash":            lastHash,
		"receiptsRoot":          resultHash,
		"sha3Uncles":            common.Hash{}, // inapplicable to Pax
		"stateRoot":             appHash,
		"timestamp":             hexutil.Uint64(header.Header.Time.Unix()), //nolint:gosec
		"transactionsRoot":      txHash,
		"mixHash":               common.Hash{},     // inapplicable to Pax
		"excessBlobGas":         hexutil.Uint64(0), // inapplicable to Pax
		"parentBeaconBlockRoot": common.Hash{},     // inapplicable to Pax
		"hash":                  blockHash,
		"baseFeePerGas":         (*hexutil.Big)(baseFee),
		"withdrawalsRoot":       common.Hash{},     // inapplicable to Pax
		"blobGasUsed":           hexutil.Uint64(0), // inapplicable to Pax
	}
	return result, nil
}

func encodeHeadGasLimit(gasLimit int64) (hexutil.Uint64, error) {
	if gasLimit == -1 {
		return hexutil.Uint64(math.MaxUint64), nil
	}
	if gasLimit < 0 {
		return 0, fmt.Errorf("new head has invalid gas limit %d", gasLimit)
	}
	return hexutil.Uint64(gasLimit), nil
}
