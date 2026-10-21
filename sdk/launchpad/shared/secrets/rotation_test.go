package secrets

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMountedRealVersionRefresh(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	records := []VersionRecord{{ID: "webhook", Version: "one", Audience: "indexer", Activate: now.Add(-time.Minute), Retire: now.Add(time.Minute), Reference: "key1"}}
	write := func(v []VersionRecord) {
		wire, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "registry.json"), wire, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "key1"), []byte(strings.Repeat("k", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	write(records)
	source := MountedVersions{Directory: dir, RegistryName: "registry.json"}
	values, err := source.Load(context.Background())
	if err != nil || len(values) != 1 || values[0].Record.Version != "one" {
		t.Fatal(values, err)
	}
	write([]VersionRecord{})
	values, err = source.Load(context.Background())
	if err != nil || len(values) != 0 {
		t.Fatal("empty revocation snapshot", err)
	}
	records[0].Reference = "../key1"
	write(records)
	if _, err = source.Load(context.Background()); err == nil {
		t.Fatal("traversal accepted")
	}
	records[0].Reference = "key1"
	write(records)
	if err = os.Chmod(filepath.Join(dir, "key1"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = source.Load(context.Background()); err == nil {
		t.Fatal("world-readable secret accepted")
	}
}

func TestEnvironmentRotationRejectsAmbiguityAndRefreshesCachedValues(t *testing.T) {
	now := time.Now()
	record := VersionRecord{ID: "hook", Version: "v1", Audience: "vendor", Activate: now.Add(-time.Minute), Retire: now.Add(time.Hour), Reference: "L45_ENV_VALUE"}
	wire, err := json.Marshal([]VersionRecord{record})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("L45_ENV_REGISTRY", string(wire))
	t.Setenv("L45_ENV_VALUE", strings.Repeat("a", 32))
	provider, err := NewChecked(Options{Provider: ProviderEnv, CacheTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	source := EnvironmentVersions{Registry: "L45_ENV_REGISTRY", Provider: provider}
	first, err := source.Load(context.Background())
	if err != nil || len(first) != 1 || string(first[0].Value) != strings.Repeat("a", 32) {
		t.Fatal("initial load", err)
	}
	t.Setenv("L45_ENV_VALUE", strings.Repeat("b", 32))
	second, err := source.Load(context.Background())
	if err != nil || len(second) != 1 || string(second[0].Value) != strings.Repeat("b", 32) {
		t.Fatal("version reload served cached key", err)
	}
	for _, registry := range []string{"null", `[{"id":"one","id":"two"}]`, `[{"id":"one","unknown":"x"}]`} {
		t.Setenv("L45_ENV_REGISTRY", registry)
		if _, err := source.Load(context.Background()); err == nil {
			t.Fatal("ambiguous version registry admitted", registry)
		}
	}
}
