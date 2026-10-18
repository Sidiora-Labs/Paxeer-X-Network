package source

// Production source contract. The legacy constructors remain available for
// compatibility; production callers must supply an approved chain fingerprint.
import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

type ErrorKind string

const (
	IdentityError      ErrorKind = "identity"
	InvalidError       ErrorKind = "invalid_response"
	OversizeError      ErrorKind = "oversized_body"
	TimeoutError       ErrorKind = "timeout"
	ThrottleError      ErrorKind = "remote_throttle"
	RPCError           ErrorKind = "semantic_rpc"
	RangeLimitError    ErrorKind = "range_limit"
	IncompleteError    ErrorKind = "incomplete"
	IntegrityError     ErrorKind = "integrity"
	ClosedError        ErrorKind = "closed"
	OverloadError      ErrorKind = "overload"
	AuthorizationError ErrorKind = "authorization"
)

type SourceError struct {
	Kind       ErrorKind
	SourceID   string
	RetryAfter time.Duration
	Cause      error
}

func (e *SourceError) Error() string { return fmt.Sprintf("source %s: %s", e.SourceID, e.Kind) }
func (e *SourceError) Unwrap() error { return e.Cause }
func sourceError(k ErrorKind, id string, e error) error {
	return &SourceError{Kind: k, SourceID: id, Cause: e}
}

type HeadObservation struct {
	ChainID    uint64
	Height     uint64
	Hash       common.Hash
	ObservedAt time.Time
	SourceID   string
}
type BlockRecord struct {
	ChainID          uint64
	Height           uint64
	Hash, ParentHash common.Hash
	TimestampSeconds int64
	TransactionCount int
	SourceID         string
}
type CoveredRange struct{ FromHeight, ToHeight uint64 }
type AddressEpoch struct {
	Address, Role, ABIRevision, ManifestDigest string
	ActivationHeight                           uint64
	RetirementHeight                           *uint64
	CreationTxIndex                            uint
}

// CommittedAnchor pins chain identity to an operator-issued (height, hash)
// inside the retention window of pruned nodes, which cannot serve block 0.
type CommittedAnchor struct {
	Height uint64
	Hash   common.Hash
}

type CommittedOptions struct {
	Endpoints map[string]string // bounded non-secret endpoint IDs -> configured URLs
	// HistoricalEndpoints are archive endpoints used only after every primary
	// endpoint failed a read (typically a height below the primary's pruning
	// floor). They never decide the head while a primary is eligible.
	HistoricalEndpoints map[string]string `json:",omitempty"`
	GenesisHash         common.Hash       `json:",omitempty"`
	// Anchor replaces GenesisHash on pruned chains; exactly one is configured.
	Anchor *CommittedAnchor `json:",omitempty"`
	// BlockIdentity is "keccak" (default) or "reported": on a CometBFT-backed
	// EVM the receiptsRoot is not an Ethereum receipt trie, so the receipt
	// fallback binds receipts by block hash and log bloom instead.
	BlockIdentity string `json:",omitempty"`
	// AnchorAttested admits endpoints whose anchor block is pruned. The caller
	// sets it only after durable, anchor-descended ingestion was verified.
	AnchorAttested             bool `json:"-"`
	ManifestDigest             string
	Epochs                     []AddressEpoch
	LiveDeadline, BulkDeadline time.Duration
	MaxResponseBytes           int64                    // may reduce, never increase, the 32 MiB limit
	SelectionAuthority         SourceSelectionAuthority `json:"-"`
	// HistoricalConcurrency bounds the requests in flight to each historical
	// endpoint, readahead included (0: default 48; at most 96).
	HistoricalConcurrency int `json:"-"`
	// HistoricalReadahead is how many blocks below the primaries' pruning
	// floor are read ahead of the range being fetched (0: default 256;
	// negative: disabled; at most 1024).
	HistoricalReadahead int `json:"-"`
	// HistoricalAttemptTimeout bounds one request attempt to a historical
	// endpoint; an unanswered attempt is retried within the bulk deadline
	// (0: default 8s; at most the bulk deadline).
	HistoricalAttemptTimeout time.Duration `json:"-"`
}
type committedEndpoint struct {
	id, url        string
	historical     bool
	permits        chan struct{}
	headPermit     chan struct{}
	bulkHeadPermit chan struct{}
	quarantined    bool
	bulkPermits    chan struct{}
	health         EndpointHealth
	selection      *SourceSelectionAuthorization
	generation     uint64
	recovering     bool
	failures       uint64
	// floor is the pruning floor a primary reported ("earliest available");
	// ranges starting below it are not sent to it.
	floor uint64
}
type CommittedSource struct {
	opts            CommittedOptions
	endpoints       []*committedEndpoint
	client          *http.Client
	ids             atomic.Uint64
	mu              sync.Mutex
	cache           map[common.Hash]BlockRecord
	cacheOrder      []common.Hash
	accepted        map[uint64]common.Hash
	polling         bool
	metrics         sourceMetrics
	acceptedOrder   []uint64
	acceptedVersion uint64
	incidents       map[uint64]IntegrityIncident
	authoritative   HeadObservation
	lifecycle       *sourceLifecycle
	backfill        backfillControl
	transport       transportMetrics
	ahead           readahead
}

var processPermits = make(chan struct{}, 64)
var historicalPermits = make(chan struct{}, 16)

func NewCommittedSource(ctx context.Context, o CommittedOptions) (*CommittedSource, error) {
	pinned := o.GenesisHash != (common.Hash{})
	anchored := o.Anchor != nil && o.Anchor.Hash != (common.Hash{})
	if len(o.Endpoints) == 0 || len(o.Endpoints)+len(o.HistoricalEndpoints) > 64 || pinned == anchored || o.Anchor != nil && !anchored || o.ManifestDigest == "" || (o.BlockIdentity != "" && o.BlockIdentity != "keccak" && o.BlockIdentity != "reported") {
		return nil, sourceError(IdentityError, "config", nil)
	}
	if o.LiveDeadline <= 0 {
		o.LiveDeadline = 2 * time.Second
	}
	if o.BulkDeadline <= 0 {
		o.BulkDeadline = 15 * time.Second
	}
	if o.LiveDeadline > 2*time.Second || o.BulkDeadline > 15*time.Second {
		return nil, sourceError(InvalidError, "config", nil)
	}
	if o.HistoricalConcurrency == 0 {
		o.HistoricalConcurrency = defaultHistoricalConcurrency
	}
	if o.HistoricalReadahead == 0 {
		o.HistoricalReadahead = defaultHistoricalReadahead
	}
	if o.HistoricalAttemptTimeout == 0 {
		o.HistoricalAttemptTimeout = min(defaultHistoricalAttempt, o.BulkDeadline)
	}
	if o.HistoricalConcurrency < 1 || o.HistoricalConcurrency > maxHistoricalConcurrency || o.HistoricalReadahead > maxHistoricalReadahead || o.HistoricalAttemptTimeout < 0 || o.HistoricalAttemptTimeout > o.BulkDeadline {
		return nil, sourceError(InvalidError, "config", nil)
	}
	o.Epochs = cloneEpochs(o.Epochs)
	if o.MaxResponseBytes <= 0 {
		o.MaxResponseBytes = 32 << 20
	}
	if o.MaxResponseBytes > 32<<20 {
		return nil, sourceError(OversizeError, "config", nil)
	}
	if err := validateEpochs(o.Epochs, o.ManifestDigest); err != nil {
		return nil, err
	}
	// Keep-alive pool sized to the per-endpoint request permits: the default
	// transport keeps only two idle connections per host, so parallel block
	// reads would otherwise pay a new TLS handshake per request.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 256
	transport.MaxIdleConnsPerHost = max(32, o.HistoricalConcurrency)
	transport.MaxConnsPerHost = max(32, o.HistoricalConcurrency)
	s := &CommittedSource{opts: o, client: &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect denied") }}, cache: map[common.Hash]BlockRecord{}, accepted: map[uint64]common.Hash{}, incidents: map[uint64]IntegrityIncident{}, lifecycle: newSourceLifecycle()}
	var pruned []*committedEndpoint
	for _, group := range []struct {
		endpoints  map[string]string
		historical bool
	}{{o.Endpoints, false}, {o.HistoricalEndpoints, true}} {
		ids := make([]string, 0, len(group.endpoints))
		for id := range group.endpoints {
			if !validSourceID(id) {
				return nil, sourceError(InvalidError, "config", nil)
			}
			if group.historical {
				if _, duplicate := o.Endpoints[id]; duplicate {
					return nil, sourceError(InvalidError, "config", nil)
				}
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			bulkPermits, permits := 4, 16
			if group.historical {
				// The archive serves whole ranges below the primaries' floor:
				// slow per request, but it sustains many requests in flight.
				bulkPermits, permits = o.HistoricalConcurrency, o.HistoricalConcurrency
			}
			e := &committedEndpoint{id: id, url: group.endpoints[id], historical: group.historical, permits: make(chan struct{}, permits), headPermit: make(chan struct{}, 1), bulkHeadPermit: make(chan struct{}, 1), bulkPermits: make(chan struct{}, bulkPermits)}
			// An archive answers in seconds: it is admitted under the bulk
			// deadline (with its attempt retries), never the 2 s live one.
			if err := s.admitIdentity(ctx, e, group.historical); err != nil {
				if s.opts.Anchor != nil && prunedSource(err) {
					pruned = append(pruned, e)
				}
				continue
			}
			s.endpoints = append(s.endpoints, e)
		}
	}
	// An endpoint that has pruned the anchor is admitted when it agrees with an
	// anchor-verified endpoint on a block both still serve: chain identity is
	// then transitive from the anchor (for example a pruned primary next to an
	// archive endpoint).
	for _, e := range pruned {
		if s.corroborate(ctx, e) == nil {
			s.endpoints = append(s.endpoints, e)
		}
	}
	sort.SliceStable(s.endpoints, func(i, j int) bool { return !s.endpoints[i].historical && s.endpoints[j].historical })
	primary := false
	for _, e := range s.endpoints {
		primary = primary || !e.historical
	}
	if !primary {
		_ = s.Close()
		return nil, sourceError(IdentityError, "pool", nil)
	}
	return s, nil
}

// admitIdentity checks an endpoint's chain id and genesis or anchor pin. An
// anchor below the endpoint's pruning floor is accepted only when the caller
// attested anchor-descended durable state (AnchorAttested).
func (s *CommittedSource) admitIdentity(ctx context.Context, e *committedEndpoint, bulk bool) error {
	var chain string
	if err := s.rpc(ctx, e, bulk, "eth_chainId", []any{}, &chain); err != nil {
		return err
	}
	if chain != "0x7d" {
		return sourceError(IdentityError, e.id, nil)
	}
	height, want := "0x0", s.opts.GenesisHash
	if s.opts.Anchor != nil {
		height, want = fmtHeight(s.opts.Anchor.Height), s.opts.Anchor.Hash
	}
	var pin blockWire
	if err := s.rpc(ctx, e, bulk, "eth_getBlockByNumber", []any{height, false}, &pin); err != nil {
		if s.opts.Anchor != nil && s.opts.AnchorAttested && prunedSource(err) {
			return nil
		}
		return err
	}
	if !validHash(pin.Hash) || common.HexToHash(pin.Hash) != want {
		return sourceError(IdentityError, e.id, nil)
	}
	return nil
}

// corroborate checks a candidate endpoint against every admitted endpoint at
// the lower of their current heads: the reported block hashes must agree.
func (s *CommittedSource) corroborate(ctx context.Context, candidate *committedEndpoint) error {
	if len(s.endpoints) == 0 {
		return sourceError(IdentityError, candidate.id, nil)
	}
	for _, verified := range s.endpoints {
		_, mine, err := s.readBlock(ctx, candidate, "latest", candidate.historical)
		if err != nil {
			return err
		}
		_, theirs, err := s.readBlock(ctx, verified, "latest", verified.historical)
		if err != nil {
			return err
		}
		height := min(mine.Height, theirs.Height)
		_, a, err := s.readBlock(ctx, candidate, fmtHeight(height), candidate.historical)
		if err != nil {
			return err
		}
		_, b, err := s.readBlock(ctx, verified, fmtHeight(height), verified.historical)
		if err != nil {
			return err
		}
		if a.Height != height || b.Height != height || a.Hash != b.Hash {
			return sourceError(IdentityError, candidate.id, nil)
		}
	}
	return nil
}

func prunedSource(err error) bool {
	var se *SourceError
	return errors.As(err, &se) && se.Kind == RPCError && se.Cause != nil && strings.Contains(strings.ToLower(se.Cause.Error()), "pruned")
}
func cloneEpochs(es []AddressEpoch) []AddressEpoch {
	out := append([]AddressEpoch(nil), es...)
	for i := range out {
		if out[i].RetirementHeight != nil {
			height := *out[i].RetirementHeight
			out[i].RetirementHeight = &height
		}
	}
	return out
}
func validateEpochs(es []AddressEpoch, d string) error {
	if len(es) == 0 || len(es) > 4096 {
		return sourceError(IdentityError, "manifest", nil)
	}
	for _, e := range es {
		if !common.IsHexAddress(e.Address) || e.ManifestDigest != d || e.Role == "" || e.ABIRevision == "" || (e.RetirementHeight != nil && *e.RetirementHeight <= e.ActivationHeight) {
			return sourceError(IdentityError, "manifest", nil)
		}
	}
	for i, a := range es {
		for _, b := range es[i+1:] {
			if !strings.EqualFold(a.Address, b.Address) {
				continue
			}
			if (a.RetirementHeight == nil || b.ActivationHeight < *a.RetirementHeight) && (b.RetirementHeight == nil || a.ActivationHeight < *b.RetirementHeight) {
				return sourceError(IdentityError, "manifest-overlap", nil)
			}
		}
	}
	return nil
}
func (s *CommittedSource) Name() string { return "committed-rpc" }
func (s *CommittedSource) Close() error {
	var err error
	if s.lifecycle != nil {
		err = s.lifecycle.close()
	}
	if s.client != nil {
		s.client.CloseIdleConnections()
	}
	return err
}
func acquire(ctx context.Context, ch chan struct{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case ch <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *CommittedSource) rpc(ctx context.Context, e *committedEndpoint, bulk bool, method string, params []any, out any) error {
	deadline := s.opts.LiveDeadline
	if bulk {
		deadline = s.opts.BulkDeadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	for attempt := 0; attempt < 4; attempt++ {
		attemptContext := ctx
		if task, ok := ctx.Value(rangeTaskKey{}).(RangeTask); ok {
			task.Attempt = uint32(attempt)
			attemptContext = context.WithValue(ctx, rangeTaskKey{}, task)
		}
		err := s.rpcOnce(attemptContext, e, bulk, method, params, out, attempt)
		s.learnFloor(e, err)
		var slow *SourceError
		if e.historical && attempt < 3 && ctx.Err() == nil && errors.As(err, &slow) && slow.Kind == TimeoutError {
			// An archive attempt that outlived its own window is abandoned and
			// retried while the request's deadline lasts; the archive is the
			// only source below the floor and is never quarantined for it.
			s.transport.timeouts.Add(1)
			continue
		}
		var responseFailure *SourceError
		if errors.As(err, &responseFailure) && (responseFailure.Kind == InvalidError || responseFailure.Kind == IncompleteError) {
			s.transport.invalidResponses.Add(1)
		}
		var timeout *SourceError
		if errors.As(err, &timeout) && timeout.Kind == TimeoutError {
			s.transport.timeouts.Add(1)
		}
		var se *SourceError
		if !errors.As(err, &se) || se.Kind != ThrottleError {
			s.mu.Lock()
			if err == nil {
				e.failures = 0
			} else if errors.As(err, &se) && !e.historical && (se.Kind == InvalidError || se.Kind == TimeoutError && !errors.Is(err, context.Canceled)) {
				// The archive is the only source below the primaries' floor:
				// slow answers are retried, never quarantined.
				e.failures++
				if e.failures >= 3 {
					e.quarantined = true
					e.generation++
					e.selection = nil
					e.health.QuarantineReason = string(se.Kind)
					if e.health.FirstFailureAt.IsZero() {
						e.health.FirstFailureAt = time.Now().UTC()
					}
				}
			}
			s.mu.Unlock()
			return err
		}
		if attempt == 3 {
			return err
		}
	}
	return sourceError(ThrottleError, e.id, nil)
}
func (s *CommittedSource) rpcOnce(ctx context.Context, e *committedEndpoint, bulk bool, method string, params []any, out any, attempt int) error {
	deadline := s.opts.LiveDeadline
	if bulk {
		deadline = s.opts.BulkDeadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	started := time.Now()
	queued := false
	defer func() {
		if queued {
			s.transport.historicalQueue.Add(-1)
		}
	}()
	if bulk {
		s.transport.historicalQueue.Add(1)
		queued = true
		if err := s.waitHistorical(ctx); err != nil {
			return err
		}
		if e.bulkPermits != nil {
			if err := acquire(ctx, e.bulkPermits); err != nil {
				return sourceError(TimeoutError, e.id, err)
			}
			defer func() { <-e.bulkPermits }()
		}
		if !e.historical {
			// Archive endpoints are bounded by their own permits above.
			if err := acquire(ctx, historicalPermits); err != nil {
				return sourceError(TimeoutError, e.id, err)
			}
			defer func() { <-historicalPermits }()
		}
	}
	// Endpoint first: a saturated endpoint never reserves process capacity.
	if err := acquire(ctx, e.permits); err != nil {
		return sourceError(TimeoutError, e.id, err)
	}
	defer func() { <-e.permits }()
	if !e.historical {
		// Archive endpoints are bounded by their own permits (at most 96 per
		// endpoint) and never draw on the process pool, so archive backfill
		// cannot erode the live reservation.
		if err := acquire(ctx, processPermits); err != nil {
			return sourceError(TimeoutError, e.id, err)
		}
		defer func() { <-processPermits }()
	}
	if queued {
		s.transport.historicalQueue.Add(-1)
		queued = false
	}
	if !bulk {
		s.transport.liveWaitNanos.Add(time.Since(started).Nanoseconds())
	}
	if e.historical && s.opts.HistoricalAttemptTimeout > 0 {
		// The attempt window starts once permits are held: waiting behind
		// other archive reads is not slowness of this request.
		var cancelAttempt context.CancelFunc
		ctx, cancelAttempt = context.WithTimeout(ctx, s.opts.HistoricalAttemptTimeout)
		defer cancelAttempt()
	}
	s.transport.inFlight.Add(1)
	defer s.transport.inFlight.Add(-1)
	if attempt > 0 {
		s.transport.retries.Add(1)
	}
	id := s.ids.Add(1)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, "POST", e.url, bytes.NewReader(body))
	if err != nil {
		return sourceError(InvalidError, e.id, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return sourceError(TimeoutError, e.id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		s.transport.throttles.Add(1)
		wait := (100 * time.Millisecond) << attempt
		if n, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			wait = time.Duration(n) * time.Second
		} else if t, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil {
			wait = time.Until(t)
		}
		if backoff := (100 * time.Millisecond) << attempt; wait < backoff {
			wait = backoff
		}
		if wait > 5*time.Second {
			wait = 5 * time.Second
		}
		// Delay is cancellable and bounded; caller can fail over afterward.
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return sourceError(TimeoutError, e.id, ctx.Err())
		}
		return &SourceError{Kind: ThrottleError, SourceID: e.id, RetryAfter: wait}
	}
	if e.historical && (resp.StatusCode == 502 || resp.StatusCode == 503 || resp.StatusCode == 504) {
		// An overloaded archive gateway: back off and retry like a throttle
		// (bounded, cancellable, never a quarantine).
		s.transport.throttles.Add(1)
		wait := (250 * time.Millisecond) << attempt
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return sourceError(TimeoutError, e.id, ctx.Err())
		}
		return &SourceError{Kind: ThrottleError, SourceID: e.id, RetryAfter: wait, Cause: fmt.Errorf("http status %d", resp.StatusCode)}
	}
	if resp.StatusCode != 200 {
		return sourceError(RPCError, e.id, fmt.Errorf("http status %d", resp.StatusCode))
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, s.opts.MaxResponseBytes+1))
	if err != nil {
		return sourceError(TimeoutError, e.id, err)
	}
	s.transport.responseBytes.Add(uint64(len(b)))
	if int64(len(b)) > s.opts.MaxResponseBytes {
		return sourceError(OversizeError, e.id, nil)
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      uint64          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int
			Message string
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil || envelope.ID != id || envelope.JSONRPC != "2.0" {
		return sourceError(InvalidError, e.id, err)
	}
	if envelope.Error != nil {
		kind := RPCError
		message := strings.ToLower(envelope.Error.Message)
		if envelope.Error.Code == -32005 || strings.Contains(message, "block range") || strings.Contains(message, "too many results") || strings.Contains(message, "query returned more than") || strings.Contains(message, "response size exceeded") || strings.Contains(message, "logs limit exceeded") {
			kind = RangeLimitError
		}
		return sourceError(kind, e.id, fmt.Errorf("rpc code %d: %s", envelope.Error.Code, message))
	}
	if len(envelope.Result) == 0 || string(envelope.Result) == "null" {
		return sourceError(IncompleteError, e.id, nil)
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return sourceError(InvalidError, e.id, err)
	}
	return nil
}
func validHash(h string) bool {
	if len(h) != 66 || !strings.HasPrefix(h, "0x") {
		return false
	}
	b, err := hex.DecodeString(h[2:])
	return err == nil && len(b) == 32
}

func parseCommittedUint64(value string) (uint64, error) {
	if !strings.HasPrefix(value, "0x") || len(value) <= 2 {
		return 0, errors.New("invalid RPC quantity")
	}
	return strconv.ParseUint(value[2:], 16, 64)
}
func parseCommittedTimestamp(value string) (int64, error) {
	n, err := parseCommittedUint64(value)
	if err != nil || n > uint64(1<<63-1) {
		return 0, errors.New("invalid RPC timestamp")
	}
	return int64(n), nil
}

type blockWire struct {
	Number, Hash, ParentHash, Timestamp string
	Transactions                        []ethTxWire
	ReceiptsRoot                        string `json:"receiptsRoot"`
	LogsBloom                           string `json:"logsBloom"`
}

func (s *CommittedSource) readBlock(ctx context.Context, e *committedEndpoint, tag string, bulk bool) (blockWire, BlockRecord, error) {
	var b blockWire
	if err := s.rpc(ctx, e, bulk, "eth_getBlockByNumber", []any{tag, true}, &b); err != nil {
		return b, BlockRecord{}, err
	}
	n, err := parseCommittedUint64(b.Number)
	ts, terr := parseCommittedTimestamp(b.Timestamp)
	if err != nil || terr != nil || ts <= 0 || !validHash(b.Hash) || !validHash(b.ParentHash) || b.Transactions == nil {
		return b, BlockRecord{}, sourceError(IncompleteError, e.id, nil)
	}
	r := BlockRecord{125, n, common.HexToHash(b.Hash), common.HexToHash(b.ParentHash), ts, len(b.Transactions), e.id}
	return b, r, nil
}
func (s *CommittedSource) block(ctx context.Context, e *committedEndpoint, tag string, bulk bool) (blockWire, BlockRecord, error) {
	b, r, err := s.readBlock(ctx, e, tag, bulk)
	if err == nil {
		err = s.retainBlock(e, r)
	}
	return b, r, err
}
func (s *CommittedSource) retainBlock(e *committedEndpoint, r BlockRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := r.Height
	if h, ok := s.accepted[n]; ok && h != r.Hash {
		s.disagreeLocked(e, n, h, r.Hash)
		return sourceError(IntegrityError, e.id, nil)
	}
	if _, ok := s.accepted[n]; !ok {
		s.acceptedVersion++
		s.acceptedOrder = append(s.acceptedOrder, n)
		s.accepted[n] = r.Hash
		if len(s.acceptedOrder) > 16384 {
			delete(s.accepted, s.acceptedOrder[0])
			s.acceptedOrder = s.acceptedOrder[1:]
		}
	}
	if known, ok := s.cache[r.Hash]; ok {
		if known.ChainID != r.ChainID || known.Height != r.Height || known.ParentHash != r.ParentHash || known.TimestampSeconds != r.TimestampSeconds || known.TransactionCount != r.TransactionCount {
			s.disagreeLocked(e, n, known.Hash, r.Hash)
			return sourceError(IntegrityError, e.id, nil)
		}
		s.metrics.cacheHits.Add(1)
	}
	if _, ok := s.cache[r.Hash]; !ok {
		if len(s.cacheOrder) == 16384 {
			delete(s.cache, s.cacheOrder[0])
			s.cacheOrder = s.cacheOrder[1:]
		}
		s.cacheOrder = append(s.cacheOrder, r.Hash)
		s.cache[r.Hash] = r
	}
	if r.Height >= e.health.LastGoodHeight {
		e.health.LastGoodHeight = r.Height
		e.health.LastHash = r.Hash
	}
	return nil
}
func (s *CommittedSource) eligible() []*committedEndpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	var es []*committedEndpoint
	for _, e := range s.endpoints {
		if e.selection != nil && !e.selection.Expires.After(time.Now()) {
			e.selection = nil
			e.quarantined = true
			e.generation++
			e.health.QuarantineReason = "authorization_expired"
			if e.health.FirstFailureAt.IsZero() {
				e.health.FirstFailureAt = time.Now().UTC()
			}
		}
		if !e.quarantined && !e.historical {
			es = append(es, e)
		}
	}
	for _, e := range es {
		if e.selection != nil && e.selection.Expires.After(time.Now()) {
			return []*committedEndpoint{e}
		}
	}
	// Historical (archive) endpoints are tried only after every primary.
	for _, e := range s.endpoints {
		if !e.quarantined && e.historical {
			es = append(es, e)
		}
	}
	return es
}

// headEndpoints are the endpoints that observe the chain head: the eligible
// primaries, or the historical endpoints only when no primary is eligible.
func (s *CommittedSource) headEndpoints() []*committedEndpoint {
	all := s.eligible()
	primaries := make([]*committedEndpoint, 0, len(all))
	for _, e := range all {
		if !e.historical {
			primaries = append(primaries, e)
		}
	}
	if len(primaries) > 0 {
		return primaries
	}
	return all
}
func (s *CommittedSource) ObserveHead(ctx context.Context, durable uint64) (HeadObservation, error) {
	return s.observeHead(ctx, durable, false)
}
func (s *CommittedSource) observeHead(ctx context.Context, durable uint64, bulk bool) (HeadObservation, error) {
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return HeadObservation{}, err
		}
		defer done()
	}
	deadline := s.opts.LiveDeadline
	if bulk {
		deadline = s.opts.BulkDeadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	type observation struct {
		block BlockRecord
		err   error
	}
	endpoints := s.headEndpoints()
	results := make(chan observation, len(endpoints))
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		wg.Add(1)
		go func(e *committedEndpoint) {
			defer wg.Done()
			permit := e.headPermit
			if bulk {
				permit = e.bulkHeadPermit
			}
			if err := acquire(ctx, permit); err != nil {
				results <- observation{err: sourceError(TimeoutError, e.id, err)}
				return
			}
			defer func() { <-permit }()
			_, block, err := s.block(ctx, e, "latest", bulk)
			results <- observation{block, err}
		}(endpoint)
	}
	defer func() { cancel(); wg.Wait() }()
	var best BlockRecord
	var last error
	var settle <-chan time.Time
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	remaining := len(endpoints)
	for remaining > 0 {
		select {
		case result := <-results:
			remaining--
			if result.err != nil {
				last = result.err
				continue
			}
			if result.block.Height < durable {
				last = sourceError(IntegrityError, result.block.SourceID, nil)
				continue
			}
			if best.Hash == (common.Hash{}) || result.block.Height > best.Height {
				best = result.block
			}
			if timer == nil {
				timer = time.NewTimer(50 * time.Millisecond)
				settle = timer.C
			}
		case <-settle:
			remaining = 0
		case <-ctx.Done():
			if best.Hash == (common.Hash{}) {
				return HeadObservation{}, ctx.Err()
			}
			remaining = 0
		}
	}
	cancel()
	wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if best.Hash == (common.Hash{}) {
		if last == nil {
			last = sourceError(IdentityError, "pool", nil)
		}
		return HeadObservation{}, last
	}
	if err := s.fenceLocked(max(best.Height, s.authoritative.Height)); err != nil {
		return HeadObservation{}, err
	}
	if best.Height < s.authoritative.Height {
		return s.authoritative, nil
	}
	s.authoritative = HeadObservation{125, best.Height, best.Hash, time.Now().UTC(), best.SourceID}
	return s.authoritative, nil
}
func committedPollInterval() time.Duration {
	return 100*time.Millisecond + time.Duration(rand.Int64N(int64(20*time.Millisecond)+1)) - 10*time.Millisecond
}
func (s *CommittedSource) PollHeads(ctx context.Context, durable func() uint64, deliver func(HeadObservation, error) error) error {
	if durable == nil || deliver == nil {
		return sourceError(InvalidError, "poll", nil)
	}
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return err
		}
		defer done()
	}
	s.mu.Lock()
	if s.polling {
		s.mu.Unlock()
		return sourceError(InvalidError, "poll", nil)
	}
	s.polling = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.polling = false
		s.mu.Unlock()
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		started := time.Now()
		interval := committedPollInterval()
		head, err := s.ObserveHead(ctx, durable())
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := deliver(head, err); err != nil {
			return err
		}
		timer := time.NewTimer(max(time.Duration(0), interval-time.Since(started)))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (s *CommittedSource) Head(ctx context.Context) (int64, error) {
	h, e := s.ObserveHead(ctx, 0)
	return int64(h.Height), e
}

// FetchCommitted accepts only coordinator-approved discoveries, supplied as
// immutable epoch input after factory/proxy validation. CreationTxIndex gates
// same-block discovery without discarding transfers later in that block.
func (s *CommittedSource) FetchCommitted(ctx context.Context, o FetchOptions, historical bool, discoveries []AddressEpoch) (FetchResult, error) {
	deadline := s.opts.LiveDeadline
	if historical {
		deadline = s.opts.BulkDeadline
	}
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.beginRange(ctx, o, historical, s.opts.ManifestDigest)
		if err != nil {
			return FetchResult{}, err
		}
		defer done()
	}
	limit := int64(128)
	if historical {
		limit = 2000
	}
	if o.FromBlock < 0 || o.ToBlock < o.FromBlock || o.ToBlock-o.FromBlock >= limit {
		return FetchResult{}, sourceError(InvalidError, "range", nil)
	}
	es := append(cloneEpochs(s.opts.Epochs), cloneEpochs(discoveries)...)
	if err := validateEpochs(es, s.opts.ManifestDigest); err != nil {
		return FetchResult{}, err
	}
	var last error
	for _, e := range s.eligible() {
		if s.prunedFor(e, uint64(o.FromBlock)) {
			last = sourceError(RPCError, e.id, errors.New("range starts below the endpoint's reported pruning floor"))
			continue
		}
		// An archive endpoint serves ranges the primaries have pruned; it is
		// read under the bulk deadline and permits with its own time budget.
		bulk, budget := historical, deadline
		if e.historical {
			bulk, budget = true, s.opts.BulkDeadline
		}
		attempt, cancel := context.WithTimeout(ctx, budget)
		r, err := s.fetch(attempt, e, o, bulk, es)
		cancel()
		if err == nil {
			s.mu.Lock()
			err = s.fenceLocked(uint64(o.ToBlock))
			s.mu.Unlock()
			if err != nil {
				return FetchResult{}, err
			}
			return r, nil
		}
		last = err
		if ctx.Err() != nil {
			return FetchResult{}, ctx.Err()
		}
	}
	if last == nil {
		last = sourceError(IdentityError, "pool", nil)
	}
	return FetchResult{}, last
}
func (s *CommittedSource) FetchLogs(ctx context.Context, o FetchOptions) (FetchResult, error) {
	return s.FetchCommitted(ctx, o, false, nil)
}
func (s *CommittedSource) fetch(ctx context.Context, e *committedEndpoint, o FetchOptions, bulk bool, epochs []AddressEpoch) (FetchResult, error) {
	deadline := s.opts.LiveDeadline
	if bulk {
		deadline = s.opts.BulkDeadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	payloadBytes := 0
	addresses := []string{}
	seen := map[string]bool{}
	for _, ep := range epochs {
		a := strings.ToLower(ep.Address)
		if !seen[a] && ep.ActivationHeight <= uint64(o.ToBlock) && (ep.RetirementHeight == nil || *ep.RetirementHeight > uint64(o.FromBlock)) {
			seen[a] = true
			addresses = append(addresses, a)
		}
	}
	sort.Strings(addresses)
	batch := map[uint64]batchBlock{}
	// Blocks are read concurrently with the log query; the prefetched set is
	// merged before the sequential linkage checks below.
	prefetched := map[uint64]batchBlock{}
	prefetchDone := make(chan error, 1)
	if e.historical {
		s.dropAheadBelow(uint64(o.FromBlock))
	}
	go func() { prefetchDone <- s.prefetchBlocks(ctx, e, o.FromBlock, o.ToBlock, bulk, prefetched) }()
	r := FetchResult{BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}, Blocks: map[uint64]BlockRecord{}, ManifestDigest: s.opts.ManifestDigest}
	if e.historical {
		// The archive's eth_getLogs scans every block of the range; the
		// blocks are read anyway, and their logsBloom names the few that can
		// hold a fetched address.
		if err := <-prefetchDone; err != nil {
			return FetchResult{}, err
		}
		prefetchDone <- nil
		for height, block := range prefetched {
			batch[height] = block
		}
		logs, err := s.archiveLogs(ctx, e, o.FromBlock, o.ToBlock, addresses, bulk, batch)
		if err != nil {
			return FetchResult{}, err
		}
		for _, log := range logs {
			payloadBytes += 256 + len(log.Data) + 32*len(log.Topics)
			if payloadBytes > 32<<20 {
				return FetchResult{}, sourceError(OversizeError, e.id, nil)
			}
		}
		r.Logs = logs
		addresses = nil
	}
	for start := 0; start < len(addresses); start += 256 {
		end := start + 256
		if end > len(addresses) {
			end = len(addresses)
		}
		logs, err := s.logs(ctx, e, o.FromBlock, o.ToBlock, addresses[start:end], bulk, batch)
		if err != nil {
			<-prefetchDone
			return FetchResult{}, err
		}
		for _, log := range logs {
			payloadBytes += 256 + len(log.Data) + 32*len(log.Topics)
			if payloadBytes > 32<<20 {
				return FetchResult{}, sourceError(OversizeError, e.id, nil)
			}
		}
		r.Logs = append(r.Logs, logs...)
	}
	if err := <-prefetchDone; err != nil {
		return FetchResult{}, err
	}
	for height, block := range prefetched {
		if _, ok := batch[height]; !ok {
			batch[height] = block
		}
	}
	for offset := int64(0); offset <= o.ToBlock-o.FromBlock; offset++ {
		n := o.FromBlock + offset
		b, record, err := s.batchBlock(ctx, e, uint64(n), bulk, batch)
		if err != nil {
			return FetchResult{}, err
		}
		if record.Height != uint64(n) {
			return FetchResult{}, sourceError(IntegrityError, e.id, nil)
		}
		if n > o.FromBlock && record.ParentHash != r.Blocks[uint64(n-1)].Hash {
			return FetchResult{}, sourceError(IntegrityError, e.id, nil)
		}
		r.Blocks[uint64(n)] = record
		r.BlockTimestamps[uint64(n)] = record.TimestampSeconds
		for i := range r.Logs {
			l := &r.Logs[i]
			if l.BlockNumber != uint64(n) {
				continue
			}
			if l.BlockHash != record.Hash || int(l.TxIndex) >= len(b.Transactions) || !strings.EqualFold(b.Transactions[l.TxIndex].Hash, l.TxHash.Hex()) {
				return FetchResult{}, sourceError(IntegrityError, e.id, nil)
			}
			if len(l.Topics) > 0 && needsTxFrom(l.Topics[0].Hex()) {
				from := b.Transactions[l.TxIndex].From
				if !common.IsHexAddress(from) {
					return FetchResult{}, sourceError(IncompleteError, e.id, nil)
				}
				r.TxFromMap[strings.ToLower(l.TxHash.Hex())] = strings.ToLower(from)
			}
		}
	}
	identities := map[string]bool{}
	for _, l := range r.Logs {
		if l.BlockNumber < uint64(o.FromBlock) || l.BlockNumber > uint64(o.ToBlock) || l.Removed {
			return FetchResult{}, sourceError(IntegrityError, e.id, nil)
		}
		active := false
		for _, ep := range epochs {
			if strings.EqualFold(ep.Address, l.Address.Hex()) && l.BlockNumber >= ep.ActivationHeight && (ep.RetirementHeight == nil || l.BlockNumber < *ep.RetirementHeight) && (l.BlockNumber != ep.ActivationHeight || l.TxIndex >= ep.CreationTxIndex) {
				active = true
				break
			}
		}
		if !active {
			return FetchResult{}, sourceError(IdentityError, e.id, nil)
		}
		key := fmt.Sprintf("%s/%d", l.BlockHash, l.Index)
		if identities[key] {
			return FetchResult{}, sourceError(IntegrityError, e.id, nil)
		}
		identities[key] = true
	}
	sort.Slice(r.Logs, func(i, j int) bool {
		a, b := r.Logs[i], r.Logs[j]
		if a.BlockNumber != b.BlockNumber {
			return a.BlockNumber < b.BlockNumber
		}
		if a.TxIndex != b.TxIndex {
			return a.TxIndex < b.TxIndex
		}
		return a.Index < b.Index
	})
	s.metrics.blocksFetched.Add(uint64(len(r.Blocks)))
	if len(r.Logs) == 0 {
		s.metrics.emptyRanges.Add(1)
	}
	r.Complete = true
	r.CoveredRanges = []CoveredRange{{uint64(o.FromBlock), uint64(o.ToBlock)}}
	return r, nil
}

// prefetchBlockConcurrency bounds parallel block reads of one range against
// one endpoint (below the endpoint's 16 request permits).
const prefetchBlockConcurrency = 12

// prefetchBlocks reads the range's blocks concurrently from one endpoint into
// batch; chain-order linkage is still checked sequentially by the caller.
func (s *CommittedSource) prefetchBlocks(ctx context.Context, e *committedEndpoint, from, to int64, bulk bool, batch map[uint64]batchBlock) error {
	type result struct {
		height uint64
		block  batchBlock
		err    error
	}
	results := make(chan result, to-from+1)
	slots := make(chan struct{}, s.rangeConcurrency(e))
	var wg sync.WaitGroup
	for n := from; n <= to; n++ {
		wg.Add(1)
		go func(height uint64) {
			defer wg.Done()
			if e.historical {
				if block, ok := s.takeAhead(ctx, e, height); ok {
					results <- result{height, block, nil}
					return
				}
			}
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				results <- result{height: height, err: sourceError(TimeoutError, e.id, ctx.Err())}
				return
			}
			defer func() { <-slots }()
			s.metrics.headerFetches.Add(1)
			wire, record, err := s.block(ctx, e, fmtHeight(height), bulk)
			results <- result{height, batchBlock{wire, record}, err}
		}(uint64(n))
	}
	wg.Wait()
	close(results)
	var first error
	for r := range results {
		if r.err != nil {
			if first == nil {
				first = r.err
			}
			continue
		}
		batch[r.height] = r.block
	}
	return first
}

type batchBlock struct {
	wire   blockWire
	record BlockRecord
}

func (s *CommittedSource) batchBlock(ctx context.Context, e *committedEndpoint, height uint64, bulk bool, batch map[uint64]batchBlock) (blockWire, BlockRecord, error) {
	if b, ok := batch[height]; ok {
		s.metrics.cacheHits.Add(1)
		return b.wire, b.record, nil
	}
	s.metrics.headerFetches.Add(1)
	b, r, err := s.block(ctx, e, fmt.Sprintf("0x%x", height), bulk)
	if err == nil {
		batch[height] = batchBlock{b, r}
	}
	return b, r, err
}
func (s *CommittedSource) logs(ctx context.Context, e *committedEndpoint, from, to int64, addresses []string, bulk bool, batch map[uint64]batchBlock) ([]ethtypes.Log, error) {
	var wire []struct {
		ethLogWire
		BlockHash string `json:"blockHash"`
		TxIndex   string `json:"transactionIndex"`
	}
	err := s.rpc(ctx, e, bulk, "eth_getLogs", []any{map[string]any{"fromBlock": fmt.Sprintf("0x%x", from), "toBlock": fmt.Sprintf("0x%x", to), "address": addresses}}, &wire)
	if err != nil {
		var se *SourceError
		if errors.As(err, &se) && (se.Kind == RangeLimitError || se.Kind == OversizeError) && from < to {
			s.metrics.rangeBisections.Add(1)
			mid := from + (to-from)/2
			a, err := s.logs(ctx, e, from, mid, addresses, bulk, batch)
			if err != nil {
				return nil, err
			}
			b, err := s.logs(ctx, e, mid+1, to, addresses, bulk, batch)
			if err != nil {
				return nil, err
			}
			size := 0
			for _, logs := range [][]ethtypes.Log{a, b} {
				for _, log := range logs {
					size += 256 + len(log.Data) + 32*len(log.Topics)
					if size > 32<<20 {
						return nil, sourceError(OversizeError, e.id, nil)
					}
				}
			}
			return append(a, b...), nil
		}
		if errors.As(err, &se) && (se.Kind == OversizeError || se.Kind == RangeLimitError) && from == to {
			// Full canonical transaction list bounds fallback work. No receipt
			// can be omitted while claiming complete single-block coverage.
			b, record, berr := s.batchBlock(ctx, e, uint64(from), bulk, batch)
			if berr != nil {
				return nil, berr
			}
			if len(b.Transactions) > 4096 {
				return nil, sourceError(IncompleteError, e.id, nil)
			}
			wanted := map[common.Address]bool{}
			for _, a := range addresses {
				wanted[common.HexToAddress(a)] = true
			}
			s.metrics.receiptFallbacks.Add(1)
			if !validHash(b.ReceiptsRoot) {
				return nil, sourceError(IncompleteError, e.id, nil)
			}
			receipts := make(ethtypes.Receipts, 0, len(b.Transactions))
			nextIndex := uint(0)
			used := 0
			var result []ethtypes.Log
			for i, tx := range b.Transactions {
				if !validHash(tx.Hash) {
					return nil, sourceError(IncompleteError, e.id, nil)
				}
				var receipt ethtypes.Receipt
				if err := s.rpc(ctx, e, bulk, "eth_getTransactionReceipt", []any{tx.Hash}, &receipt); err != nil {
					return nil, err
				}
				if receipt.BlockHash != record.Hash || receipt.BlockNumber == nil || !receipt.BlockNumber.IsUint64() || receipt.BlockNumber.Uint64() != uint64(from) || receipt.TransactionIndex != uint(i) || receipt.TxHash != common.HexToHash(tx.Hash) {
					return nil, sourceError(IntegrityError, e.id, nil)
				}
				receipts = append(receipts, &receipt)
				for _, l := range receipt.Logs {
					if l != nil && (l.BlockHash != record.Hash || l.BlockNumber != uint64(from) || l.TxHash != receipt.TxHash || l.TxIndex != uint(i) || l.Index != nextIndex || l.Removed || len(l.Topics) > 4 || len(l.Data) > 64<<10) {
						return nil, sourceError(IntegrityError, e.id, nil)
					}
					if l != nil {
						nextIndex++
						used += 256 + len(l.Data) + 32*len(l.Topics)
						if used > 32<<20 {
							return nil, sourceError(OversizeError, e.id, nil)
						}
					}
					if l == nil {
						return nil, sourceError(IncompleteError, e.id, nil)
					}
					if wanted[l.Address] {
						result = append(result, *l)
					}
				}
			}
			if s.opts.BlockIdentity == "reported" {
				// The receiptsRoot of a CometBFT-backed EVM is not an Ethereum
				// receipt trie; every receipt is bound above to this block's
				// reported hash and transaction, and the merged log bloom must
				// equal the block's.
				bloom, err := hex.DecodeString(strings.TrimPrefix(b.LogsBloom, "0x"))
				if err != nil || len(bloom) != ethtypes.BloomByteLength || ethtypes.MergeBloom(receipts) != ethtypes.BytesToBloom(bloom) {
					return nil, sourceError(IntegrityError, e.id, nil)
				}
				return result, nil
			}
			if ethtypes.DeriveSha(receipts, trie.NewStackTrie(nil)) != common.HexToHash(b.ReceiptsRoot) {
				return nil, sourceError(IntegrityError, e.id, nil)
			}
			return result, nil
		}
		return nil, err
	}
	logs := make([]ethtypes.Log, 0, len(wire))
	for _, w := range wire {
		if !strings.HasPrefix(w.Data, "0x") {
			return nil, sourceError(IncompleteError, e.id, nil)
		}
		if _, err := hex.DecodeString(w.Data[2:]); err != nil {
			return nil, sourceError(IncompleteError, e.id, nil)
		}
		l, err := w.toLog()
		blockNumber, berr := parseCommittedUint64(w.BlockNumber)
		logIndex, ierr := parseCommittedUint64(w.LogIndex)
		tx, terr := parseCommittedUint64(w.TxIndex)
		if err != nil || berr != nil || ierr != nil || terr != nil || logIndex > 1<<32-1 || tx > 1<<32-1 || len(l.Topics) > 4 || len(l.Data) > 64<<10 || !validHash(w.BlockHash) || !validHash(w.TxHash) {
			return nil, sourceError(IncompleteError, e.id, nil)
		}
		l.BlockNumber = blockNumber
		l.Index = uint(logIndex)
		l.BlockHash = common.HexToHash(w.BlockHash)
		l.TxIndex = uint(tx)
		logs = append(logs, l)
	}
	return logs, nil
}

func (s *CommittedSource) RejoinEndpoint(ctx context.Context, sourceID string) error {
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return err
		}
		defer done()
	}
	ctx, cancel := context.WithTimeout(ctx, s.opts.BulkDeadline)
	defer cancel()
	s.mu.Lock()
	var endpoint *committedEndpoint
	for _, e := range s.endpoints {
		if e.id == sourceID {
			endpoint = e
			break
		}
	}
	if endpoint == nil || !endpoint.quarantined || endpoint.recovering {
		s.mu.Unlock()
		return sourceError(InvalidError, sourceID, nil)
	}
	generation := endpoint.generation
	acceptedVersion := s.acceptedVersion
	heights := make([]uint64, 0, len(s.accepted))
	for height := range s.accepted {
		heights = append(heights, height)
	}
	sort.Slice(heights, func(i, j int) bool { return heights[i] > heights[j] })
	if len(heights) < 128 {
		s.mu.Unlock()
		return sourceError(IncompleteError, sourceID, nil)
	}
	heights = heights[:128]
	expected := make(map[uint64]common.Hash, 128)
	for _, height := range heights {
		expected[height] = s.accepted[height]
	}
	endpoint.health.RecoveryChecks = 0
	endpoint.recovering = true
	s.mu.Unlock()
	defer func() { s.mu.Lock(); endpoint.recovering = false; s.mu.Unlock() }()
	if err := s.admitIdentity(ctx, endpoint, true); err != nil {
		if s.opts.Anchor == nil || !prunedSource(err) {
			return err
		}
		s.mu.Lock()
		others := make([]*committedEndpoint, 0, len(s.endpoints))
		for _, e := range s.endpoints {
			if e != endpoint && !e.quarantined {
				others = append(others, e)
			}
		}
		s.mu.Unlock()
		verifier := &CommittedSource{opts: s.opts, endpoints: others, client: s.client, cache: map[common.Hash]BlockRecord{}, accepted: map[uint64]common.Hash{}}
		if err := verifier.corroborate(ctx, endpoint); err != nil {
			return err
		}
	}
	for _, height := range heights {
		_, record, err := s.readBlock(ctx, endpoint, fmtHeight(height), true)
		if err != nil {
			return err
		}
		if record.Height != height || record.Hash != expected[height] {
			return sourceError(IntegrityError, sourceID, nil)
		}
		s.mu.Lock()
		if endpoint.generation != generation {
			s.mu.Unlock()
			return sourceError(IntegrityError, sourceID, nil)
		}
		endpoint.health.RecoveryChecks++
		s.mu.Unlock()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if endpoint.generation != generation || endpoint.health.RecoveryChecks != 128 || s.acceptedVersion != acceptedVersion {
		return sourceError(IntegrityError, sourceID, nil)
	}
	for height, hash := range expected {
		if current, ok := s.accepted[height]; !ok || current != hash {
			return sourceError(IntegrityError, sourceID, nil)
		}
	}
	endpoint.quarantined = false
	endpoint.health.QuarantineReason = ""
	endpoint.generation++
	return nil
}
func (s *CommittedSource) ApplySourceSelection(ctx context.Context, authorizationID, sourceID string, height uint64, hash common.Hash) error {
	if s.lifecycle != nil {
		var done func()
		var err error
		ctx, done, err = s.lifecycle.begin(ctx)
		if err != nil {
			return err
		}
		defer done()
	}
	if s.opts.SelectionAuthority == nil {
		return sourceError(AuthorizationError, sourceID, nil)
	}
	authorization, err := s.opts.SelectionAuthority.AuthorizeSourceSelection(ctx, authorizationID, sourceID, height, hash)
	if err != nil {
		return sourceError(AuthorizationError, sourceID, err)
	}
	if authorizationID == "" || len(authorizationID) > 160 || authorization.ID != authorizationID || authorization.SourceID != sourceID || authorization.Height != height || authorization.Hash != hash || authorization.Kind != "incident_recovery" || authorization.State != "approved" || authorization.Scope == "" || authorization.Reason == "" || authorization.RequestedBy == "" || authorization.ApprovedBy == "" || authorization.ApprovedBy == authorization.RequestedBy || authorization.Revision == 0 || len(authorization.PayloadHash) != 64 || !authorization.Expires.After(time.Now()) || authorization.Expires.After(time.Now().Add(24*time.Hour)) {
		return sourceError(AuthorizationError, sourceID, nil)
	}
	if _, err := hex.DecodeString(authorization.PayloadHash); err != nil {
		return sourceError(AuthorizationError, sourceID, nil)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	incident, ok := s.incidents[height]
	if !ok || incident.AcceptedHash != hash || s.accepted[height] != hash {
		return sourceError(IntegrityError, sourceID, nil)
	}
	for _, endpoint := range s.endpoints {
		if endpoint.id == sourceID {
			if endpoint.quarantined || (endpoint.health.FirstFailureAt.IsZero() == false && endpoint.health.RecoveryChecks != 128) {
				return sourceError(IntegrityError, sourceID, nil)
			}
			copy := authorization
			endpoint.selection = &copy
			return nil
		}
	}
	return sourceError(IdentityError, sourceID, nil)
}
