package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/rpc"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seictl/sidecar/shadow"
)

type mockResultUploader struct{}

func (m *mockResultUploader) UploadObject(_ context.Context, in *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	if in.Body != nil {
		_, _ = io.Copy(io.Discard, in.Body)
	}
	return &transfermanager.UploadObjectOutput{}, nil
}

func mockResultUploaderFactory() seis3.UploaderFactory {
	return func(_ context.Context, _ string) (seis3.Uploader, error) {
		return &mockResultUploader{}, nil
	}
}

func failingUploaderFactory(errMsg string) seis3.UploaderFactory {
	return func(_ context.Context, _ string) (seis3.Uploader, error) {
		return nil, fmt.Errorf("%s", errMsg)
	}
}

// drainingFailingUploader drains the streamed body (so the writer never blocks)
// then fails — an S3 error after reading a valid stream.
type drainingFailingUploader struct{ err error }

func (u drainingFailingUploader) UploadObject(_ context.Context, in *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	if in.Body != nil {
		_, _ = io.Copy(io.Discard, in.Body)
	}
	return nil, u.err
}

// TestExportPage_ProducerErrorNotClassifiedAsS3 proves a block_results RPC
// failure is returned as-is, not mislabeled as an S3 access problem.
func TestExportPage_ProducerErrorNotClassifiedAsS3(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/block_results" {
			http.Error(w, "block results unavailable", http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":-1,"result":{"sync_info":{"latest_block_height":"100"}}}`)
	}))
	defer srv.Close()

	e := NewResultExporter(t.TempDir(), "test-1", "pod-0", mockResultUploaderFactory())
	err := e.exportPage(context.Background(), rpc.NewClient(srv.URL, nil), &mockResultUploader{}, "bkt", "us-east-1", "p/", 100, 100)
	if err == nil {
		t.Fatal("expected a producer error")
	}
	var te *engine.TaskError
	if errors.As(err, &te) && te.Operation == "S3" {
		t.Fatalf("producer RPC error misclassified as S3: %v", err)
	}
	if !strings.Contains(err.Error(), "block_results") {
		t.Fatalf("expected the block_results producer error, got: %v", err)
	}
}

// TestExportPage_UploadErrorKeepsS3Classification proves a genuine upload
// failure retains its ClassifyS3Error treatment (Operation S3), not lost behind
// a wrapped sink-write error.
func TestExportPage_UploadErrorKeepsS3Classification(t *testing.T) {
	srv := fakeRPCServer(100)
	defer srv.Close()

	up := drainingFailingUploader{err: errors.New("connection reset by peer")}
	e := NewResultExporter(t.TempDir(), "test-1", "pod-0", mockResultUploaderFactory())
	err := e.exportPage(context.Background(), rpc.NewClient(srv.URL, nil), up, "bkt", "us-east-1", "p/", 100, 100)
	if err == nil {
		t.Fatal("expected an upload error")
	}
	var te *engine.TaskError
	if !errors.As(err, &te) || te.Operation != "S3" {
		t.Fatalf("upload failure should be S3-classified, got: %v", err)
	}
}

// fakeRPCServer returns an httptest.Server that responds to /status and /block_results.
func fakeRPCServer(latestHeight int64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/status":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":-1,"result":{"sync_info":{"latest_block_height":"%d"}}}`, latestHeight)
		case r.URL.Path == "/block_results":
			fmt.Fprint(w, `{"jsonrpc":"2.0","id":-1,"result":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestExportBootstrapFromSnapshotHeight(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, SnapshotHeightFile), []byte("198030000"), 0o644); err != nil {
		t.Fatalf("writing snapshot height file: %v", err)
	}

	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", nil)
	state := e.readExportState()

	if state.LastExportedHeight != 198030000 {
		t.Errorf("LastExportedHeight = %d, want 198030000", state.LastExportedHeight)
	}
}

func TestExportBootstrapNoFiles(t *testing.T) {
	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", nil)
	state := e.readExportState()

	if state.LastExportedHeight != 0 {
		t.Errorf("LastExportedHeight = %d, want 0", state.LastExportedHeight)
	}
}

func TestExportBootstrapPreferStateFile(t *testing.T) {
	tmpDir := t.TempDir()

	stateData, _ := json.Marshal(exportState{LastExportedHeight: 200000000})
	if err := os.WriteFile(filepath.Join(tmpDir, exportStateFile), stateData, 0o644); err != nil {
		t.Fatalf("writing state file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, SnapshotHeightFile), []byte("198030000"), 0o644); err != nil {
		t.Fatalf("writing snapshot height file: %v", err)
	}

	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", nil)
	state := e.readExportState()

	if state.LastExportedHeight != 200000000 {
		t.Errorf("LastExportedHeight = %d, want 200000000", state.LastExportedHeight)
	}
}

func TestExportRPCUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	srv.Close()

	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())

	err := e.Export(context.Background(), ResultExportRequest{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		RPCEndpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("Export() returned error %v, want nil (fail-safe)", err)
	}
}

func TestExportRPCNon200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "node is syncing")
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())

	err := e.Export(context.Background(), ResultExportRequest{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		RPCEndpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("Export() returned error %v, want nil (fail-safe on HTTP error)", err)
	}
}

func TestQueryLatestHeight_ZeroHeight(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"jsonrpc":"2.0","id":-1,"result":{"sync_info":{"latest_block_height":"0"}}}`)
	}))
	defer srv.Close()

	_, err := queryLatestHeight(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for zero height, got nil")
	}
}

func TestExportS3UploaderFactoryError(t *testing.T) {
	srv := fakeRPCServer(100000)
	defer srv.Close()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, SnapshotHeightFile), []byte("1"), 0o644); err != nil {
		t.Fatalf("writing snapshot height file: %v", err)
	}

	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", failingUploaderFactory("simulated AWS error"))

	err := e.Export(context.Background(), ResultExportRequest{
		Bucket:      "test-bucket",
		Region:      "us-east-1",
		RPCEndpoint: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error when S3 uploader factory fails")
	}
	if !strings.Contains(err.Error(), "simulated AWS error") {
		t.Errorf("expected simulated AWS error, got: %v", err)
	}
}

func TestExportWritesStateAfterPage(t *testing.T) {
	// Latest height 1001 with start at 1 gives exactly 1001 available blocks,
	// which is 1 full page (heights 1–1000). The remaining 1 block is deferred.
	srv := fakeRPCServer(1001)
	defer srv.Close()

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, SnapshotHeightFile), []byte("0"), 0o644); err != nil {
		t.Fatalf("writing snapshot height file: %v", err)
	}

	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())
	err := e.Export(context.Background(), ResultExportRequest{
		Bucket:      "test-bucket",
		Prefix:      "results",
		Region:      "us-east-1",
		RPCEndpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	state := e.readExportState()
	if state.LastExportedHeight != 1000 {
		t.Errorf("LastExportedHeight = %d, want 1000", state.LastExportedHeight)
	}
}

func TestExportHandler_MissingParams(t *testing.T) {
	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())
	handler := e.Handler()

	cases := []struct {
		name   string
		params map[string]any
	}{
		{"missing bucket", map[string]any{"region": "us-east-1"}},
		{"missing region", map[string]any{"bucket": "my-bucket"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := handler(context.Background(), tc.params)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

func TestExportConfigJSONRoundTrip(t *testing.T) {
	cfg := ResultExportRequest{
		Bucket:       "my-bucket",
		Region:       "us-east-1",
		RPCEndpoint:  "http://custom:26657",
		CanonicalRPC: "http://canonical:26657",
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	var decoded ResultExportRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshaling: %v", err)
	}
	if decoded.Bucket != cfg.Bucket {
		t.Errorf("Bucket = %q, want %q", decoded.Bucket, cfg.Bucket)
	}
	if decoded.Region != cfg.Region {
		t.Errorf("Region = %q, want %q", decoded.Region, cfg.Region)
	}
	if decoded.RPCEndpoint != cfg.RPCEndpoint {
		t.Errorf("RPCEndpoint = %q, want %q", decoded.RPCEndpoint, cfg.RPCEndpoint)
	}
	if decoded.CanonicalRPC != cfg.CanonicalRPC {
		t.Errorf("CanonicalRPC = %q, want %q", decoded.CanonicalRPC, cfg.CanonicalRPC)
	}
}

// --- Handler routing tests ---

func TestHandlerRouting_WithCanonicalRPC_CallsExportAndCompare(t *testing.T) {
	srv := fakeRPCAndBlockServer(1, "AABB", "CCDD", nil)
	defer srv.Close()

	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := e.Handler()(ctx, map[string]any{
		"bucket":       "test-bucket",
		"region":       "us-east-1",
		"rpcEndpoint":  srv.URL,
		"canonicalRpc": srv.URL,
	})
	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}
}

func TestHandlerRouting_WithoutCanonicalRPC_CallsExport(t *testing.T) {
	srv := fakeRPCServer(0) // 0 blocks → nothing to export
	defer srv.Close()

	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())

	_, err := e.Handler()(context.Background(), map[string]any{
		"bucket":      "test-bucket",
		"region":      "us-east-1",
		"rpcEndpoint": srv.URL,
	})
	if err != nil {
		t.Fatalf("Handler() error = %v, want nil for empty export", err)
	}
}

// --- ExportAndCompare tests ---

func TestExportAndCompare_DivergenceDetected(t *testing.T) {
	// Shadow returns different app hash than canonical → immediate divergence.
	shadowSrv := fakeRPCAndBlockServer(5, "SHADOW_HASH", "RESULTS", nil)
	defer shadowSrv.Close()
	canonicalSrv := fakeRPCAndBlockServer(5, "CANONICAL_HASH", "RESULTS", nil)
	defer canonicalSrv.Close()

	tmpDir := t.TempDir()
	const testPodName = "shadow-test-0"
	e := NewResultExporter(tmpDir, "test-1", testPodName, mockResultUploaderFactory())

	divergenceBefore := testutil.ToFloat64(shadow.Divergences.WithLabelValues("test-1", testPodName, "0"))

	err := e.ExportAndCompare(context.Background(), ResultExportRequest{
		Bucket:       "test-bucket",
		Prefix:       "compare/",
		Region:       "us-east-1",
		RPCEndpoint:  shadowSrv.URL,
		CanonicalRPC: canonicalSrv.URL,
	})
	// Divergence detected = task completes successfully (nil error).
	if err != nil {
		t.Fatalf("ExportAndCompare() error = %v, want nil (divergence = success)", err)
	}

	state := e.readExportState()
	if state.LastExportedHeight != 1 {
		t.Errorf("LastExportedHeight = %d, want 1 (diverged at first block)", state.LastExportedHeight)
	}

	if got := testutil.ToFloat64(shadow.Divergences.WithLabelValues("test-1", testPodName, "0")); got-divergenceBefore != 1 {
		t.Errorf("seictl_shadow_divergences_total{chain_id=test-1,pod_name=%s,divergence_layer=0} delta = %v, want 1", testPodName, got-divergenceBefore)
	}
	if got := testutil.ToFloat64(shadow.BlocksCompared.WithLabelValues("test-1", testPodName)); got < 1 {
		t.Errorf("seictl_shadow_blocks_compared_total{chain_id=test-1,pod_name=%s} = %v, want >= 1", testPodName, got)
	}
}

func TestExportAndCompare_ContextCancelled(t *testing.T) {
	srv := fakeRPCAndBlockServer(1, "SAME", "SAME", nil)
	defer srv.Close()

	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := e.ExportAndCompare(ctx, ResultExportRequest{
		Bucket:       "test-bucket",
		Region:       "us-east-1",
		RPCEndpoint:  srv.URL,
		CanonicalRPC: srv.URL,
	})
	if err == nil {
		t.Fatal("expected context deadline exceeded error")
	}
}

func TestExportAndCompare_S3UploaderError(t *testing.T) {
	srv := fakeRPCAndBlockServer(5, "AA", "BB", nil)
	defer srv.Close()

	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", failingUploaderFactory("AWS creds expired"))

	err := e.ExportAndCompare(context.Background(), ResultExportRequest{
		Bucket:       "test-bucket",
		Region:       "us-east-1",
		RPCEndpoint:  srv.URL,
		CanonicalRPC: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error from S3 uploader factory failure")
	}
}

func TestExportAndCompare_ResumesFromExportState(t *testing.T) {
	// Set export state to height 3, shadow serves up to 5.
	// With matching hashes, it compares blocks 4 and 5 (no divergence).
	// Set up divergence at block 4 by using different servers.
	shadowSrv := fakeRPCAndBlockServer(5, "SHADOW", "RES", nil)
	defer shadowSrv.Close()
	canonicalSrv := fakeRPCAndBlockServer(5, "CANONICAL", "RES", nil)
	defer canonicalSrv.Close()

	tmpDir := t.TempDir()
	stateData, _ := json.Marshal(exportState{LastExportedHeight: 3})
	if err := os.WriteFile(filepath.Join(tmpDir, exportStateFile), stateData, 0o644); err != nil {
		t.Fatalf("writing state: %v", err)
	}

	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", mockResultUploaderFactory())
	err := e.ExportAndCompare(context.Background(), ResultExportRequest{
		Bucket:       "test-bucket",
		Region:       "us-east-1",
		RPCEndpoint:  shadowSrv.URL,
		CanonicalRPC: canonicalSrv.URL,
	})
	if err != nil {
		t.Fatalf("ExportAndCompare() error = %v", err)
	}

	state := e.readExportState()
	if state.LastExportedHeight != 4 {
		t.Errorf("LastExportedHeight = %d, want 4 (first block after resume)", state.LastExportedHeight)
	}
}

// --- Divergence report tests ---

func TestExportAndCompare_UploadsDivergenceReport(t *testing.T) {
	shadowSrv := fakeRPCAndBlockServer(5, "SHADOW", "RESULTS", nil)
	defer shadowSrv.Close()
	canonicalSrv := fakeRPCAndBlockServer(5, "CANONICAL", "RESULTS", nil)
	defer canonicalSrv.Close()

	recorder := &recordingUploader{}
	tmpDir := t.TempDir()
	e := NewResultExporter(tmpDir, "test-1", "test-pod-0", func(_ context.Context, _ string) (seis3.Uploader, error) {
		return recorder, nil
	})

	err := e.ExportAndCompare(context.Background(), ResultExportRequest{
		Bucket:       "test-bucket",
		Prefix:       "shadow/pacific-1/",
		Region:       "us-east-1",
		RPCEndpoint:  shadowSrv.URL,
		CanonicalRPC: canonicalSrv.URL,
	})
	if err != nil {
		t.Fatalf("ExportAndCompare() error = %v", err)
	}

	var reportKey string
	var compareKey string
	for _, key := range recorder.keys {
		if strings.Contains(key, "divergence-") && strings.Contains(key, ".report.json.gz") {
			reportKey = key
		}
		if strings.Contains(key, ".compare.ndjson.gz") {
			compareKey = key
		}
	}

	if reportKey == "" {
		t.Errorf("expected divergence report upload, got keys: %v", recorder.keys)
	}
	if compareKey == "" {
		t.Errorf("expected comparison page upload, got keys: %v", recorder.keys)
	}

	if reportKey != "" && reportKey != "shadow/pacific-1/divergence-1.report.json.gz" {
		t.Errorf("report key = %q, want %q", reportKey, "shadow/pacific-1/divergence-1.report.json.gz")
	}
}

// --- flushComparePage tests ---

func TestFlushComparePage_EmptyResults(t *testing.T) {
	err := flushComparePage(context.Background(), &mockResultUploader{}, "bucket", "prefix/", nil)
	if err != nil {
		t.Errorf("flushComparePage(nil) error = %v, want nil", err)
	}
}

// --- Test helpers ---

type recordingUploader struct {
	keys []string
}

func (r *recordingUploader) UploadObject(_ context.Context, in *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	if in.Key != nil {
		r.keys = append(r.keys, *in.Key)
	}
	if in.Body != nil {
		_, _ = io.Copy(io.Discard, in.Body)
	}
	return &transfermanager.UploadObjectOutput{}, nil
}

// fakeRPCAndBlockServer responds to /status, /block, and /block_results.
func fakeRPCAndBlockServer(latestHeight int64, appHash, lastResultsHash string, txResults []json.RawMessage) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/status":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":-1,"result":{"sync_info":{"latest_block_height":"%d"}}}`, latestHeight)
		case r.URL.Path == "/block":
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
			json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/block_results":
			resp := map[string]any{
				"jsonrpc": "2.0",
				"id":      -1,
				"result": map[string]any{
					"txs_results": txResults,
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
}
