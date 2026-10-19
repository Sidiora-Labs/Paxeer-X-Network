package seinode

import (
	"strings"
	"testing"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// T9 — node watch phase vocab: Running is legal; Ready (network's terminal,
// illegal for a node) errors Invalid at parse with the legal set. Guards D2.
func TestNodeWatch_PhaseVocab(t *testing.T) {
	if err := cliutil.ValidatePhase("Running", nodePhases); err != nil {
		t.Errorf("Running must be a legal SeiNode phase; got %v", err)
	}
	err := cliutil.ValidatePhase("Ready", nodePhases)
	if err == nil {
		t.Fatalf("Ready must be illegal for a SeiNode (no Ready phase)")
	}
	if !strings.Contains(err.Error(), "Running") {
		t.Errorf("error must list the legal set incl. Running; got %q", err.Error())
	}
	if strings.Contains(err.Error(), "Ready,") || strings.Contains(err.Error(), ", Ready") {
		t.Errorf("legal set must not contain Ready; got %q", err.Error())
	}
}
