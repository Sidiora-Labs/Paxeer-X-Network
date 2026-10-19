package tasks

import (
	"context"
	"fmt"
	"testing"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

// fakePurger records DeleteByType calls and can inject a failure.
type fakePurger struct {
	deleted []string
	n       int
	err     error
}

func (p *fakePurger) DeleteByType(taskType string) (int, error) {
	p.deleted = append(p.deleted, taskType)
	return p.n, p.err
}

func TestMarkNotReady_PurgesMarkReadyRecords(t *testing.T) {
	p := &fakePurger{n: 2}
	if _, err := NewMarkNotReadier(p).Handler()(context.Background(), nil); err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if len(p.deleted) != 1 || p.deleted[0] != string(engine.TaskMarkReady) {
		t.Errorf("expected a single purge of %q, got %v", engine.TaskMarkReady, p.deleted)
	}
}

func TestMarkNotReady_PurgeFailurePropagates(t *testing.T) {
	p := &fakePurger{err: fmt.Errorf("store offline")}
	_, err := NewMarkNotReadier(p).Handler()(context.Background(), nil)
	if err == nil {
		t.Fatal("expected purge failure to propagate so the engine skips the readiness flip")
	}
}
