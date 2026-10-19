package tasks

import (
	"context"
	"strings"
	"testing"

	seiconfig "github.com/sei-protocol/sei-config"
)

func TestConfigReloader_HotReloadableField(t *testing.T) {
	homeDir := t.TempDir()
	writeDefaultConfig(t, homeDir, seiconfig.ModeFull)

	reloader := NewConfigReloader(homeDir)
	handler := reloader.Handler()

	_, err := handler(context.Background(), map[string]any{
		"fields": map[string]any{
			"logging.level": "debug",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cfg, err := seiconfig.ReadConfigFromDir(homeDir)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("logging.level: got %q, want %q", cfg.Logging.Level, "debug")
	}
}

func TestConfigReloader_NonHotReloadableField(t *testing.T) {
	homeDir := t.TempDir()
	writeDefaultConfig(t, homeDir, seiconfig.ModeFull)

	reloader := NewConfigReloader(homeDir)
	handler := reloader.Handler()

	_, err := handler(context.Background(), map[string]any{
		"fields": map[string]any{
			"storage.db_backend": "rocksdb",
		},
	})
	if err == nil {
		t.Fatal("expected error for non-hot-reloadable field")
	}
	if !strings.Contains(err.Error(), "not hot-reloadable") {
		t.Errorf("error should mention hot-reloadable: %v", err)
	}
}

func TestConfigReloader_UnknownField(t *testing.T) {
	homeDir := t.TempDir()
	writeDefaultConfig(t, homeDir, seiconfig.ModeFull)

	reloader := NewConfigReloader(homeDir)
	handler := reloader.Handler()

	_, err := handler(context.Background(), map[string]any{
		"fields": map[string]any{
			"nonexistent.field": "value",
		},
	})
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "unknown field") {
		t.Errorf("error should mention unknown field: %v", err)
	}
}

func TestConfigReloader_EmptyFields(t *testing.T) {
	homeDir := t.TempDir()
	reloader := NewConfigReloader(homeDir)
	handler := reloader.Handler()

	_, err := handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
	if !strings.Contains(err.Error(), "at least one field") {
		t.Errorf("error should mention at least one field: %v", err)
	}
}
