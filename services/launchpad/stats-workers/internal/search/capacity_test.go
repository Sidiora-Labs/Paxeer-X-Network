package search

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type capacityCase struct {
	ID                 string   `json:"id"`
	Query              string   `json:"query"`
	Kind               string   `json:"kind"`
	Category           string   `json:"category"`
	Status             int      `json:"status"`
	Expected           []string `json:"expected"`
	ExpectedLabels     []string `json:"expectedLabels"`
	ExpectedPageStatus string   `json:"expectedPageStatus"`
	ApprovedIndexes    []string `json:"approvedIndexes"`
}
type capacityConfig struct {
	Version               int               `json:"version"`
	Stage                 string            `json:"stage"`
	RunID                 string            `json:"runId"`
	Isolated              bool              `json:"isolated"`
	Origin                string            `json:"origin"`
	MetricsURL            string            `json:"metricsUrl"`
	Database              string            `json:"database"`
	SourceRoot            string            `json:"sourceRoot"`
	Sources               map[string]string `json:"sources"`
	SourceDigest          string            `json:"sourceDigest"`
	ImageDigest           string            `json:"imageDigest"`
	InterfaceDigest       string            `json:"interfaceDigest"`
	CorpusSHA256          string            `json:"corpusSha256"`
	ArtifactDir           string            `json:"artifactDir"`
	RawByteBudget         int64             `json:"rawByteBudget"`
	DurationSeconds       int64             `json:"durationSeconds"`
	RPS                   int               `json:"rps"`
	Concurrency           int               `json:"concurrency"`
	SourceIPs             []string          `json:"sourceIps"`
	LoadIPCount           int               `json:"loadIpCount"`
	HotPercent            int               `json:"hotPercent"`
	ColdPrefix            string            `json:"coldPrefix"`
	MaximumGoroutines     int               `json:"maximumGoroutines"`
	MaximumHeapBytes      uint64            `json:"maximumHeapBytes"`
	RetainedGeneration    string            `json:"retainedGeneration"`
	TombstonePools        []string          `json:"tombstonePools"`
	DatasetSeed           int64             `json:"datasetSeed"`
	Cases                 []capacityCase    `json:"cases"`
	DynamicRoutes         []string          `json:"dynamicRoutes"`
	GlobalReceipt         string            `json:"globalReceipt"`
	GlobalReceiptSHA256   string            `json:"globalReceiptSha256"`
	RollbackReceipt       string            `json:"rollbackReceipt"`
	RollbackReceiptSHA256 string            `json:"rollbackReceiptSha256"`
}
type capacityMetrics struct {
	RunID        string           `json:"runId"`
	SourceDigest string           `json:"sourceDigest"`
	ImageDigest  string           `json:"imageDigest"`
	ObservedAt   time.Time        `json:"observedAt"`
	Metrics      CacheMeasurement `json:"metrics"`
}
type capacityObservation struct {
	Sequence    int64           `json:"sequence"`
	CaseID      string          `json:"caseId"`
	Category    string          `json:"category"`
	ScheduledAt time.Time       `json:"scheduledAt"`
	StartedAt   time.Time       `json:"startedAt"`
	CompletedAt time.Time       `json:"completedAt"`
	LatencyNS   int64           `json:"latencyNs"`
	Status      int             `json:"status"`
	Valid       bool            `json:"valid"`
	Failure     string          `json:"failure,omitempty"`
	BodySHA256  string          `json:"bodySha256,omitempty"`
	Body        json.RawMessage `json:"body,omitempty"`
	RequestID   string          `json:"requestId,omitempty"`
}
type capacityRecorder struct {
	mu         sync.Mutex
	writer     *bufio.Writer
	remaining  int64
	err        error
	totals     map[string]int64
	histograms map[string][]int64
	errors     int64
}

func capacityDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func capacityFile(path string, maximum int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("measurement input must be a regular file")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maximum+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > maximum {
		return nil, errors.New("measurement input exceeds bound")
	}
	return raw, nil
}
func capacityJSON(raw []byte, value any) error { return strictJSON(raw, value) }
func capacityConfigRead() (capacityConfig, error) {
	var config capacityConfig
	path := os.Getenv("L20_MEASUREMENT_CONFIG")
	if path == "" {
		return config, errors.New("L20_MEASUREMENT_CONFIG required")
	}
	raw, err := capacityFile(path, 4<<20)
	if err != nil {
		return config, err
	}
	key, err := hex.DecodeString(os.Getenv("L20_MEASUREMENT_REVIEW_KEY"))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return config, errors.New("pinned external Ed25519 measurement review key required")
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(os.Getenv("L20_MEASUREMENT_APPROVAL_SIGNATURE"))
	if err != nil || !ed25519.Verify(ed25519.PublicKey(key), raw, signature) {
		return config, errors.New("measurement configuration approval signature rejected")
	}
	if err = capacityJSON(raw, &config); err != nil {
		return config, err
	}
	if config.Version != 1 || !config.Isolated || config.Stage != "lane" && config.Stage != "release" || config.RPS != 2000 || config.DurationSeconds < 1 || config.DurationSeconds > 86400*7 || config.Concurrency < 1 || config.Concurrency > 4096 || config.DatasetSeed < 0 {
		return config, errors.New("invalid isolated measurement workload or bounds")
	}
	if config.Stage == "lane" && config.DurationSeconds > 1800 || config.Stage == "release" && config.DurationSeconds < 86400 {
		return config, errors.New("lane run exceeds30min or release run lacks24h soak")
	}
	if _, err = uuid.Parse(config.RunID); err != nil {
		return config, err
	}
	if len(config.SourceDigest) != 64 || len(config.InterfaceDigest) != 64 || !strings.HasPrefix(config.ImageDigest, "sha256:") || len(config.ImageDigest) != 71 {
		return config, errors.New("candidate source, interface and image digests required")
	}
	if _, err = hex.DecodeString(config.SourceDigest); err != nil {
		return config, err
	}
	if _, err = hex.DecodeString(config.InterfaceDigest); err != nil {
		return config, err
	}
	if _, err = hex.DecodeString(config.ImageDigest[7:]); err != nil {
		return config, err
	}
	if !filepath.IsAbs(config.SourceRoot) || !filepath.IsAbs(config.ArtifactDir) || config.Database == "" || len(config.Cases) < 4 || len(config.Cases) > 10000 {
		return config, errors.New("absolute source/artifact paths, actual database and approved cases required")
	}
	for _, target := range []string{config.Origin, config.MetricsURL} {
		u, e := url.Parse(target)
		if e != nil || u.Host == "" || u.User != nil || u.Scheme != "https" && u.Scheme != "http" || u.RawQuery != "" || u.Fragment != "" {
			return config, errors.New("explicit approved HTTP origin and operational metrics URL required")
		}
	}
	if config.MaximumGoroutines < 1 || config.MaximumHeapBytes < 1 {
		return config, errors.New("reviewed service memory and goroutine bounds required")
	}
	if config.Stage == "release" && len(config.DynamicRoutes) < 2 {
		return config, errors.New("reviewed mixed dynamic route set required")
	}
	for _, route := range config.DynamicRoutes {
		if !strings.HasPrefix(route, "/") || strings.HasPrefix(route, "/health") || route == "/metrics" {
			return config, errors.New("mixed workload includes a nondynamic route")
		}
	}
	if config.Stage == "release" && (config.RetainedGeneration == "" || len(config.TombstonePools) == 0) {
		return config, errors.New("actual retained rollback generation and current tombstone resources required")
	}
	for _, pool := range config.TombstonePools {
		if !exactIdentity(pool) {
			return config, ErrInvalid
		}
	}
	if config.HotPercent < 1 || config.HotPercent > 99 || len(config.ColdPrefix) < 10 || len(config.ColdPrefix) > 18 || !strings.HasPrefix(config.ColdPrefix, "0x") || strings.ToLower(config.ColdPrefix) != config.ColdPrefix {
		return config, errors.New("approved hot distribution and cold address prefix required")
	}
	if _, err = hex.DecodeString(config.ColdPrefix[2:]); err != nil {
		return config, err
	}
	if config.LoadIPCount < 1201 || len(config.SourceIPs) < config.LoadIPCount+(len(config.Cases)+199)/100+1 || len(config.SourceIPs) > 4096 {
		return config, errors.New("actual source IP topology must respect the100/min global and120/min search admission limits")
	}
	ipSeen := map[string]bool{}
	for _, sourceIP := range config.SourceIPs {
		ip := net.ParseIP(sourceIP)
		if ip == nil || ipSeen[ip.String()] {
			return config, errors.New("source IP topology contains invalid or duplicate identity")
		}
		ipSeen[ip.String()] = true
	}
	if config.RawByteBudget < config.DurationSeconds*int64(config.RPS)*1024 {
		return config, errors.New("raw observation storage budget insufficient")
	}
	seen := map[string]bool{}
	categories := map[string]int{}
	for _, c := range config.Cases {
		if c.ID == "" || len(c.ID) > 128 || seen[c.ID] || c.Expected == nil || len(c.ExpectedLabels) != len(c.Expected) || c.Status < 200 || c.Status > 599 {
			return config, errors.New("ambiguous or incomplete approved quality case")
		}
		seen[c.ID] = true
		if c.Category != "exact_address" && c.Category != "ticker" && c.Category != "name" && c.Category != "hot_ticker" && c.Category != "cache_miss" && c.Category != "malformed" && c.Category != "profile" {
			return config, errors.New("unknown workload category")
		}
		categories[c.Category]++
		if c.Status == 200 {
			if _, e := (IntegratedRequest{Request: Request{Query: c.Query, Kind: c.Kind, Limit: 25}, Sort: "address"}).Validate(); e != nil {
				return config, e
			}
			if c.ExpectedPageStatus != "ok" && c.ExpectedPageStatus != "not-indexed" {
				return config, ErrInvalid
			}
		}
	}
	for _, category := range []string{"exact_address", "ticker", "name", "hot_ticker", "cache_miss"} {
		if categories[category] == 0 {
			return config, errors.New("address, ticker, name, hot and cold query coverage required")
		}
	}
	if categories["cache_miss"] < 1000 {
		return config, errors.New("at least1000 distinct approved cold queries required")
	}
	return config, nil
}
func capacityBind(config capacityConfig) error {
	keys := make([]string, 0, len(config.Sources))
	for path := range config.Sources {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return errors.New("source manifest absent")
	}
	hash := sha256.New()
	for _, name := range keys {
		if filepath.IsAbs(name) || filepath.Clean(name) != name || strings.HasPrefix(name, "../") {
			return errors.New("source manifest path escapes candidate")
		}
		absolute := filepath.Join(config.SourceRoot, name)
		resolved, err := filepath.EvalSymlinks(absolute)
		if err != nil || resolved != absolute {
			return errors.New("candidate source cannot be a symlink")
		}
		raw, err := capacityFile(absolute, 32<<20)
		if err != nil {
			return err
		}
		digest := capacityDigest(raw)
		if digest != config.Sources[name] {
			return fmt.Errorf("candidate source changed: %s", name)
		}
		fmt.Fprintf(hash, "%s\x00%s\x00", name, digest)
	}
	if hex.EncodeToString(hash.Sum(nil)) != config.SourceDigest {
		return errors.New("source manifest digest mismatch")
	}
	for _, name := range []string{"services/launchpad/stats-workers/internal/search/cache.go", "services/launchpad/stats-workers/internal/search/integrated.go", "services/launchpad/stats-workers/internal/search/generation.go", "services/launchpad/stats-workers/internal/search/projection.go", "services/launchpad/stats-workers/internal/search/capacity_test.go", "services/launchpad/stats-workers/internal/httpapi/search.go", "services/launchpad/stats-workers/internal/app/app.go", "services/launchpad/stats-workers/go.mod", "services/launchpad/stats-workers/go.sum", "sdk/launchpad/shared/go.mod", "sdk/launchpad/shared/go.sum"} {
		if config.Sources[name] == "" {
			return errors.New("candidate manifest omits a required search runtime source")
		}
	}
	entries, err := os.ReadDir(filepath.Join(config.SourceRoot, "services/launchpad/stats-workers/internal/search"))
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), ".sql") || strings.HasSuffix(entry.Name(), ".json") {
			if config.Sources["services/launchpad/stats-workers/internal/search/"+entry.Name()] == "" {
				return errors.New("candidate manifest omits an owned search source")
			}
		}
	}
	corpus, err := capacityFile(filepath.Join(config.SourceRoot, "services/launchpad/stats-workers/internal/search/quality-corpus.json"), 1<<20)
	if err != nil {
		return err
	}
	if capacityDigest(corpus) != config.CorpusSHA256 {
		return errors.New("built-in multilingual corpus changed")
	}
	return nil
}
func (r *capacityRecorder) record(value capacityObservation) {
	raw, err := json.Marshal(value)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return
	}
	if err != nil {
		r.err = err
		return
	}
	if int64(len(raw)+1) > r.remaining {
		r.err = errors.New("raw evidence byte budget exhausted")
		return
	}
	r.remaining -= int64(len(raw) + 1)
	if _, err = r.writer.Write(append(raw, '\n')); err != nil {
		r.err = err
		return
	}
	r.totals[value.Category]++
	if !value.Valid {
		r.errors++
	}
	bucket := (value.LatencyNS + int64(time.Millisecond) - 1) / int64(time.Millisecond)
	if bucket < 0 {
		bucket = 0
	}
	if bucket > 2000 {
		bucket = 2001
	}
	if r.histograms[value.Category] == nil {
		r.histograms[value.Category] = make([]int64, 2002)
	}
	r.histograms[value.Category][bucket]++
}
func capacityRequest(ctx context.Context, client *http.Client, config capacityConfig, c capacityCase, sequence int64, scheduled time.Time) capacityObservation {
	value := capacityObservation{Sequence: sequence, CaseID: c.ID, Category: c.Category, ScheduledAt: scheduled, StartedAt: time.Now().UTC()}
	target := strings.TrimRight(config.Origin, "/") + "/v2/search/expanded?" + url.Values{"q": {c.Query}, "type": {c.Kind}, "limit": {"25"}, "sort": {"address"}}.Encode()
	request, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err == nil {
		request.Header.Set("X-Request-ID", uuid.NewString())
	}
	var response *http.Response
	if err == nil {
		response, err = client.Do(request)
	}
	var raw []byte
	if err == nil {
		value.Status = response.StatusCode
		value.RequestID = response.Header.Get("X-Request-ID")
		raw, err = io.ReadAll(io.LimitReader(response.Body, 131073))
		response.Body.Close()
		if len(raw) > 131072 {
			err = errors.New("search response byte limit")
		}
		value.BodySHA256 = capacityDigest(raw)
		if _, idErr := uuid.Parse(value.RequestID); idErr != nil && err == nil {
			err = errors.New("actual response request identity invalid")
		}
		if response.Header.Get("X-Schema-Version") != "2" && err == nil {
			err = errors.New("actual response schema version mismatch")
		}
	}
	if err == nil && value.Status != c.Status {
		err = errors.New("unexpected HTTP status")
	}
	if err == nil && c.Status == 200 {
		var envelope struct {
			RequestID   string          `json:"requestId"`
			Data        IntegratedPage  `json:"data"`
			Watermark   json.RawMessage `json:"watermark"`
			Revision    string          `json:"revision"`
			Quality     string          `json:"quality"`
			GeneratedAt string          `json:"generatedAt"`
		}
		if err = capacityJSON(raw, &envelope); err == nil {
			query, _ := Normalize(c.Query)
			if envelope.Revision != strconv.FormatInt(envelope.Data.Revision, 10) || envelope.Quality != "partial" || envelope.Data.MatchKind != query.Kind || envelope.RequestID != value.RequestID || envelope.Data.Items == nil || envelope.Data.Status != c.ExpectedPageStatus || len(envelope.Data.Items) != len(c.Expected) || envelope.Data.Generation == "" || envelope.Data.Revision < 1 {
				err = errors.New("search success envelope or expected set mismatch")
			}
			for index, hit := range envelope.Data.Items {
				if index >= len(c.Expected) || hit.ID != c.Expected[index] || hit.QuoteExecutable || index >= len(c.ExpectedLabels) || hit.Label != c.ExpectedLabels[index] {
					err = errors.New("canonical result order or executable disclosure mismatch")
					break
				}
			}
			if strings.Contains(string(raw), "<script>") || strings.Contains(string(raw), "<img") {
				err = errors.New("hostile markup not JSON escaped")
			}
		}
	} else if err == nil {
		var failure struct {
			Code         string            `json:"code"`
			Message      string            `json:"message"`
			RequestID    string            `json:"requestId"`
			Retryable    bool              `json:"retryable"`
			RetryAfterMS *int              `json:"retryAfterMs"`
			FieldErrors  []json.RawMessage `json:"fieldErrors"`
			Details      json.RawMessage   `json:"details"`
		}
		if err = capacityJSON(raw, &failure); err == nil && (failure.Code == "" || strings.TrimSpace(failure.Message) == "" || failure.RequestID != value.RequestID) {
			err = errors.New("error lacks accessible bounded explanation")
		}
		if err == nil && (c.Status == 429 || c.Status == 503) && response.Header.Get("Retry-After") == "" {
			err = errors.New("retryable failure lacks Retry-After")
		}
	}
	if sequence < 0 || err != nil {
		value.Body = raw
	}
	value.CompletedAt = time.Now().UTC()
	value.LatencyNS = value.CompletedAt.Sub(scheduled).Nanoseconds()
	value.Valid = err == nil
	if err != nil {
		value.Failure = "transport_or_quality_failure"
	}
	return value
}
func capacityFetchMetrics(ctx context.Context, client *http.Client, config capacityConfig) (capacityMetrics, []byte, error) {
	var metrics capacityMetrics
	request, err := http.NewRequestWithContext(ctx, "GET", config.MetricsURL, nil)
	if err != nil {
		return metrics, nil, err
	}
	if token := os.Getenv("L20_MEASUREMENT_OPERATIONS_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	} else if os.Getenv("L20_MEASUREMENT_CLIENT_CERT") == "" {
		return metrics, nil, errors.New("actual operational bearer or mTLS credential required")
	}
	response, err := client.Do(request)
	if err != nil {
		return metrics, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return metrics, nil, err
	}
	if response.StatusCode != 200 || len(raw) > 65536 || capacityJSON(raw, &metrics) != nil {
		return metrics, raw, errors.New("actual operational cache measurements unavailable")
	}
	if metrics.RunID != config.RunID || metrics.SourceDigest != config.SourceDigest || metrics.ImageDigest != config.ImageDigest || metrics.ObservedAt.IsZero() || time.Since(metrics.ObservedAt) > 5*time.Second || metrics.ObservedAt.After(time.Now().Add(time.Second)) {
		return metrics, raw, errors.New("operational measurement candidate or freshness mismatch")
	}
	return metrics, raw, nil
}
func capacityPercentile(buckets []int64, numerator int) int64 {
	var count int64
	for _, n := range buckets {
		count += n
	}
	if count == 0 {
		return -1
	}
	rank := (count*int64(numerator) + 99) / 100
	var cumulative int64
	for index, n := range buckets {
		cumulative += n
		if cumulative >= rank {
			return int64(index)
		}
	}
	return -1
}
func capacityPlanIndexes(raw json.RawMessage, approved []string) error {
	var plans []struct {
		Plan map[string]any `json:"Plan"`
	}
	if json.Unmarshal(raw, &plans) != nil || len(plans) != 1 {
		return errors.New("invalid actual PostgreSQL plan")
	}
	indexed := false
	var walk func(map[string]any) error
	walk = func(node map[string]any) error {
		if relation, _ := node["Relation Name"].(string); relation == "documents_v2" {
			if typ, _ := node["Node Type"].(string); strings.Contains(typ, "Seq Scan") {
				return errors.New("million-document search used a sequential document scan")
			}
		}
		if name, _ := node["Index Name"].(string); name != "" {
			for _, expected := range approved {
				if name == expected {
					indexed = true
				}
			}
		}
		if children, ok := node["Plans"].([]any); ok {
			for _, child := range children {
				nested, ok := child.(map[string]any)
				if !ok {
					return errors.New("invalid plan child")
				}
				if err := walk(nested); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(plans[0].Plan); err != nil {
		return err
	}
	if !indexed {
		return errors.New("approved search index absent from actual plan")
	}
	return nil
}

type capacityReceipt struct {
	Version         int       `json:"version"`
	RunID           string    `json:"runId"`
	SourceDigest    string    `json:"sourceDigest"`
	ImageDigest     string    `json:"imageDigest"`
	InterfaceDigest string    `json:"interfaceDigest"`
	RawPath         string    `json:"rawPath"`
	RawSHA256       string    `json:"rawSha256"`
	StartedAt       time.Time `json:"startedAt"`
	EndedAt         time.Time `json:"endedAt"`
}

func capacityReadReceipt(path, expected string, config capacityConfig) (capacityReceipt, error) {
	var receipt capacityReceipt
	raw, err := capacityFile(path, 1<<20)
	if err != nil {
		return receipt, err
	}
	if expected != "" && capacityDigest(raw) != expected {
		return receipt, errors.New("receipt hash mismatch")
	}
	signature, err := capacityFile(path+".sig", 4096)
	if err != nil {
		return receipt, err
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(signature)))
	if err != nil {
		return receipt, err
	}
	key, err := hex.DecodeString(os.Getenv("L20_MEASUREMENT_REVIEW_KEY"))
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(ed25519.PublicKey(key), raw, decoded) {
		return receipt, errors.New("external evidence receipt signature rejected")
	}
	if capacityJSON(raw, &receipt) != nil || receipt.Version != 1 || receipt.RunID != config.RunID || receipt.SourceDigest != config.SourceDigest || receipt.ImageDigest != config.ImageDigest || receipt.InterfaceDigest != config.InterfaceDigest || !receipt.EndedAt.After(receipt.StartedAt) {
		return receipt, errors.New("actual receipt candidate or interval mismatch")
	}
	return receipt, nil
}
func capacityGlobalReceipt(config capacityConfig, start, end time.Time) error {
	receipt, err := capacityReadReceipt(config.GlobalReceipt, config.GlobalReceiptSHA256, config)
	if err != nil {
		return err
	}
	if receipt.StartedAt.After(start) || receipt.EndedAt.Before(end) || receipt.EndedAt.Sub(receipt.StartedAt) < 24*time.Hour {
		return errors.New("global mixed run does not cover search soak")
	}
	file, err := os.Open(receipt.RawPath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(file, hash))
	scanner.Buffer(make([]byte, 65536), 1<<20)
	seconds := make([]int64, config.DurationSeconds)
	searchSeconds := make([]int64, config.DurationSeconds)
	var sequence int64
	dynamic := map[string]bool{}
	for _, route := range config.DynamicRoutes {
		dynamic[route] = true
	}
	for scanner.Scan() {
		var sample struct {
			Sequence    int64     `json:"sequence"`
			StartedAt   time.Time `json:"startedAt"`
			CompletedAt time.Time `json:"completedAt"`
			Status      int       `json:"status"`
			Route       string    `json:"route"`
		}
		if capacityJSON(scanner.Bytes(), &sample) != nil || sample.Sequence != sequence+1 || sample.StartedAt.IsZero() || sample.CompletedAt.Before(sample.StartedAt) || sample.Route == "" {
			return errors.New("global raw sample missing identity, actual timing or route")
		}
		sequence++
		if sample.StartedAt.Before(start) || !sample.StartedAt.Before(end) {
			continue
		}
		if sample.Status >= 200 && sample.Status < 300 && dynamic[sample.Route] {
			second := int64(sample.StartedAt.Sub(start) / time.Second)
			if second >= 0 && second < int64(len(seconds)) {
				seconds[second]++
				if sample.Route == "/v2/search/expanded" {
					searchSeconds[second]++
				}
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != receipt.RawSHA256 {
		return errors.New("global raw observation digest mismatch")
	}
	for index, count := range seconds {
		if count < 10000 || searchSeconds[index] < 2000 {
			return errors.New("actual mixed dynamic/search rates below10000/2000RPS")
		}
	}
	return nil
}
func capacityRollbackReceipt(config capacityConfig) error {
	receipt, err := capacityReadReceipt(config.RollbackReceipt, config.RollbackReceiptSHA256, config)
	if err != nil {
		return err
	}
	file, err := os.Open(receipt.RawPath)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(file, hash))
	scanner.Buffer(make([]byte, 65536), 1<<20)
	var sequence int64
	before, after := "", ""
	tombstoneChecked := map[string]bool{}
	for scanner.Scan() {
		var sample struct {
			Sequence   int64           `json:"sequence"`
			ObservedAt time.Time       `json:"observedAt"`
			Action     string          `json:"action"`
			Generation string          `json:"generation"`
			Query      string          `json:"query"`
			ExitCode   int             `json:"exitCode"`
			Body       json.RawMessage `json:"body"`
		}
		if capacityJSON(scanner.Bytes(), &sample) != nil || sample.Sequence != sequence+1 || sample.ObservedAt.Before(receipt.StartedAt) || sample.ObservedAt.After(receipt.EndedAt) || sample.ExitCode != 0 {
			return errors.New("rollback observation identity, interval or executable result failed")
		}
		sequence++
		switch sample.Action {
		case "before":
			before = sample.Generation
		case "after":
			after = sample.Generation
		case "tombstone_search":
			approved := false
			for _, pool := range config.TombstonePools {
				if sample.Query == pool {
					approved = true
				}
			}
			if !approved {
				return errors.New("rollback resource is absent from reviewed tombstone set")
			}
			var response struct {
				RequestID   string          `json:"requestId"`
				Data        IntegratedPage  `json:"data"`
				Watermark   json.RawMessage `json:"watermark"`
				Revision    string          `json:"revision"`
				Quality     string          `json:"quality"`
				GeneratedAt string          `json:"generatedAt"`
			}
			if capacityJSON(sample.Body, &response) != nil || response.RequestID == "" || response.Data.Generation != after || response.Data.Items == nil || len(response.Data.Items) != 0 || response.Data.Status != "not-indexed" {
				return errors.New("rollback raw response resurrected tombstone or returned invalid empty success")
			}
			tombstoneChecked[sample.Query] = true
		default:
			return errors.New("unknown rollback observation")
		}
	}
	if err = scanner.Err(); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != receipt.RawSHA256 || before == "" || after == "" || before == after || after != config.RetainedGeneration || len(tombstoneChecked) != len(config.TombstonePools) {
		return errors.New("current-candidate rollback observations incomplete")
	}
	return nil
}
func capacityCapture(path string, value any) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	err = json.NewEncoder(file).Encode(value)
	syncErr := file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
func capacityBuiltinCases(config capacityConfig) ([]capacityCase, error) {
	raw, err := capacityFile(filepath.Join(config.SourceRoot, "services/launchpad/stats-workers/internal/search/quality-corpus.json"), 1<<20)
	if err != nil {
		return nil, err
	}
	var corpus struct {
		Version   int `json:"version"`
		Documents []struct {
			Number int    `json:"number"`
			Name   string `json:"name"`
		} `json:"documents"`
		Profiles []struct {
			Number      int    `json:"number"`
			DisplayName string `json:"displayName"`
		} `json:"profiles"`
		Cases []struct {
			Query    string `json:"query"`
			Expected []int  `json:"expected"`
		} `json:"cases"`
		ProfileCases []struct {
			Query    string `json:"query"`
			Expected []int  `json:"expected"`
		} `json:"profileCases"`
	}
	if json.Unmarshal(raw, &corpus) != nil || corpus.Version != 2 || len(corpus.Cases) == 0 || len(corpus.ProfileCases) == 0 {
		return nil, errors.New("complete built-in corpus required")
	}
	labels := map[int]string{}
	for _, d := range corpus.Documents {
		labels[d.Number] = d.Name
	}
	for _, p := range corpus.Profiles {
		labels[p.Number] = p.DisplayName
	}
	cases := []capacityCase{}
	for _, family := range []struct {
		kind  string
		cases []struct {
			Query    string `json:"query"`
			Expected []int  `json:"expected"`
		}
	}{{"pool", corpus.Cases}, {"profile", corpus.ProfileCases}} {
		for _, source := range family.cases {
			expected := []string{}
			expectedLabels := []string{}
			for _, number := range source.Expected {
				expected = append(expected, "125:"+fixtureAddress(number))
				label, ok := labels[number]
				if !ok {
					return nil, errors.New("expected corpus label absent")
				}
				expectedLabels = append(expectedLabels, label)
			}
			status := "ok"
			query, err := Normalize(source.Query)
			if err != nil {
				return nil, err
			}
			if query.Kind == "exact_address" && len(expected) == 0 {
				status = "not-indexed"
			}
			cases = append(cases, capacityCase{ID: fmt.Sprintf("builtin_%s_%d", family.kind, len(cases)), Query: source.Query, Kind: family.kind, Category: "quality", Status: 200, Expected: expected, ExpectedLabels: expectedLabels, ExpectedPageStatus: status})
		}
	}
	return cases, nil
}
func TestSearchCapacityReleaseMeasurement(t *testing.T) {
	config, err := capacityConfigRead()
	if err != nil {
		t.Fatal(err)
	}
	if err = capacityBind(config); err != nil {
		t.Fatal(err)
	}
	dsn := os.Getenv("L20_MEASUREMENT_DSN")
	if dsn == "" {
		t.Fatal("actual approved L20_MEASUREMENT_DSN required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("measurement DSN configuration invalid")
	}
	poolConfig.MaxConns = 2
	poolConfig.ConnConfig.ConnectTimeout = 2 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("actual measurement PostgreSQL unavailable")
	}
	defer pool.Close()
	var database, generation string
	var documents int64
	bounded, release := context.WithTimeout(ctx, 30*time.Second)
	err = pool.QueryRow(bounded, "SELECT current_database(),generation,(SELECT count(*) FROM stats_search.documents_v2 d WHERE d.generation=a.generation) FROM stats_search.active_v2 a WHERE singleton").Scan(&database, &generation, &documents)
	release()
	if err != nil || database != config.Database || documents < 1000000 {
		t.Fatal("actual approved active million-document dataset unavailable")
	}
	if err = os.MkdirAll(config.ArtifactDir, 0700); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(config.ArtifactDir, config.RunID)
	if err = os.Mkdir(artifactPath, 0700); err != nil {
		t.Fatal(err)
	}
	rawFile, err := os.OpenFile(filepath.Join(artifactPath, "requests.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	rawHash := sha256.New()
	recorder := &capacityRecorder{writer: bufio.NewWriterSize(io.MultiWriter(rawFile, rawHash), 1<<20), remaining: config.RawByteBudget, totals: map[string]int64{}, histograms: map[string][]int64{}}
	defer rawFile.Close()
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13}
	certPath, keyPath := os.Getenv("L20_MEASUREMENT_CLIENT_CERT"), os.Getenv("L20_MEASUREMENT_CLIENT_KEY")
	if certPath != "" || keyPath != "" {
		certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			t.Fatal("actual operational client certificate unavailable")
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	if caPath := os.Getenv("L20_MEASUREMENT_CA"); caPath != "" {
		raw, err := capacityFile(caPath, 1<<20)
		if err != nil {
			t.Fatal(err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			t.Fatal(err)
		}
		if !roots.AppendCertsFromPEM(raw) {
			t.Fatal("actual operational CA bundle invalid")
		}
		tlsConfig.RootCAs = roots
	}
	clients := make([]*http.Client, 0, len(config.SourceIPs))
	for _, sourceIP := range config.SourceIPs {
		dialer := &net.Dialer{Timeout: time.Second, LocalAddr: &net.TCPAddr{IP: net.ParseIP(sourceIP)}}
		transport := &http.Transport{DialContext: dialer.DialContext, TLSClientConfig: tlsConfig.Clone(), MaxIdleConns: 4, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: time.Second, ResponseHeaderTimeout: time.Second}
		defer transport.CloseIdleConnections()
		clients = append(clients, &http.Client{Transport: transport, Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("measurement origin redirect rejected") }})
	}
	client := clients[len(clients)-1]
	var occupied bool
	prefix := config.ColdPrefix
	bounded, release = context.WithTimeout(ctx, 400*time.Millisecond)
	err = pool.QueryRow(bounded, `SELECT EXISTS(SELECT 1 FROM stats_search.source_documents_v2 WHERE pool_address>=$1 AND pool_address<$2) OR EXISTS(SELECT 1 FROM stats_search.documents_v2 WHERE generation=$3 AND token_address>=$1 AND token_address<$2)`, prefix, prefix+"~", generation).Scan(&occupied)
	release()
	if err != nil || occupied {
		t.Fatal("approved cold-address keyspace is unavailable or already indexed")
	}

	builtin, err := capacityBuiltinCases(config)
	if err != nil {
		t.Fatal(err)
	}
	for index, c := range append(builtin, config.Cases...) {
		observation := capacityRequest(ctx, clients[config.LoadIPCount+index%(len(clients)-config.LoadIPCount-1)], config, c, -int64(index+1), time.Now().UTC())
		if err = capacityCapture(filepath.Join(artifactPath, fmt.Sprintf("quality-%06d.json", index)), observation); err != nil {
			t.Fatal(err)
		}
		if !observation.Valid {
			t.Fatal("actual quality corpus failed", c.ID, observation)
		}
	}
	plans := []PlanMeasurement{}
	measured := map[string]bool{}
	for _, c := range config.Cases {
		if c.Category != "exact_address" && c.Category != "ticker" && c.Category != "name" && c.Category != "hot_ticker" || measured[c.Category] {
			continue
		}
		if len(c.ApprovedIndexes) == 0 {
			t.Fatal("approved plan indexes missing", c.ID)
		}
		plan, err := (Store{Pool: pool}).MeasurePlan(ctx, IntegratedRequest{Request: Request{Query: c.Query, Kind: c.Kind, Limit: 25}, Sort: "address"})
		if err != nil {
			t.Fatal("actual indexed plan unavailable", c.ID, err)
		}
		plans = append(plans, plan)
		if err = capacityCapture(filepath.Join(artifactPath, "plan-"+c.Category+".json"), plan); err != nil {
			t.Fatal(err)
		}
		if err = capacityPlanIndexes(plan.Plan, c.ApprovedIndexes); err != nil {
			t.Fatal(c.ID, err)
		}
		measured[c.Category] = true
	}
	initial, initialRaw, err := capacityFetchMetrics(ctx, client, config)
	if err != nil {
		t.Fatal(err)
	}
	if err = capacityCapture(filepath.Join(artifactPath, "cache-before.json"), json.RawMessage(initialRaw)); err != nil {
		t.Fatal(err)
	}
	workload := []capacityCase{}
	for _, c := range config.Cases {
		if c.Status == 200 && c.Category == "hot_ticker" {
			workload = append(workload, c)
		}
	}
	if len(workload) == 0 {
		t.Fatal("approved hot workload absent")
	}
	start := time.Now().UTC()
	finish := start.Add(time.Duration(config.DurationSeconds) * time.Second)
	slots := make(chan struct{}, config.Concurrency)
	var workers sync.WaitGroup
	var interrupted error
	observers := make(chan error, 1)
	observerDone := make(chan struct{})
	go func() {
		defer close(observerDone)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		index := 0
		for {
			select {
			case <-ctx.Done():
				return
			case observed := <-ticker.C:
				if observed.After(finish) {
					return
				}
				snapshot, raw, err := capacityFetchMetrics(ctx, client, config)
				var maximum, connections int64
				if err == nil {
					bounded, release := context.WithTimeout(ctx, 150*time.Millisecond)
					err = pool.QueryRow(bounded, "SELECT current_setting('max_connections')::bigint,(SELECT count(*) FROM pg_stat_activity)").Scan(&maximum, &connections)
					release()
				}
				if err == nil && (snapshot.Metrics.Goroutines < 1 || snapshot.Metrics.Goroutines > config.MaximumGoroutines || snapshot.Metrics.HeapAllocBytes > config.MaximumHeapBytes) {
					err = errors.New("actual service memory or goroutine budget exceeded")
				}
				if err == nil && (maximum < 1 || connections*100 > maximum*80) {
					err = errors.New("actual database connection budget exceeds80percent")
				}
				if err == nil {
					err = capacityCapture(filepath.Join(artifactPath, fmt.Sprintf("telemetry-%06d.json", index)), map[string]any{"observedAt": observed, "cache": json.RawMessage(raw), "databaseConnections": connections, "databaseMaximum": maximum, "activeFills": snapshot.Metrics.ActiveFills, "waiters": snapshot.Metrics.Waiters})
				}
				index++
				if err != nil {
					select {
					case observers <- err:
					default:
					}
					return
				}
			}
		}
	}()
	total := config.DurationSeconds * int64(config.RPS)
scheduleLoop:
	for sequence := int64(1); sequence <= total; sequence++ {
		select {
		case interrupted = <-observers:
			break scheduleLoop
		default:
		}
		recorder.mu.Lock()
		interrupted = recorder.err
		recorder.mu.Unlock()
		if interrupted != nil {
			break
		}
		due := start.Add(time.Duration(sequence-1) * time.Second / time.Duration(config.RPS))
		if delay := time.Until(due); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
			}
		}
		c := workload[(sequence%int64(len(workload))+config.DatasetSeed%int64(len(workload)))%int64(len(workload))]
		if sequence%100 >= int64(config.HotPercent) {
			suffix := capacityDigest([]byte(fmt.Sprintf("%s:%d:%d", config.RunID, config.DatasetSeed, sequence)))
			c = capacityCase{ID: "cold_" + fmt.Sprint(sequence), Query: config.ColdPrefix + suffix[:42-len(config.ColdPrefix)], Kind: "pool", Category: "cache_miss", Status: 200, Expected: []string{}, ExpectedPageStatus: "not-indexed"}
		}
		workloadClient := clients[(sequence-1)%int64(config.LoadIPCount)]
		select {
		case slots <- struct{}{}:
			workers.Add(1)
			go func(c capacityCase, sequence int64, due time.Time) {
				defer workers.Done()
				defer func() { <-slots }()
				recorder.record(capacityRequest(ctx, workloadClient, config, c, sequence, due))
			}(c, sequence, due)
		default:
			recorder.record(capacityObservation{Sequence: sequence, CaseID: c.ID, Category: c.Category, ScheduledAt: due, StartedAt: time.Now().UTC(), CompletedAt: time.Now().UTC(), LatencyNS: time.Since(due).Nanoseconds(), Failure: "generator_concurrency_exhausted"})
		}
	}
	workers.Wait()
	end := time.Now().UTC()
	cancel()
	<-observerDone
	recorder.mu.Lock()
	flushErr := recorder.writer.Flush()
	recordErr := recorder.err
	recorder.mu.Unlock()
	if err = rawFile.Sync(); err != nil {
		t.Fatal(err)
	}
	if flushErr != nil {
		t.Fatal(flushErr)
	}
	if recordErr != nil {
		t.Fatal(recordErr)
	}
	finalCtx, finalCancel := context.WithTimeout(context.Background(), time.Second)
	defer finalCancel()
	final, finalRaw, metricErr := capacityFetchMetrics(finalCtx, client, config)
	if metricErr == nil {
		metricErr = capacityCapture(filepath.Join(artifactPath, "cache-after.json"), json.RawMessage(finalRaw))
	}
	p95, p99 := map[string]int64{}, map[string]int64{}
	for category, buckets := range recorder.histograms {
		p95[category] = capacityPercentile(buckets, 95)
		p99[category] = capacityPercentile(buckets, 99)
	}
	failures := []string{}
	if interrupted != nil {
		failures = append(failures, interrupted.Error())
	}
	if recorder.errors != 0 {
		failures = append(failures, "actual transport, admission or quality errors")
	}
	observedTotal := int64(0)
	for _, n := range recorder.totals {
		observedTotal += n
	}
	if observedTotal != total {
		failures = append(failures, "actual scheduled observation count incomplete")
	}
	for _, category := range []string{"hot_ticker", "cache_miss"} {
		if recorder.totals[category] == 0 || p95[category] > 150 || p99[category] > 400 {
			failures = append(failures, "actual origin latency SLO failed: "+category)
		}
	}
	if end.Sub(start) < time.Duration(config.DurationSeconds)*time.Second-time.Second/time.Duration(config.RPS) {
		failures = append(failures, "actual run interval incomplete")
	}
	if err = capacityBind(config); err != nil {
		failures = append(failures, err.Error())
	}
	if metricErr != nil {
		failures = append(failures, "actual final cache metrics unavailable")
	}
	if final.Metrics.Requests < initial.Metrics.Requests || final.Metrics.Hits < initial.Metrics.Hits || final.Metrics.Requests == initial.Metrics.Requests {
		failures = append(failures, "cache counters reset or observed no workload")
	}
	select {
	case err := <-observers:
		failures = append(failures, err.Error())
	default:
	}
	if config.Stage == "release" {
		if err = capacityGlobalReceipt(config, start, finish); err != nil {
			failures = append(failures, err.Error())
		}
		if err = capacityRollbackReceipt(config); err != nil {
			failures = append(failures, err.Error())
		}
	}
	cacheRequests := uint64(0)
	cacheHits := uint64(0)
	if final.Metrics.Requests >= initial.Metrics.Requests && final.Metrics.Hits >= initial.Metrics.Hits {
		cacheRequests = final.Metrics.Requests - initial.Metrics.Requests
		cacheHits = final.Metrics.Hits - initial.Metrics.Hits
	}
	var hitRatio *float64
	if cacheRequests > 0 {
		ratio := float64(cacheHits) / float64(cacheRequests)
		hitRatio = &ratio
	}
	result := map[string]any{"version": 1, "runId": config.RunID, "sourceDigest": config.SourceDigest, "imageDigest": config.ImageDigest, "interfaceDigest": config.InterfaceDigest, "corpusSha256": config.CorpusSHA256, "datasetSeed": config.DatasetSeed, "documents": documents, "generation": generation, "startedAt": start, "endedAt": end, "scheduledRps": config.RPS, "sourceIpCount": len(config.SourceIPs), "hotPercent": config.HotPercent, "actualRequests": observedTotal, "actualErrors": recorder.errors, "histogramMillisecondBuckets": recorder.histograms, "p95Ms": p95, "p99Ms": p99, "rawSha256": hex.EncodeToString(rawHash.Sum(nil)), "cacheRequests": cacheRequests, "cacheHits": cacheHits, "cacheHitRatio": hitRatio, "failures": failures, "releaseMeasurementPassed": config.Stage == "release" && len(failures) == 0, "productionQualified": false, "taskQualified": false, "sharedAcceptanceDeferred": config.Stage == "lane"}
	if err = capacityCapture(filepath.Join(artifactPath, "measurement.json"), result); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Fatal("measurement failed; actual raw evidence retained", filepath.Join(artifactPath, "measurement.json"), failures)
	}
	t.Log(filepath.Join(artifactPath, "measurement.json"))
}
