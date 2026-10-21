package source

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CapacityProcessor interface {
	ProcessBatch(context.Context, int64, int64) error
}
type CapacityProviders struct {
	Live       *CommittedSource
	Historical *HistoricalSource
	Head       HeadSource
	Processor  CapacityProcessor
	Pool       *pgxpool.Pool
}
type CapacityBoundFile struct{ Path, SHA256 string }
type CapacityBinding struct {
	Revision, SourceSHA256, InterfaceSHA256, ManifestDigest, EndpointConfigurationSHA256, ApprovalSHA256 string
	Files                                                                                                []CapacityBoundFile
}
type CapacityCommand struct {
	Executable, SHA256 string
	Args               []string
	Environment        []string
}
type CapacityOptions struct {
	Binding                               CapacityBinding
	SourceRoot, StreamID, OutputDirectory string
	Duration                              time.Duration
	TargetEventsPerSecond                 uint64
	Profile                               string
	HistoricalFrom, HistoricalTo          uint64
	ParityFrom, ParityTo                  uint64
	MaxCaptureBytes                       uint64
	MaxHeapBytes                          uint64
	ApprovalDocument, InterfaceDocument   CapacityBoundFile
	Workload                              CapacityCommand
	MutationApproved                      bool
}
type CapacityMeasurement struct {
	ObservedAt                                                   time.Time
	InFlight, ProcessPermits, HistoricalPermits, HistoricalQueue int64
	PendingRanges                                                int
	HistoricalPaused                                             bool
	RPCRequests, Throttles, InvalidResponses, Timeouts           uint64
	Source                                                       SourceMetrics
	HeapBytes, TotalAllocatedBytes, GCCycles                     uint64
	HeaderCacheEntries                                           int
	EndpointInFlight                                             map[string]int
	EndpointHealth                                               []EndpointHealth
}
type CapacityBatchSample struct {
	From, To              uint64
	ObservedAt, DurableAt time.Time
	RawCount              uint64
	RPCRequests           uint64
	PayloadSHA256         string
	LatencyNanoseconds    int64
}
type CapacityReport struct {
	Version                                                                                           int
	Binding                                                                                           CapacityBinding
	Profile                                                                                           string
	RPCMetricScope                                                                                    string
	TargetEventsPerSecond                                                                             uint64
	StartedAt, FinishedAt                                                                             time.Time
	InputEvents, DurableEvents, Batches, FailedAttempts, HistoricalRanges, HistoricalFailures         uint64
	ElapsedNanoseconds, P99Nanoseconds                                                                int64
	RPCRequests, RPCCallsPer1000Numerator, RPCCallsPer1000Denominator, CacheHits, HeaderFetches       uint64
	CacheHitRatioNumerator, CacheHitRatioDenominator                                                  uint64
	PeakSampledInFlight, PeakSampledProcessPermits, PeakSampledHistoricalPermits, PeakHistoricalQueue int64
	PeakPendingRanges, PeakHeaderCacheEntries                                                         int
	PeakHeapBytes, TotalAllocatedBytes, GCCycles                                                      uint64
	LagPauseObserved, LagResumeObserved                                                               bool
	LagTransitionsValidated                                                                           bool
	DBClockOffsetLowerNanoseconds, DBClockOffsetUpperNanoseconds                                      int64
	LatencyResolutionNanoseconds                                                                      int64
	ObserverRPCRequestsUpperBound                                                                     uint64
	InputSecondCounts                                                                                 map[int64]uint64
	Parity                                                                                            *ParityReport
	RawCaptureSHA256                                                                                  string
	RawCaptureBytes                                                                                   uint64
	ReportID                                                                                          string
	ClockSamples                                                                                      []CapacityClockSample
	FaultObservations                                                                                 []CapacityFaultObservation
	Throttles, InvalidResponses, Timeouts                                                             uint64
	HarnessPassed, CapacityTargetQualified, SharedSoakQualified                                       bool
	Failures                                                                                          []string
}
type CapacityLagSample struct {
	ObservedAt, HighSince, LowSince time.Time
	LagNanoseconds                  int64
	Paused                          bool
	Head, Durable                   uint64
}
type CapacityClockSample struct {
	ObservedAt                                     time.Time
	OffsetLowerNanoseconds, OffsetUpperNanoseconds int64
}
type CapacityFaultObservation struct {
	Kind, SourceID, State string
	ObservedAt            time.Time
}
type capacityWorkloadRecord struct {
	Kind  string
	Fault *CapacityFaultObservation
	Value json.RawMessage
}
type capacityCapture struct {
	mu        sync.Mutex
	file      *os.File
	hash      io.Writer
	digest    interface{ Sum([]byte) []byte }
	used, max uint64
}

func newCapacityCapture(path string, maximum uint64) (*capacityCapture, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	return &capacityCapture{file: f, hash: io.MultiWriter(f, h), digest: h, max: maximum}, nil
}
func (c *capacityCapture) write(kind string, value any) error {
	data, err := json.Marshal(struct {
		Kind  string
		Value any
	}{kind, value})
	if err != nil {
		return err
	}
	data = append(data, '\n')
	c.mu.Lock()
	defer c.mu.Unlock()
	if uint64(len(data)) > c.max-c.used {
		return sourceError(OversizeError, "capacity-capture", nil)
	}
	n, err := c.hash.Write(data)
	c.used += uint64(n)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}
func (c *capacityCapture) finish() (string, uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.file.Sync()
	err = errors.Join(err, c.file.Close())
	return hex.EncodeToString(c.digest.Sum(nil)), c.used, err
}
func capacityHash(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}
func capacityStreamID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.' || r == ':' || r == '-') {
			return false
		}
	}
	return true
}
func capacitySHA(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && value == strings.ToLower(value)
}
func verifyCapacityBinding(ctx context.Context, options CapacityOptions, p CapacityProviders) error {
	b := options.Binding
	for _, bound := range []CapacityBoundFile{options.ApprovalDocument, options.InterfaceDocument} {
		if !filepath.IsAbs(bound.Path) || !capacitySHA(bound.SHA256) {
			return sourceError(IdentityError, "capacity-bound-input", nil)
		}
		f, err := os.Open(bound.Path)
		if err != nil {
			return sourceError(IdentityError, "capacity-bound-input", nil)
		}
		h := sha256.New()
		_, readErr := io.Copy(h, f)
		_ = f.Close()
		if readErr != nil || hex.EncodeToString(h.Sum(nil)) != bound.SHA256 {
			return sourceError(IntegrityError, "capacity-bound-input", nil)
		}
	}
	if options.ApprovalDocument.SHA256 != b.ApprovalSHA256 || options.InterfaceDocument.SHA256 != b.InterfaceSHA256 {
		return sourceError(IdentityError, "capacity-bound-input", nil)
	}
	if !capacitySHA(b.SourceSHA256) || !capacitySHA(b.InterfaceSHA256) || !capacitySHA(b.EndpointConfigurationSHA256) || !capacitySHA(b.ApprovalSHA256) || b.ManifestDigest == "" || !filepath.IsAbs(options.SourceRoot) || len(b.Files) == 0 || len(b.Files) > 100000 {
		return sourceError(IdentityError, "capacity-binding", nil)
	}
	if len(b.Revision) != 40 && len(b.Revision) != 64 {
		return sourceError(IdentityError, "capacity-revision", nil)
	}
	revision, err := exec.CommandContext(ctx, "git", "-C", options.SourceRoot, "rev-parse", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(revision)) != b.Revision {
		return sourceError(IdentityError, "capacity-revision", nil)
	}
	actual := make([]CapacityBoundFile, 0, len(b.Files))
	seen := map[string]bool{}
	for _, file := range b.Files {
		path := filepath.Clean(file.Path)
		if path != file.Path || filepath.IsAbs(path) || path == "." || strings.HasPrefix(path, "../") || seen[path] || !capacitySHA(file.SHA256) {
			return sourceError(IdentityError, "capacity-source-file", nil)
		}
		seen[path] = true
		resolved, err := filepath.EvalSymlinks(filepath.Join(options.SourceRoot, path))
		if err != nil {
			return sourceError(IdentityError, "capacity-source-file", nil)
		}
		relative, err := filepath.Rel(options.SourceRoot, resolved)
		if err != nil || strings.HasPrefix(relative, "../") {
			return sourceError(IdentityError, "capacity-source-file", nil)
		}
		f, err := os.Open(resolved)
		if err != nil {
			return err
		}
		h := sha256.New()
		_, readErr := io.Copy(h, f)
		closeErr := f.Close()
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		digest := hex.EncodeToString(h.Sum(nil))
		if digest != file.SHA256 {
			return sourceError(IntegrityError, "capacity-source-file", nil)
		}
		actual = append(actual, CapacityBoundFile{path, digest})
	}
	for _, directory := range []string{"services/launchpad/indexer/internal/source", "services/launchpad/indexer/internal/processor", "services/launchpad/indexer/internal/app", "services/launchpad/indexer/internal/store", "services/launchpad/indexer/internal/receipt"} {
		entries, err := os.ReadDir(filepath.Join(options.SourceRoot, directory))
		if err != nil {
			return sourceError(IdentityError, "capacity-source-inventory", nil)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !seen[filepath.Join(directory, entry.Name())] {
				return sourceError(IncompleteError, "capacity-source-inventory", nil)
			}
		}
	}
	for _, path := range []string{"services/launchpad/indexer/go.mod", "services/launchpad/indexer/go.sum"} {
		if !seen[path] {
			return sourceError(IncompleteError, "capacity-source-inventory", nil)
		}
	}
	sort.Slice(actual, func(i, j int) bool { return actual[i].Path < actual[j].Path })
	digest, err := capacityHash(actual)
	if err != nil || digest != b.SourceSHA256 {
		return sourceError(IntegrityError, "capacity-source", nil)
	}
	registry, err := capacityHash(p.Live.opts.Endpoints)
	if err != nil || registry != b.EndpointConfigurationSHA256 || p.Live.opts.ManifestDigest != b.ManifestDigest || p.Historical.RPC != p.Live {
		return sourceError(IdentityError, "capacity-endpoints", nil)
	}
	return nil
}
func (s *CommittedSource) capacityMeasurement() CapacityMeasurement {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	metrics := s.TransportMetrics()
	snapshot := CapacityMeasurement{ObservedAt: time.Now().UTC(), InFlight: metrics.InFlightRequests, ProcessPermits: int64(len(processPermits)), HistoricalPermits: int64(len(historicalPermits)), HistoricalQueue: metrics.HistoricalQueueDepth, PendingRanges: metrics.PendingRanges, HistoricalPaused: metrics.HistoricalPaused, RPCRequests: s.ids.Load(), Throttles: s.transport.throttles.Load(), InvalidResponses: s.transport.invalidResponses.Load(), Timeouts: s.transport.timeouts.Load(), Source: s.Metrics(), HeapBytes: memory.HeapAlloc, TotalAllocatedBytes: memory.TotalAlloc, GCCycles: uint64(memory.NumGC), EndpointInFlight: map[string]int{}}
	s.mu.Lock()
	snapshot.HeaderCacheEntries = len(s.cache)
	for _, endpoint := range s.endpoints {
		snapshot.EndpointInFlight[endpoint.id] = len(endpoint.permits)
	}
	s.mu.Unlock()
	snapshot.EndpointHealth = s.EndpointHealth()
	return snapshot
}
func capacityP99(histogram map[int64]uint64, count uint64) (int64, error) {
	if count == 0 {
		return 0, sourceError(IncompleteError, "capacity-latency", nil)
	}
	target := count - count/100
	keys := make([]int64, 0, len(histogram))
	var population uint64
	for bucket, n := range histogram {
		if bucket < 0 || bucket > math.MaxInt64/int64(time.Microsecond) || n > ^uint64(0)-population {
			return 0, sourceError(InvalidError, "capacity-latency", nil)
		}
		population += n
		keys = append(keys, bucket)
	}
	if population != count {
		return 0, sourceError(IncompleteError, "capacity-latency-population", nil)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	var total uint64
	for _, bucket := range keys {
		total += histogram[bucket]
		if total >= target {
			return bucket * int64(time.Microsecond), nil
		}
	}
	return 0, sourceError(IncompleteError, "capacity-latency", nil)
}
func capacityDurable(ctx context.Context, pool *pgxpool.Pool, stream string, from, to uint64, expected FetchResult, observations map[uint64]time.Time, clockOffsetLower int64, histogram map[int64]uint64, capture *capacityCapture) (time.Time, uint64, error) {
	read, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(read, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return time.Time{}, 0, err
	}
	defer func() {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(read), 2*time.Second)
		defer cancelCleanup()
		_ = tx.Rollback(cleanup)
	}()
	rows, err := tx.Query(read, `SELECT r.height,r.block_hash,r.transaction_hash,r.transaction_index,r.log_index,r.emitter,r.topics,r.data,pg_xact_commit_timestamp(c.xmin) FROM indexer.indexer_raw_logs r JOIN indexer.indexer_ingest_commits c ON c.chain_id=r.chain_id AND r.height>c.previous_height AND r.height<=c.last_complete_height WHERE r.chain_id=125 AND c.stream_id=$1 AND r.height BETWEEN $2 AND $3 ORDER BY r.height,r.transaction_index,r.log_index`, stream, int64(from), int64(to))
	if err != nil {
		return time.Time{}, 0, err
	}
	wanted := make(map[RawParityIdentity]ethtypes.Log, len(expected.Logs))
	for _, log := range expected.Logs {
		key := RawParityIdentity{125, log.BlockHash, log.TxHash, log.Index}
		if _, ok := wanted[key]; ok {
			rows.Close()
			return time.Time{}, 0, sourceError(IntegrityError, "capacity-duplicate-input", nil)
		}
		wanted[key] = log
	}
	seen := map[RawParityIdentity]bool{}
	var latest time.Time
	var count uint64
	for rows.Next() {
		var height int64
		var block, transaction, emitter string
		var txIndex, logIndex int64
		var topics []string
		var data []byte
		var committed *time.Time
		if err = rows.Scan(&height, &block, &transaction, &txIndex, &logIndex, &emitter, &topics, &data, &committed); err != nil {
			rows.Close()
			return latest, count, err
		}
		observed, observedOK := observations[uint64(height)]
		observedLower := observed.Add(time.Duration(clockOffsetLower))
		if !observedOK || committed == nil || committed.Before(observedLower) || !validHash(block) || !validHash(transaction) || !common.IsHexAddress(emitter) || txIndex < 0 || logIndex < 0 || txIndex > math.MaxUint32 || logIndex > math.MaxUint32 {
			rows.Close()
			return latest, count, sourceError(IntegrityError, "capacity-durable-row", nil)
		}
		key := RawParityIdentity{125, common.HexToHash(block), common.HexToHash(transaction), uint(logIndex)}
		log, ok := wanted[key]
		if !ok || seen[key] || log.BlockNumber != uint64(height) || log.TxIndex != uint(txIndex) || log.Address != common.HexToAddress(emitter) || len(log.Topics) != len(topics) || !bytes.Equal(log.Data, data) {
			rows.Close()
			return latest, count, sourceError(IntegrityError, "capacity-durable-identity", nil)
		}
		for i, topic := range topics {
			if !validHash(topic) || log.Topics[i] != common.HexToHash(topic) {
				rows.Close()
				return latest, count, sourceError(IntegrityError, "capacity-durable-payload", nil)
			}
		}
		if err = capture.write("durable_raw", struct {
			Identity                RawParityIdentity
			Height                  uint64
			TransactionIndex        uint
			Emitter                 string
			Topics                  []string
			Data                    []byte
			ObservedAt, CommittedAt time.Time
		}{key, uint64(height), uint(txIndex), strings.ToLower(emitter), topics, data, observed, committed.UTC()}); err != nil {
			rows.Close()
			return latest, count, err
		}
		elapsed := committed.Sub(observedLower).Nanoseconds()
		histogram[(elapsed+999)/1000]++
		seen[key] = true
		count++
		if committed.After(latest) {
			latest = committed.UTC()
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return latest, count, err
	}
	if count != uint64(len(wanted)) {
		return latest, count, sourceError(IncompleteError, "capacity-missing-identity", nil)
	}
	blockRows, err := tx.Query(read, `SELECT height,block_hash,raw_count,fetch_complete,required_decode_complete FROM indexer.indexer_block_completeness WHERE chain_id=125 AND stream_id=$1 AND height BETWEEN $2 AND $3 ORDER BY height`, stream, int64(from), int64(to))
	if err != nil {
		return latest, count, err
	}
	var blocks uint64
	rawCounts := map[uint64]int{}
	for _, log := range expected.Logs {
		rawCounts[log.BlockNumber]++
	}
	for blockRows.Next() {
		var height int64
		var hash string
		var raw int
		var fetch, decoded bool
		if err = blockRows.Scan(&height, &hash, &raw, &fetch, &decoded); err != nil {
			blockRows.Close()
			return latest, count, err
		}
		record, ok := expected.Blocks[uint64(height)]
		if !ok || height != int64(from+blocks) || !validHash(hash) || common.HexToHash(hash) != record.Hash || raw != rawCounts[uint64(height)] || !fetch || !decoded {
			blockRows.Close()
			return latest, count, sourceError(IntegrityError, "capacity-block-coverage", nil)
		}
		blocks++
	}
	err = blockRows.Err()
	blockRows.Close()
	if err != nil || blocks != to-from+1 {
		return latest, count, sourceError(IncompleteError, "capacity-block-coverage", err)
	}
	if len(wanted) == 0 {
		observed, ok := observations[from]
		if !ok {
			return latest, count, sourceError(IncompleteError, "capacity-empty-observation", nil)
		}
		var committed *time.Time
		err = tx.QueryRow(read, `SELECT max(pg_xact_commit_timestamp(xmin)) FROM indexer.indexer_ingest_commits WHERE chain_id=125 AND stream_id=$1 AND previous_height<$2 AND last_complete_height>=$3`, stream, int64(from), int64(to)).Scan(&committed)
		if err != nil || committed == nil || committed.Before(observed.Add(time.Duration(clockOffsetLower))) {
			return latest, count, sourceError(IncompleteError, "capacity-empty-commit", err)
		}
		latest = committed.UTC()
	}
	if err = tx.Commit(read); err != nil {
		return latest, count, err
	}
	return latest, count, nil
}

func capacityWorkload(ctx context.Context, command CapacityCommand, capture *capacityCapture, faults chan<- CapacityFaultObservation) error {
	if !filepath.IsAbs(command.Executable) || !capacitySHA(command.SHA256) || len(command.Args) > 64 || len(command.Environment) > 256 {
		return sourceError(IdentityError, "capacity-workload", nil)
	}
	stat, err := os.Lstat(command.Executable)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode()&0111 == 0 {
		return sourceError(IdentityError, "capacity-workload", err)
	}
	file, err := os.Open(command.Executable)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	_ = file.Close()
	if err != nil || hex.EncodeToString(hash.Sum(nil)) != command.SHA256 {
		return sourceError(IntegrityError, "capacity-workload", err)
	}
	for _, arg := range command.Args {
		if len(arg) > 32768 || strings.IndexByte(arg, 0) >= 0 {
			return sourceError(InvalidError, "capacity-workload", nil)
		}
	}
	child := exec.CommandContext(ctx, command.Executable, command.Args...)
	child.Env = append([]string(nil), command.Environment...)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	child.Cancel = func() error {
		if child.Process == nil {
			return nil
		}
		return syscall.Kill(-child.Process.Pid, syscall.SIGTERM)
	}
	child.WaitDelay = time.Second
	stdout, err := child.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := child.StderrPipe()
	if err != nil {
		return err
	}
	if err = child.Start(); err != nil {
		return err
	}
	stderrDone := make(chan error, 1)
	go func() { _, err := io.Copy(io.Discard, stderr); stderrDone <- err }()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var lines uint64
	for scanner.Scan() {
		var value json.RawMessage = append([]byte(nil), scanner.Bytes()...)
		var record capacityWorkloadRecord
		if err = json.Unmarshal(value, &record); err != nil || record.Kind == "" {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			_ = child.Wait()
			<-stderrDone
			return sourceError(InvalidError, "capacity-workload-capture", nil)
		}
		if record.Kind == "fault" {
			if record.Fault == nil || !validSourceID(record.Fault.SourceID) || record.Fault.ObservedAt.IsZero() || (record.Fault.Kind != "throttle" && record.Fault.Kind != "truncate" && record.Fault.Kind != "endpoint_loss" && record.Fault.Kind != "recovery") || (record.Fault.State != "injected" && record.Fault.State != "restored") {
				_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
				_ = child.Wait()
				<-stderrDone
				return sourceError(InvalidError, "capacity-fault-capture", nil)
			}
			select {
			case faults <- *record.Fault:
			case <-ctx.Done():
				_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
				_ = child.Wait()
				<-stderrDone
				return ctx.Err()
			}
		}
		if err = capture.write("workload_provider", value); err != nil {
			_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			_ = child.Wait()
			<-stderrDone
			return err
		}
		lines++
	}
	readErr := scanner.Err()
	if readErr != nil {
		_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
	}
	waitErr := child.Wait()
	stderrErr := <-stderrDone
	if readErr != nil || waitErr != nil || stderrErr != nil {
		return sourceError(IncompleteError, "capacity-workload", errors.Join(readErr, waitErr, stderrErr))
	}
	if lines == 0 {
		return sourceError(IncompleteError, "capacity-workload-capture", nil)
	}
	return nil
}
func capacityParity(ctx context.Context, p CapacityProviders, options CapacityOptions) (ParityReport, error) {
	if p.Historical.Explorer == nil {
		return ParityReport{}, sourceError(IdentityError, "capacity-explorer", nil)
	}
	left := FetchResult{ManifestDigest: p.Live.opts.ManifestDigest, Blocks: map[uint64]BlockRecord{}, BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
	right := FetchResult{ManifestDigest: p.Live.opts.ManifestDigest, Blocks: map[uint64]BlockRecord{}, BlockTimestamps: map[uint64]int64{}, TxFromMap: map[string]string{}}
	for from := options.ParityFrom; from <= options.ParityTo; {
		to := min(from+1999, options.ParityTo)
		rpc, err := p.Live.FetchCommitted(ctx, FetchOptions{FromBlock: int64(from), ToBlock: int64(to)}, true, nil)
		if err != nil {
			return ParityReport{}, err
		}
		explorer, err := p.Historical.Explorer.FetchParityLogs(ctx, FetchOptions{FromBlock: int64(from), ToBlock: int64(to), Historical: true})
		if err != nil {
			return ParityReport{}, err
		}
		if err = mergeFetch(&left, rpc); err != nil {
			return ParityReport{}, err
		}
		if err = mergeFetch(&right, explorer); err != nil {
			return ParityReport{}, err
		}
		if to == options.ParityTo {
			break
		}
		from = to + 1
	}
	left.Complete = true
	right.Complete = true
	return CompareParity(options.ParityFrom, options.ParityTo, ParityInput{SourceID: p.Live.Name(), TimestampUnit: "seconds", Result: left}, ParityInput{SourceID: p.Historical.Explorer.Name(), TimestampUnit: "seconds", Result: right})
}
func RunSourceCapacity(ctx context.Context, options CapacityOptions, p CapacityProviders) (report CapacityReport, result error) {
	report = CapacityReport{Version: 1, Binding: options.Binding, Profile: options.Profile, RPCMetricScope: "committed_source_transport_including_observer_and_historical", TargetEventsPerSecond: options.TargetEventsPerSecond, Failures: []string{}}
	if p.Live == nil || p.Historical == nil || p.Head == nil || p.Processor == nil || p.Pool == nil || p.Historical.Explorer == nil || !capacityStreamID(options.StreamID) || !filepath.IsAbs(options.OutputDirectory) || options.Duration <= 0 || options.Duration > 25*time.Hour || options.HistoricalTo < options.HistoricalFrom || options.HistoricalTo > math.MaxInt64 || options.ParityTo > math.MaxInt64 || options.ParityTo < options.ParityFrom || options.ParityTo-options.ParityFrom < 9999 || options.ParityTo-options.ParityFrom >= 100000 || options.MaxCaptureBytes < 1<<20 || options.MaxCaptureBytes > 256<<30 || options.MaxHeapBytes == 0 || options.TargetEventsPerSecond > 10000 || !options.MutationApproved {
		return report, sourceError(IdentityError, "capacity-input", nil)
	}
	switch options.Profile {
	case "harness":
		if options.TargetEventsPerSecond == 0 {
			return report, sourceError(InvalidError, "capacity-rate", nil)
		}
	case "peak":
		if options.TargetEventsPerSecond != 2000 || options.Duration < 24*time.Hour {
			return report, sourceError(InvalidError, "capacity-peak", nil)
		}
	case "burst":
		if options.TargetEventsPerSecond != 10000 || options.Duration < 5*time.Minute {
			return report, sourceError(InvalidError, "capacity-burst", nil)
		}
	default:
		return report, sourceError(InvalidError, "capacity-profile", nil)
	}
	if err := verifyCapacityBinding(ctx, options, p); err != nil {
		return report, err
	}
	probe, cancelProbe := context.WithTimeout(ctx, 2*time.Second)
	var track string
	var recovery bool
	err := p.Pool.QueryRow(probe, `SELECT current_setting('track_commit_timestamp'),pg_is_in_recovery()`).Scan(&track, &recovery)
	cancelProbe()
	if err != nil || track != "on" || recovery {
		return report, sourceError(IdentityError, "capacity-commit-time", err)
	}
	var initial, typed int64
	if err = p.Pool.QueryRow(ctx, `SELECT raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=125 AND stream_id=$1`, options.StreamID).Scan(&initial, &typed); err != nil || initial < 0 || typed != initial {
		return report, sourceError(IntegrityError, "capacity-watermark", err)
	}
	beforeClock := time.Now().UTC()
	var databaseClock time.Time
	if err = p.Pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&databaseClock); err != nil {
		return report, err
	}
	afterClock := time.Now().UTC()
	if afterClock.Sub(beforeClock) > 5*time.Millisecond {
		return report, sourceError(TimeoutError, "capacity-clock-uncertainty", nil)
	}
	report.DBClockOffsetLowerNanoseconds = databaseClock.Sub(afterClock).Nanoseconds()
	report.DBClockOffsetUpperNanoseconds = databaseClock.Sub(beforeClock).Nanoseconds()
	report.LatencyResolutionNanoseconds = int64(time.Microsecond)
	report.ClockSamples = []CapacityClockSample{{afterClock, report.DBClockOffsetLowerNanoseconds, report.DBClockOffsetUpperNanoseconds}}
	initialHead, err := p.Head.Head(ctx)
	if err != nil || initialHead != initial {
		return report, sourceError(IntegrityError, "capacity-initial-cursor", err)
	}
	if err = os.MkdirAll(options.OutputDirectory, 0700); err != nil {
		return report, err
	}
	capture, err := newCapacityCapture(filepath.Join(options.OutputDirectory, "source-capacity.raw.jsonl"), options.MaxCaptureBytes)
	if err != nil {
		return report, err
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	events := make(chan error, 3)
	faults := make(chan CapacityFaultObservation, 64)
	var measurementMu sync.Mutex
	var measured CapacityReport
	var finalMeasurement *CapacityMeasurement
	var measuredFinishedAt time.Time
	baseline := p.Live.capacityMeasurement()
	report.StartedAt = time.Now().UTC()
	defer func() {
		cancel()
		wg.Wait()
		report.PeakSampledInFlight = measured.PeakSampledInFlight
		report.PeakSampledProcessPermits = measured.PeakSampledProcessPermits
		report.PeakSampledHistoricalPermits = measured.PeakSampledHistoricalPermits
		report.PeakHistoricalQueue = measured.PeakHistoricalQueue
		report.PeakPendingRanges = measured.PeakPendingRanges
		report.PeakHeaderCacheEntries = measured.PeakHeaderCacheEntries
		report.PeakHeapBytes = measured.PeakHeapBytes
		report.LagPauseObserved = measured.LagPauseObserved
		report.LagResumeObserved = measured.LagResumeObserved
		report.LagTransitionsValidated = report.LagPauseObserved && report.LagResumeObserved
		report.HistoricalRanges = measured.HistoricalRanges
		report.HistoricalFailures = measured.HistoricalFailures
		report.FinishedAt = measuredFinishedAt
		if report.FinishedAt.IsZero() {
			report.FinishedAt = time.Now().UTC()
		}
		report.ElapsedNanoseconds = report.FinishedAt.Sub(report.StartedAt).Nanoseconds()
		snapshot := p.Live.capacityMeasurement()
		if finalMeasurement != nil {
			snapshot = *finalMeasurement
		}
		report.RPCRequests = snapshot.RPCRequests - baseline.RPCRequests
		report.CacheHits = snapshot.Source.CacheHits - baseline.Source.CacheHits
		report.HeaderFetches = snapshot.Source.HeaderFetches - baseline.Source.HeaderFetches
		report.CacheHitRatioNumerator = report.CacheHits
		report.CacheHitRatioDenominator = report.CacheHits + report.HeaderFetches
		report.RPCCallsPer1000Numerator = report.RPCRequests * 1000
		report.RPCCallsPer1000Denominator = report.InputEvents
		report.TotalAllocatedBytes = snapshot.TotalAllocatedBytes - baseline.TotalAllocatedBytes
		report.GCCycles = snapshot.GCCycles - baseline.GCCycles
		report.Throttles = p.Live.transport.throttles.Load() - baseline.Throttles
		report.InvalidResponses = p.Live.transport.invalidResponses.Load() - baseline.InvalidResponses
		report.Timeouts = p.Live.transport.timeouts.Load() - baseline.Timeouts
		var closeErr error
		report.RawCaptureSHA256, report.RawCaptureBytes, closeErr = capture.finish()
		result = errors.Join(result, closeErr)
		if result != nil {
			report.Failures = append(report.Failures, result.Error())
		}
		report.HarnessPassed = result == nil
		report.CapacityTargetQualified = report.HarnessPassed && options.Profile != "harness"
		report.SharedSoakQualified = false
		identityReport := report
		identityReport.ReportID = ""
		report.ReportID, _ = capacityHash(identityReport)
		data, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			result = errors.Join(result, marshalErr)
			return
		}
		path := filepath.Join(options.OutputDirectory, "source-capacity.report.json")
		temporary := path + ".pending"
		f, writeErr := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if writeErr == nil {
			_, writeErr = f.Write(append(data, '\n'))
			writeErr = errors.Join(writeErr, f.Sync(), f.Close())
		}
		if writeErr == nil {
			writeErr = os.Link(temporary, path)
		}
		if writeErr == nil {
			writeErr = os.Remove(temporary)
		}
		if writeErr != nil {
			report.HarnessPassed = false
			report.CapacityTargetQualified = false
		}
		result = errors.Join(result, writeErr)
	}()
	wg.Add(1)
	go func() { defer wg.Done(); events <- capacityWorkload(run, options.Workload, capture, faults) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		paused := false
		for {
			select {
			case <-run.Done():
				return
			case <-ticker.C:
				snapshot := p.Live.capacityMeasurement()
				measurementMu.Lock()
				measured.PeakSampledInFlight = max(measured.PeakSampledInFlight, snapshot.InFlight)
				measured.PeakSampledProcessPermits = max(measured.PeakSampledProcessPermits, snapshot.ProcessPermits)
				measured.PeakSampledHistoricalPermits = max(measured.PeakSampledHistoricalPermits, snapshot.HistoricalPermits)
				measured.PeakHistoricalQueue = max(measured.PeakHistoricalQueue, snapshot.HistoricalQueue)
				measured.PeakPendingRanges = max(measured.PeakPendingRanges, snapshot.PendingRanges)
				measured.PeakHeaderCacheEntries = max(measured.PeakHeaderCacheEntries, snapshot.HeaderCacheEntries)
				measured.PeakHeapBytes = max(measured.PeakHeapBytes, snapshot.HeapBytes)
				if snapshot.HistoricalPaused {
					measured.LagPauseObserved = true
				}
				if paused && !snapshot.HistoricalPaused {
					measured.LagResumeObserved = true
				}
				paused = snapshot.HistoricalPaused
				measurementMu.Unlock()
				if snapshot.InFlight > 64 || snapshot.ProcessPermits > 64 || snapshot.HistoricalPermits > 16 || snapshot.PendingRanges > 1024 || snapshot.HeaderCacheEntries > 16384 || snapshot.HeapBytes > options.MaxHeapBytes {
					events <- sourceError(OverloadError, "capacity-budget", nil)
					return
				}
				for _, active := range snapshot.EndpointInFlight {
					if active > 16 {
						events <- sourceError(OverloadError, "capacity-endpoint", nil)
						return
					}
				}
				if err := capture.write("resource_sample", snapshot); err != nil {
					events <- err
					return
				}
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for from := options.HistoricalFrom; run.Err() == nil; {
			to := min(from+1999, options.HistoricalTo)
			result, err := p.Historical.FetchHistorical(run, FetchOptions{FromBlock: int64(from), ToBlock: int64(to), Historical: true}, uint64(initial))
			if err != nil {
				if run.Err() != nil {
					return
				}
				measurementMu.Lock()
				measured.HistoricalFailures++
				measurementMu.Unlock()
				if captureErr := capture.write("historical_failure", struct {
					From, To uint64
					Error    string
					At       time.Time
				}{from, to, err.Error(), time.Now().UTC()}); captureErr != nil {
					events <- captureErr
					return
				}
				if !p.Live.TransportMetrics().HistoricalPaused {
					events <- err
					return
				}
				select {
				case <-run.Done():
					return
				case <-time.After(100 * time.Millisecond):
					continue
				}
			}
			if !result.Complete {
				events <- sourceError(IncompleteError, "capacity-history", nil)
				return
			}
			measurementMu.Lock()
			measured.HistoricalRanges++
			measurementMu.Unlock()
			if err = capture.write("historical_range", struct {
				From, To uint64
				Heads    HistoricalHeads
				Metrics  SourceMetrics
			}{from, to, result.Heads, p.Live.Metrics()}); err != nil {
				events <- err
				return
			}
			if to == options.HistoricalTo {
				from = options.HistoricalFrom
			} else {
				from = to + 1
			}
		}
	}()
	histogram := map[int64]uint64{}
	observations := map[uint64]time.Time{}
	inputSeconds := map[int64]uint64{}
	seenHead := uint64(initial)
	cursor := uint64(initial)
	deadline := time.NewTimer(options.Duration)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	workloadFinished := false
loop:
	for {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case err := <-events:
			if err != nil {
				return report, err
			}
			workloadFinished = true
		case fault := <-faults:
			if len(report.FaultObservations) >= 256 {
				return report, sourceError(OversizeError, "capacity-fault-capture", nil)
			}
			found := false
			for _, endpoint := range p.Live.endpoints {
				if endpoint.id == fault.SourceID {
					found = true
					break
				}
			}
			if !found || fault.ObservedAt.Before(report.StartedAt) || fault.ObservedAt.After(time.Now().Add(time.Second)) {
				return report, sourceError(IdentityError, "capacity-fault-source", nil)
			}
			if fault.Kind == "recovery" && fault.State == "restored" {
				if err := p.Live.RejoinEndpoint(run, fault.SourceID); err != nil {
					return report, err
				}
			}
			report.FaultObservations = append(report.FaultObservations, fault)
		case <-deadline.C:
			break loop
		case <-ticker.C:
			height, err := p.Head.Head(run)
			if err != nil {
				return report, err
			}
			if height < 0 || uint64(height) < cursor {
				return report, sourceError(IntegrityError, "capacity-head", nil)
			}
			p.Live.mu.Lock()
			head := p.Live.authoritative
			p.Live.mu.Unlock()
			if head.Height < uint64(height) || head.ObservedAt.IsZero() {
				return report, sourceError(IdentityError, "capacity-observation", nil)
			}
			lagAt := time.Now().UTC()
			if err = p.Live.ReportDurableHeight(lagAt, cursor); err != nil {
				return report, err
			}
			lag, err := p.Live.capacityLagSample(lagAt, cursor)
			if err != nil {
				return report, err
			}
			if err = capture.write("lag_sample", lag); err != nil {
				return report, err
			}
			if head.Height > seenHead {
				if head.Height-seenHead > uint64(65536-len(observations)) {
					return report, sourceError(OverloadError, "capacity-observation-backlog", nil)
				}
				for height := seenHead + 1; height <= head.Height; height++ {
					observations[height] = head.ObservedAt
				}
				seenHead = head.Height
			}
			if head.Height == cursor {
				continue
			}
			to := min(cursor+64, head.Height)
			observerBefore := p.Live.ids.Load()
			expected, err := p.Live.FetchCommitted(run, FetchOptions{FromBlock: int64(cursor + 1), ToBlock: int64(to)}, false, nil)
			if err != nil {
				return report, err
			}
			report.ObserverRPCRequestsUpperBound += p.Live.ids.Load() - observerBefore
			if err = capture.write("source_expected", struct {
				Observation HeadObservation
				From, To    uint64
				Result      FetchResult
			}{head, cursor + 1, to, expected}); err != nil {
				return report, err
			}
			before := p.Live.ids.Load()
			clockBefore := time.Now().UTC()
			var actualDatabaseTime time.Time
			if err = p.Pool.QueryRow(run, `SELECT clock_timestamp()`).Scan(&actualDatabaseTime); err != nil {
				return report, err
			}
			clockAfter := time.Now().UTC()
			if clockAfter.Sub(clockBefore) > 5*time.Millisecond {
				return report, sourceError(TimeoutError, "capacity-clock-uncertainty", nil)
			}
			report.DBClockOffsetLowerNanoseconds = actualDatabaseTime.Sub(clockAfter).Nanoseconds()
			report.DBClockOffsetUpperNanoseconds = actualDatabaseTime.Sub(clockBefore).Nanoseconds()
			clockSample := CapacityClockSample{clockAfter, report.DBClockOffsetLowerNanoseconds, report.DBClockOffsetUpperNanoseconds}
			if err = capture.write("clock_sample", clockSample); err != nil {
				return report, err
			}
			if err = p.Processor.ProcessBatch(run, int64(cursor+1), int64(to)); err != nil {
				report.FailedAttempts++
				_ = capture.write("processor_failure", struct {
					From, To uint64
					Error    string
					At       time.Time
				}{cursor + 1, to, err.Error(), time.Now().UTC()})
				return report, err
			}
			durable, count, err := capacityDurable(run, p.Pool, options.StreamID, cursor+1, to, expected, observations, report.DBClockOffsetLowerNanoseconds, histogram, capture)
			if err != nil {
				return report, err
			}
			observed := observations[cursor+1]
			elapsed := durable.Sub(observed.Add(time.Duration(report.DBClockOffsetLowerNanoseconds))).Nanoseconds()
			for _, log := range expected.Logs {
				second := observations[log.BlockNumber].Sub(report.StartedAt) / time.Second
				inputSeconds[int64(second)]++
			}
			report.InputEvents += uint64(len(expected.Logs))
			report.DurableEvents += count
			report.Batches++
			if len(histogram) > 1000000 {
				return report, sourceError(OverloadError, "capacity-histogram", nil)
			}
			payload, err := capacityHash(expected)
			if err != nil {
				return report, err
			}
			if err = capture.write("batch_sample", CapacityBatchSample{cursor + 1, to, observed, durable, count, p.Live.ids.Load() - before, payload, elapsed}); err != nil {
				return report, err
			}
			for height := cursor + 1; height <= to; height++ {
				delete(observations, height)
			}
			cursor = to
		}
	}
	if !workloadFinished {
		select {
		case err := <-events:
			if err != nil {
				return report, err
			}
			workloadFinished = true
		case <-time.After(20 * time.Second):
			return report, sourceError(IncompleteError, "capacity-workload-unfinished", nil)
		}
	}
	finalHeight, finalErr := p.Head.Head(run)
	if finalErr != nil || finalHeight < 0 || uint64(finalHeight) != cursor {
		return report, sourceError(IncompleteError, "capacity-final-head", finalErr)
	}
	cancel()
	wg.Wait()
	measuredFinishedAt = time.Now().UTC()
	snapshot := p.Live.capacityMeasurement()
	finalMeasurement = &snapshot
	report.InputSecondCounts = inputSeconds
	if len(observations) != 0 {
		return report, sourceError(IncompleteError, "capacity-unconsumed-observed", nil)
	}
	if report.InputEvents == 0 || report.InputEvents != report.DurableEvents {
		return report, sourceError(IncompleteError, "capacity-identities", nil)
	}
	report.P99Nanoseconds, err = capacityP99(histogram, report.InputEvents)
	if err != nil {
		return report, err
	}
	if report.P99Nanoseconds > int64(500*time.Millisecond) {
		return report, sourceError(TimeoutError, "capacity-p99", nil)
	}
	if report.InputEvents > ^uint64(0)/1000000000 || report.InputEvents*1000000000 < options.TargetEventsPerSecond*uint64(options.Duration) {
		return report, sourceError(OverloadError, "capacity-input-rate", nil)
	}
	if options.Profile != "harness" {
		for second := int64(0); second < int64(options.Duration/time.Second); second++ {
			if inputSeconds[second] < options.TargetEventsPerSecond {
				return report, sourceError(OverloadError, "capacity-second-input-rate", nil)
			}
		}
	}
	kinds := map[string]bool{}
	lost := map[string]time.Time{}
	recovered := map[string]bool{}
	for _, fault := range report.FaultObservations {
		kinds[fault.Kind] = true
		if fault.Kind == "endpoint_loss" {
			lost[fault.SourceID] = fault.ObservedAt
		}
		if fault.Kind == "recovery" {
			at, ok := lost[fault.SourceID]
			if !ok || !fault.ObservedAt.After(at) || fault.State != "restored" {
				return report, sourceError(IntegrityError, "capacity-recovery-order", nil)
			}
			recovered[fault.SourceID] = true
		}
	}
	for endpoint := range lost {
		if !recovered[endpoint] {
			return report, sourceError(IncompleteError, "capacity-recovery", nil)
		}
		verified := false
		for _, health := range p.Live.EndpointHealth() {
			if health.SourceID == endpoint && health.RecoveryChecks == 128 && health.QuarantineReason == "" {
				verified = true
			}
		}
		if !verified {
			return report, sourceError(IncompleteError, "capacity-recovery-checks", nil)
		}
	}
	if !kinds["throttle"] || !kinds["truncate"] || !kinds["endpoint_loss"] || !kinds["recovery"] || p.Live.transport.throttles.Load() <= baseline.Throttles || p.Live.transport.invalidResponses.Load() <= baseline.InvalidResponses {
		return report, sourceError(IncompleteError, "capacity-adversarial-inputs", nil)
	}
	parity, err := capacityParity(ctx, p, options)
	report.Parity = &parity
	if err != nil {
		return report, err
	}
	if !parity.Certified {
		return report, sourceError(IntegrityError, "capacity-parity", nil)
	}
	if err = WriteParityReport(options.OutputDirectory, parity); err != nil {
		return report, err
	}
	return report, nil
}

func (s *CommittedSource) capacityLagSample(at time.Time, durable uint64) (CapacityLagSample, error) {
	s.mu.Lock()
	head := s.authoritative.Height
	headRecord, headOK := s.cache[s.accepted[head]]
	durableRecord, durableOK := s.cache[s.accepted[durable]]
	s.mu.Unlock()
	if !headOK || !durableOK || headRecord.TimestampSeconds < durableRecord.TimestampSeconds {
		return CapacityLagSample{}, sourceError(IncompleteError, "capacity-lag-provenance", nil)
	}
	difference := headRecord.TimestampSeconds - durableRecord.TimestampSeconds
	if difference > int64((1<<63-1)/int64(time.Second)) {
		return CapacityLagSample{}, sourceError(InvalidError, "capacity-lag-provenance", nil)
	}
	s.backfill.mu.Lock()
	sample := CapacityLagSample{at, s.backfill.highSince, s.backfill.lowSince, difference * int64(time.Second), s.backfill.paused, head, durable}
	actual := s.backfill.observedAt
	s.backfill.mu.Unlock()
	if sample.Paused && !sample.HighSince.IsZero() && actual.Sub(sample.HighSince) < 30*time.Second {
		return sample, sourceError(IntegrityError, "capacity-premature-pause", nil)
	}
	if !sample.Paused && !sample.LowSince.IsZero() && actual.Sub(sample.LowSince) < 60*time.Second {
		return sample, sourceError(IntegrityError, "capacity-premature-resume", nil)
	}
	return sample, nil
}

type SourceLagRecoveryReport struct {
	Version                                             int
	Binding                                             CapacityBinding
	StartedAt, FinishedAt                               time.Time
	Samples                                             uint64
	PauseObserved, ResumeObserved, TransitionsValidated bool
	RawCaptureSHA256                                    string
	RawCaptureBytes                                     uint64
	Failure                                             string
}

func MeasureSourceLagRecovery(ctx context.Context, options CapacityOptions, p CapacityProviders) (report SourceLagRecoveryReport, result error) {
	report = SourceLagRecoveryReport{Version: 1, Binding: options.Binding}
	if p.Live == nil || p.Historical == nil || p.Pool == nil || p.Historical.RPC != p.Live || !capacityStreamID(options.StreamID) || options.Duration < 95*time.Second || options.Duration > 30*time.Minute || !filepath.IsAbs(options.OutputDirectory) {
		return report, sourceError(IdentityError, "capacity-lag-input", nil)
	}
	if err := verifyCapacityBinding(ctx, options, p); err != nil {
		return report, err
	}
	if err := os.MkdirAll(options.OutputDirectory, 0700); err != nil {
		return report, err
	}
	capture, err := newCapacityCapture(filepath.Join(options.OutputDirectory, "source-lag-recovery.raw.jsonl"), 16<<20)
	if err != nil {
		return report, err
	}
	report.StartedAt = time.Now().UTC()
	defer func() {
		report.FinishedAt = time.Now().UTC()
		var err error
		report.RawCaptureSHA256, report.RawCaptureBytes, err = capture.finish()
		result = errors.Join(result, err)
		if result != nil {
			report.Failure = result.Error()
		}
		report.TransitionsValidated = result == nil && report.PauseObserved && report.ResumeObserved
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timer := time.NewTimer(options.Duration)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return report, ctx.Err()
		case <-timer.C:
			if !report.PauseObserved || !report.ResumeObserved {
				return report, sourceError(IncompleteError, "capacity-lag-transitions", nil)
			}
			return report, nil
		case <-ticker.C:
			observed := time.Now().UTC()
			p.Live.mu.Lock()
			head := p.Live.authoritative
			p.Live.mu.Unlock()
			if head.ObservedAt.IsZero() || observed.Sub(head.ObservedAt) > 2*time.Second {
				return report, sourceError(TimeoutError, "capacity-lag-head-stale", nil)
			}
			read, cancel := context.WithTimeout(ctx, 2*time.Second)
			var raw, typed int64
			err := p.Pool.QueryRow(read, `SELECT raw_complete_height,typed_complete_height FROM indexer.indexer_ingest_watermarks WHERE chain_id=125 AND stream_id=$1`, options.StreamID).Scan(&raw, &typed)
			cancel()
			if err != nil || raw < 0 || raw != typed {
				return report, sourceError(IntegrityError, "capacity-lag-watermark", err)
			}
			observed = time.Now().UTC()
			if err = p.Live.ReportDurableHeight(observed, uint64(raw)); err != nil {
				return report, err
			}
			sample, err := p.Live.capacityLagSample(observed, uint64(raw))
			if err != nil {
				return report, err
			}
			if sample.Paused {
				report.PauseObserved = true
			}
			if report.PauseObserved && !sample.Paused && sample.LagNanoseconds < int64(500*time.Millisecond) && !sample.LowSince.IsZero() && sample.ObservedAt.Sub(sample.LowSince) >= 60*time.Second {
				report.ResumeObserved = true
			}
			if err = capture.write("lag_recovery_sample", sample); err != nil {
				return report, err
			}
			report.Samples++
		}
	}
}
