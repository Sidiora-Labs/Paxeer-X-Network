package seinode

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// T6 — PRIMARY JSON-contract round-trip (replaces the false-green fixture).
// Producer→consumer end-to-end: `node apply --network X` renders N follower
// SeiNodes carrying the §2.2b object labels; we load them into a fake
// apiserver; the §4.4 PRIMARY selector returns EXACTLY those N (a
// sei.io/role=validator object loaded alongside is excluded by the
// SELECTOR, not by select(.)); the §4.4 jq over that List yields the N
// follower URLs.
//
// The old T6 hand-fed labels into the fixture and never exercised the
// producer — a false green that masked Defect A. This renders through the
// real producer.
func TestRoundTrip_RenderListJQ(t *testing.T) {
	const network = "netX"
	followers := []string{"chaos-rpc-0", "chaos-rpc-1"}

	objs := make([]client.Object, 0, len(followers)+1)
	wantURLs := map[string]bool{}
	for _, name := range followers {
		u, err := render(renderArgs{
			preset:    "rpc",
			name:      name,
			namespace: "sei",
			chainID:   "c1",
			image:     "i:1",
			network:   network,
		})
		if err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		// Simulate the controller marking the node Running and publishing
		// its endpoint (WS-A0). render() produces spec+labels; status is
		// the controller's job — we stand in for it here.
		url := "http://" + name + ".sei.svc:8545"
		_ = unstructured.SetNestedField(u.Object, "Running", "status", "phase")
		_ = unstructured.SetNestedField(u.Object, url, "status", "endpoint", "evmJsonRpc")
		wantURLs[`"`+url+`"`] = true
		objs = append(objs, u)
	}

	// A genesis validator carrying sei.io/role=validator (as the SeiNetwork
	// controller stamps). The §4.4 selector's role=node clause must exclude
	// it at the apiserver — it must never appear in the List.
	val := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "sei.io/v1alpha1",
		"kind":       "SeiNode",
		"metadata": map[string]interface{}{
			"name":      "genesis-val-0",
			"namespace": "sei",
			"labels": map[string]interface{}{
				"sei.io/seinetwork": network,
				"sei.io/role":       "validator",
			},
		},
		"spec":   map[string]interface{}{"chainId": "c1", "validator": map[string]interface{}{}},
		"status": map[string]interface{}{"phase": "Running"},
	}}
	objs = append(objs, val)

	c := fake.NewClientBuilder().WithObjects(objs...).Build()

	// §4.4 PRIMARY selector: sei.io/seinetwork=<net>,sei.io/role=node.
	sel, err := labels.Parse("sei.io/seinetwork=" + network + ",sei.io/role=node")
	if err != nil {
		t.Fatalf("parse selector: %v", err)
	}
	list := kind.NewList()
	if err := c.List(context.Background(), list,
		client.InNamespace("sei"),
		client.MatchingLabelsSelector{Selector: sel}); err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(list.Items) != len(followers) {
		t.Fatalf("selector returned %d items; want %d followers (validator must be excluded by the SELECTOR)\nitems: %v",
			len(list.Items), len(followers), itemNames(list.Items))
	}
	for _, it := range list.Items {
		if it.GetName() == "genesis-val-0" {
			t.Fatalf("validator leaked into the follower list; the role=node clause failed")
		}
	}

	// §4.4 PRIMARY jq over the List JSON: assemble the N-endpoint load list.
	// MarshalJSON flattens .Items into the .items array the jq reads.
	listJSON, err := list.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal list: %v", err)
	}

	out, ok := runJQ(t, listJSON, "-r",
		`[.items[].status.endpoint.evmJsonRpc | select(.)] | map("\"" + . + "\"") | join(",")`)
	if !ok {
		t.Fatalf("jq failed over list output")
	}
	got := trimNL(out)
	// The order is apiserver-defined; assert each wanted URL is present and
	// the count matches, rather than a fixed string.
	gotURLs := splitQuotedCSV(got)
	if len(gotURLs) != len(wantURLs) {
		t.Fatalf("jq produced %d endpoints (%q); want %d", len(gotURLs), got, len(wantURLs))
	}
	for _, u := range gotURLs {
		if !wantURLs[u] {
			t.Errorf("unexpected endpoint %q in load list %q", u, got)
		}
	}
}

func itemNames(items []unstructured.Unstructured) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.GetName())
	}
	return out
}

// splitQuotedCSV splits the jq output `"a","b"` into [`"a"`, `"b"`].
func splitQuotedCSV(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}
