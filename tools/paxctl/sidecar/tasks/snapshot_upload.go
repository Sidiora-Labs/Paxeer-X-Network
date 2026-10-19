package tasks

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/sei-protocol/seictl/sidecar/engine"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seictl/sidecar/wire"
	"github.com/sei-protocol/seilog"
)

var uploadLog = seilog.NewLogger("seictl", "task", "snapshot-upload")

const (
	uploadStateFile       = ".sei-sidecar-last-upload.json"
	defaultUploadInterval = 7 * 24 * time.Hour // weekly

	// defaultUploadTimeout bounds a single one-shot upload. Uploads on large
	// chains legitimately run 60-90 min, so the default is generous; a wedged
	// S3 stream fails at the deadline rather than sitting 'running' forever.
	defaultUploadTimeout = 2 * time.Hour
)

// SnapshotUploadRequest holds the parameters for the snapshot upload task.
// S3 bucket, region, and prefix are derived from the sidecar's environment.
type SnapshotUploadRequest struct{}

// UploadOutcome and NoopReason are the snapshot-upload result-wire contract.
// They live in sidecar/wire (the dependency-free contract home) and are aliased
// here so handler call sites and the CLI poller reference one definition.
type (
	UploadOutcome = wire.UploadOutcome
	NoopReason    = wire.NoopReason
)

const (
	OutcomeUploaded = wire.OutcomeUploaded
	OutcomeNoop     = wire.OutcomeNoop
	OutcomeError    = wire.OutcomeError

	NoopFewerThanTwoSnapshots = wire.NoopFewerThanTwoSnapshots
	NoopAlreadyUploaded       = wire.NoopAlreadyUploaded
)

// SnapshotUploadResult is the structured result both handlers return through the
// engine so a one-shot poller can distinguish uploaded / noop / error: an error
// return carries Outcome=OutcomeError alongside the error string. On the loop
// path it is discarded; the engine persists it on TaskResult.Result for the
// one-shot path.
type SnapshotUploadResult struct {
	Outcome    UploadOutcome `json:"outcome"`
	NoopReason NoopReason    `json:"noopReason,omitempty"`
	Height     int64         `json:"height,omitempty"`
	Key        string        `json:"key,omitempty"`
}

// uploadState tracks the last successfully uploaded snapshot height and when it
// was uploaded. LastUploadedAt (unix seconds) is persisted so the uploaded
// gauges can be re-emitted on sidecar startup, avoiding a false-stale reading
// after the uploading pod restarts.
type uploadState struct {
	LastUploadedHeight int64 `json:"lastUploadedHeight"`
	LastUploadedAt     int64 `json:"lastUploadedAt,omitempty"`
}

// SnapshotUploader scans for locally produced Tendermint state-sync snapshots
// and uploads new ones to S3. When submitted as a task, it runs in a loop
// at the configured interval until the context is cancelled.
type SnapshotUploader struct {
	homeDir           string
	bucket            string
	region            string
	chainID           string
	uploadInterval    time.Duration
	s3UploaderFactory seis3.UploaderFactory
}

// NewSnapshotUploader creates an uploader targeting the given home directory.
// Bucket, region, and chainID are read from environment at construction time
// and rejected here if empty so the caller fails fast rather than entering
// runLoop and uploading nothing forever.
func NewSnapshotUploader(homeDir, bucket, region, chainID string, uploadInterval time.Duration, factory seis3.UploaderFactory) (*SnapshotUploader, error) {
	if bucket == "" || region == "" || chainID == "" {
		return nil, fmt.Errorf("snapshot-upload: bucket, region, and chainID are required")
	}
	if factory == nil {
		factory = seis3.DefaultUploaderFactory
	}
	if uploadInterval <= 0 {
		uploadInterval = defaultUploadInterval
	}
	return &SnapshotUploader{
		homeDir:           homeDir,
		bucket:            bucket,
		region:            region,
		chainID:           chainID,
		uploadInterval:    uploadInterval,
		s3UploaderFactory: factory,
	}, nil
}

// Handler returns an engine.TaskHandler for the snapshot-upload task.
// The handler runs in a loop, attempting an upload on each tick and
// sleeping for the configured interval between attempts. It stays
// running until the context is cancelled.
func (u *SnapshotUploader) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, _ SnapshotUploadRequest) error {
		return u.runLoop(ctx)
	})
}

// OnceHandler returns an engine.TaskHandler for the one-shot snapshot-upload
// task. It runs Upload exactly once and returns the structured result so the
// task reaches a real terminal (completed with an outcome, or failed with the
// error). The execution is bounded by a handler-internal deadline so a wedged
// S3 stream fails cleanly rather than stranding the task in 'running'. The
// deadline lives on a child context: it surfaces as context.DeadlineExceeded,
// which the engine persists as Failed (its cancellation-suppression guard keys
// only on context.Canceled).
func (u *SnapshotUploader) OnceHandler(timeout time.Duration) engine.TaskHandler {
	if timeout <= 0 {
		timeout = defaultUploadTimeout
	}
	return engine.TypedHandlerWithResult(func(ctx context.Context, _ SnapshotUploadRequest) (SnapshotUploadResult, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return u.Upload(ctx)
	})
}

func (u *SnapshotUploader) runLoop(ctx context.Context) error {
	uploadLog.Info("starting snapshot upload loop", "interval", u.uploadInterval, "bucket", u.bucket)
	for {
		if _, err := u.Upload(ctx); err != nil {
			uploadLog.Warn("upload attempt failed, will retry next interval", "error", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(u.uploadInterval):
		}
	}
}

// Upload finds the latest complete snapshot, archives it, and streams it to S3.
// It picks the second-to-latest snapshot height to avoid uploading an
// in-progress snapshot. If the snapshot has already been uploaded (tracked
// via a local state file), it no-ops.
//
// The archive is streamed through an io.Pipe so it never needs to be buffered
// entirely in memory; the transfermanager handles multipart upload automatically.
func (u *SnapshotUploader) Upload(ctx context.Context) (SnapshotUploadResult, error) {
	snapshotsDir := filepath.Join(u.homeDir, "data", "snapshots")

	height, err := pickUploadCandidate(snapshotsDir)
	if err != nil {
		return u.recordError(ctx), err
	}
	if height == 0 {
		uploadLog.Debug("fewer than 2 snapshots on disk, nothing to upload")
		return u.recordTerminal(SnapshotUploadResult{Outcome: OutcomeNoop, NoopReason: NoopFewerThanTwoSnapshots}), nil
	}

	last := u.readUploadState()
	if last.LastUploadedHeight >= height {
		uploadLog.Debug("height already uploaded", "height", height, "last-uploaded", last.LastUploadedHeight)
		return u.recordTerminal(SnapshotUploadResult{Outcome: OutcomeNoop, NoopReason: NoopAlreadyUploaded, Height: height}), nil
	}

	uploadLog.Info("uploading snapshot", "height", height, "bucket", u.bucket, "region", u.region)

	uploader, err := u.s3UploaderFactory(ctx, u.region)
	if err != nil {
		return u.recordError(ctx), fmt.Errorf("building S3 uploader: %w", err)
	}

	prefix := u.chainID + "/state-sync/"

	archiveKey := fmt.Sprintf("%s%d.tar.gz", prefix, height)
	uploadLog.Info("streaming archive to S3", "key", archiveKey)
	if err := u.streamUpload(ctx, uploader, u.bucket, archiveKey, snapshotsDir, height); err != nil {
		return u.recordError(ctx), fmt.Errorf("uploading %s: %w", archiveKey, err)
	}

	latestKey := prefix + "latest.txt"
	latestBody := []byte(strconv.FormatInt(height, 10))
	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(latestKey),
		Body:   bytes.NewReader(latestBody),
	})
	if err != nil {
		return u.recordError(ctx), fmt.Errorf("uploading %s: %w", latestKey, err)
	}
	uploadLog.Info("updated latest.txt", "key", latestKey, "height", height)

	if err := u.writeUploadState(uploadState{LastUploadedHeight: height, LastUploadedAt: time.Now().Unix()}); err != nil {
		return u.recordError(ctx), err
	}

	return u.recordTerminal(SnapshotUploadResult{Outcome: OutcomeUploaded, Height: height, Key: archiveKey}), nil
}

// recordTerminal emits the metrics for a clean terminal and returns the result
// unchanged so callers can `return u.recordTerminal(...), nil` in one line. Any
// clean terminal (uploaded or noop) refreshes the last-run-success gauge and the
// outcome counter; only a real upload advances the uploaded gauges.
func (u *SnapshotUploader) recordTerminal(result SnapshotUploadResult) SnapshotUploadResult {
	now := time.Now()
	snapshotUploadLastRunSuccess.WithLabelValues(u.chainID).Set(float64(now.Unix()))
	snapshotUploadOutcomes.WithLabelValues(u.chainID, string(result.Outcome)).Inc()
	if result.Outcome == OutcomeUploaded {
		snapshotUploadLastUploaded.WithLabelValues(u.chainID).Set(float64(now.Unix()))
		snapshotUploadLastUploadedHeight.WithLabelValues(u.chainID).Set(float64(result.Height))
	}
	return result
}

// recordError increments the chain-labeled outcome counter for a failed Upload
// and returns an error-tagged result so callers can `return u.recordError(ctx),
// err` in one line. It deliberately leaves last-run-success and the uploaded
// gauges untouched — those are clean-terminal-only signals — so a failing
// uploader is visible on the outcome counter without falsely refreshing health.
//
// A context.Canceled error does not tick the counter: it mirrors the engine's
// cancellation-suppression contract (a canceled task leaves no terminal record),
// so a deploy draining a node or an operator DELETE mid-upload is not counted as
// an upload failure. The predicate matches the engine's exactly — only
// context.Canceled is suppressed; a context.DeadlineExceeded (per-task timeout)
// or a genuine S3 failure still ticks.
func (u *SnapshotUploader) recordError(ctx context.Context) SnapshotUploadResult {
	if !errors.Is(ctx.Err(), context.Canceled) {
		snapshotUploadOutcomes.WithLabelValues(u.chainID, string(OutcomeError)).Inc()
	}
	return SnapshotUploadResult{Outcome: OutcomeError}
}

// EmitStartupMetrics re-emits the last-uploaded gauges from persisted state so a
// restarted sidecar does not report a false-stale reading before its first run.
// The last-run-success gauge is deliberately left unset: it is the "no clean run
// in N hours" alert signal, and re-emitting a persisted timestamp there would
// mask a genuinely stalled uploader after a restart.
func (u *SnapshotUploader) EmitStartupMetrics() {
	st := u.readUploadState()
	if st.LastUploadedHeight <= 0 {
		return
	}
	snapshotUploadLastUploadedHeight.WithLabelValues(u.chainID).Set(float64(st.LastUploadedHeight))
	if st.LastUploadedAt > 0 {
		snapshotUploadLastUploaded.WithLabelValues(u.chainID).Set(float64(st.LastUploadedAt))
	}
}

// streamUpload pipes a tar.gz archive directly into the transfermanager,
// avoiding in-memory buffering of the full archive.
func (u *SnapshotUploader) streamUpload(ctx context.Context, uploader seis3.Uploader, bucket, key, snapshotsDir string, height int64) error {
	pr, pw := io.Pipe()

	archiveErr := make(chan error, 1)
	go func() {
		archiveErr <- writeArchive(ctx, pw, snapshotsDir, height)
	}()

	_, uploadErr := uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        pr,
		ContentType: aws.String("application/gzip"),
	})

	if uploadErr != nil {
		// Unblock the archiver goroutine if it's still writing.
		pr.CloseWithError(uploadErr)
	}

	aErr := <-archiveErr
	if uploadErr != nil {
		return uploadErr
	}
	return aErr
}

// pickUploadCandidate scans the snapshots directory and returns the
// second-to-latest height. This avoids uploading an in-progress snapshot.
// Returns 0 if fewer than 2 snapshots exist.
func pickUploadCandidate(snapshotsDir string) (int64, error) {
	entries, err := os.ReadDir(snapshotsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading snapshots directory: %w", err)
	}

	var heights []int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		h, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue // skip non-numeric directories
		}
		heights = append(heights, h)
	}

	if len(heights) < 2 {
		return 0, nil
	}

	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })
	return heights[len(heights)-2], nil
}

// writeArchive streams a tar.gz archive of the snapshot at the given height
// into wc (typically the write half of an io.Pipe). It always closes wc when
// done, propagating any archiving error so the reader side sees it.
func writeArchive(ctx context.Context, wc io.WriteCloser, snapshotsDir string, height int64) (retErr error) {
	defer func() {
		if retErr != nil {
			wc.(*io.PipeWriter).CloseWithError(retErr)
		} else {
			_ = wc.Close()
		}
	}()

	gw := gzip.NewWriter(wc)
	tw := tar.NewWriter(gw)

	heightDir := filepath.Join(snapshotsDir, strconv.FormatInt(height, 10))
	if err := addDirToTar(ctx, tw, heightDir, strconv.FormatInt(height, 10)); err != nil {
		return err
	}

	// metadata.db has been a LevelDB directory in cosmos-sdk for several
	// versions, but the API allows it to be a single file too. Dispatch
	// on whichever we observe so a future revert doesn't break us either way.
	metadataPath := filepath.Join(snapshotsDir, "metadata.db")
	if info, err := os.Stat(metadataPath); err == nil {
		var addErr error
		if info.IsDir() {
			addErr = addDirToTar(ctx, tw, metadataPath, "metadata.db")
		} else {
			addErr = addFileToTar(ctx, tw, metadataPath, "metadata.db", info)
		}
		if addErr != nil {
			return fmt.Errorf("archiving metadata.db: %w", addErr)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("closing tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return fmt.Errorf("closing gzip writer: %w", err)
	}
	return nil
}

func addDirToTar(ctx context.Context, tw *tar.Writer, dir, base string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(filepath.Dir(dir), path)
		if err != nil {
			return err
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = rel

		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		_, err = io.Copy(tw, f)
		return err
	})
}

func addFileToTar(ctx context.Context, tw *tar.Writer, path, name string, info os.FileInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.Copy(tw, f)
	return err
}

func normalizePrefix(prefix string) string {
	if prefix == "" {
		return ""
	}
	if !strings.HasSuffix(prefix, "/") {
		return prefix + "/"
	}
	return prefix
}

func (u *SnapshotUploader) readUploadState() uploadState {
	data, err := os.ReadFile(filepath.Join(u.homeDir, uploadStateFile))
	if err != nil {
		return uploadState{}
	}
	var state uploadState
	if err := json.Unmarshal(data, &state); err != nil {
		return uploadState{}
	}
	return state
}

// writeUploadState persists state atomically: a temp file in the same directory
// is written, synced, and renamed over the target. A crash mid-write leaves the
// previous complete state intact rather than a torn file that readUploadState
// would parse as zero, silently re-uploading everything from height 0.
func (u *SnapshotUploader) writeUploadState(state uploadState) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshaling upload state: %w", err)
	}

	tmp, err := os.CreateTemp(u.homeDir, uploadStateFile+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp upload state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp upload state: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp upload state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temp upload state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp upload state: %w", err)
	}

	if err := os.Rename(tmpName, filepath.Join(u.homeDir, uploadStateFile)); err != nil {
		return fmt.Errorf("renaming upload state: %w", err)
	}
	return nil
}
