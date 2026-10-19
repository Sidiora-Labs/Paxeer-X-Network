package seinode

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
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl node apply <name> --preset ..."))
		return cli.Exit("", 1)
	}

	args := renderArgs{
		preset:          c.String("preset"),
		name:            name,
		namespace:       c.String("namespace"),
		chainID:         c.String("chain-id"),
		image:           c.String("image"),
		network:         c.String("network"),
		externalAddress: c.String("external-address"),
		sets:            c.StringSlice("set"),
		overrides:       c.StringSlice("override"),
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
	fmt.Fprintf(os.Stderr, "seictl: %s SeiNode %s/%s to %s\n",
		mode, obj.GetNamespace(), obj.GetName(), cfg.Host)

	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	if err := kind.Apply(ctx, kcli, obj, dryRun); err != nil {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("apply SeiNode %s/%s: %w", obj.GetNamespace(), obj.GetName(), err))
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
	Usage:                     "Render a preset and server-side-apply the resulting SeiNode",
	Description: "Loads the named preset, applies discrete-flag and --set " +
		"overrides, and server-side-applies the result. With --dry-run, " +
		"the apiserver validates and returns the would-be-applied CR " +
		"without persisting. " +
		"\n\n" +
		"A SeiNode is a SINGLE node — there is no spec.replicas. An RPC " +
		"fleet of N is N distinct `node apply` invocations. " +
		"\n\n" +
		"--network <X> auto-wires spec.peers[].label.selector to " +
		"sei.io/seinetwork=<X> (peer with that network's validators) and " +
		"stamps the metadata.labels (sei.io/seinetwork=<X>, sei.io/role=node) " +
		"that `node list -l` selects on. " +
		"\n\n" +
		"Layering, lowest precedence first: preset YAML, discrete flags " +
		"(--chain-id, --image, --network, --external-address), --override, " +
		"--set. " +
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
		&cli.StringArg{Name: "name", UsageText: "metadata.name of the SeiNode"},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "preset",
			Usage:    "Preset name (rpc)",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "namespace",
			Aliases: []string{"n"},
			Usage:   "Target namespace (defaults to kubeconfig context or in-cluster SA)",
		},
		&cli.StringFlag{
			Name:  "chain-id",
			Usage: "Chain ID — sets spec.chainId. Required by all v1 presets.",
		},
		&cli.StringFlag{
			Name:  "image",
			Usage: "seid container image (sets spec.image; required by all v1 presets)",
		},
		&cli.StringFlag{
			Name:  "network",
			Usage: "SeiNetwork to peer with — derives spec.peers[].label.selector (sei.io/seinetwork=<X>) and stamps the metadata.labels `node list -l` matches on. Required for a peering full node unless --set spec.peers... is given.",
		},
		&cli.StringFlag{
			Name:  "external-address",
			Usage: "Routable P2P host:port written to spec.externalAddress. Leave unset for in-cluster nodes (headless DNS is self-configuring); set for cross-cluster/sentry peers.",
		},
		&cli.StringSliceFlag{
			Name:  "set",
			Usage: "Strategic-merge override, dotted path with optional list-index suffix (e.g. --set spec.image=foo, --set spec.peers[0].label.namespace=other-ns). Wins on collision with discrete flags. Repeatable.",
		},
		&cli.StringSliceFlag{
			Name:  "override",
			Usage: "Set a key in spec.overrides: --override <toml-path>=<value> (e.g. --override evm.enabled_legacy_sei_apis=sei_getLogs,sei_getBlockByNumber). Keys are dotted TOML paths consumed by the controller's config-apply pipeline; --set cannot reach this map because its parser splits on every dot. Repeatable.",
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
