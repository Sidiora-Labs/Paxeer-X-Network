package shadow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sei-protocol/seictl/sidecar/rpc"
)

// In migration mode the shadow's AppHash diverges from canonical by design, so an
// AppHash-only mismatch must NOT count as a divergence; the verdict keys on
// execution-results equivalence (LastResultsHash + gas + per-tx receipts).
func TestCompareBlock_MigrationMode_AppHashExpected(t *testing.T) {
	txs := []rpc.TxResult{
		{Code: 0, GasUsed: "100", GasWanted: "200", Log: "ok", Events: json.RawMessage(`[]`)},
	}
	shadowSrv := rpcServer("SHADOW_APPHASH", "SAME_RESULTS", txs)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CANON_APPHASH", "SAME_RESULTS", txs)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL, WithMigrationMode())
	result, err := comp.CompareBlock(context.Background(), 100)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if !result.Match {
		t.Error("expected match: AppHash divergence is expected in migration mode")
	}
	if !result.MigrationMode {
		t.Error("expected result to record migration mode")
	}
	if result.Layer0.AppHashMatch {
		t.Error("expected AppHash mismatch")
	}
	if !result.Layer0.LastResultsHashMatch {
		t.Error("expected LastResultsHash match")
	}
	if result.Layer1 == nil {
		t.Error("migration mode must always run Layer 1, even when results match")
	}
	if result.DivergenceLayer != nil {
		t.Errorf("expected nil divergence layer, got %d", *result.DivergenceLayer)
	}
}

// In migration mode Layer 1 is load-bearing (AppHash is expected to differ). If
// the receipt comparison cannot run, the block must fail closed (indeterminate,
// attributed to layer 1) — never a silent clean pass.
func TestCompareBlock_MigrationMode_Layer1ErrorFailsClosed(t *testing.T) {
	// /block returns differing AppHash but matching LastResultsHash (so Layer 0 is
	// not a real divergence); /block_results errors, so Layer 1 cannot run.
	handler := func(appHash string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/block":
				_, _ = w.Write(blockJSON(appHash, "SAME_RESULTS"))
			case "/block_results":
				http.Error(w, "block_results unavailable", http.StatusInternalServerError)
			default:
				http.NotFound(w, r)
			}
		}
	}
	shadowSrv := httptest.NewServer(handler("SHADOW_APPHASH"))
	defer shadowSrv.Close()
	canonicalSrv := httptest.NewServer(handler("CANON_APPHASH"))
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL, WithMigrationMode())
	result, err := comp.CompareBlock(context.Background(), 100)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if result.Match {
		t.Error("expected NOT clean: Layer 1 could not run in migration mode, must fail closed")
	}
	if result.DivergenceLayer == nil || *result.DivergenceLayer != 1 {
		t.Errorf("expected divergence layer 1, got %v", result.DivergenceLayer)
	}
	if result.Layer1 == nil || !result.Layer1.Indeterminate {
		t.Errorf("expected Layer1 marked indeterminate, got %+v", result.Layer1)
	}
}

// A LastResultsHash mismatch is a real execution divergence even in migration
// mode, attributed to Layer 0.
func TestCompareBlock_MigrationMode_ResultsDivergence(t *testing.T) {
	shadowSrv := rpcServer("SHADOW_APPHASH", "SHADOW_RESULTS", nil)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CANON_APPHASH", "CANON_RESULTS", nil)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL, WithMigrationMode())
	result, err := comp.CompareBlock(context.Background(), 100)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if result.Match {
		t.Error("expected divergence: LastResultsHash mismatch is a real execution divergence")
	}
	if result.DivergenceLayer == nil || *result.DivergenceLayer != 0 {
		t.Errorf("expected divergence layer 0, got %v", result.DivergenceLayer)
	}
}

// When results hashes agree but a per-tx receipt differs, migration mode still
// catches it at Layer 1 (which always runs in migration mode).
func TestCompareBlock_MigrationMode_ReceiptDivergence(t *testing.T) {
	shadowTxs := []rpc.TxResult{
		{Code: 0, GasUsed: "100", GasWanted: "200", Log: "ok", Events: json.RawMessage(`[]`)},
	}
	canonicalTxs := []rpc.TxResult{
		{Code: 1, GasUsed: "150", GasWanted: "200", Log: "reverted", Events: json.RawMessage(`[]`)},
	}
	shadowSrv := rpcServer("SHADOW_APPHASH", "SAME_RESULTS", shadowTxs)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CANON_APPHASH", "SAME_RESULTS", canonicalTxs)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL, WithMigrationMode())
	result, err := comp.CompareBlock(context.Background(), 100)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if result.Match {
		t.Error("expected divergence: a per-tx receipt differs")
	}
	if result.DivergenceLayer == nil || *result.DivergenceLayer != 1 {
		t.Errorf("expected divergence layer 1, got %v", result.DivergenceLayer)
	}
	if result.Layer1 == nil || len(result.Layer1.Divergences) != 1 {
		t.Errorf("expected 1 tx divergence, got %+v", result.Layer1)
	}
}
