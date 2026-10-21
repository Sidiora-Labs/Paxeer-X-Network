package source

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// Archive-sourced backfill. Below the primaries' pruning floor every block is
// served by a historical (archive) endpoint that answers slowly per request
// but sustains many requests in flight, and whose eth_getLogs scans each block
// of the range (about 0.1 s per block, measured on chain 125). Three measures
// keep archive ingestion well above the chain's block rate without changing
// what is verified:
//
//   - routing: a range starting below a primary's self-reported pruning floor
//     is not sent to that primary first;
//   - logs from blocks: a block whose logsBloom contains none of the fetched
//     addresses has no log from them (a bloom filter has no false negatives);
//     every other block's logs come from its receipts, each bound to the
//     block's reported hash, height, transaction list and log order, and the
//     merged receipt bloom must equal the block's (the receipts root in keccak
//     mode) — the same checks as the single-block receipt fallback;
//   - readahead: blocks of the next ranges are read in the background from the
//     same archive endpoint and consumed by later fetches, which still apply
//     every per-block, linkage, log and identity check.

const (
	defaultHistoricalConcurrency = 48
	maxHistoricalConcurrency     = 96
	defaultHistoricalReadahead   = 256
	maxHistoricalReadahead       = 1024
	// defaultHistoricalAttempt bounds one archive request; the archive's
	// measured latency under load is 0.5–4 s (p99 below 4 s), so an attempt
	// still unanswered after 8 s is abandoned and retried inside the bulk
	// deadline instead of holding the range until it expires.
	defaultHistoricalAttempt = 8 * time.Second
)

var primaryFloorPattern = regexp.MustCompile(`earliest available is ([0-9]+)`)

// learnFloor records a primary's self-reported pruning floor from a "pruned"
// refusal. Floors only move up.
func (s *CommittedSource) learnFloor(e *committedEndpoint, err error) {
	if e.historical || !prunedSource(err) {
		return
	}
	var se *SourceError
	if !errors.As(err, &se) || se.Cause == nil {
		return
	}
	match := primaryFloorPattern.FindStringSubmatch(se.Cause.Error())
	if match == nil {
		return
	}
	floor, perr := strconv.ParseUint(match[1], 10, 64)
	if perr != nil {
		return
	}
	s.mu.Lock()
	if floor > e.floor {
		e.floor = floor
	}
	s.mu.Unlock()
}

// rangeConcurrency bounds the parallel reads of one range against e: the
// archive is read at its configured concurrency, a primary below its request
// permits.
func (s *CommittedSource) rangeConcurrency(e *committedEndpoint) int {
	if e.historical && s.opts.HistoricalConcurrency > 0 {
		return s.opts.HistoricalConcurrency
	}
	return prefetchBlockConcurrency
}

// prunedFor reports whether endpoint e has reported a pruning floor above
// height, so it cannot serve a range starting there.
func (s *CommittedSource) prunedFor(e *committedEndpoint, height uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !e.historical && e.floor > height
}

// ArchiveFloor is the lowest pruning floor reported by the eligible primaries:
// every height below it is served only by historical endpoints. Zero when no
// historical endpoint is eligible or some eligible primary never reported a
// floor.
func (s *CommittedSource) ArchiveFloor() uint64 {
	floor, archive := uint64(0), false
	for _, e := range s.eligible() {
		if e.historical {
			archive = true
			continue
		}
		s.mu.Lock()
		f := e.floor
		s.mu.Unlock()
		if f == 0 {
			return 0
		}
		if floor == 0 || f < floor {
			floor = f
		}
	}
	if !archive {
		return 0
	}
	return floor
}

// ReadaheadDepth is the configured number of blocks read ahead below the
// archive floor (zero when disabled).
func (s *CommittedSource) ReadaheadDepth() uint64 {
	if s.opts.HistoricalReadahead < 0 {
		return 0
	}
	return uint64(s.opts.HistoricalReadahead)
}

type aheadEntry struct {
	endpoint string
	done     chan struct{}
	block    batchBlock
	err      error
}

type readahead struct {
	mu      sync.Mutex
	entries map[uint64]*aheadEntry
}

// ReadAhead reads blocks [from, to] from the first eligible historical endpoint
// in the background (heights already read or in flight are skipped). It is a
// pure prefetch: fetch consumes an entry only for the same endpoint and then
// runs every check it runs on a block it read itself.
func (s *CommittedSource) ReadAhead(from, to uint64) {
	depth := s.ReadaheadDepth()
	if s == nil || s.lifecycle == nil || depth == 0 || to < from {
		return
	}
	to = min(to, from+depth-1)
	var archive *committedEndpoint
	for _, e := range s.eligible() {
		if e.historical {
			archive = e
			break
		}
	}
	if archive == nil {
		return
	}
	s.ahead.mu.Lock()
	if s.ahead.entries == nil {
		s.ahead.entries = map[uint64]*aheadEntry{}
	}
	heights := make([]uint64, 0, to-from+1)
	for h := from; h <= to && len(s.ahead.entries) < 2*int(depth); h++ {
		if _, ok := s.ahead.entries[h]; ok {
			continue
		}
		s.ahead.entries[h] = &aheadEntry{endpoint: archive.id, done: make(chan struct{})}
		heights = append(heights, h)
	}
	entries := make([]*aheadEntry, len(heights))
	for i, h := range heights {
		entries[i] = s.ahead.entries[h]
	}
	s.ahead.mu.Unlock()
	if len(heights) == 0 {
		return
	}
	ctx, done, err := s.lifecycle.begin(context.Background())
	if err != nil {
		for _, entry := range entries {
			entry.err = err
			close(entry.done)
		}
		return
	}
	go func() {
		defer done()
		jobs := make(chan int)
		var wg sync.WaitGroup
		for w := 0; w < min(len(heights), s.opts.HistoricalConcurrency); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range jobs {
					read, cancel := context.WithTimeout(ctx, s.opts.BulkDeadline)
					s.metrics.headerFetches.Add(1)
					wire, record, err := s.block(read, archive, fmtHeight(heights[i]), true)
					cancel()
					entries[i].block, entries[i].err = batchBlock{wire, record}, err
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

// takeAhead returns the read-ahead block at height when it was read from e,
// waiting for an in-flight read. The entry is removed either way.
func (s *CommittedSource) takeAhead(ctx context.Context, e *committedEndpoint, height uint64) (batchBlock, bool) {
	s.ahead.mu.Lock()
	entry, ok := s.ahead.entries[height]
	if ok {
		delete(s.ahead.entries, height)
	}
	s.ahead.mu.Unlock()
	if !ok || entry.endpoint != e.id {
		return batchBlock{}, false
	}
	select {
	case <-entry.done:
	case <-ctx.Done():
		return batchBlock{}, false
	}
	if entry.err != nil || entry.block.record.Height != height {
		return batchBlock{}, false
	}
	s.metrics.readaheadHits.Add(1)
	return entry.block, true
}

// dropAheadBelow discards completed read-ahead entries below height (ranges
// the caller has moved past).
func (s *CommittedSource) dropAheadBelow(height uint64) {
	s.ahead.mu.Lock()
	defer s.ahead.mu.Unlock()
	for h, entry := range s.ahead.entries {
		if h >= height {
			continue
		}
		select {
		case <-entry.done:
			delete(s.ahead.entries, h)
		default:
		}
	}
}

// archiveLogs derives the range's logs for the given addresses from its blocks
// (already read into batch, or read here) and, for blocks whose logsBloom may
// contain one of the addresses, from their verified receipts.
func (s *CommittedSource) archiveLogs(ctx context.Context, e *committedEndpoint, from, to int64, addresses []string, bulk bool, batch map[uint64]batchBlock) ([]ethtypes.Log, error) {
	if len(addresses) == 0 {
		return nil, nil
	}
	wanted := make([]common.Address, len(addresses))
	for i, a := range addresses {
		wanted[i] = common.HexToAddress(a)
	}
	var candidates []uint64
	for n := from; n <= to; n++ {
		b, _, err := s.batchBlock(ctx, e, uint64(n), bulk, batch)
		if err != nil {
			return nil, err
		}
		bloom, err := hex.DecodeString(strings.TrimPrefix(b.LogsBloom, "0x"))
		if err != nil || len(bloom) != ethtypes.BloomByteLength {
			return nil, sourceError(IncompleteError, e.id, nil)
		}
		filter := ethtypes.BytesToBloom(bloom)
		for _, a := range wanted {
			if ethtypes.BloomLookup(filter, a) {
				candidates = append(candidates, uint64(n))
				break
			}
		}
	}
	s.metrics.bloomSkipped.Add(uint64(to-from+1) - uint64(len(candidates)))
	if len(candidates) == 0 {
		return nil, nil
	}
	results := make([][]ethtypes.Log, len(candidates))
	errs := make([]error, len(candidates))
	slots := make(chan struct{}, s.rangeConcurrency(e))
	var wg sync.WaitGroup
	for i, height := range candidates {
		block := batch[height]
		wg.Add(1)
		go func(i int, height uint64, block batchBlock) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				errs[i] = sourceError(TimeoutError, e.id, ctx.Err())
				return
			}
			defer func() { <-slots }()
			results[i], errs[i] = s.receiptLogs(ctx, e, block.wire, block.record, height, wanted, bulk)
		}(i, height, block)
	}
	wg.Wait()
	var logs []ethtypes.Log
	for i := range candidates {
		if errs[i] != nil {
			return nil, errs[i]
		}
		logs = append(logs, results[i]...)
	}
	return logs, nil
}

// receiptLogs returns the logs emitted by the wanted addresses in one block,
// taken from the block's complete receipt list. Every receipt is bound to the
// block's reported hash, height and transaction at its index, log indexes run
// contiguously from zero, and the receipts must reproduce the block's
// receiptsRoot (keccak identity) or logsBloom (reported identity).
func (s *CommittedSource) receiptLogs(ctx context.Context, e *committedEndpoint, b blockWire, record BlockRecord, height uint64, wanted []common.Address, bulk bool) ([]ethtypes.Log, error) {
	if len(b.Transactions) > 4096 {
		return nil, sourceError(IncompleteError, e.id, nil)
	}
	if !validHash(b.ReceiptsRoot) {
		return nil, sourceError(IncompleteError, e.id, nil)
	}
	for _, tx := range b.Transactions {
		if !validHash(tx.Hash) {
			return nil, sourceError(IncompleteError, e.id, nil)
		}
	}
	s.metrics.receiptFallbacks.Add(1)
	var receipts []*ethtypes.Receipt
	err := s.rpc(ctx, e, bulk, "eth_getBlockReceipts", []any{record.Hash.Hex()}, &receipts)
	if err != nil {
		var se *SourceError
		if !errors.As(err, &se) || se.Kind != RPCError || se.Cause == nil || !strings.Contains(se.Cause.Error(), "rpc code -32601") {
			return nil, err
		}
		receipts = make([]*ethtypes.Receipt, 0, len(b.Transactions))
		for _, tx := range b.Transactions {
			var receipt ethtypes.Receipt
			if err := s.rpc(ctx, e, bulk, "eth_getTransactionReceipt", []any{tx.Hash}, &receipt); err != nil {
				return nil, err
			}
			receipts = append(receipts, &receipt)
		}
	}
	if len(receipts) != len(b.Transactions) {
		return nil, sourceError(IntegrityError, e.id, nil)
	}
	want := map[common.Address]bool{}
	for _, a := range wanted {
		want[a] = true
	}
	nextIndex := uint(0)
	used := 0
	var result []ethtypes.Log
	for i, receipt := range receipts {
		tx := b.Transactions[i]
		if receipt == nil || receipt.BlockHash != record.Hash || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() != height || receipt.TransactionIndex != uint(i) || receipt.TxHash != common.HexToHash(tx.Hash) {
			return nil, sourceError(IntegrityError, e.id, nil)
		}
		for _, l := range receipt.Logs {
			if l == nil {
				return nil, sourceError(IncompleteError, e.id, nil)
			}
			if l.BlockHash != record.Hash || l.BlockNumber != height || l.TxHash != receipt.TxHash || l.TxIndex != uint(i) || l.Index != nextIndex || l.Removed || len(l.Topics) > 4 || len(l.Data) > 64<<10 {
				return nil, sourceError(IntegrityError, e.id, nil)
			}
			nextIndex++
			used += 256 + len(l.Data) + 32*len(l.Topics)
			if used > 32<<20 {
				return nil, sourceError(OversizeError, e.id, nil)
			}
			if want[l.Address] {
				result = append(result, *l)
			}
		}
	}
	if s.opts.BlockIdentity == "reported" {
		bloom, err := hex.DecodeString(strings.TrimPrefix(b.LogsBloom, "0x"))
		if err != nil || len(bloom) != ethtypes.BloomByteLength || ethtypes.MergeBloom(receipts) != ethtypes.BytesToBloom(bloom) {
			return nil, sourceError(IntegrityError, e.id, nil)
		}
		return result, nil
	}
	if ethtypes.DeriveSha(ethtypes.Receipts(receipts), trie.NewStackTrie(nil)) != common.HexToHash(b.ReceiptsRoot) {
		return nil, sourceError(IntegrityError, e.id, nil)
	}
	return result, nil
}
