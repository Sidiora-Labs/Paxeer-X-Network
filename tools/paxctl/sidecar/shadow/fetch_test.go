package shadow

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type mockDownloader struct {
	objects map[string][]byte
	err     error
}

func (m *mockDownloader) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := aws.ToString(input.Key)
	data, ok := m.objects[key]
	if !ok {
		return nil, &s3types.NoSuchKey{}
	}
	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func gzipJSON(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	if err := json.NewEncoder(gw).Encode(v); err != nil {
		t.Fatalf("encoding JSON: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("closing gzip: %v", err)
	}
	return buf.Bytes()
}

func rawJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("encoding JSON: %v", err)
	}
	return data
}

func TestFetchReport_GzippedReport(t *testing.T) {
	report := DivergenceReport{
		Height:    198032451,
		Timestamp: "2026-04-02T00:00:00Z",
		Comparison: CompareResult{
			Height: 198032451,
			Match:  false,
			Layer0: Layer0Result{
				AppHashMatch:         false,
				LastResultsHashMatch: true,
				GasUsedMatch:         true,
				ShadowAppHash:        "aaa",
				CanonicalAppHash:     "bbb",
			},
		},
	}

	dl := &mockDownloader{objects: map[string][]byte{
		"shadow-results/divergence-198032451.report.json.gz": gzipJSON(t, report),
	}}

	got, err := FetchReport(context.Background(), dl, "bucket", "shadow-results/divergence-198032451.report.json.gz")
	if err != nil {
		t.Fatalf("FetchReport: %v", err)
	}

	if got.Height != 198032451 {
		t.Errorf("Height = %d, want 198032451", got.Height)
	}
	if got.Timestamp != "2026-04-02T00:00:00Z" {
		t.Errorf("Timestamp = %q, want 2026-04-02T00:00:00Z", got.Timestamp)
	}
	if got.Comparison.Match {
		t.Error("expected divergent comparison")
	}
	if got.Comparison.Layer0.AppHashMatch {
		t.Error("expected AppHash mismatch")
	}
	if got.Comparison.Layer0.ShadowAppHash != "aaa" {
		t.Errorf("ShadowAppHash = %q, want aaa", got.Comparison.Layer0.ShadowAppHash)
	}
}

func TestFetchReport_UncompressedReport(t *testing.T) {
	report := DivergenceReport{
		Height:    100,
		Timestamp: "2026-04-02T00:00:00Z",
		Comparison: CompareResult{
			Height: 100,
			Match:  true,
			Layer0: Layer0Result{AppHashMatch: true, LastResultsHashMatch: true, GasUsedMatch: true},
		},
	}

	dl := &mockDownloader{objects: map[string][]byte{
		"reports/divergence-100.report.json": rawJSON(t, report),
	}}

	got, err := FetchReport(context.Background(), dl, "bucket", "reports/divergence-100.report.json")
	if err != nil {
		t.Fatalf("FetchReport: %v", err)
	}
	if got.Height != 100 {
		t.Errorf("Height = %d, want 100", got.Height)
	}
}

func TestFetchReport_S3NotFound(t *testing.T) {
	dl := &mockDownloader{objects: map[string][]byte{}}

	_, err := FetchReport(context.Background(), dl, "bucket", "nonexistent-key.json.gz")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestFetchReport_CorruptGzip(t *testing.T) {
	dl := &mockDownloader{objects: map[string][]byte{
		"corrupt.json.gz": {0x00, 0x01, 0x02, 0x03},
	}}

	_, err := FetchReport(context.Background(), dl, "bucket", "corrupt.json.gz")
	if err == nil {
		t.Fatal("expected error for corrupt gzip data")
	}
}

func TestFetchReport_InvalidJSON(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("not valid json"))
	gw.Close()

	dl := &mockDownloader{objects: map[string][]byte{
		"bad.json.gz": buf.Bytes(),
	}}

	_, err := FetchReport(context.Background(), dl, "bucket", "bad.json.gz")
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestFetchReport_WithLayer1Data(t *testing.T) {
	layer := 0
	report := DivergenceReport{
		Height:    42,
		Timestamp: "2026-04-02T00:00:00Z",
		Comparison: CompareResult{
			Height:          42,
			Match:           false,
			DivergenceLayer: &layer,
			Layer0: Layer0Result{
				AppHashMatch:         false,
				LastResultsHashMatch: true,
				GasUsedMatch:         true,
				ShadowAppHash:        "shadow",
				CanonicalAppHash:     "canonical",
			},
			Layer1: &Layer1Result{
				TotalTxs:     5,
				TxCountMatch: true,
				Divergences: []TxDivergence{
					{
						TxIndex: 2,
						Fields: []FieldDivergence{
							{Field: "code", Shadow: 0, Canonical: 1},
							{Field: "gasUsed", Shadow: "100000", Canonical: "120000"},
						},
					},
				},
			},
		},
	}

	dl := &mockDownloader{objects: map[string][]byte{
		"divergence-42.report.json.gz": gzipJSON(t, report),
	}}

	got, err := FetchReport(context.Background(), dl, "bucket", "divergence-42.report.json.gz")
	if err != nil {
		t.Fatalf("FetchReport: %v", err)
	}

	if got.Comparison.Layer1 == nil {
		t.Fatal("expected Layer1 data")
	}
	if got.Comparison.Layer1.TotalTxs != 5 {
		t.Errorf("TotalTxs = %d, want 5", got.Comparison.Layer1.TotalTxs)
	}
	if len(got.Comparison.Layer1.Divergences) != 1 {
		t.Fatalf("expected 1 tx divergence, got %d", len(got.Comparison.Layer1.Divergences))
	}
	div := got.Comparison.Layer1.Divergences[0]
	if div.TxIndex != 2 {
		t.Errorf("TxIndex = %d, want 2", div.TxIndex)
	}
	if len(div.Fields) != 2 {
		t.Errorf("expected 2 field divergences, got %d", len(div.Fields))
	}
}

func TestFetchReport_WithChainSnapshots(t *testing.T) {
	report := DivergenceReport{
		Height:     99,
		Timestamp:  "2026-04-02T00:00:00Z",
		Comparison: CompareResult{Height: 99, Match: false, Layer0: Layer0Result{AppHashMatch: false}},
		Shadow:     ChainSnapshot{Block: json.RawMessage(`{"shadow":"block"}`), BlockResults: json.RawMessage(`{"shadow":"results"}`)},
		Canonical:  ChainSnapshot{Block: json.RawMessage(`{"canonical":"block"}`), BlockResults: json.RawMessage(`{"canonical":"results"}`)},
	}

	dl := &mockDownloader{objects: map[string][]byte{
		"report.json.gz": gzipJSON(t, report),
	}}

	got, err := FetchReport(context.Background(), dl, "bucket", "report.json.gz")
	if err != nil {
		t.Fatalf("FetchReport: %v", err)
	}

	if len(got.Shadow.Block) == 0 {
		t.Error("expected non-empty Shadow.Block")
	}
	if len(got.Canonical.Block) == 0 {
		t.Error("expected non-empty Canonical.Block")
	}
}
