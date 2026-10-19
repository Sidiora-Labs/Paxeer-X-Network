package tasks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/seictl/internal/patch"
)

func setupConfigFile(t *testing.T, homeDir, content string) string {
	t.Helper()
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config.toml: %v", err)
	}
	return configPath
}

func setupAppFile(t *testing.T, homeDir, content string) string {
	t.Helper()
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	appPath := filepath.Join(configDir, "app.toml")
	if err := os.WriteFile(appPath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing app.toml: %v", err)
	}
	return appPath
}

func readTOML(t *testing.T, path string) map[string]any {
	t.Helper()
	doc, err := patch.ReadTOML(path)
	if err != nil {
		t.Fatalf("reading TOML %s: %v", path, err)
	}
	return doc
}

func TestConfigPatcherMergesConfigToml(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[p2p]
persistent-peers = ""
laddr = "tcp://0.0.0.0:26656"
`)

	patcher := NewConfigPatcher(homeDir)
	err := patcher.PatchFiles(context.Background(), map[string]any{
		"config.toml": map[string]any{
			"p2p": map[string]any{
				"persistent-peers": "abc@1.2.3.4:26656,def@5.6.7.8:26656",
			},
		},
	})
	if err != nil {
		t.Fatalf("PatchFiles failed: %v", err)
	}

	doc := readTOML(t, configPath)
	p2p := doc["p2p"].(map[string]any)
	if p2p["persistent-peers"] != "abc@1.2.3.4:26656,def@5.6.7.8:26656" {
		t.Fatalf("expected peers, got %q", p2p["persistent-peers"])
	}
	if p2p["laddr"] != "tcp://0.0.0.0:26656" {
		t.Fatalf("expected laddr preserved, got %q", p2p["laddr"])
	}
}

func TestConfigPatcherMergesMultipleFiles(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[statesync]
enable = false
`)
	appPath := setupAppFile(t, homeDir, `
pruning = "default"
snapshot-interval = 0
`)

	patcher := NewConfigPatcher(homeDir)
	err := patcher.PatchFiles(context.Background(), map[string]any{
		"config.toml": map[string]any{
			"statesync": map[string]any{
				"use-local-snapshot": true,
				"backfill-blocks":    int64(0),
				"trust-period":       "9999h0m0s",
			},
		},
		"app.toml": map[string]any{
			"pruning":              "nothing",
			"snapshot-interval":    int64(2000),
			"snapshot-keep-recent": int64(5),
		},
	})
	if err != nil {
		t.Fatalf("PatchFiles failed: %v", err)
	}

	configDoc := readTOML(t, configPath)
	ss := configDoc["statesync"].(map[string]any)
	if ss["use-local-snapshot"] != true {
		t.Fatal("expected use-local-snapshot = true")
	}
	if ss["trust-period"] != "9999h0m0s" {
		t.Fatalf("expected trust-period = 9999h0m0s, got %v", ss["trust-period"])
	}

	appDoc := readTOML(t, appPath)
	if appDoc["pruning"] != "nothing" {
		t.Fatalf("expected pruning = nothing, got %v", appDoc["pruning"])
	}
	if appDoc["snapshot-interval"] != int64(2000) {
		t.Fatalf("expected snapshot-interval = 2000, got %v", appDoc["snapshot-interval"])
	}
}

func TestConfigPatcherPreservesUnrelatedFields(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[consensus]
timeout-commit = "5s"

[p2p]
persistent-peers = ""
max-num-inbound-peers = 40

[mempool]
size = 5000
`)

	patcher := NewConfigPatcher(homeDir)
	err := patcher.PatchFiles(context.Background(), map[string]any{
		"config.toml": map[string]any{
			"p2p": map[string]any{
				"persistent-peers": "abc@1.2.3.4:26656",
			},
		},
	})
	if err != nil {
		t.Fatalf("PatchFiles failed: %v", err)
	}

	doc := readTOML(t, configPath)
	consensus := doc["consensus"].(map[string]any)
	if consensus["timeout-commit"] != "5s" {
		t.Fatal("consensus.timeout-commit not preserved")
	}
	p2p := doc["p2p"].(map[string]any)
	if p2p["max-num-inbound-peers"] != int64(40) {
		t.Fatal("p2p.max-num-inbound-peers not preserved")
	}
	mempool := doc["mempool"].(map[string]any)
	if mempool["size"] != int64(5000) {
		t.Fatal("mempool.size not preserved")
	}
}

func TestConfigPatcherCreatesNewSections(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[p2p]
persistent-peers = ""
`)

	patcher := NewConfigPatcher(homeDir)
	err := patcher.PatchFiles(context.Background(), map[string]any{
		"config.toml": map[string]any{
			"statesync": map[string]any{
				"enable": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("PatchFiles failed: %v", err)
	}

	doc := readTOML(t, configPath)
	ss := doc["statesync"].(map[string]any)
	if ss["enable"] != true {
		t.Fatal("expected statesync.enable = true")
	}
}

func TestConfigPatcherHandlerRejectsEmptyFiles(t *testing.T) {
	patcher := NewConfigPatcher(t.TempDir())
	handler := patcher.Handler()
	_, err := handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty files, got nil")
	}
}

func TestConfigPatcherRejectsNonMapValue(t *testing.T) {
	homeDir := t.TempDir()
	setupConfigFile(t, homeDir, `
[p2p]
persistent-peers = ""
`)

	patcher := NewConfigPatcher(homeDir)
	err := patcher.PatchFiles(context.Background(), map[string]any{
		"config.toml": "not a map",
	})
	if err == nil {
		t.Fatal("expected error for non-map file value")
	}
}

func TestConfigPatcherHandlerRoundTrip(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[p2p]
persistent-peers = ""
`)

	patcher := NewConfigPatcher(homeDir)
	handler := patcher.Handler()
	_, err := handler(context.Background(), map[string]any{
		"files": map[string]any{
			"config.toml": map[string]any{
				"p2p": map[string]any{
					"persistent-peers": "node1@1.2.3.4:26656",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Handler failed: %v", err)
	}

	doc := readTOML(t, configPath)
	p2p := doc["p2p"].(map[string]any)
	if p2p["persistent-peers"] != "node1@1.2.3.4:26656" {
		t.Fatalf("expected peers via handler, got %q", p2p["persistent-peers"])
	}
}

func TestConfigPatcherCreatesFileIfMissing(t *testing.T) {
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	patcher := NewConfigPatcher(homeDir)
	err := patcher.PatchFiles(context.Background(), map[string]any{
		"app.toml": map[string]any{
			"pruning": "nothing",
		},
	})
	if err != nil {
		t.Fatalf("PatchFiles failed on missing file: %v", err)
	}

	appPath := filepath.Join(configDir, "app.toml")
	doc := readTOML(t, appPath)
	if doc["pruning"] != "nothing" {
		t.Fatalf("expected pruning=nothing in newly created file, got %v", doc["pruning"])
	}
}

func TestWritePeersToConfig(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[p2p]
persistent-peers = ""
laddr = "tcp://0.0.0.0:26656"
`)

	err := writePeersToConfig(homeDir, []string{"abc@1.2.3.4:26656", "def@5.6.7.8:26656"})
	if err != nil {
		t.Fatalf("writePeersToConfig failed: %v", err)
	}

	doc := readTOML(t, configPath)
	p2p := doc["p2p"].(map[string]any)
	if p2p["persistent-peers"] != "abc@1.2.3.4:26656,def@5.6.7.8:26656" {
		t.Fatalf("expected joined peers, got %q", p2p["persistent-peers"])
	}
	if p2p["laddr"] != "tcp://0.0.0.0:26656" {
		t.Fatal("laddr not preserved")
	}
}

func TestWriteStateSyncToConfig(t *testing.T) {
	homeDir := t.TempDir()
	configPath := setupConfigFile(t, homeDir, `
[statesync]
enable = false
trust-height = 0
trust-hash = ""
rpc-servers = ""
`)

	cfg := StateSyncConfig{
		TrustHeight: 500000,
		TrustHash:   "ABCDEF1234567890",
		RpcServers:  "1.2.3.4:26657,5.6.7.8:26657",
	}
	err := writeStateSyncToConfig(homeDir, cfg)
	if err != nil {
		t.Fatalf("writeStateSyncToConfig failed: %v", err)
	}

	doc := readTOML(t, configPath)
	ss := doc["statesync"].(map[string]any)
	if ss["enable"] != true {
		t.Fatal("expected enable=true")
	}
	if ss["trust-height"] != int64(500000) {
		t.Fatalf("expected trust-height=500000, got %v", ss["trust-height"])
	}
	if ss["trust-hash"] != "ABCDEF1234567890" {
		t.Fatalf("expected trust-hash, got %v", ss["trust-hash"])
	}
	if ss["rpc-servers"] != "1.2.3.4:26657,5.6.7.8:26657" {
		t.Fatalf("expected rpc-servers, got %v", ss["rpc-servers"])
	}
}
