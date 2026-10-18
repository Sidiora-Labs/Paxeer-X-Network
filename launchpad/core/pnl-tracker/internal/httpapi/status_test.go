package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/httpapi"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/internaltest"
	"github.com/Sidiora-Technologies/KindleLaunch/core/pnl-tracker/internal/store"
)

func TestStatusReportsLagAndCursor(t *testing.T) {
	ctx := context.Background()
	st := store.New(internaltest.NewPostgres(t))
	internaltest.EnsureIndexerSchema(t, st.Pool())
	r := chi.NewRouter()
	httpapi.RegisterStatus(r, httpapi.StatusDeps{Store: st, StartedAt: time.Now().Add(-time.Minute)})

	// Indexer head at block 100.
	if _, err := st.Pool().Exec(ctx, `
		INSERT INTO indexer.swaps (id, pool_id, pool_address, sender, is_buy, amount_in, amount_out, fee, price, block_number, block_timestamp, tx_hash, log_index)
		VALUES ('0xs-0','p','0xpool','0xa',true,'1','1','0','0',100,100,'0xs',0)`); err != nil {
		t.Fatalf("seed swap: %v", err)
	}
	// Consumer folded up to block 40.
	foldBuy(t, ctx, st, "0xf-0", "0xu", "0xpool", "0xtoken", "1000000", oneToken, 40, 1)
	// Reconciler cursor at block 30.
	if err := st.AdvanceCursor(ctx, 30, 0, "0xc", 1); err != nil {
		t.Fatalf("advance cursor: %v", err)
	}

	rec := serve(t, r, http.MethodGet, "/status", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp struct {
		IndexerHead     int64  `json:"indexerHead"`
		ConsumerBlock   int64  `json:"consumerBlock"`
		ConsumerLag     int64  `json:"consumerLag"`
		ReconcilerBlock int64  `json:"reconcilerBlock"`
		Uptime          string `json:"uptime"`
		Status          string `json:"status"`
	}
	decode(t, rec, &resp)
	if resp.IndexerHead != 100 || resp.ConsumerBlock != 40 || resp.ConsumerLag != 60 {
		t.Fatalf("head/consumer/lag = %d/%d/%d, want 100/40/60", resp.IndexerHead, resp.ConsumerBlock, resp.ConsumerLag)
	}
	if resp.ReconcilerBlock != 30 {
		t.Errorf("reconcilerBlock = %d, want 30", resp.ReconcilerBlock)
	}
	if resp.Status != "ok" || resp.Uptime == "" {
		t.Errorf("status = %s uptime = %s", resp.Status, resp.Uptime)
	}
}
