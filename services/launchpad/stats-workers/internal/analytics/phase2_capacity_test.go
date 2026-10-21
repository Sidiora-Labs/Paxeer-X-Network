package analytics_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/risk"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/accounting"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/auth"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type capacityRequest struct {
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	Body         string            `json:"body"`
	Headers      map[string]string `json:"headers"`
	Status       int               `json:"status"`
	ReplayAttack bool              `json:"replayAttack"`
}

func TestPhase2AdmittedOriginCapacity(t *testing.T) {
	base := os.Getenv("L15_CAPACITY_ORIGIN")
	path := os.Getenv("L15_CAPACITY_WORKLOAD_FILE")
	if base == "" || path == "" {
		t.Fatal("admitted origin and genuine signed L15 capacity workload required")
	}
	origin, err := url.Parse(base)
	if err != nil || origin.Scheme != "http" || net.ParseIP(origin.Hostname()) == nil || !net.ParseIP(origin.Hostname()).IsLoopback() || origin.User != nil || origin.RawQuery != "" || origin.Path != "" {
		t.Fatal("capacity origin must be the admitted loopback fixture")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var requests []capacityRequest
	if err = json.Unmarshal(raw, &requests); err != nil || len(requests) < 2 || len(requests) > 1000 {
		t.Fatal("invalid real capacity workload", err)
	}
	hasRead, hasReplay := false, false
	writes := 0
	hotPool := ""
	for _, r := range requests {
		parts := strings.Split(r.Path, "/")
		if len(parts) < 5 {
			t.Fatal("invalid market path")
		}
		if hotPool == "" {
			hotPool = parts[3]
		}
		if parts[3] != hotPool {
			t.Fatal("one hot-pool workload required")
		}
		if r.Method == "POST" {
			writes++
			if r.Headers["X-Operation-Envelope"] == "" || r.Headers["X-Operation-Signature"] == "" {
				t.Fatal("genuine signed replay authorization required")
			}
		}
		if !strings.HasPrefix(r.Path, "/v2/stats/") || strings.Contains(r.Path, "..") || r.Status < 200 || r.Status > 599 {
			t.Fatal("invalid workload binding")
		}
		hasRead = hasRead || r.Method == "GET"
		hasReplay = hasReplay || r.Method == "POST" && r.ReplayAttack
	}
	if writes*5 > len(requests) {
		t.Fatal("writes exceed global2000 per second share")
	}
	if !hasRead || !hasReplay {
		t.Fatal("mixed origin reads and signed replay attacks required")
	}
	transport := &http.Transport{MaxIdleConns: 5000, MaxIdleConnsPerHost: 5000, MaxConnsPerHost: 5000}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const total = 100000
	latencies := make([]time.Duration, 0, total)
	var mutex sync.Mutex
	var failed atomic.Int64
	var wait sync.WaitGroup
	slots := make(chan struct{}, 5000)
	start := time.Now()
	for n := 0; n < total; n++ {
		target := start.Add(time.Duration(n) * 100 * time.Microsecond)
		if delay := time.Until(target); delay > 0 {
			time.Sleep(delay)
		}
		slots <- struct{}{}
		wait.Add(1)
		go func(n int) {
			defer wait.Done()
			defer func() { <-slots }()
			r := requests[n%len(requests)]
			req, err := http.NewRequestWithContext(ctx, r.Method, base+r.Path, bytes.NewBufferString(r.Body))
			if err != nil {
				failed.Add(1)
				return
			}
			for key, value := range r.Headers {
				req.Header.Set(key, value)
			}
			begin := time.Now()
			response, err := client.Do(req)
			if err != nil {
				failed.Add(1)
				return
			}
			_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			response.Body.Close()
			elapsed := time.Since(begin)
			if readErr != nil || response.StatusCode != r.Status {
				failed.Add(1)
			}
			if r.Method == "GET" {
				mutex.Lock()
				latencies = append(latencies, elapsed)
				mutex.Unlock()
			}
		}(n)
	}
	wait.Wait()
	duration := time.Since(start)
	if failed.Load() != 0 || len(latencies) == 0 {
		t.Fatal("origin failures", failed.Load(), "reads", len(latencies))
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	p95 := latencies[(len(latencies)-1)*95/100]
	p99 := latencies[(len(latencies)-1)*99/100]
	if p95 > 150*time.Millisecond || p99 > 400*time.Millisecond || duration > 11*time.Second {
		t.Fatal("origin capacity bounds", p95, p99, duration)
	}
	t.Log(fmt.Sprintf("requests=%d dynamic_rps=10000 duration=%s origin_p95=%s origin_p99=%s; write share remains within global2000/s peak4000burst", total, duration, p95, p99))
}

type phase4File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}
type phase4Process struct {
	Role             string `json:"role"`
	PID              int    `json:"pid"`
	ExecutableSHA256 string `json:"executableSha256"`
	Cgroup           string `json:"cgroup"`
	StartTimeTicks   string `json:"startTimeTicks"`
}
type phase4Plan struct {
	Version            int             `json:"version"`
	RunID              string          `json:"runId"`
	Profile            string          `json:"profile"`
	Origin             string          `json:"origin"`
	OriginRegion       string          `json:"originRegion"`
	HotPool            string          `json:"hotPool"`
	ReadRPS            int             `json:"readRps"`
	WriteRPS           int             `json:"writeRps"`
	SourceRPS          int             `json:"sourceRps"`
	WarmupSeconds      int             `json:"warmupSeconds"`
	MeasurementSeconds int             `json:"measurementSeconds"`
	RecoverySeconds    int             `json:"recoverySeconds"`
	SourceManifest     phase4File      `json:"sourceManifest"`
	RuntimeConfig      phase4File      `json:"runtimeConfig"`
	HTTPRequests       phase4File      `json:"httpRequests"`
	SourceEvents       phase4File      `json:"sourceEvents"`
	Budget             phase4File      `json:"budget"`
	Processes          []phase4Process `json:"processes"`
}
type phase4Runtime struct {
	Schema            string                      `json:"schema"`
	ConsumerID        string                      `json:"consumerId"`
	Generation        string                      `json:"generation"`
	ProjectionVersion string                      `json:"projectionVersion"`
	Region            string                      `json:"region"`
	SourceStream      string                      `json:"sourceStream"`
	Consumers         []natsdriver.ConsumerConfig `json:"consumers"`
}
type phase4HTTPRequest struct {
	Index                  int64             `json:"index"`
	OffsetMicros           int64             `json:"offsetMicros"`
	Method                 string            `json:"method"`
	Path                   string            `json:"path"`
	Body                   string            `json:"body"`
	Headers                map[string]string `json:"headers"`
	ExpectedStatuses       []int             `json:"expectedStatuses"`
	ReplayAttack           bool              `json:"replayAttack"`
	OriginalResponseSHA256 string            `json:"originalResponseSha256"`
}
type phase4Source struct {
	Index        int64           `json:"index"`
	OffsetMicros int64           `json:"offsetMicros"`
	Subject      string          `json:"subject"`
	Wire         json.RawMessage `json:"wire"`
}
type phase4HTTPObservation struct {
	Index          int64           `json:"index"`
	ScheduledAt    time.Time       `json:"scheduledAt"`
	StartedAt      time.Time       `json:"startedAt"`
	CompletedAt    time.Time       `json:"completedAt"`
	Method         string          `json:"method"`
	Path           string          `json:"path"`
	Status         int             `json:"status"`
	Expected       bool            `json:"expected"`
	Warmup         bool            `json:"warmup"`
	RequestBytes   int             `json:"requestBytes"`
	ResponseBytes  int             `json:"responseBytes"`
	ResponseSHA256 string          `json:"responseSha256"`
	Replay         bool            `json:"replay"`
	Body           json.RawMessage `json:"publicReadBody,omitempty"`
	Failure        string          `json:"failure,omitempty"`
}
type phase4Sample struct {
	At                         time.Time                    `json:"at"`
	Processes                  map[string]map[string]string `json:"processes"`
	Host                       map[string]string            `json:"host"`
	Cgroups                    map[string]map[string]string `json:"cgroups"`
	BrokerPending              uint64                       `json:"brokerPending"`
	BrokerAckPending           int                          `json:"brokerAckPending"`
	BrokerRedelivered          int                          `json:"brokerRedelivered"`
	BrokerMessages             uint64                       `json:"brokerMessages"`
	BrokerBytes                uint64                       `json:"brokerBytes"`
	AnalyticsTrades            string                       `json:"pgReportedTradeInserts"`
	AnalyticsOutbox            string                       `json:"pgReportedAnalyticsOutboxInserts"`
	DatabaseBytes              string                       `json:"databaseBytes"`
	AnalyticsBytes             string                       `json:"analyticsBytes"`
	WALBytes                   string                       `json:"walBytes"`
	WaitingLocks               int64                        `json:"waitingLocks"`
	MaxLockWaitSeconds         string                       `json:"maxLockWaitSeconds"`
	LatestObservation          string                       `json:"latestObservation"`
	SourceObservationLagMillis *int64                       `json:"sourceObservationLagMillis"`
	CalculationLagMillis       *int64                       `json:"calculationLagMillis"`
	OldestObservation          string                       `json:"oldestObservation"`
	Failure                    string                       `json:"failure,omitempty"`
}
type phase4Archive struct {
	dir    string
	mutex  sync.Mutex
	files  map[string]*os.File
	errors []error
}

func phase4OpenArchive(dir string) (*phase4Archive, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, errors.New("explicit absolute private L15_PHASE4_OUTPUT required")
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		return nil, errors.New("phase4 output must be a new run directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	return &phase4Archive{dir: dir, files: map[string]*os.File{}}, nil
}
func (a *phase4Archive) append(name string, value any) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	file := a.files[name]
	if file == nil {
		var err error
		file, err = os.OpenFile(filepath.Join(a.dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			a.errors = append(a.errors, err)
			return err
		}
		a.files[name] = file
	}
	err := json.NewEncoder(file).Encode(value)
	if err != nil {
		a.errors = append(a.errors, err)
	}
	return err
}
func (a *phase4Archive) close() error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	for _, file := range a.files {
		if err := file.Sync(); err != nil {
			a.errors = append(a.errors, err)
		}
		if err := file.Close(); err != nil {
			a.errors = append(a.errors, err)
		}
	}
	return errors.Join(a.errors...)
}
func phase4Digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func phase4ReadBound(binding phase4File, limit int64) ([]byte, error) {
	if binding.Path == "" || len(binding.SHA256) != 64 {
		return nil, errors.New("missing bound input")
	}
	digest, err := phase4Digest(binding.Path)
	if err != nil {
		return nil, err
	}
	if digest != binding.SHA256 {
		return nil, errors.New("bound input digest mismatch")
	}
	if limit == 0 {
		return nil, nil
	}
	f, err := os.Open(binding.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("bound document exceeds limit")
	}
	return raw, nil
}
func phase4Strict(raw []byte, target any) error {
	if _, err := auth.BodyDigest(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func phase4ValidatePlan(p phase4Plan) error {
	if p.Version != 1 || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(p.RunID) || p.OriginRegion == "" || !regexp.MustCompile(`^0x[0-9a-f]{40}$`).MatchString(p.HotPool) || p.WarmupSeconds != 30 || p.MeasurementSeconds != 300 || p.SourceRPS != 10000 || p.RecoverySeconds < 1 || p.RecoverySeconds > 1800 {
		return errors.New("explicit frozen phase4 run profile required")
	}
	origin, err := url.Parse(p.Origin)
	if err != nil || origin.Scheme != "http" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || net.ParseIP(origin.Hostname()) == nil || !net.ParseIP(origin.Hostname()).IsLoopback() {
		return errors.New("origin measurements require actual loopback origin")
	}
	switch p.Profile {
	case "allocated_peak":
		if p.ReadRPS != 1000 || p.WriteRPS < 1 || p.WriteRPS > 2000 {
			return errors.New("market_stats1000 and allocated global2000 mutation subset required")
		}
	case "allocated_surge":
		if p.ReadRPS != 2000 || p.WriteRPS < 1 || p.WriteRPS > 4000 {
			return errors.New("market_stats2000 and allocated global4000 mutation subset required")
		}
	case "isolated_ceiling":
		if p.ReadRPS+p.WriteRPS != 10000 || p.WriteRPS < 1 || p.WriteRPS > 2000 {
			return errors.New("isolated10000 dynamic RPS with global2000 write subset required")
		}
	default:
		return errors.New("unapproved phase4 profile")
	}
	roles := map[string]bool{}
	pids := map[int]bool{}
	for _, process := range p.Processes {
		if roles[process.Role] || pids[process.PID] || process.PID < 2 || process.PID == os.Getpid() || len(process.ExecutableSHA256) != 64 || process.StartTimeTicks == "" || !filepath.IsAbs(process.Cgroup) || !strings.HasPrefix(filepath.Clean(process.Cgroup), "/sys/fs/cgroup/") {
			return errors.New("unique pinned service processes/cgroups required")
		}
		roles[process.Role] = true
		pids[process.PID] = true
	}
	for _, role := range []string{"stats", "indexer", "postgres", "redis", "nats"} {
		if !roles[role] {
			return errors.New("missing actual process role: " + role)
		}
	}
	return nil
}
func phase4CaptureProcesses(plan phase4Plan) (map[string]map[string]string, map[string]map[string]string, map[string]string, error) {
	processes, cgroups, host := map[string]map[string]string{}, map[string]map[string]string{}, map[string]string{}
	for _, binding := range plan.Processes {
		proc := fmt.Sprintf("/proc/%d/", binding.PID)
		digest, err := phase4Digest(proc + "exe")
		if err != nil || digest != binding.ExecutableSHA256 {
			return nil, nil, nil, errors.New("runtime executable identity missing or changed: " + binding.Role)
		}
		values := map[string]string{}
		for _, name := range []string{"stat", "status", "io", "cgroup"} {
			raw, err := os.ReadFile(proc + name)
			if err != nil {
				return nil, nil, nil, err
			}
			values[name] = string(raw)
		}
		own := fmt.Sprintf("/proc/%d/status", os.Getpid())
		raw, err := os.ReadFile(own)
		if err != nil {
			return nil, nil, nil, err
		}
		host["generator_status"] = string(raw)
		statFields := strings.Fields(values["stat"][strings.LastIndex(values["stat"], ")")+1:])
		if len(statFields) <= 19 || statFields[19] != binding.StartTimeTicks {
			return nil, nil, nil, errors.New("service PID incarnation changed")
		}
		membership := strings.Split(strings.TrimSpace(values["cgroup"]), "\n")
		unified := ""
		for _, line := range membership {
			if strings.HasPrefix(line, "0::") {
				unified = strings.TrimPrefix(line, "0::")
			}
		}
		expectedGroup := strings.TrimPrefix(filepath.Clean(binding.Cgroup), "/sys/fs/cgroup")
		if unified == "" || filepath.Clean(unified) != expectedGroup {
			return nil, nil, nil, errors.New("process cgroup differs from pinned service resource authority")
		}
		processes[binding.Role] = values
		values = map[string]string{}
		for _, name := range []string{"cpu.max", "cpu.stat", "memory.max", "memory.current", "memory.events", "io.stat"} {
			raw, err := os.ReadFile(filepath.Join(binding.Cgroup, name))
			if err != nil {
				return nil, nil, nil, err
			}
			values[name] = string(raw)
		}
		if strings.TrimSpace(values["memory.max"]) == "max" || strings.HasPrefix(values["cpu.max"], "max ") {
			return nil, nil, nil, errors.New("explicit service resource limits required")
		}
		cgroups[binding.Role] = values
	}
	for _, name := range []string{"stat", "meminfo", "cpuinfo", "net/dev"} {
		raw, err := os.ReadFile("/proc/" + name)
		if err != nil {
			return nil, nil, nil, err
		}
		host[name] = string(raw)
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	host["generator_heap_bytes"] = strconv.FormatUint(memory.HeapAlloc, 10)
	host["generator_goroutines"] = strconv.Itoa(runtime.NumGoroutine())
	return processes, cgroups, host, nil
}

func phase4BindCandidate(binding phase4File) error {
	raw, err := phase4ReadBound(binding, 16<<20)
	if err != nil {
		return err
	}
	var manifest struct {
		CandidateID  string            `json:"candidateId"`
		SourceSHA256 map[string]string `json:"sourceSha256"`
		Builds       []struct {
			Role             string     `json:"role"`
			ExecutableSHA256 string     `json:"executableSha256"`
			SourceTreeSHA256 string     `json:"sourceTreeSha256"`
			BuildExitCode    *int       `json:"buildExitCode"`
			BuildLog         phase4File `json:"buildLog"`
		} `json:"builds"`
	}
	if err = phase4Strict(raw, &manifest); err != nil {
		return err
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return errors.New("current source location unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	if manifest.CandidateID == "" || len(manifest.SourceSHA256) < 20 {
		return errors.New("complete current-candidate source manifest required")
	}
	for path, expected := range manifest.SourceSHA256 {
		if filepath.IsAbs(path) || path != filepath.Clean(path) || strings.HasPrefix(path, "../") || len(expected) != 64 {
			return errors.New("invalid source manifest path or digest")
		}
		actual, err := phase4Digest(filepath.Join(root, path))
		if err != nil || actual != expected {
			return errors.New("current candidate source differs: " + path)
		}
	}
	for _, scope := range []string{"services/launchpad/stats-workers", "sdk/launchpad/shared", "sdk/launchpad/protocol"} {
		err = filepath.WalkDir(filepath.Join(root, scope), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "evidence" {
					return filepath.SkipDir
				}
				return nil
			}
			name := entry.Name()
			if strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" {
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				if manifest.SourceSHA256[filepath.ToSlash(relative)] == "" {
					return errors.New("unbound Go source/module dependency: " + relative)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	treeRaw, err := json.Marshal(manifest.SourceSHA256)
	if err != nil {
		return err
	}
	treeDigest := delivery.Hash(treeRaw)
	builds := map[string]bool{}
	for _, build := range manifest.Builds {
		if build.Role == "" || builds[build.Role] || len(build.ExecutableSHA256) != 64 || build.SourceTreeSHA256 != treeDigest || build.BuildExitCode == nil || *build.BuildExitCode != 0 {
			return errors.New("actual successful build provenance for current source tree required")
		}
		if _, err = phase4ReadBound(build.BuildLog, 0); err != nil {
			return err
		}
		builds[build.Role] = true
	}
	if !builds["stats"] {
		return errors.New("actual stats binary build provenance missing")
	}
	for _, path := range []string{"services/launchpad/stats-workers/internal/analytics/phase2_capacity_test.go", "services/launchpad/stats-workers/internal/analytics/projector.go", "services/launchpad/stats-workers/internal/analytics/reactions.go", "services/launchpad/stats-workers/internal/analytics/reads.go", "services/launchpad/stats-workers/internal/analytics/snapshot.go", "services/launchpad/stats-workers/internal/risk/versioned.go", "services/launchpad/stats-workers/internal/risk/model-v1.json", "services/launchpad/stats-workers/internal/app/app.go", "services/launchpad/stats-workers/internal/httpapi/pressure.go", "services/launchpad/stats-workers/internal/httpapi/reactions.go", "sdk/launchpad/shared/delivery/store.go", "sdk/launchpad/shared/delivery/natsdriver/driver.go", "sdk/launchpad/shared/accounting/validate.go", "launchpad/spec/production-system/spec.kvx"} {
		if manifest.SourceSHA256[path] == "" {
			return errors.New("unbound candidate dependency: " + path)
		}
	}
	return nil
}
func phase4ReadRuntime(binding phase4File) (phase4Runtime, error) {
	raw, err := phase4ReadBound(binding, 1<<20)
	if err != nil {
		return phase4Runtime{}, err
	}
	var document map[string]json.RawMessage
	if err = phase4Strict(raw, &document); err != nil {
		return phase4Runtime{}, err
	}
	var r phase4Runtime
	for name, target := range map[string]any{"schema": &r.Schema, "consumerId": &r.ConsumerID, "generation": &r.Generation, "projectionVersion": &r.ProjectionVersion, "region": &r.Region, "sourceStream": &r.SourceStream, "consumers": &r.Consumers} {
		if err = json.Unmarshal(document[name], target); err != nil {
			return r, err
		}
	}
	if r.Schema != "stats" || r.ConsumerID == "" || r.Generation == "" || r.ProjectionVersion == "" || r.Region == "" || r.SourceStream == "" || len(r.Consumers) != 1 || r.Consumers[0].Subject != accounting.MarketSubject {
		return r, errors.New("actual admitted stats runtime consumer configuration required")
	}
	if _, err = r.Consumers[0].BrokerConsumerConfig(); err != nil {
		return r, err
	}
	return r, nil
}
func phase4Scan(path string, apply func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 65536), delivery.MaxEnvelopeBytes+32768)
	for scanner.Scan() {
		if err = apply(scanner.Bytes()); err != nil {
			return err
		}
	}
	return scanner.Err()
}
func phase4ValidateHTTP(plan phase4Plan) error {
	var n, hot, replay int64
	perSecond := map[int64][2]int{}
	routes := map[string]bool{}
	err := phase4Scan(plan.HTTPRequests.Path, func(raw []byte) error {
		var r phase4HTTPRequest
		if err := phase4Strict(raw, &r); err != nil {
			return err
		}
		totalRPS := int64(plan.ReadRPS + plan.WriteRPS)
		if r.Index != n || r.OffsetMicros != n*1000000/totalRPS {
			return errors.New("HTTP tape must retain complete uniform scheduled arrivals")
		}
		u, err := url.Parse(r.Path)
		parts := strings.Split(r.Path, "/")
		if err != nil || u.IsAbs() || u.RawQuery != "" || u.Fragment != "" || len(parts) != 5 || parts[1] != "v2" || parts[2] != "stats" || !regexp.MustCompile(`^0x[0-9a-f]{40}$`).MatchString(parts[3]) {
			return errors.New("HTTP tape outside exact durable analytics routes")
		}
		if parts[3] == plan.HotPool {
			hot++
		}
		pair := perSecond[r.OffsetMicros/1000000]
		switch r.Method {
		case "GET":
			if r.Body != "" || (parts[4] != "risk" && parts[4] != "pressure" && parts[4] != "reactions") || !reflect.DeepEqual(r.ExpectedStatuses, []int{200}) {
				return errors.New("successful durable precomputed origin reads required")
			}
			routes[parts[4]] = true
			pair[0]++
		case "POST":
			if parts[4] != "reactions" || r.Headers["X-Operation-Envelope"] == "" || r.Headers["X-Operation-Signature"] == "" || r.Headers["Authorization"] == "" {
				return errors.New("genuine scoped signed session workload required")
			}
			var intent analytics.Intent
			if err = phase4Strict([]byte(r.Body), &intent); err != nil {
				return err
			}
			if _, err = intent.Canonical(); err != nil || intent.Pool != parts[3] {
				return errors.New("signed reaction intent not bound to target")
			}
			var envelope auth.OperationEnvelope
			if err = phase4Strict([]byte(r.Headers["X-Operation-Envelope"]), &envelope); err != nil {
				return err
			}
			digest, err := auth.BodyDigest([]byte(r.Body))
			if err != nil {
				return err
			}
			if envelope.Actor != intent.Wallet || envelope.Nonce != intent.Nonce || envelope.ExpiresAt != intent.Expiry || envelope.BodyHash != digest {
				return errors.New("operation body or actor binding mismatch")
			}
			if len(r.ExpectedStatuses) < 1 || len(r.ExpectedStatuses) > 3 {
				return errors.New("explicit mutation outcome required")
			}
			for _, status := range r.ExpectedStatuses {
				if status != 200 && status != 403 && status != 409 && status != 429 && status != 503 {
					return errors.New("unclassified mutation outcome")
				}
				if status == 200 && r.ReplayAttack && len(r.OriginalResponseSHA256) != 64 {
					return errors.New("successful replay must bind original authoritative response")
				}
			}
			if r.ReplayAttack {
				replay++
			}
			pair[1]++
		default:
			return errors.New("unapproved HTTP method")
		}
		perSecond[r.OffsetMicros/1000000] = pair
		n++
		return nil
	})
	if err != nil {
		return err
	}
	expected := int64(plan.WarmupSeconds+plan.MeasurementSeconds) * int64(plan.ReadRPS+plan.WriteRPS)
	if n != expected || hot*100 < n*60 || replay < 1000 || !routes["risk"] || !routes["pressure"] || !routes["reactions"] {
		return errors.New("complete mixed hot-pool/replay tape and all durable read families required")
	}
	for _, mix := range perSecond {
		if mix[0] != plan.ReadRPS || mix[1] != plan.WriteRPS {
			return errors.New("HTTP tape exceeds or misses declared per-second family allocation")
		}
	}
	return nil
}
func phase4ValidateSources(ctx context.Context, conn *pgxpool.Conn, plan phase4Plan) error {
	if _, err := conn.Exec(ctx, "CREATE TEMP TABLE l15_phase4_source_ids(event_id text PRIMARY KEY,pool text NOT NULL) ON COMMIT PRESERVE ROWS"); err != nil {
		return err
	}
	policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{"accounting.market.v1": true}}
	var n, hot int64
	positions := map[string]accounting.SourcePosition{}
	batch := []map[string]any{}
	ids := [][]any{}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		body, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		var matched int
		err = conn.QueryRow(ctx, `SELECT count(*) FROM jsonb_to_recordset($1::jsonb) x(event_id text,transaction_hash text,log_index text,payload jsonb) JOIN indexer.indexer_ingest_outbox p ON p.chain_id=125 AND p.transaction_hash=x.transaction_hash AND p.log_index=x.log_index::bigint AND p.event_kind='accounting.market.v1' AND p.payload->>'publicationId'=x.event_id AND p.payload=x.payload`, string(body)).Scan(&matched)
		if err != nil {
			return err
		}
		if matched != len(batch) {
			return errors.New("source tape differs from actual canonical indexer authority")
		}
		if _, err = conn.CopyFrom(ctx, pgx.Identifier{"l15_phase4_source_ids"}, []string{"event_id", "pool"}, pgx.CopyFromRows(ids)); err != nil {
			return err
		}
		batch = batch[:0]
		ids = ids[:0]
		return nil
	}
	err := phase4Scan(plan.SourceEvents.Path, func(raw []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var record phase4Source
		if err := phase4Strict(raw, &record); err != nil {
			return err
		}
		if record.Index != n || record.OffsetMicros != n*1000000/int64(plan.SourceRPS) || record.Subject != accounting.MarketSubject {
			return errors.New("complete canonical10000/s burst tape required")
		}
		envelope, err := delivery.Decode(record.Wire, policy, record.Subject)
		if err != nil {
			return err
		}
		publication, err := accounting.DecodePublicationV1(envelope.Payload)
		if err != nil || publication.Fill == nil || publication.Facet != "market" || publication.PublicationID != envelope.EventID {
			return errors.New("genuine initial canonical market publication required")
		}
		fill := publication.Fill
		if previous, ok := positions[fill.PoolAddress]; ok {
			order, err := accounting.CompareSourcePosition(fill.Position, previous)
			if err != nil || order <= 0 {
				return errors.New("canonical burst order regression")
			}
		}
		positions[fill.PoolAddress] = fill.Position
		if fill.PoolAddress == plan.HotPool {
			hot++
		}
		batch = append(batch, map[string]any{"event_id": envelope.EventID, "transaction_hash": fill.Position.TransactionHash, "log_index": fill.Position.LogIndex, "payload": envelope.Payload})
		ids = append(ids, []any{envelope.EventID, fill.PoolAddress})
		n++
		if len(batch) == 1000 {
			return flush()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = flush(); err != nil {
		return err
	}
	if n != int64(plan.SourceRPS*plan.MeasurementSeconds) || hot*100 < n*60 {
		return errors.New("three million genuine burst publications and60percent popular-pool skew required")
	}
	return nil
}
func phase4TakeSample(ctx context.Context, pool *pgxpool.Pool, stream jetstream.Stream, consumer jetstream.Consumer, plan phase4Plan) (phase4Sample, error) {
	sample := phase4Sample{At: time.Now().UTC()}
	var err error
	sample.Processes, sample.Cgroups, sample.Host, err = phase4CaptureProcesses(plan)
	if err != nil {
		return sample, err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return sample, err
	}
	state, err := consumer.Info(ctx)
	if err != nil {
		return sample, err
	}
	sample.BrokerPending = state.NumPending
	sample.BrokerAckPending = state.NumAckPending
	sample.BrokerRedelivered = state.NumRedelivered
	sample.BrokerMessages = info.State.Msgs
	sample.BrokerBytes = info.State.Bytes
	err = pool.QueryRow(ctx, `SELECT (SELECT n_tup_ins::text FROM pg_stat_all_tables WHERE schemaname='stats' AND relname='analytics_trades'),(SELECT n_tup_ins::text FROM pg_stat_all_tables WHERE schemaname='stats' AND relname='analytics_outbox'),pg_database_size(current_database())::text,(SELECT COALESCE(sum(pg_total_relation_size(c.oid)),0)::text FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='stats'),(SELECT wal_bytes::text FROM pg_stat_wal),(SELECT count(*) FROM pg_locks WHERE NOT granted),(SELECT COALESCE(max(EXTRACT(EPOCH FROM clock_timestamp()-query_start)),0)::text FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock'),COALESCE((SELECT max(observed_at) FROM stats.analytics_progress),''),COALESCE((SELECT min(observed_at) FROM stats.analytics_progress),'')`).Scan(&sample.AnalyticsTrades, &sample.AnalyticsOutbox, &sample.DatabaseBytes, &sample.AnalyticsBytes, &sample.WALBytes, &sample.WaitingLocks, &sample.MaxLockWaitSeconds, &sample.LatestObservation, &sample.OldestObservation)
	if err != nil {
		return sample, err
	}
	if sample.LatestObservation != "" {
		at, err := time.Parse("2006-01-02T15:04:05.000Z", sample.LatestObservation)
		if err != nil {
			return sample, err
		}
		lag := sample.At.Sub(at).Milliseconds()
		sample.SourceObservationLagMillis = &lag
	}
	var oldest *int64
	if err = pool.QueryRow(ctx, "SELECT min((payload->>'calculated_at')::bigint) FROM stats.analytics_snapshots").Scan(&oldest); err != nil {
		return sample, err
	}
	if oldest != nil {
		lag := sample.At.Sub(time.Unix(*oldest, 0)).Milliseconds()
		sample.CalculationLagMillis = &lag
	}
	return sample, nil
}

type phase4LatencySummary struct {
	Reads                 int64 `json:"reads"`
	Writes                int64 `json:"writes"`
	IntentionalAdmissions int64 `json:"intentionalAdmissions"`
	ReplaySuccesses       int64 `json:"replaySuccesses"`
	Failures              int64 `json:"failures"`
	ReadP95Micros         int64 `json:"readP95Micros"`
	ReadP99Micros         int64 `json:"readP99Micros"`
	DispatchP99Micros     int64 `json:"dispatchP99Micros"`
	DurationMicros        int64 `json:"durationMicros"`
	HTTPBytes             int64 `json:"httpBytes"`
}

func phase4Wait(ctx context.Context, target time.Time) error {
	delay := time.Until(target)
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func phase4HTTP(ctx context.Context, plan phase4Plan, start time.Time, archive *phase4Archive) (phase4LatencySummary, error) {
	transport := &http.Transport{MaxIdleConns: 5000, MaxIdleConnsPerHost: 5000, MaxConnsPerHost: 5000}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	slots := make(chan struct{}, 5000)
	var wait sync.WaitGroup
	var mutex sync.Mutex
	summary := phase4LatencySummary{}
	latencies, dispatch := []int64{}, []int64{}
	err := phase4Scan(plan.HTTPRequests.Path, func(raw []byte) error {
		var r phase4HTTPRequest
		if err := phase4Strict(raw, &r); err != nil {
			return err
		}
		target := start.Add(time.Duration(r.OffsetMicros) * time.Microsecond)
		if err := phase4Wait(ctx, target); err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		wait.Add(1)
		go func(r phase4HTTPRequest, target time.Time) {
			defer wait.Done()
			defer func() { <-slots }()
			observation := phase4HTTPObservation{Index: r.Index, ScheduledAt: target.UTC(), StartedAt: time.Now().UTC(), Method: r.Method, Path: r.Path, Warmup: r.OffsetMicros < int64(plan.WarmupSeconds)*1000000, RequestBytes: len(r.Body)}
			request, err := http.NewRequestWithContext(ctx, r.Method, plan.Origin+r.Path, bytes.NewBufferString(r.Body))
			if err == nil {
				for key, value := range r.Headers {
					request.Header.Set(key, value)
				}
				request.Header.Set("X-Request-ID", fmt.Sprintf("%s-%d", plan.RunID, r.Index))
				response, sendErr := client.Do(request)
				err = sendErr
				if response != nil {
					body, readErr := io.ReadAll(io.LimitReader(response.Body, 262145))
					response.Body.Close()
					observation.Status = response.StatusCode
					observation.ResponseBytes = len(body)
					observation.ResponseSHA256 = delivery.Hash(body)
					observation.Replay = response.Header.Get("Idempotency-Replayed") == "true"
					if r.Method == "GET" && json.Valid(body) {
						observation.Body = append(json.RawMessage(nil), body...)
					}
					if readErr != nil {
						err = readErr
					}
					if len(body) > 262144 {
						err = errors.New("bounded fanout/API response exceeded")
					}
				}
			}
			observation.CompletedAt = time.Now().UTC()
			for _, status := range r.ExpectedStatuses {
				if status == observation.Status {
					observation.Expected = true
				}
			}
			if r.ReplayAttack && observation.Status == 200 && (!observation.Replay || observation.ResponseSHA256 != r.OriginalResponseSHA256) {
				observation.Expected = false
			}
			if err != nil {
				observation.Failure = err.Error()
			}
			if archiveErr := archive.append("http.jsonl", observation); archiveErr != nil {
				err = archiveErr
			}
			mutex.Lock()
			defer mutex.Unlock()
			if observation.Warmup {
				return
			}
			summary.HTTPBytes += int64(observation.RequestBytes + observation.ResponseBytes)
			if r.Method == "GET" {
				summary.Reads++
				latencies = append(latencies, observation.CompletedAt.Sub(target).Microseconds())
			} else {
				summary.Writes++
				if observation.Status == 429 {
					summary.IntentionalAdmissions++
				}
				if r.ReplayAttack && observation.Status == 200 && observation.Expected {
					summary.ReplaySuccesses++
				}
			}
			dispatch = append(dispatch, observation.StartedAt.Sub(target).Microseconds())
			if !observation.Expected || err != nil || observation.Status >= 500 {
				summary.Failures++
			}
		}(r, target)
		return nil
	})
	wait.Wait()
	summary.DurationMicros = time.Since(start).Microseconds() - int64(plan.WarmupSeconds)*1000000
	percentile := func(values []int64, p int) int64 {
		if len(values) == 0 {
			return -1
		}
		sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
		index := (len(values)*p+99)/100 - 1
		return values[index]
	}
	summary.ReadP95Micros = percentile(latencies, 95)
	summary.ReadP99Micros = percentile(latencies, 99)
	summary.DispatchP99Micros = percentile(dispatch, 99)
	if err != nil {
		return summary, err
	}
	if summary.Reads != int64(plan.ReadRPS*plan.MeasurementSeconds) || summary.Writes != int64(plan.WriteRPS*plan.MeasurementSeconds) || summary.Failures != 0 || summary.ReadP95Micros > 150000 || summary.ReadP99Micros > 400000 || summary.DispatchP99Micros > 10000 || summary.DurationMicros > int64(plan.MeasurementSeconds)*1000000+1000000 {
		return summary, errors.New("origin SLO, admitted throughput, generator saturation or coordinated-omission bound failed")
	}
	return summary, nil
}
func phase4Publish(ctx context.Context, plan phase4Plan, start time.Time, js jetstream.JetStream, expectedStream string, archive *phase4Archive) error {
	slots := make(chan struct{}, 256)
	var wait sync.WaitGroup
	var mutex sync.Mutex
	var firstFailure error
	var failureCount int64
	lastSequence := uint64(0)
	count := int64(0)
	err := phase4Scan(plan.SourceEvents.Path, func(raw []byte) error {
		var record phase4Source
		if err := phase4Strict(raw, &record); err != nil {
			return err
		}
		target := start.Add(time.Duration(plan.WarmupSeconds)*time.Second + time.Duration(record.OffsetMicros)*time.Microsecond)
		if err := phase4Wait(ctx, target); err != nil {
			return err
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
		policy := delivery.Policy{Subjects: map[string]bool{accounting.MarketSubject: true}, EventTypes: map[string]bool{"accounting.market.v1": true}}
		envelope, err := delivery.Decode(record.Wire, policy, record.Subject)
		if err != nil {
			<-slots
			return err
		}
		begin := time.Now().UTC()
		future, err := js.PublishAsync(record.Subject, record.Wire, jetstream.WithMsgID(envelope.EventID), jetstream.WithExpectStream(expectedStream))
		if err != nil {
			<-slots
			return err
		}
		wait.Add(1)
		go func(record phase4Source, target, begin time.Time, future jetstream.PubAckFuture, eventID string) {
			defer wait.Done()
			defer func() { <-slots }()
			var ack *jetstream.PubAck
			var err error
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case ack = <-future.Ok():
			case err = <-future.Err():
			case <-ctx.Done():
				err = ctx.Err()
			case <-timer.C:
				err = errors.New("real JetStream persistence acknowledgement deadline exceeded")
			}
			if err == nil && (ack == nil || ack.Stream != expectedStream || ack.Sequence == 0 || ack.Duplicate) {
				err = errors.New("new unique canonical broker persistence acknowledgement required")
			}
			completed := time.Now().UTC()
			failure := ""
			if err != nil {
				failure = err.Error()
			}
			sequence := uint64(0)
			if ack != nil {
				sequence = ack.Sequence
			}
			logErr := archive.append("source.jsonl", map[string]any{"index": record.Index, "eventId": eventID, "wireSha256": delivery.Hash(record.Wire), "scheduledAt": target.UTC(), "startedAt": begin, "completedAt": completed, "brokerStream": expectedStream, "brokerSequence": strconv.FormatUint(sequence, 10), "failure": failure})
			mutex.Lock()
			defer mutex.Unlock()
			count++
			if sequence > lastSequence {
				lastSequence = sequence
			}
			if err != nil || logErr != nil || begin.Sub(target) > 10*time.Millisecond {
				failureCount++
				if firstFailure == nil {
					firstFailure = errors.Join(err, logErr, errors.New("source publication or dispatch qualification failed"))
				}
			}
		}(record, target, begin, future, envelope.EventID)
		return nil
	})
	wait.Wait()
	logErr := archive.append("source-summary.jsonl", map[string]any{"events": count, "highestBrokerSequence": strconv.FormatUint(lastSequence, 10), "sourceEventsPerSecond": plan.SourceRPS, "durationSeconds": plan.MeasurementSeconds, "failures": failureCount, "liveChainCapacityClaim": false})
	if err != nil {
		return errors.Join(err, logErr)
	}
	if count != int64(plan.SourceRPS*plan.MeasurementSeconds) {
		return errors.New("incomplete source burst")
	}
	return errors.Join(firstFailure, logErr)
}
func phase4PauseWorker(ctx context.Context, plan phase4Plan, start time.Time, archive *phase4Archive) error {
	var worker phase4Process
	for _, process := range plan.Processes {
		if process.Role == "stats" {
			worker = process
		}
	}
	if err := phase4Wait(ctx, start.Add(time.Duration(plan.WarmupSeconds+10)*time.Second)); err != nil {
		return err
	}
	if _, _, _, err := phase4CaptureProcesses(plan); err != nil {
		return err
	}
	var err error
	if err = syscall.Kill(worker.PID, syscall.SIGSTOP); err != nil {
		return err
	}
	defer syscall.Kill(worker.PID, syscall.SIGCONT)
	if err = archive.append("faults.jsonl", map[string]any{"at": time.Now().UTC(), "role": "stats", "pid": worker.PID, "signal": "SIGSTOP"}); err != nil {
		return err
	}
	if err = phase4Wait(ctx, time.Now().Add(30*time.Second)); err != nil {
		return err
	}
	if err = syscall.Kill(worker.PID, syscall.SIGCONT); err != nil {
		return err
	}
	return archive.append("faults.jsonl", map[string]any{"at": time.Now().UTC(), "role": "stats", "pid": worker.PID, "signal": "SIGCONT"})
}

func phase4RiskReference(inputs analytics.RiskInputs, now int64, wm analytics.Watermark) risk.V1Result {
	result := risk.V1Result{ModelVersion: "risk-v1", Coverage: "complete", Unavailable: []string{}, QualityFlags: []string{}, Factors: []string{}, CalculatedAt: now, InputsWatermark: &wm}
	add := func(points int, code string) {
		result.PartialScore += points
		if code != "" {
			result.Factors = append(result.Factors, code)
		}
	}
	unknown := func(field string) {
		result.Unavailable = append(result.Unavailable, field)
		result.QualityFlags = append(result.QualityFlags, field+"_unavailable")
	}
	supply, err := accounting.Uint(inputs.SupplyRaw)
	validSupply := err == nil && supply.Sign() > 0
	if !validSupply {
		inputs.Top10Concentration = ""
		inputs.CreatorHoldingsPct = ""
	}
	if inputs.CreatedAt <= 0 || inputs.CreatedAt > now {
		unknown("age")
	} else {
		age := now - inputs.CreatedAt
		switch {
		case age < 3600:
			add(25, "very_new")
		case age < 86400:
			add(15, "new")
		case age < 604800:
			add(5, "")
		}
	}
	bp := func(value, name string) (int64, bool) {
		if len(value) == 0 || len(value) > 5 {
			unknown(name)
			return 0, false
		}
		for _, c := range value {
			if c < '0' || c > '9' {
				unknown(name)
				return 0, false
			}
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n > 10000 {
			unknown(name)
			return 0, false
		}
		return n, true
	}
	if n, ok := bp(inputs.Top10Concentration, "top10_concentration"); ok {
		switch {
		case n > 8000:
			add(25, "extreme_concentration")
		case n > 5000:
			add(15, "high_concentration")
		case n > 3000:
			add(5, "")
		}
	}
	if n, ok := bp(inputs.CreatorHoldingsPct, "creator_holdings"); ok {
		switch {
		case n > 5000:
			add(20, "creator_heavy")
		case n > 2000:
			add(10, "creator_significant")
		}
	}
	if inputs.UniqueTraders24h < 0 {
		unknown("activity")
	} else if inputs.UniqueTraders24h < 3 {
		add(10, "low_activity")
	}
	if inputs.HolderCount < 0 {
		unknown("holders")
	} else if inputs.HolderCount < 5 {
		add(10, "few_holders")
	}
	if wm.Validate() != nil || wm.Integrity != "healthy" {
		unknown("inputs_watermark")
	}
	if !validSupply {
		unknown("supply")
	}
	if len(result.Unavailable) > 0 {
		result.Coverage = "incomplete"
	} else {
		score := result.PartialScore
		result.Score = &score
	}
	return result
}

type phase4PressureAccumulator struct {
	buy, sell       int64
	buyRaw, sellRaw *big.Int
}

func phase4Accumulator() phase4PressureAccumulator {
	return phase4PressureAccumulator{buyRaw: new(big.Int), sellRaw: new(big.Int)}
}
func (a phase4PressureAccumulator) pressure(wash bool) analytics.Pressure {
	result := analytics.Pressure{BuyCount: a.buy, SellCount: a.sell, BuyUSDLRaw: a.buyRaw.String(), SellUSDLRaw: a.sellRaw.String(), Direction: "unavailable", Flags: []string{}}
	ratio := func(b, s *big.Int) *int64 {
		total := new(big.Int).Add(b, s)
		if total.Sign() == 0 {
			return nil
		}
		value := new(big.Int).Quo(new(big.Int).Mul(b, big.NewInt(10000)), total).Int64()
		return &value
	}
	result.BuyCountShare = ratio(big.NewInt(a.buy), big.NewInt(a.sell))
	result.BuyNotionalShare = ratio(a.buyRaw, a.sellRaw)
	total := a.buy + a.sell
	if total == 0 {
		result.Flags = append(result.Flags, "no_activity")
	} else {
		switch {
		case new(big.Int).Mul(big.NewInt(a.buy), big.NewInt(10000)).Cmp(new(big.Int).Mul(big.NewInt(total), big.NewInt(5500))) >= 0:
			result.Direction = "bullish"
		case new(big.Int).Mul(big.NewInt(a.buy), big.NewInt(10000)).Cmp(new(big.Int).Mul(big.NewInt(total), big.NewInt(4500))) <= 0:
			result.Direction = "bearish"
		default:
			result.Direction = "neutral"
		}
	}
	if total < 5 {
		result.Flags = append(result.Flags, "low_activity")
	}
	if wash {
		result.Flags = append(result.Flags, "suspected_wash")
	}
	return result
}
func phase4PublicSnapshot(ctx context.Context, client *http.Client, plan phase4Plan, pool, model string) (analytics.Snapshot, bool, []byte, error) {
	endpoint := "pressure"
	if model == "risk-v1" {
		endpoint = "risk"
	}
	request, err := http.NewRequestWithContext(ctx, "GET", plan.Origin+"/v2/stats/"+pool+"/"+endpoint, nil)
	if err != nil {
		return analytics.Snapshot{}, false, nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return analytics.Snapshot{}, false, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil || len(raw) > 262144 || response.StatusCode != 200 {
		return analytics.Snapshot{}, false, raw, errors.New("public authoritative snapshot unavailable")
	}
	var body struct {
		Snapshot      analytics.Snapshot `json:"snapshot"`
		Stale         bool               `json:"stale"`
		ModelMetadata json.RawMessage    `json:"model_metadata,omitempty"`
	}
	if err = phase4Strict(raw, &body); err != nil {
		return body.Snapshot, body.Stale, raw, err
	}
	if body.Snapshot.Pool != pool || body.Snapshot.Model != model || body.Snapshot.Validate() != nil {
		return body.Snapshot, body.Stale, raw, errors.New("public snapshot identity invalid")
	}
	if model == "risk-v1" {
		var actual, expected any
		if json.Unmarshal(body.ModelMetadata, &actual) != nil || json.Unmarshal(risk.ModelMetadata(), &expected) != nil || !reflect.DeepEqual(actual, expected) {
			return body.Snapshot, body.Stale, raw, errors.New("public frozen model metadata differs")
		}
	}
	return body.Snapshot, body.Stale, raw, nil
}
func phase4ReconcilePool(ctx context.Context, pool *pgxpool.Pool, plan phase4Plan, address string, archive *phase4Archive) error {
	client := &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	pressure, pressureStale, pressureRaw, err := phase4PublicSnapshot(ctx, client, plan, address, "pressure-v1")
	if err != nil {
		return err
	}
	riskSnapshot, riskStale, riskRaw, err := phase4PublicSnapshot(ctx, client, plan, address, "risk-v1")
	if err != nil {
		return err
	}
	if pressure.Watermark != riskSnapshot.Watermark || pressure.CalculatedAt != riskSnapshot.CalculatedAt {
		return errors.New("public risk/pressure canonical watermark mismatch")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SET LOCAL statement_timeout='120000'"); err != nil {
		return err
	}
	for _, snapshot := range []analytics.Snapshot{pressure, riskSnapshot} {
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		var exists bool
		err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM stats.analytics_outbox WHERE pool=$1 AND model=$2 AND revision=$3::numeric AND payload=$4::jsonb)", address, snapshot.Model, snapshot.Watermark.ProjectionRevision, string(raw)).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("public snapshot has no identical committed durable outbox revision")
		}
	}
	var height, hash, transactionIndex, logIndex string
	if err = tx.QueryRow(ctx, "SELECT height::text,block_hash,transaction_index::text,log_index::text FROM stats.analytics_progress WHERE pool=$1", address).Scan(&height, &hash, &transactionIndex, &logIndex); err != nil {
		return err
	}
	if height != pressure.Watermark.Height || hash != pressure.Watermark.BlockHash {
		return errors.New("independent reconciliation requires quiescent identical canonical source head")
	}
	var rawInputs []byte
	if err = tx.QueryRow(ctx, "SELECT payload FROM stats.analytics_risk_inputs WHERE pool=$1 AND height=$2::numeric AND block_hash=$3", address, height, hash).Scan(&rawInputs); err != nil {
		return errors.New("canonical risk inputs missing for exact reconciliation watermark")
	}
	var inputs analytics.RiskInputs
	if err = phase4Strict(rawInputs, &inputs); err != nil {
		return err
	}
	expectedRisk := phase4RiskReference(inputs, riskSnapshot.CalculatedAt, riskSnapshot.Watermark)
	var actualRisk risk.V1Result
	if err = phase4Strict(riskSnapshot.Payload, &actualRisk); err != nil {
		return err
	}
	if !reflect.DeepEqual(expectedRisk, actualRisk) {
		return errors.New("independent integer model reference differs from SQL/public risk")
	}
	one, day := phase4Accumulator(), phase4Accumulator()
	rows, err := tx.Query(ctx, `SELECT t.source_id,t.height::text,t.transaction_index::text,t.log_index::text,t.block_hash,t.event_time,t.side,t.usdl_raw::text,t.digest,(f.source_fill_id IS NOT NULL AND f.source_fact_digest=t.digest AND f.payload->>'poolAddress'=t.pool AND f.payload->>'side'=t.side AND f.payload->>'usdlAmountRaw'=t.usdl_raw::text AND (f.payload->>'blockTimestampSeconds')::bigint=t.event_time) FROM stats.analytics_trades t LEFT JOIN indexer.indexer_accounting_fills_v1 f ON f.source_fill_id=t.source_id WHERE t.pool=$1 AND (t.height,t.transaction_index,t.log_index)<=($2::numeric,$3::numeric,$4::numeric) ORDER BY t.height,t.transaction_index,t.log_index`, address, height, transactionIndex, logIndex)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sourceID, h, ti, li, blockHash, side, amount, digest string
		var eventTime int64
		var canonical bool
		if err = rows.Scan(&sourceID, &h, &ti, &li, &blockHash, &eventTime, &side, &amount, &digest, &canonical); err != nil {
			rows.Close()
			return err
		}
		if !canonical {
			rows.Close()
			return errors.New("persisted trade lacks matching canonical source authority")
		}
		quantity, err := accounting.Uint(amount)
		if err != nil {
			rows.Close()
			return err
		}
		for _, window := range []struct {
			seconds int64
			value   *phase4PressureAccumulator
		}{{3600, &one}, {86400, &day}} {
			if eventTime <= pressure.CalculatedAt && eventTime > pressure.CalculatedAt-window.seconds {
				if side == "buy" {
					window.value.buy++
					window.value.buyRaw.Add(window.value.buyRaw, quantity)
				} else if side == "sell" {
					window.value.sell++
					window.value.sellRaw.Add(window.value.sellRaw, quantity)
				} else {
					rows.Close()
					return errors.New("invalid canonical pressure side")
				}
			}
		}
		if err = archive.append("reconciliation-trades.jsonl", map[string]any{"pool": address, "sourceFillId": sourceID, "height": h, "transactionIndex": ti, "logIndex": li, "blockHash": blockHash, "eventTime": eventTime, "side": side, "usdlRaw": amount, "sourceFactDigest": digest, "watermark": pressure.Watermark}); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	expectedPressure := analytics.PressureWindows{OneHour: one.pressure(inputs.SuspectedWash), TwentyFourHours: day.pressure(inputs.SuspectedWash)}
	var actualPressure analytics.PressureWindows
	if err = phase4Strict(pressure.Payload, &actualPressure); err != nil {
		return err
	}
	if !reflect.DeepEqual(expectedPressure, actualPressure) {
		return errors.New("independent exact rational windows differ from SQL/public pressure")
	}
	readAt := time.Now().Unix()
	observed, err := time.Parse("2006-01-02T15:04:05.000Z", pressure.Watermark.ObservedAt)
	if err != nil {
		return err
	}
	expectedStale := pressure.Watermark.Integrity != "healthy" || readAt < pressure.CalculatedAt || readAt-pressure.CalculatedAt > 30 || readAt < observed.Unix() || readAt-observed.Unix() > 30
	if pressureStale != expectedStale || riskStale != expectedStale {
		return errors.New("public stale disclosure differs from retained source/computation timestamps")
	}
	if err = archive.append("reconciliation.jsonl", map[string]any{"pool": address, "watermark": pressure.Watermark, "inputs": json.RawMessage(rawInputs), "expectedRisk": expectedRisk, "expectedPressure": expectedPressure, "actualPublicPressure": json.RawMessage(pressureRaw), "actualPublicRisk": json.RawMessage(riskRaw), "stale": expectedStale}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type phase4Budget struct {
	Version              int        `json:"version"`
	ApprovedBy           string     `json:"approvedBy"`
	ApprovedAt           time.Time  `json:"approvedAt"`
	Currency             string     `json:"currency"`
	ContingencyBP        int64      `json:"contingencyBp"`
	ApprovedMonthlyCents string     `json:"approvedMonthlyCents"`
	Quote                phase4File `json:"quote"`
}
type phase4Quote struct {
	Vendor       string    `json:"vendor"`
	QuotedAt     time.Time `json:"quotedAt"`
	ValidThrough time.Time `json:"validThrough"`
	Region       string    `json:"region"`
	Roles        map[string]struct {
		CPUQuotaMicros  string `json:"cpuQuotaMicros"`
		CPUPeriodMicros string `json:"cpuPeriodMicros"`
		MemoryBytes     string `json:"memoryBytes"`
		StorageBytes    string `json:"storageBytes"`
		MonthlyCents    string `json:"monthlyCents"`
	} `json:"roles"`
}

func phase4ValidateBudget(plan phase4Plan, archive *phase4Archive) error {
	raw, err := phase4ReadBound(plan.Budget, 1<<20)
	if err != nil {
		return err
	}
	var budget phase4Budget
	if err = phase4Strict(raw, &budget); err != nil {
		return err
	}
	now := time.Now().UTC()
	if budget.Version != 1 || budget.ApprovedBy == "" || budget.ApprovedAt.IsZero() || budget.ApprovedAt.After(now) || budget.Currency != "USD" || budget.ContingencyBP != 3000 {
		return errors.New("actual approved operating budget with30percent contingency required")
	}
	rawQuote, err := phase4ReadBound(budget.Quote, 1<<20)
	if err != nil {
		return err
	}
	var quote phase4Quote
	if err = phase4Strict(rawQuote, &quote); err != nil {
		return err
	}
	if quote.Vendor == "" || quote.QuotedAt.IsZero() || quote.QuotedAt.After(now) || !quote.ValidThrough.After(now) || quote.Region != plan.OriginRegion {
		return errors.New("current deployment-region vendor resource quote required")
	}
	total := new(big.Int)
	for _, process := range plan.Processes {
		entry, ok := quote.Roles[process.Role]
		if !ok {
			return errors.New("unpriced actual service role")
		}
		monthly, err := accounting.Uint(entry.MonthlyCents)
		if err != nil {
			return err
		}
		total.Add(total, monthly)
		memory, err := accounting.Uint(entry.MemoryBytes)
		if err != nil || memory.Sign() == 0 {
			return errors.New("invalid quoted memory")
		}
		actualRaw, err := os.ReadFile(filepath.Join(process.Cgroup, "memory.max"))
		if err != nil {
			return err
		}
		actual, err := accounting.Uint(strings.TrimSpace(string(actualRaw)))
		if err != nil || actual.Cmp(memory) > 0 {
			return errors.New("runtime memory exceeds approved quoted capacity")
		}
		cpuRaw, err := os.ReadFile(filepath.Join(process.Cgroup, "cpu.max"))
		if err != nil {
			return err
		}
		cpu := strings.Fields(string(cpuRaw))
		if len(cpu) != 2 {
			return errors.New("runtime CPU quota unavailable")
		}
		q, err := accounting.Uint(cpu[0])
		if err != nil {
			return err
		}
		period, err := accounting.Uint(cpu[1])
		if err != nil || period.Sign() == 0 {
			return errors.New("runtime CPU period unavailable")
		}
		allowedQ, err := accounting.Uint(entry.CPUQuotaMicros)
		if err != nil {
			return err
		}
		allowedPeriod, err := accounting.Uint(entry.CPUPeriodMicros)
		if err != nil || allowedPeriod.Sign() == 0 {
			return errors.New("invalid quoted CPU capacity")
		}
		if new(big.Int).Mul(q, allowedPeriod).Cmp(new(big.Int).Mul(allowedQ, period)) > 0 {
			return errors.New("runtime CPU exceeds approved quoted capacity")
		}
		storage, err := accounting.Uint(entry.StorageBytes)
		if err != nil || storage.Sign() == 0 {
			return errors.New("explicit provisioned storage missing")
		}
	}
	allowed, err := accounting.Uint(budget.ApprovedMonthlyCents)
	if err != nil {
		return err
	}
	withContingency := new(big.Int).Quo(new(big.Int).Add(new(big.Int).Mul(total, big.NewInt(13000)), big.NewInt(9999)), big.NewInt(10000))
	if withContingency.Cmp(allowed) > 0 {
		return errors.New("measured topology quote plus contingency exceeds approved budget")
	}
	return archive.append("budget.jsonl", map[string]any{"budgetSha256": plan.Budget.SHA256, "quoteSha256": budget.Quote.SHA256, "monthlyBaseCents": total.String(), "contingencyBp": 3000, "monthlyWithContingencyCents": withContingency.String(), "approvedMonthlyCents": allowed.String(), "region": plan.OriginRegion, "fullPlatformBudgetClaim": false})
}
func phase4ReconcileReactions(ctx context.Context, pool *pgxpool.Pool, plan phase4Plan, address string, archive *phase4Archive) error {
	request, err := http.NewRequestWithContext(ctx, "GET", plan.Origin+"/v2/stats/"+address+"/reactions", nil)
	if err != nil {
		return err
	}
	response, err := (&http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 262145))
	if err != nil || len(raw) > 262144 || response.StatusCode != 200 {
		return errors.New("authoritative reaction response unavailable")
	}
	var public analytics.PublicReactions
	if err = phase4Strict(raw, &public); err != nil {
		return err
	}
	if public.Pool != address {
		return errors.New("reaction pool identity mismatch")
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	revision := "0"
	err = tx.QueryRow(ctx, "SELECT revision::text FROM stats.reaction_pools WHERE pool=$1", address).Scan(&revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if public.Revision != revision {
		return errors.New("public reaction revision differs from durable SQL")
	}
	counts := map[string]int64{}
	for _, name := range []string{"bullish", "hot", "diamond", "bearish", "trash", "warning"} {
		counts[name] = 0
	}
	rows, err := tx.Query(ctx, "SELECT reaction,count(*),count(DISTINCT wallet) FROM stats.reaction_votes WHERE pool=$1 AND reaction<>'clear' GROUP BY reaction", address)
	if err != nil {
		return err
	}
	var active int64
	for rows.Next() {
		var name string
		var count, unique int64
		if err = rows.Scan(&name, &count, &unique); err != nil {
			rows.Close()
			return err
		}
		if _, ok := counts[name]; !ok || count != unique {
			rows.Close()
			return errors.New("invalid or duplicate active durable reaction")
		}
		counts[name] = count
		active += count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var distinct int64
	if err = tx.QueryRow(ctx, "SELECT count(DISTINCT wallet) FROM stats.reaction_votes WHERE pool=$1 AND reaction<>'clear'", address).Scan(&distinct); err != nil {
		return err
	}
	if active != distinct || !reflect.DeepEqual(counts, public.Counts) {
		return errors.New("public reaction counts differ from independent durable wallet tally")
	}
	var leaked int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM stats.reaction_outbox WHERE pool=$1 AND (payload ? 'wallet' OR payload ? 'userVote' OR payload ? 'signature')", address).Scan(&leaked); err != nil {
		return err
	}
	if leaked != 0 {
		return errors.New("public outbox leaks private reaction fields")
	}
	if err = archive.append("reaction-reconciliation.jsonl", map[string]any{"pool": address, "revision": revision, "expectedCounts": counts, "activeUniqueWallets": distinct, "actualPublic": json.RawMessage(raw)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func phase4CloseManifest(archive *phase4Archive, plan phase4Plan, summary phase4LatencySummary, runErr error) error {
	files := map[string]string{}
	for _, name := range []string{"binding.jsonl", "budget.jsonl", "http.jsonl", "source.jsonl", "source-summary.jsonl", "samples.jsonl", "faults.jsonl", "recovery.jsonl", "reconciliation-trades.jsonl", "reconciliation.jsonl", "reaction-reconciliation.jsonl", "storage.jsonl"} {
		digest, err := phase4Digest(filepath.Join(archive.dir, name))
		if err != nil {
			return errors.New("missing raw evidence artifact: " + name)
		}
		files[name] = digest
	}
	status := "failed"
	if runErr == nil {
		status = "lane_measurement_passed_global_acceptance_pending"
	}
	manifest := map[string]any{"version": 1, "runId": plan.RunID, "profile": plan.Profile, "candidateManifestSha256": plan.SourceManifest.SHA256, "runtimeConfigSha256": plan.RuntimeConfig.SHA256, "httpRequestsSha256": plan.HTTPRequests.SHA256, "sourceEventsSha256": plan.SourceEvents.SHA256, "budgetSha256": plan.Budget.SHA256, "files": files, "rawLatencySummary": summary, "status": status, "fullPlatformQualification": false, "globalEvidenceTask": "9.1", "readLatencyBasis": "scheduled arrival to completed origin body including dispatch delay; warmup excluded; no last-mile claims", "pgInsertCounterQuality": "pg_stat_all_tables cumulative n_tup_ins, asynchronously reported; not an acknowledged-effect counter"}
	if runErr != nil {
		manifest["failure"] = runErr.Error()
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(archive.dir, "manifest.json"), append(raw, '\n'), 0600)
}
func TestPhase4LaneMeasurementHarness(t *testing.T) {
	planPath, output := os.Getenv("L15_PHASE4_PLAN"), os.Getenv("L15_PHASE4_OUTPUT")
	if planPath == "" || output == "" {
		t.Fatal("explicit real L15_PHASE4_PLAN and new private L15_PHASE4_OUTPUT required")
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 1<<20 {
		t.Fatal("bounded phase4 plan required")
	}
	var plan phase4Plan
	if err = phase4Strict(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if err = phase4ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	archive, err := phase4OpenArchive(output)
	if err != nil {
		t.Fatal(err)
	}
	summary := phase4LatencySummary{}
	var runErr error
	defer func() {
		if err := archive.close(); err != nil {
			runErr = errors.Join(runErr, err)
		}
		if err := phase4CloseManifest(archive, plan, summary, runErr); err != nil {
			runErr = errors.Join(runErr, err)
			failure, _ := json.Marshal(map[string]any{"status": "failed", "failure": runErr.Error(), "candidateManifestSha256": plan.SourceManifest.SHA256, "fullPlatformQualification": false})
			os.WriteFile(filepath.Join(output, "failure.json"), failure, 0600)
		}
		if runErr != nil {
			t.Error(runErr)
		}
		t.Log("phase4 raw measurement artifact: " + filepath.Join(output, "manifest.json"))
	}()
	fail := func(err error) bool {
		if err != nil {
			runErr = errors.Join(runErr, err)
			return true
		}
		return false
	}
	if fail(phase4BindCandidate(plan.SourceManifest)) {
		return
	}
	boundSource, err := phase4ReadBound(plan.SourceManifest, 16<<20)
	if fail(err) {
		return
	}
	var buildDocument struct {
		Builds []struct {
			Role             string `json:"role"`
			ExecutableSHA256 string `json:"executableSha256"`
		} `json:"builds"`
	}
	if fail(json.Unmarshal(boundSource, &buildDocument)) {
		return
	}
	for _, process := range plan.Processes {
		if process.Role == "stats" {
			matched := false
			for _, build := range buildDocument.Builds {
				matched = matched || build.Role == "stats" && build.ExecutableSHA256 == process.ExecutableSHA256
			}
			if !matched {
				fail(errors.New("running stats executable differs from current-candidate build"))
				return
			}
		}
	}
	configuration, err := phase4ReadRuntime(plan.RuntimeConfig)
	if fail(err) {
		return
	}
	if configuration.Region != plan.OriginRegion {
		fail(errors.New("runtime region differs from approved origin region"))
		return
	}
	if fail(phase4ValidateBudget(plan, archive)) {
		return
	}
	for _, binding := range []phase4File{plan.HTTPRequests, plan.SourceEvents} {
		_, err = phase4ReadBound(binding, 0)
		if fail(err) {
			return
		}
	}
	if fail(phase4ValidateHTTP(plan)) {
		return
	}
	dsn, broker := os.Getenv("L15_PHASE4_DSN"), os.Getenv("L15_PHASE4_NATS_URL")
	dbURL, err := url.Parse(dsn)
	if err != nil || dbURL.Scheme != "postgres" || net.ParseIP(dbURL.Hostname()) == nil || !net.ParseIP(dbURL.Hostname()).IsLoopback() {
		fail(errors.New("actual isolated loopback PostgreSQL DSN required"))
		return
	}
	brokerURL, err := url.Parse(broker)
	if err != nil || brokerURL.Scheme != "nats" || brokerURL.User != nil || net.ParseIP(brokerURL.Hostname()) == nil || !net.ParseIP(brokerURL.Hostname()).IsLoopback() {
		fail(errors.New("actual isolated loopback JetStream required"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	config, err := pgxpool.ParseConfig(dsn)
	if fail(err) {
		return
	}
	config.MaxConns = 2
	config.ConnConfig.RuntimeParams["application_name"] = "l15-phase4-measurement"
	config.ConnConfig.RuntimeParams["statement_timeout"] = "2000"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if fail(err) {
		return
	}
	defer pool.Close()
	conn, err := pool.Acquire(ctx)
	if fail(err) {
		return
	}
	defer conn.Release()
	if fail(phase4ValidateSources(ctx, conn, plan)) {
		return
	}
	nc, err := nats.Connect(broker)
	if fail(err) {
		return
	}
	defer nc.Close()
	js, err := jetstream.New(nc, jetstream.WithPublishAsyncMaxPending(256))
	if fail(err) {
		return
	}
	stream, err := js.Stream(ctx, configuration.Consumers[0].Stream)
	if fail(err) {
		return
	}
	info, err := stream.Info(ctx)
	if fail(err) {
		return
	}
	if info.Config.Storage != jetstream.FileStorage {
		fail(errors.New("real file-backed admitted JetStream required"))
		return
	}
	transport, err := natsdriver.NewConsumer(ctx, nc, configuration.Consumers[0])
	if fail(err) {
		return
	}
	transport.Close()
	consumer, err := stream.Consumer(ctx, configuration.Consumers[0].Durable)
	if fail(err) {
		return
	}
	state, err := consumer.Info(ctx)
	if fail(err) {
		return
	}
	if state.NumPending != 0 || state.NumAckPending != 0 {
		fail(errors.New("isolated baseline consumer must be fully caught up"))
		return
	}
	if fail(archive.append("binding.jsonl", map[string]any{"runId": plan.RunID, "startedAt": time.Now().UTC(), "planSha256": delivery.Hash(raw), "candidateManifestSha256": plan.SourceManifest.SHA256, "runtimeConfigSha256": plan.RuntimeConfig.SHA256, "originRegion": plan.OriginRegion, "profile": plan.Profile, "readRps": plan.ReadRPS, "writeRps": plan.WriteRPS, "globalDauEnvelope": 500000, "sourceBurstRps": 10000, "sourceBurstSeconds": 300, "brokerConfig": info.Config, "consumerConfig": state.Config, "processes": plan.Processes, "fullPlatformRun": false})) {
		return
	}
	baseline, err := phase4TakeSample(ctx, pool, stream, consumer, plan)
	if fail(err) {
		return
	}
	if fail(archive.append("samples.jsonl", baseline)) {
		return
	}
	var alreadyCommitted int64
	if err = conn.QueryRow(ctx, "SELECT count(*) FROM l15_phase4_source_ids s JOIN stats.delivery_inbox i ON i.event_id=s.event_id WHERE i.consumer_id=$1 AND i.projection_generation=$2", configuration.ConsumerID, configuration.Generation).Scan(&alreadyCommitted); fail(err) {
		return
	}
	if alreadyCommitted != 0 {
		fail(errors.New("source burst identities already committed in measured projection epoch"))
		return
	}
	start := time.Now().Add(time.Second)
	var maximumBacklog atomic.Uint64
	var sampleCount atomic.Int64
	sampleCtx, stopSamples := context.WithCancel(ctx)
	samplesDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-sampleCtx.Done():
				samplesDone <- nil
				return
			case <-ticker.C:
				bounded, done := context.WithTimeout(sampleCtx, 2*time.Second)
				sample, err := phase4TakeSample(bounded, pool, stream, consumer, plan)
				done()
				sampleCount.Add(1)
				backlog := sample.BrokerPending + uint64(sample.BrokerAckPending)
				for old := maximumBacklog.Load(); backlog > old; old = maximumBacklog.Load() {
					if maximumBacklog.CompareAndSwap(old, backlog) {
						break
					}
				}
				if err != nil {
					sample.Failure = err.Error()
				}
				if archiveErr := archive.append("samples.jsonl", sample); archiveErr != nil {
					samplesDone <- archiveErr
					return
				}
				if err != nil && sampleCtx.Err() == nil {
					samplesDone <- err
					return
				}
			}
		}
	}()
	tasks := make(chan error, 3)
	go func() { value, err := phase4HTTP(ctx, plan, start, archive); summary = value; tasks <- err }()
	go func() { tasks <- phase4Publish(ctx, plan, start, js, configuration.Consumers[0].Stream, archive) }()
	go func() { tasks <- phase4PauseWorker(ctx, plan, start, archive) }()
	for n := 0; n < 3; n++ {
		if err := <-tasks; err != nil {
			runErr = errors.Join(runErr, err)
		}
	}
	recoveryStart := time.Now()
	deadline := recoveryStart.Add(time.Duration(plan.RecoverySeconds) * time.Second)
	for {
		state, err = consumer.Info(ctx)
		if err != nil {
			fail(err)
			break
		}
		if err = archive.append("recovery.jsonl", map[string]any{"at": time.Now().UTC(), "pending": state.NumPending, "ackPending": state.NumAckPending, "redelivered": state.NumRedelivered, "recoveryElapsedMicros": time.Since(recoveryStart).Microseconds()}); fail(err) {
			break
		}
		if state.NumPending == 0 && state.NumAckPending == 0 {
			break
		}
		if time.Now().After(deadline) {
			fail(errors.New("broker backlog missed explicit recovery deadline"))
			break
		}
		if err = phase4Wait(ctx, time.Now().Add(time.Second)); fail(err) {
			break
		}
	}
	stopSamples()
	if err = <-samplesDone; err != nil {
		fail(err)
	}
	if sampleCount.Load() < int64(plan.MeasurementSeconds) || maximumBacklog.Load() == 0 {
		fail(errors.New("missing real backlog growth or complete resource sampling"))
	}
	if fail(archive.append("recovery.jsonl", map[string]any{"at": time.Now().UTC(), "maximumObservedBacklog": maximumBacklog.Load(), "resourceSamples": sampleCount.Load(), "recoveryElapsedMicros": time.Since(recoveryStart).Microseconds()})) {
		return
	}
	var committed, expected int64
	err = conn.QueryRow(ctx, `SELECT count(*),count(*) FILTER(WHERE EXISTS(SELECT 1 FROM stats.delivery_inbox i WHERE i.event_id=s.event_id AND i.consumer_id=$1 AND i.projection_generation=$2)) FROM l15_phase4_source_ids s`, configuration.ConsumerID, configuration.Generation).Scan(&expected, &committed)
	if fail(err) {
		return
	}
	if expected != int64(plan.SourceRPS*plan.MeasurementSeconds) || committed != expected {
		fail(errors.New("burst has missing acknowledged durable identities"))
		return
	}
	rows, err := conn.Query(ctx, "SELECT DISTINCT pool FROM l15_phase4_source_ids ORDER BY pool")
	if fail(err) {
		return
	}
	addresses := []string{}
	for rows.Next() {
		var address string
		if err = rows.Scan(&address); err != nil {
			rows.Close()
			fail(err)
			return
		}
		addresses = append(addresses, address)
	}
	err = rows.Err()
	rows.Close()
	if fail(err) {
		return
	}
	for _, address := range addresses {
		if fail(phase4ReconcilePool(ctx, pool, plan, address, archive)) {
			return
		}
		if fail(phase4ReconcileReactions(ctx, pool, plan, address, archive)) {
			return
		}
	}
	if fail(phase4StorageFits(ctx, pool, stream, plan, archive)) {
		return
	}
	if fail(phase4ValidateBudget(plan, archive)) {
		return
	}
	if fail(phase4BindCandidate(plan.SourceManifest)) {
		return
	}
	for _, binding := range []phase4File{plan.RuntimeConfig, plan.HTTPRequests, plan.SourceEvents, plan.Budget} {
		_, err = phase4ReadBound(binding, 0)
		if fail(err) {
			return
		}
	}
}

func TestPhase4IndependentReferenceBoundaries(t *testing.T) {
	wm := analytics.Watermark{ChainID: "125", Height: "1", BlockHash: "0x" + strings.Repeat("a", 64), ProjectionRevision: "1", ObservedAt: "2026-10-12T00:00:00.000Z", Integrity: "healthy"}
	for _, boundary := range []struct {
		top     string
		score   int
		factors []string
	}{{"3000", 0, []string{}}, {"3001", 5, []string{}}, {"5000", 5, []string{}}, {"5001", 15, []string{"high_concentration"}}, {"8000", 15, []string{"high_concentration"}}, {"8001", 25, []string{"extreme_concentration"}}} {
		inputs := analytics.RiskInputs{CreatedAt: 1, Top10Concentration: boundary.top, CreatorHoldingsPct: "0", SupplyRaw: "100", UniqueTraders24h: 3, HolderCount: 5}
		expected := phase4RiskReference(inputs, 604801, wm)
		if expected.Score == nil || *expected.Score != boundary.score || !reflect.DeepEqual(expected.Factors, boundary.factors) {
			t.Fatal("independent frozen boundary reference differs", boundary, expected)
		}
		raw, err := (risk.ProjectionModelV1{}).Calculate(inputs, 604801, wm)
		if err != nil {
			t.Fatal(err)
		}
		var actual risk.V1Result
		if phase4Strict(raw, &actual) != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatal("actual frozen model differs from independent boundary reference", actual, expected)
		}
	}
	for _, boundary := range []struct {
		age    int64
		score  int
		factor string
	}{{3599, 25, "very_new"}, {3600, 15, "new"}, {86399, 15, "new"}, {86400, 5, ""}, {604799, 5, ""}, {604800, 0, ""}} {
		inputs := analytics.RiskInputs{CreatedAt: 1, Top10Concentration: "0", CreatorHoldingsPct: "0", SupplyRaw: "100", UniqueTraders24h: 3, HolderCount: 5}
		expected := phase4RiskReference(inputs, 1+boundary.age, wm)
		if expected.Score == nil || *expected.Score != boundary.score || strings.Join(expected.Factors, ",") != boundary.factor {
			t.Fatal(boundary, expected)
		}
	}
	for _, inputs := range []analytics.RiskInputs{{CreatedAt: 1, Top10Concentration: "abc", CreatorHoldingsPct: "0", SupplyRaw: "100", UniqueTraders24h: 3, HolderCount: 5}, {CreatedAt: 1, Top10Concentration: "8001", CreatorHoldingsPct: "0", SupplyRaw: "", UniqueTraders24h: 3, HolderCount: 5}, {CreatedAt: 604802, Top10Concentration: "0", CreatorHoldingsPct: "0", SupplyRaw: "100", UniqueTraders24h: 3, HolderCount: 5}} {
		expected := phase4RiskReference(inputs, 604801, wm)
		if expected.Score != nil || expected.Coverage != "incomplete" || len(expected.QualityFlags) == 0 {
			t.Fatal("unknown inputs were normalized into a score", expected)
		}
	}
	count := phase4Accumulator()
	count.buy, count.sell = 3, 1
	count.buyRaw.SetInt64(3000000)
	count.sellRaw.SetInt64(1000000)
	result := count.pressure(false)
	if result.BuyCountShare == nil || *result.BuyCountShare != 7500 || result.BuyNotionalShare == nil || *result.BuyNotionalShare != 7500 {
		t.Fatal(result)
	}
	empty := phase4Accumulator().pressure(false)
	if empty.BuyCountShare != nil || empty.BuyNotionalShare != nil || empty.Direction != "unavailable" || !reflect.DeepEqual(empty.Flags, []string{"no_activity", "low_activity"}) {
		t.Fatal(empty)
	}
}

func phase4StorageFits(ctx context.Context, pool *pgxpool.Pool, stream jetstream.Stream, plan phase4Plan, archive *phase4Archive) error {
	budgetRaw, err := phase4ReadBound(plan.Budget, 1<<20)
	if err != nil {
		return err
	}
	var budget phase4Budget
	if err = phase4Strict(budgetRaw, &budget); err != nil {
		return err
	}
	quoteRaw, err := phase4ReadBound(budget.Quote, 1<<20)
	if err != nil {
		return err
	}
	var quote phase4Quote
	if err = phase4Strict(quoteRaw, &quote); err != nil {
		return err
	}
	var database string
	if err = pool.QueryRow(ctx, "SELECT pg_database_size(current_database())::text").Scan(&database); err != nil {
		return err
	}
	actualPG, err := accounting.Uint(database)
	if err != nil {
		return err
	}
	allowedPG, err := accounting.Uint(quote.Roles["postgres"].StorageBytes)
	if err != nil {
		return err
	}
	if actualPG.Cmp(allowedPG) > 0 {
		return errors.New("actual database exceeds approved provisioned storage")
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return err
	}
	allowedNATS, err := accounting.Uint(quote.Roles["nats"].StorageBytes)
	if err != nil {
		return err
	}
	if new(big.Int).SetUint64(info.State.Bytes).Cmp(allowedNATS) > 0 {
		return errors.New("actual broker retained bytes exceed approved provisioned storage")
	}
	return archive.append("storage.jsonl", map[string]any{"at": time.Now().UTC(), "actualDatabaseBytes": database, "allowedDatabaseBytes": allowedPG.String(), "actualBrokerBytes": strconv.FormatUint(info.State.Bytes, 10), "allowedBrokerBytes": allowedNATS.String(), "full72HourRetentionClaim": false})
}
