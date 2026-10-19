package seinode

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestEndpointsFrom pins the .status.endpoint field paths the caught-up gate
// reads — the contract the controller's NodeEndpointStatus publishes.
func TestEndpointsFrom(t *testing.T) {
	t.Run("fullNode publishes both", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"status": map[string]any{"endpoint": map[string]any{
				"tendermintRpc": "http://n.ns.svc:26657",
				"evmJsonRpc":    "http://n.ns.svc:8545",
			}},
		}}
		tm, evm := endpointsFrom(obj)
		if tm != "http://n.ns.svc:26657" || evm != "http://n.ns.svc:8545" {
			t.Fatalf("got tm=%q evm=%q", tm, evm)
		}
	})

	t.Run("non-EVM node: evm absent reads empty, not error", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{
			"status": map[string]any{"endpoint": map[string]any{
				"tendermintRpc": "http://v.ns.svc:26657",
			}},
		}}
		tm, evm := endpointsFrom(obj)
		if tm == "" || evm != "" {
			t.Fatalf("got tm=%q evm=%q; want tm set, evm empty", tm, evm)
		}
	})

	t.Run("no endpoint yet: both empty", func(t *testing.T) {
		obj := &unstructured.Unstructured{Object: map[string]any{"status": map[string]any{}}}
		if tm, evm := endpointsFrom(obj); tm != "" || evm != "" {
			t.Fatalf("got tm=%q evm=%q; want both empty", tm, evm)
		}
	})
}
