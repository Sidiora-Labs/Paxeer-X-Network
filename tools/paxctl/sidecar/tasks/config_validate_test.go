package tasks

import (
	"context"
	"strings"
	"testing"

	seiconfig "github.com/sei-protocol/sei-config"
)

func TestConfigValidator_ValidConfig(t *testing.T) {
	homeDir := t.TempDir()
	writeDefaultConfig(t, homeDir, seiconfig.ModeValidator)

	validator := NewConfigValidator(homeDir)
	handler := validator.Handler()

	_, err := handler(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error for valid config: %v", err)
	}
}

func TestConfigValidator_AllModes(t *testing.T) {
	modes := []seiconfig.NodeMode{
		seiconfig.ModeValidator, seiconfig.ModeFull, seiconfig.ModeSeed,
		seiconfig.ModeArchive,
	}
	for _, mode := range modes {
		t.Run(string(mode), func(t *testing.T) {
			homeDir := t.TempDir()
			writeDefaultConfig(t, homeDir, mode)

			validator := NewConfigValidator(homeDir)
			_, err := validator.Handler()(context.Background(), nil)
			if err != nil {
				t.Fatalf("mode %s validation failed: %v", mode, err)
			}
		})
	}
}

func TestConfigValidator_MissingFiles(t *testing.T) {
	homeDir := t.TempDir()
	validator := NewConfigValidator(homeDir)
	handler := validator.Handler()

	_, err := handler(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for missing config files")
	}
	if !strings.Contains(err.Error(), "reading config") {
		t.Errorf("error should mention reading: %v", err)
	}
}

func TestConfigValidator_InvalidConfig(t *testing.T) {
	homeDir := t.TempDir()
	cfg := seiconfig.DefaultForMode(seiconfig.ModeValidator)
	cfg.Chain.MinGasPrices = "" // triggers validation error
	if err := seiconfig.WriteConfigToDir(cfg, homeDir); err != nil {
		t.Fatal(err)
	}

	validator := NewConfigValidator(homeDir)
	handler := validator.Handler()

	_, err := handler(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error should mention validation: %v", err)
	}
}
