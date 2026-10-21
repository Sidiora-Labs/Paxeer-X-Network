package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SignedLoadPlan struct {
	RunID            string            `json:"runId"`
	CandidateSHA256  string            `json:"candidateSha256"`
	SpecSHA256       string            `json:"specSha256"`
	WorkloadSHA256   string            `json:"workloadSha256"`
	SourceSHA256     map[string]string `json:"sourceSha256"`
	Environment      string            `json:"environment"`
	ProofKind        string            `json:"proofKind"`
	Region           string            `json:"region"`
	DurationSeconds  int               `json:"durationSeconds"`
	WarmupSeconds    int               `json:"warmupSeconds"`
	TargetRPS        int               `json:"targetRps"`
	Concurrency      int               `json:"concurrency"`
	QueueCapacity    int               `json:"queueCapacity"`
	RequestTimeoutMS int               `json:"requestTimeoutMs"`
	SampleIntervalMS int               `json:"sampleIntervalMs"`
}

func validMeasurementDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}

func (p SignedLoadPlan) Validate() error {
	if !validIdentityID(p.RunID) || !validMeasurementDigest(p.CandidateSHA256) || !validMeasurementDigest(p.SpecSHA256) || !validMeasurementDigest(p.WorkloadSHA256) || len(p.SourceSHA256) == 0 || !tokenPattern.MatchString(p.Environment) || !tokenPattern.MatchString(p.Region) {
		return errors.New("authorization load: missing admitted source or environment identity")
	}
	for path, digest := range p.SourceSHA256 {
		if path == "" || !validMeasurementDigest(digest) {
			return errors.New("authorization load: invalid source binding")
		}
	}
	if p.ProofKind != "eoa_eip191" && p.ProofKind != "erc1271" {
		return errors.New("authorization load: explicit EOA or contract proof kind required")
	}
	if p.DurationSeconds < 1 || p.DurationSeconds > 86400 || p.WarmupSeconds < 0 || p.WarmupSeconds >= p.DurationSeconds || p.TargetRPS < 1 || p.TargetRPS > 20000 || p.Concurrency < 1 || p.Concurrency > 4096 || p.QueueCapacity < 1 || p.QueueCapacity > 100000 || p.RequestTimeoutMS < 1 || p.RequestTimeoutMS > 30000 || p.SampleIntervalMS < 1 || p.SampleIntervalMS > 1000 || int64(p.DurationSeconds)*int64(p.TargetRPS) > 1000000 {
		return errors.New("authorization load: explicit bounded workload required")
	}
	return nil
}

func SignedLoadWorkloadDigest(p SignedLoadPlan) string {
	workload := struct {
		Environment, Region, ProofKind                                                                            string
		DurationSeconds, WarmupSeconds, TargetRPS, Concurrency, QueueCapacity, RequestTimeoutMS, SampleIntervalMS int
	}{p.Environment, p.Region, p.ProofKind, p.DurationSeconds, p.WarmupSeconds, p.TargetRPS, p.Concurrency, p.QueueCapacity, p.RequestTimeoutMS, p.SampleIntervalMS}
	raw, _ := json.Marshal(workload)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

type SignedLoadSample struct {
	Sequence           int64     `json:"sequence"`
	RequestID          string    `json:"requestId"`
	ScheduledAt        time.Time `json:"scheduledAt"`
	StartedAt          time.Time `json:"startedAt"`
	FinishedAt         time.Time `json:"finishedAt"`
	HTTPStatus         int       `json:"httpStatus"`
	Outcome            string    `json:"outcome"`
	QueueDelayNS       int64     `json:"queueDelayNs"`
	ServiceLatencyNS   int64     `json:"serviceLatencyNs"`
	CorrectedLatencyNS int64     `json:"correctedLatencyNs"`
	ResponseSHA256     string    `json:"responseSha256"`
	Warmup             bool      `json:"warmup"`
}

type SignedLoadQueueSample struct {
	ObservedAt                time.Time `json:"observedAt"`
	GeneratorQueued           int       `json:"generatorQueued"`
	GeneratorCapacity         int       `json:"generatorCapacity"`
	DatabaseAcquired          int32     `json:"databaseAcquired"`
	DatabaseMaximum           int32     `json:"databaseMaximum"`
	DatabaseCanceledAcquires  int64     `json:"databaseCanceledAcquires"`
	DatabaseAcquireDurationNS int64     `json:"databaseAcquireDurationNs"`
	PendingAudits             int64     `json:"pendingAudits"`
	OldestAuditAgeNS          int64     `json:"oldestAuditAgeNs"`
	MissingOperationAudits    int64     `json:"missingOperationAudits"`
	ObservationError          bool      `json:"observationError"`
}

type AuthStateSnapshot struct {
	ObservedAt             time.Time `json:"observedAt"`
	LedgerRecords          int64     `json:"ledgerRecords"`
	LedgerSHA256           string    `json:"ledgerSha256"`
	AuditRecords           int64     `json:"auditRecords"`
	AuditSHA256            string    `json:"auditSha256"`
	MissingOperationAudits int64     `json:"missingOperationAudits"`
}

func ReadAuthStateSnapshot(ctx context.Context, pool *pgxpool.Pool) (out AuthStateSnapshot, err error) {
	if pool == nil {
		return out, errors.New("authorization measurement: missing owning database")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),(SELECT count(*) FROM operation_ledger l LEFT JOIN security_audit_outbox a ON a.request_id=l.request_id AND a.action=l.action AND a.result='accepted' WHERE a.id IS NULL)`).Scan(&out.ObservedAt, &out.MissingOperationAudits); err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT actor,action,nonce,envelope_digest,body_digest,request_id::text,response_digest,encode(response,'hex'),to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US'),to_char(retain_until AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US') FROM operation_ledger ORDER BY actor,action,nonce`)
	if err != nil {
		return out, err
	}
	ledgerHash := sha256.New()
	for rows.Next() {
		var row [10]string
		if err = rows.Scan(&row[0], &row[1], &row[2], &row[3], &row[4], &row[5], &row[6], &row[7], &row[8], &row[9]); err != nil {
			rows.Close()
			return out, err
		}
		raw, e := json.Marshal(row)
		if e != nil {
			rows.Close()
			return out, e
		}
		ledgerHash.Write(raw)
		ledgerHash.Write([]byte{'\n'})
		out.LedgerRecords++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.LedgerSHA256 = hex.EncodeToString(ledgerHash.Sum(nil))
	rows, err = tx.Query(ctx, `SELECT id::text,request_id::text,actor_pseudonym,action,result,policy_version,request_digest,to_char(created_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS.US') FROM security_audit_outbox ORDER BY id`)
	if err != nil {
		return out, err
	}
	auditHash := sha256.New()
	for rows.Next() {
		var row [8]string
		if err = rows.Scan(&row[0], &row[1], &row[2], &row[3], &row[4], &row[5], &row[6], &row[7]); err != nil {
			rows.Close()
			return out, err
		}
		raw, e := json.Marshal(row)
		if e != nil {
			rows.Close()
			return out, e
		}
		auditHash.Write(raw)
		auditHash.Write([]byte{'\n'})
		out.AuditRecords++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	out.AuditSHA256 = hex.EncodeToString(auditHash.Sum(nil))
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}

type SignedLoadResult struct {
	Plan                       SignedLoadPlan                     `json:"plan"`
	StartedAt                  time.Time                          `json:"startedAt"`
	FinishedAt                 time.Time                          `json:"finishedAt"`
	ScheduledRequests          int64                              `json:"scheduledRequests"`
	MeasuredRequests           int64                              `json:"measuredRequests"`
	WarmupRequests             int64                              `json:"warmupRequests"`
	SuccessfulRequests         int64                              `json:"successfulRequests"`
	Failures                   int64                              `json:"failures"`
	GeneratorRejected          int64                              `json:"generatorRejected"`
	ValidOperationFailureRatio float64                            `json:"validOperationFailureRatio"`
	P95CorrectedNS             int64                              `json:"p95CorrectedNs"`
	P99CorrectedNS             int64                              `json:"p99CorrectedNs"`
	P95ServiceNS               int64                              `json:"p95ServiceNs"`
	P99ServiceNS               int64                              `json:"p99ServiceNs"`
	HistogramUpperBoundsNS     []int64                            `json:"histogramUpperBoundsNs"`
	HistogramCounts            []int64                            `json:"histogramCounts"`
	MaxGeneratorQueue          int                                `json:"maxGeneratorQueue"`
	MaxPendingAudits           int64                              `json:"maxPendingAudits"`
	MaxAuditAgeNS              int64                              `json:"maxAuditAgeNs"`
	QueueObservationFailures   int64                              `json:"queueObservationFailures"`
	QueueObservations          int64                              `json:"queueObservations"`
	SampleSHA256               string                             `json:"sampleSha256"`
	Before                     AuthStateSnapshot                  `json:"before"`
	After                      AuthStateSnapshot                  `json:"after"`
	AssertionFailures          []string                           `json:"assertionFailures"`
	GlobalAcceptanceDeferred   bool                               `json:"globalAcceptanceDeferred"`
	RequirementIDs             []string                           `json:"requirementIds"`
	MeasurementScope           string                             `json:"measurementScope"`
	Origin                     string                             `json:"origin"`
	ServerCertificateSHA256    string                             `json:"serverCertificateSha256"`
	DatabaseName               string                             `json:"databaseName"`
	DatabaseSchema             string                             `json:"databaseSchema"`
	DatabaseVersion            string                             `json:"databaseVersion"`
	VerificationMeasurements   map[string]AuthVerificationSummary `json:"verificationMeasurements"`
}

type SignedLoadHarness struct {
	Client                  *http.Client
	Endpoint                string
	Identity                IdentityPrincipal
	Signer                  ServiceAssertionIssuer
	WalletKey               *ecdsa.PrivateKey
	Pool                    *pgxpool.Pool
	Action, Method, Path    string
	Body                    []byte
	Transport               OperationTransport
	ServerCertificateSHA256 string
	Verification            *AuthVerificationMeasurements
}

type AuthVerificationSummary struct {
	Samples    int64            `json:"samples"`
	P95NS      int64            `json:"p95Ns"`
	P99NS      int64            `json:"p99Ns"`
	Categories map[string]int64 `json:"categories"`
}

type AuthVerificationMeasurements struct {
	mu         sync.Mutex
	durations  map[string][]int64
	categories map[string]map[string]int64
	invalid    bool
}

func (m *AuthVerificationMeasurements) Observe(method, category string, duration time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	validCategory := false
	for _, known := range []string{"accepted", "address", "message_size", "signature_length", "recovery_id", "noncanonical", "recovery", "signer", "upstream", "chain", "proof_kind", "contract_missing", "contract_rejected", "rejected"} {
		if category == known {
			validCategory = true
			break
		}
	}
	if (method != "eip191" && method != "eip1271" && method != "wallet_authority") || !validCategory || duration <= 0 {
		m.invalid = true
		return
	}
	if m.durations == nil {
		m.durations = map[string][]int64{}
		m.categories = map[string]map[string]int64{}
	}
	if len(m.durations[method]) >= 1000000 {
		m.invalid = true
		return
	}
	m.durations[method] = append(m.durations[method], duration.Nanoseconds())
	if m.categories[method] == nil {
		m.categories[method] = map[string]int64{}
	}
	m.categories[method][category]++
}

func (m *AuthVerificationMeasurements) Snapshot() (map[string]AuthVerificationSummary, error) {
	if m == nil {
		return nil, errors.New("authorization measurement: verification observer required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]AuthVerificationSummary{}
	for method, values := range m.durations {
		categories := map[string]int64{}
		for key, count := range m.categories[method] {
			categories[key] = count
		}
		out[method] = AuthVerificationSummary{Samples: int64(len(values)), P95NS: nearestRank(values, 95), P99NS: nearestRank(values, 99), Categories: categories}
	}
	if m.invalid || len(out) == 0 {
		return out, errors.New("authorization measurement: verification samples invalid or absent")
	}
	return out, nil
}

type signedMeasurementRecorder struct {
	mu                 sync.Mutex
	writer             io.Writer
	digest             hash.Hash
	err                error
	result             *SignedLoadResult
	corrected, service []int64
	acknowledged       []string
}

func (r *signedMeasurementRecorder) write(value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		r.err = err
		return
	}
	raw = append(raw, '\n')
	if r.err == nil {
		n, err := r.writer.Write(raw)
		if err != nil {
			r.err = err
			return
		}
		if n != len(raw) {
			r.err = io.ErrShortWrite
			return
		}
		r.digest.Write(raw)
	}
}

func (r *signedMeasurementRecorder) sample(s SignedLoadSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.write(s)
	if s.Outcome == "generator_saturated" {
		r.result.GeneratorRejected++
	}
	if s.Warmup {
		r.result.WarmupRequests++
		return
	}
	r.result.MeasuredRequests++
	if s.Outcome == "accepted" {
		r.result.SuccessfulRequests++
		r.acknowledged = append(r.acknowledged, s.RequestID)
	} else {
		r.result.Failures++
	}
	r.corrected = append(r.corrected, s.CorrectedLatencyNS)
	r.service = append(r.service, s.ServiceLatencyNS)
}

func nearestRank(values []int64, percent int) int64 {
	if len(values) == 0 {
		return 0
	}
	copyValues := append([]int64(nil), values...)
	sort.Slice(copyValues, func(i, j int) bool { return copyValues[i] < copyValues[j] })
	return copyValues[(len(copyValues)*percent+99)/100-1]
}

func (h SignedLoadHarness) validate(plan SignedLoadPlan) error {
	if err := plan.Validate(); err != nil {
		return err
	}
	if plan.WorkloadSHA256 != SignedLoadWorkloadDigest(plan) {
		return errors.New("authorization load: workload digest mismatch")
	}
	u, err := url.Parse(h.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || h.Client == nil || h.Pool == nil || h.WalletKey == nil || h.Identity.Validate(time.Now()) != nil || (plan.ProofKind == "eoa_eip191" && h.Identity.Wallet != stringsLowerAddress(h.WalletKey)) || h.Action == "" || h.Method == "" || h.Path == "" || h.Transport.EnvelopeHeader == "" || h.Transport.SignatureHeader == "" {
		return errors.New("authorization load: real signed HTTPS/database providers required")
	}
	if h.Identity.AccessExpiresAt.Before(time.Now().Add(time.Duration(plan.DurationSeconds)*time.Second + time.Duration(plan.RequestTimeoutMS)*time.Millisecond)) {
		return errors.New("authorization load: session expires during workload")
	}
	transport, ok := h.Client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < tls.VersionTLS13 || transport.TLSClientConfig.RootCAs == nil || len(transport.TLSClientConfig.Certificates) == 0 || transport.DialTLS != nil || transport.DialTLSContext != nil || !validMeasurementDigest(h.ServerCertificateSHA256) || h.Verification == nil {
		return errors.New("authorization load: authenticated TLS and real verification observer required")
	}
	if _, err = BodyDigest(h.Body); err != nil {
		return err
	}
	return nil
}

func stringsLowerAddress(key *ecdsa.PrivateKey) string {
	address := crypto.PubkeyToAddress(key.PublicKey)
	return hexutil.Encode(address.Bytes())
}

func (h SignedLoadHarness) execute(ctx context.Context, plan SignedLoadPlan, sequence int64, scheduled time.Time, runStart time.Time) SignedLoadSample {
	started := time.Now()
	s := SignedLoadSample{Sequence: sequence, RequestID: uuid.NewString(), ScheduledAt: scheduled.UTC(), StartedAt: started.UTC(), Outcome: "request_invalid", Warmup: scheduled.Before(runStart.Add(time.Duration(plan.WarmupSeconds) * time.Second))}
	finish := func() SignedLoadSample {
		finished := time.Now()
		s.FinishedAt = finished.UTC()
		s.QueueDelayNS = started.Sub(scheduled).Nanoseconds()
		s.ServiceLatencyNS = finished.Sub(started).Nanoseconds()
		s.CorrectedLatencyNS = finished.Sub(scheduled).Nanoseconds()
		return s
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return finish()
	}
	digest, err := BodyDigest(h.Body)
	if err != nil {
		return finish()
	}
	now := time.Now().Unix()
	envelope := OperationEnvelope{Version: 2, ChainID: "125", Environment: plan.Environment, Actor: h.Identity.Wallet, Action: h.Action, Method: h.Method, Path: h.Path, BodyHash: digest, Nonce: hex.EncodeToString(nonce), IssuedAt: now, ExpiresAt: now + 300}
	statement, err := envelope.Statement()
	if err != nil {
		return finish()
	}
	signature, err := crypto.Sign(accounts.TextHash([]byte(statement)), h.WalletKey)
	if err != nil {
		return finish()
	}
	signature[64] += 27
	raw, err := json.Marshal(envelope)
	if err != nil {
		return finish()
	}
	request, err := http.NewRequestWithContext(ctx, h.Method, h.Endpoint+h.Path, bytes.NewReader(h.Body))
	if err != nil {
		return finish()
	}
	request.Header.Set("X-Request-ID", s.RequestID)
	request.Header.Set(h.Transport.EnvelopeHeader, base64.RawURLEncoding.EncodeToString(raw))
	request.Header.Set(h.Transport.SignatureHeader, hexutil.Encode(signature))
	request.Header.Set("Content-Type", "application/json")
	if err = h.Signer.SignRequest(request, h.Identity, []string{"owner"}, h.Body); err != nil {
		return finish()
	}
	response, err := h.Client.Do(request)
	if err != nil {
		s.Outcome = "transport_failure"
		return finish()
	}
	defer response.Body.Close()
	if response.TLS == nil || response.TLS.Version < tls.VersionTLS13 || len(response.TLS.VerifiedChains) == 0 || len(response.TLS.PeerCertificates) == 0 {
		s.Outcome = "response_invalid"
		return finish()
	}
	peer := sha256.Sum256(response.TLS.PeerCertificates[0].Raw)
	if hex.EncodeToString(peer[:]) != h.ServerCertificateSHA256 {
		s.Outcome = "response_invalid"
		return finish()
	}
	s.HTTPStatus = response.StatusCode
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		s.Outcome = "response_invalid"
		return finish()
	}
	responseDigest := sha256.Sum256(body)
	s.ResponseSHA256 = hex.EncodeToString(responseDigest[:])
	var result struct {
		RequestID string          `json:"requestId"`
		Data      json.RawMessage `json:"data"`
	}
	if response.StatusCode != 200 {
		s.Outcome = "valid_operation_failed"
		return finish()
	}
	if json.Unmarshal(body, &result) != nil || result.RequestID != s.RequestID || !json.Valid(result.Data) {
		s.Outcome = "response_invalid"
		return finish()
	}
	s.Outcome = "accepted"
	return finish()
}

func (h SignedLoadHarness) Run(ctx context.Context, plan SignedLoadPlan, raw io.Writer) (out SignedLoadResult, err error) {
	out.Plan = plan
	out.GlobalAcceptanceDeferred = true
	out.MeasurementScope = "owned signed HTTP authorization and PostgreSQL lane subset"
	out.Origin = h.Endpoint
	out.ServerCertificateSHA256 = h.ServerCertificateSHA256
	out.RequirementIDs = []string{"D11_014.1", "D11_015.1", "D11_015.4", "D11_015.5", "D11_016.1", "D11_028.2", "D11_028.5"}
	if plan.ProofKind == "erc1271" {
		out.RequirementIDs = append(out.RequirementIDs, "D11_005.5")
	} else {
		out.RequirementIDs = append(out.RequirementIDs, "D11_004.5")
	}
	if raw == nil {
		return out, errors.New("authorization load: raw sample writer required")
	}
	if err = h.validate(plan); err != nil {
		return out, err
	}
	if err = h.Pool.QueryRow(ctx, `SELECT current_database(),current_schema(),version()`).Scan(&out.DatabaseName, &out.DatabaseSchema, &out.DatabaseVersion); err != nil {
		return out, err
	}
	out.Before, err = ReadAuthStateSnapshot(ctx, h.Pool)
	if err != nil {
		return out, err
	}
	out.StartedAt = time.Now().UTC()
	start := time.Now()
	recorder := &signedMeasurementRecorder{writer: raw, digest: sha256.New(), result: &out}
	type job struct {
		sequence  int64
		scheduled time.Time
	}
	jobs := make(chan job, plan.QueueCapacity)
	var workers sync.WaitGroup
	for index := 0; index < plan.Concurrency; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := range jobs {
				requestContext, cancel := context.WithTimeout(ctx, time.Duration(plan.RequestTimeoutMS)*time.Millisecond)
				sample := h.execute(requestContext, plan, j.sequence, j.scheduled, start)
				cancel()
				recorder.sample(sample)
			}
		}()
	}
	stopMonitor := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Duration(plan.SampleIntervalMS) * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopMonitor:
				return
			case <-ticker.C:
				observation := SignedLoadQueueSample{ObservedAt: time.Now().UTC(), GeneratorQueued: len(jobs), GeneratorCapacity: cap(jobs)}
				stats := h.Pool.Stat()
				observation.DatabaseAcquired = stats.AcquiredConns()
				observation.DatabaseMaximum = stats.MaxConns()
				observation.DatabaseCanceledAcquires = stats.CanceledAcquireCount()
				observation.DatabaseAcquireDurationNS = stats.AcquireDuration().Nanoseconds()
				bounded, cancel := context.WithTimeout(ctx, time.Duration(plan.RequestTimeoutMS)*time.Millisecond)
				bridge := AuditBridge{Pool: h.Pool}
				pending, age, e := bridge.Pending(bounded)
				missing, e2 := bridge.MissingOperationAudits(bounded)
				cancel()
				observation.PendingAudits = pending
				observation.OldestAuditAgeNS = age.Nanoseconds()
				observation.MissingOperationAudits = missing
				observation.ObservationError = e != nil || e2 != nil
				recorder.mu.Lock()
				recorder.write(observation)
				out.QueueObservations++
				if observation.GeneratorQueued > out.MaxGeneratorQueue {
					out.MaxGeneratorQueue = observation.GeneratorQueued
				}
				if pending > out.MaxPendingAudits {
					out.MaxPendingAudits = pending
				}
				if age.Nanoseconds() > out.MaxAuditAgeNS {
					out.MaxAuditAgeNS = age.Nanoseconds()
				}
				if observation.ObservationError {
					out.QueueObservationFailures++
				}
				recorder.mu.Unlock()
			}
		}
	}()
	total := int64(plan.DurationSeconds) * int64(plan.TargetRPS)
	for sequence := int64(0); sequence < total; sequence++ {
		scheduled := start.Add(time.Duration(sequence * int64(time.Second) / int64(plan.TargetRPS)))
		delay := time.Until(scheduled)
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
			}
		}
		if err != nil || ctx.Err() != nil {
			if err == nil {
				err = ctx.Err()
			}
			break
		}
		out.ScheduledRequests++
		select {
		case jobs <- job{sequence, scheduled}:
		default:
			now := time.Now()
			recorder.sample(SignedLoadSample{Sequence: sequence, RequestID: uuid.NewString(), ScheduledAt: scheduled.UTC(), StartedAt: now.UTC(), FinishedAt: now.UTC(), Outcome: "generator_saturated", QueueDelayNS: now.Sub(scheduled).Nanoseconds(), CorrectedLatencyNS: now.Sub(scheduled).Nanoseconds(), Warmup: scheduled.Before(start.Add(time.Duration(plan.WarmupSeconds) * time.Second))})
		}
	}
	close(jobs)
	workers.Wait()
	if remaining := time.Until(start.Add(time.Duration(plan.DurationSeconds) * time.Second)); remaining > 0 && ctx.Err() == nil {
		timer := time.NewTimer(remaining)
		select {
		case <-ctx.Done():
			timer.Stop()
			err = ctx.Err()
		case <-timer.C:
		}
	}
	close(stopMonitor)
	<-monitorDone
	out.FinishedAt = time.Now().UTC()
	out.SampleSHA256 = hex.EncodeToString(recorder.digest.Sum(nil))
	out.P95CorrectedNS = nearestRank(recorder.corrected, 95)
	out.P99CorrectedNS = nearestRank(recorder.corrected, 99)
	out.P95ServiceNS = nearestRank(recorder.service, 95)
	out.P99ServiceNS = nearestRank(recorder.service, 99)
	out.HistogramUpperBoundsNS = []int64{1000000, 2000000, 5000000, 10000000, 25000000, 50000000, 100000000, 150000000, 250000000, 400000000, 750000000, 1000000000, 2000000000, 5000000000, 10000000000, 30000000000, math.MaxInt64}
	out.HistogramCounts = make([]int64, len(out.HistogramUpperBoundsNS))
	for _, latency := range recorder.corrected {
		index := sort.Search(len(out.HistogramUpperBoundsNS), func(i int) bool { return latency <= out.HistogramUpperBoundsNS[i] })
		out.HistogramCounts[index]++
	}
	if out.MeasuredRequests > 0 {
		out.ValidOperationFailureRatio = float64(out.Failures) / float64(out.MeasuredRequests)
	} else {
		out.AssertionFailures = append(out.AssertionFailures, "no measured valid operations")
	}
	if out.ValidOperationFailureRatio > 0.001 {
		out.AssertionFailures = append(out.AssertionFailures, "valid operation failure ratio exceeds 0.1 percent")
	}
	if out.GeneratorRejected > 0 {
		out.AssertionFailures = append(out.AssertionFailures, "load generator saturated")
	}
	if out.ScheduledRequests != total {
		out.AssertionFailures = append(out.AssertionFailures, "workload interrupted")
	}
	if out.QueueObservations == 0 {
		out.AssertionFailures = append(out.AssertionFailures, "queue observation samples missing")
	}
	if out.QueueObservationFailures > 0 {
		out.AssertionFailures = append(out.AssertionFailures, "queue observations unavailable")
	}
	var snapshotErr error
	out.After, snapshotErr = ReadAuthStateSnapshot(ctx, h.Pool)
	if snapshotErr != nil {
		out.AssertionFailures = append(out.AssertionFailures, "final durable snapshot unavailable")
	}
	if snapshotErr == nil && out.After.MissingOperationAudits != 0 {
		out.AssertionFailures = append(out.AssertionFailures, "committed operation lacks accepted audit")
	}
	for index := 0; index < len(recorder.acknowledged); index += 1000 {
		end := index + 1000
		if end > len(recorder.acknowledged) {
			end = len(recorder.acknowledged)
		}
		var committed int64
		queryErr := h.Pool.QueryRow(ctx, `SELECT count(*) FROM operation_ledger l JOIN security_audit_outbox a ON a.request_id=l.request_id AND a.action=l.action AND a.result='accepted' WHERE l.request_id=ANY($1::uuid[])`, recorder.acknowledged[index:end]).Scan(&committed)
		if queryErr != nil || committed != int64(end-index) {
			out.AssertionFailures = append(out.AssertionFailures, "acknowledged result missing durable operation or audit")
			break
		}
	}
	var verificationErr error
	out.VerificationMeasurements, verificationErr = h.Verification.Snapshot()
	expectedMethod := "eip191"
	if plan.ProofKind == "erc1271" {
		expectedMethod = "eip1271"
	}
	if out.VerificationMeasurements[expectedMethod].Samples == 0 {
		out.AssertionFailures = append(out.AssertionFailures, "expected wallet proof method samples missing")
	}
	if verificationErr != nil {
		out.AssertionFailures = append(out.AssertionFailures, "verification duration samples missing or invalid")
	}
	if recorder.err != nil {
		out.AssertionFailures = append(out.AssertionFailures, "raw evidence write failed")
	}
	if err != nil || snapshotErr != nil || recorder.err != nil || len(out.AssertionFailures) > 0 {
		return out, errors.Join(err, snapshotErr, recorder.err, errors.New("authorization load: measured assertions failed"))
	}
	return out, nil
}

func CompareAuthRecovery(before, after AuthStateSnapshot) error {
	if before.ObservedAt.IsZero() || after.ObservedAt.Before(before.ObservedAt) || !validMeasurementDigest(before.LedgerSHA256) || !validMeasurementDigest(before.AuditSHA256) || before.LedgerRecords != after.LedgerRecords || before.LedgerSHA256 != after.LedgerSHA256 || before.AuditRecords != after.AuditRecords || before.AuditSHA256 != after.AuditSHA256 || before.MissingOperationAudits != 0 || after.MissingOperationAudits != 0 {
		return errors.New("authorization recovery: durable record count or checksum mismatch")
	}
	return nil
}

func MeasureIdentityRevocation(ctx context.Context, authority MTLSIdentityAuthority, principal StreamPrincipal, revokedAt time.Time) (time.Duration, error) {
	if revokedAt.IsZero() || revokedAt.After(time.Now()) || !validIdentityID(principal.AccountID) || !validIdentityID(principal.SessionID) {
		return 0, errors.New("authorization revocation: committed revocation timestamp required")
	}
	deadline := revokedAt.Add(5 * time.Second)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return time.Since(revokedAt), errors.New("authorization revocation: five second propagation bound exceeded")
		}
		bounded, cancel := context.WithTimeout(ctx, remaining)
		err := authority.CheckStreamSession(bounded, principal)
		cancel()
		if err != nil {
			var rejection *IdentityAuthorityError
			if !errors.As(err, &rejection) || rejection.Status != 401 {
				return time.Since(revokedAt), errors.New("authorization revocation: authority unavailable or unrelated rejection")
			}
			elapsed := time.Since(revokedAt)
			if elapsed > 5*time.Second {
				return elapsed, errors.New("authorization revocation: five second propagation bound exceeded")
			}
			return elapsed, nil
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return time.Since(revokedAt), ctx.Err()
		case <-timer.C:
		}
	}
}

func (r SignedLoadResult) WriteJSON(writer io.Writer) error {
	if writer == nil {
		return fmt.Errorf("authorization measurement: result writer required")
	}
	return json.NewEncoder(writer).Encode(r)
}

func MeasureLogoutRevocation(ctx context.Context, authority MTLSIdentityAuthority, principal StreamPrincipal, client *http.Client, logoutEndpoint, bearer string) (time.Duration, error) {
	if err := authority.validate(); err != nil {
		return 0, errors.New("authorization revocation: admitted mTLS identity authority required")
	}
	endpoint, err := url.Parse(logoutEndpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.Path != "/v2/auth/sessions:logout-all" || endpoint.RawQuery != "" || endpoint.User != nil || endpoint.Fragment != "" || client == nil || bearer == "" || len(bearer) > 2048 {
		return 0, errors.New("authorization revocation: actual logout endpoint/session credential required")
	}
	if err = authority.CheckStreamSession(ctx, principal); err != nil {
		return 0, errors.New("authorization revocation: principal not active before revocation")
	}
	started := time.Now()
	bounded, cancel := context.WithDeadline(ctx, started.Add(5*time.Second))
	defer cancel()
	request, err := http.NewRequestWithContext(bounded, "POST", logoutEndpoint, bytes.NewBufferString("{}"))
	if err != nil {
		return 0, err
	}
	request.Header.Set("Authorization", "Bearer "+bearer)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Request-ID", uuid.NewString())
	response, err := client.Do(request)
	if err != nil {
		return time.Since(started), errors.New("authorization revocation: logout transport failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return time.Since(started), errors.New("authorization revocation: logout not acknowledged")
	}
	return MeasureIdentityRevocation(bounded, authority, principal, started)
}

type PrivateSocketRevocationProbe struct {
	connection     *websocket.Conn
	subscriptionID string
	activeAt       time.Time
}

func PreparePrivateSocketRevocation(ctx context.Context, connection *websocket.Conn, subscriptionID, privateTopic string) (*PrivateSocketRevocationProbe, error) {
	deadline, bounded := ctx.Deadline()
	if !bounded || connection == nil || (len(subscriptionID) < 1 || len(subscriptionID) > 64) || !strings.HasPrefix(privateTopic, "private.") {
		return nil, errors.New("authorization revocation: actual private socket subscription and bounded preparation required")
	}
	if err := connection.SetReadDeadline(deadline); err != nil {
		return nil, err
	}
	connection.SetReadLimit(65536)
	accepted := false
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		kind, raw, err := connection.ReadMessage()
		if err != nil {
			return nil, err
		}
		if kind != websocket.TextMessage {
			return nil, errors.New("authorization revocation: invalid subscription frame")
		}
		var control struct {
			Op             string `json:"op"`
			SubscriptionID string `json:"subscriptionId"`
			Code           string `json:"code"`
			Cursor         string `json:"cursor"`
			Topics         []struct {
				Topic    string `json:"topic"`
				Accepted bool   `json:"accepted"`
			} `json:"topics"`
		}
		if json.Unmarshal(raw, &control) != nil {
			return nil, errors.New("authorization revocation: malformed subscription frame")
		}
		if control.Op == "error" || control.Op == "resnapshot_required" {
			return nil, errors.New("authorization revocation: socket did not become live")
		}
		if control.SubscriptionID != subscriptionID {
			continue
		}
		if control.Op == "subscribed" {
			for _, topic := range control.Topics {
				if topic.Topic == privateTopic && topic.Accepted {
					accepted = true
				}
			}
		}
		if accepted && control.Op == "checkpoint" && control.Code == "REPLAY_COMPLETE" && control.Cursor != "" {
			return &PrivateSocketRevocationProbe{connection: connection, subscriptionID: subscriptionID, activeAt: time.Now()}, nil
		}
	}
}

func MeasurePrivateSocketRevocation(ctx context.Context, probe *PrivateSocketRevocationProbe, revokedAt time.Time) (time.Duration, error) {
	if probe == nil || probe.connection == nil || probe.activeAt.IsZero() || revokedAt.Before(probe.activeAt) || revokedAt.IsZero() || revokedAt.After(time.Now()) {
		return 0, errors.New("authorization revocation: actual live private subscription before revocation required")
	}
	connection := probe.connection
	deadline := revokedAt.Add(5 * time.Second)
	if earlier, ok := ctx.Deadline(); ok && earlier.Before(deadline) {
		deadline = earlier
	}
	if err := connection.SetReadDeadline(deadline); err != nil {
		return 0, err
	}
	connection.SetReadLimit(65536)
	observedAuthDenial := false
	for {
		if err := ctx.Err(); err != nil {
			return time.Since(revokedAt), err
		}
		kind, raw, err := connection.ReadMessage()
		if err != nil {
			elapsed := time.Since(revokedAt)
			var network interface{ Timeout() bool }
			if observedAuthDenial && elapsed <= 5*time.Second && !(errors.As(err, &network) && network.Timeout()) {
				return elapsed, nil
			}
			return elapsed, errors.New("authorization revocation: socket closed without authentication denial or exceeded deadline")
		}
		if kind != websocket.TextMessage {
			return time.Since(revokedAt), errors.New("authorization revocation: invalid private control frame")
		}
		var control struct {
			Code           string `json:"code"`
			SubscriptionID string `json:"subscriptionId"`
		}
		if json.Unmarshal(raw, &control) != nil {
			return time.Since(revokedAt), errors.New("authorization revocation: malformed private control frame")
		}
		if control.Code == "AUTH_REQUIRED" && control.SubscriptionID == probe.subscriptionID {
			observedAuthDenial = true
		}
	}
}
