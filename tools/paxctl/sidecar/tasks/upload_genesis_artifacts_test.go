package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactUploader_UploadsGentxAndIdentity(t *testing.T) {
	homeDir := t.TempDir()

	gentxDir := filepath.Join(homeDir, "config", "gentx")
	if err := os.MkdirAll(gentxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gentxDir, "gentx-abc123.json"), []byte(`{"gentx":"data"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(homeDir, "config")
	if err := os.WriteFile(filepath.Join(configDir, "node_key.json"), []byte(`{"priv_key":{"value":"base64key"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := newMockS3Uploader()
	uploader := NewGenesisArtifactUploader(homeDir, "test-bucket", "us-east-2", "test-chain", mockUploaderFactory(mock))
	handler := uploader.Handler()

	_, err := handler(context.Background(), map[string]any{
		"nodeName": "val-0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, ok := mock.uploads["test-bucket/test-chain/val-0/gentx.json"]; !ok {
		t.Errorf("expected gentx.json upload, uploads: %v", keys(mock.uploads))
	}
	if _, ok := mock.uploads["test-bucket/test-chain/val-0/identity.json"]; !ok {
		t.Errorf("expected identity.json upload, uploads: %v", keys(mock.uploads))
	}
}

func TestArtifactUploader_Idempotent(t *testing.T) {
	homeDir := t.TempDir()

	gentxDir := filepath.Join(homeDir, "config", "gentx")
	if err := os.MkdirAll(gentxDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gentxDir, "gentx-abc.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "config", "node_key.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := newMockS3Uploader()
	uploader := NewGenesisArtifactUploader(homeDir, "test-bucket", "us-east-2", "test-chain", mockUploaderFactory(mock))
	handler := uploader.Handler()

	params := map[string]any{
		"nodeName": "n",
	}
	if _, err := handler(context.Background(), params); err != nil {
		t.Fatalf("first call: %v", err)
	}
	firstUploads := len(mock.uploads)

	if _, err := handler(context.Background(), params); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if len(mock.uploads) != firstUploads {
		t.Fatal("expected no new uploads on second call")
	}
}

func TestArtifactUploader_MissingParams(t *testing.T) {
	handler := NewGenesisArtifactUploader(t.TempDir(), "test-bucket", "us-east-2", "test-chain", nil).Handler()

	tests := []struct {
		name   string
		params map[string]any
	}{
		{"missing nodeName", map[string]any{}},
		{"empty nodeName", map[string]any{"nodeName": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := handler(context.Background(), tt.params); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestArtifactUploader_NoGentxFile(t *testing.T) {
	homeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(homeDir, "config", "gentx"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, "config", "node_key.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	mock := newMockS3Uploader()
	handler := NewGenesisArtifactUploader(homeDir, "test-bucket", "us-east-2", "test-chain", mockUploaderFactory(mock)).Handler()

	_, err := handler(context.Background(), map[string]any{
		"nodeName": "n",
	})
	if err == nil {
		t.Fatal("expected error when no gentx file exists")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
