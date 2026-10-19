package tasks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/prometheus/client_golang/prometheus/testutil"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
)

type mockS3Uploader struct {
	uploads map[string][]byte
}

func newMockS3Uploader() *mockS3Uploader {
	return &mockS3Uploader{uploads: make(map[string][]byte)}
}

func (m *mockS3Uploader) UploadObject(_ context.Context, input *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	var buf bytes.Buffer
	if input.Body != nil {
		_, _ = io.Copy(&buf, input.Body)
	}
	key := *input.Bucket + "/" + *input.Key
	m.uploads[key] = buf.Bytes()
	return &transfermanager.UploadObjectOutput{}, nil
}

func mockUploaderFactory(client *mockS3Uploader) seis3.UploaderFactory {
	return func(_ context.Context, _ string) (seis3.Uploader, error) {
		return client, nil
	}
}

func setupSnapshotDirs(t *testing.T, homeDir string, heights []int64) {
	t.Helper()
	snapshotsDir := filepath.Join(homeDir, "data", "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0o755); err != nil {
		t.Fatalf("creating snapshots dir: %v", err)
	}

	for _, h := range heights {
		dir := filepath.Join(snapshotsDir, strconv.FormatInt(h, 10))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating height dir %d: %v", h, err)
		}
		chunkDir := filepath.Join(dir, "1")
		if err := os.MkdirAll(chunkDir, 0o755); err != nil {
			t.Fatalf("creating chunk dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(chunkDir, "0"), []byte("chunk-data"), 0o644); err != nil {
			t.Fatalf("writing chunk file: %v", err)
		}
	}

	// Create metadata.db
	metadataPath := filepath.Join(snapshotsDir, "metadata.db")
	if err := os.WriteFile(metadataPath, []byte("metadata-content"), 0o644); err != nil {
		t.Fatalf("writing metadata.db: %v", err)
	}
}

func TestPickUploadCandidate(t *testing.T) {
	cases := []struct {
		name    string
		heights []int64
		want    int64
	}{
		{"two snapshots returns second to latest", []int64{1000, 2000}, 1000},
		{"three snapshots returns second to latest", []int64{1000, 3000, 2000}, 2000},
		{"single snapshot returns zero", []int64{1000}, 0},
		{"no snapshots returns zero", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			homeDir := t.TempDir()
			if tc.heights != nil {
				setupSnapshotDirs(t, homeDir, tc.heights)
			}
			h, err := pickUploadCandidate(filepath.Join(homeDir, "data", "snapshots"))
			if err != nil {
				t.Fatalf("pickUploadCandidate() error = %v", err)
			}
			if h != tc.want {
				t.Errorf("height = %d, want %d", h, tc.want)
			}
		})
	}
}

func TestUpload_UploadsArchiveAndLatestTxt(t *testing.T) {
	homeDir := t.TempDir()
	setupSnapshotDirs(t, homeDir, []int64{1000, 2000})

	mock := newMockS3Uploader()
	uploader, err := NewSnapshotUploader(homeDir, "my-bucket", "eu-central-1", "testchain", 0, mockUploaderFactory(mock))
	if err != nil {
		t.Fatalf("NewSnapshotUploader: %v", err)
	}

	result, err := uploader.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Outcome != OutcomeUploaded {
		t.Errorf("outcome = %q, want %q", result.Outcome, OutcomeUploaded)
	}
	if result.NoopReason != "" {
		t.Errorf("NoopReason = %q, want empty on OutcomeUploaded", result.NoopReason)
	}
	if result.Height != 1000 {
		t.Errorf("result height = %d, want 1000", result.Height)
	}
	if result.Key != "testchain/state-sync/1000.tar.gz" {
		t.Errorf("result key = %q, want testchain/state-sync/1000.tar.gz", result.Key)
	}

	if _, ok := mock.uploads["my-bucket/testchain/state-sync/1000.tar.gz"]; !ok {
		t.Error("expected archive upload at testchain/state-sync/1000.tar.gz")
	}

	latest, ok := mock.uploads["my-bucket/testchain/state-sync/latest.txt"]
	if !ok {
		t.Fatal("expected latest.txt upload")
	}
	if string(latest) != "1000" {
		t.Errorf("latest.txt = %q, want %q", string(latest), "1000")
	}
}

func TestUpload_SkipsWhenAlreadyUploaded(t *testing.T) {
	homeDir := t.TempDir()
	setupSnapshotDirs(t, homeDir, []int64{1000, 2000})

	state := uploadState{LastUploadedHeight: 1000}
	data, _ := json.Marshal(state)
	_ = os.WriteFile(filepath.Join(homeDir, uploadStateFile), data, 0o644)

	mock := newMockS3Uploader()
	uploader, err := NewSnapshotUploader(homeDir, "my-bucket", "eu-central-1", "testchain", 0, mockUploaderFactory(mock))
	if err != nil {
		t.Fatalf("NewSnapshotUploader: %v", err)
	}

	result, err := uploader.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Outcome != OutcomeNoop || result.NoopReason != NoopAlreadyUploaded {
		t.Errorf("result = %+v, want noop/already-uploaded", result)
	}

	if len(mock.uploads) != 0 {
		t.Errorf("expected no uploads (already uploaded), got %d", len(mock.uploads))
	}
}

func TestUpload_UploadsNewerSnapshot(t *testing.T) {
	homeDir := t.TempDir()
	setupSnapshotDirs(t, homeDir, []int64{1000, 2000, 3000})

	state := uploadState{LastUploadedHeight: 1000}
	data, _ := json.Marshal(state)
	_ = os.WriteFile(filepath.Join(homeDir, uploadStateFile), data, 0o644)

	mock := newMockS3Uploader()
	uploader, err := NewSnapshotUploader(homeDir, "my-bucket", "eu-central-1", "testchain", 0, mockUploaderFactory(mock))
	if err != nil {
		t.Fatalf("NewSnapshotUploader: %v", err)
	}

	_, err = uploader.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if _, ok := mock.uploads["my-bucket/testchain/state-sync/2000.tar.gz"]; !ok {
		t.Error("expected archive upload at testchain/state-sync/2000.tar.gz")
	}
}

func TestUpload_NoOpsWhenTooFewSnapshots(t *testing.T) {
	homeDir := t.TempDir()
	setupSnapshotDirs(t, homeDir, []int64{1000})

	mock := newMockS3Uploader()
	uploader, err := NewSnapshotUploader(homeDir, "my-bucket", "eu-central-1", "testchain", 0, mockUploaderFactory(mock))
	if err != nil {
		t.Fatalf("NewSnapshotUploader: %v", err)
	}

	result, err := uploader.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if result.Outcome != OutcomeNoop || result.NoopReason != NoopFewerThanTwoSnapshots {
		t.Errorf("result = %+v, want noop/fewer-than-2-snapshots", result)
	}

	if len(mock.uploads) != 0 {
		t.Errorf("expected no uploads, got %d", len(mock.uploads))
	}
}

func TestUpload_WritesUploadState(t *testing.T) {
	homeDir := t.TempDir()
	setupSnapshotDirs(t, homeDir, []int64{1000, 2000})

	mock := newMockS3Uploader()
	uploader, err := NewSnapshotUploader(homeDir, "my-bucket", "eu-central-1", "testchain", 0, mockUploaderFactory(mock))
	if err != nil {
		t.Fatalf("NewSnapshotUploader: %v", err)
	}

	_, err = uploader.Upload(context.Background())
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	state := uploader.readUploadState()
	if state.LastUploadedHeight != 1000 {
		t.Errorf("LastUploadedHeight = %d, want 1000", state.LastUploadedHeight)
	}
	if state.LastUploadedAt == 0 {
		t.Error("expected LastUploadedAt to be persisted on upload")
	}
}

// readTarGzNames decompresses + reads a tar.gz produced by writeArchive
// and returns the entry names + their typeflags.
func readTarGzNames(t *testing.T, body []byte) map[string]byte {
	t.Helper()
	gzr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer func() { _ = gzr.Close() }()
	tr := tar.NewReader(gzr)
	out := map[string]byte{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		out[h.Name] = h.Typeflag
	}
	return out
}

// runWriteArchive captures writeArchive output via an io.Pipe.
func runWriteArchive(t *testing.T, snapshotsDir string, height int64) []byte {
	t.Helper()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- writeArchive(context.Background(), pw, snapshotsDir, height)
	}()
	body, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("writeArchive error: %v", err)
	}
	return body
}

func TestWriteArchive_MetadataAsDirectory(t *testing.T) {
	homeDir := t.TempDir()
	snapshotsDir := filepath.Join(homeDir, "data", "snapshots")
	if err := os.MkdirAll(filepath.Join(snapshotsDir, "1000", "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotsDir, "1000", "1", "0"), []byte("chunk"), 0o644); err != nil {
		t.Fatal(err)
	}
	// metadata.db as a directory containing typical LevelDB files
	mdDir := filepath.Join(snapshotsDir, "metadata.db")
	if err := os.MkdirAll(mdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mdDir, "CURRENT"), []byte("MANIFEST-000001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mdDir, "MANIFEST-000001"), []byte("manifest-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := runWriteArchive(t, snapshotsDir, 1000)
	names := readTarGzNames(t, body)

	// Height directory entries present and recursive
	if _, ok := names["1000/1/0"]; !ok {
		t.Errorf("missing height-dir file 1000/1/0 in archive; entries: %v", names)
	}
	// metadata.db directory header + recursive contents
	if tf, ok := names["metadata.db"]; !ok || tf != tar.TypeDir {
		t.Errorf("metadata.db missing or not a directory entry (typeflag=%d)", tf)
	}
	if _, ok := names["metadata.db/CURRENT"]; !ok {
		t.Errorf("missing metadata.db/CURRENT in archive; entries: %v", names)
	}
	if _, ok := names["metadata.db/MANIFEST-000001"]; !ok {
		t.Errorf("missing metadata.db/MANIFEST-000001 in archive; entries: %v", names)
	}
}

func TestWriteArchive_MetadataAsFile(t *testing.T) {
	homeDir := t.TempDir()
	snapshotsDir := filepath.Join(homeDir, "data", "snapshots")
	if err := os.MkdirAll(filepath.Join(snapshotsDir, "1000", "1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotsDir, "1000", "1", "0"), []byte("chunk"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(snapshotsDir, "metadata.db"), []byte("metadata-content"), 0o644); err != nil {
		t.Fatal(err)
	}

	body := runWriteArchive(t, snapshotsDir, 1000)
	names := readTarGzNames(t, body)

	if tf, ok := names["metadata.db"]; !ok || tf != tar.TypeReg {
		t.Errorf("metadata.db missing or not a regular file (typeflag=%d)", tf)
	}
	if _, ok := names["1000/1/0"]; !ok {
		t.Errorf("missing height-dir file 1000/1/0 in archive; entries: %v", names)
	}
}

// blockingUploader models a wedged S3 stream: UploadObject never returns until
// the context is cancelled, then surfaces the context error.
type blockingUploader struct{}

func (blockingUploader) UploadObject(ctx context.Context, _ *transfermanager.UploadObjectInput, _ ...func(*transfermanager.Options)) (*transfermanager.UploadObjectOutput, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func decodeUploadResult(t *testing.T, raw json.RawMessage) SnapshotUploadResult {
	t.Helper()
	var r SnapshotUploadResult
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("unmarshal result %q: %v", raw, err)
	}
	return r
}

func TestOnceHandler_ReturnsDistinguishableOutcomes(t *testing.T) {
	t.Run("uploaded", func(t *testing.T) {
		home := t.TempDir()
		setupSnapshotDirs(t, home, []int64{1000, 2000})
		uploader, err := NewSnapshotUploader(home, "b", "r", "once-uploaded", 0, mockUploaderFactory(newMockS3Uploader()))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := uploader.OnceHandler(time.Minute)(context.Background(), nil)
		if err != nil {
			t.Fatalf("handler error = %v", err)
		}
		got := decodeUploadResult(t, raw)
		if got.Outcome != OutcomeUploaded || got.Height != 1000 {
			t.Fatalf("result = %+v, want uploaded @1000", got)
		}
	})

	t.Run("noop fewer than 2 snapshots", func(t *testing.T) {
		home := t.TempDir()
		setupSnapshotDirs(t, home, []int64{1000})
		uploader, err := NewSnapshotUploader(home, "b", "r", "once-few", 0, mockUploaderFactory(newMockS3Uploader()))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := uploader.OnceHandler(time.Minute)(context.Background(), nil)
		if err != nil {
			t.Fatalf("handler error = %v", err)
		}
		got := decodeUploadResult(t, raw)
		if got.Outcome != OutcomeNoop || got.NoopReason != NoopFewerThanTwoSnapshots {
			t.Fatalf("result = %+v, want noop/fewer-than-2-snapshots", got)
		}
	})

	t.Run("noop already uploaded", func(t *testing.T) {
		home := t.TempDir()
		setupSnapshotDirs(t, home, []int64{1000, 2000})
		data, _ := json.Marshal(uploadState{LastUploadedHeight: 1000})
		if err := os.WriteFile(filepath.Join(home, uploadStateFile), data, 0o644); err != nil {
			t.Fatal(err)
		}
		uploader, err := NewSnapshotUploader(home, "b", "r", "once-already", 0, mockUploaderFactory(newMockS3Uploader()))
		if err != nil {
			t.Fatal(err)
		}
		raw, err := uploader.OnceHandler(time.Minute)(context.Background(), nil)
		if err != nil {
			t.Fatalf("handler error = %v", err)
		}
		got := decodeUploadResult(t, raw)
		if got.Outcome != OutcomeNoop || got.NoopReason != NoopAlreadyUploaded {
			t.Fatalf("result = %+v, want noop/already-uploaded", got)
		}
	})
}

// A handler-internal deadline must surface as context.DeadlineExceeded so the
// engine persists Failed rather than stranding the task in 'running'. Unlike a
// context.Canceled, a DeadlineExceeded is a genuine upload failure and must tick
// the error outcome counter — the recordError suppression keys only on Canceled.
func TestOnceHandler_DeadlineFailsCleanly(t *testing.T) {
	home := t.TempDir()
	setupSnapshotDirs(t, home, []int64{1000, 2000})
	chain := "once-deadline"
	factory := func(context.Context, string) (seis3.Uploader, error) { return blockingUploader{}, nil }
	uploader, err := NewSnapshotUploader(home, "b", "r", chain, 0, factory)
	if err != nil {
		t.Fatal(err)
	}

	_, err = uploader.OnceHandler(50*time.Millisecond)(context.Background(), nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("handler error = %v, want context.DeadlineExceeded", err)
	}
	if got := testutil.ToFloat64(snapshotUploadOutcomes.WithLabelValues(chain, string(OutcomeError))); got != 1 {
		t.Errorf("deadline-exceeded is a genuine failure: error outcome counter = %v, want 1", got)
	}
}

// A canceled context — an operator DELETE or a node drain mid-upload — must not
// tick the error counter. It mirrors the engine's cancellation-suppression
// contract: a canceled task leaves no terminal record, so a drain is not counted
// as an upload failure. The result still carries OutcomeError, but the engine
// suppresses it on the canceled path so its value there is inert.
func TestUpload_CanceledDoesNotTickErrorCounter(t *testing.T) {
	home := t.TempDir()
	setupSnapshotDirs(t, home, []int64{1000, 2000})
	chain := "cancel-no-tick"
	factory := func(context.Context, string) (seis3.Uploader, error) { return blockingUploader{}, nil }
	uploader, err := NewSnapshotUploader(home, "b", "r", chain, 0, factory)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	result, err := uploader.Upload(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Upload error = %v, want context.Canceled", err)
	}
	if result.Outcome != OutcomeError {
		t.Errorf("result outcome = %q, want %q", result.Outcome, OutcomeError)
	}
	if got := testutil.ToFloat64(snapshotUploadOutcomes.WithLabelValues(chain, string(OutcomeError))); got != 0 {
		t.Errorf("canceled upload must not tick error counter, got %v", got)
	}
	if got := testutil.ToFloat64(snapshotUploadLastRunSuccess.WithLabelValues(chain)); got != 0 {
		t.Errorf("canceled path must not refresh last run success, got %v", got)
	}
}

// The loop must swallow per-iteration errors, keep running, and stop cleanly on
// context cancellation.
func TestRunLoop_SwallowsErrorsAndStopsOnCancel(t *testing.T) {
	home := t.TempDir()
	setupSnapshotDirs(t, home, []int64{1000, 2000})
	var calls atomic.Int64
	factory := func(context.Context, string) (seis3.Uploader, error) {
		calls.Add(1)
		return nil, errors.New("s3 down")
	}
	uploader, err := NewSnapshotUploader(home, "b", "r", "loop-chain", 5*time.Millisecond, factory)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- uploader.runLoop(ctx) }()

	time.Sleep(60 * time.Millisecond) // let it iterate and swallow several failures
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runLoop returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not stop after cancel")
	}
	if calls.Load() < 2 {
		t.Fatalf("expected the loop to retry after swallowing errors, got %d attempts", calls.Load())
	}
}

// Concurrent writers exercise the temp-file + rename path: the persisted file
// must always parse to a complete written value, never a torn zero, and no temp
// files may be left behind.
func TestWriteUploadState_AtomicUnderConcurrentWriters(t *testing.T) {
	home := t.TempDir()
	uploader, err := NewSnapshotUploader(home, "b", "r", "atomic-chain", 0, mockUploaderFactory(newMockS3Uploader()))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := int64(1); i <= 50; i++ {
		wg.Add(1)
		go func(h int64) {
			defer wg.Done()
			if err := uploader.writeUploadState(uploadState{LastUploadedHeight: h, LastUploadedAt: h}); err != nil {
				t.Errorf("writeUploadState(%d): %v", h, err)
			}
		}(i)
	}
	wg.Wait()

	st := uploader.readUploadState()
	if st.LastUploadedHeight < 1 || st.LastUploadedHeight > 50 {
		t.Fatalf("torn/lost write: height=%d, want a complete value in 1..50", st.LastUploadedHeight)
	}
	if st.LastUploadedAt != st.LastUploadedHeight {
		t.Fatalf("torn write: height=%d but at=%d (a single writer paired them)", st.LastUploadedHeight, st.LastUploadedAt)
	}

	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file after atomic write: %s", e.Name())
		}
	}
}

// A pre-existing complete state survives a subsequent failed write attempt: the
// rename is all-or-nothing, so a torn temp never clobbers the live file.
func TestWriteUploadState_PreservesPriorStateOnRoundTrip(t *testing.T) {
	home := t.TempDir()
	uploader, err := NewSnapshotUploader(home, "b", "r", "roundtrip-chain", 0, mockUploaderFactory(newMockS3Uploader()))
	if err != nil {
		t.Fatal(err)
	}
	if err := uploader.writeUploadState(uploadState{LastUploadedHeight: 4242, LastUploadedAt: 99}); err != nil {
		t.Fatal(err)
	}

	// A stray temp file (as a crashed write would leave) must be ignored by the
	// reader, which keys only on the canonical filename.
	if err := os.WriteFile(filepath.Join(home, uploadStateFile+".tmp-garbage"), []byte("{ torn"), 0o644); err != nil {
		t.Fatal(err)
	}

	st := uploader.readUploadState()
	if st.LastUploadedHeight != 4242 || st.LastUploadedAt != 99 {
		t.Fatalf("state = %+v, want height=4242 at=99", st)
	}
}

func TestUpload_EmitsMetrics(t *testing.T) {
	t.Run("upload sets all gauges", func(t *testing.T) {
		home := t.TempDir()
		setupSnapshotDirs(t, home, []int64{1000, 2000})
		chain := "metrics-uploaded"
		uploader, err := NewSnapshotUploader(home, "b", "r", chain, 0, mockUploaderFactory(newMockS3Uploader()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := uploader.Upload(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := testutil.ToFloat64(snapshotUploadOutcomes.WithLabelValues(chain, string(OutcomeUploaded))); got != 1 {
			t.Errorf("uploaded outcome counter = %v, want 1", got)
		}
		if got := testutil.ToFloat64(snapshotUploadLastUploadedHeight.WithLabelValues(chain)); got != 1000 {
			t.Errorf("last uploaded height = %v, want 1000", got)
		}
		if testutil.ToFloat64(snapshotUploadLastUploaded.WithLabelValues(chain)) == 0 {
			t.Error("last uploaded timestamp not set on upload")
		}
		if testutil.ToFloat64(snapshotUploadLastRunSuccess.WithLabelValues(chain)) == 0 {
			t.Error("last run success not set on upload")
		}
	})

	t.Run("noop refreshes success but not uploaded gauges", func(t *testing.T) {
		home := t.TempDir()
		setupSnapshotDirs(t, home, []int64{1000}) // fewer than 2 -> noop
		chain := "metrics-noop"
		uploader, err := NewSnapshotUploader(home, "b", "r", chain, 0, mockUploaderFactory(newMockS3Uploader()))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := uploader.Upload(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := testutil.ToFloat64(snapshotUploadOutcomes.WithLabelValues(chain, string(OutcomeNoop))); got != 1 {
			t.Errorf("noop outcome counter = %v, want 1", got)
		}
		if testutil.ToFloat64(snapshotUploadLastRunSuccess.WithLabelValues(chain)) == 0 {
			t.Error("noop must refresh last run success (a not-yet-advanced chain is healthy)")
		}
		if got := testutil.ToFloat64(snapshotUploadLastUploadedHeight.WithLabelValues(chain)); got != 0 {
			t.Errorf("noop must not advance uploaded height gauge, got %v", got)
		}
	})
}

// A failed Upload must return OutcomeError (so a poller reading the persisted
// result can tell failure apart from an empty outcome) and increment the
// chain-labeled outcome counter, while leaving the clean-terminal gauges
// untouched so a real S3 outage cannot read as green.
func TestUpload_ErrorOutcomeOnFailure(t *testing.T) {
	home := t.TempDir()
	setupSnapshotDirs(t, home, []int64{1000, 2000})
	chain := "metrics-error"
	factory := func(context.Context, string) (seis3.Uploader, error) {
		return nil, errors.New("s3 down")
	}
	uploader, err := NewSnapshotUploader(home, "b", "r", chain, 0, factory)
	if err != nil {
		t.Fatal(err)
	}

	result, err := uploader.Upload(context.Background())
	if err == nil {
		t.Fatal("expected Upload to error when the S3 uploader cannot be built")
	}
	if result.Outcome != OutcomeError {
		t.Errorf("result outcome = %q, want %q", result.Outcome, OutcomeError)
	}
	if got := testutil.ToFloat64(snapshotUploadOutcomes.WithLabelValues(chain, string(OutcomeError))); got != 1 {
		t.Errorf("error outcome counter = %v, want 1", got)
	}
	if got := testutil.ToFloat64(snapshotUploadLastRunSuccess.WithLabelValues(chain)); got != 0 {
		t.Errorf("error path must not refresh last run success, got %v", got)
	}
	if got := testutil.ToFloat64(snapshotUploadLastUploadedHeight.WithLabelValues(chain)); got != 0 {
		t.Errorf("error path must not advance uploaded height gauge, got %v", got)
	}
}

func TestEmitStartupMetrics_RehydratesUploadedGauges(t *testing.T) {
	home := t.TempDir()
	chain := "startup-chain"
	uploader, err := NewSnapshotUploader(home, "b", "r", chain, 0, mockUploaderFactory(newMockS3Uploader()))
	if err != nil {
		t.Fatal(err)
	}
	if err := uploader.writeUploadState(uploadState{LastUploadedHeight: 7777, LastUploadedAt: 1234567890}); err != nil {
		t.Fatal(err)
	}

	uploader.EmitStartupMetrics()

	if got := testutil.ToFloat64(snapshotUploadLastUploadedHeight.WithLabelValues(chain)); got != 7777 {
		t.Errorf("startup height gauge = %v, want 7777", got)
	}
	if got := testutil.ToFloat64(snapshotUploadLastUploaded.WithLabelValues(chain)); got != 1234567890 {
		t.Errorf("startup uploaded timestamp = %v, want 1234567890", got)
	}
	// The alert signal must stay unset on startup so a genuinely stalled uploader
	// is not masked by a persisted success timestamp.
	if got := testutil.ToFloat64(snapshotUploadLastRunSuccess.WithLabelValues(chain)); got != 0 {
		t.Errorf("startup must not set last run success, got %v", got)
	}
}

func TestNormalizePrefix(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"state-sync", "state-sync/"},
		{"state-sync/", "state-sync/"},
		{"a/b/c", "a/b/c/"},
		{"a/b/c/", "a/b/c/"},
	}
	for _, tt := range tests {
		got := normalizePrefix(tt.input)
		if got != tt.want {
			t.Errorf("normalizePrefix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
