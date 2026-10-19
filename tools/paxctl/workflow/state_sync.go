package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/urfave/cli/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// stateSyncPreset is the embedded preset the state-sync command renders.
const stateSyncPreset = "state-sync"

// phaseComplete is the SeiNodeTaskWorkflow terminal-success phase the paved-road
// command waits for. Reaching it exits 0; the Failed dual exits nonzero (via
// cliutil.MatchPhase), giving kubectl-wait-compatible semantics.
const phaseComplete = "Complete"

// phaseFailed is the SeiNodeTaskWorkflow terminal-failure phase.
const phaseFailed = "Failed"

// forceDeleteAnnotation mirrors the controller's
// v1alpha1.WorkflowForceDeleteAnnotation. seictl does not import the controller
// api, so the published API string is duplicated here for the refusal guidance.
const forceDeleteAnnotation = "sei.io/force-delete-workflow"

// preflightPhaseError returns an actionable refusal when a same-named workflow
// already sits in a terminal phase, and nil otherwise. Re-running is not an
// in-place edit: spec params are immutable (CEL rejects a change), a no-op SSA
// leaves the stale terminal status in place, and the list-first watch would
// then match it — exiting 0 on a stale Complete, or replaying a stale failure
// on Failed — having done nothing.
func preflightPhaseError(ns, name, phase string) error {
	switch phase {
	case phaseComplete:
		return cliutil.UsageError(
			"workflow %s/%s is already Complete (its hold released on completion). "+
				"Delete it (`seictl workflow delete %s`) and re-run, or use --name for a fresh run.",
			ns, name, name)
	case phaseFailed:
		return cliutil.UsageError(
			"workflow %s/%s previously Failed and still holds the node. Recovery is a "+
				"force-delete (annotate it %s=<reason>, see docs) then re-run, or --name for a fresh run.",
			ns, name, forceDeleteAnnotation)
	}
	return nil
}

// preflight refuses to watch when a same-named workflow is already terminal.
// A NotFound (first run) or a non-terminal phase (join an in-progress run)
// passes.
func preflight(ctx context.Context, kcli client.Client, ns, name string) error {
	obj := kind.New()
	err := kcli.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("preflight get SeiNodeTaskWorkflow %s/%s: %w", ns, name, err)
	}
	phase, _, _ := unstructured.NestedString(obj.Object, "status", "phase")
	return preflightPhaseError(ns, name, phase)
}

func stateSyncAction(ctx context.Context, c *cli.Command) error {
	node := c.StringArg("node")
	if node == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("node argument required: seictl workflow state-sync <node>"))
		return cli.Exit("", 1)
	}
	name := c.String("name")
	if name == "" {
		name = node + "-state-sync"
	}

	if cp := c.String("config-patch"); cp != "" {
		cliutil.EmitStatus(os.Stderr, configPatchRemovedError())
		return cli.Exit("", 1)
	}

	args := renderArgs{
		preset:       stateSyncPreset,
		name:         name,
		namespace:    c.String("namespace"),
		target:       node,
		requirePhase: c.String("require-phase"),
		migration:    c.String("migration"),
		backend:      c.String("backend"),
		rpcServers:   c.StringSlice("rpc-servers"),
		sets:         c.StringSlice("set"),
	}
	dryRun := c.Bool("dry-run")
	noWatch := c.Bool("no-watch")
	timeout := c.Duration("timeout")

	kc := cliutil.LoadKubeconfig(c.String("kubeconfig"), args.namespace)
	cfg, err := kc.RESTConfig()
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	resolvedNS, err := kc.Namespace()
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	args.namespace = resolvedNS

	obj, err := render(args)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	mode := "applying"
	if dryRun {
		mode = "applying (dry-run)"
	}
	fmt.Fprintf(os.Stderr, "seictl: %s SeiNodeTaskWorkflow %s/%s (state-sync target=%s) to %s\n",
		mode, obj.GetNamespace(), obj.GetName(), node, cfg.Host)
	if args.migration != "" {
		emitMigrationPreamble(os.Stderr, args.migration, args.backend, node)
	}

	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	// Refuse a re-run over a same-named terminal workflow before touching it:
	// the no-op SSA + list-first watch would false-green on a stale Complete or
	// replay a stale Failed. Only guards the watch path (dry-run/--no-watch do
	// not watch, so there is nothing to false-green).
	if !dryRun && !noWatch {
		if err := preflight(ctx, kcli, resolvedNS, name); err != nil {
			cliutil.EmitStatus(os.Stderr, err)
			return cli.Exit("", 1)
		}
	}

	if err := kind.Apply(ctx, kcli, obj, dryRun); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("apply SeiNodeTaskWorkflow %s/%s: %w", obj.GetNamespace(), obj.GetName(), err))
		return cli.Exit("", 1)
	}

	// dry-run and --no-watch stop after apply, emitting the CR for inspection or
	// GitOps capture; the watch path streams plan progress instead.
	if dryRun || noWatch {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(obj.Object); err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
		return nil
	}

	// Gate the watch on the controller observing at least the generation this
	// apply produced, so a terminal phase left by a prior run is never honored
	// until the controller re-confirms it for this apply (kills the false-green
	// and the re-run TOCTOU). SSA stamped the live generation onto obj.
	appliedGen := obj.GetGeneration()
	fmt.Fprintf(os.Stderr, "seictl: watching %s/%s until %s at generation >= %d (timeout %s)\n",
		resolvedNS, name, phaseComplete, appliedGen, timeout)
	if err := cliutil.RunWatchGen(ctx, cfg, kind.GVR, resolvedNS, name, phaseComplete, appliedGen, timeout, os.Stdout); err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	emitCompleteHandoff(os.Stderr, resolvedNS, node)
	return nil
}

// emitCompleteHandoff hands the operator the node-side catch-up check on
// workflow Complete. Complete means the recipe's mutations landed and the node
// was released; the resync runs after Complete, so catch-up is the operator's
// verification, via the SDK-backed readiness watch.
func emitCompleteHandoff(w io.Writer, ns, node string) {
	_, _ = fmt.Fprintf(w, "seictl: workflow Complete: node %s released, seid is restarting and the resync begins now\n", node)
	_, _ = fmt.Fprintf(w, "seictl: Complete means the recipe's mutations landed, not that the node caught up. Verify catch-up:\n")
	_, _ = fmt.Fprintf(w, "seictl:   seictl node watch %s --until=caught-up -n %s\n", node, ns)
}

var stateSyncCmd = cli.Command{
	Name: "state-sync",
	// urfave/cli's StringSliceFlag splits values on `,` by default, which would
	// mangle repeatable values (a --set TOML list, or witness endpoints passed
	// to --rpc-servers). Same precedent as `node apply`.
	DisableSliceFlagSeparator: true,
	Usage:                     "Re-bootstrap a node through CometBFT state sync and watch it to release",
	Description: "Renders the StateSync recipe against <node> from the embedded " +
		"state-sync preset, server-side-applies the resulting " +
		"SeiNodeTaskWorkflow, then streams its plan progress as NDJSON on " +
		"stdout until the workflow reaches a terminal phase. " +
		"\n\n" +
		"Complete means the recipe's mutations landed and the node was " +
		"released to re-bootstrap; the resync itself runs after Complete, and " +
		"the command prints the node-side catch-up check on success. A Failed " +
		"workflow always means the node is still held. " +
		"\n\n" +
		"Exit codes are kubectl-wait-compatible: 0 when .status.phase reaches " +
		"Complete, nonzero on Failed or --timeout. " +
		"\n\n" +
		"The workflow is named <node>-state-sync unless --name is given. " +
		"The common case is a standard resync (no migration flags). " +
		"--migration GigaStore --backend <pebbledb|rocksdb> instead requests a " +
		"typed store migration: a DESTRUCTIVE, irreversible, slow wipe-and-resync " +
		"that discards local state and re-bootstraps on the chosen backend — both " +
		"tokens are required. --rpc-servers overrides witness resolution (>=2 or " +
		"the controller fails the plan closed). " +
		"\n\n" +
		"Re-run semantics: spec params are immutable, so re-running over a " +
		"same-named workflow already in a terminal phase is refused — delete a " +
		"Complete run, force-delete a Failed one (it holds the node), or pass " +
		"--name for a fresh run. " +
		"\n\n" +
		"Cluster + namespace resolve from --kubeconfig and -n exactly as " +
		"`node apply`; the resolved target prints on stderr before applying.",
	ArgsUsage: "<node>",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "node", UsageText: "metadata.name of the target SeiNode"},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.StringFlag{
			Name:  "name",
			Usage: "metadata.name of the workflow CR (default: <node>-state-sync)",
		},
		&cli.StringFlag{
			Name:  "migration",
			Usage: "Request a typed store migration instead of a standard resync (kind: GigaStore). DESTRUCTIVE, irreversible, and slow: wipes local state and re-bootstraps. Requires --backend. Omit for the common standard-resync case.",
		},
		&cli.StringFlag{
			Name:  "backend",
			Usage: "Target store backend for --migration (pebbledb|rocksdb). Required with --migration; a standard resync takes no backend.",
		},
		&cli.StringFlag{
			Name:  "config-patch",
			Usage: "REMOVED: config patching is now a typed migration. Use --migration GigaStore --backend <pebbledb|rocksdb>. Passing a value is an error.",
		},
		&cli.StringSliceFlag{
			Name:  "rpc-servers",
			Usage: "State-sync witness RPC endpoints (host:port). Overrides the node's resolved syncers; must carry >=2 or the plan refuses to compile. Repeatable.",
		},
		&cli.StringFlag{
			Name:  "require-phase",
			Usage: "SeiNode phase the target must reach before the workflow dispatches (default Running, per the CRD)",
		},
		&cli.StringSliceFlag{
			Name:  "set",
			Usage: "Strategic-merge override, dotted path with optional list-index suffix. Repeatable.",
		},
		&cli.BoolFlag{
			Name:  "no-watch",
			Usage: "Apply and exit without watching (print the applied CR)",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Validate via server-side-apply dry-run and emit the would-be-applied CR without persisting or watching",
		},
		&cli.DurationFlag{
			Name:  "timeout",
			Value: 15 * time.Minute,
			Usage: "Watch timeout; exits with metav1.Status reason=Timeout when exceeded. The watch ends when the workflow releases the node (minutes on a healthy node; reset-data on a very large data directory is the slow step), so a timeout usually means a wedged recipe step. Raise it for archive-scale data directories, where reset-data can legitimately run long.",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	},
	Action: stateSyncAction,
}
