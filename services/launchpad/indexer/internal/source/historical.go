package source

import (
	"context"
	"errors"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/receipt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"math"
	"sort"
	"strings"
	"sync/atomic"
)

type sourceMetrics struct {
	blocksFetched, emptyRanges, rangeBisections, receiptFallbacks, cacheHits, headerFetches, readaheadHits, bloomSkipped atomic.Uint64
}
type SourceMetrics struct {
	BlocksFetched, EmptyRanges, RangeBisections, ReceiptFallbacks, CacheHits, HeaderFetches uint64
	// ReadaheadHits counts archive blocks served from readahead; BloomSkipped
	// counts archive blocks whose logsBloom excluded every fetched address.
	ReadaheadHits, BloomSkipped uint64
}

func (s *CommittedSource) Metrics() SourceMetrics {
	m := &s.metrics
	return SourceMetrics{m.blocksFetched.Load(), m.emptyRanges.Load(), m.rangeBisections.Load(), m.receiptFallbacks.Load(), m.cacheHits.Load(), m.headerFetches.Load(), m.readaheadHits.Load(), m.bloomSkipped.Load()}
}
func (s *CommittedSource) BlockAt(ctx context.Context, height uint64) (BlockRecord, error) {
	if height > math.MaxInt64 {
		return BlockRecord{}, sourceError(InvalidError, "range", nil)
	}
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return BlockRecord{}, err
		}
		defer done()
	}
	var last error
	for _, endpoint := range s.eligible() {
		_, record, err := s.block(ctx, endpoint, fmtHeight(height), true)
		if err == nil && record.Height == height {
			s.mu.Lock()
			err = s.fenceLocked(height)
			s.mu.Unlock()
			if err != nil {
				return BlockRecord{}, err
			}
			return record, nil
		}
		if err == nil {
			err = sourceError(IntegrityError, endpoint.id, nil)
		}
		last = err
	}
	if last == nil {
		last = sourceError(IdentityError, "pool", nil)
	}
	return BlockRecord{}, last
}
func fmtHeight(height uint64) string {
	const digits = "0123456789abcdef"
	var buf [18]byte
	i := len(buf)
	for {
		i--
		buf[i] = digits[height&15]
		height >>= 4
		if height == 0 {
			break
		}
	}
	i -= 2
	buf[i] = '0'
	buf[i+1] = 'x'
	return string(buf[i:])
}

type HistoricalSource struct {
	RPC      *CommittedSource
	Explorer *CommittedPaxscanSource
}
type HistoricalHeads struct {
	ExplorerIndexedHeight    *uint64
	RPCCommittedHeight       uint64
	ApplicationDurableHeight uint64
}
type HistoricalResult struct {
	FetchResult
	Heads HistoricalHeads
}

func (s *HistoricalSource) Name() string { return "historical-committed" }
func (s *HistoricalSource) Close() error {
	if s.RPC != nil {
		if err := s.RPC.Close(); err != nil {
			return err
		}
	}
	if s.Explorer != nil {
		return s.Explorer.Close()
	}
	return nil
}
func mergeFetch(out *FetchResult, page FetchResult) error {
	if !page.Complete || len(page.CoveredRanges) != 1 || page.ManifestDigest != out.ManifestDigest {
		return sourceError(IncompleteError, "historical", nil)
	}
	if len(out.CoveredRanges) > 0 && out.CoveredRanges[len(out.CoveredRanges)-1].ToHeight+1 != page.CoveredRanges[0].FromHeight {
		return sourceError(IntegrityError, "historical", nil)
	}
	for height, block := range page.Blocks {
		if _, ok := out.Blocks[height]; ok {
			return sourceError(IntegrityError, "historical", nil)
		}
		out.Blocks[height] = block
	}
	for height, ts := range page.BlockTimestamps {
		out.BlockTimestamps[height] = ts
	}
	if page.Explorer != nil {
		if out.Explorer == nil {
			provenance := *page.Explorer
			provenance.ValidatedCheckpoints = append([]BlockRecord(nil), page.Explorer.ValidatedCheckpoints...)
			out.Explorer = &provenance
		} else {
			if out.Explorer.SourceID != page.Explorer.SourceID || out.Explorer.SchemaVersion != page.Explorer.SchemaVersion {
				return sourceError(IdentityError, "historical", nil)
			}
			out.Explorer.IndexedHeight = max(out.Explorer.IndexedHeight, page.Explorer.IndexedHeight)
			out.Explorer.ValidatedCheckpoints = append(out.Explorer.ValidatedCheckpoints, page.Explorer.ValidatedCheckpoints...)
		}
	}
	for tx, from := range page.TxFromMap {
		if old, ok := out.TxFromMap[tx]; ok && old != from {
			return sourceError(IntegrityError, "historical", nil)
		}
		out.TxFromMap[tx] = from
	}
	size := 0
	for _, logs := range [][]ethtypes.Log{out.Logs, page.Logs} {
		for _, log := range logs {
			size += 256 + len(log.Data) + 32*len(log.Topics)
			if size > 128<<20 {
				return sourceError(OversizeError, "historical", nil)
			}
		}
	}
	if len(out.Logs)+len(page.Logs) > 131072 {
		return sourceError(OversizeError, "historical", nil)
	}
	out.Logs = append(out.Logs, page.Logs...)
	out.CoveredRanges = append(out.CoveredRanges, page.CoveredRanges[0])
	return nil
}
func (s *HistoricalSource) FetchHistorical(ctx context.Context, o FetchOptions, durable uint64) (HistoricalResult, error) {
	return s.FetchHistoricalWith(ctx, o, durable, nil)
}

// FetchHistoricalWith is FetchHistorical including coordinator-approved
// discovered epochs (factory-created tokens and pools) in the RPC fetch.
func (s *HistoricalSource) FetchHistoricalWith(ctx context.Context, o FetchOptions, durable uint64, discoveries []AddressEpoch) (HistoricalResult, error) {
	var result HistoricalResult
	if s == nil || s.RPC == nil || o.FromBlock < 0 || o.ToBlock < o.FromBlock || o.ToBlock-o.FromBlock >= 100000 {
		return result, sourceError(InvalidError, "historical", nil)
	}
	if s.RPC.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.RPC.lifecycle.begin(ctx)
		if err != nil {
			return result, err
		}
		defer done()
	}
	if err := s.RPC.waitHistorical(ctx); err != nil {
		return result, err
	}
	head, err := s.RPC.observeHead(ctx, durable, true)
	if err != nil {
		return result, err
	}
	if uint64(o.ToBlock) > head.Height {
		return result, sourceError(IncompleteError, "historical", nil)
	}
	result.Heads = HistoricalHeads{RPCCommittedHeight: head.Height, ApplicationDurableHeight: durable}
	if s.Explorer != nil {
		height, err := s.Explorer.Head(ctx)
		if err != nil {
			return result, err
		}
		indexed := uint64(height)
		result.Heads.ExplorerIndexedHeight = &indexed
	}
	result.FetchResult = FetchResult{ManifestDigest: s.RPC.opts.ManifestDigest, Blocks: map[uint64]BlockRecord{}, BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
	for from := o.FromBlock; from <= o.ToBlock; {
		to := from + min(int64(1999), o.ToBlock-from)
		useExplorer := s.Explorer != nil && uint64(from) <= *result.Heads.ExplorerIndexedHeight
		if useExplorer {
			to = min(to, int64(*result.Heads.ExplorerIndexedHeight))
		}
		pageOptions := o
		pageOptions.FromBlock = from
		pageOptions.ToBlock = to
		var page FetchResult
		if useExplorer {
			page, err = s.Explorer.FetchLogs(ctx, pageOptions)
		} else {
			page, err = s.RPC.FetchCommitted(ctx, pageOptions, true, discoveries)
		}
		if err != nil {
			return HistoricalResult{Heads: result.Heads}, err
		}
		if err = mergeFetch(&result.FetchResult, page); err != nil {
			return HistoricalResult{Heads: result.Heads}, err
		}
		if to == o.ToBlock {
			break
		}
		from = to + 1
	}
	sort.Slice(result.Logs, func(i, j int) bool {
		a, b := result.Logs[i], result.Logs[j]
		if a.BlockNumber != b.BlockNumber {
			return a.BlockNumber < b.BlockNumber
		}
		if a.TxIndex != b.TxIndex {
			return a.TxIndex < b.TxIndex
		}
		return a.Index < b.Index
	})
	s.RPC.mu.Lock()
	err = s.RPC.fenceLocked(uint64(o.ToBlock))
	s.RPC.mu.Unlock()
	if err != nil {
		return HistoricalResult{Heads: result.Heads}, err
	}
	result.Complete = true
	return result, nil
}
func (s *HistoricalSource) FetchLogs(ctx context.Context, o FetchOptions) (FetchResult, error) {
	result, err := s.FetchHistorical(ctx, o, 0)
	return result.FetchResult, err
}

type HistoricalQualifiedSource struct {
	History       *HistoricalSource
	Authority     *receipt.RPCQualification
	DurableHeight func() uint64
}

// DiscoveredAddressEpochs converts the authority's factory-discovered code
// epochs into committed-source address epochs, so the independent committed
// log fetch covers exactly the emitters the authority qualifies.
func DiscoveredAddressEpochs(authority *receipt.RPCQualification, manifestDigest string) []AddressEpoch {
	discovered := authority.DiscoveredEpochs()
	out := make([]AddressEpoch, 0, len(discovered))
	for _, e := range discovered {
		out = append(out, AddressEpoch{Address: strings.ToLower(e.Deployment.Address.Hex()), Role: e.Deployment.Role, ABIRevision: e.Deployment.ABIRevision, ManifestDigest: manifestDigest, ActivationHeight: e.Deployment.From.Block, CreationTxIndex: e.Deployment.From.Transaction})
	}
	return out
}

// qualifyWithDiscoveries fetches the committed range with the authority's
// current discoveries and qualifies it. A market created inside the range is
// discovered by the authority during qualification; its token and pool logs
// are then absent from the first committed fetch, so the range is fetched and
// qualified once more with the grown discovery set.
func qualifyWithDiscoveries(ctx context.Context, authority *receipt.RPCQualification, manifestDigest string, from, to uint64, prefix string, fetch func([]AddressEpoch) (FetchResult, error)) (store.CanonicalBatch, []*receipt.QualifiedReceipt, error) {
	before := DiscoveredAddressEpochs(authority, manifestDigest)
	result, err := fetch(before)
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	batch, qualified, err := qualifyFetch(ctx, result, authority, from, to, prefix)
	if err == nil || !errors.Is(err, receipt.ErrBinding) {
		return batch, qualified, err
	}
	after := DiscoveredAddressEpochs(authority, manifestDigest)
	if len(after) <= len(before) {
		return store.CanonicalBatch{}, nil, err
	}
	if result, err = fetch(after); err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	return qualifyFetch(ctx, result, authority, from, to, prefix)
}

func (s *HistoricalQualifiedSource) FetchQualifiedBatch(ctx context.Context, from, to uint64) (store.CanonicalBatch, []*receipt.QualifiedReceipt, error) {
	var batch store.CanonicalBatch
	if s == nil || s.History == nil || s.History.RPC == nil || s.Authority == nil || s.DurableHeight == nil || from > math.MaxInt64 || to > math.MaxInt64 || to < from || to-from >= 64 {
		return batch, nil, sourceError(IdentityError, "historical-qualified", nil)
	}
	if s.History.RPC.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.History.RPC.lifecycle.begin(ctx)
		if err != nil {
			return batch, nil, err
		}
		defer done()
	}
	readAheadBelowFloor(s.History.RPC, s.Authority, to)
	batch, qualified, err := qualifyWithDiscoveries(ctx, s.Authority, s.History.RPC.opts.ManifestDigest, from, to, "historical-", func(discoveries []AddressEpoch) (FetchResult, error) {
		historical, err := s.History.FetchHistoricalWith(ctx, FetchOptions{FromBlock: int64(from), ToBlock: int64(to)}, s.DurableHeight(), discoveries)
		return historical.FetchResult, err
	})
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	readAheadBelowFloor(s.History.RPC, s.Authority, to)
	s.History.RPC.mu.Lock()
	err = s.History.RPC.fenceLocked(to)
	s.History.RPC.mu.Unlock()
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	return batch, qualified, nil
}

// readAheadBelowFloor starts background reads of the blocks after to that only
// the archive can serve (below every eligible primary's pruning floor), for
// both the committed source and the receipt authority. Both read
// independently, and every block read ahead is checked when consumed exactly
// as if it were read then.
func readAheadBelowFloor(rpc *CommittedSource, authority *receipt.RPCQualification, to uint64) {
	floor, depth := rpc.ArchiveFloor(), rpc.ReadaheadDepth()
	if floor == 0 || depth == 0 || to+1 >= floor {
		return
	}
	end := min(to+depth, floor-1)
	rpc.ReadAhead(to+1, end)
	authority.ReadAhead(to+1, end)
}

type LiveQualifiedSource struct {
	RPC           *CommittedSource
	Authority     *receipt.RPCQualification
	DurableHeight func() uint64
	// PriorHash returns the durable block hash at a height (the last committed
	// block, or the issued initial checkpoint). When set, every live batch must
	// extend it: the first block's parent hash equals the durable hash.
	PriorHash func(context.Context, uint64) (common.Hash, error)
}

func (s *LiveQualifiedSource) FetchQualifiedBatch(ctx context.Context, from, to uint64) (store.CanonicalBatch, []*receipt.QualifiedReceipt, error) {
	if s == nil || s.RPC == nil || s.Authority == nil || s.DurableHeight == nil || from > math.MaxInt64 || to > math.MaxInt64 || to < from || to-from >= 64 {
		return store.CanonicalBatch{}, nil, sourceError(IdentityError, "live-qualified", nil)
	}
	durable := s.DurableHeight()
	if durable == ^uint64(0) || from != durable+1 {
		return store.CanonicalBatch{}, nil, sourceError(IntegrityError, "live-qualified", nil)
	}
	if s.RPC.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.RPC.lifecycle.begin(ctx)
		if err != nil {
			return store.CanonicalBatch{}, nil, err
		}
		defer done()
	}
	readAheadBelowFloor(s.RPC, s.Authority, to)
	var first BlockRecord
	batch, qualified, err := qualifyWithDiscoveries(ctx, s.Authority, s.RPC.opts.ManifestDigest, from, to, "live-", func(discoveries []AddressEpoch) (FetchResult, error) {
		fetch, err := s.RPC.FetchCommitted(ctx, FetchOptions{FromBlock: int64(from), ToBlock: int64(to)}, false, discoveries)
		if err == nil {
			first = fetch.Blocks[from]
		}
		return fetch, err
	})
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	readAheadBelowFloor(s.RPC, s.Authority, to)
	if s.PriorHash != nil {
		prior, err := s.PriorHash(ctx, from-1)
		if err != nil {
			return store.CanonicalBatch{}, nil, err
		}
		if first.Height != from || first.ParentHash != prior {
			return store.CanonicalBatch{}, nil, sourceError(IntegrityError, "live-qualified", errors.New("batch does not extend the durable checkpoint"))
		}
	}
	s.RPC.mu.Lock()
	err = s.RPC.fenceLocked(to)
	s.RPC.mu.Unlock()
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	return batch, qualified, nil
}
func qualifyFetch(ctx context.Context, historical FetchResult, authority *receipt.RPCQualification, from, to uint64, prefix string) (store.CanonicalBatch, []*receipt.QualifiedReceipt, error) {
	var batch store.CanonicalBatch
	if !historical.Complete {
		return batch, nil, sourceError(IncompleteError, "historical-qualified", nil)
	}
	batch = store.CanonicalBatch{ChainID: 125, PreviousHeight: int64(from) - 1, Blocks: []store.BlockCompleteness{}, Records: []store.RawRecord{}}
	counts := map[uint64]int{}
	for _, log := range historical.Logs {
		if log.TxIndex > math.MaxUint32 || log.Index > math.MaxUint32 {
			return store.CanonicalBatch{}, nil, sourceError(InvalidError, "historical-qualified", nil)
		}
		topics := make([]string, len(log.Topics))
		for i, topic := range log.Topics {
			topics[i] = strings.ToLower(topic.Hex())
		}
		batch.Records = append(batch.Records, store.RawRecord{TransactionHash: strings.ToLower(log.TxHash.Hex()), BlockHash: strings.ToLower(log.BlockHash.Hex()), Emitter: strings.ToLower(log.Address.Hex()), Height: int64(log.BlockNumber), TransactionIndex: uint32(log.TxIndex), LogIndex: uint32(log.Index), Topics: topics, Data: append([]byte{}, log.Data...)})
		counts[log.BlockNumber]++
	}
	for height := from; height <= to; height++ {
		record, ok := historical.Blocks[height]
		if !ok {
			return store.CanonicalBatch{}, nil, sourceError(IncompleteError, "historical-qualified", nil)
		}
		batch.Blocks = append(batch.Blocks, store.BlockCompleteness{Height: int64(height), Hash: strings.ToLower(record.Hash.Hex()), RawCount: counts[height], FetchComplete: true, RequiredDecodeComplete: true})
	}
	identity, err := parityDigest(struct {
		Manifest string
		Blocks   []store.BlockCompleteness
		Records  []store.RawRecord
	}{historical.ManifestDigest, batch.Blocks, batch.Records})
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	batch.BatchID = prefix + identity
	qualified, err := authority.QualifiedBatch(ctx, batch)
	if err != nil {
		return store.CanonicalBatch{}, nil, err
	}
	return batch, qualified, nil
}
