package task

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sei-protocol/seictl/internal/cliutil"
	sidecar "github.com/sei-protocol/seictl/sidecar/client"
)

// sidecarProxyPort is the in-pod kube-rbac-proxy port the task verbs address,
// not the sidecar's own loopback :7777. The proxy runs TokenReview + SAR and
// forwards passed requests to 127.0.0.1:7777.
const sidecarProxyPort int32 = 8443

// requestTimeout bounds a single sidecar HTTP call. Every /v0/tasks call is
// sidecar-local and cheap (submit returns immediately; the task runs async), so
// a hung connection fails fast rather than stalling a poll iteration.
const requestTimeout = 10 * time.Second

// Discovery labels for snapshot-upload target selection. Discovery selects
// nothing when no pods carry the label; --node is the working path.
const (
	labelSnapshotPublish = "sei.io/snapshot-publish"
	labelChain           = "sei.io/chain"
)

// resolveKube resolves the cluster connection and effective namespace from
// --kubeconfig / -n exactly as the workflow and node trees do.
func resolveKube(c *cli.Command) (*rest.Config, string, error) {
	kc := cliutil.LoadKubeconfig(c.String("kubeconfig"), c.String("namespace"))
	cfg, err := kc.RESTConfig()
	if err != nil {
		return nil, "", err
	}
	ns, err := kc.Namespace()
	if err != nil {
		return nil, "", err
	}
	return cfg, ns, nil
}

// newSidecarClient builds a SidecarClient targeting one pod's proxy. The HTTP
// client is derived from the resolved rest.Config via rest.HTTPClientFor, so the
// full client-go auth chain (inline token, token file, exec plugin, auth
// provider) injects the caller's identity uniformly — the in-pod
// kube-rbac-proxy's TokenReview passes regardless of kubeconfig auth mode
// (in-cluster SA, EKS exec plugin, cert, OIDC). The composed transport's TLS
// settings are inert against the plaintext http:// proxy; auth injection is
// scheme-independent. requestTimeout rides through as the client's per-request
// Timeout. The config is copied so the per-request bound does not leak onto the
// shared cfg used by discovery.
func newSidecarClient(cfg *rest.Config, ns, node string, port int32) (*sidecar.SidecarClient, error) {
	authCfg := rest.CopyConfig(cfg)
	authCfg.Timeout = requestTimeout
	hc, err := rest.HTTPClientFor(authCfg)
	if err != nil {
		return nil, fmt.Errorf("building authenticated HTTP client: %w", err)
	}
	return sidecar.NewSidecarClientFromPodDNS(node, ns, port, sidecar.WithHTTPDoer(hc))
}

// discoverPublishNode label-selects snapshot-publish pods for the chain and
// returns a random one's node name. It is the discovery half of
// snapshot-upload's target selection; --node bypasses it.
func discoverPublishNode(ctx context.Context, cfg *rest.Config, ns, chain string) (string, error) {
	kcli, err := cliutil.NewClient(cfg)
	if err != nil {
		return "", err
	}
	pods := &unstructured.UnstructuredList{}
	pods.SetGroupVersionKind(schema.GroupVersionKind{Version: "v1", Kind: "PodList"})
	err = kcli.List(ctx, pods,
		client.InNamespace(ns),
		client.MatchingLabels{labelSnapshotPublish: "true", labelChain: chain},
	)
	if err != nil {
		return "", fmt.Errorf("list snapshot-publish pods for chain %s in %s: %w", chain, ns, err)
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pods match %s=true,%s=%s in %s; use --node to target a node explicitly",
			labelSnapshotPublish, labelChain, chain, ns)
	}
	pod := pods.Items[rand.IntN(len(pods.Items))].GetName()
	return nodeFromPod(pod), nil
}

// nodeFromPod derives a SeiNode / headless-service name from a StatefulSet pod
// name by stripping the trailing ordinal. Publish nodes are single-replica, so
// the pod is always <node>-0 and the sidecar resolves at <node>-0.<node>.<ns>.
func nodeFromPod(pod string) string {
	i := strings.LastIndex(pod, "-")
	if i < 0 {
		return pod
	}
	if _, err := strconv.Atoi(pod[i+1:]); err != nil {
		return pod
	}
	return pod[:i]
}
