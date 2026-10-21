package receipt

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// Archive-rate qualification. Below the primary's pruning floor every pinned
// block comes from the archive endpoint, which is slow per request but serves
// many in parallel. Ranges are therefore read at a configurable concurrency,
// blocks of the next ranges are read ahead in the background, and a block whose
// logsBloom contains no address the authority qualifies (code epochs and the
// factory) is pinned without its receipts: a bloom filter has no false
// negatives, so such a block has no log the authority would record or use for
// discovery. Blocks that may hold one are pinned with receipts exactly as
// before (bound to the block identity, the merged bloom equal to the block's).

const (
	// DefaultArchiveConcurrency bounds parallel pinned reads of one range and
	// of readahead (the archive measured ~95 req/s at 64 in flight).
	DefaultArchiveConcurrency = 48
	MaxArchiveConcurrency     = 96
	// DefaultArchiveReadahead is the number of blocks read ahead of a range.
	DefaultArchiveReadahead = 256
	MaxArchiveReadahead     = 1024
	// DefaultArchiveAttempt bounds one archive request attempt; p99 latency
	// under load is below 4 s, so an attempt unanswered after 8 s is retried
	// inside the method's archive window.
	DefaultArchiveAttempt = 8 * time.Second
)

// ArchiveOptions tunes the archive leg of the bounded qualification client.
type ArchiveOptions struct {
	// Concurrency bounds archive requests in flight (0: default 48).
	Concurrency int
	// AttemptTimeout bounds one archive request attempt (0: default 8s).
	AttemptTimeout time.Duration
}

func (o ArchiveOptions) normalized() (ArchiveOptions, error) {
	if o.Concurrency == 0 {
		o.Concurrency = DefaultArchiveConcurrency
	}
	if o.AttemptTimeout == 0 {
		o.AttemptTimeout = DefaultArchiveAttempt
	}
	if o.Concurrency < 1 || o.Concurrency > MaxArchiveConcurrency || o.AttemptTimeout < 500*time.Millisecond || o.AttemptTimeout > 45*time.Second {
		return o, ErrQualification
	}
	return o, nil
}

// ConfigureArchiveReads sets the range read concurrency and the readahead depth
// (blocks; negative disables readahead, 0 keeps the default).
func (a *RPCQualification) ConfigureArchiveReads(concurrency, readahead int) error {
	if a == nil || a.reads == nil || concurrency < 0 || concurrency > MaxArchiveConcurrency || readahead > MaxArchiveReadahead {
		return ErrQualification
	}
	a.reads.mu.Lock()
	defer a.reads.mu.Unlock()
	if concurrency > 0 {
		a.reads.concurrency = concurrency
	}
	switch {
	case readahead < 0:
		a.reads.readahead = -1
	case readahead > 0:
		a.reads.readahead = readahead
	}
	return nil
}

func (r *rpcReadState) rangeConcurrency() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.concurrency <= 0 {
		return DefaultArchiveConcurrency
	}
	return r.concurrency
}

func (r *rpcReadState) readaheadDepth() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.readahead < 0 {
		return 0
	}
	if r.readahead == 0 {
		return DefaultArchiveReadahead
	}
	return r.readahead
}

// bloomGate is the set of addresses whose logs this authority records or uses
// for discovery, as single-address bloom filters.
type bloomGate []types.Bloom

func (a *RPCQualification) bloomGate() bloomGate {
	seen := map[common.Address]bool{}
	var gate bloomGate
	add := func(address common.Address) {
		if seen[address] {
			return
		}
		seen[address] = true
		var b types.Bloom
		b.Add(address.Bytes())
		gate = append(gate, b)
	}
	for _, e := range a.options.Epochs {
		add(e.Deployment.Address)
	}
	if factory, ok := a.options.Manifest.Roles["SidioraFactory"]; ok && common.IsHexAddress(factory.Address) {
		add(common.HexToAddress(factory.Address))
	}
	return gate
}

// relevant reports whether the block bloom may contain a log of a gated
// address (true on any filter match; false proves absence).
func (g bloomGate) relevant(bloom types.Bloom) bool {
	for _, single := range g {
		match := true
		for i := range single {
			if single[i]&bloom[i] != single[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// pinnedBlockAtHeight reads the canonical block at a height in one call (the
// node names the block; header, identity and transactions come together) and
// binds its receipts to that identity. Used for ordered range reads, where the
// height lookup is itself the canonicality proof. A block the optional bloom
// gate proves irrelevant is returned without receipts (skipped).
func (a *RPCQualification) pinnedBlockAtHeight(ctx context.Context, height uint64, gate bloomGate) (pinnedBlock, types.Receipts, bool, error) {
	raw, err := a.rawBlock(ctx, "eth_getBlockByNumber", hexutil.EncodeUint64(height), true)
	if err != nil {
		if IsPruned(err) {
			return pinnedBlock{}, nil, false, err
		}
		return pinnedBlock{}, nil, false, ErrQualification
	}
	block, err := a.parseBlock(raw)
	if err != nil {
		return pinnedBlock{}, nil, false, err
	}
	header := block.Header
	if header.Number.Uint64() != height || header.Number.Uint64() < a.options.Manifest.ActivationHeight || !header.Number.IsInt64() || len(block.Transactions) > 4096 || header.Time == 0 || header.Time > math.MaxInt64/1000 {
		return pinnedBlock{}, nil, false, ErrQualification
	}
	if gate != nil && !gate.relevant(header.Bloom) {
		if !a.reportedIdentity() && types.DeriveSha(block.Transactions, trie.NewStackTrie(nil)) != header.TxHash {
			return pinnedBlock{}, nil, false, ErrQualification
		}
		return block, nil, true, nil
	}
	block, receipts, err := a.pinnedReceipts(ctx, block)
	return block, receipts, false, err
}

type pinnedAhead struct {
	done   chan struct{}
	pinned pinnedHeight
	err    error
}

// ReadAhead pins blocks [from, to] in the background (bounded by the readahead
// depth; heights already pinned or in flight are skipped) for later ranges to
// consume. Entries carry exactly what pinRange would have read; the range's
// linkage, discovery and decoding checks still run when it is qualified.
func (a *RPCQualification) ReadAhead(from, to uint64) {
	if a == nil || a.reads == nil || to < from {
		return
	}
	depth := a.reads.readaheadDepth()
	if depth == 0 || from < a.options.Manifest.ActivationHeight {
		return
	}
	to = min(to, from+uint64(depth)-1)
	gate := a.current().bloomGate()
	r := a.reads
	r.mu.Lock()
	if r.ahead == nil {
		r.ahead = map[uint64]*pinnedAhead{}
	}
	var heights []uint64
	var entries []*pinnedAhead
	for h := from; h <= to && len(r.ahead) < 2*depth; h++ {
		if _, ok := r.ahead[h]; ok {
			continue
		}
		entry := &pinnedAhead{done: make(chan struct{})}
		r.ahead[h] = entry
		heights = append(heights, h)
		entries = append(entries, entry)
	}
	r.mu.Unlock()
	if len(heights) == 0 {
		return
	}
	workers := min(len(heights), r.rangeConcurrency())
	go func() {
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					read, cancel := context.WithTimeout(context.Background(), a.options.Timeout)
					block, receipts, skipped, err := a.pinnedBlockAtHeight(read, heights[i], gate)
					cancel()
					entries[i].pinned = pinnedHeight{header: block.Header, block: block, receipts: receipts, skipped: skipped}
					entries[i].err = err
					close(entries[i].done)
				}
			}()
		}
		for i := range heights {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
	}()
}

// takeAhead returns the read-ahead pin of height, waiting for an in-flight
// read; the entry is removed either way.
func (r *rpcReadState) takeAhead(ctx context.Context, height uint64) (pinnedHeight, bool) {
	r.mu.Lock()
	entry, ok := r.ahead[height]
	if ok {
		delete(r.ahead, height)
	}
	r.mu.Unlock()
	if !ok {
		return pinnedHeight{}, false
	}
	select {
	case <-entry.done:
	case <-ctx.Done():
		return pinnedHeight{}, false
	}
	if entry.err != nil || entry.pinned.header == nil || entry.pinned.header.Number.Uint64() != height {
		return pinnedHeight{}, false
	}
	r.aheadHits.Add(1)
	return entry.pinned, true
}

// dropAheadBelow discards completed read-ahead pins below height.
func (r *rpcReadState) dropAheadBelow(height uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for h, entry := range r.ahead {
		if h >= height {
			continue
		}
		select {
		case <-entry.done:
			delete(r.ahead, h)
		default:
		}
	}
}

// Process-wide archive transport counters: attempts retried after a timeout
// or transport failure, and gateway overload answers (502/503/504).
var archiveRetries, archiveOverloads atomic.Uint64

// ArchiveTransportCounters reports the process-wide archive retry and
// overload counts.
func ArchiveTransportCounters() (retries, overloads uint64) {
	return archiveRetries.Load(), archiveOverloads.Load()
}

// ArchiveReadMetrics reports readahead hits and bloom-skipped receipt reads.
type ArchiveReadMetrics struct{ ReadaheadHits, BloomSkipped, ReceiptReads uint64 }

func (a *RPCQualification) ArchiveReadMetrics() ArchiveReadMetrics {
	if a == nil || a.reads == nil {
		return ArchiveReadMetrics{}
	}
	return ArchiveReadMetrics{a.reads.aheadHits.Load(), a.reads.bloomSkipped.Load(), a.reads.receiptReads.Load()}
}
