package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/sei-protocol/seictl/sidecar/engine"
	seis3 "github.com/sei-protocol/seictl/sidecar/s3"
	"github.com/sei-protocol/seilog"
)

var artifactLog = seilog.NewLogger("seictl", "task", "upload-genesis-artifacts")

const artifactUploadMarkerFile = ".sei-sidecar-artifact-upload-done"

// UploadArtifactsRequest holds the typed parameters for the upload-genesis-artifacts task.
type UploadArtifactsRequest struct {
	NodeName string `json:"nodeName"`
}

// GenesisArtifactUploader uploads the gentx file and a node identity
// manifest to S3 so the assembler can collect them.
type GenesisArtifactUploader struct {
	homeDir           string
	bucket            string
	region            string
	chainID           string
	s3UploaderFactory seis3.UploaderFactory
}

// NewGenesisArtifactUploader creates an uploader targeting the given home directory.
// Bucket, region, and chainID are read from environment at construction time.
func NewGenesisArtifactUploader(homeDir, bucket, region, chainID string, factory seis3.UploaderFactory) *GenesisArtifactUploader {
	if factory == nil {
		factory = seis3.DefaultUploaderFactory
	}
	return &GenesisArtifactUploader{
		homeDir:           homeDir,
		bucket:            bucket,
		region:            region,
		chainID:           chainID,
		s3UploaderFactory: factory,
	}
}

// Handler returns an engine.TaskHandler for the upload-genesis-artifacts task type.
func (u *GenesisArtifactUploader) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, cfg UploadArtifactsRequest) error {
		if markerExists(u.homeDir, artifactUploadMarkerFile) {
			artifactLog.Debug("already completed, skipping")
			return nil
		}

		if cfg.NodeName == "" {
			return fmt.Errorf("upload-genesis-artifacts: missing required param 'nodeName'")
		}

		uploader, err := u.s3UploaderFactory(ctx, u.region)
		if err != nil {
			return fmt.Errorf("upload-genesis-artifacts: building S3 uploader: %w", err)
		}

		prefix := u.chainID + "/"
		nodePrefix := prefix + cfg.NodeName + "/"

		if err := u.uploadGentx(ctx, uploader, u.bucket, nodePrefix); err != nil {
			return err
		}

		if err := u.uploadIdentity(ctx, uploader, u.bucket, nodePrefix); err != nil {
			return err
		}

		artifactLog.Info("artifacts uploaded", "bucket", u.bucket, "prefix", nodePrefix)
		return writeMarker(u.homeDir, artifactUploadMarkerFile)
	})
}

// uploadGentx finds the single gentx file in config/gentx/ and uploads it.
func (u *GenesisArtifactUploader) uploadGentx(ctx context.Context, uploader seis3.Uploader, bucket, nodePrefix string) error {
	gentxDir := filepath.Join(u.homeDir, "config", "gentx")
	entries, err := os.ReadDir(gentxDir)
	if err != nil {
		return fmt.Errorf("upload-genesis-artifacts: reading gentx dir: %w", err)
	}

	var gentxFile string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			gentxFile = e.Name()
			break
		}
	}
	if gentxFile == "" {
		return fmt.Errorf("upload-genesis-artifacts: no gentx JSON file found in %s", gentxDir)
	}

	data, err := os.ReadFile(filepath.Join(gentxDir, gentxFile))
	if err != nil {
		return fmt.Errorf("upload-genesis-artifacts: reading %s: %w", gentxFile, err)
	}

	key := nodePrefix + "gentx.json"
	artifactLog.Info("uploading gentx", "key", key)
	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return seis3.ClassifyS3Error("upload-genesis-artifacts", bucket, key, u.region, err)
	}
	return nil
}

// uploadIdentity reads node_key.json, extracts the node ID, and uploads
// a minimal identity manifest. The assembler uses this to know which
// nodes participated.
func (u *GenesisArtifactUploader) uploadIdentity(ctx context.Context, uploader seis3.Uploader, bucket, nodePrefix string) error {
	nodeKeyPath := filepath.Join(u.homeDir, "config", "node_key.json")
	nodeKeyData, err := os.ReadFile(nodeKeyPath)
	if err != nil {
		return fmt.Errorf("upload-genesis-artifacts: reading node_key.json: %w", err)
	}

	identity := map[string]any{
		"node_key": json.RawMessage(nodeKeyData),
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return fmt.Errorf("upload-genesis-artifacts: marshaling identity: %w", err)
	}

	key := nodePrefix + "identity.json"
	artifactLog.Info("uploading identity", "key", key)
	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return seis3.ClassifyS3Error("upload-genesis-artifacts", bucket, key, u.region, err)
	}
	return nil
}
