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
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/config"
	"github.com/sidiora-labs/paxeer-network/human/wallet/attestor/internal/health"
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
