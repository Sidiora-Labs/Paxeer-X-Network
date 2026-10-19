package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityGenerator_CreatesNodeKey(t *testing.T) {
	homeDir := t.TempDir()
	os.MkdirAll(filepath.Join(homeDir, "config"), 0o755)

	handler := NewIdentityGenerator(homeDir).Handler()
	_, err := handler(context.Background(), map[string]any{
		"chainId": "test-chain-1",
		"moniker": "val-0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify identity files were created
	for _, f := range []string{
		"config/node_key.json",
		"config/config.toml",
	} {
		path := filepath.Join(homeDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist", f)
		}
	}
}

func TestIdentityGenerator_Idempotent(t *testing.T) {
	homeDir := t.TempDir()

	handler := NewIdentityGenerator(homeDir).Handler()
	params := map[string]any{"chainId": "test-chain-1", "moniker": "val-0"}

	if _, err := handler(context.Background(), params); err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Read node_key.json after first call
	nodeKeyBefore, _ := os.ReadFile(filepath.Join(homeDir, "config", "node_key.json"))

	if _, err := handler(context.Background(), params); err != nil {
		t.Fatalf("second call: %v", err)
	}

	// Verify node_key.json wasn't regenerated (marker file skips)
	nodeKeyAfter, _ := os.ReadFile(filepath.Join(homeDir, "config", "node_key.json"))
	if string(nodeKeyBefore) != string(nodeKeyAfter) {
		t.Error("node_key.json changed on second call — idempotency broken")
	}
}

func TestIdentityGenerator_MissingChainID(t *testing.T) {
	handler := NewIdentityGenerator(t.TempDir()).Handler()
	_, err := handler(context.Background(), map[string]any{"moniker": "val-0"})
	if err == nil {
		t.Fatal("expected error for missing chainId")
	}
}

func TestIdentityGenerator_MissingMoniker(t *testing.T) {
	handler := NewIdentityGenerator(t.TempDir()).Handler()
	_, err := handler(context.Background(), map[string]any{"chainId": "test-chain-1"})
	if err == nil {
		t.Fatal("expected error for missing moniker")
	}
}
