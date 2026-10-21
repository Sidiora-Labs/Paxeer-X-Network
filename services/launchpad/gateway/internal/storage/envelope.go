package storage

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
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EnvelopeFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type EnvelopeAllocation struct {
	GlobalDAU         int    `json:"globalDau"`
	GlobalPeakRPS     int    `json:"globalPeakRps"`
	Lane              string `json:"lane"`
	AssignedRPS       int    `json:"assignedRps"`
	OriginRPS         int    `json:"originRps"`
	MetadataRPS       int    `json:"metadataRps"`
	CDNRPS            int    `json:"cdnRps"`
	UploadConnections int    `json:"uploadConnections"`
	CDNViewerTarget   int    `json:"cdnViewerTarget"`
}
type EnvelopeProviderSnapshot struct {
	Account               string    `json:"account"`
	Provider              string    `json:"provider"`
	RequestID             string    `json:"requestId"`
	ObservedAt            time.Time `json:"observedAt"`
	Currency              string    `json:"currency"`
	CostMicros            int64     `json:"costMicros"`
	BudgetMicros          int64     `json:"budgetMicros"`
	UploadConcurrency     int       `json:"uploadConcurrency"`
	CDNViewers            int       `json:"cdnViewers"`
	OriginalRetentionDays int       `json:"originalRetentionDays"`
	RecordingOptIn        bool      `json:"recordingOptIn"`
}
type EnvelopeTarget struct {
	ID                string            `json:"id"`
	Class             string            `json:"class"`
	URL               string            `json:"url"`
	HeaderEnvironment map[string]string `json:"headerEnvironment"`
	Headers           map[string]string `json:"headers"`
	FixturePath       string            `json:"fixturePath,omitempty"`
	ExpectedStatus    int               `json:"expectedStatus"`
	ExpectedSHA256    string            `json:"expectedSha256,omitempty"`
	Wallet            string            `json:"wallet,omitempty"`
}
type EnvelopeCommand struct {
	Source         EnvelopeFile `json:"source"`
	Arguments      []string     `json:"arguments"`
	TimeoutSeconds int          `json:"timeoutSeconds"`
}
type EnvelopeProbe struct {
	Target          EnvelopeTarget             `json:"target"`
	Method          string                     `json:"method"`
	JSONEquals      map[string]json.RawMessage `json:"jsonEquals"`
	RequiredHeaders map[string]string          `json:"requiredHeaders"`
}
type EnvelopeSQLCheck struct {
	DSNEnvironment string          `json:"dsnEnvironment"`
	Query          string          `json:"query"`
	ExpectedJSON   json.RawMessage `json:"expectedJson"`
}
type EnvelopeCase struct {
	ID     int                `json:"id"`
	Before []EnvelopeCommand  `json:"before"`
	After  []EnvelopeCommand  `json:"after"`
	Probes []EnvelopeProbe    `json:"probes"`
	SQL    []EnvelopeSQLCheck `json:"sql"`
}
type EnvelopeConfig struct {
	Scope             string             `json:"scope"`
	CandidateSHA256   string             `json:"candidateSha256"`
	CandidateManifest EnvelopeFile       `json:"candidateManifest"`
	PlaybackBinary    EnvelopeFile       `json:"playbackBinary"`
	PlaybackSeconds   int                `json:"playbackSeconds"`
	PlaybackTargetID  string             `json:"playbackTargetId"`
	InterfaceManifest EnvelopeFile       `json:"interfaceManifest"`
	SourceFiles       []EnvelopeFile     `json:"sourceFiles"`
	Allocation        EnvelopeFile       `json:"allocation"`
	ProviderSnapshot  EnvelopeFile       `json:"providerSnapshot"`
	Release           MediaReleaseInputs `json:"release"`
	TLSRootCA         string             `json:"tlsRootCa"`
	TLSCertificate    string             `json:"tlsCertificate"`
	TLSKey            string             `json:"tlsKey"`
	DurationSeconds   int                `json:"durationSeconds"`
	ReadWorkers       int                `json:"readWorkers"`
	Targets           []EnvelopeTarget   `json:"targets"`
	SlowUploads       []EnvelopeTarget   `json:"slowUploads"`
	ResourceURL       string             `json:"resourceUrl"`
	ResourceHeaders   map[string]string  `json:"resourceHeaders"`
	MaxResidentBytes  int64              `json:"maxResidentBytes"`
	MaxGoroutines     int64              `json:"maxGoroutines"`
	MaxFailureRatio   float64            `json:"maxFailureRatio"`
	Cases             []EnvelopeCase     `json:"cases"`
	OutputDirectory   string             `json:"outputDirectory"`
}
type EnvelopeSample struct {
	ID        string    `json:"requestId"`
	TargetID  string    `json:"targetId"`
	Class     string    `json:"class"`
	Started   time.Time `json:"startedAt"`
	Ended     time.Time `json:"endedAt"`
	LatencyNS int64     `json:"latencyNs"`
	Status    int       `json:"status"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"responseSha256,omitempty"`
	Outcome   string    `json:"outcome"`
}
type EnvelopeResourceSample struct {
	At            time.Time `json:"at"`
	ResidentBytes int64     `json:"residentBytes"`
	Goroutines    int64     `json:"goroutines"`
}
type EnvelopeResult struct {
	Scope                  string                          `json:"scope"`
	CandidateSHA256        string                          `json:"candidateSha256"`
	InterfaceSHA256        string                          `json:"interfaceSha256"`
	AllocationSHA256       string                          `json:"allocationSha256"`
	ProviderAccount        string                          `json:"providerAccount"`
	QuotaSHA256            string                          `json:"quotaSha256"`
	ProviderSnapshotSHA256 string                          `json:"providerSnapshotSha256"`
	SourceFiles            []EnvelopeFile                  `json:"sourceFiles"`
	FixtureProvenance      []LicensedFixture               `json:"fixtureProvenance"`
	Started                time.Time                       `json:"startedAt"`
	Ended                  time.Time                       `json:"endedAt"`
	LoadStarted            time.Time                       `json:"loadStartedAt"`
	LoadEnded              time.Time                       `json:"loadEndedAt"`
	Distributions          map[string]EnvelopeDistribution `json:"distributions"`
	SuccessfulRPS          map[string]float64              `json:"successfulRps"`
	MaximumResidentBytes   int64                           `json:"maximumResidentBytes"`
	MaximumGoroutines      int64                           `json:"maximumGoroutines"`
	HeldUploadConnections  int                             `json:"heldUploadConnections"`
	ScenarioResults        map[int]string                  `json:"scenarioResults"`
	Artifacts              map[string]string               `json:"artifacts"`
	Blockers               []string                        `json:"blockers"`
	SharedReleaseQualified bool                            `json:"sharedReleaseQualified"`
}
type envelopeRunner struct {
	config     EnvelopeConfig
	allocation EnvelopeAllocation
	client     *http.Client
	tlsConfig  *tls.Config
	aggregate  *EnvelopeAccumulator
	raw        *os.File
	resources  *os.File
	events     *os.File
	mu         sync.Mutex
	result     EnvelopeResult
	writeError error
	root       *os.Root
}

func RunMediaEnvelope(ctx context.Context, config EnvelopeConfig) (EnvelopeResult, error) {
	r, e := prepareMediaEnvelope(config)
	if e != nil {
		return EnvelopeResult{Scope: config.Scope, Blockers: []string{e.Error()}, SharedReleaseQualified: false}, e
	}
	defer r.root.Close()
	defer r.raw.Close()
	defer r.resources.Close()
	defer r.events.Close()
	defer r.client.CloseIdleConnections()
	r.result.Started = time.Now().UTC()
	r.event(map[string]any{"type": "unlicensed_fixture_rejected_before_requests", "at": r.result.Started, "fixtureSha256": config.Release.Fixtures[0].SHA256})
	r.event(map[string]any{"type": "provider_quota_cost_retention_binding", "at": r.result.Started, "snapshotSha256": config.ProviderSnapshot.SHA256, "providerAccount": config.Release.ProviderAccount})
	runErr := r.run(ctx)
	r.result.Ended = time.Now().UTC()
	r.result.Distributions = r.aggregate.SnapshotDistributions()
	r.result.SuccessfulRPS = map[string]float64{}
	seconds := r.result.LoadEnded.Sub(r.result.LoadStarted).Seconds()
	for name, d := range r.result.Distributions {
		if seconds > 0 {
			r.result.SuccessfulRPS[name] = float64(d.Succeeded) / seconds
		}
	}
	if runErr != nil {
		r.result.Blockers = append(r.result.Blockers, runErr.Error())
	}
	if e = r.raw.Sync(); e != nil {
		r.result.Blockers = append(r.result.Blockers, "raw_sample_sync_failed")
	}
	if e = r.resources.Sync(); e != nil {
		r.result.Blockers = append(r.result.Blockers, "resource_sample_sync_failed")
	}
	if e = r.events.Sync(); e != nil {
		r.result.Blockers = append(r.result.Blockers, "event_sample_sync_failed")
	}
	for _, name := range []string{"requests.jsonl", "resources.jsonl", "events.jsonl"} {
		f, e := r.root.Open(name)
		if e != nil {
			return r.result, e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		_ = f.Close()
		if e != nil {
			return r.result, e
		}
		r.result.Artifacts[name] = hex.EncodeToString(h.Sum(nil))
	}
	if r.writeError != nil {
		r.result.Blockers = append(r.result.Blockers, "raw_capture_failed")
	}
	for _, name := range []string{"origin", "metadata", "cdn"} {
		d := r.result.Distributions[name]
		if d.Count == 0 || d.Succeeded == 0 {
			r.result.Blockers = append(r.result.Blockers, name+"_no_successful_samples")
			continue
		}
		assigned := map[string]int{"origin": r.allocation.OriginRPS, "metadata": r.allocation.MetadataRPS, "cdn": r.allocation.CDNRPS}[name]
		if r.result.SuccessfulRPS[name] < float64(assigned)*(1-config.MaxFailureRatio) {
			r.result.Blockers = append(r.result.Blockers, name+"_assigned_throughput_unmet")
		}
		if float64(d.Failed)/float64(d.Count) > config.MaxFailureRatio {
			r.result.Blockers = append(r.result.Blockers, name+"_failure_budget_exceeded")
		}
		if (name == "origin" && (d.P95UpperBoundMS > 150 || d.P99UpperBoundMS > 400)) || (name == "metadata" && d.P99UpperBoundMS > 400) {
			r.result.Blockers = append(r.result.Blockers, name+"_latency_bound_exceeded")
		}
	}
	if r.result.MaximumResidentBytes > config.MaxResidentBytes || r.result.MaximumGoroutines > config.MaxGoroutines {
		r.result.Blockers = append(r.result.Blockers, "upload_resource_budget_exceeded")
	}
	if r.result.HeldUploadConnections != 200 {
		r.result.Blockers = append(r.result.Blockers, "required_200_held_connections_unobserved")
	}
	if config.Scope == "shared-release" {
		r.result.Blockers = append(r.result.Blockers, "shared_load_restore_and_cdn_viewer_authority_requires_serial_release_gate")
	}
	encoded, e := json.MarshalIndent(r.result, "", "  ")
	if e != nil {
		return r.result, e
	}
	out, e := r.root.OpenFile("result.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return r.result, e
	}
	_, e = out.Write(append(encoded, '\n'))
	if e == nil {
		e = out.Sync()
	}
	closeErr := out.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return r.result, e
	}
	if len(r.result.Blockers) > 0 {
		return r.result, errors.New("media envelope has unresolved blockers")
	}
	return r.result, nil
}

func prepareMediaEnvelope(c EnvelopeConfig) (*envelopeRunner, error) {
	if c.Scope != "lane-measurement" && c.Scope != "shared-release" {
		return nil, errors.New("measurement_scope_required")
	}
	if !digest.MatchString(c.CandidateSHA256) || len(c.SourceFiles) == 0 || c.CandidateManifest.SHA256 != c.CandidateSHA256 || c.PlaybackSeconds < 1 || c.PlaybackSeconds > 60 || c.DurationSeconds < 30 || c.DurationSeconds > 7*86400 || c.ReadWorkers < 3 || c.ReadWorkers > 256 || c.MaxResidentBytes <= 0 || c.MaxGoroutines <= 0 || c.MaxFailureRatio < 0 || c.MaxFailureRatio > 0.001 || c.OutputDirectory == "" {
		return nil, errors.New("measurement_budget_or_candidate_missing")
	}
	if c.Scope == "shared-release" && c.DurationSeconds < 24*3600 {
		return nil, errors.New("shared_profile_requires_24h")
	}
	if e := ValidateMediaReleaseInputs(c.Release, time.Now()); e != nil {
		return nil, e
	}
	unlicensed := c.Release
	unlicensed.Fixtures = append([]LicensedFixture(nil), c.Release.Fixtures...)
	unlicensed.Fixtures[0].Ownership = ""
	if ValidateMediaReleaseInputs(unlicensed, time.Now()) == nil {
		return nil, errors.New("unlicensed_fixture_was_admitted")
	}
	for _, f := range append(append([]EnvelopeFile(nil), c.SourceFiles...), c.CandidateManifest, c.InterfaceManifest, c.Allocation, c.ProviderSnapshot) {
		if _, e := readEnvelopeFile(f, 16<<20); e != nil {
			return nil, e
		}
	}
	raw, e := readEnvelopeFile(c.Allocation, 1<<20)
	if e != nil {
		return nil, e
	}
	providerRaw, e := readEnvelopeFile(c.ProviderSnapshot, 1<<20)
	if e != nil {
		return nil, e
	}
	var provider EnvelopeProviderSnapshot
	if e = strictEnvelopeJSON(providerRaw, &provider); e != nil {
		return nil, e
	}
	if provider.Account != c.Release.ProviderAccount || provider.Provider == "" || !atom.MatchString(provider.RequestID) || provider.ObservedAt.IsZero() || provider.ObservedAt.After(time.Now()) || time.Since(provider.ObservedAt) > time.Hour || provider.Currency != "USD" || provider.CostMicros < 0 || provider.BudgetMicros <= 0 || provider.CostMicros > provider.BudgetMicros || provider.UploadConcurrency < 200 || provider.CDNViewers < 20000 || provider.OriginalRetentionDays != 30 || provider.RecordingOptIn != c.Release.RecordingOptIn {
		return nil, errors.New("actual_provider_quota_cost_retention_snapshot_missing_or_outside_budget")
	}
	var allocation EnvelopeAllocation
	if e = strictEnvelopeJSON(raw, &allocation); e != nil {
		return nil, e
	}
	if allocation.Lane != "L41" || allocation.GlobalDAU != 500000 || allocation.GlobalPeakRPS != 10000 || allocation.AssignedRPS < 1 || allocation.AssignedRPS > 10000 || allocation.OriginRPS < 1 || allocation.MetadataRPS < 1 || allocation.CDNRPS < 1 || allocation.OriginRPS+allocation.MetadataRPS+allocation.CDNRPS > allocation.AssignedRPS || allocation.UploadConnections != 200 || allocation.CDNViewerTarget < 20000 {
		return nil, errors.New("workload_allocation_invalid")
	}
	if _, e := readEnvelopeFile(c.PlaybackBinary, 256<<20); e != nil {
		return nil, e
	}
	ca, e := os.ReadFile(c.TLSRootCA)
	if e != nil {
		return nil, errors.New("tls_authority_missing")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, errors.New("tls_authority_invalid")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, NextProtos: []string{"http/1.1"}}
	if c.TLSCertificate != "" || c.TLSKey != "" {
		cert, e := tls.LoadX509KeyPair(c.TLSCertificate, c.TLSKey)
		if e != nil {
			return nil, errors.New("tls_client_identity_unavailable")
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil, TLSClientConfig: tlsConfig, MaxConnsPerHost: c.ReadWorkers + 4, MaxIdleConns: 512, MaxIdleConnsPerHost: c.ReadWorkers, ResponseHeaderTimeout: 3 * time.Second, ForceAttemptHTTP2: false}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	classes := map[string]int{}
	playbackFound := false
	ids := map[string]bool{}
	for _, target := range c.Targets {
		if target.ExpectedStatus != 200 {
			return nil, errors.New("load_reads_must_expect_actual_success")
		}
		if target.Class != "origin" && target.Class != "metadata" && target.Class != "cdn" {
			return nil, errors.New("read_target_class_invalid")
		}
		if e = validateEnvelopeTarget(c, target); e != nil {
			return nil, e
		}
		if ids[target.ID] {
			return nil, ErrConflict
		}
		ids[target.ID] = true
		classes[target.Class]++
		if target.Class == "cdn" && target.ID == c.PlaybackTargetID && len(target.HeaderEnvironment) == 0 && len(target.Headers) == 0 {
			playbackFound = true
		}
	}
	if !playbackFound {
		return nil, errors.New("licensed_public_cdn_playback_target_required")
	}
	if classes["origin"] == 0 || classes["metadata"] == 0 || classes["cdn"] == 0 || len(c.SlowUploads) != 200 {
		return nil, errors.New("complete_media_target_topology_required")
	}
	wallets := map[string]int{}
	for _, target := range c.SlowUploads {
		if target.Class != "upload" || !regexpWallet(target.Wallet) {
			return nil, errors.New("authenticated_upload_wallet_required")
		}
		if e = validateEnvelopeTarget(c, target); e != nil {
			return nil, e
		}
		if ids[target.ID] {
			return nil, ErrConflict
		}
		ids[target.ID] = true
		wallets[target.Wallet]++
		if wallets[target.Wallet] > 2 {
			return nil, errors.New("upload_wallet_concurrency_exceeds_two")
		}
		body, e := licensedEnvelopeBytes(c, target.FixturePath)
		if e != nil || len(body) != 6<<20 {
			return nil, errors.New("licensed_6mib_upload_required")
		}
	}
	if _, e = secureEnvelopeURL(c.ResourceURL); e != nil {
		return nil, e
	}
	for _, name := range c.ResourceHeaders {
		if os.Getenv(name) == "" {
			return nil, errors.New("resource_identity_missing")
		}
	}
	cases := map[int]bool{}
	for _, test := range c.Cases {
		if e = validateEnvelopeCase(test); e != nil {
			return nil, e
		}
		if test.ID < 1 || test.ID > 8 || (test.ID == 5 || test.ID == 8) || cases[test.ID] || len(test.Probes) == 0 {
			return nil, errors.New("adversarial_case_invalid")
		}
		cases[test.ID] = true
		for _, command := range append(append([]EnvelopeCommand(nil), test.Before...), test.After...) {
			if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 300 {
				return nil, ErrConflict
			}
			if _, e = readEnvelopeFile(command.Source, 16<<20); e != nil {
				return nil, e
			}
		}
		for _, probe := range test.Probes {
			if e = validateEnvelopeTarget(c, probe.Target); e != nil {
				return nil, e
			}
			if probe.Method != "GET" && probe.Method != "HEAD" && probe.Method != "PUT" && probe.Method != "POST" {
				return nil, ErrConflict
			}
		}
		for _, check := range test.SQL {
			if os.Getenv(check.DSNEnvironment) == "" || !json.Valid(check.ExpectedJSON) || !strings.HasPrefix(strings.TrimSpace(strings.ToUpper(check.Query)), "SELECT ") || strings.Contains(check.Query, ";") {
				return nil, errors.New("sql_observation_authority_missing")
			}
		}
	}
	for _, id := range []int{1, 2, 3, 4, 6, 7} {
		if !cases[id] {
			return nil, errors.New("adversarial_case_missing")
		}
	}
	if e = os.Mkdir(c.OutputDirectory, 0700); e != nil {
		return nil, errors.New("new_private_output_directory_required")
	}
	root, e := os.OpenRoot(c.OutputDirectory)
	if e != nil {
		return nil, e
	}
	samples, e := root.OpenFile("requests.jsonl", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		root.Close()
		return nil, e
	}
	resources, e := root.OpenFile("resources.jsonl", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		samples.Close()
		root.Close()
		return nil, e
	}
	events, e := root.OpenFile("events.jsonl", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		resources.Close()
		samples.Close()
		root.Close()
		return nil, e
	}
	return &envelopeRunner{config: c, allocation: allocation, client: client, tlsConfig: tlsConfig, aggregate: NewEnvelopeAccumulator(), raw: samples, resources: resources, events: events, root: root, result: EnvelopeResult{Scope: c.Scope, CandidateSHA256: c.CandidateSHA256, InterfaceSHA256: c.InterfaceManifest.SHA256, AllocationSHA256: c.Allocation.SHA256, ProviderAccount: c.Release.ProviderAccount, QuotaSHA256: c.Release.QuotaConfirmationSHA256, ProviderSnapshotSHA256: c.ProviderSnapshot.SHA256, SourceFiles: c.SourceFiles, FixtureProvenance: c.Release.Fixtures, ScenarioResults: map[int]string{8: "unlicensed_fixture_rejected_before_network_requests"}, Artifacts: map[string]string{}, Blockers: []string{}}}, nil
}

func regexpWallet(wallet string) bool {
	if len(wallet) != 42 || !strings.HasPrefix(wallet, "0x") {
		return false
	}
	for _, c := range wallet[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func secureEnvelopeURL(value string) (*url.URL, error) {
	u, e := url.Parse(value)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("explicit_https_target_required")
	}
	return u, nil
}
func readEnvelopeFile(file EnvelopeFile, limit int64) ([]byte, error) {
	if !digest.MatchString(file.SHA256) {
		return nil, errors.New("evidence_digest_missing")
	}
	f, e := os.Open(file.Path)
	if e != nil {
		return nil, errors.New("evidence_source_missing")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		return nil, errors.New("evidence_source_invalid")
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) != info.Size() {
		return nil, errors.New("evidence_source_read_failed")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != file.SHA256 {
		return nil, errors.New("evidence_digest_mismatch")
	}
	return raw, nil
}
func strictEnvelopeJSON(raw []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(value); e != nil {
		return errors.New("measurement_configuration_invalid")
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("measurement_configuration_trailing_data")
	}
	return nil
}
func licensedEnvelopeBytes(c EnvelopeConfig, path string) ([]byte, error) {
	for _, fixture := range c.Release.Fixtures {
		if fixture.Path == path {
			root, e := os.OpenRoot(c.Release.FixtureRoot)
			if e != nil {
				return nil, e
			}
			defer root.Close()
			file, e := root.Open(path)
			if e != nil {
				return nil, e
			}
			defer file.Close()
			raw, e := io.ReadAll(io.LimitReader(file, (64<<20)+1))
			if e != nil || len(raw) > 64<<20 {
				return nil, ErrConflict
			}
			sum := sha256.Sum256(raw)
			if hex.EncodeToString(sum[:]) != fixture.SHA256 {
				return nil, ErrConflict
			}
			return raw, nil
		}
	}
	return nil, errors.New("request_fixture_outside_provenance")
}
func validateEnvelopeTarget(c EnvelopeConfig, t EnvelopeTarget) error {
	if !atom.MatchString(t.ID) || t.ExpectedStatus < 200 || t.ExpectedStatus > 599 {
		return ErrConflict
	}
	if _, e := secureEnvelopeURL(t.URL); e != nil {
		return e
	}
	for key, name := range t.HeaderEnvironment {
		if strings.ContainsAny(key+name, "\r\n") || name == "" || os.Getenv(name) == "" {
			return errors.New("target_identity_missing")
		}
	}
	for key, value := range t.Headers {
		if strings.ContainsAny(key+value, "\r\n") || strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "Cookie") {
			return errors.New("inline_credentials_denied")
		}
	}
	if t.ExpectedSHA256 != "" || t.Class == "origin" || t.Class == "cdn" {
		if !digest.MatchString(t.ExpectedSHA256) {
			return errors.New("approved_response_digest_missing")
		}
		found := false
		for _, f := range c.Release.Fixtures {
			if f.SHA256 == t.ExpectedSHA256 {
				found = true
			}
		}
		if !found {
			return errors.New("response_content_not_in_licensed_inventory")
		}
	}
	if t.FixturePath != "" {
		_, e := licensedEnvelopeBytes(c, t.FixturePath)
		return e
	}
	return nil
}
func envelopeHeaders(t EnvelopeTarget) (http.Header, error) {
	h := http.Header{}
	for key, value := range t.Headers {
		h.Set(key, value)
	}
	for key, name := range t.HeaderEnvironment {
		value := os.Getenv(name)
		if value == "" || strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("target_identity_unavailable")
		}
		h.Set(key, value)
	}
	h.Set("X-Request-ID", uuid.NewString())
	return h, nil
}

func (r *envelopeRunner) capture(sample EnvelopeSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := json.NewEncoder(r.raw).Encode(sample); e != nil && r.writeError == nil {
		r.writeError = e
	}
	accepted := sample.Outcome == "accepted"
	if sample.Class == "origin" || sample.Class == "metadata" || sample.Class == "cdn" || sample.Class == "upload" {
		if e := r.aggregate.Observe(sample.Class, time.Duration(sample.LatencyNS), sample.Status, sample.Bytes, accepted); e != nil && r.writeError == nil {
			r.writeError = e
		}
	}
}
func (r *envelopeRunner) request(ctx context.Context, target EnvelopeTarget, method string) ([]byte, http.Header, EnvelopeSample, error) {
	sample := EnvelopeSample{ID: uuid.NewString(), TargetID: target.ID, Class: target.Class, Started: time.Now().UTC(), Outcome: "failed"}
	started := time.Now()
	finish := func() {
		sample.Ended = time.Now().UTC()
		sample.LatencyNS = time.Since(started).Nanoseconds()
		r.capture(sample)
	}
	var body []byte
	var e error
	if target.FixturePath != "" {
		body, e = licensedEnvelopeBytes(r.config, target.FixturePath)
		if e != nil {
			finish()
			return nil, nil, sample, e
		}
	}
	timeout := 3 * time.Second
	if method == "PUT" || method == "POST" {
		timeout = 10 * time.Second
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, e := http.NewRequestWithContext(bounded, method, target.URL, bytes.NewReader(body))
	if e != nil {
		finish()
		return nil, nil, sample, e
	}
	req.Header, e = envelopeHeaders(target)
	if e != nil {
		finish()
		return nil, nil, sample, e
	}
	req.Header.Set("X-Request-ID", sample.ID)
	response, e := r.client.Do(req)
	if e != nil {
		finish()
		return nil, nil, sample, errors.New("target_transport_failed")
	}
	defer response.Body.Close()
	sample.Status = response.StatusCode
	var raw []byte
	hash := sha256.New()
	if target.Class == "origin" || target.Class == "cdn" {
		sample.Bytes, e = io.Copy(hash, io.LimitReader(response.Body, (64<<20)+1))
	} else {
		raw, e = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
		sample.Bytes = int64(len(raw))
		_, _ = hash.Write(raw)
		if len(raw) > 1<<20 {
			e = ErrConflict
		}
	}
	sample.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if e != nil || sample.Bytes > 64<<20 {
		finish()
		return nil, response.Header, sample, errors.New("target_body_failed")
	}
	if response.StatusCode != target.ExpectedStatus || (target.ExpectedSHA256 != "" && sample.SHA256 != target.ExpectedSHA256) {
		finish()
		return raw, response.Header, sample, errors.New("target_response_mismatch")
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		sample.Outcome = "accepted"
	} else {
		sample.Outcome = "expected_rejection"
	}
	finish()
	return raw, response.Header, sample, nil
}

func (r *envelopeRunner) run(ctx context.Context) error {
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(r.config.DurationSeconds)*time.Second)
	var uploads sync.WaitGroup
	defer func() { cancel(); uploads.Wait() }()
	uploadReady := make(chan struct{}, 200)
	uploadDone := make(chan error, 200)
	for _, target := range r.config.SlowUploads {
		uploads.Add(1)
		go func(target EnvelopeTarget) {
			defer uploads.Done()
			uploadDone <- r.slowUpload(runCtx, target, uploadReady)
		}(target)
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for i := 0; i < 200; i++ {
		select {
		case <-uploadReady:
		case e := <-uploadDone:
			if e == nil {
				e = errors.New("upload_finished_before_hold")
			}
			return e
		case <-deadline.C:
			return errors.New("upload_topology_not_open_within_three_seconds")
		case <-runCtx.Done():
			return runCtx.Err()
		}
	}
	holdTimer := time.NewTimer(2 * time.Second)
	select {
	case e := <-uploadDone:
		holdTimer.Stop()
		if e == nil {
			e = errors.New("upload_rejected_before_mixed_load_hold")
		}
		return e
	case <-holdTimer.C:
	case <-runCtx.Done():
		holdTimer.Stop()
		return runCtx.Err()
	}
	r.result.HeldUploadConnections = 200
	r.result.ScenarioResults[5] = "200_upload_connections_sent_headers_and_body_prefix"
	r.result.LoadStarted = time.Now().UTC()
	var group sync.WaitGroup
	errs := make(chan error, 16)
	slots := make(chan struct{}, r.config.ReadWorkers)
	for class, rate := range map[string]int{"origin": r.allocation.OriginRPS, "metadata": r.allocation.MetadataRPS, "cdn": r.allocation.CDNRPS} {
		targets := []EnvelopeTarget{}
		for _, target := range r.config.Targets {
			if target.Class == class {
				targets = append(targets, target)
			}
		}
		group.Add(1)
		go func(targets []EnvelopeTarget, rate int) {
			defer group.Done()
			period := time.Second / time.Duration(rate)
			ticker := time.NewTicker(period)
			defer ticker.Stop()
			index := 0
			for {
				select {
				case scheduled := <-ticker.C:
					if time.Since(scheduled) > period*2 {
						select {
						case errs <- errors.New("load_generator_cannot_sustain_assigned_schedule"):
						default:
						}
						cancel()
						return
					}
					select {
					case slots <- struct{}{}:
					default:
						select {
						case errs <- errors.New("load_generator_worker_budget_exhausted"):
						default:
						}
						cancel()
						return
					}
					target := targets[index%len(targets)]
					index++
					group.Add(1)
					go func() { defer group.Done(); defer func() { <-slots }(); _, _, _, _ = r.request(ctx, target, "GET") }()
				case <-runCtx.Done():
					return
				}
			}
		}(targets, rate)
	}
	group.Add(1)
	go func() {
		defer group.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			if runCtx.Err() != nil {
				return
			}
			if e := r.sampleResources(runCtx); e != nil {
				if runCtx.Err() != nil {
					return
				}
				select {
				case errs <- e:
				default:
				}
				cancel()
				return
			}
			select {
			case <-ticker.C:
			case <-runCtx.Done():
				return
			}
		}
	}()
	var uploadErr error
	for i := 0; i < 200; i++ {
		select {
		case e := <-uploadDone:
			if e != nil && uploadErr == nil {
				uploadErr = e
			}
		case <-runCtx.Done():
			if uploadErr == nil {
				uploadErr = errors.New("uploads_did_not_reach_bounded_terminal_response")
			}
			i = 199
		}
	}
	<-runCtx.Done()
	group.Wait()
	r.result.LoadEnded = time.Now().UTC()
	close(errs)
	for e := range errs {
		if e != nil {
			return e
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if uploadErr != nil {
		return uploadErr
	}
	if e := r.playback(ctx); e != nil {
		return e
	}
	var scenarioErrors error
	for _, test := range r.config.Cases {
		if e := r.runCase(ctx, test); e != nil {
			r.result.ScenarioResults[test.ID] = "failed"
			scenarioErrors = errors.Join(scenarioErrors, fmt.Errorf("scenario_%d_failed", test.ID))
			if ctx.Err() != nil {
				break
			}
		} else {
			r.result.ScenarioResults[test.ID] = "observed_assertions_passed"
		}
	}
	return scenarioErrors
}

func (r *envelopeRunner) slowUpload(ctx context.Context, target EnvelopeTarget, ready chan<- struct{}) error {
	sample := EnvelopeSample{ID: uuid.NewString(), TargetID: target.ID, Class: "upload", Started: time.Now().UTC(), Outcome: "failed"}
	start := time.Now()
	defer func() {
		sample.Ended = time.Now().UTC()
		sample.LatencyNS = time.Since(start).Nanoseconds()
		r.capture(sample)
	}()
	prefix, bodySize, e := licensedEnvelopePrefix(r.config, target.FixturePath)
	if e != nil {
		return e
	}
	u, e := secureEnvelopeURL(target.URL)
	if e != nil {
		return e
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	cfg := r.tlsConfig.Clone()
	cfg.ServerName = u.Hostname()
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, e := (&tls.Dialer{NetDialer: &net.Dialer{Timeout: 3 * time.Second}, Config: cfg}).DialContext(dialCtx, "tcp", host)
	if e != nil {
		return errors.New("upload_tls_connection_failed")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(35 * time.Second))
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	headers, e := envelopeHeaders(target)
	if e != nil {
		return e
	}
	headers.Set("Content-Length", strconv.FormatInt(bodySize, 10))
	headers.Set("Connection", "close")
	headers.Set("X-Request-ID", sample.ID)
	var wire bytes.Buffer
	fmt.Fprintf(&wire, "PUT %s HTTP/1.1\r\nHost: %s\r\n", u.RequestURI(), u.Host)
	if e = headers.Write(&wire); e != nil {
		return e
	}
	wire.WriteString("\r\n")
	wire.Write(prefix)
	if _, e = conn.Write(wire.Bytes()); e != nil {
		return errors.New("upload_header_write_failed")
	}
	ready <- struct{}{}
	response, e := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "PUT"})
	if e != nil {
		return errors.New("slow_upload_terminal_response_missing")
	}
	defer response.Body.Close()
	sample.Status = response.StatusCode
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	sample.Bytes = int64(len(raw))
	hash := sha256.Sum256(raw)
	sample.SHA256 = hex.EncodeToString(hash[:])
	if e != nil || len(raw) > 1<<20 {
		return errors.New("slow_upload_response_invalid")
	}
	if response.StatusCode != target.ExpectedStatus {
		return errors.New("slow_upload_unexpected_status")
	}
	if response.StatusCode != 408 && response.StatusCode != 429 && response.StatusCode != 503 && response.StatusCode != 413 {
		return errors.New("slow_upload_was_acknowledged")
	}
	if (response.StatusCode == 429 || response.StatusCode == 503) && response.Header.Get("Retry-After") == "" {
		return errors.New("upload_backpressure_retry_missing")
	}
	if time.Since(start) > 30*time.Second {
		return errors.New("upload_deadline_exceeded")
	}
	sample.Outcome = "expected_rejection"
	return nil
}

func (r *envelopeRunner) sampleResources(ctx context.Context) error {
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(bounded, "GET", r.config.ResourceURL, nil)
	if e != nil {
		return e
	}
	for name, reference := range r.config.ResourceHeaders {
		value := os.Getenv(reference)
		if value == "" || strings.ContainsAny(value, "\r\n") {
			return errors.New("resource_identity_unavailable")
		}
		req.Header.Set(name, value)
	}
	response, e := r.client.Do(req)
	if e != nil {
		return errors.New("resource_endpoint_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("resource_endpoint_denied")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return errors.New("resource_capture_invalid")
	}
	values := map[string]int64{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 2 {
			continue
		}
		if parts[0] != "process_resident_memory_bytes" && parts[0] != "go_goroutines" {
			continue
		}
		if _, seen := values[parts[0]]; seen {
			return errors.New("resource_metric_ambiguous")
		}
		value, e := strconv.ParseFloat(parts[1], 64)
		if e != nil || value < 0 || value > float64(1<<53) || value != float64(int64(value)) {
			return errors.New("resource_metric_invalid")
		}
		values[parts[0]] = int64(value)
	}
	if scanner.Err() != nil || len(values) != 2 {
		return errors.New("actual_process_resource_metrics_missing")
	}
	sample := EnvelopeResourceSample{At: time.Now().UTC(), ResidentBytes: values["process_resident_memory_bytes"], Goroutines: values["go_goroutines"]}
	r.mu.Lock()
	defer r.mu.Unlock()
	if sample.ResidentBytes > r.result.MaximumResidentBytes {
		r.result.MaximumResidentBytes = sample.ResidentBytes
	}
	if sample.Goroutines > r.result.MaximumGoroutines {
		r.result.MaximumGoroutines = sample.Goroutines
	}
	if e = json.NewEncoder(r.resources).Encode(sample); e != nil {
		r.writeError = e
		return e
	}
	if sample.ResidentBytes > r.config.MaxResidentBytes || sample.Goroutines > r.config.MaxGoroutines {
		return errors.New("process_resource_limit_exceeded")
	}
	return nil
}

func (r *envelopeRunner) runCase(ctx context.Context, test EnvelopeCase) (err error) {
	defer func() {
		for _, command := range test.After {
			if cleanupErr := r.runCommand(context.WithoutCancel(ctx), command, test.ID); cleanupErr != nil {
				err = errors.Join(err, errors.New("scenario_cleanup_failed"))
			}
		}
	}()
	for _, command := range test.Before {
		if e := r.runCommand(ctx, command, test.ID); e != nil {
			return e
		}
	}
	if test.ID == 7 {
		timer := time.NewTimer(61 * time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
	for _, probe := range test.Probes {
		raw, headers, _, e := r.request(ctx, probe.Target, probe.Method)
		if e != nil {
			return e
		}
		for name, expected := range probe.RequiredHeaders {
			if headers.Get(name) != expected {
				return errors.New("scenario_header_assertion_failed")
			}
		}
		if len(probe.JSONEquals) > 0 {
			var object map[string]json.RawMessage
			if json.Unmarshal(raw, &object) != nil {
				return errors.New("scenario_json_invalid")
			}
			for key, expected := range probe.JSONEquals {
				observed, exists := object[key]
				if !exists || !sameEnvelopeJSON(observed, expected) {
					return errors.New("scenario_json_assertion_failed")
				}
			}
		}
	}
	for _, check := range test.SQL {
		if e := r.runSQL(ctx, check, test.ID); e != nil {
			return e
		}
	}
	return nil
}
func sameEnvelopeJSON(a, b []byte) bool {
	var left, right any
	dl := json.NewDecoder(bytes.NewReader(a))
	dl.UseNumber()
	dr := json.NewDecoder(bytes.NewReader(b))
	dr.UseNumber()
	if dl.Decode(&left) != nil || dr.Decode(&right) != nil {
		return false
	}
	ca, e := json.Marshal(left)
	if e != nil {
		return false
	}
	cb, e := json.Marshal(right)
	return e == nil && bytes.Equal(ca, cb)
}
func (r *envelopeRunner) runCommand(ctx context.Context, command EnvelopeCommand, scenario int) error {
	if _, e := readEnvelopeFile(command.Source, 16<<20); e != nil {
		return e
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(command.TimeoutSeconds)*time.Second)
	defer cancel()
	program, e := filepath.Abs(command.Source.Path)
	if e != nil {
		return e
	}
	process := exec.CommandContext(bounded, program, command.Arguments...)
	process.Stdout = io.Discard
	process.Stderr = io.Discard
	started := time.Now().UTC()
	e = process.Run()
	exit := 0
	if e != nil {
		exit = -1
		if exitError, ok := e.(*exec.ExitError); ok {
			exit = exitError.ExitCode()
		}
	}
	r.event(map[string]any{"type": "fixture_operation", "scenario": scenario, "sourceSha256": command.Source.SHA256, "startedAt": started, "endedAt": time.Now().UTC(), "exitCode": exit})
	if e != nil {
		return errors.New("authorized_fixture_operation_failed")
	}
	return nil
}
func (r *envelopeRunner) runSQL(ctx context.Context, check EnvelopeSQLCheck, scenario int) error {
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	pool, e := pgxpool.New(bounded, os.Getenv(check.DSNEnvironment))
	if e != nil {
		return errors.New("sql_observation_unavailable")
	}
	defer pool.Close()
	connection, e := pool.Acquire(bounded)
	if e != nil {
		return errors.New("sql_observation_unavailable")
	}
	defer connection.Release()
	tx, e := connection.Begin(bounded)
	if e != nil {
		return errors.New("sql_observation_unavailable")
	}
	defer tx.Rollback(context.WithoutCancel(bounded))
	if _, e = tx.Exec(bounded, "SET TRANSACTION READ ONLY"); e != nil {
		return errors.New("sql_observation_unavailable")
	}
	if _, e = tx.Exec(bounded, "SET LOCAL statement_timeout='2000ms'"); e != nil {
		return errors.New("sql_observation_unavailable")
	}
	var observed []byte
	if e = tx.QueryRow(bounded, check.Query).Scan(&observed); e != nil {
		return errors.New("sql_observation_unavailable")
	}
	if len(observed) > 1<<20 || !safeEnvelopeState(observed) {
		return errors.New("sql_observation_not_bounded_safe_state")
	}
	queryHash := sha256.Sum256([]byte(check.Query))
	r.event(map[string]any{"type": "persisted_state", "scenario": scenario, "querySha256": hex.EncodeToString(queryHash[:]), "observedAt": time.Now().UTC(), "state": json.RawMessage(observed)})
	if !sameEnvelopeJSON(observed, check.ExpectedJSON) {
		return errors.New("persisted_state_assertion_failed")
	}
	return nil
}

func (r *envelopeRunner) event(value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := json.NewEncoder(r.events).Encode(value); e != nil && r.writeError == nil {
		r.writeError = e
	}
}
func safeEnvelopeState(raw []byte) bool {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || len(object) == 0 || len(object) > 32 {
		return false
	}
	for key, value := range object {
		switch key {
		case "state", "hash", "revision", "operationId", "assetId", "objectCount", "tombstones", "actor", "wallet", "providerId", "providerState", "recordingsDeleted", "writeReady", "readReady", "scanAlert", "quarantineCount", "removedCount":
		default:
			return false
		}
		if len(value) > 4096 {
			return false
		}
	}
	return true
}
func validateEnvelopeCase(test EnvelopeCase) error {
	if test.ID == 5 || test.ID == 8 {
		return ErrConflict
	}
	for _, probe := range test.Probes {
		if probe.Target.Class != "scenario" {
			return errors.New("scenario_probes_must_not_mix_with_load_metrics")
		}
	}
	if test.ID != 2 && test.ID != 8 && len(test.SQL) == 0 {
		return errors.New("persisted_scenario_state_required")
	}
	if test.ID == 4 || test.ID == 6 || test.ID == 7 {
		if len(test.Before) == 0 || len(test.After) == 0 {
			return errors.New("actual_fault_and_recovery_operation_required")
		}
	}
	if test.ID == 1 {
		forged := false
		for _, probe := range test.Probes {
			forged = forged || probe.Target.Headers["X-Actor-Wallet"] != ""
		}
		actor := false
		for _, check := range test.SQL {
			var state map[string]json.RawMessage
			_ = json.Unmarshal(check.ExpectedJSON, &state)
			var wallet string
			if json.Unmarshal(state["actor"], &wallet) == nil && regexpWallet(wallet) {
				actor = true
			}
		}
		if !forged || !actor {
			return errors.New("forged_actor_and_authenticated_durable_actor_required")
		}
	}
	if test.ID == 2 {
		for _, probe := range test.Probes {
			if probe.Target.ExpectedStatus != 404 || probe.RequiredHeaders["Cache-Control"] != "no-store" {
				return errors.New("traversal_must_be_denied_without_public_cache")
			}
		}
	}
	if test.ID == 3 {
		allowed, denied := false, false
		for _, probe := range test.Probes {
			allowed = allowed || probe.Target.ExpectedStatus == 200 && digest.MatchString(probe.Target.ExpectedSHA256)
			denied = denied || probe.Target.ExpectedStatus == 404 && probe.RequiredHeaders["Cache-Control"] == "no-store"
		}
		if !allowed || !denied {
			return errors.New("new_immutable_content_and_old_denial_required")
		}
	}
	if test.ID == 4 {
		for _, check := range test.SQL {
			var state map[string]json.RawMessage
			if json.Unmarshal(check.ExpectedJSON, &state) != nil {
				return ErrConflict
			}
			var value string
			if json.Unmarshal(state["state"], &value) != nil || (value != "requested" && value != "accepted") {
				return errors.New("purge_timeout_must_preserve_pending_state")
			}
		}
	}
	if test.ID == 6 {
		for _, check := range test.SQL {
			var state map[string]json.RawMessage
			if json.Unmarshal(check.ExpectedJSON, &state) != nil {
				return ErrConflict
			}
			var value string
			if json.Unmarshal(state["state"], &value) != nil || value != "deleted" {
				return errors.New("restored_tombstone_must_remain_deleted")
			}
		}
	}
	if test.ID == 7 {
		quarantined, readable := false, false
		for _, probe := range test.Probes {
			quarantined = quarantined || probe.Target.ExpectedStatus == 202
			readable = readable || probe.Target.ExpectedStatus == 200 && digest.MatchString(probe.Target.ExpectedSHA256)
		}
		alert := false
		for _, check := range test.SQL {
			var state map[string]json.RawMessage
			_ = json.Unmarshal(check.ExpectedJSON, &state)
			var enabled bool
			if json.Unmarshal(state["scanAlert"], &enabled) == nil && enabled {
				alert = true
			}
		}
		if !quarantined || !readable || !alert {
			return errors.New("scan_outage_alert_quarantine_and_approved_reads_required")
		}
	}
	return nil
}

func (r *envelopeRunner) playback(ctx context.Context) error {
	if _, e := readEnvelopeFile(r.config.PlaybackBinary, 256<<20); e != nil {
		return e
	}
	var target EnvelopeTarget
	for _, candidate := range r.config.Targets {
		if candidate.ID == r.config.PlaybackTargetID {
			target = candidate
		}
	}
	program, e := filepath.Abs(r.config.PlaybackBinary.Path)
	if e != nil {
		return e
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(r.config.PlaybackSeconds+10)*time.Second)
	defer cancel()
	process := exec.CommandContext(bounded, program, "-nostdin", "-v", "error", "-tls_verify", "1", "-ca_file", r.config.TLSRootCA, "-rw_timeout", "3000000", "-i", target.URL, "-t", strconv.Itoa(r.config.PlaybackSeconds), "-f", "null", "-", "-progress", "pipe:1")
	stdout, e := process.StdoutPipe()
	if e != nil {
		return errors.New("cdn_playback_pipe_failed")
	}
	process.Stderr = io.Discard
	started := time.Now().UTC()
	if e = process.Start(); e != nil {
		return errors.New("cdn_playback_player_unavailable")
	}
	scanner := bufio.NewScanner(io.LimitReader(stdout, 1<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var frames, mediaMicros int64
	ended := false
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch parts[0] {
		case "frame":
			value, e := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			if e == nil && value > frames {
				frames = value
			}
		case "out_time_us":
			value, e := strconv.ParseInt(parts[1], 10, 64)
			if e == nil && value > mediaMicros {
				mediaMicros = value
			}
		case "progress":
			ended = parts[1] == "end"
		}
	}
	e = process.Wait()
	r.event(map[string]any{"type": "cdn_decoded_playback", "targetId": target.ID, "binarySha256": r.config.PlaybackBinary.SHA256, "startedAt": started, "endedAt": time.Now().UTC(), "frames": frames, "mediaMicros": mediaMicros, "cleanEnd": ended && e == nil})
	if e != nil || scanner.Err() != nil || !ended || frames < 1 || mediaMicros < int64(r.config.PlaybackSeconds)*1000000 {
		return errors.New("cdn_decoded_playback_incomplete")
	}
	return nil
}

func licensedEnvelopePrefix(c EnvelopeConfig, path string) ([]byte, int64, error) {
	for _, fixture := range c.Release.Fixtures {
		if fixture.Path != path {
			continue
		}
		root, e := os.OpenRoot(c.Release.FixtureRoot)
		if e != nil {
			return nil, 0, e
		}
		defer root.Close()
		file, e := root.Open(path)
		if e != nil {
			return nil, 0, e
		}
		defer file.Close()
		info, e := file.Stat()
		if e != nil || info.Size() != 6<<20 || !info.Mode().IsRegular() {
			return nil, 0, ErrConflict
		}
		actual, e := releaseFileDigest(file, 6<<20)
		if e != nil || actual != fixture.SHA256 {
			return nil, 0, ErrConflict
		}
		if _, e = file.Seek(0, io.SeekStart); e != nil {
			return nil, 0, e
		}
		prefix := make([]byte, 32768)
		if _, e = io.ReadFull(file, prefix); e != nil {
			return nil, 0, e
		}
		return prefix, info.Size(), nil
	}
	return nil, 0, errors.New("upload_fixture_provenance_missing")
}
