package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/ranker"
	"github.com/Sidiora-Technologies/KindleLaunch/core/ranking-algo/internal/store"
	v1 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v1"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"net/http/httptest"
	"testing"
)

func TestSnapshotQueryBoundsAndCanonicalCategories(t *testing.T) {
	for _, raw := range []string{"0", "-1", "101", "+1", " 1", "1garbage", "1e2"} {
		if _, err := snapshotLimit(raw); err == nil {
			t.Fatalf("accepted limit %q", raw)
		}
	}
	for raw, want := range map[string]int{"": 50, "1": 1, "100": 100} {
		n, err := snapshotLimit(raw)
		if err != nil || n != want {
			t.Fatalf("valid limit %q", raw)
		}
	}
	category, err := snapshotCategory("top_volume")
	if err != nil || category != "volume" {
		t.Fatal("volume alias")
	}
	for _, raw := range []string{"gainers", "losers", "arbitrary"} {
		if _, err = snapshotCategory(raw); err == nil {
			t.Fatalf("unsupported category %s", raw)
		}
	}
}
func TestSnapshotRoutesRequireActualDependencies(t *testing.T) {
	if _, err := NewSnapshots(nil, nil, nil, nil, 8); err == nil {
		t.Fatal("missing persistence/cursor/visibility accepted")
	}
}

func TestSnapshotErrorUsesFrozenEnvelope(t *testing.T) {
	w := httptest.NewRecorder()
	snapshotError(w, 410, "SNAPSHOT_EXPIRED", "Restart without a cursor.", 0, rankingGuidance{Restart: true})
	var body v1.Error
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 410 || body.Code != "SNAPSHOT_EXPIRED" || body.RequestId == "" || body.FieldErrors == nil || body.Retryable {
		t.Fatal("invalid frozen error envelope", err)
	}
	w = httptest.NewRecorder()
	unavailable(w)
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 503 || body.RetryAfterMs == nil || *body.RetryAfterMs != 10000 || !body.Retryable {
		t.Fatal("missing retry guidance", err)
	}
}

func TestPhase3ActualReadHandlersRedisLossAndExpiredCursor(t *testing.T) {
	dsn, schema := os.Getenv("L19_POSTGRES_DSN"), os.Getenv("L19_PHASE3_HTTP_SCHEMA")
	visibilityURL, expiredID := os.Getenv("L19_VISIBILITY_URL"), os.Getenv("L19_PHASE3_EXPIRED_RUN_ID")
	emptyCategory, staleCategory := os.Getenv("L19_PHASE3_EMPTY_CATEGORY"), os.Getenv("L19_PHASE3_STALE_CATEGORY")
	if dsn == "" || schema == "" || visibilityURL == "" || expiredID == "" || emptyCategory == "" || staleCategory == "" || os.Getenv("L19_REDIS_URL") == "" {
		t.Fatal("actual isolated PostgreSQL/Redis/moderation and retained expired run required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := store.NewSnapshotStore(pool, schema)
	if err != nil {
		t.Fatal(err)
	}
	visibility, err := store.NewHTTPRankingVisibility(visibilityURL, &http.Client{Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	header, err := repository.Current(ctx, "trending")
	if err != nil || header.Total < 2 {
		t.Fatal("owner-completed nonempty ranking fixture required", err)
	}
	if time.Now().Unix() >= header.CutoffTime+900 {
		t.Fatal("current actual fixture expired before journey")
	}
	codec, err := ranker.NewCursorCodec([]byte(strings.Repeat("phase3-cursor", 3)))
	if err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(os.Getenv("L19_REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	cache, err := store.NewSnapshotCache(client, "l19_http_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewSnapshots(repository, cache, codec, visibility, 1)
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	RegisterSnapshots(router, handler)
	server := httptest.NewServer(router)
	defer server.Close()
	fetch := func(path string, expected int) ([]byte, http.Header) {
		t.Helper()
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		if err != nil || response.StatusCode != expected {
			t.Fatalf("handler %s status=%d expected=%d body=%s err=%v", path, response.StatusCode, expected, raw, err)
		}
		return raw, response.Header
	}
	firstRaw, headers := fetch("/rankings/trending?limit=1", 200)
	var first v1.RankingsResponse
	if err = json.Unmarshal(firstRaw, &first); err != nil || first.NextCursor == nil || len(first.Data.Snapshot.Items) != 1 || headers.Get("ETag") == "" {
		t.Fatal("actual first-page fields missing", err)
	}
	if _, err = cache.Get(ctx, header, 0, 1); err != nil {
		t.Fatal("real Redis did not materialize immutable page", err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	fallbackRaw, fallbackHeaders := fetch("/rankings/trending?limit=1", 200)
	var fallback v1.RankingsResponse
	if err = json.Unmarshal(fallbackRaw, &fallback); err != nil || fallback.Data.Snapshot.SnapshotId != first.Data.Snapshot.SnapshotId || fallbackHeaders.Get("ETag") != headers.Get("ETag") || fallback.Data.Snapshot.Items[0] != first.Data.Snapshot.Items[0] {
		t.Fatal("Redis connection loss changed durable read", err)
	}
	secondRaw, _ := fetch("/rankings/trending?limit=1&cursor="+url.QueryEscape(*first.NextCursor), 200)
	var second v1.RankingsResponse
	if err = json.Unmarshal(secondRaw, &second); err != nil || second.Data.Snapshot.SnapshotId != first.Data.Snapshot.SnapshotId || len(second.Data.Snapshot.Items) != 1 || second.Data.Snapshot.Items[0].PoolAddress == first.Data.Snapshot.Items[0].PoolAddress || second.Data.Snapshot.Items[0].Rank != 2 {
		t.Fatal("cursor continuation mixed or repeated members", err)
	}
	emptyRaw, _ := fetch("/rankings/"+url.PathEscape(emptyCategory), 200)
	var empty v1.RankingsResponse
	if err = json.Unmarshal(emptyRaw, &empty); err != nil || empty.Data.Snapshot.Items == nil || len(empty.Data.Snapshot.Items) != 0 || empty.Data.Snapshot.TotalEligible != "0" {
		t.Fatal("completed empty category confused with unavailable market", err)
	}
	staleHeader, err := repository.Current(ctx, staleCategory)
	if err != nil || !staleHeader.Stale(time.Now()) {
		t.Fatal("actual failed-refresh stale category fixture required", err)
	}
	staleRaw, _ := fetch("/rankings/"+url.PathEscape(staleCategory), 200)
	var stale struct {
		v1.RankingsResponse
		Freshness struct {
			Stale  bool   `json:"stale"`
			Age    int64  `json:"ageSeconds"`
			Reason string `json:"staleReason"`
		} `json:"freshness"`
	}
	if err = json.Unmarshal(staleRaw, &stale); err != nil || stale.Quality != "stale" || !stale.Freshness.Stale || stale.Freshness.Age <= 0 || stale.Freshness.Reason != "refresh-delayed" {
		t.Fatal("retained stale ranking lacks age/retry behavior", err)
	}
	expiredHeader, err := repository.Header(ctx, expiredID, "trending")
	if err != nil || expiredHeader.Total < 2 || time.Now().Unix() < expiredHeader.CutoffTime+900 {
		t.Fatal("genuine retained expired ranking fixture required", err)
	}
	expiredPage, err := repository.Page(ctx, expiredHeader, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	expiredCursor, err := codec.EncodePage(expiredHeader, expiredPage.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	expiredRaw, _ := fetch("/rankings/trending?limit=1&cursor="+url.QueryEscape(expiredCursor), 410)
	var restart struct {
		Code    string `json:"code"`
		Restart bool   `json:"restart"`
	}
	if err = json.Unmarshal(expiredRaw, &restart); err != nil || restart.Code != "SNAPSHOT_EXPIRED" || !restart.Restart {
		t.Fatal("expired cursor lost restart guidance", err)
	}
	wrongBoundary := first.Data.Snapshot.Items[0]
	bad, err := codec.EncodePage(header, ranker.RankedItem{Address: wrongBoundary.PoolAddress, Score: "999", Rank: 1})
	if err != nil {
		t.Fatal(err)
	}
	fetch("/rankings/trending?limit=1&cursor="+url.QueryEscape(bad), 400)
	fetch("/rankings/unsupported", 400)
	for _, limit := range []string{"0", "-1", "101"} {
		fetch("/rankings/trending?limit="+limit, 400)
	}
	handler.admission <- struct{}{}
	fetch("/rankings/trending", 503)
	<-handler.admission
	t.Log("actual handler: Redis connection loss used bounded durable fallback; signed retained cursor continued; expired retained cursor returned410; altered boundary returned400; admission returned503")
}

func TestPhase3DuplicateModerationUsesActualOwnerReplay(t *testing.T) {
	endpoint, eventPath := os.Getenv("L19_MODERATION_REPLAY_URL"), os.Getenv("L19_MODERATION_EVENT_FILE")
	eventSHA, target := os.Getenv("L19_MODERATION_EVENT_SHA256"), os.Getenv("L19_MODERATION_TARGET_POOL")
	authorityPath := os.Getenv("L19_MODERATION_REPLAY_AUTH_FILE")
	if endpoint == "" || eventPath == "" || eventSHA == "" || target == "" || authorityPath == "" {
		t.Fatal("real owner-authorized moderation replay capability and immutable event required")
	}
	u, err := url.Parse(endpoint)
	if err != nil || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1") || u.User != nil {
		t.Fatal("moderation replay requires isolated loopback owner endpoint")
	}
	event, err := os.ReadFile(eventPath)
	if err != nil || len(event) == 0 || len(event) > 1<<20 {
		t.Fatal("bounded genuine moderation event required", err)
	}
	sum := sha256.Sum256(event)
	if hex.EncodeToString(sum[:]) != eventSHA {
		t.Fatal("owner moderation event differs")
	}
	authorization, err := os.ReadFile(authorityPath)
	if err != nil || len(authorization) == 0 || len(authorization) > 8192 {
		t.Fatal("owner replay authority required")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	visibility, err := store.NewHTTPRankingVisibility(os.Getenv("L19_VISIBILITY_URL"), client)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	before, err := visibility.ReadRankingVisibility(ctx)
	if err != nil || !before.Pools[target] {
		t.Fatal("actual visible target required before owner removal replay", err)
	}
	var committed store.RankingVisibility
	for attempt := 0; attempt < 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(event))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", strings.TrimSpace(string(authorization)))
		response, err := client.Do(request)
		if err != nil {
			t.Fatal("owner moderation replay unavailable")
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if response.StatusCode != 200 || readErr != nil {
			t.Fatalf("owner replay attempt%d status%d", attempt, response.StatusCode)
		}
		after, err := visibility.ReadRankingVisibility(ctx)
		if err != nil {
			t.Fatal("actual visibility read failed after replay")
		}
		visible, exists := after.Pools[target]
		if !exists || visible {
			t.Fatal("owner removal did not suppress canonical target")
		}
		if attempt == 0 {
			if after.Revision == before.Revision {
				t.Fatal("moderation effect did not publish a new revision")
			}
			committed = after
		} else if after.Revision != committed.Revision || !reflect.DeepEqual(after.Pools, committed.Pools) {
			t.Fatal("duplicate moderation replay changed visible revision")
		}
	}
	t.Log("genuine owner moderation removal replayed three times with one public visibility revision")
}
