package tasks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// mustAppState builds an app_state seed map from a JSON literal so tests
// read like the on-the-wire structure they're verifying.
func mustAppState(t *testing.T, body string) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("seed app_state: %v", err)
	}
	return out
}

func TestApplyGenesisOverrides_NilAndEmpty(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":{"unbonding_time":"21 days"}}}`)
	before, _ := json.Marshal(state)

	if err := applyGenesisOverrides(state, nil); err != nil {
		t.Fatalf("nil overrides: %v", err)
	}
	if err := applyGenesisOverrides(state, map[string]json.RawMessage{}); err != nil {
		t.Fatalf("empty overrides: %v", err)
	}
	after, _ := json.Marshal(state)
	if string(before) != string(after) {
		t.Errorf("app_state mutated on no-op input:\nbefore=%s\nafter=%s", before, after)
	}
}

func TestApplyGenesisOverrides_StringLeaf(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":{"unbonding_time":"21 days"}}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	got := unbondingTime(t, state)
	if got != "600s" {
		t.Errorf("unbonding_time = %q, want %q", got, "600s")
	}
}

func TestApplyGenesisOverrides_NumberLeaf(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":{"max_validators":100}}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.params.max_validators": json.RawMessage(`50`),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	stakingState := unmarshalObj(t, state["staking"])
	params := unmarshalObj(t, stakingState["params"])
	if got := string(params["max_validators"]); got != "50" {
		t.Errorf("max_validators raw = %q, want %q", got, "50")
	}
}

func TestApplyGenesisOverrides_ObjectLeaf(t *testing.T) {
	state := mustAppState(t, `{"gov":{"params":{"voting_params":{"voting_period":"172800s"}}}}`)
	newParams := json.RawMessage(`{"voting_period":"60s","quorum":"0.4"}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"gov.params.voting_params": newParams,
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	govState := unmarshalObj(t, state["gov"])
	params := unmarshalObj(t, govState["params"])
	vp := unmarshalObj(t, params["voting_params"])
	if got := string(vp["voting_period"]); got != `"60s"` {
		t.Errorf("voting_period = %q, want %q", got, `"60s"`)
	}
	if got := string(vp["quorum"]); got != `"0.4"` {
		t.Errorf("quorum = %q, want %q", got, `"0.4"`)
	}
}

func TestApplyGenesisOverrides_MultipleKeysAcrossModules(t *testing.T) {
	state := mustAppState(t, `{
		"staking": {"params": {"unbonding_time": "21 days", "max_validators": 100}},
		"gov":     {"params": {"max_deposit_period": "172800s"}}
	}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
		"staking.params.max_validators": json.RawMessage(`50`),
		"gov.params.max_deposit_period": json.RawMessage(`"60s"`),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	if got := unbondingTime(t, state); got != "600s" {
		t.Errorf("unbonding_time = %q, want 600s", got)
	}

	stakingState := unmarshalObj(t, state["staking"])
	params := unmarshalObj(t, stakingState["params"])
	if got := string(params["max_validators"]); got != "50" {
		t.Errorf("max_validators = %q, want 50", got)
	}

	govState := unmarshalObj(t, state["gov"])
	govParams := unmarshalObj(t, govState["params"])
	if got := string(govParams["max_deposit_period"]); got != `"60s"` {
		t.Errorf("max_deposit_period = %q, want \"60s\"", got)
	}
}

func TestApplyGenesisOverrides_DeepNestedPath(t *testing.T) {
	state := mustAppState(t, `{"mod":{"a":{"b":{"c":{"d":"old"}}}}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"mod.a.b.c.d": json.RawMessage(`"new"`),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	mod := unmarshalObj(t, state["mod"])
	a := unmarshalObj(t, mod["a"])
	b := unmarshalObj(t, a["b"])
	c := unmarshalObj(t, b["c"])
	if got := string(c["d"]); got != `"new"` {
		t.Errorf("deep leaf = %q, want \"new\"", got)
	}
}

func TestApplyGenesisOverrides_CreatesMissingIntermediate(t *testing.T) {
	// Override under an existing module that doesn't yet have the
	// intermediate path. The helper auto-creates empty objects so
	// new sub-fields can be added without seeding the path first.
	state := mustAppState(t, `{"staking":{"params":{"unbonding_time":"21 days"}}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.future.new_field": json.RawMessage(`true`),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	stakingState := unmarshalObj(t, state["staking"])
	future := unmarshalObj(t, stakingState["future"])
	if got := string(future["new_field"]); got != "true" {
		t.Errorf("future.new_field = %q, want true", got)
	}
}

func TestApplyGenesisOverrides_Idempotent(t *testing.T) {
	overrides := map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
		"staking.params.max_validators": json.RawMessage(`50`),
	}

	stateA := mustAppState(t, `{"staking":{"params":{"unbonding_time":"21 days","max_validators":100}}}`)
	if err := applyGenesisOverrides(stateA, overrides); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	afterFirst, _ := json.Marshal(stateA)

	if err := applyGenesisOverrides(stateA, overrides); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	afterSecond, _ := json.Marshal(stateA)

	if string(afterFirst) != string(afterSecond) {
		t.Errorf("not idempotent:\nfirst=%s\nsecond=%s", afterFirst, afterSecond)
	}
}

func TestApplyGenesisOverrides_RejectsBadKey(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":{"unbonding_time":"21 days"}}}`)

	cases := []struct {
		name      string
		overrides map[string]json.RawMessage
		want      string
	}{
		{
			name:      "empty key",
			overrides: map[string]json.RawMessage{"": json.RawMessage(`"x"`)},
			want:      "empty key",
		},
		{
			name:      "single token",
			overrides: map[string]json.RawMessage{"staking": json.RawMessage(`"x"`)},
			want:      "module.field",
		},
		{
			name:      "trailing dot",
			overrides: map[string]json.RawMessage{"staking.params.": json.RawMessage(`"x"`)},
			want:      "empty segment",
		},
		{
			name:      "double dot",
			overrides: map[string]json.RawMessage{"staking..unbonding_time": json.RawMessage(`"x"`)},
			want:      "empty segment",
		},
		{
			name:      "empty value",
			overrides: map[string]json.RawMessage{"staking.params.unbonding_time": json.RawMessage(``)},
			want:      "empty value",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := applyGenesisOverrides(copyAppState(t, state), c.overrides)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not contain %q", err.Error(), c.want)
			}
		})
	}
}

func TestApplyGenesisOverrides_UnknownModule(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":{"unbonding_time":"21 days"}}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"nope.params.x": json.RawMessage(`"y"`),
	})
	if err == nil {
		t.Fatal("expected error for unknown module")
	}
	if !strings.Contains(err.Error(), "unknown module") {
		t.Errorf("error = %q, want substring 'unknown module'", err.Error())
	}
}

func TestApplyGenesisOverrides_TraverseScalar(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":"this-is-a-string"}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
	})
	if err == nil {
		t.Fatal("expected error traversing scalar intermediate")
	}
	if !strings.Contains(err.Error(), "non-object") {
		t.Errorf("error = %q, want substring 'non-object'", err.Error())
	}
}

func TestApplyGenesisOverrides_TraverseArray(t *testing.T) {
	state := mustAppState(t, `{"staking":{"params":["a","b"]}}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
	})
	if err == nil {
		t.Fatal("expected error traversing array intermediate")
	}
	if !strings.Contains(err.Error(), "non-object") {
		t.Errorf("error = %q, want substring 'non-object'", err.Error())
	}
}

func TestApplyGenesisOverrides_LeavesUnrelatedKeysIntact(t *testing.T) {
	state := mustAppState(t, `{
		"staking": {"params": {"unbonding_time": "21 days", "max_validators": 100}},
		"gov":     {"params": {"max_deposit_period": "172800s"}}
	}`)
	err := applyGenesisOverrides(state, map[string]json.RawMessage{
		"staking.params.unbonding_time": json.RawMessage(`"600s"`),
	})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	// staking.params.max_validators preserved
	stakingState := unmarshalObj(t, state["staking"])
	params := unmarshalObj(t, stakingState["params"])
	if got := string(params["max_validators"]); got != "100" {
		t.Errorf("max_validators = %q, want preserved 100", got)
	}

	// entire gov module preserved
	gov := unmarshalObj(t, state["gov"])
	govParams := unmarshalObj(t, gov["params"])
	if got := string(govParams["max_deposit_period"]); got != `"172800s"` {
		t.Errorf("gov.params.max_deposit_period = %q, want preserved", got)
	}
}

// unbondingTime is a convenience helper used by multiple tests to drill
// into staking.params.unbonding_time and return the unquoted string.
func unbondingTime(t *testing.T, state map[string]json.RawMessage) string {
	t.Helper()
	staking := unmarshalObj(t, state["staking"])
	params := unmarshalObj(t, staking["params"])
	var s string
	if err := json.Unmarshal(params["unbonding_time"], &s); err != nil {
		t.Fatalf("decoding unbonding_time: %v", err)
	}
	return s
}

func unmarshalObj(t *testing.T, raw json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding object: %v", err)
	}
	return out
}

func copyAppState(t *testing.T, in map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	bz, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(bz, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if reflect.DeepEqual(in, out) == false {
		// Sanity guard — not a test assertion, just paranoia about the helper.
		t.Logf("note: marshal/unmarshal round-trip diverged for state; tests should still be valid")
	}
	return out
}
