package capabilities

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
)

type ConsumerBuildIdentity struct {
	BuildSHA256          string            `json:"buildSha256"`
	CandidateSHA256      string            `json:"candidateSha256"`
	ManifestSHA256       string            `json:"manifestSha256"`
	SourceManifestSHA256 string            `json:"sourceManifestSha256"`
	RevisionSHA256       string            `json:"revisionSha256"`
	NetworkSHA256        string            `json:"networkSha256"`
	Addresses            map[string]string `json:"addresses"`
}

type MeasurementCase struct {
	Kind             string           `json:"kind"`
	BlockHash        common.Hash      `json:"blockHash"`
	BlockNumber      uint64           `json:"blockNumber"`
	Request          PreflightRequest `json:"request"`
	OpticalRequest   PreflightRequest `json:"opticalRequest"`
	ReceiptHashes    []common.Hash    `json:"receiptHashes,omitempty"`
	TreasuryIdentity deployment.Role  `json:"treasuryIdentity"`
}

type MeasurementPlan struct {
	SchemaVersion         int                        `json:"schemaVersion"`
	CandidateSHA256       string                     `json:"candidateSha256"`
	HarnessBuildSHA256    string                     `json:"harnessBuildSha256"`
	AuthoritySHA256       string                     `json:"authoritySha256"`
	PolicySHA256          string                     `json:"policySha256"`
	GoBuild               ConsumerBuildIdentity      `json:"goBuild"`
	FrontendBuild         ConsumerBuildIdentity      `json:"frontendBuild"`
	PublicEndpoints       deployment.PublicEndpoints `json:"publicEndpoints"`
	SamplesPerCase        int                        `json:"samplesPerCase"`
	Concurrency           int                        `json:"concurrency"`
	DeadlineMilliseconds  int64                      `json:"deadlineMilliseconds"`
	P95BudgetMicroseconds int64                      `json:"p95BudgetMicroseconds"`
	MaxErrorBasisPoints   int                        `json:"maxErrorBasisPoints"`
	MaxRawBytes           int64                      `json:"maxRawBytes"`
	Cases                 []MeasurementCase          `json:"cases"`
}

type MeasurementSample struct {
	Index              int                               `json:"index"`
	Kind               string                            `json:"kind"`
	StartedAt          time.Time                         `json:"startedAt"`
	ElapsedNanoseconds int64                             `json:"elapsedNanoseconds"`
	Passed             bool                              `json:"passed"`
	Issue              *deployment.Issue                 `json:"issue,omitempty"`
	Attestation        *deployment.ActivationAttestation `json:"attestation,omitempty"`
	Publication        *CapabilityPublication            `json:"publication,omitempty"`
	Preflight          *Preflight                        `json:"preflight,omitempty"`
	OpticalPreflight   *Preflight                        `json:"opticalPreflight,omitempty"`
	Fees               []CreationFeeReconciliation       `json:"fees,omitempty"`
}

type MeasurementReport struct {
	SchemaVersion                         int                 `json:"schemaVersion"`
	CandidateSHA256                       string              `json:"candidateSha256"`
	HarnessBuildSHA256                    string              `json:"harnessBuildSha256"`
	AuthoritySHA256                       string              `json:"authoritySha256"`
	PlanSHA256                            string              `json:"planSha256"`
	PlanFileSHA256                        string              `json:"planFileSha256,omitempty"`
	ObservationFormat                     string              `json:"observationFormat"`
	PolicySHA256                          string              `json:"policySha256"`
	Revision                              deployment.Revision `json:"revision"`
	StartedAt                             time.Time           `json:"startedAt"`
	FinishedAt                            time.Time           `json:"finishedAt"`
	ElapsedNanoseconds                    int64               `json:"elapsedNanoseconds"`
	CompletedSamples                      int                 `json:"completedSamples"`
	SupportedPreflightSamples             int                 `json:"supportedPreflightSamples"`
	SupportedPreflightThroughputNumerator string              `json:"supportedPreflightThroughputNumerator"`
	RawReadCount                          uint64              `json:"rawReadCount"`
	ThroughputScope                       string              `json:"throughputScope"`
	SharedCapacityQualified               bool                `json:"sharedCapacityQualified"`
	FailedSamples                         int                 `json:"failedSamples"`
	P50Nanoseconds                        int64               `json:"p50Nanoseconds"`
	P95Nanoseconds                        int64               `json:"p95Nanoseconds"`
	P99Nanoseconds                        int64               `json:"p99Nanoseconds"`
	ThroughputNumerator                   string              `json:"throughputNumerator"`
	ThroughputDenominator                 string              `json:"throughputDenominator"`
	RawSHA256                             string              `json:"rawSha256"`
	RawBytes                              int64               `json:"rawBytes"`
	Coverage                              map[string][]int    `json:"coverage"`
	Samples                               []MeasurementSample `json:"samples"`
	Blockers                              []deployment.Issue  `json:"blockers"`
	MeasurementPassed                     bool                `json:"measurementPassed"`
	ReleaseReady                          bool                `json:"releaseReady"`
}

var measurementKinds = []string{"exports", "pending-governance", "unknown-implementation", "no-oracle", "quotes", "creation-fees", "zero-real-sell", "private-export", "quoter-revoked"}
var measurementRequirements = map[string]string{"010": "exports", "011": "pending-governance", "012": "unknown-implementation", "023": "no-oracle", "024": "quotes", "025": "creation-fees", "026": "zero-real-sell", "029": "private-export", "030": "quoter-revoked"}

func validMeasurementDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	bytes, err := hex.DecodeString(value)
	return err == nil && len(bytes) == 32 && value != strings.Repeat("0", 64)
}

func (p MeasurementPlan) Validate(e *PreflightEngine) error {
	if e == nil || e.reader == nil || p.SchemaVersion != 1 || !validMeasurementDigest(p.CandidateSHA256) || !validMeasurementDigest(p.HarnessBuildSHA256) || !validMeasurementDigest(p.AuthoritySHA256) || !validMeasurementDigest(p.PolicySHA256) || p.SamplesPerCase < 1 || p.SamplesPerCase > 1000 || p.Concurrency < 1 || p.Concurrency > 16 || p.DeadlineMilliseconds < 1 || p.DeadlineMilliseconds > 1800000 || p.P95BudgetMicroseconds < 1 || p.P95BudgetMicroseconds > 1800000000 || p.MaxErrorBasisPoints < 0 || p.MaxErrorBasisPoints > 10000 || p.MaxRawBytes < 1 || p.MaxRawBytes > 64<<20 || len(p.Cases) != len(measurementKinds) {
		return deployment.Issue{Code: "measurement_inputs_invalid"}
	}
	for _, consumer := range []ConsumerBuildIdentity{p.GoBuild, p.FrontendBuild} {
		if !validMeasurementDigest(consumer.BuildSHA256) || consumer.CandidateSHA256 != p.CandidateSHA256 || !validMeasurementDigest(consumer.NetworkSHA256) || consumer.ManifestSHA256 != e.revision.ManifestSHA256 || consumer.SourceManifestSHA256 != e.manifest.SourceManifestSHA256 || consumer.RevisionSHA256 != e.revision.RevisionSHA256 {
			return deployment.Issue{Code: "consumer_build_identity_mismatch"}
		}
		if err := e.manifest.CompareAddresses(consumer.Addresses); err != nil {
			return err
		}
	}
	if p.GoBuild.NetworkSHA256 != p.FrontendBuild.NetworkSHA256 {
		return deployment.Issue{Code: "consumer_network_identity_mismatch"}
	}
	seen := map[string]bool{}
	for _, probe := range p.Cases {
		known := false
		for _, kind := range measurementKinds {
			if probe.Kind == kind {
				known = true
			}
		}
		if !known || seen[probe.Kind] || probe.BlockHash == (common.Hash{}) || probe.BlockNumber < e.manifest.ActivationHeight {
			return deployment.Issue{Code: "measurement_case_invalid"}
		}
		seen[probe.Kind] = true
		if probe.Kind == "quotes" || probe.Kind == "zero-real-sell" || probe.Kind == "no-oracle" {
			if probe.Request.BlockHash != probe.BlockHash || probe.Request.ExpectedRevision != e.revision.RevisionSHA256 || probe.Request.Family != PlainQuote || probe.Request.Pool == (common.Address{}) || probe.Request.Trader == (common.Address{}) || probe.Request.Recipient == (common.Address{}) {
				return deployment.Issue{Code: "measurement_quote_context_invalid"}
			}
		}
		if probe.Kind == "quotes" && (probe.OpticalRequest.BlockHash != probe.BlockHash || probe.OpticalRequest.ExpectedRevision != e.revision.RevisionSHA256 || probe.OpticalRequest.Family != PlainQuote || probe.OpticalRequest.Pool == (common.Address{}) || probe.OpticalRequest.Pool == probe.Request.Pool || probe.OpticalRequest.Trader != probe.Request.Trader || probe.OpticalRequest.Recipient != probe.Request.Recipient) {
			return deployment.Issue{Code: "measurement_optical_context_missing"}
		}
		if probe.Kind == "creation-fees" && (len(probe.ReceiptHashes) != 2 || probe.ReceiptHashes[0] == (common.Hash{}) || probe.ReceiptHashes[1] == (common.Hash{}) || probe.ReceiptHashes[0] == probe.ReceiptHashes[1]) {
			return deployment.Issue{Code: "creation_receipt_inputs_missing"}
		}
	}
	return nil
}

type measurementScopeKey struct{}
type measurementScope struct {
	Index int    `json:"index"`
	Kind  string `json:"kind"`
}

type rawMeasurement struct {
	Sample             *measurementScope `json:"sample,omitempty"`
	Sequence           uint64            `json:"sequence"`
	Method             string            `json:"method"`
	Context            any               `json:"context"`
	StartedAt          time.Time         `json:"startedAt"`
	ElapsedNanoseconds int64             `json:"elapsedNanoseconds"`
	Result             json.RawMessage   `json:"result,omitempty"`
	Encoding           string            `json:"encoding"`
	Issue              *deployment.Issue `json:"issue,omitempty"`
}

type measurementCapture struct {
	reader   deployment.Reader
	writer   io.Writer
	hash     io.Writer
	maxBytes int64
	bytes    int64
	sequence uint64
	mu       sync.Mutex
	err      error
	cancel   context.CancelFunc
}

func measurementIssue(err error) deployment.Issue {
	var issue deployment.Issue
	if errors.As(err, &issue) {
		return issue
	}
	var unavailable Unavailable
	if errors.As(err, &unavailable) {
		return deployment.Issue{Code: string(unavailable.ReasonCode)}
	}
	if errors.Is(err, context.Canceled) {
		return deployment.Issue{Code: "measurement_canceled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return deployment.Issue{Code: "measurement_deadline_exceeded"}
	}
	return deployment.Issue{Code: "measurement_read_unavailable"}
}

func captureRead[T any](ctx context.Context, r *measurementCapture, method string, args any, read func() (T, error)) (T, error) {
	r.mu.Lock()
	priorErr := r.err
	r.mu.Unlock()
	if priorErr != nil {
		var zero T
		return zero, priorErr
	}
	start := time.Now()
	result, err := read()
	record := rawMeasurement{Method: method, Context: args, StartedAt: start.UTC(), ElapsedNanoseconds: time.Since(start).Nanoseconds(), Encoding: "json"}
	if scope, ok := ctx.Value(measurementScopeKey{}).(measurementScope); ok {
		record.Sample = &scope
	}
	if err != nil {
		issue := measurementIssue(err)
		record.Issue = &issue
	} else {
		if block, ok := any(result).(*types.Block); ok && block != nil {
			encoded, encodeErr := rlp.EncodeToBytes(block)
			if encodeErr != nil {
				err = encodeErr
			} else {
				record.Result, err = json.Marshal(encoded)
				record.Encoding = "ethereum_block_rlp_base64"
			}
		} else {
			record.Result, err = json.Marshal(result)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		var zero T
		return zero, r.err
	}
	record.Sequence = r.sequence + 1
	encoded, encodeErr := json.Marshal(record)
	if encodeErr != nil {
		r.err = deployment.Issue{Code: "measurement_capture_encoding_failed"}
	} else {
		encoded = append(encoded, '\n')
		if r.bytes+int64(len(encoded)) > r.maxBytes {
			r.err = deployment.Issue{Code: "measurement_raw_budget_exceeded"}
		} else {
			written, writeErr := r.writer.Write(encoded)
			if written > 0 {
				_, _ = r.hash.Write(encoded[:written])
				r.bytes += int64(written)
			}
			if writeErr != nil || written != len(encoded) {
				r.err = deployment.Issue{Code: "measurement_capture_write_failed"}
			} else {
				r.sequence++
			}
		}
	}
	if r.err != nil {
		if r.cancel != nil {
			r.cancel()
		}
		var zero T
		return zero, r.err
	}
	return result, err
}

func (r *measurementCapture) ChainID(ctx context.Context) (*big.Int, error) {
	return captureRead(ctx, r, "eth_chainId", nil, func() (*big.Int, error) { return r.reader.ChainID(ctx) })
}
func (r *measurementCapture) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return captureRead(ctx, r, "eth_getBlockByHash", hash, func() (*types.Header, error) { return r.reader.HeaderByHash(ctx, hash) })
}

// HeaderByReportedHash forwards the node-vouched header lookup so
// attestation under measurement keeps using the node-reported identity.
func (r *measurementCapture) HeaderByReportedHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return captureRead(ctx, r, "eth_getBlockByHash_reported", hash, func() (*types.Header, error) {
		reader, ok := r.reader.(deployment.ReportedHeaderReader)
		if !ok {
			return nil, deployment.Issue{Code: "reported_header_provider_missing"}
		}
		return reader.HeaderByReportedHash(ctx, hash)
	})
}

type reportedHeader struct {
	Header *types.Header `json:"header"`
	Hash   common.Hash   `json:"hash"`
}

// ReportedHeaderByNumber returns the header at number and the hash the node
// reports for it (the canonical identity on chain 125).
func (r *measurementCapture) ReportedHeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, common.Hash, error) {
	out, err := captureRead(ctx, r, "eth_getBlockByNumber", number, func() (reportedHeader, error) {
		reader, ok := r.reader.(ReportedHeaderByNumberReader)
		if !ok {
			return reportedHeader{}, deployment.Issue{Code: "canonical_header_provider_missing"}
		}
		header, hash, err := reader.ReportedHeaderByNumber(ctx, number)
		return reportedHeader{header, hash}, err
	})
	return out.Header, out.Hash, err
}
func (r *measurementCapture) heightReader() (HeightReader, error) {
	reader, ok := r.reader.(HeightReader)
	if !ok {
		return nil, deployment.Issue{Code: "height_reader_missing"}
	}
	return reader, nil
}
func (r *measurementCapture) CodeAt(ctx context.Context, address common.Address, number *big.Int) ([]byte, error) {
	return captureRead(ctx, r, "eth_getCode", []any{address, number}, func() ([]byte, error) {
		reader, err := r.heightReader()
		if err != nil {
			return nil, err
		}
		return reader.CodeAt(ctx, address, number)
	})
}
func (r *measurementCapture) StorageAt(ctx context.Context, address common.Address, slot common.Hash, number *big.Int) ([]byte, error) {
	return captureRead(ctx, r, "eth_getStorageAt", []any{address, slot, number}, func() ([]byte, error) {
		reader, err := r.heightReader()
		if err != nil {
			return nil, err
		}
		return reader.StorageAt(ctx, address, slot, number)
	})
}
func (r *measurementCapture) CallContract(ctx context.Context, msg ethereum.CallMsg, number *big.Int) ([]byte, error) {
	return captureRead(ctx, r, "eth_call", []any{msg, number}, func() ([]byte, error) {
		reader, err := r.heightReader()
		if err != nil {
			return nil, err
		}
		return reader.CallContract(ctx, msg, number)
	})
}
func (r *measurementCapture) CodeAtHash(ctx context.Context, address common.Address, hash common.Hash) ([]byte, error) {
	return captureRead(ctx, r, "eth_getCode", []any{address, hash}, func() ([]byte, error) { return r.reader.CodeAtHash(ctx, address, hash) })
}
func (r *measurementCapture) StorageAtHash(ctx context.Context, address common.Address, slot, hash common.Hash) ([]byte, error) {
	return captureRead(ctx, r, "eth_getStorageAt", []any{address, slot, hash}, func() ([]byte, error) { return r.reader.StorageAtHash(ctx, address, slot, hash) })
}
func (r *measurementCapture) CallContractAtHash(ctx context.Context, msg ethereum.CallMsg, hash common.Hash) ([]byte, error) {
	return captureRead(ctx, r, "eth_call", []any{msg, hash}, func() ([]byte, error) { return r.reader.CallContractAtHash(ctx, msg, hash) })
}
func (r *measurementCapture) EstimateGasAtHash(ctx context.Context, msg ethereum.CallMsg, hash common.Hash) (uint64, error) {
	return captureRead(ctx, r, "eth_estimateGas", []any{msg, hash}, func() (uint64, error) {
		reader, ok := r.reader.(GasReader)
		if !ok {
			return 0, deployment.Issue{Code: "pinned_gas_provider_missing"}
		}
		return reader.EstimateGasAtHash(ctx, msg, hash)
	})
}
func (r *measurementCapture) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	return captureRead(ctx, r, "eth_gasPrice", nil, func() (*big.Int, error) {
		reader, ok := r.reader.(GasReader)
		if !ok {
			return nil, deployment.Issue{Code: "gas_price_provider_missing"}
		}
		return reader.SuggestGasPrice(ctx)
	})
}
func (r *measurementCapture) ReportedBlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return captureRead(ctx, r, "eth_getBlockByHash_full", hash, func() (*types.Block, error) {
		reader, ok := r.reader.(CreationReceiptReader)
		if !ok {
			return nil, deployment.Issue{Code: "receipt_provider_missing"}
		}
		return reader.ReportedBlockByHash(ctx, hash)
	})
}
func (r *measurementCapture) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	return captureRead(ctx, r, "eth_getTransactionReceipt", hash, func() (*types.Receipt, error) {
		reader, ok := r.reader.(CreationReceiptReader)
		if !ok {
			return nil, deployment.Issue{Code: "receipt_provider_missing"}
		}
		return reader.TransactionReceipt(ctx, hash)
	})
}

func (e *PreflightEngine) Measure(ctx context.Context, plan MeasurementPlan, raw io.Writer) (MeasurementReport, error) {
	report := MeasurementReport{SchemaVersion: 1, ObservationFormat: "typed_ethereum_reads_jsonl_v1", ThroughputScope: "completed_measurement_cases_per_second", CandidateSHA256: plan.CandidateSHA256, HarnessBuildSHA256: plan.HarnessBuildSHA256, AuthoritySHA256: plan.AuthoritySHA256, PolicySHA256: plan.PolicySHA256, Coverage: map[string][]int{}, Samples: []MeasurementSample{}, Blockers: []deployment.Issue{}}
	if err := plan.Validate(e); err != nil {
		issue := measurementIssue(err)
		report.Blockers = append(report.Blockers, issue)
		return report, err
	}
	if raw == nil {
		return report, deployment.Issue{Code: "measurement_capture_missing"}
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return report, err
	}
	planHash := sha256.Sum256(encoded)
	report.PlanSHA256 = hex.EncodeToString(planHash[:])
	report.Revision = e.revision
	hash := sha256.New()
	capture := &measurementCapture{reader: e.reader, writer: raw, hash: hash, maxBytes: plan.MaxRawBytes}
	measured := *e
	measured.reader = capture
	ctx, cancel := context.WithTimeout(ctx, time.Duration(plan.DeadlineMilliseconds)*time.Millisecond)
	defer cancel()
	capture.cancel = cancel
	started := time.Now()
	report.StartedAt = started.UTC()
	jobs := make(chan int)
	results := make(chan MeasurementSample, plan.Concurrency)
	var workers sync.WaitGroup
	for i := 0; i < plan.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				probe := plan.Cases[index%len(plan.Cases)]
				sampleContext := context.WithValue(ctx, measurementScopeKey{}, measurementScope{Index: index, Kind: probe.Kind})
				sample := measured.measureCase(sampleContext, plan, probe, capture)
				sample.Index = index
				results <- sample
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := 0; i < len(plan.Cases)*plan.SamplesPerCase; i++ {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { workers.Wait(); close(results) }()
	for sample := range results {
		report.Samples = append(report.Samples, sample)
		if !sample.Passed {
			report.FailedSamples++
		}
	}
	report.FinishedAt = time.Now().UTC()
	report.ElapsedNanoseconds = time.Since(started).Nanoseconds()
	report.CompletedSamples = len(report.Samples)
	sort.Slice(report.Samples, func(i, j int) bool { return report.Samples[i].Index < report.Samples[j].Index })
	durations := make([]int64, 0, len(report.Samples))
	for _, sample := range report.Samples {
		durations = append(durations, sample.ElapsedNanoseconds)
		if sample.Preflight != nil && sample.Preflight.Supported {
			report.SupportedPreflightSamples++
		}
		if sample.Passed {
			for req, kind := range measurementRequirements {
				if kind == sample.Kind {
					for clause := 1; clause <= 5; clause++ {
						id := "D01_" + req + "." + big.NewInt(int64(clause)).String()
						report.Coverage[id] = append(report.Coverage[id], sample.Index)
					}
				}
			}
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	report.P50Nanoseconds = measurementPercentile(durations, 50)
	report.P95Nanoseconds = measurementPercentile(durations, 95)
	report.P99Nanoseconds = measurementPercentile(durations, 99)
	report.SupportedPreflightThroughputNumerator = new(big.Int).Mul(big.NewInt(int64(report.SupportedPreflightSamples)), big.NewInt(1000000000)).String()
	report.ThroughputNumerator = new(big.Int).Mul(big.NewInt(int64(report.CompletedSamples)), big.NewInt(1000000000)).String()
	report.ThroughputDenominator = big.NewInt(report.ElapsedNanoseconds).String()
	capture.mu.Lock()
	report.RawReadCount = capture.sequence
	report.RawBytes = capture.bytes
	report.RawSHA256 = hex.EncodeToString(hash.Sum(nil))
	captureErr := capture.err
	capture.mu.Unlock()
	if captureErr != nil {
		report.Blockers = append(report.Blockers, measurementIssue(captureErr))
	}
	if report.CompletedSamples != len(plan.Cases)*plan.SamplesPerCase {
		report.Blockers = append(report.Blockers, deployment.Issue{Code: "measurement_incomplete"})
	}
	if report.P95Nanoseconds > plan.P95BudgetMicroseconds*1000 {
		report.Blockers = append(report.Blockers, deployment.Issue{Code: "measurement_latency_budget_exceeded"})
	}
	if int64(report.FailedSamples)*10000 > int64(report.CompletedSamples)*int64(plan.MaxErrorBasisPoints) {
		report.Blockers = append(report.Blockers, deployment.Issue{Code: "measurement_error_budget_exceeded"})
	}
	if len(report.Coverage) != 45 {
		report.Blockers = append(report.Blockers, deployment.Issue{Code: "measurement_coverage_incomplete"})
	}
	report.MeasurementPassed = len(report.Blockers) == 0
	for _, entry := range Frozen().Capabilities {
		if entry.RequiredForRelease {
			if err := Require(entry.ID); err != nil {
				report.Blockers = append(report.Blockers, deployment.Issue{Code: "frozen_release_prerequisite_unqualified"})
			}
		}
	}
	report.ReleaseReady = report.MeasurementPassed && len(report.Blockers) == 0
	if !report.MeasurementPassed {
		return report, deployment.Issue{Code: "measurement_gate_failed"}
	}
	return report, nil
}

func measurementPercentile(sorted []int64, percent int) int64 {
	if len(sorted) == 0 {
		return 0
	}
	index := (len(sorted)*percent+99)/100 - 1
	return sorted[index]
}

func (e *PreflightEngine) measureCase(ctx context.Context, plan MeasurementPlan, probe MeasurementCase, reader *measurementCapture) (sample MeasurementSample) {
	started := time.Now()
	sample.Kind = probe.Kind
	sample.StartedAt = started.UTC()
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	defer func() { sample.ElapsedNanoseconds = time.Since(started).Nanoseconds() }()
	fail := func(code string, role string) {
		issue := deployment.Issue{Code: code, Role: role}
		sample.Issue = &issue
	}
	header, reported, err := reader.ReportedHeaderByNumber(ctx, new(big.Int).SetUint64(probe.BlockNumber))
	if err != nil || header == nil || reported != probe.BlockHash {
		fail("measurement_canonical_pin_unverified", "")
		return
	}
	policy := deployment.Policy{Now: time.Now().UTC(), MaxAge: e.maxAge, Timeout: e.timeout}
	manifest := e.manifest
	if probe.Kind == "pending-governance" {
		manifest.Status = "pending-beacon-acceptance"
	}
	attestation, err := deployment.Attest(ctx, reader, manifest, probe.BlockHash, policy)
	sample.Attestation = &attestation
	if err != nil {
		issue := measurementIssue(err)
		sample.Issue = &issue
		return
	}
	if probe.Kind == "pending-governance" {
		actionable := false
		for _, issue := range attestation.Issues {
			if issue.Role == "PoolBeacon" && (issue.Code == "governance_handoff_pending" || issue.Code == "manifest_governance_pending") {
				actionable = true
			}
		}
		if attestation.Eligible || !actionable || !attestation.Identities["PoolBeacon"].Verified || len(attestation.ActualOwner) != 42 || !common.IsHexAddress(attestation.ActualOwner) || len(attestation.PendingOwner) != 42 || !common.IsHexAddress(attestation.PendingOwner) || (common.HexToAddress(attestation.ActualOwner) == common.HexToAddress(manifest.Governance.ExpectedBeaconOwner) && common.HexToAddress(attestation.PendingOwner) == (common.Address{})) {
			fail("pending_governance_fixture_not_observed", "PoolBeacon")
			return
		}
		sample.Passed = true
		return
	}
	publication, err := e.Capabilities(ctx, probe.BlockHash)
	sample.Publication = &publication
	if err != nil {
		issue := measurementIssue(err)
		sample.Issue = &issue
		return
	}
	families := map[Family]FamilyCapability{}
	for _, published := range publication.Families {
		capability := published.Capability
		if _, duplicate := families[capability.Family]; duplicate {
			fail("catalog_family_duplicate", "")
			return
		}
		families[capability.Family] = capability
		if capability.ReleaseEnabled && !capability.Supported {
			fail("unsupported_family_release_enabled", string(capability.Family))
			return
		}
		if capability.Supported {
			if len(capability.Selectors) == 0 || capability.AttestationReference != probe.BlockHash.Hex() {
				fail("enabled_family_evidence_incomplete", string(capability.Family))
				return
			}
			for _, selector := range capability.Selectors {
				expected := "0x" + hex.EncodeToString(crypto.Keccak256([]byte(selector.Signature))[:4])
				if selector.Selector != expected {
					fail("capability_selector_identity_mismatch", string(capability.Family))
					return
				}
			}
			if capability.Family == Creation || capability.Family == Trading || capability.Family == NFTFees {
				if len(capability.EventCoverage) == 0 {
					fail("enabled_family_event_coverage_missing", string(capability.Family))
					return
				}
			}
			for _, event := range capability.EventCoverage {
				known := false
				for _, supported := range ContractSupport().EventFamilies {
					if supported == event {
						known = true
					}
				}
				if !known {
					fail("capability_event_family_unknown", string(capability.Family))
					return
				}
			}
			if err := attestation.ValidateFamilyFor(manifest, policy, familyRoles[capability.Family], capability.Family == Creation); err != nil {
				fail("enabled_family_attestation_missing", string(capability.Family))
				return
			}
		}
	}
	if len(families) != 9 {
		fail("catalog_family_coverage_incomplete", "")
		return
	}
	if issue := e.measureWiring(ctx, probe.BlockHash, families); issue != nil {
		sample.Issue = issue
		return
	}
	switch probe.Kind {
	case "exports":
		exported, err := deployment.ExportRevision(manifest, attestation, policy, plan.PublicEndpoints, e.revision, plan.GoBuild.Addresses, plan.FrontendBuild.Addresses)
		if err != nil {
			issue := measurementIssue(err)
			sample.Issue = &issue
			return
		}
		if exported.NetworkSHA256 != plan.GoBuild.NetworkSHA256 || exported.Network.ManifestHash != e.revision.ManifestSHA256 {
			fail("consumer_network_identity_mismatch", "")
			return
		}
		changed := map[string]string{}
		for name, address := range plan.FrontendBuild.Addresses {
			changed[name] = address
		}
		changed["Router"] = manifest.Roles["Quoter"].Address
		rejected, err := deployment.ExportRevision(manifest, attestation, policy, plan.PublicEndpoints, e.revision, plan.GoBuild.Addresses, changed)
		if err == nil || rejected.SchemaVersion != 0 || !strings.Contains(err.Error(), "Router") {
			fail("router_conflict_not_rejected", "Router")
			return
		}
	case "unknown-implementation":
		distinguished := false
		for name, identity := range attestation.Identities {
			if identity.Verified || identity.ImplementationAddress == "" {
				continue
			}
			for other, peer := range attestation.Identities {
				if name != other && identity.RuntimeHash != "" && identity.RuntimeHash == manifest.Roles[name].RuntimeHash && identity.RuntimeHash == peer.RuntimeHash && peer.RuntimeHash == manifest.Roles[other].RuntimeHash && len(identity.ImplementationAddress) == 42 && common.IsHexAddress(identity.ImplementationAddress) && len(peer.ImplementationAddress) == 42 && common.IsHexAddress(peer.ImplementationAddress) && common.HexToAddress(identity.ImplementationAddress) != common.HexToAddress(peer.ImplementationAddress) {
					distinguished = true
				}
			}
			for _, capability := range families {
				for _, role := range capability.DependencyIDs {
					if role == name && capability.Supported {
						fail("unknown_implementation_enabled_family", name)
						return
					}
				}
			}
		}
		if !distinguished {
			fail("different_implementation_fixture_missing", "")
			return
		}
	case "no-oracle":
		if !families[Creation].Supported || !families[PlainQuote].Supported || families[Creation].OracleRequired || families[PlainQuote].OracleRequired || families[DisplayConversion].Supported || families[DisplayConversion].ReasonCode != DisplayUnavailable {
			fail("core_oracle_independence_failed", "")
			return
		}
		result, err := e.Preflight(ctx, probe.Request)
		sample.Preflight = &result
		if err != nil || !result.Supported || result.OracleRequired || result.Quote == nil || result.CreationCost != nil {
			fail("core_quote_without_oracle_unavailable", "Quoter")
			return
		}
	case "quotes", "zero-real-sell":
		result, err := e.Preflight(ctx, probe.Request)
		sample.Preflight = &result
		if err != nil {
			issue := measurementIssue(err)
			sample.Issue = &issue
			return
		}
		if result.Pool == nil {
			fail("actual_pool_snapshot_missing", "Pool")
			return
		}
		if probe.Kind == "zero-real-sell" {
			if probe.Request.IsBuy || result.Pool.RealUSDLBalance != "0" || result.Pool.VirtualUSDLReserve == "0" || result.Pool.RedeemableLiquidityRaw != "0" || result.Supported || result.ExecutionPermitted || result.ReasonCode != UnexecutableQuote {
				fail("virtual_liquidity_sell_not_blocked", "Pool")
				return
			}
			break
		}
		actual, quoteErr := ReadPinnedQuote(ctx, reader, common.HexToAddress(manifest.Roles["Quoter"].Address), probe.Request.Pool, probe.Request.Trader, probe.Request.AmountIn, probe.Request.IsBuy, probe.BlockHash)
		if common.HexToAddress(result.Pool.Optical) != (common.Address{}) || quoteErr != nil || !result.Supported || result.Quote == nil || *result.Quote != actual {
			fail("contract_quote_tuple_mismatch", "Quoter")
			return
		}
		optical, err := e.Preflight(ctx, probe.OpticalRequest)
		if err != nil {
			issue := measurementIssue(err)
			sample.Issue = &issue
			return
		}
		_, opticalErr := ReadPinnedQuote(ctx, reader, common.HexToAddress(manifest.Roles["Quoter"].Address), probe.OpticalRequest.Pool, probe.OpticalRequest.Trader, probe.OpticalRequest.AmountIn, probe.OpticalRequest.IsBuy, probe.BlockHash)
		var unavailable Unavailable
		if optical.Pool == nil || common.HexToAddress(optical.Pool.Optical) == (common.Address{}) || optical.Supported || optical.ExecutionPermitted || optical.ReasonCode != OpticalQuoteUnsupported || !errors.As(opticalErr, &unavailable) || unavailable.ReasonCode != OpticalQuoteUnsupported {
			fail("optical_quote_not_typed", "Quoter")
			return
		}
		sample.OpticalPreflight = &optical
	case "creation-fees":
		paths := map[string]bool{}
		for _, hash := range probe.ReceiptHashes {
			fee, err := e.ReconcileCreationFee(ctx, hash, probe.TreasuryIdentity)
			if err != nil {
				issue := measurementIssue(err)
				sample.Issue = &issue
				return
			}
			receiptBlock, err := reader.HeaderByReportedHash(ctx, common.HexToHash(fee.BlockHash))
			if err != nil || receiptBlock == nil || receiptBlock.Number == nil {
				fail("creation_receipt_block_unavailable", fee.Path)
				return
			}
			canonical, reported, err := reader.ReportedHeaderByNumber(ctx, receiptBlock.Number)
			if err != nil || canonical == nil || reported != common.HexToHash(fee.BlockHash) {
				fail("creation_receipt_block_not_canonical", fee.Path)
				return
			}
			sample.Fees = append(sample.Fees, fee)
			if paths[fee.Path] || fee.FeeRaw != deployment.CreationFeeRaw || fee.PayerDebitRaw != deployment.CreationFeeRaw || fee.TreasuryCreditRaw != deployment.CreationFeeRaw || fee.TreasuryLedgerCreditRaw != deployment.CreationFeeRaw {
				fail("creation_fee_policy_mismatch", fee.Path)
				return
			}
			paths[fee.Path] = true
		}
		if !paths["Router"] || !paths["SidioraFactory"] {
			fail("creation_paths_incomplete", "")
			return
		}
	case "private-export":
		if err := attestation.ValidateFor(manifest, policy); err != nil {
			fail("private_export_fixture_not_eligible", "")
			return
		}
		changed := map[string]string{}
		for name, address := range plan.GoBuild.Addresses {
			changed[name] = address
		}
		changed["Router"] = manifest.Roles["Quoter"].Address
		endpoints := plan.PublicEndpoints
		endpoints.RPC = "https://redaction-check:PRIVATE_EXPORT_SENTINEL@invalid.example/rpc"
		exported, err := deployment.ExportRevision(manifest, attestation, policy, endpoints, e.revision, changed, plan.FrontendBuild.Addresses)
		if err == nil || exported.SchemaVersion != 0 || !strings.Contains(err.Error(), "Router") || strings.Contains(err.Error(), "PRIVATE_EXPORT_SENTINEL") {
			fail("private_export_redaction_failed", "Router")
			return
		}
		exported, err = deployment.ExportRevision(manifest, attestation, policy, endpoints, e.revision, plan.GoBuild.Addresses, plan.FrontendBuild.Addresses)
		if err == nil || exported.SchemaVersion != 0 || strings.Contains(err.Error(), "PRIVATE_EXPORT_SENTINEL") {
			fail("private_endpoint_export_not_rejected", "")
			return
		}
	case "quoter-revoked":
		if attestation.Identities["Quoter"].Verified || families[PlainQuote].Supported || !families[Creation].Supported || !families[NFTFees].Supported || families[Opticals].Supported || families[Opticals].ReasonCode != OpticalQuoteUnsupported {
			fail("dependency_revocation_not_isolated", "Quoter")
			return
		}
	default:
		fail("measurement_case_unknown", "")
		return
	}
	sample.Passed = true
	return
}

func DecodeMeasurementPlan(data []byte) (MeasurementPlan, error) {
	var plan MeasurementPlan
	if len(data) == 0 || len(data) > 8<<20 || measurementUniqueJSON(data) != nil {
		return plan, deployment.Issue{Code: "measurement_plan_malformed"}
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || decoder.Decode(new(any)) != io.EOF {
		return MeasurementPlan{}, deployment.Issue{Code: "measurement_plan_malformed"}
	}
	return plan, nil
}

func measurementUniqueJSON(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 16 {
			return deployment.Issue{Code: "json_depth_exceeded"}
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, composite := token.(json.Delim)
		if !composite {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				name, ok := key.(string)
				if err != nil || !ok || seen[strings.ToLower(name)] || len(seen) >= 256 {
					return deployment.Issue{Code: "json_duplicate_or_size"}
				}
				seen[strings.ToLower(name)] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			count := 0
			for decoder.More() {
				count++
				if count > 10000 {
					return deployment.Issue{Code: "json_array_size"}
				}
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return deployment.Issue{Code: "json_delimiter_invalid"}
		}
		_, err = decoder.Token()
		return err
	}
	if err := visit(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return deployment.Issue{Code: "json_trailing_data"}
	}
	return nil
}

func (e *PreflightEngine) measureWiring(ctx context.Context, block common.Hash, families map[Family]FamilyCapability) *deployment.Issue {
	wiring := map[string]map[string]string{
		"Router":         {"factory()": "SidioraFactory", "poolRegistry()": "PoolRegistry", "protocolConfig()": "ProtocolConfig", "usdlAddress()": "USDL"},
		"Quoter":         {"poolRegistry()": "PoolRegistry", "protocolConfig()": "ProtocolConfig"},
		"SidioraFactory": {"poolBeacon()": "PoolBeacon", "nftContract()": "SidioraNFT", "poolRegistry()": "PoolRegistry", "eventEmitter()": "EventEmitter", "protocolConfig()": "ProtocolConfig", "feeAccumulator()": "FeeAccumulator", "usdlAddress()": "USDL"},
		"PoolRegistry":   {"eventEmitter()": "EventEmitter"},
		"FeeAccumulator": {"protocolConfig()": "ProtocolConfig", "poolRegistry()": "PoolRegistry", "eventEmitter()": "EventEmitter", "usdlAddress()": "USDL"},
		"SidioraNFT":     {"eventEmitter()": "EventEmitter"},
		"FeesRouter":     {"nftContract()": "SidioraNFT", "feeAccumulator()": "FeeAccumulator", "poolRegistry()": "PoolRegistry"},
	}
	required := map[string]bool{}
	for family, capability := range families {
		if capability.Supported {
			for _, role := range familyRoles[family] {
				required[role] = true
			}
		}
	}
	roles := make([]string, 0, len(required))
	for role := range required {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		getters := wiring[role]
		signatures := make([]string, 0, len(getters))
		for signature := range getters {
			signatures = append(signatures, signature)
		}
		sort.Strings(signatures)
		for _, signature := range signatures {
			target := getters[signature]
			word, err := e.call(ctx, common.HexToAddress(e.manifest.Roles[role].Address), signature, block)
			if err != nil || len(word) != 32 || !bytes.Equal(word[:12], make([]byte, 12)) || common.BytesToAddress(word[12:]) != common.HexToAddress(e.manifest.Roles[target].Address) {
				issue := deployment.Issue{Code: "enabled_family_wiring_unverified", Role: role}
				return &issue
			}
		}
	}
	return nil
}
