package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sei-protocol/seictl/internal/patch"
	"github.com/urfave/cli/v3"
)

var outputterFlags = cli.MutuallyExclusiveFlags{
	Required: false,
	Flags: [][]cli.Flag{
		{
			&cli.StringFlag{
				Name:        "output",
				DefaultText: "STDOUT",
				Usage:       "The destination to which to write the modified configuration file.",
				Destination: &destinations.outputter.output,
				Aliases:     []string{"o"},
				TakesFile:   true,
				Action: func(ctx context.Context, command *cli.Command, s string) error {
					return command.Set("output", filepath.Clean(s))
				},
				OnlyOnce: true,
			},
		},
		{
			&cli.BoolFlag{
				Name:        "in-place-rewrite",
				Usage:       "Weather to directly replace the resulting changes in the current file. Cannot be set in conjunction with output.",
				Destination: &destinations.outputter.inPlace,
				Aliases:     []string{"i"},
				OnlyOnce:    true,
			},
		},
	},
}

func output(originalPath string, content bytes.Buffer) error {
	switch {
	case destinations.outputter.inPlace:
		stat, err := os.Stat(originalPath)
		if err != nil {
			return fmt.Errorf("failed to get config file stat %s: %w", originalPath, err)
		}
		return patch.WriteFileAtomic(originalPath, content.Bytes(), stat.Mode().Perm())
	case destinations.outputter.output != "":
		return patch.WriteFileAtomic(destinations.outputter.output, content.Bytes(), 0600)
	default:
		_, err := io.Copy(os.Stdout, &content)
		return err
	}
}
