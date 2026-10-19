// Package task registers the `seictl task` verb tree, the operator-facing
// surface over each node's sidecar task API (/v0/tasks) reached directly
// through the pod's in-pod kube-rbac-proxy.
//
// It is the sibling of `seictl workflow`: where workflow manages
// SeiNodeTaskWorkflow custom resources the controller executes, task drives
// the sidecar HTTP API on one addressed pod, with the same two-path shape:
//
//   - `task get|list|delete|submit` — the raw verbs: thin wrappers over the
//     typed SidecarClient (read one/all task results, cancel-or-delete a task,
//     or POST an arbitrary task). `submit` is the generic escape hatch, the
//     analogue of `workflow apply`.
//   - `task snapshot-upload` — the paved road: submit one snapshot-upload-once
//     with a fresh task ID and poll it to a terminal state with
//     kubectl-wait-compatible exit codes. The procedure a per-(network,cluster)
//     CronJob invokes daily.
//
// Every verb addresses a single pod's sidecar. The target is either an explicit
// --node (the SeiNode / headless-service name; the sidecar is reached at
// <node>-0.<node>.<ns>.svc.cluster.local through its proxy) or, for
// snapshot-upload, discovered by label selection. Cluster + namespace resolve
// from --kubeconfig and -n exactly as the workflow and node trees do.
package task

import (
	"github.com/urfave/cli/v3"
)

// Cmd is the `seictl task` command tree.
var Cmd = cli.Command{
	Name:  "task",
	Usage: "Drive a node's sidecar task API directly (/v0/tasks through its kube-rbac-proxy)",
	Commands: []*cli.Command{
		&snapshotUploadCmd,
		&getCmd,
		&listCmd,
		&submitCmd,
		&deleteCmd,
	},
}
