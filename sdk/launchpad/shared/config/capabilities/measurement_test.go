package capabilities

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Sidiora-Technologies/KindleLaunch/shared/config/deployment"
	"github.com/ethereum/go-ethereum/common"
)

func TestMeasurementStrictPlanRejectsAmbiguousInputs(t *testing.T) {
	for _, data := range []string{`{"schemaVersion":1,"schemaVersion":1}`, `{"schemaVersion":1,"SchemaVersion":1}`, `{"cases":[{"kind":"quotes","kind":"exports"}]}`, `{"unexpected":"secret"}`, `{} {}`, strings.Repeat(" ", 8<<20+1)} {
		if _, err := DecodeMeasurementPlan([]byte(data)); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("ambiguous plan accepted: %v", err)
		}
	}
	if _, err := DecodeMeasurementPlan([]byte(`{"schemaVersion":1}`)); err != nil {
		t.Fatal(err)
	}
}

func TestMeasurementPercentilesUseNearestRankAndNoInterpolation(t *testing.T) {
	values := []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	for _, test := range []struct {
		percent  int
		expected int64
	}{{50, 5}, {95, 10}, {99, 10}} {
		if actual := measurementPercentile(values, test.percent); actual != test.expected {
			t.Fatalf("p%d=%d expected%d", test.percent, actual, test.expected)
		}
	}
	if measurementPercentile(nil, 95) != 0 {
		t.Fatal("empty sample invented percentile")
	}
}

func TestMeasurementRequiresActualPinnedInputIdentity(t *testing.T) {
	if err := (MeasurementPlan{}).Validate(nil); err == nil {
		t.Fatal("missing real engine admitted")
	}
	manifest, _, _ := policyFixture()
	engine := &PreflightEngine{manifest: manifest, revision: deployment.Revision{ManifestSHA256: strings.Repeat("a", 64), RevisionSHA256: strings.Repeat("b", 64)}}
	plan := MeasurementPlan{SchemaVersion: 1, CandidateSHA256: strings.Repeat("a", 64), AuthoritySHA256: strings.Repeat("b", 64), PolicySHA256: strings.Repeat("c", 64), SamplesPerCase: 1, Concurrency: 1, DeadlineMilliseconds: 1000, P95BudgetMicroseconds: 1000, MaxRawBytes: 1 << 20, Cases: make([]MeasurementCase, 9)}
	if err := plan.Validate(engine); err == nil {
		t.Fatal("absent consumer build identity admitted")
	}
	report, err := engine.Measure(context.Background(), plan, io.Discard)
	if err == nil || report.MeasurementPassed || report.ReleaseReady || report.CompletedSamples != 0 || report.RawBytes != 0 {
		t.Fatal("invalid inputs created measured success")
	}
}

func assertMeasuredActualReads(t *testing.T, ctx context.Context, engine *PreflightEngine, block common.Hash, number uint64, pool, trader common.Address) {
	t.Helper()
	ctx = context.WithValue(ctx, measurementScopeKey{}, measurementScope{Index: 0, Kind: "no-oracle"})
	var raw bytes.Buffer
	digest := sha256.New()
	capture := &measurementCapture{reader: engine.reader, writer: &raw, hash: digest, maxBytes: 16 << 20}
	measured := *engine
	measured.reader = capture
	sample := measured.measureCase(ctx, MeasurementPlan{}, MeasurementCase{Kind: "no-oracle", BlockHash: block, BlockNumber: number, Request: PreflightRequest{Family: PlainQuote, ExpectedRevision: engine.Revision().RevisionSHA256, BlockHash: block, Trader: trader, Recipient: trader, Pool: pool, AmountIn: "1000000", IsBuy: true}}, capture)
	if !sample.Passed || sample.Attestation == nil || sample.Publication == nil || sample.ElapsedNanoseconds <= 0 || sample.Issue != nil {
		t.Fatalf("actual measurement failed: %+v", sample)
	}
	if capture.sequence == 0 || capture.bytes != int64(raw.Len()) || capture.err != nil {
		t.Fatal("actual RPC observations missing")
	}
	observed := sha256.Sum256(raw.Bytes())
	if hex.EncodeToString(observed[:]) != hex.EncodeToString(digest.Sum(nil)) {
		t.Fatal("raw capture digest differs from written bytes")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw.Bytes()))
	var sequence uint64
	for {
		var record rawMeasurement
		err := decoder.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		sequence++
		if record.Sequence != sequence || record.Method == "" || record.Sample == nil || record.Sample.Index != 0 || record.Sample.Kind != "no-oracle" || record.StartedAt.IsZero() || record.ElapsedNanoseconds < 0 || record.StartedAt.After(time.Now()) || len(record.Result) == 0 || record.Issue != nil {
			t.Fatalf("actual raw observation malformed: %+v", record)
		}
	}
	if sequence != capture.sequence {
		t.Fatal("actual observations omitted")
	}
	var bounded bytes.Buffer
	limited := &measurementCapture{reader: engine.reader, writer: &bounded, hash: sha256.New(), maxBytes: 1}
	if _, err := limited.HeaderByHash(ctx, block); err == nil || limited.err == nil || limited.bytes != 0 || bounded.Len() != 0 {
		t.Fatal("raw byte budget did not fail closed")
	}
}
