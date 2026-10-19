package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestArgumentPrecedence(t *testing.T) {
	tests := []struct {
		name            string
		configContent   string
		cliArgs         []string
		expectedStats   time.Duration
		expectedWorkers int
		expectedTPS     float64
	}{
		{
			name: "config file only",
			configContent: `{
				"statsInterval": "5s",
				"workers": 3,
				"tps": 100.5
			}`,
			cliArgs:         []string{},
			expectedStats:   5 * time.Second,
			expectedWorkers: 3,
			expectedTPS:     100.5,
		},
		{
			name: "CLI overrides config",
			configContent: `{
				"statsInterval": "5s",
				"workers": 3,
				"tps": 100.5
			}`,
			cliArgs:         []string{"--stats-interval", "3s", "--workers", "7"},
			expectedStats:   3 * time.Second,
			expectedWorkers: 7,
			expectedTPS:     100.5, // Not overridden by CLI
		},
		{
			name: "defaults when neither CLI nor config",
			configContent: `{
				"endpoints": ["http://localhost:8545"]
			}`,
			cliArgs:         []string{},
			expectedStats:   10 * time.Second, // Default
			expectedWorkers: 1,                // Default
			expectedTPS:     0.0,              // Default
		},
		{
			name: "CLI overrides defaults",
			configContent: `{
				"endpoints": ["http://localhost:8545"]
			}`,
			cliArgs:         []string{"--stats-interval", "15s", "--tps", "50"},
			expectedStats:   15 * time.Second,
			expectedWorkers: 1,    // Default (not overridden)
			expectedTPS:     50.0, // CLI override
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper for each test
			viper.Reset()

			// create Settings struct
			configSettings := &Settings{}
			err := json.Unmarshal([]byte(tt.configContent), configSettings)
			require.NoError(t, err, "Failed to unmarshal config file")

			// Create test command with flags
			cmd := &cobra.Command{
				Use: "test",
			}

			// Add flags (with zero defaults to avoid precedence issues)
			cmd.Flags().Duration("stats-interval", 0, "Stats interval")
			cmd.Flags().Int("workers", 0, "Number of workers")
			cmd.Flags().Float64("tps", 0, "TPS")
			cmd.Flags().Bool("dry-run", false, "Dry run")
			cmd.Flags().Bool("debug", false, "Debug")
			cmd.Flags().Bool("track-receipts", false, "Track receipts")
			cmd.Flags().Bool("track-blocks", false, "Track blocks")
			cmd.Flags().Bool("prewarm", false, "Prewarm")
			cmd.Flags().Bool("track-user-latency", false, "Track user latency")
			cmd.Flags().Int("buffer-size", 0, "Buffer size")
			cmd.Flags().Bool("ramp-up", false, "Ramp up loadtest")
			cmd.Flags().String("report-path", "", "Report path")
			cmd.Flags().String("txs-dir", "", "Txs dir")
			cmd.Flags().Uint64("target-gas", 0, "Target gas")
			cmd.Flags().Int("num-blocks-to-write", 0, "Number of blocks to write")

			// Parse CLI args
			if len(tt.cliArgs) > 0 {
				cmd.SetArgs(tt.cliArgs)
				require.NoError(t, cmd.Execute(), "Failed to parse CLI args")
			}

			// Initialize Viper
			require.NoError(t, InitializeViper(cmd), "Failed to initialize Viper")

			// Load settings
			require.NoError(t, LoadSettings(configSettings), "Failed to load settings")

			// Resolve settings
			settings := ResolveSettings()

			// Verify expectations
			require.Equal(t, tt.expectedStats, settings.StatsInterval.ToDuration(), "StatsInterval: expected %v, got %v", tt.expectedStats, settings.StatsInterval.ToDuration())
			require.Equal(t, tt.expectedWorkers, settings.Workers, "Workers: expected %d, got %d", tt.expectedWorkers, settings.Workers)
			require.Equal(t, tt.expectedTPS, settings.TPS, "TPS: expected %f, got %f", tt.expectedTPS, settings.TPS)
		})
	}
}

func TestDefaultSettings(t *testing.T) {
	defaults := DefaultSettings()

	expected := Settings{
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

	if defaults != expected {
		t.Errorf("DefaultSettings mismatch.\nExpected: %+v\nGot: %+v", expected, defaults)
	}
}
