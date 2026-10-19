package tasks

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sei-protocol/seictl/sidecar/engine"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seilog"
)

var restoreLog = seilog.NewLogger("seictl", "task", "snapshot-restore")

const restoreMarkerFile = ".sei-sidecar-snapshot-done"

// SnapshotHeightFile records the snapshot height the node was restored from.
// The result-export task uses this to know where to start exporting.
const SnapshotHeightFile = ".sei-sidecar-snapshot-height"

// snapshotHeightRe extracts the block height from S3 snapshot keys of the form
// <chainID>/state-sync/<height>.tar.gz. The leading "/" anchor prevents the
// regex from picking up trailing digits embedded in other path segments.
var snapshotHeightRe = regexp.MustCompile(`/(\d+)\.tar\.gz$`)

// SnapshotRestoreRequest holds the typed parameters for the snapshot-restore task.
// S3 bucket, region, and chain prefix are derived from the sidecar's environment.
// TargetHeight, when set, selects the highest available snapshot <= that height.
// When zero, the latest snapshot (from latest.txt) is used.
type SnapshotRestoreRequest struct {
	TargetHeight int64 `json:"targetHeight,omitempty"`
}

// SnapshotRestorer downloads and extracts a snapshot archive from S3.
type SnapshotRestorer struct {
	homeDir       string
	bucket        string
	region        string
	chainID       string
	clientFactory seis3.TransferClientFactory
	listerFactory seis3.ObjectListerFactory
}

// NewSnapshotRestorer creates a restorer targeting the given home directory.
// Bucket, region, and chainID are read from environment at construction time.
func NewSnapshotRestorer(homeDir, bucket, region, chainID string, clientFactory seis3.TransferClientFactory, listerFactory seis3.ObjectListerFactory) (*SnapshotRestorer, error) {
	if bucket == "" || region == "" || chainID == "" {
		return nil, fmt.Errorf("snapshot-restore: bucket, region, and chainID are required")
	}
	if clientFactory == nil {
		clientFactory = seis3.DefaultTransferClientFactory
	}
	if listerFactory == nil {
		listerFactory = seis3.DefaultObjectListerFactory
	}
	return &SnapshotRestorer{
		homeDir:       homeDir,
		bucket:        bucket,
		region:        region,
		chainID:       chainID,
		clientFactory: clientFactory,
		listerFactory: listerFactory,
	}, nil
}

// Handler returns an engine.TaskHandler for the snapshot-restore task.
func (r *SnapshotRestorer) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, req SnapshotRestoreRequest) error {
		return r.Restore(ctx, req.TargetHeight)
	})
}

// Restore downloads and extracts the snapshot, skipping if the marker file exists.
// It lists objects under the chain's state-sync prefix and picks the highest
// snapshot height; when targetHeight > 0, the search is capped at that height.
func (r *SnapshotRestorer) Restore(ctx context.Context, targetHeight int64) error {
	if markerExists(r.homeDir, restoreMarkerFile) {
		restoreLog.Debug("already completed, skipping")
		return nil
	}

	if targetHeight < 0 {
		return fmt.Errorf("snapshot-restore: targetHeight must be >= 0, got %d", targetHeight)
	}

	prefix := r.chainID + "/state-sync/"

	client, err := r.clientFactory(ctx, r.region)
	if err != nil {
		return fmt.Errorf("building S3 transfer client: %w", err)
	}

	lister, err := r.listerFactory(ctx, r.region)
	if err != nil {
		return fmt.Errorf("building S3 lister: %w", err)
	}

	snapshotKey, err := resolveKeyForHeight(ctx, lister, r.bucket, prefix, r.region, targetHeight)
	if err != nil {
		return err
	}

	if snapshotKey == "" {
		return fmt.Errorf("snapshot-restore: resolved snapshot key is empty for %s in s3://%s/%s", r.chainID, r.bucket, prefix)
	}

	tmpDir := filepath.Join(r.homeDir, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}

	tmpFile, err := os.CreateTemp(tmpDir, "snapshot-*.tar.gz")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	restoreLog.Info("downloading snapshot", "bucket", r.bucket, "key", snapshotKey, "dest", tmpPath)
	_, err = client.DownloadObject(ctx, &transfermanager.DownloadObjectInput{
		Bucket:   aws.String(r.bucket),
		Key:      aws.String(snapshotKey),
		WriterAt: tmpFile,
	})
	_ = tmpFile.Close()
	if err != nil {
		return seis3.ClassifyS3Error("snapshot-restore", r.bucket, snapshotKey, r.region, err)
	}

	if h := parseHeightFromKey(snapshotKey); h > 0 {
		if err := os.WriteFile(
			filepath.Join(r.homeDir, SnapshotHeightFile),
			[]byte(strconv.FormatInt(h, 10)),
			0o644,
		); err != nil {
			restoreLog.Warn("failed to write snapshot height file", "err", err)
		}
	}

	destDir := filepath.Join(r.homeDir, "data", "snapshots")
	restoreLog.Info("extracting archive", "dest", destDir)
	if err := extractArchive(ctx, tmpPath, destDir); err != nil {
		return fmt.Errorf("extracting snapshot: %w", err)
	}

	restoreLog.Info("restore complete")
	return writeMarker(r.homeDir, restoreMarkerFile)
}

// resolveKeyForHeight lists snapshot objects under prefix and returns the key
// with the highest parsed height. When targetHeight > 0 it caps the search at
// that height; targetHeight == 0 picks the highest available snapshot.
func resolveKeyForHeight(ctx context.Context, lister seis3.ObjectLister, bucket, prefix, region string, targetHeight int64) (string, error) {
	var bestHeight int64
	var bestKey string

	var continuationToken *string
	for {
		output, err := lister.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return "", seis3.ClassifyS3Error("snapshot-restore", bucket, prefix, region, err)
		}

		for _, obj := range output.Contents {
			if obj.Key == nil {
				continue
			}
			h := parseHeightFromKey(*obj.Key)
			if h <= 0 {
				continue
			}
			if targetHeight > 0 && h > targetHeight {
				continue
			}
			if h > bestHeight {
				bestHeight = h
				bestKey = *obj.Key
			}
		}

		if !aws.ToBool(output.IsTruncated) {
			break
		}
		continuationToken = output.NextContinuationToken
	}

	if bestKey == "" {
		if targetHeight > 0 {
			return "", fmt.Errorf("no snapshot found at or below height %d in s3://%s/%s", targetHeight, bucket, prefix)
		}
		return "", fmt.Errorf("no snapshots found in s3://%s/%s", bucket, prefix)
	}

	restoreLog.Info("resolved snapshot",
		"targetHeight", targetHeight, "snapshotHeight", bestHeight, "key", bestKey)
	return bestKey, nil
}

func parseHeightFromKey(key string) int64 {
	m := snapshotHeightRe.FindStringSubmatch(key)
	if len(m) < 2 {
		return 0
	}
	h, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0
	}
	return h
}

// extractArchive opens a .tar.gz file and extracts it to destDir.
func extractArchive(ctx context.Context, archivePath, destDir string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("opening archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	return extractTarStream(ctx, f, destDir)
}

func extractTarStream(ctx context.Context, r io.Reader, destDir string) error {
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer func() { _ = gzr.Close() }()
	tr := tar.NewReader(gzr)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading tar header: %w", err)
		}

		target := filepath.Join(destDir, filepath.Clean(header.Name))

		if !isInsideDir(target, destDir) {
			return fmt.Errorf("tar entry %q escapes destination directory", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)|0o700); err != nil {
				return fmt.Errorf("creating directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := extractFile(tr, target, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			linkTarget := filepath.Join(filepath.Dir(target), header.Linkname)
			if !isInsideDir(linkTarget, destDir) {
				return fmt.Errorf("symlink %q points outside destination directory", header.Name)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("creating symlink %s: %w", target, err)
			}
		}
	}
}

func extractFile(r io.Reader, path string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating parent directory for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o600)
	if err != nil {
		return fmt.Errorf("creating file %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("writing file %s: %w", path, err)
	}
	return nil
}

// isInsideDir checks that target is within or equal to baseDir.
func isInsideDir(target, baseDir string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}
	if absTarget == absBase {
		return true
	}
	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return false
	}
	return len(rel) > 0 && rel[0] != '.'
}

func markerExists(homeDir, name string) bool {
	_, err := os.Stat(filepath.Join(homeDir, name))
	return err == nil
}

func writeMarker(homeDir, name string) error {
	path := filepath.Join(homeDir, name)
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("writing marker file %s: %w", path, err)
	}
	return f.Close()
}
