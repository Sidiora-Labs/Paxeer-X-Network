package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

func applyAction(ctx context.Context, c *cli.Command) error {
	name := c.StringArg("name")
	if name == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl workflow apply <name> --target ..."))
		return cli.Exit("", 1)
	}

	if cp := c.String("config-patch"); cp != "" {
		cliutil.EmitStatus(os.Stderr, configPatchRemovedError())
		return cli.Exit("", 1)
	}

	args := renderArgs{
		preset:       c.String("preset"),
		name:         name,
		namespace:    c.String("namespace"),
		target:       c.String("target"),
		requirePhase: c.String("require-phase"),
		migration:    c.String("migration"),
		backend:      c.String("backend"),
		rpcServers:   c.StringSlice("rpc-servers"),
		sets:         c.StringSlice("set"),
	}
	dryRun := c.Bool("dry-run")

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
	fmt.Fprintf(os.Stderr, "seictl: %s SeiNodeTaskWorkflow %s/%s to %s\n",
		mode, obj.GetNamespace(), obj.GetName(), cfg.Host)
	if args.migration != "" {
		emitMigrationPreamble(os.Stderr, args.migration, args.backend, args.target)
	}

	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	if err := kind.Apply(ctx, kcli, obj, dryRun); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("apply SeiNodeTaskWorkflow %s/%s: %w", obj.GetNamespace(), obj.GetName(), err))
		return cli.Exit("", 1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obj.Object); err != nil {
		return fmt.Errorf("encode result: %w", err)
	}
	return nil
}

var applyCmd = cli.Command{
	Name: "apply",
	// urfave/cli's StringSliceFlag splits values on `,` by default, which would
	// mangle repeatable values (a --set TOML list, or witness endpoints passed
	// to --rpc-servers). Same precedent as `node apply`.
	DisableSliceFlagSeparator: true,
	Usage:                     "Render a preset and server-side-apply the resulting SeiNodeTaskWorkflow",
	Description: "The raw-resource GitOps path: loads the named preset, applies " +
		"--target and recipe-parameter flags, and server-side-applies the " +
		"result without watching (contrast `workflow state-sync`, which also " +
		"watches to completion). With --dry-run the apiserver validates and " +
		"returns the would-be-applied CR without persisting. " +
		"\n\n" +
		"Output: post-apply CR on stdout as JSON. Errors on stderr as a " +
		"metav1.Status object (parse with `jq -r .reason`).",
	ArgsUsage: "<name>",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "name", UsageText: "metadata.name of the SeiNodeTaskWorkflow"},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "preset",
			Usage: "Preset name (state-sync)",
			Value: stateSyncPreset,
		},
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.StringFlag{
			Name:     "target",
			Usage:    "metadata.name of the target SeiNode (sets spec.target.nodeRef.name)",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "migration",
			Usage: "Request a typed store migration on the StateSync recipe (kind: GigaStore). DESTRUCTIVE, irreversible, slow. Requires --backend.",
		},
		&cli.StringFlag{
			Name:  "backend",
			Usage: "Target store backend for --migration (pebbledb|rocksdb). Required with --migration.",
		},
		&cli.StringFlag{
			Name:  "config-patch",
			Usage: "REMOVED: config patching is now a typed migration. Use --migration GigaStore --backend <pebbledb|rocksdb>. Passing a value is an error.",
		},
		&cli.StringSliceFlag{
			Name:  "rpc-servers",
			Usage: "State-sync witness RPC endpoints (host:port); overrides the node's resolved syncers. Repeatable.",
		},
		&cli.StringFlag{
			Name:  "require-phase",
			Usage: "SeiNode phase the target must reach before dispatch (default Running, per the CRD)",
		},
		&cli.StringSliceFlag{
			Name:  "set",
			Usage: "Strategic-merge override, dotted path with optional list-index suffix. Repeatable.",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Validate via server-side-apply dry-run and emit the would-be-applied CR without persisting",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	},
	Action: applyAction,
}
