package policy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func realSecurityDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("L45_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = pool.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	schema := "l45_phase2_" + time.Now().Format("150405000000000")
	ddl, err := SecurityDDL(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, ddl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		pool.Close()
	})
	return pool, schema
}

func TestProductionOperatorRealMTLSAndPostgres(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	ctx := context.Background()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(245), Subject: pkix.Name{CommonName: "L45-private-workload"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	pair := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	digest := sha256.Sum256(der)
	v := WorkloadVerifier{Pool: pool, Schema: schema, Issuer: "gateway", Audience: "indexer", PeerCertificateSHA256: hex.EncodeToString(digest[:]), PublicKey: pub}
	journal := PostgresJournal{Pool: pool, Schema: schema}
	_, err = pool.Exec(ctx, "CREATE TABLE "+pgx.Identifier{schema, "caps"}.Sanitize()+"(n integer NOT NULL);INSERT INTO "+pgx.Identifier{schema, "caps"}.Sanitize()+" VALUES(0)")
	if err != nil {
		t.Fatal(err)
	}
	business := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := pool.Exec(r.Context(), "UPDATE "+pgx.Identifier{schema, "caps"}.Sanitize()+" SET n=n+1"); err != nil {
			http.Error(w, "storage unavailable", 503)
			return
		}
		w.WriteHeader(204)
	})
	server := httptest.NewUnstartedServer(Authorize(v, journal, "indexer", "replay", "v2", business))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	wallet := "0x52908400098527886E0F7030069857D2E4169EE7"
	session := "operator-session"
	sendAltered := func(signed bool, want int, alter func(*http.Request)) {
		t.Helper()
		request, err := http.NewRequest("POST", server.URL+"/status/dead-letter/drain", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Actor-Role", "operator")
		request.Header.Set("X-Request-ID", "l45-trace")
		if signed {
			n := time.Now()
			assertion := auth.ServiceAssertion{Version: 1, Issuer: "gateway", Audience: "indexer", Principal: auth.Principal{Wallet: wallet, SessionID: session, Roles: []string{"operator", "replay"}, TraceID: "l45-trace"}, RequestDigest: auth.RequestDigest("POST", "/status/dead-letter/drain", nil), IssuedAt: n.Unix(), ExpiresAt: n.Unix() + 30}
			wire, err := json.Marshal(assertion)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Service-Assertion", base64.RawURLEncoding.EncodeToString(wire))
			request.Header.Set("X-Service-Signature", base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, wire)))
		}
		if alter != nil {
			alter(request)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != want {
			t.Fatal(response.StatusCode, want)
		}
	}
	send := func(signed bool, want int) { sendAltered(signed, want, nil) }
	send(false, 401)
	send(true, 403)
	_, err = pool.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "operator_sessions"}.Sanitize()+"(session_id,wallet,actor,audience,issued_at,expires_at,mfa_expires_at,scopes)VALUES($1,$2,'operator-a','indexer',clock_timestamp()-interval '1 second',clock_timestamp()+interval '1 minute',clock_timestamp()+interval '1 minute',ARRAY['replay'])", session, wallet)
	if err != nil {
		t.Fatal(err)
	}
	sendAltered(true, 401, func(r *http.Request) { r.Header.Set("X-Request-ID", "unbound-trace") })
	sendAltered(true, 401, func(r *http.Request) { r.Header.Add("X-Service-Signature", r.Header.Get("X-Service-Signature")) })
	sendAltered(true, 401, func(r *http.Request) {
		wire, err := base64.RawURLEncoding.DecodeString(r.Header.Get("X-Service-Assertion"))
		if err != nil {
			t.Fatal(err)
		}
		wire = append([]byte(`{"version":0,`), wire[1:]...)
		r.Header.Set("X-Service-Assertion", base64.RawURLEncoding.EncodeToString(wire))
	})
	send(true, 204)
	if _, err = pool.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "operator_sessions"}.Sanitize()+" SET revoked=true"); err != nil {
		t.Fatal(err)
	}
	send(true, 403)
	var count int
	if err = pool.QueryRow(ctx, "SELECT n FROM "+pgx.Identifier{schema, "caps"}.Sanitize()).Scan(&count); err != nil || count != 1 {
		t.Fatal("unauthorized mutation", count, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE "+pgx.Identifier{schema, "authorization_decisions"}.Sanitize()+" SET allowed=true"); err == nil {
		t.Fatal("journal mutated")
	}
}

func realPrivateOperatorServer(t *testing.T, pool *pgxpool.Pool, schema string, bind func(WorkloadVerifier) http.Handler) (*httptest.Server, *http.Client, ed25519.PrivateKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(345), Subject: pkix.Name{CommonName: "l45-gateway"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	pair := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	digest := sha256.Sum256(der)
	v := WorkloadVerifier{Pool: pool, Schema: schema, Issuer: "gateway", Audience: "indexer", PeerCertificateSHA256: hex.EncodeToString(digest[:]), PublicKey: pub}
	server := httptest.NewUnstartedServer(bind(v))
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{pair}}}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	t.Cleanup(func() { server.Close(); transport.CloseIdleConnections() })
	return server, client, key
}

func signRealOperatorRequest(t *testing.T, r *http.Request, key ed25519.PrivateKey, session string) {
	t.Helper()
	now := time.Now().Unix()
	assertion := auth.ServiceAssertion{Version: 1, Issuer: "gateway", Audience: "indexer", Principal: auth.Principal{Wallet: "0x52908400098527886E0F7030069857D2E4169EE7", SessionID: session, TraceID: "l45-real-request"}, RequestDigest: auth.RequestDigest(r.Method, r.URL.RequestURI(), nil), IssuedAt: now, ExpiresAt: now + 30}
	wire, err := json.Marshal(assertion)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("X-Request-ID", assertion.Principal.TraceID)
	r.Header.Set("X-Service-Assertion", base64.RawURLEncoding.EncodeToString(wire))
	r.Header.Set("X-Service-Signature", base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, wire)))
}

func TestNativeAuthorizationCaptureUsesRealPrivateIdentity(t *testing.T) {
	pool, schema := realSecurityDatabase(t)
	file, err := os.CreateTemp(t.TempDir(), "authorization-*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	capture := &MiddlewareCapture{Writer: file, Key: []byte(strings.Repeat("k", 32)), RunID: "release-run", ReplicaID: "private-a", CandidateSHA256: strings.Repeat("a", 64), PolicyVersion: "v4"}
	server, client, key := realPrivateOperatorServer(t, pool, schema, func(v WorkloadVerifier) http.Handler {
		return MeasuredAuthorize(v, PostgresJournal{Pool: pool, Schema: schema}, "indexer", "replay", "v4", http.NotFoundHandler(), capture)
	})
	request, err := http.NewRequest("POST", server.URL+"/operator/replay", nil)
	if err != nil {
		t.Fatal(err)
	}
	signRealOperatorRequest(t, request, key, "ordinary-session")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("ordinary session gained replay scope", response.StatusCode)
	}
	if err = file.Sync(); err != nil {
		t.Fatal(err)
	}
	wire, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	var record MiddlewareRecord
	if err = json.Unmarshal(bytes.TrimSpace(wire), &record); err != nil {
		t.Fatal(err)
	}
	if record.Stage != "authorization" || record.Allowed || record.DurationNS <= 0 || record.CandidateSHA256 != capture.CandidateSHA256 || !digestPattern.MatchString(record.RequestReference) {
		t.Fatal("native operator timing/decision absent", record)
	}
}
