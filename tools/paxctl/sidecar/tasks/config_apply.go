package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	seiconfig "github.com/sei-protocol/sei-config"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seilog"
)

var applyLog = seilog.NewLogger("seictl", "task", "config-apply")

// ConfigApplier generates or patches node config using sei-config's
// intent resolution pipeline. The handler deserializes a ConfigIntent from
// task params, calls the appropriate resolver, and writes the result to disk.
type ConfigApplier struct {
	homeDir string
}

// NewConfigApplier creates an applier targeting the given home directory.
func NewConfigApplier(homeDir string) *ConfigApplier {
	return &ConfigApplier{homeDir: homeDir}
}

// Handler returns an engine.TaskHandler for the config-apply task type.
func (a *ConfigApplier) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, intent seiconfig.ConfigIntent) error {
		if intent.Incremental {
			return a.applyIncremental(ctx, intent)
		}
		return a.applyFull(ctx, intent)
	})
}

// applyFull resolves an intent from mode defaults and writes the result.
func (a *ConfigApplier) applyFull(_ context.Context, intent seiconfig.ConfigIntent) error {
	applyLog.Info("resolving config intent", "mode", intent.Mode, "targetVersion", intent.TargetVersion)

	result, err := seiconfig.ResolveIntent(intent)
	if err != nil {
		return fmt.Errorf("config-apply: %w", err)
	}

	if !result.Valid {
		return resultError(result)
	}

	for _, d := range result.Diagnostics {
		if d.Severity == seiconfig.SeverityWarning {
			applyLog.Warn("config warning", "field", d.Field, "message", d.Message)
		}
	}

	configDir := filepath.Join(a.homeDir, "config")
	if err := seiconfig.WriteConfigToDir(result.Config, a.homeDir); err != nil {
		return fmt.Errorf("config-apply: writing config to %s: %w", configDir, err)
	}

	applyLog.Info("config written",
		"mode", result.Mode,
		"version", result.Version,
		"overrides", len(intent.Overrides),
	)
	return nil
}

// applyIncremental reads current on-disk config and resolves the intent
// incrementally against it.
func (a *ConfigApplier) applyIncremental(_ context.Context, intent seiconfig.ConfigIntent) error {
	current, err := seiconfig.ReadConfigFromDir(a.homeDir)
	if err != nil {
		return fmt.Errorf("config-apply incremental: reading current config: %w", err)
	}

	applyLog.Info("resolving incremental intent", "overrides", len(intent.Overrides))

	result, err := seiconfig.ResolveIncrementalIntent(intent, current)
	if err != nil {
		return fmt.Errorf("config-apply incremental: %w", err)
	}

	if !result.Valid {
		return resultError(result)
	}

	for _, d := range result.Diagnostics {
		if d.Severity == seiconfig.SeverityWarning {
			applyLog.Warn("config warning", "field", d.Field, "message", d.Message)
		}
	}

	if err := seiconfig.WriteConfigToDir(result.Config, a.homeDir); err != nil {
		return fmt.Errorf("config-apply incremental: writing config: %w", err)
	}

	applyLog.Info("incremental config applied",
		"mode", result.Mode,
		"version", result.Version,
		"overrides", len(intent.Overrides),
	)
	return nil
}

// resultError formats a ConfigResult's diagnostics as a structured JSON error
// so the controller can parse them from the task result.
func resultError(result *seiconfig.ConfigResult) error {
	return diagnosticsError(result.Diagnostics)
}

// validationError formats ValidationResult diagnostics as a structured JSON
// error. Used by handlers that call seiconfig.Validate() directly (e.g. reload).
func validationError(vr *seiconfig.ValidationResult) error {
	return diagnosticsError(vr.Diagnostics)
}

// diagnosticsError converts a slice of Diagnostic findings into a structured
// JSON error string suitable for returning to the controller.
func diagnosticsError(diags []seiconfig.Diagnostic) error {
	type diagJSON struct {
		Severity string `json:"severity"`
		Field    string `json:"field"`
		Message  string `json:"message"`
	}
	out := make([]diagJSON, len(diags))
	for i, d := range diags {
		out[i] = diagJSON{
			Severity: d.Severity.String(),
			Field:    d.Field,
			Message:  d.Message,
		}
	}
	data, _ := json.Marshal(out)
	return fmt.Errorf("config validation failed: %s", data)
}
