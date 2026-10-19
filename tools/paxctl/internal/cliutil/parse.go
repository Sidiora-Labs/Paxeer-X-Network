package cliutil

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ApplyGenesisOverride writes a single key=value pair into the
// string-keyed JSON-valued map at fieldPath (for SeiNetwork this is
// spec.genesis.overrides). The key is a dotted cosmos-module path
// (module.field[.field...]) — the first segment must be a key in
// app_state of the genesis JSON. Value parses as JSON if it parses
// (number, bool, object, array, or JSON-quoted string); otherwise it's
// stored as a raw string.
//
// Distinct from ApplyOverride because the genesis-overrides map's value
// type is map[string]<JSON> rather than map[string]string, and the key
// shape is validated upstream by the sidecar's applyGenesisOverrides.
// Single-segment keys are rejected here so the user sees the issue at
// apply time rather than after the network has stalled retrying the
// assemble-genesis task.
func ApplyGenesisOverride(root map[string]interface{}, expr string, fieldPath ...string) error {
	eq := strings.Index(expr, "=")
	if eq < 0 {
		return fmt.Errorf("missing '=' in --genesis-override expression")
	}
	key := expr[:eq]
	val := expr[eq+1:]
	if key == "" {
		return fmt.Errorf("empty key before '='")
	}
	if val == "" {
		return fmt.Errorf("empty value after '=' (the sidecar rejects empty json.RawMessage at assemble-genesis)")
	}
	parts := strings.Split(key, ".")
	if len(parts) < 2 {
		return fmt.Errorf("key %q must be of the form module.field[.field...]", key)
	}
	for i, p := range parts {
		if p == "" {
			return fmt.Errorf("key %q has empty segment at index %d", key, i)
		}
	}

	var parsed interface{}
	if jsonErr := json.Unmarshal([]byte(val), &parsed); jsonErr != nil {
		parsed = val
	}

	existing, _, err := unstructured.NestedMap(root, fieldPath...)
	if err != nil {
		return fmt.Errorf("read existing overrides: %w", err)
	}
	if existing == nil {
		existing = map[string]interface{}{}
	}
	existing[key] = parsed
	return unstructured.SetNestedMap(root, existing, fieldPath...)
}

// ApplyOverride writes a single key=value pair into the string-keyed map
// at fieldPath (for SeiNode this is spec.overrides). Keys may contain
// dots because the overrides map's keys are themselves dotted TOML paths.
func ApplyOverride(root map[string]interface{}, expr string, fieldPath ...string) error {
	eq := strings.Index(expr, "=")
	if eq < 0 {
		return fmt.Errorf("missing '=' in --override expression")
	}
	key := expr[:eq]
	val := expr[eq+1:]
	if key == "" {
		return fmt.Errorf("empty key before '='")
	}
	existing, _, err := unstructured.NestedStringMap(root, fieldPath...)
	if err != nil {
		return fmt.Errorf("read existing overrides: %w", err)
	}
	if existing == nil {
		existing = map[string]string{}
	}
	existing[key] = val
	return unstructured.SetNestedStringMap(root, existing, fieldPath...)
}

// ApplySet writes a single dotted-path --set expression. Each segment is a
// map key, optionally suffixed with `[N]` to step into a list at index N
// after the key. Empty intermediate maps and lists are created on demand.
//
// List-index rules:
//   - idx == len(list) appends a new element (extends the list by one).
//   - idx <  len(list) sets in place on the existing element.
//   - idx >  len(list) errors; sparse indices are not supported.
func ApplySet(root map[string]interface{}, expr string) error {
	eq := strings.Index(expr, "=")
	if eq < 0 {
		return fmt.Errorf("missing '=' in --set expression")
	}
	rawPath := expr[:eq]
	rawVal := expr[eq+1:]
	if rawPath == "" {
		return fmt.Errorf("empty path before '='")
	}
	// `--set foo=null` would silently set the literal string "null"; reject
	// loudly so the user sees the gap. Field clearing is not in scope.
	if rawVal == "null" {
		return fmt.Errorf("value 'null' is not supported")
	}
	segments, err := parsePathSegments(rawPath)
	if err != nil {
		return err
	}
	return writeMap(root, segments, parseValue(rawVal))
}

// pathSegment names one step in a --set path. Every segment has a `key`
// (the map field). If isList is set, the step also indexes into a list
// at the value of that key (so `accounts[0]` is one segment with
// key="accounts", isList=true, listIdx=0).
type pathSegment struct {
	key     string
	isList  bool
	listIdx int
}

// parsePathSegments splits a dotted path into segments, recognizing an
// optional `[N]` suffix on each segment as a list index.
func parsePathSegments(path string) ([]pathSegment, error) {
	fields := strings.Split(path, ".")
	out := make([]pathSegment, 0, len(fields))
	for _, f := range fields {
		if f == "" {
			return nil, fmt.Errorf("empty segment in path %q", path)
		}
		bracket := strings.Index(f, "[")
		if bracket < 0 {
			out = append(out, pathSegment{key: f})
			continue
		}
		if !strings.HasSuffix(f, "]") {
			return nil, fmt.Errorf("malformed segment %q in path %q: '[' without closing ']'", f, path)
		}
		if bracket == 0 {
			return nil, fmt.Errorf("malformed segment %q in path %q: '[N]' without preceding key", f, path)
		}
		key := f[:bracket]
		idxStr := f[bracket+1 : len(f)-1]
		if idxStr == "" {
			return nil, fmt.Errorf("empty list index in segment %q", f)
		}
		if strings.ContainsAny(idxStr, "[]") {
			return nil, fmt.Errorf("nested list-index syntax not supported in segment %q", f)
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil, fmt.Errorf("malformed list index in segment %q: %w", f, err)
		}
		if idx < 0 {
			return nil, fmt.Errorf("negative list index in segment %q", f)
		}
		out = append(out, pathSegment{key: key, isList: true, listIdx: idx})
	}
	return out, nil
}

// writeMap descends one map step (segments[0].key), recursing or setting.
func writeMap(m map[string]interface{}, segments []pathSegment, value interface{}) error {
	if len(segments) == 0 {
		return fmt.Errorf("internal: writeMap called with empty segments")
	}
	seg := segments[0]
	rest := segments[1:]

	if seg.isList {
		var list []interface{}
		if existing, ok := m[seg.key]; ok && existing != nil {
			l, isList := existing.([]interface{})
			if !isList {
				return fmt.Errorf("path expects list at key %q but found %T", seg.key, existing)
			}
			list = l
		}
		newList, err := writeList(list, seg.listIdx, rest, value)
		if err != nil {
			return fmt.Errorf("at .%s[%d]: %w", seg.key, seg.listIdx, err)
		}
		m[seg.key] = newList
		return nil
	}

	if len(rest) == 0 {
		m[seg.key] = value
		return nil
	}

	existing, ok := m[seg.key]
	if !ok || existing == nil {
		inner := map[string]interface{}{}
		m[seg.key] = inner
		return writeMap(inner, rest, value)
	}
	inner, isMap := existing.(map[string]interface{})
	if !isMap {
		return fmt.Errorf("path expects map at key %q but found %T", seg.key, existing)
	}
	return writeMap(inner, rest, value)
}

// writeList sets or appends at index, recursing into the element if needed.
func writeList(list []interface{}, idx int, rest []pathSegment, value interface{}) ([]interface{}, error) {
	if idx > len(list) {
		return nil, fmt.Errorf("list index %d out of range (length %d); sparse indices are not supported", idx, len(list))
	}
	if idx == len(list) {
		if len(rest) == 0 {
			return append(list, value), nil
		}
		inner := map[string]interface{}{}
		list = append(list, inner)
		if err := writeMap(inner, rest, value); err != nil {
			return nil, err
		}
		return list, nil
	}
	if len(rest) == 0 {
		list[idx] = value
		return list, nil
	}
	inner, isMap := list[idx].(map[string]interface{})
	if !isMap {
		return nil, fmt.Errorf("path expects map at index [%d] but found %T", idx, list[idx])
	}
	return list, writeMap(inner, rest, value)
}

// ParseGenesisAccount parses a `<address>:<balance>` --genesis-account entry.
// The balance side accepts the standard cosmos coin format (one or more
// `<int><denom>` separated by commas — e.g. `1000usei,500uatom`).
func ParseGenesisAccount(entry string) (string, string, error) {
	idx := strings.Index(entry, ":")
	if idx < 0 {
		return "", "", fmt.Errorf("missing ':' between address and balance")
	}
	addr := strings.TrimSpace(entry[:idx])
	balance := strings.TrimSpace(entry[idx+1:])
	if addr == "" {
		return "", "", fmt.Errorf("empty address before ':'")
	}
	if balance == "" {
		return "", "", fmt.Errorf("empty balance after ':'")
	}
	return addr, balance, nil
}

// parseValue coerces a --set RHS into the narrowest scalar type: bools,
// base-10 int64s, then string fallback.
func parseValue(raw string) interface{} {
	switch raw {
	case "":
		return ""
	case "true":
		return true
	case "false":
		return false
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n
	}
	return raw
}
