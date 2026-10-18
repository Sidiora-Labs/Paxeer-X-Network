package safety_test

import (
	"context"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/safety"
	"github.com/Sidiora-Technologies/KindleLaunch/media/social/internal/social"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func httpFixture(t *testing.T) *safety.Store {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("L38_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://kindle_fixture:kindle_fixture_password@127.0.0.1:25432/kindle_fixture?sslmode=disable"
	}
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := "kd_9677e1202ca2546e7bac624f_" + uuid.NewString()[:8]
	ident := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+ident); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		db.Close()
		if _, e := admin.Exec(context.Background(), "DROP SCHEMA "+ident+" CASCADE"); e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	ddl, e := os.ReadFile("../../../../spec/production-system/handoffs/L38/schema-proposal.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, string(ddl)); e != nil {
		t.Fatal(e)
	}
	return &safety.Store{DB: db}
}
func TestNativeModeratorHTTPAuthorityAndSpoofedActor(t *testing.T) {
	s := httpFixture(t)
	now := time.Now().UTC()
	authenticated := "0x1111111111111111111111111111111111111111"
	spoof := "0x9999999999999999999999999999999999999999"
	target := "0x2222222222222222222222222222222222222222"
	p := safety.Principal{Wallet: authenticated, SessionID: "native-http-session", Role: "reviewer", Expires: now.Add(time.Hour), StepUp: now}
	h := social.New(social.Deps{AdminAPIKey: "private-fixture-key"})
	router := chi.NewRouter()
	h.RegisterRoutes(router)
	request := func(principal *safety.Principal, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/bans", strings.NewReader(body))
		req.Header.Set("X-API-Key", "private-fixture-key")
		req.Header.Set("X-Actor-Wallet", spoof)
		ctx := social.WithModeration(req.Context(), s)
		if principal != nil {
			ctx = safety.WithPrincipal(ctx, *principal)
		}
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	body := `{"wallet":"` + target + `","banned_by":"` + spoof + `","reason":"abuse","duration_seconds":60}`
	if w := request(&p, body); w.Code != 403 {
		t.Fatalf("reviewer global ban: %d %s", w.Code, w.Body.String())
	}
	var actor string
	if e := s.DB.QueryRow(context.Background(), `SELECT actor FROM safety_actions WHERE kind='denied:ban'`).Scan(&actor); e != nil || actor != authenticated {
		t.Fatal("denial actor spoofed", actor, e)
	}
	if w := request(nil, body); w.Code != 403 {
		t.Fatal("static key only accepted", w.Code)
	}
	p.Role = "global_administrator"
	if w := request(&p, body); w.Code != 200 || !strings.Contains(w.Body.String(), authenticated) || strings.Contains(w.Body.String(), spoof) {
		t.Fatalf("admin %d %s", w.Code, w.Body.String())
	}
	if e := s.DB.QueryRow(context.Background(), `SELECT actor FROM safety_bans`).Scan(&actor); e != nil || actor != authenticated {
		t.Fatal("ban actor spoofed", actor, e)
	}
	p.StepUp = now.Add(-6 * time.Minute)
	if w := request(&p, body); w.Code != 403 {
		t.Fatal("stale stepup accepted", w.Code)
	}
	p.StepUp = now
	if w := request(&p, body+` {}`); w.Code != 400 {
		t.Fatal("trailing JSON accepted", w.Code)
	}
	if w := request(&p, `{"wallet":"`+target+`","reason":"`+strings.Repeat("a", 20000)+`"}`); w.Code != 400 {
		t.Fatal("oversized body accepted", w.Code)
	}
}

func TestPostgresHTTPCommentPolicyOutageReturnsUnavailableAndCommitsNothing(t *testing.T) {
	s := httpFixture(t)
	wallet := "0x1111111111111111111111111111111111111111"
	pool := "0x2222222222222222222222222222222222222222"
	now := time.Now().UTC()
	principal := safety.Principal{Wallet: wallet, SessionID: "real-policy-test", Role: "participant", Expires: now.Add(time.Hour)}
	if _, e := s.DB.Exec(context.Background(), `ALTER TABLE safety_bans RENAME TO unavailable_safety_bans`); e != nil {
		t.Fatal(e)
	}
	router := chi.NewRouter()
	social.New(social.Deps{}).RegisterRoutes(router)
	request := httptest.NewRequest(http.MethodPost, "/pool/"+pool+"/comments", strings.NewReader(`{"content":"must not commit"}`))
	request.Header.Set("X-Actor-Wallet", wallet)
	request = request.WithContext(safety.WithPrincipal(social.WithModeration(request.Context(), s), principal))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatal("policy outage was not retryable", response.Code, response.Body.String())
	}
	var writes int
	if e := s.DB.QueryRow(context.Background(), `SELECT count(*) FROM safety_outbox`).Scan(&writes); e != nil || writes != 0 {
		t.Fatal("policy outage committed outbox", writes, e)
	}
}
