package holdings

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

type Phase4Pool struct {
	Pool    string `json:"pool"`
	Token   string `json:"token"`
	Creator string `json:"creator"`
}
type Phase4Phase struct {
	Name            string `json:"name"`
	Seconds         int    `json:"seconds"`
	RPS             int    `json:"rps"`
	HotBPS          int    `json:"hotBps"`
	SourceEventsRPS int    `json:"sourceEventsRps"`
}
type Phase4Host struct {
	ID              string   `json:"id"`
	Role            string   `json:"role"`
	MetricsURL      string   `json:"metricsUrl"`
	RequiredMetrics []string `json:"requiredMetrics"`
}
type Phase4Command struct {
	Executable  string            `json:"executable"`
	SHA256      string            `json:"sha256"`
	Args        []string          `json:"args"`
	Environment map[string]string `json:"environment"`
	Output      string            `json:"output"`
}
type Phase4Plan struct {
	Candidate                 string            `json:"candidate"`
	Generator                 string            `json:"generator"`
	PythonExecutable          string            `json:"pythonExecutable"`
	PythonSHA256              string            `json:"pythonSha256"`
	Interface                 string            `json:"interface"`
	Topology                  string            `json:"topology"`
	SourceRoot                string            `json:"sourceRoot"`
	Sources                   map[string]string `json:"sources"`
	Origin                    string            `json:"origin"`
	SourceStream              string            `json:"sourceStream"`
	HolderSchema              string            `json:"holderSchema"`
	BrokerStream              string            `json:"brokerStream"`
	BrokerConsumer            string            `json:"brokerConsumer"`
	GlobalDAU                 int               `json:"globalDau"`
	GlobalMix                 map[string]int    `json:"globalMix"`
	Pools                     []Phase4Pool      `json:"pools"`
	Phases                    []Phase4Phase     `json:"phases"`
	Hosts                     []Phase4Host      `json:"hosts"`
	GeneratorHostID           string            `json:"generatorHostId"`
	GeneratorNIC              string            `json:"generatorNic"`
	GeneratorNICBitsPerSecond uint64            `json:"generatorNicBitsPerSecond"`
	MaxGeneratorHeapBytes     uint64            `json:"maxGeneratorHeapBytes"`
	MaxReceiptBytes           uint64            `json:"maxReceiptBytes"`
	MaxGeneratorRSSBytes      uint64            `json:"maxGeneratorRssBytes"`
	MaxBrokerBacklog          uint64            `json:"maxBrokerBacklog"`
	MaxDatabaseLockWaiters    int64             `json:"maxDatabaseLockWaiters"`
	MaxProjectionLagMS        int64             `json:"maxProjectionLagMs"`
	RecoverySeconds           int               `json:"recoverySeconds"`
	Workers                   int               `json:"workers"`
	WarmupSeconds             int               `json:"warmupSeconds"`
	CalibrationPath           string            `json:"calibrationPath"`
	CalibrationSHA256         string            `json:"calibrationSha256"`
	BudgetPath                string            `json:"budgetPath"`
	BudgetSHA256              string            `json:"budgetSha256"`
	TopologyPath              string            `json:"topologyPath"`
	Feeder                    Phase4Command     `json:"feeder"`
}

type Phase4Sample struct {
	Phase             string `json:"phase"`
	ID                int    `json:"id"`
	Pool              string `json:"pool"`
	ScheduledUTCNS    int64  `json:"scheduledUtcNs"`
	ScheduledOffsetNS int64  `json:"scheduledOffsetNs"`
	AttemptedOffsetNS int64  `json:"attemptedOffsetNs"`
	CompletedOffsetNS int64  `json:"completedOffsetNs"`
	OriginNS          int64  `json:"originNs"`
	Status            int    `json:"status"`
	Bytes             int    `json:"bytes"`
	Error             string `json:"error,omitempty"`
	ServerTiming      string `json:"serverTiming"`
	ResponseSHA256    string `json:"responseSha256"`
}
type Phase4Summary struct {
	Scheduled      int      `json:"scheduled"`
	Completed      int      `json:"completed"`
	Failed         int      `json:"failed"`
	Missed         int      `json:"missed"`
	OriginP95NS    int64    `json:"originP95Ns"`
	OriginP99NS    int64    `json:"originP99Ns"`
	CorrectedP99NS int64    `json:"correctedP99Ns"`
	SchedulerP99NS int64    `json:"schedulerP99Ns"`
	Errors         []string `json:"errors"`
}

type phase4Artifacts struct {
	directory string
	mu        sync.Mutex
	files     map[string]*os.File
}

func (a *phase4Artifacts) append(name string, value any) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	f := a.files[name]
	if f == nil {
		var err error
		f, err = os.OpenFile(filepath.Join(a.directory, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		a.files[name] = f
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}
func (a *phase4Artifacts) close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, f := range a.files {
		if err := f.Sync(); err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
	}
	a.files = map[string]*os.File{}
	return nil
}
func phase4Hash(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func phase4BoundFile(path, expected string, limit int64) ([]byte, error) {
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(expected) || !filepath.IsAbs(path) {
		return nil, errors.New("explicit absolute input and SHA256 required")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("nonregular or oversized authority input")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("authority byte budget exceeded")
	}
	if phase4Hash(raw) != expected {
		return nil, errors.New("authority checksum mismatch")
	}
	return raw, nil
}
func phase4Origin(s string) error {
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return errors.New("exact HTTP origin required")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1")) {
		return errors.New("TLS required outside owned loopback")
	}
	return nil
}
func (p Phase4Plan) Validate() error {
	for _, h := range []string{p.Candidate, p.Interface, p.Topology, p.Generator} {
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(h) {
			return errors.New("candidate/interface/topology binding missing")
		}
	}
	if p.GlobalDAU != 500000 || !filepath.IsAbs(p.SourceRoot) || len(p.Sources) < 2 || len(p.Pools) < 2 || len(p.Pools) > 100 || p.Workers < 1 || p.Workers > 256 || p.WarmupSeconds < 1 || p.WarmupSeconds > 60 || p.RecoverySeconds < 60 || p.RecoverySeconds > 600 || p.MaxProjectionLagMS < 1 || p.MaxProjectionLagMS > 1000 || p.MaxDatabaseLockWaiters < 0 || len(p.Hosts) > 32 || p.MaxBrokerBacklog == 0 || p.MaxReceiptBytes == 0 || p.MaxReceiptBytes > 64*1024*1024*1024 || p.MaxGeneratorHeapBytes == 0 || p.MaxGeneratorRSSBytes == 0 || p.GeneratorNICBitsPerSecond == 0 || !regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,128}$`).MatchString(p.SourceStream) {
		return errors.New("bounded approved topology/resource/freshness inputs required")
	}
	if err := phase4Origin(p.Origin); err != nil {
		return err
	}
	if _, err := SchemaSQL(p.HolderSchema); err != nil {
		return err
	}
	expected := map[string]int{"chart": 800, "portfolio": 800, "search": 600, "quotes": 2000, "writes": 2000, "market": 1000, "rankings": 600, "identity_profile": 500, "metadata_media": 500, "notifications_social": 800, "capabilities": 400}
	if len(p.Sources) > 2048 {
		return errors.New("candidate binding source count bound exceeded")
	}
	if len(expected) != len(p.GlobalMix) {
		return errors.New("frozen global500000DAU mix required")
	}
	for k, v := range expected {
		if p.GlobalMix[k] != v {
			return errors.New("global mix changed")
		}
	}
	if len(p.Phases) != 4 {
		return errors.New("normal/hot/burst/recovery phases required")
	}
	var seconds int
	for _, phase := range p.Phases {
		seconds += phase.Seconds
	}
	if seconds > 1500 || p.Phases[3].Seconds != p.RecoverySeconds {
		return errors.New("bounded task duration/recovery interval required")
	}
	names := []string{"normal", "hot", "burst", "recovery"}
	for i, s := range p.Phases {
		if s.Name != names[i] || s.Seconds < 60 || s.Seconds > 600 || s.RPS < 1 || s.RPS > p.GlobalMix["market"] || s.HotBPS < 0 || s.HotBPS > 10000 {
			return errors.New("invalid allocated market read phase")
		}
		if i == 0 && s.HotBPS != 6000 || i == 1 && s.HotBPS != 8000 || i == 2 && (s.Seconds != 300 || s.SourceEventsRPS != 10000) || i != 2 && s.SourceEventsRPS != 2000 {
			return errors.New("frozen skew/source burst envelope changed")
		}
	}
	seen := map[string]bool{}
	for _, v := range p.Pools {
		for _, s := range []string{v.Pool, v.Token} {
			a, err := Address(s)
			if err != nil || a != s || a == Zero {
				return errors.New("exact normalized pool/token required")
			}
		}
		if seen[v.Pool] {
			return errors.New("duplicate pool")
		}
		seen[v.Pool] = true
		if v.Creator != "" {
			if a, err := Address(v.Creator); err != nil || a != v.Creator {
				return errors.New("invalid creator")
			}
		}
	}
	roles := map[string]bool{}
	ids := map[string]bool{}
	for _, h := range p.Hosts {
		u, err := url.Parse(h.MetricsURL)
		if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || h.ID == "" || ids[h.ID] || len(h.RequiredMetrics) < 2 {
			return errors.New("actual unique telemetry hosts required")
		}
		base := *u
		base.Path = ""
		if err = phase4Origin(base.String()); err != nil {
			return err
		}
		ids[h.ID] = true
		roles[h.Role] = true
	}
	for _, role := range []string{"api", "postgres", "broker", "worker"} {
		if !roles[role] {
			return fmt.Errorf("missing host role %s", role)
		}
	}
	if p.GeneratorHostID == "" || ids[p.GeneratorHostID] || !regexp.MustCompile(`^[a-zA-Z0-9_.:-]{1,64}$`).MatchString(p.GeneratorNIC) || p.BrokerStream == "" || p.BrokerConsumer == "" {
		return errors.New("independent generator and exact broker consumer required")
	}
	if !filepath.IsAbs(p.Feeder.Executable) || p.Feeder.SHA256 == "" || len(p.Feeder.Args) == 0 || len(p.Feeder.Environment) == 0 || !filepath.IsAbs(p.Feeder.Output) {
		return errors.New("approved real source feeder missing")
	}
	return nil
}
func phase4Quantile(values []int64, bps int) int64 {
	if len(values) == 0 {
		return -1
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values[(len(values)*bps+9999)/10000-1]
}
func Phase4Summarize(samples []Phase4Sample, expected int) Phase4Summary {
	out := Phase4Summary{Scheduled: expected, Errors: []string{}}
	origin, corrected, scheduler := []int64{}, []int64{}, []int64{}
	seen := map[int]bool{}
	for _, s := range samples {
		if s.ID < 0 || s.ID >= expected || seen[s.ID] || s.CompletedOffsetNS < s.ScheduledOffsetNS {
			out.Errors = append(out.Errors, "invalid or duplicated arrival sample")
			continue
		}
		seen[s.ID] = true
		if s.AttemptedOffsetNS == 0 {
			out.Missed++
			out.Failed++
			continue
		}
		out.Completed++
		if s.Status != 200 || s.Error != "" || s.OriginNS <= 0 {
			out.Failed++
		}
		if s.AttemptedOffsetNS < s.ScheduledOffsetNS || s.CompletedOffsetNS < s.AttemptedOffsetNS {
			out.Errors = append(out.Errors, "invalid monotonic sample")
		}
		if s.Status == 200 && s.Error == "" && s.OriginNS > 0 {
			origin = append(origin, s.OriginNS)
		}
		corrected = append(corrected, s.CompletedOffsetNS-s.ScheduledOffsetNS)
		scheduler = append(scheduler, s.AttemptedOffsetNS-s.ScheduledOffsetNS)
	}
	if len(seen) != expected {
		out.Errors = append(out.Errors, "missing scheduled arrivals")
	}
	out.OriginP95NS = phase4Quantile(origin, 9500)
	out.OriginP99NS = phase4Quantile(origin, 9900)
	out.CorrectedP99NS = phase4Quantile(corrected, 9900)
	out.SchedulerP99NS = phase4Quantile(scheduler, 9900)
	if out.OriginP95NS < 0 || out.OriginP95NS > 150000000 || out.OriginP99NS > 400000000 {
		out.Errors = append(out.Errors, "origin SLO failed")
	}
	if expected == 0 || int64(out.Failed)*1000 > int64(expected) {
		out.Errors = append(out.Errors, "valid-operation error fraction exceeds0.1percent")
	}
	if out.Missed > 0 || out.SchedulerP99NS > 10000000 {
		out.Errors = append(out.Errors, "generator saturation")
	}
	return out
}
func phase4HTTP(ctx context.Context, client *http.Client, address string) ([]byte, http.Header, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1048577))
	if err == nil && len(raw) > 1048576 {
		err = errors.New("HTTP byte budget exceeded")
	}
	return raw, response.Header, response.StatusCode, err
}
func phase4OriginNS(header string) (int64, error) {
	for _, v := range strings.Split(header, ",") {
		v = strings.TrimSpace(v)
		if strings.HasPrefix(v, "origin;dur=") {
			value := strings.TrimPrefix(v, "origin;dur=")
			if !regexp.MustCompile(`^[0-9]{1,13}(\.[0-9]{1,6})?$`).MatchString(value) {
				return 0, errors.New("invalid bounded origin timing")
			}
			r, ok := new(big.Rat).SetString(value)
			if !ok || r.Sign() <= 0 {
				return 0, errors.New("invalid origin timing")
			}
			r.Mul(r, big.NewRat(1000000, 1))
			n := new(big.Int).Quo(r.Num(), r.Denom())
			if !n.IsInt64() {
				return 0, errors.New("origin timing overflow")
			}
			return n.Int64(), nil
		}
	}
	return 0, errors.New("real origin Server-Timing missing")
}

type phase4DatabaseObservation struct {
	PendingProjectionEvents        int64   `json:"pendingProjectionEvents"`
	OldestPendingMS                *string `json:"oldestPendingMs"`
	MissingPendingCommitTimestamps int64   `json:"missingPendingCommitTimestamps"`
	ObservedUTCNS                  int64   `json:"observedUtcNs"`
	Locks                          int64   `json:"ungrantedLocks"`
	BlockedSessions                int64   `json:"blockedSessions"`
	SourceHeight                   string  `json:"sourceHeight"`
	RawSourceHeight                string  `json:"rawSourceHeight"`
	SourceLeaseAlive               bool    `json:"sourceLeaseAlive"`
	CanonicalHeadHealthy           bool    `json:"canonicalHeadHealthy"`
	SourceEvents                   string  `json:"sourceEvents"`
	HolderLagRows                  int64   `json:"holderLagRows"`
	LagP99MS                       *string `json:"lagP99Ms"`
	MissingCommitTimestamps        int64   `json:"missingCommitTimestamps"`
	WALBytes                       string  `json:"walBytes"`
	DatabaseBytes                  int64   `json:"databaseBytes"`
}

func phase4Observe(ctx context.Context, pg *pgxpool.Pool, js nats.JetStreamContext, p Phase4Plan, a *phase4Artifacts, phase string, started time.Time) (uint64, error) {
	observation := phase4DatabaseObservation{ObservedUTCNS: time.Now().UnixNano()}
	err := pg.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_locks WHERE NOT granted),(SELECT count(*) FROM pg_stat_activity WHERE cardinality(pg_blocking_pids(pid))>0),(SELECT typed_complete_height::text FROM indexer.indexer_ingest_watermarks_v1 WHERE chain_id=125 AND stream_id=$1),(SELECT COALESCE(sum(record_count),0)::text FROM indexer.indexer_ingest_commits WHERE chain_id=125 AND stream_id=$1 AND pg_xact_commit_timestamp(xmin)>=$2),(SELECT wal_bytes::text FROM pg_stat_wal),pg_database_size(current_database())`, p.SourceStream, started).Scan(&observation.Locks, &observation.BlockedSessions, &observation.SourceHeight, &observation.SourceEvents, &observation.WALBytes, &observation.DatabaseBytes)
	if err != nil {
		return 0, err
	}
	if err = pg.QueryRow(ctx, `SELECT w.raw_complete_height::text,l.expires_at>clock_timestamp() AND l.fence_token>0 AND l.owner_instance<>'',EXISTS(SELECT 1 FROM indexer.indexer_committed_heads h WHERE h.chain_id=125 AND h.stream_id=$1 AND h.height=w.typed_complete_height AND h.integrity='healthy') FROM indexer.indexer_ingest_watermarks_v1 w JOIN indexer.indexer_ingest_leases_v1 l USING(chain_id,stream_id) WHERE w.chain_id=125 AND w.stream_id=$1`, p.SourceStream).Scan(&observation.RawSourceHeight, &observation.SourceLeaseAlive, &observation.CanonicalHeadHealthy); err != nil {
		return 0, err
	}
	query := `SELECT count(*),(percentile_disc(0.99) WITHIN GROUP (ORDER BY extract(epoch FROM (pg_xact_commit_timestamp(t.xmin)-pg_xact_commit_timestamp(c.xmin)))*1000))::text,count(*) FILTER (WHERE pg_xact_commit_timestamp(t.xmin) IS NULL OR pg_xact_commit_timestamp(c.xmin) IS NULL) FROM ` + pgx.Identifier{p.HolderSchema}.Sanitize() + `.transfers t JOIN indexer.indexer_ingest_commits c ON c.chain_id=125 AND c.stream_id=$1 AND (t.event->>'height')::bigint>c.previous_height AND (t.event->>'height')::bigint<=c.last_complete_height WHERE pg_xact_commit_timestamp(c.xmin)>=$2`
	if err = pg.QueryRow(ctx, query, p.SourceStream, started).Scan(&observation.HolderLagRows, &observation.LagP99MS, &observation.MissingCommitTimestamps); err != nil {
		return 0, err
	}
	tokens := make([]string, 0, len(p.Pools))
	for _, pool := range p.Pools {
		tokens = append(tokens, pool.Token)
	}
	pendingQuery := `SELECT count(*),max(extract(epoch FROM (clock_timestamp()-pg_xact_commit_timestamp(c.xmin)))*1000)::text,count(*) FILTER(WHERE pg_xact_commit_timestamp(c.xmin) IS NULL) FROM indexer.indexer_token_transfers_v1 x JOIN indexer.indexer_ingest_commits c ON c.chain_id=x.chain_id AND c.stream_id=$1 AND x.height>c.previous_height AND x.height<=c.last_complete_height LEFT JOIN ` + pgx.Identifier{p.HolderSchema}.Sanitize() + `.transfers t ON t.identity='125:'||x.token||':'||x.transaction_hash||':'||x.log_index::text WHERE x.chain_id=125 AND x.token=ANY($2::text[]) AND t.identity IS NULL`
	if err = pg.QueryRow(ctx, pendingQuery, p.SourceStream, tokens).Scan(&observation.PendingProjectionEvents, &observation.OldestPendingMS, &observation.MissingPendingCommitTimestamps); err != nil {
		return 0, err
	}
	if err = a.append("database.ndjson", map[string]any{"phase": phase, "measurement": observation}); err != nil {
		return 0, err
	}
	if !observation.SourceLeaseAlive || !observation.CanonicalHeadHealthy || observation.RawSourceHeight != observation.SourceHeight {
		return 0, errors.New("actual canonical source unavailable or incomplete")
	}
	if observation.MissingPendingCommitTimestamps > 0 {
		return 0, errors.New("pending source commit timestamp unavailable")
	}
	if observation.PendingProjectionEvents > 0 {
		if observation.OldestPendingMS == nil {
			return 0, errors.New("pending source age unavailable")
		}
		pendingLag, ok := new(big.Rat).SetString(*observation.OldestPendingMS)
		if !ok || pendingLag.Sign() < 0 || pendingLag.Cmp(big.NewRat(p.MaxProjectionLagMS, 1)) > 0 {
			return 0, errors.New("unprojected accepted event freshness bound failed")
		}
	}
	if observation.Locks > p.MaxDatabaseLockWaiters || observation.MissingCommitTimestamps != 0 {
		return 0, errors.New("database lock or commit timestamp authority bound failed")
	}
	if observation.HolderLagRows > 0 {
		if observation.LagP99MS == nil {
			return 0, errors.New("projection lag missing")
		}
		lag, ok := new(big.Rat).SetString(*observation.LagP99MS)
		if !ok || lag.Sign() < 0 || lag.Cmp(big.NewRat(p.MaxProjectionLagMS, 1)) > 0 {
			return 0, errors.New("measured projection lag bound failed")
		}
	}
	info, err := js.ConsumerInfo(p.BrokerStream, p.BrokerConsumer, nats.Context(ctx))
	if err != nil {
		return 0, err
	}
	if err = a.append("broker.ndjson", map[string]any{"phase": phase, "observedUtcNs": time.Now().UnixNano(), "consumerInfo": info}); err != nil {
		return 0, err
	}
	return info.NumPending + uint64(info.NumAckPending), nil
}
func phase4Hosts(ctx context.Context, client *http.Client, p Phase4Plan, a *phase4Artifacts, phase string) error {
	for _, h := range p.Hosts {
		raw, _, status, err := phase4HTTP(ctx, client, h.MetricsURL)
		if err != nil {
			return err
		}
		if status != 200 {
			return errors.New("host telemetry unavailable")
		}
		values := map[string]string{}
		scanner := bufio.NewScanner(strings.NewReader(string(raw)))
		scanner.Buffer(make([]byte, 4096), 1048576)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				values[fields[0]] = fields[1]
			}
		}
		if err = scanner.Err(); err != nil {
			return err
		}
		if err = a.append("hosts.ndjson", map[string]any{"phase": phase, "hostId": h.ID, "role": h.Role, "observedUtcNs": time.Now().UnixNano(), "raw": string(raw), "sha256": phase4Hash(raw)}); err != nil {
			return err
		}
		for _, name := range h.RequiredMetrics {
			value, exists := values[name]
			n, ok := new(big.Rat).SetString(value)
			if !exists || !ok || n.Sign() < 0 {
				return fmt.Errorf("required measured host metric absent: %s/%s", h.ID, name)
			}
		}
	}
	return nil
}
func phase4Generator(p Phase4Plan, a *phase4Artifacts, phase string) error {
	values := map[string]string{}
	for _, name := range []string{"stat", "meminfo", "self/status"} {
		raw, err := os.ReadFile("/proc/" + name)
		if err != nil {
			return err
		}
		values[name] = string(raw)
	}
	for _, name := range []string{"rx_bytes", "tx_bytes"} {
		raw, err := os.ReadFile(filepath.Join("/sys/class/net", p.GeneratorNIC, "statistics", name))
		if err != nil {
			return err
		}
		if _, err = strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64); err != nil {
			return err
		}
		values[name] = strings.TrimSpace(string(raw))
	}
	scanner := bufio.NewScanner(strings.NewReader(values["self/status"]))
	var rss uint64
	var heap runtime.MemStats
	runtime.ReadMemStats(&heap)
	if heap.HeapAlloc > p.MaxGeneratorHeapBytes {
		return errors.New("generator heap exceeds approved envelope")
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 3 && fields[0] == "VmRSS:" {
			n, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return err
			}
			rss = n * 1024
		}
	}
	if rss == 0 || rss > p.MaxGeneratorRSSBytes {
		return errors.New("generator RSS unavailable or exceeds approved envelope")
	}
	return a.append("generator.ndjson", map[string]any{"phase": phase, "hostId": p.GeneratorHostID, "observedUtcNs": time.Now().UnixNano(), "resources": values, "rssBytes": rss, "heapAllocBytes": heap.HeapAlloc, "nicCapacityBitsPerSecond": p.GeneratorNICBitsPerSecond})
}
func phase4ReadPhase(ctx context.Context, client *http.Client, p Phase4Plan, a *phase4Artifacts, phase Phase4Phase) (Phase4Summary, error) {
	started := time.Now()
	expected := phase.Seconds * phase.RPS
	jobs := make(chan int, p.Workers)
	results := make(chan Phase4Sample, p.Workers)
	var workers sync.WaitGroup
	for n := 0; n < p.Workers; n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for id := range jobs {
				scheduled := time.Duration(int64(id) * int64(time.Second) / int64(phase.RPS))
				poolIndex := 1 + id%(len(p.Pools)-1)
				if (id+1)*phase.HotBPS/10000 > id*phase.HotBPS/10000 {
					poolIndex = 0
				}
				pool := p.Pools[poolIndex].Pool
				sample := Phase4Sample{ID: id, Phase: phase.Name, Pool: pool, ScheduledUTCNS: started.Add(scheduled).UnixNano(), ScheduledOffsetNS: int64(scheduled), AttemptedOffsetNS: time.Since(started).Nanoseconds()}
				requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				raw, headers, status, err := phase4HTTP(requestCtx, client, p.Origin+"/v1/pools/"+pool+"/holders?limit=100")
				cancel()
				sample.CompletedOffsetNS = time.Since(started).Nanoseconds()
				sample.Status = status
				sample.Bytes = len(raw)
				sample.ResponseSHA256 = phase4Hash(raw)
				if headers != nil {
					sample.ServerTiming = headers.Get("Server-Timing")
				}
				if err == nil && status == 200 {
					sample.OriginNS, err = phase4OriginNS(headers.Get("Server-Timing"))
					if err == nil {
						var body phase4HolderWire
						err = json.Unmarshal(raw, &body)
						if err == nil && (body.Data.Coverage != "complete" || (body.Quality != "complete" && body.Quality != "stale")) {
							err = errors.New("valid holder read unavailable")
						}
					}
				}
				if err != nil {
					sample.Error = "transport_or_origin_measurement_failure"
				}
				if status != 200 && sample.Error == "" {
					sample.Error = "valid_read_failed"
				}
				results <- sample
			}
		}()
	}
	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		defer close(jobs)
		for id := 0; id < expected; id++ {
			scheduled := time.Duration(int64(id) * int64(time.Second) / int64(phase.RPS))
			timer := time.NewTimer(time.Until(started.Add(scheduled)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case jobs <- id:
			default:
				results <- Phase4Sample{Phase: phase.Name, ID: id, ScheduledUTCNS: started.Add(scheduled).UnixNano(), ScheduledOffsetNS: int64(scheduled), CompletedOffsetNS: time.Since(started).Nanoseconds(), Error: "generator_queue_full"}
			}
		}
	}()
	go func() { <-dispatched; workers.Wait(); close(results) }()
	samples := make([]Phase4Sample, 0, expected)
	var captureErr error
	for sample := range results {
		samples = append(samples, sample)
		if err := a.append("http.ndjson", sample); err != nil && captureErr == nil {
			captureErr = err
		}
	}
	summary := Phase4Summarize(samples, expected)
	if captureErr != nil {
		return summary, captureErr
	}
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if len(summary.Errors) > 0 {
		return summary, errors.New(strings.Join(summary.Errors, "; "))
	}
	return summary, nil
}

type phase4HolderWire struct {
	Revision  string `json:"revision"`
	Quality   string `json:"quality"`
	Watermark struct {
		Height             string `json:"height"`
		BlockHash          string `json:"blockHash"`
		ProjectionRevision string `json:"projectionRevision"`
		Integrity          string `json:"integrity"`
	} `json:"watermark"`
	Data struct {
		Token        string       `json:"tokenAddress"`
		Pool         string       `json:"poolAddress"`
		Supply       string       `json:"supplyRaw"`
		Coverage     string       `json:"coverage"`
		SnapshotID   string       `json:"snapshotId"`
		Position     *Transfer    `json:"toPosition"`
		Distribution Distribution `json:"distribution"`
		Decimals     int          `json:"decimals"`
		Provenance   *Provenance  `json:"lastTransferProvenance"`
		Items        []struct {
			Wallet string `json:"wallet"`
			Raw    string `json:"balanceRaw"`
		} `json:"items"`
	} `json:"data"`
}

func phase4Reference(ctx context.Context, pg *pgxpool.Pool, p Phase4Plan, pool Phase4Pool, wire phase4HolderWire, a *phase4Artifacts) error {
	if wire.Data.Token != pool.Token || wire.Data.Pool != pool.Pool || wire.Data.Coverage != "complete" || (wire.Quality != "complete" && wire.Quality != "stale") || wire.Data.Position == nil || wire.Watermark.Integrity != "healthy" || wire.Revision != wire.Watermark.ProjectionRevision || wire.Watermark.Height != wire.Data.Position.Height || wire.Watermark.BlockHash != wire.Data.Position.BlockHash {
		return errors.New("public response lacks identical canonical watermark")
	}
	position, err := wire.Data.Position.Normalize()
	if err != nil {
		return err
	}
	if wire.Data.Provenance == nil {
		return errors.New("public transfer provenance missing")
	}
	if err = ValidateProvenance(position, *wire.Data.Provenance); err != nil {
		return err
	}
	tx, err := pg.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var complete, creation, creationTransaction string
	var metadata []byte
	if err = tx.QueryRow(ctx, `SELECT count(DISTINCT c.height)::text,k.height::text,k.transaction_hash,k.payload FROM indexer.indexer_canonical_creations_v1 k JOIN indexer.indexer_block_completeness_v1 c ON c.chain_id=125 AND c.stream_id=$2 AND c.height BETWEEN k.height AND $3::bigint AND c.fetch_complete AND c.required_decode_complete WHERE k.token=$1 GROUP BY k.height,k.transaction_hash,k.payload`, pool.Token, p.SourceStream, position.Height).Scan(&complete, &creation, &creationTransaction, &metadata); err != nil {
		return err
	}
	var fact struct {
		Metadata *struct {
			Decimals    *int    `json:"decimals"`
			TotalSupply *string `json:"totalSupply"`
		} `json:"metadata"`
	}
	if err = json.Unmarshal(metadata, &fact); err != nil {
		return err
	}
	if fact.Metadata == nil || fact.Metadata.Decimals == nil || fact.Metadata.TotalSupply == nil {
		return errors.New("verified token creation metadata absent")
	}
	if *fact.Metadata.Decimals != wire.Data.Decimals {
		return errors.New("public token decimals differ from verified creation")
	}
	var genesis string
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN from_address=$3 THEN amount_raw WHEN to_address=$3 THEN -amount_raw ELSE 0 END),0)::text FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND transaction_hash=$2`, pool.Token, creationTransaction, Zero).Scan(&genesis); err != nil {
		return err
	}
	if genesis != *fact.Metadata.TotalSupply {
		return errors.New("independent genesis issuance differs from verified token metadata")
	}
	height, _ := new(big.Int).SetString(position.Height, 10)
	first, ok := new(big.Int).SetString(creation, 10)
	if !ok {
		return errors.New("invalid creation checkpoint")
	}
	count := new(big.Int).Sub(height, first)
	count.Add(count, big.NewInt(1))
	if complete != count.String() {
		return errors.New("independent canonical range incomplete")
	}
	var rawCanonical bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_raw_logs_v1 WHERE chain_id=125 AND emitter=$1 AND transaction_hash=$2 AND log_index=$3::bigint AND block_hash=$4 AND height=$5::bigint)`, pool.Token, position.TransactionHash, position.LogIndex, position.BlockHash, position.Height).Scan(&rawCanonical); err != nil || !rawCanonical {
		return errors.New("public position has no canonical raw event")
	}
	if wire.Data.Provenance.Actor != nil {
		var actorConfirmed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM indexer.indexer_accounting_fills_v1 WHERE payload->'position'->>'transactionHash'=$1 AND payload->'position'->>'blockHash'=$2 AND payload->'token'->>'address'=$3 AND COALESCE(payload->'attribution'->>'routerCaller',payload->'attribution'->>'transactionSender')=$4)`, position.TransactionHash, position.BlockHash, pool.Token, *wire.Data.Provenance.Actor).Scan(&actorConfirmed); err != nil {
			return err
		}
		if !actorConfirmed {
			return errors.New("public actor has no canonical authenticated fill fact")
		}
	}
	reference := `WITH events AS (SELECT from_address,to_address,amount_raw FROM indexer.indexer_token_transfers_v1 WHERE chain_id=125 AND token=$1 AND height>=$2::bigint AND (height,transaction_index,log_index)<=($3::bigint,$4::bigint,$5::bigint)), deltas AS (SELECT from_address AS address,-amount_raw AS delta FROM events WHERE from_address<>$6 UNION ALL SELECT to_address,amount_raw FROM events WHERE to_address<>$6), expected AS (SELECT address,sum(delta) AS balance FROM deltas GROUP BY address)`
	args := []any{pool.Token, creation, position.Height, position.TransactionIndex, position.LogIndex, Zero}
	var negative int64
	var supply string
	if err = tx.QueryRow(ctx, reference+` SELECT count(*) FILTER(WHERE balance<0),COALESCE(sum(balance),0)::text FROM expected`, args...).Scan(&negative, &supply); err != nil {
		return err
	}
	if negative != 0 || supply != wire.Data.Supply {
		return errors.New("independent integer conservation differs")
	}
	args = append(args, wire.Data.SnapshotID)
	var differing int64
	if err = tx.QueryRow(ctx, reference+` SELECT count(*) FROM (SELECT address,balance FROM expected WHERE balance>0) e FULL OUTER JOIN (SELECT address,balance FROM `+pgx.Identifier{p.HolderSchema}.Sanitize()+`.snapshot_balances WHERE snapshot_id=$7::uuid) s ON s.address=e.address WHERE e.address IS NULL OR s.address IS NULL OR e.balance<>s.balance`, args...).Scan(&differing); err != nil {
		return err
	}
	if differing != 0 {
		return errors.New("persisted snapshot differs from independent transfer sums")
	}
	var saved []byte
	if err = tx.QueryRow(ctx, "SELECT metadata FROM "+pgx.Identifier{p.HolderSchema}.Sanitize()+".snapshots WHERE snapshot_id=$1 AND token=$2 AND pool=$3", wire.Data.SnapshotID, pool.Token, pool.Pool).Scan(&saved); err != nil {
		return err
	}
	var persisted snapshotMetadata
	if err = json.Unmarshal(saved, &persisted); err != nil {
		return err
	}
	if strconv.FormatInt(persisted.Snapshot.Revision, 10) != wire.Revision || persisted.Snapshot.Supply != supply || persisted.Snapshot.Position == nil || persisted.Snapshot.Position.Digest() != position.Digest() {
		return errors.New("persisted snapshot metadata differs from public watermark")
	}
	rows, err := tx.Query(ctx, reference+` SELECT address,balance::text FROM expected WHERE balance>0 ORDER BY balance DESC,address LIMIT 100`, args[:6]...)
	if err != nil {
		return err
	}
	expectedRows := []Balance{}
	for rows.Next() {
		var v Balance
		if err = rows.Scan(&v.Address, &v.Raw); err != nil {
			rows.Close()
			return err
		}
		expectedRows = append(expectedRows, v)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(expectedRows) != len(wire.Data.Items) {
		return errors.New("public top100 row count differs")
	}
	leaders := new(big.Int)
	for i, v := range expectedRows {
		if v.Address != wire.Data.Items[i].Wallet || v.Raw != wire.Data.Items[i].Raw {
			return errors.New("public numeric ordering/balance differs")
		}
		if i < 10 {
			n, _ := new(big.Int).SetString(v.Raw, 10)
			leaders.Add(leaders, n)
		}
	}
	denominator, ok := new(big.Int).SetString(supply, 10)
	if !ok || denominator.Sign() == 0 {
		if wire.Data.Distribution.Top10.BasisPoints != nil {
			return errors.New("zero denominator fabricated")
		}
	} else {
		leaders.Mul(leaders, big.NewInt(10000))
		leaders.Quo(leaders, denominator)
		if wire.Data.Distribution.Top10.BasisPoints == nil || *wire.Data.Distribution.Top10.BasisPoints != leaders.String() {
			return errors.New("independent top10 basis points differs")
		}
	}
	creatorRaw := "0"
	if pool.Creator != "" {
		if err = tx.QueryRow(ctx, reference+` SELECT COALESCE((SELECT balance FROM expected WHERE address=$7),0)::text`, append(args[:6], pool.Creator)...).Scan(&creatorRaw); err != nil {
			return err
		}
		if expectedBPS := phase4BPS(creatorRaw, supply); expectedBPS != nil {
			if wire.Data.Distribution.Creator.BasisPoints == nil || *wire.Data.Distribution.Creator.BasisPoints != *expectedBPS {
				return errors.New("independent creator concentration differs")
			}
		} else if wire.Data.Distribution.Creator.BasisPoints != nil {
			return errors.New("unknown creator denominator fabricated")
		}
	} else if wire.Data.Distribution.Creator.BasisPoints != nil {
		return errors.New("missing creator fabricated")
	}
	var external, unknown int64
	var circulating string
	if err = tx.QueryRow(ctx, "SELECT count(*) FILTER(WHERE category='external_wallet'),count(*) FILTER(WHERE category='unknown'),COALESCE(sum(balance) FILTER(WHERE category='external_wallet'),0)::text FROM "+pgx.Identifier{p.HolderSchema}.Sanitize()+".snapshot_balances WHERE snapshot_id=$1", wire.Data.SnapshotID).Scan(&external, &unknown, &circulating); err != nil {
		return err
	}
	if uint64(external) != wire.Data.Distribution.ExternalWalletCount || uint64(unknown) != wire.Data.Distribution.UnknownCount || circulating != wire.Data.Distribution.CirculatingRaw || wire.Data.Distribution.InclusiveDenominator.Raw != supply || wire.Data.Distribution.InclusiveDenominator.Policy != "indexed_supply_v1" || wire.Data.Distribution.CirculatingDenominator.Raw != circulating || wire.Data.Distribution.CirculatingDenominator.Policy != "verified_external_wallets_v1" {
		return errors.New("durable category arithmetic/denominator policy differs")
	}
	rows, err = tx.Query(ctx, reference+` SELECT address,balance::text FROM expected WHERE balance>0 ORDER BY address`, args[:6]...)
	if err != nil {
		return err
	}
	digest := sha256.New()
	digest.Write([]byte("holdings_v1\n125\n" + pool.Token + "\n" + supply + "\n" + position.Height + "\n"))
	var population int64
	var bands [10]Band
	for rows.Next() {
		var address, amount string
		if err = rows.Scan(&address, &amount); err != nil {
			rows.Close()
			return err
		}
		digest.Write([]byte(address + ":" + amount + "\n"))
		population++
		band := len(amount) - 1
		if band > 9 {
			band = 9
		}
		bands[band].Count++
		prior, _ := new(big.Int).SetString(bands[band].Raw, 10)
		if prior == nil {
			prior = new(big.Int)
		}
		n, _ := new(big.Int).SetString(amount, 10)
		bands[band].Raw = prior.Add(prior, n).String()
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range bands {
		if bands[i].Raw == "" {
			bands[i].Raw = "0"
		}
		if bands[i] != wire.Data.Distribution.Bands[i] {
			return errors.New("independent frozen distribution band differs")
		}
	}
	if uint64(population) != wire.Data.Distribution.AllPositiveCount {
		return errors.New("independent holder population differs")
	}
	if err = a.append("arithmetic.ndjson", map[string]any{"pool": pool.Pool, "token": pool.Token, "revision": wire.Revision, "position": position, "supply": supply, "population": population, "bands": bands, "balanceDigest": hex.EncodeToString(digest.Sum(nil)), "top100": expectedRows, "canonicalMetadata": json.RawMessage(metadata), "method": "independent_signed_integer_transfer_sums"}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func phase4Reconcile(ctx context.Context, pg *pgxpool.Pool, client *http.Client, p Phase4Plan, a *phase4Artifacts) error {
	for _, pool := range p.Pools {
		raw, _, status, err := phase4HTTP(ctx, client, p.Origin+"/v1/pools/"+pool.Pool+"/holders?limit=100")
		if err != nil {
			return err
		}
		if status != 200 {
			return errors.New("reconciliation API failed")
		}
		if err = a.append("reconciliation-api.ndjson", map[string]any{"observedUtcNs": time.Now().UnixNano(), "pool": pool.Pool, "raw": json.RawMessage(raw), "sha256": phase4Hash(raw)}); err != nil {
			return err
		}
		var wire phase4HolderWire
		if err = json.Unmarshal(raw, &wire); err != nil {
			return err
		}
		if err = phase4Reference(ctx, pg, p, pool, wire, a); err != nil {
			return err
		}
	}
	return nil
}

func phase4Decode(raw []byte, value any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	var visit func() error
	visit = func() error {
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
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return errors.New("duplicate JSON authority key")
				}
				seen[name] = true
				if err = visit(); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err = visit(); err != nil {
					return err
				}
			}
		default:
			return errors.New("unexpected JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := visit(); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing authority bytes")
	}
	decoder = json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}
func phase4Calibration(ctx context.Context, p Phase4Plan, a *phase4Artifacts) error {
	if _, err := phase4BoundFile(p.CalibrationPath, p.CalibrationSHA256, 67108864); err != nil {
		return err
	}
	if _, err := phase4BoundFile(p.PythonExecutable, p.PythonSHA256, 268435456); err != nil {
		return err
	}
	for _, source := range []string{"tools/launchpad/production/load/calibration.py", "tools/launchpad/production/load/scheduler.py"} {
		if p.Sources[source] == "" {
			return errors.New("existing raw calibration validator source binding required")
		}
	}
	program := "import json,sys;sys.path.insert(0,sys.argv[1]);from calibration import evaluate;r=evaluate(json.load(open(sys.argv[2])),candidate=sys.argv[3],topology=sys.argv[4],generator=sys.argv[5]);print(json.dumps(r));sys.exit(0 if r['generator_calibrated'] else 1)"
	command := exec.CommandContext(ctx, p.PythonExecutable, "-c", program, filepath.Join(p.SourceRoot, "tools/launchpad/production/load"), p.CalibrationPath, p.Candidate, p.Topology, p.Generator)
	output, err := command.Output()
	if len(output) > 1048576 {
		return errors.New("calibration output byte bound")
	}
	if captureErr := a.append("calibration.ndjson", map[string]any{"sourceSha256": p.Sources["tools/launchpad/production/load/calibration.py"], "reportsSha256": p.CalibrationSHA256, "raw": string(output), "exitSuccess": err == nil}); captureErr != nil {
		return captureErr
	}
	if err != nil {
		return errors.New("actual four-host raw calibration failed")
	}
	return nil
}
func phase4Budget(p Phase4Plan, a *phase4Artifacts) error {
	raw, err := phase4BoundFile(p.BudgetPath, p.BudgetSHA256, 1048576)
	if err != nil {
		return err
	}
	var budget struct {
		Candidate            string `json:"candidate"`
		Topology             string `json:"topology"`
		Currency             string `json:"currency"`
		ApprovedBy           string `json:"approvedBy"`
		ApprovedMonthlyMinor string `json:"approvedMonthlyMinor"`
		ModeledMonthlyMinor  string `json:"modeledMonthlyMinor"`
		ContingencyBPS       int64  `json:"contingencyBps"`
		ValidUntil           string `json:"validUntil"`
		Quotes               []struct {
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Vendor string `json:"vendor"`
		} `json:"quotes"`
	}
	if err = phase4Decode(raw, &budget); err != nil {
		return err
	}
	deadline, err := time.Parse(time.RFC3339, budget.ValidUntil)
	if err != nil || !deadline.After(time.Now()) || budget.Candidate != p.Candidate || budget.Topology != p.Topology || budget.ApprovedBy == "" || budget.Currency == "" || budget.ContingencyBPS != 3000 || len(budget.Quotes) == 0 {
		return errors.New("candidate-bound approved current vendor budget required")
	}
	approved, ok := new(big.Int).SetString(budget.ApprovedMonthlyMinor, 10)
	if !ok || approved.Sign() <= 0 {
		return errors.New("invalid approved budget integer")
	}
	modeled, ok := new(big.Int).SetString(budget.ModeledMonthlyMinor, 10)
	if !ok || modeled.Sign() <= 0 {
		return errors.New("invalid modeled budget integer")
	}
	cost := new(big.Int).Mul(modeled, big.NewInt(13000))
	cost.Add(cost, big.NewInt(9999))
	cost.Quo(cost, big.NewInt(10000))
	if cost.Cmp(approved) > 0 {
		return errors.New("approved operating budget exceeded including30percent contingency")
	}
	for _, quote := range budget.Quotes {
		if quote.Vendor == "" {
			return errors.New("vendor quote authority absent")
		}
		if _, err = phase4BoundFile(quote.Path, quote.SHA256, 4194304); err != nil {
			return err
		}
	}
	return a.append("budget.ndjson", map[string]any{"inputSha256": p.BudgetSHA256, "candidate": p.Candidate, "topology": p.Topology, "modeledWithContingencyMinor": cost.String(), "approvedMinor": approved.String(), "currency": budget.Currency, "quoteCount": len(budget.Quotes)})
}
func phase4SourceBindings(ctx context.Context, p Phase4Plan) error {
	for _, required := range []string{"services/launchpad/stats-workers/internal/holdings/phase4_measurement.go", "services/launchpad/stats-workers/internal/holdings/phase4_measurement_test.go", "services/launchpad/stats-workers/internal/httpapi/pool_holders.go", "services/launchpad/stats-workers/internal/holdings/ingestion.go", "services/launchpad/stats-workers/internal/holdings/postgres.go", "launchpad/spec/production-system/spec.kvx"} {
		if p.Sources[required] == "" {
			return errors.New("measurement/production/spec source missing from candidate binding")
		}
	}
	for name, sha := range p.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		if filepath.IsAbs(name) || filepath.Clean(name) != name || strings.HasPrefix(name, ".."+string(os.PathSeparator)) {
			return errors.New("source binding escapes candidate")
		}
		path := filepath.Join(p.SourceRoot, name)
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil || resolved != path {
			return errors.New("source binding symlink or path mismatch")
		}
		if _, err = phase4BoundFile(path, sha, 67108864); err != nil {
			return err
		}
	}
	_, err := phase4BoundFile(p.TopologyPath, p.Topology, 4194304)
	return err
}
func phase4Feeder(ctx context.Context, p Phase4Plan, a *phase4Artifacts) error {
	if os.Getenv("L14_PHASE4_FEED_ADMITTED") != "YES" {
		return errors.New("explicit approved real source feeder mutation admission missing")
	}
	if _, err := phase4BoundFile(p.Feeder.Executable, p.Feeder.SHA256, 268435456); err != nil {
		return err
	}
	if _, err := os.Lstat(p.Feeder.Output); !errors.Is(err, os.ErrNotExist) {
		return errors.New("fresh real feeder receipt output required")
	}
	command := exec.CommandContext(ctx, p.Feeder.Executable, p.Feeder.Args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	command.Dir = p.SourceRoot
	command.Env = []string{}
	for key, value := range p.Feeder.Environment {
		if !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(key) || strings.ContainsRune(value, 0) {
			return errors.New("invalid explicit feeder environment")
		}
		command.Env = append(command.Env, key+"="+value)
	}
	stdout, err := os.OpenFile(filepath.Join(a.directory, "source-feeder.stdout"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(a.directory, "source-feeder.stderr"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	command.Stdout = &phase4BoundedWriter{target: stdout, remaining: 67108864}
	command.Stderr = &phase4BoundedWriter{target: stderr, remaining: 67108864}
	if err = command.Run(); err != nil {
		return errors.New("actual source feeder failed")
	}
	if err = stdout.Sync(); err != nil {
		return err
	}
	if err = stderr.Sync(); err != nil {
		return err
	}
	info, err := os.Lstat(p.Feeder.Output)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || uint64(info.Size()) > p.MaxReceiptBytes {
		return errors.New("actual source receipt output missing or exceeds approved storage envelope")
	}
	input, err := os.Open(p.Feeder.Output)
	if err != nil {
		return err
	}
	defer input.Close()
	receipt, err := os.OpenFile(filepath.Join(a.directory, "source-receipts.ndjson"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer receipt.Close()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(receipt, hasher), io.LimitReader(input, int64(p.MaxReceiptBytes)+1))
	if err != nil {
		return err
	}
	if uint64(size) > p.MaxReceiptBytes {
		return errors.New("receipt storage envelope exceeded")
	}
	if err = receipt.Sync(); err != nil {
		return err
	}
	return a.append("source-feeder.ndjson", map[string]any{"executableSha256": p.Feeder.SHA256, "receiptFileSha256": hex.EncodeToString(hasher.Sum(nil)), "receiptBytes": size, "authority": "approved_real_source_feeder"})
}
func RunPhase4Measurement(ctx context.Context, planPath, planSHA256, output string) (retErr error) {
	raw, err := phase4BoundFile(planPath, planSHA256, 1048576)
	if err != nil {
		return err
	}
	var p Phase4Plan
	if err = phase4Decode(raw, &p); err != nil {
		return err
	}
	if err = p.Validate(); err != nil {
		return err
	}
	if os.Getenv("L14_PHASE4_APPROVED_CANDIDATE") != p.Candidate || os.Getenv("L14_PHASE4_APPROVED_INTERFACE") != p.Interface || os.Getenv("L14_PHASE4_APPROVED_TOPOLOGY") != p.Topology {
		return errors.New("approved runtime candidate/interface/topology bindings missing")
	}
	if err = phase4SourceBindings(ctx, p); err != nil {
		return err
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return errors.New("exact private output directory required")
	}
	if err = os.Mkdir(output, 0700); err != nil {
		return err
	}
	a := &phase4Artifacts{directory: output, files: map[string]*os.File{}}
	summaries := map[string]Phase4Summary{}
	stage := "budget"
	defer func() {
		if err := a.close(); err != nil && retErr == nil {
			retErr = err
		}
		manifest := map[string]string{}
		entries, err := os.ReadDir(output)
		if err != nil {
			if retErr == nil {
				retErr = err
			}
			return
		}
		for _, entry := range entries {
			if !entry.Type().IsRegular() {
				if retErr == nil {
					retErr = errors.New("nonregular evidence artifact")
				}
				continue
			}
			file, err := os.Open(filepath.Join(output, entry.Name()))
			if err != nil {
				if retErr == nil {
					retErr = err
				}
				continue
			}
			hash := sha256.New()
			_, err = io.Copy(hash, file)
			file.Close()
			if err != nil {
				if retErr == nil {
					retErr = err
				}
				continue
			}
			manifest[entry.Name()] = hex.EncodeToString(hash.Sum(nil))
		}
		manifestBytes, err := json.Marshal(manifest)
		if err == nil {
			err = phase4WriteFile(filepath.Join(output, "artifact-sha256.json"), append(manifestBytes, '\n'))
		}
		if err != nil && retErr == nil {
			retErr = err
		}
		result := map[string]any{"candidate": p.Candidate, "interface": p.Interface, "topology": p.Topology, "planSha256": planSHA256, "sourceBindings": p.Sources, "laneMeasurementPassed": retErr == nil, "globalCapacityQualified": false, "globalDauEnvelope": p.GlobalDAU, "assignedMarketMix": p.GlobalMix, "globalEvidenceTask": "9.1", "failureType": fmt.Sprintf("%T", retErr), "failureStage": stage, "failure": phase4Failure(retErr), "summaries": summaries, "artifactManifestSha256": phase4Hash(manifestBytes)}
		bytes, err := json.Marshal(result)
		if err == nil {
			err = phase4WriteFile(filepath.Join(output, "result.json"), append(bytes, '\n'))
		}
		if err != nil && retErr == nil {
			retErr = err
		}
	}()
	if err = phase4Budget(p, a); err != nil {
		return err
	}
	stage = "calibration"
	if err = phase4Calibration(ctx, p, a); err != nil {
		return err
	}
	stage = "providers"
	dsn, natsURL := os.Getenv("L14_PHASE4_POSTGRES_DSN"), os.Getenv("L14_PHASE4_NATS_URL")
	if dsn == "" || natsURL == "" {
		return errors.New("actual primary PostgreSQL and broker credentials missing")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid private PostgreSQL configuration")
	}
	config.MaxConns = 4
	pg, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer pg.Close()
	var commitAuthority string
	var replica bool
	if err = pg.QueryRow(ctx, "SELECT current_setting('track_commit_timestamp'),pg_is_in_recovery()").Scan(&commitAuthority, &replica); err != nil {
		return err
	}
	if commitAuthority != "on" || replica {
		return errors.New("actual commit timestamp enabled primary required")
	}
	nc, err := nats.Connect(natsURL, nats.Timeout(3*time.Second), nats.MaxReconnects(0))
	if err != nil {
		return err
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		return err
	}
	transport := &http.Transport{MaxConnsPerHost: p.Workers, MaxIdleConns: p.Workers * 2, MaxIdleConnsPerHost: p.Workers, ResponseHeaderTimeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirected origin denied") }}
	stage = "warmup"
	warmupDeadline := time.Now().Add(time.Duration(p.WarmupSeconds) * time.Second)
	for time.Now().Before(warmupDeadline) {
		if _, _, status, err := phase4HTTP(ctx, client, p.Origin+"/v1/pools/"+p.Pools[0].Pool+"/holders?limit=100"); err != nil || status != 200 {
			return errors.New("actual warmup request failed")
		}
	}
	var initialBaseline uint64
	for phaseIndex, phase := range p.Phases {
		stage = "measurement_" + phase.Name
		started := time.Now()
		baseline, err := phase4Observe(ctx, pg, js, p, a, phase.Name, started)
		if err != nil {
			return err
		}
		if phaseIndex == 0 {
			initialBaseline = baseline
		}
		phaseCtx, cancel := context.WithTimeout(ctx, time.Duration(phase.Seconds+30)*time.Second)
		measurement := make(chan error, 1)
		go func() {
			before, err := phase4Counters(p)
			if err != nil {
				measurement <- err
				return
			}
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				if err := phase4Hosts(phaseCtx, client, p, a, phase.Name); err != nil {
					if phaseCtx.Err() != nil {
						measurement <- nil
					} else {
						measurement <- err
					}
					return
				}
				if err := phase4Generator(p, a, phase.Name); err != nil {
					if phaseCtx.Err() != nil {
						measurement <- nil
					} else {
						measurement <- err
					}
					return
				}
				backlog, err := phase4Observe(phaseCtx, pg, js, p, a, phase.Name, started)
				if err != nil {
					if phaseCtx.Err() != nil {
						measurement <- nil
					} else {
						measurement <- err
					}
					return
				}
				if backlog > p.MaxBrokerBacklog {
					measurement <- errors.New("broker queue exceeds approved envelope")
					return
				}
				select {
				case <-phaseCtx.Done():
					measurement <- nil
					return
				case <-ticker.C:
				}
				after, err := phase4Counters(p)
				if err != nil {
					measurement <- err
					return
				}
				cpu, nic, saturationErr := phase4Saturation(before, after, p.GeneratorNICBitsPerSecond)
				if err = a.append("generator-intervals.ndjson", map[string]any{"phase": phase.Name, "before": before, "after": after, "cpuFraction": cpu, "nicFraction": nic, "targetPassed": saturationErr == nil}); err != nil {
					measurement <- err
					return
				}
				if saturationErr != nil {
					measurement <- saturationErr
					return
				}
				before = after
			}
		}()
		feeder := make(chan error, 1)
		if phase.Name == "burst" {
			go func() { feeder <- phase4Feeder(phaseCtx, p, a) }()
		} else {
			feeder <- nil
		}
		summary, readErr := phase4ReadPhase(phaseCtx, client, p, a, phase)
		summaries[phase.Name] = summary
		feederErr := <-feeder
		cancel()
		observerErr := <-measurement
		if readErr != nil {
			return readErr
		}
		if feederErr != nil {
			return feederErr
		}
		if observerErr != nil {
			return observerErr
		}
		end, err := phase4Observe(ctx, pg, js, p, a, phase.Name, started)
		if err != nil {
			return err
		}
		var projected int64
		if err = pg.QueryRow(ctx, "SELECT count(*) FROM "+pgx.Identifier{p.HolderSchema}.Sanitize()+".transfers WHERE pg_xact_commit_timestamp(xmin)>=$1", started).Scan(&projected); err != nil {
			return err
		}
		if projected == 0 {
			return errors.New("actual projection lag samples missing from phase")
		}
		var records int64
		if err = pg.QueryRow(ctx, "SELECT COALESCE(sum(record_count),0)::bigint FROM indexer.indexer_ingest_commits WHERE chain_id=125 AND stream_id=$1 AND pg_xact_commit_timestamp(xmin)>=$2", p.SourceStream, started).Scan(&records); err != nil {
			return err
		}
		if records < int64(phase.Seconds)*int64(phase.SourceEventsRPS) {
			return errors.New("measured real source event rate below declared envelope")
		}
		if phase.Name == "recovery" && end > initialBaseline {
			return errors.New("broker backlog failed to drain to pre-recovery baseline")
		}
		stage = "arithmetic_" + phase.Name
		if err = phase4Reconcile(ctx, pg, client, p, a); err != nil {
			return err
		}
	}
	return nil
}

func phase4WriteFile(path string, raw []byte) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(raw); err != nil {
		return err
	}
	return file.Sync()
}

func phase4BPS(balance, supply string) *string {
	b, ok := new(big.Int).SetString(balance, 10)
	if !ok || b.Sign() < 0 {
		return nil
	}
	s, ok := new(big.Int).SetString(supply, 10)
	if !ok || s.Sign() <= 0 || b.Cmp(s) > 0 {
		return nil
	}
	b.Mul(b, big.NewInt(10000))
	b.Quo(b, s)
	out := b.String()
	return &out
}

type phase4GeneratorCounters struct {
	At   time.Time
	CPU  uint64
	Idle uint64
	RX   uint64
	TX   uint64
}

func phase4Counters(p Phase4Plan) (out phase4GeneratorCounters, err error) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return out, err
	}
	line := strings.SplitN(string(raw), "\n", 2)[0]
	fields := strings.Fields(line)
	if len(fields) < 9 || fields[0] != "cpu" {
		return out, errors.New("actual CPU counters absent")
	}
	for i := 1; i <= 8; i++ {
		n, parseErr := strconv.ParseUint(fields[i], 10, 64)
		if parseErr != nil {
			return out, parseErr
		}
		out.CPU += n
		if i == 4 || i == 5 {
			out.Idle += n
		}
	}
	for _, kind := range []string{"rx_bytes", "tx_bytes"} {
		raw, err = os.ReadFile(filepath.Join("/sys/class/net", p.GeneratorNIC, "statistics", kind))
		if err != nil {
			return out, err
		}
		n, parseErr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		if parseErr != nil {
			return out, parseErr
		}
		if kind == "rx_bytes" {
			out.RX = n
		} else {
			out.TX = n
		}
	}
	out.At = time.Now()
	return out, nil
}
func phase4Saturation(before, after phase4GeneratorCounters, capacity uint64) (string, string, error) {
	if capacity == 0 || after.CPU <= before.CPU || after.Idle < before.Idle || after.RX < before.RX || after.TX < before.TX || !after.At.After(before.At) {
		return "", "", errors.New("missing/reset actual generator counter interval")
	}
	total, idle := after.CPU-before.CPU, after.Idle-before.Idle
	if idle > total {
		return "", "", errors.New("inconsistent actual CPU counters")
	}
	cpu := new(big.Rat).SetFrac(new(big.Int).SetUint64(total-idle), new(big.Int).SetUint64(total))
	bytes := after.RX - before.RX
	if tx := after.TX - before.TX; tx > bytes {
		bytes = tx
	}
	numerator := new(big.Int).SetUint64(bytes)
	numerator.Mul(numerator, big.NewInt(8000000000))
	denominator := new(big.Int).SetUint64(capacity)
	denominator.Mul(denominator, big.NewInt(after.At.Sub(before.At).Nanoseconds()))
	nic := new(big.Rat).SetFrac(numerator, denominator)
	if cpu.Cmp(big.NewRat(7, 10)) > 0 || nic.Cmp(big.NewRat(7, 10)) > 0 {
		return cpu.RatString(), nic.RatString(), errors.New("actual generator CPU/NIC saturation exceeds70percent")
	}
	return cpu.RatString(), nic.RatString(), nil
}

type phase4BoundedWriter struct {
	target    io.Writer
	remaining int64
}

func (w *phase4BoundedWriter) Write(raw []byte) (int, error) {
	if int64(len(raw)) > w.remaining {
		return 0, errors.New("source feeder log byte budget exceeded")
	}
	n, err := w.target.Write(raw)
	w.remaining -= int64(n)
	return n, err
}

func phase4Failure(err error) string {
	if err == nil {
		return ""
	}
	var database *pgconn.PgError
	if errors.As(err, &database) {
		return "postgres_sqlstate_" + database.Code
	}
	var path *os.PathError
	if errors.As(err, &path) {
		return "filesystem_" + path.Op
	}
	var transport *url.Error
	if errors.As(err, &transport) {
		return "http_transport_" + transport.Op
	}
	message := err.Error()
	for _, key := range []string{"L14_PHASE4_POSTGRES_DSN", "L14_PHASE4_NATS_URL"} {
		secret := os.Getenv(key)
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "<private-connection>")
		}
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return message
}
