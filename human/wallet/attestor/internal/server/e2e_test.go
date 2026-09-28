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
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/auth/jwt"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/policy"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
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
	claims, _ := json.Marshal(map[string]any{"sub": subject, "iss": p.issuer, "aud": "authenticated", "iat": now, "exp": now + 600})
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
	apiAddr string
	server  *Server
	audit   *audit.Log
	store   *store.Store
}

type testCluster struct {
	ca     *testCA
	nodes  []*testNode
	client *http.Client
	idp    *identityProvider
	ids    []string
}

func testPolicy() *policy.Document {
	chain := uint64(testChainID)
	rate := uint32(1000)
	return &policy.Document{
		Version: policy.Version,
		Defaults: policy.Rules{
			ChainID:       &chain,
			Kinds:         append([]string{}, SignKinds()...),
			Caps:          map[string]policy.Cap{policy.AssetNative: {PerTransaction: "1000000000000000000", Daily: "10000000000000000000"}},
			RatePerMinute: &rate,
		},
	}
}

func newTestCluster(t *testing.T, n int, ceremony bool) *testCluster {
	t.Helper()
	dir := t.TempDir()
	c := &testCluster{ca: newTestCA(t, dir), idp: newIdentityProvider(t)}
	clientCert := c.ca.issue(t, dir, "api-client", x509.ExtKeyUsageClientAuth)
	roots := x509.NewCertPool()
	roots.AddCert(c.ca.cert)
	c.client = &http.Client{Timeout: 10 * time.Minute, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{clientCert.pair}, RootCAs: roots,
	}}}

	peerListeners := make([]net.Listener, n)
	apiListeners := make([]net.Listener, n)
	certs := make([]testCert, n)
	peers := make([]transport.Peer, n)
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
		certs[i] = c.ca.issue(t, dir, id, x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth)
		pin := transport.SPKIHash(certs[i].cert)
		peers[i] = transport.Peer{ID: id, Address: peerListeners[i].Addr().String(), SPKISHA256: hex.EncodeToString(pin[:])}
	}
	registry, err := lxwire.NewRegistry(0x10005, 0x10007, 0x30002, 0x90005)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		nodeDir := filepath.Join(dir, c.ids[i])
		keyPath := filepath.Join(dir, c.ids[i]+".key")
		seed := sha256.Sum256([]byte("store key " + c.ids[i]))
		if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(seed[:])), 0o600); err != nil {
			t.Fatal(err)
		}
		nodeKey, err := config.ReadKeyFile(keyPath)
		if err != nil {
			t.Fatal(err)
		}
		st, err := store.Open(filepath.Join(nodeDir, "shares"), nodeKey)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		lg, err := audit.Open(filepath.Join(nodeDir, "audit"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lg.Close() })
		tr, err := transport.New(transport.Config{
			SelfID: c.ids[i], CertFile: certs[i].certPath, KeyFile: certs[i].keyPath,
			CAFile: c.ca.pemPath, Peers: peers, OperatorCAFile: c.ca.pemPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		pl := peerListeners[i]
		go func() { _ = tr.Serve(pl) }()
		t.Cleanup(func() { _ = tr.Close() })
		tokens, err := jwt.NewTokenVerifier(jwt.Config{JWKSURL: c.idp.srv.URL, Issuer: c.idp.issuer, Audience: "authenticated", HTTPClient: c.idp.srv.Client()})
		if err != nil {
			t.Fatal(err)
		}
		probe := map[string]string{}
		for j, p := range peers {
			if j != i {
				probe[p.ID] = p.Address
			}
		}
		srv, err := New(Options{
			NodeID: c.ids[i], Region: "test", ChainID: testChainID, Ceremony: ceremony, Participants: c.ids,
			Store: st, Audit: lg, Transport: tr, Policy: policy.New(testPolicy()),
			Ledger: policy.NewMemoryLedger(time.Now), Tokens: tokens, Activities: registry,
			PeerProbe: TCPPeerProbe(probe), ProtocolTimeout: 8 * time.Minute,
		})
		if err != nil {
			t.Fatal(err)
		}
		tlsCfg, err := APITLSConfig(certs[i].certPath, certs[i].keyPath, c.ca.pemPath)
		if err != nil {
			t.Fatal(err)
		}
		api := srv.Serve(apiListeners[i], tlsCfg)
		t.Cleanup(func() { _ = api.Close() })
		c.nodes = append(c.nodes, &testNode{id: c.ids[i], apiAddr: apiListeners[i].Addr().String(), server: srv, audit: lg, store: st})
	}
	return c
}

type apiResult struct {
	status int
	body   []byte
}

func (c *testCluster) call(t *testing.T, node *testNode, path string, body any, token string) apiResult {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://"+node.apiAddr+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Errorf("%s %s: %v", node.id, path, err)
		return apiResult{}
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return apiResult{status: resp.StatusCode, body: out}
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

type fixtureActivity struct {
	Name              string `json:"name"`
	Unsigned          any    `json:"unsigned"`
	SignaturePreimage string `json:"signature_preimage"`
}

func loadActivity(t *testing.T, name string) ([]byte, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "lxwire", "testdata", "activities.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Activities []fixtureActivity `json:"activities"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, a := range doc.Activities {
		if a.Name != name {
			continue
		}
		field, ok := a.Unsigned.(map[string]any)
		if !ok {
			t.Fatalf("%s: unsigned is not inline", name)
		}
		unsigned, err := hex.DecodeString(field["hex"].(string))
		if err != nil {
			t.Fatal(err)
		}
		pre, err := hex.DecodeString(a.SignaturePreimage)
		if err != nil {
			t.Fatal(err)
		}
		return unsigned, pre
	}
	t.Fatalf("activity %s missing from fixture", name)
	return nil, nil
}

const mailTypedData = `{"types":{"EIP712Domain":[{"name":"name","type":"string"},{"name":"version","type":"string"},{"name":"chainId","type":"uint256"},{"name":"verifyingContract","type":"address"}],"Person":[{"name":"name","type":"string"},{"name":"wallet","type":"address"}],"Mail":[{"name":"from","type":"Person"},{"name":"to","type":"Person"},{"name":"contents","type":"string"}]},"primaryType":"Mail","domain":{"name":"Ether Mail","version":"1","chainId":125,"verifyingContract":"0xCcCCccccCCCCcCCCCCCcCcCccCcCCCcCcccccccC"},"message":{"from":{"name":"Cow","wallet":"0xCD2a3d9F938E13CD947Ec05AbC7FE734Df8DD826"},"to":{"name":"Bob","wallet":"0xbBbBBBBbbBBBbbbBbbBbbbbBBbBbbbbBbBbbBBbB"},"contents":"Hello, Bob!"}}`

func TestFiveNodeEndToEnd(t *testing.T) {
	c := newTestCluster(t, 5, true)
	token := c.idp.mint(t, c.idp.key, testOwner)

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
	tdResults := decodeOK[SignResponse](t, "typed data", sign("sign-typed-data", SignRequest{KeyID: "evm-key", Kind: KindTypedData, TypedData: mailTypedData}, token))
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
	bindResults := decodeOK[SignResponse](t, "bind", sign("sign-bind", SignRequest{KeyID: "lx-key", Kind: KindLXBind, Message: hex.EncodeToString(bind)}, token))
	for _, r := range bindResults {
		sig, _ := hex.DecodeString(r.Signature)
		if r.RecoveryID != nil || !ed25519.Verify(edPub, bind, sig) {
			t.Fatalf("%s: binding signature does not verify", r.NodeID)
		}
	}

	unsigned, preimage := loadActivity(t, "native-send")
	actResults := decodeOK[SignResponse](t, "activity", sign("sign-activity", SignRequest{KeyID: "lx-key", Kind: KindLXActivity, Activity: hex.EncodeToString(unsigned)}, token))
	for _, r := range actResults {
		sig, _ := hex.DecodeString(r.Signature)
		if r.SignedBytes != hex.EncodeToString(preimage) || !ed25519.Verify(edPub, preimage, sig) {
			t.Fatalf("%s: activity signature does not verify over the fixture preimage", r.NodeID)
		}
	}

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
	for _, r := range sign("sign-over-cap", SignRequest{KeyID: "evm-key", Kind: KindEVMTransaction, Transaction: hex.EncodeToString(rawBig)}, token) {
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
