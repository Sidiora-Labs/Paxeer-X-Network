package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type IdentityMeasurementBinding struct {
	CandidateSHA256 string `json:"candidateSha256"`
	InterfaceSHA256 string `json:"interfaceSha256"`
	TopologySHA256  string `json:"topologySha256"`
	Region          string `json:"region"`
	RunID           string `json:"runId"`
}
type IdentityMeasurementClient struct {
	BaseURL string
	Origin  string
	Client  *http.Client
}
type IdentityMeasurementPhase struct {
	Name             string        `json:"name"`
	SignInRefreshRPS int           `json:"signInRefreshRps"`
	DynamicBudgetRPS int           `json:"dynamicBudgetRps"`
	WriteBudgetRPS   int           `json:"writeBudgetRps"`
	Duration         time.Duration `json:"durationNs"`
	Warmup           bool          `json:"warmup"`
}
type IdentityMeasurementConfig struct {
	Binding             IdentityMeasurementBinding
	Clients             []IdentityMeasurementClient
	Phases              []IdentityMeasurementPhase
	Workers             int
	QueueCapacity       int
	MaxAcknowledgements int
	RefreshesPerLogin   int
	SourceRoot          string
	RequestTimeout      time.Duration
	EvidenceDirectory   string
	Pool                *pgxpool.Pool
	Schema              string
	Runtime             *Identity
}
type IdentityRequestMeasurement struct {
	RequestID          string    `json:"requestId"`
	Phase              string    `json:"phase"`
	Operation          string    `json:"operation"`
	ScheduledAt        time.Time `json:"scheduledAt"`
	StartedAt          time.Time `json:"startedAt"`
	CompletedAt        time.Time `json:"completedAt"`
	LatencyNS          int64     `json:"latencyNs"`
	CorrectedLatencyNS int64     `json:"correctedLatencyNs"`
	QueueDelayNS       int64     `json:"queueDelayNs"`
	Status             int       `json:"status"`
	ExpectedStatus     int       `json:"expectedStatus"`
	RequestSHA256      string    `json:"requestSha256"`
	ResponseSHA256     string    `json:"responseSha256"`
	CorrelationID      string    `json:"correlationId"`
	Failure            string    `json:"failure"`
	Warmup             bool      `json:"warmup"`
}
type IdentityRuntimeMeasurement struct {
	CapturedAt                 time.Time `json:"capturedAt"`
	ProcessID                  int       `json:"processId"`
	ProcessCPUNS               int64     `json:"processCpuNs"`
	EOAVerifications           uint64    `json:"eoaVerifications"`
	EOAVerificationWallNS      uint64    `json:"eoaVerificationWallNs"`
	ContractVerifications      uint64    `json:"contractVerifications"`
	AdmissionUsed              int       `json:"admissionUsed"`
	AdmissionCapacity          int       `json:"admissionCapacity"`
	AdmissionObservedHighWater int64     `json:"admissionObservedHighWater"`
	AdmissionRejected          uint64    `json:"admissionRejected"`
	PoolAcquired               int32     `json:"poolAcquired"`
	PoolMax                    int32     `json:"poolMax"`
	PoolAcquireCount           int64     `json:"poolAcquireCount"`
	PoolAcquireNS              int64     `json:"poolAcquireNs"`
	PoolEmptyAcquireCount      int64     `json:"poolEmptyAcquireCount"`
	PoolCanceledAcquireCount   int64     `json:"poolCanceledAcquireCount"`
}

func (s *Identity) CaptureRuntimeMeasurement() (IdentityRuntimeMeasurement, error) {
	if s == nil || s.pool == nil {
		return IdentityRuntimeMeasurement{}, errors.New("identity measurement: runtime missing")
	}
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return IdentityRuntimeMeasurement{}, errors.New("identity measurement: process CPU unavailable")
	}
	stat := s.pool.Stat()
	return IdentityRuntimeMeasurement{CapturedAt: time.Now().UTC(), ProcessID: os.Getpid(), ProcessCPUNS: int64(usage.Utime.Sec+usage.Stime.Sec)*int64(time.Second) + int64(usage.Utime.Usec+usage.Stime.Usec)*1000, EOAVerifications: s.measureEOACount.Load(), EOAVerificationWallNS: s.measureEOAWallNS.Load(), ContractVerifications: s.measureContractCount.Load(), AdmissionUsed: len(s.admission), AdmissionCapacity: cap(s.admission), AdmissionObservedHighWater: s.measureAdmissionHighWater.Load(), AdmissionRejected: s.measureAdmissionRejected.Load(), PoolAcquired: stat.AcquiredConns(), PoolMax: stat.MaxConns(), PoolAcquireCount: stat.AcquireCount(), PoolAcquireNS: int64(stat.AcquireDuration()), PoolEmptyAcquireCount: stat.EmptyAcquireCount(), PoolCanceledAcquireCount: stat.CanceledAcquireCount()}, nil
}

type IdentityMeasurementSummary struct {
	Binding                     IdentityMeasurementBinding    `json:"binding"`
	StartedAt                   time.Time                     `json:"startedAt"`
	CompletedAt                 time.Time                     `json:"completedAt"`
	Phases                      []IdentityMeasurementPhase    `json:"phases"`
	PhaseElapsedNS              map[string]int64              `json:"phaseElapsedNs"`
	PhaseActualSignInRefreshRPS map[string]float64            `json:"phaseActualSignInRefreshRps"`
	Requests                    int64                         `json:"requests"`
	SuccessfulRequests          int64                         `json:"successfulRequests"`
	SuccessfulSignInRefresh     int64                         `json:"successfulSignInRefresh"`
	Admission429                int64                         `json:"admission429"`
	Dependency503               int64                         `json:"dependency503"`
	UnexpectedFailures          int64                         `json:"unexpectedFailures"`
	GeneratorRejected           int64                         `json:"generatorRejected"`
	P95NS                       int64                         `json:"p95Ns"`
	P99NS                       int64                         `json:"p99Ns"`
	CorrectedP95NS              int64                         `json:"correctedP95Ns"`
	CorrectedP99NS              int64                         `json:"correctedP99Ns"`
	GeneratorQueueCapacity      int                           `json:"generatorQueueCapacity"`
	GeneratorMaxQueueDepth      int                           `json:"generatorMaxQueueDepth"`
	RawFile                     string                        `json:"rawFile"`
	RawSHA256                   string                        `json:"rawSha256"`
	SourceSHA256                string                        `json:"sourceSha256"`
	SourceFiles                 map[string]string             `json:"sourceFiles"`
	CPUPerEOAProcessNS          *float64                      `json:"cpuPerEoaProcessNs"`
	SignatureWallMeanNS         *float64                      `json:"signatureWallMeanNs"`
	PoolAcquireMeanNS           *float64                      `json:"poolAcquireMeanNs"`
	CPUAttribution              string                        `json:"cpuAttribution"`
	RuntimeSnapshots            []IdentityRuntimeMeasurement  `json:"runtimeSnapshots"`
	Checkpoint                  *IdentityDurabilityCheckpoint `json:"checkpoint"`
	Assertions                  map[string]bool               `json:"assertions"`
	Failures                    []string                      `json:"failures"`
	FinalAcceptance             bool                          `json:"finalAcceptance"`
	FinalSharedGate             string                        `json:"finalSharedGate"`
}
type IdentityMeasurementRecorder struct {
	mu                  sync.Mutex
	file                *os.File
	digest              hash.Hash
	failure             error
	summary             IdentityMeasurementSummary
	latencies           identityLatencyHistogram
	corrected           identityLatencyHistogram
	acknowledged        []IdentityAcknowledgement
	phaseSuccessful     map[string]int64
	phaseRequests       map[string]int64
	phaseFailures       map[string]int64
	refreshesPerLogin   int
	maxAcknowledgements int
}

func measurementHash(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func (r *IdentityMeasurementRecorder) append(sample IdentityRequestMeasurement) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wire, err := json.Marshal(sample)
	if err == nil {
		wire = append(wire, '\n')
		_, err = r.file.Write(wire)
	}
	if err != nil {
		r.failure = errors.New("identity measurement: raw capture write failed")
		return
	}
	r.digest.Write(wire)
	if sample.Warmup {
		return
	}
	r.summary.Requests++
	r.phaseRequests[sample.Phase]++
	switch {
	case sample.Status == sample.ExpectedStatus && sample.Failure == "":
		r.summary.SuccessfulRequests++
		if sample.Operation == "login" || sample.Operation == "refresh" {
			r.summary.SuccessfulSignInRefresh++
			r.phaseSuccessful[sample.Phase]++
		}
		r.latencies.add(sample.LatencyNS)
		r.corrected.add(sample.CorrectedLatencyNS)
	case sample.Failure == "generator_queue_full":
		r.summary.GeneratorRejected++
		r.phaseFailures[sample.Phase]++
	case sample.Status == 429:
		r.summary.Admission429++
		r.phaseFailures[sample.Phase]++
	case sample.Status == 503:
		r.summary.Dependency503++
		r.summary.UnexpectedFailures++
		r.phaseFailures[sample.Phase]++
	default:
		r.summary.UnexpectedFailures++
		r.phaseFailures[sample.Phase]++
	}
}
func (r *IdentityMeasurementRecorder) request(ctx context.Context, c IdentityMeasurementClient, phase IdentityMeasurementPhase, operation, path, access, refresh, csrf string, data any, expected int, scheduled time.Time, timeout time.Duration) ([]byte, http.Header, error) {
	id := uuid.NewString()
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, nil, errors.New("identity measurement: request encoding failed")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, "POST", c.BaseURL+path, strings.NewReader(string(raw)))
	if err != nil {
		return nil, nil, errors.New("identity measurement: request construction failed")
	}
	req.Header.Set("Origin", c.Origin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", id)
	if operation == "challenge" {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	if refresh != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-kindle_refresh", Value: refresh})
		req.Header.Set("X-CSRF-Token", csrf)
	}
	start := time.Now().UTC()
	sample := IdentityRequestMeasurement{RequestID: id, Phase: phase.Name, Operation: operation, ScheduledAt: scheduled.UTC(), StartedAt: start, ExpectedStatus: expected, RequestSHA256: measurementHash(raw), Warmup: phase.Warmup, QueueDelayNS: max(int64(0), start.Sub(scheduled).Nanoseconds())}
	safeClient := *c.Client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, callErr := safeClient.Do(req)
	var body []byte
	var headers http.Header
	if callErr == nil {
		sample.Status = response.StatusCode
		headers = response.Header.Clone()
		sample.CorrelationID = headers.Get("X-Request-Id")
		body, callErr = io.ReadAll(io.LimitReader(response.Body, 65537))
		response.Body.Close()
		if len(body) > 65536 {
			callErr = errors.New("bounded response exceeded")
		}
	}
	done := time.Now().UTC()
	sample.CompletedAt = done
	sample.LatencyNS = done.Sub(start).Nanoseconds()
	sample.CorrectedLatencyNS = max(sample.LatencyNS, done.Sub(scheduled).Nanoseconds())
	sample.ResponseSHA256 = measurementHash(body)
	if callErr != nil {
		sample.Failure = "transport_or_response_unavailable"
	} else if sample.Status != expected {
		sample.Failure = "unexpected_status"
	} else if sample.CorrelationID != id {
		sample.Failure = "request_correlation_mismatch"
	}
	r.append(sample)
	if sample.Failure != "" {
		return nil, headers, errors.New("identity measurement: " + sample.Failure)
	}
	return body, headers, nil
}
func measurementProof(key *ecdsa.PrivateKey, challenge Challenge) (WalletProof, error) {
	sig, err := crypto.Sign(accounts.TextHash([]byte(challenge.Message)), key)
	if err != nil {
		return WalletProof{}, errors.New("identity measurement: signing failed")
	}
	sig[64] += 27
	return WalletProof{Kind: "eoa_eip191", Wallet: strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex()), Signature: "0x" + hex.EncodeToString(sig)}, nil
}
func (r *IdentityMeasurementRecorder) journey(ctx context.Context, c IdentityMeasurementClient, phase IdentityMeasurementPhase, due time.Time, timeout time.Duration) {
	key, err := crypto.GenerateKey()
	if err != nil {
		r.recordFailure(phase, "signer_unavailable")
		return
	}
	wallet := strings.ToLower(crypto.PubkeyToAddress(key.PublicKey).Hex())
	raw, _, err := r.request(ctx, c, phase, "challenge", "/v2/auth/challenges", "", "", "", ChallengeRequest{wallet, c.Origin}, 201, due, timeout)
	if err != nil {
		return
	}
	var challenge Envelope[struct {
		Challenge Challenge `json:"challenge"`
	}]
	if json.Unmarshal(raw, &challenge) != nil || challenge.Data.Challenge.Wallet != wallet || challenge.Data.Challenge.ChainID != "125" || challenge.Data.Challenge.Purpose != "login" || challenge.Data.Challenge.ExpiresAt.Sub(challenge.Data.Challenge.IssuedAt) != 300*time.Second {
		r.recordFailure(phase, "challenge_wire_invalid")
		return
	}
	canonical, err := BuildSIWE(challenge.Data.Challenge, "")
	if err != nil || canonical != challenge.Data.Challenge.Message {
		r.recordFailure(phase, "challenge_signing_bytes_invalid")
		return
	}
	proof, err := measurementProof(key, challenge.Data.Challenge)
	if err != nil {
		r.recordFailure(phase, "signer_unavailable")
		return
	}
	raw, headers, err := r.request(ctx, c, phase, "login", "/v2/auth/sessions", "", "", "", LoginRequest{challenge.Data.Challenge.ChallengeID, proof, "identity measurement"}, 200, time.Now(), timeout)
	if err != nil {
		return
	}
	var login Envelope[LoginData]
	if json.Unmarshal(raw, &login) != nil || !validUUID(login.Data.Session.SessionID) || login.Data.Session.Wallet != wallet || login.Data.AccessToken == "" {
		r.recordFailure(phase, "login_wire_invalid")
		return
	}
	r.mu.Lock()
	if len(r.acknowledged) >= r.maxAcknowledgements {
		r.summary.Failures = append(r.summary.Failures, "acknowledgement_capture_saturated")
		r.mu.Unlock()
		return
	}
	r.acknowledged = append(r.acknowledged, IdentityAcknowledgement{SessionID: login.Data.Session.SessionID, ChallengeID: challenge.Data.Challenge.ChallengeID})
	r.mu.Unlock()
	refresh, csrf := "", headers.Get("X-CSRF-Token")
	for _, cookie := range (&http.Response{Header: headers}).Cookies() {
		if cookie.Name == "__Host-kindle_refresh" {
			if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Domain != "" || cookie.Path != "/" {
				r.recordFailure(phase, "refresh_cookie_policy_invalid")
				return
			}
			refresh = cookie.Value
		}
	}
	if len(refresh) != 129 || len(csrf) != 64 {
		r.recordFailure(phase, "refresh_transport_missing")
		return
	}
	access := login.Data.AccessToken
	for index := 0; index < r.refreshesPerLogin; index++ {
		raw, nextHeaders, callErr := r.request(ctx, c, phase, "refresh", "/v2/auth/sessions:refresh", "", refresh, csrf, RefreshRequest{}, 200, time.Now(), timeout)
		if callErr != nil {
			return
		}
		var rotated Envelope[LoginData]
		if json.Unmarshal(raw, &rotated) != nil || rotated.Data.Session.SessionID != login.Data.Session.SessionID || rotated.Data.AccessToken == access || !rotated.Data.Session.RefreshExpiresAt.Equal(login.Data.Session.RefreshExpiresAt) {
			r.recordFailure(phase, "refresh_family_invalid")
			return
		}
		nextRefresh := ""
		for _, cookie := range (&http.Response{Header: nextHeaders}).Cookies() {
			if cookie.Name == "__Host-kindle_refresh" {
				nextRefresh = cookie.Value
			}
		}
		nextCSRF := nextHeaders.Get("X-CSRF-Token")
		if len(nextRefresh) != 129 || nextRefresh == refresh || len(nextCSRF) != 64 || nextCSRF == csrf {
			r.recordFailure(phase, "refresh_secret_rotation_invalid")
			return
		}
		refresh, csrf, access = nextRefresh, nextCSRF, rotated.Data.AccessToken
	}
	_, _, _ = r.request(ctx, c, phase, "logout", "/v2/auth/sessions:logout-all", access, "", "", RefreshRequest{}, 204, time.Now(), timeout)
}
func (r *IdentityMeasurementRecorder) recordFailure(phase IdentityMeasurementPhase, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.summary.Failures = append(r.summary.Failures, reason)
	if !phase.Warmup {
		r.summary.UnexpectedFailures++
		r.phaseFailures[phase.Name]++
	}
}
func validateIdentityMeasurement(c IdentityMeasurementConfig) error {
	for _, v := range []string{c.Binding.CandidateSHA256, c.Binding.InterfaceSHA256, c.Binding.TopologySHA256} {
		b, e := hex.DecodeString(v)
		if e != nil || len(b) != 32 || strings.ToLower(v) != v {
			return errors.New("identity measurement: exact candidate/interface/topology digest required")
		}
	}
	if !validUUID(c.Binding.RunID) || c.Binding.Region == "" || strings.ContainsAny(c.Binding.Region, "\r\n") || len(c.Clients) == 0 || c.Pool == nil || !measurementSchema.MatchString(c.Schema) || c.Workers < 1 || c.Workers > 4096 || c.QueueCapacity < 1 || c.QueueCapacity > 8192 || c.RequestTimeout <= 0 || c.RequestTimeout > 5*time.Second || c.EvidenceDirectory == "" || c.SourceRoot == "" || c.MaxAcknowledgements < 1 || c.MaxAcknowledgements > 1000000 || c.RefreshesPerLogin < 1 || c.RefreshesPerLogin > 99 || len(c.Phases) == 0 {
		return errors.New("identity measurement: explicit providers and bounded workload required")
	}
	names := map[string]bool{}
	var acknowledgedBound int64
	for _, p := range c.Phases {
		if p.Name == "" || names[p.Name] || p.Duration <= 0 || p.Duration > 24*time.Hour || p.SignInRefreshRPS < 2 || p.SignInRefreshRPS > 3000 || p.SignInRefreshRPS%(c.RefreshesPerLogin+1) != 0 || p.DynamicBudgetRPS < p.SignInRefreshRPS*(c.RefreshesPerLogin+3)/(c.RefreshesPerLogin+1) || p.WriteBudgetRPS < p.SignInRefreshRPS*(c.RefreshesPerLogin+3)/(c.RefreshesPerLogin+1) || p.DynamicBudgetRPS > 20000 {
			return errors.New("identity measurement: invalid workload or additive request budget")
		}
		names[p.Name] = true
		acknowledgedBound += int64(p.Duration) * int64(p.SignInRefreshRPS) / (int64(time.Second) * int64(c.RefreshesPerLogin+1))
	}
	if acknowledgedBound > int64(c.MaxAcknowledgements) {
		return errors.New("identity measurement: admitted capture bound below offered workload")
	}
	for _, client := range c.Clients {
		u, e := url.Parse(client.BaseURL)
		o, oe := url.Parse(client.Origin)
		if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) || oe != nil || o.Scheme != "https" || o.Host == "" || o.User != nil || o.Path != "" || o.RawQuery != "" || o.Fragment != "" || client.Client == nil || client.Client.CheckRedirect == nil {
			return errors.New("identity measurement: reviewed HTTP transport and HTTPS origin required")
		}
	}
	return nil
}
func RunIdentityMeasurement(ctx context.Context, c IdentityMeasurementConfig) (IdentityMeasurementSummary, error) {
	if err := validateIdentityMeasurement(c); err != nil {
		return IdentityMeasurementSummary{}, err
	}
	sourceFiles, sourceSHA, sourceErr := identityMeasurementSources(c.SourceRoot)
	if sourceErr != nil {
		return IdentityMeasurementSummary{}, sourceErr
	}
	if err := os.MkdirAll(c.EvidenceDirectory, 0700); err != nil {
		return IdentityMeasurementSummary{}, errors.New("identity measurement: evidence directory unavailable")
	}
	file, err := os.OpenFile(filepath.Join(c.EvidenceDirectory, "requests.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return IdentityMeasurementSummary{}, errors.New("identity measurement: immutable raw capture already exists or unavailable")
	}
	defer file.Close()
	r := &IdentityMeasurementRecorder{file: file, digest: sha256.New(), summary: IdentityMeasurementSummary{Binding: c.Binding, StartedAt: time.Now().UTC(), Phases: append([]IdentityMeasurementPhase(nil), c.Phases...), GeneratorQueueCapacity: c.QueueCapacity, RawFile: "requests.jsonl", Assertions: map[string]bool{}, Failures: []string{}, FinalSharedGate: "9.1", PhaseElapsedNS: map[string]int64{}, PhaseActualSignInRefreshRPS: map[string]float64{}, SourceSHA256: sourceSHA, SourceFiles: sourceFiles, CPUAttribution: "Whole owning-process CPU delta per EOA verification; includes other process work and is not exclusive signature CPU."}, phaseSuccessful: map[string]int64{}, phaseRequests: map[string]int64{}, phaseFailures: map[string]int64{}, refreshesPerLogin: c.RefreshesPerLogin, maxAcknowledgements: c.MaxAcknowledgements}
	if c.Runtime != nil {
		if snapshot, e := c.Runtime.CaptureRuntimeMeasurement(); e == nil {
			r.summary.RuntimeSnapshots = append(r.summary.RuntimeSnapshots, snapshot)
		} else {
			r.summary.Failures = append(r.summary.Failures, "runtime_measurement_unavailable")
		}
	}
	for _, phase := range c.Phases {
		stopSampling := make(chan struct{})
		samplingDone := make(chan struct{})
		go func() {
			defer close(samplingDone)
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-stopSampling:
					return
				case <-ticker.C:
					if c.Runtime != nil {
						snapshot, e := c.Runtime.CaptureRuntimeMeasurement()
						r.mu.Lock()
						if e != nil {
							r.summary.Failures = append(r.summary.Failures, "runtime_measurement_unavailable")
						} else if len(r.summary.RuntimeSnapshots) < 86410 {
							r.summary.RuntimeSnapshots = append(r.summary.RuntimeSnapshots, snapshot)
						} else {
							r.summary.Failures = append(r.summary.Failures, "runtime_capture_saturated")
						}
						r.mu.Unlock()
					}
				}
			}
		}()
		type work struct {
			due    time.Time
			client int
		}
		jobs := make(chan work, c.QueueCapacity)
		var wg sync.WaitGroup
		for worker := 0; worker < c.Workers; worker++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range jobs {
					r.journey(ctx, c.Clients[job.client], phase, job.due, c.RequestTimeout)
				}
			}()
		}
		start := time.Now()
		count := int64(phase.Duration) * int64(phase.SignInRefreshRPS) / (int64(time.Second) * int64(c.RefreshesPerLogin+1))
		for index := int64(0); index < count; index++ {
			due := start.Add(time.Duration(index) * time.Second * time.Duration(c.RefreshesPerLogin+1) / time.Duration(phase.SignInRefreshRPS))
			timer := time.NewTimer(max(time.Duration(0), time.Until(due)))
			select {
			case <-ctx.Done():
				timer.Stop()
				r.recordFailure(phase, "workload_canceled")
				index = count
				continue
			case <-timer.C:
			}
			select {
			case jobs <- work{due, int(index % int64(len(c.Clients)))}:
				r.mu.Lock()
				r.summary.GeneratorMaxQueueDepth = max(r.summary.GeneratorMaxQueueDepth, len(jobs))
				r.mu.Unlock()
			default:
				now := time.Now().UTC()
				r.append(IdentityRequestMeasurement{RequestID: uuid.NewString(), Phase: phase.Name, Operation: "generator_admission", ScheduledAt: due.UTC(), StartedAt: now, CompletedAt: now, QueueDelayNS: max(int64(0), now.Sub(due).Nanoseconds()), Failure: "generator_queue_full", Warmup: phase.Warmup})
			}
		}
		if remaining := time.Until(start.Add(phase.Duration)); remaining > 0 && ctx.Err() == nil {
			timer := time.NewTimer(remaining)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
			}
		}
		close(jobs)
		wg.Wait()
		close(stopSampling)
		<-samplingDone
		r.summary.PhaseElapsedNS[phase.Name] = time.Since(start).Nanoseconds()
		if ctx.Err() != nil {
			break
		}
	}
	r.summary.CompletedAt = time.Now().UTC()
	if c.Runtime != nil {
		if snapshot, e := c.Runtime.CaptureRuntimeMeasurement(); e == nil {
			r.summary.RuntimeSnapshots = append(r.summary.RuntimeSnapshots, snapshot)
		} else {
			r.summary.Failures = append(r.summary.Failures, "runtime_measurement_unavailable")
		}
	}
	if err = file.Sync(); err != nil {
		r.failure = errors.New("identity measurement: capture sync failed")
	}
	r.summary.RawSHA256 = hex.EncodeToString(r.digest.Sum(nil))
	r.summary.P95NS = r.latencies.percentile(95)
	r.summary.P99NS = r.latencies.percentile(99)
	r.summary.CorrectedP95NS = r.corrected.percentile(95)
	r.summary.CorrectedP99NS = r.corrected.percentile(99)
	r.summary.Assertions["D11_029.2_latency"] = r.latencies.count > 0 && r.summary.P95NS <= int64(150*time.Millisecond) && r.summary.P99NS <= int64(400*time.Millisecond) && r.summary.CorrectedP95NS <= int64(150*time.Millisecond) && r.summary.CorrectedP99NS <= int64(400*time.Millisecond)
	r.summary.Assertions["generator_unsaturated"] = r.summary.GeneratorRejected == 0 && ctx.Err() == nil
	r.summary.Assertions["steady_error_budget"] = r.summary.Requests > 0 && r.summary.UnexpectedFailures*1000 <= r.summary.Requests
	fullSteady, fullBurst := false, false
	for _, phase := range c.Phases {
		if phase.Warmup {
			continue
		}
		achieved := float64(r.phaseSuccessful[phase.Name]) / (float64(r.summary.PhaseElapsedNS[phase.Name]) / float64(time.Second))
		r.summary.PhaseActualSignInRefreshRPS[phase.Name] = achieved
		r.summary.Assertions["achieved_load_"+phase.Name] = achieved >= float64(phase.SignInRefreshRPS)
		r.summary.Assertions["offered_load_"+phase.Name] = r.phaseSuccessful[phase.Name] == int64(phase.Duration)*int64(phase.SignInRefreshRPS)/int64(time.Second) && r.phaseFailures[phase.Name] == 0
		if phase.SignInRefreshRPS == 1000 && phase.DynamicBudgetRPS == 10000 && phase.WriteBudgetRPS == 2000 {
			fullSteady = true
		}
		if phase.SignInRefreshRPS == 3000 && phase.DynamicBudgetRPS == 20000 && phase.WriteBudgetRPS == 4000 && phase.Duration == 600*time.Second {
			fullBurst = true
		}
	}
	r.summary.Assertions["configured_D11_AUTH_PROFILE"] = fullSteady && fullBurst
	r.summary.Assertions["AZ_loss_recovery"] = false
	r.summary.Assertions["event_replay_integrity"] = false
	r.summary.Assertions["private_socket_revocation_p99"] = false
	r.summary.Assertions["runtime_telemetry_present"] = len(r.summary.RuntimeSnapshots) >= 2
	if len(r.summary.RuntimeSnapshots) >= 2 {
		a, b := r.summary.RuntimeSnapshots[0], r.summary.RuntimeSnapshots[len(r.summary.RuntimeSnapshots)-1]
		if a.ProcessID == b.ProcessID && b.EOAVerifications > a.EOAVerifications && b.ProcessCPUNS >= a.ProcessCPUNS && b.ContractVerifications == a.ContractVerifications {
			value := float64(b.ProcessCPUNS-a.ProcessCPUNS) / float64(b.EOAVerifications-a.EOAVerifications)
			r.summary.CPUPerEOAProcessNS = &value
			wall := float64(b.EOAVerificationWallNS-a.EOAVerificationWallNS) / float64(b.EOAVerifications-a.EOAVerifications)
			r.summary.SignatureWallMeanNS = &wall
		}
		if b.PoolAcquireCount > a.PoolAcquireCount {
			value := float64(b.PoolAcquireNS-a.PoolAcquireNS) / float64(b.PoolAcquireCount-a.PoolAcquireCount)
			r.summary.PoolAcquireMeanNS = &value
		}
	}
	_, sourceAfter, sourceErr := identityMeasurementSources(c.SourceRoot)
	r.summary.Assertions["source_unchanged"] = sourceErr == nil && sourceAfter == sourceSHA
	checkpoint, e := CaptureIdentityDurability(ctx, c.Pool, c.Schema, c.Binding, r.acknowledged)
	if e != nil {
		r.summary.Failures = append(r.summary.Failures, "acknowledged_mutation_checkpoint_unavailable")
	} else {
		r.summary.Checkpoint = &checkpoint
	}
	r.summary.Assertions["acknowledged_rows_durable"] = e == nil && len(r.acknowledged) > 0
	for assertion, passed := range r.summary.Assertions {
		if !passed {
			r.summary.Failures = append(r.summary.Failures, assertion)
		}
	}
	if r.failure != nil {
		r.summary.Failures = append(r.summary.Failures, "raw_capture_incomplete")
	}
	if err := writeIdentityImmutableJSON(filepath.Join(c.EvidenceDirectory, "summary.json"), r.summary); err != nil {
		return r.summary, err
	}
	if len(r.summary.Failures) > 0 {
		return r.summary, errors.New("identity measurement: explicit assertions failed; final shared acceptance remains pending")
	}
	return r.summary, nil
}
func identityPercentile(values []int64, p int) int64 {
	if len(values) == 0 || p < 1 || p > 100 {
		return 0
	}
	v := append([]int64(nil), values...)
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[int(math.Ceil(float64(len(v))*float64(p)/100))-1]
}
func writeIdentityImmutableJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return errors.New("identity measurement: evidence encoding failed")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("identity measurement: immutable evidence unavailable")
	}
	defer f.Close()
	if _, err = f.Write(append(raw, '\n')); err == nil {
		err = f.Sync()
	}
	if err != nil {
		return errors.New("identity measurement: evidence persistence failed")
	}
	return nil
}

var measurementSchema = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

type IdentityAcknowledgement struct {
	SessionID   string `json:"sessionId"`
	ChallengeID string `json:"challengeId"`
}
type IdentityDurableRecord struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}
type IdentityDurabilityCheckpoint struct {
	Binding       IdentityMeasurementBinding `json:"binding"`
	CapturedAt    time.Time                  `json:"capturedAt"`
	Database      string                     `json:"database"`
	Schema        string                     `json:"schema"`
	Acknowledged  []IdentityAcknowledgement  `json:"acknowledged"`
	RecordDigests map[string]string          `json:"recordDigests"`
	Counts        map[string]int             `json:"counts"`
	SHA256        string                     `json:"sha256"`
}

func CaptureIdentityDurability(ctx context.Context, pool *pgxpool.Pool, schema string, binding IdentityMeasurementBinding, acknowledged []IdentityAcknowledgement) (IdentityDurabilityCheckpoint, error) {
	out := IdentityDurabilityCheckpoint{Binding: binding, CapturedAt: time.Now().UTC(), Schema: schema, Acknowledged: append([]IdentityAcknowledgement(nil), acknowledged...), RecordDigests: map[string]string{}, Counts: map[string]int{}}
	if pool == nil || !measurementSchema.MatchString(schema) || len(acknowledged) == 0 {
		return out, errors.New("identity durability: acknowledged input and real PostgreSQL required")
	}
	ids := []string{}
	challenges := []string{}
	seen := map[string]bool{}
	sort.Slice(out.Acknowledged, func(i, j int) bool { return out.Acknowledged[i].SessionID < out.Acknowledged[j].SessionID })
	for _, a := range acknowledged {
		if !validUUID(a.SessionID) || !validUUID(a.ChallengeID) || seen[a.SessionID] {
			return out, errors.New("identity durability: invalid or repeated receipt")
		}
		seen[a.SessionID] = true
		ids = append(ids, a.SessionID)
		challenges = append(challenges, a.ChallengeID)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, errors.New("identity durability: snapshot unavailable")
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'; SET LOCAL datestyle='ISO,YMD'`); err != nil {
		return out, errors.New("identity durability: canonical database serialization unavailable")
	}
	if err = tx.QueryRow(ctx, `SELECT current_database()`).Scan(&out.Database); err != nil {
		return out, errors.New("identity durability: database identity unavailable")
	}
	prefix := pgx.Identifier{schema}.Sanitize() + "."
	rows, err := tx.Query(ctx, `SELECT session_id::text,account_id::text FROM `+prefix+`identity_sessions WHERE session_id=ANY($1::uuid[]) ORDER BY session_id`, ids)
	if err != nil {
		return out, errors.New("identity durability: session read unavailable")
	}
	accounts := []string{}
	found := 0
	for rows.Next() {
		var id, account string
		if err = rows.Scan(&id, &account); err != nil {
			rows.Close()
			return out, errors.New("identity durability: invalid session record")
		}
		accounts = append(accounts, account)
		found++
	}
	rows.Close()
	if rows.Err() != nil || found != len(ids) {
		return out, errors.New("identity durability: acknowledged session missing")
	}
	queries := []struct {
		table, id, predicate string
		arg                  any
	}{
		{"identity_accounts", "account_id::text", "account_id=ANY($1::uuid[])", accounts},
		{"identity_sessions", "session_id::text", "session_id=ANY($1::uuid[])", ids},
		{"identity_wallet_links", "link_id::text", "account_id=ANY($1::uuid[])", accounts},
		{"identity_challenges", "challenge_id::text", "challenge_id=ANY($1::uuid[])", challenges},
		{"identity_refresh", "encode(refresh_hash,'hex')", "session_id=ANY($1::uuid[])", ids},
		{"identity_outbox", "event_id::text", "account_id=ANY($1::uuid[])", accounts},
		{"identity_revisions", "account_id::text", "account_id=ANY($1::uuid[])", accounts},
	}
	for _, q := range queries {
		ledgerHash := sha256.New()
		rows, err = tx.Query(ctx, `SELECT `+q.id+`,row_to_json(t)::text FROM `+prefix+q.table+` t WHERE `+q.predicate+` ORDER BY 1`, q.arg)
		if err != nil {
			return out, errors.New("identity durability: owned ledger read unavailable")
		}
		for rows.Next() {
			var id, raw string
			if err = rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return out, errors.New("identity durability: record unavailable")
			}
			record, _ := json.Marshal(IdentityDurableRecord{q.table, measurementHash([]byte(id)), measurementHash([]byte(raw))})
			ledgerHash.Write(record)
			ledgerHash.Write([]byte{'\n'})
			out.Counts[q.table]++
		}
		rows.Close()
		if rows.Err() != nil {
			return out, errors.New("identity durability: ledger iteration failed")
		}
		out.RecordDigests[q.table] = hex.EncodeToString(ledgerHash.Sum(nil))
	}
	var consumedCount int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM `+prefix+`identity_challenges c JOIN unnest($1::uuid[],$2::uuid[]) AS a(session_id,challenge_id) ON a.challenge_id=c.challenge_id JOIN `+prefix+`identity_sessions s ON s.session_id=a.session_id AND s.wallet=c.wallet WHERE c.consumed_at IS NOT NULL AND c.consumption_id IS NOT NULL`, ids, challenges).Scan(&consumedCount); err != nil || consumedCount != len(ids) {
		return out, errors.New("identity durability: acknowledged challenge/session binding missing")
	}
	if out.Counts["identity_challenges"] != len(challenges) || out.Counts["identity_refresh"] < len(ids) || out.Counts["identity_outbox"] < len(ids) {
		return out, errors.New("identity durability: acknowledged ledger incomplete")
	}
	deliveryHash := sha256.New()
	rows, err = tx.Query(ctx, `SELECT event_id,envelope FROM `+prefix+`delivery_outbox WHERE convert_from(envelope,'UTF8')::jsonb->'payload'->>'accountId'=ANY($1::text[]) ORDER BY event_id`, accounts)
	if err != nil {
		return out, errors.New("identity durability: durable delivery read unavailable")
	}
	for rows.Next() {
		var id string
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return out, errors.New("identity durability: delivery record unavailable")
		}
		record, _ := json.Marshal(IdentityDurableRecord{"delivery_outbox", measurementHash([]byte(id)), measurementHash(raw)})
		deliveryHash.Write(record)
		deliveryHash.Write([]byte{'\n'})
		out.Counts["delivery_outbox"]++
	}
	rows.Close()
	if rows.Err() != nil || out.Counts["delivery_outbox"] < len(ids) {
		return out, errors.New("identity durability: acknowledged delivery absent")
	}
	out.RecordDigests["delivery_outbox"] = hex.EncodeToString(deliveryHash.Sum(nil))
	out.SHA256 = identityDurabilityDigest(out)
	if err = tx.Commit(ctx); err != nil {
		return out, errors.New("identity durability: checkpoint read commit unavailable")
	}
	return out, nil
}
func CompareIdentityRecovery(before, after IdentityDurabilityCheckpoint) error {
	if before.Binding != after.Binding || before.Database != after.Database || before.Schema != after.Schema || len(before.RecordDigests) == 0 || !after.CapturedAt.After(before.CapturedAt) {
		return errors.New("identity recovery: checkpoint environment mismatch")
	}
	for _, snapshot := range []IdentityDurabilityCheckpoint{before, after} {
		if identityDurabilityDigest(snapshot) != snapshot.SHA256 {
			return errors.New("identity recovery: checkpoint checksum mismatch")
		}
	}
	if before.SHA256 != after.SHA256 {
		return errors.New("identity recovery: acknowledged record checksum changed")
	}
	a, _ := json.Marshal(before.Counts)
	b, _ := json.Marshal(after.Counts)
	if string(a) != string(b) {
		return errors.New("identity recovery: record counts changed")
	}
	return nil
}

type IdentityReplayMeasurement struct {
	Binding     IdentityMeasurementBinding `json:"binding"`
	StartedAt   time.Time                  `json:"startedAt"`
	CompletedAt time.Time                  `json:"completedAt"`
	Fetched     int                        `json:"fetched"`
	Committed   int                        `json:"committed"`
	InboxCount  int64                      `json:"inboxCount"`
	InboxSHA256 string                     `json:"inboxSha256"`
	Failure     string                     `json:"failure"`
}

func (s *Identity) MeasureIdentityReplay(ctx context.Context, nc *nats.Conn, cfg natsdriver.ConsumerConfig, projection, generation string, expected int, binding IdentityMeasurementBinding) (IdentityReplayMeasurement, error) {
	out := IdentityReplayMeasurement{Binding: binding, StartedAt: time.Now().UTC()}
	if s == nil || s.events == nil || nc == nil || expected < 1 || expected > 1000000 || projection == "" || generation == "" || cfg.Subject != IdentityRevisionSubject {
		return out, errors.New("identity replay measurement: explicit real broker and owned projection required")
	}
	if _, ok := ctx.Deadline(); !ok {
		return out, errors.New("identity replay measurement: bounded recovery deadline required")
	}
	consumer, err := natsdriver.NewConsumer(ctx, nc, cfg)
	if err != nil {
		return out, errors.New("identity replay measurement: real durable consumer unavailable")
	}
	defer consumer.Close()
	records := map[string]string{}
	for out.Committed < expected {
		messages, err := consumer.Fetch(ctx)
		if err != nil {
			out.Failure = "broker_fetch_failed"
			return out, errors.New("identity replay measurement: fetch failed")
		}
		for _, message := range messages {
			out.Fetched++
			before, e := s.identityReplayRecord(ctx, message.EventID)
			if e != nil {
				out.Failure = "committed_replay_baseline_absent"
				return out, e
			}
			if err = s.ConsumeIdentityRevision(ctx, projection, generation, message.Subject, message.Wire, message); err != nil {
				out.Failure = "projection_commit_failed"
				return out, errors.New("identity replay measurement: actual commit failed")
			}
			after, e := s.identityReplayRecord(ctx, message.EventID)
			if e != nil || before != after {
				out.Failure = "replay_changed_committed_record"
				return out, errors.New("identity replay measurement: duplicate replay changed committed projection")
			}
			records[measurementHash([]byte(message.EventID))] = after
			out.Committed++
		}
	}
	raw, err := json.Marshal(records)
	if err != nil {
		return out, errors.New("identity replay measurement: digest unavailable")
	}
	out.InboxCount = int64(len(records))
	out.InboxSHA256 = measurementHash(raw)
	out.CompletedAt = time.Now().UTC()
	return out, nil
}
func (s *Identity) identityReplayRecord(ctx context.Context, event string) (string, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT jsonb_build_object('inbox',row_to_json(i),'projection',row_to_json(r)) FROM identity_revision_inbox i JOIN identity_remote_revisions r USING(account_id) WHERE i.event_id=$1`, event).Scan(&raw)
	if err != nil {
		return "", errors.New("identity replay measurement: real committed inbox/projection absent")
	}
	return measurementHash(raw), nil
}
func WriteIdentityRecoveryEvidence(path string, before, after IdentityDurabilityCheckpoint, faultReceiptPath, faultAuthoritySHA256 string, faultStarted, faultEnded time.Time) error {
	raw, err := hex.DecodeString(faultAuthoritySHA256)
	if err != nil || len(raw) != 32 || !faultStarted.After(before.CapturedAt) || !faultEnded.After(faultStarted) || !after.CapturedAt.After(faultEnded) {
		return errors.New("identity recovery: reviewed external fault authority and ordered actual checkpoints required")
	}
	info, err := os.Lstat(faultReceiptPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 4*1024*1024 {
		return errors.New("identity recovery: actual approved fault receipt unavailable")
	}
	receipt, err := os.ReadFile(faultReceiptPath)
	if err != nil || measurementHash(receipt) != faultAuthoritySHA256 {
		return errors.New("identity recovery: fault receipt digest mismatch")
	}
	failure := CompareIdentityRecovery(before, after)
	result := struct {
		Before             IdentityDurabilityCheckpoint `json:"before"`
		After              IdentityDurabilityCheckpoint `json:"after"`
		FaultReceiptSHA256 string                       `json:"faultReceiptSha256"`
		FaultStarted       time.Time                    `json:"faultStarted"`
		FaultEnded         time.Time                    `json:"faultEnded"`
		DurabilityPassed   bool                         `json:"durabilityPassed"`
		AZLossAcceptance   bool                         `json:"azLossAcceptance"`
		Failure            string                       `json:"failure"`
	}{before, after, faultAuthoritySHA256, faultStarted, faultEnded, failure == nil, false, ""}
	if failure != nil {
		result.Failure = "durability_or_environment_mismatch"
	}
	if err = writeIdentityImmutableJSON(path, result); err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	return nil
}
func (s IdentityMeasurementSummary) String() string {
	return fmt.Sprintf("identity measurement: requests=%d success=%d failures=%d final_acceptance=%t", s.Requests, s.SuccessfulRequests, len(s.Failures), s.FinalAcceptance)
}

type identityLatencyHistogram struct {
	buckets  [60001]int64
	count    int64
	overflow int64
	maximum  int64
}

func (h *identityLatencyHistogram) add(value int64) {
	if value < 0 {
		value = 0
	}
	h.count++
	h.maximum = max(h.maximum, value)
	bucket := (value + 9999) / 10000
	if bucket > 60000 {
		h.overflow++
		return
	}
	h.buckets[bucket]++
}
func (h *identityLatencyHistogram) percentile(p int) int64 {
	if h.count == 0 || p < 1 || p > 100 {
		return 0
	}
	target := (h.count*int64(p) + 99) / 100
	var count int64
	for index, n := range h.buckets {
		count += n
		if count >= target {
			return int64(index) * 10000
		}
	}
	return h.maximum
}
func identityMeasurementSources(root string) (map[string]string, string, error) {
	sources := map[string]string{}
	directory := filepath.Join(root, "media/gateway/internal/auth")
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink source rejected")
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		if len(sources) >= 1000 {
			return errors.New("source bound exceeded")
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		relative, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		sources[filepath.ToSlash(relative)] = measurementHash(raw)
		return nil
	})
	if err != nil || len(sources) == 0 {
		return nil, "", errors.New("identity measurement: actual source binding unavailable")
	}
	raw, e := json.Marshal(sources)
	if e != nil {
		return nil, "", errors.New("identity measurement: source digest unavailable")
	}
	return sources, measurementHash(raw), nil
}

func identityDurabilityDigest(c IdentityDurabilityCheckpoint) string {
	raw, err := json.Marshal(struct {
		Binding          IdentityMeasurementBinding
		Database, Schema string
		Acknowledged     []IdentityAcknowledgement
		Counts           map[string]int
		Digests          map[string]string
	}{c.Binding, c.Database, c.Schema, c.Acknowledged, c.Counts, c.RecordDigests})
	if err != nil {
		return ""
	}
	return measurementHash(raw)
}

type IdentityRevocationProbe struct {
	Client      IdentityMeasurementClient
	AccessToken string
	Socket      *websocket.Conn
}
type IdentityRevocationObservation struct {
	RequestID       string    `json:"requestId"`
	HTTPDeniedAt    time.Time `json:"httpDeniedAt"`
	SocketClosedAt  time.Time `json:"socketClosedAt"`
	UpperBoundLagNS int64     `json:"upperBoundLagNs"`
	Failure         string    `json:"failure"`
}
type IdentityRevocationMeasurement struct {
	Binding         IdentityMeasurementBinding      `json:"binding"`
	LogoutSentAt    time.Time                       `json:"logoutSentAt"`
	AcknowledgedAt  time.Time                       `json:"acknowledgedAt"`
	Observations    []IdentityRevocationObservation `json:"observations"`
	P99UpperBoundNS int64                           `json:"p99UpperBoundNs"`
	Passed          bool                            `json:"passed"`
}

func (s *Identity) MeasureRevocationPropagation(ctx context.Context, actor IdentityRevocationProbe, probes []IdentityRevocationProbe, binding IdentityMeasurementBinding) (IdentityRevocationMeasurement, error) {
	out := IdentityRevocationMeasurement{Binding: binding, Observations: []IdentityRevocationObservation{}}
	bounded, cancelMeasurement := context.WithTimeout(ctx, 10*time.Second)
	defer cancelMeasurement()
	ctx = bounded
	if s == nil || s.tokens == nil || actor.Client.Client == nil || len(probes) < 2 || len(probes) > 1000 {
		return out, errors.New("identity revocation measurement: two real device/socket probes required")
	}
	actorClient, err := identityMeasurementSafeClient(actor.Client)
	if err != nil {
		return out, err
	}
	actor.Client.Client = actorClient
	validatedProbes := append([]IdentityRevocationProbe(nil), probes...)
	for index := range validatedProbes {
		safe, e := identityMeasurementSafeClient(validatedProbes[index].Client)
		if e != nil {
			return out, e
		}
		validatedProbes[index].Client.Client = safe
	}
	probes = validatedProbes
	owner, err := s.AuthenticateJWT(ctx, s.tokens, actor.AccessToken)
	if err != nil {
		return out, errors.New("identity revocation measurement: actor not authoritative")
	}
	sessions := map[string]bool{}
	for _, probe := range probes {
		if probe.Client.Client == nil || probe.Socket == nil {
			return out, errors.New("identity revocation measurement: real private socket missing")
		}
		p, e := s.AuthenticateJWT(ctx, s.tokens, probe.AccessToken)
		if e != nil || p.AccountID() != owner.AccountID() || sessions[p.SessionID()] {
			return out, errors.New("identity revocation measurement: distinct authorized device sessions required")
		}
		sessions[p.SessionID()] = true
	}
	request, err := http.NewRequestWithContext(ctx, "POST", actor.Client.BaseURL+"/v2/auth/sessions:logout-all", strings.NewReader(`{}`))
	if err != nil {
		return out, errors.New("identity revocation measurement: logout request unavailable")
	}
	request.Header.Set("Origin", actor.Client.Origin)
	request.Header.Set("Authorization", "Bearer "+actor.AccessToken)
	request.Header.Set("X-Request-Id", uuid.NewString())
	out.LogoutSentAt = time.Now().UTC()
	response, err := actor.Client.Client.Do(request)
	if err != nil {
		return out, errors.New("identity revocation measurement: logout not acknowledged")
	}
	io.Copy(io.Discard, io.LimitReader(response.Body, 65537))
	response.Body.Close()
	out.AcknowledgedAt = time.Now().UTC()
	if response.StatusCode != 204 {
		return out, errors.New("identity revocation measurement: logout not committed")
	}
	deadline := out.LogoutSentAt.Add(5 * time.Second)
	results := make(chan IdentityRevocationObservation, len(probes))
	for _, probe := range probes {
		go func(probe IdentityRevocationProbe) {
			observation := IdentityRevocationObservation{RequestID: uuid.NewString()}
			socketClosed := make(chan time.Time, 1)
			socketError := make(chan error, 1)
			go func() {
				if e := probe.Socket.SetReadDeadline(deadline); e != nil {
					socketError <- e
					return
				}
				for {
					_, _, e := probe.Socket.ReadMessage()
					if e != nil {
						if websocket.IsCloseError(e, websocket.ClosePolicyViolation) {
							socketClosed <- time.Now().UTC()
						} else {
							socketError <- e
						}
						return
					}
				}
			}()
			for time.Now().Before(deadline) && observation.HTTPDeniedAt.IsZero() {
				bounded, cancel := context.WithDeadline(ctx, deadline)
				req, e := http.NewRequestWithContext(bounded, "GET", probe.Client.BaseURL+"/v2/auth/sessions", nil)
				if e != nil {
					cancel()
					observation.Failure = "http_probe_construction_failed"
					break
				}
				req.Header.Set("Origin", probe.Client.Origin)
				req.Header.Set("Authorization", "Bearer "+probe.AccessToken)
				req.Header.Set("X-Request-Id", observation.RequestID)
				result, e := probe.Client.Client.Do(req)
				if e != nil {
					cancel()
					observation.Failure = "http_authority_unavailable"
					break
				}
				io.Copy(io.Discard, io.LimitReader(result.Body, 65537))
				result.Body.Close()
				cancel()
				if result.StatusCode == 401 {
					observation.HTTPDeniedAt = time.Now().UTC()
					break
				}
				if result.StatusCode != 200 {
					observation.Failure = "unexpected_http_authority_status"
					break
				}
				timer := time.NewTimer(20 * time.Millisecond)
				select {
				case <-timer.C:
				case <-ctx.Done():
					timer.Stop()
					observation.Failure = "revocation_probe_canceled"
				}
			}
			wait, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			select {
			case observation.SocketClosedAt = <-socketClosed:
			case <-socketError:
				observation.Failure = "private_socket_close_not_authoritative"
			case <-wait.Done():
				observation.Failure = "revocation_deadline_exceeded"
			}
			if observation.HTTPDeniedAt.IsZero() || observation.SocketClosedAt.IsZero() {
				observation.Failure = "private_authority_not_revoked"
			} else {
				observed := observation.HTTPDeniedAt
				if observation.SocketClosedAt.After(observed) {
					observed = observation.SocketClosedAt
				}
				observation.UpperBoundLagNS = observed.Sub(out.LogoutSentAt).Nanoseconds()
			}
			results <- observation
		}(probe)
	}
	latencies := []int64{}
	failed := false
	for range probes {
		observation := <-results
		out.Observations = append(out.Observations, observation)
		if observation.Failure != "" || observation.UpperBoundLagNS > int64(5*time.Second) {
			failed = true
		}
		latencies = append(latencies, observation.UpperBoundLagNS)
	}
	out.P99UpperBoundNS = identityPercentile(latencies, 99)
	out.Passed = !failed && out.P99UpperBoundNS <= int64(5*time.Second)
	if !out.Passed {
		return out, errors.New("identity revocation measurement: authoritative propagation exceeded requirement")
	}
	return out, nil
}

type IdentityPreferenceContinuityMeasurement struct {
	Binding            IdentityMeasurementBinding `json:"binding"`
	AccountSHA256      string                     `json:"accountSha256"`
	PreferencesSHA256  string                     `json:"preferencesSha256"`
	ProofHistorySHA256 string                     `json:"proofHistorySha256"`
	VerifiedWallets    int                        `json:"verifiedWallets"`
	CapturedAt         time.Time                  `json:"capturedAt"`
	Passed             bool                       `json:"passed"`
}

func (s *Identity) MeasurePreferenceContinuity(ctx context.Context, client IdentityMeasurementClient, accessA, accessB string, binding IdentityMeasurementBinding) (IdentityPreferenceContinuityMeasurement, error) {
	out := IdentityPreferenceContinuityMeasurement{Binding: binding}
	if s == nil || s.tokens == nil || client.Client == nil {
		return out, errors.New("identity preference measurement: actual account service and identity required")
	}
	safe, e := identityMeasurementSafeClient(client)
	if e != nil {
		return out, e
	}
	client.Client = safe
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = bounded
	a, err := s.AuthenticateJWT(ctx, s.tokens, accessA)
	if err != nil {
		return out, errors.New("identity preference measurement: first wallet unauthorized")
	}
	b, err := s.AuthenticateJWT(ctx, s.tokens, accessB)
	if err != nil || a.AccountID() != b.AccountID() || a.Wallet() == b.Wallet() {
		return out, errors.New("identity preference measurement: two distinct verified wallets of one account required")
	}
	hashes := []string{}
	for _, token := range []string{accessA, accessB} {
		req, e := http.NewRequestWithContext(ctx, "GET", client.BaseURL+"/v3/me/preferences", nil)
		if e != nil {
			return out, errors.New("identity preference measurement: request unavailable")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Origin", client.Origin)
		req.Header.Set("X-Request-Id", uuid.NewString())
		safeClient := *client.Client
		safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, e := safeClient.Do(req)
		if e != nil {
			return out, errors.New("identity preference measurement: actual service unavailable")
		}
		raw, e := io.ReadAll(io.LimitReader(response.Body, 65537))
		response.Body.Close()
		if e != nil || len(raw) > 65536 || response.StatusCode != 200 {
			return out, errors.New("identity preference measurement: preference read failed")
		}
		var envelope struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data["preferences"]) == 0 {
			return out, errors.New("identity preference measurement: actual preference record absent")
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(envelope.Data["preferences"])))
		decoder.UseNumber()
		if decoder.Decode(&value) != nil {
			return out, errors.New("identity preference measurement: invalid preference wire")
		}
		canonical, e := json.Marshal(value)
		if e != nil {
			return out, errors.New("identity preference measurement: preference digest unavailable")
		}
		hashes = append(hashes, measurementHash(canonical))
	}
	if hashes[0] != hashes[1] {
		return out, errors.New("identity preference measurement: wallet switch changed preference record")
	}
	rows, err := s.pool.Query(ctx, `SELECT link_id::text,linked_at FROM identity_wallet_links WHERE account_id=$1 AND wallet=ANY($2::text[]) AND state='verified' AND chain_id=125 ORDER BY link_id`, a.AccountID(), []string{a.Wallet(), b.Wallet()})
	if err != nil {
		return out, errors.New("identity preference measurement: proof history unavailable")
	}
	defer rows.Close()
	history := sha256.New()
	for rows.Next() {
		var id string
		var at time.Time
		if rows.Scan(&id, &at) != nil || at.IsZero() {
			return out, errors.New("identity preference measurement: verified proof timestamp absent")
		}
		raw, _ := json.Marshal([]string{id, wireTime(at)})
		history.Write(raw)
		history.Write([]byte{'\n'})
		out.VerifiedWallets++
	}
	if rows.Err() != nil || out.VerifiedWallets != 2 {
		return out, errors.New("identity preference measurement: distinct verified proof records absent")
	}
	out.AccountSHA256 = measurementHash([]byte(a.AccountID()))
	out.PreferencesSHA256 = hashes[0]
	out.ProofHistorySHA256 = hex.EncodeToString(history.Sum(nil))
	out.CapturedAt = time.Now().UTC()
	out.Passed = true
	return out, nil
}

func identityMeasurementSafeClient(c IdentityMeasurementClient) (*http.Client, error) {
	u, err := url.Parse(c.BaseURL)
	origin, oe := url.Parse(c.Origin)
	if c.Client == nil || err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) || oe != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("identity measurement: admitted secure endpoint/origin required")
	}
	safe := *c.Client
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &safe, nil
}
