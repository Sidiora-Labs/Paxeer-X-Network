package seinode

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

func parseCascade(raw string) (*metav1.DeletionPropagation, error) {
	switch raw {
	case "", "foreground":
		p := metav1.DeletePropagationForeground
		return &p, nil
	case "background":
		p := metav1.DeletePropagationBackground
		return &p, nil
	case "orphan":
		p := metav1.DeletePropagationOrphan
		return &p, nil
	}
	return nil, fmt.Errorf("invalid --cascade %q (use foreground|background|orphan)", raw)
}

func deleteAction(ctx context.Context, c *cli.Command) error {
	name := c.StringArg("name")
	if name == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl node delete <name>"))
		return cli.Exit("", 1)
	}

	cascade, err := parseCascade(c.String("cascade"))
	if err != nil {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("%s", err.Error()))
		return cli.Exit("", 1)
	}

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

	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	obj := kind.New()
	obj.SetName(name)
	obj.SetNamespace(ns)
	if err := kcli.Delete(ctx, obj, &client.DeleteOptions{PropagationPolicy: cascade}); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("delete SeiNode %s/%s: %w", ns, name, err))
		return cli.Exit("", 1)
	}
	fmt.Printf("seinode.sei.io/%s deleted\n", name)
	return nil
}

var deleteCmd = cli.Command{
	Name:      "delete",
	Usage:     "Delete a SeiNode by name",
	ArgsUsage: "<name>",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "name", UsageText: "metadata.name of the SeiNode"},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.StringFlag{
			Name:  "cascade",
			Value: "foreground",
			Usage: "Client delete propagation policy: foreground (default) | background | orphan",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	},
	Action: deleteAction,
}
