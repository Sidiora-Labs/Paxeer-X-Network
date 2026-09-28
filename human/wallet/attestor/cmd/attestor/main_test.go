package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/audit"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/backup"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/store"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/transport"
)

type pki struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func writePEM(t *testing.T, path, kind string, der []byte) {
	t.Helper()
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeCert(t *testing.T, dir, name string, parent *pki, isCA bool) (*pki, string, string) {
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
	if isCA {
		tmpl.KeyUsage |= x509.KeyUsageCertSign
		tmpl.BasicConstraintsValid, tmpl.IsCA = true, true
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	signer, signerKey := tmpl, key
	if parent != nil {
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
	certPath := filepath.Join(dir, name+".pem")
	keyPath := filepath.Join(dir, name+"-key.pem")
	writePEM(t, certPath, "CERTIFICATE", der)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	writePEM(t, keyPath, "EC PRIVATE KEY", keyDER)
	return &pki{cert: cert, key: key}, certPath, keyPath
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

func TestRunServesHealthOverMutualTLS(t *testing.T) {
	dir := t.TempDir()
	ca, caPath, _ := makeCert(t, dir, "ca", nil, true)
	_, nodeCert, nodeKey := makeCert(t, dir, "node-1", ca, false)
	peer, _, _ := makeCert(t, dir, "node-2", ca, false)
	_, clientCert, clientKey := makeCert(t, dir, "client", ca, false)
	keyFile := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(make([]byte, 31))+"01"), 0o600); err != nil {
		t.Fatal(err)
	}
	pin := transport.SPKIHash(peer.cert)
	peerPin := hex.EncodeToString(pin[:])
	env := map[string]string{
		config.EnvNodeID:         "node-1",
		config.EnvRegion:         "region-a",
		config.EnvListenAddr:     "127.0.0.1:0",
		config.EnvPeerListenAddr: freeAddr(t),
		config.EnvPeers:          "node-2=" + freeAddr(t),
		config.EnvPeerPins:       "node-2=" + peerPin,
		config.EnvNodeKeyFile:    keyFile,
		config.EnvDataDir:        filepath.Join(dir, "data"),
		config.EnvChainID:        "125",
		config.EnvTLSCertFile:    nodeCert,
		config.EnvTLSKeyFile:     nodeKey,
		config.EnvTLSCAFile:      caPath,
		config.EnvOperatorCAFile: caPath,
		config.EnvActivityTypes:  "0x10005",
	}
	ctx, cancel := context.WithCancel(context.Background())
	readyCh := make(chan listening, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, func(l listening) { readyCh <- l }) }()
	var addrs listening
	select {
	case addrs = <-readyCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not start")
	}

	pair, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: roots,
	}}}
	resp, err := client.Get("https://" + addrs.API + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var report health.Report
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("health body %s: %v", body, err)
	}
	if report.NodeID != "node-1" || report.Region != "region-a" || report.ShareCount != 0 {
		t.Fatalf("health report %+v", report)
	}
	if resp.StatusCode != http.StatusServiceUnavailable || report.Ready {
		t.Fatalf("a node with no reachable peer reported ready: %d %s", resp.StatusCode, body)
	}

	anonymous := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, RootCAs: roots,
	}}}
	if resp, err := anonymous.Get("https://" + addrs.API + "/health"); err == nil {
		resp.Body.Close()
		t.Fatalf("health answered without a client certificate: %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v after termination", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestRunRefusesPeerWithoutPin(t *testing.T) {
	dir := t.TempDir()
	ca, caPath, _ := makeCert(t, dir, "ca", nil, true)
	_, nodeCert, nodeKey := makeCert(t, dir, "node-1", ca, false)
	keyFile := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(make([]byte, 31))+"02"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		config.EnvNodeID: "node-1", config.EnvListenAddr: "127.0.0.1:0", config.EnvPeerListenAddr: freeAddr(t),
		config.EnvPeers: "node-2=" + freeAddr(t), config.EnvNodeKeyFile: keyFile, config.EnvDataDir: filepath.Join(dir, "data"),
		config.EnvChainID: "125", config.EnvTLSCertFile: nodeCert, config.EnvTLSKeyFile: nodeKey,
		config.EnvTLSCAFile: caPath, config.EnvOperatorCAFile: caPath,
	}
	if err := run(context.Background(), func(k string) string { return env[k] }, nil); err == nil {
		t.Fatal("run accepted a peer without a pin")
	}
}

func postStatus(t *testing.T, client *http.Client, url string) (int, string) {
	t.Helper()
	resp, err := client.Post(url, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &body); err != nil || body.Error == nil {
		t.Fatalf("%s: status %d body %s", url, resp.StatusCode, raw)
	}
	return resp.StatusCode, body.Error.Code
}

func TestRunSeparatesOperatorAndGatewayAuthority(t *testing.T) {
	dir := t.TempDir()
	gatewayDir, operatorDir := filepath.Join(dir, "gateway"), filepath.Join(dir, "operator")
	for _, d := range []string{gatewayDir, operatorDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	gateway, gatewayCA, _ := makeCert(t, gatewayDir, "ca", nil, true)
	operator, operatorCA, _ := makeCert(t, operatorDir, "ca", nil, true)
	_, nodeCert, nodeKey := makeCert(t, gatewayDir, "node-1", gateway, false)
	peer, _, _ := makeCert(t, gatewayDir, "node-2", gateway, false)
	_, gatewayCert, gatewayKey := makeCert(t, gatewayDir, "gateway-client", gateway, false)
	_, operatorCert, operatorKey := makeCert(t, operatorDir, "operator-client", operator, false)
	keyFile := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(make([]byte, 31))+"03"), 0o600); err != nil {
		t.Fatal(err)
	}
	kernelFile := filepath.Join(dir, "kernel.json")
	if err := os.WriteFile(kernelFile, []byte(`{"version":1,"defaults":{"modules":{"asset":[5]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	agentKey := make([]byte, 32)
	agentKey[0] = 1
	agentsFile := filepath.Join(dir, "agents.json")
	if err := os.WriteFile(agentsFile, []byte(`[{"public_key":"`+hex.EncodeToString(agentKey)+`","key_ids":["key-1"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	pin := transport.SPKIHash(peer.cert)
	env := map[string]string{
		config.EnvNodeID:         "node-1",
		config.EnvListenAddr:     "127.0.0.1:0",
		config.EnvPeerListenAddr: freeAddr(t),
		config.EnvPeers:          "node-2=" + freeAddr(t),
		config.EnvPeerPins:       "node-2=" + hex.EncodeToString(pin[:]),
		config.EnvNodeKeyFile:    keyFile,
		config.EnvDataDir:        filepath.Join(dir, "data"),
		config.EnvChainID:        "125",
		config.EnvTLSCertFile:    nodeCert,
		config.EnvTLSKeyFile:     nodeKey,
		config.EnvTLSCAFile:      gatewayCA,
		config.EnvOperatorCAFile: operatorCA,
		config.EnvActivityTypes:  "0x10005",
		config.EnvKernelPolicy:   kernelFile,
		config.EnvRPCURL:         "http://" + freeAddr(t),
		config.EnvAgentsFile:     agentsFile,
		config.EnvAgentMaxExpiry: "2m",
		config.EnvJWTMaxAge:      "30m",
	}
	ctx, cancel := context.WithCancel(context.Background())
	readyCh := make(chan listening, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, func(l listening) { readyCh <- l }) }()
	var addrs listening
	select {
	case addrs = <-readyCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not start")
	}
	roots := x509.NewCertPool()
	roots.AddCert(gateway.cert)
	clientFor := func(certPath, keyPath string) *http.Client {
		pair, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: roots,
		}}}
	}
	gatewayClient, operatorClient := clientFor(gatewayCert, gatewayKey), clientFor(operatorCert, operatorKey)
	base := "https://" + addrs.API
	for _, path := range []string{"/v1/sign", "/v1/keys/generate"} {
		if status, code := postStatus(t, operatorClient, base+path); status != http.StatusForbidden || code != "operator_required" {
			t.Fatalf("operator identity on %s: %d %s", path, status, code)
		}
		if status, code := postStatus(t, gatewayClient, base+path); status == http.StatusForbidden || code == "operator_required" {
			t.Fatalf("gateway identity refused on %s: %d %s", path, status, code)
		}
	}
	for _, path := range []string{"/v1/keys/import", "/v1/keys/refresh", "/v1/keys/addshare"} {
		if status, code := postStatus(t, gatewayClient, base+path); status != http.StatusForbidden || code != "operator_required" {
			t.Fatalf("gateway identity on %s: %d %s", path, status, code)
		}
		if status, code := postStatus(t, operatorClient, base+path); status == http.StatusForbidden || code == "operator_required" {
			t.Fatalf("operator identity refused on %s: %d %s", path, status, code)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v after termination", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestRunRefusesKernelPolicyWithoutChainReader(t *testing.T) {
	dir := t.TempDir()
	ca, caPath, _ := makeCert(t, dir, "ca", nil, true)
	_, nodeCert, nodeKey := makeCert(t, dir, "node-1", ca, false)
	keyFile := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(make([]byte, 31))+"04"), 0o600); err != nil {
		t.Fatal(err)
	}
	kernelFile := filepath.Join(dir, "kernel.json")
	if err := os.WriteFile(kernelFile, []byte(`{"version":1,"defaults":{"modules":{"asset":[5]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		config.EnvNodeID: "node-1", config.EnvListenAddr: "127.0.0.1:0", config.EnvPeerListenAddr: freeAddr(t),
		config.EnvNodeKeyFile: keyFile, config.EnvDataDir: filepath.Join(dir, "data"),
		config.EnvChainID: "125", config.EnvTLSCertFile: nodeCert, config.EnvTLSKeyFile: nodeKey,
		config.EnvTLSCAFile: caPath, config.EnvOperatorCAFile: caPath, config.EnvKernelPolicy: kernelFile,
	}
	if err := run(context.Background(), func(k string) string { return env[k] }, nil); err == nil {
		t.Fatal("run accepted a kernel policy without a chain reader")
	}
	env[config.EnvRPCURL] = "http://" + freeAddr(t)
	env[config.EnvAgentsFile] = filepath.Join(dir, "absent.json")
	if err := run(context.Background(), func(k string) string { return env[k] }, nil); err == nil {
		t.Fatal("run accepted a missing agent principal file")
	}
}

func TestRunWritesSnapshotsOnTheInterval(t *testing.T) {
	dir := t.TempDir()
	ca, caPath, _ := makeCert(t, dir, "ca", nil, true)
	_, nodeCert, nodeKey := makeCert(t, dir, "node-1", ca, false)
	peer, _, _ := makeCert(t, dir, "node-2", ca, false)
	_, clientCert, clientKey := makeCert(t, dir, "client", ca, false)
	keyFile := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(make([]byte, 31))+"03"), 0o600); err != nil {
		t.Fatal(err)
	}
	backupKeyFile := filepath.Join(dir, "backup.key")
	if err := os.WriteFile(backupKeyFile, []byte(hex.EncodeToString(make([]byte, 31))+"04"), 0o600); err != nil {
		t.Fatal(err)
	}
	pin := transport.SPKIHash(peer.cert)
	backupDir := filepath.Join(dir, "backup")
	env := map[string]string{
		config.EnvNodeID:           "node-1",
		config.EnvRegion:           "region-a",
		config.EnvListenAddr:       "127.0.0.1:0",
		config.EnvPeerListenAddr:   freeAddr(t),
		config.EnvPeers:            "node-2=" + freeAddr(t),
		config.EnvPeerPins:         "node-2=" + hex.EncodeToString(pin[:]),
		config.EnvNodeKeyFile:      keyFile,
		config.EnvDataDir:          filepath.Join(dir, "data"),
		config.EnvChainID:          "125",
		config.EnvTLSCertFile:      nodeCert,
		config.EnvTLSKeyFile:       nodeKey,
		config.EnvTLSCAFile:        caPath,
		config.EnvOperatorCAFile:   caPath,
		config.EnvBackupDir:        backupDir,
		config.EnvBackupKeyFile:    backupKeyFile,
		config.EnvSnapshotInterval: "1s",
		config.EnvSnapshotRetain:   "2",
	}
	ctx, cancel := context.WithCancel(context.Background())
	readyCh := make(chan listening, 1)
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }, func(l listening) { readyCh <- l }) }()
	var addrs listening
	select {
	case addrs = <-readyCh:
	case err := <-done:
		t.Fatalf("run exited early: %v", err)
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not start")
	}

	deadline := time.Now().Add(30 * time.Second)
	var names []string
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(backupDir)
		if err != nil {
			t.Fatal(err)
		}
		names = names[:0]
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if len(names) == 2 {
			if _, seq, ok := backup.ParseName(names[1]); ok && seq >= 3 {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(names) != 2 {
		t.Fatalf("snapshot directory holds %v, want the two newest interval snapshots", names)
	}
	_, first, ok1 := backup.ParseName(names[0])
	_, second, ok2 := backup.ParseName(names[1])
	if !ok1 || !ok2 || second < 3 || second != first+1 {
		t.Fatalf("snapshot directory holds %v, want two consecutive snapshots after at least three writes", names)
	}

	pair, err := tls.LoadX509KeyPair(clientCert, clientKey)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.cert)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, RootCAs: roots,
	}}}
	resp, err := client.Get("https://" + addrs.API + "/health")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var report health.Report
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatalf("health body %s: %v", body, err)
	}
	if report.Snapshot == nil || report.Snapshot.LastWrittenAgeSeconds == nil || report.Snapshot.LastError != "" || report.Snapshot.Failures != 0 {
		t.Fatalf("health snapshot section %s", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run returned %v after termination", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("daemon did not stop")
	}
}

func TestRestoreSubcommand(t *testing.T) {
	dir := t.TempDir()
	nodeKey := make([]byte, store.KeySize)
	nodeKey[31] = 5
	backupKey := make([]byte, store.KeySize)
	backupKey[31] = 6
	keyFile := filepath.Join(dir, "store.key")
	if err := os.WriteFile(keyFile, []byte(hex.EncodeToString(nodeKey)), 0o600); err != nil {
		t.Fatal(err)
	}
	backupKeyFile := filepath.Join(dir, "backup.key")
	if err := os.WriteFile(backupKeyFile, []byte(hex.EncodeToString(backupKey)), 0o600); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "source")
	st, err := store.Open(srcDir, nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	pub := make([]byte, 32)
	pub[0] = 9
	if err := st.Put(store.ShareRecord{KeyID: "lx-key", Curve: store.CurveEd25519, PublicKey: pub, Participants: []string{"node-1", "node-2", "node-3"}}, []byte("share material")); err != nil {
		t.Fatal(err)
	}
	lg, err := audit.Open(filepath.Join(srcDir, "audit"))
	if err != nil {
		t.Fatal(err)
	}
	w, err := backup.New(backup.Config{NodeID: "node-1", Dir: filepath.Join(dir, "backup"), BackupKey: backupKey, Retain: 5, Store: st, Audit: lg})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := w.WriteSnapshot("test")
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	lg.Close()
	digest := hex.EncodeToString(snap.SHA256[:])

	env := func(nodeID, dataDir string) func(string) string {
		m := map[string]string{
			config.EnvNodeID: nodeID, config.EnvListenAddr: "127.0.0.1:0", config.EnvPeerListenAddr: "127.0.0.1:0",
			config.EnvNodeKeyFile: keyFile, config.EnvDataDir: dataDir, config.EnvChainID: "125",
			config.EnvBackupKeyFile: backupKeyFile,
		}
		return func(k string) string { return m[k] }
	}

	var out strings.Builder
	wrong := strings.Repeat("00", 32)
	if err := restore([]string{"-snapshot", snap.Path, "-sha256", wrong}, env("node-1", filepath.Join(dir, "r1")), &out); !errors.Is(err, backup.ErrDigestMismatch) {
		t.Fatalf("wrong digest: %v", err)
	}
	if err := restore([]string{"-snapshot", snap.Path, "-sha256", digest}, env("node-2", filepath.Join(dir, "r2")), &out); !errors.Is(err, store.ErrSnapshotNode) {
		t.Fatalf("foreign node: %v", err)
	}
	used := filepath.Join(dir, "used")
	if err := os.MkdirAll(used, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(used, "leftover"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restore([]string{"-snapshot", snap.Path, "-sha256", digest}, env("node-1", used), &out); !errors.Is(err, store.ErrDataDirInUse) {
		t.Fatalf("non-empty data directory: %v", err)
	}
	if err := restore([]string{"-snapshot", snap.Path}, env("node-1", filepath.Join(dir, "r3")), &out); err == nil {
		t.Fatal("restore ran without an expected digest")
	}

	target := filepath.Join(dir, "restored")
	if err := restore([]string{"-snapshot", snap.Path, "-sha256", digest}, env("node-1", filepath.Join(dir, "ignored")), &out); err != nil {
		t.Fatalf("restore into the configured data directory: %v", err)
	}
	if err := restore([]string{"-snapshot", snap.Path, "-sha256", digest, "-data-dir", target}, env("node-1", filepath.Join(dir, "ignored-2")), &out); err != nil {
		t.Fatalf("restore into -data-dir: %v", err)
	}
	if !strings.Contains(out.String(), snap.Name) || !strings.Contains(out.String(), "1 shares") {
		t.Fatalf("restore output %q", out.String())
	}
	restored, err := store.Open(target, nodeKey)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	rec, err := restored.Get("lx-key")
	if err != nil || rec.Curve != store.CurveEd25519 {
		t.Fatalf("restored record %+v: %v", rec, err)
	}
	var share []byte
	if err := restored.WithShare("lx-key", func(p []byte) error { share = append(share, p...); return nil }); err != nil || string(share) != "share material" {
		t.Fatalf("restored share: %v", err)
	}
}
