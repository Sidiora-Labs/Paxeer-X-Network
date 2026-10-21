package l16

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/production/l17"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
)

type InventoryMeasurementFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type InventoryMeasurementCase struct {
	ID                        string     `json:"id"`
	Scenario                  int        `json:"scenario"`
	Wallet                    string     `json:"wallet"`
	Token                     string     `json:"token"`
	AccessTokenEnv            string     `json:"accessTokenEnv"`
	Operation                 string     `json:"operation"`
	Day                       string     `json:"day,omitempty"`
	Height                    int64      `json:"height"`
	BlockHash                 string     `json:"blockHash"`
	ExpectedState             *Inventory `json:"expectedState"`
	ExpectedDayState          *Inventory `json:"expectedDayState,omitempty"`
	PricingQuality            string     `json:"pricingQuality,omitempty"`
	ExpectedUnrealized        *string    `json:"expectedUnrealizedUsdlRaw"`
	ExpectedNormalizedAverage *string    `json:"expectedNormalizedAverage"`
	ExpectedError             string     `json:"expectedError,omitempty"`
	Requests                  int        `json:"requests"`
}

type InventoryMeasurementPlan struct {
	Version              int                        `json:"version"`
	RunID                string                     `json:"runId"`
	InputPlanRaw         json.RawMessage            `json:"-"`
	InputPlanSHA256      string                     `json:"-"`
	CandidateDigest      string                     `json:"candidateDigest"`
	CandidateManifest    InventoryMeasurementFile   `json:"candidateManifest"`
	ReleaseDigest        string                     `json:"releaseDigest"`
	InterfaceDigest      string                     `json:"interfaceDigest"`
	SourceRoot           string                     `json:"sourceRoot"`
	SourceFiles          []InventoryMeasurementFile `json:"sourceFiles"`
	Topology             InventoryMeasurementFile   `json:"topology"`
	Dependencies         InventoryMeasurementFile   `json:"dependencies"`
	Region               string                     `json:"region"`
	APIOrigin            string                     `json:"apiOrigin"`
	RequestsPerSecond    int                        `json:"requestsPerSecond"`
	Concurrency          int                        `json:"concurrency"`
	RequestTimeoutMillis int                        `json:"requestTimeoutMillis"`
	MaxDurationSeconds   int                        `json:"maxDurationSeconds"`
	MaxArtifactBytes     int64                      `json:"maxArtifactBytes"`
	MaxHeapAllocBytes    uint64                     `json:"maxHeapAllocBytes"`
	MaxPoolAcquired      int32                      `json:"maxPoolAcquired"`
	MaxArchiveRecords    int64                      `json:"maxArchiveRecords"`
	Cases                []InventoryMeasurementCase `json:"cases"`
}

type InventoryMeasurementSample struct {
	CaseID                 string          `json:"caseId"`
	Scenario               int             `json:"scenario"`
	Operation              string          `json:"operation"`
	Sequence               int             `json:"sequence"`
	ScheduledOffsetNS      int64           `json:"scheduledOffsetNs"`
	StartOffsetNS          int64           `json:"startOffsetNs"`
	FinishedOffsetNS       int64           `json:"finishedOffsetNs"`
	ServiceElapsedNS       int64           `json:"serviceElapsedNs"`
	CorrectedElapsedNS     int64           `json:"correctedElapsedNs"`
	AdmissionDelayNS       int64           `json:"admissionDelayNs"`
	ErrorClass             string          `json:"errorClass,omitempty"`
	ExpectedErrorMatched   bool            `json:"expectedErrorMatched"`
	HTTPStatus             int             `json:"httpStatus,omitempty"`
	OriginElapsedNS        *int64          `json:"originElapsedNs"`
	HTTPRoundTripElapsedNS *int64          `json:"httpRoundTripElapsedNs"`
	ServerTiming           string          `json:"serverTiming,omitempty"`
	Payload                json.RawMessage `json:"payload,omitempty"`
	PayloadSHA256          string          `json:"payloadSha256,omitempty"`
	HeapAllocBytes         uint64          `json:"heapAllocBytes"`
	PoolAcquired           int32           `json:"poolAcquired"`
	PoolTotal              int32           `json:"poolTotal"`
}

type InventoryMeasurementManifest struct {
	Version                         int                        `json:"version"`
	RunID                           string                     `json:"runId"`
	Scope                           string                     `json:"scope"`
	Algorithm                       string                     `json:"algorithm"`
	CandidateDigest                 string                     `json:"candidateDigest"`
	ReleaseDigest                   string                     `json:"releaseDigest"`
	InterfaceDigest                 string                     `json:"interfaceDigest"`
	PlanSHA256                      string                     `json:"planSha256"`
	InputPlanSHA256                 string                     `json:"inputPlanSha256"`
	ExecutableSHA256                string                     `json:"executableSha256"`
	Region                          string                     `json:"region"`
	TopologySHA256                  string                     `json:"topologySha256"`
	DependencySHA256                string                     `json:"dependencySha256"`
	StartedAt                       time.Time                  `json:"startedAt"`
	FinishedAt                      time.Time                  `json:"finishedAt"`
	Offered                         int                        `json:"offered"`
	Completed                       int                        `json:"completed"`
	Errors                          int                        `json:"errors"`
	UnexpectedErrors                int                        `json:"unexpectedErrors"`
	ExpectedAuthorizationRejections int                        `json:"expectedAuthorizationRejections"`
	DependencyBlockedOperations     int                        `json:"dependencyBlockedOperations"`
	GeneratorSaturated              int                        `json:"generatorSaturated"`
	ServiceSampleCount              int                        `json:"serviceSampleCount"`
	OriginSampleCount               int                        `json:"originSampleCount"`
	HTTPRoundTripSampleCount        int                        `json:"httpRoundTripSampleCount"`
	AdmissionDelayP95NS             *int64                     `json:"admissionDelayP95Ns"`
	AdmissionDelayP99NS             *int64                     `json:"admissionDelayP99Ns"`
	ServiceP95NS                    *int64                     `json:"serviceP95Ns"`
	ServiceP99NS                    *int64                     `json:"serviceP99Ns"`
	CorrectedP95NS                  *int64                     `json:"correctedP95Ns"`
	CorrectedP99NS                  *int64                     `json:"correctedP99Ns"`
	OriginP95NS                     *int64                     `json:"originP95Ns"`
	OriginP99NS                     *int64                     `json:"originP99Ns"`
	HTTPRoundTripP95NS              *int64                     `json:"httpRoundTripP95Ns"`
	HTTPRoundTripP99NS              *int64                     `json:"httpRoundTripP99Ns"`
	PeakHeapAllocBytes              uint64                     `json:"peakHeapAllocBytes"`
	PeakPoolAcquired                int32                      `json:"peakPoolAcquired"`
	ResourceSamplePeriodNS          int64                      `json:"resourceSamplePeriodNs"`
	Artifacts                       []InventoryMeasurementFile `json:"artifacts"`
	Failures                        []string                   `json:"failures"`
	GlobalAcceptanceQualified       bool                       `json:"globalAcceptanceQualified"`
}

func LoadInventoryMeasurementPlan(path string) (InventoryMeasurementPlan, error) {
	file, err := os.Open(path)
	if err != nil {
		return InventoryMeasurementPlan{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
	if err != nil {
		return InventoryMeasurementPlan{}, err
	}
	return decodeInventoryMeasurementPlan(raw)
}

func decodeInventoryMeasurementPlan(raw []byte) (InventoryMeasurementPlan, error) {
	var plan InventoryMeasurementPlan
	if len(raw) > 4*1024*1024 || !json.Valid(raw) {
		return plan, errors.New("invalid bounded measurement plan")
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if err := uniqueInventoryMeasurementJSON(tokens, 0); err != nil {
		return plan, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return plan, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return plan, errors.New("inventory measurement plan has trailing data")
	}
	plan.InputPlanRaw = append(json.RawMessage(nil), raw...)
	plan.InputPlanSHA256 = delivery.Hash(raw)
	return plan, nil
}

func uniqueInventoryMeasurementJSON(decoder *json.Decoder, depth int) error {
	if depth > 128 {
		return errors.New("measurement JSON nesting exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := map[string]bool{}
		for decoder.More() {
			token, err = decoder.Token()
			if err != nil {
				return err
			}
			key, ok := token.(string)
			if !ok || keys[key] {
				return errors.New("duplicate measurement JSON field")
			}
			keys[key] = true
			if err = uniqueInventoryMeasurementJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueInventoryMeasurementJSON(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected measurement JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}

func measurementReadFile(file InventoryMeasurementFile, root string) ([]byte, error) {
	if !digestHex(file.SHA256) || file.Path == "" {
		return nil, errors.New("bound measurement input required")
	}
	path := file.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 16*1024*1024 {
		return nil, errors.New("regular bounded measurement input required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if delivery.Hash(raw) != file.SHA256 {
		return nil, errors.New("measurement input digest mismatch")
	}
	return raw, nil
}

func validateInventoryMeasurementPlan(worker *LiveTransferWorker, plan InventoryMeasurementPlan) error {
	if worker == nil || worker.service == nil || worker.identity == nil || worker.service.authority == nil || plan.Version != 1 || !digestHex(plan.InputPlanSHA256) || delivery.Hash(plan.InputPlanRaw) != plan.InputPlanSHA256 || !regexpToken(plan.RunID) || !digestHex(plan.CandidateDigest) || plan.ReleaseDigest != worker.service.release || !digestHex(plan.InterfaceDigest) || !filepath.IsAbs(plan.SourceRoot) || plan.Region == "" || len(plan.Region) > 128 || plan.RequestsPerSecond < 1 || plan.RequestsPerSecond > 800 || plan.Concurrency < 1 || plan.Concurrency > 32 || plan.RequestTimeoutMillis < 1 || plan.RequestTimeoutMillis > 90000 || plan.MaxDurationSeconds < 1 || plan.MaxDurationSeconds > 1800 || plan.MaxArtifactBytes < 1 || plan.MaxArtifactBytes > 32*1024*1024*1024 || plan.MaxHeapAllocBytes == 0 || plan.MaxPoolAcquired < 1 || plan.MaxArchiveRecords < 1 || plan.MaxArchiveRecords > 1000000 || len(plan.SourceFiles) < 2 || len(plan.SourceFiles) > 1000 || len(plan.Cases) < 11 || len(plan.Cases) > 1000 {
		return errors.New("invalid or missing inventory measurement authority, plan, or isolated budget")
	}
	boundPlan, bindErr := decodeInventoryMeasurementPlan(plan.InputPlanRaw)
	boundJSON, boundErr := json.Marshal(boundPlan)
	currentJSON, currentErr := json.Marshal(plan)
	if bindErr != nil || boundErr != nil || currentErr != nil || string(boundJSON) != string(currentJSON) {
		return errors.New("typed measurement plan differs from its approved exact input bytes")
	}
	origin, err := url.Parse(plan.APIOrigin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || origin.Path != "" && origin.Path != "/" || origin.Scheme != "https" && !(origin.Scheme == "http" && (origin.Hostname() == "127.0.0.1" || origin.Hostname() == "localhost" || origin.Hostname() == "::1")) {
		return errors.New("verified inventory API origin required")
	}
	seenFiles := map[string]bool{}
	for _, file := range plan.SourceFiles {
		clean := filepath.Clean(file.Path)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || seenFiles[clean] {
			return errors.New("invalid source manifest path")
		}
		seenFiles[clean] = true
		if clean == "spec/production-system/integration/contracts/v2/manifest.json" && file.SHA256 != plan.InterfaceDigest {
			return errors.New("measurement interface digest does not match actual frozen manifest")
		}
		if _, err = measurementReadFile(file, plan.SourceRoot); err != nil {
			return err
		}
	}
	for _, name := range []string{"core/pnl-tracker/internal/production/l16/measurement.go", "core/pnl-tracker/internal/production/l16/measurement_production_test.go", "core/pnl-tracker/internal/production/l16/live_transfers.go", "core/pnl-tracker/internal/production/l16/service.go", "core/pnl-tracker/internal/production/l16/inventory.go", "spec/production-system/integration/contracts/v2/manifest.json"} {
		if !seenFiles[name] {
			return errors.New("inventory measurement source binding incomplete")
		}
	}
	if plan.CandidateManifest.SHA256 != plan.CandidateDigest {
		return errors.New("actual candidate manifest binding required")
	}
	for _, file := range []InventoryMeasurementFile{plan.CandidateManifest, plan.Topology, plan.Dependencies} {
		if _, err = measurementReadFile(file, plan.SourceRoot); err != nil {
			return err
		}
	}
	scenarios := map[int]bool{}
	ids := map[string]bool{}
	total := 0
	for _, c := range plan.Cases {
		if !regexpToken(c.ID) || ids[c.ID] || c.Scenario < 1 || c.Scenario > 11 || !address.MatchString(c.Wallet) || !address.MatchString(c.Token) || c.Wallet == zeroAddress || c.Token == zeroAddress || c.Height <= 0 || !hash.MatchString(c.BlockHash) || !regexpToken(c.AccessTokenEnv) || os.Getenv(c.AccessTokenEnv) == "" || c.Requests < 1 || c.Requests > 100000 || c.ExpectedState == nil && c.ExpectedError == "" {
			return errors.New("missing genuine scenario input or expected result")
		}
		if c.ExpectedState != nil {
			if _, err = c.ExpectedState.values(); err != nil {
				return err
			}
		}
		if c.ExpectedUnrealized != nil {
			if _, err = aggregate(*c.ExpectedUnrealized, true); err != nil {
				return err
			}
		}
		if c.Scenario == 3 && c.ExpectedNormalizedAverage == nil {
			return errors.New("normalized decimal scenario expectation required")
		}
		switch c.Operation {
		case "rebuild_owned", "active", "http_portfolio", "historical_day":
		default:
			return errors.New("unsupported inventory measurement operation")
		}
		switch c.ExpectedError {
		case "", "parity", "baseline_unavailable", "attribution_unavailable", "cost_allocation_unavailable", "authorization":
		default:
			return errors.New("unsupported expected dependency failure")
		}
		if c.Operation == "historical_day" {
			if _, err = time.Parse("2006-01-02", c.Day); err != nil {
				return err
			}
			if c.ExpectedDayState == nil {
				return errors.New("actual historical expected state required")
			}
			if _, err = c.ExpectedDayState.values(); err != nil {
				return err
			}
		}
		if c.Scenario == 10 && (c.Operation != "http_portfolio" || c.PricingQuality == "") {
			return errors.New("price scenario requires actual API provenance")
		}
		if c.PricingQuality != "" && c.PricingQuality != "priced" && c.PricingQuality != "unpriced" && c.PricingQuality != "stale" {
			return errors.New("invalid expected pricing quality")
		}
		if c.Scenario == 8 && c.Requests < 100 {
			return errors.New("duplicate scenario requires at least100 actual replay requests")
		}
		ids[c.ID], scenarios[c.Scenario] = true, true
		total += c.Requests
	}
	if len(scenarios) != 11 || total > 100000 || int64(total)*int64(time.Second)/int64(plan.RequestsPerSecond) > int64(plan.MaxDurationSeconds)*int64(time.Second) {
		return errors.New("incomplete scenarios or exceeded measurement envelope")
	}
	return nil
}

func measurementError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrParity):
		return "parity"
	case errors.Is(err, ErrBaselineUnavailable):
		return "baseline_unavailable"
	case errors.Is(err, ErrAttributionUnavailable):
		return "attribution_unavailable"
	case errors.Is(err, ErrCostAllocationUnavailable):
		return "cost_allocation_unavailable"
	case errors.Is(err, auth.ErrScopeDenied):
		return "authorization"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, delivery.ErrIntegrity):
		return "integrity"
	default:
		return "dependency_or_operation_failure"
	}
}

func inventoryMeasurementOperation(ctx context.Context, worker *LiveTransferWorker, client *http.Client, plan InventoryMeasurementPlan, c InventoryMeasurementCase) (json.RawMessage, int, inventoryMeasurementHTTPFacts, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(plan.APIOrigin, "/")+"/v3/me/portfolio", nil)
	if err != nil {
		return nil, 0, inventoryMeasurementHTTPFacts{}, err
	}
	request.Header.Set("Authorization", "Bearer "+os.Getenv(c.AccessTokenEnv))
	principal, err := worker.identity.AuthenticateRequest(ctx, request)
	if err != nil {
		return nil, 0, inventoryMeasurementHTTPFacts{}, err
	}
	if principal.Wallet != c.Wallet {
		return nil, 0, inventoryMeasurementHTTPFacts{}, auth.ErrScopeDenied
	}
	var result any
	switch c.Operation {
	case "rebuild_owned":
		result, err = worker.RebuildOwned(ctx, principal)
	case "active":
		if err = worker.identity.CheckStreamSession(ctx, principal.StreamPrincipal()); err == nil {
			result, err = worker.service.Active(ctx, c.Wallet, c.Token)
		}
	case "historical_day":
		day, parseErr := time.Parse("2006-01-02", c.Day)
		if parseErr != nil {
			return nil, 0, inventoryMeasurementHTTPFacts{}, parseErr
		}
		if err = worker.identity.CheckStreamSession(ctx, principal.StreamPrincipal()); err == nil {
			result, err = worker.service.HistoricalDay(ctx, c.Wallet, c.Token, day)
		}
	case "http_portfolio":
		beginHTTP := time.Now()
		response, httpErr := client.Do(request)
		if httpErr != nil {
			return nil, 0, inventoryMeasurementHTTPFacts{}, httpErr
		}
		defer response.Body.Close()
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 8*1024*1024+1))
		roundTrip := time.Since(beginHTTP).Nanoseconds()
		facts := inventoryMeasurementHTTPFacts{RoundTripNS: &roundTrip, ServerTiming: response.Header.Get("Server-Timing")}
		if readErr != nil || len(raw) > 8*1024*1024 || !json.Valid(raw) {
			return nil, response.StatusCode, facts, errors.New("invalid or oversized actual portfolio response")
		}
		if response.StatusCode != http.StatusOK {
			return raw, response.StatusCode, facts, errors.New("actual portfolio request rejected")
		}
		originNS, timingErr := inventoryOriginTiming(facts.ServerTiming)
		if timingErr != nil || originNS > roundTrip {
			return raw, response.StatusCode, facts, errors.New("actual origin timing missing or contradictory")
		}
		facts.OriginNS = &originNS
		if e := validateInventoryHTTPResponse(raw, c); e != nil {
			return raw, response.StatusCode, facts, e
		}
		return raw, response.StatusCode, facts, nil
	}
	if err != nil {
		return nil, 0, inventoryMeasurementHTTPFacts{}, err
	}
	raw, err := json.Marshal(result)
	if c.Operation == "historical_day" && err == nil {
		var checkpoint Checkpoint
		if json.Unmarshal(raw, &checkpoint) != nil || c.ExpectedDayState == nil || checkpoint.State != *c.ExpectedDayState {
			return raw, 0, inventoryMeasurementHTTPFacts{}, delivery.ErrIntegrity
		}
	}
	return raw, 0, inventoryMeasurementHTTPFacts{}, err
}

func measurementPercentile(values []int64, numerator int) *int64 {
	if len(values) == 0 {
		return nil
	}
	ordered := append([]int64(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	rank := (len(ordered)*numerator + 99) / 100
	value := ordered[rank-1]
	return &value
}

func RunInventoryMeasurement(ctx context.Context, worker *LiveTransferWorker, client *http.Client, plan InventoryMeasurementPlan, output string) (InventoryMeasurementManifest, error) {
	manifest := InventoryMeasurementManifest{Version: 1, RunID: plan.RunID, Scope: "isolated_inventory_harness_global_acceptance_deferred_9.1", Algorithm: Algorithm, CandidateDigest: plan.CandidateDigest, ReleaseDigest: plan.ReleaseDigest, InterfaceDigest: plan.InterfaceDigest, Region: plan.Region, TopologySHA256: plan.Topology.SHA256, DependencySHA256: plan.Dependencies.SHA256, Artifacts: []InventoryMeasurementFile{}, Failures: []string{}, StartedAt: time.Now().UTC()}
	if err := validateInventoryMeasurementPlan(worker, plan); err != nil {
		return manifest, err
	}
	if client == nil || client.Timeout <= 0 || client.Timeout > 90*time.Second {
		return manifest, errors.New("bounded real inventory HTTP client required")
	}
	rawPlan, err := json.Marshal(plan)
	if err != nil {
		return manifest, err
	}
	manifest.PlanSHA256 = delivery.Hash(rawPlan)
	manifest.InputPlanSHA256 = plan.InputPlanSHA256
	executable, err := os.Executable()
	if err != nil {
		return manifest, err
	}
	manifest.ExecutableSHA256, err = measurementFileSHA256(executable)
	if err != nil {
		return manifest, err
	}
	if !filepath.IsAbs(output) {
		return manifest, errors.New("absolute private measurement output directory required")
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return manifest, err
	}
	var artifactBytes int64
	writeArtifact := func(name string, raw []byte) error {
		if int64(len(raw)) > plan.MaxArtifactBytes-artifactBytes {
			return errors.New("inventory measurement artifact budget exhausted")
		}
		path := filepath.Join(output, name)
		file, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		if _, e = file.Write(raw); e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e == nil {
			e = closeErr
		}
		if e == nil {
			artifactBytes += int64(len(raw))
			manifest.Artifacts = append(manifest.Artifacts, InventoryMeasurementFile{Path: name, SHA256: delivery.Hash(raw)})
		}
		return e
	}
	if err = writeArtifact("plan.json", rawPlan); err != nil {
		return manifest, err
	}
	if err = writeArtifact("input-plan.json", plan.InputPlanRaw); err != nil {
		return manifest, err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(plan.MaxDurationSeconds)*time.Second)
	defer cancel()
	resources := startInventoryResourceObserver(ctx, worker)
	defer resources.stop()
	manifest.ResourceSamplePeriodNS = int64(20 * time.Millisecond)
	samples := make([]InventoryMeasurementSample, 0)
	journal, err := os.OpenFile(filepath.Join(output, "samples.jsonl"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return manifest, err
	}
	defer journal.Close()
	var journalErr error
	var mu sync.Mutex
	appendSample := func(sample InventoryMeasurementSample) {
		mu.Lock()
		defer mu.Unlock()
		raw, e := json.Marshal(sample)
		if e == nil && int64(len(raw)+1) > plan.MaxArtifactBytes-artifactBytes {
			e = errors.New("inventory measurement artifact budget exhausted")
		}
		if e == nil {
			raw = append(raw, '\n')
			_, e = journal.Write(raw)
		}
		if e == nil {
			e = journal.Sync()
			artifactBytes += int64(len(raw))
		}
		if e != nil && journalErr == nil {
			journalErr = e
			cancel()
		}
		sample.Payload = nil
		samples = append(samples, sample)
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, plan.Concurrency)
	start := time.Now()
	sequence := 0
	for _, c := range plan.Cases {
		for i := 0; i < c.Requests; i++ {
			sequence++
			scheduled := time.Duration(sequence-1) * time.Second / time.Duration(plan.RequestsPerSecond)
			wait := time.Until(start.Add(scheduled))
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-bounded.Done():
					timer.Stop()
					err = bounded.Err()
				case <-timer.C:
				}
			}
			if bounded.Err() != nil {
				err = bounded.Err()
				break
			}
			manifest.Offered++
			sample := InventoryMeasurementSample{CaseID: c.ID, Scenario: c.Scenario, Operation: c.Operation, Sequence: sequence, ScheduledOffsetNS: int64(scheduled)}
			select {
			case slots <- struct{}{}:
				wg.Add(1)
				go func(c InventoryMeasurementCase, sample InventoryMeasurementSample) {
					defer wg.Done()
					defer func() { <-slots }()
					begin := time.Now()
					sample.StartOffsetNS = begin.Sub(start).Nanoseconds()
					sample.AdmissionDelayNS = sample.StartOffsetNS - sample.ScheduledOffsetNS
					requestCtx, done := context.WithTimeout(bounded, time.Duration(plan.RequestTimeoutMillis)*time.Millisecond)
					payload, status, facts, operationErr := inventoryMeasurementOperation(requestCtx, worker, client, plan, c)
					sample.Payload, sample.HTTPStatus = payload, status
					sample.OriginElapsedNS, sample.HTTPRoundTripElapsedNS, sample.ServerTiming = facts.OriginNS, facts.RoundTripNS, facts.ServerTiming
					done()
					finish := time.Now()
					sample.FinishedOffsetNS = finish.Sub(start).Nanoseconds()
					sample.ServiceElapsedNS = finish.Sub(begin).Nanoseconds()
					sample.CorrectedElapsedNS = sample.FinishedOffsetNS - sample.ScheduledOffsetNS
					sample.ErrorClass = measurementError(operationErr)
					sample.ExpectedErrorMatched = sample.ErrorClass == c.ExpectedError
					if sample.ErrorClass != c.ExpectedError {
						if sample.ErrorClass == "" {
							sample.ErrorClass = "expected_dependency_failure_absent"
						}
					}
					if sample.Payload != nil {
						sample.PayloadSHA256 = delivery.Hash(sample.Payload)
					}
					var memory runtime.MemStats
					runtime.ReadMemStats(&memory)
					sample.HeapAllocBytes = memory.HeapAlloc
					stats := worker.service.authority.pool.Stat()
					sample.PoolAcquired, sample.PoolTotal = stats.AcquiredConns(), stats.TotalConns()
					appendSample(sample)
				}(c, sample)
			default:
				manifest.GeneratorSaturated++
				sample.ErrorClass = "generator_saturated"
				sample.StartOffsetNS = time.Since(start).Nanoseconds()
				sample.FinishedOffsetNS = sample.StartOffsetNS
				appendSample(sample)
			}
		}
		if bounded.Err() != nil {
			break
		}
	}
	wg.Wait()
	sort.Slice(samples, func(i, j int) bool { return samples[i].Sequence < samples[j].Sequence })
	var service, corrected, httpTimes, originTimes, admission []int64
	for _, sample := range samples {
		if sample.ErrorClass != "generator_saturated" {
			admission = append(admission, sample.AdmissionDelayNS)
		}
		if sample.HeapAllocBytes > manifest.PeakHeapAllocBytes {
			manifest.PeakHeapAllocBytes = sample.HeapAllocBytes
		}
		if sample.ErrorClass != "" {
			manifest.Errors++
			if !sample.ExpectedErrorMatched {
				manifest.UnexpectedErrors++
			}
			if sample.ExpectedErrorMatched && sample.ErrorClass == "authorization" {
				manifest.ExpectedAuthorizationRejections++
			} else {
				manifest.DependencyBlockedOperations++
			}
		} else {
			manifest.Completed++
			if sample.Operation == "http_portfolio" {
				if sample.HTTPRoundTripElapsedNS != nil {
					httpTimes = append(httpTimes, *sample.HTTPRoundTripElapsedNS)
				}
				if sample.OriginElapsedNS != nil {
					originTimes = append(originTimes, *sample.OriginElapsedNS)
				}
			} else {
				service = append(service, sample.ServiceElapsedNS)
			}
			corrected = append(corrected, sample.CorrectedElapsedNS)
		}
	}
	manifest.ServiceSampleCount, manifest.OriginSampleCount, manifest.HTTPRoundTripSampleCount = len(service), len(originTimes), len(httpTimes)
	manifest.AdmissionDelayP95NS, manifest.AdmissionDelayP99NS = measurementPercentile(admission, 95), measurementPercentile(admission, 99)
	manifest.ServiceP95NS, manifest.ServiceP99NS = measurementPercentile(service, 95), measurementPercentile(service, 99)
	manifest.CorrectedP95NS, manifest.CorrectedP99NS = measurementPercentile(corrected, 95), measurementPercentile(corrected, 99)
	manifest.HTTPRoundTripP95NS, manifest.HTTPRoundTripP99NS = measurementPercentile(httpTimes, 95), measurementPercentile(httpTimes, 99)
	manifest.OriginP95NS, manifest.OriginP99NS = measurementPercentile(originTimes, 95), measurementPercentile(originTimes, 99)
	if manifest.OriginP95NS == nil || manifest.OriginP99NS == nil {
		manifest.Failures = append(manifest.Failures, "required actual HTTP origin observations absent")
	} else if *manifest.OriginP95NS > 150*int64(time.Millisecond) || *manifest.OriginP99NS > 400*int64(time.Millisecond) {
		manifest.Failures = append(manifest.Failures, "measured inventory origin read percentile exceeded150ms/400ms SLO")
	}
	if journalErr != nil {
		manifest.Failures = append(manifest.Failures, "durable raw sample journal failed or exhausted its admitted artifact budget")
	}
	if err = journal.Sync(); err != nil {
		return manifest, err
	}
	if err = journal.Close(); err != nil {
		return manifest, err
	}
	journalSHA, hashErr := measurementFileSHA256(filepath.Join(output, "samples.jsonl"))
	if hashErr != nil {
		return manifest, hashErr
	}
	manifest.Artifacts = append(manifest.Artifacts, InventoryMeasurementFile{Path: "samples.jsonl", SHA256: journalSHA})
	if manifest.PeakHeapAllocBytes > plan.MaxHeapAllocBytes {
		manifest.Failures = append(manifest.Failures, "measured heap exceeded the admitted topology envelope")
	}
	for _, sample := range samples {
		if sample.PoolAcquired > plan.MaxPoolAcquired {
			manifest.Failures = append(manifest.Failures, "measured database admission consumed reserved sessions")
			break
		}
	}
	if manifest.UnexpectedErrors > 0 || manifest.DependencyBlockedOperations > 0 {
		manifest.Failures = append(manifest.Failures, "one or more actual operations failed, depended on missing authority, or exceeded generator admission")
	}
	if manifest.Offered != sequence || bounded.Err() != nil {
		manifest.Failures = append(manifest.Failures, "bounded workload did not finish every scheduled operation")
	}
	for _, c := range plan.Cases {
		capture, captureErr := captureInventoryMeasurement(ctx, worker, c, plan.ReleaseDigest, plan.MaxArchiveRecords)
		if capture != nil {
			if e := writeArtifact(c.ID+"-reconciliation.json", capture); e != nil {
				return manifest, e
			}
		}
		if captureErr != nil {
			manifest.Failures = append(manifest.Failures, c.ID+":"+measurementError(captureErr))
		}
	}
	resources.stop()
	if resources.heap > manifest.PeakHeapAllocBytes {
		manifest.PeakHeapAllocBytes = resources.heap
	}
	manifest.PeakPoolAcquired = resources.pool
	if resources.heap > plan.MaxHeapAllocBytes || resources.pool > plan.MaxPoolAcquired {
		manifest.Failures = append(manifest.Failures, "observed resource high-water exceeded the admitted topology budget")
	}
	if e := validateInventoryMeasurementPlan(worker, plan); e != nil {
		manifest.Failures = append(manifest.Failures, "candidate, source, dependency or topology binding changed during measurement")
	}
	manifest.FinishedAt = time.Now().UTC()
	raw, marshalErr := json.Marshal(manifest)
	if marshalErr != nil {
		return manifest, marshalErr
	}
	if err = writeArtifact("manifest.json", raw); err != nil {
		return manifest, err
	}
	if len(manifest.Failures) > 0 {
		return manifest, errors.New("inventory measurement failed; inspect bounded raw artifacts")
	}
	return manifest, nil
}

func captureInventoryMeasurement(ctx context.Context, worker *LiveTransferWorker, c InventoryMeasurementCase, release string, maxArchiveRecords int64) ([]byte, error) {
	if c.ExpectedError == "parity" {
		return captureInventoryQuarantine(ctx, worker, c)
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a := worker.service.authority
	tx, err := a.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer inventoryRollback(tx)
	if err = a.epochGuard(bounded, tx); err != nil {
		return nil, err
	}
	var raw []byte
	var digest, indexedHash, indexedRelease, indexedGeneration string
	var indexedHeight, activeHeight int64
	partition := "125:" + c.Wallet + ":" + c.Token
	err = tx.QueryRow(bounded, `SELECT p.payload,p.snapshot_digest,p.height,p.block_hash,p.release_digest,active.height,active.generation FROM `+a.table("inventory_l16_active_v2")+` active JOIN `+a.table("inventory_l16_snapshots_v2")+` p USING(generation,partition,snapshot_digest) WHERE active.partition=$1`, partition).Scan(&raw, &digest, &indexedHeight, &indexedHash, &indexedRelease, &activeHeight, &indexedGeneration)
	if err != nil {
		return nil, err
	}
	if delivery.Hash(raw) != digest {
		return nil, delivery.ErrIntegrity
	}
	var snapshot RebuildSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	if indexedHeight != snapshot.Height || activeHeight != snapshot.Height || indexedHash != snapshot.BlockHash || indexedRelease != snapshot.ReleaseDigest || indexedGeneration != snapshot.Generation || snapshot.Generation != a.generation || snapshot.Partition != partition || snapshot.ReleaseDigest != release || snapshot.Height != c.Height || snapshot.BlockHash != c.BlockHash || snapshot.Algorithm != Algorithm || c.ExpectedState != nil && snapshot.State != *c.ExpectedState {
		observed, _ := json.Marshal(struct {
			CaseID     string          `json:"caseId"`
			Expected   *Inventory      `json:"expectedState"`
			Observed   json.RawMessage `json:"observedSnapshot"`
			SHA256     string          `json:"snapshotSha256"`
			ErrorClass string          `json:"errorClass"`
		}{c.ID, c.ExpectedState, raw, digest, "integrity"})
		return observed, delivery.ErrIntegrity
	}
	var admittedArchiveCount int64
	if err = tx.QueryRow(bounded, `SELECT count(*) FROM `+a.table("inventory_l16_archive_v2")+` WHERE generation=$1 AND partition=$2 AND (convert_from(payload,'UTF8')::jsonb->'Leg'->>'Height')::numeric<=$3`, a.generation, partition, c.Height).Scan(&admittedArchiveCount); err != nil {
		return raw, err
	}
	if admittedArchiveCount > maxArchiveRecords {
		return raw, errors.New("actual archive exceeds admitted resource envelope")
	}
	records, err := worker.service.readReplayArchiveTx(bounded, tx, c.Wallet, snapshot)
	if err != nil {
		return nil, err
	}
	var normalizedAverage *string
	if len(records) > 0 {
		decimals := uint8(*records[len(records)-1].TokenDecimals)
		average, e := Average(snapshot.State, &decimals)
		if e != nil {
			return raw, e
		}
		if average != nil {
			value := average.RatString()
			normalizedAverage = &value
		}
	}
	if c.ExpectedNormalizedAverage != nil && (normalizedAverage == nil || *normalizedAverage != *c.ExpectedNormalizedAverage) {
		return raw, delivery.ErrIntegrity
	}
	if len(snapshot.Checkpoints) == 0 {
		return raw, delivery.ErrIntegrity
	}
	var previous Checkpoint
	for _, checkpoint := range snapshot.Checkpoints {
		day, e := time.Parse("2006-01-02", checkpoint.Day)
		if e != nil {
			return raw, e
		}
		daily := []TimedLeg{}
		for _, record := range records {
			if !record.CommittedAt.Before(day) && record.CommittedAt.Before(day.AddDate(0, 0, 1)) {
				daily = append(daily, record.TimedLeg)
			}
		}
		rebuilt, e := RebuildDayPages(bounded, previous, daily, day)
		if e != nil || rebuilt != checkpoint || !checkpoint.CompleteHistory {
			return raw, delivery.ErrIntegrity
		}
		previous = rebuilt
	}
	checkpointRows, err := tx.Query(bounded, `SELECT checkpoint_digest,payload FROM `+a.table("inventory_l16_checkpoints_v2")+` WHERE generation=$1 AND partition=$2 AND snapshot_digest=$3 ORDER BY day`, a.generation, partition, digest)
	if err != nil {
		return raw, err
	}
	checkpointPayloads := []json.RawMessage{}
	for checkpointRows.Next() {
		var hash string
		var payload json.RawMessage
		if err = checkpointRows.Scan(&hash, &payload); err != nil {
			checkpointRows.Close()
			return raw, err
		}
		var bound struct {
			Checkpoint    Checkpoint `json:"checkpoint"`
			ReleaseDigest string     `json:"releaseDigest"`
			InputDigest   string     `json:"inputDigest"`
		}
		index := len(checkpointPayloads)
		if delivery.Hash(payload) != hash || json.Unmarshal(payload, &bound) != nil || index >= len(snapshot.Checkpoints) || bound.Checkpoint != snapshot.Checkpoints[index] || bound.ReleaseDigest != release || bound.InputDigest != snapshot.InputDigest {
			checkpointRows.Close()
			return raw, delivery.ErrIntegrity
		}
		checkpointPayloads = append(checkpointPayloads, payload)
	}
	err = checkpointRows.Err()
	checkpointRows.Close()
	if err != nil {
		return raw, err
	}
	if len(checkpointPayloads) != len(snapshot.Checkpoints) {
		return raw, delivery.ErrIntegrity
	}
	values, err := snapshot.State.values()
	if err != nil {
		return nil, err
	}
	if snapshot.NodeIDs != worker.service.nodeIDs || snapshot.NodeBalances[0] != snapshot.NodeBalances[1] || snapshot.NodeBalances[0] != add(values[0], values[1]).String() {
		return nil, ErrParity
	}
	var archiveCount, commitCount, pendingCount, publishedCount int64
	if err = tx.QueryRow(bounded, `SELECT count(*) FROM `+a.table("inventory_l16_archive_v2")+` WHERE generation=$1 AND partition=$2 AND (convert_from(payload,'UTF8')::jsonb->'Leg'->>'Height')::numeric<=$3`, a.generation, partition, c.Height).Scan(&archiveCount); err != nil {
		return nil, err
	}
	if archiveCount != int64(len(records)) {
		return nil, delivery.ErrIntegrity
	}
	if err = tx.QueryRow(bounded, `SELECT count(*) FROM `+a.table("inventory_l16_live_commits_v1")+` WHERE generation=$1 AND wallet=$2 AND height=$3 AND block_hash=$4`, a.generation, c.Wallet, c.Height, c.BlockHash).Scan(&commitCount); err != nil {
		return nil, err
	}
	if commitCount != 1 {
		return nil, delivery.ErrIntegrity
	}
	rows, err := tx.Query(bounded, `SELECT envelope,state,broker_stream,broker_sequence,published_at FROM `+a.table("delivery_outbox")+` WHERE subject=$1 AND convert_from(envelope,'UTF8')::jsonb->'payload'->>'snapshotId'=(SELECT accepted_snapshot::text FROM `+a.table("inventory_l16_live_owners_v1")+` WHERE wallet=$2) ORDER BY event_id LIMIT 2`, "portfolio.snapshot.v2", c.Wallet)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type publication struct {
		Envelope       json.RawMessage `json:"envelope"`
		State          string          `json:"state"`
		BrokerStream   *string         `json:"brokerStream"`
		BrokerSequence *int64          `json:"brokerSequence"`
		PublishedAt    *time.Time      `json:"publishedAt"`
	}
	publications := []publication{}
	for rows.Next() {
		var p publication
		if err = rows.Scan(&p.Envelope, &p.State, &p.BrokerStream, &p.BrokerSequence, &p.PublishedAt); err != nil {
			return nil, err
		}
		envelope, e := delivery.Decode(p.Envelope, delivery.Policy{Subjects: map[string]bool{"portfolio.snapshot.v2": true}, EventTypes: map[string]bool{"portfolio.snapshot.v2": true}}, "portfolio.snapshot.v2")
		if e != nil {
			return nil, e
		}
		if envelope.ChainID != 125 {
			return nil, delivery.ErrIntegrity
		}
		if p.State == "pending" {
			pendingCount++
		} else if p.State == "published" {
			publishedCount++
			if p.BrokerStream == nil || p.BrokerSequence == nil || *p.BrokerSequence <= 0 || p.PublishedAt == nil {
				return nil, delivery.ErrIntegrity
			}
		} else {
			return nil, delivery.ErrIntegrity
		}
		publications = append(publications, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(publications) != 1 {
		return nil, delivery.ErrIntegrity
	}
	planRows, err := tx.Query(bounded, `EXPLAIN (FORMAT JSON) SELECT payload FROM `+a.table("inventory_l16_archive_v2")+` WHERE generation=$1 AND partition=$2`, a.generation, partition)
	if err != nil {
		return nil, err
	}
	var queryPlan json.RawMessage
	if !planRows.Next() {
		planRows.Close()
		return nil, delivery.ErrIntegrity
	}
	if err = planRows.Scan(&queryPlan); err != nil {
		planRows.Close()
		return nil, err
	}
	err = planRows.Err()
	planRows.Close()
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(struct {
		CaseID                string            `json:"caseId"`
		SnapshotSHA256        string            `json:"snapshotSha256"`
		Snapshot              json.RawMessage   `json:"snapshot"`
		Archive               []EconomicRecord  `json:"archive"`
		ArchiveCount          int64             `json:"archiveCount"`
		CanonicalCommitCount  int64             `json:"canonicalCommitCount"`
		PendingPublications   int64             `json:"pendingPublications"`
		PublishedPublications int64             `json:"publishedPublications"`
		Publications          []publication     `json:"publications"`
		QueryPlan             json.RawMessage   `json:"queryPlan"`
		CheckpointPayloads    []json.RawMessage `json:"checkpointPayloads"`
		NormalizedAverage     *string           `json:"normalizedAverageUsdlPerToken"`
	}{c.ID, digest, raw, records, archiveCount, commitCount, pendingCount, publishedCount, publications, queryPlan, checkpointPayloads, normalizedAverage})
	if err != nil {
		return nil, err
	}
	if err = a.epochGuard(bounded, tx); err != nil {
		return nil, err
	}
	if err = tx.Commit(bounded); err != nil {
		return nil, err
	}
	return raw, nil
}

func measurementFileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type inventoryMeasurementHTTPFacts struct {
	OriginNS, RoundTripNS *int64
	ServerTiming          string
}

func inventoryOriginTiming(header string) (int64, error) {
	prefix := "portfolio_origin;dur="
	if !strings.HasPrefix(header, prefix) {
		return 0, errors.New("portfolio origin timing absent")
	}
	value := strings.TrimPrefix(header, prefix)
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,6})?$`).MatchString(value) || len(value) > 20 {
		return 0, errors.New("invalid origin timing")
	}
	duration, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0, errors.New("invalid origin timing")
	}
	duration.Mul(duration, new(big.Rat).SetInt64(1000000))
	if !duration.IsInt() || !duration.Num().IsInt64() {
		return 0, errors.New("unbounded origin timing")
	}
	return duration.Num().Int64(), nil
}

func validateInventoryHTTPResponse(raw []byte, c InventoryMeasurementCase) error {
	var response struct {
		Version int `json:"version"`
		l17.PortfolioResponse
	}
	if json.Unmarshal(raw, &response) != nil || response.Version != 3 || response.Algorithm != Algorithm || response.Page.Summary.Wallet != c.Wallet || response.Page.Summary.Watermark.ChainID != "125" || response.Page.Summary.Watermark.Height != strconv.FormatInt(c.Height, 10) || response.Page.Summary.Watermark.BlockHash != c.BlockHash || response.Page.Summary.Watermark.Integrity != "healthy" {
		return delivery.ErrIntegrity
	}
	found := false
	for _, position := range response.Page.Positions {
		if position.Wallet != c.Wallet {
			return delivery.ErrIntegrity
		}
		derived, _, err := l17.Derive(position)
		if err != nil {
			return err
		}
		original, _ := json.Marshal(position)
		computed, _ := json.Marshal(derived)
		if string(original) != string(computed) {
			return delivery.ErrIntegrity
		}
		if position.TokenAddress != c.Token {
			continue
		}
		if found {
			return delivery.ErrIntegrity
		}
		found = true
		if c.ExpectedState != nil && (position.KnownQuantityRaw != c.ExpectedState.Known || position.UnknownQuantityRaw != c.ExpectedState.Unknown || position.KnownRemainingCostUsdlRaw != c.ExpectedState.Cost || position.KnownRealizedSubtotalUsdlRaw != c.ExpectedState.Realized) {
			return delivery.ErrIntegrity
		}
		if c.PricingQuality != "" {
			if position.PricingQuality != c.PricingQuality || (position.UnrealizedPnlRaw == nil) != (c.ExpectedUnrealized == nil) || position.UnrealizedPnlRaw != nil && *position.UnrealizedPnlRaw != *c.ExpectedUnrealized {
				return delivery.ErrIntegrity
			}
		}
	}
	if !found {
		return delivery.ErrGap
	}
	return nil
}

func captureInventoryQuarantine(ctx context.Context, worker *LiveTransferWorker, c InventoryMeasurementCase) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	a := worker.service.authority
	tx, err := a.pool.BeginTx(bounded, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return nil, err
	}
	defer inventoryRollback(tx)
	if err = a.epochGuard(bounded, tx); err != nil {
		return nil, err
	}
	rows, err := tx.Query(bounded, `SELECT height,block_hash,reason,payload FROM `+a.table("inventory_l16_quarantine_v2")+` WHERE generation=$1 AND partition=$2 AND height<=$3 ORDER BY height,block_hash LIMIT 1001`, a.generation, "125:"+c.Wallet+":"+c.Token, c.Height)
	if err != nil {
		return nil, err
	}
	type actualQuarantine struct {
		Height    int64           `json:"height"`
		BlockHash string          `json:"blockHash"`
		Reason    string          `json:"reason"`
		Payload   json.RawMessage `json:"payload"`
		SHA256    string          `json:"sha256"`
	}
	entries := []actualQuarantine{}
	for rows.Next() {
		var entry actualQuarantine
		if err = rows.Scan(&entry.Height, &entry.BlockHash, &entry.Reason, &entry.Payload); err != nil {
			rows.Close()
			return nil, err
		}
		if !hash.MatchString(entry.BlockHash) || !json.Valid(entry.Payload) {
			rows.Close()
			return nil, delivery.ErrIntegrity
		}
		entry.SHA256 = delivery.Hash(entry.Payload)
		entries = append(entries, entry)
		if len(entries) > 1000 {
			rows.Close()
			return nil, errors.New("quarantine evidence exceeds admitted capture bound")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		CaseID           string             `json:"caseId"`
		ExpectedError    string             `json:"expectedError"`
		Quarantine       []actualQuarantine `json:"quarantine"`
		ReleaseQualified bool               `json:"releaseQualified"`
	}{c.ID, c.ExpectedError, entries, false})
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return raw, delivery.ErrIntegrity
	}
	if err = a.epochGuard(bounded, tx); err != nil {
		return raw, err
	}
	if err = tx.Commit(bounded); err != nil {
		return raw, err
	}
	return raw, ErrParity
}

type inventoryResourceObserver struct {
	heap   uint64
	pool   int32
	done   chan struct{}
	cancel chan struct{}
	once   sync.Once
}

func startInventoryResourceObserver(ctx context.Context, worker *LiveTransferWorker) *inventoryResourceObserver {
	observer := &inventoryResourceObserver{done: make(chan struct{}), cancel: make(chan struct{})}
	go func() {
		defer close(observer.done)
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			if memory.HeapAlloc > observer.heap {
				observer.heap = memory.HeapAlloc
			}
			acquired := worker.service.authority.pool.Stat().AcquiredConns()
			if acquired > observer.pool {
				observer.pool = acquired
			}
			select {
			case <-ticker.C:
			case <-observer.cancel:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
	return observer
}
func (observer *inventoryResourceObserver) stop() {
	observer.once.Do(func() { close(observer.cancel) })
	<-observer.done
}
