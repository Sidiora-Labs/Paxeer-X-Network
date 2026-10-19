package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	seiconfig "github.com/sei-protocol/sei-config"
	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seilog"
)

var validateLog = seilog.NewLogger("seictl", "task", "config-validate")

// ConfigValidator reads on-disk config and returns validation diagnostics.
type ConfigValidator struct {
	homeDir string
}

// NewConfigValidator creates a validator targeting the given home directory.
func NewConfigValidator(homeDir string) *ConfigValidator {
	return &ConfigValidator{homeDir: homeDir}
}

// Handler returns an engine.TaskHandler for the config-validate task type.
func (v *ConfigValidator) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(_ context.Context, _ struct{}) error {
		cfg, err := seiconfig.ReadConfigFromDir(v.homeDir)
		if err != nil {
			return fmt.Errorf("config-validate: reading config: %w", err)
		}

		vr := seiconfig.Validate(cfg)

		type resultJSON struct {
			Valid       bool   `json:"valid"`
			Version     int    `json:"version"`
			Mode        string `json:"mode"`
			Diagnostics []struct {
				Severity string `json:"severity"`
				Field    string `json:"field"`
				Message  string `json:"message"`
			} `json:"diagnostics"`
		}

		result := resultJSON{
			Valid:   !vr.HasErrors(),
			Version: cfg.Version,
			Mode:    string(cfg.Mode),
		}
		for _, d := range vr.Diagnostics {
			result.Diagnostics = append(result.Diagnostics, struct {
				Severity string `json:"severity"`
				Field    string `json:"field"`
				Message  string `json:"message"`
			}{
				Severity: d.Severity.String(),
				Field:    d.Field,
				Message:  d.Message,
			})
		}

		if vr.HasErrors() {
			data, _ := json.Marshal(result)
			return fmt.Errorf("config validation failed: %s", data)
		}

		validateLog.Info("config validated",
			"valid", result.Valid,
			"version", result.Version,
			"mode", result.Mode,
			"diagnostics", len(result.Diagnostics),
		)
		return nil
	})
}
