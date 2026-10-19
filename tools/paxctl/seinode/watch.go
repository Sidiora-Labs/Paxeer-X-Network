package seinode

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/urfave/cli/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sei-protocol/sei-k8s-controller/sdk/sei"
	"github.com/sei-protocol/seictl/internal/cliutil"
)

// nodePhases is the SeiNodePhase enum (seinode_types.go:251). A SeiNode
// reaches Running, NOT Ready — `watch --until=Ready` against a node is a
// usage error, not a 15m timeout (LLD §1.5 / D2).
var nodePhases = []string{"Pending", "Initializing", "Running", "Failed", "Terminating"}

// caughtUp is a readiness sentinel for --until (not a phase): wait for Running,
// then gate on serve-readiness — the node has joined consensus and is caught up
// (TM /status height>1 && catching_up==false) and, if it serves EVM, the EVM
// JSON-RPC listener is bound. The gating logic is the SDK's shared readiness
// primitive, so this replaces the nightly's bespoke curl/jq catching_up loop.
const caughtUp = "caught-up"

func watchAction(ctx context.Context, c *cli.Command) error {
	name := c.StringArg("name")
	if name == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("name argument required: seictl node watch <name>"))
		return cli.Exit("", 1)
	}
	until := c.String("until")
	if until == "" {
		cliutil.EmitStatus(os.Stderr, cliutil.UsageError("--until=<phase|caught-up> is required (e.g. --until=Running)"))
		return cli.Exit("", 1)
	}
	if until != caughtUp {
		if err := cliutil.ValidatePhase(until, nodePhases); err != nil {
			cliutil.EmitStatus(os.Stderr, err)
			return cli.Exit("", 1)
		}
	}
	timeout := c.Duration("timeout")

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

	if until == caughtUp {
		return watchCaughtUp(ctx, cfg, ns, name, timeout)
	}
	if err := cliutil.RunWatch(ctx, cfg, kind.GVR, ns, name, until, timeout, os.Stdout); err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	return nil
}

// watchCaughtUp waits for Running, then gates on the SDK serve-readiness probe
// against the node's published endpoints. `timeout` bounds the whole sequence.
func watchCaughtUp(ctx context.Context, cfg *rest.Config, ns, name string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := cliutil.RunWatch(ctx, cfg, kind.GVR, ns, name, "Running", timeout, os.Stdout); err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}

	tmRPC, evmRPC, err := nodeEndpoint(ctx, cfg, ns, name)
	if err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	if tmRPC == "" {
		cliutil.EmitStatus(os.Stderr, fmt.Errorf("SeiNode %s/%s Running but .status.endpoint.tendermintRpc is empty", ns, name))
		return cli.Exit("", 1)
	}
	if err := sei.WaitCaughtUp(ctx, nil, tmRPC); err != nil {
		cliutil.EmitStatus(os.Stderr, err)
		return cli.Exit("", 1)
	}
	if evmRPC != "" {
		if err := sei.WaitEVMServing(ctx, nil, evmRPC); err != nil {
			cliutil.EmitStatus(os.Stderr, err)
			return cli.Exit("", 1)
		}
	}
	return nil
}

// nodeEndpoint reads the node's published TM and EVM JSON-RPC URLs off
// .status.endpoint (verbatim — never reconstructed).
func nodeEndpoint(ctx context.Context, cfg *rest.Config, ns, name string) (tmRPC, evmRPC string, err error) {
	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		return "", "", err
	}
	obj := kind.New()
	if err := kcli.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, obj); err != nil {
		return "", "", fmt.Errorf("get SeiNode %s/%s: %w", ns, name, err)
	}
	tmRPC, evmRPC = endpointsFrom(obj)
	return tmRPC, evmRPC, nil
}

// endpointsFrom reads the TM and EVM JSON-RPC URLs off .status.endpoint. Absent
// fields (a validator/replayer node, or pre-publish) read as "".
func endpointsFrom(obj *unstructured.Unstructured) (tmRPC, evmRPC string) {
	tmRPC, _, _ = unstructured.NestedString(obj.Object, "status", "endpoint", "tendermintRpc")
	evmRPC, _, _ = unstructured.NestedString(obj.Object, "status", "endpoint", "evmJsonRpc")
	return tmRPC, evmRPC
}

var watchCmd = cli.Command{
	Name:      "watch",
	Usage:     "Stream SeiNode events as NDJSON until a phase (or caught-up) is reached",
	ArgsUsage: "<name>",
	Description: "Streams every SeiNode event for <name> as one NDJSON line " +
		"on stdout, exiting 0 when .status.phase matches --until or 1 on " +
		"timeout, terminal Failed phase, or transient API error. " +
		"Legal --until phases: Pending, Initializing, Running, Failed, " +
		"Terminating. A SeiNode reaches Running (there is no Ready). " +
		"--until=caught-up waits for Running then gates on serve-readiness " +
		"(TM caught_up + EVM serving) via the SDK — no bash curl/jq probe. " +
		"Discrimination on stderr via metav1.Status.reason (Timeout, " +
		"InternalError, etc.).",
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
			Name:     "until",
			Required: true,
			Usage:    "Phase to wait for (e.g. --until=Running), or caught-up for serve-readiness. Matches .status.phase exactly; validated against the SeiNode enum at parse.",
		},
		&cli.DurationFlag{
			Name:  "timeout",
			Value: 15 * time.Minute,
			Usage: "Watch timeout; exits with metav1.Status reason=Timeout when exceeded",
		},
		&cli.StringFlag{
			Name:    "kubeconfig",
			Sources: cli.EnvVars("KUBECONFIG"),
			Usage:   "Path to kubeconfig (honors KUBECONFIG colon-merge); defaults to $HOME/.kube/config or in-cluster",
		},
	},
	Action: watchAction,
}
