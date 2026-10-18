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
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func phase2AuthDatabase(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("L34_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("real L34_POSTGRES_DSN required")
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "kd_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:24] + "_auth2"
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 12
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	identitySQL, err := os.ReadFile(filepath.Join("../../spec/production-system/handoffs/L33/schema-proposal.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, string(identitySQL)+LedgerDDL+AuditDeliveryDDL+`CREATE TABLE auth_committed_blocks(chain_id integer NOT NULL CHECK(chain_id=125),height numeric(20,0) NOT NULL,block_hash text NOT NULL,integrity text NOT NULL,PRIMARY KEY(chain_id,height));CREATE TABLE auth_owned_effects(request_id uuid PRIMARY KEY,body_digest text NOT NULL);`); err != nil {
		t.Fatal(err)
	}
	return pool, schema
}

func TestPhase2RealHTTPAuthorizationAndReplay(t *testing.T) {
	pool, _ := phase2AuthDatabase(t)
	ctx := context.Background()
	endpoint := os.Getenv("L34_ANVIL_URL")
	if endpoint == "" {
		t.Fatal("owned chain125 required")
	}
	backend, err := ethclient.DialContext(ctx, endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	committed, err := NewReportedHashBackend(backend.Client()).ReportedBlockByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO auth_committed_blocks VALUES(125,$1,$2,'healthy')`, committed.Header.Number.String(), committed.Hash.Hex()); err != nil {
		t.Fatal(err)
	}
	walletKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(walletKey.PublicKey).Hex())
	accountID, sessionID := uuid.NewString(), uuid.NewString()
	if _, err = pool.Exec(ctx, `INSERT INTO identity_accounts VALUES($1,0,clock_timestamp())`, accountID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO identity_wallet_links VALUES($1,$2,125,$3,'private','verified',clock_timestamp())`, uuid.NewString(), accountID, wallet); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO identity_sessions VALUES($1,$2,$3,0,$4,$4,clock_timestamp(),clock_timestamp()+interval '1 hour',clock_timestamp()+interval '1 day',clock_timestamp(),clock_timestamp(),'auth-phase2',NULL)`, sessionID, accountID, wallet, []byte("phase2-secret-hash")); err != nil {
		t.Fatal(err)
	}
	public, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(234), Subject: pkix.Name{CommonName: "auth-phase2"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, signing)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	cert := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: signing}
	certHash := sha256.Sum256(der)
	assertions := ServiceAssertionVerifier{Transport: AssertionTransport{"X-Test-Assertion", "X-Test-Service-Signature"}, Issuer: "gateway", Audience: "account", PeerCertSHA256: hex.EncodeToString(certHash[:]), Key: public}
	policies, routes := AccountOwnerPoliciesV2()
	var effects, duplicates, requests atomic.Int64
	var interruptMutation, rotateIdentity atomic.Bool
	var ownerCalls atomic.Int64
	var observationsMu sync.Mutex
	var observations []string
	authorizer := OperationAuthorizer{Ledger: Ledger{Pool: pool, Environment: "test", AuditKey: bytes.Repeat([]byte("a"), 32)}, Verifier: CommittedProofVerifier{Backend: NewReportedHashBackend(backend.Client()), Blocks: PostgresCommittedBlockProvider{Pool: pool}}, Policies: policies, Routes: routes, Transport: OperationTransport{"X-Test-Operation", "X-Test-Wallet-Signature"}, Observe: func(category string) {
		observationsMu.Lock()
		observations = append(observations, category)
		observationsMu.Unlock()
		if category == "duplicate_accepted" {
			duplicates.Add(1)
		}
	}}
	owner := func(ctx context.Context, p IdentityPrincipal, r *http.Request) (string, error) {
		ownerCalls.Add(1)
		if rotateIdentity.Swap(false) {
			if _, err := pool.Exec(ctx, `UPDATE identity_accounts SET auth_epoch=auth_epoch+1 WHERE account_id=$1`, p.AccountID); err != nil {
				return "", err
			}
		}
		if strings.HasPrefix(r.URL.Path, "/v1/profiles/") {
			return strings.TrimPrefix(r.URL.Path, "/v1/profiles/"), nil
		}
		return p.Wallet, nil
	}
	mutation := func(ctx context.Context, tx pgx.Tx, body []byte) ([]byte, error) {
		effects.Add(1)
		digest, err := BodyDigest(body)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO auth_owned_effects VALUES($1,$2)`, uuid.NewString(), digest)
		if err == nil && interruptMutation.Swap(false) {
			_, err = tx.Exec(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`)
			if err == nil {
				_, err = tx.Exec(ctx, `SELECT 1`)
			}
		}
		return []byte(`{"updated":true}`), err
	}
	profile := authorizer.Handler("profile.update", assertions, PostgresIdentityResolver{Pool: pool}, owner, mutation)
	watchlist := authorizer.Handler("watchlist.update", assertions, PostgresIdentityResolver{Pool: pool}, owner, mutation)
	link := authorizer.Handler("wallet.link", assertions, PostgresIdentityResolver{Pool: pool}, owner, mutation)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/profiles/") {
			profile.ServeHTTP(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/v1/me/wallet-links/") {
			link.ServeHTTP(w, r)
		} else {
			watchlist.ServeHTTP(w, r)
		}
	}), ReadHeaderTimeout: time.Second}
	go server.Serve(listener)
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}}, MaxConnsPerHost: 20}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	body := []byte(`{"name":"owner"}`)
	bodyHash, _ := BodyDigest(body)
	path := "/v1/profiles/" + wallet
	issued := time.Now().Unix()
	envelope := OperationEnvelope{Version: 1, ChainID: "125", Environment: "test", Actor: wallet, Action: "profile.update", Method: "PATCH", Path: path, BodyHash: bodyHash, Nonce: strings.Repeat("e", 64), IssuedAt: issued, ExpiresAt: issued + 300}
	statement, err := envelope.Statement()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(statement)), walletKey)
	if err != nil {
		t.Fatal(err)
	}
	sig[64] += 27
	rawEnvelope, _ := json.Marshal(envelope)
	operation := base64.RawURLEncoding.EncodeToString(rawEnvelope)
	requestID := uuid.NewString()
	roles := []string{"owner"}
	do := func(method, path string, body []byte, operation, signature string, spoof bool) (int, []byte, error) {
		requests.Add(1)
		r, err := http.NewRequest(method, "https://"+listener.Addr().String()+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		r.Header.Set("X-Request-ID", requestID)
		r.Header.Set("X-Test-Operation", operation)
		r.Header.Set("X-Test-Wallet-Signature", signature)
		if spoof {
			r.Header.Set("X-Actor-Wallet", wallet)
			r.Header.Set("X-API-Key", "untrusted")
		} else {
			now := time.Now().Unix()
			assertion := ServiceAssertion{Version: 1, Issuer: "gateway", Audience: "account", Principal: Principal{Wallet: wallet, SessionID: sessionID, Roles: roles, TraceID: requestID}, RequestDigest: RequestDigest(method, path, body), IssuedAt: now, ExpiresAt: now + 30}
			raw, _ := json.Marshal(assertion)
			r.Header.Set("X-Test-Assertion", base64.RawURLEncoding.EncodeToString(raw))
			r.Header.Set("X-Test-Service-Signature", base64.RawURLEncoding.EncodeToString(ed25519.Sign(signing, raw)))
		}
		response, err := client.Do(r)
		if err != nil {
			return 0, nil, err
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		return response.StatusCode, data, err
	}
	code, _, err := do("PATCH", path, body, operation, hexutil.Encode(sig), true)
	if err != nil || code != 401 || effects.Load() != 0 || ownerCalls.Load() != 0 {
		t.Fatal("spoof reached state", code, err, effects.Load())
	}
	var wg sync.WaitGroup
	var failures atomic.Int64
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code, data, err := do("PATCH", path, body, operation, hexutil.Encode(sig), false)
			if err != nil || code != 200 || !bytes.Contains(data, []byte(`"updated":true`)) {
				failures.Add(1)
			}
		}()
	}
	wg.Wait()
	if failures.Load() != 0 || effects.Load() != 1 || duplicates.Load() != 99 {
		t.Fatal("concurrent effects", failures.Load(), effects.Load(), duplicates.Load())
	}
	for _, c := range []struct {
		method, path string
		body         []byte
		signature    string
	}{{"PUT", "/v1/me/watchlist/" + goldenAddr, body, hexutil.Encode(sig)}, {"PATCH", path, []byte(`{"name":"changed"}`), hexutil.Encode(sig)}, {"PATCH", path, body, "0xzz"}, {"PATCH", path, body, goldenSig}} {
		requestID = uuid.NewString()
		code, _, err = do(c.method, c.path, c.body, operation, c.signature, false)
		if err != nil || code < 400 || effects.Load() != 1 {
			t.Fatal("substitution accepted", code, err)
		}
	}
	requestID = uuid.NewString()
	code, _, err = do("PATCH", "/v1/profiles/"+goldenAddr, body, operation, hexutil.Encode(sig), false)
	if err != nil || code != 403 || effects.Load() != 1 {
		t.Fatal("foreign owner accepted", code, err)
	}
	roles = []string{"moderator"}
	requestID = uuid.NewString()
	code, _, err = do("POST", "/v1/me/wallet-links/"+uuid.NewString()+":verify", body, operation, hexutil.Encode(sig), false)
	if err != nil || code != 403 || effects.Load() != 1 {
		t.Fatal("moderator wallet override", code, err)
	}
	roles = []string{"owner"}
	watchEnvelope := envelope
	watchEnvelope.Action = "watchlist.update"
	watchEnvelope.Method = "PUT"
	watchEnvelope.Path = "/v1/me/watchlist/" + goldenAddr
	watchStatement, err := watchEnvelope.Statement()
	if err != nil {
		t.Fatal(err)
	}
	watchSignature, err := crypto.Sign(accounts.TextHash([]byte(watchStatement)), walletKey)
	if err != nil {
		t.Fatal(err)
	}
	watchRaw, _ := json.Marshal(watchEnvelope)
	requestID = uuid.NewString()
	code, _, err = do("PUT", "/v1/me/watchlist/"+wallet, body, base64.RawURLEncoding.EncodeToString(watchRaw), hexutil.Encode(watchSignature), false)
	if err != nil || code < 400 || effects.Load() != 1 {
		t.Fatal("pool substitution accepted", code, err)
	}
	var ledgerCount, audits, effectRows int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM operation_ledger),(SELECT count(*) FROM security_audit_outbox WHERE result='accepted'),(SELECT count(*) FROM auth_owned_effects)`).Scan(&ledgerCount, &audits, &effectRows); err != nil || ledgerCount != 1 || audits != 1 || effectRows != 1 {
		t.Fatal(ledgerCount, audits, effectRows, err)
	}
	assertRows := func(want int) {
		t.Helper()
		var rows, accepted, business, missing int
		if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM operation_ledger),(SELECT count(*) FROM security_audit_outbox WHERE result='accepted'),(SELECT count(*) FROM auth_owned_effects),(SELECT count(*) FROM operation_ledger l LEFT JOIN security_audit_outbox a ON a.request_id=l.request_id AND a.action=l.action AND a.result='accepted' WHERE a.id IS NULL)`).Scan(&rows, &accepted, &business, &missing); err != nil || rows != want || accepted != want || business != want || missing != 0 {
			t.Fatal("committed mutation correlation", rows, accepted, business, missing, err)
		}
	}
	sign := func(e OperationEnvelope) (string, string) {
		t.Helper()
		statement, err := e.Statement()
		if err != nil {
			t.Fatal(err)
		}
		signature, err := crypto.Sign(accounts.TextHash([]byte(statement)), walletKey)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(raw), hexutil.Encode(signature)
	}
	assertPrivate := func(data []byte, secrets ...string) {
		t.Helper()
		for _, secret := range secrets {
			if secret != "" && bytes.Contains(data, []byte(secret)) {
				t.Fatal("private input present in public failure")
			}
		}
	}
	roles = nil
	requestID = uuid.NewString()
	code, data, err := do("PUT", "/v1/me/watchlist/"+wallet, body, operation, hexutil.Encode(sig), false)
	if err != nil || code != 403 {
		t.Fatal("anonymous watchlist authorized", code, err)
	}
	assertRows(1)
	roles = []string{"owner"}
	for _, version := range []int{0, 3} {
		invalid := envelope
		invalid.Version = version
		raw, err := json.Marshal(invalid)
		if err != nil {
			t.Fatal(err)
		}
		requestID = uuid.NewString()
		code, data, err = do("PATCH", path, body, base64.RawURLEncoding.EncodeToString(raw), hexutil.Encode(sig), false)
		if err != nil || code < 400 {
			t.Fatal("unsupported version accepted", version, code, err)
		}
		assertPrivate(data, string(body), hexutil.Encode(sig), operation)
	}
	assertRows(1)
	privateBody := []byte(`{"name":"private-canary-do-not-expose"}`)
	privateEnvelope := envelope
	privateEnvelope.Nonce = strings.Repeat("2", 64)
	privateEnvelope.BodyHash, err = BodyDigest(privateBody)
	if err != nil {
		t.Fatal(err)
	}
	privateOperation, privateSignature := sign(privateEnvelope)
	requestID = uuid.NewString()
	interruptMutation.Store(true)
	code, data, err = do("PATCH", path, privateBody, privateOperation, privateSignature, false)
	if err != nil || code != 503 {
		t.Fatal("database interruption acknowledged", code, err)
	}
	assertPrivate(data, string(privateBody), "private-canary-do-not-expose", privateSignature, privateOperation, wallet, sessionID)
	assertRows(1)
	code, _, err = do("PATCH", path, privateBody, privateOperation, privateSignature, false)
	if err != nil || code != 200 {
		t.Fatal("interrupted transaction did not recover", code, err)
	}
	assertRows(2)
	code, data, err = do("PATCH", path, privateBody, privateOperation, privateSignature, false)
	if err != nil || code != 200 || !bytes.Contains(data, []byte(requestID)) {
		t.Fatal("recovered result replay", code, err)
	}
	assertRows(2)
	changed := privateEnvelope
	changed.BodyHash, err = BodyDigest([]byte(`{"name":"different-private-canary"}`))
	if err != nil {
		t.Fatal(err)
	}
	changedOperation, changedSignature := sign(changed)
	requestID = uuid.NewString()
	code, data, err = do("PATCH", path, []byte(`{"name":"different-private-canary"}`), changedOperation, changedSignature, false)
	if err != nil || code != 409 {
		t.Fatal("signed nonce mismatch accepted", code, err)
	}
	assertPrivate(data, "different-private-canary", changedSignature, changedOperation)
	assertRows(2)
	requestID = uuid.NewString()
	auditEnvelope := envelope
	auditEnvelope.Nonce = strings.Repeat("3", 64)
	auditOperation, auditSignature := sign(auditEnvelope)
	if _, err = pool.Exec(ctx, `ALTER TABLE security_audit_outbox RENAME TO security_audit_outbox_unavailable`); err != nil {
		t.Fatal(err)
	}
	code, data, err = do("PATCH", path, body, auditOperation, auditSignature, false)
	if _, restoreErr := pool.Exec(ctx, `ALTER TABLE security_audit_outbox_unavailable RENAME TO security_audit_outbox`); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err != nil || code != 503 {
		t.Fatal("missing audit acknowledged", code, err)
	}
	assertPrivate(data, string(body), auditSignature, auditOperation, "security_audit_outbox")
	assertRows(2)
	code, _, err = do("PATCH", path, body, auditOperation, auditSignature, false)
	if err != nil || code != 200 {
		t.Fatal("audit recovery failed", code, err)
	}
	assertRows(3)
	if _, err = pool.Exec(ctx, `ALTER TABLE identity_sessions RENAME TO identity_sessions_unavailable`); err != nil {
		t.Fatal(err)
	}
	requestID = uuid.NewString()
	code, data, err = do("PATCH", path, body, auditOperation, auditSignature, false)
	if _, restoreErr := pool.Exec(ctx, `ALTER TABLE identity_sessions_unavailable RENAME TO identity_sessions`); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err != nil || code != 503 {
		t.Fatal("identity outage authorized", code, err)
	}
	assertPrivate(data, string(body), auditSignature, "identity_sessions", wallet, sessionID)
	assertRows(3)
	requestID = uuid.NewString()
	rotateIdentity.Store(true)
	code, _, err = do("PATCH", path, body, auditOperation, auditSignature, false)
	if err != nil || code != 403 {
		t.Fatal("stale epoch crossed mutation boundary", code, err)
	}
	assertRows(3)
	requestID = uuid.NewString()
	code, _, err = do("PATCH", path, body, auditOperation, auditSignature, false)
	if err != nil || code != 403 {
		t.Fatal("stale session replay authorized", code, err)
	}
	assertRows(3)
	if _, err = pool.Exec(ctx, `UPDATE identity_sessions SET revoked_at=clock_timestamp() WHERE session_id=$1`, sessionID); err != nil {
		t.Fatal(err)
	}
	requestID = uuid.NewString()
	code, _, err = do("PATCH", path, body, operation, hexutil.Encode(sig), false)
	if err != nil || code != 403 {
		t.Fatal("revoked session", code, err)
	}
	assertRows(3)
	observationsMu.Lock()
	observed := append([]string(nil), observations...)
	observationsMu.Unlock()
	for _, category := range observed {
		for _, secret := range []string{"private-canary", wallet, sessionID, privateSignature, string(privateBody)} {
			if strings.Contains(category, secret) {
				t.Fatal("private metric field")
			}
		}
	}
	t.Log(fmt.Sprintf("D11_015,D11_017,D11_028 interrupted_transaction_503=1 recovered_commit=1 audit_outage_rollback=1 identity_outage_503=1 stale_epoch_denied=1 accepted_audit_correlations=3 privacy_categories=%d", len(observed)))

	t.Logf("D11_004,D11_014,D11_015,D11_016,D11_017,D11_028 requests=%d mutation_attempts=%d duplicate_results=%d initial_committed_audits=%d", requests.Load(), effects.Load(), duplicates.Load(), audits)
}
