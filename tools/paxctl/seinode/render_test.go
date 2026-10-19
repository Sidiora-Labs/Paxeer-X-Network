package seinode

import (
	"context"
	"strings"
	"testing"

	"github.com/urfave/cli/v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// T2 — node apply golden render: rpc preset + --chain-id/--image/--network.
func TestRender_RPCGolden(t *testing.T) {
	got, err := render(renderArgs{
		preset:  "rpc",
		name:    "chaos-rpc-0",
		chainID: "c1",
		image:   "i:1",
		network: "netX",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got.GroupVersionKind().Kind != "SeiNode" {
		t.Errorf("kind = %q; want SeiNode", got.GroupVersionKind().Kind)
	}
	if got.GetName() != "chaos-rpc-0" {
		t.Errorf("name = %q; want chaos-rpc-0", got.GetName())
	}
	chainID, _, _ := unstructured.NestedString(got.Object, "spec", "chainId")
	if chainID != "c1" {
		t.Errorf("spec.chainId = %q; want c1", chainID)
	}
	image, _, _ := unstructured.NestedString(got.Object, "spec", "image")
	if image != "i:1" {
		t.Errorf("spec.image = %q; want i:1", image)
	}
	if _, found, _ := unstructured.NestedMap(got.Object, "spec", "fullNode"); !found {
		t.Errorf("spec.fullNode not set by rpc preset")
	}
	selector, _, _ := selectorOf(t, got)
	if selector["sei.io/seinetwork"] != "netX" {
		t.Errorf("peers[0].label.selector[sei.io/seinetwork] = %q; want netX", selector["sei.io/seinetwork"])
	}
	// SeiNode is flat — there must be NO spec.template wrapper or spec.replicas.
	if _, found, _ := unstructured.NestedMap(got.Object, "spec", "template"); found {
		t.Errorf("spec.template present; SeiNode is flat, no template wrapper")
	}
	if _, found, _ := unstructured.NestedInt64(got.Object, "spec", "replicas"); found {
		t.Errorf("spec.replicas present; SeiNode has no replicas field")
	}
}

func selectorOf(t *testing.T, u *unstructured.Unstructured) (map[string]string, bool, error) {
	t.Helper()
	peers, found, err := unstructured.NestedSlice(u.Object, "spec", "peers")
	if err != nil || !found || len(peers) == 0 {
		t.Fatalf("expected at least one peer source, found=%v err=%v", found, err)
	}
	peer := peers[0].(map[string]interface{})
	return unstructured.NestedStringMap(peer, "label", "selector")
}

// T3 — peer-wiring: --network sets exactly sei.io/seinetwork, NOT
// sei.io/chain or sei.io/nodedeployment. Guards the §3 one-way decision.
func TestRender_PeerWiringKey(t *testing.T) {
	got, err := render(renderArgs{
		preset:  "rpc",
		name:    "rpc-0",
		chainID: "bench-nightly-12345",
		image:   "i:1",
		network: "bench-nightly-12345",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	selector, _, _ := selectorOf(t, got)
	if _, present := selector["sei.io/seinetwork"]; !present {
		t.Errorf("selector missing sei.io/seinetwork; got %v", selector)
	}
	if _, present := selector["sei.io/chain"]; present {
		t.Errorf("selector has sei.io/chain; the split surface binds network identity, NOT chain")
	}
	if _, present := selector["sei.io/nodedeployment"]; present {
		t.Errorf("selector has sei.io/nodedeployment; that frozen key is slated for retirement")
	}
	if len(selector) != 1 {
		t.Errorf("selector = %v; want exactly one key (sei.io/seinetwork)", selector)
	}
}

// T4 — no --network and no --set spec.peers => Invalid with guidance.
func TestRender_RequiresPeerSource(t *testing.T) {
	_, err := render(renderArgs{
		preset:  "rpc",
		name:    "rpc-0",
		chainID: "c1",
		image:   "i:1",
	})
	if err == nil {
		t.Fatalf("expected error when neither --network nor --set spec.peers given")
	}
	if !strings.Contains(err.Error(), "--network") || !strings.Contains(err.Error(), "spec.peers") {
		t.Errorf("err = %q; want guidance naming --network and spec.peers", err.Error())
	}
}

// T4 (cont.) — an explicit --set spec.peers satisfies the requirement
// even without --network.
func TestRender_ExplicitPeerSetSatisfiesRequirement(t *testing.T) {
	got, err := render(renderArgs{
		preset:  "rpc",
		name:    "rpc-0",
		chainID: "c1",
		image:   "i:1",
		sets:    []string{"spec.peers[0].label.selector.sei.io/role=validator"},
	})
	if err != nil {
		t.Fatalf("render with explicit --set spec.peers: %v", err)
	}
	// No --network => no sei.io/seinetwork object label (T-new contract).
	if _, present := got.GetLabels()["sei.io/seinetwork"]; present {
		t.Errorf("sei.io/seinetwork stamped without --network; got %v", got.GetLabels())
	}
}

// T-new — object-label stamping (Defect A producer side). --network X
// renders metadata.labels {sei.io/seinetwork=X, sei.io/role=node};
// without --network only sei.io/role=node.
func TestRender_ObjectLabels(t *testing.T) {
	withNet, err := render(renderArgs{
		preset:  "rpc",
		name:    "rpc-0",
		chainID: "c1",
		image:   "i:1",
		network: "netX",
	})
	if err != nil {
		t.Fatalf("render with --network: %v", err)
	}
	labels := withNet.GetLabels()
	if labels["sei.io/role"] != "node" {
		t.Errorf("labels[sei.io/role] = %q; want node", labels["sei.io/role"])
	}
	if labels["sei.io/seinetwork"] != "netX" {
		t.Errorf("labels[sei.io/seinetwork] = %q; want netX", labels["sei.io/seinetwork"])
	}

	withoutNet, err := render(renderArgs{
		preset:  "rpc",
		name:    "rpc-0",
		chainID: "c1",
		image:   "i:1",
		sets:    []string{"spec.peers[0].label.selector.sei.io/role=validator"},
	})
	if err != nil {
		t.Fatalf("render without --network: %v", err)
	}
	labels = withoutNet.GetLabels()
	if labels["sei.io/role"] != "node" {
		t.Errorf("labels[sei.io/role] = %q; want node (unconditional)", labels["sei.io/role"])
	}
	if _, present := labels["sei.io/seinetwork"]; present {
		t.Errorf("labels[sei.io/seinetwork] present without --network; got %v", labels)
	}
}

// T5 — node apply has no --replicas flag (SeiNode has no replicas field).
func TestApplyCmd_NoReplicasFlag(t *testing.T) {
	for _, f := range applyCmd.Flags {
		for _, name := range f.Names() {
			if name == "replicas" {
				t.Fatalf("node apply must not expose --replicas; SeiNode has no spec.replicas")
			}
		}
	}
}

func TestRender_RequiredFlags(t *testing.T) {
	cases := []struct {
		name string
		args renderArgs
		want string
	}{
		{"missing preset", renderArgs{name: "x", network: "n"}, "--preset is required"},
		{"missing name", renderArgs{preset: "rpc", network: "n"}, "name is required"},
		{"unknown preset", renderArgs{preset: "no-such", name: "x", network: "n"}, "unknown preset"},
		{"rpc without chain-id", renderArgs{preset: "rpc", name: "x", image: "i:1", network: "n"}, "requires .spec.chainId"},
		{"rpc without image", renderArgs{preset: "rpc", name: "x", chainID: "c", network: "n"}, "requires .spec.image"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := render(tc.args)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want containing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestRender_SetPrecedence(t *testing.T) {
	got, err := render(renderArgs{
		preset:  "rpc",
		name:    "x",
		chainID: "pacific-1",
		image:   "from-flag:1",
		network: "netX",
		sets:    []string{"spec.image=from-set:2"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	image, _, _ := unstructured.NestedString(got.Object, "spec", "image")
	if image != "from-set:2" {
		t.Errorf("image = %q; --set should beat --image", image)
	}
}

func TestRender_SetCannotRetargetMetadata(t *testing.T) {
	got, err := render(renderArgs{
		preset:    "rpc",
		name:      "x",
		namespace: "nightly",
		chainID:   "pacific-1",
		image:     "i:1",
		network:   "netX",
		sets: []string{
			"metadata.namespace=kube-system",
			"metadata.name=hijacked",
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if got.GetNamespace() != "nightly" {
		t.Errorf("namespace = %q; --set must not retarget post-resolution", got.GetNamespace())
	}
	if got.GetName() != "x" {
		t.Errorf("name = %q; --set must not rename", got.GetName())
	}
}

func TestRender_OverrideFlag(t *testing.T) {
	got, err := render(renderArgs{
		preset:  "rpc",
		name:    "qa",
		chainID: "qa",
		image:   "i:1",
		network: "netX",
		overrides: []string{
			"evm.enabled_legacy_sei_apis=sei_getLogs,sei_getBlockByNumber",
			"tx_index.indexer=kv",
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	overrides, found, _ := unstructured.NestedStringMap(got.Object, "spec", "overrides")
	if !found {
		t.Fatal("spec.overrides not found")
	}
	if overrides["evm.enabled_legacy_sei_apis"] != "sei_getLogs,sei_getBlockByNumber" {
		t.Errorf("override not preserved: %q", overrides["evm.enabled_legacy_sei_apis"])
	}
	if overrides["tx_index.indexer"] != "kv" {
		t.Errorf("override not written: %q", overrides["tx_index.indexer"])
	}
	// The preset's pprof override must survive alongside the added keys.
	if overrides["network.rpc.pprof_listen_address"] != "0.0.0.0:6060" {
		t.Errorf("preset override clobbered: %v", overrides)
	}
}

func TestRender_ExternalAddress(t *testing.T) {
	got, err := render(renderArgs{
		preset:          "rpc",
		name:            "sentry",
		chainID:         "c1",
		image:           "i:1",
		network:         "netX",
		externalAddress: "1.2.3.4:26656",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	ext, _, _ := unstructured.NestedString(got.Object, "spec", "externalAddress")
	if ext != "1.2.3.4:26656" {
		t.Errorf("spec.externalAddress = %q; want 1.2.3.4:26656", ext)
	}
}

// urfave/cli's StringSliceFlag splits on ',' by default; assert the guard.
func TestApplyCmd_FlagsDoNotSplitOnComma(t *testing.T) {
	if !applyCmd.DisableSliceFlagSeparator {
		t.Fatal("applyCmd.DisableSliceFlagSeparator must be true")
	}

	var capturedOverride, capturedSet []string
	origAction := applyCmd.Action
	applyCmd.Action = func(_ context.Context, c *cli.Command) error {
		capturedOverride = c.StringSlice("override")
		capturedSet = c.StringSlice("set")
		return nil
	}
	t.Cleanup(func() { applyCmd.Action = origAction })

	err := applyCmd.Run(context.Background(), []string{
		"apply", "qa-rpc",
		"--preset", "rpc",
		"--chain-id", "qa-rpc",
		"--image", "i:1",
		"--network", "netX",
		"--override", "evm.enabled_legacy_sei_apis=sei_getLogs,sei_getBlockByNumber",
		"--set", "spec.image=foo,bar",
	})
	if err != nil {
		t.Fatalf("apply Run: %v", err)
	}
	if len(capturedOverride) != 1 || capturedOverride[0] != "evm.enabled_legacy_sei_apis=sei_getLogs,sei_getBlockByNumber" {
		t.Errorf("--override split by parser: %v", capturedOverride)
	}
	if len(capturedSet) != 1 || capturedSet[0] != "spec.image=foo,bar" {
		t.Errorf("--set split by parser: %v", capturedSet)
	}
}

func TestParseCascade(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", "Foreground"},
		{"foreground", "Foreground"},
		{"background", "Background"},
		{"orphan", "Orphan"},
	}
	for _, tc := range cases {
		got, err := parseCascade(tc.raw)
		if err != nil {
			t.Fatalf("parseCascade(%q): %v", tc.raw, err)
		}
		if string(*got) != tc.want {
			t.Errorf("parseCascade(%q) = %q; want %q", tc.raw, *got, tc.want)
		}
	}
	for _, bad := range []string{"async", "FOREGROUND", "delete"} {
		if _, err := parseCascade(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestPresetNames(t *testing.T) {
	names := presetNames()
	found := false
	for _, n := range names {
		if n == "rpc" {
			found = true
		}
	}
	if !found {
		t.Errorf("preset rpc not found in %v", names)
	}
}
