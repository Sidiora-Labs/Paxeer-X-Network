package auth

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestRealMTLSServiceAssertion(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(34), Subject: pkix.Name{CommonName: "L34-private-fixture"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	digest := sha256.Sum256(der)
	assertion := ServiceAssertion{1, "gateway", "account", Principal{goldenAddr, "session", []string{"owner"}, "trace"}, RequestDigest("PATCH", "/profile", nil), now.Unix(), now.Unix() + 30}
	data, _ := json.Marshal(assertion)
	signature := ed25519.Sign(key, data)
	port := os.Getenv("L34_MTLS_PORT")
	if port == "" {
		port = "23401"
	}
	address := net.JoinHostPort("127.0.0.1", port)
	listener, err := tls.Listen("tcp", address, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := VerifyServiceAssertion(assertion, signature, pub, r.TLS, hex.EncodeToString(digest[:]), "gateway", "account", assertion.RequestDigest, time.Now())
		if err != nil {
			http.Error(w, "denied", 401)
			return
		}
		w.WriteHeader(204)
	})}
	defer server.Close()
	go server.Serve(listener)
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{certificate}}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	resp, err := client.Get("https://" + address)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatal("authenticated peer denied", resp.StatusCode)
	}
	noPeer := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}}
	defer noPeer.CloseIdleConnections()
	if resp, err := (&http.Client{Transport: noPeer, Timeout: 2 * time.Second}).Get("https://" + address); err == nil {
		resp.Body.Close()
		t.Fatal("missing peer accepted")
	}
}

func TestContractRPCDeadlineNoEOAFallback(t *testing.T) {
	port := os.Getenv("L34_RPC_TIMEOUT_PORT")
	if port == "" {
		port = "23402"
	}
	address := net.JoinHostPort("127.0.0.1", port)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })}
	defer server.Close()
	go server.Serve(listener)
	client, err := ethclient.Dial("http://" + address)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	for _, kind := range []string{"erc1271", "eoa_eip191"} {
		t.Run(kind, func(t *testing.T) {
			var observations []string
			start := time.Now()
			_, err := VerifyProof(context.Background(), NewReportedHashBackend(client.Client()), WalletProof{Kind: kind, Wallet: goldenAddr, Signature: goldenSig}, goldenMsg, func(method, category string, duration time.Duration) {
				if duration <= 0 {
					t.Fatal("verification duration missing")
				}
				observations = append(observations, method+":"+category)
			})
			failure, ok := err.(*VerificationError)
			if !ok || !failure.Retryable || time.Since(start) > 3*time.Second {
				t.Fatal("deadline/retry", err, time.Since(start))
			}
			method := "eip1271"
			if kind == "eoa_eip191" {
				method = "eip191"
			}
			if len(observations) != 1 || observations[0] != method+":upstream" {
				t.Fatal("upstream observation", observations)
			}
			status, code, retryable := PublicCode(err)
			if status != 503 || code != "DEPENDENCY_UNAVAILABLE" || !retryable {
				t.Fatal(status, code, retryable)
			}
		})
	}
}

func TestRealMTLSServiceRequestAssertion(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(23434), Subject: pkix.Name{CommonName: "identity-service-request"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	hash := sha256.Sum256(der)
	pin := hex.EncodeToString(hash[:])
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}}}
	defer transport.CloseIdleConnections()
	signer := ServiceRequestIssuer{Issuer: "social", Audience: "identity", Key: key, Transport: AssertionTransport{IdentityAssertionHeader, IdentitySignatureHeader}}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	if err != nil {
		t.Fatal(err)
	}
	verifier := ServiceRequestVerifier{Issuer: "social", Audience: "identity", Key: public, PeerCertSHA256: pin, Transport: signer.Transport}
	server := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
		if err != nil || verifier.Authenticate(r, body) != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	go server.Serve(listener)
	defer server.Close()
	endpoint := "https://" + listener.Addr().String()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	if _, err = NewMTLSIdentityAuthority(endpoint, pin, client, signer); err != nil {
		t.Fatal(err)
	}
	signedBody := []byte(`{"action":"verify_service_request"}`)
	for _, c := range []struct {
		name, path string
		body       []byte
		sign       bool
		status     int
	}{{"matching", "/internal/v2/identity/test:verify", signedBody, true, 204}, {"altered_body", "/internal/v2/identity/test:verify", []byte(`{"action":"altered"}`), true, 401}, {"altered_path", "/internal/v2/identity/test:other", signedBody, true, 401}, {"unsigned_header_spoof", "/internal/v2/identity/test:verify", signedBody, false, 401}} {
		t.Run(c.name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, endpoint+"/internal/v2/identity/test:verify", bytes.NewReader(signedBody))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Request-ID", uuid.NewString())
			if c.sign {
				if err = signer.SignRequest(request, signedBody); err != nil {
					t.Fatal(err)
				}
			} else {
				request.Header.Set("X-Actor-Wallet", goldenAddr)
				request.Header.Set("X-API-Key", "untrusted")
			}
			request.URL.Path = c.path
			request.Body = io.NopCloser(bytes.NewReader(c.body))
			request.ContentLength = int64(len(c.body))
			response, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != c.status {
				t.Fatal(response.StatusCode, c.status)
			}
		})
	}
}
