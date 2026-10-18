// Package source provides swappable EVM log + chain-head sources for the
// indexer, parity with the TS source-factory (paxscan | rpc | rpc-getlogs).
//
//   - rpc-getlogs : load-balanced eth_getLogs over a pool of validator RPCs with
//     stale-node quarantine + failover (live-preferred). [evm.go]
//   - rpc         : single-node eth_getLogs (local-dev / disaster-recovery). [evm.go]
//   - paxscan     : SQL reads from the Blockscout DB (backfill-preferred). [paxscan.go]
//
// Meta-AG is excluded from the new chain (L4), so the monitored address set is
// just the EventEmitter and there is no meta-ag log partition.
package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol"
)

// FetchOptions bounds one fetchLogs call.
type FetchOptions struct {
	// MonitoredAddresses is the set of lowercased 0x-hex contract addresses to
	// fetch logs for (the EventEmitter for this build).
	MonitoredAddresses []string
	// FromBlock / ToBlock is the inclusive range.
	FromBlock int64
	ToBlock   int64
	// Concurrency caps parallel per-block enrichment calls (rpc sources only).
	Concurrency int
	Historical  bool
}

// FetchResult is the source-agnostic output consumed by the processor.
type FetchResult struct {
	// Production completeness metadata; legacy adapters leave Complete false.
	Complete       bool
	CoveredRanges  []CoveredRange
	Blocks         map[uint64]BlockRecord
	ManifestDigest string
	Explorer       *ExplorerProvenance
	// Logs are the matched logs in (block, logIndex) ascending order.
	Logs []ethtypes.Log
	// BlockTimestamps maps block number -> unix epoch seconds.
	BlockTimestamps map[uint64]int64
	// TxFromMap maps a lowercased tx hash -> the EOA `from` that signed it,
	// populated only for the Swap-like events that need it (see TxFromTopics).
	TxFromMap map[string]string
}

// LogSource fetches decoded-ready logs for a block range.
type LogSource interface {
	FetchLogs(ctx context.Context, opts FetchOptions) (FetchResult, error)
	Name() string
	Close() error
}

// HeadSource reports the current chain head.
type HeadSource interface {
	Head(ctx context.Context) (int64, error)
	Name() string
	Close() error
}

// txFromTopics is the set of topic0s whose authoritative EOA sender lives on the
// transaction's `from` rather than in the event args (Swap, TokenForTokenSwap).
// Computed once from the protocol registry so it always tracks the bindings.
// (BestRouteSwap / MultiHopSwap are meta-ag events, excluded — L4.)
var txFromTopics = buildTxFromTopics()

func buildTxFromTopics() map[string]struct{} {
	want := map[string]struct{}{"Swap": {}, "TokenForTokenSwap": {}}
	out := make(map[string]struct{})
	for _, d := range protocol.Events() {
		if _, ok := want[d.Name]; ok {
			out[strings.ToLower(d.Topic0.Hex())] = struct{}{}
		}
	}
	return out
}

// needsTxFrom reports whether a log's topic0 requires an EOA-sender lookup.
func needsTxFrom(topic0 string) bool {
	_, ok := txFromTopics[strings.ToLower(topic0)]
	return ok
}

type EndpointHealth struct {
	SourceID         string
	LastGoodHeight   uint64
	LastHash         common.Hash
	QuarantineReason string
	FirstFailureAt   time.Time
	RecoveryChecks   uint64
}
type IntegrityIncident struct {
	Height                                uint64
	AcceptedHash, ConflictingHash         common.Hash
	AcceptedSourceID, ConflictingSourceID string
	DetectedAt                            time.Time
}
type SourceSelectionAuthorization struct {
	ID, Kind, Scope, Reason, RequestedBy, ApprovedBy, State, PayloadHash string
	Revision                                                             uint64
	Expires                                                              time.Time
	SourceID                                                             string
	Height                                                               uint64
	Hash                                                                 common.Hash
}
type SourceSelectionAuthority interface {
	AuthorizeSourceSelection(context.Context, string, string, uint64, common.Hash) (SourceSelectionAuthorization, error)
}
type transportMetrics struct {
	throttles, invalidResponses      atomic.Uint64
	inFlight                         atomic.Int64
	responseBytes, retries, timeouts atomic.Uint64
	liveWaitNanos                    atomic.Int64
	historicalQueue                  atomic.Int64
}
type TransportMetrics struct {
	InFlightRequests                       int64
	ResponseBytes, RetryAttempts, Timeouts uint64
	// Throttles counts 429 answers and archive gateway overloads (502-504).
	Throttles                         uint64
	LiveWaitNanoseconds               int64
	PendingRanges                     int
	HistoricalQueueDepth              int64
	HistoricalPaused                  bool
	PausedBackfillNanoseconds         int64
	CancellationCompletionNanoseconds int64
}
type sourceLifecycle struct {
	mu         sync.Mutex
	closed     bool
	active     map[uint64]context.CancelFunc
	next       uint64
	drained    chan struct{}
	closeOnce  sync.Once
	completion atomic.Int64
	closeErr   error
}

func newSourceLifecycle() *sourceLifecycle {
	return &sourceLifecycle{active: map[uint64]context.CancelFunc{}, drained: make(chan struct{})}
}
func (l *sourceLifecycle) begin(parent context.Context) (context.Context, func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil, nil, sourceError(ClosedError, "scheduler", nil)
	}
	if err := parent.Err(); err != nil {
		return nil, nil, err
	}
	if len(l.active) >= 1024 {
		return nil, nil, sourceError(OverloadError, "scheduler", nil)
	}
	ctx, cancel := context.WithCancel(parent)
	l.next++
	id := l.next
	l.active[id] = cancel
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			cancel()
			l.mu.Lock()
			delete(l.active, id)
			if l.closed && len(l.active) == 0 {
				select {
				case <-l.drained:
				default:
					close(l.drained)
				}
			}
			l.mu.Unlock()
		})
	}, nil
}
func (l *sourceLifecycle) close() error {
	l.closeOnce.Do(func() {
		started := time.Now()
		l.mu.Lock()
		l.closed = true
		for _, cancel := range l.active {
			cancel()
		}
		if len(l.active) == 0 {
			close(l.drained)
		}
		l.mu.Unlock()
		timer := time.NewTimer(20 * time.Second)
		defer timer.Stop()
		select {
		case <-l.drained:
		case <-timer.C:
			l.closeErr = sourceError(TimeoutError, "shutdown", context.DeadlineExceeded)
		}
		l.completion.Store(time.Since(started).Nanoseconds())
	})
	return l.closeErr
}

type backfillControl struct {
	mu                                           sync.Mutex
	highSince, lowSince, pausedSince, observedAt time.Time
	paused                                       bool
	pausedNanos                                  int64
	changed                                      chan struct{}
}

func (s *CommittedSource) ReportLiveLag(observedAt time.Time, lag time.Duration) error {
	if observedAt.IsZero() || lag < 0 || observedAt.After(time.Now().Add(time.Second)) {
		return sourceError(InvalidError, "lag", nil)
	}
	b := &s.backfill
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.observedAt.IsZero() && observedAt.Before(b.observedAt) {
		return sourceError(InvalidError, "lag", nil)
	}
	b.observedAt = observedAt
	if !b.paused {
		if lag > 2*time.Second {
			if b.highSince.IsZero() {
				b.highSince = observedAt
			}
			if observedAt.Sub(b.highSince) >= 30*time.Second {
				b.paused = true
				b.pausedSince = observedAt
				b.changed = make(chan struct{})
				b.lowSince = time.Time{}
			}
		} else {
			b.highSince = time.Time{}
		}
	} else {
		if lag < 500*time.Millisecond {
			if b.lowSince.IsZero() {
				b.lowSince = observedAt
			}
			if observedAt.Sub(b.lowSince) >= 60*time.Second {
				b.paused = false
				b.pausedNanos += observedAt.Sub(b.pausedSince).Nanoseconds()
				b.highSince = time.Time{}
				close(b.changed)
			}
		} else {
			b.lowSince = time.Time{}
		}
	}
	return nil
}
func (s *CommittedSource) waitHistorical(ctx context.Context) error {
	for {
		s.backfill.mu.Lock()
		paused, changed := s.backfill.paused, s.backfill.changed
		s.backfill.mu.Unlock()
		if !paused {
			return ctx.Err()
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return sourceError(TimeoutError, "historical-paused", ctx.Err())
		}
	}
}
func (s *CommittedSource) TransportMetrics() TransportMetrics {
	m := TransportMetrics{HistoricalQueueDepth: s.transport.historicalQueue.Load(), InFlightRequests: s.transport.inFlight.Load(), ResponseBytes: s.transport.responseBytes.Load(), RetryAttempts: s.transport.retries.Load(), Timeouts: s.transport.timeouts.Load(), Throttles: s.transport.throttles.Load(), LiveWaitNanoseconds: s.transport.liveWaitNanos.Load()}
	if s.lifecycle != nil {
		s.lifecycle.mu.Lock()
		m.PendingRanges = len(s.lifecycle.active)
		s.lifecycle.mu.Unlock()
		m.CancellationCompletionNanoseconds = s.lifecycle.completion.Load()
	}
	s.backfill.mu.Lock()
	m.HistoricalPaused = s.backfill.paused
	m.PausedBackfillNanoseconds = s.backfill.pausedNanos
	if s.backfill.paused {
		m.PausedBackfillNanoseconds += max(int64(0), time.Since(s.backfill.pausedSince).Nanoseconds())
	}
	s.backfill.mu.Unlock()
	return m
}
func (s *CommittedSource) disagreeLocked(e *committedEndpoint, height uint64, accepted, conflicting common.Hash) {
	e.quarantined = true
	e.generation++
	e.selection = nil
	e.health.QuarantineReason = "canonical_hash_disagreement"
	e.health.SourceID = e.id
	if e.health.FirstFailureAt.IsZero() {
		e.health.FirstFailureAt = time.Now().UTC()
	}
	e.health.RecoveryChecks = 0
	if s.incidents == nil {
		s.incidents = map[uint64]IntegrityIncident{}
	}
	if _, exists := s.incidents[height]; !exists {
		acceptedSource := ""
		if record, ok := s.cache[accepted]; ok {
			acceptedSource = record.SourceID
		}
		s.incidents[height] = IntegrityIncident{height, accepted, conflicting, acceptedSource, e.id, time.Now().UTC()}
	}
}
func (s *CommittedSource) fenceLocked(height uint64) error {
	for boundary, incident := range s.incidents {
		if height >= boundary {
			resolved := false
			for _, endpoint := range s.endpoints {
				if a := endpoint.selection; a != nil && !endpoint.quarantined && a.Height == boundary && a.Hash == incident.AcceptedHash && a.Expires.After(time.Now()) {
					resolved = true
					break
				}
			}
			if !resolved {
				return sourceError(IntegrityError, incident.ConflictingSourceID, nil)
			}
		}
	}
	return nil
}
func (s *CommittedSource) EndpointHealth() []EndpointHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]EndpointHealth, 0, len(s.endpoints))
	for _, e := range s.endpoints {
		health := e.health
		health.SourceID = e.id
		out = append(out, health)
	}
	return out
}
func (s *CommittedSource) IntegrityIncidents() []IntegrityIncident {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]IntegrityIncident, 0, len(s.incidents))
	for _, incident := range s.incidents {
		out = append(out, incident)
	}
	return out
}

type RangeTask struct {
	ID           string
	Priority     string
	Deadline     time.Time
	Range        CoveredRange
	Attempt      uint32
	Cancellation context.Context
}
type rangeTaskKey struct{}

func (l *sourceLifecycle) beginRange(parent context.Context, options FetchOptions, historical bool, manifest string) (context.Context, func(), error) {
	ctx, done, err := l.begin(parent)
	if err != nil {
		return nil, nil, err
	}
	priority := "live"
	if historical {
		priority = "historical"
	}
	identity, _ := json.Marshal(struct {
		Priority, Manifest string
		From, To           int64
	}{priority, manifest, options.FromBlock, options.ToBlock})
	hash := sha256.Sum256(identity)
	deadline, _ := ctx.Deadline()
	task := RangeTask{ID: hex.EncodeToString(hash[:]), Priority: priority, Deadline: deadline, Range: CoveredRange{uint64(options.FromBlock), uint64(options.ToBlock)}, Cancellation: ctx}
	return context.WithValue(ctx, rangeTaskKey{}, task), done, nil
}

func (s *CommittedSource) ReportDurableHeight(observedAt time.Time, durable uint64) error {
	s.mu.Lock()
	headHash, headKnown := s.accepted[s.authoritative.Height]
	durableHash, durableKnown := s.accepted[durable]
	head, headCached := s.cache[headHash]
	committed, committedCached := s.cache[durableHash]
	s.mu.Unlock()
	if !headKnown || !durableKnown || !headCached || !committedCached || head.TimestampSeconds < committed.TimestampSeconds || head.Height < durable {
		return sourceError(IncompleteError, "durable-lag", nil)
	}
	seconds := head.TimestampSeconds - committed.TimestampSeconds
	if seconds > int64((1<<63-1)/int64(time.Second)) {
		return sourceError(InvalidError, "durable-lag", nil)
	}
	return s.ReportLiveLag(observedAt, time.Duration(seconds)*time.Second)
}

func (s *CommittedSource) Ready() error {
	if s == nil {
		return sourceError(IdentityError, "pool", nil)
	}
	if s.lifecycle != nil {
		s.lifecycle.mu.Lock()
		closed, pending := s.lifecycle.closed, len(s.lifecycle.active)
		s.lifecycle.mu.Unlock()
		if closed {
			return sourceError(ClosedError, "scheduler", nil)
		}
		if pending >= 1024 {
			return sourceError(OverloadError, "scheduler", nil)
		}
	}
	if len(s.eligible()) == 0 {
		return sourceError(IdentityError, "pool", nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	boundary := s.authoritative.Height
	if boundary < ^uint64(0) {
		boundary++
	}
	return s.fenceLocked(boundary)
}
