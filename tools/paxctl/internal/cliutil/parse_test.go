package cliutil

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// T12 — the --set/--override/--genesis-override parsers moved to cliutil
// must produce byte-identical results to the old nodedeployment.preset
// parsers on the existing corpus. These cases are ported verbatim from
// nodedeployment/preset_test.go, adjusted only for the parameterized
// fieldPath signatures.

func TestApplySet_Errors(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"no-equals", "missing '='"},
		{"=value", "empty path"},
		{"a..b=v", "empty segment"},
		{"a[=v", "without closing"},
		{"a[]=v", "empty list index"},
		{"a[abc]=v", "malformed list index"},
		{"a[-1]=v", "negative list index"},
		{"[0]=v", "without preceding key"},
		{"foo=null", "value 'null' is not supported"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			err := ApplySet(map[string]interface{}{}, tc.expr)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want containing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestApplySet_ListIndex_AppendIntoEmptyList(t *testing.T) {
	root := map[string]interface{}{}
	if err := ApplySet(root, "spec.genesis.accounts[0].address=sei1abc"); err != nil {
		t.Fatalf("ApplySet: %v", err)
	}
	if err := ApplySet(root, "spec.genesis.accounts[0].balance=1000usei"); err != nil {
		t.Fatalf("ApplySet: %v", err)
	}
	accounts, _, _ := unstructured.NestedSlice(root, "spec", "genesis", "accounts")
	if len(accounts) != 1 {
		t.Fatalf("len(accounts) = %d; want 1", len(accounts))
	}
	a0, ok := accounts[0].(map[string]interface{})
	if !ok {
		t.Fatalf("accounts[0] is %T; want map", accounts[0])
	}
	if a0["address"] != "sei1abc" {
		t.Errorf("accounts[0].address = %v; want sei1abc", a0["address"])
	}
	if a0["balance"] != "1000usei" {
		t.Errorf("accounts[0].balance = %v; want 1000usei", a0["balance"])
	}
}

func TestApplySet_ListIndex_AppendSequential(t *testing.T) {
	root := map[string]interface{}{}
	exprs := []string{
		"spec.genesis.accounts[0].address=sei1aaa",
		"spec.genesis.accounts[0].balance=100usei",
		"spec.genesis.accounts[1].address=sei1bbb",
		"spec.genesis.accounts[1].balance=200usei",
	}
	for _, e := range exprs {
		if err := ApplySet(root, e); err != nil {
			t.Fatalf("ApplySet(%q): %v", e, err)
		}
	}
	accounts, _, _ := unstructured.NestedSlice(root, "spec", "genesis", "accounts")
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d; want 2", len(accounts))
	}
	a1 := accounts[1].(map[string]interface{})
	if a1["address"] != "sei1bbb" || a1["balance"] != "200usei" {
		t.Errorf("accounts[1] = %v; want {address=sei1bbb, balance=200usei}", a1)
	}
}

func TestApplySet_ListIndex_SetOnExisting(t *testing.T) {
	root := map[string]interface{}{
		"spec": map[string]interface{}{
			"peers": []interface{}{
				map[string]interface{}{
					"label": map[string]interface{}{
						"original": "value",
					},
				},
			},
		},
	}
	if err := ApplySet(root, "spec.peers[0].label.original=overridden"); err != nil {
		t.Fatalf("ApplySet: %v", err)
	}
	peers, _, _ := unstructured.NestedSlice(root, "spec", "peers")
	p0 := peers[0].(map[string]interface{})
	label := p0["label"].(map[string]interface{})
	if label["original"] != "overridden" {
		t.Errorf("peers[0].label.original = %v; want overridden", label["original"])
	}
}

func TestApplySet_ListIndex_SparseRejected(t *testing.T) {
	root := map[string]interface{}{}
	err := ApplySet(root, "spec.accounts[2].address=foo")
	if err == nil {
		t.Fatalf("expected error for sparse index, got nil")
	}
	if !strings.Contains(err.Error(), "out of range") {
		t.Errorf("err = %v; want containing 'out of range'", err)
	}
	if !strings.Contains(err.Error(), ".accounts[2]") {
		t.Errorf("err = %v; want containing failing segment '.accounts[2]'", err)
	}
}

func TestApplySet_ListIndex_LeafScalar(t *testing.T) {
	root := map[string]interface{}{}
	if err := ApplySet(root, "tags[0]=alpha"); err != nil {
		t.Fatalf("ApplySet: %v", err)
	}
	if err := ApplySet(root, "tags[1]=beta"); err != nil {
		t.Fatalf("ApplySet: %v", err)
	}
	tags, _, _ := unstructured.NestedSlice(root, "tags")
	if len(tags) != 2 || tags[0] != "alpha" || tags[1] != "beta" {
		t.Errorf("tags = %v; want [alpha beta]", tags)
	}
}

func TestApplySet_ListIndex_TypeMismatchSurfaces(t *testing.T) {
	root := map[string]interface{}{
		"spec": map[string]interface{}{
			"accounts": "not-a-list",
		},
	}
	err := ApplySet(root, "spec.accounts[0]=foo")
	if err == nil {
		t.Fatalf("expected error for non-list field, got nil")
	}
	if !strings.Contains(err.Error(), "expects list") {
		t.Errorf("err = %v; want containing 'expects list'", err)
	}
}

func TestApplyOverride_PreservesExistingMap(t *testing.T) {
	root := map[string]interface{}{
		"spec": map[string]interface{}{
			"overrides": map[string]interface{}{
				"chain.moniker": "preset-default",
			},
		},
	}
	if err := ApplyOverride(root, "evm.enabled_legacy_sei_apis=sei_getLogs", "spec", "overrides"); err != nil {
		t.Fatalf("ApplyOverride: %v", err)
	}
	overrides, _, _ := unstructured.NestedStringMap(root, "spec", "overrides")
	if overrides["chain.moniker"] != "preset-default" {
		t.Errorf("preset-supplied entry was clobbered: chain.moniker = %q", overrides["chain.moniker"])
	}
	if overrides["evm.enabled_legacy_sei_apis"] != "sei_getLogs" {
		t.Errorf("override not written: evm.enabled_legacy_sei_apis = %q", overrides["evm.enabled_legacy_sei_apis"])
	}
}

func TestApplyOverride_ValueWithEquals(t *testing.T) {
	root := map[string]interface{}{}
	if err := ApplyOverride(root, "chain.min_gas_prices=0.1usei,0.5=stake", "spec", "overrides"); err != nil {
		t.Fatalf("ApplyOverride: %v", err)
	}
	overrides, _, _ := unstructured.NestedStringMap(root, "spec", "overrides")
	if got := overrides["chain.min_gas_prices"]; got != "0.1usei,0.5=stake" {
		t.Errorf("value with embedded '=' not preserved: got %q", got)
	}
}

func TestApplyOverride_Errors(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"no-equals", "missing '='"},
		{"=value", "empty key"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			err := ApplyOverride(map[string]interface{}{}, tc.expr, "spec", "overrides")
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want containing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestApplyGenesisOverride_Errors(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"no-equals", "missing '='"},
		{"=value", "empty key"},
		{"staking.params.unbonding_time=", "empty value"},
		{"single=value", "must be of the form module.field"},
		{"staking..params=value", "empty segment"},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			err := ApplyGenesisOverride(map[string]interface{}{}, tc.expr, "spec", "genesis", "overrides")
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want containing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestApplyGenesisOverride_JSONStringEscape(t *testing.T) {
	root := map[string]interface{}{}
	if err := ApplyGenesisOverride(root, `staking.params.unbonding_time="42"`, "spec", "genesis", "overrides"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	overrides, _, _ := unstructured.NestedMap(root, "spec", "genesis", "overrides")
	if v := overrides["staking.params.unbonding_time"]; v != "42" {
		t.Errorf("want string \"42\", got %v (%T)", v, v)
	}
}

func TestApplyGenesisOverride_PreservesExistingKeys(t *testing.T) {
	root := map[string]interface{}{}
	if err := ApplyGenesisOverride(root, "staking.params.unbonding_time=600s", "spec", "genesis", "overrides"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := ApplyGenesisOverride(root, "bank.params.default_send_enabled=true", "spec", "genesis", "overrides"); err != nil {
		t.Fatalf("second: %v", err)
	}
	overrides, found, _ := unstructured.NestedMap(root, "spec", "genesis", "overrides")
	if !found {
		t.Fatalf("overrides not found")
	}
	if len(overrides) != 2 {
		t.Errorf("expected 2 keys, got %v", overrides)
	}
}

func TestParseGenesisAccount_Errors(t *testing.T) {
	cases := []struct {
		entry string
		want  string
	}{
		{"no-colon", "missing ':'"},
		{":1000usei", "empty address"},
		{"sei1abc:", "empty balance"},
	}
	for _, tc := range cases {
		t.Run(tc.entry, func(t *testing.T) {
			_, _, err := ParseGenesisAccount(tc.entry)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q; want containing %q", err.Error(), tc.want)
			}
		})
	}
}

func TestParseValue(t *testing.T) {
	cases := map[string]interface{}{
		"":       "",
		"true":   true,
		"false":  false,
		"42":     int64(42),
		"-1":     int64(-1),
		"abc":    "abc",
		"v1.2.3": "v1.2.3",
		"truex":  "truex",
		"42x":    "42x",
	}
	for in, want := range cases {
		if got := parseValue(in); got != want {
			t.Errorf("parseValue(%q) = %v (%T); want %v (%T)", in, got, got, want, want)
		}
	}
}
