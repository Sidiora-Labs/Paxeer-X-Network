package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sei-protocol/seictl/internal/patch"
	"github.com/urfave/cli/v3"
)

var genesisCmd = cli.Command{
	Name:  "genesis",
	Usage: "Manage Sei genesis JSON file",
	Commands: []*cli.Command{
		{
			Name:                   "patch",
			Usage:                  "Apply a merge-patch to the Sei genesis JSON file",
			MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{outputterFlags},
			Arguments: []cli.Argument{
				&cli.StringArg{
					Name:        "file",
					Destination: &destinations.genesis.patch.file,
					Config: cli.StringConfig{
						TrimSpace: true,
					},
				},
			},
			Action: func(ctx context.Context, command *cli.Command) error {
				var patchBytes []byte
				if destinations.genesis.patch.file == "" {
					// Read full multi-line input from stdin
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
					patchBytes, err = os.ReadFile(destinations.genesis.patch.file)
					if err != nil {
						return fmt.Errorf("reading patch file: %w", err)
					}
				}
				patchBytes = []byte(strings.TrimSpace(string(patchBytes)))
				if len(patchBytes) == 0 {
					return nil
				}

				patchData := make(map[string]any)
				if err := json.Unmarshal(patchBytes, &patchData); err != nil {
					return fmt.Errorf("parsing patch: %w", err)
				}

				if destinations.home == "" {
					userHome, err := os.UserHomeDir()
					if err != nil {
						return fmt.Errorf("failed to get user home directory: %w", err)
					}
					destinations.home = filepath.Clean(filepath.Join(userHome, ".sei"))
				}

				genesisPath := filepath.Join(destinations.home, "config", "genesis.json")
				genesisBytes, err := os.ReadFile(genesisPath)
				if err != nil {
					return fmt.Errorf("reading genesis file: %w", err)
				}
				genesis := make(map[string]any)
				if err := json.Unmarshal(genesisBytes, &genesis); err != nil {
					return fmt.Errorf("parsing genesis: %w", err)
				}

				patchedGenesis := patch.Merge(genesis, patchData)

				var prettyPatchedGenesis bytes.Buffer
				encoder := json.NewEncoder(&prettyPatchedGenesis)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(patchedGenesis); err != nil {
					return fmt.Errorf("marshalling patched genesis: %w", err)
				}
				return output(genesisPath, prettyPatchedGenesis)
			},
		},
	},
}
