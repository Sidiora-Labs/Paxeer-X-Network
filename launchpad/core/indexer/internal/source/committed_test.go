package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const fh = "0x1111111111111111111111111111111111111111111111111111111111111111"

func fixture(t *testing.T, chain string) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     uint64
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&q)
		var result any
		switch q.Method {
		case "eth_chainId":
			result = chain
		case "eth_getLogs":
			result = []any{}
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(q.Params[0], &tag)
			if tag == "latest" {
				tag = "0x65"
			}
			result = map[string]any{"number": tag, "hash": fh, "parentHash": fh, "timestamp": "0x64", "transactions": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	t.Cleanup(s.Close)
	return s
}
func fopts(url string) CommittedOptions {
	return CommittedOptions{Endpoints: map[string]string{"fixture": url}, GenesisHash: common.HexToHash(fh), ManifestDigest: "fixture", Epochs: []AddressEpoch{{Address: "0x0000000000000000000000000000000000000001", Role: "emitter", ABIRevision: "1", ManifestDigest: "fixture"}}}
}

// Synthetic chain fixtures exercise real HTTP behavior, not receipt evidence.
func TestCommittedIdentityAndCoverage(t *testing.T) {
	bad := fixture(t, "0x1")
	if _, err := NewCommittedSource(context.Background(), fopts(bad.URL)); err == nil {
		t.Fatal("chain 1 eligible")
	}
	good := fixture(t, "0x7d")
	s, err := NewCommittedSource(context.Background(), fopts(good.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	h, err := s.ObserveHead(context.Background(), 100)
	if err != nil || h.Height != 101 {
		t.Fatalf("head %+v %v", h, err)
	}
	if _, err = s.ObserveHead(context.Background(), 102); err == nil {
		t.Fatal("regression accepted")
	}
	r, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 101})
	if err != nil || !r.Complete || len(r.Blocks) != 2 || len(r.CoveredRanges) != 1 {
		t.Fatalf("coverage %+v %v", r, err)
	}
	for _, o := range []FetchOptions{{FromBlock: -1}, {FromBlock: 2, ToBlock: 1}, {ToBlock: 128}} {
		if _, err = s.FetchLogs(context.Background(), o); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}
func TestCommittedTransport(t *testing.T) {
	for _, mode := range []string{"oversize", "id", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "oversize":
					fmt.Fprint(w, strings.Repeat("x", 1025))
				case "id":
					fmt.Fprint(w, `{"jsonrpc":"2.0","id":999,"result":"0x7d"}`)
				case "cancel":
					select {
					case <-r.Context().Done():
					case <-time.After(100 * time.Millisecond):
					}
				}
			}))
			defer srv.Close()
			s := &CommittedSource{opts: CommittedOptions{LiveDeadline: 30 * time.Millisecond, MaxResponseBytes: 1024}, client: srv.Client()}
			e := &committedEndpoint{id: "fixture", url: srv.URL, permits: make(chan struct{}, 16)}
			var out string
			err := s.rpc(context.Background(), e, false, "eth_chainId", nil, &out)
			var se *SourceError
			if !errors.As(err, &se) {
				t.Fatalf("untyped %v", err)
			}
			want := map[string]ErrorKind{"oversize": OversizeError, "id": InvalidError, "cancel": TimeoutError}[mode]
			if se.Kind != want {
				t.Fatalf("%s want %s", se.Kind, want)
			}
			if len(e.permits) != 0 || len(processPermits) != 0 {
				t.Fatal("permit leak")
			}
		})
	}
}
func TestCommittedBlockedPermitCancellation(t *testing.T) {
	e := &committedEndpoint{id: "blocked", permits: make(chan struct{}, 1)}
	e.permits <- struct{}{}
	s := &CommittedSource{opts: CommittedOptions{LiveDeadline: time.Second}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out string
	if err := s.rpc(ctx, e, false, "eth_chainId", nil, &out); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel %v", err)
	}
	if len(e.permits) != 1 || len(processPermits) != 0 {
		t.Fatal("leaked permit")
	}
}

func TestCommittedSubdivisionAndSameBlockDiscovery(t *testing.T) {
	var logCalls int
	address := "0x0000000000000000000000000000000000000002"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     uint64
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&q)
		result := any("0x7d")
		switch q.Method {
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(q.Params[0], &tag)
			result = map[string]any{"number": tag, "hash": fh, "parentHash": fh, "timestamp": "0x64", "transactions": []any{map[string]any{"hash": fh, "from": address}}}
		case "eth_getLogs":
			logCalls++
			var filter struct {
				From string `json:"fromBlock"`
				To   string `json:"toBlock"`
			}
			json.Unmarshal(q.Params[0], &filter)
			if filter.From != filter.To {
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "error": map[string]any{"code": -32005, "message": "range limited"}})
				return
			}
			result = []any{}
			if filter.From == "0x64" {
				result = []any{map[string]any{"address": address, "topics": []string{fh}, "data": "0x", "blockNumber": "0x64", "blockHash": fh, "transactionHash": fh, "transactionIndex": "0x0", "logIndex": "0x0"}}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	defer srv.Close()
	s, err := NewCommittedSource(context.Background(), fopts(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	discoveries := []AddressEpoch{{Address: address, Role: "token", ABIRevision: "1", ManifestDigest: "fixture", ActivationHeight: 100}}
	r, err := s.FetchCommitted(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 102}, false, discoveries)
	if err != nil || !r.Complete || len(r.Blocks) != 3 || len(r.Logs) != 1 || logCalls != 5 {
		t.Fatalf("coverage %+v err %v calls %d", r, err, logCalls)
	}
	discoveries[0].CreationTxIndex = 1
	if _, err = s.FetchCommitted(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 100}, false, discoveries); err == nil {
		t.Fatal("precreation log accepted")
	}
}

func TestCommittedSingleBlockReceiptFallback(t *testing.T) {
	address := "0x0000000000000000000000000000000000000001"
	headerCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     uint64
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&q)
		var result any
		switch q.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getLogs":
			fmt.Fprint(w, strings.Repeat("x", 4097))
			return
		case "eth_getBlockByNumber":
			headerCalls++
			var tag string
			json.Unmarshal(q.Params[0], &tag)
			result = map[string]any{"number": tag, "hash": fh, "parentHash": fh, "timestamp": "0x64", "transactions": []any{map[string]any{"hash": fh, "from": address}}}
		case "eth_getTransactionReceipt":
			result = map[string]any{"type": "0x0", "root": "0x", "status": "0x1", "cumulativeGasUsed": "0x1", "logsBloom": "0x" + strings.Repeat("0", 512), "logs": []any{}, "transactionHash": fh, "gasUsed": "0x1", "effectiveGasPrice": "0x1", "blockHash": fh, "blockNumber": "0x64", "transactionIndex": "0x0"}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	defer srv.Close()
	opts := fopts(srv.URL)
	opts.MaxResponseBytes = 4096
	s, err := NewCommittedSource(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 100})
	if err != nil || !r.Complete || len(r.Logs) != 0 || headerCalls != 2 {
		t.Fatalf("receipt fallback %+v %v", r, err)
	}
}

func TestCommittedDeadlineAndEpochIsolation(t *testing.T) {
	srv := fixture(t, "0x7d")
	for _, o := range []CommittedOptions{func() CommittedOptions { o := fopts(srv.URL); o.LiveDeadline = 3 * time.Second; return o }(), func() CommittedOptions { o := fopts(srv.URL); o.BulkDeadline = 16 * time.Second; return o }()} {
		if _, err := NewCommittedSource(context.Background(), o); err == nil {
			t.Fatal("oversized deadline accepted")
		}
	}
	o := fopts(srv.URL)
	retire := uint64(200)
	o.Epochs[0].RetirementHeight = &retire
	originalAddress := o.Epochs[0].Address
	s, err := NewCommittedSource(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	retire = 1
	o.Epochs[0].Address = "0x0000000000000000000000000000000000000002"
	o.Epochs[0].ManifestDigest = "tampered"
	if s.opts.Epochs[0].Address != originalAddress || *s.opts.Epochs[0].RetirementHeight != 200 || s.opts.Epochs[0].ManifestDigest != "fixture" {
		t.Fatal("caller changed approved epoch")
	}
	if _, err = s.FetchLogs(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 100}); err != nil {
		t.Fatal(err)
	}
	a := s.opts.Epochs[0]
	b := a
	b.ABIRevision = "conflicting"
	if err := validateEpochs([]AddressEpoch{a, b}, "fixture"); err == nil {
		t.Fatal("overlap accepted")
	}
	b.ActivationHeight = 200
	b.RetirementHeight = nil
	if err := validateEpochs([]AddressEpoch{a, b}, "fixture"); err != nil {
		t.Fatalf("exclusive adjacent epochs rejected: %v", err)
	}
	copied := cloneEpochs([]AddressEpoch{a})
	*a.RetirementHeight = 300
	if *copied[0].RetirementHeight != 200 {
		t.Fatal("discovery pointer remained aliased")
	}
}

func TestCommittedThrottleBackoff(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		var q struct{ ID uint64 }
		json.NewDecoder(r.Body).Decode(&q)
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": "0x7d"})
	}))
	defer srv.Close()
	s := &CommittedSource{opts: CommittedOptions{LiveDeadline: time.Second, MaxResponseBytes: 4096}, client: srv.Client()}
	e := &committedEndpoint{id: "fixture", url: srv.URL, permits: make(chan struct{}, 16)}
	start := time.Now()
	var result string
	if err := s.rpc(context.Background(), e, false, "eth_chainId", nil, &result); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || time.Since(start) < 300*time.Millisecond || result != "0x7d" || len(e.permits) != 0 || len(processPermits) != 0 {
		t.Fatal("bounded exponential throttle recovery failed")
	}
}

func TestCommittedRequiredTransactionShape(t *testing.T) {
	for _, shape := range []string{"missing", "null", "empty"} {
		t.Run(shape, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					ID     uint64
					Method string
					Params []json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&q)
				var result any = "0x7d"
				if q.Method == "eth_getBlockByNumber" {
					var tag string
					json.Unmarshal(q.Params[0], &tag)
					b := map[string]any{"number": tag, "hash": fh, "parentHash": fh, "timestamp": "0x64"}
					if shape == "empty" {
						b["transactions"] = []any{}
					} else if shape == "null" {
						b["transactions"] = nil
					}
					result = b
				} else if q.Method == "eth_getLogs" {
					result = []any{}
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
			}))
			defer srv.Close()
			s, err := NewCommittedSource(context.Background(), fopts(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			result, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 100})
			if shape == "empty" {
				if err != nil || !result.Complete {
					t.Fatalf("explicit empty array rejected %v", err)
				}
			} else {
				var se *SourceError
				if !errors.As(err, &se) || se.Kind != IncompleteError || result.Complete {
					t.Fatalf("missing transaction shape accepted %+v %v", result, err)
				}
			}
		})
	}
}

func TestCommittedHeadPolling(t *testing.T) {
	for i := 0; i < 10000; i++ {
		interval := committedPollInterval()
		if interval < 90*time.Millisecond || interval > 110*time.Millisecond {
			t.Fatalf("poll jitter outside bound: %v", interval)
		}
	}
	var active, peak atomic.Int32
	entered := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     uint64
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			return
		}
		var result any = "0x7d"
		if q.Method == "eth_getBlockByNumber" {
			var tag string
			json.Unmarshal(q.Params[0], &tag)
			if tag == "latest" {
				count := active.Add(1)
				for old := peak.Load(); count > old && !peak.CompareAndSwap(old, count); old = peak.Load() {
				}
				defer active.Add(-1)
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-time.After(130 * time.Millisecond):
				case <-r.Context().Done():
					return
				}
				tag = "0x65"
			}
			result = map[string]any{"number": tag, "hash": fh, "parentHash": fh, "timestamp": "0x64", "transactions": []any{}}
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
	}))
	defer srv.Close()
	s, err := NewCommittedSource(context.Background(), fopts(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stop := errors.New("consumer finished")
	pollDone := make(chan error, 1)
	polls := 0
	go func() {
		pollDone <- s.PollHeads(ctx, func() uint64 { return 100 }, func(h HeadObservation, err error) error {
			if err != nil {
				return err
			}
			if h.Height != 101 || h.SourceID != "fixture" || h.ObservedAt.Location() != time.UTC {
				return fmt.Errorf("invalid observation %+v", h)
			}
			polls++
			if polls == 3 {
				return stop
			}
			return nil
		})
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := s.PollHeads(ctx, func() uint64 { return 0 }, func(HeadObservation, error) error { return nil }); err == nil {
		t.Fatal("overlapping poll loop admitted")
	}
	if _, err := s.ObserveHead(ctx, 100); err != nil {
		t.Fatal(err)
	}
	if err := <-pollDone; !errors.Is(err, stop) {
		t.Fatalf("consumer error lost: %v", err)
	}
	if peak.Load() != 1 || polls != 3 || active.Load() != 0 {
		t.Fatalf("overlapping head polls: peak=%d polls=%d active=%d", peak.Load(), polls, active.Load())
	}
	cancelled, stopCancelled := context.WithCancel(context.Background())
	stopCancelled()
	if err := s.PollHeads(cancelled, func() uint64 { return 0 }, func(HeadObservation, error) error { t.Error("cancelled poll delivered"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestCommittedHeaderCacheMemoryBound(t *testing.T) {
	s := &CommittedSource{cache: map[common.Hash]BlockRecord{}, accepted: map[uint64]common.Hash{}}
	e := &committedEndpoint{id: "cache"}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := uint64(1); i <= 17000; i++ {
		hash := common.BigToHash(new(big.Int).SetUint64(i))
		r := BlockRecord{ChainID: 125, Height: i, Hash: hash, ParentHash: common.HexToHash(fh), TimestampSeconds: 100, SourceID: fmt.Sprintf("%064x", i)}
		if err := s.retainBlock(e, r); err != nil {
			t.Fatal(err)
		}
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("header cache allocation: %d bytes for 17000 inserts; retained headers=%d, identities=%d", allocated, len(s.cache), len(s.accepted))
	if allocated > 32<<20 || len(s.cache) != 16384 || len(s.accepted) != 16384 || len(s.cacheOrder) != 16384 {
		t.Fatalf("header cache exceeded bound: bytes=%d headers=%d identities=%d order=%d", allocated, len(s.cache), len(s.accepted), len(s.cacheOrder))
	}
	if _, ok := s.cache[common.BigToHash(big.NewInt(1))]; ok {
		t.Fatal("oldest immutable header was not evicted")
	}
	last := s.cache[common.BigToHash(big.NewInt(17000))]
	last.Hash = common.HexToHash(fh)
	var integrity *SourceError
	if err := s.retainBlock(e, last); !errors.As(err, &integrity) || integrity.Kind != IntegrityError || !e.quarantined {
		t.Fatalf("conflicting cached identity accepted: %v", err)
	}
}

func TestCommittedQuantitiesRejectOverflow(t *testing.T) {
	for _, field := range []string{"number", "timestamp", "blockNumber", "logIndex", "transactionIndex"} {
		t.Run(field, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					ID     uint64
					Method string
					Params []json.RawMessage
				}
				json.NewDecoder(r.Body).Decode(&q)
				var result any = "0x7d"
				if q.Method == "eth_getBlockByNumber" {
					var tag string
					json.Unmarshal(q.Params[0], &tag)
					block := map[string]any{"number": tag, "hash": fh, "parentHash": fh, "timestamp": "0x64", "transactions": []any{map[string]any{"hash": fh}}}
					if tag != "0x0" && (field == "number" || field == "timestamp") {
						block[field] = "0x10000000000000064"
					}
					result = block
				} else if q.Method == "eth_getLogs" {
					log := map[string]any{"address": "0x0000000000000000000000000000000000000001", "topics": []string{fh}, "data": "0x", "blockNumber": "0x64", "blockHash": fh, "transactionHash": fh, "transactionIndex": "0x0", "logIndex": "0x0"}
					if field != "number" && field != "timestamp" {
						log[field] = "0x10000000000000064"
					}
					result = []any{log}
				}
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
			}))
			defer srv.Close()
			s, err := NewCommittedSource(context.Background(), fopts(srv.URL))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			result, err := s.FetchLogs(context.Background(), FetchOptions{FromBlock: 100, ToBlock: 100})
			var incomplete *SourceError
			if !errors.As(err, &incomplete) || incomplete.Kind != IncompleteError || result.Complete {
				t.Fatalf("overflowed %s accepted as canonical: %+v %v", field, result, err)
			}
		})
	}
}

func TestSourceRangeAdmissionAndShutdown(t *testing.T) {
	lifecycle := newSourceLifecycle()
	contexts := make([]context.Context, 0, 1024)
	finishes := make([]func(), 0, 1024)
	for i := 0; i < 1024; i++ {
		ctx, done, err := lifecycle.begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		contexts = append(contexts, ctx)
		finishes = append(finishes, done)
	}
	if _, _, err := lifecycle.begin(context.Background()); err == nil {
		t.Fatal("unbounded range admission")
	}
	closed := make(chan struct{})
	go func() { lifecycle.close(); close(closed) }()
	for _, ctx := range contexts {
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
			t.Fatal("range cancellation missing")
		}
	}
	for _, done := range finishes {
		done()
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not drain")
	}
	if _, _, err := lifecycle.begin(context.Background()); err == nil {
		t.Fatal("range admitted after shutdown")
	}
}
func TestHistoricalLagHysteresis(t *testing.T) {
	s := &CommittedSource{}
	start := time.Now().Add(-3 * time.Minute)
	for _, observation := range []struct {
		at  time.Duration
		lag time.Duration
	}{{0, 3 * time.Second}, {29 * time.Second, 3 * time.Second}, {30 * time.Second, 3 * time.Second}} {
		if err := s.ReportLiveLag(start.Add(observation.at), observation.lag); err != nil {
			t.Fatal(err)
		}
	}
	if !s.TransportMetrics().HistoricalPaused {
		t.Fatal("historical work was not paused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.waitHistorical(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, at := range []time.Duration{31 * time.Second, 90 * time.Second} {
		if err := s.ReportLiveLag(start.Add(at), 100*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if !s.TransportMetrics().HistoricalPaused {
		t.Fatal("resumed before continuous sixty seconds")
	}
	if err := s.ReportLiveLag(start.Add(91*time.Second), 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if s.TransportMetrics().HistoricalPaused {
		t.Fatal("historical work did not resume")
	}
	if err := s.ReportLiveLag(start, time.Second); err == nil {
		t.Fatal("out of order lag accepted")
	}
}
func TestExpiredSelectionPreservesIntegrityFence(t *testing.T) {
	hash := common.HexToHash(fh)
	e := &committedEndpoint{id: "approved", selection: &SourceSelectionAuthorization{Height: 7, Hash: hash, Expires: time.Now().Add(-time.Second)}}
	s := &CommittedSource{endpoints: []*committedEndpoint{e}, incidents: map[uint64]IntegrityIncident{7: {Height: 7, AcceptedHash: hash, ConflictingHash: common.HexToHash("0x2"), ConflictingSourceID: "conflicting"}}}
	if err := s.fenceLocked(6); err != nil {
		t.Fatal(err)
	}
	if err := s.fenceLocked(7); err == nil {
		t.Fatal("expired authority removed integrity fence")
	}
	if len(s.eligible()) != 0 || !e.quarantined {
		t.Fatal("expired selected endpoint retained live eligibility")
	}
	if err := s.ApplySourceSelection(context.Background(), "forged", "approved", 7, hash); err == nil {
		t.Fatal("missing authority admitted selection")
	}
	if len(s.IntegrityIncidents()) != 1 {
		t.Fatal("incident was deleted on authorization expiration")
	}
}

func TestSourceCapacityNearestRankPopulation(t *testing.T) {
	for _, test := range []struct {
		count     uint64
		histogram map[int64]uint64
		want      int64
	}{{100, map[int64]uint64{1: 98, 500000: 1, 600000: 1}, 500000000}, {101, map[int64]uint64{1: 99, 500000: 1, 600000: 1}, 500000000}, {1, map[int64]uint64{17: 1}, 17000}} {
		got, err := capacityP99(test.histogram, test.count)
		if err != nil || got != test.want {
			t.Fatalf("nearest rank %d %v expected %d", got, err, test.want)
		}
	}
	for _, test := range []struct {
		count     uint64
		histogram map[int64]uint64
	}{{0, map[int64]uint64{}}, {2, map[int64]uint64{1: 1}}, {1, map[int64]uint64{-1: 1}}, {1, map[int64]uint64{1: 2}}} {
		if _, err := capacityP99(test.histogram, test.count); err == nil {
			t.Fatal("invalid latency population accepted")
		}
	}
}
