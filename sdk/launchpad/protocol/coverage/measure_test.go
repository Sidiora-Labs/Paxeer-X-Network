package coverage

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/protocol/catalog"
)

func TestMeasurementInputRejectsMissingReviewAndAuthority(t *testing.T) {
	raw := []byte(`{"schemaVersion":1,"chainId":125}`)
	if _, err := ReadMeasurementManifest(raw, strings.Repeat("0", 64)); err == nil {
		t.Fatal("unreviewed bytes admitted")
	}
	if _, err := ReadMeasurementManifest(raw, measurementDigest(raw)); err == nil {
		t.Fatal("absent real receipt/provider expectations admitted")
	}
	raw = []byte(`{"schemaVersion":1,"chainId":125,"unexpected":true}`)
	if _, err := ReadMeasurementManifest(raw, measurementDigest(raw)); err == nil {
		t.Fatal("unknown measurement input admitted")
	}
	raw = append(make([]byte, MaxMeasurementInput), byte(1))
	if _, err := ReadMeasurementManifest(raw, measurementDigest(raw)); err == nil {
		t.Fatal("oversized reviewed input admitted")
	}
}
func TestMeasurementMissingEnabledFamilyFailsBeforeCapture(t *testing.T) {
	_, _, err := MeasurementRegistry(MeasurementManifest{EnabledFamilies: []string{"MissingEnabledFamily"}})
	if err == nil {
		t.Fatal("missing enabled family passed release admission")
	}
}
func TestMeasurementBurstRejectsMissingHistoricalReceiptCorpus(t *testing.T) {
	registry, err := catalog.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = MeasureDecoderBurst(context.Background(), registry, &HistoricalCapture{ChainID: 125}); err == nil {
		t.Fatal("empty corpus manufactured a rate result")
	}
}
func TestCompiledMeasurementSourcesIncludeTheMeasureCommand(t *testing.T) {
	sources, err := CompiledMeasurementSourceSHA256()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("cmd/measure/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if got := sources["protocol/coverage/cmd/measure/main.go"]; got != measurementDigest(raw) {
		t.Fatalf("compiled provenance digest of the measurement command = %q, want %s", got, measurementDigest(raw))
	}
	if sources["protocol/coverage/measure.go"] == "" {
		t.Fatal("compiled provenance lacks protocol/coverage/measure.go")
	}
}
func TestRealReviewedHistoricalDecoderMeasurement(t *testing.T) {
	manifestPath := os.Getenv("L02_MEASUREMENT_MANIFEST")
	review := os.Getenv("L02_MEASUREMENT_MANIFEST_SHA256")
	endpoint := os.Getenv("L02_MEASUREMENT_RPC_URL")
	if manifestPath == "" || review == "" || endpoint == "" {
		t.Fatal("actual reviewed L02 historical measurement manifest/digest and chain125 RPC inputs are required")
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(file, MaxMeasurementInput+1))
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	input, err := ReadMeasurementManifest(raw, review)
	if err != nil {
		t.Fatal(err)
	}
	registry, _, err := MeasurementRegistry(input)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	client, err := DialMeasurementRPC(ctx, endpoint)
	if err != nil {
		t.Fatal("historical RPC unavailable")
	}
	defer client.Close()
	capture, err := CaptureHistorical(ctx, client, input, review)
	if err != nil {
		t.Fatal(err)
	}
	source := &RPCSource{RPC: client, Registry: registry, PoolRegistry: input.PoolRegistry, Permissions: input.Permissions}
	replay, err := ReplayHistorical(ctx, source, input, capture)
	if err != nil {
		t.Fatal(err)
	}
	if !replay.ByteIdentical || replay.FirstSHA256 != replay.SecondSHA256 || len(replay.Blockers) != 0 {
		t.Fatalf("real replay qualification blocked: %v", replay.Blockers)
	}
	burst, err := MeasureDecoderBurst(ctx, registry, capture)
	if err != nil {
		t.Fatal(err)
	}
	if !burst.Passed || burst.Attempted != 3000000 || burst.Matched != 3000000 || len(burst.Windows) != 300 {
		t.Fatalf("real decoder replay throughput failed: %+v", burst)
	}
	evidence, err := json.Marshal(struct {
		Capture *HistoricalCapture
		Replay  *ReplayMeasurement
		Burst   *BurstMeasurement
	}{capture, replay, burst})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("L02_REAL_MEASUREMENT=%s", evidence)
}
