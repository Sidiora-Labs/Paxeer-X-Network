package seinetwork

import (
	"context"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// networkPhases is the SeiNetworkPhase enum (seinetwork_types.go:157). A
// network reaches Ready (NOT Running — that is the SeiNode vocab); D2.
var networkPhases = []string{"Pending", "Initializing", "Ready", "Paused", "Degraded", "Failed", "Terminating"}

func watchAction(ctx context.Context, c *cli.Command) error {
	name := c.StringArg("name")
	if name == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl network watch <name>"))
		return cli.Exit("", 1)
	}
	until := c.String("until")
	if until == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--until=<phase> is required (e.g. --until=Ready)"))
		return cli.Exit("", 1)
	}
	if err := cliutil.ValidatePhase(until, networkPhases); err != nil {
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
	Usage:     "Stream SeiNetwork events as NDJSON until a phase is reached",
	ArgsUsage: "<name>",
	Description: "Streams every SeiNetwork event for <name> as one NDJSON " +
		"line on stdout, exiting 0 when .status.phase matches --until or 1 " +
		"on timeout, terminal Failed phase, or transient API error. " +
		"Legal --until phases: Pending, Initializing, Ready, Paused, " +
		"Degraded, Failed, Terminating. Discrimination on stderr via " +
		"metav1.Status.reason (Timeout, InternalError, etc.).",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "name", UsageText: "metadata.name of the SeiNetwork"},
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
			Usage:    "Phase to wait for (e.g. --until=Ready). Matches .status.phase exactly; validated against the SeiNetwork enum at parse.",
		},
		&cli.DurationFlag{
			Name:  "timeout",
			Value: 15 * time.Minute,
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
