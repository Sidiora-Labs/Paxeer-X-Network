package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
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
	"testing"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/audit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/auth/jwt"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/lxwire"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/policy/lx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/server"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/human/wallet/attestor/internal/transport"
)

const (
	testChainID  = 125
	testSubject  = "loadtest-user-0001"
	signPolicy   = `{"version":1,"defaults":{"chain_id":125,"kinds":["evm_tx","lx_activity"],"caps":{"native":{"per_transaction":"1000000000000000000","daily":"10000000000000000000"}},"rate_per_minute":100000}}`
	kernelPolicy = `{"version":1,"defaults":{"modules":{"programs":[5]},"caps":{"native":{"per_operation":"1000","daily":"1000000000"}}}}`
)

type authority struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	path string
}

func writePEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func certificate(t *testing.T, dir, name string, parent *authority) (*authority, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, big.NewInt(1<<62))
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	signer, signerKey := tmpl, key
	if parent == nil {
		tmpl.KeyUsage |= x509.KeyUsageCertSign
		tmpl.BasicConstraintsValid, tmpl.IsCA = true, true
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, name+".pem"), filepath.Join(dir, name+"-key.pem")
	writePEM(t, certPath, "CERTIFICATE", der)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)
	return &authority{cert: cert, key: key, path: certPath}, certPath, keyPath
}

type issuer struct {
	srv *httptest.Server
	key *rsa.PrivateKey
	url string
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func newIssuer(t *testing.T) *issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	is := &issuer{key: key}
	is.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/v1/.well-known/jwks.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "loadtest-key", "alg": "RS256", "use": "sig",
			"n": b64(key.PublicKey.N.Bytes()), "e": b64(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	}))
	t.Cleanup(is.srv.Close)
	is.url = is.srv.URL + "/auth/v1"
	return is
}

func (is *issuer) mint(t *testing.T, signer *rsa.PrivateKey, subject string) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "loadtest-key", "typ": "JWT"})
	now := time.Now().Unix()
	claims, _ := json.Marshal(map[string]any{"sub": subject, "iss": is.url, "aud": "authenticated", "iat": now, "exp": now + 1800})
	signing := b64(header) + "." + b64(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, signer, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + b64(sig)
}

type network struct {
	endpoints  []string
	caFile     string
	clientCert string
	clientKey  string
	issuer     *issuer
}

func startNetwork(t *testing.T, n int) *network {
	t.Helper()
	dir := t.TempDir()
	gatewayDir, operatorDir := filepath.Join(dir, "gateway-ca"), filepath.Join(dir, "operator-ca")
	for _, d := range []string{gatewayDir, operatorDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gateway, _, _ := certificate(t, gatewayDir, "ca", nil)
	operator, _, _ := certificate(t, operatorDir, "ca", nil)
	_, clientCert, clientKey := certificate(t, gatewayDir, "wallet-gateway", gateway)
	nw := &network{caFile: gateway.path, clientCert: clientCert, clientKey: clientKey, issuer: newIssuer(t)}

	policyFile, kernelFile := filepath.Join(dir, "policy.json"), filepath.Join(dir, "kernel-policy.json")
	if err := os.WriteFile(policyFile, []byte(signPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kernelFile, []byte(kernelPolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the load test reads no chain state", http.StatusServiceUnavailable)
	}))
	t.Cleanup(rpc.Close)
	clients, err := server.LoadClientAuthorities(gateway.path, operator.path)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := lxwire.NewRegistry(lx.OpAssetTransfer, lx.OpProgramCall)
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, n)
	peers := make([]transport.Peer, n)
	peerListeners, apiListeners := make([]net.Listener, n), make([]net.Listener, n)
	certs, keys := make([]string, n), make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("node-%d", i+1)
		if peerListeners[i], err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		if apiListeners[i], err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			t.Fatal(err)
		}
		var nodeCert *authority
		nodeCert, certs[i], keys[i] = certificate(t, gatewayDir, ids[i], gateway)
		pin := transport.SPKIHash(nodeCert.cert)
		peers[i] = transport.Peer{ID: ids[i], Address: peerListeners[i].Addr().String(), SPKISHA256: hex.EncodeToString(pin[:])}
		nw.endpoints = append(nw.endpoints, ids[i]+"="+apiListeners[i].Addr().String())
	}
	for i, id := range ids {
		nodeKey := sha256.Sum256([]byte("load test store key " + id))
		dataDir := filepath.Join(dir, id)
		st, err := store.Open(dataDir, nodeKey[:])
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		lg, err := audit.Open(filepath.Join(dataDir, "audit"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = lg.Close() })
		doc, err := policy.LoadFile(policyFile)
		if err != nil {
			t.Fatal(err)
		}
		engine := policy.New(doc)
		ledger, err := policy.NewSpendLedger(st, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		kdoc, err := lx.LoadFile(kernelFile)
		if err != nil {
			t.Fatal(err)
		}
		chain, err := lx.NewChain(rpc.URL, &http.Client{Timeout: lx.DefaultRPCWait})
		if err != nil {
			t.Fatal(err)
		}
		kernel, err := lx.New(engine, kdoc, chain)
		if err != nil {
			t.Fatal(err)
		}
		tokens, err := jwt.NewTokenVerifier(jwt.Config{
			JWKSURL: nw.issuer.url + "/.well-known/jwks.json", Issuer: nw.issuer.url, Audience: "authenticated",
			HTTPClient: &http.Client{Timeout: 10 * time.Second}, MaxAge: time.Hour, Replay: st,
		})
		if err != nil {
			t.Fatal(err)
		}
		tr, err := transport.New(transport.Config{
			SelfID: id, ListenAddr: peers[i].Address, CertFile: certs[i], KeyFile: keys[i],
			CAFile: gateway.path, Peers: peers, OperatorCAFile: operator.path,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tr.Close() })
		probe := map[string]string{}
		for j, p := range peers {
			if j != i {
				probe[p.ID] = p.Address
			}
		}
		srv, err := server.New(server.Options{
			NodeID: id, Region: fmt.Sprintf("region-%d", i+1), ChainID: testChainID, Participants: ids,
			Store: st, Audit: lg, Transport: tr, Policy: engine, Kernel: kernel, Ledger: ledger,
			Clients: clients, Tokens: tokens, Activities: registry, PeerProbe: server.TCPPeerProbe(probe),
		})
		if err != nil {
			t.Fatal(err)
		}
		tlsCfg, err := server.APITLSConfig(certs[i], keys[i], clients)
		if err != nil {
			t.Fatal(err)
		}
		go func(l net.Listener) { _ = tr.Serve(l) }(peerListeners[i])
		api := srv.Serve(apiListeners[i], tlsCfg)
		t.Cleanup(func() { _ = api.Close() })
	}
	return nw
}

func (n *network) env(token string) map[string]string {
	return map[string]string{
		EnvEndpoints:      strings.Join(n.endpoints, ","),
		EnvClientCertFile: n.clientCert,
		EnvClientKeyFile:  n.clientKey,
		EnvCAFile:         n.caFile,
		EnvChainID:        "125",
		EnvToken:          token,
	}
}

func runTool(t *testing.T, env map[string]string, args ...string) (Summary, []byte) {
	t.Helper()
	var out, errs bytes.Buffer
	summary, err := run(context.Background(), args, func(k string) string { return env[k] }, &out, &errs)
	if err != nil {
		t.Fatalf("loadtest: %v (stderr %s)", err, errs.String())
	}
	return summary, out.Bytes()
}

func checkShape(t *testing.T, label string, s Summary, raw []byte, concurrency int, duration time.Duration) {
	t.Helper()
	var decoded Summary
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("%s: summary is not JSON: %v\n%s", label, err, raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"chain_id", "endpoints", "quorum", "concurrency", "duration_seconds", "elapsed_seconds", "keys", "sessions", "successes", "refusals", "throughput_per_second", "latency_ms", "curves"} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("%s: summary lacks %q: %s", label, name, raw)
		}
	}
	if decoded.Sessions != s.Sessions || decoded.Successes != s.Successes || len(decoded.Refusals) != len(s.Refusals) {
		t.Fatalf("%s: printed summary %+v differs from the returned one %+v", label, decoded, s)
	}
	if s.ChainID != testChainID || s.Endpoints != 5 || s.Quorum != 3 || s.Concurrency != concurrency || s.DurationSeconds != duration.Seconds() {
		t.Fatalf("%s: summary header %+v", label, s)
	}
	if s.ElapsedSeconds < duration.Seconds() {
		t.Fatalf("%s: elapsed %.3fs is shorter than the %s bound", label, s.ElapsedSeconds, duration)
	}
	if len(s.Keys) != 2 || s.Keys[0].Curve != store.CurveSecp256k1 || s.Keys[1].Curve != store.CurveEd25519 {
		t.Fatalf("%s: keys %+v", label, s.Keys)
	}
	if s.Keys[0].Address == "" || s.Keys[0].PublicKey == "" || s.Keys[1].DID == "" || len(s.Keys[1].PublicKey) != 64 || s.Keys[0].GenerateMillis <= 0 || s.Keys[1].GenerateMillis <= 0 {
		t.Fatalf("%s: generated keys %+v", label, s.Keys)
	}
	refused := 0
	for class, count := range s.Refusals {
		if class == "" || count <= 0 {
			t.Fatalf("%s: refusal class %q counted %d", label, class, count)
		}
		refused += count
	}
	if s.Sessions == 0 || s.Sessions != s.Successes+refused {
		t.Fatalf("%s: %d sessions, %d successes, %d refusals", label, s.Sessions, s.Successes, refused)
	}
	if len(s.Curves) != 2 {
		t.Fatalf("%s: curves %v", label, s.Curves)
	}
	sessions, successes := 0, 0
	for _, curve := range []string{store.CurveSecp256k1, store.CurveEd25519} {
		c := s.Curves[curve]
		if c == nil || c.Sessions == 0 {
			t.Fatalf("%s: curve %s ran no sessions: %+v", label, curve, c)
		}
		curveRefused := 0
		for _, count := range c.Refusals {
			curveRefused += count
		}
		if c.Sessions != c.Successes+curveRefused {
			t.Fatalf("%s: curve %s: %d sessions, %d successes, %d refusals", label, curve, c.Sessions, c.Successes, curveRefused)
		}
		sessions += c.Sessions
		successes += c.Successes
		checkLatency(t, label+" "+curve, c.Successes, c.LatencyMillis)
	}
	if sessions != s.Sessions || successes != s.Successes {
		t.Fatalf("%s: curve totals %d/%d differ from %d/%d", label, sessions, successes, s.Sessions, s.Successes)
	}
	checkLatency(t, label, s.Successes, s.LatencyMillis)
	if want := float64(s.Successes) / s.ElapsedSeconds; s.ThroughputPerSecond != want {
		t.Fatalf("%s: throughput %f, want %f", label, s.ThroughputPerSecond, want)
	}
}

func checkLatency(t *testing.T, label string, successes int, l Latency) {
	t.Helper()
	if successes == 0 {
		if l != (Latency{}) {
			t.Fatalf("%s: latency %+v without a success", label, l)
		}
		return
	}
	if l.P50 <= 0 || l.P50 > l.P90 || l.P90 > l.P99 || l.P99 > l.Max {
		t.Fatalf("%s: latency percentiles %+v", label, l)
	}
}

func TestLoadTestAgainstFiveHardenedDaemons(t *testing.T) {
	nw := startNetwork(t, 5)
	token := nw.issuer.mint(t, nw.issuer.key, testSubject)

	const concurrency = 4
	duration := 15 * time.Second
	summary, raw := runTool(t, nw.env(token), "-duration", duration.String(), "-concurrency", "4")
	checkShape(t, "owner token", summary, raw, concurrency, duration)
	if len(summary.Refusals) != 0 {
		t.Fatalf("owner token: refusals %v\n%s", summary.Refusals, raw)
	}
	for _, curve := range []string{store.CurveSecp256k1, store.CurveEd25519} {
		if c := summary.Curves[curve]; c.Successes == 0 || c.ThroughputPerSecond <= 0 {
			t.Fatalf("owner token: curve %s %+v", curve, c)
		}
	}
	if summary.Successes < concurrency {
		t.Fatalf("owner token: %d successes at concurrency %d", summary.Successes, concurrency)
	}

	foreign, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	forged := nw.issuer.mint(t, foreign, testSubject)
	env := nw.env(forged)
	env[EnvDuration], env[EnvConcurrency] = "2s", "2"
	refused, raw := runTool(t, env)
	checkShape(t, "forged token", refused, raw, 2, 2*time.Second)
	if refused.Successes != 0 || len(refused.Refusals) != 1 || refused.Refusals[server.CodeTokenInvalid] != refused.Sessions {
		t.Fatalf("forged token: %d successes, refusals %v over %d sessions", refused.Successes, refused.Refusals, refused.Sessions)
	}
	for _, curve := range []string{store.CurveSecp256k1, store.CurveEd25519} {
		if c := refused.Curves[curve]; c.Refusals[server.CodeTokenInvalid] != c.Sessions || c.ThroughputPerSecond != 0 {
			t.Fatalf("forged token: curve %s %+v", curve, c)
		}
	}
}

func TestLoadTestRequiresEveryConnectionSetting(t *testing.T) {
	full := map[string]string{
		EnvEndpoints: "node-1=127.0.0.1:1,node-2=127.0.0.1:2,node-3=127.0.0.1:3", EnvClientCertFile: "client.pem",
		EnvClientKeyFile: "client-key.pem", EnvCAFile: "ca.pem", EnvChainID: "125",
		EnvToken:    b64([]byte(`{"alg":"RS256"}`)) + "." + b64([]byte(`{"sub":"someone"}`)) + ".c2ln",
		EnvDuration: "1s", EnvConcurrency: "1",
	}
	for _, name := range []string{EnvEndpoints, EnvClientCertFile, EnvClientKeyFile, EnvCAFile, EnvChainID, EnvToken, EnvDuration, EnvConcurrency} {
		env := map[string]string{}
		for k, v := range full {
			if k != name {
				env[k] = v
			}
		}
		_, err := run(context.Background(), nil, func(k string) string { return env[k] }, io.Discard, io.Discard)
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("without %s: %v", name, err)
		}
	}
	cfg, err := load([]string{"-chain-id", "7", "-concurrency", "3"}, func(k string) string { return full[k] }, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.chainID != 7 || cfg.concurrency != 3 || cfg.subject != "someone" || len(cfg.endpoints) != 3 || cfg.sessionTimeout != 2*time.Minute || cfg.keygenTimeout != 10*time.Minute {
		t.Fatalf("flags over environment: %+v", cfg)
	}
	for name, value := range map[string]string{EnvEndpoints: "node-1=127.0.0.1:1,node-2=127.0.0.1:2", EnvChainID: "0", EnvToken: "not-a-jwt", EnvDuration: "-1s", EnvConcurrency: "0"} {
		env := map[string]string{}
		for k, v := range full {
			env[k] = v
		}
		env[name] = value
		if _, err := load(nil, func(k string) string { return env[k] }, io.Discard); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("%s=%q: %v", name, value, err)
		}
	}
}
