package seinode

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

func listAction(ctx context.Context, c *cli.Command) error {
	allNS := c.Bool("all-namespaces")

	printer, err := cliutil.MakePrinter(c.String("output"))
	if err != nil {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("%s", err.Error()))
		return cli.Exit("", 1)
	}

	var sel labels.Selector
	if raw := c.String("selector"); raw != "" {
		sel, err = labels.Parse(raw)
		if err != nil {
			cliutil.EmitStatus(os.Stderr, cliutil.UsageError("parse --selector: %s", err.Error()))
			return cli.Exit("", 1)
		}
	}

	kc := cliutil.LoadKubeconfig(c.String("kubeconfig"), c.String("namespace"))
	cfg, err := kc.RESTConfig()
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	listOpts := []client.ListOption{}
	if !allNS {
		ns, err := kc.Namespace()
		if err != nil {
			cliutil.EmitStatus(os.Stderr, err)
			return cli.Exit("", 1)
		}
		listOpts = append(listOpts, client.InNamespace(ns))
	}
	if sel != nil {
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: sel})
	}

	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	list := kind.NewList()
	if err := kcli.List(ctx, list, listOpts...); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("list SeiNodes: %w", err))
		return cli.Exit("", 1)
	}
	if err := printer.PrintObj(list, os.Stdout); err != nil {
		return fmt.Errorf("print: %w", err)
	}
	return nil
}

var listCmd = cli.Command{
	Name:  "list",
	Usage: "List SeiNodes",
	Description: "Prints a List of native SeiNode CRs with full status. " +
		"The PRIMARY load-list consumer reads .items[].status.endpoint." +
		"evmJsonRpc across the follower fleet selected by " +
		"-l sei.io/seinetwork=<net>,sei.io/role=node (the object labels " +
		"`node apply --network` stamps).",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.BoolFlag{
			Name:    "all-namespaces",
			Aliases: []string{"A"},
			Usage:   "List across all namespaces; overrides --namespace",
		},
		&cli.StringFlag{
			Name:    "selector",
			Aliases: []string{"l"},
			Usage:   "Label selector (e.g. -l sei.io/seinetwork=netX,sei.io/role=node)",
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
	Action: listAction,
}
