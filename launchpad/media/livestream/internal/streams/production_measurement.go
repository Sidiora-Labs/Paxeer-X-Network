package streams

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	sharedauth "github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
)

type MeasurementArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type MediaMeasurementConfig struct {
	Version                int                 `json:"version"`
	RunID                  string              `json:"runId"`
	Candidate              string              `json:"candidate"`
	Interface              string              `json:"interface"`
	Topology               string              `json:"topology"`
	Generator              string              `json:"generator"`
	SourceRoot             string              `json:"sourceRoot"`
	SourceFiles            map[string]string   `json:"sourceFiles"`
	Authority              MeasurementArtifact `json:"authority"`
	OutputRoot             string              `json:"outputRoot"`
	Origin                 string              `json:"origin"`
	StreamID               string              `json:"streamId"`
	ProviderURL            string              `json:"providerUrl"`
	ProviderKeyEnvironment string              `json:"providerKeyEnvironment"`
	Pool                   string              `json:"pool"`
	Region                 string              `json:"region"`
	DurationSeconds        int                 `json:"durationSeconds"`
	OriginRPS              int                 `json:"originRps"`
	HeartbeatRPS           int                 `json:"heartbeatRps"`
	MaxFlights             int                 `json:"maxFlights"`
	ReadWeights            map[string]int      `json:"readWeights"`
	PlaybackSessions       int                 `json:"playbackSessions"`
	SegmentIntervalSeconds int                 `json:"segmentIntervalSeconds"`
	SegmentURLs            []string            `json:"segmentUrls"`
	Quota                  MeasurementArtifact `json:"quota"`
	License                MeasurementArtifact `json:"license"`
	Budget                 MeasurementArtifact `json:"budget"`
	NetworkProfile         MeasurementArtifact `json:"networkProfile"`
}
type MediaMeasurementAuthority struct {
	RunID             string    `json:"runId"`
	Candidate         string    `json:"candidate"`
	Interface         string    `json:"interface"`
	Topology          string    `json:"topology"`
	Generator         string    `json:"generator"`
	WorkloadVersion   string    `json:"workloadVersion"`
	RootMixSHA256     string    `json:"rootMixSha256"`
	RootLedgerSHA256  string    `json:"rootLedgerSha256"`
	Lane              string    `json:"lane"`
	LaneRPS           int       `json:"laneRps"`
	ApprovalReference string    `json:"approvalReference"`
	ExpiresAt         time.Time `json:"expiresAt"`
}
type MediaQuotaAuthority struct {
	AccountID              string    `json:"accountId"`
	ConcurrentViewers      int       `json:"concurrentViewers"`
	ConcurrentBroadcasters int       `json:"concurrentBroadcasters"`
	ApprovalReference      string    `json:"approvalReference"`
	ExpiresAt              time.Time `json:"expiresAt"`
}
type MediaLicenseAuthority struct {
	ContentID       string            `json:"contentId"`
	StreamID        string            `json:"streamId"`
	ProviderID      string            `json:"providerId"`
	PlaybackID      string            `json:"playbackId"`
	RightsReference string            `json:"rightsReference"`
	Regions         []string          `json:"regions"`
	SegmentURLs     []string          `json:"segmentUrls"`
	SegmentHashes   map[string]string `json:"segmentHashes"`
	ValidUntil      time.Time         `json:"validUntil"`
}
type MediaNetworkProfile struct {
	Region                  string `json:"region"`
	AuthorityReference      string `json:"authorityReference"`
	DownstreamBitsPerSecond uint64 `json:"downstreamBitsPerSecond"`
	LatencyMS               int    `json:"latencyMs"`
}
type MediaRequestSample struct {
	RunID             string `json:"run_id"`
	RequestID         string `json:"request_id"`
	Category          string `json:"category"`
	SessionID         string `json:"session_id,omitempty"`
	ScheduledUTCNS    string `json:"scheduled_utc_ns"`
	StartedUTCNS      string `json:"started_utc_ns"`
	EndedUTCNS        string `json:"ended_utc_ns"`
	LatencyNS         int64  `json:"latency_ns"`
	SchedulingDelayNS int64  `json:"scheduling_delay_ns"`
	Status            int    `json:"status"`
	Bytes             int64  `json:"bytes"`
	BodySHA256        string `json:"body_sha256"`
	Reason            string `json:"reason"`
}
type MediaMeasurementSummary struct {
	RunID             string            `json:"run_id"`
	Candidate         string            `json:"candidate"`
	Interface         string            `json:"interface"`
	Topology          string            `json:"topology"`
	Generator         string            `json:"generator"`
	Scope             string            `json:"scope"`
	FinalGate         string            `json:"final_gate"`
	CapacityQualified bool              `json:"capacity_qualified"`
	Requests          int               `json:"requests"`
	Failures          int               `json:"failures"`
	OriginP95NS       int64             `json:"origin_p95_ns"`
	OriginP99NS       int64             `json:"origin_p99_ns"`
	PlaybackSessions  int               `json:"playback_sessions"`
	CDNBytes          int64             `json:"cdn_payload_bytes"`
	AppVideoBytes     *int64            `json:"application_video_payload_bytes"`
	Spend             SpendEstimate     `json:"modeled_one_hour_20k_spend"`
	Artifacts         map[string]string `json:"artifact_sha256"`
	Blockers          []string          `json:"blockers"`
}

func measurementSHA(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func measurementHashValid(h string) bool {
	b, e := hex.DecodeString(h)
	return e == nil && len(b) == 32 && strings.ToLower(h) == h
}
func measurementRead(a MeasurementArtifact, out any) error {
	if !measurementHashValid(a.SHA256) || !filepath.IsAbs(a.Path) {
		return errors.New("measurement artifact authority missing")
	}
	f, e := os.Open(a.Path)
	if e != nil {
		return errors.New("measurement artifact unavailable")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 1048577))
	if e != nil || len(raw) > 1048576 || measurementSHA(raw) != a.SHA256 {
		return errors.New("measurement artifact digest mismatch")
	}
	if _, e = sharedauth.BodyDigest(raw); e != nil {
		return errors.New("measurement artifact ambiguous JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("measurement artifact schema mismatch")
	}
	return nil
}
func LoadMediaMeasurement(path, sha string) (MediaMeasurementConfig, error) {
	var c MediaMeasurementConfig
	e := measurementRead(MeasurementArtifact{path, sha}, &c)
	return c, e
}
func (c MediaMeasurementConfig) validate() (MediaLicenseAuthority, BudgetPolicy, error) {
	var license MediaLicenseAuthority
	var budget BudgetPolicy
	if c.Version != 1 || !identityPattern.MatchString(c.RunID) || !frozenUUID.MatchString(c.StreamID) || !addressPattern.MatchString(c.Pool) || !identityPattern.MatchString(c.Region) || c.DurationSeconds < 600 || c.DurationSeconds > 1800 || c.DurationSeconds%30 != 0 || c.OriginRPS < 1 || c.OriginRPS > 1000 || c.HeartbeatRPS != 667 || c.OriginRPS+c.HeartbeatRPS > 1000 || c.MaxFlights < 1 || c.MaxFlights > 1000 || c.PlaybackSessions != 20000 || c.SegmentIntervalSeconds < 1 || c.SegmentIntervalSeconds > 30 || c.DurationSeconds%c.SegmentIntervalSeconds != 0 || len(c.SegmentURLs) < 1 || len(c.SegmentURLs) > 100 || !filepath.IsAbs(c.OutputRoot) || !filepath.IsAbs(c.SourceRoot) || len(c.SourceFiles) < 3 {
		return license, budget, errors.New("measurement configuration incomplete")
	}
	origin, e := url.Parse(c.Origin)
	if e != nil || origin.Scheme != "https" || origin.Host == "" || origin.User != nil || origin.RawQuery != "" || origin.Fragment != "" || strings.HasSuffix(c.Origin, "/") {
		return license, budget, errors.New("exact TLS origin required")
	}
	provider, e := url.Parse(c.ProviderURL)
	if e != nil || provider.Scheme != "https" || provider.Host != "livepeer.studio" || provider.Path != "/api" || provider.RawQuery != "" || provider.Fragment != "" || provider.User != nil || !identityPattern.MatchString(c.ProviderKeyEnvironment) {
		return license, budget, errors.New("actual provider account binding required")
	}
	if len(c.ReadWeights) != 3 || c.ReadWeights["live"] < 1 || c.ReadWeights["pool"] < 1 || c.ReadWeights["detail"] < 1 {
		return license, budget, errors.New("all actual origin read categories required")
	}
	weightTotal := 0
	for _, weight := range c.ReadWeights {
		if weight < 1 || weight > 10000 {
			return license, budget, errors.New("read weight bound exceeded")
		}
		weightTotal += weight
	}
	if weightTotal > 10000 {
		return license, budget, errors.New("read weight total exceeded")
	}
	for _, hash := range []string{c.Candidate, c.Interface, c.Topology, c.Generator} {
		if !measurementHashValid(hash) {
			return license, budget, errors.New("candidate interface topology generator bindings required")
		}
	}
	for _, path := range []string{"media/livestream/internal/streams/production_measurement.go", "media/livestream/internal/streams/lifecycle.go", "media/livestream/internal/streams/credentials.go", "media/livestream/internal/streams/provision_worker.go", "media/livestream/internal/streams/reconcile_worker.go", "media/livestream/internal/streams/handlers.go", "media/livestream/internal/livepeer/client.go", "media/livestream/internal/config/config.go", "client/apps/web/src/widgets/trading/live-stream.tsx", "client/apps/web/src/core/clients/livestream-api.ts"} {
		if !measurementHashValid(c.SourceFiles[path]) {
			return license, budget, errors.New("complete runtime/harness source inventory required")
		}
	}
	for path, hash := range c.SourceFiles {
		if filepath.IsAbs(path) || filepath.Clean(path) != path || strings.HasPrefix(path, "..") || !measurementHashValid(hash) {
			return license, budget, errors.New("invalid source binding")
		}
		raw, err := os.ReadFile(filepath.Join(c.SourceRoot, path))
		if err != nil || measurementSHA(raw) != hash {
			return license, budget, errors.New("current source binding mismatch")
		}
	}
	var authority MediaMeasurementAuthority
	if e = measurementRead(c.Authority, &authority); e != nil {
		return license, budget, e
	}
	if authority.RunID != c.RunID || authority.Candidate != c.Candidate || authority.Interface != c.Interface || authority.Topology != c.Topology || authority.Generator != c.Generator || authority.WorkloadVersion != "kindle-500k-dau-v1" || authority.Lane != "L40" || authority.LaneRPS != c.OriginRPS+c.HeartbeatRPS || !measurementHashValid(authority.RootMixSHA256) || !measurementHashValid(authority.RootLedgerSHA256) || authority.ApprovalReference == "" || !authority.ExpiresAt.After(time.Now().Add(time.Duration(c.DurationSeconds)*time.Second)) {
		return license, budget, errors.New("assigned mixed-workload authority mismatch")
	}
	var quota MediaQuotaAuthority
	if e = measurementRead(c.Quota, &quota); e != nil {
		return license, budget, e
	}
	if quota.AccountID == "" || quota.ConcurrentViewers < 20000 || quota.ConcurrentBroadcasters < 1 || quota.ApprovalReference == "" || !quota.ExpiresAt.After(time.Now().Add(time.Duration(c.DurationSeconds)*time.Second)) {
		return license, budget, errors.New("actual account quota authority insufficient")
	}
	if e = measurementRead(c.License, &license); e != nil {
		return license, budget, e
	}
	if license.ContentID == "" || license.StreamID != c.StreamID || !identityPattern.MatchString(license.ProviderID) || !identityPattern.MatchString(license.PlaybackID) || license.RightsReference == "" || !license.ValidUntil.After(time.Now().Add(time.Duration(c.DurationSeconds)*time.Second)) || len(license.SegmentURLs) != len(c.SegmentURLs) {
		return license, budget, errors.New("licensed content authority mismatch")
	}
	region := false
	for _, r := range license.Regions {
		if r == c.Region {
			region = true
		}
	}
	if !region {
		return license, budget, errors.New("licensed region missing")
	}
	for i, raw := range c.SegmentURLs {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host != "livepeercdn.studio" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/hls/"+license.PlaybackID+"/") || raw != license.SegmentURLs[i] || !measurementHashValid(license.SegmentHashes[raw]) {
			return license, budget, errors.New("licensed CDN segment binding invalid")
		}
	}
	if e = measurementRead(c.Budget, &budget); e != nil {
		return license, budget, e
	}
	if e = budget.ValidateVendor(); e != nil || budget.VendorUserID != quota.AccountID || budget.PeakDurationSeconds < uint64(c.DurationSeconds) {
		return license, budget, errors.New("approved account spend authority unavailable")
	}
	var profile MediaNetworkProfile
	if e = measurementRead(c.NetworkProfile, &profile); e != nil {
		return license, budget, e
	}
	if profile.Region != c.Region || profile.AuthorityReference == "" || profile.DownstreamBitsPerSecond < 40000000000 || profile.LatencyMS < 0 {
		return license, budget, errors.New("actual generator network profile insufficient")
	}
	return license, budget, nil
}

type mediaCapture struct {
	mu                 sync.Mutex
	file               *os.File
	encoder            *json.Encoder
	latencies          []int64
	requests, failures int
	cdnBytes           int64
	failed             error
}

func (w *mediaCapture) append(v MediaRequestSample) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e := w.encoder.Encode(v); e != nil && w.failed == nil {
		w.failed = e
	}
	w.requests++
	if v.Reason != "ok" {
		w.failures++
	}
	if v.Category == "cdn_segment" {
		w.cdnBytes += v.Bytes
	}
	if v.Category == "live" || v.Category == "pool" || v.Category == "detail" {
		w.latencies = append(w.latencies, v.LatencyNS)
	}
}
func mediaFetch(ctx context.Context, client *http.Client, run, category, target, method, session string, body []byte, scheduled time.Time, headers map[string]string) (MediaRequestSample, []byte) {
	started := time.Now()
	id, _ := randomID()
	v := MediaRequestSample{RunID: run, RequestID: id, Category: category, SessionID: session, ScheduledUTCNS: fmt.Sprint(scheduled.UnixNano()), StartedUTCNS: fmt.Sprint(started.UnixNano()), SchedulingDelayNS: started.Sub(scheduled).Nanoseconds(), Reason: "ok"}
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(bounded, method, target, bytes.NewReader(body))
	var raw []byte
	if e == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-ID", id)
		for k, value := range headers {
			req.Header.Set(k, value)
		}
		resp, err := client.Do(req)
		e = err
		if err == nil {
			v.Status = resp.StatusCode
			limit := int64(1048576)
			if category == "cdn_segment" {
				limit = 16 << 20
			}
			if category == "cdn_segment" {
				sum := sha256.New()
				v.Bytes, e = io.Copy(sum, io.LimitReader(resp.Body, limit+1))
				v.BodySHA256 = hex.EncodeToString(sum.Sum(nil))
			} else {
				raw, e = io.ReadAll(io.LimitReader(resp.Body, limit+1))
				v.Bytes = int64(len(raw))
			}
			resp.Body.Close()
			if v.Bytes > limit {
				e = errors.New("response_limit")
			}
			mime := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
			if category != "cdn_segment" && (mime != "application/json" || !json.Valid(raw)) {
				e = errors.New("origin_content_type")
			}
			if category != "cdn_segment" && (strings.Contains(string(raw), "streamKey") || strings.Contains(string(raw), "rtmp://") || strings.Contains(string(raw), "rtmps://")) {
				e = errors.New("public_credential_leak")
			}
		}
	}
	if e != nil {
		v.Reason = "transport_or_payload_failure"
	}
	if v.Status < 200 || v.Status >= 300 {
		v.Reason = "http_failure"
	}
	if category != "cdn_segment" {
		v.BodySHA256 = measurementSHA(raw)
	}
	v.EndedUTCNS = fmt.Sprint(time.Now().UnixNano())
	v.LatencyNS = time.Since(scheduled).Nanoseconds()
	return v, raw
}
func mediaPercentile(samples []int64, q float64) int64 {
	if len(samples) == 0 {
		return 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[int(math.Ceil(float64(len(samples))*q))-1]
}

func RunMediaMeasurement(ctx context.Context, c MediaMeasurementConfig) (MediaMeasurementSummary, error) {
	summary := MediaMeasurementSummary{RunID: c.RunID, Candidate: c.Candidate, Interface: c.Interface, Topology: c.Topology, Generator: c.Generator, Scope: "isolated_component", FinalGate: "9.1", Artifacts: map[string]string{}, Blockers: []string{}}
	license, budget, e := c.validate()
	if e != nil {
		return summary, e
	}
	for name, artifact := range map[string]MeasurementArtifact{"workload_authority": c.Authority, "quota_authority": c.Quota, "content_license": c.License, "cost_budget": c.Budget, "network_profile": c.NetworkProfile} {
		summary.Artifacts[name] = artifact.SHA256
	}
	providerKey, present := os.LookupEnv(c.ProviderKeyEnvironment)
	if !present || strings.TrimSpace(providerKey) == "" {
		return summary, errors.New("actual provider credential unavailable")
	}
	provider := livepeer.New(providerKey, c.ProviderURL, nil)
	now := time.Now().UTC()
	period := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	before, e := provider.GetUsage(ctx, period, now)
	if e != nil || before.UserID != budget.VendorUserID {
		return summary, errors.New("actual provider account usage unavailable")
	}
	remote, e := provider.GetStream(ctx, license.ProviderID)
	if e != nil || remote.PlaybackID != license.PlaybackID || !remote.IsActive || remote.Name != livepeer.CorrelationName(c.StreamID) || remote.PlaybackPolicy == nil || remote.PlaybackPolicy.Type != "webhook" || remote.PlaybackPolicy.Context["operationId"] != c.StreamID || remote.PlaybackPolicy.RefreshInterval != 1 {
		return summary, errors.New("actual licensed provider stream identity mismatch")
	}
	profiles := map[string]int{}
	for _, profile := range remote.Profiles {
		if _, present := profiles[profile.Name]; present {
			return summary, errors.New("provider rendition identity duplicated")
		}
		profiles[profile.Name] = profile.Bitrate
	}
	if len(profiles) != 3 || profiles["720p"] != 2000000 || profiles["480p"] != 1000000 || profiles["360p"] != 500000 {
		return summary, errors.New("actual provider rendition ladder differs from required source profiles")
	}
	runRoot := filepath.Join(c.OutputRoot, c.RunID)
	if e = os.Mkdir(runRoot, 0700); e != nil {
		return summary, errors.New("exclusive measurement output directory required")
	}
	file, e := os.OpenFile(filepath.Join(runRoot, "http-arrivals.ndjson"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return summary, e
	}
	capture := &mediaCapture{file: file, encoder: json.NewEncoder(file)}
	defer file.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = c.MaxFlights
	transport.MaxIdleConnsPerHost = c.MaxFlights
	transport.MaxConnsPerHost = c.MaxFlights
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	type session struct{ ID, Token string }
	sessions := make([]session, c.PlaybackSessions)
	setupCtx, setupCancel := context.WithTimeout(ctx, 5*time.Minute)
	defer setupCancel()
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var setupErr error
	for worker := 0; worker < c.MaxFlights; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				v, raw := mediaFetch(setupCtx, client, c.RunID, "viewer_session", c.Origin+"/v2/streams/"+c.StreamID+"/viewer-session", "POST", "", []byte("{}"), time.Now(), nil)
				capture.append(v)
				var result struct {
					StreamID  string    `json:"streamId"`
					SessionID string    `json:"sessionId"`
					Token     string    `json:"token"`
					ExpiresAt time.Time `json:"expiresAt"`
				}
				if v.Reason != "ok" || json.Unmarshal(raw, &result) != nil || result.StreamID != c.StreamID || !frozenUUID.MatchString(result.SessionID) || len(result.Token) < 1 || len(result.Token) > 512 || !result.ExpiresAt.After(time.Now().Add(time.Duration(c.DurationSeconds)*time.Second)) {
					mu.Lock()
					setupErr = errors.New("actual playback session unavailable or expires within run")
					mu.Unlock()
					continue
				}
				sessions[i] = session{result.SessionID, result.Token}
			}
		}()
	}
	for i := range sessions {
		select {
		case jobs <- i:
		case <-setupCtx.Done():
			close(jobs)
			wg.Wait()
			return summary, setupCtx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if setupErr != nil {
		return summary, setupErr
	}
	seen := map[string]bool{}
	for _, s := range sessions {
		if seen[s.ID] {
			return summary, errors.New("duplicate actual viewer session")
		}
		seen[s.ID] = true
	}
	summary.PlaybackSessions = len(sessions)
	start := time.Now()
	duration := time.Duration(c.DurationSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, duration+15*time.Second)
	defer cancel()
	type request struct {
		category, target, session, method string
		body                              []byte
		scheduled                         time.Time
		headers                           map[string]string
	}
	queue := make(chan request, c.MaxFlights)
	for worker := 0; worker < c.MaxFlights; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range queue {
				v, _ := mediaFetch(runCtx, client, c.RunID, job.category, job.target, job.method, job.session, job.body, job.scheduled, job.headers)
				if job.category == "cdn_segment" && v.Reason == "ok" && v.BodySHA256 != license.SegmentHashes[job.target] {
					v.Reason = "licensed_segment_digest_mismatch"
				}
				capture.append(v)
			}
		}()
	}
	categories := []string{}
	for _, category := range []string{"live", "pool", "detail"} {
		for n := 0; n < c.ReadWeights[category]; n++ {
			categories = append(categories, category)
		}
	}
	var schedWG sync.WaitGroup
	schedWG.Add(3)
	dispatch := func(job request) {
		select {
		case queue <- job:
		default:
			id, _ := randomID()
			capture.append(MediaRequestSample{RunID: c.RunID, RequestID: id, Category: job.category, SessionID: job.session, ScheduledUTCNS: fmt.Sprint(job.scheduled.UnixNano()), StartedUTCNS: fmt.Sprint(time.Now().UnixNano()), EndedUTCNS: fmt.Sprint(time.Now().UnixNano()), LatencyNS: time.Since(job.scheduled).Nanoseconds(), Reason: "generator_queue_full"})
		}
	}
	go func() {
		defer schedWG.Done()
		for n := 0; n < c.OriginRPS*c.DurationSeconds; n++ {
			due := start.Add(time.Duration(n) * time.Second / time.Duration(c.OriginRPS))
			timer := time.NewTimer(time.Until(due))
			select {
			case <-timer.C:
			case <-runCtx.Done():
				timer.Stop()
				return
			}
			category := categories[n%len(categories)]
			path := "/v2/streams/live"
			if category == "pool" {
				path = "/v2/streams/pool/" + c.Pool
			}
			if category == "detail" {
				path = "/v2/streams/" + c.StreamID
			}
			dispatch(request{category: category, target: c.Origin + path, method: "GET", scheduled: due})
		}
	}()
	go func() {
		defer schedWG.Done()
		for round := 0; round < c.DurationSeconds/c.SegmentIntervalSeconds; round++ {
			due := start.Add(time.Duration(round*c.SegmentIntervalSeconds) * time.Second)
			timer := time.NewTimer(time.Until(due))
			select {
			case <-timer.C:
			case <-runCtx.Done():
				timer.Stop()
				return
			}
			for i, s := range sessions {
				arrival := due.Add(time.Duration(i) * time.Duration(c.SegmentIntervalSeconds) * time.Second / time.Duration(len(sessions)))
				wait := time.NewTimer(time.Until(arrival))
				select {
				case <-wait.C:
				case <-runCtx.Done():
					wait.Stop()
					return
				}
				dispatch(request{category: "cdn_segment", target: c.SegmentURLs[(round+i)%len(c.SegmentURLs)], method: "GET", session: s.ID, scheduled: arrival, headers: map[string]string{"Livepeer-Access-Key": s.Token}})
			}
		}
	}()
	go func() {
		defer schedWG.Done()
		for round := 0; round < c.DurationSeconds/30; round++ {
			for i, s := range sessions {
				due := start.Add(time.Duration(round*30)*time.Second + time.Duration(i)*30*time.Second/time.Duration(len(sessions)))
				timer := time.NewTimer(time.Until(due))
				select {
				case <-timer.C:
				case <-runCtx.Done():
					timer.Stop()
					return
				}
				body, _ := json.Marshal(map[string]string{"sessionId": s.ID, "token": s.Token})
				dispatch(request{category: "viewer_heartbeat", target: c.Origin + "/v2/streams/" + c.StreamID + "/heartbeat", method: "POST", session: s.ID, body: body, scheduled: due})
			}
		}
	}()
	schedWG.Wait()
	close(queue)
	wg.Wait()
	if e = file.Sync(); e != nil {
		return summary, e
	}
	if capture.failed != nil {
		return summary, capture.failed
	}
	latencies := capture.latencies
	summary.Requests = capture.requests
	summary.Failures = capture.failures
	summary.CDNBytes = capture.cdnBytes
	summary.OriginP95NS = mediaPercentile(latencies, .95)
	summary.OriginP99NS = mediaPercentile(latencies, .99)
	summary.Spend, e = EstimateSpend(SpendScenario{Viewers: 20000, BitrateBitsPerSecond: 2000000, DurationSeconds: 3600, DeliveryPricePerTB: budget.DeliveryPricePerTB})
	if e != nil {
		return summary, e
	}
	if summary.Failures > 0 {
		summary.Blockers = append(summary.Blockers, "actual request or generator failures")
	}
	if summary.OriginP95NS > 150000000 || summary.OriginP99NS > 400000000 || len(latencies) != c.OriginRPS*c.DurationSeconds {
		summary.Blockers = append(summary.Blockers, "origin latency or scheduled arrival bound failed")
	}
	rawFile, e := os.Open(filepath.Join(runRoot, "http-arrivals.ndjson"))
	if e != nil {
		return summary, e
	}
	rawHash := sha256.New()
	_, e = io.Copy(rawHash, rawFile)
	rawFile.Close()
	if e != nil {
		return summary, e
	}
	summary.Artifacts["http-arrivals.ndjson"] = hex.EncodeToString(rawHash.Sum(nil))
	after, e := provider.GetUsage(ctx, period, time.Now().UTC())
	if e != nil || after.UserID != budget.VendorUserID {
		return summary, errors.New("actual provider usage after run unavailable")
	}
	cost := new(big.Rat)
	for i, minutes := range []string{after.DeliveryMinutes.String(), after.TranscodeMinutes.String(), after.StorageMinutes.String()} {
		unit, ok := new(big.Rat).SetString(minutes)
		price, priceOK := new(big.Rat).SetString([]string{budget.DeliveryPricePerMinute, budget.TranscodePricePerMinute, budget.StoragePricePerMinute}[i])
		if !ok || !priceOK || unit.Sign() < 0 || price.Sign() < 0 {
			return summary, errors.New("actual vendor units unavailable")
		}
		cost.Add(cost, new(big.Rat).Mul(unit, price))
	}
	limit, _ := new(big.Rat).SetString(budget.MonthlyUnits)
	if cost.Cmp(limit) > 0 {
		summary.Blockers = append(summary.Blockers, "actual monthly vendor spend exceeded approved units")
	}
	thresholds, e := BudgetThresholds(cost.RatString(), budget.MonthlyUnits)
	if e != nil {
		return summary, e
	}
	vendorRaw, e := json.Marshal(map[string]any{"run_id": c.RunID, "account_id": budget.VendorUserID, "before": before, "after": after, "provider_renditions": profiles, "configured_monthly_cost_units": cost.FloatString(18), "exact_monthly_cost_numerator": cost.Num().String(), "exact_monthly_cost_denominator": cost.Denom().String(), "budget_thresholds": thresholds, "currency": budget.Currency, "approval_reference": budget.ApprovalReference, "storage_bytes": nil, "delivery_bytes": nil, "units_source_revision": livepeer.UsageSDKSourceRevision})
	if e != nil {
		return summary, e
	}
	if e = os.WriteFile(filepath.Join(runRoot, "provider-usage.json"), vendorRaw, 0600); e != nil {
		return summary, e
	}
	summary.Artifacts["provider-usage.json"] = measurementSHA(vendorRaw)
	summary.Blockers = append(summary.Blockers, "browser startup/rebuffer/accessibility and whole-topology application network inventory require serial capture")
	output, e := json.MarshalIndent(summary, "", "  ")
	if e != nil {
		return summary, e
	}
	if e = os.WriteFile(filepath.Join(runRoot, "summary.json"), output, 0600); e != nil {
		return summary, e
	}
	if summary.Failures > 0 || summary.OriginP95NS > 150000000 || summary.OriginP99NS > 400000000 || len(latencies) != c.OriginRPS*c.DurationSeconds || summary.CDNBytes <= 0 || cost.Cmp(limit) > 0 {
		return summary, errors.New("measured media envelope failed")
	}
	return summary, nil
}
