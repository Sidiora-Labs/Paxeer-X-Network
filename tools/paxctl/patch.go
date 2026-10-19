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

	"github.com/pelletier/go-toml/v2"
	"github.com/sei-protocol/seictl/internal/patch"
	"github.com/urfave/cli/v3"
)

var patchCmd = cli.Command{
	Name:                   "patch",
	Usage:                  "Apply a merge-patch to any TOML or JSON file",
	MutuallyExclusiveFlags: []cli.MutuallyExclusiveFlags{outputterFlags},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "target",
			Usage:       "Path to TOML or JSON file to patch. The file extension is used to determine the format.",
			Destination: &destinations.patch.target,
			Config: cli.StringConfig{
				TrimSpace: true,
			},
			Action: func(_ context.Context, command *cli.Command, s string) error {
				return command.Set("target", filepath.Clean(s))
			},
			Required: true,
		},
	},
	Arguments: []cli.Argument{
		&cli.StringArg{
			Name:        "file",
			Destination: &destinations.patch.file,
			Config: cli.StringConfig{
				TrimSpace: true,
			},
		},
	},
	Action: func(ctx context.Context, command *cli.Command) error {
		targetExt := strings.ToLower(filepath.Ext(destinations.patch.target))
		if targetExt != ".toml" && targetExt != ".json" {
			return fmt.Errorf("unsupported target file extension: %s (must be .toml or .json)", targetExt)
		}

		var patchBytes []byte
		if destinations.patch.file == "" {
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
			patchBytes, err = os.ReadFile(destinations.patch.file)
			if err != nil {
				return fmt.Errorf("reading patch file: %w", err)
			}
		}
		patchBytes = []byte(strings.TrimSpace(string(patchBytes)))
		if len(patchBytes) == 0 {
			return nil
		}

		targetBytes, err := os.ReadFile(destinations.patch.target)
		if err != nil {
			return fmt.Errorf("reading target file: %w", err)
		}

		target := make(map[string]any)
		patchData := make(map[string]any)

		switch targetExt {
		case ".toml":
			if err := toml.Unmarshal(patchBytes, &patchData); err != nil {
				return fmt.Errorf("parsing patch as TOML: %w", err)
			}
			if err := toml.Unmarshal(targetBytes, &target); err != nil {
				return fmt.Errorf("parsing target as TOML: %w", err)
			}
		case ".json":
			if err := json.Unmarshal(patchBytes, &patchData); err != nil {
				return fmt.Errorf("parsing patch as JSON: %w", err)
			}
			if err := json.Unmarshal(targetBytes, &target); err != nil {
				return fmt.Errorf("parsing target as JSON: %w", err)
			}
		}

		patchedTarget := patch.Merge(target, patchData)

		// Marshal the patched result
		var patchedTargetBuffer bytes.Buffer
		switch targetExt {
		case ".toml":
			encoder := toml.NewEncoder(&patchedTargetBuffer)
			if err := encoder.Encode(patchedTarget); err != nil {
				return fmt.Errorf("marshalling patched target as TOML: %w", err)
			}
		case ".json":
			encoder := json.NewEncoder(&patchedTargetBuffer)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(patchedTarget); err != nil {
				return fmt.Errorf("marshalling patched target as JSON: %w", err)
			}
		}

		return output(destinations.patch.target, patchedTargetBuffer)
	},
}
