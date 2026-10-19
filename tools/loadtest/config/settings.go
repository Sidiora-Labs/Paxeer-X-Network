package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Settings holds all CLI-configurable parameters
type Settings struct {
	Workers          int      `json:"workers,omitempty"`
	TPS              float64  `json:"tps,omitempty"`
	StatsInterval    Duration `json:"statsInterval,omitempty"`
	BufferSize       int      `json:"bufferSize,omitempty"`
	DryRun           bool     `json:"dryRun,omitempty"`
	Debug            bool     `json:"debug,omitempty"`
	TrackReceipts    bool     `json:"trackReceipts,omitempty"`
	TrackBlocks      bool     `json:"trackBlocks,omitempty"`
	TrackUserLatency bool     `json:"trackUserLatency,omitempty"`
	Prewarm          bool     `json:"prewarm,omitempty"`
	RampUp           bool     `json:"rampUp,omitempty"`
	ReportPath       string   `json:"reportPath,omitempty"`
	TxsDir           string   `json:"txsDir,omitempty"`
	TargetGas        uint64   `json:"targetGas,omitempty"`
	NumBlocksToWrite int      `json:"numBlocksToWrite,omitempty"`
}

// DefaultSettings returns the default configuration values
func DefaultSettings() Settings {
	return Settings{
		Workers:          1,
		TPS:              0.0,
		StatsInterval:    Duration(10 * time.Second),
		BufferSize:       1000,
		DryRun:           false,
		Debug:            false,
		TrackReceipts:    false,
		TrackBlocks:      false,
		TrackUserLatency: false,
		Prewarm:          false,
		RampUp:           false,
		ReportPath:       "",
		TxsDir:           "",
		TargetGas:        10_000_000,
		NumBlocksToWrite: 100,
	}
}

// InitializeViper sets up Viper with CLI flags and defaults
func InitializeViper(cmd *cobra.Command) error {
	// Bind flags to viper with error checking
	flagBindings := map[string]string{
		"statsInterval":    "stats-interval",
		"bufferSize":       "buffer-size",
		"tps":              "tps",
		"dryRun":           "dry-run",
		"debug":            "debug",
		"trackReceipts":    "track-receipts",
		"trackBlocks":      "track-blocks",
		"prewarm":          "prewarm",
		"trackUserLatency": "track-user-latency",
		"workers":          "workers",
		"rampUp":           "ramp-up",
		"reportPath":       "report-path",
		"txsDir":           "txs-dir",
		"targetGas":        "target-gas",
		"numBlocksToWrite": "num-blocks-to-write",
	}

	for viperKey, flagName := range flagBindings {
		if err := viper.BindPFlag(viperKey, cmd.Flags().Lookup(flagName)); err != nil {
			return fmt.Errorf("failed to bind flag %s: %w", flagName, err)
		}
	}

	// Set defaults in Viper
	defaults := DefaultSettings()
	viper.SetDefault("statsInterval", defaults.StatsInterval.ToDuration())
	viper.SetDefault("bufferSize", defaults.BufferSize)
	viper.SetDefault("tps", defaults.TPS)
	viper.SetDefault("dryRun", defaults.DryRun)
	viper.SetDefault("debug", defaults.Debug)
	viper.SetDefault("trackReceipts", defaults.TrackReceipts)
	viper.SetDefault("trackBlocks", defaults.TrackBlocks)
	viper.SetDefault("prewarm", defaults.Prewarm)
	viper.SetDefault("trackUserLatency", defaults.TrackUserLatency)
	viper.SetDefault("workers", defaults.Workers)
	viper.SetDefault("rampUp", defaults.RampUp)
	viper.SetDefault("reportPath", defaults.ReportPath)
	viper.SetDefault("txsDir", defaults.TxsDir)
	viper.SetDefault("targetGas", defaults.TargetGas)
	viper.SetDefault("numBlocksToWrite", defaults.NumBlocksToWrite)
	return nil
}

// LoadSettings reads and merges the config file into Viper
func LoadSettings(settings *Settings) error {
	if settings == nil {
		return fmt.Errorf("config settings are required")
	}

	// settings converted to JSON bytes
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("failed to marshal settings: %w", err)
	}

	viper.SetConfigType("json")
	if err := viper.ReadConfig(bytes.NewBuffer(settingsJSON)); err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	return nil
}

// ResolveSettings gets the final resolved settings from Viper
func ResolveSettings() Settings {
	return Settings{
		Workers:          viper.GetInt("workers"),
		TPS:              viper.GetFloat64("tps"),
		StatsInterval:    Duration(viper.GetDuration("statsInterval")),
		BufferSize:       viper.GetInt("bufferSize"),
		DryRun:           viper.GetBool("dryRun"),
		Debug:            viper.GetBool("debug"),
		TrackReceipts:    viper.GetBool("trackReceipts"),
		TrackBlocks:      viper.GetBool("trackBlocks"),
		TrackUserLatency: viper.GetBool("trackUserLatency"),
		Prewarm:          viper.GetBool("prewarm"),
		RampUp:           viper.GetBool("rampUp"),
		ReportPath:       viper.GetString("reportPath"),
		TxsDir:           viper.GetString("txsDir"),
		TargetGas:        viper.GetUint64("targetGas"),
		NumBlocksToWrite: viper.GetInt("numBlocksToWrite"),
	}
}
