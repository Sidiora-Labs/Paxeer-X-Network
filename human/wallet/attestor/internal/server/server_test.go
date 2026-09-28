package server

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/getamis/alice/crypto/birkhoffinterpolation"
	pt "github.com/getamis/alice/crypto/ecpointgrouplaw"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
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
		"keys.generate.request": GenerateRequest{SessionID: "session-generate", KeyID: "key-ed", Curve: "ed25519", Owner: "user-0001", Account: "0x1111111111111111111111111111111111111111"},
		"keys.import.request":   ImportRequest{SessionID: "session-import", KeyID: "key-evm", Owner: "user-0001", Share: EncodeBundle(bundles[0])},
		"keys.refresh.request":  RefreshRequest{SessionID: "session-refresh", KeyID: "key-evm"},
		"keys.addshare.request": AddShareRequest{SessionID: "session-addshare", KeyID: "key-evm", Curve: "secp256k1", PublicKey: "02" + strings.Repeat("11", 32), Owner: "user-0001", NewParticipantID: "node-6", Quorum: []string{"node-1", "node-3", "node-4", "node-5"}},
		"keys.response":         KeyResponse{NodeID: "node-1", KeyID: "key-evm", Curve: "secp256k1", PublicKey: "04" + strings.Repeat("22", 64), Address: "0x2222222222222222222222222222222222222222", Epoch: 1, Participants: ids, Refreshed: true, AuditSequence: 3},
		"sign.request":          SignRequest{SessionID: "session-sign", KeyID: "key-evm", Kind: KindEVMTransaction, Signers: []string{"node-1", "node-3", "node-5"}, Transaction: "02f86c"},
		"sign.request.lx_grant": SignRequest{SessionID: "session-grant", KeyID: "key-ed", Kind: KindLXGrant, Signers: []string{"node-1", "node-2", "node-3"}, Grant: &GrantJSON{
			From: strings.Repeat("81", 32), Recipient: strings.Repeat("1e", 32), Asset: strings.Repeat("0a", 32), PerDrawMaximum: "1000", Allowance: "50000",
			Expiration: 1900000000, PurposeHash: strings.Repeat("64", 32),
		}},
		"sign.request.eth_sign_digest": SignRequest{SessionID: "session-authorization", KeyID: "key-evm", Kind: KindEthSignDigest, Signers: []string{"node-1", "node-3", "node-5"}, Digest: "0x" + strings.Repeat("66", 32), Construction: &ConstructionJSON{
			Kind: "eip7702_authorization", ChainID: "125", Address: "0x2222222222222222222222222222222222222222", Nonce: "3",
		}},
		"sign.response":   SignResponse{NodeID: "node-1", KeyID: "key-evm", Kind: KindEVMTransaction, SignedBytes: strings.Repeat("33", 32), Signature: strings.Repeat("44", 64) + "01", RecoveryID: &recovery, AuditSequence: 9},
		"error.policy":    errorBody{Error: policyError("value_cap", "amount exceeds the per-transaction cap")},
		"error.token":     errorBody{Error: newError(CodeTokenMissing, "a bearer token or agent signature is required")},
		"health.response": health.Report{NodeID: "node-1", Region: "region-a", ShareCount: 2, RefreshEpoch: 1, AuditSequence: 9, AuditHead: strings.Repeat("55", 32), Peers: map[string]health.PeerState{"node-2": {Reachable: true, RTT: 1000}}, ReachablePeers: 1},
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
	for _, path := range []string{PathGenerate, PathImport, PathRefresh, PathAddShare, PathSign, PathHealth} {
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

	for _, path := range []string{PathImport, PathRefresh, PathAddShare} {
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
