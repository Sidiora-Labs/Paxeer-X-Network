package transport

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type IdentityKind int

const (
	IdentityPeer IdentityKind = iota + 1
	IdentityOperator
)

func (k IdentityKind) String() string {
	switch k {
	case IdentityPeer:
		return "peer"
	case IdentityOperator:
		return "operator"
	}
	return "unknown"
}

type Identity struct {
	Kind IdentityKind
	ID   string
	SPKI [32]byte
}

func SPKIHash(cert *x509.Certificate) [32]byte {
	return sha256.Sum256(cert.RawSubjectPublicKeyInfo)
}

func ParseSPKIHash(s string) ([32]byte, error) {
	var out [32]byte
	raw, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(strings.ToLower(s)), "0x"))
	if err != nil {
		return out, fmt.Errorf("%w: %v", ErrBadPin, err)
	}
	if len(raw) != len(out) {
		return out, fmt.Errorf("%w: want %d bytes, got %d", ErrBadPin, len(out), len(raw))
	}
	copy(out[:], raw)
	return out, nil
}

func loadKeyPair(certFile, keyFile string) (tls.Certificate, error) {
	if certFile == "" || keyFile == "" {
		return tls.Certificate{}, fmt.Errorf("%w: certificate and key paths are required", ErrConfig)
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: load key pair: %v", ErrConfig, err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("%w: parse certificate: %v", ErrConfig, err)
	}
	cert.Leaf = leaf
	return cert, nil
}

func loadCertPool(files ...string) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	for _, f := range files {
		if f == "" {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("%w: read CA file: %v", ErrConfig, err)
		}
		if !pool.AppendCertsFromPEM(raw) {
			return nil, fmt.Errorf("%w: CA file %s holds no PEM certificate", ErrConfig, f)
		}
	}
	return pool, nil
}

type operatorPin struct {
	spki    [32]byte
	hasSPKI bool
	pool    *x509.CertPool
}

func (t *Transport) identify(cs tls.ConnectionState) (Identity, error) {
	if len(cs.PeerCertificates) == 0 {
		return Identity{}, ErrUnknownIdentity
	}
	leaf := cs.PeerCertificates[0]
	h := SPKIHash(leaf)
	if id, ok := t.bySPKI[h]; ok {
		return Identity{Kind: IdentityPeer, ID: id, SPKI: h}, nil
	}
	if t.operator.hasSPKI && h == t.operator.spki {
		return Identity{Kind: IdentityOperator, ID: hex.EncodeToString(h[:]), SPKI: h}, nil
	}
	if t.operator.pool != nil {
		inter := x509.NewCertPool()
		for _, c := range cs.PeerCertificates[1:] {
			inter.AddCert(c)
		}
		_, err := leaf.Verify(x509.VerifyOptions{
			Roots:         t.operator.pool,
			Intermediates: inter,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		})
		if err == nil {
			return Identity{Kind: IdentityOperator, ID: hex.EncodeToString(h[:]), SPKI: h}, nil
		}
	}
	return Identity{}, ErrUnknownIdentity
}

func (t *Transport) RequestIdentity(r *http.Request) (Identity, error) {
	if r.TLS == nil {
		return Identity{}, ErrUnknownIdentity
	}
	return t.identify(*r.TLS)
}

func (t *Transport) serverTLSConfig(clientCAs *x509.CertPool) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{t.cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
		VerifyConnection: func(cs tls.ConnectionState) error {
			_, err := t.identify(cs)
			return err
		},
	}
}

func (t *Transport) clientTLSConfig(want [32]byte, serverName string) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		ServerName:   serverName,
		Certificates: []tls.Certificate{t.cert},
		RootCAs:      t.roots,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrUnknownIdentity
			}
			if SPKIHash(cs.PeerCertificates[0]) != want {
				return ErrPinMismatch
			}
			return nil
		},
	}
}

var (
	ErrBadPin          = errors.New("transport: bad SPKI pin")
	ErrConfig          = errors.New("transport: invalid configuration")
	ErrUnknownIdentity = errors.New("transport: certificate matches no pinned identity")
	ErrPinMismatch     = errors.New("transport: peer certificate does not match its pin")
)
