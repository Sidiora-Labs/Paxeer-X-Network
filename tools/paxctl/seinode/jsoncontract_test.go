package seinode

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// runJQ pipes objJSON through `jq <args>` and returns stdout + exit-ok.
// Skips the test if jq is not on PATH so the suite stays green in
// minimal environments.
func runJQ(t *testing.T, objJSON []byte, args ...string) (string, bool) {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not on PATH; skipping JSON-contract jq assertion")
	}
	cmd := exec.Command("jq", args...)
	cmd.Stdin = bytes.NewReader(objJSON)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err == nil
}

// fullNodeWithEndpoint is the §4.3 Running fullNode fixture.
func fullNodeWithEndpoint() *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "sei.io/v1alpha1",
		"kind":       "SeiNode",
		"metadata":   map[string]interface{}{"name": "chaos-rpc-0", "namespace": "sei"},
		"spec":       map[string]interface{}{"chainId": "c1", "fullNode": map[string]interface{}{}},
		"status": map[string]interface{}{
			"phase": "Running",
			"endpoint": map[string]interface{}{
				"evmJsonRpc":     "http://chaos-rpc-0.sei.svc:8545",
				"evmWs":          "ws://chaos-rpc-0.sei.svc:8546",
				"tendermintRpc":  "http://chaos-rpc-0.sei.svc:26657",
				"tendermintRest": "http://chaos-rpc-0.sei.svc:1317",
			},
		},
	}}
	return obj
}

// validatorNoEndpoint is the §4.3 validator fixture: .status.endpoint absent.
func validatorNoEndpoint() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "sei.io/v1alpha1",
		"kind":       "SeiNode",
		"metadata":   map[string]interface{}{"name": "genesis-val-0", "namespace": "sei"},
		"spec":       map[string]interface{}{"chainId": "c1", "validator": map[string]interface{}{}},
		"status":     map[string]interface{}{"phase": "Running"},
	}}
}

// T7 — node get degenerate read + omitempty-absence contract.
func TestJSONContract_NodeGet(t *testing.T) {
	fullJSON, err := json.Marshal(fullNodeWithEndpoint().Object)
	if err != nil {
		t.Fatalf("marshal fullNode: %v", err)
	}
	// fullNode: `jq -er '.status.endpoint.evmJsonRpc'` returns the scalar URL.
	out, ok := runJQ(t, fullJSON, "-er", ".status.endpoint.evmJsonRpc")
	if !ok {
		t.Fatalf("jq -e failed on fullNode; .status.endpoint.evmJsonRpc must be present")
	}
	if got := trimNL(out); got != "http://chaos-rpc-0.sei.svc:8545" {
		t.Errorf("evmJsonRpc = %q; want the scalar URL", got)
	}

	valJSON, err := json.Marshal(validatorNoEndpoint().Object)
	if err != nil {
		t.Fatalf("marshal validator: %v", err)
	}
	// validator: `.status.endpoint` absent => `jq -e` exits non-zero (fail-closed).
	if _, ok := runJQ(t, valJSON, "-er", ".status.endpoint.evmJsonRpc"); ok {
		t.Errorf("jq -e succeeded on validator; absent endpoint must fail closed")
	}
}

func trimNL(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
