package task

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/internal/cliutil"
	sidecar "github.com/sei-protocol/seictl/sidecar/client"
)

func getAction(ctx context.Context, c *cli.Command) error {
	id, err := uuid.Parse(c.StringArg("id"))
	if err != nil {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("id argument must be a task UUID: %s", err.Error()))
		return cli.Exit("", 1)
	}

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

	res, err := sc.GetTask(ctx, id)
	if err != nil {
		if errors.Is(err, sidecar.ErrNotFound) {
			cliutil.EmitStatus(os.Stderr, fmt.Errorf("task %s not found on node %s", id, c.String("node")))
			return cli.Exit("", 1)
		}
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	if err := printJSON(os.Stdout, res); err != nil {
		return fmt.Errorf("print: %w", err)
	}
	return nil
}

var getCmd = cli.Command{
	Name:      "get",
	Usage:     "Read one task result from a node's sidecar",
	ArgsUsage: "<id>",
	Description: "GET /v0/tasks/{id} on the target node's sidecar and print the " +
		"TaskResult as JSON, including .status, .result (the handler's " +
		"structured output), and .error when failed.",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "id", UsageText: "task UUID"},
	},
	Flags:  append([]cli.Flag{nodeFlag(true)}, commonFlags()...),
	Action: getAction,
}
