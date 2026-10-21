package readpolicy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

type MeasurementBinding struct {
	CandidateSHA256   string `json:"candidateSha256"`
	InterfaceSHA256   string `json:"interfaceSha256"`
	ManifestSHA256    string `json:"manifestSha256"`
	TopologySHA256    string `json:"topologySha256"`
	EnvironmentSHA256 string `json:"environmentSha256"`
	BuildSHA256       string `json:"buildSha256"`
	WorkloadSeed      string `json:"workloadSeed"`
	ProfileVersion    string `json:"profileVersion"`
}

func (b MeasurementBinding) Validate() error {
	digest := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, value := range []string{b.CandidateSHA256, b.InterfaceSHA256, b.ManifestSHA256, b.TopologySHA256, b.EnvironmentSHA256, b.BuildSHA256} {
		if !digest.MatchString(value) {
			return errors.New("measurement requires exact candidate/interface/manifest/topology/environment/build digests")
		}
	}
	identity := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	if !identity.MatchString(b.WorkloadSeed) || !identity.MatchString(b.ProfileVersion) {
		return errors.New("measurement seed and profile identity required")
	}
	return nil
}

type BoundMeasurementArtifact struct {
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	MaxBytes int64  `json:"maxBytes"`
}

func (a BoundMeasurementArtifact) Open() (*os.File, error) {
	if a.Path == "" || a.MaxBytes < 1 || a.MaxBytes > 1<<40 || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(a.SHA256) {
		return nil, errors.New("bounded artifact identity required")
	}
	info, err := os.Lstat(a.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > a.MaxBytes {
		return nil, errors.New("missing, symlinked or oversized measurement artifact")
	}
	file, err := os.Open(a.Path)
	if err != nil {
		return nil, err
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, errors.New("measurement artifact changed while opening")
	}
	hash := sha256.New()
	count, err := io.Copy(hash, io.LimitReader(file, a.MaxBytes+1))
	if err != nil || count != info.Size() || count > a.MaxBytes || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
		file.Close()
		return nil, errors.New("measurement artifact digest or size differs")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

type MeasurementSample struct {
	Sequence              uint64 `json:"sequence"`
	RequestID             string `json:"requestId"`
	Scenario              string `json:"scenario"`
	ChainID               string `json:"chainId"`
	Height                string `json:"height,omitempty"`
	BlockHash             string `json:"blockHash,omitempty"`
	ParentHash            string `json:"parentHash,omitempty"`
	BlockTimeSeconds      uint64 `json:"blockTimeSeconds,omitempty"`
	ObservedUTCNS         int64  `json:"observedUtcNs"`
	FinishedUTCNS         int64  `json:"finishedUtcNs"`
	ElapsedNS             int64  `json:"elapsedNs"`
	DurableVisibilityNS   int64  `json:"durableVisibilityNs,omitempty"`
	DatabaseRecordedUTCNS int64  `json:"databaseRecordedUtcNs,omitempty"`
	EndpointID            string `json:"endpointId,omitempty"`
	Outcome               string `json:"outcome"`
	RawCount              int64  `json:"rawCount,omitempty"`
	UpstreamGroups        uint64 `json:"upstreamGroups,omitempty"`
	CacheHits             uint64 `json:"cacheHits,omitempty"`
	JoinedWaiters         uint64 `json:"joinedWaiters,omitempty"`
	PayloadSHA256         string `json:"payloadSha256,omitempty"`
	RPCRequests           uint64 `json:"rpcRequests,omitempty"`
	WindowNS              int64  `json:"windowNs,omitempty"`
	RPCAttempts           uint64 `json:"rpcAttempts,omitempty"`
	RPCRetryableFailures  uint64 `json:"rpcRetryableFailures,omitempty"`
	RPCSemanticFailures   uint64 `json:"rpcSemanticFailures,omitempty"`
	RPCCanceledAttempts   uint64 `json:"rpcCanceledAttempts,omitempty"`
}

type LatencyDistribution struct {
	Count         uint64 `json:"count"`
	P50UpperNS    int64  `json:"p50UpperNs"`
	P95UpperNS    int64  `json:"p95UpperNs"`
	P99UpperNS    int64  `json:"p99UpperNs"`
	MaxNS         int64  `json:"maxNs"`
	BucketWidthNS int64  `json:"bucketWidthNs"`
}

type latencyHistogram struct {
	counts  [30001]uint64
	total   uint64
	maximum int64
}

func (h *latencyHistogram) add(ns int64) error {
	if ns < 0 || ns > int64(30*time.Second) {
		return errors.New("measurement latency outside30second bounded histogram")
	}
	bucket := (ns + int64(time.Millisecond) - 1) / int64(time.Millisecond)
	h.counts[bucket]++
	h.total++
	if ns > h.maximum {
		h.maximum = ns
	}
	return nil
}
func (h *latencyHistogram) percentile(percent uint64) int64 {
	rank := (h.total*percent + 99) / 100
	var count uint64
	for index, value := range h.counts {
		count += value
		if count >= rank {
			return int64(index) * int64(time.Millisecond)
		}
	}
	return 0
}
func (h *latencyHistogram) distribution() (LatencyDistribution, error) {
	if h.total == 0 {
		return LatencyDistribution{}, errors.New("actual latency samples required")
	}
	return LatencyDistribution{h.total, h.percentile(50), h.percentile(95), h.percentile(99), h.maximum, int64(time.Millisecond)}, nil
}

type MeasurementCapture struct {
	mu         sync.Mutex
	file       *os.File
	directory  string
	binding    MeasurementBinding
	started    time.Time
	maxRows    uint64
	maxBytes   int64
	rows       uint64
	bytes      int64
	histograms map[string]*latencyHistogram
	failures   map[string]uint64
	requestIDs map[string]struct{}
	artifacts  map[string]BoundMeasurementArtifact
	closed     bool
	failed     error
}

type MeasurementResult struct {
	Binding                            MeasurementBinding                  `json:"binding"`
	StartedUTCNS                       int64                               `json:"startedUtcNs"`
	EndedUTCNS                         int64                               `json:"endedUtcNs"`
	RawRows                            uint64                              `json:"rawRows"`
	RawBytes                           int64                               `json:"rawBytes"`
	RawSHA256                          string                              `json:"rawSha256"`
	RawPath                            string                              `json:"rawPath"`
	Distributions                      map[string]LatencyDistribution      `json:"distributions"`
	Failures                           map[string]uint64                   `json:"failures"`
	LaneHarnessPassed                  bool                                `json:"laneHarnessPassed"`
	FinalCapacityQualified             bool                                `json:"finalCapacityQualified"`
	FinalCapacityTask                  string                              `json:"finalCapacityTask"`
	FullProtocolCompatibilityQualified bool                                `json:"fullProtocolCompatibilityQualified"`
	UnresolvedReleaseRequirements      []string                            `json:"unresolvedReleaseRequirements"`
	FailureCode                        string                              `json:"failureCode,omitempty"`
	Inputs                             map[string]BoundMeasurementArtifact `json:"inputs"`
}

func NewMeasurementCapture(directory string, binding MeasurementBinding, maxRows uint64, maxBytes int64) (*MeasurementCapture, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	if directory == "" || maxRows < 1000 || maxRows > 1000000000 || maxBytes < 1<<20 || maxBytes > 1<<40 {
		return nil, errors.New("explicit bounded raw capture budget required")
	}
	if err := os.MkdirAll(filepath.Dir(directory), 0700); err != nil {
		return nil, err
	}
	if err := os.Mkdir(directory, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(directory, "chain-samples.ndjson"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	return &MeasurementCapture{file: file, directory: directory, binding: binding, started: time.Now().UTC(), maxRows: maxRows, maxBytes: maxBytes, histograms: make(map[string]*latencyHistogram), failures: make(map[string]uint64), requestIDs: make(map[string]struct{}), artifacts: make(map[string]BoundMeasurementArtifact)}, nil
}

func (c *MeasurementCapture) OpenArtifact(name string, artifact BoundMeasurementArtifact) (*os.File, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.failed != nil || len(c.artifacts) >= 32 || !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`).MatchString(name) {
		return nil, errors.New("invalid or exhausted measurement artifact binding")
	}
	if _, exists := c.artifacts[name]; exists {
		return nil, errors.New("duplicate measurement artifact binding")
	}
	file, err := artifact.Open()
	if err != nil {
		return nil, err
	}
	c.artifacts[name] = artifact
	return file, nil
}

func (c *MeasurementCapture) Append(sample MeasurementSample) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.failed != nil {
		return errors.New("measurement capture closed or failed")
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	if !valid.MatchString(sample.RequestID) || !valid.MatchString(sample.Scenario) || sample.ChainID != "125" || sample.ObservedUTCNS <= 0 || sample.FinishedUTCNS < sample.ObservedUTCNS || sample.ElapsedNS < 0 || sample.DurableVisibilityNS < 0 || sample.RawCount < 0 || !valid.MatchString(sample.Outcome) {
		c.failed = errors.New("invalid measured sample identity/timestamps")
		return c.failed
	}
	if sample.Height != "" {
		if _, err := ParseHeight(sample.Height); err != nil {
			c.failed = err
			return err
		}
	}
	if sample.BlockHash != "" && !regexp.MustCompile(`^0x[0-9a-f]{64}$`).MatchString(sample.BlockHash) {
		c.failed = errors.New("invalid measured block hash")
		return c.failed
	}
	if _, exists := c.requestIDs[sample.RequestID]; exists {
		c.failed = errors.New("duplicate measured request identity")
		return c.failed
	}
	if len(c.requestIDs) >= 1000000 {
		c.failed = errors.New("measurement request identity memory budget exhausted")
		return c.failed
	}
	sample.Sequence = c.rows + 1
	data, err := json.Marshal(sample)
	if err != nil {
		c.failed = err
		return err
	}
	data = append(data, '\n')
	if len(data) > 65536 || c.rows >= c.maxRows || int64(len(data)) > c.maxBytes-c.bytes {
		c.failed = errors.New("raw measurement capture budget exhausted")
		return c.failed
	}
	if _, err := c.file.Write(data); err != nil {
		c.failed = err
		return err
	}
	c.rows++
	c.bytes += int64(len(data))
	c.requestIDs[sample.RequestID] = struct{}{}
	if sample.Outcome != "success" {
		c.failures[sample.Scenario]++
	}
	h := c.histograms[sample.Scenario]
	if h == nil {
		if len(c.histograms) >= 32 {
			c.failed = errors.New("measurement scenario budget exhausted")
			return c.failed
		}
		h = &latencyHistogram{}
		c.histograms[sample.Scenario] = h
	}
	if err := h.add(sample.ElapsedNS); err != nil {
		c.failed = err
		return err
	}
	return nil
}

func ParseHeight(value string) (uint64, error) {
	if value == "" || len(value) > 20 {
		return 0, errors.New("invalid measured height")
	}
	var n uint64
	for _, digit := range value {
		if digit < '0' || digit > '9' || n > (^uint64(0)-uint64(digit-'0'))/10 {
			return 0, errors.New("invalid measured height")
		}
		n = n*10 + uint64(digit-'0')
	}
	return n, nil
}

func (c *MeasurementCapture) Finish(passed bool, failureCode string) (MeasurementResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return MeasurementResult{}, errors.New("measurement already finalized")
	}
	c.closed = true
	result := MeasurementResult{Binding: c.binding, StartedUTCNS: c.started.UnixNano(), EndedUTCNS: time.Now().UTC().UnixNano(), RawRows: c.rows, RawBytes: c.bytes, RawPath: filepath.Join(c.directory, "chain-samples.ndjson"), Distributions: make(map[string]LatencyDistribution), Failures: c.failures, LaneHarnessPassed: passed, FinalCapacityQualified: false, FinalCapacityTask: "9.1", FailureCode: failureCode, Inputs: c.artifacts, FullProtocolCompatibilityQualified: false, UnresolvedReleaseRequirements: []string{"D01_031.1 full enabled selector/event-origin matrix and cross-lane proxy/asset-scale artifacts", "D01_031.2 serial9.1 full24hour100000connection frozen mixed soak", "D01_031.3 serial9.1 five-minute10000events/s burst"}}
	var failedRows uint64
	for _, count := range c.failures {
		failedRows += count
	}
	if c.failed != nil || c.rows == 0 || failedRows > 0 {
		result.LaneHarnessPassed = false
		if result.FailureCode == "" {
			result.FailureCode = "RAW_MEASUREMENT_INCOMPLETE"
		}
	}
	if err := c.file.Sync(); err != nil {
		c.file.Close()
		return result, err
	}
	if err := c.file.Close(); err != nil {
		return result, err
	}
	file, err := os.Open(result.RawPath)
	if err != nil {
		return result, err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	file.Close()
	if err != nil {
		return result, err
	}
	result.RawSHA256 = hex.EncodeToString(hash.Sum(nil))
	for scenario, h := range c.histograms {
		distribution, err := h.distribution()
		if err != nil {
			return result, err
		}
		result.Distributions[scenario] = distribution
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return result, err
	}
	output, err := os.OpenFile(filepath.Join(c.directory, "measurement-result.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	if _, err = output.Write(append(data, '\n')); err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		directory, openErr := os.Open(c.directory)
		if openErr != nil {
			err = openErr
		} else {
			err = directory.Sync()
			directory.Close()
		}
	}
	return result, err
}
