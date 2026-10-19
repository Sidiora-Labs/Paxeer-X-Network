package tasks

import (
	"context"
	"fmt"

	seiconfig "github.com/sei-protocol/sei-config"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seilog"
)

var reloadLog = seilog.NewLogger("seictl", "task", "config-reload")

// ConfigReloadRequest holds the typed parameters for the config-reload task.
type ConfigReloadRequest struct {
	Fields map[string]string `json:"fields"`
}

// ConfigReloader patches hot-reloadable fields on disk and signals seid
// to re-read its configuration. The signal mechanism is deferred to a future
// release; for now only the on-disk write is performed.
type ConfigReloader struct {
	homeDir string
}

// NewConfigReloader creates a reloader targeting the given home directory.
func NewConfigReloader(homeDir string) *ConfigReloader {
	return &ConfigReloader{homeDir: homeDir}
}

// Handler returns an engine.TaskHandler for the config-reload task type.
func (r *ConfigReloader) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(_ context.Context, params ConfigReloadRequest) error {
		if len(params.Fields) == 0 {
			return fmt.Errorf("config-reload: at least one field is required")
		}

		registry := seiconfig.BuildRegistry()
		registry.EnrichAll(seiconfig.DefaultEnrichments())

		var nonHotReload []string
		for key := range params.Fields {
			f := registry.Field(key)
			if f == nil {
				return fmt.Errorf("config-reload: unknown field %q", key)
			}
			if !f.HotReload {
				nonHotReload = append(nonHotReload, key)
			}
		}
		if len(nonHotReload) > 0 {
			return fmt.Errorf(
				"config-reload: fields %v are not hot-reloadable and require a restart",
				nonHotReload)
		}

		cfg, err := seiconfig.ReadConfigFromDir(r.homeDir)
		if err != nil {
			return fmt.Errorf("config-reload: reading config: %w", err)
		}

		if err := seiconfig.ApplyOverrides(cfg, params.Fields); err != nil {
			return fmt.Errorf("config-reload: applying fields: %w", err)
		}

		vr := seiconfig.Validate(cfg)
		if vr.HasErrors() {
			return validationError(vr)
		}

		if err := seiconfig.WriteConfigToDir(cfg, r.homeDir); err != nil {
			return fmt.Errorf("config-reload: writing config: %w", err)
		}

		// TODO: signal seid to re-read config (SIGHUP or API call)
		reloadLog.Info("hot-reloadable fields written, seid signal pending implementation",
			"fields", len(params.Fields))

		return nil
	})
}
