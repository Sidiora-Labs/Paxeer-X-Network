package notifications

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/discovery"
)

type ReleaseBinding struct {
	CandidateDigest string `json:"candidate_digest"`
	InterfaceDigest string `json:"interface_digest"`
	TopologyDigest  string `json:"topology_digest"`
	DatasetDigest   string `json:"dataset_digest"`
	SchemaVersion   string `json:"schema_version"`
	PolicyVersion   string `json:"policy_version"`
}

type ReleaseAssertion struct {
	Pointer     string          `json:"pointer"`
	Equals      json.RawMessage `json:"equals,omitempty"`
	FromStep    string          `json:"from_step,omitempty"`
	FromPointer string          `json:"from_pointer,omitempty"`
}

type ReleaseStep struct {
	Name       string             `json:"name"`
	Method     string             `json:"method"`
	URL        string             `json:"url"`
	BearerEnv  string             `json:"bearer_env"`
	Body       json.RawMessage    `json:"body,omitempty"`
	Status     int                `json:"status"`
	Assertions []ReleaseAssertion `json:"assertions"`
}

type ReleaseConfig struct {
	Binding                ReleaseBinding    `json:"binding"`
	SourceRoot             string            `json:"source_root"`
	TopologyFile           string            `json:"topology_file"`
	InterfaceFile          string            `json:"interface_file"`
	SourceFiles            map[string]string `json:"source_files"`
	OutputDir              string            `json:"output_dir"`
	TriggerFile            string            `json:"trigger_file"`
	AllowedAccounts        []string          `json:"allowed_accounts"`
	AllowedOrigins         []string          `json:"allowed_origins"`
	IsolatedSandbox        bool              `json:"isolated_sandbox"`
	Concurrency            int               `json:"concurrency"`
	QueueCapacity          int               `json:"queue_capacity"`
	MaxCaptureBytes        int64             `json:"max_capture_bytes"`
	Steps                  []ReleaseStep     `json:"steps"`
	DeviceReceiptFile      string            `json:"device_receipt_file"`
	DeviceCaptureKeys      map[string]string `json:"device_capture_keys"`
	AppBuildDigests        map[string]string `json:"app_build_digests"`
	GlobalArtifacts        map[string]string `json:"global_artifacts"`
	GlobalCaptureKeys      map[string]string `json:"global_capture_keys"`
	MaxSocketQueueMessages int64             `json:"max_socket_queue_messages"`
	RequireGlobal          bool              `json:"require_global"`
}

type ReleaseSample struct {
	Kind             string          `json:"kind"`
	At               time.Time       `json:"at"`
	Identity         string          `json:"identity"`
	ElapsedNS        int64           `json:"elapsed_ns"`
	SourceRevision   uint64          `json:"source_revision,omitempty"`
	SourceObservedAt time.Time       `json:"source_observed_at,omitempty"`
	SourceLagNS      int64           `json:"source_lag_ns,omitempty"`
	Status           int             `json:"status,omitempty"`
	Created          bool            `json:"created,omitempty"`
	Error            string          `json:"error,omitempty"`
	Response         json.RawMessage `json:"response,omitempty"`
}

type ReleaseQueueSample struct {
	At                   time.Time `json:"at"`
	Expected             int64     `json:"expected"`
	Inbox                int64     `json:"inbox"`
	Missing              int64     `json:"missing"`
	Duplicates           int64     `json:"duplicates"`
	Pending              int64     `json:"pending"`
	Unknown              int64     `json:"unknown"`
	Accepted             int64     `json:"accepted"`
	Rejected             int64     `json:"rejected"`
	Suppressed           int64     `json:"suppressed"`
	OldestPendingSeconds float64   `json:"oldest_pending_seconds"`
}

type ReleaseLatency struct {
	Count int   `json:"count"`
	P50NS int64 `json:"p50_ns"`
	P95NS int64 `json:"p95_ns"`
	P99NS int64 `json:"p99_ns"`
	MaxNS int64 `json:"max_ns"`
}

type ReleaseResult struct {
	Binding              ReleaseBinding            `json:"binding"`
	StartedAt            time.Time                 `json:"started_at"`
	FinishedAt           time.Time                 `json:"finished_at"`
	LanePassed           bool                      `json:"lane_passed"`
	Qualified            bool                      `json:"qualified"`
	GlobalStatus         string                    `json:"global_status"`
	Failures             []string                  `json:"failures"`
	Scheduled            int64                     `json:"scheduled"`
	Completed            int64                     `json:"completed"`
	Created              int64                     `json:"created"`
	Succeeded            int64                     `json:"succeeded"`
	AdmissionFailures    int64                     `json:"admission_failures"`
	BurstDurationNS      int64                     `json:"burst_duration_ns"`
	DrainDurationNS      int64                     `json:"drain_duration_ns"`
	TriggerLatency       ReleaseLatency            `json:"trigger_latency"`
	HTTPLatency          map[string]ReleaseLatency `json:"http_latency"`
	DeviceReceiveLatency map[string]ReleaseLatency `json:"device_receive_latency"`
	Files                map[string]string         `json:"files"`
}

func releaseDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func (c ReleaseConfig) Validate() error {
	for _, digest := range []string{c.Binding.CandidateDigest, c.Binding.InterfaceDigest, c.Binding.TopologyDigest, c.Binding.DatasetDigest} {
		if !releaseDigest(digest) {
			return fmt.Errorf("release binding digest: %w", ErrInvalid)
		}
	}
	if c.Binding.SchemaVersion == "" || c.Binding.PolicyVersion == "" || !c.IsolatedSandbox || c.SourceRoot == "" || c.TopologyFile == "" || c.InterfaceFile == "" || c.OutputDir == "" || c.TriggerFile == "" || len(c.SourceFiles) == 0 || len(c.AllowedAccounts) == 0 || len(c.AllowedOrigins) == 0 || c.Concurrency < 1 || c.Concurrency > 256 || c.QueueCapacity < 1 || c.QueueCapacity > 16000 || c.MaxCaptureBytes < 1 || c.MaxCaptureBytes > 10<<30 {
		return ErrInvalid
	}
	accounts := map[string]bool{}
	for _, a := range c.AllowedAccounts {
		if a == "" || accounts[a] {
			return ErrInvalid
		}
		accounts[a] = true
	}
	origins := map[string]bool{}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return ErrInvalid
		}
		origins[origin] = true
	}
	required := map[string]bool{"pool_launch": false, "callout_publish": false, "follow_author": false, "squad_join": false, "threshold_alert": false, "inbox": false, "feed": false, "leaderboard": false}
	seen := map[string]bool{}
	for _, step := range c.Steps {
		if _, ok := required[step.Name]; !ok || seen[step.Name] || step.Status < 200 || step.Status >= 300 || len(step.Assertions) == 0 {
			return ErrInvalid
		}
		if step.Method != "GET" && step.Method != "POST" && step.Method != "PUT" && step.Method != "PATCH" {
			return ErrInvalid
		}
		u, err := url.Parse(step.URL)
		if err != nil || u.User != nil || !origins[u.Scheme+"://"+u.Host] || u.Fragment != "" {
			return ErrInvalid
		}
		for _, assertion := range step.Assertions {
			if assertion.Pointer == "" || (len(assertion.Equals) == 0) == (assertion.FromStep == "") || (assertion.FromStep != "" && (!seen[assertion.FromStep] || assertion.FromPointer == "")) {
				return ErrInvalid
			}
		}
		if step.Name == "callout_publish" || step.Name == "threshold_alert" {
			bound := false
			for _, assertion := range step.Assertions {
				if assertion.FromStep == "pool_launch" {
					bound = true
				}
			}
			if !bound {
				return fmt.Errorf("%s must bind actual canonical launch identity", step.Name)
			}
		}
		if step.Name == "feed" {
			bound := false
			for _, assertion := range step.Assertions {
				if assertion.FromStep == "callout_publish" {
					bound = true
				}
			}
			if !bound {
				return fmt.Errorf("feed must bind actual callout identity")
			}
		}
		if step.Name == "inbox" {
			bound := false
			for _, assertion := range step.Assertions {
				if assertion.FromStep == "threshold_alert" {
					bound = true
				}
			}
			if !bound {
				return fmt.Errorf("inbox must bind actual threshold identity")
			}
		}
		required[step.Name] = true
		seen[step.Name] = true
	}
	for _, present := range required {
		if !present {
			return ErrInvalid
		}
	}
	if c.DeviceReceiptFile == "" || len(c.DeviceCaptureKeys) == 0 || !releaseDigest(c.AppBuildDigests["apns"]) || !releaseDigest(c.AppBuildDigests["fcm"]) {
		return fmt.Errorf("real sandbox device capture inputs: %w", ErrUnavailable)
	}
	return nil
}

func releasePointer(raw json.RawMessage, pointer string) (json.RawMessage, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if pointer == "" {
		return json.Marshal(value)
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, ErrInvalid
	}
	for _, part := range strings.Split(pointer[1:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch current := value.(type) {
		case map[string]any:
			var found bool
			value, found = current[part]
			if !found {
				return nil, ErrInvalid
			}
		case []any:
			var i int
			if _, err := fmt.Sscanf(part, "%d", &i); err != nil || fmt.Sprint(i) != part || i < 0 || i >= len(current) {
				return nil, ErrInvalid
			}
			value = current[i]
		default:
			return nil, ErrInvalid
		}
	}
	return json.Marshal(value)
}

func releaseQuantiles(samples []int64) ReleaseLatency {
	out := ReleaseLatency{Count: len(samples)}
	if len(samples) == 0 {
		return out
	}
	values := append([]int64(nil), samples...)
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	quantile := func(percent int) int64 { return values[(len(values)*percent+99)/100-1] }
	out.P50NS = quantile(50)
	out.P95NS = quantile(95)
	out.P99NS = quantile(99)
	out.MaxNS = values[len(values)-1]
	return out
}

type releaseCapture struct {
	mu           sync.Mutex
	file         *os.File
	written, max int64
}

func (w *releaseCapture) append(record any) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	w.mu.Lock()
	defer w.mu.Unlock()
	if int64(len(raw)) > w.max-w.written {
		return fmt.Errorf("raw capture budget exhausted: %w", ErrUnavailable)
	}
	n, err := w.file.Write(raw)
	w.written += int64(n)
	return err
}

func releaseLimitedFile(path string, max int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > max {
		return nil, ErrInvalid
	}
	return raw, nil
}

func releaseFileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err = io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c ReleaseConfig) bindSource() error {
	names := make([]string, 0, len(c.SourceFiles))
	for name, want := range c.SourceFiles {
		clean := filepath.Clean(name)
		if filepath.IsAbs(name) || clean != name || strings.HasPrefix(clean, "..") || !releaseDigest(want) {
			return ErrInvalid
		}
		got, err := releaseFileDigest(filepath.Join(c.SourceRoot, name))
		if err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("candidate file drift: %s", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		fmt.Fprintf(h, "%s\x00%s\n", name, c.SourceFiles[name])
	}
	if hex.EncodeToString(h.Sum(nil)) != c.Binding.CandidateDigest {
		return fmt.Errorf("candidate manifest digest mismatch")
	}
	digest, err := releaseFileDigest(c.TriggerFile)
	if err != nil {
		return err
	}
	if digest != c.Binding.DatasetDigest {
		return fmt.Errorf("real trigger dataset digest mismatch")
	}
	for path, want := range map[string]string{c.TopologyFile: c.Binding.TopologyDigest, c.InterfaceFile: c.Binding.InterfaceDigest} {
		got, e := releaseFileDigest(path)
		if e != nil {
			return e
		}
		if got != want {
			return fmt.Errorf("release authority artifact digest mismatch")
		}
	}
	return nil
}

func releaseReadTriggers(path string, consume func(discovery.AlertTrigger, json.RawMessage) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 65536)
	for scanner.Scan() {
		raw := append(json.RawMessage(nil), scanner.Bytes()...)
		var trigger discovery.AlertTrigger
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&trigger); err != nil {
			return err
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return ErrInvalid
		}
		if trigger.ID == "" || trigger.CreatedAt.IsZero() || trigger.Rule.Validate() != nil || trigger.Metric.Revision == 0 || trigger.Metric.Quality != "complete" || trigger.Metric.ObservedAt.IsZero() {
			return ErrInvalid
		}
		if err = consume(trigger, raw); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func releasePrepareCohort(ctx context.Context, db *sql.DB, c ReleaseConfig) (*sql.Conn, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	if _, err = conn.ExecContext(ctx, `CREATE TEMP TABLE release_notification_cohort(id text PRIMARY KEY,trigger_id text UNIQUE NOT NULL,account_id text NOT NULL,document jsonb NOT NULL) ON COMMIT PRESERVE ROWS`); err != nil {
		conn.Close()
		return nil, err
	}
	accounts := map[string]bool{}
	for _, a := range c.AllowedAccounts {
		accounts[a] = true
	}
	keys, events, owners, documents := []string{}, []string{}, []string{}, []string{}
	total := 0
	flush := func() error {
		if len(keys) == 0 {
			return nil
		}
		_, e := conn.ExecContext(ctx, `INSERT INTO release_notification_cohort SELECT id,trigger_id,account_id,document::jsonb FROM unnest($1::text[],$2::text[],$3::text[],$4::text[]) AS input(id,trigger_id,account_id,document)`, keys, events, owners, documents)
		keys, events, owners, documents = []string{}, []string{}, []string{}, []string{}
		return e
	}
	err = releaseReadTriggers(c.TriggerFile, func(t discovery.AlertTrigger, raw json.RawMessage) error {
		if !accounts[t.Rule.OwnerAccountID] || t.CreatedAt.After(time.Now()) || time.Since(t.CreatedAt) > 10*time.Minute || t.Metric.ObservedAt.After(t.CreatedAt) || t.CreatedAt.Sub(t.Metric.ObservedAt) > 10*time.Second {
			return fmt.Errorf("trigger outside isolated or fresh authority: %w", ErrForbidden)
		}
		if t.ID != fmt.Sprintf("%s:%d:%d", t.Rule.ID, t.Rule.Revision, t.Metric.Revision) {
			return ErrInvalid
		}
		key, e := (Identity{t.Rule.OwnerAccountID, t.ID, "market-crossing-v2"}).Key()
		if e != nil {
			return e
		}
		keys = append(keys, key)
		events = append(events, t.ID)
		owners = append(owners, t.Rule.OwnerAccountID)
		documents = append(documents, string(raw))
		total++
		if total > 1200000 {
			return ErrInvalid
		}
		if len(keys) == 1000 {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err == nil && total != 1200000 {
		err = fmt.Errorf("need 1200000 actual trigger events; got %d", total)
	}
	if err != nil {
		conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS release_notification_cohort`)
		conn.Close()
		return nil, err
	}
	var missingAuthority int64
	err = conn.QueryRowContext(ctx, `SELECT count(*) FROM release_notification_cohort c LEFT JOIN narrative_discovery.triggers t ON t.id=c.trigger_id AND t.document=c.document WHERE t.id IS NULL`).Scan(&missingAuthority)
	if err == nil && missingAuthority != 0 {
		err = fmt.Errorf("trigger dataset lacks %d exact canonical durable source events", missingAuthority)
	}
	if err != nil {
		conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS release_notification_cohort`)
		conn.Close()
		return nil, err
	}
	var existing int64
	err = conn.QueryRowContext(ctx, `SELECT count(*) FROM release_notification_cohort c JOIN discovery_notifications.inbox i ON i.id=c.id`).Scan(&existing)
	if err == nil && existing != 0 {
		err = fmt.Errorf("trigger cohort already has inbox effects")
	}
	if err != nil {
		conn.ExecContext(context.Background(), `DROP TABLE IF EXISTS release_notification_cohort`)
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func releaseQueue(ctx context.Context, conn *sql.Conn) (ReleaseQueueSample, error) {
	out := ReleaseQueueSample{At: time.Now().UTC()}
	err := conn.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM release_notification_cohort),count(i.id),count(*) FILTER(WHERE i.id IS NULL),count(i.id)-count(DISTINCT i.id),count(*) FILTER(WHERE o.id IS NOT NULL AND NOT o.terminal),COALESCE(max(EXTRACT(epoch FROM clock_timestamp()-i.created_at)) FILTER(WHERE NOT o.terminal),0) FROM release_notification_cohort c LEFT JOIN discovery_notifications.inbox i ON i.id=c.id AND i.account_id=c.account_id AND i.trigger_id=c.trigger_id AND i.template_version='market-crossing-v2' LEFT JOIN discovery_notifications.outbox o ON o.id=i.id`).Scan(&out.Expected, &out.Inbox, &out.Missing, &out.Duplicates, &out.Pending, &out.OldestPendingSeconds)
	if err != nil {
		return out, err
	}
	err = conn.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE d->>'State'='unknown'),count(*) FILTER(WHERE d->>'State'='accepted'),count(*) FILTER(WHERE d->>'State'='rejected'),count(*) FILTER(WHERE d->>'State'='suppressed') FROM release_notification_cohort c JOIN discovery_notifications.outbox o ON o.id=c.id CROSS JOIN LATERAL jsonb_array_elements(o.deliveries) d`).Scan(&out.Unknown, &out.Accepted, &out.Rejected, &out.Suppressed)
	return out, err
}

func releaseJourney(ctx context.Context, c ReleaseConfig, client *http.Client, capture *releaseCapture, result *ReleaseResult) error {
	responses := map[string]json.RawMessage{}
	for _, step := range c.Steps {
		req, err := http.NewRequestWithContext(ctx, step.Method, step.URL, bytes.NewReader(step.Body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		if step.BearerEnv != "" {
			token := os.Getenv(step.BearerEnv)
			if token == "" {
				return fmt.Errorf("missing journey credential for %s", step.Name)
			}
			req.Header.Set("Authorization", "Bearer "+token)
		}
		start := time.Now()
		response, err := client.Do(req)
		elapsed := time.Since(start)
		sample := ReleaseSample{Kind: "journey", At: time.Now().UTC(), Identity: step.Name, ElapsedNS: elapsed.Nanoseconds()}
		if err != nil {
			sample.Error = "transport_unavailable"
			if captureErr := capture.append(sample); captureErr != nil {
				return captureErr
			}
			return fmt.Errorf("real %s transport unavailable", step.Name)
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, 1048577))
		response.Body.Close()
		if readErr != nil || len(raw) > 1048576 {
			return ErrUnavailable
		}
		sample.Status = response.StatusCode
		if json.Valid(raw) {
			sample.Response = raw
		} else {
			return fmt.Errorf("%s returned non-JSON trace", step.Name)
		}
		if err = capture.append(sample); err != nil {
			return err
		}
		result.HTTPLatency[step.Name] = releaseQuantiles([]int64{elapsed.Nanoseconds()})
		if response.StatusCode != step.Status {
			return fmt.Errorf("%s actual status %d, wanted %d", step.Name, response.StatusCode, step.Status)
		}
		for _, assertion := range step.Assertions {
			got, e := releasePointer(raw, assertion.Pointer)
			if e != nil {
				return fmt.Errorf("%s missing assertion pointer", step.Name)
			}
			expected := assertion.Equals
			if assertion.FromStep != "" {
				expected, e = releasePointer(responses[assertion.FromStep], assertion.FromPointer)
				if e != nil {
					return e
				}
				if bytes.Equal(expected, []byte("null")) || bytes.Equal(expected, []byte(`""`)) {
					return fmt.Errorf("missing actual identity binding")
				}
			}
			want, e := releasePointer(expected, "")
			if e != nil {
				return e
			}
			if !bytes.Equal(got, want) {
				return fmt.Errorf("%s response identity assertion failed at %s", step.Name, assertion.Pointer)
			}
		}
		responses[step.Name] = raw
	}
	return nil
}

type releaseJob struct {
	trigger   discovery.AlertTrigger
	raw       json.RawMessage
	scheduled time.Time
}

func releaseBurst(ctx context.Context, c ReleaseConfig, consumer TriggerConsumer, capture *releaseCapture, result *ReleaseResult) error {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan releaseJob, c.QueueCapacity)
	var wg sync.WaitGroup
	var mu sync.Mutex
	latencies := make([]int64, 0, 1200000)
	var failure error
	for i := 0; i < c.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				e := consumer.Publish(run, "threshold-crossing-v2", job.trigger.ID, job.raw)
				sample := ReleaseSample{Kind: "trigger", At: time.Now().UTC(), Identity: job.trigger.ID, ElapsedNS: time.Since(job.scheduled).Nanoseconds(), SourceRevision: job.trigger.Metric.Revision, SourceObservedAt: job.trigger.Metric.ObservedAt, SourceLagNS: time.Since(job.trigger.Metric.ObservedAt).Nanoseconds()}
				if e != nil {
					sample.Error = "trigger_not_committed"
				}
				captureErr := capture.append(sample)
				mu.Lock()
				result.Completed++
				latencies = append(latencies, sample.ElapsedNS)
				if e == nil {
					result.Succeeded++
				} else if failure == nil {
					failure = fmt.Errorf("actual trigger consumer failure: %w", e)
				}
				if captureErr != nil && failure == nil {
					failure = captureErr
					cancel()
				}
				mu.Unlock()
			}
		}()
	}
	start := time.Now()
	err := releaseReadTriggers(c.TriggerFile, func(t discovery.AlertTrigger, raw json.RawMessage) error {
		due := start.Add(time.Duration(result.Scheduled) * time.Second / 4000)
		if wait := time.Until(due); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-run.Done():
				timer.Stop()
				return run.Err()
			}
		}
		result.Scheduled++
		job := releaseJob{t, raw, due}
		select {
		case jobs <- job:
			return nil
		default:
			result.AdmissionFailures++
			return fmt.Errorf("bounded trigger admission exhausted")
		}
	})
	close(jobs)
	wg.Wait()
	result.BurstDurationNS = time.Since(start).Nanoseconds()
	result.TriggerLatency = releaseQuantiles(latencies)
	if err != nil {
		return err
	}
	if failure != nil {
		return failure
	}
	if result.Scheduled != 1200000 || result.Completed != 1200000 || result.Succeeded != 1200000 || result.AdmissionFailures != 0 {
		return fmt.Errorf("incomplete actual burst")
	}
	if time.Since(start) > 301*time.Second {
		return fmt.Errorf("actual 4000/s burst missed scheduling budget")
	}
	return nil
}

type ReleaseDeviceReceipt struct {
	NotificationID       string    `json:"notification_id"`
	Provider             string    `json:"provider"`
	ProviderReceipt      string    `json:"provider_receipt"`
	DeviceInstallationID string    `json:"device_installation_id"`
	AppBuildDigest       string    `json:"app_build_digest"`
	Sandbox              bool      `json:"sandbox"`
	ReceivedAt           time.Time `json:"received_at"`
	CaptureKeyID         string    `json:"capture_key_id"`
	Signature            string    `json:"signature"`
}

func releaseDevices(ctx context.Context, db *sql.DB, c ReleaseConfig, started time.Time, capture *releaseCapture, result *ReleaseResult) error {
	raw, err := releaseLimitedFile(c.DeviceReceiptFile, 1048576)
	if err != nil {
		return err
	}
	var receipts []ReleaseDeviceReceipt
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&receipts); err != nil {
		return err
	}
	var extra any
	if err = decoder.Decode(&extra); err != io.EOF {
		return ErrInvalid
	}
	seen := map[string]bool{}
	latency := map[string][]int64{}
	for _, receipt := range receipts {
		if receipt.Provider != "apns" && receipt.Provider != "fcm" {
			return ErrInvalid
		}
		if !receipt.Sandbox || receipt.ProviderReceipt == "" || receipt.NotificationID == "" || receipt.DeviceInstallationID == "" || receipt.AppBuildDigest != c.AppBuildDigests[receipt.Provider] || receipt.ReceivedAt.Before(started) || receipt.ReceivedAt.After(time.Now()) {
			return ErrInvalid
		}
		key, err := base64.StdEncoding.DecodeString(c.DeviceCaptureKeys[receipt.CaptureKeyID])
		if err != nil || len(key) != ed25519.PublicKeySize {
			return ErrForbidden
		}
		originalSignature := receipt.Signature
		signature, err := base64.StdEncoding.DecodeString(receipt.Signature)
		if err != nil {
			return ErrForbidden
		}
		receipt.Signature = ""
		message, err := json.Marshal(receipt)
		if err != nil || !ed25519.Verify(ed25519.PublicKey(key), message, signature) {
			return ErrForbidden
		}
		var accepted bool
		var createdAt time.Time
		err = db.QueryRowContext(ctx, `SELECT i.created_at,EXISTS(SELECT 1 FROM discovery_notifications.outbox o CROSS JOIN LATERAL jsonb_array_elements(o.deliveries) d WHERE o.id=i.id AND d->>'TargetID'=$2 AND d->>'State'='accepted' AND d->>'ProviderReceipt'=$3) FROM discovery_notifications.inbox i WHERE i.id=$1 AND i.created_at>=$4`, receipt.NotificationID, receipt.DeviceInstallationID, receipt.ProviderReceipt, started).Scan(&createdAt, &accepted)
		if err != nil {
			return err
		}
		if !accepted || receipt.ReceivedAt.Before(createdAt) {
			return fmt.Errorf("device receipt lacks matching accepted provider effect")
		}
		seen[receipt.Provider] = true
		latency[receipt.Provider] = append(latency[receipt.Provider], receipt.ReceivedAt.Sub(createdAt).Nanoseconds())
		receipt.Signature = originalSignature
		if err = capture.append(receipt); err != nil {
			return err
		}
	}
	if !seen["apns"] || !seen["fcm"] {
		return fmt.Errorf("both actual signed application device receipts required")
	}
	for provider, samples := range latency {
		result.DeviceReceiveLatency[provider] = releaseQuantiles(samples)
	}
	return nil
}

type ReleaseGlobalArtifact struct {
	Binding    ReleaseBinding    `json:"binding"`
	Gate       string            `json:"gate"`
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at"`
	RawFiles   map[string]string `json:"raw_files"`
}

type ReleaseGlobalObservation struct {
	Binding                  ReleaseBinding   `json:"binding"`
	Gate                     string           `json:"gate"`
	At                       time.Time        `json:"at"`
	CaptureKeyID             string           `json:"capture_key_id"`
	Signature                string           `json:"signature"`
	Kind                     string           `json:"kind"`
	ActiveSockets            int64            `json:"active_sockets"`
	MaxQueuedMessages        int64            `json:"max_queued_messages"`
	SnapshotRecoveryFailures int64            `json:"snapshot_recovery_failures"`
	LostAZCount              int64            `json:"lost_az_count"`
	MissingAcknowledgedRules int64            `json:"missing_acknowledged_rules"`
	MissingAcknowledgedInbox int64            `json:"missing_acknowledged_inbox"`
	CatchupNS                int64            `json:"catchup_ns"`
	OriginHistogram          map[string]int64 `json:"origin_histogram"`
	HTTPRequestsTotal        int64            `json:"http_requests_total"`
	Channel                  string           `json:"channel"`
	ChannelLatencyNS         int64            `json:"channel_latency_ns"`
}

type releaseGlobalTotals struct {
	first, last                                        time.Time
	surgeFirst, surgeLast                              time.Time
	peak, origin, within150, within400, channelSamples int64
	requestFirst, requestLast                          int64
	maxRPS                                             float64
	observedAZ                                         bool
	originFirst, originLast                            time.Time
	seen                                               map[string]bool
}

func releaseRawGlobal(c ReleaseConfig, a ReleaseGlobalArtifact) (releaseGlobalTotals, error) {
	total := releaseGlobalTotals{seen: map[string]bool{}}
	files := make([]string, 0, len(a.RawFiles))
	for file := range a.RawFiles {
		files = append(files, file)
	}
	sort.Strings(files)
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			return total, err
		}
		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 4096), 65536)
		for scanner.Scan() {
			var o ReleaseGlobalObservation
			decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
			decoder.DisallowUnknownFields()
			if err = decoder.Decode(&o); err != nil {
				f.Close()
				return total, err
			}
			var extra any
			if err = decoder.Decode(&extra); err != io.EOF {
				f.Close()
				return total, ErrInvalid
			}
			signature, err := base64.StdEncoding.DecodeString(o.Signature)
			if err != nil {
				f.Close()
				return total, ErrForbidden
			}
			key, err := base64.StdEncoding.DecodeString(c.GlobalCaptureKeys[o.CaptureKeyID])
			if err != nil || len(key) != ed25519.PublicKeySize {
				f.Close()
				return total, ErrForbidden
			}
			o.Signature = ""
			message, err := json.Marshal(o)
			if err != nil || !ed25519.Verify(ed25519.PublicKey(key), message, signature) {
				f.Close()
				return total, ErrForbidden
			}
			identity := sha256.Sum256(message)
			sampleID := hex.EncodeToString(identity[:])
			if total.seen[sampleID] || len(total.seen) >= 2000000 {
				f.Close()
				return total, fmt.Errorf("duplicated or unbounded global raw capture")
			}
			total.seen[sampleID] = true
			if o.Binding != c.Binding || o.Gate != a.Gate || o.At.Before(a.StartedAt) || o.At.After(a.FinishedAt) || (!total.last.IsZero() && o.At.Before(total.last)) {
				f.Close()
				return total, ErrInvalid
			}
			if total.first.IsZero() {
				total.first = o.At
			}
			total.last = o.At
			switch a.Gate {
			case "socket_surge":
				if c.MaxSocketQueueMessages <= 0 || o.Kind != "sockets" || o.ActiveSockets < 0 || o.MaxQueuedMessages < 0 || o.MaxQueuedMessages > c.MaxSocketQueueMessages || o.SnapshotRecoveryFailures != 0 {
					f.Close()
					return total, fmt.Errorf("socket recovery or queue bound failed")
				}
				total.peak = max(total.peak, o.ActiveSockets)
				if o.ActiveSockets >= 200000 {
					if total.surgeFirst.IsZero() {
						total.surgeFirst = o.At
					}
					total.surgeLast = o.At
				} else if !total.surgeFirst.IsZero() && total.surgeLast.Sub(total.surgeFirst) < 600*time.Second {
					total.surgeFirst = time.Time{}
					total.surgeLast = time.Time{}
				}
			case "az_loss":
				if o.Kind != "recovery" || o.LostAZCount < 0 || o.MissingAcknowledgedRules != 0 || o.MissingAcknowledgedInbox != 0 || o.CatchupNS < 0 || time.Duration(o.CatchupNS) > 10*time.Minute {
					f.Close()
					return total, fmt.Errorf("actual AZ recovery integrity failed")
				}
				if o.LostAZCount >= 1 {
					total.observedAZ = true
				}
			case "mixed_soak":
				switch o.Kind {
				case "origin":
					if o.HTTPRequestsTotal < 0 || o.HTTPRequestsTotal < total.requestLast || len(o.OriginHistogram) == 0 || len(o.OriginHistogram) > 256 {
						f.Close()
						return total, ErrInvalid
					}
					var measured int64
					for bound, count := range o.OriginHistogram {
						upper, e := strconv.ParseInt(bound, 10, 64)
						if e != nil || upper < 0 || upper > int64(time.Minute) || count < 0 || count > 1000000000 || measured > 1000000000-count {
							f.Close()
							return total, ErrInvalid
						}
						measured += count
						if upper <= int64(150*time.Millisecond) {
							total.within150 += count
						}
						if upper <= int64(400*time.Millisecond) {
							total.within400 += count
						}
					}
					if measured != o.HTTPRequestsTotal-total.requestLast {
						f.Close()
						return total, fmt.Errorf("latency histogram does not cover actual origin request counter")
					}
					if total.originFirst.IsZero() {
						total.originFirst = o.At
						total.requestFirst = o.HTTPRequestsTotal
					}
					if !total.originLast.IsZero() {
						seconds := o.At.Sub(total.originLast).Seconds()
						if seconds <= 0 {
							f.Close()
							return total, ErrInvalid
						}
						total.maxRPS = max(total.maxRPS, float64(measured)/seconds)
					}
					total.originLast = o.At
					total.requestLast = o.HTTPRequestsTotal
					total.origin += measured
				case "channel":
					if o.Channel == "" || o.ChannelLatencyNS < 0 {
						f.Close()
						return total, ErrInvalid
					}
					total.channelSamples++
				default:
					f.Close()
					return total, ErrInvalid
				}
			default:
				f.Close()
				return total, ErrInvalid
			}
		}
		err = scanner.Err()
		f.Close()
		if err != nil {
			return total, err
		}
	}
	if len(total.seen) == 0 {
		return total, ErrUnavailable
	}
	switch a.Gate {
	case "socket_surge":
		if total.peak < 200000 || total.surgeFirst.IsZero() || total.surgeLast.Sub(total.surgeFirst) < 600*time.Second {
			return total, fmt.Errorf("actual 200000 socket ten-minute surge missing")
		}
	case "az_loss":
		if !total.observedAZ {
			return total, fmt.Errorf("actual AZ loss observation missing")
		}
	case "mixed_soak":
		elapsed := total.originLast.Sub(total.originFirst)
		if total.last.Sub(total.first) < 24*time.Hour || elapsed <= 0 || total.origin == 0 || total.within150*100 < total.origin*95 || total.within400*100 < total.origin*99 || total.channelSamples == 0 || total.maxRPS < 10000 {
			return total, fmt.Errorf("actual mixed soak percentile/rate/channel bounds failed")
		}
	}
	return total, nil
}

func releaseGlobals(c ReleaseConfig, result *ReleaseResult) error {
	gates := []string{"socket_surge", "az_loss", "mixed_soak"}
	for _, gate := range gates {
		path := c.GlobalArtifacts[gate]
		if path == "" {
			result.GlobalStatus = "deferred_to_serial_9.1"
			if c.RequireGlobal {
				return fmt.Errorf("missing actual global gate %s", gate)
			}
			return nil
		}
		raw, err := releaseLimitedFile(path, 1048576)
		if err != nil {
			return err
		}
		var artifact ReleaseGlobalArtifact
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&artifact); err != nil {
			return err
		}
		var extra any
		if err = decoder.Decode(&extra); err != io.EOF {
			return ErrInvalid
		}
		if artifact.Binding != c.Binding || artifact.Gate != gate || artifact.StartedAt.IsZero() || !artifact.FinishedAt.After(artifact.StartedAt) || artifact.FinishedAt.After(time.Now()) || len(artifact.RawFiles) == 0 {
			return ErrInvalid
		}
		for file, want := range artifact.RawFiles {
			if !releaseDigest(want) {
				return ErrInvalid
			}
			got, e := releaseFileDigest(file)
			if e != nil {
				return e
			}
			if got != want {
				return fmt.Errorf("global raw evidence drift")
			}
			result.Files[file] = got
		}
		if _, err = releaseRawGlobal(c, artifact); err != nil {
			return err
		}
		digest, err := releaseFileDigest(path)
		if err != nil {
			return err
		}
		result.Files[path] = digest
	}
	result.GlobalStatus = "imported_actual_serial_evidence"
	return nil
}

func RunReleaseMeasurements(ctx context.Context, c ReleaseConfig, db *sql.DB, authority TargetAuthority, client *http.Client) (result ReleaseResult, err error) {
	result = ReleaseResult{Binding: c.Binding, StartedAt: time.Now().UTC(), GlobalStatus: "not_run", Failures: []string{}, HTTPLatency: map[string]ReleaseLatency{}, DeviceReceiveLatency: map[string]ReleaseLatency{}, Files: map[string]string{}}
	if e := c.Validate(); e != nil {
		return result, e
	}
	if db == nil || authority == nil || client == nil || client.Timeout <= 0 || client.Timeout > 10*time.Second {
		return result, ErrUnavailable
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || transport.TLSClientConfig.InsecureSkipVerify || transport.TLSClientConfig.MinVersion < 0x0303 {
		return result, ErrInvalid
	}
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	client = &copyClient
	if e := c.bindSource(); e != nil {
		return result, e
	}
	if e := os.Mkdir(c.OutputDir, 0700); e != nil {
		return result, e
	}
	rawPath := filepath.Join(c.OutputDir, "raw.jsonl")
	file, e := os.OpenFile(rawPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return result, e
	}
	capture := &releaseCapture{file: file, max: c.MaxCaptureBytes}
	defer func() {
		result.FinishedAt = time.Now().UTC()
		if err != nil {
			result.Failures = append(result.Failures, err.Error())
		}
		if e := file.Sync(); err == nil && e != nil {
			err = e
		}
		if e := file.Close(); err == nil && e != nil {
			err = e
		}
		if digest, e := releaseFileDigest(rawPath); e == nil {
			result.Files[rawPath] = digest
		} else if err == nil {
			err = e
		}
		result.Qualified = false
		if err != nil {
			result.LanePassed = false
		}
		raw, e := json.MarshalIndent(result, "", "  ")
		if e == nil {
			e = os.WriteFile(filepath.Join(c.OutputDir, "result.json"), append(raw, '\n'), 0600)
		}
		if err == nil {
			err = e
		}
	}()
	result.Files[c.TriggerFile] = c.Binding.DatasetDigest
	result.Files[c.TopologyFile] = c.Binding.TopologyDigest
	result.Files[c.InterfaceFile] = c.Binding.InterfaceDigest
	if err = releaseJourney(ctx, c, client, capture, &result); err != nil {
		return result, err
	}
	cohort, e := releasePrepareCohort(ctx, db, c)
	if e != nil {
		return result, e
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, e := cohort.ExecContext(cleanup, `DROP TABLE IF EXISTS release_notification_cohort`)
		if err == nil {
			err = e
		}
		if e = cohort.Close(); err == nil {
			err = e
		}
	}()
	if err = releaseBurst(ctx, c, TriggerConsumer{Store: Store{DB: db}, Authority: authority}, capture, &result); err != nil {
		return result, err
	}
	if err = c.bindSource(); err != nil {
		return result, err
	}
	drainStart := time.Now()
	drainDeadline := drainStart.Add(10 * time.Minute)
	for {
		sample, e := releaseQueue(ctx, cohort)
		if e != nil {
			return result, e
		}
		if err = capture.append(sample); err != nil {
			return result, err
		}
		if sample.Missing == 0 && sample.Duplicates == 0 && sample.Pending == 0 {
			result.Created = sample.Inbox
			break
		}
		if !time.Now().Before(drainDeadline) {
			return result, fmt.Errorf("queue did not reconcile and drain within ten minutes")
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		}
	}
	result.DrainDurationNS = time.Since(drainStart).Nanoseconds()
	if err = releaseDevices(ctx, db, c, result.StartedAt, capture, &result); err != nil {
		return result, err
	}
	digest, e := releaseFileDigest(c.DeviceReceiptFile)
	if e != nil {
		return result, e
	}
	result.Files[c.DeviceReceiptFile] = digest
	if err = releaseGlobals(c, &result); err != nil {
		return result, err
	}
	result.LanePassed = true
	return result, nil
}
