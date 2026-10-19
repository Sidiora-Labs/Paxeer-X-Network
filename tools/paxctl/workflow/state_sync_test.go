package workflow

import (
	"strings"
	"testing"
)

// preflightPhaseError refuses a re-run only over a terminal same-named workflow;
// absent or non-terminal phases pass (first run, or join an in-progress run).
func TestPreflightPhaseError(t *testing.T) {
	for _, phase := range []string{"", "Pending", "Running"} {
		if err := preflightPhaseError("sei", "n-state-sync", phase); err != nil {
			t.Errorf("phase %q: expected no refusal, got %v", phase, err)
		}
	}

	complete := preflightPhaseError("sei", "n-state-sync", phaseComplete)
	if complete == nil {
		t.Fatal("Complete must be refused")
	}
	if !strings.Contains(strings.ToLower(complete.Error()), "delete") {
		t.Errorf("Complete refusal must guide to delete; got %q", complete.Error())
	}
	if !strings.Contains(complete.Error(), "--name") {
		t.Errorf("Complete refusal must offer --name; got %q", complete.Error())
	}

	failed := preflightPhaseError("sei", "n-state-sync", phaseFailed)
	if failed == nil {
		t.Fatal("Failed must be refused")
	}
	if !strings.Contains(failed.Error(), forceDeleteAnnotation) {
		t.Errorf("Failed refusal must name the force-delete annotation; got %q", failed.Error())
	}
	if !strings.Contains(failed.Error(), "holds the node") {
		t.Errorf("Failed refusal must explain the node stays held; got %q", failed.Error())
	}
}

// The Complete handoff hands the operator the node-side catch-up check: the
// workflow completes at release, so the resync runs after exit 0 and the
// printed commands are the verification path.
func TestEmitCompleteHandoff(t *testing.T) {
	var b strings.Builder
	emitCompleteHandoff(&b, "pacific-1", "rpc-node-0")
	out := b.String()
	for _, want := range []string{
		"node rpc-node-0 released",
		"seictl node watch rpc-node-0 --until=caught-up -n pacific-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("handoff output missing %q; got:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if !strings.HasPrefix(line, "seictl:") {
			t.Errorf("handoff line lacks the seictl: stderr prefix: %q", line)
		}
	}
}
