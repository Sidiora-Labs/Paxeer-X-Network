package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/referral"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

func newEventsRouter(t *testing.T) (http.Handler, *store.Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	svc := referral.New(st, quietLogger(), 1, func() int64 { return 1 })
	r := chi.NewRouter()
	httpapi.RegisterReferrals(r, svc)
	return r, st, ctx
}

func seedCard(t *testing.T, ctx context.Context, st *store.Store, cardID, code, sharer string) {
	t.Helper()
	if err := st.InsertCard(ctx, store.CardRow{
		CardID: cardID, ShortCode: code, OwnerAddress: sharer,
		PoolAddress: "0xp", TokenAddress: "0xt", Snapshot: json.RawMessage(`{}`), CreatedAt: 1,
	}); err != nil {
		t.Fatalf("seed card: %v", err)
	}
}

func TestLogEventAndSharerStats(t *testing.T) {
	r, st, ctx := newEventsRouter(t)
	seedCard(t, ctx, st, "c1", "code1", "0xsharer")

	t.Run("click is accepted", func(t *testing.T) {
		body := mustJSON(t, map[string]string{"type": "click", "shortCode": "code1"})
		rec := serve(t, r, http.MethodPost, "/pnl/events", body, map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body=%s)", rec.Code, rec.Body.String())
		}
		var resp struct {
			OK bool `json:"ok"`
		}
		decode(t, rec, &resp)
		if !resp.OK {
			t.Fatal("ok = false")
		}
	})

	t.Run("unknown referral is 400", func(t *testing.T) {
		body := mustJSON(t, map[string]string{"type": "click", "shortCode": "nope"})
		rec := serve(t, r, http.MethodPost, "/pnl/events", body, map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing type is 400", func(t *testing.T) {
		rec := serve(t, r, http.MethodPost, "/pnl/events", []byte(`{}`), map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("invalid event type is 400", func(t *testing.T) {
		body := mustJSON(t, map[string]string{"type": "bogus", "shortCode": "code1"})
		rec := serve(t, r, http.MethodPost, "/pnl/events", body, map[string]string{"Content-Type": "application/json"})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("sharer stats reflects logged events", func(t *testing.T) {
		rec := serve(t, r, http.MethodGet, "/referrals/0xsharer/stats", nil, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		var stats store.SharerStats
		decode(t, rec, &stats)
		if stats.Address != "0xsharer" || stats.TotalClicks != 1 {
			t.Fatalf("stats = %+v", stats)
		}
		if len(stats.ShortCodes) != 1 || stats.ShortCodes[0] != "code1" {
			t.Fatalf("shortCodes = %v", stats.ShortCodes)
		}
	})
}
