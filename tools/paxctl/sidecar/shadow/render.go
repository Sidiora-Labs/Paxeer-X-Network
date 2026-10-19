package shadow

import (
	"fmt"
	"strings"
)

// RenderMarkdown produces a human-readable investigation report from a
// DivergenceReport. The output is designed to be consumed by engineers
// or LLM agents analyzing why a shadow node diverged from the canonical chain.
func RenderMarkdown(r *DivergenceReport) string {
	var b strings.Builder

	writeHeader(&b, r)
	writeLayer0(&b, r.Comparison.Layer0)

	if r.Comparison.Layer1 != nil {
		writeLayer1(&b, r.Comparison.Layer1)
	}

	if r.Comparison.Layer2 != nil {
		writeLayer2(&b, r.Comparison.Layer2)
	}

	writeRawDataNote(&b, r.Height)
	return b.String()
}

func writeHeader(b *strings.Builder, r *DivergenceReport) {
	fmt.Fprintf(b, "# Divergence Report — Height %d\n\n", r.Height)
	fmt.Fprintf(b, "**Detected at:** %s\n\n", r.Timestamp)
	fmt.Fprintf(b, "App-hash divergence detected. The shadow node and canonical chain ")
	fmt.Fprintf(b, "produce different execution results at this block.\n\n")
}

func writeLayer0(b *strings.Builder, l0 Layer0Result) {
	fmt.Fprintf(b, "## Layer 0: Block Header Comparison\n\n")
	fmt.Fprintf(b, "| Field | Shadow | Canonical | Match |\n")
	fmt.Fprintf(b, "|-------|--------|-----------|-------|\n")

	writeL0Row(b, "AppHash", l0.AppHashMatch, l0.ShadowAppHash, l0.CanonicalAppHash)
	writeL0Row(b, "LastResultsHash", l0.LastResultsHashMatch, l0.ShadowLastResultsHash, l0.CanonicalLastResultsHash)
	writeL0GasRow(b, l0)
	fmt.Fprintf(b, "\n")
}

func writeL0Row(b *strings.Builder, field string, match bool, shadow, canonical string) {
	icon := "✅"
	if !match {
		icon = "❌"
	}
	s := truncateHash(shadow)
	c := truncateHash(canonical)
	if match {
		s = "—"
		c = "—"
	}
	fmt.Fprintf(b, "| %s | %s | %s | %s |\n", field, s, c, icon)
}

func writeL0GasRow(b *strings.Builder, l0 Layer0Result) {
	icon := "✅"
	s := "—"
	c := "—"
	if !l0.GasUsedMatch {
		icon = "❌"
		s = fmt.Sprintf("%d", l0.ShadowGasUsed)
		c = fmt.Sprintf("%d", l0.CanonicalGasUsed)
	}
	fmt.Fprintf(b, "| GasUsed | %s | %s | %s |\n", s, c, icon)
}

func writeLayer1(b *strings.Builder, l1 *Layer1Result) {
	fmt.Fprintf(b, "## Layer 1: Transaction Receipt Comparison\n\n")
	if l1.Indeterminate {
		fmt.Fprintf(b, "**Indeterminate** — receipt comparison could not run, so this block is not validated: %s\n\n", l1.Error)
		return
	}
	fmt.Fprintf(b, "**Total transactions:** %d\n", l1.TotalTxs)

	if !l1.TxCountMatch {
		fmt.Fprintf(b, "**Transaction count mismatch** — chains have different numbers of transactions in this block.\n")
	}

	fmt.Fprintf(b, "**Divergent transactions:** %d\n\n", len(l1.Divergences))

	for _, div := range l1.Divergences {
		writeTxDivergence(b, div)
	}
}

func writeLayer2(b *strings.Builder, l2 *Layer2Result) {
	fmt.Fprintf(b, "## Layer 2: Logical State Comparison\n\n")
	if l2.Indeterminate {
		fmt.Fprintf(b, "**Indeterminate** — the logical state check could not run, so this block is not validated: %s\n\n", l2.Error)
		return
	}
	fmt.Fprintf(b, "**Accounts checked:** %d &nbsp;&nbsp; **Keys checked:** %d\n", l2.AccountsChecked, l2.KeysChecked)
	fmt.Fprintf(b, "**Divergent keys:** %d\n\n", len(l2.Divergences))

	if len(l2.Divergences) == 0 {
		return
	}

	fmt.Fprintf(b, "| Kind | Address | Slot | Shadow | Canonical |\n")
	fmt.Fprintf(b, "|------|---------|------|--------|----------|\n")
	for _, d := range l2.Divergences {
		slot := d.Slot
		if slot == "" {
			slot = "—"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n",
			d.Kind, truncateHash(d.Addr), truncateHash(slot),
			truncateHash(d.Shadow), truncateHash(d.Canonical))
	}
	fmt.Fprintf(b, "\n")
}

func writeTxDivergence(b *strings.Builder, div TxDivergence) {
	fmt.Fprintf(b, "### Transaction %d\n\n", div.TxIndex)
	fmt.Fprintf(b, "| Field | Shadow | Canonical |\n")
	fmt.Fprintf(b, "|-------|--------|----------|\n")

	for _, f := range div.Fields {
		fmt.Fprintf(b, "| %s | %s | %s |\n",
			f.Field,
			truncateValue(f.Shadow),
			truncateValue(f.Canonical))
	}
	fmt.Fprintf(b, "\n")
}

func writeRawDataNote(b *strings.Builder, height int64) {
	fmt.Fprintf(b, "## Raw Data\n\n")
	fmt.Fprintf(b, "Full block and block_results JSON from both chains is included in this report.\n")
	fmt.Fprintf(b, "Use `--json` flag to output the raw DivergenceReport for programmatic analysis.\n")
}

func truncateHash(h string) string {
	if len(h) <= 16 {
		return h
	}
	return h[:8] + "..." + h[len(h)-4:]
}

func truncateValue(v any) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}
