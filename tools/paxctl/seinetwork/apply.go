package seinetwork

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
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl network apply <name> --preset ..."))
		return cli.Exit("", 1)
	}

	args := renderArgs{
		preset:           c.String("preset"),
		name:             name,
		namespace:        c.String("namespace"),
		chainID:          c.String("chain-id"),
		image:            c.String("image"),
		sets:             c.StringSlice("set"),
		genesisAccounts:  c.StringSlice("genesis-account"),
		genesisOverrides: c.StringSlice("genesis-override"),
	}
	if c.IsSet("replicas") {
		args.replicas = int(c.Int("replicas"))
		args.hasReps = true
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
	fmt.Fprintf(os.Stderr, "seictl: %s SeiNetwork %s/%s to %s\n",
		mode, obj.GetNamespace(), obj.GetName(), cfg.Host)

	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	if err := kind.Apply(ctx, kcli, obj, dryRun); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("apply SeiNetwork %s/%s: %w", obj.GetNamespace(), obj.GetName(), err))
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
	// urfave/cli's StringSliceFlag splits values on `,` by default,
	// mangling multi-denom coins and TOML list values.
	DisableSliceFlagSeparator: true,
	Usage:                     "Render a preset and server-side-apply the resulting SeiNetwork",
	Description: "Loads the named preset, applies discrete-flag and --set " +
		"overrides, and server-side-applies the result. With --dry-run, " +
		"the apiserver validates and returns the would-be-applied CR " +
		"without persisting. " +
		"\n\n" +
		"IMMUTABILITY: spec.genesis (incl. --chain-id, --genesis-account, " +
		"--genesis-override) and spec.replicas are admission-immutable. " +
		"Re-applying with a changed --chain-id or --replicas is REJECTED " +
		"(metav1.Status Invalid, exit 1), not a silent no-op — genesis and " +
		"validator-set size are network identity. To change them, delete " +
		"and re-create. " +
		"\n\n" +
		"Layering, lowest precedence first: preset YAML, discrete flags " +
		"(--chain-id, --image, --replicas), --set. " +
		"\n\n" +
		"Cluster + namespace come from --kubeconfig (or $KUBECONFIG, " +
		"or $HOME/.kube/config, or in-cluster) and -n (or the kubeconfig " +
		"context's default-namespace, or the in-cluster ServiceAccount's " +
		"namespace). seictl prints the resolved cluster + namespace on " +
		"stderr before applying. " +
		"\n\n" +
		"Output: post-apply CR on stdout as JSON. Errors on stderr as a " +
		"metav1.Status object (parse with `jq -r .reason`).",
	ArgsUsage: "<name>",
	Arguments: []cli.Argument{
		&cli.StringArg{Name: "name", UsageText: "metadata.name of the SeiNetwork"},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "preset",
			Usage:    "Preset name (genesis-chain)",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.StringFlag{
			Name:  "chain-id",
			Usage: "Chain ID — sets spec.genesis.chainId. Required. Admission-immutable after create.",
		},
		&cli.StringFlag{
			Name:  "image",
			Usage: "seid container image (sets spec.image; required by all v1 presets)",
		},
		&cli.IntFlag{
			Name:  "replicas",
			Usage: "Genesis validator count (overrides preset default 4). Admission-immutable after create — minted into genesis state.",
		},
		&cli.StringSliceFlag{
			Name:  "set",
			Usage: "Strategic-merge override, dotted path with optional list-index suffix (e.g. --set spec.image=foo, --set spec.configOverrides.evm.http_port=8545). Wins on collision with discrete flags. Repeatable.",
		},
		&cli.StringSliceFlag{
			Name:  "genesis-account",
			Usage: "Append a GenesisAccount to spec.genesis.accounts: --genesis-account <address>:<balance> (e.g. --genesis-account sei1abc...:1000000000usei). Balance accepts the standard cosmos coin format (comma-separated denominations). Repeatable. --set spec.genesis.accounts[N]... overrides on collision.",
		},
		&cli.StringSliceFlag{
			Name:  "genesis-override",
			Usage: `Set a key in spec.genesis.overrides: --genesis-override <module.field[.field...]>=<value> (e.g. --genesis-override staking.params.unbonding_time=600s). Keys must be dotted cosmos-module paths — the first segment is a module in app_state (staking, bank, gov, ...). Values parse as JSON when possible (numbers, bools, objects); otherwise as strings. To force a numeric-looking value to render as string, wrap in JSON quotes (e.g. --genesis-override foo.bar='"42"'). Repeatable. --set cannot reach this map because its parser splits on every dot.`,
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
