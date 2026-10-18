package storage

import (
	"context"
	"errors"
	sharedhttp "github.com/Sidiora-Technologies/KindleLaunch/shared/http"
	"sort"
	"sync"
	"time"
)

type MediaHealth struct {
	ScannerUnavailableSince time.Time `json:"scannerUnavailableSince,omitempty"`
	ScanAlert               bool      `json:"scanAlert"`
	PurgeBacklogAgeSeconds  float64   `json:"purgeBacklogAgeSeconds"`
	PurgeAlert              bool      `json:"purgeAlert"`
	QuarantineOldestSeconds float64   `json:"quarantineOldestSeconds"`
	WriteReady              bool      `json:"writeReady"`
	ReadReady               bool      `json:"readReady"`
}
type MediaObserver struct {
	mu             sync.Mutex
	scannerFailure time.Time
	outcomes       map[string]uint64
	scanCount      uint64
	scanTotal      time.Duration
	originReads    uint64
	cacheReads     uint64
}

func NewMediaObserver() *MediaObserver {
	return &MediaObserver{outcomes: map[string]uint64{}, scannerFailure: time.Now().UTC()}
}
func (o *MediaObserver) ScannerResult(available bool, elapsed time.Duration, now time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if available {
		o.scannerFailure = time.Time{}
	} else if o.scannerFailure.IsZero() {
		o.scannerFailure = now
	}
	if elapsed >= 0 && elapsed <= 30*time.Second {
		o.scanCount++
		o.scanTotal += elapsed
	}
}
func (o *MediaObserver) RecordUpload(outcome string) error {
	switch outcome {
	case "approved", "quarantined", "rejected", "rate_limited", "unauthenticated", "unavailable":
	default:
		return ErrConflict
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.outcomes[outcome]++
	return nil
}
func (o *MediaObserver) RecordRead(origin bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if origin {
		o.originReads++
	} else {
		o.cacheReads++
	}
}
func (o *MediaObserver) Snapshot() map[string]any {
	o.mu.Lock()
	defer o.mu.Unlock()
	outcomes := map[string]uint64{}
	for key, value := range o.outcomes {
		outcomes[key] = value
	}
	ratio := float64(0)
	if total := o.originReads + o.cacheReads; total > 0 {
		ratio = float64(o.originReads) / float64(total)
	}
	return map[string]any{"uploadOutcomes": outcomes, "scanCount": o.scanCount, "scanTotalSeconds": o.scanTotal.Seconds(), "originHitRatio": ratio}
}
func (o *MediaObserver) Health(ctx context.Context, a *Authority, now time.Time, uploadsEnabled bool) (MediaHealth, error) {
	o.mu.Lock()
	failure := o.scannerFailure
	o.mu.Unlock()
	h := MediaHealth{ScannerUnavailableSince: failure, ScanAlert: uploadsEnabled && !failure.IsZero() && now.Sub(failure) >= 60*time.Second}
	if a == nil {
		return h, ErrUnavailable
	}
	if e := a.Pool.Ping(ctx); e != nil {
		return h, ErrUnavailable
	}
	h.ReadReady = true
	h.WriteReady = !uploadsEnabled || failure.IsZero()
	var seconds float64
	if e := a.Pool.QueryRow(ctx, "SELECT COALESCE(EXTRACT(EPOCH FROM (clock_timestamp()-min(requested_at))),0)::float8 FROM "+a.table("operations")+" WHERE kind='purge' AND state<>'complete'").Scan(&seconds); e != nil {
		return h, ErrUnavailable
	}
	h.PurgeBacklogAgeSeconds = seconds
	h.PurgeAlert = seconds > 30
	if e := a.Pool.QueryRow(ctx, "SELECT COALESCE(EXTRACT(EPOCH FROM (clock_timestamp()-min(d.created_at))),0)::float8 FROM "+a.metadata("assets")+" asset JOIN "+a.metadata("asset_details_v2")+" d ON d.asset_id=asset.id WHERE asset.state IN ('QUARANTINED','VALIDATING')").Scan(&h.QuarantineOldestSeconds); e != nil {
		return h, ErrUnavailable
	}
	return h, nil
}

func (o *MediaObserver) ProbeScanner(ctx context.Context, options sharedhttp.ScanOptions, now time.Time) error {
	bounded, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	started := time.Now()
	_, e := sharedhttp.ScannerVersion(bounded, options)
	o.ScannerResult(e == nil, time.Since(started), now)
	if e != nil {
		return ErrUnavailable
	}
	return nil
}

type EnvelopeDistribution struct {
	Count           uint64         `json:"count"`
	Succeeded       uint64         `json:"succeeded"`
	Failed          uint64         `json:"failed"`
	Bytes           uint64         `json:"bytes"`
	P95UpperBoundMS int64          `json:"p95UpperBoundMs"`
	P99UpperBoundMS int64          `json:"p99UpperBoundMs"`
	Statuses        map[int]uint64 `json:"statuses"`
}

type EnvelopeAccumulator struct {
	mu      sync.Mutex
	classes map[string]*envelopeHistogram
}

type envelopeHistogram struct {
	count, succeeded, failed, bytes uint64
	buckets                         map[int64]uint64
	statuses                        map[int]uint64
}

func NewEnvelopeAccumulator() *EnvelopeAccumulator {
	return &EnvelopeAccumulator{classes: map[string]*envelopeHistogram{}}
}

func (a *EnvelopeAccumulator) Observe(class string, latency time.Duration, status int, size int64, accepted bool) error {
	switch class {
	case "origin", "metadata", "upload", "cdn":
	default:
		return errors.New("unknown media measurement class")
	}
	if latency < 0 || latency > time.Minute || size < 0 || size > 64<<20 || status < 0 || status > 599 || (accepted && (status < 200 || status >= 300)) {
		return ErrConflict
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	h := a.classes[class]
	if h == nil {
		h = &envelopeHistogram{buckets: map[int64]uint64{}, statuses: map[int]uint64{}}
		a.classes[class] = h
	}
	h.count++
	h.bytes += uint64(size)
	if accepted {
		h.succeeded++
	} else {
		h.failed++
	}
	bucket := int64((latency + time.Millisecond - 1) / time.Millisecond)
	h.buckets[bucket]++
	h.statuses[status]++
	return nil
}

func (a *EnvelopeAccumulator) SnapshotDistributions() map[string]EnvelopeDistribution {
	a.mu.Lock()
	defer a.mu.Unlock()
	result := map[string]EnvelopeDistribution{}
	for name, h := range a.classes {
		statuses := map[int]uint64{}
		for code, count := range h.statuses {
			statuses[code] = count
		}
		result[name] = EnvelopeDistribution{Count: h.count, Succeeded: h.succeeded, Failed: h.failed, Bytes: h.bytes, P95UpperBoundMS: envelopeQuantile(h.buckets, h.count, 95), P99UpperBoundMS: envelopeQuantile(h.buckets, h.count, 99), Statuses: statuses}
	}
	return result
}

func envelopeQuantile(buckets map[int64]uint64, count uint64, percentile uint64) int64 {
	if count == 0 {
		return -1
	}
	keys := make([]int64, 0, len(buckets))
	for key := range buckets {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	rank := (count*percentile + 99) / 100
	var seen uint64
	for _, key := range keys {
		seen += buckets[key]
		if seen >= rank {
			return key
		}
	}
	return -1
}
