package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"math/big"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func signedOperation(t *testing.T) (OperationRequest, func(context.Context, string, string, string) error) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	body := []byte(`{"name":"new"}`)
	digest, _ := BodyDigest(body)
	now := time.Now().Unix()
	e := OperationEnvelope{1, "125", "test", wallet, "profile.update", "PATCH", "/profiles/me", digest, strings.Repeat("a", 64), now, now + 300}
	statement, err := e.Statement()
	if err != nil {
		t.Fatal(err)
	}
	sig, err := crypto.Sign(accounts.TextHash([]byte(statement)), key)
	if err != nil {
		t.Fatal(err)
	}
	return OperationRequest{Envelope: e, Signature: hexutil.Encode(sig), Body: body, Actor: wallet, Action: e.Action, Method: e.Method, Path: e.Path, RequestID: "10000000-0000-4000-8000-000000000034", PolicyVersion: "1"}, func(ctx context.Context, wallet, message, sig string) error {
		_, err := VerifyEOA(wallet, message, sig, nil)
		return err
	}
}
func resignOperation(t *testing.T, req *OperationRequest) {
	t.Helper()
	key, err := crypto.HexToECDSA(strings.Repeat("1", 64))
	if err != nil {
		t.Fatal(err)
	}
	statement, err := req.Envelope.Statement()
	if err != nil {
		t.Fatal(err)
	}
	signature, err := crypto.Sign(accounts.TextHash([]byte(statement)), key)
	if err != nil {
		t.Fatal(err)
	}
	req.Signature = hexutil.Encode(signature)
}

func TestStrictEOACanonicalAndRecoveryIDs(t *testing.T) {
	key, _ := crypto.GenerateKey()
	wallet := crypto.PubkeyToAddress(key.PublicKey).Hex()
	sig, _ := crypto.Sign(accounts.TextHash([]byte("exact")), key)
	for _, offset := range []byte{0, 27} {
		v := append([]byte(nil), sig...)
		v[64] += offset
		if _, err := VerifyEOA(wallet, "exact", hexutil.Encode(v), nil); err != nil {
			t.Fatal(err)
		}
	}
	high := append([]byte(nil), sig...)
	s := new(big.Int).Sub(crypto.S256().Params().N, new(big.Int).SetBytes(sig[32:64]))
	s.FillBytes(high[32:64])
	high[64] ^= 1
	if VerifyWalletSignature(wallet, "exact", hexutil.Encode(high)) {
		t.Fatal("high-S accepted")
	}
	for _, addr := range []string{"bad", "0x0000000000000000000000000000000000000000"} {
		if VerifyWalletSignature(addr, "exact", hexutil.Encode(sig)) {
			t.Fatal("address accepted")
		}
	}
}
func TestOperationBindingsAndOwnership(t *testing.T) {
	req, _ := signedOperation(t)
	for _, change := range []func(*OperationRequest){func(r *OperationRequest) { r.Action = "watchlist.update" }, func(r *OperationRequest) { r.Path = "/watchlists" }, func(r *OperationRequest) { r.Body = []byte(`{"name":"changed"}`) }, func(r *OperationRequest) { r.Envelope.ChainID = "1" }, func(r *OperationRequest) { r.Envelope.Environment = "production" }, func(r *OperationRequest) { r.Envelope.ExpiresAt = time.Now().Unix() }, func(r *OperationRequest) { r.Envelope.Path = "/a/../b" }} {
		copy := req
		change(&copy)
		if copy.Envelope.Validate(time.Now(), "test", copy.Actor, copy.Action, copy.Method, copy.Path, copy.Body) == nil {
			t.Fatal("binding accepted")
		}
	}
	policies := DefaultPolicies()
	p := Principal{Wallet: req.Actor, SessionID: "session", Roles: []string{"owner"}}
	if policies.Authorize(p, "profile.update", req.Actor) != nil {
		t.Fatal("owner denied")
	}
	for _, role := range []string{"moderator", "administrator", ""} {
		p.Roles = []string{role}
		if policies.Authorize(p, "wallet.link", req.Actor) == nil {
			t.Fatal("role elevated")
		}
	}
	p.Roles = []string{"owner"}
	if policies.Authorize(p, "profile.update", goldenAddr) == nil || policies.Authorize(p, "unknown", req.Actor) == nil {
		t.Fatal("ownership default allow")
	}
}
func TestServiceAssertionCryptoAndTLS(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Now().Unix()
	a := ServiceAssertion{1, "gateway", "profile", Principal{goldenAddr, "session", []string{"owner"}, "trace"}, RequestDigest("PATCH", "/profile", nil), now, now + 30}
	encoded, _ := json.Marshal(a)
	sig := ed25519.Sign(key, encoded)
	cert := &x509.Certificate{Raw: []byte("fixture-cert")}
	sum := sha256.Sum256(cert.Raw)
	tlsState := &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}, PeerCertificates: []*x509.Certificate{cert}}
	verify := func(state *tls.ConnectionState, aud, digest string, at time.Time) error {
		_, err := VerifyServiceAssertion(a, sig, pub, state, hex.EncodeToString(sum[:]), "gateway", aud, digest, at)
		return err
	}
	if verify(tlsState, "profile", a.RequestDigest, time.Unix(now, 0)) != nil {
		t.Fatal("valid denied")
	}
	if verify(nil, "profile", a.RequestDigest, time.Unix(now, 0)) == nil || verify(tlsState, "other", a.RequestDigest, time.Unix(now, 0)) == nil || verify(tlsState, "profile", a.RequestDigest, time.Unix(now+30, 0)) == nil {
		t.Fatal("invalid accepted")
	}
	sig[0] ^= 1
	if verify(tlsState, "profile", a.RequestDigest, time.Unix(now, 0)) == nil {
		t.Fatal("bad signature")
	}
}
func TestPostgresOperationReplayRollbackAndAudit(t *testing.T) {
	dsn := os.Getenv("L34_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires L34_POSTGRES_DSN private fixture")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	prefix := os.Getenv("L34_FIXTURE_SCHEMA_PREFIX")
	if prefix == "" {
		prefix = "kd_d6f921318f9695d48d823bc6"
	}
	if !regexp.MustCompile(`^kd_[a-f0-9]{24}$`).MatchString(prefix) {
		t.Fatal("invalid fixture schema namespace")
	}
	schema := prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = admin.Exec(ctx, "CREATE SCHEMA "+schema)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error("cleanup", err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.MaxConns = 10
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, LedgerDDL+"CREATE TABLE effects(id int);"); err != nil {
		t.Fatal(err)
	}
	ledger := Ledger{pool, "test", []byte(strings.Repeat("k", 32))}
	req, verify := signedOperation(t)
	var calls atomic.Int64
	mutate := func(ctx context.Context, tx pgx.Tx, validatedBody []byte) ([]byte, error) {
		calls.Add(1)
		_, err := tx.Exec(ctx, "INSERT INTO effects VALUES(1)")
		return []byte(`{"updated":true}`), err
	}
	var wg sync.WaitGroup
	var duplicate atomic.Int64
	var failures atomic.Int64
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, dup, err := ledger.ExecuteValidated(ctx, req, verify, mutate)
			if err != nil {
				t.Log(err)
				failures.Add(1)
			}
			if dup {
				duplicate.Add(1)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 || duplicate.Load() != 99 || failures.Load() != 0 {
		t.Fatalf("calls=%d duplicates=%d failures=%d", calls.Load(), duplicate.Load(), failures.Load())
	}

	malicious := req
	malicious.Body = []byte(`{"name":"other"}`)
	malicious.Envelope.BodyHash, _ = BodyDigest(malicious.Body)
	msg, _ := malicious.Envelope.Statement()
	fixtureKey, _ := crypto.HexToECDSA(strings.Repeat("1", 64))
	changedSig, _ := crypto.Sign(accounts.TextHash([]byte(msg)), fixtureKey)
	malicious.Signature = hexutil.Encode(changedSig)
	if _, _, err := ledger.ExecuteValidated(ctx, malicious, verify, mutate); !errors.Is(err, ErrReplayMismatch) {
		t.Fatal("mismatched retry", err)
	}
	denied := req
	denied.Path = "/v1/me/watchlist/other"
	if _, _, err := ledger.ExecuteValidated(ctx, denied, verify, mutate); err == nil {
		t.Fatal("route substitution accepted")
	}
	var effects, audits int
	pool.QueryRow(ctx, "SELECT count(*) FROM effects").Scan(&effects)
	pool.QueryRow(ctx, "SELECT count(*) FROM security_audit_outbox").Scan(&audits)
	if effects != 1 || audits != 1 {
		t.Fatal(effects, audits)
	}
	req.Envelope.Nonce = strings.Repeat("b", 64)
	resignOperation(t, &req)
	_, _, err = ledger.ExecuteValidated(ctx, req, verify, func(ctx context.Context, tx pgx.Tx, validatedBody []byte) ([]byte, error) {
		tx.Exec(ctx, "INSERT INTO effects VALUES(2)")
		return nil, errors.New("mutation fail")
	})
	if err == nil {
		t.Fatal("rollback expected")
	}
	var rows int
	pool.QueryRow(ctx, "SELECT count(*) FROM operation_ledger").Scan(&rows)
	pool.QueryRow(ctx, "SELECT count(*) FROM effects").Scan(&effects)
	if rows != 1 || effects != 1 {
		t.Fatal("rollback failed", rows, effects)
	}
	// Lock-wait expiry must be evaluated after acquisition, never before waiting.
	expireReq, expireVerify := signedOperation(t)
	expireReq.Envelope.Nonce = strings.Repeat("c", 64)
	expireReq.Envelope.IssuedAt = time.Now().Unix()
	expireReq.Envelope.ExpiresAt = expireReq.Envelope.IssuedAt + 2
	resignOperation(t, &expireReq)
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockKey := strings.Join([]string{expireReq.Actor, expireReq.Action, expireReq.Envelope.Nonce}, "|")
	if _, err = blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", lockKey); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := ledger.ExecuteValidated(ctx, expireReq, expireVerify, mutate)
		done <- err
	}()
	time.Sleep(2200 * time.Millisecond)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err == nil {
		t.Fatal("expired waiter committed")
	}
	// An unavailable audit insert must roll back target AND nonce intent.
	if _, err = pool.Exec(ctx, "ALTER TABLE security_audit_outbox ADD CONSTRAINT reject_test CHECK(false) NOT VALID"); err != nil {
		t.Fatal(err)
	}
	auditReq, auditVerify := signedOperation(t)
	auditReq.Envelope.Nonce = strings.Repeat("d", 64)
	resignOperation(t, &auditReq)
	_, _, err = ledger.ExecuteValidated(ctx, auditReq, auditVerify, mutate)
	if err == nil {
		t.Fatal("audit failure accepted")
	}
	pool.QueryRow(ctx, "SELECT count(*) FROM effects").Scan(&effects)
	pool.QueryRow(ctx, "SELECT count(*) FROM operation_ledger").Scan(&rows)
	if effects != 1 || rows != 1 {
		t.Fatal("audit rollback", effects, rows)
	}

}

func TestBoundedInputsAndTimes(t *testing.T) {
	req, _ := signedOperation(t)
	for _, times := range [][2]int64{{-9223372036854775808, 9223372036854775807}, {-1, 10}, {100, 99}} {
		e := req.Envelope
		e.IssuedAt = times[0]
		e.ExpiresAt = times[1]
		if _, err := e.Statement(); err == nil {
			t.Fatal("timestamp accepted")
		}
	}
	for _, body := range []string{`{"a":1,"a":2}`, `{"a":{"b":1,"b":2}}`, `{} {}`} {
		if _, err := BodyDigest([]byte(body)); err == nil {
			t.Fatal("ambiguous body accepted")
		}
	}
}

func TestPhase3StoredReplayIntegrityAndCanceledTransaction(t *testing.T) {
	pool, _ := phase2AuthDatabase(t)
	ctx := context.Background()
	ledger := Ledger{Pool: pool, Environment: "test", AuditKey: []byte(strings.Repeat("k", 32))}
	req, verify := signedOperation(t)
	var calls int
	mutation := func(ctx context.Context, tx pgx.Tx, body []byte) ([]byte, error) {
		calls++
		digest, err := BodyDigest(body)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO auth_owned_effects VALUES($1,$2)`, req.RequestID, digest)
		return []byte(`{"updated":true}`), err
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err := ledger.ExecuteValidated(canceled, req, verify, mutation)
	status, _, retryable := PublicCode(err)
	if status != 503 || !retryable || calls != 0 {
		t.Fatal("canceled transaction acknowledged", status, retryable, calls, err)
	}
	response, duplicate, err := ledger.ExecuteValidated(ctx, req, verify, mutation)
	if err != nil || duplicate || calls != 1 {
		t.Fatal("real committed effect", duplicate, calls, err)
	}
	hash := sha256.Sum256(response)
	for _, statement := range []string{
		`UPDATE operation_ledger SET response=convert_to('{"updated":false}','UTF8')`,
		`UPDATE operation_ledger SET response_digest=repeat('0',64)`,
		`UPDATE operation_ledger SET body_digest=repeat('0',64)`,
	} {
		if _, err = pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
		got, dup, err := ledger.ExecuteValidated(ctx, req, verify, mutation)
		var failure *VerificationError
		if !errors.As(err, &failure) || failure.Category != "replay_integrity" || !failure.Retryable || dup || got != nil || calls != 1 {
			t.Fatal("corrupt stored result accepted", dup, calls, err)
		}
		if _, err = pool.Exec(ctx, `UPDATE operation_ledger SET response=$1,response_digest=$2,body_digest=$3`, response, hex.EncodeToString(hash[:]), req.Envelope.BodyHash); err != nil {
			t.Fatal(err)
		}
	}
	got, duplicate, err := ledger.ExecuteValidated(ctx, req, verify, mutation)
	if err != nil || !duplicate || string(got) != string(response) || calls != 1 {
		t.Fatal("intact stored replay", duplicate, calls, err)
	}
	var effects, audits int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM auth_owned_effects),(SELECT count(*) FROM security_audit_outbox WHERE request_id=$1 AND result='accepted')`, req.RequestID).Scan(&effects, &audits); err != nil || effects != 1 || audits != 1 {
		t.Fatal("integrity failure mutated state", effects, audits, err)
	}
	bridge := AuditBridge{Pool: pool}
	if missing, err := bridge.MissingOperationAudits(ctx); err != nil || missing != 0 {
		t.Fatal("missing committed audit correlation", missing, err)
	}
	if pending, age, err := bridge.Pending(ctx); err != nil || pending != 1 || age < 0 {
		t.Fatal("committed audit intent discarded", pending, age, err)
	}
	t.Log("D11_015,D11_028 canceled_transaction_rejected=1 response_corruption_rejected=1 response_digest_corruption_rejected=1 body_digest_corruption_rejected=1 stored_result_recovered=1 committed_effects=1 committed_audits=1")
}
