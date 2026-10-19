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

func deleteAction(ctx context.Context, c *cli.Command) error {
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

	if err := sc.DeleteTask(ctx, id); err != nil {
		if errors.Is(err, sidecar.ErrNotFound) {
			cliutil.EmitStatus(os.Stderr, fmt.Errorf("task %s not found on node %s", id, c.String("node")))
			return cli.Exit("", 1)
		}
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	fmt.Printf("task %s deleted\n", id)
	return nil
}

var deleteCmd = cli.Command{
	Name:      "delete",
	Usage:     "Delete a task result, or cancel it if still running",
	ArgsUsage: "<id>",
	Description: "DELETE /v0/tasks/{id} on the target node's sidecar. A terminal " +
		"task's result row is removed; a still-running task is cancelled (its " +
		"context is signalled) and then removed.",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "id", UsageText: "task UUID"},
	},
	Flags:  append([]cli.Flag{nodeFlag(true)}, commonFlags()...),
	Action: deleteAction,
}
