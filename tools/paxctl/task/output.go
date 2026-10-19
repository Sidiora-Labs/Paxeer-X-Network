package task

import (
	"encoding/json"
	"io"

	"github.com/urfave/cli/v3"
)

// commonFlags are the addressing + auth flags every task verb shares.
func commonFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.IntFlag{
			Name:  "port",
			Value: int(sidecarProxyPort),
			Usage: "Sidecar proxy port on the pod (the kube-rbac-proxy port, not the sidecar's :7777)",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	}
}

// nodeFlag is the explicit-target flag shared by the raw verbs (required) and
// snapshot-upload (optional, discovery fallback).
func nodeFlag(required bool) *cli.StringFlag {
	return &cli.StringFlag{
		Name:     "node",
		Required: required,
		Usage:    "Target SeiNode / headless-service name; sidecar resolves at <node>-0.<node>.<ns>",
	}
}

// printJSON writes v as indented JSON, the raw-verb output convention (mirrors
// `workflow apply`'s post-apply CR dump). Task results are plain structs, not
// runtime.Objects, so the -o ResourcePrinter path does not apply here.
func printJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
