package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/profile"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/chain/readpolicy"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// rpcServer is a minimal JSON-RPC responder: it maps method -> result hex.
func rpcServer(t *testing.T, results map[string]string, failAll bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failAll {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		body, _ := io.ReadAll(r.Body)
		s := string(body)
		var method string
		for m := range results {
			if strings.Contains(s, `"`+m+`"`) {
				method = m
				break
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if method == "" {
			_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":"`+results[method]+`"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestPaxeerNetworkConfig(t *testing.T) {
	t.Parallel()
	if PaxeerNetwork.ChainID != 125 || PaxeerNetwork.NativeSymbol != "PAX" || PaxeerNetwork.NativeDecimals != 18 {
		t.Errorf("PaxeerNetwork = %+v", PaxeerNetwork)
	}
}

func TestNewClientRequiresURL(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(context.Background(), "", ""); err == nil {
		t.Fatal("empty rpc url should error")
	}
}

func TestChainIDAndBlockNumber(t *testing.T) {
	t.Parallel()
	srv := rpcServer(t, map[string]string{
		"eth_chainId":     "0x7d",  // 125
		"eth_blockNumber": "0x1a4", // 420
	}, false)

	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	id, err := c.ChainID(context.Background())
	if err != nil || id.Int64() != 125 {
		t.Fatalf("ChainID = %v, err %v", id, err)
	}
	bn, err := c.BlockNumber(context.Background())
	if err != nil || bn != 420 {
		t.Fatalf("BlockNumber = %d, err %v", bn, err)
	}
}

func TestFailoverToSecondary(t *testing.T) {
	t.Parallel()
	primary := rpcServer(t, nil, true) // always 500
	fallback := rpcServer(t, map[string]string{"eth_chainId": "0x7d"}, false)

	c, err := NewClient(context.Background(), primary.URL, fallback.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer c.Close()

	id, err := c.ChainID(context.Background())
	if err != nil || id.Int64() != 125 {
		t.Fatalf("failover ChainID = %v, err %v", id, err)
	}
}

func TestEthAccessor(t *testing.T) {
	t.Parallel()
	srv := rpcServer(t, map[string]string{"eth_chainId": "0x7d"}, false)
	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Eth() == nil {
		t.Fatal("Eth() returned nil")
	}
}

func TestWrongChainCannotServe(t *testing.T) {
	srv := rpcServer(t, map[string]string{"eth_chainId": "0x1", "eth_blockNumber": "0x9"}, false)
	c, e := NewClient(context.Background(), srv.URL, "")
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if _, e = c.BlockNumber(context.Background()); e == nil {
		t.Fatal("wrong chain served")
	}
	if _, e = c.ChainID(context.Background()); e == nil {
		t.Fatal("quarantine bypass")
	}
}
func pinnedServer(t *testing.T, header *types.Header, revert bool, calls *atomic.Int64) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getBlockByNumber":
			result = reportedJSON(header)
		case "eth_call":
			calls.Add(1)
			if revert {
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": 3, "message": "OpticalQuoteUnsupported", "data": "0x12345678"}})
				return
			}
			var height string
			json.Unmarshal(req.Params[1], &height)
			if height != hexutil.EncodeBig(header.Number) {
				t.Error("call not addressed to the pinned height", string(req.Params[1]))
			}
			result = "0x01"
		default:
			t.Error(req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}
func testHeader(n int64) *types.Header {
	return &types.Header{Number: big.NewInt(n), Difficulty: big.NewInt(0), GasLimit: 100000, Time: 123, Extra: []byte{1}}
}

// consensusHash is the block hash these JSON-RPC responders report for h. Like
// Paxeer's CometBFT-derived hash it differs from go-ethereum's recomputed
// h.Hash(), but it still changes whenever the header content changes.
func consensusHash(h *types.Header) common.Hash {
	return crypto.Keccak256Hash([]byte("cometbft"), h.Hash().Bytes())
}

// reportedJSON encodes h the way a CometBFT-backed node does: the "hash" field
// carries the consensus hash, not the recomputed header hash.
func reportedJSON(h *types.Header) map[string]any {
	if h == nil {
		return nil
	}
	encoded, err := json.Marshal(h)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		panic(err)
	}
	out["hash"] = consensusHash(h).Hex()
	return out
}
func TestAnchorPinnedSnapshotAndSemanticRevert(t *testing.T) {
	h := testHeader(100)
	var pc, fc atomic.Int64
	p := pinnedServer(t, h, true, &pc)
	f := pinnedServer(t, h, false, &fc)
	cfg := VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(h)}
	c, e := NewVerifiedClient(context.Background(), p.URL, f.URL, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	pin, e := c.Pin(context.Background(), 100)
	if e != nil {
		t.Fatal(e)
	}
	to := common.HexToAddress("0x1")
	if _, e = c.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &to}}); e == nil || fc.Load() != 0 {
		t.Fatal("semantic fallback", e, fc.Load())
	}
	c2, e := NewVerifiedClient(context.Background(), f.URL, "", cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer c2.Close()
	pin, e = c2.Pin(context.Background(), 100)
	if e != nil {
		t.Fatal(e)
	}
	out, e := c2.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &to}, {To: &to}})
	if e != nil || len(out) != 2 {
		t.Fatal(e)
	}
	bad := testHeader(100)
	bad.Extra = []byte{2}
	b := pinnedServer(t, bad, false, &fc)
	if c3, e := NewVerifiedClient(context.Background(), f.URL, b.URL, cfg); e == nil {
		c3.Close()
		t.Fatal("conflicting anchor accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := fc.Load()
	if _, e = c2.ReadPinned(ctx, pin, []ethereum.CallMsg{{To: &to}}); e == nil || fc.Load() != before {
		t.Fatal("canceled call dispatched")
	}
}

func TestVerifiedFailoverAndConflictQuarantine(t *testing.T) {
	h := testHeader(100)
	var calls atomic.Int64
	primary := pinnedServer(t, h, false, &calls)
	fallback := pinnedServer(t, h, false, &calls)
	cfg := VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(h)}
	c, err := NewVerifiedClient(context.Background(), primary.URL, fallback.URL, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	primary.Close()
	pin, err := c.Pin(context.Background(), 100)
	if err != nil || pin.Hash != consensusHash(h) || pin.Hash == h.Hash() {
		t.Fatal(pin, err)
	}
	forged := pin
	forged.Hash = common.HexToHash("0xff")
	to := common.HexToAddress("0x1")
	before := calls.Load()
	if _, err = c.ReadPinned(context.Background(), forged, []ethereum.CallMsg{{To: &to}}); err == nil {
		t.Fatal("fabricated pin accepted")
	}
	if calls.Load() != before || c.EndpointAttestations()["fallback"].State != "verified" {
		t.Fatal("forged pin reached RPC or quarantined endpoint")
	}
	if _, err = c.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &to}}); err != nil {
		t.Fatal("healthy endpoint affected", err)
	}

}

func TestRawAccessorRejectsWrongChain(t *testing.T) {
	srv := rpcServer(t, map[string]string{"eth_chainId": "0x1"}, false)
	c, err := NewClient(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err = c.ChainID(context.Background()); err == nil {
		t.Fatal("wrong chain accepted")
	}
	if c.Eth() != nil {
		t.Fatal("quarantined raw handle exposed")
	}
}
func TestRPCBoundedRangeSplit(t *testing.T) {
	h := testHeader(100)
	var covered []readpolicy.Range
	var splits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		json.NewDecoder(r.Body).Decode(&req)
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getBlockByNumber":
			var tag string
			if err := json.Unmarshal(req.Params[0], &tag); err != nil {
				t.Error(err)
				return
			}
			height, err := hexutil.DecodeUint64(tag)
			if err != nil {
				t.Error(err)
				return
			}
			result = reportedJSON(testHeader(int64(height)))
		case "eth_getLogs":
			var q struct {
				From string `json:"fromBlock"`
				To   string `json:"toBlock"`
			}
			json.Unmarshal(req.Params[0], &q)
			from, _ := strconv.ParseUint(strings.TrimPrefix(q.From, "0x"), 16, 64)
			to, _ := strconv.ParseUint(strings.TrimPrefix(q.To, "0x"), 16, 64)
			if to-from > 249 {
				splits++
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32005, "message": "range too large"}})
				return
			}
			covered = append(covered, readpolicy.Range{From: from, To: to})
			result = []any{}
		default:
			t.Error(req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer server.Close()
	c, err := NewVerifiedClient(context.Background(), server.URL, "", VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(h)})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	logs, err := c.LogsBounded(context.Background(), ethereum.FilterQuery{FromBlock: big.NewInt(10), ToBlock: big.NewInt(1009)}, 1000)
	if err != nil || len(logs) != 0 || splits != 3 || len(covered) != 4 {
		t.Fatal(err, len(logs), splits, covered)
	}
	next := uint64(10)
	for _, r := range covered {
		if r.From != next {
			t.Fatal("coverage gap", covered)
		}
		next = r.To + 1
	}
	if next != 1010 {
		t.Fatal(next)
	}
}

func TestRejectUnboundedTransportSchemes(t *testing.T) {
	for _, endpoint := range []string{"ws://127.0.0.1:1", "wss://example.test/rpc", "/tmp/rpc.ipc", "ftp://example.test/rpc"} {
		if c, err := NewClient(context.Background(), endpoint, ""); err == nil {
			c.Close()
			t.Fatal("unsupported transport accepted", endpoint)
		}
	}
}

type mutableRPC struct {
	header         atomic.Pointer[types.Header]
	calls          atomic.Int64
	latest         atomic.Uint64
	failAfterFirst atomic.Bool
	blockCall      bool
	started        chan struct{}
	canceled       chan struct{}
}

func newMutableRPC(t *testing.T, m *mutableRPC) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getBlockByNumber":
			var tag string
			json.Unmarshal(req.Params[0], &tag)
			if tag == "latest" {
				result = reportedJSON(testHeader(int64(m.latest.Load())))
			} else {
				result = reportedJSON(m.header.Load())
			}
		case "eth_call":
			n := m.calls.Add(1)
			if m.blockCall {
				close(m.started)
				<-r.Context().Done()
				close(m.canceled)
				return
			}
			if m.failAfterFirst.Load() && n >= 2 {
				http.Error(w, "transport unavailable", 503)
				return
			}
			var height string
			json.Unmarshal(req.Params[1], &height)
			if height == "0x64" {
				result = "0x64"
			} else if height == "0x65" {
				result = "0x65"
			} else {
				t.Error("unexpected pin", string(req.Params[1]))
				http.Error(w, "invalid hash", 400)
				return
			}
			m.latest.Store(101)
		default:
			t.Error(req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(srv.Close)
	return srv
}
func TestRealPostPinConflictQuarantines(t *testing.T) {
	m := &mutableRPC{}
	m.header.Store(testHeader(100))
	srv := newMutableRPC(t, m)
	c, err := NewVerifiedClient(context.Background(), srv.URL, "", VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(testHeader(100))})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pin, err := c.Pin(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	bad := testHeader(100)
	bad.Extra = []byte{9}
	m.header.Store(bad)
	to := common.HexToAddress("0x1")
	if _, err = c.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &to}}); err == nil {
		t.Fatal("real conflict accepted")
	}
	if c.EndpointAttestations()["primary"].State != "quarantined" {
		t.Fatal("conflict not quarantined")
	}
	if _, err = c.Pin(context.Background(), 100); err == nil {
		t.Fatal("automatic recovery")
	}
}
func TestInflightCancellationStopsHTTPAndFallback(t *testing.T) {
	p := &mutableRPC{blockCall: true, started: make(chan struct{}), canceled: make(chan struct{})}
	p.header.Store(testHeader(100))
	f := &mutableRPC{}
	f.header.Store(testHeader(100))
	ps := newMutableRPC(t, p)
	fs := newMutableRPC(t, f)
	c, err := NewVerifiedClient(context.Background(), ps.URL, fs.URL, VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(testHeader(100))})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pin, err := c.Pin(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	to := common.HexToAddress("0x1")
	go func() { _, err := c.ReadPinned(ctx, pin, []ethereum.CallMsg{{To: &to}}); done <- err }()
	select {
	case <-p.started:
	case <-time.After(time.Second):
		t.Fatal("call not started")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RPC cancellation slow")
	}
	select {
	case <-p.canceled:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("HTTP request context not canceled")
	}
	if f.calls.Load() != 0 || time.Since(start) > time.Second {
		t.Fatal("cancellation dispatched fallback or exceeded budget")
	}
}
func TestChangingHeadSnapshotFallbackRetainsHash(t *testing.T) {
	p := &mutableRPC{}
	p.header.Store(testHeader(100))
	p.latest.Store(100)
	p.failAfterFirst.Store(true)
	f := &mutableRPC{}
	f.header.Store(testHeader(100))
	f.latest.Store(101)
	ps := newMutableRPC(t, p)
	fs := newMutableRPC(t, f)
	c, err := NewVerifiedClient(context.Background(), ps.URL, fs.URL, VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(testHeader(100))})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	latest, err := c.fallback.HeaderByNumber(context.Background(), nil)
	if err != nil || latest.Number.Uint64() != 101 {
		t.Fatal("fixture latest header", latest, err)
	}
	to := common.HexToAddress("0x1")
	fresh, err := c.fallback.CallContract(context.Background(), ethereum.CallMsg{To: &to}, latest.Number)
	if err != nil || len(fresh) != 1 || fresh[0] != 101 {
		t.Fatal("fixture reserves did not change at101", fresh, err)
	}
	f.calls.Store(0)

	pin, err := c.Pin(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	calls := []ethereum.CallMsg{{To: &to}, {To: &to}}
	out, err := c.ReadPinned(context.Background(), pin, calls)
	if err != nil || len(out) != 2 {
		t.Fatal(err, out)
	}
	for _, v := range out {
		if len(v) != 1 || v[0] != 100 {
			t.Fatal("mixed-height reserves", out)
		}
	}
	if p.calls.Load() != 2 || f.calls.Load() != 2 || p.latest.Load() != 101 {
		t.Fatal("fixture did not advance head and retry full group", p.calls.Load(), f.calls.Load())
	}
	f.failAfterFirst.Store(true)
	out, err = c.ReadPinned(context.Background(), pin, calls)
	if err == nil || out != nil {
		t.Fatal("partial snapshot published", out, err)
	}
}

func TestPhase2ReadAdapterRealRegisteredPool(t *testing.T) {
	path := os.Getenv("KINDLE_L01_REAL_FIXTURE_JSON")
	if path == "" {
		t.Fatal("KINDLE_L01_REAL_FIXTURE_JSON required: actual chain125 deployed pool, approved manifest/anchor, and delayed mined transaction")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 128<<10 {
		t.Fatal("fixture exceeds bounded manifest size")
	}
	var fixture struct {
		Primary              string            `json:"primary"`
		Fallback             string            `json:"fallback"`
		Config               ReadAdapterConfig `json:"config"`
		Pool                 common.Address    `json:"pool"`
		Height               uint64            `json:"height"`
		ExpectedVirtualUSDL  string            `json:"expectedVirtualUsdl"`
		ExpectedRealUSDL     string            `json:"expectedRealUsdl"`
		ExpectedTokens       string            `json:"expectedTokens"`
		ExpectedNextRealUSDL string            `json:"expectedNextRealUsdl"`
		ExpectedNextTokens   string            `json:"expectedNextTokens"`
		ExpectedFeeBps       *uint16           `json:"expectedFeeBps"`
		SignedRawTransaction string            `json:"signedRawTransaction"`
		Isolated             bool              `json:"isolated"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Fallback == "" || fixture.Pool == (common.Address{}) || fixture.SignedRawTransaction == "" || fixture.ExpectedVirtualUSDL == "" || fixture.ExpectedRealUSDL == "" || fixture.ExpectedTokens == "" || fixture.ExpectedNextRealUSDL == "" || fixture.ExpectedNextTokens == "" || fixture.ExpectedFeeBps == nil {
		t.Fatal("incomplete real provider fixture")
	}
	controlURL, err := url.Parse(fixture.Primary)
	if err != nil || !fixture.Isolated || controlURL.Port() == "" || (controlURL.Hostname() != "127.0.0.1" && controlURL.Hostname() != "localhost" && controlURL.Hostname() != "::1") {
		t.Fatal("receipt fixture requires explicitly isolated loopback EVM provider")
	}
	var poolCalls atomic.Int64
	proxyFor := func(endpoint string) *httptest.Server {
		target, err := url.Parse(endpoint)
		if err != nil || target.Host == "" {
			t.Fatal("invalid approved fixture endpoint")
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				http.Error(w, "proxy read failed", 500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var request struct {
				Method string            `json:"method"`
				Params []json.RawMessage `json:"params"`
			}
			if json.Unmarshal(body, &request) == nil && request.Method == "eth_call" && len(request.Params) > 0 {
				var call struct {
					To common.Address `json:"to"`
				}
				if json.Unmarshal(request.Params[0], &call) == nil && call.To == fixture.Pool {
					poolCalls.Add(1)
				}
			}
			proxy.ServeHTTP(w, r)
		}))
		t.Cleanup(server.Close)
		return server
	}
	primary := proxyFor(fixture.Primary)
	fallbackURL := ""
	if fixture.Fallback != "" {
		fallbackURL = proxyFor(fixture.Fallback).URL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	adapter, err := NewReadAdapter(ctx, primary.URL, fallbackURL, fixture.Config)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	pin, err := adapter.Pin(ctx, fixture.Height)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 1000)
	start := make(chan struct{})
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			snapshot, err := adapter.Snapshot(ctx, pin, fixture.Pool)
			if err == nil && (snapshot.BlockHash != pin.Hash || snapshot.BlockNumber != strconv.FormatUint(fixture.Height, 10) || snapshot.VirtualUSDLReserve != fixture.ExpectedVirtualUSDL || snapshot.RealUSDLBalance != fixture.ExpectedRealUSDL || snapshot.TokenReserve != fixture.ExpectedTokens || snapshot.FeeBps != *fixture.ExpectedFeeBps || snapshot.ManifestHash != fixture.Config.ManifestHash || snapshot.InterfaceHash != fixture.Config.InterfaceHash) {
				err = errors.New("real snapshot expected-result/provenance mismatch")
			}
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if poolCalls.Load() != 4 {
		t.Fatalf("real upstream pool calls=%d, want exactly one four-read group", poolCalls.Load())
	}
	primary.Close()
	cacheExpiry := time.NewTimer(110 * time.Millisecond)
	select {
	case <-ctx.Done():
		cacheExpiry.Stop()
		t.Fatal(ctx.Err())
	case <-cacheExpiry.C:
	}
	failover, err := adapter.Snapshot(ctx, pin, fixture.Pool)
	if err != nil || failover.BlockHash != pin.Hash || failover.RealUSDLBalance != fixture.ExpectedRealUSDL || failover.TokenReserve != fixture.ExpectedTokens {
		t.Fatal("real endpoint-loss fallback changed selected snapshot", err)
	}
	if poolCalls.Load() != 8 {
		t.Fatal("endpoint-loss did not reissue one complete group on fallback", poolCalls.Load())
	}
	wallet := common.HexToAddress("0x1111111111111111111111111111111111111111")
	if _, err := adapter.client.ReadPublicPinned(ctx, pin, fixture.Config.ConfigurationRevision, []ethereum.CallMsg{{From: wallet, To: &fixture.Pool, Data: []byte{0, 0, 0, 1}}}); err == nil {
		t.Fatal("wallet read entered public group")
	}
	control, err := rpc.DialOptions(ctx, fixture.Primary, rpc.WithHTTPClient(&http.Client{Transport: readpolicy.ResponseTransport{}}))
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	raw, err := hexutil.Decode(fixture.SignedRawTransaction)
	if err != nil {
		t.Fatal("malformed signed fixture transaction")
	}
	var signed types.Transaction
	if err = signed.UnmarshalBinary(raw); err != nil || signed.ChainId().Cmp(big.NewInt(125)) != 0 {
		t.Fatal("fixture signed transaction must target isolated chain125")
	}
	var ignored any
	if err = control.CallContext(ctx, &ignored, "evm_setAutomine", false); err != nil {
		t.Fatal("isolated provider cannot disable mining", err)
	}
	defer func() {
		restore, cancel := context.WithTimeout(context.Background(), readpolicy.DefaultDeadline)
		defer cancel()
		var result any
		if err := control.CallContext(restore, &result, "evm_setAutomine", true); err != nil {
			t.Error("restore isolated mining", err)
		}
	}()
	var submitted common.Hash
	if err = control.CallContext(ctx, &submitted, "eth_sendRawTransaction", fixture.SignedRawTransaction); err != nil || submitted != signed.Hash() {
		t.Fatal("real fixture transaction submission failed", err)
	}
	state, err := readpolicy.Submitted(submitted, pin.DeploymentVersion)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	mineResult := make(chan error, 1)
	go func() {
		timer := time.NewTimer(10 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			mineResult <- ctx.Err()
		case <-timer.C:
			var mined any
			mineResult <- control.CallContext(ctx, &mined, "evm_mine")
		}
	}()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-timer.C:
	}
	state, view, err := adapter.client.ReceiptState(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != "awaiting_receipt" || view.Receipt != nil || view.Status != "submitted" {
		t.Fatal("receipt success before genuine delayed inclusion")
	}
	for {
		state, view, err = adapter.client.ReceiptState(ctx, state)
		if err != nil {
			t.Fatal(err)
		}
		if view.Receipt != nil {
			if time.Since(started) < 10*time.Second || state.State != "included_success" || view.Status != "included_success" || view.Receipt.BlockHash != state.BlockHash.Hex() {
				t.Fatal("delayed receipt identity/timing mismatch")
			}
			break
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			t.Fatal("genuine delayed receipt unavailable", ctx.Err())
		case <-timer.C:
		}
	}
	if err := <-mineResult; err != nil {
		t.Fatal("real delayed block mining failed", err)
	}
	includedHeight, err := strconv.ParseUint(state.BlockNumber, 10, 64)
	if err != nil || includedHeight != fixture.Height+1 {
		t.Fatal("fixture swap was not mined in next committed block", err)
	}
	newPin, err := adapter.Pin(ctx, includedHeight)
	if err != nil {
		t.Fatal(err)
	}
	current, err := adapter.Snapshot(ctx, newPin, fixture.Pool)
	if err != nil {
		t.Fatal(err)
	}
	if current.RealUSDLBalance != fixture.ExpectedNextRealUSDL || current.TokenReserve != fixture.ExpectedNextTokens || (current.RealUSDLBalance == fixture.ExpectedRealUSDL && current.TokenReserve == fixture.ExpectedTokens) {
		t.Fatal("genuine next-block swap did not change expected reserves")
	}
	old, err := adapter.Snapshot(ctx, pin, fixture.Pool)
	if err != nil || old.BlockHash != pin.Hash || old.RealUSDLBalance != fixture.ExpectedRealUSDL || old.TokenReserve != fixture.ExpectedTokens || old.VirtualUSDLReserve != fixture.ExpectedVirtualUSDL {
		t.Fatal("later committed reserve update leaked into old pinned snapshot", err)
	}
}

func phase3ProviderFixture(t *testing.T) (string, string, ReadAdapterConfig, common.Address, uint64) {
	t.Helper()
	path := os.Getenv("KINDLE_L01_REAL_FIXTURE_JSON")
	if path == "" {
		t.Fatal("KINDLE_L01_REAL_FIXTURE_JSON required for real adversarial provider qualification")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 128<<10 {
		t.Fatal("bounded genuine provider fixture unavailable", err)
	}
	var fixture struct {
		Primary  string            `json:"primary"`
		Fallback string            `json:"fallback"`
		Config   ReadAdapterConfig `json:"config"`
		Pool     common.Address    `json:"pool"`
		Height   uint64            `json:"height"`
		Isolated bool              `json:"isolated"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{fixture.Primary, fixture.Fallback} {
		u, err := url.Parse(endpoint)
		if err != nil || !fixture.Isolated || u.Port() == "" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
			t.Fatal("adversarial provider must be an explicitly isolated loopback EVM")
		}
	}
	if fixture.Pool == (common.Address{}) || fixture.Height == 0 {
		t.Fatal("real pool and committed history required")
	}
	return fixture.Primary, fixture.Fallback, fixture.Config, fixture.Pool, fixture.Height
}

type phase3RPCFault struct {
	method  string
	mutate  func(json.RawMessage, []json.RawMessage) (json.RawMessage, error)
	block   <-chan struct{}
	started chan struct{}
	once    sync.Once
}

func phase3FaultProxy(t *testing.T, endpoint string, enabled *atomic.Bool, fault *phase3RPCFault) *httptest.Server {
	t.Helper()
	target, err := url.Parse(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(response *http.Response) error {
		if !enabled.Load() {
			return nil
		}
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if response.Request.GetBody == nil {
			return errors.New("fault proxy lost actual RPC request")
		}
		body, err := response.Request.GetBody()
		if err != nil {
			return err
		}
		defer body.Close()
		if err := json.NewDecoder(body).Decode(&request); err != nil {
			return err
		}
		if request.Method != fault.method {
			return nil
		}
		if fault.started != nil {
			fault.once.Do(func() { close(fault.started) })
		}
		if fault.block != nil {
			select {
			case <-response.Request.Context().Done():
				return response.Request.Context().Err()
			case <-fault.block:
			}
		}
		if fault.mutate == nil {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, readpolicy.MaxResponseBytes+1))
		response.Body.Close()
		if err != nil || int64(len(data)) > readpolicy.MaxResponseBytes {
			return errors.New("fault proxy upstream payload invalid")
		}
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			return err
		}
		changed, err := fault.mutate(envelope["result"], request.Params)
		if err != nil {
			return err
		}
		envelope["result"] = changed
		data, err = json.Marshal(envelope)
		if err != nil {
			return err
		}
		response.Body = io.NopCloser(bytes.NewReader(data))
		response.ContentLength = int64(len(data))
		response.Header.Set("Content-Length", strconv.Itoa(len(data)))
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil || len(data) >= 1<<20 {
			http.Error(w, "bounded fault request unavailable", 400)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		r.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPhase3RealFirstPinConflictPublishesNothing(t *testing.T) {
	primary, fallback, cfg, _, height := phase3ProviderFixture(t)
	if height == cfg.AnchorHeight {
		height--
	}
	var enabled atomic.Bool
	fault := &phase3RPCFault{method: "eth_getBlockByNumber", mutate: func(raw json.RawMessage, params []json.RawMessage) (json.RawMessage, error) {
		var tag string
		if len(params) == 0 || json.Unmarshal(params[0], &tag) != nil {
			return nil, errors.New("missing actual block tag")
		}
		if tag != hexutil.EncodeUint64(height) {
			return raw, nil
		}
		var header types.Header
		if err := json.Unmarshal(raw, &header); err != nil {
			return nil, err
		}
		header.Extra = append(header.Extra, 0x99)
		return json.Marshal(&header)
	}}
	peer := phase3FaultProxy(t, fallback, &enabled, fault)
	client, err := NewVerifiedClient(context.Background(), primary, peer.URL, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	enabled.Store(true)
	pin, err := client.Pin(context.Background(), height)
	if err == nil || pin != (BlockPin{}) {
		t.Fatal("first conflicting committed response published", pin, err)
	}
	for endpoint, attestation := range client.EndpointAttestations() {
		if attestation.State != "quarantined" {
			t.Fatal("conflicting peer retained eligibility", endpoint, attestation)
		}
	}
	if _, err := client.Pin(context.Background(), height); err == nil {
		t.Fatal("conflict recovered without incident approval")
	}
}

func TestPhase3RealQueuedCancellationReleasesCapacity(t *testing.T) {
	primary, fallback, cfg, _, _ := phase3ProviderFixture(t)
	adapter, err := NewReadAdapter(context.Background(), primary, fallback, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	adapter.headPermit <- struct{}{}
	adapter.historicalPermit <- struct{}{}
	ctx, cancel := context.WithCancel(context.Background())
	results := make(chan error, 2)
	go func() { _, err := adapter.ObserveHead(ctx); results <- err }()
	for len(adapter.historicalPermit) < cap(adapter.historicalPermit) {
		adapter.historicalPermit <- struct{}{}
	}
	go func() {
		_, err := adapter.HistoricalLogs(ctx, ethereum.FilterQuery{FromBlock: big.NewInt(10), ToBlock: big.NewInt(15)}, 1)
		results <- err
	}()
	cancel()
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("queued cancellation lost", err)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("canceled queued read stranded")
		}
	}
	<-adapter.headPermit
	for len(adapter.historicalPermit) > 0 {
		<-adapter.historicalPermit
	}
	if _, err := adapter.ObserveHead(context.Background()); err != nil {
		t.Fatal("queued cancellation leaked head permit", err)
	}
	if _, err := adapter.HistoricalLogs(context.Background(), ethereum.FilterQuery{FromBlock: big.NewInt(10), ToBlock: big.NewInt(15)}, 1); err != nil {
		t.Fatal("queued cancellation leaked history permit", err)
	}
}

func TestPhase3RealMalformedAndOversizedLogsPublishNothing(t *testing.T) {
	primary, fallback, cfg, _, height := phase3ProviderFixture(t)
	for _, kind := range []string{"duplicate", "conflicting_hash", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			var enabled atomic.Bool
			fault := &phase3RPCFault{method: "eth_getLogs", mutate: func(raw json.RawMessage, _ []json.RawMessage) (json.RawMessage, error) {
				var logs []types.Log
				if err := json.Unmarshal(raw, &logs); err != nil {
					return nil, err
				}
				if len(logs) == 0 {
					return nil, errors.New("real fixture must contain pool logs at selected height")
				}
				switch kind {
				case "duplicate":
					logs = append(logs, logs[0])
				case "conflicting_hash":
					logs[0].BlockHash[0] ^= 1
				case "oversized":
					logs[0].Data = append(logs[0].Data, make([]byte, readpolicy.MaxResponseBytes)...)
				}
				return json.Marshal(logs)
			}}
			proxy := phase3FaultProxy(t, primary, &enabled, fault)
			client, err := NewVerifiedClient(context.Background(), proxy.URL, fallback, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			enabled.Store(true)
			logs, err := client.LogsBounded(context.Background(), ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(height), ToBlock: new(big.Int).SetUint64(height)}, 1)
			if err == nil || logs != nil {
				t.Fatal("malformed single block advanced completeness", kind, logs, err)
			}
			if kind == "conflicting_hash" && client.EndpointAttestations()["primary"].State != "quarantined" {
				t.Fatal("log/header corruption not quarantined")
			}
		})
	}
}

func TestPhase3RealLaggingFallbackPreservesObservedCursor(t *testing.T) {
	primary, fallback, cfg, _, height := phase3ProviderFixture(t)
	target, err := url.Parse(fallback)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var lagging atomic.Bool
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read failed", 500)
			return
		}
		var request struct {
			ID      json.RawMessage   `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
			JSONRPC string            `json:"jsonrpc"`
		}
		if json.Unmarshal(body, &request) == nil && lagging.Load() && request.Method == "eth_getBlockByNumber" && len(request.Params) > 0 && string(request.Params[0]) == `"latest"` {
			request.Params[0], err = json.Marshal(hexutil.EncodeUint64(height - 1))
			if err == nil {
				body, err = json.Marshal(request)
			}
			if err != nil {
				http.Error(w, "rewrite failed", 500)
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		proxy.ServeHTTP(w, r)
	}))
	defer peer.Close()
	var enabled atomic.Bool
	live := phase3FaultProxy(t, primary, &enabled, &phase3RPCFault{})
	adapter, err := NewReadAdapter(context.Background(), live.URL, peer.URL, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	previous, err := adapter.ObserveHead(context.Background())
	if err != nil || previous.Height < height {
		t.Fatal("real current head unavailable", previous, err)
	}
	live.Close()
	lagging.Store(true)
	observed, err := adapter.ObserveHead(context.Background())
	if err == nil || observed != (ObservedHead{}) {
		t.Fatal("lagging fallback advanced observation", observed, err)
	}
	retained, ok := adapter.LastHead()
	if !ok || retained != previous {
		t.Fatal("lagging fallback rewound observed cursor", retained, previous)
	}
}

func TestPhase3RealTimedOutHeightRetainsCompleteReplay(t *testing.T) {
	primary, fallback, cfg, _, _ := phase3ProviderFixture(t)
	target, err := url.Parse(primary)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var interrupted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "request failed", 500)
			return
		}
		var request struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &request) == nil && request.Method == "eth_getLogs" && len(request.Params) > 0 {
			var query struct {
				From string `json:"fromBlock"`
				To   string `json:"toBlock"`
			}
			if json.Unmarshal(request.Params[0], &query) == nil && query.From == "0xc" && query.To == "0xc" && interrupted.CompareAndSwap(false, true) {
				<-r.Context().Done()
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	client, err := NewVerifiedClient(context.Background(), server.URL, fallback, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	archive, err := os.CreateTemp(t.TempDir(), "committed-replay-")
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	encoder := json.NewEncoder(archive)
	for height := uint64(10); height <= 15; height++ {
		logs, err := client.LogsBounded(context.Background(), ethereum.FilterQuery{FromBlock: new(big.Int).SetUint64(height), ToBlock: new(big.Int).SetUint64(height)}, 1)
		if err != nil {
			t.Fatal("real replay must retain failed height", height, err)
		}
		pin, err := client.Pin(context.Background(), height)
		if err != nil {
			t.Fatal(err)
		}
		if err := encoder.Encode(struct {
			Height uint64
			Hash   common.Hash
			Logs   []types.Log
		}{height, pin.Hash, logs}); err != nil {
			t.Fatal(err)
		}
		if err := archive.Sync(); err != nil {
			t.Fatal(err)
		}
	}
	if !interrupted.Load() {
		t.Fatal("height12 timeout was not exercised")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(archive)
	for height := uint64(10); height <= 15; height++ {
		var row struct {
			Height uint64
			Hash   common.Hash
			Logs   []types.Log
		}
		if err := decoder.Decode(&row); err != nil || row.Height != height || row.Hash == (common.Hash{}) {
			t.Fatal("durable replay gap or duplicate", height, row, err)
		}
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatal("durable replay published extra height", err)
	}
}

func TestPhase3RealCoalescedCancellationReleasesWorker(t *testing.T) {
	primary, fallback, cfg, pool, height := phase3ProviderFixture(t)
	var enabled atomic.Bool
	release := make(chan struct{})
	fault := &phase3RPCFault{method: "eth_call", block: release, started: make(chan struct{})}
	proxy := phase3FaultProxy(t, primary, &enabled, fault)
	client, err := NewVerifiedClient(context.Background(), proxy.URL, fallback, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pin, err := client.Pin(context.Background(), height)
	if err != nil {
		t.Fatal(err)
	}
	enabled.Store(true)
	call := ethereum.CallMsg{To: &pool, Data: crypto.Keccak256([]byte("virtualUsdlReserve()"))[:4]}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.ReadPublicPinned(ctx, pin, cfg.ConfigurationRevision, []ethereum.CallMsg{call})
		result <- err
	}()
	select {
	case <-fault.started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("real coalesced read did not reach provider")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("caller cancellation lost", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("coalesced caller stranded")
	}
	close(release)
	enabled.Store(false)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		values, err := client.ReadPublicPinned(context.Background(), pin, cfg.ConfigurationRevision, []ethereum.CallMsg{call})
		if err == nil {
			if len(values) != 1 || len(values[0]) != 32 {
				t.Fatal("real recovery amount malformed")
			}
			if len(client.readPermit) != 0 {
				t.Fatal("canceled worker retained RPC permit")
			}
			return
		}
		if !errors.Is(err, readpolicy.ErrReadCapacity) {
			t.Fatal("canceled group did not release for recovery", err)
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("canceled worker did not release bounded group")
}

func TestPhase3RealBatchIDsFailClosed(t *testing.T) {
	primary, fallback, cfg, pool, height := phase3ProviderFixture(t)
	client, err := NewVerifiedClient(context.Background(), primary, fallback, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pin, err := client.Pin(context.Background(), height)
	if err != nil {
		t.Fatal(err)
	}
	values, err := client.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &pool, Data: crypto.Keccak256([]byte("virtualUsdlReserve()"))[:4]}, {To: &pool, Data: crypto.Keccak256([]byte("tokenReserve()"))[:4]}})
	if err != nil || len(values) != 2 {
		t.Fatal("real batch inputs unavailable", err)
	}
	matched, err := readpolicy.MatchBatch([]uint64{100, 101}, []readpolicy.BatchResponse{{ID: 101, Value: values[1]}, {ID: 100, Value: values[0]}})
	if err != nil || !bytes.Equal(matched[0], values[0]) || !bytes.Equal(matched[1], values[1]) {
		t.Fatal("shuffled genuine results mismatched", err)
	}
	for _, malformed := range [][]readpolicy.BatchResponse{
		{{ID: 100, Value: values[0]}, {ID: 100, Value: values[1]}},
		{{ID: 100, Value: values[0]}, {ID: 102, Value: values[1]}},
		{{ID: 100, Value: values[0]}},
	} {
		matched, err := readpolicy.MatchBatch([]uint64{100, 101}, malformed)
		if err == nil || matched != nil {
			t.Fatal("malformed genuine response batch partially published", malformed, err)
		}
	}
}

func TestPhase3RealOpticalRejectionNeverUsesNumericFallback(t *testing.T) {
	primary, fallback, cfg, pool, height := phase3ProviderFixture(t)
	data, err := os.ReadFile(os.Getenv("KINDLE_L01_REAL_FIXTURE_JSON"))
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		OpticalQuoteCalldata string `json:"opticalQuoteCalldata"`
	}
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	calldata, err := hexutil.Decode(input.OpticalQuoteCalldata)
	if err != nil || len(calldata) < 4 {
		t.Fatal("genuine deployed optical quote calldata required", err)
	}
	quoter := common.HexToAddress(cfg.Manifest.Roles["Quoter"].Address)
	target, err := url.Parse(fallback)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var fallbackCalls atomic.Int64
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "read failed", 500)
			return
		}
		var request struct {
			ID      json.RawMessage   `json:"id"`
			Method  string            `json:"method"`
			Params  []json.RawMessage `json:"params"`
			JSONRPC string            `json:"jsonrpc"`
		}
		if json.Unmarshal(body, &request) == nil && request.Method == "eth_call" && len(request.Params) == 2 {
			fallbackCalls.Add(1)
			request.Params[0], err = json.Marshal(map[string]any{"to": pool.Hex(), "input": hexutil.Encode(crypto.Keccak256([]byte("virtualUsdlReserve()"))[:4])})
			if err == nil {
				body, err = json.Marshal(request)
			}
			if err != nil {
				http.Error(w, "rewrite failed", 500)
				return
			}
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		proxy.ServeHTTP(w, r)
	}))
	defer peer.Close()
	client, err := NewVerifiedClient(context.Background(), primary, peer.URL, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pin, err := client.Pin(context.Background(), height)
	if err != nil {
		t.Fatal(err)
	}
	numeric, err := client.fallback.CallContract(context.Background(), ethereum.CallMsg{To: &quoter, Data: calldata}, new(big.Int).SetUint64(pin.Number))
	if err != nil || len(numeric) != 32 {
		t.Fatal("fault fallback must return a genuine pool reserve number", err)
	}
	fallbackCalls.Store(0)
	values, err := client.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &quoter, Data: calldata}})
	var rejection *readpolicy.ReadError
	selector := hexutil.Encode(crypto.Keccak256([]byte("OpticalQuoteUnsupported()"))[:4])
	if !errors.As(err, &rejection) || rejection.Code != "SEMANTIC_REVERT" || rejection.RevertSelector != selector || rejection.Retryable || values != nil {
		t.Fatal("real optical semantic rejection was lost", values, err)
	}
	if fallbackCalls.Load() != 0 {
		t.Fatal("semantic rejection tried the numeric fallback")
	}
}

func TestPhase3RealQuarantineInvalidatesCachedSnapshot(t *testing.T) {
	primary, fallback, cfg, pool, height := phase3ProviderFixture(t)
	var enabled atomic.Bool
	fault := &phase3RPCFault{method: "eth_getBlockByNumber", mutate: func(raw json.RawMessage, params []json.RawMessage) (json.RawMessage, error) {
		var tag string
		if len(params) == 0 || json.Unmarshal(params[0], &tag) != nil {
			return nil, errors.New("missing committed block tag")
		}
		if tag != hexutil.EncodeUint64(height) {
			return raw, nil
		}
		var header types.Header
		if err := json.Unmarshal(raw, &header); err != nil {
			return nil, err
		}
		header.Extra = append(header.Extra, 0x77)
		return json.Marshal(&header)
	}}
	peer := phase3FaultProxy(t, fallback, &enabled, fault)
	adapter, err := NewReadAdapter(context.Background(), primary, peer.URL, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	pin, err := adapter.Pin(context.Background(), height)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := adapter.Snapshot(context.Background(), pin, pool)
	if err != nil || cached.BlockHash != pin.Hash {
		t.Fatal("genuine cache input unavailable", err)
	}
	enabled.Store(true)
	if _, err := adapter.Pin(context.Background(), height); err == nil {
		t.Fatal("committed history conflict not detected")
	}
	if snapshot, err := adapter.Snapshot(context.Background(), pin, pool); err == nil || snapshot != (PoolSnapshot{}) {
		t.Fatal("quarantined endpoint published cached derived state", snapshot, err)
	}
}

func TestPhase3RealThousandBlockRangeSplitCoversChildren(t *testing.T) {
	primary, fallback, cfg, _, _ := phase3ProviderFixture(t)
	target, err := url.Parse(primary)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	var mu sync.Mutex
	var covered []readpolicy.Range
	var splits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			http.Error(w, "request failed", 500)
			return
		}
		var request struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(body, &request) == nil && request.Method == "eth_getLogs" && len(request.Params) > 0 {
			var query struct {
				From string `json:"fromBlock"`
				To   string `json:"toBlock"`
			}
			if err := json.Unmarshal(request.Params[0], &query); err != nil {
				http.Error(w, "invalid query", 400)
				return
			}
			from, fromErr := hexutil.DecodeUint64(query.From)
			to, toErr := hexutil.DecodeUint64(query.To)
			if fromErr != nil || toErr != nil || from > to {
				http.Error(w, "invalid range", 400)
				return
			}
			if to-from >= 250 {
				splits.Add(1)
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32005, "message": "adversarial range budget"}})
				return
			}
			mu.Lock()
			covered = append(covered, readpolicy.Range{From: from, To: to})
			mu.Unlock()
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		proxy.ServeHTTP(w, r)
	}))
	defer server.Close()
	client, err := NewVerifiedClient(context.Background(), server.URL, fallback, VerifiedConfig{profile.Paxeer(cfg.EndpointSetVersion, cfg.Manifest.DeploymentVersion), cfg.AnchorHeight, cfg.AnchorHash})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	logs, err := client.LogsBounded(context.Background(), ethereum.FilterQuery{FromBlock: big.NewInt(10), ToBlock: big.NewInt(1009)}, 1000)
	if err != nil {
		t.Fatal("genuine fixture must retain committed heights10..1009", err)
	}
	if splits.Load() != 3 {
		t.Fatal("range limit did not create exact bisection tree", splits.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(covered) != 4 {
		t.Fatal("split parent did not produce four complete genuine child reads", covered)
	}
	next := uint64(10)
	for _, child := range covered {
		if child.From != next {
			t.Fatal("split child coverage gap or overlap", covered)
		}
		next = child.To + 1
	}
	if next != 1010 {
		t.Fatal("split range was skipped", covered)
	}
	for _, log := range logs {
		if log.BlockNumber < 10 || log.BlockNumber > 1009 {
			t.Fatal("genuine split returned out-of-range event")
		}
	}
}

func TestPhase4ChainMeasurementRealMixedWorkload(t *testing.T) {
	path := os.Getenv("KINDLE_L01_PHASE4_INPUT_JSON")
	if path == "" {
		t.Fatal("KINDLE_L01_PHASE4_INPUT_JSON required: actual isolated adapter, durable indexer and overlapping frozen workload artifacts")
	}
	input, err := os.ReadFile(path)
	if err != nil || len(input) > 256<<10 {
		t.Fatal("bounded phase4 real provider inputs required")
	}
	var fixture struct {
		Primary          string                              `json:"primary"`
		Fallback         string                              `json:"fallback"`
		DatabaseURL      string                              `json:"databaseUrl"`
		ReadConfig       ReadAdapterConfig                   `json:"readConfig"`
		Measurement      ChainMeasurementConfig              `json:"measurement"`
		EvidenceManifest readpolicy.BoundMeasurementArtifact `json:"evidenceManifest"`
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fixture); err != nil || decoder.Decode(new(any)) != io.EOF {
		t.Fatal("malformed real measurement inputs")
	}
	if fixture.DatabaseURL == "" {
		t.Fatal("actual indexer durability database URL required")
	}
	primary, err := NewChainMeasurementEndpoint(fixture.Primary)
	if err != nil {
		t.Fatal(err)
	}
	defer primary.Close()
	fallback, err := NewChainMeasurementEndpoint(fixture.Fallback)
	if err != nil {
		t.Fatal(err)
	}
	defer fallback.Close()
	ctx, cancel := context.WithTimeout(context.Background(), fixture.Measurement.Duration+45*time.Second)
	defer cancel()
	adapter, err := NewReadAdapter(ctx, primary.URL, fallback.URL, fixture.ReadConfig)
	if err != nil {
		t.Fatal("real serving adapter unavailable", err)
	}
	defer adapter.Close()
	database, err := pgxpool.New(ctx, fixture.DatabaseURL)
	if err != nil {
		t.Fatal("actual durability database configuration unavailable")
	}
	defer database.Close()
	startupContext, startupCancel := context.WithTimeout(ctx, readpolicy.DefaultDeadline)
	defer startupCancel()
	if err := database.Ping(startupContext); err != nil {
		t.Fatal("actual durability database unavailable")
	}
	run, runErr := RunChainMeasurement(ctx, adapter, database, primary, fallback, fixture.Measurement)
	if run == nil {
		t.Fatal("measurement could not start", runErr)
	}
	if runErr != nil {
		result, finishErr := FinishChainMeasurement(run, ChainMeasurementEvidence{})
		t.Logf("phase4 measurement failed artifact=%s rawSha256=%s", result.RawPath, result.RawSHA256)
		t.Fatal("real adapter measurement failed", runErr, finishErr)
	}
	manifest, err := fixture.EvidenceManifest.Open()
	if err != nil {
		result, finishErr := FinishChainMeasurement(run, ChainMeasurementEvidence{})
		t.Logf("phase4 missing evidence artifact=%s rawSha256=%s", result.RawPath, result.RawSHA256)
		t.Fatal("source-bound actual mixed workload and compatibility manifest required", err, finishErr)
	}
	var evidence ChainMeasurementEvidence
	evidenceDecoder := json.NewDecoder(manifest)
	evidenceDecoder.DisallowUnknownFields()
	err = evidenceDecoder.Decode(&evidence)
	if err == nil && evidenceDecoder.Decode(new(any)) != io.EOF {
		err = errors.New("evidence manifest trailing data")
	}
	manifest.Close()
	if err != nil {
		_, _ = FinishChainMeasurement(run, ChainMeasurementEvidence{})
		t.Fatal("malformed genuine evidence binding")
	}
	result, err := FinishChainMeasurement(run, evidence)
	t.Logf("phase4 measured result raw=%s rawSha256=%s blocks=%d hotRequests=%d upstreamGroups=%d failoverRequests=%d rpcRequests=%d", result.RawPath, result.RawSHA256, run.IngestedBlocks, run.HotKeyRequests, run.HotKeyUpstreamGroups, run.EndpointLossRequests, run.UpstreamRPCRequests)
	if err != nil {
		t.Fatal("actual lane measurement evidence failed", err)
	}
	if !result.LaneHarnessPassed || result.FinalCapacityQualified || result.FinalCapacityTask != "9.1" || result.Distributions["ingest"].P99UpperNS > int64(500*time.Millisecond) || run.HotKeyRequests != 1000 || run.HotKeyUpstreamGroups != 1 || run.AfterLossIngestedBlocks == 0 {
		t.Fatal("measured lane result violated frozen gate or final shared evidence boundary")
	}
}

func TestPhase4MeasurementMissingInputsFailClosed(t *testing.T) {
	if _, err := RunChainMeasurement(context.Background(), nil, nil, nil, nil, ChainMeasurementConfig{}); err == nil {
		t.Fatal("missing real measurement providers admitted")
	}
	if _, err := FinishChainMeasurement(nil, ChainMeasurementEvidence{}); err == nil {
		t.Fatal("missing runtime measurement accepted")
	}
	for _, endpoint := range []string{"http://example.com:8545", "https://127.0.0.1:8545", "http://user:secret@127.0.0.1:8545", "http://127.0.0.1:8545/?token=secret"} {
		if proxy, err := NewChainMeasurementEndpoint(endpoint); err == nil {
			proxy.Close()
			t.Fatal("unapproved real measurement endpoint accepted")
		}
	}
}

func TestRecomputedAnchorIsConfigurationError(t *testing.T) {
	h := testHeader(100)
	var calls atomic.Int64
	srv := pinnedServer(t, h, false, &calls)
	c, err := NewVerifiedClient(context.Background(), srv.URL, "", VerifiedConfig{profile.Paxeer("e1", "d1"), 100, h.Hash()})
	if !errors.Is(err, ErrRecomputedAnchor) {
		if c != nil {
			c.Close()
		}
		t.Fatal("recomputed header hash accepted or misclassified as anchor", err)
	}
	c, err = NewVerifiedClient(context.Background(), srv.URL, "", VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(h)})
	if err != nil {
		t.Fatal("reported anchor rejected after a recomputed-anchor configuration error", err)
	}
	c.Close()
}

func TestPinnedReadRechecksReportedHashAfterCalls(t *testing.T) {
	m := &mutableRPC{}
	m.header.Store(testHeader(100))
	var swapped atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage
			Method string
			Params []json.RawMessage
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x7d"
		case "eth_getBlockByNumber":
			result = reportedJSON(m.header.Load())
		case "eth_call":
			if swapped.Load() {
				bad := testHeader(100)
				bad.Extra = []byte{7}
				m.header.Store(bad)
			}
			result = "0x64"
		default:
			t.Error(req.Method)
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	defer srv.Close()
	c, err := NewVerifiedClient(context.Background(), srv.URL, "", VerifiedConfig{profile.Paxeer("e1", "d1"), 100, consensusHash(testHeader(100))})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	pin, err := c.Pin(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	to := common.HexToAddress("0x1")
	if out, err := c.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &to}}); err != nil || len(out) != 1 || out[0][0] != 0x64 {
		t.Fatal(out, err)
	}
	swapped.Store(true)
	if out, err := c.ReadPinned(context.Background(), pin, []ethereum.CallMsg{{To: &to}}); err == nil || out != nil {
		t.Fatal("identity change during the pinned call was published", out)
	}
	if c.EndpointAttestations()["primary"].State != "quarantined" {
		t.Fatal("post-call identity conflict not quarantined")
	}
}
