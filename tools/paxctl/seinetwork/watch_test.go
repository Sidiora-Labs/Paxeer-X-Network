package seinetwork

import (
	"strings"
	"testing"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// T8 — network watch phase vocab: Ready is legal; Running (the SeiNode
// terminal, illegal for a network) errors Invalid at parse with the legal set.
func TestNetworkWatch_PhaseVocab(t *testing.T) {
	if err := cliutil.ValidatePhase("Ready", networkPhases); err != nil {
		t.Errorf("Ready must be a legal SeiNetwork phase; got %v", err)
	}
	err := cliutil.ValidatePhase("Running", networkPhases)
	if err == nil {
		t.Fatalf("Running must be illegal for a SeiNetwork (no Running phase)")
	}
	if !strings.Contains(err.Error(), "Ready") {
		t.Errorf("error must list the legal set incl. Ready; got %q", err.Error())
	}
}
