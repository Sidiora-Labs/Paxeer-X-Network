package seinode

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

func getAction(ctx context.Context, c *cli.Command) error {
	name := c.StringArg("name")
	if name == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl node get <name>"))
		return cli.Exit("", 1)
	}

	printer, err := cliutil.MakePrinter(c.String("output"))
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
	if err := kcli.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, obj); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("get SeiNode %s/%s: %w", ns, name, err))
		return cli.Exit("", 1)
	}
	if err := printer.PrintObj(obj, os.Stdout); err != nil {
		return fmt.Errorf("print: %w", err)
	}
	return nil
}

var getCmd = cli.Command{
	Name:      "get",
	Usage:     "Read a SeiNode by name",
	ArgsUsage: "<name>",
	Description: "Prints the native SeiNode CR. With -o json the full " +
		".status.endpoint leaf (evmJsonRpc, evmWs, tendermintRpc, " +
		"tendermintRest) rides through unprojected for fullNode/archive " +
		"nodes; it is absent for validator/replayer (jq -e fails closed).",
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
			Name:    "output",
			Aliases: []string{"o"},
			Usage:   "Output format: yaml (default) | json | name | jsonpath=<template>",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	},
	Action: getAction,
}
