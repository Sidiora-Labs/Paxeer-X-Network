package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

// genesisFetchFixture wires a GenesisFetcher to a mock S3 holding a single
// genesis.json under the unknown chain's key, returning the fetcher plus the
// bytes' true SHA-256 hex digest. The chain ID is intentionally not embedded so
// the handler takes the S3 fallback path.
func genesisFetchFixture(t *testing.T, body []byte) (*GenesisFetcher, string, string) {
	t.Helper()
	homeDir := t.TempDir()
	const chainID = "custom-devnet-1"
	key := chainID + "/genesis.json"
	s3 := &mockS3GetObject{objects: map[string][]byte{key: body}}
	factory := func(_ context.Context, _ string) (S3GetObjectAPI, error) { return s3, nil }
	fetcher := NewGenesisFetcher(homeDir, chainID, "genesis-bucket", "us-east-2", factory)
	sum := sha256.Sum256(body)
	return fetcher, hex.EncodeToString(sum[:]), homeDir
}

func TestGenesisFetcher_S3_MatchingHashSucceeds(t *testing.T) {
	body := []byte(`{"chain_id":"custom-devnet-1","app_state":{}}`)
	fetcher, wantHash, homeDir := genesisFetchFixture(t, body)

	_, err := fetcher.Handler()(context.Background(), map[string]any{"expectedGenesisHash": wantHash})
	if err != nil {
		t.Fatalf("matching hash should succeed, got: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(homeDir, "config", "genesis.json"))
	if err != nil {
		t.Fatalf("reading genesis.json: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("genesis bytes = %q, want %q", got, body)
	}
	if !markerExists(homeDir, genesisMarkerFile) {
		t.Error("expected completion marker to be written on verified download")
	}
}

func TestGenesisFetcher_S3_MismatchedHashFailsClosed(t *testing.T) {
	body := []byte(`{"chain_id":"custom-devnet-1","app_state":{}}`)
	fetcher, _, homeDir := genesisFetchFixture(t, body)

	_, err := fetcher.Handler()(context.Background(), map[string]any{
		"expectedGenesisHash": "0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("mismatched hash must fail closed, got nil error")
	}

	var te *engine.TaskError
	if !errors.As(err, &te) {
		t.Fatalf("error type = %T, want *engine.TaskError", err)
	}
	if te.Retryable {
		t.Error("hash-mismatch error must be terminal (non-retryable)")
	}

	if _, statErr := os.Stat(filepath.Join(homeDir, "config", "genesis.json")); !os.IsNotExist(statErr) {
		t.Error("partial genesis.json must be deleted on mismatch")
	}
	if markerExists(homeDir, genesisMarkerFile) {
		t.Error("completion marker must NOT be written on mismatch (re-verify safety)")
	}
}

func TestGenesisFetcher_S3_EmptyHashPreservesBehavior(t *testing.T) {
	body := []byte(`{"chain_id":"custom-devnet-1","app_state":{}}`)
	fetcher, _, homeDir := genesisFetchFixture(t, body)

	// No expectedGenesisHash in params — the current controller's wire shape.
	if _, err := fetcher.Handler()(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("empty expected hash should download unverified, got: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(homeDir, "config", "genesis.json"))
	if err != nil {
		t.Fatalf("reading genesis.json: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("genesis bytes = %q, want %q", got, body)
	}
	if !markerExists(homeDir, genesisMarkerFile) {
		t.Error("expected completion marker on unverified download")
	}
}

func TestGenesisFetcher_EmbeddedChain(t *testing.T) {
	homeDir := t.TempDir()
	fetcher := NewGenesisFetcher(homeDir, "pacific-1", "test-bucket", "us-east-2", nil)
	handler := fetcher.Handler()

	_, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(homeDir, "config", "genesis.json"))
	if err != nil {
		t.Fatalf("reading genesis.json: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("genesis.json is empty")
	}

	var doc struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("genesis.json is not valid JSON: %v", err)
	}
	if doc.ChainID != "pacific-1" {
		t.Errorf("chain_id = %q, want %q", doc.ChainID, "pacific-1")
	}
}

func TestGenesisFetcher_EmbeddedChain_Idempotent(t *testing.T) {
	homeDir := t.TempDir()
	fetcher := NewGenesisFetcher(homeDir, "atlantic-2", "test-bucket", "us-east-2", nil)
	handler := fetcher.Handler()

	if _, err := handler(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := handler(context.Background(), map[string]any{}); err != nil {
		t.Fatalf("second call (should skip via marker): %v", err)
	}
}

func TestGenesisFetcher_UnknownChainFallsBackToS3(t *testing.T) {
	homeDir := t.TempDir()
	called := false
	mockFactory := func(ctx context.Context, region string) (S3GetObjectAPI, error) {
		called = true
		if region != "us-east-2" {
			t.Errorf("region = %q, want %q", region, "us-east-2")
		}
		return nil, fmt.Errorf("mock: intentional S3 error")
	}
	fetcher := NewGenesisFetcher(homeDir, "custom-devnet-1", "my-genesis-bucket", "us-east-2", mockFactory)
	handler := fetcher.Handler()

	_, err := handler(context.Background(), map[string]any{})
	if !called {
		t.Fatal("expected S3 fallback for unknown chain")
	}
	if err == nil {
		t.Fatal("expected error from mock S3 factory")
	}
}

func TestGenesisFetcher_UnknownChainNoBucket(t *testing.T) {
	homeDir := t.TempDir()
	fetcher := NewGenesisFetcher(homeDir, "custom-devnet-1", "", "", nil)
	handler := fetcher.Handler()

	_, err := handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown chain with no bucket configured")
	}
}

func TestGenesisFetcher_NoChainID(t *testing.T) {
	homeDir := t.TempDir()
	fetcher := NewGenesisFetcher(homeDir, "", "bucket", "region", nil)
	handler := fetcher.Handler()

	_, err := handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error when chainID is empty")
	}
}
