package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/config"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/rest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/api/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	interfaces "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
)

const webOrigin = "https://web-production-d956e.up.railway.app"

func repoFile(t *testing.T, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The constants standing in for the bound document's reviewed inputs must stay
// equal to the repository sources they are derived from.
func TestDevelopmentReviewedInputsMatchRepository(t *testing.T) {
	var artifact struct {
		DeployedBytecode string `json:"deployedBytecode"`
	}
	if err := json.Unmarshal(repoFile(t, "contracts/artifacts/contracts/base/BeaconProxy.sol/BeaconProxy.json"), &artifact); err != nil {
		t.Fatal(err)
	}
	code, err := hexutil.Decode(artifact.DeployedBytecode)
	if err != nil || len(code) == 0 {
		t.Fatalf("BeaconProxy runtime: %v", err)
	}
	if got := crypto.Keccak256Hash(code).Hex(); got != developmentPoolRuntime {
		t.Fatalf("pool runtime = %s, want %s", got, developmentPoolRuntime)
	}
	if bytes.Contains(repoFile(t, "contracts/contracts/base/BeaconProxy.sol"), []byte("immutable")) {
		t.Fatal("BeaconProxy gained immutables; the runtime hash is no longer pool independent")
	}
	var registry map[string]any
	if err := json.Unmarshal(repoFile(t, "tools/production/bootstrap/check-registry.json"), &registry); err != nil {
		t.Fatal(err)
	}
	var find func(any) string
	find = func(v any) string {
		switch x := v.(type) {
		case map[string]any:
			if task, ok := x["1.99"].(map[string]any); ok {
				if bundle, ok := task["interface_bundle"].(map[string]any); ok {
					if s, ok := bundle["expected_sha256"].(string); ok {
						return s
					}
				}
			}
			for _, child := range x {
				if s := find(child); s != "" {
					return s
				}
			}
		case []any:
			for _, child := range x {
				if s := find(child); s != "" {
					return s
				}
			}
		}
		return ""
	}
	if got := find(registry); got != developmentSerialInterface {
		t.Fatalf("serial interface = %q, want %s", got, developmentSerialInterface)
	}
	// The web pins sourceManifestSha256 and every role address to CORE_DEPLOYMENT;
	// the export uses the manifest addresses as the frontend map.
	web := string(repoFile(t, "client/apps/web/src/core/network/deployment.ts"))
	field := func(name string) string {
		m := regexp.MustCompile(`"` + name + `": "([^"]+)"`).FindStringSubmatch(web)
		if m == nil {
			t.Fatalf("deployment.ts lacks %s", name)
		}
		return m[1]
	}
	manifest := deployment.Current()
	if field("manifestSha256") != manifest.SourceManifestSHA256 {
		t.Fatal("web CORE_DEPLOYMENT.manifestSha256 differs from current.json sourceManifestSha256")
	}
	for role, key := range map[string]string{"Router": "router", "Quoter": "quoter", "FeesRouter": "feesRouter", "OpticalRegistry": "opticalRegistry", "ProtocolConfig": "protocolConfig", "SidioraFactory": "factory", "SidioraNFT": "nft", "EventEmitter": "eventEmitter", "FeeAccumulator": "feeAccumulator", "PoolRegistry": "poolRegistry", "PoolBeacon": "poolBeacon", "USDL": "usdl"} {
		if common.HexToAddress(field(key)) != common.HexToAddress(manifest.Roles[role].Address) {
			t.Fatalf("web %s differs from manifest %s", key, role)
		}
	}
}

func TestDevelopmentPreflightOrigin(t *testing.T) {
	origins := map[string]bool{webOrigin: true}
	proxies := []string{"10.0.0.0/8", "fd12::/16"}
	request := func(remote, host, origin string, headers map[string]string) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "http://"+host+"/v2/preflight/trade", nil)
		r.RemoteAddr = remote
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r
	}
	web := strings.TrimPrefix(webOrigin, "https://")
	cases := []struct {
		name string
		r    *http.Request
		want bool
	}{
		{"production rule: origin equals https host", request("203.0.113.9:4000", web, webOrigin, nil), true},
		{"trusted proxy rewrite", request("10.1.2.3:5000", "api.railway.internal:8080", webOrigin, map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": web}), true},
		{"trusted ipv6 proxy, appended values", request("[fd12:aa::7]:5000", "api.railway.internal:8080", webOrigin, map[string]string{"X-Forwarded-Proto": "https,http", "X-Forwarded-Host": web + ", api.railway.internal:8080"}), true},
		{"untrusted peer cannot claim a forwarded host", request("203.0.113.9:4000", "api.railway.internal:8080", webOrigin, map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": web}), false},
		{"forwarded plain http", request("10.1.2.3:5000", "api.railway.internal:8080", webOrigin, map[string]string{"X-Forwarded-Proto": "http", "X-Forwarded-Host": web}), false},
		{"forwarded host differs from origin", request("10.1.2.3:5000", "api.railway.internal:8080", webOrigin, map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.example"}), false},
		{"origin outside allowlist", request("10.1.2.3:5000", "evil.example", "https://evil.example", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-Host": "evil.example"}), false},
		{"cross-origin api host without proxy", request("203.0.113.9:4000", "api-production-e4bd.up.railway.app", webOrigin, nil), false},
	}
	for _, c := range cases {
		if got := developmentPreflightOrigin(c.r, origins, proxies); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func developmentRouter(t *testing.T, deps developmentReadDeps) *httptest.Server {
	t.Helper()
	admission, err := rest.NewAdmission(64, 64, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	deps.admission = admission
	r := chi.NewRouter()
	if err := registerDevelopmentReads(r, deps); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server
}

func call(t *testing.T, method, target string, body []byte, headers map[string]string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func assertAPIError(t *testing.T, status int, raw []byte, wantStatus int, wantCode string) {
	t.Helper()
	var e struct {
		Code string `json:"code"`
	}
	if status != wantStatus || json.Unmarshal(raw, &e) != nil || e.Code != wantCode {
		t.Fatalf("got %d %s, want %d %s", status, raw, wantStatus, wantCode)
	}
}

// Without NETWORK_RPC_URL the routes exist (no 404) and answer the v2 error.
func TestDevelopmentAuthorityWithoutRPC(t *testing.T) {
	server := developmentRouter(t, developmentReadDeps{cfg: &config.Config{CORSAllowedOrigins: webOrigin}})
	status, raw := call(t, http.MethodGet, server.URL+"/v2/network/authority", nil, nil)
	assertAPIError(t, status, raw, 503, "DEPENDENCY_UNAVAILABLE")
	status, raw = call(t, http.MethodGet, server.URL+"/v2/network/authority?x=1", nil, nil)
	assertAPIError(t, status, raw, 400, "QUERY_INVALID")
	status, raw = call(t, http.MethodPost, server.URL+"/v2/preflight/trade", []byte(`{}`), map[string]string{"Origin": "https://evil.example"})
	assertAPIError(t, status, raw, 403, "SCOPE_DENIED")
}

// indexerCursors creates indexer.cursors from the indexer's own migration.
func indexerCursors(t *testing.T, pool *pgxpool.Pool, height uint64) {
	t.Helper()
	ddl := regexp.MustCompile(`(?s)CREATE TABLE "indexer"\."cursors" \(.*?\);`).Find(repoFile(t, "core/indexer/migrations/00001_init.sql"))
	if ddl == nil {
		t.Fatal("indexer.cursors DDL not found")
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS indexer"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(ddl)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO indexer.cursors (chain_id,last_processed_block,updated_at) VALUES (125,$1,now())", int64(height)); err != nil {
		t.Fatal(err)
	}
}

func networkRPC(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("NETWORK_RPC_URL")
	if endpoint == "" {
		t.Skip("requires the live chain 125 RPC: set NETWORK_RPC_URL (e.g. https://api5.mainnet-beta.paxeer.network)")
	}
	return endpoint
}

// forkChain starts anvil forked from the live chain 125 RPC. Blocks it mines
// carry standard Ethereum header hashes over the real contract state.
func forkChain(t *testing.T, endpoint string) (string, *rpc.Client) {
	t.Helper()
	if _, err := exec.LookPath("anvil"); err != nil {
		t.Skip("requires anvil (foundry) on PATH")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	_ = listener.Close()
	command := exec.Command("anvil", "--fork-url", endpoint, "--hardfork", "shanghai", "--host", "127.0.0.1", "--port", port, "--silent")
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() { _ = command.Process.Kill(); <-done })
	url := "http://127.0.0.1:" + port
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for {
		client, err := rpc.DialContext(ctx, url)
		if err == nil {
			var id hexutil.Uint64
			if err = client.CallContext(ctx, &id, "eth_chainId"); err == nil && id == 125 {
				t.Cleanup(client.Close)
				return url, client
			}
			client.Close()
		}
		select {
		case <-ctx.Done():
			t.Fatal("anvil fork did not start")
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// End to end over a fork of the live chain: the document is assembled with the
// production builders over the final governance state (PoolBeacon owned by the
// timelock, no pending owner), so the attestation is eligible, the route serves
// a contract-valid document and preflight answers over it.
func TestDevelopmentAuthorityForkedChain(t *testing.T) {
	endpoint := networkRPC(t)
	httpURL, client := forkChain(t, endpoint)
	ctx := context.Background()
	if err := client.CallContext(ctx, nil, "evm_mine"); err != nil {
		t.Fatal(err)
	}
	var head struct {
		Number hexutil.Uint64 `json:"number"`
		Hash   common.Hash    `json:"hash"`
	}
	if err := client.CallContext(ctx, &head, "eth_getBlockByNumber", "latest", false); err != nil {
		t.Fatal(err)
	}
	pool := internaltest.NewPostgres(t)
	indexerCursors(t, pool, uint64(head.Number))
	// The authority reaches the fork through a relay that can fail the reads
	// naming one address, the way an upstream RPC outage answers.
	relay := &rpcOutageRelay{upstream: httpURL}
	relayServer := httptest.NewServer(relay)
	t.Cleanup(relayServer.Close)
	cfg := &config.Config{NetworkRPCURL: relayServer.URL, CORSAllowedOrigins: webOrigin, TrustedProxyCIDRs: "127.0.0.0/8"}
	deps := developmentReadDeps{cfg: cfg, store: store.New(pool, nil)}

	authority, err := newDevelopmentAuthority(ctx, deps)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := authority.head(ctx)
	if err != nil || committed.Number != strconv.FormatUint(uint64(head.Number), 10) || committed.Hash != head.Hash.Hex() {
		t.Fatalf("head = %+v, %v; want %d %s", committed, err, head.Number, head.Hash.Hex())
	}
	manifest := deployment.Current()
	attestation, err := deployment.Attest(ctx, authority.reader, manifest, head.Hash, deployment.Policy{Now: time.Now().UTC(), MaxAge: time.Minute, Timeout: 20 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !attestation.Eligible || common.HexToAddress(attestation.PendingOwner) != (common.Address{}) || common.HexToAddress(attestation.ActualOwner) != common.HexToAddress(manifest.Governance.ExpectedBeaconOwner) {
		t.Fatalf("governance state = owner %s pending %s eligible %v", attestation.ActualOwner, attestation.PendingOwner, attestation.Eligible)
	}
	for role, id := range attestation.Identities {
		if !id.Verified {
			t.Errorf("%s identity unverified on the fork: %+v", role, id.Issues)
		}
	}
	// The fork is a plain-http local node; the export only advertises https
	// endpoints, so the document names the public RPC the fork was taken from.
	authority.endpoints = deployment.PublicEndpoints{RPC: endpoint, Explorer: developmentExplorer, AllowedRPCs: []string{endpoint}, AllowedExplorers: []string{developmentExplorer}}
	document, err := authority.document(ctx)
	if err != nil {
		t.Fatalf("document: %v", err)
	}
	assertWebAuthorityFields(t, document, manifest)

	admission, err := rest.NewAdmission(64, 64, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	authority.Register(router, admission)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	status, raw := call(t, http.MethodGet, server.URL+"/v2/network/authority", nil, nil)
	if status != 200 || interfaces.ValidateJSON("ConnectedNetworkAuthorityResponse", raw) != nil {
		t.Fatalf("authority route = %d %s", status, raw)
	}
	assertWebAuthorityFields(t, raw, manifest)

	web := strings.TrimPrefix(webOrigin, "https://")
	proxied := map[string]string{"Origin": webOrigin, "X-Forwarded-Proto": "https", "X-Forwarded-Host": web}
	trader := "0x00000000000000000000000000000000000000a1"
	zero := "0x0000000000000000000000000000000000000000"
	// The web's canonical request (runtime-authority.ts preflight()).
	canonical := func(blockHash string) []byte {
		return []byte(`{"family":"trading","expectedRevision":"` + authority.engine.Revision().RevisionSHA256 + `","blockHash":"` + blockHash + `","trader":"` + trader + `","recipient":"` + trader + `","pool":"` + zero + `","amountIn":"","isBuy":false,"creationPath":"","name":"","symbol":"","feeStrategy":0,"optical":"` + zero + `","nftId":""}`)
	}
	body := canonical(strings.ToLower(head.Hash.Hex()))
	status, raw = call(t, http.MethodPost, server.URL+"/v2/preflight/trade", body, map[string]string{"Origin": webOrigin})
	assertAPIError(t, status, raw, 403, "SCOPE_DENIED")
	status, raw = call(t, http.MethodPost, server.URL+"/v2/preflight/trade", canonical("0x"+strings.Repeat("ab", 32)), proxied)
	assertAPIError(t, status, raw, 409, "REVISION_CONFLICT")
	status, raw = call(t, http.MethodPost, server.URL+"/v2/preflight/quote", body, proxied)
	assertAPIError(t, status, raw, 422, "FIELD_INVALID")
	// With governance final the trading family is supported, so the engine
	// asks the verified PoolRegistry about the requested pool. The zero
	// address, an address without code and a contract that is not a pool are
	// all unknown: a deterministic, non-retryable 200 blocked preflight with
	// reasonCode pool_unknown, never the retryable dependency error.
	forPool := func(address string) []byte {
		return []byte(strings.Replace(string(body), `"pool":"`+zero+`"`, `"pool":"`+address+`"`, 1))
	}
	for _, unknown := range []string{zero, "0x00000000000000000000000000000000000dead1", manifest.Roles["Quoter"].Address} {
		status, raw = call(t, http.MethodPost, server.URL+"/v2/preflight/trade", forPool(unknown), proxied)
		var blocked struct {
			Supported          bool   `json:"supported"`
			ExecutionPermitted bool   `json:"executionPermitted"`
			ReasonCode         string `json:"reasonCode"`
			Pool               any    `json:"pool"`
		}
		if status != 200 || interfaces.ValidateJSON("ConnectedCapabilitiesPreflight", raw) != nil || json.Unmarshal(raw, &blocked) != nil || blocked.Supported || blocked.ExecutionPermitted || blocked.ReasonCode != "pool_unknown" || blocked.Pool != nil {
			t.Fatalf("unknown pool %s preflight = %d %s", unknown, status, raw)
		}
	}
	// A genuine RPC failure while reading the pool stays the retryable 503
	// (the engine reports preflight_unavailable), never pool_unknown.
	target := livePoolOrFallback(t, ctx, client, manifest)
	relay.fail(target)
	status, raw = call(t, http.MethodPost, server.URL+"/v2/preflight/trade", forPool(target), proxied)
	relay.fail("")
	assertAPIError(t, status, raw, 503, "DEPENDENCY_UNAVAILABLE")
	var envelope struct {
		Retryable bool `json:"retryable"`
	}
	if interfaces.ValidateJSON("Error", raw) != nil || json.Unmarshal(raw, &envelope) != nil || !envelope.Retryable {
		t.Fatalf("RPC outage envelope not retryable: %s", raw)
	}
	authority.mu.Lock()
	reason := authority.lastReason
	authority.mu.Unlock()
	if !strings.Contains(reason, "preflight_unavailable") {
		t.Fatalf("RPC outage preflight reason %q", reason)
	}
	assertProductionPreflightParity(t, pool, authority, head.Number, head.Hash, server.URL, proxied, body, forPool, relay, target)
}

// assertProductionPreflightParity mounts the production networkAuthority over
// the same engine and reader as the development authority and drives both
// /v2/preflight/* routes with the same requests at the same block. Each answer
// must be contract-valid and byte-identical between the two paths (error
// envelopes after normalising the per-request requestId).
func assertProductionPreflightParity(t *testing.T, db *pgxpool.Pool, dev *developmentAuthority, number hexutil.Uint64, hash common.Hash, devURL string, devHeaders map[string]string, body []byte, forPool func(string) []byte, relay *rpcOutageRelay, outagePool string) {
	t.Helper()
	ctx := context.Background()
	schema := "preflight_parity_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := db.Exec(ctx, "CREATE SCHEMA "+schema+"; CREATE TABLE "+schema+".indexer_committed_heads_v2 (chain_id integer NOT NULL, height numeric NOT NULL, block_hash text NOT NULL, integrity text NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })
	if _, err := db.Exec(ctx, "INSERT INTO "+schema+".indexer_committed_heads_v2 VALUES (125,$1,$2,'healthy')", uint64(number), hash.Hex()); err != nil {
		t.Fatal(err)
	}
	production := &networkAuthority{pool: db, config: networkAuthorityConfig{SchemaVersion: 2, Manifest: dev.manifest, SourceSchema: schema}, reader: dev.reader, rpc: dev.rpc, engine: dev.engine, origins: map[string]bool{webOrigin: true}}
	admission, err := rest.NewAdmission(64, 64, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	production.Register(router, admission)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	web := strings.TrimPrefix(webOrigin, "https://")
	productionCall := func(path string, payload []byte) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Host = web
		req.Header.Set("Origin", webOrigin)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, raw
	}
	requestID := regexp.MustCompile(`"requestId":"[^"]*"`)
	trader := `"trader":"0x00000000000000000000000000000000000000a1"`
	zeroTrader := []byte(strings.Replace(string(body), trader, `"trader":"0x0000000000000000000000000000000000000000"`, 1))
	claim := []byte(strings.NewReplacer(`"family":"trading"`, `"family":"nft_fees"`, `"nftId":""`, `"nftId":"999999999"`).Replace(string(body)))
	for _, c := range []struct {
		name, path string
		payload    []byte
		status     int
		reason     string
		outage     bool
	}{
		{"unknown pool", "/v2/preflight/trade", forPool("0x00000000000000000000000000000000000dead1"), 200, "pool_unknown", false},
		{"zero trader", "/v2/preflight/trade", zeroTrader, 200, "preflight_context_invalid", false},
		{"missing nft", "/v2/preflight/claim", claim, 200, "", false},
		{"rpc outage", "/v2/preflight/trade", forPool(outagePool), 503, "", true},
	} {
		if c.outage {
			relay.fail(outagePool)
		}
		devStatus, devBody := call(t, http.MethodPost, devURL+c.path, c.payload, devHeaders)
		prodStatus, prodBody := productionCall(c.path, c.payload)
		if c.outage {
			relay.fail("")
		}
		if devStatus != c.status || prodStatus != c.status {
			t.Fatalf("%s: dev %d %s, production %d %s, want %d", c.name, devStatus, devBody, prodStatus, prodBody, c.status)
		}
		schemaName := "ConnectedCapabilitiesPreflight"
		if c.status != 200 {
			schemaName = "Error"
		}
		for _, answer := range [][]byte{devBody, prodBody} {
			if err := interfaces.ValidateJSON(schemaName, bytes.TrimSpace(answer)); err != nil {
				t.Fatalf("%s: body violates %s: %v %s", c.name, schemaName, err, answer)
			}
		}
		if c.status != 200 {
			devBody, prodBody = requestID.ReplaceAll(devBody, []byte(`"requestId":"-"`)), requestID.ReplaceAll(prodBody, []byte(`"requestId":"-"`))
		}
		if !bytes.Equal(devBody, prodBody) {
			t.Fatalf("%s: dev and production bodies differ:\n dev  %s\n prod %s", c.name, devBody, prodBody)
		}
		var decoded struct {
			Supported  bool   `json:"supported"`
			ReasonCode string `json:"reasonCode"`
			Retryable  bool   `json:"retryable"`
			Code       string `json:"code"`
		}
		if err := json.Unmarshal(prodBody, &decoded); err != nil {
			t.Fatal(err)
		}
		switch {
		case c.status == 200 && (decoded.Supported || decoded.ReasonCode == "" || (c.reason != "" && decoded.ReasonCode != c.reason)):
			t.Fatalf("%s: blocked result = %s", c.name, prodBody)
		case c.status == 503 && (decoded.Code != "DEPENDENCY_UNAVAILABLE" || !decoded.Retryable):
			t.Fatalf("%s: outage envelope = %s", c.name, prodBody)
		}
		t.Logf("%s: %d %s", c.name, prodStatus, bytes.TrimSpace(prodBody))
	}
}

// livePoolOrFallback returns the first pool the live PoolRegistry lists on the
// fork, so the outage case targets a registered pool; with no pools it falls
// back to a fixed address (the registry read fails before membership matters).
func livePoolOrFallback(t *testing.T, ctx context.Context, client *rpc.Client, manifest deployment.Manifest) string {
	t.Helper()
	// getAllPools(offset 0, limit 1)
	data := append(append([]byte{}, crypto.Keccak256([]byte("getAllPools(uint256,uint256)"))[:4]...), make([]byte, 64)...)
	data[len(data)-1] = 1
	var out hexutil.Bytes
	registry := manifest.Roles["PoolRegistry"].Address
	if err := client.CallContext(ctx, &out, "eth_call", map[string]any{"to": registry, "data": hexutil.Bytes(data)}, "latest"); err == nil && len(out) >= 96 && out[63] == 1 {
		return common.BytesToAddress(out[64:96]).Hex()
	}
	return "0x00000000000000000000000000000000000dead2"
}

// rpcOutageRelay forwards JSON-RPC requests to the fork unchanged, except that
// while a target is set any request naming that address is answered with HTTP
// 503, as a failing upstream RPC would.
type rpcOutageRelay struct {
	upstream string
	mu       sync.Mutex
	target   string
}

func (r *rpcOutageRelay) fail(address string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.target = strings.ToLower(strings.TrimPrefix(address, "0x"))
}

func (r *rpcOutageRelay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	target := r.target
	r.mu.Unlock()
	if target != "" && bytes.Contains(bytes.ToLower(body), []byte(target)) {
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
		return
	}
	resp, err := http.Post(r.upstream, "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
