package source

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// EVMOptions configures an EVM JSON-RPC source.
type EVMOptions struct {
	// RPCURLs is the validator RPC pool (>=1). A single URL gives the legacy
	// "rpc" mode; multiple URLs give load-balanced "rpc-getlogs".
	RPCURLs []string
	Logger  *slog.Logger
	// Name overrides the reported source name ("rpc" vs "rpc-getlogs").
	Name string
	// Timeout is the per-request HTTP timeout (default 5s).
	Timeout time.Duration
	// StaleThreshold quarantines a node more than this many blocks behind the
	// pool median (default 50). HealthInterval is the re-rank cadence (default
	// 30s). ReceiptConcurrency caps parallel block fetches (default 10).
	StaleThreshold     int
	HealthInterval     time.Duration
	ReceiptConcurrency int
	// now / httpClient are overridable for deterministic tests.
	now        func() time.Time
	httpClient *http.Client
}

type clientSlot struct {
	url                 string
	lastBlock           int64
	lastSeenAt          int64 // unix ms; 0 = never
	consecutiveFailures int
	recentLatencyMs     int64
	isStale             bool
	endpoint            *committedEndpoint
}

// EVMSource is an eth_getLogs-based LogSource + HeadSource with health-ranked
// failover across an RPC pool.
type EVMSource struct {
	name               string
	logger             *slog.Logger
	client             *http.Client
	timeout            time.Duration
	staleThreshold     int
	receiptConcurrency int
	now                func() time.Time
	nextID             atomic.Int64

	mu    sync.Mutex
	slots []*clientSlot

	stop            chan struct{}
	stopOnce        sync.Once
	healthWG        sync.WaitGroup
	lifecycle       *sourceLifecycle
	transportSource *CommittedSource
	maximumHead     int64
	shutdownErr     error
}

// NewEVM builds an EVMSource and starts its background health monitor.
func NewEVM(ctx context.Context, opts EVMOptions) (*EVMSource, error) {
	if len(opts.RPCURLs) == 0 || len(opts.RPCURLs) > 64 {
		return nil, fmt.Errorf("source: at least one RPC URL is required")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	if timeout > 2*time.Second {
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
		return nil, sourceError(InvalidError, "receipt-concurrency", nil)
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
	clientCopy.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("redirect denied") }
	hc = &clientCopy
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	name := opts.Name
	if name == "" {
		if len(opts.RPCURLs) > 1 {
			name = "rpc-getlogs"
		} else {
			name = "rpc"
		}
	}

	slots := make([]*clientSlot, len(opts.RPCURLs))
	for i, u := range opts.RPCURLs {
		slots[i] = &clientSlot{url: u, endpoint: &committedEndpoint{id: fmt.Sprintf("rpc-%d", i), url: u, permits: make(chan struct{}, 16), bulkPermits: make(chan struct{}, 4)}}
	}

	s := &EVMSource{
		name:               name,
		logger:             logger,
		client:             hc,
		timeout:            timeout,
		staleThreshold:     stale,
		receiptConcurrency: receiptConc,
		now:                now,
		slots:              slots,
		stop:               make(chan struct{}),
		lifecycle:          newSourceLifecycle(),
		transportSource:    &CommittedSource{client: hc, opts: CommittedOptions{LiveDeadline: timeout, BulkDeadline: 15 * time.Second, MaxResponseBytes: 32 << 20}},
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

// Name implements LogSource / HeadSource.
func (s *EVMSource) Name() string { return s.name }

// Close stops the health monitor and waits for it to exit.
func (s *EVMSource) Close() error {
	s.stopOnce.Do(func() { close(s.stop); s.shutdownErr = s.lifecycle.close() })
	if s.shutdownErr != nil {
		return s.shutdownErr
	}
	s.healthWG.Wait()
	s.client.CloseIdleConnections()
	return nil
}

// Head implements HeadSource via eth_blockNumber with failover.
func (s *EVMSource) Head(ctx context.Context) (int64, error) {
	raw, err := s.callWithFallback(ctx, "eth_blockNumber")
	if err != nil {
		return 0, err
	}
	var hexStr string
	if err := json.Unmarshal(raw, &hexStr); err != nil {
		return 0, fmt.Errorf("eth_blockNumber decode: %w", err)
	}
	height, err := parseHexInt64(hexStr)
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	s.maximumHead = max(s.maximumHead, height)
	height = s.maximumHead
	s.mu.Unlock()
	return height, nil
}

// FetchLogs implements LogSource: one ranged eth_getLogs, then parallel block
// fetches for timestamps + tx-from enrichment.
func (s *EVMSource) FetchLogs(ctx context.Context, opts FetchOptions) (FetchResult, error) {
	ctx, done, err := s.lifecycle.begin(ctx)
	if err != nil {
		return FetchResult{}, err
	}
	defer done()
	if opts.FromBlock < 0 || opts.ToBlock < opts.FromBlock || opts.ToBlock-opts.FromBlock >= 2000 {
		return FetchResult{}, sourceError(InvalidError, s.name, nil)
	}
	ctx = context.WithValue(ctx, sourceHistoricalKey{}, opts.Historical)
	filter := map[string]any{
		"fromBlock": fmt.Sprintf("0x%x", opts.FromBlock),
		"toBlock":   fmt.Sprintf("0x%x", opts.ToBlock),
	}
	if len(opts.MonitoredAddresses) > 0 {
		filter["address"] = opts.MonitoredAddresses
	}
	raw, err := s.callWithFallback(ctx, "eth_getLogs", filter)
	if err != nil {
		return FetchResult{}, err
	}
	var wire []ethLogWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return FetchResult{}, fmt.Errorf("eth_getLogs decode: %w", err)
	}

	res := FetchResult{
		BlockTimestamps: map[uint64]int64{},
		TxFromMap:       map[string]string{},
	}
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

	blocks := make([]uint64, 0, len(uniqueBlocks))
	for b := range uniqueBlocks {
		blocks = append(blocks, b)
	}

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
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					continue
				}
				ts, err := parseHexInt64(blk.Timestamp)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("block %d timestamp: %w", bn, err)
					}
					mu.Unlock()
					continue
				}
				mu.Lock()
				res.BlockTimestamps[bn] = ts
				for _, tx := range blk.Transactions {
					if tx.Hash == "" || tx.From == "" {
						continue
					}
					res.TxFromMap[strings.ToLower(tx.Hash)] = strings.ToLower(tx.From)
				}
				mu.Unlock()
			}
		}()
	}
	for _, bn := range blocks {
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

	// Drop tx-from entries that no Swap-like log needs, matching the TS scope.
	needed := map[string]struct{}{}
	for _, l := range logs {
		if len(l.Topics) > 0 && needsTxFrom(l.Topics[0].Hex()) {
			needed[strings.ToLower(l.TxHash.Hex())] = struct{}{}
		}
	}
	for h := range res.TxFromMap {
		if _, ok := needed[h]; !ok {
			delete(res.TxFromMap, h)
		}
	}
	return res, nil
}

// ── JSON-RPC wire types ────────────────────────────────────────────

type ethLogWire struct {
	Address     common.Address `json:"address"`
	Topics      []common.Hash  `json:"topics"`
	Data        string         `json:"data"`
	BlockNumber string         `json:"blockNumber"`
	TxHash      string         `json:"transactionHash"`
	LogIndex    string         `json:"logIndex"`
	Removed     bool           `json:"removed"`
}

func (w ethLogWire) toLog() (ethtypes.Log, error) {
	bn, err := parseHexUint64(w.BlockNumber)
	if err != nil {
		return ethtypes.Log{}, fmt.Errorf("log blockNumber: %w", err)
	}
	idx, err := parseHexUint64(w.LogIndex)
	if err != nil {
		return ethtypes.Log{}, fmt.Errorf("log logIndex: %w", err)
	}
	return ethtypes.Log{
		Address:     w.Address,
		Topics:      w.Topics,
		Data:        common.FromHex(w.Data),
		BlockNumber: bn,
		TxHash:      common.HexToHash(w.TxHash),
		Index:       uint(idx),
		Removed:     w.Removed,
	}, nil
}

type ethBlockWire struct {
	Timestamp    string      `json:"timestamp"`
	Transactions []ethTxWire `json:"transactions"`
}

type ethTxWire struct {
	Hash string `json:"hash"`
	From string `json:"from"`
}

func (s *EVMSource) getBlock(ctx context.Context, height uint64) (*ethBlockWire, error) {
	raw, err := s.callWithFallback(ctx, "eth_getBlockByNumber", fmt.Sprintf("0x%x", height), true)
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
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callWithFallback dispatches a JSON-RPC request to the best healthy node,
// rotating through the rest on failure. Errors only when every healthy node fails.
func (s *EVMSource) callWithFallback(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	order := s.rankedIndices()
	if len(order) == 0 {
		return nil, fmt.Errorf("source %s: no healthy clients available", s.name)
	}
	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: method, Params: params, ID: s.nextID.Add(1)})
	if err != nil {
		return nil, err
	}
	requestContext, cancel := context.WithCancel(ctx)
	defer cancel()
	type callResult struct {
		result json.RawMessage
		err    error
	}
	results := make(chan callResult, len(order))
	var wg sync.WaitGroup
	for _, idx := range order {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			t0 := s.now()
			result, err := s.doRequest(requestContext, s.slotURL(idx), body, method)
			if err != nil {
				if requestContext.Err() == nil {
					s.markFailure(idx, err)
				}
			} else {
				s.markSuccess(idx, s.now().Sub(t0).Milliseconds())
			}
			results <- callResult{result, err}
		}(idx)
	}
	defer func() { cancel(); wg.Wait() }()
	var lastErr error
	for range order {
		select {
		case result := <-results:
			if result.err == nil {
				return result.result, nil
			}
			lastErr = result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("source %s: all %d healthy client(s) failed for %s: %w", s.name, len(order), method, lastErr)
}

type sourceHistoricalKey struct{}

func (s *EVMSource) doRequest(ctx context.Context, url string, body []byte, method string) (json.RawMessage, error) {
	ctx, done, err := s.lifecycle.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	var request jsonRPCRequest
	if err := json.Unmarshal(body, &request); err != nil || request.Method != method {
		return nil, sourceError(InvalidError, s.name, err)
	}
	var endpoint *committedEndpoint
	s.mu.Lock()
	for _, slot := range s.slots {
		if slot.url == url {
			endpoint = slot.endpoint
			break
		}
	}
	s.mu.Unlock()
	if endpoint == nil {
		return nil, sourceError(IdentityError, s.name, nil)
	}
	bulk, _ := ctx.Value(sourceHistoricalKey{}).(bool)
	var result json.RawMessage
	if err := s.transportSource.rpc(ctx, endpoint, bulk, method, request.Params, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *EVMSource) slotURL(idx int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.slots[idx].url
}

// rankedIndices returns non-stale slot indices, best-first: (failures asc,
// latency asc, index asc).
func (s *EVMSource) rankedIndices() []int {
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

func (s *EVMSource) markSuccess(idx int, latencyMs int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sl := s.slots[idx]
	sl.consecutiveFailures = 0
	if sl.recentLatencyMs == 0 {
		sl.recentLatencyMs = latencyMs
	} else {
		sl.recentLatencyMs = (sl.recentLatencyMs*7 + latencyMs*3) / 10
	}
	sl.lastSeenAt = s.now().UnixMilli()
}

func (s *EVMSource) markFailure(idx int, err error) {
	s.mu.Lock()
	sl := s.slots[idx]
	sl.consecutiveFailures++
	url, failures := sl.url, sl.consecutiveFailures
	s.mu.Unlock()
	s.logger.Warn("source: node request failed", slog.String("url", url), slog.Int("failures", failures), slog.String("err", err.Error()))
}

// runHealthCheck pings eth_blockNumber on every node, computes the median, and
// quarantines nodes >staleThreshold behind median or with 3+ failures.
func (s *EVMSource) runHealthCheck(ctx context.Context) {
	type probe struct {
		head    int64
		latency int64
		ok      bool
	}
	s.mu.Lock()
	urls := make([]string, len(s.slots))
	for i, sl := range s.slots {
		urls[i] = sl.url
	}
	s.mu.Unlock()

	body, err := json.Marshal(jsonRPCRequest{JSONRPC: "2.0", Method: "eth_blockNumber", ID: s.nextID.Add(1)})
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
			raw, err := s.doRequest(ctx, url, body, "eth_blockNumber")
			if err != nil {
				return
			}
			var hexStr string
			if err := json.Unmarshal(raw, &hexStr); err != nil {
				return
			}
			h, err := parseHexInt64(hexStr)
			if err != nil {
				return
			}
			probes[i] = probe{head: h, latency: s.now().Sub(t0).Milliseconds(), ok: true}
		}(i, url)
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	heads := make([]int64, 0, len(probes))
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
		s.logger.Error("source: no nodes responded to health check", slog.String("name", s.name))
		return
	}
	sort.Slice(heads, func(a, b int) bool { return heads[a] < heads[b] })
	median := heads[len(heads)/2]
	for _, sl := range s.slots {
		wasStale := sl.isStale
		var blocksBehind int64
		if sl.lastSeenAt != 0 {
			blocksBehind = median - sl.lastBlock
		}
		sl.isStale = sl.consecutiveFailures >= 3 || (sl.lastSeenAt != 0 && blocksBehind > int64(s.staleThreshold))
		if sl.isStale && !wasStale {
			s.logger.Warn("source: node quarantined as stale", slog.String("url", sl.url), slog.Int64("lastBlock", sl.lastBlock), slog.Int64("median", median))
		} else if !sl.isStale && wasStale {
			s.logger.Info("source: node recovered", slog.String("url", sl.url), slog.Int64("lastBlock", sl.lastBlock))
		}
	}
}

func parseHexUint64(s string) (uint64, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"), 16)
	if !ok {
		return 0, fmt.Errorf("invalid hex %q", s)
	}
	return v.Uint64(), nil
}

func parseHexInt64(s string) (int64, error) {
	v, ok := new(big.Int).SetString(strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X"), 16)
	if !ok {
		return 0, fmt.Errorf("invalid hex %q", s)
	}
	return v.Int64(), nil
}
