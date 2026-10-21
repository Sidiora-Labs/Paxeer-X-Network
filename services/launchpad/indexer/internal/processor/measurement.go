package processor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/indexer/internal/store"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type IngestMeasurementSpec struct {
	RunID, CandidateSHA256, Profile, DeliverySchema, Region, TopologySHA256 string
	TopologyPath                                                            string
	CaptureMaxBytes                                                         int64
	SourceFiles                                                             map[string]string
	Duration                                                                time.Duration
	BatchBlocks                                                             int
	MaxMemoryBytes                                                          uint64
	MaxDatabaseConnections                                                  int32
	HotPool                                                                 string
	OutputPath                                                              string
	Backfill                                                                *store.ReplayRequest
}
type IngestMeasurementHarness struct {
	SQLTrace  *IngestSQLMeasurementTracer
	Processor *Processor
	Store     *store.Store
	Pool      *pgxpool.Pool
	Replay    *ReplayRunner
	Archive   *Archive
}
type IngestMeasurementReport struct {
	RunID, CandidateSHA256, Profile, Region, TopologySHA256, CaptureSHA256 string
	StartedAt, FinishedAt                                                  time.Time
	TargetEventsPerSecond                                                  int
	OfferedRaw, CommittedRaw, CommittedBlocks, Transactions                int64
	SourceBytes, WALBytes                                                  uint64
	MaxRawBatchBytes                                                       uint64
	MaxRawBatchRows                                                        int
	SQLTrace                                                               IngestSQLMeasurementCounters
	CaptureDigestScope                                                     string
	FinalCanonicalRange                                                    *store.CanonicalRangeDigest
	WALBytesPerEvent, EventsPerSecond                                      float64
	ObservedToDurableP99MS, ScheduledToDurableP99MS, CommitP99MS           int64
	PeakGoHeapBytes, PeakRSSBytes                                          uint64
	PeakDatabaseConnections                                                int32
	HotPoolFills, TotalFills                                               int64
	Violations                                                             []string
	FinalSharedAcceptanceDeferred                                          bool
	WarmupExcludedSeconds                                                  int
	LatencyHistogramOverflowSentinelMS                                     int
	ObservedLatencySamples, ScheduledLatencySamples, CommitLatencySamples  uint64
	WALScope, SQLTraceScope                                                string
}
type measurementHistogram struct {
	bins  [60002]uint64
	count uint64
}

func (h *measurementHistogram) add(duration time.Duration, count uint64) {
	if count == 0 {
		return
	}
	ms := (duration.Nanoseconds() + int64(time.Millisecond) - 1) / int64(time.Millisecond)
	if ms < 0 {
		ms = 0
	}
	if ms > 60000 {
		ms = 60001
	}
	h.bins[ms] += count
	h.count += count
}
func (h *measurementHistogram) p99() int64 {
	if h.count == 0 {
		return -1
	}
	required := (h.count*99 + 99) / 100
	var total uint64
	for ms, count := range h.bins {
		total += count
		if total >= required {
			return int64(ms)
		}
	}
	return -1
}

type measurementCapture struct {
	file    *os.File
	hash    io.Writer
	encoder *json.Encoder
}
type boundedCaptureWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *boundedCaptureWriter) Write(body []byte) (int, error) {
	if int64(len(body)) > w.remaining {
		return 0, errors.New("actual raw capture exceeds admitted storage budget")
	}
	n, err := w.writer.Write(body)
	w.remaining -= int64(n)
	return n, err
}

type IngestTopologyEvidence struct {
	Region, Database, ApprovedAuthorityID string
	CapturedAt                            time.Time
	Zones, NodeIDs                        []string
}

func readTopology(path, digest string) (IngestTopologyEvidence, []byte, error) {
	var evidence IngestTopologyEvidence
	file, err := os.Open(path)
	if err != nil {
		return evidence, nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return evidence, nil, err
	}
	if len(body) > 1<<20 || delivery.Hash(body) != digest {
		return evidence, nil, errors.New("topology evidence digest mismatch")
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err = d.Decode(&evidence); err != nil {
		return evidence, nil, err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return evidence, nil, errors.New("topology trailing data")
	}
	if evidence.Region == "" || evidence.Database == "" || evidence.ApprovedAuthorityID == "" || evidence.CapturedAt.IsZero() || len(evidence.Zones) < 3 || len(evidence.NodeIDs) < 3 {
		return evidence, nil, errors.New("approved three-zone topology evidence missing")
	}
	for _, values := range [][]string{evidence.Zones, evidence.NodeIDs} {
		seen := map[string]bool{}
		for _, value := range values {
			if value == "" || seen[value] {
				return evidence, nil, errors.New("topology must name distinct actual zones and nodes")
			}
			seen[value] = true
		}
	}
	return evidence, body, nil
}
func openMeasurementCapture(path string, maxBytes int64) (*measurementCapture, error) {
	if maxBytes < 1 {
		return nil, errors.New("explicit capture storage budget required")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	return &measurementCapture{file: f, hash: h, encoder: json.NewEncoder(&boundedCaptureWriter{writer: io.MultiWriter(f, h), remaining: maxBytes})}, nil
}
func (c *measurementCapture) write(kind string, value any) error {
	if err := c.encoder.Encode(struct {
		Kind  string `json:"kind"`
		Value any    `json:"value"`
	}{kind, value}); err != nil {
		return err
	}
	return c.file.Sync()
}
func validateMeasurementSource(spec IngestMeasurementSpec) error {
	if spec.TopologyPath == "" || spec.CaptureMaxBytes < 1 || spec.RunID == "" || len(spec.RunID) > 128 || spec.Region == "" || spec.OutputPath == "" || len(spec.SourceFiles) == 0 || len(spec.SourceFiles) > 512 || spec.BatchBlocks < 1 || spec.BatchBlocks > 1000 || spec.MaxMemoryBytes == 0 || spec.MaxDatabaseConnections < 1 || len(spec.TopologySHA256) != 64 {
		return errors.New("measurement requires explicit candidate, source files, region, topology, resource budgets, output and bounded batch")
	}
	topologyEvidence, _, err := readTopology(spec.TopologyPath, spec.TopologySHA256)
	if err != nil {
		return err
	}
	if topologyEvidence.Region != spec.Region {
		return errors.New("topology source region disagrees with run")
	}
	topology, err := hex.DecodeString(spec.TopologySHA256)
	if err != nil || len(topology) != 32 {
		return errors.New("topology digest invalid")
	}
	if err := validateCandidateFiles(spec.SourceFiles, spec.CandidateSHA256); err != nil {
		return err
	}
	switch spec.Profile {
	case "steady":
		if spec.Duration < 5*time.Minute {
			return errors.New("steady measurement must run at least five minutes")
		}
	case "burst":
		if spec.Duration != 5*time.Minute {
			return errors.New("declared burst requires exactly five minutes")
		}
	case "soak":
		if spec.Duration != 24*time.Hour {
			return errors.New("declared mixed soak requires exactly24hours")
		}
	default:
		return errors.New("explicit steady, burst or soak profile required")
	}
	return nil
}
func processRSS() (uint64, error) {
	body, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
			value, err := strconv.ParseUint(fields[1], 10, 64)
			return value * 1024, err
		}
	}
	return 0, errors.New("actual process RSS observation unavailable")
}
func (h *IngestMeasurementHarness) Run(ctx context.Context, spec IngestMeasurementSpec) (report IngestMeasurementReport, result error) {
	report = IngestMeasurementReport{RunID: spec.RunID, CandidateSHA256: spec.CandidateSHA256, Profile: spec.Profile, Region: spec.Region, TopologySHA256: spec.TopologySHA256, Violations: []string{}, FinalSharedAcceptanceDeferred: true, TargetEventsPerSecond: 2000}
	if spec.Profile == "burst" {
		report.TargetEventsPerSecond = 10000
	}
	if err := validateMeasurementSource(spec); err != nil {
		return report, err
	}
	if h == nil || h.SQLTrace == nil || h.Pool == nil || h.Pool.Config().ConnConfig.Tracer != h.SQLTrace || h.Processor == nil || h.Processor.readyFence == nil || h.Store == nil || !h.Store.LifecycleEnabled() || h.Pool == nil || h.Processor.deps.QualifiedBatchSource == nil || h.Processor.deps.Head == nil || h.Processor.deps.Store != h.Store || h.Processor.deps.CanonicalStreamID == "" || h.Processor.deps.CanonicalOwner == "" {
		return report, errors.New("measurement requires actual integrated guarded processor, committed head, qualified source and database")
	}
	if !strings.HasPrefix(spec.HotPool, "0x") || len(spec.HotPool) != 42 {
		return report, errors.New("actual hot pool identity required")
	}
	capture, err := openMeasurementCapture(spec.OutputPath, spec.CaptureMaxBytes)
	if err != nil {
		return report, err
	}
	defer capture.file.Close()
	var observed, scheduled, commit measurementHistogram
	var finalRange *store.CanonicalRangeDigest
	defer func() {
		report.FinishedAt = time.Now().UTC()
		report.LatencyHistogramOverflowSentinelMS = 60001
		report.ObservedLatencySamples = observed.count
		report.ScheduledLatencySamples = scheduled.count
		report.CommitLatencySamples = commit.count
		report.WALScope = "whole observed PostgreSQL cluster WAL including concurrent workload"
		report.SQLTraceScope = "instrumented pool lifetime including declared backfill and measurement queries"
		report.ObservedToDurableP99MS = observed.p99()
		report.ScheduledToDurableP99MS = scheduled.p99()
		report.CommitP99MS = commit.p99()
		if !report.StartedAt.IsZero() {
			elapsed := report.FinishedAt.Sub(report.StartedAt).Seconds()
			if elapsed > 0 {
				report.EventsPerSecond = float64(report.CommittedRaw) / elapsed
			}
		}
		if report.CommittedRaw > 0 {
			report.WALBytesPerEvent = float64(report.WALBytes) / float64(report.CommittedRaw)
		}
		if result != nil {
			report.Violations = append(report.Violations, result.Error())
		}
		if report.ObservedToDurableP99MS < 0 || report.ObservedToDurableP99MS > 500 {
			report.Violations = append(report.Violations, "observed committed block to durable acknowledgement p99 exceeds500ms or lacks samples")
		}
		if report.ScheduledToDurableP99MS < 0 || report.ScheduledToDurableP99MS > 500 {
			report.Violations = append(report.Violations, "open-loop scheduled event to acknowledgement p99 exceeds500ms or lacks samples")
		}
		if report.EventsPerSecond < float64(report.TargetEventsPerSecond) {
			report.Violations = append(report.Violations, "actual durable events per second below declared profile")
		}
		if report.OfferedRaw != report.CommittedRaw {
			report.Violations = append(report.Violations, "offered identities were not all committed")
		}
		if report.TotalFills == 0 || report.HotPoolFills*100 < report.TotalFills*60 {
			report.Violations = append(report.Violations, "actual workload lacks required60percent hot pool concentration")
		}
		if report.PeakGoHeapBytes > spec.MaxMemoryBytes || report.PeakRSSBytes > spec.MaxMemoryBytes {
			report.Violations = append(report.Violations, "measured process memory exceeds admitted budget")
		}
		if report.PeakDatabaseConnections > spec.MaxDatabaseConnections {
			report.Violations = append(report.Violations, "database connections exceed admitted budget")
		}
		report.FinalCanonicalRange = finalRange
		report.SQLTrace = h.SQLTrace.Snapshot()
		if report.SQLTrace.CopyCalls == 0 {
			report.Violations = append(report.Violations, "actual ingest executed no COPY staging")
		}
		if report.SQLTrace.RawScalarInsertQueries >= uint64(report.CommittedRaw) && report.CommittedRaw > 0 {
			report.Violations = append(report.Violations, "actual ingest used one scalar raw insert per event")
		}
		if report.SQLTrace.MaxCopyRows > 5000 || report.MaxRawBatchBytes > 8<<20 || report.MaxRawBatchRows > 5000 {
			report.Violations = append(report.Violations, "observed raw staging input exceeds5000records or8MiB")
		}
		report.CaptureDigestScope = "ndjson-before-report"
		report.CaptureSHA256 = hex.EncodeToString(capture.hash.(interface{ Sum([]byte) []byte }).Sum(nil))
		writeErr := capture.write("report", report)
		if writeErr != nil {
			result = errors.Join(result, writeErr)
		}
		if len(report.Violations) > 0 {
			result = errors.Join(result, errors.New("ingestion measurement has observed violations"))
		}
	}()
	if err = capture.write("input", spec); err != nil {
		return report, err
	}
	topology, topologyBody, err := readTopology(spec.TopologyPath, spec.TopologySHA256)
	if err != nil {
		return report, err
	}
	if err = capture.write("approved-topology-input", json.RawMessage(topologyBody)); err != nil {
		return report, err
	}
	first, err := h.Store.MeasureIngestStorage(ctx, spec.DeliverySchema)
	if err != nil {
		return report, err
	}
	if first.Database != topology.Database {
		return report, errors.New("observed database differs from approved topology")
	}
	if err = capture.write("storage-start", first); err != nil {
		return report, err
	}
	if !first.RawPartitioned || !first.DateRangePartitionKey {
		report.Violations = append(report.Violations, "actual raw history lacks required UTC block-date partition layout")
	}
	checkpoint, err := h.Store.GetCanonicalWatermark(ctx, 125, h.Processor.deps.CanonicalStreamID)
	if err != nil {
		return report, err
	}
	fence, err := h.Store.AcquireLease(ctx, 125, h.Processor.deps.CanonicalStreamID, h.Processor.deps.CanonicalOwner, 30*time.Second)
	if err != nil {
		return report, err
	}
	h.Processor.readyFence.Store(fence)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		result = errors.Join(result, h.Store.ReleaseLease(cleanup, 125, h.Processor.deps.CanonicalStreamID, h.Processor.deps.CanonicalOwner, fence))
		h.Processor.readyFence.Store(0)
	}()
	measurementCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	backfillDone := make(chan error, 1)
	if spec.Backfill != nil {
		if h.Replay == nil {
			return report, errors.New("actual authorized backfill runner required")
		}
		if spec.Backfill.Mode != "backfill" {
			return report, errors.New("concurrent job must be explicit backfill")
		}
		go func() { backfillDone <- h.Replay.Run(measurementCtx, *spec.Backfill) }()
	}
	defer func() {
		cancel()
		if spec.Backfill != nil && backfillDone != nil {
			select {
			case err := <-backfillDone:
				if err != nil && !errors.Is(err, context.Canceled) {
					result = errors.Join(result, err)
				}
			case <-time.After(5 * time.Second):
				result = errors.Join(result, errors.New("backfill did not finish bounded shutdown"))
			}
		}
	}()
	report.StartedAt = time.Now().UTC()
	deadline := report.StartedAt.Add(spec.Duration)
	firstObserved := report.StartedAt
	initialHead, err := h.Processor.deps.Head.Head(ctx)
	if err != nil {
		return report, err
	}
	firstObserved = time.Now().UTC()
	observedHead := initialHead
	observations := []ingestHeadObservation{{Height: initialHead, ObservedAt: firstObserved}}
	headUpdates, headErrors, headDone := h.observeCommittedHeads(measurementCtx, initialHead)
	defer func() {
		cancel()
		select {
		case <-headDone:
		case <-time.After(5 * time.Second):
			result = errors.Join(result, errors.New("independent head observer did not stop within5seconds"))
		}
	}()
	firstHeight := checkpoint + 1
	nextSample := report.StartedAt
	planCaptured := false
	for time.Now().Before(deadline) {
		if err = ctx.Err(); err != nil {
			return report, err
		}
		if spec.Backfill != nil {
			select {
			case err := <-backfillDone:
				if err != nil {
					return report, err
				}
				backfillDone = nil
			default:
			}
		}
		if err = h.Store.RenewLease(ctx, 125, h.Processor.deps.CanonicalStreamID, h.Processor.deps.CanonicalOwner, fence, 30*time.Second); err != nil {
			return report, err
		}
		from := checkpoint + 1
		for {
			select {
			case observation := <-headUpdates:
				observations = append(observations, observation)
				observedHead = observation.Height
				if len(observations) > 4096 {
					return report, errors.New("committed observation backlog exceeded bounded admission")
				}
			case headErr := <-headErrors:
				return report, headErr
			default:
				goto observedHeadsDrained
			}
		}
	observedHeadsDrained:
		if from > observedHead {
			if !sleep(ctx, 10*time.Millisecond) {
				return report, ctx.Err()
			}
			continue
		}
		for len(observations) > 1 && from > observations[0].Height {
			observations = observations[1:]
		}
		firstObserved = observations[0].ObservedAt
		to := from + int64(spec.BatchBlocks) - 1
		if to > observedHead {
			to = observedHead
		}
		readCtx, readCancel := context.WithTimeout(ctx, 20*time.Second)
		batch, receipts, err := h.Processor.deps.QualifiedBatchSource.FetchQualifiedBatch(readCtx, uint64(from), uint64(to))
		readCancel()
		if err != nil {
			return report, err
		}
		if len(batch.Blocks) == 0 {
			return report, errors.New("actual complete source blocks absent")
		}
		batch.StreamID = h.Processor.deps.CanonicalStreamID
		batch.Owner = h.Processor.deps.CanonicalOwner
		batch.Fence = fence
		batch.PreviousHeight = checkpoint
		if batch.BatchID == "" {
			batch.BatchID = fmt.Sprintf("%d-%d-%s", from, to, strings.TrimPrefix(batch.Blocks[len(batch.Blocks)-1].Hash, "0x"))
		}
		segment, err := qualifiedSegment(ctx, batch.StreamID, batch, receipts)
		if err != nil {
			return report, err
		}
		if segment.FromHeight != from || segment.ToHeight != to {
			return report, store.ErrIntegrity
		}
		rawBytes := uint64(0)
		for _, raw := range segment.Records {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return report, err
			}
			rawBytes += uint64(len(encoded))
		}
		if rawBytes > report.MaxRawBatchBytes {
			report.MaxRawBatchBytes = rawBytes
		}
		if len(segment.Records) > report.MaxRawBatchRows {
			report.MaxRawBatchRows = len(segment.Records)
		}
		if rawBytes > 8<<20 || len(segment.Records) > 5000 {
			return report, errors.New("actual input exceeds5000records or8MiB staging boundary")
		}
		body, err := json.Marshal(segment)
		if err != nil {
			return report, err
		}
		report.SourceBytes += uint64(len(body))
		report.OfferedRaw += int64(len(segment.Records))
		due := report.StartedAt.Add(time.Duration(report.CommittedRaw) * time.Second / time.Duration(report.TargetEventsPerSecond))
		if wait := time.Until(due); wait > 0 {
			if !sleep(ctx, wait) {
				return report, ctx.Err()
			}
		}
		if err = capture.write("source-batch", struct {
			ObservedCommittedAt, ScheduledAt time.Time
			HeadObservations                 []ingestHeadObservation
			Segment                          store.ReplaySegment
		}{firstObserved, due, observations, segment}); err != nil {
			return report, err
		}
		started := time.Now()
		commitCtx, commitCancel := context.WithTimeout(ctx, 30*time.Second)
		err = h.Processor.ProcessQualifiedCanonicalBatch(commitCtx, batch, receipts)
		commitCancel()
		ack := time.Now()
		if err != nil {
			if captureErr := capture.write("commit-response-error", err.Error()); captureErr != nil {
				return report, errors.Join(err, captureErr)
			}
			if committed, reconcileErr := h.Store.ReconcileCanonicalCommit(ctx, batch); reconcileErr != nil || !committed {
				return report, errors.Join(err, reconcileErr)
			}
			ack = time.Now()
		}
		committed, reconcileErr := h.Store.ReconcileCanonicalCommit(ctx, batch)
		if reconcileErr != nil || !committed {
			return report, errors.Join(store.ErrIntegrity, reconcileErr)
		}
		count := uint64(len(segment.Records))
		if count == 0 {
			count = 1
		}
		for _, block := range segment.Blocks {
			matched := false
			for _, observation := range observations {
				if observation.Height >= block.Height {
					weight := uint64(block.RawCount)
					if weight == 0 {
						weight = 1
					}
					observed.add(ack.Sub(observation.ObservedAt), weight)
					matched = true
					break
				}
			}
			if !matched {
				return report, errors.New("block lacks actual independent committed-head observation")
			}
		}
		scheduled.add(ack.Sub(due), count)
		commit.add(ack.Sub(started), 1)
		report.CommittedRaw += int64(len(segment.Records))
		report.CommittedBlocks += int64(len(segment.Blocks))
		report.Transactions++
		checkpoint = to
		if err = capture.write("durable-ack", struct {
			BatchID                             string
			From, To, RawCount                  int64
			AcknowledgedAt                      time.Time
			CommitNS, ObservationNS, ScheduleNS int64
		}{batch.BatchID, from, to, int64(len(segment.Records)), ack, ack.Sub(started).Nanoseconds(), ack.Sub(firstObserved).Nanoseconds(), ack.Sub(due).Nanoseconds()}); err != nil {
			return report, err
		}
		for _, event := range segment.Events {
			if event.Kind == accounting.MarketSubject {
				var fill accounting.FillV1
				if err = json.Unmarshal(event.Payload, &fill); err != nil {
					return report, err
				}
				report.TotalFills++
				if fill.PoolAddress == spec.HotPool {
					report.HotPoolFills++
				}
			}
		}
		if !planCaptured && len(segment.Records) > 0 {
			plans, err := h.Store.MeasureIngestPlans(ctx, batch.StreamID, segment.Records[0], spec.HotPool)
			if err != nil {
				return report, err
			}
			if err = capture.write("query-plans", plans); err != nil {
				return report, err
			}
			planCaptured = true
		}
		if !time.Now().Before(nextSample) {
			var memory runtime.MemStats
			runtime.ReadMemStats(&memory)
			rss, err := processRSS()
			if err != nil {
				return report, err
			}
			if memory.HeapAlloc > report.PeakGoHeapBytes {
				report.PeakGoHeapBytes = memory.HeapAlloc
			}
			if rss > report.PeakRSSBytes {
				report.PeakRSSBytes = rss
			}
			connections := h.Pool.Stat().AcquiredConns()
			if connections > report.PeakDatabaseConnections {
				report.PeakDatabaseConnections = connections
			}
			sample, err := h.Store.MeasureIngestStorage(ctx, spec.DeliverySchema)
			if err != nil {
				return report, err
			}
			if err = capture.write("resources", struct {
				Storage     store.IngestStorageSample
				GoHeap, RSS uint64
				Connections int32
			}{sample, memory.HeapAlloc, rss, connections}); err != nil {
				return report, err
			}
			nextSample = time.Now().Add(time.Second)
		}
	}
	select {
	case headErr := <-headErrors:
		return report, headErr
	default:
	}
	cancel()
	last, err := h.Store.MeasureIngestStorage(ctx, spec.DeliverySchema)
	if err != nil {
		return report, err
	}
	report.WALBytes, err = store.WALBytesBetween(first.WALInsertLSN, last.WALInsertLSN)
	if err != nil {
		return report, err
	}
	if err = capture.write("storage-finish", last); err != nil {
		return report, err
	}
	if !planCaptured {
		return report, errors.New("no actual raw data available for query plans")
	}
	if checkpoint < firstHeight {
		return report, errors.New("no complete range committed")
	}
	rangeDigest, err := h.Store.MeasureCanonicalRange(ctx, h.Processor.deps.CanonicalStreamID, firstHeight, checkpoint)
	if err != nil {
		return report, err
	}
	if rangeDigest.RawCount != report.OfferedRaw || rangeDigest.AccountingFillCount != report.TotalFills {
		return report, store.ErrIntegrity
	}
	if err = validateMeasurementSource(spec); err != nil {
		return report, err
	}
	finalRange = &rangeDigest
	if err = capture.write("canonical-identity-reconciliation", rangeDigest); err != nil {
		return report, err
	}
	return report, nil
}

type IngestRestoreCheckpoint struct {
	CandidateSHA256, Region, TopologySHA256 string
	CapturedAt                              time.Time
	Storage                                 store.IngestStorageSample
	Canonical                               store.CanonicalRangeDigest
}

func (h *IngestMeasurementHarness) CaptureRestoreCheckpoint(ctx context.Context, spec IngestMeasurementSpec, from, to int64, path string) (IngestRestoreCheckpoint, error) {
	var checkpoint IngestRestoreCheckpoint
	if err := validateMeasurementSource(spec); err != nil {
		return checkpoint, err
	}
	if h == nil || h.Processor == nil || h.Store == nil || !h.Store.LifecycleEnabled() {
		return checkpoint, errors.New("actual guarded source checkpoint provider required")
	}
	canonical, err := h.Store.MeasureCanonicalRange(ctx, h.Processor.deps.CanonicalStreamID, from, to)
	if err != nil {
		return checkpoint, err
	}
	sample, err := h.Store.MeasureIngestStorage(ctx, spec.DeliverySchema)
	if err != nil {
		return checkpoint, err
	}
	checkpoint = IngestRestoreCheckpoint{CandidateSHA256: spec.CandidateSHA256, Region: spec.Region, TopologySHA256: spec.TopologySHA256, CapturedAt: time.Now().UTC(), Storage: sample, Canonical: canonical}
	body, err := json.Marshal(checkpoint)
	if err != nil {
		return checkpoint, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return checkpoint, err
	}
	defer file.Close()
	if _, err = file.Write(body); err != nil {
		return checkpoint, err
	}
	return checkpoint, file.Sync()
}

type IngestReconstructionSpec struct {
	RunID, CandidateSHA256, OutputPath, RecordedCheckpointPath, RecordedCheckpointSHA256, DeliverySchema string
	CaptureMaxBytes                                                                                      int64
	Request, DuplicateRequest                                                                            store.ReplayRequest
}
type IngestReconstructionReport struct {
	RunID, CandidateSHA256, RecordedCheckpointSHA256                   string
	StartedAt, FinishedAt                                              time.Time
	CheckpointMatched, ArchiveOlderThan72Hours, DuplicateReplayMatched bool
	RestoredRaw, RestoredTyped                                         int64
	SegmentDigests                                                     []string
	Before, AfterDuplicate                                             store.IngestStorageSample
	SourceCheckpoint, RestoredCheckpoint                               store.CanonicalRangeDigest
	InfrastructureRPOOrRTOCertified, TrafficActivationAuthorized       bool
}

func loadRestoreCheckpoint(path, digest string) (IngestRestoreCheckpoint, error) {
	var checkpoint IngestRestoreCheckpoint
	file, err := os.Open(path)
	if err != nil {
		return checkpoint, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return checkpoint, err
	}
	if len(body) > 1<<20 || delivery.Hash(body) != digest {
		return checkpoint, errors.New("recorded restore checkpoint digest mismatch")
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&checkpoint); err != nil {
		return checkpoint, err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return checkpoint, errors.New("checkpoint trailing data")
	}
	if checkpoint.CandidateSHA256 == "" || checkpoint.Storage.Database == "" || checkpoint.CapturedAt.IsZero() || checkpoint.Canonical.RawSHA256 == "" || checkpoint.Canonical.TypedSHA256 == "" {
		return checkpoint, errors.New("actual recorded checkpoint fields missing")
	}
	return checkpoint, nil
}
func (h *IngestMeasurementHarness) Reconstruct(ctx context.Context, spec IngestReconstructionSpec) (report IngestReconstructionReport, result error) {
	report = IngestReconstructionReport{RunID: spec.RunID, CandidateSHA256: spec.CandidateSHA256, RecordedCheckpointSHA256: spec.RecordedCheckpointSHA256, StartedAt: time.Now().UTC(), SegmentDigests: []string{}}
	if h == nil || h.Pool == nil || h.Store == nil || !h.Store.LifecycleEnabled() || h.Replay == nil || h.Archive == nil || h.Archive.Objects == nil || h.Replay.Store != h.Store || h.Replay.Archive != h.Archive || spec.OutputPath == "" || spec.RunID == "" {
		return report, errors.New("actual isolated guarded replay, immutable archive and captured restore input required")
	}
	if err := h.Replay.validate(); err != nil {
		return report, err
	}
	if h.Replay.DeliverySchema != spec.DeliverySchema {
		return report, errors.New("reconstruction outbox schema differs from actual runner")
	}
	if err := store.ValidateReplayRequest(spec.Request); err != nil {
		return report, err
	}
	if err := store.ValidateReplayRequest(spec.DuplicateRequest); err != nil {
		return report, err
	}
	if spec.Request.Mode != "reconstruct" || spec.DuplicateRequest.Mode != "reconstruct" || spec.Request.JobID == spec.DuplicateRequest.JobID {
		return report, errors.New("two distinct authorized archive reconstruction jobs required")
	}
	a, b := spec.Request, spec.DuplicateRequest
	a.JobID = ""
	b.JobID = ""
	a.AuthorizationID = ""
	b.AuthorizationID = ""
	a.Requester = ""
	b.Requester = ""
	if a != b {
		return report, errors.New("duplicate replay must address identical original facts and projection identity")
	}
	checkpoint, err := loadRestoreCheckpoint(spec.RecordedCheckpointPath, spec.RecordedCheckpointSHA256)
	if err != nil {
		return report, err
	}
	if checkpoint.CandidateSHA256 != spec.CandidateSHA256 || checkpoint.Canonical.SourceStream != spec.Request.SourceStream || checkpoint.Canonical.ToHeight >= spec.Request.FromHeight {
		return report, errors.New("recorded checkpoint must bind this candidate and precede recovery range")
	}
	capture, err := openMeasurementCapture(spec.OutputPath, spec.CaptureMaxBytes)
	if err != nil {
		return report, err
	}
	defer capture.file.Close()
	defer func() {
		report.FinishedAt = time.Now().UTC()
		if err := capture.write("reconstruction-report", report); err != nil {
			result = errors.Join(result, err)
		}
	}()
	if err = capture.write("input", spec); err != nil {
		return report, err
	}
	if err = capture.write("recorded-source-checkpoint", checkpoint); err != nil {
		return report, err
	}
	report.Before, err = h.Store.MeasureIngestStorage(ctx, spec.DeliverySchema)
	if err != nil {
		return report, err
	}
	if report.Before.Database == checkpoint.Storage.Database {
		return report, errors.New("restore comparison requires independently restored database identity")
	}
	report.SourceCheckpoint = checkpoint.Canonical
	report.RestoredCheckpoint, err = h.Store.MeasureCanonicalRange(ctx, checkpoint.Canonical.SourceStream, checkpoint.Canonical.FromHeight, checkpoint.Canonical.ToHeight)
	if err != nil {
		return report, err
	}
	if report.RestoredCheckpoint != report.SourceCheckpoint {
		return report, store.ErrIntegrity
	}
	report.CheckpointMatched = true
	for from := spec.Request.FromHeight; from <= spec.Request.ToHeight; {
		to := from + int64(h.Replay.BatchSize) - 1
		if to > spec.Request.ToHeight {
			to = spec.Request.ToHeight
		}
		segment, err := h.Archive.Restore(ctx, spec.Request.SourceStream, from, to)
		if err != nil {
			return report, err
		}
		if len(segment.Events) == 0 {
			return report, errors.New("archive age cannot be proven without original typed event timestamp")
		}
		for _, event := range segment.Events {
			if event.Timestamp > time.Now().Add(-72*time.Hour).Unix() {
				return report, errors.New("archive replay input is inside72hour delivery window")
			}
		}
		body, err := json.Marshal(segment)
		if err != nil {
			return report, err
		}
		digest := delivery.Hash(body)
		report.SegmentDigests = append(report.SegmentDigests, digest)
		if err = capture.write("verified-archive-segment", struct {
			SHA256  string
			Segment store.ReplaySegment
		}{digest, segment}); err != nil {
			return report, err
		}
		from = to + 1
	}
	report.ArchiveOlderThan72Hours = true
	if err = h.Replay.Run(ctx, spec.Request); err != nil {
		return report, err
	}
	for from := spec.Request.FromHeight; from <= spec.Request.ToHeight; {
		to := from + int64(h.Replay.BatchSize) - 1
		if to > spec.Request.ToHeight {
			to = spec.Request.ToHeight
		}
		segment, err := h.Archive.Restore(ctx, spec.Request.SourceStream, from, to)
		if err != nil {
			return report, err
		}
		raw, typed, err := h.Store.CompareReconstructionSegment(ctx, spec.Request, segment)
		if err != nil {
			return report, err
		}
		if err = h.verifyReconstructionOutbox(ctx, spec.Request, segment); err != nil {
			return report, err
		}
		report.RestoredRaw += raw
		report.RestoredTyped += typed
		from = to + 1
	}
	firstEffects, err := h.reconstructionEffects(ctx, spec.Request)
	if err != nil {
		return report, err
	}
	if err = h.Replay.Run(ctx, spec.DuplicateRequest); err != nil {
		return report, err
	}
	duplicateEffects, err := h.reconstructionEffects(ctx, spec.Request)
	if err != nil {
		return report, err
	}
	if firstEffects != duplicateEffects {
		return report, store.ErrIntegrity
	}
	for from := spec.Request.FromHeight; from <= spec.Request.ToHeight; {
		to := from + int64(h.Replay.BatchSize) - 1
		if to > spec.Request.ToHeight {
			to = spec.Request.ToHeight
		}
		segment, err := h.Archive.Restore(ctx, spec.Request.SourceStream, from, to)
		if err != nil {
			return report, err
		}
		if _, _, err = h.Store.CompareReconstructionSegment(ctx, spec.Request, segment); err != nil {
			return report, err
		}
		if err = h.verifyReconstructionOutbox(ctx, spec.Request, segment); err != nil {
			return report, err
		}
		from = to + 1
	}
	report.DuplicateReplayMatched = true
	report.AfterDuplicate, err = h.Store.MeasureIngestStorage(ctx, spec.DeliverySchema)
	if err != nil {
		return report, err
	}
	if err = capture.write("effective-projection-digest", firstEffects); err != nil {
		return report, err
	}
	return report, nil
}
func (h *IngestMeasurementHarness) reconstructionEffects(ctx context.Context, r store.ReplayRequest) (string, error) {
	rows, err := h.Pool.Query(ctx, `SELECT effect_id,raw_digest,height,event_kind,payload FROM indexer.indexer_replay_effects WHERE target=$1 AND generation=$2 AND projection_revision=$3 AND height BETWEEN $4 AND $5 ORDER BY height,raw_digest,event_kind,effect_id`, r.Target, r.Generation, r.ProjectionRevision, r.FromHeight, r.ToHeight)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	digest := sha256.New()
	for rows.Next() {
		var effect, raw, kind string
		var height int64
		var payload []byte
		if err = rows.Scan(&effect, &raw, &height, &kind, &payload); err != nil {
			return "", err
		}
		var value any
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.UseNumber()
		if err = decoder.Decode(&value); err != nil {
			return "", err
		}
		payload, err = json.Marshal(value)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "%d:%s:%d:%s:%d:%s:%d:", len(effect), effect, len(raw), raw, height, kind, len(payload))
		digest.Write(payload)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

type IngestSQLMeasurementCounters struct{ QueryCalls, QueryFailures, RawScalarInsertQueries, CopyCalls, CopyRows, MaxCopyRows uint64 }
type IngestSQLMeasurementTracer struct{ queries, failures, rawScalar, copies, copyRows, maxCopyRows atomic.Uint64 }

func (t *IngestSQLMeasurementTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	t.queries.Add(1)
	sql := strings.ToLower(strings.ReplaceAll(data.SQL, "\"", ""))
	if strings.Contains(sql, "insert into indexer.indexer_raw_logs") && (strings.Contains(sql, "values(") || strings.Contains(sql, "values (")) {
		t.rawScalar.Add(1)
	}
	return ctx
}
func (t *IngestSQLMeasurementTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		t.failures.Add(1)
	}
}
func (t *IngestSQLMeasurementTracer) TraceCopyFromStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceCopyFromStartData) context.Context {
	t.copies.Add(1)
	return ctx
}
func (t *IngestSQLMeasurementTracer) TraceCopyFromEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceCopyFromEndData) {
	if data.Err != nil {
		t.failures.Add(1)
		return
	}
	rows := uint64(data.CommandTag.RowsAffected())
	t.copyRows.Add(rows)
	for {
		old := t.maxCopyRows.Load()
		if rows <= old || t.maxCopyRows.CompareAndSwap(old, rows) {
			return
		}
	}
}
func (t *IngestSQLMeasurementTracer) Snapshot() IngestSQLMeasurementCounters {
	return IngestSQLMeasurementCounters{QueryCalls: t.queries.Load(), QueryFailures: t.failures.Load(), RawScalarInsertQueries: t.rawScalar.Load(), CopyCalls: t.copies.Load(), CopyRows: t.copyRows.Load(), MaxCopyRows: t.maxCopyRows.Load()}
}

type IngestInfrastructureObservation struct {
	ObservedAt                                   time.Time
	CandidateSHA256, FailureDriverEvidenceSHA256 string
	Ready                                        bool
	FailureDriverEvidenceExternallyAsserted      bool
	ReadinessError                               string
	Storage                                      *store.IngestStorageSample
	Canonical                                    *store.CanonicalRangeDigest
	ActualReadErrors                             []string
	InfrastructureRPOOrRTOCertified              bool
}

func (h *IngestMeasurementHarness) ObserveInfrastructureFailure(ctx context.Context, spec IngestMeasurementSpec, failureEvidencePath, failureEvidenceDigest string, from, to int64) (IngestInfrastructureObservation, error) {
	observation := IngestInfrastructureObservation{ObservedAt: time.Now().UTC(), CandidateSHA256: spec.CandidateSHA256, FailureDriverEvidenceSHA256: failureEvidenceDigest, ActualReadErrors: []string{}}
	if err := validateMeasurementSource(spec); err != nil {
		return observation, err
	}
	file, err := os.Open(failureEvidencePath)
	if err != nil {
		return observation, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return observation, err
	}
	if len(body) == 0 || len(body) > 1<<20 || delivery.Hash(body) != failureEvidenceDigest {
		return observation, errors.New("actual external node or zone failure evidence must be supplied and pinned")
	}
	observation.FailureDriverEvidenceExternallyAsserted = true
	if h == nil || h.Processor == nil || h.Store == nil {
		return observation, errors.New("actual indexer providers unavailable")
	}
	bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = h.Processor.Ready(bounded); err != nil {
		observation.ReadinessError = err.Error()
	} else {
		observation.Ready = true
	}
	storage, err := h.Store.MeasureIngestStorage(bounded, spec.DeliverySchema)
	if err != nil {
		observation.ActualReadErrors = append(observation.ActualReadErrors, err.Error())
	} else {
		observation.Storage = &storage
	}
	canonical, err := h.Store.MeasureCanonicalRange(bounded, h.Processor.deps.CanonicalStreamID, from, to)
	if err != nil {
		observation.ActualReadErrors = append(observation.ActualReadErrors, err.Error())
	} else {
		observation.Canonical = &canonical
	}
	return observation, nil
}

func (h *IngestMeasurementHarness) verifyReconstructionOutbox(ctx context.Context, r store.ReplayRequest, segment store.ReplaySegment) error {
	policy := delivery.Policy{Subjects: map[string]bool{ReplaySubjectV2: true}, EventTypes: map[string]bool{ReplaySubjectV2: true}}
	table := pgx.Identifier{h.Replay.DeliverySchema, "delivery_outbox"}.Sanitize()
	for _, event := range segment.Events {
		effect := ReplayEffectIdentity(r, event)
		wrapper := ReplayEnvelopeV2{Version: "indexer-replay-v2", Mode: "historical", Target: r.Target, Generation: r.Generation, ProjectionRevision: r.ProjectionRevision, SourceStream: r.SourceStream, RawDigest: event.RawDigest, Raw: event.Raw, OriginalTimestampSeconds: strconv.FormatInt(event.Timestamp, 10), EventKind: event.Kind, Event: event.Payload}
		expected, err := canonicalReplayPayload(wrapper)
		if err != nil {
			return err
		}
		var payloadHash, subject string
		var wire []byte
		if err = h.Pool.QueryRow(ctx, "SELECT payload_hash,subject,envelope FROM "+table+" WHERE event_id=$1", effect).Scan(&payloadHash, &subject, &wire); err != nil {
			return err
		}
		envelope, err := delivery.Decode(wire, policy, ReplaySubjectV2)
		if err != nil {
			return err
		}
		if subject != ReplaySubjectV2 || payloadHash != delivery.Hash(expected) || envelope.EventID != effect || envelope.PayloadHash != payloadHash || !bytes.Equal(envelope.Payload, expected) || envelope.SourceOrder.Height != strconv.FormatInt(event.Raw.Height, 10) || envelope.SourceOrder.TransactionIndex != strconv.FormatUint(uint64(event.Raw.TransactionIndex), 10) || envelope.SourceOrder.LogIndex != strconv.FormatUint(uint64(event.Raw.LogIndex), 10) || envelope.EmittedAt.Unix() != event.Timestamp {
			return store.ErrIntegrity
		}
	}
	return nil
}

type ingestHeadObservation struct {
	Height     int64
	ObservedAt time.Time
}

func (h *IngestMeasurementHarness) observeCommittedHeads(ctx context.Context, initial int64) (<-chan ingestHeadObservation, <-chan error, <-chan struct{}) {
	updates := make(chan ingestHeadObservation, 4096)
	failures := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		last := initial
		for {
			if err := ctx.Err(); err != nil {
				return
			}
			readCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			height, err := h.Processor.deps.Head.Head(readCtx)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					failures <- err
				}
				return
			}
			if height < last {
				failures <- store.ErrIntegrity
				return
			}
			if height > last {
				observation := ingestHeadObservation{Height: height, ObservedAt: time.Now().UTC()}
				select {
				case updates <- observation:
					last = height
				default:
					failures <- errors.New("independent head observation queue saturated; no samples dropped")
					return
				}
			}
			if !sleep(ctx, h.Processor.deps.PollInterval) {
				return
			}
		}
	}()
	return updates, failures, done
}

func validateCandidateFiles(files map[string]string, candidateDigest string) error {
	if len(files) == 0 || len(files) > 512 {
		return errors.New("bounded actual candidate source set required")
	}
	keys := []string{}
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidate := sha256.New()
	for _, path := range keys {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 64<<20 {
			return errors.New("bounded actual source file required")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		h := sha256.New()
		size, readErr := io.Copy(h, io.LimitReader(f, 64<<20+1))
		closeErr := f.Close()
		if size > 64<<20 {
			return errors.New("source file exceeded admitted bound while reading")
		}
		if readErr != nil || closeErr != nil {
			return errors.Join(readErr, closeErr)
		}
		digest := hex.EncodeToString(h.Sum(nil))
		if digest != files[path] {
			return fmt.Errorf("candidate source digest changed: %s", path)
		}
		fmt.Fprintf(candidate, "%d:%s:%s\n", len(path), path, digest)
	}
	if hex.EncodeToString(candidate.Sum(nil)) != candidateDigest {
		return errors.New("candidate digest does not bind actual measurement sources")
	}
	return nil
}
