package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/seinetwork"
	"github.com/sei-protocol/seictl/seinode"
	"github.com/sei-protocol/seictl/task"
	"github.com/sei-protocol/seictl/workflow"
)

var (
	destinations = struct {
		home      string
		outputter struct {
			output  string
			inPlace bool
		}
		config struct {
			patch struct {
				file string
			}
			target string
		}
		genesis struct {
			patch struct {
				file string
			}
		}
		patch struct {
			file   string
			target string
		}
		await struct {
			timeout   time.Duration
			validator struct {
				address string
			}
		}
	}{}

	seictlCmd = cli.Command{
		Name: "seictl",
		Commands: []*cli.Command{
			&configCmd,
			&genesisCmd,
			&patchCmd,
			&awaitCmd,
			&serveCmd,
			&reportCmd,
			&seinetwork.Cmd,
			&seinode.Cmd,
			&workflow.Cmd,
			&task.Cmd,
		},
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:        "home",
				Sources:     cli.EnvVars("SEI_HOME"),
				Destination: &destinations.home,
				TakesFile:   true,
				Config: cli.StringConfig{
					TrimSpace: true,
				},
				Action: func(ctx context.Context, command *cli.Command, s string) error {
					return command.Set("home", filepath.Clean(s))
				},
			},
		},
	}
)

func main() {
	err := seictlCmd.Run(context.Background(), os.Args)
	if err == nil {
		return
	}
	// Cluster-facing verbs return cli.ExitCoder so their JSON envelope's
	// exit code reaches the OS. Other verbs return plain errors that
	// log.Fatal renders as exit 1 — preserved for backwards compat.
	if coder, ok := err.(cli.ExitCoder); ok {
		os.Exit(coder.ExitCode())
	}
	log.Fatal(err)
}
