package streams

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/media/livestream/internal/livepeer"
	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"
	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
)

type directoryFixture struct {
	store      *LifecycleStore
	handlers   *Handlers
	router     *chi.Mux
	playbacks  *atomic.Int32
	operations []Operation
	pools      []string
}

type directoryPage struct {
	Streams    []v2.Stream `json:"streams"`
	NextCursor *string     `json:"nextCursor"`
}

func randomAddress(t *testing.T) string {
	t.Helper()
	var b [20]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "0x" + hex.EncodeToString(b[:])
}

// directoryHarness drives three streams to LIVE against the real lifecycle
// store, a real Redis viewer counter and an httptest Livepeer Studio, then
// mounts the public routes. Market data lives in the shared indexer and stats
// fixtures the production readers query by name.
func directoryHarness(t *testing.T) *directoryFixture {
	t.Helper()
	redisURL := os.Getenv("L40_TEST_REDIS_URL")
	if redisURL == "" {
		t.Fatal("L40_TEST_REDIS_URL is required; no silent fixture skip")
	}
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, s.PlaybackPrivacySchemaSQL()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS chat; CREATE TABLE IF NOT EXISTS chat.safety_bans(wallet text NOT NULL,revoked_at timestamptz,expires_at timestamptz,scope text NOT NULL,resource text);
CREATE OR REPLACE VIEW chat.safety_bans_v1 AS SELECT wallet,scope,resource,expires_at,revoked_at FROM chat.safety_bans;
CREATE SCHEMA IF NOT EXISTS indexer; CREATE TABLE IF NOT EXISTS indexer.indexer_canonical_creations_v1(token text PRIMARY KEY,pool text NOT NULL UNIQUE,creator text NOT NULL);
CREATE SCHEMA IF NOT EXISTS stats; CREATE TABLE IF NOT EXISTS stats.pool_stats(pool_address varchar(42) PRIMARY KEY,market_cap text NOT NULL DEFAULT '0');
CREATE OR REPLACE VIEW stats.pool_market_caps_v1 AS SELECT pool_address,market_cap FROM stats.pool_stats`); err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	namespace := fmt.Sprintf("kd-29e4009f8b95235d0159d804-dir-%d", time.Now().UnixNano())
	counter, err := NewViewerCounter(client, namespace, 100)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	operationByProvider := map[string]string{}
	thumbnails := map[string]string{}
	var playbacks atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Error("missing provider authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/stream/"):
			id := strings.TrimPrefix(r.URL.Path, "/stream/")
			operation, ok := operationByProvider[id]
			if !ok {
				w.WriteHeader(404)
				_, _ = w.Write([]byte(`{}`))
				return
			}
			_, _ = fmt.Fprintf(w, `{"id":%q,"playbackId":%q,"name":"KindleLaunch:%s","isActive":true,"playbackPolicy":{"type":"webhook","webhookId":"hook_1","webhookContext":{"operationId":%q},"refreshInterval":1}}`, id, "pb_"+id, operation, operation)
		case strings.HasPrefix(r.URL.Path, "/playback/"):
			playbacks.Add(1)
			playback := strings.TrimPrefix(r.URL.Path, "/playback/")
			sources := `{"hrn":"HLS (TS)","type":"html5/application/vnd.apple.mpegurl","url":"https://livepeercdn.studio/hls/` + playback + `/index.m3u8"}`
			if thumbnail := thumbnails[playback]; thumbnail != "" {
				sources += `,{"hrn":"Thumbnail (JPEG)","type":"image/jpeg","url":"` + thumbnail + `"}`
			}
			_, _ = w.Write([]byte(`{"type":"live","meta":{"live":1,"source":[` + sources + `]}}`))
		default:
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(provider.Close)
	livepeerClient := livepeer.New("fixture-key", provider.URL, nil, livepeer.WithWebhookPlayback("hook_1"))
	cache, err := livepeer.NewThumbnailCache(livepeerClient, 15*time.Second, 100)
	if err != nil {
		t.Fatal(err)
	}

	f := &directoryFixture{store: s, playbacks: &playbacks}
	marketCaps := []string{"1000", "5000000", "250"}
	heartbeats := []int{3, 1, 2}
	withThumbnails := []bool{true, true, false}
	for i := 0; i < 3; i++ {
		wallet, pool := randomAddress(t), randomAddress(t)
		o, err := s.Create(ctx, CreateIntent{Wallet: wallet, Pool: pool, Title: fmt.Sprintf("Directory stream %d", i), IdempotencyKey: fmt.Sprintf("directory-%d", i)}, CreatorDecision{Wallet: wallet, Pool: pool, Allowed: true, ExpiresAt: time.Now().Add(time.Minute)}, true)
		if err != nil {
			t.Fatal(err)
		}
		providerID := fmt.Sprintf("prov_%d_%d", i, time.Now().UnixNano())
		mu.Lock()
		operationByProvider[providerID] = o.ID
		if withThumbnails[i] {
			thumbnails["pb_"+providerID] = "https://livepeercdn.studio/hls/pb_" + providerID + "/thumbnail.jpg"
		}
		mu.Unlock()
		if _, err = s.BindProvider(ctx, o.ID, providerID); err != nil {
			t.Fatal(err)
		}
		if outcome, err := s.Observe(ctx, ProviderEvent{EventID: "started_" + providerID, ProviderID: providerID, State: "LIVE", ObservedAt: time.Now().UTC().Truncate(time.Microsecond)}); err != nil || outcome != "applied" {
			t.Fatal(outcome, err)
		}
		live, err := s.Get(ctx, o.ID)
		if err != nil || live.State != "LIVE" {
			t.Fatal(live, err)
		}
		if err = counter.Initialize(ctx, o.ID); err != nil {
			t.Fatal(err)
		}
		for k := 0; k < heartbeats[i]; k++ {
			session, err := randomID()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = counter.Heartbeat(ctx, o.ID, session); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = s.pool.Exec(ctx, `INSERT INTO indexer.indexer_canonical_creations_v1(token,pool,creator) VALUES($1,$2,$3)`, randomAddress(t), pool, wallet); err != nil {
			t.Fatal(err)
		}
		if _, err = s.pool.Exec(ctx, `INSERT INTO stats.pool_stats(pool_address,market_cap) VALUES($1,$2)`, pool, marketCaps[i]); err != nil {
			t.Fatal(err)
		}
		f.operations = append(f.operations, live)
		f.pools = append(f.pools, pool)
		time.Sleep(2 * time.Millisecond)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, o := range f.operations {
			members, meta := counter.keys(o.ID)
			if err := client.Del(c, members, meta).Err(); err != nil {
				t.Error(err)
			}
		}
		for _, statement := range []string{`DELETE FROM stats.pool_stats WHERE pool_address=ANY($1::text[])`, `DELETE FROM indexer.indexer_canonical_creations_v1 WHERE pool=ANY($1::text[])`} {
			if _, err := s.pool.Exec(c, statement, f.pools); err != nil {
				t.Error(err)
			}
		}
		_ = client.Close()
	})
	f.handlers = New(Deps{Lifecycle: s, Restrictions: &RestrictionReader{Pool: s.pool}, Viewers: counter, Playback: ProviderPlayback(livepeerClient, cache), MarketCaps: &MarketCapReader{Pool: s.pool}})
	f.router = chi.NewRouter()
	f.handlers.RegisterRoutes(f.router)
	return f
}

func (f *directoryFixture) get(t *testing.T, target string) (int, directoryPage, []byte) {
	t.Helper()
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, target, nil))
	var page directoryPage
	if response.Code == 200 {
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err, response.Body.String())
		}
	}
	return response.Code, page, response.Body.Bytes()
}

func ids(page directoryPage) []string {
	out := make([]string, 0, len(page.Streams))
	for _, s := range page.Streams {
		out = append(out, s.StreamId)
	}
	return out
}

func TestLiveDirectorySortsByViewersAndMarketCapWithThumbnails(t *testing.T) {
	f := directoryHarness(t)
	a, b, c := f.operations[0].ID, f.operations[1].ID, f.operations[2].ID

	status, page, raw := f.get(t, "/v2/streams/live")
	if status != 200 || strings.Join(ids(page), ",") != strings.Join([]string{c, b, a}, ",") || page.NextCursor != nil {
		t.Fatal("default newest-first order changed", status, ids(page), string(raw))
	}
	var objects struct {
		Streams []map[string]json.RawMessage `json:"streams"`
	}
	if err := json.Unmarshal(raw, &objects); err != nil {
		t.Fatal(err)
	}
	for i, object := range objects.Streams {
		item, err := json.Marshal(object)
		if err != nil {
			t.Fatal(err)
		}
		if err = v2.ValidateJSON("Stream", item); err != nil {
			t.Fatal(string(item), err)
		}
		_, present := object["thumbnailUrl"]
		if present != (string(object["streamId"]) != `"`+c+`"`) {
			t.Fatal("thumbnail presence does not follow provider sources", i, string(item))
		}
	}
	for _, s := range page.Streams {
		if s.StreamId == c {
			if s.ThumbnailUrl.Present {
				t.Fatal("stream without provider thumbnail carried a poster")
			}
			continue
		}
		if !s.ThumbnailUrl.Present || !strings.HasPrefix(s.ThumbnailUrl.Value, "https://livepeercdn.studio/hls/pb_") || !strings.HasSuffix(s.ThumbnailUrl.Value, "/thumbnail.jpg") || s.ViewerCount == nil {
			t.Fatal(s)
		}
	}
	firstRound := f.playbacks.Load()
	if firstRound != 3 {
		t.Fatal("expected one playback-info call per live stream", firstRound)
	}

	status, page, raw = f.get(t, "/v2/streams/live?sort=viewers&limit=2")
	if status != 200 || strings.Join(ids(page), ",") != strings.Join([]string{a, c}, ",") || page.NextCursor == nil {
		t.Fatal(status, ids(page), string(raw))
	}
	if *page.Streams[0].ViewerCount != 3 || *page.Streams[1].ViewerCount != 2 {
		t.Fatal(page.Streams)
	}
	status, page, raw = f.get(t, "/v2/streams/live?sort=viewers&limit=2&cursor="+*page.NextCursor)
	if status != 200 || strings.Join(ids(page), ",") != b || page.NextCursor != nil {
		t.Fatal(status, ids(page), string(raw))
	}
	if f.playbacks.Load() != firstRound {
		t.Fatal("thumbnail lookups were not cached across directory requests", f.playbacks.Load())
	}

	status, page, raw = f.get(t, "/v2/streams/live?sort=mcap")
	if status != 200 || strings.Join(ids(page), ",") != strings.Join([]string{b, a, c}, ",") || page.NextCursor != nil {
		t.Fatal(status, ids(page), string(raw))
	}
	if _, err := f.store.pool.Exec(context.Background(), `DELETE FROM stats.pool_stats WHERE pool_address=$1`, f.pools[1]); err != nil {
		t.Fatal(err)
	}
	status, page, raw = f.get(t, "/v2/streams/live?sort=mcap&limit=1")
	if status != 200 || strings.Join(ids(page), ",") != a || page.NextCursor == nil {
		t.Fatal("stream without market cap did not sort last", status, ids(page), string(raw))
	}
	status, page, _ = f.get(t, "/v2/streams/live?sort=mcap&limit=1&cursor="+*page.NextCursor)
	if status != 200 || strings.Join(ids(page), ",") != c || page.NextCursor == nil {
		t.Fatal(status, ids(page))
	}
	status, page, _ = f.get(t, "/v2/streams/live?sort=mcap&limit=1&cursor="+*page.NextCursor)
	if status != 200 || strings.Join(ids(page), ",") != b || page.NextCursor != nil {
		t.Fatal(status, ids(page))
	}

	for _, target := range []string{"/v2/streams/live?sort=random", "/v2/streams/live?sort=viewers&limit=0", "/v2/streams/live?sort=viewers&limit=101", "/v2/streams/live?sort=viewers&limit=07", "/v2/streams/live?sort=mcap&cursor=not-a-cursor", "/v2/streams/live?sort=viewers&cursor=" + mustCursor(t, SortMarketCap, 1)} {
		if status, _, _ := f.get(t, target); status != 422 {
			t.Fatal(target, status)
		}
	}
	viewersOnly := New(Deps{Lifecycle: f.store, Restrictions: &RestrictionReader{Pool: f.store.pool}, Viewers: f.handlers.viewers, Playback: f.handlers.playback})
	router := chi.NewRouter()
	viewersOnly.RegisterRoutes(router)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v2/streams/live?sort=mcap", nil))
	if response.Code != 503 {
		t.Fatal("market cap sort without a reader must be unavailable, not silently unsorted", response.Code)
	}
}

func mustCursor(t *testing.T, sortKey string, offset int) string {
	t.Helper()
	cursor, err := encodeRankCursor(sortKey, offset)
	if err != nil {
		t.Fatal(err)
	}
	return *cursor
}

func TestMarketCapReaderJoinsCanonicalPoolsAndSkipsUnparseableValues(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS indexer; CREATE TABLE IF NOT EXISTS indexer.indexer_canonical_creations_v1(token text PRIMARY KEY,pool text NOT NULL UNIQUE,creator text NOT NULL);
CREATE SCHEMA IF NOT EXISTS stats; CREATE TABLE IF NOT EXISTS stats.pool_stats(pool_address varchar(42) PRIMARY KEY,market_cap text NOT NULL DEFAULT '0');
CREATE OR REPLACE VIEW stats.pool_market_caps_v1 AS SELECT pool_address,market_cap FROM stats.pool_stats`); err != nil {
		t.Fatal(err)
	}
	canonical, unregistered, garbage, negative := randomAddress(t), randomAddress(t), randomAddress(t), randomAddress(t)
	pools := []string{canonical, unregistered, garbage, negative}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, statement := range []string{`DELETE FROM stats.pool_stats WHERE lower(pool_address)=ANY($1::text[])`, `DELETE FROM indexer.indexer_canonical_creations_v1 WHERE pool=ANY($1::text[])`} {
			if _, err := s.pool.Exec(c, statement, pools); err != nil {
				t.Error(err)
			}
		}
	})
	for _, pool := range []string{canonical, garbage, negative} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO indexer.indexer_canonical_creations_v1(token,pool,creator) VALUES($1,$2,$3)`, randomAddress(t), pool, randomAddress(t)); err != nil {
			t.Fatal(err)
		}
	}
	mixedCase := "0x" + strings.ToUpper(canonical[2:6]) + canonical[6:]
	for pool, value := range map[string]string{mixedCase: "123456.789", unregistered: "999", garbage: "not-a-number", negative: "-5"} {
		if _, err := s.pool.Exec(ctx, `INSERT INTO stats.pool_stats(pool_address,market_cap) VALUES($1,$2)`, pool, value); err != nil {
			t.Fatal(err)
		}
	}
	reader := &MarketCapReader{Pool: s.pool}
	caps, err := reader.MarketCaps(ctx, []string{"0x" + strings.ToUpper(canonical[2:]), unregistered, garbage, negative})
	if err != nil {
		t.Fatal(err)
	}
	if len(caps) != 1 || caps[canonical] == nil || caps[canonical].FloatString(3) != "123456.789" {
		t.Fatal(caps)
	}
	if _, err = reader.MarketCaps(ctx, []string{"not-an-address"}); err == nil {
		t.Fatal("invalid pool accepted")
	}
	if _, err = (&MarketCapReader{}).MarketCaps(ctx, []string{canonical}); err == nil {
		t.Fatal("reader without a pool answered")
	}
	empty, err := reader.MarketCaps(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatal(empty, err)
	}
}
