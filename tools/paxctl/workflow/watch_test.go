package workflow

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/sei-protocol/seictl/internal/cliutil"
)

// Complete (the terminal-success gate) is legal; Initializing (SeiNode vocab,
// not a workflow phase) errors Invalid at parse with the legal set.
func TestWorkflowWatch_PhaseVocab(t *testing.T) {
	if err := cliutil.ValidatePhase("Complete", workflowPhases); err != nil {
		t.Errorf("Complete must be a legal SeiNodeTaskWorkflow phase; got %v", err)
	}
	err := cliutil.ValidatePhase("Initializing", workflowPhases)
	if err == nil {
		t.Fatal("Initializing must be illegal for a workflow (SeiNode vocab, not a workflow phase)")
	}
	if !strings.Contains(err.Error(), "Complete") {
		t.Errorf("error must list the legal set incl. Complete; got %q", err.Error())
	}
}

// MatchPhase drives the kubectl-wait-compatible exit codes: Complete satisfies
// --until (exit 0), Failed is a terminal error carrying the plan's failure
// detail (nonzero), Running keeps streaming.
func TestWorkflowWatch_MatchPhase(t *testing.T) {
	cases := []struct {
		name         string
		phase        string
		failedDetail string
		wantDone     bool
		wantErrSub   string
	}{
		{"complete satisfies until", "Complete", "", true, ""},
		{"running keeps streaming", "Running", "", false, ""},
		{"failed is terminal with detail", "Failed", "reset-data: seid RPC is serving", false, "reset-data: seid RPC is serving"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]interface{}{
				"status": map[string]interface{}{"phase": tc.phase},
			}}
			if tc.failedDetail != "" {
				_ = unstructured.SetNestedField(obj.Object, tc.failedDetail, "status", "plan", "failedTaskDetail", "error")
			}
			done, err := cliutil.MatchPhase(obj, phaseComplete)
			if done != tc.wantDone {
				t.Errorf("done = %v; want %v", done, tc.wantDone)
			}
			if tc.wantErrSub == "" && err != nil {
				t.Errorf("err = %v; want nil", err)
			}
			if tc.wantErrSub != "" {
				if err == nil {
					t.Fatalf("err = nil; want containing %q", tc.wantErrSub)
				}
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("err = %q; want containing %q", err.Error(), tc.wantErrSub)
				}
			}
		})
	}
}
