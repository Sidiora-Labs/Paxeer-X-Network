package httpapi_test

import (
	"bytes"
	"context"
	"image/png"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/card"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/ogrender"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

func newCardRouter(t *testing.T) (http.Handler, *store.Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	rdb := internaltest.NewRedis(t)
	internaltest.EnsureStatsSchema(t, st.Pool())
	internaltest.EnsureMetadataSchema(t, st.Pool())

	svc := card.New(st, "https://sidiora.fun", "https://og.example", func() int64 { return 7 })
	r := chi.NewRouter()
	httpapi.RegisterCards(r, httpapi.CardDeps{
		Cards:      svc,
		Renderer:   ogrender.New(""),
		Redis:      rdb,
		ShareLabel: func(code string) string { return "sidiora.fun/pnl/" + code },
	})
	return r, st, ctx
}

func TestMintAndHydrateCard(t *testing.T) {
	r, st, ctx := newCardRouter(t)

	foldBuy(t, ctx, st, "0xb-0", "0xowner", "0xpool", "0xtoken", "1000000", oneToken, 1, 1)
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO stats.pool_stats (pool_address, token_address, price, market_cap, price_change_24h)
		VALUES ('0xpool','0xtoken','2500000','5000000','100')`); err != nil {
		t.Fatalf("seed stats: %v", err)
	}

	body := mustJSON(t, map[string]string{"ownerAddress": "0xOWNER", "poolAddress": "0xPool"})
	rec := serve(t, r, http.MethodPost, "/pnl/cards", body, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("mint status = %d (body=%s)", rec.Code, rec.Body.String())
	}
	var minted card.Minted
	decode(t, rec, &minted)
	if minted.CardID == "" || minted.ShortCode == "" {
		t.Fatalf("minted = %+v", minted)
	}
	if minted.Snapshot.OwnerAddress != "0xowner" {
		t.Errorf("owner = %s", minted.Snapshot.OwnerAddress)
	}

	t.Run("hydrate by id", func(t *testing.T) {
		got := serve(t, r, http.MethodGet, "/pnl/cards/"+minted.CardID, nil, nil)
		if got.Code != http.StatusOK {
			t.Fatalf("get status = %d", got.Code)
		}
		var hydrated card.Minted
		decode(t, got, &hydrated)
		if hydrated.ShortCode != minted.ShortCode {
			t.Errorf("shortCode = %s, want %s", hydrated.ShortCode, minted.ShortCode)
		}
	})

	t.Run("og image renders a PNG", func(t *testing.T) {
		got := serve(t, r, http.MethodGet, "/pnl/cards/"+minted.CardID+"/og.png", nil, nil)
		if got.Code != http.StatusOK {
			t.Fatalf("og status = %d", got.Code)
		}
		if ct := got.Header().Get("Content-Type"); ct != "image/png" {
			t.Fatalf("content-type = %s", ct)
		}
		if _, err := png.Decode(bytes.NewReader(got.Body.Bytes())); err != nil {
			t.Fatalf("decode png: %v", err)
		}
	})

	t.Run("og image second hit served from cache", func(t *testing.T) {
		got := serve(t, r, http.MethodGet, "/pnl/cards/"+minted.CardID+"/og.png", nil, nil)
		if got.Code != http.StatusOK || got.Body.Len() == 0 {
			t.Fatalf("cached og status = %d len = %d", got.Code, got.Body.Len())
		}
	})
}

func TestMintRejectsNoPosition(t *testing.T) {
	r, _, _ := newCardRouter(t)
	body := mustJSON(t, map[string]string{"ownerAddress": "0xnobody", "poolAddress": "0xpool"})
	rec := serve(t, r, http.MethodPost, "/pnl/cards", body, map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestMintBadBody(t *testing.T) {
	r, _, _ := newCardRouter(t)
	rec := serve(t, r, http.MethodPost, "/pnl/cards", []byte(`{"ownerAddress":""}`), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetCardNotFound(t *testing.T) {
	r, _, _ := newCardRouter(t)
	rec := serve(t, r, http.MethodGet, "/pnl/cards/missing", nil, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	og := serve(t, r, http.MethodGet, "/pnl/cards/missing/og.png", nil, nil)
	if og.Code != http.StatusNotFound {
		t.Fatalf("og status = %d, want 404", og.Code)
	}
}
