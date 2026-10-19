// Package workflow registers the `seictl workflow` verb tree, which manages
// SeiNodeTaskWorkflow (seinodetaskworkflows.sei.io/v1alpha1) custom resources.
//
// A SeiNodeTaskWorkflow is a pure request object: the SeiNode controller adopts
// it and is its single executor. This tree offers two paths onto that surface:
//
//   - `workflow state-sync <node>` — the paved road: render the StateSync recipe
//     against a target node from an embedded preset, apply it, and watch its
//     per-step plan progress to a terminal phase with kubectl-wait-compatible
//     exit codes (0 on Complete, nonzero on Failed/timeout).
//   - `workflow apply|get|list|watch|delete` — the raw-resource verbs for the
//     GitOps path (render/apply a preset, or read/watch/delete a CR by name).
package workflow

import (
	"github.com/urfave/cli/v3"
)

// Cmd is the `seictl workflow` command tree.
var Cmd = cli.Command{
	Name:  "workflow",
	Usage: "Manage SeiNodeTaskWorkflow custom resources (composable node recipes)",
	Commands: []*cli.Command{
		&stateSyncCmd,
		&applyCmd,
		&getCmd,
		&listCmd,
		&watchCmd,
		&deleteCmd,
	},
}
