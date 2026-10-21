// Package source follows the head of the Paxeer X Network for the perps data
// services: a health-ranked EVM JSON-RPC pool (RPC), the consensus header and
// block results (Comet) and the parent-hash-continuous Follower.
//
// Origin: Launch-x core/indexer/internal/source (evm.go: EVMOptions,
// EVMSource, NewEVM, Head, FetchLogs, toLog, getBlock, callWithFallback,
// rankedIndices, markSuccess, markFailure, runHealthCheck, parseHexUint64;
// source.go: FetchOptions, FetchResult), renamed RPC.
package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/decode"
)

// MaxLogRange is the largest inclusive block span of one eth_getLogs call.
const MaxLogRange = 500

const (
	maxResponseBytes = 32 << 20
	bulkTimeout      = 15 * time.Second
)

// Venue addresses of the address set; the share pointers join at run time.
var (
	PerpsModule    = common.HexToAddress("0x0000000000000000000000000000000000001015")
	SpotRouter     = common.HexToAddress("0x0000000000000000000000000000000000001020")
	SpotPool       = common.HexToAddress("0x0000000000000000000000000000000000001021")
	SpotLimit      = common.HexToAddress("0x0000000000000000000000000000000000001022")
	SpotStop       = common.HexToAddress("0x0000000000000000000000000000000000001023")
	SpotFees       = common.HexToAddress("0x0000000000000000000000000000000000001024")
	SpotVault      = common.HexToAddress("0x0000000000000000000000000000000000001025")
	USDL           = common.HexToAddress("0x85FcD13735F4309833A503EE804ea32395851479")
	VenueAddresses = []common.Address{PerpsModule, SpotRouter, SpotPool, SpotLimit, SpotStop, SpotFees, SpotVault, USDL}
)

// ErrResponseSize reports a node refusal of a request by its result size.
var ErrResponseSize = errors.New("source: response size refused")

// ErrNodeBehind reports that no healthy node holds the block a request needs.
var ErrNodeBehind = errors.New("source: no healthy node holds the block")

// FetchOptions is one ranged log query.
type FetchOptions struct {
	// MonitoredAddresses is the address filter; an empty filter is refused.
	MonitoredAddresses []string
	// FromBlock / ToBlock is the inclusive range.
	FromBlock int64
	ToBlock   int64
	// Concurrency caps parallel per-block enrichment calls.
	Concurrency int
	Historical  bool
}

// FetchResult is the output of one ranged log query.
type FetchResult struct {
	// Logs are the matched logs in (block, logIndex) ascending order.
	Logs []ethtypes.Log
	// BlockTimestamps maps block number -> unix epoch seconds.
	BlockTimestamps map[uint64]int64
	// TxFromMap maps a lowercased tx hash -> the lowercased sender.
	TxFromMap map[string]string
}

// RPCOptions configures an EVM JSON-RPC pool.
type RPCOptions struct {
	URLs   []string
	Logger *slog.Logger
	// Timeout is the per-request HTTP timeout (default and cap 2s).
	Timeout time.Duration
	// StaleThreshold quarantines a node more than this many blocks behind the
	// pool median (default 50). HealthInterval is the re-rank cadence (default
	// 30s). ReceiptConcurrency caps parallel block fetches (default 10).
	StaleThreshold     int
	HealthInterval     time.Duration
	ReceiptConcurrency int
	now                func() time.Time
	httpClient         *http.Client
}

type clientSlot struct {
	url                 string
	lastBlock           uint64
	lastSeenAt          int64 // unix ms; 0 = never
	consecutiveFailures int
	recentLatencyMs     int64
	isStale             bool
}

// RPC is an eth_getLogs log source and head source with health-ranked
// failover across a node pool.
type RPC struct {
	logger             *slog.Logger
	client             *http.Client
	timeout            time.Duration
	staleThreshold     int
	receiptConcurrency int
	now                func() time.Time
	nextID             atomic.Int64

	mu          sync.Mutex
	slots       []*clientSlot
	maximumHead uint64

	stop     chan struct{}
	stopOnce sync.Once
	healthWG sync.WaitGroup
}

// NewRPC builds an RPC pool and starts its background health monitor.
func NewRPC(ctx context.Context, opts RPCOptions) (*RPC, error) {
	if len(opts.URLs) == 0 || len(opts.URLs) > 64 {
		return nil, errors.New("source: between 1 and 64 RPC URLs are required")
	}
	timeout := opts.Timeout
	if timeout <= 0 || timeout > 2*time.Second {
		timeout = 2 * time.Second
	}
	stale := opts.StaleThreshold
	if stale <= 0 {
		stale = 50
	}
	healthInterval := opts.HealthInterval
	if healthInterval <= 0 {
		healthInterval = 30 * time.Second
	}
	receiptConc := opts.ReceiptConcurrency
	if receiptConc <= 0 {
		receiptConc = 10
	}
	if receiptConc > 16 {
		return nil, errors.New("source: receipt concurrency above 16")
	}
	now := opts.now
	if now == nil {
		now = time.Now
	}
	hc := opts.httpClient
	if hc == nil {
		hc = &http.Client{}
	}
	clientCopy := *hc
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	slots := make([]*clientSlot, len(opts.URLs))
	for i, u := range opts.URLs {
		slots[i] = &clientSlot{url: u}
	}
	s := &RPC{
		logger:             logger,
		client:             &clientCopy,
		timeout:            timeout,
		staleThreshold:     stale,
		receiptConcurrency: receiptConc,
		now:                now,
		slots:              slots,
		stop:               make(chan struct{}),
	}
	// Initial health check + periodic re-rank. Until the first check
	// completes every node is treated as healthy in slot order.
	s.runHealthCheck(ctx)
	s.healthWG.Add(1)
	go func() {
		defer s.healthWG.Done()
		t := time.NewTicker(healthInterval)
		defer t.Stop()
		for {
			select {
			case <-s.stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				s.runHealthCheck(ctx)
			}
		}
	}()
	return s, nil
}

// Close stops the health monitor and waits for it to exit.
func (s *RPC) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.healthWG.Wait()
	s.client.CloseIdleConnections()
}

// Head returns the highest eth_blockNumber seen, with failover.
func (s *RPC) Head(ctx context.Context) (uint64, error) {
	raw, idx, err := s.request(ctx, s.timeout, 0, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	height, err := decodeBlockNumber(raw)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	sl := s.slots[idx]
	sl.lastBlock = max(sl.lastBlock, height)
	s.maximumHead = max(s.maximumHead, height)
	height = s.maximumHead
	s.mu.Unlock()
	return height, nil
}

func decodeBlockNumber(raw json.RawMessage) (uint64, error) {
	var hexStr string
	if err := json.Unmarshal(raw, &hexStr); err != nil {
		return 0, fmt.Errorf("eth_blockNumber decode: %w", err)
	}
	return parseHexUint64(hexStr)
}

// EVMBlock is the header and transaction senders of one EVM block.
type EVMBlock struct {
	Number       uint64
	Hash         common.Hash
	ParentHash   common.Hash
	Time         int64
	Transactions []EVMTx
}

// EVMTx is one body transaction.
type EVMTx struct {
	Hash common.Hash
	From common.Address
}

// Block returns block n with its transaction senders.
func (s *RPC) Block(ctx context.Context, n uint64) (*EVMBlock, error) {
	w, err := s.getBlock(ctx, n)
	if err != nil {
		return nil, err
	}
	number, err := parseHexUint64(w.Number)
	if err != nil {
		return nil, fmt.Errorf("block %d number: %w", n, err)
	}
	if number != n {
		return nil, fmt.Errorf("block %d: node served block %d", n, number)
	}
	ts, err := parseHexUint64(w.Timestamp)
	if err != nil || ts > 1<<62 {
		return nil, fmt.Errorf("block %d timestamp %q", n, w.Timestamp)
	}
	b := &EVMBlock{Number: n, Hash: w.Hash, ParentHash: w.ParentHash, Time: int64(ts), Transactions: make([]EVMTx, 0, len(w.Transactions))}
	for _, tx := range w.Transactions {
		if !common.IsHexAddress(tx.From) || len(common.FromHex(tx.Hash)) != common.HashLength {
			return nil, fmt.Errorf("block %d: transaction %q without sender", n, tx.Hash)
		}
		b.Transactions = append(b.Transactions, EVMTx{Hash: common.HexToHash(tx.Hash), From: common.HexToAddress(tx.From)})
	}
	return b, nil
}

// Logs runs eth_getLogs over at most MaxLogRange blocks, halving the range on
// a size refusal, then fetches the blocks of the logs in parallel for
// timestamps and transaction senders.
func (s *RPC) Logs(ctx context.Context, opts FetchOptions) (FetchResult, error) {
	if len(opts.MonitoredAddresses) == 0 || opts.FromBlock < 0 || opts.ToBlock < opts.FromBlock || opts.ToBlock-opts.FromBlock >= MaxLogRange {
		return FetchResult{}, fmt.Errorf("source: invalid log range %d-%d", opts.FromBlock, opts.ToBlock)
	}
	timeout := s.timeout
	if opts.Historical {
		timeout = bulkTimeout
	}
	wire, err := s.getLogs(ctx, timeout, opts.MonitoredAddresses, uint64(opts.FromBlock), uint64(opts.ToBlock))
	if err != nil {
		return FetchResult{}, err
	}
	res := FetchResult{BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
	if len(wire) == 0 {
		return res, nil
	}
	logs := make([]ethtypes.Log, 0, len(wire))
	uniqueBlocks := map[uint64]struct{}{}
	for _, w := range wire {
		l, err := w.toLog()
		if err != nil {
			return FetchResult{}, err
		}
		if l.BlockNumber < uint64(opts.FromBlock) || l.BlockNumber > uint64(opts.ToBlock) {
			return FetchResult{}, fmt.Errorf("source: log of block %d outside %d-%d", l.BlockNumber, opts.FromBlock, opts.ToBlock)
		}
		logs = append(logs, l)
		uniqueBlocks[l.BlockNumber] = struct{}{}
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].BlockNumber != logs[j].BlockNumber {
			return logs[i].BlockNumber < logs[j].BlockNumber
		}
		return logs[i].Index < logs[j].Index
	})
	res.Logs = logs

	conc := opts.Concurrency
	if conc <= 0 || conc > s.receiptConcurrency {
		conc = s.receiptConcurrency
	}
	var mu sync.Mutex
	var firstErr error
	jobs := make(chan uint64)
	var wg sync.WaitGroup
	for worker := 0; worker < conc; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for bn := range jobs {
				blk, err := s.getBlock(ctx, bn)
				var ts uint64
				if err == nil {
					ts, err = parseHexUint64(blk.Timestamp)
				}
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("block %d: %w", bn, err)
					}
				} else {
					res.BlockTimestamps[bn] = int64(ts)
					for _, tx := range blk.Transactions {
						if tx.Hash != "" && tx.From != "" {
							res.TxFromMap[strings.ToLower(tx.Hash)] = strings.ToLower(tx.From)
						}
					}
				}
				mu.Unlock()
			}
		}()
	}
	for bn := range uniqueBlocks {
		select {
		case jobs <- bn:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return FetchResult{}, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return FetchResult{}, firstErr
	}
	return res, nil
}

// getLogs splits [from,to] in halves while the node refuses it by size; a
// single block that is still refused is an error.
func (s *RPC) getLogs(ctx context.Context, timeout time.Duration, addresses []string, from, to uint64) ([]ethLogWire, error) {
	filter := map[string]any{
		"fromBlock": fmt.Sprintf("0x%x", from),
		"toBlock":   fmt.Sprintf("0x%x", to),
		"address":   addresses,
	}
	// Only a node that holds block to answers: a node behind it would serve
	// the blocks it lacks with no logs.
	raw, _, err := s.request(ctx, timeout, to, "eth_getLogs", filter)
	if errors.Is(err, ErrResponseSize) && to > from {
		mid := from + (to-from)/2
		low, err := s.getLogs(ctx, timeout, addresses, from, mid)
		if err != nil {
			return nil, err
		}
		high, err := s.getLogs(ctx, timeout, addresses, mid+1, to)
		if err != nil {
			return nil, err
		}
		return append(low, high...), nil
	}
	if err != nil {
		return nil, err
	}
	var wire []ethLogWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("eth_getLogs decode: %w", err)
	}
	return wire, nil
}

// Call runs eth_call of data at to on the latest block.
func (s *RPC) Call(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	return s.call(ctx, to, data, nil)
}

// CallAt runs eth_call of data at to on block height. A node that no longer
// holds the state of height answers decode.ErrStatePruned.
func (s *RPC) CallAt(ctx context.Context, to common.Address, data []byte, height uint64) ([]byte, error) {
	return s.call(ctx, to, data, new(big.Int).SetUint64(height))
}

// BlockNumber is Head under the name rest.SpotNode reads.
func (s *RPC) BlockNumber(ctx context.Context) (uint64, error) {
	return s.Head(ctx)
}

// BalanceAt runs eth_getBalance of account at blockNumber; nil is the latest
// block.
func (s *RPC) BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error) {
	block, need, err := blockTag(blockNumber)
	if err != nil {
		return nil, err
	}
	raw, _, err := s.request(ctx, s.timeout, need, "eth_getBalance", account.Hex(), block)
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(raw, &hexStr); err != nil {
		return nil, fmt.Errorf("eth_getBalance decode: %w", err)
	}
	v, ok := new(big.Int).SetString(strings.TrimPrefix(strings.TrimPrefix(hexStr, "0x"), "0X"), 16)
	if !ok || v.Sign() < 0 {
		return nil, fmt.Errorf("eth_getBalance: invalid quantity %q", hexStr)
	}
	return v, nil
}

// CallContract runs eth_call of call.Data at call.To on blockNumber; nil is
// the latest block. The other CallMsg fields are not sent.
func (s *RPC) CallContract(ctx context.Context, call ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
	if call.To == nil {
		return nil, errors.New("source: eth_call without a target")
	}
	return s.call(ctx, *call.To, call.Data, blockNumber)
}

// blockTag is the block parameter of n, nil the latest block, and the height
// a node must hold to answer it, zero for the latest block.
func blockTag(n *big.Int) (string, uint64, error) {
	if n == nil {
		return "latest", 0, nil
	}
	if n.Sign() < 0 || !n.IsUint64() {
		return "", 0, fmt.Errorf("source: invalid block number %s", n)
	}
	return fmt.Sprintf("0x%x", n.Uint64()), n.Uint64(), nil
}

func (s *RPC) call(ctx context.Context, to common.Address, data []byte, blockNumber *big.Int) ([]byte, error) {
	block, need, err := blockTag(blockNumber)
	if err != nil {
		return nil, err
	}
	raw, _, err := s.request(ctx, s.timeout, need, "eth_call", map[string]string{"to": to.Hex(), "data": "0x" + common.Bytes2Hex(data)}, block)
	if err != nil {
		return nil, err
	}
	var hexStr string
	if err := json.Unmarshal(raw, &hexStr); err != nil {
		return nil, fmt.Errorf("eth_call decode: %w", err)
	}
	return common.FromHex(hexStr), nil
}

// ── JSON-RPC wire types ────────────────────────────────────────────

type ethLogWire struct {
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	Data        string         `json:"data"`
	BlockNumber string         `json:"blockNumber"`
	BlockHash   common.Hash    `json:"blockHash"`
	TxHash      common.Hash    `json:"transactionHash"`
	TxIndex     string         `json:"transactionIndex"`
	LogIndex    string         `json:"logIndex"`
	Removed     bool           `json:"removed"`
}

func (w ethLogWire) toLog() (ethtypes.Log, error) {
	bn, err := parseHexUint64(w.BlockNumber)
	if err != nil {
		return ethtypes.Log{}, fmt.Errorf("log blockNumber: %w", err)
	}
	idx, err := parseHexUint64(w.LogIndex)
	if err != nil || idx > 1<<31 {
		return ethtypes.Log{}, fmt.Errorf("log logIndex %q", w.LogIndex)
	}
	txIdx, err := parseHexUint64(w.TxIndex)
	if err != nil || txIdx > 1<<31 {
		return ethtypes.Log{}, fmt.Errorf("log transactionIndex %q", w.TxIndex)
	}
	if w.Removed {
		return ethtypes.Log{}, fmt.Errorf("log %d/%d marked removed", bn, idx)
	}
	return ethtypes.Log{
		Address:     w.Address,
		Topics:      w.Topics,
		Data:        common.FromHex(w.Data),
		BlockNumber: bn,
		BlockHash:   w.BlockHash,
		TxHash:      w.TxHash,
		TxIndex:     uint(txIdx),
		Index:       uint(idx),
	}, nil
}

type ethBlockWire struct {
	Number       string      `json:"number"`
	Hash         common.Hash `json:"hash"`
	ParentHash   common.Hash `json:"parentHash"`
	Timestamp    string      `json:"timestamp"`
	Transactions []ethTxWire `json:"transactions"`
}

type ethTxWire struct {
	Hash string `json:"hash"`
	From string `json:"from"`
}

func (s *RPC) getBlock(ctx context.Context, height uint64) (*ethBlockWire, error) {
	raw, _, err := s.request(ctx, s.timeout, height, "eth_getBlockByNumber", fmt.Sprintf("0x%x", height), true)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("eth_getBlockByNumber %d: null result", height)
	}
	var blk ethBlockWire
	if err := json.Unmarshal(raw, &blk); err != nil {
		return nil, fmt.Errorf("eth_getBlockByNumber decode: %w", err)
	}
	return &blk, nil
}

// ── Pool failover + health ─────────────────────────────────────────

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
	ID      int64  `json:"id"`
}

type jsonRPCResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

// revertCode is the JSON-RPC error code of a reverted eth_call, whose data is
// the revert data.
const revertCode = 3

// request sends a JSON-RPC request to the healthy nodes in rank order until
// one answers, and returns the answer and the slot of the node. With need
// above zero only a node that holds block need is asked: one whose last seen
// head is below need is asked for its head first and skipped while it is
// still behind. A size refusal is the request's fault and a revert is the
// state's answer, not the node's health: both return at once without a
// failure mark.
func (s *RPC) request(ctx context.Context, timeout time.Duration, need uint64, method string, params ...any) (json.RawMessage, int, error) {
	order := s.rankedIndices()
	if len(order) == 0 {
		return nil, 0, errors.New("source: no healthy RPC nodes")
	}
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: s.nextID.Add(1)})
	if err != nil {
		return nil, 0, err
	}
	var lastErr error
	pruned, behind := 0, 0
	for _, idx := range order {
		if need > 0 {
			holds, err := s.holds(ctx, idx, need)
			if ctx.Err() != nil {
				return nil, 0, ctx.Err()
			}
			if err != nil {
				lastErr = err
				s.markFailure(idx, err)
				continue
			}
			if !holds {
				behind++
				continue
			}
		}
		t0 := s.now()
		result, err := s.doRequest(ctx, timeout, s.slotURL(idx), body)
		if err == nil {
			s.markSuccess(idx, s.now().Sub(t0).Milliseconds())
			return result, idx, nil
		}
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		var revert *decode.RevertError
		if errors.Is(err, ErrResponseSize) || errors.As(err, &revert) {
			return nil, 0, err
		}
		lastErr = err
		if errors.Is(err, decode.ErrStatePruned) {
			// A pruned height is the request's age, not the node's health.
			pruned++
			continue
		}
		s.markFailure(idx, err)
	}
	if pruned == len(order) {
		return nil, 0, lastErr
	}
	if behind == len(order) {
		return nil, 0, fmt.Errorf("%w: %s needs block %d", ErrNodeBehind, method, need)
	}
	if behind > 0 {
		lastErr = fmt.Errorf("%w (%d node(s) behind block %d)", lastErr, behind, need)
	}
	return nil, 0, fmt.Errorf("source: all %d healthy node(s) failed for %s: %w", len(order), method, lastErr)
}

// holds reports whether the node of slot idx holds block need: at once when
// its last seen head is at or above need, else by asking it for its head.
// A node's head never goes back, so a head seen at or above need stays a
// proof that it holds the block.
func (s *RPC) holds(ctx context.Context, idx int, need uint64) (bool, error) {
	s.mu.Lock()
	sl := s.slots[idx]
	seen, url := sl.lastBlock, sl.url
	s.mu.Unlock()
	if seen >= need {
		return true, nil
	}
	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: "eth_blockNumber", Params: []any{}, ID: s.nextID.Add(1)})
	if err != nil {
		return false, err
	}
	raw, err := s.doRequest(ctx, s.timeout, url, body)
	if err != nil {
		return false, err
	}
	head, err := decodeBlockNumber(raw)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	sl.lastBlock = max(sl.lastBlock, head)
	s.maximumHead = max(s.maximumHead, head)
	s.mu.Unlock()
	return head >= need, nil
}

// blockParam is the position of the block parameter of the state reads that
// Transport routes to a node holding that block.
var blockParam = map[string]int{"eth_call": 1, "eth_getBalance": 1, "eth_getCode": 1, "eth_getTransactionCount": 1, "eth_getStorageAt": 2, "eth_getBlockByNumber": 0}

// Transport is an http.RoundTripper that answers each single JSON-RPC request
// through the pool, with its failover and, for a state read at a block
// number, only from a node that holds that block. It lets a go-ethereum
// client (ethclient over rpc.WithHTTPClient) use the pool; the request URL is
// ignored. A batch request is refused.
func (s *RPC) Transport() http.RoundTripper { return poolTransport{s} }

type poolTransport struct{ s *RPC }

func (t poolTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Body == nil {
		return nil, errors.New("source: json-rpc request without a body")
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxResponseBytes+1))
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%w: request above %d bytes", ErrResponseSize, maxResponseBytes)
	}
	var req struct {
		ID     json.RawMessage   `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("source: transport accepts single json-rpc requests: %w", err)
	}
	var need uint64
	if at, ok := blockParam[req.Method]; ok && at < len(req.Params) {
		var tag string
		if json.Unmarshal(req.Params[at], &tag) == nil {
			if h, err := parseHexUint64(tag); err == nil {
				need = h
			}
		}
	}
	params := make([]any, len(req.Params))
	for i, p := range req.Params {
		params[i] = p
	}
	out := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	raw, _, err := t.s.request(r.Context(), t.s.timeout, need, req.Method, params...)
	if err != nil {
		if ctxErr := r.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		out["error"] = map[string]any{"code": -32000, "message": err.Error()}
	} else {
		out["result"] = raw
	}
	body, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		Status:        "200 OK",
		StatusCode:    http.StatusOK,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       r,
	}, nil
}

func (s *RPC) doRequest(ctx context.Context, timeout time.Duration, url string, body []byte) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return nil, fmt.Errorf("%w: http %d", ErrResponseSize, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source: http %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%w: response above %d bytes", ErrResponseSize, maxResponseBytes)
	}
	var out jsonRPCResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("source: json-rpc decode: %w", err)
	}
	if out.Error != nil {
		if sizeRefusal(out.Error.Message) {
			return nil, fmt.Errorf("%w: %s", ErrResponseSize, out.Error.Message)
		}
		if prunedRefusal(out.Error.Message) {
			return nil, fmt.Errorf("%w: %s", decode.ErrStatePruned, out.Error.Message)
		}
		if out.Error.Code == revertCode {
			var data string
			if err := json.Unmarshal(out.Error.Data, &data); err != nil {
				return nil, fmt.Errorf("source: json-rpc revert without hex data: %s", out.Error.Message)
			}
			raw, err := hexutil.Decode(data)
			if err != nil {
				return nil, fmt.Errorf("source: json-rpc revert data: %w", err)
			}
			return nil, &decode.RevertError{Data: raw}
		}
		return nil, fmt.Errorf("source: json-rpc error %d: %s", out.Error.Code, out.Error.Message)
	}
	return out.Result, nil
}

// prunedRefusal matches the node refusals of a call at a height whose state
// the node no longer holds.
func prunedRefusal(message string) bool {
	m := strings.ToLower(message)
	for _, s := range []string{"version does not exist", "pruned", "not available", "failed to load state", "missing trie node"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// sizeRefusal matches the node refusals of a query by its size: "block range
// too large (%d), maximum allowed is %d blocks", "log query rate limit
// exceeded for large queries" and the common "too many results".
func sizeRefusal(message string) bool {
	m := strings.ToLower(message)
	return strings.Contains(m, "too large") || strings.Contains(m, "limit exceeded") || strings.Contains(m, "too many")
}

func (s *RPC) slotURL(idx int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slots[idx].url
}

// rankedIndices returns non-stale slot indices, best-first: (failures asc,
// latency asc, index asc).
func (s *RPC) rankedIndices() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	healthy := make([]int, 0, len(s.slots))
	for i, sl := range s.slots {
		if !sl.isStale {
			healthy = append(healthy, i)
		}
	}
	sort.SliceStable(healthy, func(a, b int) bool {
		sa, sb := s.slots[healthy[a]], s.slots[healthy[b]]
		if sa.consecutiveFailures != sb.consecutiveFailures {
			return sa.consecutiveFailures < sb.consecutiveFailures
		}
		la, lb := sa.recentLatencyMs, sb.recentLatencyMs
		if la == 0 {
			la = int64(1) << 62
		}
		if lb == 0 {
			lb = int64(1) << 62
		}
		if la != lb {
			return la < lb
		}
		return healthy[a] < healthy[b]
	})
	return healthy
}

func (s *RPC) markSuccess(idx int, latencyMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.slots[idx]
	sl.consecutiveFailures = 0
	latencyMs = max(latencyMs, 1)
	if sl.recentLatencyMs == 0 {
		sl.recentLatencyMs = latencyMs
	} else {
		sl.recentLatencyMs = (sl.recentLatencyMs*7 + latencyMs*3) / 10
	}
	sl.lastSeenAt = s.now().UnixMilli()
}

func (s *RPC) markFailure(idx int, err error) {
	s.mu.Lock()
	sl := s.slots[idx]
	sl.consecutiveFailures++
	url, failures := sl.url, sl.consecutiveFailures
	s.mu.Unlock()
	s.logger.Warn("source: node request failed", slog.String("url", url), slog.Int("failures", failures), slog.String("err", err.Error()))
}

// runHealthCheck pings eth_blockNumber on every node, computes the median, and
// quarantines nodes >staleThreshold behind median or with 3+ failures.
func (s *RPC) runHealthCheck(ctx context.Context) {
	type probe struct {
		head    uint64
		latency int64
		ok      bool
	}
	s.mu.Lock()
	urls := make([]string, len(s.slots))
	for i, sl := range s.slots {
		urls[i] = sl.url
	}
	s.mu.Unlock()

	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: "eth_blockNumber", Params: []any{}, ID: s.nextID.Add(1)})
	if err != nil {
		return
	}
	probes := make([]probe, len(urls))
	var wg sync.WaitGroup
	for i, url := range urls {
		wg.Add(1)
		go func(i int, url string) {
			defer wg.Done()
			t0 := s.now()
			raw, err := s.doRequest(ctx, s.timeout, url, body)
			if err != nil {
				return
			}
			var hexStr string
			if err := json.Unmarshal(raw, &hexStr); err != nil {
				return
			}
			h, err := parseHexUint64(hexStr)
			if err != nil {
				return
			}
			probes[i] = probe{head: h, latency: max(s.now().Sub(t0).Milliseconds(), 1), ok: true}
		}(i, url)
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	heads := make([]uint64, 0, len(probes))
	for i, p := range probes {
		sl := s.slots[i]
		if p.ok {
			sl.lastBlock = p.head
			sl.lastSeenAt = s.now().UnixMilli()
			if sl.recentLatencyMs == 0 {
				sl.recentLatencyMs = p.latency
			} else {
				sl.recentLatencyMs = (sl.recentLatencyMs*7 + p.latency*3) / 10
			}
			sl.consecutiveFailures = 0
			heads = append(heads, p.head)
		} else {
			sl.consecutiveFailures++
		}
	}
	if len(heads) == 0 {
		s.logger.Error("source: no nodes responded to health check")
		return
	}
	sort.Slice(heads, func(a, b int) bool { return heads[a] < heads[b] })
	median := heads[len(heads)/2]
	for _, sl := range s.slots {
		wasStale := sl.isStale
		behind := sl.lastSeenAt != 0 && median > sl.lastBlock && median-sl.lastBlock > uint64(s.staleThreshold)
		sl.isStale = sl.consecutiveFailures >= 3 || behind
		if sl.isStale && !wasStale {
			s.logger.Warn("source: node quarantined as stale", slog.String("url", sl.url), slog.Uint64("lastBlock", sl.lastBlock), slog.Uint64("median", median))
		} else if !sl.isStale && wasStale {
			s.logger.Info("source: node recovered", slog.String("url", sl.url), slog.Uint64("lastBlock", sl.lastBlock))
		}
	}
}

func parseHexUint64(s string) (uint64, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"), 16)
	if !ok || v.Sign() < 0 || !v.IsUint64() {
		return 0, fmt.Errorf("invalid hex uint64 %q", s)
	}
	return v.Uint64(), nil
}
