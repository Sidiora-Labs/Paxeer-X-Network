package workflow

import (
	"context"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// workflowPhases is the SeiNodeTaskWorkflowPhase enum
// (seinodetaskworkflow_types.go). Complete is the terminal-success phase; a
// workflow never reaches Running-node vocab like Initializing.
var workflowPhases = []string{"Pending", "Running", "Complete", "Failed"}

func watchAction(ctx context.Context, c *cli.Command) error {
	name := c.StringArg("name")
	if name == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl workflow watch <name>"))
		return cli.Exit("", 1)
	}
	until := c.String("until")
	if until == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--until=<phase> is required (e.g. --until=Complete)"))
		return cli.Exit("", 1)
	}
	if err := cliutil.ValidatePhase(until, workflowPhases); err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	timeout := c.Duration("timeout")

	kc := cliutil.LoadKubeconfig(c.String("kubeconfig"), c.String("namespace"))
	cfg, err := kc.RESTConfig()
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	ns, err := kc.Namespace()
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	if err := cliutil.RunWatch(ctx, cfg, kind.GVR, ns, name, until, timeout, os.Stdout); err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	return nil
}

var watchCmd = cli.Command{
	Name:      "watch",
	Usage:     "Stream SeiNodeTaskWorkflow events as NDJSON until a phase is reached",
	ArgsUsage: "<name>",
	Description: "Streams every workflow event for <name> as one NDJSON line on " +
		"stdout, exiting 0 when .status.phase matches --until or nonzero on " +
		"timeout, terminal Failed phase, or transient API error. " +
		"Legal --until phases: Pending, Running, Complete, Failed. " +
		"--until=Complete is the kubectl-wait-compatible success gate. " +
		"Discrimination on stderr via metav1.Status.reason (Timeout, " +
		"InternalError, etc.); on a Failed workflow the message carries " +
		"status.plan.failedTaskDetail.error.",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "name", UsageText: "metadata.name of the SeiNodeTaskWorkflow"},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.StringFlag{
			Name:     "until",
			Required: true,
			Usage:    "Phase to wait for (e.g. --until=Complete). Matches .status.phase exactly; validated against the SeiNodeTaskWorkflow enum at parse.",
		},
		&cli.DurationFlag{
			Name:  "timeout",
			Value: 60 * time.Minute,
			Usage: "Watch timeout; exits with metav1.Status reason=Timeout when exceeded",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	},
	Action: watchAction,
}
