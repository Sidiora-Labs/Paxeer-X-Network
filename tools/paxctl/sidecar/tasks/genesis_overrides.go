package tasks

import (
	"encoding/json"
	"fmt"
	"strings"
)

// applyGenesisOverrides applies a flat map of dotted-path overrides to a
// cosmos-sdk genesis app_state map. Keys take the shape
// "<module>.<field>[.<field>...]" — the first segment must be a key in
// appState (the cosmos module name), and remaining segments walk into the
// module's JSON tree. The leaf JSON value is replaced verbatim.
//
// Intermediate path segments must resolve to JSON objects. A missing
// intermediate key is created as an empty object so callers can add new
// nested paths. A non-object intermediate is a hard error — we never
// silently overwrite scalars or arrays.
//
// The function fails loudly on malformed input (empty keys, single-token
// keys, unknown modules, non-object intermediates) so misconfiguration
// surfaces at ceremony time via the task's failure path rather than as
// silently-ignored fields in the final genesis.
func applyGenesisOverrides(appState map[string]json.RawMessage, overrides map[string]json.RawMessage) error {
	if len(overrides) == 0 {
		return nil
	}

	for key, value := range overrides {
		if key == "" {
			return fmt.Errorf("genesis-overrides: empty key")
		}
		parts := strings.Split(key, ".")
		if len(parts) < 2 {
			return fmt.Errorf("genesis-overrides: key %q must be of the form module.field[.field...]", key)
		}
		for i, p := range parts {
			if p == "" {
				return fmt.Errorf("genesis-overrides: key %q has empty segment at index %d", key, i)
			}
		}
		if len(value) == 0 {
			return fmt.Errorf("genesis-overrides: key %q has empty value", key)
		}

		module := parts[0]
		moduleRaw, ok := appState[module]
		if !ok {
			return fmt.Errorf("genesis-overrides: unknown module %q (key %q); app_state has no such top-level key", module, key)
		}

		var moduleState map[string]json.RawMessage
		if err := json.Unmarshal(moduleRaw, &moduleState); err != nil {
			return fmt.Errorf("genesis-overrides: module %q is not a JSON object: %w", module, err)
		}
		if moduleState == nil {
			moduleState = map[string]json.RawMessage{}
		}

		if err := setNestedRaw(moduleState, parts[1:], value, key); err != nil {
			return err
		}

		moduleBz, err := json.Marshal(moduleState)
		if err != nil {
			return fmt.Errorf("genesis-overrides: re-marshaling module %q: %w", module, err)
		}
		appState[module] = moduleBz
	}
	return nil
}

// setNestedRaw walks path into state, creating empty objects for missing
// intermediates, and writes value at the leaf. The originalKey is carried
// only for error messages.
func setNestedRaw(state map[string]json.RawMessage, path []string, value json.RawMessage, originalKey string) error {
	if len(path) == 0 {
		return fmt.Errorf("genesis-overrides: key %q has no field path under module", originalKey)
	}
	if len(path) == 1 {
		state[path[0]] = value
		return nil
	}

	head, rest := path[0], path[1:]
	var child map[string]json.RawMessage
	if raw, ok := state[head]; ok && string(raw) != "null" {
		if err := json.Unmarshal(raw, &child); err != nil {
			return fmt.Errorf("genesis-overrides: key %q traverses non-object at segment %q: %w", originalKey, head, err)
		}
	}
	if child == nil {
		child = map[string]json.RawMessage{}
	}

	if err := setNestedRaw(child, rest, value, originalKey); err != nil {
		return err
	}

	childBz, err := json.Marshal(child)
	if err != nil {
		return fmt.Errorf("genesis-overrides: re-marshaling intermediate %q for key %q: %w", head, originalKey, err)
	}
	state[head] = childBz
	return nil
}
