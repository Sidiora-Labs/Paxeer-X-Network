package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/sei-protocol/seictl/internal/patch"
	"github.com/urfave/cli/v3"
)

// configTargetHints maps top-level keys or sections to a configuration target,
// i.e. one of app, client or config.
var configTargetHints = map[string]string{
	// From app.toml - top-level keys
	"minimum-gas-prices":               "app",
	"min-retain-blocks":                "app",
	"concurrency-workers":              "app",
	"occ-enabled":                      "app",
	"halt-height":                      "app",
	"halt-time":                        "app",
	"inter-block-cache":                "app",
	"index-events":                     "app",
	"iavl-disable-fastnode":            "app",
	"compaction-interval":              "app",
	"no-versioning":                    "app",
	"separate-orphan-storage":          "app",
	"separate-orphan-versions-to-keep": "app",
	"num-orphan-per-file":              "app",
	"orphan-dir":                       "app",

	// From app.toml - section headers
	"state-sync":       "app",
	"state-commit":     "app",
	"state-store":      "app",
	"evm":              "app",
	"telemetry":        "app",
	"api":              "app",
	"rosetta":          "app",
	"grpc":             "app",
	"grpc-web":         "app",
	"genesis":          "app",
	"iavl":             "app",
	"wasm":             "app",
	"eth_replay":       "app",
	"eth_blocktest":    "app",
	"evm_query":        "app",
	"light_invariance": "app",

	// From config.toml - top-level keys
	"proxy-app":     "config",
	"moniker":       "config",
	"mode":          "config",
	"db-backend":    "config",
	"db-dir":        "config",
	"log-level":     "config",
	"log-format":    "config",
	"genesis-file":  "config",
	"node-key-file": "config",
	"abci":          "config",
	"filter-peers":  "config",

	// From config.toml - section headers
	"rpc":              "config",
	"p2p":              "config",
	"mempool":          "config",
	"statesync":        "config",
	"blocksync":        "config",
	"consensus":        "config",
	"tx-index":         "config",
	"instrumentation":  "config",
	"priv-validator":   "config",
	"self-remediation": "config",
	"db-sync":          "config",

	// From client.toml - top-level keys
	"chain-id":        "client",
	"keyring-backend": "client",
	"output":          "client",
	"node":            "client",
	"broadcast-mode":  "client",
}

var configCmd = cli.Command{
	Name:  "config",
	Usage: "Manage Sei Daemon configuration files",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "target",
			Usage:       "The target TOML configuration file, one of 'app', 'client', or 'config'.",
			DefaultText: "Automatically determined on best-effort basis.",
			Destination: &destinations.config.target,
			Config: cli.StringConfig{
				TrimSpace: true,
			},
			Action: func(_ context.Context, command *cli.Command, s string) error {
				return command.Set("target", strings.ToLower(s))
			},
			Validator: func(s string) error {
				switch s {
				case "app", "client", "config":
					return nil
				default:
					return fmt.Errorf("invalid target: %s, must be one of app, client, or config", s)
				}
			},
		},
	},
	Commands: []*cli.Command{
		{
			Name:                   "patch",
			Usage:                  "Apply a merge-patch to a Sei config toml file",
			MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{outputterFlags},
			Arguments: []cli.Argument{
				&cli.StringArg{
					Name:        "file",
					Destination: &destinations.config.patch.file,
					Config: cli.StringConfig{
						TrimSpace: true,
					},
				},
			},
			Action: func(ctx context.Context, command *cli.Command) error {
				var patchBytes []byte
				if destinations.config.patch.file == "" {
					var buffer bytes.Buffer
					scanner := bufio.NewScanner(os.Stdin)
					for scanner.Scan() {
						buffer.Write(scanner.Bytes())
						buffer.WriteByte('\n')
					}
					if err := scanner.Err(); err != nil {
						return fmt.Errorf("reading input from stdin: %w", err)
					}
					patchBytes = buffer.Bytes()
				} else {
					var err error
					patchBytes, err = os.ReadFile(destinations.config.patch.file)
					if err != nil {
						return fmt.Errorf("reading patch file: %w", err)
					}
				}
				patchBytes = []byte(strings.TrimSpace(string(patchBytes)))
				if len(patchBytes) == 0 {
					return nil
				}

				patchData := make(map[string]any)
				if err := toml.Unmarshal(patchBytes, &patchData); err != nil {
					return fmt.Errorf("parsing patch: %w", err)
				}

				if destinations.home == "" {
					userHome, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("failed to get user home directory: %w", err)
					}
					destinations.home = filepath.Clean(filepath.Join(userHome, ".sei"))
				}
				if destinations.config.target == "" {
					// Attempt to auto-detect the target config using top-level keys matching hints.
					for key := range patchData {
						switch hint, found := configTargetHints[key]; {
						case !found:
							// If not found in hints but destinations.config.target is set already then we
							// take it as user intent to add some unknown keys to the detected target.
							continue
						case destinations.config.target == "":
							// Found first matching hint.
							destinations.config.target = hint
						case destinations.config.target != hint:
							// Multiple matching hits; not OK for safety reasons.
							return fmt.Errorf("patch is applicable to at least two target configurations (%s, %s). Either set target explicitly to apply the patch to a target config, or patch configurations one at a time", destinations.config.target, hint)
						}
					}
					if destinations.config.target == "" {
						return errors.New("configuration target could not be detected; it must be set explicitly")
					}
				}

				configPath := filepath.Join(destinations.home, "config", destinations.config.target+".toml")
				configBytes, err := os.ReadFile(configPath)
				if err != nil {
					return fmt.Errorf("reading config file: %w", err)
				}
				config := make(map[string]any)
				if err := toml.Unmarshal(configBytes, &config); err != nil {
					return fmt.Errorf("parsing config: %w", err)
				}

				patchedConfig := patch.Merge(config, patchData)

				var prettyPatchedConfig bytes.Buffer
				encoder := toml.NewEncoder(&prettyPatchedConfig)
				if err := encoder.Encode(patchedConfig); err != nil {
					return fmt.Errorf("marshalling patched config: %w", err)
				}
				return output(configPath, prettyPatchedConfig)
			},
		},
	},
}
