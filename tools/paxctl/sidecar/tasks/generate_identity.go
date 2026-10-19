package tasks

import (
	"context"
	"fmt"
	"os"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"

	"github.com/sei-protocol/sei-chain/sei-cosmos/x/genutil"

	"github.com/sei-protocol/seictl/sidecar/engine"
	"github.com/sei-protocol/seilog"
)

var identityLog = seilog.NewLogger("seictl", "task", "generate-identity")

const identityMarkerFile = ".sei-sidecar-identity-done"

// GenerateIdentityRequest holds the typed parameters for the generate-identity task.
type GenerateIdentityRequest struct {
	ChainID string `json:"chainId"`
	Moniker string `json:"moniker"`
}

// IdentityGenerator creates the validator identity by calling the same
// SDK functions as seid init: genutil.InitializeNodeValidatorFilesFromMnemonic
// for keys, tmcfg.WriteConfigFile for config.toml, and genutil.ExportGenesisFile
// for genesis.json.
type IdentityGenerator struct {
	homeDir string
}

// NewIdentityGenerator creates a generator targeting the given home directory.
func NewIdentityGenerator(homeDir string) *IdentityGenerator {
	return &IdentityGenerator{homeDir: homeDir}
}

// Handler returns an engine.TaskHandler for the generate-identity task type.
//
// Expected params: {"chainId": "...", "moniker": "..."}
func (g *IdentityGenerator) Handler() engine.TaskHandler {
	return engine.TypedHandler(func(ctx context.Context, params GenerateIdentityRequest) error {
		if markerExists(g.homeDir, identityMarkerFile) {
			identityLog.Debug("already completed, skipping")
			return nil
		}

		if params.ChainID == "" {
			return fmt.Errorf("generate-identity: missing required param 'chainId'")
		}
		if params.Moniker == "" {
			return fmt.Errorf("generate-identity: missing required param 'moniker'")
		}

		identityLog.Info("generating identity", "chainId", params.ChainID, "moniker", params.Moniker)

		cfg := tmcfg.DefaultConfig()
		cfg.SetRoot(g.homeDir)
		tmcfg.EnsureRoot(g.homeDir)

		// Same call as seid init — generates node_key.json,
		// priv_validator_key.json, priv_validator_state.json.
		nodeID, _, err := genutil.InitializeNodeValidatorFilesFromMnemonic(cfg, "")
		if err != nil {
			return fmt.Errorf("generate-identity: initializing validator files: %w", err)
		}

		cfg.Moniker = params.Moniker

		if err := tmcfg.WriteConfigFile(cfg.RootDir, cfg); err != nil {
			return fmt.Errorf("generate-identity: writing config.toml: %w", err)
		}

		// If no genesis.json exists yet (seid-init container didn't run
		// or this is a standalone invocation), write a minimal one.
		// The seid-init container normally creates the full genesis with
		// all module defaults; this fallback produces a bare genesis that
		// will be populated by subsequent ceremony steps.
		genFile := cfg.GenesisFile()
		if _, err := os.Stat(genFile); os.IsNotExist(err) {
			genDoc := &tmtypes.GenesisDoc{
				ChainID:  params.ChainID,
				AppState: []byte("{}"),
			}
			if err := genutil.ExportGenesisFile(genDoc, genFile); err != nil {
				return fmt.Errorf("generate-identity: writing genesis: %w", err)
			}
		}

		identityLog.Info("identity generated", "nodeId", nodeID, "moniker", params.Moniker)
		return writeMarker(g.homeDir, identityMarkerFile)
	})
}
