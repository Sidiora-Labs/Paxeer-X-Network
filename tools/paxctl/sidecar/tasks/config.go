package tasks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/seictl/internal/patch"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seictl/sidecar/tasks/defaults"
	"github.com/sei-protocol/seilog"
)

var patchLog = seilog.NewLogger("seictl", "task", "config-patch")

// ConfigPatchRequest holds the typed parameters for the config-patch task.
// Files is intentionally map[string]map[string]any because TOML patches
// are inherently untyped.
type ConfigPatchRequest struct {
	Files map[string]map[string]any `json:"files"`
}

// ConfigPatcher applies generic TOML merge-patches to seid configuration files.
type ConfigPatcher struct {
	homeDir string
}

// NewConfigPatcher creates a patcher targeting the given home directory.
func NewConfigPatcher(homeDir string) *ConfigPatcher {
	return &ConfigPatcher{homeDir: homeDir}
}

// Handler returns an engine.TaskHandler that reads a "files" map from params
// and merge-patches each named file under homeDir/config/.
//
// Expected params format:
//
//	{
//	  "files": {
//	    "config.toml": {"p2p": {"persistent-peers": "..."}},
//	    "app.toml":    {"pruning": "nothing"}
//	  }
//	}
func (p *ConfigPatcher) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, params ConfigPatchRequest) error {
		if len(params.Files) == 0 {
			return fmt.Errorf("config-patch: at least one file is required")
		}
		// Convert to map[string]any for PatchFiles (public API).
		files := make(map[string]any, len(params.Files))
		for k, v := range params.Files {
			files[k] = v
		}
		return p.PatchFiles(ctx, files)
	})
}

// PatchFiles merge-patches each named TOML file under homeDir/config/.
func (p *ConfigPatcher) PatchFiles(_ context.Context, files map[string]any) error {
	for filename, rawPatch := range files {
		patchMap, ok := rawPatch.(map[string]any)
		if !ok {
			return fmt.Errorf("config-patch: value for %q must be a map", filename)
		}
		filePath := filepath.Join(p.homeDir, "config", filename)
		patchLog.Debug("patching file", "file", filename)
		if err := mergeAndWrite(filePath, patchMap); err != nil {
			return fmt.Errorf("config-patch %s: %w", filename, err)
		}
	}
	patchLog.Info("files patched", "count", len(files))
	return nil
}

func mergeAndWrite(filePath string, patchMap map[string]any) error {
	doc, err := patch.ReadTOML(filePath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filepath.Base(filePath), err)
	}
	merged, ok := patch.Merge(doc, patchMap).(map[string]any)
	if !ok {
		return fmt.Errorf("merge produced non-map result for %s", filepath.Base(filePath))
	}
	return patch.WriteTOML(filePath, merged)
}

// EnsureDefaultConfig creates the seid home directory structure and writes a
// minimal default config.toml if one does not already exist. The default is
// embedded from defaults/config.toml.
func EnsureDefaultConfig(homeDir string) error {
	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	dataDir := filepath.Join(homeDir, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("creating data directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}

	defaultConfig, err := defaults.FS.ReadFile("config.toml")
	if err != nil {
		return fmt.Errorf("reading embedded default config: %w", err)
	}

	if err := os.WriteFile(configPath, defaultConfig, 0o644); err != nil {
		return fmt.Errorf("writing default config.toml: %w", err)
	}

	return nil
}
