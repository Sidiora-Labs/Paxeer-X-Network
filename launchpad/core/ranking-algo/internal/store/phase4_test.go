package store

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/queue"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type phase4Config struct {
	Mode                     string                          `json:"mode"`
	Binding                  ranker.MeasurementBinding       `json:"binding"`
	SourceRoot               string                          `json:"sourceRoot"`
	SourceManifest           ranker.BoundFile                `json:"sourceManifest"`
	Output                   string                          `json:"output"`
	RawMaxBytes              int64                           `json:"rawMaxBytes"`
	DSNEnvironment           string                          `json:"dsnEnvironment"`
	RankingSchema            string                          `json:"rankingSchema"`
	SourceSchema             string                          `json:"sourceSchema"`
	StatsSchema              string                          `json:"statsSchema"`
	SourceStream             string                          `json:"sourceStream"`
	ProjectionVersion        string                          `json:"projectionVersion"`
	RecoverySchema           string                          `json:"recoverySchema"`
	Epoch                    int64                           `json:"epoch"`
	Region                   string                          `json:"region"`
	VisibilityURL            string                          `json:"visibilityURL"`
	Origin                   string                          `json:"origin"`
	Category                 string                          `json:"category"`
	HotSeconds               int64                           `json:"hotSeconds"`
	WorkloadCommand          []string                        `json:"workloadCommand"`
	WorkloadBinary           ranker.BoundFile                `json:"workloadBinary"`
	WorkloadHosts            []string                        `json:"workloadHosts"`
	HTTPCA                   ranker.BoundFile                `json:"httpCA"`
	HTTPClientCertificate    ranker.BoundFile                `json:"httpClientCertificate"`
	HTTPClientKeyEnvironment string                          `json:"httpClientKeyEnvironment"`
	StartUTCNanoseconds      int64                           `json:"startUTCNanoseconds"`
	TelemetryURL             string                          `json:"telemetryURL"`
	TelemetryEverySeconds    int64                           `json:"telemetryEverySeconds"`
	Replay                   []ranker.HistoricalReplaySample `json:"replay"`
	RollbackCommand          []string                        `json:"rollbackCommand"`
	RollbackBinary           ranker.BoundFile                `json:"rollbackBinary"`
	RollbackReceipt          string                          `json:"rollbackReceipt"`
}

type phase4Artifacts struct {
	directory string
	limit     int64
	written   int64
	mu        sync.Mutex
	rows      *os.File
	histogram ranker.LatencyHistogram
	count     uint64
	failures  uint64
}

func newPhase4Artifacts(c phase4Config) (*phase4Artifacts, error) {
	if c.RawMaxBytes < 1<<20 || c.RawMaxBytes > 1<<40 || !filepath.IsAbs(c.Output) {
		return nil, ranker.ErrUnavailable
	}
	allowed := filepath.Join(c.SourceRoot, "evidence", "production-system", "L19", "phase4")
	relative, err := filepath.Rel(allowed, c.Output)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil, errors.New("fresh lane-owned phase4 artifact directory required")
	}
	if err = os.MkdirAll(filepath.Dir(c.Output), 0700); err != nil {
		return nil, err
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(c.Output))
	if err != nil || resolvedParent != filepath.Clean(filepath.Dir(c.Output)) {
		return nil, errors.New("artifact parents must not contain symlinks")
	}
	if err = os.Mkdir(c.Output, 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(c.Output, "raw.ndjson"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	return &phase4Artifacts{directory: c.Output, limit: c.RawMaxBytes, rows: f, histogram: ranker.NewLatencyHistogram()}, nil
}
func (a *phase4Artifacts) append(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if int64(len(raw)+1) > a.limit-a.written {
		return errors.New("raw artifact disk budget exhausted")
	}
	raw = append(raw, '\n')
	n, err := a.rows.Write(raw)
	a.written += int64(n)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	return err
}
func (a *phase4Artifacts) document(name string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(a.directory, name)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(append(raw, '\n'))
	syncErr := f.Sync()
	closeErr := f.Close()
	return errors.Join(writeErr, syncErr, closeErr)
}
func (a *phase4Artifacts) close() error { return errors.Join(a.rows.Sync(), a.rows.Close()) }
func phase4Client(c phase4Config) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	if c.HTTPCA.Path != "" {
		ca, err := ranker.ReadBoundFile(c.HTTPCA, 1<<20)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(ca) {
			return nil, errors.New("actual measurement CA required")
		}
		tlsConfig.RootCAs = roots
	}
	if c.HTTPClientCertificate.Path != "" {
		certificate, err := ranker.ReadBoundFile(c.HTTPClientCertificate, 1<<20)
		if err != nil {
			return nil, err
		}
		privateKey := os.Getenv(c.HTTPClientKeyEnvironment)
		if privateKey == "" {
			return nil, errors.New("actual client certificate private key unavailable")
		}
		pair, err := tls.X509KeyPair(certificate, []byte(privateKey))
		if err != nil {
			return nil, errors.New("actual client certificate/key invalid")
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}
	return &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: tlsConfig, MaxIdleConns: 128, MaxIdleConnsPerHost: 100, MaxConnsPerHost: 100, IdleConnTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("measurement redirects are forbidden") }}, nil
}
func phase4Endpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))) {
		return ranker.ErrUnavailable
	}
	return nil
}
func phase4LoadConfig() (phase4Config, error) {
	var c phase4Config
	raw, err := ranker.ReadBoundFile(ranker.BoundFile{Path: os.Getenv("L19_PHASE4_CONFIG_FILE"), SHA256: os.Getenv("L19_PHASE4_CONFIG_SHA256")}, 4<<20)
	if err != nil {
		return c, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&c); err != nil {
		return c, err
	}
	if decoder.Decode(new(any)) != io.EOF || c.Binding.Validate() != nil {
		return c, ranker.ErrUnavailable
	}
	files, err := ranker.ValidateMeasurementSource(c.SourceRoot, c.SourceManifest, c.Binding)
	if err != nil {
		return c, err
	}
	_, formula, err := ranker.CurrentFormulaManifest(files)
	if err != nil || formula != c.Binding.Formula {
		return c, errors.New("running formula source/policy binding differs")
	}
	source, _ := filepath.Abs("../../../..")
	root, _ := filepath.Abs(c.SourceRoot)
	if source != root {
		return c, errors.New("measurement must execute inside the bound candidate checkout")
	}
	switch c.Mode {
	case "refresh", "hot", "soak", "replay", "rollback":
	default:
		return c, ranker.ErrUnavailable
	}
	return c, nil
}
func phase4Refresh(ctx context.Context, c phase4Config, a *phase4Artifacts) error {
	if c.DSNEnvironment == "" || os.Getenv(c.DSNEnvironment) == "" || c.Epoch < 1 || c.Region == "" || phase4Endpoint(c.VisibilityURL) != nil {
		return errors.New("actual isolated database, epoch and moderation authority required")
	}
	pool, err := pgxpool.New(ctx, os.Getenv(c.DSNEnvironment))
	if err != nil {
		return err
	}
	defer pool.Close()
	client, err := phase4Client(c)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	visibility, err := NewHTTPRankingVisibility(c.VisibilityURL, client)
	if err != nil {
		return err
	}
	reader, err := NewCanonicalInputReader(c.SourceSchema, c.StatsSchema, c.SourceStream, c.ProjectionVersion, visibility)
	if err != nil {
		return err
	}
	frozen, err := NewFrozenInputs(pool, reader)
	if err != nil {
		return err
	}
	guard, err := queue.NewEpochGuard(c.RecoverySchema, c.Epoch, c.Region)
	if err != nil {
		return err
	}
	publication, err := NewPublicationStore(pool, c.RankingSchema, guard)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().Truncate(time.Second)
	var memoryBefore runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	started := time.Now()
	in, err := frozen.Freeze(ctx, uuid.NewString(), cutoff)
	if err != nil {
		return errors.Join(errors.New("actual canonical complete ranking input unavailable"), err)
	}
	if len(in.Candidates) != 1000000 {
		return fmt.Errorf("one million actual candidates required; observed%d", len(in.Candidates))
	}
	snapshots, err := ranker.ComputeRun(in, false)
	if err != nil {
		return err
	}
	if err = publication.Complete(ctx, snapshots); err != nil {
		return err
	}
	elapsed := time.Since(started)
	var memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryAfter)
	outputs := make(map[string]any, len(snapshots))
	for _, snapshot := range snapshots {
		outputs[snapshot.Category()] = map[string]any{"runId": snapshot.RunID(), "inputDigest": snapshot.InputDigest(), "outputDigest": snapshot.OutputDigest(), "eligible": len(snapshot.Entries()), "excluded": len(snapshot.Exclusions())}
	}
	if err = a.document("snapshot-digests.json", outputs); err != nil {
		return err
	}
	if err = a.append(map[string]any{"kind": "refresh", "startedAt": started.UTC(), "candidateCount": len(in.Candidates), "runId": in.RunID, "inputDigest": snapshots[0].InputDigest(), "durationNanoseconds": elapsed.Nanoseconds(), "categories": len(snapshots), "heapInUseBeforeBytes": memoryBefore.HeapInuse, "heapInUseAfterBytes": memoryAfter.HeapInuse, "allocationBeforeBytes": memoryBefore.TotalAlloc, "allocationAfterBytes": memoryAfter.TotalAlloc, "actualProcessGoroutines": runtime.NumGoroutine(), "measurementProcess": "current-source-native-harness", "binding": c.Binding}); err != nil {
		return err
	}
	plan, err := reader.ExplainUniverse(ctx, pool, in.CutoffHeight, cutoff)
	if err != nil {
		return err
	}
	if err = a.document("query-plan.json", plan); err != nil {
		return err
	}
	if elapsed > 30*time.Second {
		return fmt.Errorf("actual million-candidate refresh exceeded30seconds: %s", elapsed)
	}
	return nil
}

type phase4Read struct {
	ID                   uint64    `json:"id"`
	ScheduledUTC         time.Time `json:"scheduledUTC"`
	ScheduledNanoseconds int64     `json:"scheduledNanoseconds"`
	StartedNanoseconds   int64     `json:"startedNanoseconds"`
	CompletedNanoseconds int64     `json:"completedNanoseconds"`
	LatencyNanoseconds   int64     `json:"latencyNanoseconds"`
	Status               int       `json:"status"`
	Error                string    `json:"error,omitempty"`
	ResponseSHA256       string    `json:"responseSHA256,omitempty"`
	RequestID            string    `json:"requestId,omitempty"`
	Snapshot             string    `json:"snapshotId,omitempty"`
	Revision             string    `json:"revision,omitempty"`
	Quality              string    `json:"quality,omitempty"`
	ETag                 string    `json:"etag,omitempty"`
}

func phase4ValidItems(snapshot v1.RankingSnapshot) bool {
	total, err := strconv.ParseUint(string(snapshot.TotalEligible), 10, 64)
	if err != nil || total > 1000000 || uint64(len(snapshot.Items)) > total || !snapshot.Replace {
		return false
	}
	seen := make(map[string]bool, len(snapshot.Items))
	for i, item := range snapshot.Items {
		if len(item.PoolAddress) != 42 || !strings.HasPrefix(item.PoolAddress, "0x") || item.PoolAddress != strings.ToLower(item.PoolAddress) || seen[item.PoolAddress] || item.Rank != v1.SafeInteger(i+1) || item.Category != snapshot.Category || item.ModelVersion != snapshot.ModelVersion || item.ComputedAt != snapshot.ComputedAt {
			return false
		}
		address, err := hex.DecodeString(item.PoolAddress[2:])
		if err != nil || len(address) != 20 {
			return false
		}
		if _, err = ranker.Decimal(item.Score, false); err != nil {
			return false
		}
		score, err := strconv.ParseFloat(item.Score, 64)
		if err != nil || math.IsInf(score, 0) || math.IsNaN(score) || score > 1e12 {
			return false
		}
		seen[item.PoolAddress] = true
	}
	expected := total
	if expected > 100 {
		expected = 100
	}
	return uint64(len(snapshot.Items)) == expected && snapshot.Truncated == (total > uint64(len(snapshot.Items)))
}
func phase4ObserveRead(a *phase4Artifacts, sample phase4Read) error {
	a.mu.Lock()
	err := a.histogram.Observe(time.Duration(sample.LatencyNanoseconds))
	a.count++
	if sample.Status != 200 || sample.Error != "" {
		a.failures++
	}
	a.mu.Unlock()
	if err != nil {
		return err
	}
	return a.append(sample)
}

type phase4LimitedLog struct {
	artifacts *phase4Artifacts
	file      *os.File
}

func (w *phase4LimitedLog) Write(raw []byte) (int, error) {
	w.artifacts.mu.Lock()
	defer w.artifacts.mu.Unlock()
	if int64(len(raw)) > w.artifacts.limit-w.artifacts.written {
		return 0, errors.New("actual command log disk budget exhausted")
	}
	n, err := w.file.Write(raw)
	w.artifacts.written += int64(n)
	return n, err
}
func phase4Command(ctx context.Context, argv []string, binary ranker.BoundFile, a *phase4Artifacts, stem string) (*exec.Cmd, *os.File, error) {
	if len(argv) < 1 || len(argv) > 64 || filepath.Clean(argv[0]) != filepath.Clean(binary.Path) {
		return nil, nil, errors.New("checksum-bound actual executable required")
	}
	if _, err := ranker.ReadBoundFile(binary, 1<<30); err != nil {
		return nil, nil, err
	}
	log, err := os.OpenFile(filepath.Join(a.directory, stem+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	limited := &phase4LimitedLog{artifacts: a, file: log}
	cmd.Stdout, cmd.Stderr = limited, limited
	if err = cmd.Start(); err != nil {
		log.Close()
		return nil, nil, err
	}
	return cmd, log, nil
}
func phase4Hot(ctx context.Context, c phase4Config, a *phase4Artifacts) error {
	if c.HotSeconds < 1 || c.HotSeconds > 86400 {
		return errors.New("bounded actual measurement duration required")
	}
	duration := time.Duration(c.HotSeconds) * time.Second
	if c.Mode == "soak" {
		if c.HotSeconds != 86400 {
			return errors.New("full24hour actual peak soak required")
		}
	} else if c.HotSeconds < 600 || c.HotSeconds > 3600 {
		return errors.New("hot measurement requires600..3600seconds")
	}
	if phase4Endpoint(c.Origin) != nil || phase4Endpoint(c.TelemetryURL) != nil || c.TelemetryEverySeconds < 1 || c.TelemetryEverySeconds > 60 {
		return errors.New("actual origin and runtime telemetry endpoints required")
	}
	if _, err := ranker.Policy(c.Category); err != nil {
		return err
	}
	origin, err := url.Parse(c.Origin)
	if err != nil {
		return err
	}
	if origin.Path != "" || origin.RawQuery != "" {
		return errors.New("exact origin without path/query required")
	}
	if c.StartUTCNanoseconds <= time.Now().UnixNano() || c.StartUTCNanoseconds-time.Now().UnixNano() > int64(5*time.Minute) {
		return errors.New("actual coordinated future baseline start required")
	}
	if _, err = phase4RuntimeRead(ctx, c); err != nil {
		return err
	}
	commandCtx, stop := context.WithCancel(ctx)
	defer stop()
	workload, log, err := phase4Command(commandCtx, c.WorkloadCommand, c.WorkloadBinary, a, "mixed-workload")
	if err != nil {
		return err
	}
	defer log.Close()
	mixedDone := make(chan error, 1)
	go func() { mixedDone <- workload.Wait() }()
	completedMixed := false
	defer func() {
		stop()
		if !completedMixed {
			<-mixedDone
		}
	}()
	baseline := time.Unix(0, c.StartUTCNanoseconds)
	baselineTimer := time.NewTimer(time.Until(baseline))
	select {
	case <-ctx.Done():
		baselineTimer.Stop()
		return ctx.Err()
	case <-baselineTimer.C:
	}
	started := time.Now()
	end := started.Add(duration)
	client, err := phase4Client(c)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	jobs := make(chan phase4Read, 100)
	fail := make(chan error, 1)
	report := func(err error) {
		if err != nil {
			select {
			case fail <- err:
			default:
			}
		}
	}
	var workers sync.WaitGroup
	var active, maximum atomic.Int64
	for worker := 0; worker < 100; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for sample := range jobs {
				n := active.Add(1)
				for prior := maximum.Load(); n > prior && !maximum.CompareAndSwap(prior, n); prior = maximum.Load() {
				}
				sample.StartedNanoseconds = time.Since(started).Nanoseconds()
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Origin+"/rankings/"+c.Category+"?limit=100", nil)
				if err == nil {
					response, e := client.Do(request)
					err = e
					if e == nil {
						sample.Status = response.StatusCode
						sample.ETag = response.Header.Get("ETag")
						raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
						response.Body.Close()
						if e != nil || len(raw) > 1<<20 {
							err = errors.New("bounded actual response unavailable")
						} else {
							sum := sha256.Sum256(raw)
							sample.ResponseSHA256 = hex.EncodeToString(sum[:])
							var body v1.RankingsResponse
							if json.Unmarshal(raw, &body) != nil || response.StatusCode != 200 || body.RequestId == "" || body.Data.Snapshot.Category != c.Category || body.Data.Snapshot.SnapshotId == "" || body.Data.Snapshot.ModelVersion != ranker.AlgorithmVersion || body.Data.Snapshot.Items == nil || !phase4ValidItems(body.Data.Snapshot) || body.Quality != "complete" || body.Revision == nil || sample.ETag == "" || len(body.Data.Snapshot.Items) > 100 {
								err = errors.New("actual origin ranking response invalid or stale")
							} else {
								sample.RequestID = body.RequestId
								sample.Snapshot = body.Data.Snapshot.SnapshotId
								sample.Revision = string(*body.Revision)
								sample.Quality = body.Quality
							}
						}
					}
				}
				if err != nil {
					sample.Error = "origin-read-failed"
				}
				sample.CompletedNanoseconds = time.Since(started).Nanoseconds()
				sample.LatencyNanoseconds = sample.CompletedNanoseconds - sample.ScheduledNanoseconds
				active.Add(-1)
				report(phase4ObserveRead(a, sample))
			}
		}()
	}
	telemetryDone := make(chan error, 1)
	telemetryCtx, cancelTelemetry := context.WithCancel(ctx)
	go func() { err := phase4Telemetry(telemetryCtx, c, a, started, end); telemetryDone <- err; report(err) }()
	timer := time.NewTimer(0)
	defer timer.Stop()
	var scheduleErr error
	scheduled := uint64(duration/time.Second) * 600
	for id := uint64(0); id < scheduled; id++ {
		deadline := started.Add(time.Duration(id) * time.Second / 600)
		wait := time.Until(deadline)
		if wait < 0 {
			wait = 0
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(wait)
		select {
		case <-ctx.Done():
			scheduleErr = ctx.Err()
		case err := <-fail:
			scheduleErr = err
		case err := <-mixedDone:
			completedMixed = true
			scheduleErr = errors.Join(errors.New("mixed global workload exited before ranking measurement completed"), err)
		case <-timer.C:
		}
		if scheduleErr != nil {
			break
		}
		sample := phase4Read{ID: id + 1, ScheduledUTC: deadline.UTC(), ScheduledNanoseconds: deadline.Sub(started).Nanoseconds()}
		select {
		case jobs <- sample:
		default:
			sample.CompletedNanoseconds = time.Since(started).Nanoseconds()
			sample.LatencyNanoseconds = sample.CompletedNanoseconds - sample.ScheduledNanoseconds
			sample.Error = "bounded-reader-queue-rejected"
			report(phase4ObserveRead(a, sample))
		}
	}
	close(jobs)
	workers.Wait()
	if scheduleErr == nil {
		select {
		case scheduleErr = <-fail:
		default:
		}
	}
	if scheduleErr != nil {
		cancelTelemetry()
		return errors.Join(scheduleErr, <-telemetryDone)
	}
	telemetryErr := <-telemetryDone
	cancelTelemetry()
	if scheduleErr != nil {
		return errors.Join(scheduleErr, telemetryErr)
	}
	if time.Now().Before(end) {
		timer.Reset(time.Until(end))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
	var mixedErr error
	select {
	case mixedErr = <-mixedDone:
		completedMixed = true
	case <-time.After(30 * time.Second):
		stop()
		mixedErr = <-mixedDone
		completedMixed = true
		mixedErr = errors.Join(errors.New("mixed global workload did not finish within30seconds after interval"), mixedErr)
	case <-ctx.Done():
		return ctx.Err()
	}
	if mixedErr != nil {
		return errors.Join(errors.New("actual mixed workload did not exit successfully"), mixedErr)
	}
	a.mu.Lock()
	histogram, count, failures := a.histogram, a.count, a.failures
	a.mu.Unlock()
	p95, err := histogram.QuantileUpper(95)
	if err != nil {
		return err
	}
	p99, err := histogram.QuantileUpper(99)
	if err != nil {
		return err
	}
	if err = a.document("latency-histogram.json", map[string]any{"histogram": histogram, "quantilePrecisionNanoseconds": 1000000, "p95UpperNanoseconds": p95, "p99UpperNanoseconds": p99, "scheduled": scheduled, "recorded": count, "failed": failures, "readerWorkers": 100, "additionalMeasuredRankingsRPS": 600, "mixedGlobalPeakRPS": 10000, "maximumConcurrentActualReads": maximum.Load(), "latencyIncludesSchedulerQueue": true, "binding": c.Binding}); err != nil {
		return err
	}
	if telemetryErr != nil {
		return telemetryErr
	}
	if count != scheduled || failures != 0 || p95 > 150000000 || p99 > 400000000 {
		return errors.New("actual hot-origin completeness/error/latency bound failed")
	}
	return phase4MixedEvidence(c, a, started, end)
}

type phase4Runtime struct {
	ObservedAt         time.Time `json:"observedAt"`
	Candidate          string    `json:"candidate"`
	Interface          string    `json:"interface"`
	Topology           string    `json:"topology"`
	Image              string    `json:"image"`
	Formula            string    `json:"formula"`
	Instance           string    `json:"instance"`
	Goroutines         uint64    `json:"goroutines"`
	HeapInUseBytes     uint64    `json:"heapInUseBytes"`
	RSSBytes           uint64    `json:"rssBytes"`
	CacheHits          uint64    `json:"cacheHits"`
	CacheReads         uint64    `json:"cacheReads"`
	CompletedRefreshes uint64    `json:"completedRefreshes"`
	CanonicalDigest    string    `json:"canonicalDigest"`
	ModerationDigest   string    `json:"moderationDigest"`
	AlgorithmVersion   string    `json:"algorithmVersion"`
}

func phase4RuntimeRead(ctx context.Context, c phase4Config) (phase4Runtime, error) {
	var sample phase4Runtime
	if phase4Endpoint(c.TelemetryURL) != nil {
		return sample, ranker.ErrUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.TelemetryURL, nil)
	if err != nil {
		return sample, err
	}
	client, err := phase4Client(c)
	if err != nil {
		return sample, err
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return sample, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || response.StatusCode != 200 || json.Unmarshal(raw, &sample) != nil || sample.Instance == "" || sample.Candidate != c.Binding.Candidate || sample.Interface != c.Binding.Interface || sample.Topology != c.Binding.Topology || sample.Image != c.Binding.Image || sample.Formula != c.Binding.Formula || sample.ObservedAt.IsZero() || sample.ObservedAt.After(time.Now()) || time.Since(sample.ObservedAt) > time.Duration(c.TelemetryEverySeconds)*time.Second || sample.Goroutines == 0 || sample.HeapInUseBytes == 0 || sample.RSSBytes == 0 || sample.CacheHits > sample.CacheReads || len(sample.CanonicalDigest) != 64 || len(sample.ModerationDigest) != 64 {
		return sample, errors.New("actual bound ranking-process telemetry capability unavailable")
	}
	return sample, nil
}
func phase4Telemetry(ctx context.Context, c phase4Config, a *phase4Artifacts, start, end time.Time) error {
	period := time.Duration(c.TelemetryEverySeconds) * time.Second
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	var first, last phase4Runtime
	var count int64
	monotonicGoroutines, monotonicHeap := true, true
	var sampledUntil time.Time
	for {
		sample, err := phase4RuntimeRead(ctx, c)
		if err != nil {
			return err
		}
		if count == 0 {
			first = sample
		} else {
			if sample.Instance != last.Instance || !sample.ObservedAt.After(last.ObservedAt) || sample.CacheHits < last.CacheHits || sample.CacheReads < last.CacheReads || sample.CompletedRefreshes < last.CompletedRefreshes {
				return errors.New("runtime identity/counter regression or repeated stale telemetry")
			}
			if sample.Goroutines < last.Goroutines {
				monotonicGoroutines = false
			}
			if sample.HeapInUseBytes < last.HeapInUseBytes {
				monotonicHeap = false
			}
		}
		var generator runtime.MemStats
		runtime.ReadMemStats(&generator)
		if err = a.append(map[string]any{"kind": "runtime", "target": sample, "generatorGoroutines": runtime.NumGoroutine(), "generatorHeapAllocBytes": generator.HeapAlloc, "binding": c.Binding}); err != nil {
			return err
		}
		count++
		last = sample
		sampledUntil = time.Now()
		if !sampledUntil.Before(end) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
	cacheReads, cacheHits := last.CacheReads-first.CacheReads, last.CacheHits-first.CacheHits
	if err := a.document("runtime-summary.json", map[string]any{"samples": count, "startedAt": start.UTC(), "lastSampleAt": sampledUntil.UTC(), "targetFirst": first, "targetLast": last, "cacheReadsDelta": cacheReads, "cacheHitsDelta": cacheHits, "monotonicGoroutineGrowth": monotonicGoroutines && last.Goroutines > first.Goroutines, "monotonicHeapGrowth": monotonicHeap && last.HeapInUseBytes > first.HeapInUseBytes, "binding": c.Binding}); err != nil {
		return err
	}
	if count < 2 || sampledUntil.Sub(start) < end.Sub(start) || cacheReads == 0 || last.CompletedRefreshes <= first.CompletedRefreshes {
		return errors.New("actual target workload/refresh telemetry did not cover interval")
	}
	if c.Mode == "soak" && ((monotonicGoroutines && last.Goroutines > first.Goroutines) || (monotonicHeap && last.HeapInUseBytes > first.HeapInUseBytes)) {
		return errors.New("actual24hour target goroutine or heap growth")
	}
	return nil
}
func phase4ProducedFile(path string, maximum int64, started time.Time) ([]byte, string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > maximum || info.ModTime().Before(started) {
		return nil, "", errors.New("fresh actual producer artifact required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil || int64(len(raw)) > maximum {
		return nil, "", errors.New("bounded actual producer artifact required")
	}
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:]), nil
}
func phase4MixedEvidence(c phase4Config, a *phase4Artifacts, start, end time.Time) error {
	if len(c.WorkloadHosts) != 4 {
		return errors.New("four actual mixed workload host reports required")
	}
	ids := map[string]bool{}
	for index, file := range c.WorkloadHosts {
		raw, checksum, err := phase4ProducedFile(file, 16<<20, start)
		if err != nil {
			return err
		}
		if err = a.document(fmt.Sprintf("mixed-host-%d.json", index), json.RawMessage(raw)); err != nil {
			return err
		}
		if err = a.append(map[string]any{"kind": "mixed-host-artifact", "index": index, "sha256": checksum, "binding": c.Binding}); err != nil {
			return err
		}
		var host struct {
			HostID           string                                                     `json:"host_id"`
			Bindings         struct{ Candidate, Interface, Topology, Generator string } `json:"bindings"`
			Version          string                                                     `json:"workload_version"`
			Started          int64                                                      `json:"started_utc_ns"`
			Ended            int64                                                      `json:"ended_utc_ns"`
			Duration         int64                                                      `json:"duration_ns"`
			FailureCount     int64                                                      `json:"failure_count"`
			Accepted         int64                                                      `json:"accepted_connections"`
			Authenticated    int64                                                      `json:"authenticated_connections"`
			Public           int64                                                      `json:"public_connections"`
			Subscriptions    int64                                                      `json:"accepted_subscriptions"`
			MaxSubscriptions int64                                                      `json:"max_subscriptions"`
			Replay           bool                                                       `json:"replay_qualified"`
			HTTP             struct {
				Scheduled, Received, Missed uint64
				CategoryCounts              map[string]uint64 `json:"category_counts"`
			} `json:"http"`
		}
		if json.Unmarshal(raw, &host) != nil || host.HostID == "" || ids[host.HostID] || host.Version != "kindle-500k-dau-v1" || host.Bindings.Candidate != c.Binding.Candidate || host.Bindings.Interface != c.Binding.Interface || host.Bindings.Topology != c.Binding.Topology || host.Bindings.Generator != c.Binding.Workload || host.Started > start.UnixNano() || host.Ended < end.UnixNano() || host.Duration < end.Sub(start).Nanoseconds() || host.FailureCount != 0 || !host.Replay || host.Accepted != 25000 || host.Authenticated != 12500 || host.Public != 12500 || host.Subscriptions != 125000 || host.MaxSubscriptions > 20 || host.HTTP.Missed != 0 || host.HTTP.Received != host.HTTP.Scheduled {
			return errors.New("actual overlapping mixed global peak report incomplete or differs")
		}
		ids[host.HostID] = true
		rates := map[string]uint64{"chart": 800, "portfolio": 800, "search": 600, "quotes": 2000, "writes": 2000, "market": 1000, "rankings": 600, "identity_profile": 500, "metadata_media": 500, "notifications_social": 800, "capabilities": 400}
		if len(host.HTTP.CategoryCounts) != len(rates) {
			return errors.New("exact current frozen traffic category inventory required")
		}
		seconds := uint64(c.HotSeconds)
		if host.HTTP.Scheduled < seconds*2500 {
			return errors.New("actual mixed host below frozen peak throughput")
		}
		for category, rate := range rates {
			if host.HTTP.CategoryCounts[category] < seconds*rate/4 {
				return fmt.Errorf("actual mixed traffic category%s below frozen peak", category)
			}
		}
	}
	return nil
}
func phase4Replay(c phase4Config, a *phase4Artifacts) error {
	if len(c.Replay) < 78 || len(c.Replay) > 10000 {
		return errors.New("72hour sampled archive requires all six categories at13 or more cutoff sets")
	}
	cutoffs := map[int64]map[string]bool{}
	var minimum, maximum int64
	for _, sample := range c.Replay {
		if _, err := ranker.Policy(sample.Category); err != nil {
			return err
		}
		if cutoffs[sample.Cutoff] == nil {
			cutoffs[sample.Cutoff] = map[string]bool{}
		}
		if cutoffs[sample.Cutoff][sample.Category] {
			return errors.New("duplicate historical category/cutoff")
		}
		cutoffs[sample.Cutoff][sample.Category] = true
		if minimum == 0 || sample.Cutoff < minimum {
			minimum = sample.Cutoff
		}
		if sample.Cutoff > maximum {
			maximum = sample.Cutoff
		}
		measured, err := ranker.MeasureHistoricalReplay(sample)
		if captureErr := a.append(map[string]any{"kind": "historical-replay", "measurement": measured, "binding": c.Binding}); captureErr != nil {
			return captureErr
		}
		if err != nil {
			return err
		}
	}
	if maximum-minimum < 72*3600 {
		return errors.New("genuine historical sample span is less than72hours")
	}
	var sorted []int64
	for cutoff, categories := range cutoffs {
		if len(categories) != 6 {
			return errors.New("historical cutoff missing category")
		}
		sorted = append(sorted, cutoff)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for i := 1; i < len(sorted); i++ {
		if sorted[i]-sorted[i-1] > 6*3600 {
			return errors.New("72hour historical sampling gap exceeds6hours")
		}
	}
	return a.document("historical-summary.json", map[string]any{"samples": len(c.Replay), "cutoffSets": len(cutoffs), "firstCutoff": minimum, "lastCutoff": maximum, "allInputAndOutputDigestsEqual": true, "binding": c.Binding})
}
func phase4Rollback(ctx context.Context, c phase4Config, a *phase4Artifacts) error {
	before, err := phase4RuntimeRead(ctx, c)
	if err != nil {
		return err
	}
	cmd, log, err := phase4Command(ctx, c.RollbackCommand, c.RollbackBinary, a, "rollback")
	if err != nil {
		return err
	}
	defer log.Close()
	if err = cmd.Wait(); err != nil {
		return errors.New("actual approved rollback command failed")
	}
	raw, receiptSHA, err := phase4ProducedFile(c.RollbackReceipt, 4<<20, before.ObservedAt)
	if err != nil {
		return err
	}
	var receipt struct {
		Candidate         string           `json:"candidate"`
		Interface         string           `json:"interface"`
		Topology          string           `json:"topology"`
		PreviousCandidate string           `json:"previousCandidate"`
		PreviousImage     string           `json:"previousImage"`
		PreviousFormula   string           `json:"previousFormula"`
		PreviousAlgorithm string           `json:"previousAlgorithm"`
		CurrentImage      string           `json:"currentImage"`
		StartedAt         time.Time        `json:"startedAt"`
		CompletedAt       time.Time        `json:"completedAt"`
		Reviewer          string           `json:"reviewer"`
		RetainedInput     ranker.BoundFile `json:"retainedInput"`
		RestoredArtifact  ranker.BoundFile `json:"restoredArtifact"`
	}
	if json.Unmarshal(raw, &receipt) != nil || receipt.Candidate != c.Binding.Candidate || receipt.Interface != c.Binding.Interface || receipt.Topology != c.Binding.Topology || receipt.CurrentImage != c.Binding.Image || len(receipt.PreviousCandidate) != 64 || len(receipt.PreviousImage) != 64 || len(receipt.PreviousFormula) != 64 || receipt.PreviousAlgorithm == "" || receipt.Reviewer == "" || receipt.StartedAt.Before(before.ObservedAt) || !receipt.CompletedAt.After(receipt.StartedAt) || receipt.CompletedAt.After(time.Now()) {
		return errors.New("fresh actual reviewed rollback receipt required")
	}
	changed := c
	changed.Binding.Candidate = receipt.PreviousCandidate
	changed.Binding.Image = receipt.PreviousImage
	changed.Binding.Formula = receipt.PreviousFormula
	after, err := phase4RuntimeRead(ctx, changed)
	if err != nil {
		return err
	}
	if after.AlgorithmVersion != receipt.PreviousAlgorithm || after.CanonicalDigest != before.CanonicalDigest || after.ModerationDigest != before.ModerationDigest || after.ObservedAt.Before(receipt.CompletedAt) {
		return errors.New("rollback failed prior runtime restoration or changed chain/moderation state")
	}
	retainedRaw, err := ranker.ReadBoundFile(receipt.RetainedInput, 1<<30)
	if err != nil {
		return err
	}
	retained, err := ranker.ReadComparisonArtifact(retainedRaw, receipt.RetainedInput.SHA256)
	if err != nil {
		return err
	}
	restoredRaw, err := ranker.ReadBoundFile(receipt.RestoredArtifact, 1<<30)
	if err != nil {
		return err
	}
	restored, err := ranker.ReadComparisonArtifact(restoredRaw, receipt.RestoredArtifact.SHA256)
	if err != nil {
		return err
	}
	if retained.Metadata().Provenance == nil || retained.Metadata().Provenance.ModerationRevision != after.ModerationDigest {
		return errors.New("retained rollback snapshot requires current moderation reevaluation")
	}
	if retained.InputDigest() != restored.InputDigest() || retained.OutputDigest() != restored.OutputDigest() || restored.Input().AlgorithmVersion != receipt.PreviousAlgorithm {
		return errors.New("actual previous binary did not restore frozen ranking output")
	}
	return a.document("rollback-evidence.json", map[string]any{"before": before, "after": after, "receipt": receipt, "receiptSHA256": receiptSHA, "binding": c.Binding, "promotionAuthorizedByHarness": false})
}
func phase4Package(c phase4Config, a *phase4Artifacts, executionErr error) error {
	files, err := ranker.ValidateMeasurementSource(c.SourceRoot, c.SourceManifest, c.Binding)
	if err != nil {
		return err
	}
	formula, digest, err := ranker.CurrentFormulaManifest(files)
	if err != nil || digest != c.Binding.Formula {
		return errors.New("source changed during measurement")
	}
	if err = a.document("formula-manifest.json", formula); err != nil {
		return err
	}
	entries, err := os.ReadDir(a.directory)
	if err != nil {
		return err
	}
	artifacts := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			return errors.New("unexpected artifact directory")
		}
		f, err := os.Open(filepath.Join(a.directory, entry.Name()))
		if err != nil {
			return err
		}
		h := sha256.New()
		_, copyErr := io.Copy(h, bufio.NewReaderSize(f, 1<<20))
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
		artifacts[entry.Name()] = hex.EncodeToString(h.Sum(nil))
	}
	return a.document("measurement-result.json", map[string]any{"mode": c.Mode, "binding": c.Binding, "artifacts": artifacts, "measurementPassed": executionErr == nil, "capacityQualified": false, "productionQualified": false, "finalSharedAcceptanceTask": "9.1"})
}
func TestPhase4ActualMeasurementHarness(t *testing.T) {
	c, err := phase4LoadConfig()
	if err != nil {
		t.Fatal("actual source-bound phase4 configuration required", err)
	}
	a, err := newPhase4Artifacts(c)
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Duration(1800) * time.Second
	if c.Mode == "soak" {
		duration = 24*time.Hour + 10*time.Minute
	} else if c.Mode == "hot" {
		duration = time.Duration(c.HotSeconds)*time.Second + 10*time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	switch c.Mode {
	case "refresh":
		err = phase4Refresh(ctx, c, a)
	case "hot", "soak":
		err = phase4Hot(ctx, c, a)
	case "replay":
		err = phase4Replay(c, a)
	case "rollback":
		err = phase4Rollback(ctx, c, a)
	}
	closeErr := a.close()
	packageErr := phase4Package(c, a, errors.Join(err, closeErr))
	if err != nil || closeErr != nil || packageErr != nil {
		t.Fatal("actual measurement failed", errors.Join(err, closeErr, packageErr))
	}
	t.Logf("actual lane measurement captured in%s; final shared acceptance remains9.1", c.Output)
}
