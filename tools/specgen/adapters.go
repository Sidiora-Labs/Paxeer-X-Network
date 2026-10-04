package main

import (
	"fmt"
	"strings"
)

// buildBrief composes the shared, IDE-neutral operating brief from
// workflow.kvx. Every per-IDE adapter wraps this same body.
func buildBrief(wf *Doc) string {
	var b strings.Builder
	name := wf.Str("meta", "name")
	fmt.Fprintf(&b, "# %s\n\n", name)
	b.WriteString("The single source of truth for how to work in this repo is **`spec/`** (kvx).\n")
	b.WriteString("This file is a GENERATED pointer to it. Do not hand-edit it; edit `spec/workflow.kvx` and run `spec/specgen`.\n\n")

	if sot := wf.Str("meta", "source_of_truth"); sot != "" {
		fmt.Fprintf(&b, "> %s\n\n", sot)
	}

	b.WriteString("## Principles\n\n")
	for _, kv := range wf.OrderedKV("principle", "") {
		fmt.Fprintf(&b, "- **%s**: %s\n", humanize(kv[0]), kv[1])
	}

	b.WriteString("\n## The loop (every session)\n\n")
	n := 0
	for _, kv := range wf.OrderedKV("loop", "") {
		n++
		fmt.Fprintf(&b, "%d. %s\n", n, kv[1])
	}

	b.WriteString("\n## Persistent project memory (when configured)\n\n")
	for _, kv := range wf.OrderedKV("cortex", "") {
		fmt.Fprintf(&b, "- **%s**: %s\n", humanize(kv[0]), kv[1])
	}

	if active := wf.Str("meta", "active_feature"); active != "" {
		b.WriteString("\n## Active feature\n\n")
		fmt.Fprintf(&b, "`spec/%s/spec.kvx` — read its `[meta]` status, `[req.*]` acceptance criteria, and the `[task.*]` list (status + wave + requires). Work one task at a time in wave order; update `status` in the kvx as you go.\n", active)
	}

	b.WriteString("\n## Hard rules\n\n")
	for _, kv := range wf.OrderedKV("hard_rules", "") {
		fmt.Fprintf(&b, "- **%s**: %s\n", humanize(kv[0]), kv[1])
	}
	return b.String()
}

// adapter renders one IDE pointer file from the shared brief + a banner.
func adapter(label, brief, banner string) string {
	cmt := "<!-- " + banner + " -->\n\n"
	switch label {
	case "cursor":
		// Cursor rules: .mdc with YAML frontmatter; alwaysApply keeps it on.
		fm := "---\n" +
			"description: Ion portable spec workflow (task-driven + cortex)\n" +
			"alwaysApply: true\n" +
			"---\n\n"
		return fm + cmt + brief
	case "windsurf_workflow":
		return cmt + brief
	case "kiro":
		// Kiro steering: frontmatter inclusion=always.
		fm := "---\n" +
			"inclusion: always\n" +
			"---\n\n"
		return fm + cmt + brief
	default:
		// windsurf_rules, claude (CLAUDE.md), codex (AGENTS.md), copilot — plain md.
		return cmt + brief
	}
}

// humanize turns a snake_case kvx key into a Title Cased label.
func humanize(k string) string {
	parts := strings.Split(k, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
