package auth

import (
	"bufio"
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

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func admittedSignedLoadPlan(t *testing.T) SignedLoadPlan {
	t.Helper()
	path := os.Getenv("L34_LOAD_PLAN")
	if path == "" {
		t.Fatal("L34_LOAD_PLAN with admitted workload/source identities required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 65536 || rejectDuplicateKeys(raw) != nil {
		t.Fatal("invalid admitted load plan")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var plan SignedLoadPlan
	if err = decoder.Decode(&plan); err != nil {
		t.Fatal(err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		t.Fatal("trailing admitted plan")
	}
	if err = plan.Validate(); err != nil {
		t.Fatal(err)
	}
	if plan.WorkloadSHA256 != SignedLoadWorkloadDigest(plan) {
		t.Fatal("admitted workload digest mismatch")
	}
	for _, required := range []string{"shared/auth/load_measurement.go", "shared/auth/load_measurement_integration_test.go", "shared/auth/assertion.go", "shared/auth/contract.go", "shared/auth/eip191.go", "shared/auth/http_authorization.go", "shared/auth/ledger.go", "shared/auth/audit_delivery.go"} {
		if _, ok := plan.SourceSHA256[required]; !ok {
			t.Fatal("required source binding missing", required)
		}
	}
	for relative, want := range plan.SourceSHA256 {
		if !strings.HasPrefix(relative, "shared/auth/") || filepath.Clean(relative) != relative || filepath.IsAbs(relative) {
			t.Fatal("source outside exact auth scope", relative)
		}
		path := filepath.Join("../..", relative)
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatal("source binding unavailable", relative)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(data)
		if hex.EncodeToString(digest[:]) != want {
			t.Fatal("source identity mismatch", relative)
		}
	}
	spec, err := os.ReadFile("../../../../launchpad/spec/production-system/spec.kvx")
	if err != nil {
		t.Fatal(err)
	}
	specDigest := sha256.Sum256(spec)
	if hex.EncodeToString(specDigest[:]) != plan.SpecSHA256 {
		t.Fatal("current spec digest mismatch")
	}
	return plan
}

type signedLoadRequestIDKey struct{}

func TestPhase4RealSignedLoadMeasurement(t *testing.T) {
	plan := admittedSignedLoadPlan(t)
	output := os.Getenv("L34_LOAD_OUTPUT_DIR")
	if output == "" || !filepath.IsAbs(output) {
		t.Fatal("absolute allocated L34_LOAD_OUTPUT_DIR required")
	}
	if err := os.MkdirAll(output, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.OpenFile(filepath.Join(output, plan.RunID+"-requests.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	pool, _ := phase2AuthDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(plan.DurationSeconds)*time.Second+time.Duration(plan.RequestTimeoutMS)*time.Millisecond+30*time.Second)
	defer cancel()
	chainEndpoint := os.Getenv("L34_ANVIL_URL")
	if chainEndpoint == "" {
		t.Fatal("owned chain125 RPC required")
	}
	backend, err := ethclient.DialContext(ctx, chainEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	reported := NewReportedHashBackend(backend.Client())
	committed, err := reported.ReportedBlockByNumber(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wallet := stringsLowerAddress(key)
	if plan.ProofKind == "erc1271" {
		sender := crypto.PubkeyToAddress(key.PublicKey)
		if err = backend.Client().CallContext(ctx, nil, "anvil_setBalance", sender, "0x56bc75e2d63100000"); err != nil {
			t.Fatal(err)
		}
		bytecode, err := os.ReadFile("testdata/TestWallet.bin")
		if err != nil {
			t.Fatal(err)
		}
		manifestBytes, err := os.ReadFile("testdata/fixture-manifest.json")
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			BytecodeSHA256 string `json:"bytecode_sha256"`
			SourceSHA256   string `json:"source_sha256"`
		}
		if json.Unmarshal(manifestBytes, &manifest) != nil {
			t.Fatal("invalid real contract fixture manifest")
		}
		bytecodeDigest := sha256.Sum256(bytecode)
		source, err := os.ReadFile("testdata/TestWallet.sol")
		if err != nil {
			t.Fatal(err)
		}
		sourceDigest := sha256.Sum256(source)
		if hex.EncodeToString(bytecodeDigest[:]) != manifest.BytecodeSHA256 || hex.EncodeToString(sourceDigest[:]) != manifest.SourceSHA256 {
			t.Fatal("real contract fixture digest mismatch")
		}
		price, err := backend.SuggestGasPrice(ctx)
		if err != nil {
			t.Fatal(err)
		}
		nonce, err := backend.PendingNonceAt(ctx, sender)
		if err != nil {
			t.Fatal(err)
		}
		deploy := types.NewContractCreation(nonce, big.NewInt(0), 500000, price, append(common.FromHex(string(bytecode)), common.LeftPadBytes(sender.Bytes(), 32)...))
		signed, err := types.SignTx(deploy, types.NewEIP155Signer(big.NewInt(125)), key)
		if err != nil {
			t.Fatal(err)
		}
		if err = backend.SendTransaction(ctx, signed); err != nil {
			t.Fatal(err)
		}
		receipt, err := bind.WaitMined(ctx, backend, signed)
		if err != nil || receipt.Status != 1 {
			t.Fatal("real contract wallet deployment", err)
		}
		wallet = strings.ToLower(receipt.ContractAddress.Hex())
		committed, err = reported.ReportedBlockByNumber(ctx, receipt.BlockNumber)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = pool.Exec(ctx, `INSERT INTO auth_committed_blocks VALUES(125,$1,$2,'healthy')`, committed.Header.Number.String(), committed.Hash.Hex()); err != nil {
		t.Fatal(err)
	}
	account, session := uuid.NewString(), uuid.NewString()
	accessHash := make([]byte, 32)
	if _, err = rand.Read(accessHash); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO identity_accounts VALUES($1,0,clock_timestamp());`, account); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO identity_wallet_links VALUES($1,$2,125,$3,'private','verified',clock_timestamp())`, uuid.NewString(), account, wallet); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO identity_sessions VALUES($1,$2,$3,0,$4,$4,clock_timestamp(),clock_timestamp()+interval '2 days',clock_timestamp()+interval '3 days',clock_timestamp(),clock_timestamp(),'auth-load',NULL)`, session, account, wallet, accessHash); err != nil {
		t.Fatal(err)
	}
	identity, err := (PostgresIdentityResolver{Pool: pool}).Resolve(ctx, Principal{Wallet: wallet, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	public, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{SerialNumber: big.NewInt(434), Subject: pkix.Name{CommonName: "auth-load-owned"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(48 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
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
	pin := sha256.Sum256(der)
	assertionTransport := AssertionTransport{ActorAssertionHeader, ActorSignatureHeader}
	assertions := ServiceAssertionVerifier{Transport: assertionTransport, Issuer: "gateway", Audience: "account", PeerCertSHA256: hex.EncodeToString(pin[:]), Key: public}
	policies, routes := AccountOwnerPoliciesV2()
	verification := &AuthVerificationMeasurements{}
	authorizer := OperationAuthorizer{Ledger: Ledger{Pool: pool, Environment: plan.Environment, AuditKey: accessHash}, Verifier: CommittedProofVerifier{Backend: reported, Blocks: PostgresCommittedBlockProvider{Pool: pool}, Observe: verification.Observe}, Policies: policies, Routes: routes, Transport: OperationTransport{OperationEnvelopeHeader, OperationSignatureHeader}}
	owner := func(ctx context.Context, p IdentityPrincipal, r *http.Request) (string, error) {
		return strings.TrimPrefix(r.URL.Path, "/v1/profiles/"), nil
	}
	mutation := func(ctx context.Context, tx pgx.Tx, body []byte) ([]byte, error) {
		requestID, ok := ctx.Value(signedLoadRequestIDKey{}).(string)
		if !ok || !validIdentityID(requestID) {
			return nil, ErrScopeDenied
		}
		digest, err := BodyDigest(body)
		if err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO auth_owned_effects VALUES($1,$2)`, requestID, digest)
		return []byte(`{"updated":true}`), err
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert})
	if err != nil {
		t.Fatal(err)
	}
	authorized := authorizer.Handler("profile.update", assertions, PostgresIdentityResolver{Pool: pool}, owner, mutation)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), signedLoadRequestIDKey{}, r.Header.Get("X-Request-ID"))))
	}), ReadHeaderTimeout: time.Second}
	go server.Serve(listener)
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{cert}}, MaxConnsPerHost: plan.Concurrency, MaxIdleConnsPerHost: plan.Concurrency}
	defer transport.CloseIdleConnections()
	harness := SignedLoadHarness{Client: &http.Client{Transport: transport, Timeout: time.Duration(plan.RequestTimeoutMS) * time.Millisecond}, Endpoint: "https://" + listener.Addr().String(), Identity: identity, Signer: ServiceAssertionIssuer{Issuer: "gateway", Audience: "account", Key: signing, Transport: assertionTransport}, WalletKey: key, Pool: pool, Action: "profile.update", Method: "PATCH", Path: "/v1/profiles/" + wallet, Body: []byte(`{"name":"load-owner"}`), Transport: authorizer.Transport, ServerCertificateSHA256: hex.EncodeToString(pin[:]), Verification: verification}
	result, runErr := harness.Run(ctx, plan, raw)
	if err = raw.Sync(); err != nil {
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}
	captured, err := os.Open(filepath.Join(output, plan.RunID+"-requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	captureDigest := sha256.New()
	scanner := bufio.NewScanner(captured)
	scanner.Buffer(make([]byte, 4096), 65536)
	seenSequence := make([]bool, int64(plan.DurationSeconds)*int64(plan.TargetRPS))
	var requestSamples, queueSamples, measuredSamples int64
	for scanner.Scan() {
		line := scanner.Bytes()
		captureDigest.Write(line)
		captureDigest.Write([]byte{'\n'})
		for _, secret := range []string{wallet, account, session, string(harness.Body), hex.EncodeToString(accessHash)} {
			if bytes.Contains(line, []byte(secret)) {
				captured.Close()
				t.Fatal("raw measurement leaked identity or private payload")
			}
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(line, &record) != nil {
			captured.Close()
			t.Fatal("raw measurement JSON corrupt")
		}
		if _, ok := record["sequence"]; ok {
			var sample SignedLoadSample
			if json.Unmarshal(line, &sample) != nil || sample.Sequence < 0 || sample.Sequence >= int64(len(seenSequence)) || seenSequence[sample.Sequence] || !validIdentityID(sample.RequestID) {
				captured.Close()
				t.Fatal("raw sample identity or count corrupt")
			}
			seenSequence[sample.Sequence] = true
			requestSamples++
			if !sample.Warmup {
				measuredSamples++
			}
			if sample.CorrectedLatencyNS < sample.ServiceLatencyNS || sample.QueueDelayNS < 0 || (sample.Outcome == "accepted" && sample.HTTPStatus != 200) {
				captured.Close()
				t.Fatal("raw latency or admission measurement inconsistent")
			}
		} else {
			queueSamples++
		}
	}
	captureErr := scanner.Err()
	closeErr := captured.Close()
	if captureErr != nil || closeErr != nil {
		t.Fatal("raw capture unavailable", captureErr, closeErr)
	}
	if hex.EncodeToString(captureDigest.Sum(nil)) != result.SampleSHA256 || requestSamples != result.ScheduledRequests || queueSamples != result.QueueObservations || measuredSamples != result.MeasuredRequests {
		t.Fatal("raw evidence digest or sample count mismatch")
	}
	report, err := os.OpenFile(filepath.Join(output, plan.RunID+"-result.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = result.WriteJSON(report); err != nil {
		report.Close()
		t.Fatal(err)
	}
	if err = report.Sync(); err != nil {
		report.Close()
		t.Fatal(err)
	}
	if err = report.Close(); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal("measured load failed; raw evidence retained", runErr)
	}
	var effects, ledger, audits int64
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM auth_owned_effects),(SELECT count(*) FROM operation_ledger),(SELECT count(*) FROM security_audit_outbox WHERE result='accepted')`).Scan(&effects, &ledger, &audits); err != nil || effects != result.ScheduledRequests || ledger != effects || audits != effects {
		t.Fatal("real side effect/audit count mismatch", effects, ledger, audits, err)
	}
	var missingEffects int64
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM auth_owned_effects e LEFT JOIN operation_ledger l USING(request_id) LEFT JOIN security_audit_outbox a ON a.request_id=l.request_id AND a.action=l.action AND a.result='accepted' WHERE l.request_id IS NULL OR a.id IS NULL`).Scan(&missingEffects); err != nil || missingEffects != 0 {
		t.Fatal("business effect lacks committed request/audit correlation", missingEffects, err)
	}
	before, err := ReadAuthStateSnapshot(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	lossAt := time.Now().UTC()
	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, interruptionErr := connection.Exec(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`)
	if interruptionErr == nil {
		_, interruptionErr = connection.Exec(ctx, `SELECT 1`)
	}
	connection.Release()
	if interruptionErr == nil {
		t.Fatal("owned PostgreSQL connection interruption did not interrupt client")
	}
	after, err := ReadAuthStateSnapshot(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	recoveryErr := CompareAuthRecovery(before, after)
	recoveryRecord := struct {
		RunID                    string    `json:"runId"`
		CandidateSHA256          string    `json:"candidateSha256"`
		FaultKind                string    `json:"faultKind"`
		LossAt                   time.Time `json:"lossAt"`
		RecoveredAt              time.Time `json:"recoveredAt"`
		Before, After            AuthStateSnapshot
		AssertionPassed          bool `json:"assertionPassed"`
		GlobalAcceptanceDeferred bool `json:"globalAcceptanceDeferred"`
	}{plan.RunID, plan.CandidateSHA256, "single owned PostgreSQL connection termination", lossAt, time.Now().UTC(), before, after, recoveryErr == nil, true}
	recoveryFile, err := os.OpenFile(filepath.Join(output, plan.RunID+"-recovery.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(recoveryFile).Encode(recoveryRecord); err != nil {
		recoveryFile.Close()
		t.Fatal(err)
	}
	if err = recoveryFile.Sync(); err != nil {
		recoveryFile.Close()
		t.Fatal(err)
	}
	if err = recoveryFile.Close(); err != nil {
		t.Fatal(err)
	}
	if recoveryErr != nil {
		t.Fatal(recoveryErr)
	}
	if result.MeasuredRequests == 0 || result.QueueObservations == 0 || result.P95CorrectedNS <= 0 || result.P99CorrectedNS < result.P95CorrectedNS || !result.GlobalAcceptanceDeferred {
		t.Fatal("required raw measurements absent")
	}
	t.Logf("L34_PHASE4_MEASUREMENT run=%s requests=%d p95_ns=%d p99_ns=%d failures=%d queue_samples=%d checksum=%s full_az_restore_deferred=true", plan.RunID, result.ScheduledRequests, result.P95CorrectedNS, result.P99CorrectedNS, result.Failures, result.QueueObservations, result.SampleSHA256)
}

func TestPhase4RejectsMissingLoadAuthority(t *testing.T) {
	if (SignedLoadPlan{}).Validate() == nil {
		t.Fatal("missing workload authority accepted")
	}
	if CompareAuthRecovery(AuthStateSnapshot{}, AuthStateSnapshot{}) == nil {
		t.Fatal("missing recovery state accepted")
	}
	_, err := MeasureIdentityRevocation(context.Background(), MTLSIdentityAuthority{}, StreamPrincipal{}, time.Time{})
	if err == nil {
		t.Fatal("missing revocation authority accepted")
	}
	var rejection *IdentityAuthorityError
	if errors.As(err, &rejection) {
		t.Fatal("missing authority invented revocation")
	}
}
