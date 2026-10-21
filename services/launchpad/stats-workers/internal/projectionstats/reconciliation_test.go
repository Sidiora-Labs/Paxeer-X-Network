package projectionstats

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPhase3ActualReceiptAssetReconciliation(t *testing.T) {
	ctx, repo := repository(t)
	authority, labels := historicalAuthority(t)
	expected := []AssetFee{}
	var watermark Position
	var observed int64
	for _, label := range []string{"buy", "sell", "direct-swap", "rounding-buy", "multihop"} {
		for _, trade := range authority.Trades(labels[label]) {
			if watermark.ChainID == "" || trade.Position.Compare(watermark) > 0 {
				watermark = trade.Position
			}
			if timestamp := authority.admitted[trade.EventID].timestamp; timestamp > observed {
				observed = timestamp
			}
			expected = append(expected, AssetFee{trade.FeeAssetAddress, trade.FeeAssetDecimals, trade.FeeDisposition, trade.FeeRaw})
			if _, err := repo.ApplyAuthorized(ctx, authority, trade, "phase3-fees", "phase3-fees"); err != nil {
				t.Fatal(err)
			}
		}
	}
	actual, err := repo.AggregateWindow(ctx, "phase3-fees", watermark.Height, observed, 86400)
	if err != nil {
		t.Fatal(err)
	}
	result, err := ReconcileTradingFees(expected, actual.Fees, "0", watermark, "1")
	if err != nil || len(result.Disagreements) != 0 {
		t.Fatal(result, err)
	}
	changed := append([]AssetFee(nil), actual.Fees...)
	changed[0].Raw = "0"
	result, err = ReconcileTradingFees(expected, changed, "0", watermark, "2")
	if err != nil || len(result.Disagreements) != 1 {
		t.Fatal(result, err)
	}
	disagreement := result.Disagreements[0]
	if disagreement.AssetAddress != actual.Fees[0].Asset || disagreement.AssetDecimals != actual.Fees[0].Decimals || disagreement.ExpectedRaw != actual.Fees[0].Raw || disagreement.ObservedRaw != "0" || disagreement.ThroughHeight != watermark.Height || disagreement.Revision != "2" || disagreement.RawUnit == "" {
		t.Fatal(disagreement)
	}
	unchanged, err := repo.AggregateWindow(ctx, "phase3-fees", watermark.Height, observed, 86400)
	if err != nil || unchanged.Fees[0].Raw != actual.Fees[0].Raw {
		t.Fatal("reconciliation changed immutable facts", unchanged, err)
	}
	t.Logf("actual canonical asset discrepancies=%+v", result.Disagreements)
}

func TestPhase4RealMeasurementProvidersAndIntegerReference(t *testing.T) {
	config := os.Getenv("L13_PHASE4_CONFIG_FILE")
	configDigest := os.Getenv("L13_PHASE4_CONFIG_SHA256")
	if config == "" || !qualifiedDigest.MatchString(configDigest) {
		t.Fatal("admitted current-source Phase4 config and SHA256 required")
	}
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "evidence/production-system/L13/phase4", fmt.Sprintf("native-reference-%d", time.Now().UnixNano()))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runner := filepath.Join(root, "core/stats-workers/internal/projectionstats/qualify_phase2.py")
	command := exec.CommandContext(ctx, "python3", runner, "--config", config, "--config-sha256", configDigest, "--output", output, "--reconcile-only")
	captured, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("real provider/reference runner exit=%v log=%s output=%s", err, output, captured)
	}
	raw, err := os.ReadFile(filepath.Join(output, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Inputs    bool   `json:"measurement_inputs_available"`
		Matched   bool   `json:"lane_arithmetic_matched"`
		Measured  bool   `json:"measurements_executed"`
		Qualified bool   `json:"capacity_qualified"`
		FinalTask string `json:"final_shared_capacity_task"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.Inputs || !result.Matched || result.Measured || result.Qualified || result.FinalTask != "9.1" {
		t.Fatal("actual provider/reference boundary differs", string(raw))
	}
	raw, err = os.ReadFile(filepath.Join(output, "artifact-sha256.json"))
	if err != nil {
		t.Fatal(err)
	}
	var hashes map[string]string
	if json.Unmarshal(raw, &hashes) != nil {
		t.Fatal("artifact digest manifest malformed")
	}
	for _, name := range []string{"candidate-bindings.json", "sql-telemetry.ndjson", "broker-telemetry.ndjson", "resource-telemetry.ndjson", "canonical-reference.ndjson", "integer-reconciliation.json", "reference.sqlite", "result.json"} {
		bytes, err := os.ReadFile(filepath.Join(output, name))
		if err != nil || len(bytes) == 0 || digestBytes(bytes) != hashes[name] {
			t.Fatal("actual raw artifact digest missing/mismatched", name, err)
		}
	}
	t.Logf("real Phase4 native arithmetic/reference output=%s exit=0 final_capacity_task=9.1", output)
}
