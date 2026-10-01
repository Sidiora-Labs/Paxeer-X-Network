package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Validate checks the active feature document against the workflow contract:
// supported task statuses, requirement and task references, dependency
// cycles, forward wave violations, and that every task declares exact paths
// and one gate. It returns every violation in deterministic order.
func Validate(wf, spec *Doc) []string {
	var errs []string
	bad := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }

	statuses := map[string]bool{}
	for _, s := range wf.List("status_values", "task") {
		statuses[s] = true
	}
	if len(statuses) == 0 {
		bad("workflow: [status_values].task is empty")
	}

	reqs := map[string]bool{}
	for _, id := range spec.SectionsWithPrefix("req") {
		reqs[id] = true
	}

	var ids []string
	waves := map[string]uint64{}
	for _, id := range spec.SectionsWithPrefix("task") {
		sec := "task." + id
		ids = append(ids, id)
		raw := spec.Raw(sec, "wave")
		w, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			bad("task %s: wave %q is not a non-negative integer", id, raw)
		}
		waves[id] = w
	}
	SortDottedIDs(ids)
	known := map[string]bool{}
	for _, id := range ids {
		known[id] = true
	}

	deps := map[string][]string{}
	for _, id := range ids {
		sec := "task." + id
		if st := spec.Str(sec, "status"); !statuses[st] {
			bad("task %s: unsupported status %q", id, st)
		}
		for _, r := range spec.List(sec, "reqs") {
			if !reqs[r] && !acceptanceExists(spec, r) {
				bad("task %s: unknown requirement %q", id, r)
			}
		}
		for _, d := range spec.List(sec, "requires") {
			if !known[d] {
				bad("task %s: requires unknown task %q", id, d)
				continue
			}
			if d == id {
				bad("task %s: requires itself", id)
				continue
			}
			if waves[d] > waves[id] {
				bad("task %s (wave %d): requires %s from later wave %d", id, waves[id], d, waves[d])
			}
			deps[id] = append(deps[id], d)
		}
		if len(spec.List(sec, "touches")) == 0 {
			bad("task %s: declares no exact paths in touches", id)
		}
		if strings.TrimSpace(spec.Str(sec, "verify_cmd")) == "" {
			bad("task %s: declares no verify_cmd gate", id)
		}
	}

	// Cycle detection by iterative DFS colouring, visiting IDs in sorted order.
	const (
		white = iota
		grey
		black
	)
	colour := map[string]int{}
	for _, start := range ids {
		if colour[start] != white {
			continue
		}
		type frame struct {
			id string
			i  int
		}
		stack := []frame{{start, 0}}
		colour[start] = grey
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.i < len(deps[top.id]) {
				next := deps[top.id][top.i]
				top.i++
				switch colour[next] {
				case white:
					colour[next] = grey
					stack = append(stack, frame{next, 0})
				case grey:
					bad("dependency cycle through %s -> %s", top.id, next)
				}
				continue
			}
			colour[top.id] = black
			stack = stack[:len(stack)-1]
		}
	}

	// Every resolution must name clauses that exist in this document.
	for _, rid := range spec.SectionsWithPrefix("resolution") {
		sec := "resolution." + rid
		for _, ref := range spec.List(sec, "applies_to") {
			if !clauseExists(spec, ref) {
				bad("%s: applies_to unknown clause %q", sec, ref)
			}
		}
	}
	return errs
}

// clauseExists reports whether ref names a section, a group of sections
// sharing the dotted prefix ref, or a key inside a section written as
// <section>.<key>.
func clauseExists(d *Doc, ref string) bool {
	if d.Has(ref) || len(d.SectionsWithPrefix(ref)) > 0 {
		return true
	}
	for i := len(ref) - 1; i > 0; i-- {
		if ref[i] != '.' || !d.Has(ref[:i]) {
			continue
		}
		key := ref[i+1:]
		for _, k := range d.Keys(ref[:i]) {
			if k == key {
				return true
			}
		}
	}
	return false
}

// acceptanceExists reports whether ref written as <req>.<n> names criterion
// ac_<n> of [req.<req>].
func acceptanceExists(d *Doc, ref string) bool {
	i := strings.LastIndexByte(ref, '.')
	return i > 0 && clauseExists(d, "req."+ref[:i]+".ac_"+ref[i+1:])
}
