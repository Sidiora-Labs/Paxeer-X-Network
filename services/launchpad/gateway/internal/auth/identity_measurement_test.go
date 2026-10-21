package auth

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"github.com/Sidiora-Technologies/KindleLaunch/shared/delivery/natsdriver"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func measurementFixtureConfig(t *testing.T) (*identityHTTPFixture, IdentityMeasurementConfig) {
	t.Helper()
	if os.Getenv("L33_POSTGRES_DSN") == "" {
		t.Fatal("identity measurement requires explicitly admitted L33_POSTGRES_DSN")
	}
	f := ownerHTTPFixture(t)
	var database, schema string
	if err := f.pool.QueryRow(f.ctx, `SELECT current_database(),current_schema()`).Scan(&database, &schema); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	_, sourceSHA, err := identityMeasurementSources(root)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := os.ReadFile(filepath.Join(root, "launchpad/spec/production-system/integration/contracts/v2/catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	topology, err := json.Marshal(struct {
		Endpoint, Database, Schema string
		MaxConns                   int32
	}{f.server.URL, database, schema, f.pool.Config().MaxConns})
	if err != nil {
		t.Fatal(err)
	}
	client := f.server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return f, IdentityMeasurementConfig{Binding: IdentityMeasurementBinding{CandidateSHA256: sourceSHA, InterfaceSHA256: measurementHash(contract), TopologySHA256: measurementHash(topology), Region: "loopback", RunID: uuid.NewString()}, Clients: []IdentityMeasurementClient{{BaseURL: f.server.URL, Origin: "https://kindle.test", Client: client}}, Phases: []IdentityMeasurementPhase{{Name: "harness", SignInRefreshRPS: 2, DynamicBudgetRPS: 4, WriteBudgetRPS: 4, Duration: time.Second}}, Workers: 2, QueueCapacity: 4, MaxAcknowledgements: 8, RefreshesPerLogin: 1, SourceRoot: root, RequestTimeout: time.Second, EvidenceDirectory: t.TempDir(), Pool: f.pool, Schema: schema, Runtime: f.s}
}
func TestIdentityMeasurementActualHTTPPostgresAndImmutableCapture(t *testing.T) {
	f, cfg := measurementFixtureConfig(t)
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	summary, err := RunIdentityMeasurement(ctx, cfg)
	if err == nil || summary.FinalAcceptance || summary.FinalSharedGate != "9.1" {
		t.Fatal("local harness claimed missing global acceptance")
	}
	if summary.Requests != 4 || summary.SuccessfulRequests != 4 || summary.SuccessfulSignInRefresh != 2 || summary.UnexpectedFailures != 0 || summary.GeneratorRejected != 0 || summary.Checkpoint == nil {
		t.Fatalf("real measured workload failed: %s", summary.String())
	}
	if !summary.Assertions["D11_029.2_latency"] || !summary.Assertions["source_unchanged"] || !summary.Assertions["acknowledged_rows_durable"] {
		t.Fatalf("owned harness assertions failed: %v", summary.Failures)
	}
	raw, err := os.ReadFile(filepath.Join(cfg.EvidenceDirectory, "requests.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if measurementHash(raw) != summary.RawSHA256 {
		t.Fatal("raw capture digest mismatch")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	count := 0
	for scanner.Scan() {
		var sample IdentityRequestMeasurement
		if json.Unmarshal(scanner.Bytes(), &sample) != nil || sample.Status != sample.ExpectedStatus || sample.CorrelationID != sample.RequestID || sample.LatencyNS <= 0 || sample.CorrectedLatencyNS < sample.LatencyNS || sample.CompletedAt.Before(sample.StartedAt) {
			t.Fatal("actual raw sample integrity failed")
		}
		count++
	}
	if scanner.Err() != nil || count != 4 {
		t.Fatal("raw request count changed")
	}
	after, err := CaptureIdentityDurability(ctx, cfg.Pool, cfg.Schema, cfg.Binding, summary.Checkpoint.Acknowledged)
	if err != nil {
		t.Fatal(err)
	}
	if err = CompareIdentityRecovery(*summary.Checkpoint, after); err != nil {
		t.Fatal("unchanged actual durable records mismatch", err)
	}
	if _, err = cfg.Pool.Exec(ctx, `UPDATE identity_sessions SET device_label='changed actual record' WHERE session_id=$1`, after.Acknowledged[0].SessionID); err != nil {
		t.Fatal(err)
	}
	changed, err := CaptureIdentityDurability(ctx, cfg.Pool, cfg.Schema, cfg.Binding, after.Acknowledged)
	if err != nil {
		t.Fatal(err)
	}
	if CompareIdentityRecovery(after, changed) == nil {
		t.Fatal("actual changed record passed restore comparison")
	}
	if _, err = RunIdentityMeasurement(ctx, cfg); err == nil {
		t.Fatal("immutable evidence directory overwritten")
	}
	preserved, err := os.ReadFile(filepath.Join(cfg.EvidenceDirectory, "requests.jsonl"))
	if err != nil || measurementHash(preserved) != summary.RawSHA256 {
		t.Fatal("second invocation corrupted immutable raw evidence")
	}
	if summary.CPUPerEOAProcessNS == nil || summary.SignatureWallMeanNS == nil || summary.PoolAcquireMeanNS == nil {
		t.Fatal("real process/signature/pool counters unavailable")
	}
}
func TestIdentityMeasurementMissingAuthorityAndBounds(t *testing.T) {
	_, cfg := measurementFixtureConfig(t)
	cases := []func(*IdentityMeasurementConfig){
		func(c *IdentityMeasurementConfig) { c.Binding.TopologySHA256 = "" },
		func(c *IdentityMeasurementConfig) { c.Pool = nil },
		func(c *IdentityMeasurementConfig) { c.Clients = nil },
		func(c *IdentityMeasurementConfig) { c.Workers = 4097 },
		func(c *IdentityMeasurementConfig) { c.MaxAcknowledgements = 0 },
		func(c *IdentityMeasurementConfig) {
			c.Clients = append([]IdentityMeasurementClient(nil), c.Clients...)
			c.Clients[0].Origin = "*"
		},
		func(c *IdentityMeasurementConfig) {
			c.Phases = append([]IdentityMeasurementPhase(nil), c.Phases...)
			c.Phases[0].WriteBudgetRPS = 1
		},
	}
	for _, mutate := range cases {
		candidate := cfg
		mutate(&candidate)
		if validateIdentityMeasurement(candidate) == nil {
			t.Fatal("missing authority or impossible bound accepted")
		}
	}
}
func TestIdentityMeasurementHistogramPreservesStrictLatencyBounds(t *testing.T) {
	var histogram identityLatencyHistogram
	for index := 0; index < 95; index++ {
		histogram.add(int64(150 * time.Millisecond))
	}
	for index := 0; index < 4; index++ {
		histogram.add(int64(400 * time.Millisecond))
	}
	histogram.add(int64(401 * time.Millisecond))
	if histogram.percentile(95) != int64(150*time.Millisecond) || histogram.percentile(99) != int64(400*time.Millisecond) {
		t.Fatal("nearest-rank latency bounds changed")
	}
	var outside identityLatencyHistogram
	outside.add(int64(150*time.Millisecond) + 1)
	if outside.percentile(95) <= int64(150*time.Millisecond) {
		t.Fatal("histogram rounded down a failing bound")
	}
	outside.add(int64(7 * time.Second))
	if outside.percentile(99) != int64(7*time.Second) {
		t.Fatal("overflow latency omitted")
	}
}
func TestIdentityMeasurementEvidenceRejectsAbsentRecovery(t *testing.T) {
	if WriteIdentityRecoveryEvidence(filepath.Join(t.TempDir(), "recovery.json"), IdentityDurabilityCheckpoint{}, IdentityDurabilityCheckpoint{}, "", "", time.Now(), time.Now()) == nil {
		t.Fatal("missing real recovery evidence accepted")
	}
	if _, err := CaptureIdentityDurability(context.Background(), nil, "users", IdentityMeasurementBinding{}, nil); err == nil {
		t.Fatal("missing real PostgreSQL accepted")
	}
	var s *Identity
	if _, err := s.CaptureRuntimeMeasurement(); err == nil {
		t.Fatal("missing runtime emitted metrics")
	}
}
func TestIdentityMeasurementRawProjectionContainsNoReusableSecrets(t *testing.T) {
	f, cfg := measurementFixtureConfig(t)
	credentials, _ := ownerCredentials(t, f)
	sample := IdentityRequestMeasurement{RequestID: uuid.NewString(), Operation: "login", RequestSHA256: measurementHash([]byte(credentials.RefreshSecret)), ResponseSHA256: measurementHash([]byte(credentials.AccessToken))}
	raw, err := json.Marshal(sample)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{f.wallet, credentials.AccessToken, credentials.RefreshSecret, credentials.CSRFSecret} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("measurement projection leaks identity secrets")
		}
	}
	path := filepath.Join(cfg.EvidenceDirectory, "safe.json")
	if err = writeIdentityImmutableJSON(path, sample); err != nil {
		t.Fatal(err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatal("private evidence permissions changed")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	actual, err := io.ReadAll(file)
	if err != nil || !strings.Contains(string(actual), sample.RequestSHA256) {
		t.Fatal("actual immutable evidence missing digest")
	}
}

func TestIdentityMeasurementAuthoritativeSocketsAndHTTPRevocation(t *testing.T) {
	f, cfg := measurementFixtureConfig(t)
	first, p1 := ownerCredentials(t, f)
	second, p2 := ownerCredentials(t, f)
	ctx, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://kindle.test" }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := f.s.ConsumeTicket(r.Context(), r.URL.Query().Get("ticket"), r.Header.Get("Origin"), "/measurement/private")
		if err != nil {
			f.s.writeIdentityError(w, err)
			return
		}
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		f.s.EnforceWebSocket(ctx, p, connection)
	}))
	t.Cleanup(server.Close)
	probes := []IdentityRevocationProbe{}
	for index, p := range []Principal{p1, p2} {
		ticket, err := f.s.IssueTicket(ctx, p, "https://kindle.test", "/measurement/private")
		if err != nil {
			t.Fatal(err)
		}
		connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/measurement/private?ticket="+ticket, http.Header{"Origin": []string{"https://kindle.test"}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connection.Close() })
		token := first.AccessToken
		if index == 1 {
			token = second.AccessToken
		}
		probes = append(probes, IdentityRevocationProbe{Client: cfg.Clients[0], AccessToken: token, Socket: connection})
	}
	measurement, err := f.s.MeasureRevocationPropagation(ctx, probes[0], probes, cfg.Binding)
	if err != nil || !measurement.Passed || len(measurement.Observations) != 2 || measurement.P99UpperBoundNS > int64(5*time.Second) {
		t.Fatalf("real revocation measurement failed: %+v %v", measurement, err)
	}
	for _, observation := range measurement.Observations {
		if observation.HTTPDeniedAt.IsZero() || observation.SocketClosedAt.IsZero() || observation.Failure != "" {
			t.Fatal("missing authoritative revocation observation")
		}
	}
}
func TestIdentityMeasurementReplayRejectsMissingBrokerAndBaseline(t *testing.T) {
	f, cfg := measurementFixtureConfig(t)
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	if _, err := f.s.MeasureIdentityReplay(ctx, nil, natsdriver.ConsumerConfig{}, "identity_measurement", "g1", 1, cfg.Binding); err == nil {
		t.Fatal("missing real broker produced replay evidence")
	}
	if _, err := f.s.identityReplayRecord(ctx, uuid.NewString()); err == nil {
		t.Fatal("missing committed projection produced replay evidence")
	}
}

func TestIdentityMeasurementPreferenceRequiresActualServicesAndTwoWallets(t *testing.T) {
	f, cfg := measurementFixtureConfig(t)
	credentials, _ := ownerCredentials(t, f)
	if _, err := f.s.MeasurePreferenceContinuity(f.ctx, IdentityMeasurementClient{}, credentials.AccessToken, credentials.AccessToken, cfg.Binding); err == nil {
		t.Fatal("missing actual preference service accepted")
	}
	if _, err := f.s.MeasurePreferenceContinuity(f.ctx, cfg.Clients[0], credentials.AccessToken, credentials.AccessToken, cfg.Binding); err == nil {
		t.Fatal("one wallet represented as distinct proof history")
	}
}

func TestSerialIdentityPhase4ConfiguredWorkload(t *testing.T) {
	path := os.Getenv("L33_PHASE4_MEASUREMENT_INPUT")
	if path == "" {
		t.Fatal("explicit serial9.1 identity workload input required")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("serial identity input unavailable")
	}
	defer file.Close()
	var input struct {
		Binding IdentityMeasurementBinding `json:"binding"`
		Clients []struct {
			BaseURL  string `json:"baseUrl"`
			Origin   string `json:"origin"`
			SourceIP string `json:"sourceIp"`
		} `json:"clients"`
		Phases                  []IdentityMeasurementPhase `json:"phases"`
		Workers                 int                        `json:"workers"`
		QueueCapacity           int                        `json:"queueCapacity"`
		MaxAcknowledgements     int                        `json:"maxAcknowledgements"`
		RefreshesPerLogin       int                        `json:"refreshesPerLogin"`
		RequestTimeoutNS        int64                      `json:"requestTimeoutNs"`
		SourceRoot              string                     `json:"sourceRoot"`
		EvidenceDirectory       string                     `json:"evidenceDirectory"`
		DatabaseSecretReference string                     `json:"databaseSecretReference"`
		Schema                  string                     `json:"schema"`
	}
	decoder := json.NewDecoder(io.LimitReader(file, 1024*1024))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil {
		t.Fatal("serial identity input invalid")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		t.Fatal("serial identity input trailing data")
	}
	if input.DatabaseSecretReference == "" {
		t.Fatal("explicit read-role database secret reference required")
	}
	dsn, ok := os.LookupEnv(input.DatabaseSecretReference)
	if !ok || dsn == "" {
		t.Fatal("actual database credential unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pgConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("database configuration invalid")
	}
	pgConfig.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, pgConfig)
	if err != nil {
		t.Fatal("actual PostgreSQL provider unavailable")
	}
	defer pool.Close()
	if pool.Ping(ctx) != nil {
		t.Fatal("actual PostgreSQL provider unreachable")
	}
	clients := []IdentityMeasurementClient{}
	for _, client := range input.Clients {
		ip := net.ParseIP(client.SourceIP)
		if ip == nil {
			t.Fatal("actual admitted client source IP required")
		}
		dialer := &net.Dialer{Timeout: 2 * time.Second, LocalAddr: &net.TCPAddr{IP: ip}}
		transport := &http.Transport{DialContext: dialer.DialContext, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13}, MaxIdleConns: input.Workers, MaxIdleConnsPerHost: input.Workers, MaxConnsPerHost: input.Workers}
		t.Cleanup(transport.CloseIdleConnections)
		clients = append(clients, IdentityMeasurementClient{BaseURL: client.BaseURL, Origin: client.Origin, Client: &http.Client{Transport: transport, Timeout: time.Duration(input.RequestTimeoutNS), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}})
	}
	cfg := IdentityMeasurementConfig{Binding: input.Binding, Clients: clients, Phases: input.Phases, Workers: input.Workers, QueueCapacity: input.QueueCapacity, MaxAcknowledgements: input.MaxAcknowledgements, RefreshesPerLogin: input.RefreshesPerLogin, SourceRoot: input.SourceRoot, RequestTimeout: time.Duration(input.RequestTimeoutNS), EvidenceDirectory: input.EvidenceDirectory, Pool: pool, Schema: input.Schema}
	summary, err := RunIdentityMeasurement(ctx, cfg)
	if err != nil {
		t.Fatalf("serial identity measurement failed; captured assertions remain failures: %s", summary.String())
	}
	if !summary.FinalAcceptance {
		t.Fatal("global load, runtime telemetry, AZ-loss, replay and revocation evidence remain pending serial9.1 acceptance")
	}
}
