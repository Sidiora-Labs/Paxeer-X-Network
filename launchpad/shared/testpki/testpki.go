// Package testpki issues a throwaway private PKI for tests of the private
// mTLS channels (gateway identity listener, service upstreams): an ECDSA P-256
// CA, leaf certificates usable as TLS server and client (the shape of
// scratchpad prod/mtls-recipe/gen.sh: serverAuth+clientAuth, one DNS SAN) and
// ed25519 request-assertion keys. Everything is written as PEM files under a
// test-owned directory. It is imported only by tests.
package testpki

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PKI is a private CA writing its files into Dir.
type PKI struct {
	Dir    string
	CAFile string
	ca     *x509.Certificate
	caKey  *ecdsa.PrivateKey
	t      testing.TB
}

// Leaf is an issued certificate and its key.
type Leaf struct {
	CertFile, KeyFile string
	CertPEM, KeyPEM   []byte
	// SHA256 is the lowercase hex sha256 of the certificate DER (the pin).
	SHA256 string
	Pair   tls.Certificate
}

// AssertionKey is an ed25519 request-assertion key.
type AssertionKey struct {
	Private ed25519.PrivateKey
	// StdBase64 is the 64-byte private key in standard base64 (the
	// IDENTITY_ASSERTION_SIGNING_KEY form); PublicRawURL is the public key in
	// unpadded base64url (the gateway identityPeers[].publicKey form).
	StdBase64, PublicRawURL string
}

// New creates the CA under t.TempDir().
func New(t testing.TB) *PKI {
	t.Helper()
	dir := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial(t), Subject: pkix.Name{CommonName: "sidiora-test-private-ca"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	p := &PKI{Dir: dir, CAFile: filepath.Join(dir, "ca.pem"), ca: ca, caKey: key, t: t}
	write(t, p.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	return p
}

// Roots returns a pool holding only this CA.
func (p *PKI) Roots() *x509.CertPool {
	roots := x509.NewCertPool()
	roots.AddCert(p.ca)
	return roots
}

// Leaf issues name.pem / name.key for dnsName (plus 127.0.0.1 and ::1, so a
// test can dial the loopback listener while verifying dnsName).
func (p *PKI) Leaf(name, dnsName string) Leaf {
	return issue(p.t, p.Dir, name, dnsName, p.ca, p.caKey)
}

// Foreign issues a leaf from a different, untrusted CA.
func Foreign(t testing.TB, name, dnsName string) Leaf {
	other := New(t)
	return other.Leaf(name, dnsName)
}

// Ed25519 returns a fresh request-assertion key.
func Ed25519(t testing.TB) AssertionKey {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return AssertionKey{Private: private, StdBase64: base64.StdEncoding.EncodeToString(private), PublicRawURL: base64.RawURLEncoding.EncodeToString(public)}
}

func issue(t testing.TB, dir, name, dnsName string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) Leaf {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial(t), Subject: pkix.Name{CommonName: dnsName}, DNSNames: []string{dnsName}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	leaf := Leaf{CertFile: filepath.Join(dir, name+".pem"), KeyFile: filepath.Join(dir, name+".key"), CertPEM: certPEM, KeyPEM: keyPEM}
	write(t, leaf.CertFile, certPEM)
	write(t, leaf.KeyFile, keyPEM)
	digest := sha256.Sum256(der)
	leaf.SHA256 = hex.EncodeToString(digest[:])
	if leaf.Pair, err = tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatal(err)
	}
	return leaf
}

func serial(t testing.TB) *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func write(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
