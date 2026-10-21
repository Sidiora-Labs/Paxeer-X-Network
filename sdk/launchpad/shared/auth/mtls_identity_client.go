package auth

import (
	"bytes"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

// MTLSIdentityClientConfig is the environment-level description of a peer's
// private identity channel to the gateway identity authority (the gateway's
// identityListenAddress, e.g. https://gateway.railway.internal:8443): the
// private CA, this peer's client certificate and key, the pinned gateway
// server certificate and the ed25519 request-assertion key registered for
// Issuer in the gateway's identityPeers.
type MTLSIdentityClientConfig struct {
	// Endpoint is https://host:port with no path, not even "/".
	Endpoint string
	// PeerCertSHA256 is the lowercase hex sha256 of the gateway server
	// certificate's DER.
	PeerCertSHA256 string
	// Exactly one of CAFile and CAPEM supplies the PEM bundle of the private
	// CA that issued the gateway server certificate.
	CAFile string
	CAPEM  string
	// Exactly one of ClientCertFile and ClientCertPEM supplies this peer's PEM
	// client certificate; its DER sha256 is the identityPeers[].peerCertSHA256
	// the gateway pins.
	ClientCertFile string
	ClientCertPEM  string
	// Exactly one of ClientKeyFile (a PEM file) and ClientKeyPEM (the PEM
	// itself, typically a secret) supplies the client private key.
	ClientKeyFile string
	ClientKeyPEM  string
	// Issuer is this peer's identityPeers[].issuer.
	Issuer string
	// SigningKey is the 64-byte ed25519 private key (seed || public key),
	// standard base64 (padded) or unpadded base64url. The public half is the
	// identityPeers[].publicKey.
	SigningKey string
}

// DecodeIdentitySigningKey decodes an ed25519 private key given as standard
// base64 or unpadded base64url of the 64-byte seed||public form and checks the
// public half matches the seed.
func DecodeIdentitySigningKey(encoded string) (ed25519.PrivateKey, error) {
	encoded = strings.TrimSpace(encoded)
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		raw, err = base64.RawURLEncoding.Strict().DecodeString(encoded)
	}
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("identity assertion signing key must be a 64-byte ed25519 private key")
	}
	key := ed25519.PrivateKey(raw)
	if !bytes.Equal(ed25519.NewKeyFromSeed(key.Seed()), key) {
		return nil, errors.New("identity assertion signing key public half does not match its seed")
	}
	return key, nil
}

// NewMTLSIdentityClient loads c and returns the shared mTLS identity
// authority client (TLS 1.3, private-CA roots, client certificate, pinned
// gateway server certificate, ed25519 request assertions with audience
// "identity").
func NewMTLSIdentityClient(c MTLSIdentityClientConfig) (MTLSIdentityAuthority, error) {
	if (c.CAFile == "") == (c.CAPEM == "") || (c.ClientCertFile == "") == (c.ClientCertPEM == "") || (c.ClientKeyFile == "") == (c.ClientKeyPEM == "") {
		return MTLSIdentityAuthority{}, errors.New("identity mTLS requires exactly one source each for the CA, the client certificate and the client key")
	}
	if !tokenPattern.MatchString(c.Issuer) || c.Issuer == "gateway" {
		return MTLSIdentityAuthority{}, errors.New("identity assertion issuer invalid")
	}
	rootPEM := []byte(c.CAPEM)
	if c.CAFile != "" {
		info, err := os.Stat(c.CAFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
			return MTLSIdentityAuthority{}, errors.New("identity mTLS CA file unavailable")
		}
		if rootPEM, err = os.ReadFile(c.CAFile); err != nil {
			return MTLSIdentityAuthority{}, errors.New("identity mTLS CA file unavailable")
		}
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(rootPEM) {
		return MTLSIdentityAuthority{}, errors.New("identity mTLS CA invalid")
	}
	certPEM := []byte(c.ClientCertPEM)
	var err error
	if c.ClientCertFile != "" {
		certPEM, err = os.ReadFile(c.ClientCertFile)
	}
	if err != nil || len(certPEM) > 1<<20 {
		return MTLSIdentityAuthority{}, errors.New("identity mTLS client certificate unavailable")
	}
	keyPEM := []byte(c.ClientKeyPEM)
	if c.ClientKeyFile != "" {
		if keyPEM, err = os.ReadFile(c.ClientKeyFile); err != nil || len(keyPEM) > 1<<20 {
			return MTLSIdentityAuthority{}, errors.New("identity mTLS client key unavailable")
		}
	}
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return MTLSIdentityAuthority{}, errors.New("identity mTLS client certificate invalid")
	}
	key, err := DecodeIdentitySigningKey(c.SigningKey)
	if err != nil {
		return MTLSIdentityAuthority{}, err
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}}, Proxy: nil, MaxIdleConns: 64, MaxIdleConnsPerHost: 32, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 2 * time.Second}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	signer := ServiceRequestIssuer{Issuer: c.Issuer, Audience: "identity", Key: key, Transport: AssertionTransport{AssertionHeader: IdentityAssertionHeader, SignatureHeader: IdentitySignatureHeader}}
	authority, err := NewMTLSIdentityAuthority(c.Endpoint, c.PeerCertSHA256, client, signer)
	if err != nil {
		return MTLSIdentityAuthority{}, errors.New("identity mTLS authority invalid: endpoint must be https://host:port without path and the gateway pin a lowercase sha256")
	}
	return authority, nil
}
