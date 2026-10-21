package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/config"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// developmentApp boots the real service wiring (goose migrations, pgxpool,
// Redis, router, rate limiter, readiness) in development mode against the
// dedicated fixture database. No Livepeer key or webhook secret is configured.
func developmentApp(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	dsn, redisURL := os.Getenv("L40_TEST_APP_POSTGRES_DSN"), os.Getenv("L40_TEST_REDIS_URL")
	if dsn == "" || redisURL == "" {
		t.Fatal("L40_TEST_APP_POSTGRES_DSN (dedicated database) and L40_TEST_REDIS_URL are required; no silent skip")
	}
	cfg := config.Config{NodeEnv: "development", DatabaseURL: dsn, RedisURL: redisURL, LivepeerAPIURL: "https://livepeer.studio/api", ProviderMode: "unavailable", LogLevel: "info", Port: 3000, MaxStreamsPerWallet: 3, TrustedProxyCIDRs: "127.0.0.0/8,::1/128"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	a, err := New(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	server := httptest.NewServer(a.Router)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		server.Close()
		pool.Close()
		a.Close()
		cancel()
	})
	// chat.safety_bans and its versioned view chat.safety_bans_v1 belong to
	// media/social (goose 00003/00004); the shared development database
	// carries them, the dedicated fixture database needs them created.
	if _, err = pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS chat; CREATE TABLE IF NOT EXISTS chat.safety_bans(wallet text NOT NULL,revoked_at timestamptz,expires_at timestamptz,scope text NOT NULL,resource text);
CREATE OR REPLACE VIEW chat.safety_bans_v1 AS SELECT wallet,scope,resource,expires_at,revoked_at FROM chat.safety_bans`); err != nil {
		t.Fatal(err)
	}
	return server, pool
}

func uuid4(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func address(t *testing.T) string {
	t.Helper()
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "0x" + hex.EncodeToString(b[:])
}

// testClients maps a test server URL to the client that reaches it (the
// platform runtime's private listener needs the gateway's mTLS client).
var testClients sync.Map

func clientFor(url string) *http.Client {
	client := http.DefaultClient
	testClients.Range(func(key, value any) bool {
		if strings.HasPrefix(url, key.(string)+"/") {
			client = value.(*http.Client)
			return false
		}
		return true
	})
	return client
}

func do(t *testing.T, method, url, body string, headers map[string]string) (int, []byte, http.Header) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if _, ok := headers["X-Real-IP"]; !ok {
		// Each call gets its own client identity so the 60/min limiter never
		// couples unrelated assertions.
		var b [2]byte
		if _, err = rand.Read(b[:]); err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Real-IP", fmt.Sprintf("198.18.%d.%d", b[0], b[1]))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := clientFor(url).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, raw, res.Header
}

type directory struct {
	Streams     []json.RawMessage `json:"streams"`
	NextCursor  *string           `json:"nextCursor"`
	GeneratedAt string            `json:"generatedAt"`
}

func decodeDirectory(t *testing.T, raw []byte) directory {
	t.Helper()
	var d directory
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&d); err != nil || d.Streams == nil {
		t.Fatalf("directory shape %s: %v", raw, err)
	}
	if _, err := time.Parse(time.RFC3339Nano, d.GeneratedAt); err != nil {
		t.Fatal(err)
	}
	for _, item := range d.Streams {
		if err := v2.ValidateJSON("Stream", item); err != nil {
			t.Fatalf("stream %s: %v", item, err)
		}
	}
	return d
}

func requireError(t *testing.T, status int, raw []byte, wantStatus int, wantCode string) v2.Error {
	t.Helper()
	if status != wantStatus {
		t.Fatalf("status %d want %d: %s", status, wantStatus, raw)
	}
	if err := v2.ValidateJSON("Error", raw); err != nil {
		t.Fatalf("error envelope %s: %v", raw, err)
	}
	var e v2.Error
	if err := json.Unmarshal(raw, &e); err != nil || e.Code != wantCode {
		t.Fatalf("code %q want %q: %s", e.Code, wantCode, raw)
	}
	return e
}

func insertOperation(t *testing.T, pool *pgxpool.Pool, id, wallet, poolAddress, state string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `INSERT INTO livestream.stream_operations(id,wallet,pool,title,idem_key,request_hash,state,provider_id,provider_time) VALUES($1,$2,$3,'Development read fixture',$4,'fixture',$5,$6,CASE WHEN $5='LIVE' THEN clock_timestamp() END)`, id, wallet, poolAddress, "idem_"+id, state, "prov_"+strings.ReplaceAll(id, "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM livestream.stream_operations WHERE id=$1`, id)
	})
}

func TestDevelopmentDirectoryReadsServeRealEmptyStore(t *testing.T) {
	server, _ := developmentApp(t)
	for _, path := range []string{"/v2/streams/live", "/v2/streams/live?sort=viewers", "/v2/streams/live?sort=mcap&limit=10", "/v2/streams/pool/" + address(t), "/v2/streams/pool/" + address(t) + "?live=true"} {
		status, raw, header := do(t, http.MethodGet, server.URL+path, "", nil)
		if status != 200 || !strings.HasPrefix(header.Get("Content-Type"), "application/json") {
			t.Fatalf("%s: %d %s", path, status, raw)
		}
		d := decodeDirectory(t, raw)
		if path == "/v2/streams/live" && (d.NextCursor != nil) {
			t.Fatalf("%s: unexpected cursor", path)
		}
	}
	status, raw, _ := do(t, http.MethodGet, server.URL+"/v2/streams/pool/not-an-address", "", nil)
	requireError(t, status, raw, 422, "FIELD_INVALID")
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/live?sort=volume", "", nil)
	requireError(t, status, raw, 422, "FIELD_INVALID")
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/"+uuid4(t), "", nil)
	requireError(t, status, raw, 404, "NOT_FOUND")
}

func TestDevelopmentStreamDocumentsComeFromLifecycleStore(t *testing.T) {
	server, pool := developmentApp(t)
	wallet, poolAddress := address(t), address(t)
	ended, suspendedByBan := uuid4(t), uuid4(t)
	insertOperation(t, pool, ended, wallet, poolAddress, "ENDED")
	insertOperation(t, pool, suspendedByBan, wallet, poolAddress, "READY")
	status, raw, _ := do(t, http.MethodGet, server.URL+"/v2/streams/"+ended, "", nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	if err := v2.ValidateJSON("Stream", raw); err != nil {
		t.Fatal(err)
	}
	var doc v2.Stream
	if err := json.Unmarshal(raw, &doc); err != nil || doc.StreamId != ended || doc.Status != "ended" || doc.PoolAddress != poolAddress {
		t.Fatalf("%s %v", raw, err)
	}
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/pool/"+poolAddress, "", nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	if d := decodeDirectory(t, raw); len(d.Streams) != 2 {
		t.Fatalf("want 2 pool streams: %s", raw)
	}
	// A pool-scoped ban from media/social suspends the public projection.
	if _, err := pool.Exec(context.Background(), `INSERT INTO chat.safety_bans(wallet,scope,resource) VALUES($1,'pool',$2)`, wallet, poolAddress); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM chat.safety_bans WHERE wallet=$1`, wallet) })
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/"+suspendedByBan, "", nil)
	if status != 200 || v2.ValidateJSON("Stream", raw) != nil || !strings.Contains(string(raw), `"status":"suspended"`) {
		t.Fatalf("%d %s", status, raw)
	}
	// The live directory carries only live rows.
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/live", "", nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	for _, item := range decodeDirectory(t, raw).Streams {
		if strings.Contains(string(item), ended) {
			t.Fatal("ended stream in live directory")
		}
	}
}

func TestDevelopmentBroadcastRoutesAnswerNotProvisioned(t *testing.T) {
	server, pool := developmentApp(t)
	id := uuid4(t)
	insertOperation(t, pool, id, address(t), address(t), "READY")
	routes := []struct{ method, path, body string }{
		{http.MethodPost, "/v2/streams", `{"poolAddress":"` + address(t) + `","title":"x"}`},
		{http.MethodPatch, "/v2/streams/" + id, `{"title":"x","revision":"1"}`},
		{http.MethodPost, "/v2/streams/" + id + "/go-live", `{"revision":"1"}`},
		{http.MethodPost, "/v2/streams/" + id + "/end", `{"revision":"1"}`},
		{http.MethodPost, "/v2/streams/" + id + "/rotate-key", `{"revision":"1"}`},
		{http.MethodPost, "/v2/streams/" + id + "/restore", `{"revision":"1"}`},
		{http.MethodPost, "/v2/streams/" + id + "/ingest-credential", `{}`},
		{http.MethodPost, "/v2/streams/" + id + "/ingest-key", `{}`},
		{http.MethodGet, "/v2/streams/" + id + "/key-rotation", ""},
		{http.MethodGet, "/v2/streams/" + id + "/private-playback", ""},
		{http.MethodGet, "/v2/streams/operations/" + id, ""},
		{http.MethodPost, "/webhooks/livepeer", `{}`},
		{http.MethodPost, "/webhooks/livepeer/playback-access", `{}`},
	}
	for _, route := range routes {
		status, raw, header := do(t, route.method, server.URL+route.path, route.body, nil)
		e := requireError(t, status, raw, 409, "BROADCAST_NOT_PROVISIONED")
		if e.Retryable || e.Details == nil || e.Details.Capability.Value != "livestream.broadcast" || header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: %s", route.method, route.path, raw)
		}
	}
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM livestream.stream_operations WHERE id<>$1 AND title='x'`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("unprovisioned create persisted state", count, err)
	}
}

func TestDevelopmentViewerSessionAndHeartbeatUseRealStores(t *testing.T) {
	server, pool := developmentApp(t)
	ready, unknown := uuid4(t), uuid4(t)
	insertOperation(t, pool, ready, address(t), address(t), "READY")
	status, raw, _ := do(t, http.MethodPost, server.URL+"/v2/streams/"+unknown+"/viewer-session", `{}`, nil)
	requireError(t, status, raw, 404, "NOT_FOUND")
	// A stream that is not live is a state condition, a forged viewer token a
	// credential the viewer replaces: both 4xx, never a 5xx.
	status, raw, _ = do(t, http.MethodPost, server.URL+"/v2/streams/"+ready+"/viewer-session", `{}`, nil)
	requireError(t, status, raw, 409, "STATE_CONFLICT")
	status, raw, _ = do(t, http.MethodPost, server.URL+"/v2/streams/"+ready+"/heartbeat", `{"sessionId":"`+uuid4(t)+`","token":"forged.token"}`, nil)
	requireError(t, status, raw, 401, "AUTH_EXPIRED")
	status, raw, _ = do(t, http.MethodPost, server.URL+"/v2/streams/not-a-stream/heartbeat", `{"sessionId":"`+uuid4(t)+`","token":"forged.token"}`, nil)
	requireError(t, status, raw, 422, "FIELD_INVALID")

	// A LIVE row cannot arise without a provider in development, but when one
	// exists the session/heartbeat path is fully served by Postgres + Redis.
	live := uuid4(t)
	insertOperation(t, pool, live, address(t), address(t), "LIVE")
	status, raw, _ = do(t, http.MethodPost, server.URL+"/v2/streams/"+live+"/viewer-session", `{}`, nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	var session struct {
		StreamID  string `json:"streamId"`
		SessionID string `json:"sessionId"`
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(raw, &session); err != nil || session.StreamID != live || session.Token == "" {
		t.Fatalf("%s %v", raw, err)
	}
	body, _ := json.Marshal(map[string]string{"sessionId": session.SessionID, "token": session.Token})
	status, raw, _ = do(t, http.MethodPost, server.URL+"/v2/streams/"+live+"/heartbeat", string(body), nil)
	if status != 200 || !strings.Contains(string(raw), `"viewerCount":1`) {
		t.Fatalf("%d %s", status, raw)
	}
	requireRequestErrors(t, server.URL, live, string(body))
	// Without provider playback evidence a LIVE row is never published: this
	// deployment has no broadcast provider, a deployment state (409), not a
	// store outage.
	status, raw, _ = do(t, http.MethodGet, server.URL+"/v2/streams/"+live, "", nil)
	if e := requireError(t, status, raw, 409, "BROADCAST_NOT_PROVISIONED"); e.Retryable || e.Details == nil || e.Details.Dependency.Value != "livepeer" {
		t.Fatalf("%s", raw)
	}
	// Once the stream ends, the session's heartbeat is a state conflict.
	if _, err := pool.Exec(context.Background(), `UPDATE livestream.stream_operations SET state='ENDED' WHERE id=$1`, live); err != nil {
		t.Fatal(err)
	}
	status, raw, _ = do(t, http.MethodPost, server.URL+"/v2/streams/"+live+"/heartbeat", string(body), nil)
	requireError(t, status, raw, 409, "STATE_CONFLICT")
}

func TestDevelopmentReadinessReflectsDatabaseRedisAndReadSchema(t *testing.T) {
	server, _ := developmentApp(t)
	status, raw, _ := do(t, http.MethodGet, server.URL+"/health/ready", "", nil)
	if status != 200 {
		t.Fatalf("%d %s", status, raw)
	}
	for _, check := range []string{"database", "redis", "stream_read_schema", "lifecycle"} {
		if !strings.Contains(string(raw), `"`+check+`"`) {
			t.Fatalf("missing %s: %s", check, raw)
		}
	}
	if strings.Contains(string(raw), "stream_provider_authority") {
		t.Fatal("development readiness depends on the absent provider authority")
	}
}

func TestDevelopmentRateLimitKeysOnTrustedForwardedClient(t *testing.T) {
	server, _ := developmentApp(t)
	first := fmt.Sprintf("203.0.113.%d", time.Now().UnixNano()%200+1)
	second := fmt.Sprintf("198.51.100.%d", time.Now().UnixNano()%200+1)
	limited := false
	for i := 0; i < rateLimitMax+1; i++ {
		status, raw, header := do(t, http.MethodGet, server.URL+"/v2/streams/live", "", map[string]string{"X-Real-IP": first, "X-Forwarded-For": "10.9.9.9, " + first})
		if status == 429 {
			// The shared limiter answers the v2 Error envelope: RATE_LIMITED,
			// retryable, retryAfterMs clamped to the schema's 60000 while the
			// Retry-After header keeps window+1 seconds.
			e := requireError(t, status, raw, 429, "RATE_LIMITED")
			if !e.Retryable || e.RetryAfterMs == nil || *e.RetryAfterMs != 60000 || header.Get("Retry-After") != "61" || len(e.FieldErrors) != 0 {
				t.Fatalf("shared limiter envelope %+v Retry-After %q: %s", e, header.Get("Retry-After"), raw)
			}
			limited = true
			break
		}
		if status != 200 {
			t.Fatalf("%d %s", status, raw)
		}
	}
	if !limited {
		t.Fatal("forwarded client was never limited")
	}
	status, raw, _ := do(t, http.MethodGet, server.URL+"/v2/streams/live", "", map[string]string{"X-Real-IP": second})
	if status != 200 {
		t.Fatalf("distinct forwarded client shared the limit: %d %s", status, raw)
	}
}

// requireRequestErrors asserts that the heartbeat route answers the v2
// envelope for every body it cannot admit and for a throttled repeat.
func requireRequestErrors(t *testing.T, base, stream, body string) {
	t.Helper()
	for _, c := range []struct {
		name, body, contentType string
		status                  int
		rule                    string
	}{
		{"text/plain", body, "text/plain", 415, "media_type"},
		{"oversize", `{"sessionId":"` + strings.Repeat("a", 1024) + `"}`, "", 413, "max_bytes"},
		{"syntax", `{"sessionId":`, "", 400, "json"},
		{"unknown field", `{"other":1}`, "", 400, "json"},
	} {
		headers := map[string]string{}
		if c.contentType != "" {
			headers["Content-Type"] = c.contentType
		}
		status, raw, _ := do(t, http.MethodPost, base+"/v2/streams/"+stream+"/heartbeat", c.body, headers)
		if e := requireError(t, status, raw, c.status, "FIELD_INVALID"); e.Retryable || len(e.FieldErrors) != 1 || e.FieldErrors[0].Rule != c.rule {
			t.Fatalf("%s: %s", c.name, raw)
		}
	}
	// The session was accepted moments ago: a repeat inside the 15 s window
	// is throttled with Retry-After and the matching retryAfterMs.
	status, raw, header := do(t, http.MethodPost, base+"/v2/streams/"+stream+"/heartbeat", body, nil)
	if e := requireError(t, status, raw, 429, "RATE_LIMITED"); !e.Retryable || e.RetryAfterMs == nil || *e.RetryAfterMs != 15000 || header.Get("Retry-After") != "15" {
		t.Fatalf("throttled heartbeat: %s %q", raw, header.Get("Retry-After"))
	}
}
