package auth_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/testpki"
)

func TestDecodeIdentitySigningKeyForms(t *testing.T) {
	key := testpki.Ed25519(t)
	for _, encoded := range []string{key.StdBase64, base64.RawURLEncoding.EncodeToString(key.Private), " " + key.StdBase64 + "\n"} {
		decoded, err := auth.DecodeIdentitySigningKey(encoded)
		if err != nil || !bytes.Equal(decoded, key.Private) {
			t.Fatalf("%q: %v", encoded, err)
		}
	}
	mismatched := append(append([]byte(nil), key.Private.Seed()...), testpki.Ed25519(t).Private.Public().(ed25519.PublicKey)...)
	for _, encoded := range []string{"", base64.StdEncoding.EncodeToString(key.Private.Seed()), base64.StdEncoding.EncodeToString(mismatched), base64.URLEncoding.EncodeToString(key.Private)} {
		if _, err := auth.DecodeIdentitySigningKey(encoded); err == nil {
			t.Fatalf("%q accepted", encoded)
		}
	}
}

// TestMTLSIdentityClientProbe drives NewMTLSIdentityClient and Probe against a
// listener with the gateway private listener's transport and service
// verification: Probe succeeds only when the listener authenticated the
// service and then rejected the malformed check as 422.
func TestMTLSIdentityClientProbe(t *testing.T) {
	pki := testpki.New(t)
	server := pki.Leaf("gateway-server", "gateway.railway.internal")
	client := pki.Leaf("stats-client", "stats.railway.internal")
	key := testpki.Ed25519(t)
	verifier := auth.ServiceRequestVerifier{Transport: auth.AssertionTransport{AssertionHeader: auth.IdentityAssertionHeader, SignatureHeader: auth.IdentitySignatureHeader}, Issuer: "stats", Audience: "identity", PeerCertSHA256: client.SHA256, Key: key.Private.Public().(ed25519.PublicKey)}
	status := http.StatusUnprocessableEntity
	listener := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if r.URL.Path != "/internal/v2/identity/session:check" || verifier.Authenticate(r, raw) != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if string(raw) != "[]" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(status)
	}))
	listener.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pki.Roots(), Certificates: []tls.Certificate{server.Pair}}
	listener.StartTLS()
	t.Cleanup(listener.Close)
	base := auth.MTLSIdentityClientConfig{Endpoint: listener.URL, PeerCertSHA256: server.SHA256, CAFile: pki.CAFile, ClientCertFile: client.CertFile, ClientKeyPEM: string(client.KeyPEM), Issuer: "stats", SigningKey: key.StdBase64}
	authority, err := auth.NewMTLSIdentityClient(base)
	if err != nil {
		t.Fatal(err)
	}
	if err = authority.Probe(context.Background()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	for _, s := range []int{http.StatusOK, http.StatusBadRequest, http.StatusInternalServerError} {
		status = s
		var failure *auth.IdentityAuthorityError
		if err = authority.Probe(context.Background()); !errors.As(err, &failure) || failure.Status != http.StatusServiceUnavailable {
			t.Fatalf("status %d: %v", s, err)
		}
	}
	status = http.StatusUnprocessableEntity
	caPEM, err := os.ReadFile(pki.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	inline := base
	inline.CAFile, inline.CAPEM, inline.ClientCertFile, inline.ClientCertPEM = "", string(caPEM), "", string(client.CertPEM)
	inlineAuthority, err := auth.NewMTLSIdentityClient(inline)
	if err != nil {
		t.Fatal(err)
	}
	if err = inlineAuthority.Probe(context.Background()); err != nil {
		t.Fatalf("inline material probe: %v", err)
	}
	other := base
	other.SigningKey = testpki.Ed25519(t).StdBase64
	rejected, err := auth.NewMTLSIdentityClient(other)
	if err != nil {
		t.Fatal(err)
	}
	var failure *auth.IdentityAuthorityError
	if err = rejected.Probe(context.Background()); !errors.As(err, &failure) || failure.Status != http.StatusUnauthorized {
		t.Fatalf("unregistered key: %v", err)
	}
	for name, mutate := range map[string]func(*auth.MTLSIdentityClientConfig){
		"no ca":          func(c *auth.MTLSIdentityClientConfig) { c.CAFile = "" },
		"two cas":        func(c *auth.MTLSIdentityClientConfig) { c.CAPEM = "-----BEGIN CERTIFICATE-----" },
		"no client cert": func(c *auth.MTLSIdentityClientConfig) { c.ClientCertFile = "" },
		"two certs":      func(c *auth.MTLSIdentityClientConfig) { c.ClientCertPEM = string(client.CertPEM) },
		"missing ca":     func(c *auth.MTLSIdentityClientConfig) { c.CAFile = c.CAFile + ".missing" },
		"no key":         func(c *auth.MTLSIdentityClientConfig) { c.ClientKeyPEM = "" },
		"two keys":       func(c *auth.MTLSIdentityClientConfig) { c.ClientKeyFile = client.KeyFile },
		"key mismatch":   func(c *auth.MTLSIdentityClientConfig) { c.ClientKeyPEM = string(server.KeyPEM) },
		"gateway issuer": func(c *auth.MTLSIdentityClientConfig) { c.Issuer = "gateway" },
		"bad issuer":     func(c *auth.MTLSIdentityClientConfig) { c.Issuer = "stats service" },
		"endpoint path":  func(c *auth.MTLSIdentityClientConfig) { c.Endpoint += "/" },
		"plain endpoint": func(c *auth.MTLSIdentityClientConfig) { c.Endpoint = strings.Replace(c.Endpoint, "https", "http", 1) },
		"upper pin":      func(c *auth.MTLSIdentityClientConfig) { c.PeerCertSHA256 = strings.ToUpper(c.PeerCertSHA256) },
		"signing key":    func(c *auth.MTLSIdentityClientConfig) { c.SigningKey = "" },
	} {
		c := base
		mutate(&c)
		if _, err := auth.NewMTLSIdentityClient(c); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}
