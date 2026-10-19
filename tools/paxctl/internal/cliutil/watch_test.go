package cliutil

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func crWithPhase(kind, phase, failedErr string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "sei.io/v1alpha1",
		"kind":       kind,
		"metadata":   map[string]interface{}{"name": "demo", "namespace": "nightly"},
	}}
	if phase != "" {
		_ = unstructured.SetNestedField(obj.Object, phase, "status", "phase")
	}
	if failedErr != "" {
		_ = unstructured.SetNestedField(obj.Object, failedErr, "status", "plan", "failedTaskDetail", "error")
	}
	return obj
}

func TestMatchPhase(t *testing.T) {
	cases := []struct {
		name      string
		obj       *unstructured.Unstructured
		until     string
		wantDone  bool
		wantError string
	}{
		{"network match", crWithPhase("SeiNetwork", "Ready", ""), "Ready", true, ""},
		{"node match", crWithPhase("SeiNode", "Running", ""), "Running", true, ""},
		{"no phase yet", crWithPhase("SeiNode", "", ""), "Running", false, ""},
		{"intermediate phase", crWithPhase("SeiNode", "Initializing", ""), "Running", false, ""},
		// T10 — terminal Failed lifts .status.plan.failedTaskDetail.error.
		{"failed surfaces error", crWithPhase("SeiNode", "Failed", "task seid-init failed: oom"), "Running", false, "terminal Failed phase: task seid-init failed: oom"},
		{"failed without detail", crWithPhase("SeiNode", "Failed", ""), "Running", false, "(no failedTaskDetail.error on status.plan)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done, err := MatchPhase(tc.obj, tc.until)
			if done != tc.wantDone {
				t.Errorf("done = %v; want %v", done, tc.wantDone)
			}
			if tc.wantError == "" && err != nil {
				t.Errorf("err = %v; want nil", err)
			}
			if tc.wantError != "" {
				if err == nil {
					t.Fatalf("err = nil; want containing %q", tc.wantError)
				}
				if !strings.Contains(err.Error(), tc.wantError) {
					t.Errorf("err = %q; want containing %q", err.Error(), tc.wantError)
				}
			}
		})
	}
}

func crWithGen(phase string, failedErr string, observedGen int64) *unstructured.Unstructured {
	obj := crWithPhase("SeiNodeTaskWorkflow", phase, failedErr)
	_ = unstructured.SetNestedField(obj.Object, observedGen, "status", "observedGeneration")
	return obj
}

func TestMatchPhaseAtLeastGeneration(t *testing.T) {
	cases := []struct {
		name     string
		obj      *unstructured.Unstructured
		until    string
		minGen   int64
		wantDone bool
		wantErr  bool
	}{
		// A terminal phase from a prior run (generation behind the apply) must
		// not be honored — no false-green, no replayed stale failure.
		{"complete but generation behind", crWithGen("Complete", "", 1), "Complete", 2, false, false},
		{"failed but generation behind not replayed", crWithGen("Failed", "boom", 1), "Complete", 2, false, false},
		// Once the controller catches up to the applied generation, honor it.
		{"complete and caught up", crWithGen("Complete", "", 2), "Complete", 2, true, false},
		{"complete and ahead", crWithGen("Complete", "", 3), "Complete", 2, true, false},
		{"failed and caught up errors", crWithGen("Failed", "boom", 2), "Complete", 2, false, true},
		// minGen 0 is the ungated case (existing RunWatch callers).
		{"ungated behaves like matchphase", crWithGen("Complete", "", 0), "Complete", 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done, err := MatchPhaseAtLeastGeneration(tc.obj, tc.until, tc.minGen)
			if done != tc.wantDone {
				t.Errorf("done = %v; want %v", done, tc.wantDone)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v; wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestWatchExitError_TimeoutShapedAsStatus(t *testing.T) {
	err := WatchExitError(context.DeadlineExceeded, "demo", "nightly", "Ready", 5*time.Minute)
	var apiErr apierrors.APIStatus
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v; want APIStatus", err)
	}
	s := apiErr.Status()
	if s.Reason != metav1.StatusReasonTimeout {
		t.Errorf("reason = %q; want Timeout", s.Reason)
	}
	if s.Code != 504 {
		t.Errorf("code = %d; want 504", s.Code)
	}
	if !strings.Contains(s.Message, "nightly/demo") || !strings.Contains(s.Message, "Ready") {
		t.Errorf("message %q missing namespace/name/until", s.Message)
	}
}

func TestWatchExitError_NonTimeoutPassesThrough(t *testing.T) {
	in := errors.New("some watch error")
	out := WatchExitError(in, "demo", "nightly", "Ready", time.Minute)
	if !errors.Is(out, in) {
		t.Errorf("non-timeout err must pass through unchanged; got %v", out)
	}
}

func TestValidatePhase(t *testing.T) {
	legal := []string{"Pending", "Running", "Failed"}
	if err := ValidatePhase("Running", legal); err != nil {
		t.Errorf("Running is legal; got err %v", err)
	}
	err := ValidatePhase("Ready", legal)
	if err == nil {
		t.Fatalf("Ready is illegal for this set; want error")
	}
	if !strings.Contains(err.Error(), "Pending, Running, Failed") {
		t.Errorf("err = %q; want the legal set in the message", err.Error())
	}
}
