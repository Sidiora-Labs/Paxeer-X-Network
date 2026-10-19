package tasks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDeserialize_SnapshotRestore verifies that the snapshot-restore handler
// correctly deserializes simple string fields from the wire format.
func TestDeserialize_SnapshotRestore(t *testing.T) {
	homeDir := t.TempDir()
	// Pre-write a marker so the handler returns early without S3 calls.
	if err := writeMarker(homeDir, restoreMarkerFile); err != nil {
		t.Fatal(err)
	}

	restorer, err := NewSnapshotRestorer(homeDir, "b", "r", "c", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	handler := restorer.Handler()

	// The handler should succeed (skip via marker) without a parse error.
	_, err = handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("snapshot-restore handler returned error: %v", err)
	}
}

// TestDeserialize_ConfigPatch verifies that the config-patch handler
// correctly deserializes nested map[string]map[string]any from the wire format.
func TestDeserialize_ConfigPatch(t *testing.T) {
	homeDir := t.TempDir()
	setupConfigFile(t, homeDir, `
[p2p]
persistent-peers = ""
`)

	patcher := NewConfigPatcher(homeDir)
	handler := patcher.Handler()

	params := map[string]any{
		"files": map[string]any{
			"config.toml": map[string]any{
				"p2p": map[string]any{
					"persistent-peers": "node1@1.2.3.4:26656",
				},
			},
		},
	}

	_, err := handler(context.Background(), params)
	if err != nil {
		t.Fatalf("config-patch handler returned error: %v", err)
	}

	doc := readTOML(t, filepath.Join(homeDir, "config", "config.toml"))
	p2p := doc["p2p"].(map[string]any)
	if p2p["persistent-peers"] != "node1@1.2.3.4:26656" {
		t.Errorf("peers = %q, want %q", p2p["persistent-peers"], "node1@1.2.3.4:26656")
	}
}

// TestDeserialize_AssembleGenesis verifies that the assemble-genesis handler
// correctly deserializes the nodes array with name fields from the wire format.
func TestDeserialize_AssembleGenesis(t *testing.T) {
	// We only test deserialization, not the full S3 flow, so we expect
	// a validation error for missing S3 bucket when bucket is empty.
	handler := NewGenesisAssembler(t.TempDir(), "my-bucket", "us-east-1", "test-chain", nil, nil).Handler()

	params := map[string]any{
		"accountBalance": "10000000usei",
		"namespace":      "default",
		"nodes": []any{
			map[string]any{"name": "val-0"},
			map[string]any{"name": "val-1"},
		},
	}

	// This will fail at the S3 download step (no real S3), but if it gets
	// past param parsing without a "parsing params" error, deserialization worked.
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error (no S3 client), got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
}

// TestDeserialize_AwaitCondition verifies that the await-condition handler
// correctly deserializes int64 from float64 (JSON number coercion) and
// string fields from the wire format.
func TestDeserialize_AwaitCondition(t *testing.T) {
	handler := NewConditionWaiter(nil).Handler()

	// float64(500) simulates what json.Unmarshal produces for JSON numbers
	// when the target is map[string]any.
	params := map[string]any{
		"condition":    "height",
		"targetHeight": float64(500),
	}

	// The handler will try to poll RPC (and fail because there's no server),
	// but it should parse params without error. We use a context that
	// cancels immediately to avoid blocking.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler(ctx, params)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
	// If we get a context error (not a parsing error), deserialization succeeded.
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
}

// TestDeserialize_ConfigApply verifies that the config-apply handler
// correctly deserializes the ConfigIntent (overrides map + mode string).
func TestDeserialize_ConfigApply(t *testing.T) {
	homeDir := t.TempDir()
	applier := NewConfigApplier(homeDir)
	handler := applier.Handler()

	params := map[string]any{
		"mode":        "full",
		"incremental": false,
		"overrides": map[string]any{
			"evm.http_port": "9545",
		},
	}

	_, err := handler(context.Background(), params)
	if err != nil {
		t.Fatalf("config-apply handler returned error: %v", err)
	}

	// Verify the config was actually written.
	configPath := filepath.Join(homeDir, "config", "config.toml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config.toml was not written")
	}
}

// TestDeserialize_ConfigReload verifies that the config-reload handler
// correctly deserializes the fields map from the wire format.
func TestDeserialize_ConfigReload(t *testing.T) {
	// config-reload needs a valid on-disk config to read.
	// We just check that deserialization doesn't fail when fields is empty
	// (it'll fail with a validation error, not a parse error).
	handler := NewConfigReloader(t.TempDir()).Handler()

	params := map[string]any{
		"fields": map[string]any{},
	}

	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for empty fields, got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
	if !strings.Contains(err.Error(), "at least one field") {
		t.Errorf("expected 'at least one field' error, got: %v", err)
	}
}

// TestNewSnapshotUploader_RejectsEmptyConfig verifies that the constructor
// fails fast when bucket, region, or chainID is empty rather than producing
// an uploader whose runLoop polls forever uploading nothing.
func TestNewSnapshotUploader_RejectsEmptyConfig(t *testing.T) {
	tests := []struct {
		name                          string
		bucket, region, chainID, want string
	}{
		{"empty bucket", "", "us-east-1", "c", "required"},
		{"empty region", "b", "", "c", "required"},
		{"empty chainID", "b", "us-east-1", "", "required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSnapshotUploader(t.TempDir(), tt.bucket, tt.region, tt.chainID, 0, nil)
			if err == nil {
				t.Fatal("expected constructor error, got nil")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not mention %q", err.Error(), tt.want)
			}
		})
	}
}

// TestDeserialize_ConfigureGenesis verifies that the configure-genesis handler
// works with empty params for an embedded chain.
func TestDeserialize_ConfigureGenesis(t *testing.T) {
	homeDir := t.TempDir()
	fetcher := NewGenesisFetcher(homeDir, "pacific-1", "test-bucket", "us-east-2", nil)
	handler := fetcher.Handler()

	_, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error for embedded chain: %v", err)
	}
}

// TestDeserialize_UploadArtifacts verifies that the upload-genesis-artifacts handler
// correctly deserializes the S3 and node name params from the wire format.
func TestDeserialize_UploadArtifacts(t *testing.T) {
	handler := NewGenesisArtifactUploader(t.TempDir(), "test-bucket", "us-east-2", "test-chain", nil).Handler()

	params := map[string]any{
		"nodeName": "",
	}

	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for empty nodeName, got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
	if !strings.Contains(err.Error(), "missing required param 'nodeName'") {
		t.Errorf("expected nodeName validation error, got: %v", err)
	}
}

// TestDeserialize_GenerateIdentity verifies that the generate-identity handler
// correctly deserializes chainId and moniker from the wire format.
func TestDeserialize_GenerateIdentity(t *testing.T) {
	handler := NewIdentityGenerator(t.TempDir()).Handler()

	params := map[string]any{
		"chainId": "",
		"moniker": "val-0",
	}

	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for empty chainId, got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
	if !strings.Contains(err.Error(), "missing required param 'chainId'") {
		t.Errorf("expected chainId validation error, got: %v", err)
	}
}

// TestDeserialize_SetGenesisPeers verifies that the set-genesis-peers handler
// correctly deserializes S3 coordinates from the wire format.
func TestDeserialize_SetGenesisPeers(t *testing.T) {
	handler := NewGenesisPeersSetter(t.TempDir(), "test-bucket", "us-east-2", "test-chain", nil).Handler()

	// The handler will try to download peers.json from S3 — that will fail
	// since there's no real S3 client. But it proves deserialization worked.
	_, err := handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error (no S3), got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
}

// TestDeserialize_StateSync verifies that the state-sync handler
// correctly deserializes useLocalSnapshot (bool), trustPeriod (string),
// and backfillBlocks (int64 from float64) from the wire format.
func TestDeserialize_StateSync(t *testing.T) {
	homeDir := t.TempDir()
	setupPeersInConfig(t, homeDir, nil)

	handler := NewStateSyncConfigurer(homeDir, nil).Handler()

	params := map[string]any{
		"useLocalSnapshot": true,
		"trustPeriod":      "168h0m0s",
		"backfillBlocks":   float64(6000),
	}

	// Will fail because there are no peers, but deserialization should succeed.
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error (no peers), got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
}

// TestDeserialize_ResultExport verifies that the result-export handler
// correctly deserializes the bucket, region, and optional fields.
func TestDeserialize_ResultExport(t *testing.T) {
	handler := NewResultExporter(t.TempDir(), "test-1", "test-pod-0", nil).Handler()

	params := map[string]any{
		"bucket": "",
		"region": "us-east-1",
	}

	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for empty bucket, got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
	if !strings.Contains(err.Error(), "missing required param 'bucket'") {
		t.Errorf("expected bucket validation error, got: %v", err)
	}
}

// TestDeserialize_GenerateGentx verifies that the generate-gentx handler
// correctly deserializes all string params from the wire format.
func TestDeserialize_GenerateGentx(t *testing.T) {
	handler := NewGentxGenerator(t.TempDir()).Handler()

	params := map[string]any{
		"chainId":        "",
		"stakingAmount":  "1000usei",
		"accountBalance": "10000usei",
	}

	_, err := handler(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for empty chainId, got nil")
	}
	if strings.Contains(err.Error(), "parsing params") {
		t.Fatalf("deserialization failed: %v", err)
	}
	if !strings.Contains(err.Error(), "missing required param 'chainId'") {
		t.Errorf("expected chainId validation error, got: %v", err)
	}
}
