// Package devreads serves the web's /v2 and /v3 portfolio read surface outside
// production (NODE_ENV != "production"). Production registers these paths only
// when the whole journey runtime boots (archive RPCs, mTLS identity, NATS, bound
// documents); development has none of that, so this package projects the dev
// pnl schema (pnl.user_positions / pnl.user_trades, folded by the swap webhook
// and the reconciler from indexer.swaps) through the production document
// builders (l16.ProjectPosition, l17.Aggregate) and validates every body with
// the shared v2 interface validators before writing it.
package devreads

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rpc"
)

// ErrChainUnavailable reports that no block anchor could be read from the RPC.
var ErrChainUnavailable = errors.New("devreads: chain anchor unavailable")

var blockHashPattern = regexp.MustCompile(`^0x[0-9a-fA-F]{64}$`)

// Block is a chain anchor exactly as the node reports it. The hash is the
// node's own "hash" field, never a locally recomputed header hash (Paxeer is a
// Cosmos-EVM chain whose header hashes do not match go-ethereum's RLP hash).
type Block struct {
	Number    uint64
	Hash      string
	Timestamp time.Time
}

// Chain reads block anchors over JSON-RPC and memoises them by height (block
// contents at a height are immutable once read; the head is cached briefly).
type Chain struct {
	client *rpc.Client

	mu       sync.Mutex
	byNumber map[uint64]Block
	head     Block
	headAt   time.Time
	now      func() time.Time
}

// headTTL bounds how long a "latest" read is reused.
const headTTL = 2 * time.Second

// maxCachedBlocks bounds the per-height memo.
const maxCachedBlocks = 1024

// DialChain builds a Chain over rawURL (http/https/ws). Dialling an HTTP
// endpoint does not perform I/O, so a node outage surfaces per request.
func DialChain(ctx context.Context, rawURL string) (*Chain, error) {
	if strings.TrimSpace(rawURL) == "" {
		return nil, errors.New("devreads: RPC_URL is empty")
	}
	client, err := rpc.DialContext(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return &Chain{client: client, byNumber: map[uint64]Block{}, now: time.Now}, nil
}

// Close releases the RPC client.
func (c *Chain) Close() {
	if c != nil && c.client != nil {
		c.client.Close()
	}
}

// Head returns the node's latest block.
func (c *Chain) Head(ctx context.Context) (Block, error) {
	if c == nil {
		return Block{}, ErrChainUnavailable
	}
	c.mu.Lock()
	if c.head.Hash != "" && c.now().Sub(c.headAt) < headTTL {
		b := c.head
		c.mu.Unlock()
		return b, nil
	}
	c.mu.Unlock()
	b, found, err := c.fetch(ctx, "latest")
	if err != nil {
		return Block{}, err
	}
	if !found {
		return Block{}, ErrChainUnavailable
	}
	c.mu.Lock()
	c.head, c.headAt = b, c.now()
	c.remember(b)
	c.mu.Unlock()
	return b, nil
}

// BlockAt returns the block at height n; found is false when the node does not
// serve it (pruned or not yet produced).
func (c *Chain) BlockAt(ctx context.Context, n uint64) (Block, bool, error) {
	if c == nil {
		return Block{}, false, ErrChainUnavailable
	}
	c.mu.Lock()
	if b, ok := c.byNumber[n]; ok {
		c.mu.Unlock()
		return b, true, nil
	}
	c.mu.Unlock()
	b, found, err := c.fetch(ctx, hexutil.EncodeUint64(n))
	if err != nil || !found {
		return Block{}, found, err
	}
	if b.Number != n {
		return Block{}, false, fmt.Errorf("%w: node answered height %d for %d", ErrChainUnavailable, b.Number, n)
	}
	c.mu.Lock()
	c.remember(b)
	c.mu.Unlock()
	return b, true, nil
}

func (c *Chain) remember(b Block) {
	if len(c.byNumber) >= maxCachedBlocks {
		c.byNumber = map[uint64]Block{}
	}
	c.byNumber[b.Number] = b
}

func (c *Chain) fetch(ctx context.Context, tag string) (Block, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var raw json.RawMessage
	if err := c.client.CallContext(ctx, &raw, "eth_getBlockByNumber", tag, false); err != nil {
		return Block{}, false, fmt.Errorf("%w: %v", ErrChainUnavailable, err)
	}
	if len(raw) == 0 || string(raw) == "null" {
		return Block{}, false, nil
	}
	var head struct {
		Number    *hexutil.Big `json:"number"`
		Hash      string       `json:"hash"`
		Timestamp *hexutil.Big `json:"timestamp"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return Block{}, false, fmt.Errorf("%w: %v", ErrChainUnavailable, err)
	}
	if head.Number == nil || head.Timestamp == nil || !blockHashPattern.MatchString(head.Hash) {
		return Block{}, false, fmt.Errorf("%w: incomplete block header", ErrChainUnavailable)
	}
	number, ts := (*big.Int)(head.Number), (*big.Int)(head.Timestamp)
	if !number.IsUint64() || !ts.IsInt64() {
		return Block{}, false, fmt.Errorf("%w: block header out of range", ErrChainUnavailable)
	}
	return Block{Number: number.Uint64(), Hash: strings.ToLower(head.Hash), Timestamp: time.Unix(ts.Int64(), 0).UTC()}, true, nil
}
