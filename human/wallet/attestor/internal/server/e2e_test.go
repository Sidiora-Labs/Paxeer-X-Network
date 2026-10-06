package server

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/audit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/auth/jwt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/lxwire"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy/evm"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy/lx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/transport"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/tss/dealer"
)

const (
	testChainID = 125
	testOwner   = "user-0001"
)

type testCA struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	pemPath string
}

type testCert struct {
	certPath string
	keyPath  string
	cert     *x509.Certificate
	pair     tls.Certificate
}

func writeTestPEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func testSerial(t *testing.T) *big.Int {
	t.Helper()
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func newTestCA(t *testing.T, dir string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          testSerial(t),
		Subject:               pkix.Name{CommonName: "attestor test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ca.pem")
	writeTestPEM(t, path, "CERTIFICATE", der)
	return &testCA{cert: cert, key: key, pemPath: path}
}

func (a *testCA) issue(t *testing.T, dir, name string, usages ...x509.ExtKeyUsage) testCert {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: testSerial(t),
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  usages,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	out := testCert{certPath: filepath.Join(dir, name+".pem"), keyPath: filepath.Join(dir, name+"-key.pem"), cert: cert}
	writeTestPEM(t, out.certPath, "CERTIFICATE", der)
	writeTestPEM(t, out.keyPath, "EC PRIVATE KEY", keyDER)
	if out.pair, err = tls.LoadX509KeyPair(out.certPath, out.keyPath); err != nil {
		t.Fatal(err)
	}
	return out
}

type identityProvider struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	issuer string
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func newIdentityProvider(t *testing.T) *identityProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &identityProvider{key: key}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		set := map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "test-key", "alg": "RS256", "use": "sig",
			"n": b64(key.PublicKey.N.Bytes()), "e": b64(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	}))
	t.Cleanup(p.srv.Close)
	p.issuer = p.srv.URL + "/auth/v1"
	return p
}

func (p *identityProvider) mint(t *testing.T, signer *rsa.PrivateKey, subject string) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "test-key", "typ": "JWT"})
	now := time.Now().Unix()
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		t.Fatal(err)
	}
	claims, _ := json.Marshal(map[string]any{"sub": subject, "iss": p.issuer, "aud": "authenticated", "iat": now, "exp": now + 600, "jti": hex.EncodeToString(jti)})
	signing := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, signer, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + b64(sig)
}

type testNode struct {
	id      string
	index   int
	apiAddr string
	server  *Server
	audit   *audit.Log
	store   *store.Store
	stop    func()
}

type testNodeConfig struct {
	id       string
	peerAddr string
	apiAddr  string
	cert     testCert
	nodeDir  string
	keyPath  string
}

type testCluster struct {
	ca       *testCA
	nodes    []*testNode
	client   *http.Client
	idp      *identityProvider
	ids      []string
	peers    []transport.Peer
	configs  []testNodeConfig
	registry *lxwire.Registry
	ceremony bool
	doc      *policy.Document
	tune     func(*Options)
	operator *testCA
	opClient *http.Client
	clients  *ClientAuthorities
	kernel   *lx.Document
	nonceURL string
	nonceRPC *http.Client
}

const kernelTestPolicy = `{"version":1,"defaults":{"modules":{"asset":[5]},"caps":{"native":{"per_operation":"6000000","daily":"8000000"}}}}`

func newBindNonceServer(t *testing.T, nonce uint64) *httptest.Server {
	t.Helper()
	parsed, err := evm.PrecompileABI("addr")
	if err != nil {
		t.Fatal(err)
	}
	method, ok := parsed.Methods["layerXBindNonce"]
	if !ok {
		t.Fatal("addr precompile ABI has no layerXBindNonce")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Method != "eth_call" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		out, err := method.Outputs.Pack(nonce)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": "0x" + hex.EncodeToString(out)})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mtlsClient(cert testCert, roots *x509.CertPool) *http.Client {
	return &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert.pair}, RootCAs: roots,
	}}}
}

func operatorPath(path string) bool {
	return path == PathImport || path == PathRefresh || path == PathAddShare || path == PathDescribe
}

func testPolicy() *policy.Document {
	chain := uint64(testChainID)
	rate := uint32(1000)
	return &policy.Document{
		Version: policy.Version,
		Defaults: policy.Rules{
			ChainID:       &chain,
			Kinds:         append([]string{policy.KindSponsoredBatch, policy.KindAuthorization}, SignKinds()...),
			Caps:          map[string]policy.Cap{policy.AssetNative: {PerTransaction: "1000000000000000000", Daily: "10000000000000000000"}},
			RatePerMinute: &rate,
		},
	}
}

func newTestCluster(t *testing.T, n int, ceremony bool) *testCluster {
	t.Helper()
	return newTestClusterWith(t, n, ceremony, testPolicy(), nil)
}

func newTestClusterWith(t *testing.T, n int, ceremony bool, doc *policy.Document, tune func(*Options)) *testCluster {
	t.Helper()
	dir := t.TempDir()
	gatewayDir, operatorDir := filepath.Join(dir, "gateway-ca"), filepath.Join(dir, "operator-ca")
	for _, d := range []string{gatewayDir, operatorDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	c := &testCluster{ca: newTestCA(t, gatewayDir), operator: newTestCA(t, operatorDir), idp: newIdentityProvider(t), ceremony: ceremony, doc: doc, tune: tune}
	roots := x509.NewCertPool()
	roots.AddCert(c.ca.cert)
	c.client = mtlsClient(c.ca.issue(t, dir, "gateway-client", x509.ExtKeyUsageClientAuth), roots)
	c.opClient = mtlsClient(c.operator.issue(t, dir, "operator-client", x509.ExtKeyUsageClientAuth), roots)
	clients, err := LoadClientAuthorities(c.ca.pemPath, c.operator.pemPath)
	if err != nil {
		t.Fatal(err)
	}
	kernelDoc, err := lx.Parse([]byte(kernelTestPolicy))
	if err != nil {
		t.Fatal(err)
	}
	nonces := newBindNonceServer(t, 1)
	c.clients, c.kernel, c.nonceURL, c.nonceRPC = clients, kernelDoc, nonces.URL, nonces.Client()

	peerListeners := make([]net.Listener, n)
	apiListeners := make([]net.Listener, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("node-%d", i+1)
		c.ids = append(c.ids, id)
		var err error
		if peerListeners[i], err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		if apiListeners[i], err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		cert := c.ca.issue(t, dir, id, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
		pin := transport.SPKIHash(cert.cert)
		c.peers = append(c.peers, transport.Peer{ID: id, Address: peerListeners[i].Addr().String(), SPKISHA256: hex.EncodeToString(pin[:])})
		keyPath := filepath.Join(dir, id+".key")
		seed := sha256.Sum256([]byte("store key " + id))
		if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(seed[:])), 0o600); err != nil {
			t.Fatal(err)
		}
		c.configs = append(c.configs, testNodeConfig{id: id, peerAddr: peerListeners[i].Addr().String(), apiAddr: apiListeners[i].Addr().String(), cert: cert, nodeDir: filepath.Join(dir, id), keyPath: keyPath})
	}
	registry, err := lxwire.NewRegistry(0x10005, 0x10007, 0x30002, 0x90005)
	if err != nil {
		t.Fatal(err)
	}
	c.registry = registry
	for i := 0; i < n; i++ {
		c.nodes = append(c.nodes, c.startNode(t, i, peerListeners[i], apiListeners[i]))
	}
	return c
}

func (c *testCluster) startNode(t *testing.T, i int, peerListener, apiListener net.Listener) *testNode {
	t.Helper()
	cfg := c.configs[i]
	nodeKey, err := config.ReadKeyFile(cfg.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(cfg.nodeDir, "shares"), nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	lg, err := audit.Open(filepath.Join(cfg.nodeDir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := transport.New(transport.Config{
		SelfID: cfg.id, CertFile: cfg.cert.certPath, KeyFile: cfg.cert.keyPath,
		CAFile: c.ca.pemPath, Peers: c.peers, OperatorCAFile: c.operator.pemPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = tr.Serve(peerListener) }()
	tokens, err := jwt.NewTokenVerifier(jwt.Config{JWKSURL: c.idp.srv.URL, Issuer: c.idp.issuer, Audience: "authenticated", HTTPClient: c.idp.srv.Client(), MaxAge: time.Hour, Replay: st})
	if err != nil {
		t.Fatal(err)
	}
	probe := map[string]string{}
	for j, p := range c.peers {
		if j != i {
			probe[p.ID] = p.Address
		}
	}
	engine := policy.New(c.doc)
	ledger := policy.NewMemoryLedger(time.Now)
	chain, err := lx.NewChain(c.nonceURL, c.nonceRPC)
	if err != nil {
		t.Fatal(err)
	}
	kernel, err := lx.New(engine, c.kernel, chain)
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		NodeID: cfg.id, Region: "test", ChainID: testChainID, Ceremony: c.ceremony, Participants: c.ids,
		Store: st, Audit: lg, Transport: tr, Policy: engine, Kernel: kernel, Clients: c.clients,
		Ledger: ledger, Tokens: tokens, Activities: c.registry,
		PeerProbe: TCPPeerProbe(probe), ProtocolTimeout: 8 * time.Minute,
	}
	if c.tune != nil {
		c.tune(&opts)
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	tlsCfg, err := APITLSConfig(cfg.cert.certPath, cfg.cert.keyPath, c.clients)
	if err != nil {
		t.Fatal(err)
	}
	api := srv.Serve(apiListener, tlsCfg)
	var once sync.Once
	node := &testNode{id: cfg.id, index: i, apiAddr: cfg.apiAddr, server: srv, audit: lg, store: st}
	node.stop = func() {
		once.Do(func() {
			_ = api.Close()
			_ = tr.Close()
			_ = st.Close()
			_ = lg.Close()
		})
	}
	t.Cleanup(node.stop)
	return node
}

func (c *testCluster) restart(t *testing.T, node *testNode) *testNode {
	t.Helper()
	node.stop()
	cfg := c.configs[node.index]
	var peerListener, apiListener net.Listener
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		if peerListener, err = net.Listen("tcp", cfg.peerAddr); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 50; attempt++ {
		if apiListener, err = net.Listen("tcp", cfg.apiAddr); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	fresh := c.startNode(t, node.index, peerListener, apiListener)
	c.nodes[node.index] = fresh
	return fresh
}

type apiResult struct {
	status int
	body   []byte
}

func (c *testCluster) post(apiAddr, path string, body any, token string) (apiResult, error) {
	client := c.client
	if operatorPath(path) {
		client = c.opClient
	}
	return c.postAs(client, apiAddr, path, body, token)
}

func (c *testCluster) postAs(client *http.Client, apiAddr, path string, body any, token string) (apiResult, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return apiResult{}, err
	}
	req, err := http.NewRequest(http.MethodPost, "https://"+apiAddr+path, bytes.NewReader(raw))
	if err != nil {
		return apiResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return apiResult{}, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return apiResult{status: resp.StatusCode, body: out}, nil
}

func (c *testCluster) call(t *testing.T, node *testNode, path string, body any, token string) apiResult {
	t.Helper()
	r, err := c.post(node.apiAddr, path, body, token)
	if err != nil {
		t.Errorf("%s %s: %v", node.id, path, err)
	}
	return r
}

func (c *testCluster) callAs(t *testing.T, client *http.Client, node *testNode, path string, body any, token string) apiResult {
	t.Helper()
	r, err := c.postAs(client, node.apiAddr, path, body, token)
	if err != nil {
		t.Errorf("%s %s: %v", node.id, path, err)
	}
	return r
}

func (c *testCluster) callAll(t *testing.T, nodes []*testNode, path string, body func(*testNode) any, token string) []apiResult {
	t.Helper()
	results := make([]apiResult, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node *testNode) {
			defer wg.Done()
			results[i] = c.call(t, node, path, body(node), token)
		}(i, node)
	}
	wg.Wait()
	return results
}

func (c *testCluster) byID(ids ...string) []*testNode {
	var out []*testNode
	for _, id := range ids {
		for _, n := range c.nodes {
			if n.id == id {
				out = append(out, n)
			}
		}
	}
	return out
}

func decodeOK[T any](t *testing.T, label string, results []apiResult) []T {
	t.Helper()
	out := make([]T, len(results))
	for i, r := range results {
		if r.status != http.StatusOK {
			t.Fatalf("%s: result %d status %d body %s", label, i, r.status, r.body)
		}
		if err := json.Unmarshal(r.body, &out[i]); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	return out
}

func expectError(t *testing.T, label string, r apiResult, code string) Error {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(r.body, &body); err != nil || body.Error == nil {
		t.Fatalf("%s: status %d body %s", label, r.status, r.body)
	}
	if body.Error.Code != code || body.Error.Category != errorCategories[code] {
		t.Fatalf("%s: got %s/%s (%s), want %s", label, body.Error.Category, body.Error.Code, body.Error.Message, code)
	}
	return *body.Error
}

func importKey(t *testing.T, c *testCluster, keyID string, curve dealer.Curve, secret *big.Int, account string) []KeyResponse {
	t.Helper()
	bundles, _, err := dealer.Split(curve, secret, c.ids)
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]ShareBundleJSON{}
	for _, b := range bundles {
		byNode[b.ParticipantID] = EncodeBundle(b)
	}
	results := c.callAll(t, c.nodes, PathImport, func(n *testNode) any {
		return ImportRequest{SessionID: "import-" + keyID, KeyID: keyID, Owner: testOwner, Account: account, Share: byNode[n.id]}
	}, "")
	return decodeOK[KeyResponse](t, "import "+keyID, results)
}

func kernelActivityFor(t *testing.T, signer ed25519.PrivateKey, sequence uint64) ([]byte, []byte) {
	t.Helper()
	var key [32]byte
	copy(key[:], signer.Public().(ed25519.PublicKey))
	did := lxwire.DIDFromKey(key)
	from, err := lxwire.AccountID([]byte(lxwire.MainAccountName(did)))
	if err != nil {
		t.Fatal(err)
	}
	var peer [32]byte
	for i := range peer {
		peer[i] = 0x52
	}
	to, err := lxwire.AccountID([]byte(lxwire.MainAccountName(lxwire.DIDFromKey(peer))))
	if err != nil {
		t.Fatal(err)
	}
	now := uint64(time.Now().Unix())
	idempotency := sha256.Sum256([]byte(fmt.Sprintf("attestor end-to-end activity %d", sequence)))
	context := sha256.Sum256([]byte(fmt.Sprintf("attestor end-to-end send context %d", sequence)))
	send := &lxwire.Send{
		From:              from,
		To:                to,
		Amount:            lxwire.Uint128{Lo: 5_000_000},
		SourceSequence:    sequence,
		IdempotencyKey:    idempotency,
		ExpiresAt:         now + 600,
		ContextHash:       context,
		AuthorizationKind: lxwire.OwnerAuthorization,
		Controller:        from,
		PublicKey:         key,
		SignedContextHash: context,
		NetworkID:         testChainID,
		ProtocolVersion:   lxwire.MaxProtocolVersion,
	}
	digest, err := send.AuthorizationDigest()
	if err != nil {
		t.Fatal(err)
	}
	copy(send.Signature[:], ed25519.Sign(signer, digest[:]))
	payload, err := send.Encode()
	if err != nil {
		t.Fatal(err)
	}
	a := &lxwire.Activity{
		ProtocolVersion: lxwire.MaxProtocolVersion,
		NetworkID:       testChainID,
		Type:            lx.OpAssetTransfer,
		ActorDID:        []byte(did),
		Authority:       key[:],
		AccountSequence: sequence,
		NotBefore:       now - 60,
		NotAfter:        now + 600,
		IdempotencyKey:  idempotency,
		FeeLimit:        lxwire.Uint128{Lo: 1000},
		PayloadHash:     lxwire.PayloadHash(payload),
		Payload:         payload,
	}
	unsigned, err := lxwire.EncodeUnsignedActivity(a)
	if err != nil {
		t.Fatal(err)
	}
	pre, err := lxwire.SignaturePreimage(a)
	if err != nil {
		t.Fatal(err)
	}
	return unsigned, pre[:]
}

// approvedActivity plays the original approval boundary: it reviews the canonical activity
// bytes and returns the structured disclosure it approved with its authorization binding.
func approvedActivity(t *testing.T, c *testCluster, unsigned []byte, keyID, session, principal string) (*lx.Disclosure, *lx.Approval) {
	t.Helper()
	a, err := lxwire.DecodeUnsignedActivity(unsigned, c.registry)
	if err != nil {
		t.Fatal(err)
	}
	effect, err := lx.DecodeEffect(a)
	if err != nil {
		t.Fatal(err)
	}
	module, ok := lx.ModuleName(a.Type.Module())
	if !ok {
		t.Fatalf("activity module %d is unknown", a.Type.Module())
	}
	pre, err := lxwire.SignaturePreimage(a)
	if err != nil {
		t.Fatal(err)
	}
	disclosure := &lx.Disclosure{
		Account: effect.Account, Module: module, Operation: a.Type.Ordinal(), Amounts: effect.Amounts,
		Destinations: effect.Destinations, Sequence: a.AccountSequence, NotBefore: a.NotBefore, NotAfter: a.NotAfter,
	}
	approval := &lx.Approval{
		Version: lx.ApprovalVersion, Principal: principal, KeyID: keyID, NetworkID: a.NetworkID,
		ProtocolVersion: a.ProtocolVersion, SessionID: session, ActivityDigest: lx.ID(pre), ExpiresAt: a.NotAfter - 300,
	}
	return disclosure, approval
}

func expectPolicy(t *testing.T, label string, results []apiResult, policyCode string) {
	t.Helper()
	if len(results) == 0 {
		t.Fatalf("%s: no results", label)
	}
	for _, r := range results {
		if e := expectError(t, label, r, CodePolicyDenied); e.PolicyCode != policyCode {
			t.Fatalf("%s: policy code %s (%s), want %s", label, e.PolicyCode, e.Message, policyCode)
		}
	}
}

func TestApprovedDisclosureBindsActivitySignatures(t *testing.T) {
	c := newTestCluster(t, 5, true)
	fresh := func() string { return c.idp.mint(t, c.idp.key, testOwner) }
	seed := sha256.Sum256([]byte("approved disclosure ed25519 key"))
	signer := ed25519.NewKeyFromSeed(seed[:])
	pub := signer.Public().(ed25519.PublicKey)
	scalar, err := dealer.Ed25519ScalarFromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	importKey(t, c, "approved-lx", dealer.Ed25519, scalar, common.HexToAddress("0x4444444444444444444444444444444444444444").Hex())
	signers := []string{"node-1", "node-2", "node-4"}
	sign := func(req SignRequest, tok string) []apiResult {
		req.Signers = signers
		return c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any { return req }, tok)
	}
	approvedBytes, preimage := kernelActivityFor(t, signer, 11)
	substitute, _ := kernelActivityFor(t, signer, 12)
	const session = "approved-activity"
	request := func(activity []byte, d *lx.Disclosure, a *lx.Approval) SignRequest {
		return SignRequest{SessionID: session, KeyID: "approved-lx", Kind: KindLXActivity, Activity: hex.EncodeToString(activity), Disclosure: d, Approval: a}
	}
	disclosure, approval := approvedActivity(t, c, approvedBytes, "approved-lx", session, testOwner)

	expectPolicy(t, "old request shape without disclosure", sign(request(approvedBytes, nil, nil), fresh()), lx.CodeDisclosureMissing)
	expectPolicy(t, "partial shape with disclosure only", sign(request(approvedBytes, disclosure, nil), fresh()), lx.CodeDisclosureMissing)
	expectPolicy(t, "partial shape with approval only", sign(request(approvedBytes, nil, approval), fresh()), lx.CodeDisclosureMissing)
	for _, r := range sign(SignRequest{SessionID: "bind-with-disclosure", KeyID: "approved-lx", Kind: KindLXBind, Message: hex.EncodeToString(lxwire.BindMessage(testChainID, common.HexToAddress("0x4444444444444444444444444444444444444444"), 1)), Disclosure: disclosure, Approval: approval}, fresh()) {
		expectError(t, "disclosure on a non-activity kind", r, CodeSessionBadRequest)
	}

	expectPolicy(t, "in-policy substituted activity under the approved binding", sign(request(substitute, disclosure, approval), fresh()), lx.CodeApprovalMismatch)
	_, substituteApproval := approvedActivity(t, c, substitute, "approved-lx", session, testOwner)
	expectPolicy(t, "in-policy substituted activity against the approved disclosure", sign(request(substitute, disclosure, substituteApproval), fresh()), lx.CodeDisclosureMismatch)
	altered := *disclosure
	altered.Amounts = []lx.Amount{{Asset: disclosure.Amounts[0].Asset, Amount: big.NewInt(4_000_000)}}
	expectPolicy(t, "approved disclosure with another amount", sign(request(approvedBytes, &altered, approval), fresh()), lx.CodeDisclosureMismatch)
	redirected := *disclosure
	redirected.Destinations = []lx.ID{disclosure.Account}
	expectPolicy(t, "approved disclosure with another destination", sign(request(approvedBytes, &redirected, approval), fresh()), lx.CodeDisclosureMismatch)

	moved := func(edit func(*lx.Approval)) *lx.Approval {
		a := *approval
		edit(&a)
		return &a
	}
	for label, a := range map[string]*lx.Approval{
		"approval for another principal":  moved(func(a *lx.Approval) { a.Principal = "user-9999" }),
		"approval for another key":        moved(func(a *lx.Approval) { a.KeyID = "lx-key" }),
		"approval for another session":    moved(func(a *lx.Approval) { a.SessionID = "approved-elsewhere" }),
		"approval for another network":    moved(func(a *lx.Approval) { a.NetworkID++ }),
		"approval for another protocol":   moved(func(a *lx.Approval) { a.ProtocolVersion-- }),
		"approval of another digest":      moved(func(a *lx.Approval) { a.ActivityDigest[0] ^= 1 }),
		"approval outliving the activity": moved(func(a *lx.Approval) { a.ExpiresAt = disclosure.NotAfter + 1 }),
		"approval of an unknown version":  moved(func(a *lx.Approval) { a.Version = lx.ApprovalVersion + 1 }),
	} {
		expectPolicy(t, label, sign(request(approvedBytes, disclosure, a), fresh()), lx.CodeApprovalMismatch)
	}
	expired := moved(func(a *lx.Approval) { a.ExpiresAt = uint64(time.Now().Unix()) - 1 })
	expectPolicy(t, "expired approval", sign(request(approvedBytes, disclosure, expired), fresh()), lx.CodeApprovalExpired)

	token := fresh()
	approved := request(approvedBytes, disclosure, approval)
	for _, r := range decodeOK[SignResponse](t, "approved activity", sign(approved, token)) {
		sig, _ := hex.DecodeString(r.Signature)
		if r.SignedBytes != hex.EncodeToString(preimage) || !ed25519.Verify(pub, preimage, sig) {
			t.Fatalf("%s: approved activity signature does not verify over the approved digest", r.NodeID)
		}
	}
	for _, r := range sign(approved, token) {
		expectError(t, "replayed approved request", r, CodeTokenInvalid)
	}

	restarted := c.restart(t, c.byID("node-2")[0])
	replayed := c.call(t, restarted, PathSign, func() SignRequest { r := approved; r.Signers = signers; return r }(), token)
	expectError(t, "replayed approved request after a restart", replayed, CodeTokenInvalid)
	_, afterApproval := approvedActivity(t, c, substitute, "approved-lx", session, testOwner)
	expectPolicy(t, "substitution after a restart", sign(request(substitute, disclosure, afterApproval), fresh()), lx.CodeDisclosureMismatch)

	recoverySeed := sha256.Sum256([]byte("approved disclosure recovery key"))
	recoverySigner := ed25519.NewKeyFromSeed(recoverySeed[:])
	recoveryScalar, err := dealer.Ed25519ScalarFromSeed(recoverySeed[:])
	if err != nil {
		t.Fatal(err)
	}
	importKey(t, c, "approved-recovery", dealer.Ed25519, recoveryScalar, common.HexToAddress("0x5555555555555555555555555555555555555555").Hex())
	recoveryBytes, recoveryPreimage := kernelActivityFor(t, recoverySigner, 1)
	recoveryDisclosure, recoveryApproval := approvedActivity(t, c, recoveryBytes, "approved-recovery", "approved-recovery-session", testOwner)
	recovered := SignRequest{SessionID: "approved-recovery-session", KeyID: "approved-recovery", Kind: KindLXActivity, Activity: hex.EncodeToString(recoveryBytes), Disclosure: recoveryDisclosure, Approval: recoveryApproval}
	for _, r := range decodeOK[SignResponse](t, "approved activity after a restart", sign(recovered, fresh())) {
		sig, _ := hex.DecodeString(r.Signature)
		if !ed25519.Verify(recoverySigner.Public().(ed25519.PublicKey), recoveryPreimage, sig) {
			t.Fatalf("%s: recovered signature does not verify", r.NodeID)
		}
	}
}

const mailTypedData = `{"types":{"EIP712Domain":[{"name":"name","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"verifyingContract","type":"address"}],"Person":[{"name":"name","type":"string"},{"name":"wallet","type":"address"}],"Mail":[{"name":"from","type":"Person"},{"name":"to","type":"Person"},{"name":"contents","type":"string"}]},"primaryType":"Mail","domain":{"name":"Ether Mail","version":"1","chainId":125,"verifyingContract":"0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"},"message":{"from":{"name":"Cow","wallet":"0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"},"to":{"name":"Bob","wallet":"0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB"},"contents":"Hello, Bob!"}}`

func TestFiveNodeEndToEnd(t *testing.T) {
	c := newTestCluster(t, 5, true)
	token := c.idp.mint(t, c.idp.key, testOwner)
	fresh := func() string { return c.idp.mint(t, c.idp.key, testOwner) }

	secpSeed := sha256.Sum256([]byte("attestor end-to-end secp256k1 key"))
	secpKey, err := gethcrypto.ToECDSA(secpSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	address := gethcrypto.PubkeyToAddress(secpKey.PublicKey)
	edSeed := sha256.Sum256([]byte("attestor end-to-end ed25519 key"))
	edPub := ed25519.NewKeyFromSeed(edSeed[:]).Public().(ed25519.PublicKey)
	edScalar, err := dealer.Ed25519ScalarFromSeed(edSeed[:])
	if err != nil {
		t.Fatal(err)
	}

	var imported [2][]KeyResponse
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		imported[0] = importKey(t, c, "evm-key", dealer.Secp256k1, new(big.Int).SetBytes(secpSeed[:]), "")
	}()
	go func() {
		defer wg.Done()
		imported[1] = importKey(t, c, "lx-key", dealer.Ed25519, edScalar, address.Hex())
	}()
	wg.Wait()
	for _, r := range imported[0] {
		if !common.IsHexAddress(r.Address) || common.HexToAddress(r.Address) != address || r.Refreshed {
			t.Fatalf("%s: imported address %s, want %s", r.NodeID, r.Address, address.Hex())
		}
	}
	for _, r := range imported[1] {
		if r.PublicKey != hex.EncodeToString(edPub) {
			t.Fatalf("%s: imported ed25519 key %s, want %x", r.NodeID, r.PublicKey, edPub)
		}
	}

	refreshed := make([][]KeyResponse, 2)
	wg.Add(2)
	for i, keyID := range []string{"evm-key", "lx-key"} {
		go func(i int, keyID string) {
			defer wg.Done()
			results := c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
				return RefreshRequest{SessionID: "refresh-" + keyID, KeyID: keyID}
			}, "")
			for j, r := range results {
				if r.status != http.StatusOK {
					t.Errorf("refresh %s on %s: %d %s", keyID, c.nodes[j].id, r.status, r.body)
					return
				}
				var kr KeyResponse
				if err := json.Unmarshal(r.body, &kr); err != nil {
					t.Error(err)
					return
				}
				refreshed[i] = append(refreshed[i], kr)
			}
		}(i, keyID)
	}
	wg.Wait()
	if t.Failed() {
		t.FailNow()
	}
	for i, set := range refreshed {
		for _, r := range set {
			if r.Epoch != 1 || !r.Refreshed || r.PublicKey != imported[i][0].PublicKey {
				t.Fatalf("%s %s: refresh response %+v", r.NodeID, r.KeyID, r)
			}
		}
	}

	signers := []string{"node-1", "node-3", "node-5"}
	signerNodes := c.byID(signers...)
	sign := func(label string, req SignRequest, tok string) []apiResult {
		req.Signers = signers
		req.SessionID = label
		return c.callAll(t, signerNodes, PathSign, func(*testNode) any { return req }, tok)
	}

	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(testChainID), Nonce: 7, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000),
		Gas: 21000, To: &to, Value: big.NewInt(1_000_000_000_000_000),
	})
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	txSigner := types.LatestSignerForChainID(big.NewInt(testChainID))
	txResults := decodeOK[SignResponse](t, "evm tx", sign("sign-evm-tx", SignRequest{KeyID: "evm-key", Kind: KindEVMTransaction, Transaction: hex.EncodeToString(rawTx)}, token))
	for _, r := range txResults {
		sig, err := hex.DecodeString(r.Signature)
		if err != nil || len(sig) != 65 || r.RecoveryID == nil || sig[64] != *r.RecoveryID {
			t.Fatalf("%s: signature %s recovery %v", r.NodeID, r.Signature, r.RecoveryID)
		}
		signed, err := tx.WithSignature(txSigner, sig)
		if err != nil {
			t.Fatal(err)
		}
		from, err := types.Sender(txSigner, signed)
		if err != nil || from != address {
			t.Fatalf("%s: recovered %s, want %s (%v)", r.NodeID, from.Hex(), address.Hex(), err)
		}
		if r.AuditSequence == 0 {
			t.Fatalf("%s: no audit sequence", r.NodeID)
		}
	}

	var mail apitypes.TypedData
	if err := json.Unmarshal([]byte(mailTypedData), &mail); err != nil {
		t.Fatal(err)
	}
	mailDigest, _, err := apitypes.TypedDataAndHash(mail)
	if err != nil {
		t.Fatal(err)
	}
	tdResults := decodeOK[SignResponse](t, "typed data", sign("sign-typed-data", SignRequest{KeyID: "evm-key", Kind: KindTypedData, TypedData: mailTypedData}, fresh()))
	for _, r := range tdResults {
		if r.SignedBytes != hex.EncodeToString(mailDigest) {
			t.Fatalf("%s: typed data digest %s", r.NodeID, r.SignedBytes)
		}
		digest, _ := hex.DecodeString(r.SignedBytes)
		sig, _ := hex.DecodeString(r.Signature)
		pub, err := gethcrypto.SigToPub(digest, sig)
		if err != nil || gethcrypto.PubkeyToAddress(*pub) != address {
			t.Fatalf("%s: typed data signature does not recover the key (%v)", r.NodeID, err)
		}
	}

	bind := lxwire.BindMessage(testChainID, address, 1)
	bindResults := decodeOK[SignResponse](t, "bind", sign("sign-bind", SignRequest{KeyID: "lx-key", Kind: KindLXBind, Message: hex.EncodeToString(bind)}, fresh()))
	for _, r := range bindResults {
		sig, _ := hex.DecodeString(r.Signature)
		if r.RecoveryID != nil || !ed25519.Verify(edPub, bind, sig) {
			t.Fatalf("%s: binding signature does not verify", r.NodeID)
		}
	}

	unsigned, preimage := kernelActivityFor(t, ed25519.NewKeyFromSeed(edSeed[:]), 1)
	actDisclosure, actApproval := approvedActivity(t, c, unsigned, "lx-key", "sign-activity", testOwner)
	actResults := decodeOK[SignResponse](t, "activity", sign("sign-activity", SignRequest{KeyID: "lx-key", Kind: KindLXActivity, Activity: hex.EncodeToString(unsigned), Disclosure: actDisclosure, Approval: actApproval}, fresh()))
	for _, r := range actResults {
		sig, _ := hex.DecodeString(r.Signature)
		if r.SignedBytes != hex.EncodeToString(preimage) || !ed25519.Verify(edPub, preimage, sig) {
			t.Fatalf("%s: activity signature does not verify over the fixture preimage", r.NodeID)
		}
	}

	delegate := common.HexToAddress("0x2222222222222222222222222222222222222222")
	authDigest, err := evm.AuthorizationDigest(big.NewInt(testChainID), delegate, 3)
	if err != nil {
		t.Fatal(err)
	}
	authConstruction := &ConstructionJSON{Kind: policy.KindAuthorization, ChainID: "125", Address: delegate.Hex(), Nonce: "3"}
	authResults := decodeOK[SignResponse](t, "authorization digest", sign("sign-authorization", SignRequest{KeyID: "evm-key", Kind: KindEthSignDigest, Digest: authDigest.Hex(), Construction: authConstruction}, fresh()))
	for _, r := range authResults {
		sig, _ := hex.DecodeString(r.Signature)
		pub, err := gethcrypto.SigToPub(authDigest.Bytes(), sig)
		if r.SignedBytes != hex.EncodeToString(authDigest.Bytes()) || err != nil || gethcrypto.PubkeyToAddress(*pub) != address {
			t.Fatalf("%s: authorization signature does not recover the key over the recomputed digest (%v)", r.NodeID, err)
		}
	}
	for _, r := range sign("sign-bare-digest", SignRequest{KeyID: "evm-key", Kind: KindEthSignDigest, Digest: authDigest.Hex()}, fresh()) {
		expectError(t, "bare digest", r, CodeSessionBadRequest)
	}
	txDigest := txSigner.Hash(tx)
	for _, r := range sign("sign-foreign-digest", SignRequest{KeyID: "evm-key", Kind: KindEthSignDigest, Digest: txDigest.Hex(), Construction: authConstruction}, fresh()) {
		e := expectError(t, "authorization over a foreign digest", r, CodePolicyDenied)
		if e.PolicyCode != policy.CodeDigestMismatch {
			t.Fatalf("authorization over a foreign digest: policy code %s", e.PolicyCode)
		}
	}
	batchConstruction := &ConstructionJSON{
		Kind: policy.KindSponsoredBatch, ChainID: "125", Account: address.Hex(), Nonce: "0",
		Calls: []BatchCallJSON{{To: to.Hex(), Value: "0", Data: "0x"}},
		Quote: &QuoteJSON{Sponsor: delegate.Hex(), Token: to.Hex(), MaxTokenAmount: "10", TokenAmount: "5", Deadline: "4102444800", QuoteNonce: "1", GasCost: "21000"},
	}
	for _, r := range sign("sign-batch-foreign-digest", SignRequest{KeyID: "evm-key", Kind: KindEthSignDigest, Digest: txDigest.Hex(), Construction: batchConstruction}, fresh()) {
		e := expectError(t, "sponsored batch over a foreign digest", r, CodePolicyDenied)
		if e.PolicyCode != policy.CodeDigestMismatch {
			t.Fatalf("sponsored batch over a foreign digest: policy code %s", e.PolicyCode)
		}
	}

	for _, r := range sign("sign-evm-tx", SignRequest{KeyID: "evm-key", Kind: KindEVMTransaction, Transaction: hex.EncodeToString(rawTx)}, token) {
		e := expectError(t, "replayed token", r, CodeTokenInvalid)
		if !strings.Contains(e.Message, "already authorised") {
			t.Fatalf("replayed token: %s", e.Message)
		}
	}
	peerSeed := sha256.Sum256([]byte("attestor end-to-end foreign identity"))
	foreign, _ := kernelActivityFor(t, ed25519.NewKeyFromSeed(peerSeed[:]), 3)
	foreignDisclosure, foreignApproval := approvedActivity(t, c, foreign, "lx-key", "sign-foreign-activity", testOwner)
	for _, r := range sign("sign-foreign-activity", SignRequest{KeyID: "lx-key", Kind: KindLXActivity, Activity: hex.EncodeToString(foreign), Disclosure: foreignDisclosure, Approval: foreignApproval}, fresh()) {
		e := expectError(t, "activity for another identity", r, CodePolicyDenied)
		if e.PolicyCode != lx.CodeAuthorityMismatch {
			t.Fatalf("activity for another identity: policy code %s", e.PolicyCode)
		}
	}
	for _, r := range sign("sign-stale-bind", SignRequest{KeyID: "lx-key", Kind: KindLXBind, Message: hex.EncodeToString(lxwire.BindMessage(testChainID, address, 2))}, fresh()) {
		e := expectError(t, "stale bind nonce", r, CodePolicyDenied)
		if e.PolicyCode != lx.CodeStaleBindNonce {
			t.Fatalf("stale bind nonce: policy code %s", e.PolicyCode)
		}
	}
	gatewayOnRefresh := c.callAs(t, c.client, c.nodes[0], PathRefresh, RefreshRequest{SessionID: "refresh-by-gateway", KeyID: "evm-key"}, "")
	if gatewayOnRefresh.status != http.StatusForbidden {
		t.Fatalf("gateway identity on refresh: status %d", gatewayOnRefresh.status)
	}
	expectError(t, "gateway identity on refresh", gatewayOnRefresh, CodeOperatorRequired)
	for _, path := range []string{PathImport, PathAddShare} {
		expectError(t, "gateway identity on "+path, c.callAs(t, c.client, c.nodes[0], path, map[string]string{}, ""), CodeOperatorRequired)
	}
	operatorOnSign := c.callAs(t, c.opClient, c.nodes[0], PathSign, SignRequest{SessionID: "sign-by-operator", KeyID: "evm-key", Kind: KindEVMTransaction, Signers: signers, Transaction: hex.EncodeToString(rawTx)}, fresh())
	if operatorOnSign.status != http.StatusForbidden {
		t.Fatalf("operator identity on sign: status %d", operatorOnSign.status)
	}
	expectError(t, "operator identity on sign", operatorOnSign, CodeOperatorRequired)
	expectError(t, "operator identity on generate", c.callAs(t, c.opClient, c.nodes[0], PathGenerate, GenerateRequest{SessionID: "generate-by-operator", KeyID: "op-key", Curve: "secp256k1", Owner: testOwner}, ""), CodeOperatorRequired)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sign("sign-bad-token", SignRequest{KeyID: "evm-key", Kind: KindEVMTransaction, Transaction: hex.EncodeToString(rawTx)}, c.idp.mint(t, otherKey, testOwner)) {
		expectError(t, "bad token", r, CodeTokenInvalid)
	}
	for _, r := range sign("sign-other-owner", SignRequest{KeyID: "evm-key", Kind: KindEVMTransaction, Transaction: hex.EncodeToString(rawTx)}, c.idp.mint(t, c.idp.key, "user-0002")) {
		expectError(t, "other owner", r, CodeTokenNotOwner)
	}
	bigTx := types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(testChainID), Nonce: 8, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000),
		Gas: 21000, To: &to, Value: new(big.Int).Mul(big.NewInt(5), big.NewInt(1_000_000_000_000_000_000)),
	})
	rawBig, err := bigTx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range sign("sign-over-cap", SignRequest{KeyID: "evm-key", Kind: KindEVMTransaction, Transaction: hex.EncodeToString(rawBig)}, fresh()) {
		e := expectError(t, "over cap", r, CodePolicyDenied)
		if e.PolicyCode != policy.CodeValueCap {
			t.Fatalf("over cap: policy code %s", e.PolicyCode)
		}
	}

	for _, n := range c.nodes {
		if err := n.audit.Verify(); err != nil {
			t.Fatalf("%s: audit chain: %v", n.id, err)
		}
		seq, _ := n.audit.Head()
		if seq < 4 {
			t.Fatalf("%s: audit head %d after imports and refreshes", n.id, seq)
		}
	}
	for _, id := range signers {
		n := c.byID(id)[0]
		seq, _ := n.audit.Head()
		if seq < 11 {
			t.Fatalf("%s: audit head %d does not hold every signing decision", id, seq)
		}
	}
}

func TestAddShareBindsOwnerAcrossParticipants(t *testing.T) {
	c := newTestCluster(t, 6, true)
	for _, n := range c.nodes {
		n.server.opts.ProtocolTimeout = 30 * time.Second
	}
	holders := c.ids[:5]
	edSeed := sha256.Sum256([]byte("attestor add-share binding ed25519 key"))
	edPub := ed25519.NewKeyFromSeed(edSeed[:]).Public().(ed25519.PublicKey)
	edScalar, err := dealer.Ed25519ScalarFromSeed(edSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	account := common.HexToAddress("0x3333333333333333333333333333333333333333").Hex()
	bundles, _, err := dealer.Split(dealer.Ed25519, edScalar, holders)
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]ShareBundleJSON{}
	for _, b := range bundles {
		byNode[b.ParticipantID] = EncodeBundle(b)
	}
	decodeOK[KeyResponse](t, "import", c.callAll(t, c.byID(holders...), PathImport, func(n *testNode) any {
		return ImportRequest{SessionID: "import-bound", KeyID: "bound-key", Owner: testOwner, Account: account, Share: byNode[n.id]}
	}, ""))

	quorum := []string{"node-1", "node-2", "node-3"}
	newcomer := "node-6"
	addShare := func(session, newcomerOwner string) []apiResult {
		return c.callAll(t, c.byID(append(append([]string{}, quorum...), newcomer)...), PathAddShare, func(n *testNode) any {
			owner := testOwner
			if n.id == newcomer {
				owner = newcomerOwner
			}
			return AddShareRequest{SessionID: session, KeyID: "bound-key", Curve: "ed25519", PublicKey: hex.EncodeToString(edPub), Owner: owner, Account: account, NewParticipantID: newcomer, Quorum: quorum}
		}, "")
	}

	for i, r := range addShare("addshare-foreign-owner", "user-9999") {
		if r.status == http.StatusOK {
			t.Fatalf("add-share with a foreign owner on the new participant succeeded on result %d: %s", i, r.body)
		}
	}
	if _, err := c.byID(newcomer)[0].store.Get("bound-key"); err == nil {
		t.Fatal("the new participant stored a share under a foreign owner")
	}
	for _, r := range c.callAll(t, c.byID("node-1"), PathAddShare, func(*testNode) any {
		return AddShareRequest{SessionID: "addshare-quorum-owner", KeyID: "bound-key", Curve: "ed25519", PublicKey: hex.EncodeToString(edPub), Owner: "user-9999", Account: account, NewParticipantID: newcomer, Quorum: quorum}
	}, "") {
		expectError(t, "quorum member told a foreign owner", r, CodeSessionBadRequest)
	}

	added := decodeOK[KeyResponse](t, "add-share", addShare("addshare-bound", testOwner))
	for _, r := range added {
		if r.PublicKey != hex.EncodeToString(edPub) {
			t.Fatalf("%s: add-share public key %s", r.NodeID, r.PublicKey)
		}
	}

	token := c.idp.mint(t, c.idp.key, testOwner)
	signers := []string{"node-1", "node-2", newcomer}
	bind := lxwire.BindMessage(testChainID, common.HexToAddress(account), 1)
	results := decodeOK[SignResponse](t, "sign with the new share", c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: "sign-bound", KeyID: "bound-key", Kind: KindLXBind, Signers: signers, Message: hex.EncodeToString(bind)}
	}, token))
	for _, r := range results {
		sig, _ := hex.DecodeString(r.Signature)
		if !ed25519.Verify(edPub, bind, sig) {
			t.Fatalf("%s: signature with the added share does not verify", r.NodeID)
		}
	}
}

func TestImportRefusesSharesOutsideTheCustodyScheme(t *testing.T) {
	c := newTestCluster(t, 5, true)
	curve, err := dealer.Secp256k1.Elliptic()
	if err != nil {
		t.Fatal(err)
	}
	order := curve.Params().N
	seed := sha256.Sum256([]byte("attestor import scheme secret"))
	secret := new(big.Int).Mod(new(big.Int).SetBytes(seed[:]), order)
	coefficient := func(label string) *big.Int {
		sum := sha256.Sum256([]byte("attestor import scheme coefficient " + label))
		return new(big.Int).Mod(new(big.Int).SetBytes(sum[:]), order)
	}
	evaluate := func(coefficients []*big.Int, x int64, rank uint32) *big.Int {
		out := new(big.Int)
		for i := len(coefficients) - 1; i >= int(rank); i-- {
			term := new(big.Int).Set(coefficients[i])
			for k := 0; k < int(rank); k++ {
				term.Mul(term, big.NewInt(int64(i-k)))
			}
			out.Mul(out, big.NewInt(x))
			out.Add(out, term)
			out.Mod(out, order)
		}
		return out
	}
	type point struct {
		x    int64
		rank uint32
	}
	bundleFor := func(coefficients []*big.Int, points []point, threshold uint32) ShareBundleJSON {
		b := dealer.ShareBundle{
			Curve: dealer.Secp256k1, ParticipantID: "node-1", PublicKey: pt.ScalarBaseMult(curve, secret),
			PartialPublicKeys: map[string]*pt.ECPoint{}, Bks: map[string]*birkhoffinterpolation.BkParameter{}, Threshold: threshold,
		}
		for i, id := range c.ids {
			v := evaluate(coefficients, points[i].x, points[i].rank)
			b.Bks[id] = birkhoffinterpolation.NewBkParameter(big.NewInt(points[i].x), points[i].rank)
			b.PartialPublicKeys[id] = pt.ScalarBaseMult(curve, v)
			if id == "node-1" {
				b.Share = v
			}
		}
		if err := b.Validate(); err != nil {
			t.Fatalf("crafted bundle does not validate: %v", err)
		}
		return EncodeBundle(b)
	}
	plain := []point{{1, 0}, {2, 0}, {3, 0}, {4, 0}, {5, 0}}
	cases := map[string]ShareBundleJSON{
		"two of five": bundleFor([]*big.Int{secret, coefficient("a1")}, plain, 2),
		"a derivative share that discloses the key with one other share": bundleFor([]*big.Int{secret, coefficient("b1"), coefficient("b2")}, []point{{3, 0}, {4, 0}, {5, 0}, {2, 0}, {1, 1}}, dealer.Threshold),
	}
	for label, bundle := range cases {
		r := c.call(t, c.byID("node-1")[0], PathImport, ImportRequest{SessionID: "import-scheme", KeyID: "scheme-key", Owner: testOwner, Share: bundle}, "")
		expectError(t, label, r, CodeKeyInvalidShare)
	}
	if _, err := c.byID("node-1")[0].store.Get("scheme-key"); err == nil {
		t.Fatal("a share outside the custody scheme was stored")
	}
	good := bundleFor([]*big.Int{secret, coefficient("c1"), coefficient("c2")}, plain, dealer.Threshold)
	decodeOK[KeyResponse](t, "custody scheme import", []apiResult{c.call(t, c.byID("node-1")[0], PathImport, ImportRequest{SessionID: "import-scheme-good", KeyID: "scheme-key", Owner: testOwner, Share: good}, "")})
}

func (c *testCluster) verifyOn(t *testing.T, client *http.Client, signers []string, session, keyID, importSession string) []apiResult {
	t.Helper()
	nodes := c.byID(signers...)
	results := make([]apiResult, len(nodes))
	var wg sync.WaitGroup
	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node *testNode) {
			defer wg.Done()
			results[i] = c.callAs(t, client, node, PathSign, VerificationRequest{SessionID: session, KeyID: keyID, Kind: KindOperatorVerification, Signers: signers, ImportSessionID: importSession}, "")
		}(i, node)
	}
	wg.Wait()
	return results
}

func auditBytes(t *testing.T, c *testCluster, n *testNode) []byte {
	t.Helper()
	logged, err := os.ReadFile(filepath.Join(c.configs[n.index].nodeDir, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	return logged
}

func TestOperatorVerificationInTheCeremonyWindow(t *testing.T) {
	c := newTestCluster(t, 5, true)
	fresh := func() string { return c.idp.mint(t, c.idp.key, testOwner) }
	signers := []string{"node-1", "node-3", "node-5"}

	secpSeed := sha256.Sum256([]byte("operator verification secp256k1 key"))
	secpKey, err := gethcrypto.ToECDSA(secpSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	address := gethcrypto.PubkeyToAddress(secpKey.PublicKey)
	secpPub := gethcrypto.FromECDSAPub(&secpKey.PublicKey)
	edSeed := sha256.Sum256([]byte("operator verification ed25519 key"))
	edPub := ed25519.NewKeyFromSeed(edSeed[:]).Public().(ed25519.PublicKey)
	edScalar, err := dealer.Ed25519ScalarFromSeed(edSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	importKey(t, c, "verify-evm", dealer.Secp256k1, new(big.Int).SetBytes(secpSeed[:]), "")
	importKey(t, c, "verify-lx", dealer.Ed25519, edScalar, address.Hex())
	for _, keyID := range []string{"verify-evm", "verify-lx"} {
		decodeOK[KeyResponse](t, "refresh "+keyID, c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
			return RefreshRequest{SessionID: "refresh-" + keyID, KeyID: keyID}
		}, ""))
	}
	secpMsg := policy.VerificationMessage("verify-evm", secpPub, "import-verify-evm")
	edMsg := policy.VerificationMessage("verify-lx", edPub, "import-verify-lx")

	for _, r := range c.verifyOn(t, c.opClient, signers, "verify-mismatch", "verify-evm", "import-other") {
		expectError(t, "mismatched import session", r, CodeVerificationNotImported)
	}
	gateway := c.callAs(t, c.client, c.nodes[0], PathSign, VerificationRequest{SessionID: "verify-by-gateway", KeyID: "verify-evm", Kind: KindOperatorVerification, Signers: signers, ImportSessionID: "import-verify-evm"}, "")
	if gateway.status != http.StatusForbidden {
		t.Fatalf("gateway identity on verification: status %d", gateway.status)
	}
	expectError(t, "gateway identity on verification", gateway, CodeOperatorRequired)
	expectError(t, "verification with a bearer token", c.callAs(t, c.opClient, c.nodes[0], PathSign, VerificationRequest{SessionID: "verify-with-token", KeyID: "verify-evm", Kind: KindOperatorVerification, Signers: signers, ImportSessionID: "import-verify-evm"}, fresh()), CodeSessionBadRequest)
	expectError(t, "verification carrying a message", c.callAs(t, c.opClient, c.nodes[0], PathSign, map[string]any{"session_id": "verify-with-message", "key_id": "verify-evm", "kind": KindOperatorVerification, "signers": signers, "import_session_id": "import-verify-evm", "message": hex.EncodeToString(secpMsg)}, ""), CodeSessionBadRequest)

	for _, r := range decodeOK[SignResponse](t, "secp256k1 verification", c.verifyOn(t, c.opClient, signers, "verify-evm-grant", "verify-evm", "import-verify-evm")) {
		if r.Kind != KindOperatorVerification || r.KeyID != "verify-evm" || r.Message != hex.EncodeToString(secpMsg) || r.SignedBytes != hex.EncodeToString(gethcrypto.Keccak256(secpMsg)) || r.RecoveryID == nil {
			t.Fatalf("%s: secp256k1 verification response %+v", r.NodeID, r)
		}
		sig, err := hex.DecodeString(r.Signature)
		if err != nil || len(sig) != 65 || sig[64] != *r.RecoveryID {
			t.Fatalf("%s: secp256k1 verification signature %s", r.NodeID, r.Signature)
		}
		pub, err := gethcrypto.SigToPub(gethcrypto.Keccak256(secpMsg), sig)
		if err != nil || gethcrypto.PubkeyToAddress(*pub) != address {
			t.Fatalf("%s: verification signature does not recover the imported address: %v", r.NodeID, err)
		}
	}
	for _, r := range decodeOK[SignResponse](t, "ed25519 verification", c.verifyOn(t, c.opClient, signers, "verify-lx-grant", "verify-lx", "import-verify-lx")) {
		sig, err := hex.DecodeString(r.Signature)
		if err != nil || r.Message != hex.EncodeToString(edMsg) || r.SignedBytes != hex.EncodeToString(edMsg) || r.RecoveryID != nil || !ed25519.Verify(edPub, edMsg, sig) {
			t.Fatalf("%s: ed25519 verification response %+v", r.NodeID, r)
		}
	}
	for _, keyID := range []string{"verify-evm", "verify-lx"} {
		for _, r := range c.verifyOn(t, c.opClient, signers, keyID+"-again", keyID, "import-"+keyID) {
			expectError(t, "second verification of "+keyID, r, CodeVerificationUsed)
		}
	}

	keyFor := map[dealer.Curve]string{dealer.Secp256k1: "verify-evm", dealer.Ed25519: "verify-lx"}
	msgFor := map[string][]byte{"verify-evm": secpMsg, "verify-lx": edMsg}
	for _, kind := range SignKinds() {
		keyID := keyFor[kindCurves[kind]]
		raw := hex.EncodeToString(msgFor[keyID])
		req := SignRequest{SessionID: "isolated-" + kind, KeyID: keyID, Kind: kind, Signers: signers, Message: raw}
		switch kind {
		case KindEVMTransaction:
			req.Message, req.Transaction = "", raw
		case KindTypedData:
			req.Message, req.TypedData = "", `{"types":{"EIP712Domain":[{"name":"chainId","type":"uint256"}],"Note":[{"name":"text","type":"string"}]},"primaryType":"Note","domain":{"chainId":125},"message":{"text":"`+VerificationDomain+`"}}`
		case KindLXActivity:
			req.Message, req.Activity = "", raw
		case KindEthSignDigest:
			req.Message, req.Digest = "", hex.EncodeToString(gethcrypto.Keccak256(secpMsg))
			req.Construction = &ConstructionJSON{Kind: policy.KindAuthorization, ChainID: "125", Address: address.Hex(), Nonce: "1"}
		}
		for _, r := range c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any { return req }, fresh()) {
			if e := expectError(t, "verification message under "+kind, r, CodePolicyDenied); e.PolicyCode != policy.CodeVerificationIsolated {
				t.Fatalf("verification message under %s: policy code %s", kind, e.PolicyCode)
			}
		}
	}

	decodeOK[KeyResponse](t, "generate", c.callAll(t, c.nodes, PathGenerate, func(*testNode) any {
		return GenerateRequest{SessionID: "generate-verify", KeyID: "verify-generated", Curve: "ed25519", Owner: testOwner, Account: address.Hex()}
	}, ""))
	for _, session := range []string{"generate-verify", "import-verify-generated"} {
		for _, r := range c.verifyOn(t, c.opClient, signers, "verify-generated-"+session, "verify-generated", session) {
			expectError(t, "generated key", r, CodeVerificationNotImported)
		}
	}

	for _, id := range signers {
		n := c.byID(id)[0]
		logged := auditBytes(t, c, n)
		if got := bytes.Count(logged, []byte("verification granted in window")); got != 2 {
			t.Fatalf("%s: %d verification grants audited, want 2", id, got)
		}
		for _, want := range []string{"ceremony.verification", "import-verify-evm", "import-verify-lx", CodeVerificationUsed, CodeVerificationNotImported, "sign." + KindPersonalMessage} {
			if !bytes.Contains(logged, []byte(want)) {
				t.Fatalf("%s: audit log lacks %q", id, want)
			}
		}
		if err := n.audit.Verify(); err != nil {
			t.Fatalf("%s: audit chain: %v", id, err)
		}
	}
	if logged := auditBytes(t, c, c.nodes[0]); !bytes.Contains(logged, []byte(CodeOperatorRequired)) || !bytes.Contains(logged, []byte(CodeSessionBadRequest)) {
		t.Fatal("node-1 did not audit the refused gateway and token verification requests")
	}
	for _, id := range []string{"node-2", "node-4"} {
		if bytes.Contains(auditBytes(t, c, c.byID(id)[0]), []byte("verification granted")) {
			t.Fatalf("%s granted a verification it was not asked for", id)
		}
	}

	restartSeed := sha256.Sum256([]byte("operator verification restart key"))
	restartScalar, err := dealer.Ed25519ScalarFromSeed(restartSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	importKey(t, c, "verify-restart", dealer.Ed25519, restartScalar, address.Hex())
	c.ceremony = false
	restarted := c.restart(t, c.nodes[0])
	expectError(t, "verification after a restart without the ceremony flag", c.callAs(t, c.opClient, restarted, PathSign, VerificationRequest{SessionID: "verify-after-restart", KeyID: "verify-restart", Kind: KindOperatorVerification, Signers: signers, ImportSessionID: "import-verify-restart"}, ""), CodeVerificationWindow)
	if !bytes.Contains(auditBytes(t, c, restarted), []byte(CodeVerificationWindow)) {
		t.Fatal("the restarted node did not audit the refusal outside the window")
	}
}

func TestDescribedFieldsReplaceALostParticipant(t *testing.T) {
	c := newTestCluster(t, 6, true)
	holders := c.ids[:5]
	lost, replacement := "node-5", "node-6"
	survivors := []string{"node-1", "node-2", "node-3", "node-4"}

	secpSeed := sha256.Sum256([]byte("attestor replacement secp256k1 key"))
	secpKey, err := gethcrypto.ToECDSA(secpSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	address := gethcrypto.PubkeyToAddress(secpKey.PublicKey)
	edSeed := sha256.Sum256([]byte("attestor replacement ed25519 key"))
	edPub := ed25519.NewKeyFromSeed(edSeed[:]).Public().(ed25519.PublicKey)
	edScalar, err := dealer.Ed25519ScalarFromSeed(edSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	importOn := func(keyID string, curve dealer.Curve, secret *big.Int, account string) {
		bundles, _, err := dealer.Split(curve, secret, holders)
		if err != nil {
			t.Fatal(err)
		}
		byNode := map[string]ShareBundleJSON{}
		for _, b := range bundles {
			byNode[b.ParticipantID] = EncodeBundle(b)
		}
		decodeOK[KeyResponse](t, "import "+keyID, c.callAll(t, c.byID(holders...), PathImport, func(n *testNode) any {
			return ImportRequest{SessionID: "import-" + keyID, KeyID: keyID, Owner: testOwner, Account: account, Share: byNode[n.id]}
		}, ""))
	}
	importOn("replace-evm", dealer.Secp256k1, new(big.Int).SetBytes(secpSeed[:]), "")
	importOn("replace-lx", dealer.Ed25519, edScalar, address.Hex())
	keys := []string{"replace-evm", "replace-lx"}

	refreshAll := func(label string, nodes []string) [][]apiResult {
		out := make([][]apiResult, len(keys))
		var wg sync.WaitGroup
		for i, keyID := range keys {
			wg.Add(1)
			go func(i int, keyID string) {
				defer wg.Done()
				out[i] = c.callAll(t, c.byID(nodes...), PathRefresh, func(*testNode) any {
					return RefreshRequest{SessionID: label + "-" + keyID, KeyID: keyID}
				}, "")
			}(i, keyID)
		}
		wg.Wait()
		return out
	}
	for i, results := range refreshAll("refresh-before-loss", holders) {
		for _, r := range decodeOK[KeyResponse](t, "refresh before loss "+keys[i], results) {
			if r.Epoch != 1 {
				t.Fatalf("%s %s: epoch %d after the first refresh", r.NodeID, r.KeyID, r.Epoch)
			}
		}
	}

	c.byID(lost)[0].stop()

	wantPublic := map[string]string{"replace-evm": "", "replace-lx": hex.EncodeToString(edPub)}
	wantAccount := map[string]string{"replace-evm": address.Hex(), "replace-lx": address.Hex()}
	for _, keyID := range keys {
		described := decodeOK[DescribeResponse](t, "describe "+keyID, []apiResult{
			c.call(t, c.byID("node-1")[0], PathDescribe, DescribeRequest{SessionID: "describe-a-" + keyID, KeyID: keyID}, ""),
			c.call(t, c.byID("node-2")[0], PathDescribe, DescribeRequest{SessionID: "describe-b-" + keyID, KeyID: keyID}, ""),
		})
		a, b := described[0], described[1]
		if a.Curve != b.Curve || a.PublicKey != b.PublicKey || a.Owner != b.Owner || a.Account != b.Account || a.Epoch != b.Epoch || strings.Join(a.Participants, ",") != strings.Join(b.Participants, ",") {
			t.Fatalf("%s: survivors describe the key differently: %+v and %+v", keyID, a, b)
		}
		if strings.Join(a.Participants, ",") != strings.Join(holders, ",") || a.Owner != testOwner || a.Account != wantAccount[keyID] || a.Epoch != 1 {
			t.Fatalf("%s: described %+v", keyID, a)
		}
		if want := wantPublic[keyID]; want != "" && a.PublicKey != want {
			t.Fatalf("%s: described public key %s, want %s", keyID, a.PublicKey, want)
		}
		if keyID == "replace-evm" && common.HexToAddress(a.Address) != address {
			t.Fatalf("%s: described address %s, want %s", keyID, a.Address, address.Hex())
		}

		var quorum []string
		for _, id := range a.Participants {
			if id != lost {
				quorum = append(quorum, id)
			}
		}
		body := AddShareRequest{SessionID: "addshare-" + keyID, KeyID: keyID, Curve: a.Curve, PublicKey: a.PublicKey, Owner: a.Owner, Account: a.Account, NewParticipantID: replacement, Quorum: quorum, Epoch: a.Epoch}
		holdersAfter := append(append([]string{}, quorum...), replacement)
		added := decodeOK[KeyResponse](t, "add-share "+keyID, c.callAll(t, c.byID(holdersAfter...), PathAddShare, func(*testNode) any { return body }, ""))
		for _, r := range added {
			if r.PublicKey != a.PublicKey || r.Epoch != a.Epoch || strings.Join(r.Participants, ",") != strings.Join(append(append([]string{}, survivors...), replacement), ",") {
				t.Fatalf("%s: add-share response %+v", r.NodeID, r)
			}
		}
		refreshed := decodeOK[KeyResponse](t, "refresh after add-share "+keyID, c.callAll(t, c.byID(holdersAfter...), PathRefresh, func(*testNode) any {
			return RefreshRequest{SessionID: "refresh-after-addshare-" + keyID, KeyID: keyID}
		}, ""))
		for _, r := range refreshed {
			if r.PublicKey != a.PublicKey || r.Epoch != a.Epoch+1 || !r.Refreshed {
				t.Fatalf("%s: refresh after add-share %+v", r.NodeID, r)
			}
		}
		onReplacement := decodeOK[DescribeResponse](t, "describe on the replacement "+keyID, []apiResult{
			c.call(t, c.byID(replacement)[0], PathDescribe, DescribeRequest{SessionID: "describe-new-" + keyID, KeyID: keyID}, ""),
		})[0]
		if onReplacement.PublicKey != a.PublicKey || onReplacement.Owner != a.Owner || onReplacement.Account != a.Account || onReplacement.Epoch != a.Epoch+1 || strings.Join(onReplacement.Participants, ",") != strings.Join(holdersAfter, ",") {
			t.Fatalf("%s: the replacement describes %+v", keyID, onReplacement)
		}
	}

	evmSigners := []string{"node-1", "node-3", replacement}
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID: big.NewInt(testChainID), Nonce: 1, GasTipCap: big.NewInt(1_000_000_000), GasFeeCap: big.NewInt(2_000_000_000),
		Gas: 21000, To: &to, Value: big.NewInt(1_000_000_000_000_000),
	})
	rawTx, err := tx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	txSigner := types.LatestSignerForChainID(big.NewInt(testChainID))
	txResults := decodeOK[SignResponse](t, "sign with the replacement", c.callAll(t, c.byID(evmSigners...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: "sign-replacement-evm", KeyID: "replace-evm", Kind: KindEVMTransaction, Signers: evmSigners, Transaction: hex.EncodeToString(rawTx)}
	}, c.idp.mint(t, c.idp.key, testOwner)))
	for _, r := range txResults {
		sig, err := hex.DecodeString(r.Signature)
		if err != nil {
			t.Fatal(err)
		}
		signed, err := tx.WithSignature(txSigner, sig)
		if err != nil {
			t.Fatal(err)
		}
		from, err := types.Sender(txSigner, signed)
		if err != nil || from != address {
			t.Fatalf("%s: recovered %s, want %s (%v)", r.NodeID, from.Hex(), address.Hex(), err)
		}
	}

	edSigners := []string{"node-2", "node-4", replacement}
	bind := lxwire.BindMessage(testChainID, address, 1)
	bindResults := decodeOK[SignResponse](t, "bind with the replacement", c.callAll(t, c.byID(edSigners...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: "sign-replacement-bind", KeyID: "replace-lx", Kind: KindLXBind, Signers: edSigners, Message: hex.EncodeToString(bind)}
	}, c.idp.mint(t, c.idp.key, testOwner)))
	for _, r := range bindResults {
		sig, _ := hex.DecodeString(r.Signature)
		if !ed25519.Verify(edPub, bind, sig) {
			t.Fatalf("%s: binding signature with the replacement does not verify", r.NodeID)
		}
	}
}
