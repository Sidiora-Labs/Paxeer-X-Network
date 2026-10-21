package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/capabilities"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
)

// memoryChain is an in-memory deployment.Reader over one real block header.
// Every account carries the real BeaconProxy runtime bytes from the repository
// artifact, storage and call results are real 32-byte zero words, so
// deployment.Attest runs its full role walk and reports the identity issues
// (the manifest's runtimes are not those bytes). ChainID, the first read of
// every attestation, counts attestations and models public-RPC latency.
type memoryChain struct {
	header  *types.Header
	code    []byte
	delay   time.Duration
	attests atomic.Int64
}

func (m *memoryChain) ChainID(ctx context.Context) (*big.Int, error) {
	m.attests.Add(1)
	select {
	case <-time.After(m.delay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return big.NewInt(deployment.ChainID), nil
}

func (m *memoryChain) HeaderByHash(_ context.Context, h common.Hash) (*types.Header, error) {
	if h != m.header.Hash() {
		return nil, ethereum.NotFound
	}
	return types.CopyHeader(m.header), nil
}

func (m *memoryChain) CodeAtHash(_ context.Context, _ common.Address, h common.Hash) ([]byte, error) {
	if h != m.header.Hash() {
		return nil, ethereum.NotFound
	}
	return append([]byte(nil), m.code...), nil
}

func (m *memoryChain) StorageAtHash(_ context.Context, _ common.Address, _, h common.Hash) ([]byte, error) {
	if h != m.header.Hash() {
		return nil, ethereum.NotFound
	}
	return make([]byte, 32), nil
}

func (m *memoryChain) CallContractAtHash(_ context.Context, _ ethereum.CallMsg, h common.Hash) ([]byte, error) {
	if h != m.header.Hash() {
		return nil, ethereum.NotFound
	}
	return make([]byte, 32), nil
}

// memoryNode answers eth_getBlockByNumber over a real in-process JSON-RPC
// server, the call developmentAuthority.head makes against NETWORK_RPC_URL.
type memoryNode struct{ header *types.Header }

func (n memoryNode) GetBlockByNumber(number hexutil.Uint64, _ bool) (map[string]any, error) {
	if uint64(number) != n.header.Number.Uint64() {
		return nil, nil
	}
	return map[string]any{"number": hexutil.Uint64(number), "hash": n.header.Hash()}, nil
}

func TestDevelopmentAuthorityAttestationCache(t *testing.T) {
	manifest := deployment.Current()
	var artifact struct {
		DeployedBytecode string `json:"deployedBytecode"`
	}
	if err := json.Unmarshal(repoFile(t, "contracts/launchpad/artifacts/contracts/base/BeaconProxy.sol/BeaconProxy.json"), &artifact); err != nil {
		t.Fatal(err)
	}
	code, err := hexutil.Decode(artifact.DeployedBytecode)
	if err != nil || len(code) == 0 {
		t.Fatalf("BeaconProxy runtime: %v", err)
	}
	height := manifest.ActivationHeight + 7
	header := &types.Header{Number: new(big.Int).SetUint64(height), Time: uint64(time.Now().Unix()), Difficulty: big.NewInt(0), GasLimit: 30_000_000}
	chain := &memoryChain{header: header, code: code, delay: 400 * time.Millisecond}

	server := rpc.NewServer()
	t.Cleanup(server.Stop)
	if err := server.RegisterName("eth", memoryNode{header}); err != nil {
		t.Fatal(err)
	}
	client := rpc.DialInProc(server)
	t.Cleanup(client.Close)

	pool := internaltest.NewPostgres(t)
	indexerCursors(t, pool, height)
	engine, err := capabilities.NewPreflightEngine(chain, manifest, developmentPoolRuntime, developmentSerialInterface, time.Minute, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	const ttl = 2 * time.Second
	authority := &developmentAuthority{
		store: store.New(pool, nil), rpc: client, attester: chain, engine: engine, manifest: manifest,
		endpoints: deployment.PublicEndpoints{RPC: "https://rpc.example", Explorer: developmentExplorer, AllowedRPCs: []string{"https://rpc.example"}, AllowedExplorers: []string{developmentExplorer}},
		addresses: manifest.Addresses(), origins: map[string]bool{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), cacheTTL: ttl,
	}
	admission, err := rest.NewAdmission(64, 64, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	authority.Register(router, admission)
	httpServer := httptest.NewServer(router)
	t.Cleanup(httpServer.Close)
	srv := httpServer.URL

	type answer struct {
		status int
		retry  string
		body   []byte
		took   time.Duration
	}
	get := func() answer {
		started := time.Now()
		resp, err := http.Get(srv + "/v2/network/authority")
		if err != nil {
			t.Error(err)
			return answer{}
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return answer{resp.StatusCode, resp.Header.Get("Retry-After"), body, time.Since(started)}
	}
	unavailable := func(a answer) {
		t.Helper()
		var e struct {
			Code         string `json:"code"`
			Retryable    bool   `json:"retryable"`
			RetryAfterMS *int   `json:"retryAfterMs"`
		}
		if a.status != 503 || json.Unmarshal(a.body, &e) != nil || e.Code != "DEPENDENCY_UNAVAILABLE" {
			t.Fatalf("got %d %s, want 503 DEPENDENCY_UNAVAILABLE", a.status, a.body)
		}
		if err := interfaces.ValidateJSON("Error", a.body); err != nil {
			t.Fatalf("not a v2 Error: %v %s", err, a.body)
		}
		if e.Retryable {
			seconds, err := strconv.Atoi(a.retry)
			if err != nil || seconds < 1 || seconds > int(ttl/time.Second) || e.RetryAfterMS == nil || *e.RetryAfterMS != seconds*1000 {
				t.Fatalf("Retry-After %q retryAfterMs %v, want the remaining TTL (1..%d s)", a.retry, e.RetryAfterMS, int(ttl/time.Second))
			}
		}
	}

	// N concurrent cold requests share one attestation.
	const concurrent = 8
	answers := make([]answer, concurrent)
	var wg sync.WaitGroup
	for i := range answers {
		wg.Add(1)
		go func(i int) { defer wg.Done(); answers[i] = get() }(i)
	}
	wg.Wait()
	for _, a := range answers {
		unavailable(a)
	}
	if n := chain.attests.Load(); n != 1 {
		t.Fatalf("%d concurrent requests ran %d attestations, want 1", concurrent, n)
	}
	// The structured refusal is preserved: an ineligible attestation is
	// activation_blocked with its identity issues.
	authority.mu.Lock()
	reason := authority.lastReason
	authority.mu.Unlock()
	if !strings.Contains(reason, "activation_blocked") || !strings.Contains(reason, "unknown_runtime_hash") {
		t.Fatalf("reported reason %q, want activation_blocked with the identity issues", reason)
	}

	// Within the TTL: no attestation, the cached failure is served at once.
	a := get()
	unavailable(a)
	if n := chain.attests.Load(); n != 1 {
		t.Fatalf("request within TTL ran an attestation (%d total)", n)
	}
	if a.took >= chain.delay {
		t.Fatalf("cached answer took %s", a.took)
	}

	// After the TTL: the stale outcome is served within the short budget while
	// exactly one background attestation refreshes it.
	time.Sleep(ttl + 100*time.Millisecond)
	a = get()
	unavailable(a)
	if a.took >= chain.delay {
		t.Fatalf("stale answer waited for the refresh: %s", a.took)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		authority.cache.mu.Lock()
		idle := authority.cache.inflight == nil
		authority.cache.mu.Unlock()
		if idle && chain.attests.Load() == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("refresh after TTL: %d attestations, idle %v", chain.attests.Load(), idle)
		}
		time.Sleep(20 * time.Millisecond)
	}
	unavailable(get())
	if n := chain.attests.Load(); n != 2 {
		t.Fatalf("refreshed outcome not reused: %d attestations", n)
	}
}
