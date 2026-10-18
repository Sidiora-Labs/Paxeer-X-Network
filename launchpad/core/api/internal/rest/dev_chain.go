package rest

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
)

// errChainReferenceUnavailable reports that a block hash or event position
// the development indexer does not persist could not be resolved from chain.
var errChainReferenceUnavailable = errors.New("chain reference unavailable")

// marketCreatedTopic is EventEmitter.MarketCreated, the event the development
// indexer turns into indexer.pools rows (topic1 is the poolId).
var marketCreatedTopic = crypto.Keccak256Hash([]byte("MarketCreated(bytes32,address,address,address,address,uint256,uint256)")).Hex()

const developmentPositionCacheSize = 8192

// developmentChain resolves the chain references the development indexer
// does not store (block hashes, transaction indexes, creation log indexes)
// from the chain RPC. Resolved positions are immutable and cached; the head
// hash is cached per height.
type developmentChain struct {
	client     *rpc.Client
	mu         sync.Mutex
	headHeight int64
	headHash   string
	positions  map[string]accounting.SourcePosition
	// served is the newest head watermark already served; it answers while
	// the hash of a newer indexer head is still being resolved.
	served *store.ChainWatermark
	// pending is the single in-flight head hash resolution.
	pending *headResolution
	// failed reports that the latest completed resolution failed: the chain
	// RPC is down, so no older watermark stands in for the head.
	failed bool
}

// headResolution is one detached eth_getBlockByNumber for an indexer head.
type headResolution struct {
	height int64
	done   chan struct{}
	hash   string
	err    error
}

// developmentHeadReserve is the part of the request budget kept for the
// store reads that follow the head watermark. Every development read runs
// under the 400ms ResponseMetadata deadline; an uncached head hash used to
// consume it on the chain RPC and leave the next store transaction an
// expired context (a 503 on every indexer head advance).
const developmentHeadReserve = 200 * time.Millisecond

func newDevelopmentChain(ctx context.Context, rawURL string) (*developmentChain, error) {
	c := &developmentChain{positions: map[string]accounting.SourcePosition{}}
	if rawURL == "" {
		return c, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("development chain RPC URL invalid")
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{ResponseHeaderTimeout: 3 * time.Second, MaxIdleConnsPerHost: 8}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	c.client, err = rpc.DialOptions(ctx, rawURL, rpc.WithHTTPClient(client))
	if err != nil {
		return nil, err
	}
	return c, nil
}

// rpcContext detaches chain lookups from the 400ms public read budget so a
// cold cache still fills (bounded at 3s); the results are cached.
func rpcContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
}

// fetchBlockHash reads one block hash from the chain RPC, bounded at 3s and
// detached from the caller's cancellation.
func (c *developmentChain) fetchBlockHash(ctx context.Context, height int64) (string, error) {
	rctx, cancel := rpcContext(ctx)
	defer cancel()
	var block *struct {
		Hash   string `json:"hash"`
		Number string `json:"number"`
	}
	if err := c.client.CallContext(rctx, &block, "eth_getBlockByNumber", hexutil.EncodeUint64(uint64(height)), false); err != nil || block == nil {
		return "", errChainReferenceUnavailable
	}
	number, err := hexutil.DecodeUint64(block.Number)
	if err != nil || number != uint64(height) || !validHash(block.Hash) {
		return "", errChainReferenceUnavailable
	}
	return strings.ToLower(block.Hash), nil
}

// resolve runs one head resolution and caches a successful hash unless a
// newer head was cached meanwhile.
func (c *developmentChain) resolve(p *headResolution) {
	p.hash, p.err = c.fetchBlockHash(context.Background(), p.height)
	c.mu.Lock()
	c.failed = p.err != nil
	if p.err == nil && p.height >= c.headHeight {
		c.headHeight, c.headHash = p.height, p.hash
	}
	if c.pending == p {
		c.pending = nil
	}
	c.mu.Unlock()
	close(p.done)
}

// headWatermark is the watermark for the development indexer head. A cached
// head answers immediately. An uncached head starts (or joins) one detached
// resolution and waits for it only while the request budget still leaves
// developmentHeadReserve for the store reads; past that, the newest
// watermark already served (an older, still valid height/hash pair) answers
// and the resolution keeps filling the cache for the next request. Without a
// chain RPC the watermark is not configured. A failed resolution, or one that
// misses the budget with no served watermark (or after the previous
// resolution failed), is a transient dependency failure.
func (c *developmentChain) headWatermark(ctx context.Context, head store.DevelopmentHead) (store.ChainWatermark, error) {
	if c == nil || c.client == nil {
		return store.ChainWatermark{}, ErrDependencyNotConfigured
	}
	if head.Height <= 0 {
		return store.ChainWatermark{}, errChainReferenceUnavailable
	}
	c.mu.Lock()
	if c.headHeight == head.Height {
		hash := c.headHash
		c.mu.Unlock()
		return c.serve(head, hash)
	}
	var fallback *store.ChainWatermark
	if height := servedHeight(c.served); !c.failed && height > 0 && height < head.Height {
		copied := *c.served
		fallback = &copied
	}
	p := c.pending
	if p == nil || p.height != head.Height {
		p = &headResolution{height: head.Height, done: make(chan struct{})}
		c.pending = p
		go c.resolve(p)
	}
	c.mu.Unlock()
	wait := 3 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		wait = time.Until(deadline) - developmentHeadReserve
	}
	if wait > 0 {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-p.done:
			if p.err != nil {
				return store.ChainWatermark{}, errChainReferenceUnavailable
			}
			return c.serve(head, p.hash)
		case <-timer.C:
		case <-ctx.Done():
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return store.ChainWatermark{}, errChainReferenceUnavailable
}

// serve builds the head watermark and remembers it as the fallback unless a
// newer one was already served.
func (c *developmentChain) serve(head store.DevelopmentHead, hash string) (store.ChainWatermark, error) {
	wm, err := store.DevelopmentWatermark(head, hash)
	if err != nil {
		return wm, err
	}
	c.mu.Lock()
	if servedHeight(c.served) <= head.Height {
		copied := wm
		c.served = &copied
	}
	c.mu.Unlock()
	return wm, nil
}

// servedHeight is the height of a served watermark, -1 when there is none.
func servedHeight(wm *store.ChainWatermark) int64 {
	if wm == nil {
		return -1
	}
	height, err := strconv.ParseInt(wm.Height, 10, 64)
	if err != nil {
		return -1
	}
	return height
}

// watermark is the development indexer cursor with its block hash.
func (c *developmentChain) watermark(ctx context.Context, st *store.Store) (store.ChainWatermark, error) {
	if c == nil || c.client == nil {
		return store.ChainWatermark{}, ErrDependencyNotConfigured
	}
	head, err := st.DevelopmentIndexerHead(ctx)
	if err != nil {
		return store.ChainWatermark{}, err
	}
	return c.headWatermark(ctx, head)
}

func (c *developmentChain) cached(key string) (accounting.SourcePosition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.positions[key]
	return p, ok
}

func (c *developmentChain) remember(key string, p accounting.SourcePosition) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.positions) >= developmentPositionCacheSize {
		c.positions = map[string]accounting.SourcePosition{}
	}
	c.positions[key] = p
}

type rpcLog struct {
	TransactionHash  string `json:"transactionHash"`
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	TransactionIndex string `json:"transactionIndex"`
	LogIndex         string `json:"logIndex"`
	Removed          bool   `json:"removed"`
}

type rpcReceipt struct {
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	TransactionIndex string `json:"transactionIndex"`
}

func position(height uint64, txIndex, logIndex uint64, txHash, blockHash string) accounting.SourcePosition {
	return accounting.SourcePosition{ChainID: "125", Height: strconv.FormatUint(height, 10), TransactionIndex: strconv.FormatUint(txIndex, 10), LogIndex: strconv.FormatUint(logIndex, 10), TransactionHash: strings.ToLower(txHash), BlockHash: strings.ToLower(blockHash)}
}

// creationPositions resolves each pool's MarketCreated log (topic1 = poolId)
// in its creation block. Pools whose log cannot be resolved are omitted.
func (c *developmentChain) creationPositions(ctx context.Context, pools []store.DevelopmentPool) map[string]accounting.SourcePosition {
	out := map[string]accounting.SourcePosition{}
	var pending []store.DevelopmentPool
	for _, p := range pools {
		if cached, ok := c.cached("creation:" + p.Pool); ok {
			out[p.Pool] = cached
		} else {
			pending = append(pending, p)
		}
	}
	if len(pending) == 0 || c == nil || c.client == nil {
		return out
	}
	results := make([][]rpcLog, len(pending))
	batch := make([]rpc.BatchElem, len(pending))
	for i, p := range pending {
		block := hexutil.EncodeUint64(uint64(p.CreatedHeight))
		batch[i] = rpc.BatchElem{Method: "eth_getLogs", Args: []any{map[string]any{"fromBlock": block, "toBlock": block, "topics": []any{marketCreatedTopic, p.PoolID}}}, Result: &results[i]}
	}
	rctx, cancel := rpcContext(ctx)
	defer cancel()
	if c.client.BatchCallContext(rctx, batch) != nil {
		return out
	}
	for i, p := range pending {
		if batch[i].Error != nil {
			continue
		}
		var found *accounting.SourcePosition
		matches := 0
		for _, l := range results[i] {
			if l.Removed || !strings.EqualFold(l.TransactionHash, p.CreationTransaction) {
				continue
			}
			height, errHeight := hexutil.DecodeUint64(l.BlockNumber)
			txIndex, errTx := hexutil.DecodeUint64(l.TransactionIndex)
			logIndex, errLog := hexutil.DecodeUint64(l.LogIndex)
			if errHeight != nil || errTx != nil || errLog != nil || height != uint64(p.CreatedHeight) || !validHash(l.BlockHash) {
				continue
			}
			pos := position(height, txIndex, logIndex, l.TransactionHash, l.BlockHash)
			found = &pos
			matches++
		}
		if matches == 1 && accounting.ValidateSourcePosition(*found) == nil {
			c.remember("creation:"+p.Pool, *found)
			out[p.Pool] = *found
		}
	}
	return out
}

func eventKey(ref store.DevelopmentEventRef) string {
	return "event:" + ref.TransactionHash + ":" + strconv.FormatInt(ref.LogIndex, 10)
}

// eventPositions completes indexed event references with the transaction
// index and block hash from their receipts. Unresolvable events are omitted.
func (c *developmentChain) eventPositions(ctx context.Context, refs []store.DevelopmentEventRef) map[string]accounting.SourcePosition {
	out := map[string]accounting.SourcePosition{}
	var pending []store.DevelopmentEventRef
	for _, ref := range refs {
		if cached, ok := c.cached(eventKey(ref)); ok {
			out[eventKey(ref)] = cached
		} else {
			pending = append(pending, ref)
		}
	}
	if len(pending) == 0 || c == nil || c.client == nil {
		return out
	}
	results := make([]*rpcReceipt, len(pending))
	batch := make([]rpc.BatchElem, len(pending))
	for i, ref := range pending {
		batch[i] = rpc.BatchElem{Method: "eth_getTransactionReceipt", Args: []any{ref.TransactionHash}, Result: &results[i]}
	}
	rctx, cancel := rpcContext(ctx)
	defer cancel()
	if c.client.BatchCallContext(rctx, batch) != nil {
		return out
	}
	for i, ref := range pending {
		receipt := results[i]
		if batch[i].Error != nil || receipt == nil {
			continue
		}
		height, errHeight := hexutil.DecodeUint64(receipt.BlockNumber)
		txIndex, errTx := hexutil.DecodeUint64(receipt.TransactionIndex)
		if errHeight != nil || errTx != nil || height != uint64(ref.Height) || !validHash(receipt.BlockHash) {
			continue
		}
		pos := position(height, txIndex, uint64(ref.LogIndex), ref.TransactionHash, receipt.BlockHash)
		if accounting.ValidateSourcePosition(pos) != nil {
			continue
		}
		c.remember(eventKey(ref), pos)
		out[eventKey(ref)] = pos
	}
	return out
}

func validHash(hash string) bool {
	if len(hash) != 66 || !strings.HasPrefix(hash, "0x") {
		return false
	}
	_, err := hexutil.Decode(hash)
	return err == nil
}
