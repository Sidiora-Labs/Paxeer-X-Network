package analytics_test

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/analytics"
	"github.com/Sidiora-Technologies/KindleLaunch/core/stats-workers/internal/httpapi"
	"github.com/go-chi/chi/v5"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPhase2PublicReactionReadKeepsWalletPrivate(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	pool := "0x" + strings.Repeat("3", 40)
	wallet := "0x" + strings.Repeat("4", 40)
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "INSERT INTO "+a.Schema+".reaction_pools(pool,revision) VALUES($1,9)", pool); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+a.Schema+".reaction_votes(pool,wallet,reaction,revision) VALUES($1,$2,'diamond',2)", pool, wallet); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	result, err := a.ReadReactions(ctx, pool)
	if err != nil || result.Revision != "9" || result.Counts["diamond"] != 1 || result.Counts["hot"] != 0 {
		t.Fatal(result, err)
	}
	for _, cursor := range []string{"-1", "01", "1.0", "9223372036854775808", "0;DROP"} {
		if _, err = a.ReplaySnapshots(ctx, pool, cursor, 1); err == nil {
			t.Fatal(cursor)
		}
	}
	for _, limit := range []int{-1, 0, 101} {
		if _, err = a.ReplaySnapshots(ctx, pool, "0", limit); err == nil {
			t.Fatal(limit)
		}
	}
}

func TestPhase3SnapshotCorruptionAndStaleDisclosure(t *testing.T) {
	a := phase2Authority(t)
	ctx := context.Background()
	pool := "0x" + strings.Repeat("9", 40)
	now := time.Now().Unix()
	wm := analytics.Watermark{ChainID: "125", Height: "9", BlockHash: "0x" + strings.Repeat("d", 64), ProjectionRevision: "1", ObservedAt: time.Unix(now-31, 0).UTC().Format("2006-01-02T15:04:05.000Z"), Integrity: "healthy"}
	payload, err := json.Marshal(analytics.PressureWindows{})
	if err != nil {
		t.Fatal(err)
	}
	s := analytics.Snapshot{Pool: pool, Model: "pressure-v1", Watermark: wm, CalculatedAt: now - 31, Payload: payload}
	tx, err := a.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = a.PersistSnapshot(ctx, tx, s); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+a.Schema+".analytics_outbox(pool,model,revision,payload) VALUES($1,$2,1,$3::jsonb)", pool, s.Model, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	httpapi.RegisterDurableAnalytics(router, a)
	read := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", "/v2/stats/"+pool+"/pressure", nil))
		return response
	}
	response := read()
	var served analytics.SnapshotRead
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &served) != nil || !served.Stale || served.Snapshot.CalculatedAt != now-31 || served.Snapshot.Watermark.ObservedAt != wm.ObservedAt {
		t.Fatal(response.Code, response.Body.String())
	}
	for _, mutation := range []string{
		"jsonb_set(payload,'{inputs_watermark,height}','\"8\"')",
		"jsonb_set(payload,'{inputs_watermark,projectionRevision}','\"2\"')",
		"jsonb_set(payload,'{payload}','null')",
		"jsonb_set(payload,'{calculated_at}','0')",
	} {
		if _, err = a.Pool.Exec(ctx, "UPDATE "+a.Schema+".analytics_snapshots SET payload="+mutation); err != nil {
			t.Fatal(err)
		}
		if _, err = a.ReadSnapshot(ctx, pool, s.Model, now); !errors.Is(err, analytics.ErrConflict) {
			t.Fatal("corrupt persisted row served successfully", mutation, err)
		}
		response = read()
		if response.Code != 409 {
			t.Fatal(response.Code, response.Body.String())
		}
		if _, err = a.Pool.Exec(ctx, "UPDATE "+a.Schema+".analytics_snapshots SET payload=$1::jsonb", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = a.Pool.Exec(ctx, "UPDATE "+a.Schema+".analytics_outbox SET model='risk-v1'"); err != nil {
		t.Fatal(err)
	}
	if _, err = a.ReplaySnapshots(ctx, pool, "0", 100); !errors.Is(err, analytics.ErrConflict) {
		t.Fatal("conflicting outbox identity served", err)
	}
	if _, err = a.Pool.Exec(ctx, "UPDATE "+a.Schema+".analytics_snapshots SET payload=jsonb_set(payload,'{inputs_watermark,integrity}','\"quarantined\"')"); err != nil {
		t.Fatal(err)
	}
	quarantined, err := a.ReadSnapshot(ctx, pool, s.Model, now-31)
	if err != nil || !quarantined.Stale {
		t.Fatal("quarantine presented as fresh", quarantined, err)
	}
}
