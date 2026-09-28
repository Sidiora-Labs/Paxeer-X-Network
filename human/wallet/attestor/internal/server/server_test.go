package server

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/backup"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/lxwire"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/tss/dealer"
)

var updateGolden = flag.Bool("update", false, "rewrite the attestor API golden files")

const schemaDir = "../../../schema/attestor-api"

func goldenValues(t *testing.T) map[string]any {
	t.Helper()
	ids := []string{"node-1", "node-2", "node-3", "node-4", "node-5"}
	bundles := fixedBundles(t, ids)
	recovery := uint8(1)
	return map[string]any{
		"keys.generate.request":  GenerateRequest{SessionID: "session-generate", KeyID: "key-ed", Curve: "ed25519", Owner: "user-0001", Account: "0x1111111111111111111111111111111111111111"},
		"keys.import.request":    ImportRequest{SessionID: "session-import", KeyID: "key-evm", Owner: "user-0001", Share: EncodeBundle(bundles[0])},
		"keys.refresh.request":   RefreshRequest{SessionID: "session-refresh", KeyID: "key-evm"},
		"keys.addshare.request":  AddShareRequest{SessionID: "session-addshare", KeyID: "key-evm", Curve: "secp256k1", PublicKey: "02" + strings.Repeat("11", 32), Owner: "user-0001", NewParticipantID: "node-6", Quorum: []string{"node-1", "node-3", "node-4", "node-5"}},
		"keys.describe.request":  DescribeRequest{SessionID: "session-describe", KeyID: "key-evm"},
		"keys.describe.response": DescribeResponse{NodeID: "node-1", KeyID: "key-evm", Curve: "secp256k1", PublicKey: "04" + strings.Repeat("22", 64), Address: "0x2222222222222222222222222222222222222222", Owner: "user-0001", Account: "0x2222222222222222222222222222222222222222", Epoch: 1, Participants: ids, AuditSequence: 4},
		"keys.response":          KeyResponse{NodeID: "node-1", KeyID: "key-evm", Curve: "secp256k1", PublicKey: "04" + strings.Repeat("22", 64), Address: "0x2222222222222222222222222222222222222222", Epoch: 1, Participants: ids, Refreshed: true, AuditSequence: 3},
		"sign.request":           SignRequest{SessionID: "session-sign", KeyID: "key-evm", Kind: KindEVMTransaction, Signers: []string{"node-1", "node-3", "node-5"}, Transaction: "02f86c"},
		"sign.request.lx_grant": SignRequest{SessionID: "session-grant", KeyID: "key-ed", Kind: KindLXGrant, Signers: []string{"node-1", "node-2", "node-3"}, Grant: &GrantJSON{
			From: strings.Repeat("81", 32), Recipient: strings.Repeat("1e", 32), Asset: strings.Repeat("0a", 32), PerDrawMaximum: "1000", Allowance: "50000",
			Expiration: 1900000000, PurposeHash: strings.Repeat("64", 32),
		}},
		"sign.request.lx_send_authorization": SignRequest{SessionID: "session-send-authorization", KeyID: "key-ed", Kind: KindLXSendAuth, Signers: []string{"node-1", "node-2", "node-3"}, Activity: "0003" + strings.Repeat("77", 16)},
		"sign.request.eth_sign_digest": SignRequest{SessionID: "session-authorization", KeyID: "key-evm", Kind: KindEthSignDigest, Signers: []string{"node-1", "node-3", "node-5"}, Digest: "0x" + strings.Repeat("66", 32), Construction: &ConstructionJSON{
			Kind: "eip7702_authorization", ChainID: "125", Address: "0x2222222222222222222222222222222222222222", Nonce: "3",
		}},
		"sign.request.operator_verification": VerificationRequest{SessionID: "session-verification", KeyID: "key-evm", Kind: KindOperatorVerification, Signers: []string{"node-1", "node-3", "node-5"}, ImportSessionID: "session-import"},
		"sign.response":                      SignResponse{NodeID: "node-1", KeyID: "key-evm", Kind: KindEVMTransaction, SignedBytes: strings.Repeat("33", 32), Signature: strings.Repeat("44", 64) + "01", RecoveryID: &recovery, AuditSequence: 9},
		"error.policy":                       errorBody{Error: policyError("value_cap", "amount exceeds the per-transaction cap")},
		"error.token":                        errorBody{Error: newError(CodeTokenMissing, "a bearer token or agent signature is required")},
		"health.response":                    health.Report{NodeID: "node-1", Region: "region-a", ShareCount: 2, RefreshEpoch: 1, AuditSequence: 9, AuditHead: strings.Repeat("55", 32), Peers: map[string]health.PeerState{"node-2": {Reachable: true, RTT: 1000}}, ReachablePeers: 1},
	}
}

func fixedBundles(t *testing.T, ids []string) []dealer.ShareBundle {
	t.Helper()
	ec, err := dealer.Secp256k1.Elliptic()
	if err != nil {
		t.Fatal(err)
	}
	order := ec.Params().N
	coefficients := []*big.Int{big.NewInt(0x5eed), big.NewInt(0x1d0c), big.NewInt(0x2b7f)}
	bks := make(map[string]*birkhoffinterpolation.BkParameter, len(ids))
	shares := make(map[string]*big.Int, len(ids))
	partials := make(map[string]*pt.ECPoint, len(ids))
	for i, id := range ids {
		x := big.NewInt(int64(i + 1))
		share := new(big.Int)
		for j := len(coefficients) - 1; j >= 0; j-- {
			share.Mul(share, x)
			share.Add(share, coefficients[j])
			share.Mod(share, order)
		}
		bks[id] = birkhoffinterpolation.NewBkParameter(x, 0)
		shares[id] = share
		partials[id] = pt.ScalarBaseMult(ec, share)
	}
	publicKey := pt.ScalarBaseMult(ec, coefficients[0])
	out := make([]dealer.ShareBundle, len(ids))
	for i, id := range ids {
		pp := make(map[string]*pt.ECPoint, len(partials))
		for k, v := range partials {
			pp[k] = v.Copy()
		}
		bk := make(map[string]*birkhoffinterpolation.BkParameter, len(bks))
		for k, v := range bks {
			bk[k] = v
		}
		out[i] = dealer.ShareBundle{Curve: dealer.Secp256k1, ParticipantID: id, Share: shares[id], PublicKey: publicKey.Copy(), PartialPublicKeys: pp, Bks: bk, Threshold: dealer.Threshold}
		if err := out[i].Validate(); err != nil {
			t.Fatalf("fixed bundle %s: %v", id, err)
		}
	}
	return out
}

func parseKVX(raw string) map[string]map[string]string {
	sections := make(map[string]map[string]string)
	current := ""
	sections[current] = make(map[string]string)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if sections[current] == nil {
				sections[current] = make(map[string]string)
			}
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		sections[current][strings.TrimSpace(key)] = value
	}
	return sections
}

func TestParseKVXToleratesPadding(t *testing.T) {
	got := parseKVX("[op.a]\npath         = \"/v1/a\"\ncategory=\"token\"\n")
	if got["op.a"]["path"] != "/v1/a" || got["op.a"]["category"] != "token" {
		t.Fatalf("parsed %v", got)
	}
}

func TestGoldenMessages(t *testing.T) {
	dir := filepath.Join(schemaDir, "golden")
	for name, v := range goldenValues(t) {
		got, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		path := filepath.Join(dir, name+".json")
		if *updateGolden {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run with -update)", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: golden differs\n got %s\nwant %s", name, got, want)
		}
	}
}

func TestSchemaListsEveryErrorCodeAndOperation(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(schemaDir, "v1.kvx"))
	if err != nil {
		t.Fatal(err)
	}
	schema := string(raw)
	sections := parseKVX(schema)
	for code, category := range ErrorCodes() {
		block, ok := sections["error."+code]
		if !ok || block["category"] != category {
			t.Fatalf("v1.kvx does not list error %s in category %s", code, category)
		}
	}
	for _, kind := range SignKinds() {
		if !strings.Contains(schema, "\""+kind+"\"") {
			t.Fatalf("v1.kvx does not list sign kind %s", kind)
		}
	}
	described := make(map[string]bool)
	for _, block := range sections {
		if p, ok := block["path"]; ok {
			described[p] = true
		}
	}
	for _, path := range []string{PathGenerate, PathImport, PathRefresh, PathAddShare, PathDescribe, PathSign, PathHealth} {
		if !described[path] {
			t.Fatalf("v1.kvx does not describe %s", path)
		}
	}
}

func verifiedRequest(root *x509.Certificate, method, path string, body []byte) *http.Request {
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{root}}}
	return r
}

func TestHandlerRefusals(t *testing.T) {
	c := newTestCluster(t, 2, false)
	s := c.nodes[0].server
	h := s.Handler()
	do := func(r *http.Request) apiResult {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return apiResult{status: w.Code, body: w.Body.Bytes()}
	}
	op := func(method, path string, body []byte) *http.Request {
		return verifiedRequest(c.operator.cert, method, path, body)
	}
	gw := func(method, path string, body []byte) *http.Request {
		return verifiedRequest(c.ca.cert, method, path, body)
	}

	plain := httptest.NewRequest(http.MethodPost, PathSign, strings.NewReader("{}"))
	expectError(t, "no client certificate", do(plain), CodeOperatorRequired)
	expectError(t, "wrong method", do(gw(http.MethodGet, PathSign, nil)), CodeSessionBadRequest)
	expectError(t, "import without ceremony", do(op(http.MethodPost, PathImport, []byte(`{}`))), CodeKeyImportDisabled)
	expectError(t, "unknown field", do(op(http.MethodPost, PathRefresh, []byte(`{"session_id":"s","key_id":"k","extra":1}`))), CodeSessionBadRequest)
	expectError(t, "slash in session", do(op(http.MethodPost, PathRefresh, []byte(`{"session_id":"a/b","key_id":"k"}`))), CodeSessionBadRequest)
	expectError(t, "missing key", do(op(http.MethodPost, PathRefresh, []byte(`{"session_id":"s","key_id":"absent"}`))), CodeKeyNotFound)
	expectError(t, "unknown kind", do(gw(http.MethodPost, PathSign, []byte(`{"session_id":"s","key_id":"absent","kind":"raw"}`))), CodeSessionKind)
	expectError(t, "sign missing key", do(gw(http.MethodPost, PathSign, []byte(`{"session_id":"s","key_id":"absent","kind":"evm_tx"}`))), CodeKeyNotFound)
	expectError(t, "unknown curve", do(gw(http.MethodPost, PathGenerate, []byte(`{"session_id":"s","key_id":"k","curve":"p256","owner":"o"}`))), CodeKeyCurve)
	expectError(t, "ed25519 without account", do(gw(http.MethodPost, PathGenerate, []byte(`{"session_id":"s","key_id":"k","curve":"ed25519","owner":"o"}`))), CodeSessionBadRequest)
	expectError(t, "addshare small quorum", do(op(http.MethodPost, PathAddShare, []byte(`{"session_id":"s","key_id":"k","curve":"secp256k1","public_key":"02`+strings.Repeat("79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", 1)+`","owner":"o","new_participant_id":"node-9","quorum":["node-1","node-2"]}`))), CodeQuorumTooFew)

	for _, path := range []string{PathImport, PathRefresh, PathAddShare, PathDescribe} {
		before, _ := c.nodes[0].audit.Head()
		r := do(gw(http.MethodPost, path, []byte(`{"session_id":"s","key_id":"k"}`)))
		if r.status != http.StatusForbidden {
			t.Fatalf("gateway identity on %s: status %d", path, r.status)
		}
		expectError(t, "gateway identity on "+path, r, CodeOperatorRequired)
		if after, _ := c.nodes[0].audit.Head(); after != before+1 {
			t.Fatalf("refusal on %s was not audited: head %d then %d", path, before, after)
		}
	}
	for _, path := range []string{PathGenerate, PathSign} {
		expectError(t, "operator identity on "+path, do(op(http.MethodPost, path, []byte(`{"session_id":"s","key_id":"k","kind":"evm_tx","curve":"secp256k1","owner":"o"}`))), CodeOperatorRequired)
	}
	stranger := newTestCA(t, t.TempDir())
	expectError(t, "unknown root on sign", do(verifiedRequest(stranger.cert, http.MethodPost, PathSign, []byte(`{}`))), CodeOperatorRequired)
	expectError(t, "unknown root on refresh", do(verifiedRequest(stranger.cert, http.MethodPost, PathRefresh, []byte(`{}`))), CodeOperatorRequired)

	before, _ := c.nodes[0].audit.Head()
	expectError(t, "audited sign refusal", do(gw(http.MethodPost, PathSign, []byte(`{"session_id":"s","key_id":"absent","kind":"evm_tx"}`))), CodeKeyNotFound)
	if after, _ := c.nodes[0].audit.Head(); after != before+1 {
		t.Fatalf("sign refusal was not audited: head %d then %d", before, after)
	}

	r := do(gw(http.MethodGet, PathHealth, nil))
	var report health.Report
	if err := json.Unmarshal(r.body, &report); err != nil || report.NodeID != "node-1" {
		t.Fatalf("health: %d %s", r.status, r.body)
	}
	if report.Ready || r.status != http.StatusServiceUnavailable {
		t.Fatalf("health of a two-node cluster reported ready: %d %s", r.status, r.body)
	}

	if err := c.nodes[0].audit.Close(); err != nil {
		t.Fatal(err)
	}
	expectError(t, "refusal with a closed audit log", do(gw(http.MethodPost, PathSign, []byte(`{"session_id":"s","key_id":"absent","kind":"evm_tx"}`))), CodeStoreAuditFailed)
	expectError(t, "authority refusal with a closed audit log", do(gw(http.MethodPost, PathRefresh, []byte(`{}`))), CodeStoreAuditFailed)
}

func TestClientAuthoritiesRefuseMissingFiles(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, dir)
	if _, err := LoadClientAuthorities(ca.pemPath, filepath.Join(dir, "absent.pem")); err == nil {
		t.Fatal("missing operator CA file accepted")
	}
	empty := filepath.Join(dir, "empty.pem")
	if err := os.WriteFile(empty, []byte("no certificate here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClientAuthorities(empty, ca.pemPath); err == nil {
		t.Fatal("gateway CA file without a certificate accepted")
	}
	clients, err := LoadClientAuthorities(ca.pemPath, ca.pemPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := clients.Of(&tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{ca.cert}}}); got != AuthorityGateway|AuthorityOperator {
		t.Fatalf("one CA in both files grants %d", got)
	}
	if got := clients.Of(nil); got != 0 {
		t.Fatalf("no connection state grants %d", got)
	}
}

func TestBundleRoundTripAndPublicKeyParsing(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e"}
	for _, curve := range []dealer.Curve{dealer.Secp256k1, dealer.Ed25519} {
		bundles, pub, err := dealer.Split(curve, big.NewInt(424242), ids)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range bundles {
			enc := EncodeBundle(b)
			raw, _ := json.Marshal(enc)
			var back ShareBundleJSON
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatal(err)
			}
			got, err := DecodeBundle(back)
			if err != nil || got.Share.Cmp(b.Share) != 0 || !got.PublicKey.Equal(pub) {
				t.Fatalf("%s %s: round trip failed: %v", curve, b.ParticipantID, err)
			}
		}
		keyBytes, err := publicKeyBytes(curve, pub)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parsePublicKey(curve, "0x"+strings.TrimPrefix(string(mustHex(keyBytes)), "0x"))
		if err != nil || !parsed.Equal(pub) {
			t.Fatalf("%s: public key parse: %v", curve, err)
		}
		tampered := EncodeBundle(bundles[0])
		tampered.Share = "01"
		if _, err := DecodeBundle(tampered); err == nil {
			t.Fatalf("%s: tampered share accepted", curve)
		}
	}
}

func mustHex(b []byte) []byte {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 2*len(b))
	for _, v := range b {
		out = append(out, digits[v>>4], digits[v&0x0f])
	}
	return out
}

func TestProtocolSessionKinds(t *testing.T) {
	want := map[string]transport.SessionKind{
		protocolRefreshSecp: transport.KindRefreshSecp256k1,
		protocolRefreshEd:   transport.KindRefreshEd25519,
		protocolAddShare:    transport.KindAddShare,
		protocolKeygenSecp:  transport.KindKeygenSecp256k1,
		protocolKeygenEd:    transport.KindKeygenEd25519,
		protocolSignSecp:    transport.KindSignSecp256k1,
		protocolSignEd:      transport.KindSignEd25519,
	}
	if len(protocolKinds) != len(want) {
		t.Fatalf("protocol kinds %v", protocolKinds)
	}
	for protocol, kind := range want {
		if got, ok := protocolKinds[protocol]; !ok || got != kind {
			t.Fatalf("%s: kind %v, want %v", protocol, got, kind)
		}
	}
}

type snapshotHarness struct {
	root    string
	mu      sync.Mutex
	writers map[string]*backup.Writer
}

func snapshotBackupKey(nodeID string) []byte {
	sum := sha256.Sum256([]byte("snapshot backup key " + nodeID))
	return sum[:]
}

func (h *snapshotHarness) tune(t *testing.T) func(*Options) {
	return func(opts *Options) {
		w, err := backup.New(backup.Config{NodeID: opts.NodeID, Dir: filepath.Join(h.root, opts.NodeID), BackupKey: snapshotBackupKey(opts.NodeID), Retain: 10, Store: opts.Store, Audit: opts.Audit, Logger: log.New(io.Discard, "", 0)})
		if err != nil {
			t.Fatal(err)
		}
		opts.Snapshots = w
		opts.ProtocolTimeout = 2 * time.Minute
		h.mu.Lock()
		h.writers[opts.NodeID] = w
		h.mu.Unlock()
	}
}

func (h *snapshotHarness) snapshots(t *testing.T, nodeID string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.root, nodeID))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("%s: temporary snapshot %s left behind", nodeID, e.Name())
		}
		out = append(out, e.Name())
	}
	return out
}

func (h *snapshotHarness) expect(t *testing.T, c *testCluster, operation string, want map[string]int, keyID string) {
	t.Helper()
	for _, cfg := range c.configs {
		names := h.snapshots(t, cfg.id)
		if len(names) != want[cfg.id] {
			t.Fatalf("after %s: %s holds %d snapshots %v, want %d", operation, cfg.id, len(names), names, want[cfg.id])
		}
		if want[cfg.id] == 0 {
			continue
		}
		latest := names[len(names)-1]
		if latest != backup.Name(cfg.id, uint64(want[cfg.id])) {
			t.Fatalf("after %s: %s latest snapshot %s", operation, cfg.id, latest)
		}
		logged, err := os.ReadFile(filepath.Join(cfg.nodeDir, "audit", audit.FileName))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(logged, []byte("name="+latest)) || !bytes.Contains(logged, []byte(backup.SnapshotEvent)) {
			t.Fatalf("after %s: %s audit log lacks the snapshot event for %s", operation, cfg.id, latest)
		}
		if keyID == "" {
			continue
		}
		nodeKey, err := config.ReadKeyFile(cfg.keyPath)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(h.root, cfg.id, latest))
		if err != nil {
			t.Fatal(err)
		}
		st, err := store.Restore(bytes.NewReader(raw), snapshotBackupKey(cfg.id), cfg.id, filepath.Join(t.TempDir(), "check"), nodeKey)
		if err != nil {
			t.Fatalf("after %s: %s snapshot %s: %v", operation, cfg.id, latest, err)
		}
		_, getErr := st.Get(keyID)
		st.Close()
		if getErr != nil {
			t.Fatalf("after %s: %s snapshot %s lacks %s: %v", operation, cfg.id, latest, keyID, getErr)
		}
	}
}

func importSnapshotKey(t *testing.T, c *testCluster, holders []string, keyID, account string) ed25519.PublicKey {
	t.Helper()
	edSeed := sha256.Sum256([]byte("attestor snapshot ed25519 key " + keyID))
	edPub := ed25519.NewKeyFromSeed(edSeed[:]).Public().(ed25519.PublicKey)
	edScalar, err := dealer.Ed25519ScalarFromSeed(edSeed[:])
	if err != nil {
		t.Fatal(err)
	}
	bundles, _, err := dealer.Split(dealer.Ed25519, edScalar, holders)
	if err != nil {
		t.Fatal(err)
	}
	byNode := map[string]ShareBundleJSON{}
	for _, b := range bundles {
		byNode[b.ParticipantID] = EncodeBundle(b)
	}
	decodeOK[KeyResponse](t, "import", c.callAll(t, c.byID(holders...), PathImport, func(n *testNode) any {
		return ImportRequest{SessionID: "import-" + keyID, KeyID: keyID, Owner: testOwner, Account: account, Share: byNode[n.id]}
	}, ""))
	return edPub
}

func TestSnapshotFollowsEveryKeyChangeAndRestoresToSign(t *testing.T) {
	h := &snapshotHarness{root: t.TempDir(), writers: map[string]*backup.Writer{}}
	c := newTestClusterWith(t, 5, true, testPolicy(), h.tune(t))
	h.expect(t, c, "start", map[string]int{}, "")

	genAccount := common.HexToAddress("0x4444444444444444444444444444444444444444").Hex()
	decodeOK[KeyResponse](t, "generate", c.callAll(t, c.nodes, PathGenerate, func(*testNode) any {
		return GenerateRequest{SessionID: "generate-snap", KeyID: "gen-key", Curve: "ed25519", Owner: testOwner, Account: genAccount}
	}, ""))
	h.expect(t, c, "keys.generate", map[string]int{"node-1": 1, "node-2": 1, "node-3": 1, "node-4": 1, "node-5": 1}, "gen-key")

	decodeOK[KeyResponse](t, "refresh", c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
		return RefreshRequest{SessionID: "refresh-snap", KeyID: "gen-key"}
	}, ""))
	h.expect(t, c, "keys.refresh", map[string]int{"node-1": 2, "node-2": 2, "node-3": 2, "node-4": 2, "node-5": 2}, "gen-key")

	account := common.HexToAddress("0x5555555555555555555555555555555555555555").Hex()
	edPub := importSnapshotKey(t, c, c.ids, "snap-key", account)
	h.expect(t, c, "keys.import", map[string]int{"node-1": 3, "node-2": 3, "node-3": 3, "node-4": 3, "node-5": 3}, "snap-key")

	for _, id := range c.ids {
		h.mu.Lock()
		state := h.writers[id].State()
		h.mu.Unlock()
		if state.LastWritten.IsZero() || state.LastError != "" || state.Failures != 0 {
			t.Fatalf("%s: snapshot state %+v", id, state)
		}
	}
	healthResp, err := c.client.Get("https://" + c.byID("node-1")[0].apiAddr + PathHealth)
	if err != nil {
		t.Fatal(err)
	}
	healthBody, err := io.ReadAll(healthResp.Body)
	healthResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	var report health.Report
	if err := json.Unmarshal(healthBody, &report); err != nil || report.Snapshot == nil || report.Snapshot.LastWrittenAgeSeconds == nil || report.Snapshot.LastError != "" || report.Snapshot.Failures != 0 {
		t.Fatalf("health report lacks the snapshot state: %s", healthBody)
	}

	cfg := c.configs[0]
	latest := backup.Name(cfg.id, 3)
	snapPath := filepath.Join(h.root, cfg.id, latest)
	raw, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	nodeKey, err := config.ReadKeyFile(cfg.keyPath)
	if err != nil {
		t.Fatal(err)
	}
	c.byID(cfg.id)[0].stop()
	sharesDir := filepath.Join(cfg.nodeDir, "shares")
	if err := os.Rename(sharesDir, filepath.Join(cfg.nodeDir, "shares-lost")); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.RestoreFile(snapPath, hex.EncodeToString(digest[:]), "node-2", snapshotBackupKey(cfg.id), sharesDir, nodeKey); err == nil {
		t.Fatal("restore under a foreign node id succeeded")
	}
	res, err := backup.RestoreFile(snapPath, hex.EncodeToString(digest[:]), cfg.id, snapshotBackupKey(cfg.id), sharesDir, nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	if res.Name != latest || res.Shares != 2 {
		t.Fatalf("restore result %+v", res)
	}
	c.restart(t, c.byID(cfg.id)[0])

	token := c.idp.mint(t, c.idp.key, testOwner)
	signers := []string{"node-1", "node-3", "node-5"}
	bind := lxwire.BindMessage(testChainID, common.HexToAddress(account), 1)
	results := decodeOK[SignResponse](t, "sign after restore", c.callAll(t, c.byID(signers...), PathSign, func(*testNode) any {
		return SignRequest{SessionID: "sign-restored", KeyID: "snap-key", Kind: KindLXBind, Signers: signers, Message: hex.EncodeToString(bind)}
	}, token))
	for _, r := range results {
		sig, _ := hex.DecodeString(r.Signature)
		if !ed25519.Verify(edPub, bind, sig) {
			t.Fatalf("%s: signature after restore does not verify", r.NodeID)
		}
	}
}

func TestSnapshotFollowsAddShare(t *testing.T) {
	h := &snapshotHarness{root: t.TempDir(), writers: map[string]*backup.Writer{}}
	c := newTestClusterWith(t, 6, true, testPolicy(), h.tune(t))
	holders := c.ids[:5]
	account := common.HexToAddress("0x6666666666666666666666666666666666666666").Hex()
	edPub := importSnapshotKey(t, c, holders, "share-key", account)
	h.expect(t, c, "keys.import", map[string]int{"node-1": 1, "node-2": 1, "node-3": 1, "node-4": 1, "node-5": 1}, "share-key")

	quorum := []string{"node-1", "node-2", "node-3"}
	newcomer := "node-6"
	decodeOK[KeyResponse](t, "add-share", c.callAll(t, c.byID(append(append([]string{}, quorum...), newcomer)...), PathAddShare, func(*testNode) any {
		return AddShareRequest{SessionID: "addshare-snap", KeyID: "share-key", Curve: "ed25519", PublicKey: hex.EncodeToString(edPub), Owner: testOwner, Account: account, NewParticipantID: newcomer, Quorum: quorum}
	}, ""))
	h.expect(t, c, "keys.addshare", map[string]int{"node-1": 2, "node-2": 2, "node-3": 2, "node-4": 1, "node-5": 1, "node-6": 1}, "share-key")
	logged, err := os.ReadFile(filepath.Join(c.configs[5].nodeDir, "audit", audit.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logged, []byte("trigger=keys.addshare")) {
		t.Fatal("the new participant's snapshot event does not name keys.addshare")
	}
}

func heldShareHex(t *testing.T, n *testNode, keyID string) string {
	t.Helper()
	_, payload, e := n.server.loadShare(keyID)
	if e != nil {
		t.Fatalf("%s %s: %v", n.id, keyID, e)
	}
	b, _, e := payload.bundle()
	if e != nil {
		t.Fatalf("%s %s: %v", n.id, keyID, e)
	}
	defer dealer.Wipe(b.Share)
	return hexInt(b.Share)
}

func TestDescribeReturnsPublicMaterialToTheOperator(t *testing.T) {
	c := newTestCluster(t, 5, true)
	edAccount := common.HexToAddress("0x4444444444444444444444444444444444444444").Hex()
	secpSecret := new(big.Int).SetBytes([]byte("attestor describe secp256k1 key"))
	edSecret := new(big.Int).SetBytes([]byte("attestor describe ed25519 key"))
	type keyCase struct {
		id      string
		curve   string
		account string
		created []KeyResponse
	}
	cases := []*keyCase{
		{id: "generated-evm", curve: "secp256k1"},
		{id: "generated-lx", curve: "ed25519", account: edAccount},
		{id: "imported-evm", curve: "secp256k1"},
		{id: "imported-lx", curve: "ed25519", account: edAccount},
	}
	var wg sync.WaitGroup
	results := make([][]apiResult, len(cases))
	for i, kc := range cases[:2] {
		wg.Add(1)
		go func(i int, kc *keyCase) {
			defer wg.Done()
			results[i] = c.callAll(t, c.nodes, PathGenerate, func(*testNode) any {
				return GenerateRequest{SessionID: "generate-" + kc.id, KeyID: kc.id, Curve: kc.curve, Owner: testOwner, Account: kc.account}
			}, "")
		}(i, kc)
	}
	wg.Wait()
	for i, kc := range cases[:2] {
		kc.created = decodeOK[KeyResponse](t, "generate "+kc.id, results[i])
	}
	cases[2].created = importKey(t, c, cases[2].id, dealer.Secp256k1, secpSecret, "")
	cases[3].created = importKey(t, c, cases[3].id, dealer.Ed25519, edSecret, edAccount)
	for _, kc := range cases {
		if kc.account == "" {
			kc.account = common.HexToAddress(kc.created[0].Address).Hex()
		}
	}

	check := func(stage string, epoch uint64) {
		for _, kc := range cases {
			for i, n := range c.nodes {
				r := c.call(t, n, PathDescribe, DescribeRequest{SessionID: "describe-" + stage + "-" + kc.id, KeyID: kc.id}, "")
				if r.status != http.StatusOK {
					t.Fatalf("%s %s on %s: status %d body %s", stage, kc.id, n.id, r.status, r.body)
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(r.body, &fields); err != nil {
					t.Fatal(err)
				}
				want := []string{"account", "audit_sequence", "curve", "epoch", "key_id", "node_id", "owner", "participants", "public_key"}
				if kc.curve == "secp256k1" {
					want = append(want, "address")
				} else {
					want = append(want, "did")
				}
				if len(fields) != len(want) {
					t.Fatalf("%s %s on %s: fields %s", stage, kc.id, n.id, r.body)
				}
				for _, name := range want {
					if _, ok := fields[name]; !ok {
						t.Fatalf("%s %s on %s: field %s missing from %s", stage, kc.id, n.id, name, r.body)
					}
				}
				if share := heldShareHex(t, n, kc.id); strings.Contains(string(r.body), share) {
					t.Fatalf("%s %s on %s: the response carries the held share", stage, kc.id, n.id)
				}
				var got DescribeResponse
				if err := json.Unmarshal(r.body, &got); err != nil {
					t.Fatal(err)
				}
				created := kc.created[i]
				head, _ := n.audit.Head()
				if got.NodeID != n.id || got.KeyID != kc.id || got.Curve != kc.curve || got.PublicKey != created.PublicKey || got.Address != created.Address || got.DID != created.DID ||
					got.Owner != testOwner || got.Account != kc.account || got.Epoch != epoch || strings.Join(got.Participants, ",") != strings.Join(c.ids, ",") || got.AuditSequence == 0 || got.AuditSequence != head {
					t.Fatalf("%s %s on %s: described %+v, created %+v, audit head %d", stage, kc.id, n.id, got, created, head)
				}
			}
		}
	}
	check("created", 0)

	for i, kc := range cases {
		wg.Add(1)
		go func(i int, kc *keyCase) {
			defer wg.Done()
			results[i] = c.callAll(t, c.nodes, PathRefresh, func(*testNode) any {
				return RefreshRequest{SessionID: "refresh-" + kc.id, KeyID: kc.id}
			}, "")
		}(i, kc)
	}
	wg.Wait()
	for i, kc := range cases {
		for _, r := range decodeOK[KeyResponse](t, "refresh "+kc.id, results[i]) {
			if r.Epoch != 1 || r.PublicKey != kc.created[0].PublicKey {
				t.Fatalf("refresh %s: %+v", kc.id, r)
			}
		}
	}
	check("refreshed", 1)

	before, _ := c.nodes[0].audit.Head()
	gateway := c.callAs(t, c.client, c.nodes[0], PathDescribe, DescribeRequest{SessionID: "describe-by-gateway", KeyID: "generated-evm"}, "")
	if gateway.status != http.StatusForbidden {
		t.Fatalf("gateway identity on describe: status %d", gateway.status)
	}
	expectError(t, "gateway identity on describe", gateway, CodeOperatorRequired)
	if after, _ := c.nodes[0].audit.Head(); after != before+1 {
		t.Fatalf("gateway refusal on describe was not audited: head %d then %d", before, after)
	}
	expectError(t, "unheld key", c.call(t, c.nodes[0], PathDescribe, DescribeRequest{SessionID: "describe-unheld", KeyID: "unheld-key"}, ""), CodeKeyNotFound)
	expectError(t, "describe without a key id", c.call(t, c.nodes[0], PathDescribe, DescribeRequest{SessionID: "describe-empty"}, ""), CodeSessionBadRequest)
}
