package shadow

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sei-protocol/seictl/sidecar/rpc"
)

// blockJSON builds a minimal /block JSON-RPC response with the given header fields.
func blockJSON(appHash, lastResultsHash string) []byte {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      -1,
		"result": map[string]any{
			"block_id": map[string]any{"hash": ""},
			"block": map[string]any{
				"header": map[string]any{
					"app_hash":          appHash,
					"last_results_hash": lastResultsHash,
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// blockResultsJSON builds a minimal /block_results JSON-RPC response.
func blockResultsJSON(txs []rpc.TxResult) []byte {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      -1,
		"result": map[string]any{
			"txs_results": txs,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// rpcServer creates an httptest server that responds to /block and /block_results.
func rpcServer(appHash, lastResultsHash string, txs []rpc.TxResult) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/block":
			w.Write(blockJSON(appHash, lastResultsHash))
		case r.URL.Path == "/block_results":
			w.Write(blockResultsJSON(txs))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestCompareBlock_Match(t *testing.T) {
	srv := rpcServer("AABB", "CCDD", nil)
	defer srv.Close()

	comp := NewComparator(srv.URL, srv.URL)
	result, err := comp.CompareBlock(context.Background(), 100)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if !result.Match {
		t.Error("expected match when both endpoints return identical data")
	}
	if result.DivergenceLayer != nil {
		t.Errorf("expected nil divergence layer, got %d", *result.DivergenceLayer)
	}
	if !result.Layer0.AppHashMatch {
		t.Error("expected AppHash match")
	}
	if result.Layer1 != nil {
		t.Error("expected nil Layer1 when Layer0 matches")
	}
}

func TestCompareBlock_Layer0Divergence(t *testing.T) {
	shadowSrv := rpcServer("SHADOW_HASH", "RESULTS_HASH", nil)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CANONICAL_HASH", "RESULTS_HASH", nil)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL)
	result, err := comp.CompareBlock(context.Background(), 100)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if result.Match {
		t.Error("expected divergence")
	}
	if result.DivergenceLayer == nil || *result.DivergenceLayer != 0 {
		t.Errorf("expected divergence layer 0, got %v", result.DivergenceLayer)
	}
	if result.Layer0.AppHashMatch {
		t.Error("expected AppHash mismatch")
	}
	if result.Layer0.ShadowAppHash != "SHADOW_HASH" {
		t.Errorf("shadow app hash = %q, want SHADOW_HASH", result.Layer0.ShadowAppHash)
	}
	if result.Layer0.CanonicalAppHash != "CANONICAL_HASH" {
		t.Errorf("canonical app hash = %q, want CANONICAL_HASH", result.Layer0.CanonicalAppHash)
	}
}

func TestCompareBlock_Layer1TxDivergence(t *testing.T) {
	shadowTxs := []rpc.TxResult{
		{Code: 0, GasUsed: "100", GasWanted: "200", Log: "ok", Events: json.RawMessage(`[]`)},
	}
	canonicalTxs := []rpc.TxResult{
		{Code: 1, GasUsed: "150", GasWanted: "200", Log: "reverted", Events: json.RawMessage(`[]`)},
	}

	shadowSrv := rpcServer("AAA", "BBB", shadowTxs)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CCC", "BBB", canonicalTxs)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL)
	result, err := comp.CompareBlock(context.Background(), 50)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if result.Match {
		t.Error("expected divergence")
	}
	if result.Layer1 == nil {
		t.Fatal("expected Layer1 to be populated")
	}
	if len(result.Layer1.Divergences) != 1 {
		t.Fatalf("expected 1 tx divergence, got %d", len(result.Layer1.Divergences))
	}

	div := result.Layer1.Divergences[0]
	if div.TxIndex != 0 {
		t.Errorf("tx index = %d, want 0", div.TxIndex)
	}

	// Should have divergences for code, gasUsed, and log.
	fieldNames := make(map[string]bool)
	for _, f := range div.Fields {
		fieldNames[f.Field] = true
	}
	for _, expected := range []string{"code", "gasUsed", "log"} {
		if !fieldNames[expected] {
			t.Errorf("expected field %q in divergences", expected)
		}
	}
}

func TestCompareBlock_Layer1TxCountMismatch(t *testing.T) {
	shadowTxs := []rpc.TxResult{
		{Code: 0, GasUsed: "100", GasWanted: "200"},
		{Code: 0, GasUsed: "100", GasWanted: "200"},
	}
	canonicalTxs := []rpc.TxResult{
		{Code: 0, GasUsed: "100", GasWanted: "200"},
	}

	shadowSrv := rpcServer("AAA", "BBB", shadowTxs)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CCC", "BBB", canonicalTxs)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL)
	result, err := comp.CompareBlock(context.Background(), 50)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	if result.Layer1 == nil {
		t.Fatal("expected Layer1 result")
	}
	if result.Layer1.TxCountMatch {
		t.Error("expected tx count mismatch")
	}

	// Extra tx on shadow side should be reported as "presence" divergence.
	found := false
	for _, d := range result.Layer1.Divergences {
		for _, f := range d.Fields {
			if f.Field == "presence" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a presence divergence for the extra tx")
	}
}

func TestCompareResult_Diverged(t *testing.T) {
	match := CompareResult{Match: true}
	if match.Diverged() {
		t.Error("expected Diverged()=false for matching result")
	}

	mismatch := CompareResult{Match: false}
	if !mismatch.Diverged() {
		t.Error("expected Diverged()=true for mismatching result")
	}
}

func TestLayer0Result_Match(t *testing.T) {
	cases := []struct {
		name string
		r    Layer0Result
		want bool
	}{
		{"all match", Layer0Result{AppHashMatch: true, LastResultsHashMatch: true, GasUsedMatch: true}, true},
		{"app hash mismatch", Layer0Result{AppHashMatch: false, LastResultsHashMatch: true, GasUsedMatch: true}, false},
		{"results hash mismatch", Layer0Result{AppHashMatch: true, LastResultsHashMatch: false, GasUsedMatch: true}, false},
		{"gas mismatch", Layer0Result{AppHashMatch: true, LastResultsHashMatch: true, GasUsedMatch: false}, false},
		{"all mismatch", Layer0Result{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.Match(); got != tc.want {
				t.Errorf("Match() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCompareBlock_AllMatch_NoLayer1(t *testing.T) {
	srv := rpcServer("SAME", "SAME", nil)
	defer srv.Close()

	comp := NewComparator(srv.URL, srv.URL)
	result, err := comp.CompareBlock(context.Background(), 1)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}
	if !result.Match {
		t.Error("expected match")
	}
	if result.Layer0.ShadowAppHash != "" {
		t.Error("expected empty ShadowAppHash when matching")
	}
	if result.Layer0.CanonicalAppHash != "" {
		t.Error("expected empty CanonicalAppHash when matching")
	}
	if result.Layer1 != nil {
		t.Error("expected nil Layer1 when all hashes match")
	}
}

func TestCompareBlock_LastResultsHashDivergence(t *testing.T) {
	shadowSrv := rpcServer("SAME_APP", "SHADOW_RES", nil)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("SAME_APP", "CANONICAL_RES", nil)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL)
	result, err := comp.CompareBlock(context.Background(), 1)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}
	if result.Match {
		t.Error("expected divergence on LastResultsHash mismatch")
	}
	if !result.Layer0.AppHashMatch {
		t.Error("expected AppHash to match")
	}
	if result.Layer0.LastResultsHashMatch {
		t.Error("expected LastResultsHash mismatch")
	}
	if result.Layer0.ShadowLastResultsHash != "SHADOW_RES" {
		t.Errorf("ShadowLastResultsHash = %q, want SHADOW_RES", result.Layer0.ShadowLastResultsHash)
	}
}

// --- DivergenceReport tests ---

func TestBuildDivergenceReport_CapturesBothChains(t *testing.T) {
	shadowSrv := rpcServer("SHADOW_HASH", "RESULTS", nil)
	defer shadowSrv.Close()
	canonicalSrv := rpcServer("CANONICAL_HASH", "RESULTS", nil)
	defer canonicalSrv.Close()

	comp := NewComparator(shadowSrv.URL, canonicalSrv.URL)
	comparison, err := comp.CompareBlock(context.Background(), 42)
	if err != nil {
		t.Fatalf("CompareBlock: %v", err)
	}

	report, err := comp.BuildDivergenceReport(context.Background(), 42, *comparison)
	if err != nil {
		t.Fatalf("BuildDivergenceReport: %v", err)
	}

	if report.Height != 42 {
		t.Errorf("Height = %d, want 42", report.Height)
	}
	if report.Timestamp == "" {
		t.Error("expected non-empty Timestamp")
	}
	if report.Comparison.Match {
		t.Error("expected divergent comparison in report")
	}
	if len(report.Shadow.Block) == 0 {
		t.Error("expected non-empty Shadow.Block")
	}
	if len(report.Shadow.BlockResults) == 0 {
		t.Error("expected non-empty Shadow.BlockResults")
	}
	if len(report.Canonical.Block) == 0 {
		t.Error("expected non-empty Canonical.Block")
	}
	if len(report.Canonical.BlockResults) == 0 {
		t.Error("expected non-empty Canonical.BlockResults")
	}
}

func TestBuildDivergenceReport_RPCFailure(t *testing.T) {
	goodSrv := rpcServer("AA", "BB", nil)
	defer goodSrv.Close()
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer badSrv.Close()

	comp := NewComparator(badSrv.URL, goodSrv.URL)
	comparison := CompareResult{Height: 1, Match: false}

	_, err := comp.BuildDivergenceReport(context.Background(), 1, comparison)
	if err == nil {
		t.Error("expected error when shadow RPC fails during report capture")
	}
}

// --- RenderMarkdown tests ---

func TestRenderMarkdown_DivergentReport(t *testing.T) {
	layer := 0
	report := &DivergenceReport{
		Height:    198740042,
		Timestamp: "2026-03-28T01:00:00Z",
		Comparison: CompareResult{
			Height: 198740042, Match: false, DivergenceLayer: &layer,
			Layer0: Layer0Result{
				AppHashMatch: false, LastResultsHashMatch: true, GasUsedMatch: true,
				ShadowAppHash: "AABBCCDD11223344AABBCCDD11223344", CanonicalAppHash: "11223344AABBCCDD11223344AABBCCDD",
			},
			Layer1: &Layer1Result{
				TotalTxs: 4, TxCountMatch: true,
				Divergences: []TxDivergence{
					{TxIndex: 3, Fields: []FieldDivergence{
						{Field: "code", Shadow: float64(0), Canonical: float64(1)},
						{Field: "gasUsed", Shadow: "142000", Canonical: "154000"},
					}},
				},
			},
		},
	}

	md := RenderMarkdown(report)

	checks := []string{
		"# Divergence Report — Height 198740042",
		"2026-03-28T01:00:00Z",
		"AppHash",
		"AABBCCDD...3344",
		"11223344...CCDD",
		"❌",
		"LastResultsHash",
		"✅",
		"## Layer 1",
		"Transaction 3",
		"code",
		"gasUsed",
		"142000",
		"154000",
	}

	for _, want := range checks {
		found := false
		for i := 0; i <= len(md)-len(want); i++ {
			if md[i:i+len(want)] == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("markdown missing %q", want)
		}
	}
}

func TestRenderMarkdown_MatchingReport(t *testing.T) {
	report := &DivergenceReport{
		Height:    100,
		Timestamp: "2026-03-28T00:00:00Z",
		Comparison: CompareResult{
			Height: 100, Match: true,
			Layer0: Layer0Result{AppHashMatch: true, LastResultsHashMatch: true, GasUsedMatch: true},
		},
	}

	md := RenderMarkdown(report)
	if !contains(md, "✅") {
		t.Error("expected checkmarks for matching report")
	}
	if contains(md, "Layer 1") {
		t.Error("should not include Layer 1 section when Layer0 matches")
	}
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestCompareBlock_RPCError(t *testing.T) {
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer badSrv.Close()

	goodSrv := rpcServer("AA", "BB", nil)
	defer goodSrv.Close()

	comp := NewComparator(badSrv.URL, goodSrv.URL)
	_, err := comp.CompareBlock(context.Background(), 1)
	if err == nil {
		t.Error("expected error when shadow RPC fails")
	}
}
