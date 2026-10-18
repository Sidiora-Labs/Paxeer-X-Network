package social_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	v2 "github.com/Sidiora-Technologies/KindleLaunch/shared/interfaces/v2"

	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/db/sqlcdb"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/social"
)

func newContractHarness(t *testing.T) *harness {
	t.Helper()
	pool := internaltest.NewPostgres(t)
	q := sqlcdb.New(pool)
	h := social.New(social.Deps{Queries: q, MaxMessageLength: 500, AdminAPIKey: adminKey})
	r := chi.NewRouter()
	h.RegisterRoutes(r)
	h.RegisterContractRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &harness{q: q, pool: pool, baseURL: srv.URL}
}

func decodeContract[T any](t *testing.T, raw []byte) (T, map[string]json.RawMessage) {
	t.Helper()
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("envelope: %v (%s)", err, raw)
	}
	for _, key := range []string{"requestId", "data", "nextCursor", "watermark", "revision", "quality", "generatedAt"} {
		if _, ok := envelope[key]; !ok {
			t.Fatalf("envelope missing %s: %s", key, raw)
		}
	}
	var data T
	if err := json.Unmarshal(envelope["data"], &data); err != nil {
		t.Fatalf("data: %v (%s)", err, raw)
	}
	return data, envelope
}

func TestContractFollowGraph(t *testing.T) {
	h := newContractHarness(t)
	alice := addr(0x1a)
	bob := addr(0x1b)

	resp, out := h.do(t, http.MethodPut, "/v2/me/following/"+bob, alice, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("follow status = %d body=%s", resp.StatusCode, out)
	}
	follow, _ := decodeContract[v2.FollowData](t, out)
	if !follow.Following {
		t.Fatalf("follow data = %+v", follow)
	}
	resp, _ = h.do(t, http.MethodPut, "/v2/me/following/"+bob, alice, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("repeated follow status = %d", resp.StatusCode)
	}

	resp, out = h.do(t, http.MethodGet, "/v2/profiles/"+bob+"/follow-stats", alice, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status = %d", resp.StatusCode)
	}
	stats, _ := decodeContract[v2.FollowStatsData](t, out)
	if stats.Followers != 1 || stats.Following != 0 || !stats.IsFollowing {
		t.Fatalf("viewer stats = %+v", stats)
	}
	resp, out = h.do(t, http.MethodGet, "/v2/profiles/"+bob+"/follow-stats", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous stats status = %d", resp.StatusCode)
	}
	stats, _ = decodeContract[v2.FollowStatsData](t, out)
	if stats.Followers != 1 || stats.IsFollowing {
		t.Fatalf("anonymous stats = %+v (isFollowing must be false without a session)", stats)
	}
	_, out = h.do(t, http.MethodGet, "/v2/profiles/"+alice+"/follow-stats", bob, nil)
	stats, _ = decodeContract[v2.FollowStatsData](t, out)
	if stats.Following != 1 || stats.IsFollowing {
		t.Fatalf("alice stats seen by bob = %+v", stats)
	}

	resp, _ = h.do(t, http.MethodPut, "/v2/me/following/"+alice, alice, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("self-follow status = %d", resp.StatusCode)
	}
	resp, _ = h.do(t, http.MethodPut, "/v2/me/following/"+bob, "", nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous follow status = %d", resp.StatusCode)
	}
	resp, _ = h.do(t, http.MethodGet, "/v2/profiles/not-a-wallet/follow-stats", "", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid wallet status = %d", resp.StatusCode)
	}

	resp, out = h.do(t, http.MethodDelete, "/v2/me/following/"+bob, alice, nil)
	if resp.StatusCode != http.StatusNoContent || len(out) != 0 {
		t.Fatalf("unfollow status = %d body=%q", resp.StatusCode, out)
	}
	_, out = h.do(t, http.MethodGet, "/v2/profiles/"+bob+"/follow-stats", alice, nil)
	stats, _ = decodeContract[v2.FollowStatsData](t, out)
	if stats.Followers != 0 || stats.IsFollowing {
		t.Fatalf("stats after unfollow = %+v", stats)
	}

	resp, _ = h.do(t, http.MethodGet, "/v2/profiles/"+bob+"/followers", alice, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("followers must be unavailable without a profile authority, got %d", resp.StatusCode)
	}
}
