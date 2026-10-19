package shadow

import (
	"strings"
	"testing"
)

func TestRenderMarkdown_Layer2(t *testing.T) {
	layer := 2
	report := &DivergenceReport{
		Height:    1000,
		Timestamp: "2026-06-17T00:00:00Z",
		Comparison: CompareResult{
			Height:          1000,
			Match:           false,
			MigrationMode:   true,
			DivergenceLayer: &layer,
			Layer2: &Layer2Result{
				AccountsChecked: 3,
				KeysChecked:     12,
				Divergences: []StateDivergence{
					{Kind: "storage", Addr: "0xabc", Slot: "0x01", Shadow: "0x2a", Canonical: "0x2b"},
					{Kind: "nonce", Addr: "0xdef", Shadow: "7", Canonical: "8"},
				},
			},
		},
	}

	md := RenderMarkdown(report)
	for _, want := range []string{
		"## Layer 2: Logical State Comparison",
		"Accounts checked:",
		"Divergent keys:",
		"storage",
		"nonce",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("rendered report missing %q\n---\n%s", want, md)
		}
	}
}
