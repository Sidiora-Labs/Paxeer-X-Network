package task

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

func listAction(ctx context.Context, c *cli.Command) error {
	cfg, ns, err := resolveKube(c)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	sc, err := newSidecarClient(cfg, ns, c.String("node"), int32(c.Int("port")))
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	results, err := sc.ListTasks(ctx)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	if err := printJSON(os.Stdout, results); err != nil {
		return fmt.Errorf("print: %w", err)
	}
	return nil
}

var listCmd = cli.Command{
	Name:  "list",
	Usage: "List recent task results from a node's sidecar",
	Description: "GET /v0/tasks on the target node's sidecar and print the recent " +
		"TaskResults as a JSON array.",
	Flags:  append([]cli.Flag{nodeFlag(true)}, commonFlags()...),
	Action: listAction,
}
