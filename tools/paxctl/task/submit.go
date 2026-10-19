package task

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/internal/cliutil"
	sidecar "github.com/sei-protocol/seictl/sidecar/client"
)

func submitAction(ctx context.Context, c *cli.Command) error {
	taskType := c.StringArg("type")
	if taskType == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("type argument required: seictl task submit <type> --node ..."))
		return cli.Exit("", 1)
	}

	req := sidecar.TaskRequest{Type: taskType}
	if raw := c.String("params"); raw != "" {
		var params map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &params); err != nil {
			cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--params must be a JSON object: %s", err.Error()))
			return cli.Exit("", 1)
		}
		req.Params = &params
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

	id, err := sc.SubmitTask(ctx, req)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	if err := printJSON(os.Stdout, sidecar.TaskSubmitResponse{Id: id}); err != nil {
		return fmt.Errorf("print: %w", err)
	}
	return nil
}

var submitCmd = cli.Command{
	Name:      "submit",
	Usage:     "POST a raw task to a node's sidecar (generic escape hatch)",
	ArgsUsage: "<type>",
	Description: "POST /v0/tasks with an arbitrary task type and optional JSON " +
		"params, printing the assigned task ID. The raw analogue of " +
		"`workflow apply`: params are validated server-side, not by this verb. " +
		"For the daily snapshot publish use `task snapshot-upload`, which owns " +
		"fresh-ID generation and terminal polling.",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "type", UsageText: "task type identifier (e.g. config-validate)"},
	},
	Flags: append([]cli.Flag{
		nodeFlag(true),
		&cli.StringFlag{
			Name:  "params",
			Usage: "Task parameters as a JSON object (validated server-side)",
		},
	}, commonFlags()...),
	Action: submitAction,
}
