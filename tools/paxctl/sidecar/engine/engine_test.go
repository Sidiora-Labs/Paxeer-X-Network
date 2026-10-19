package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func newTestEngine(t *testing.T, handlers map[TaskType]TaskHandler) *Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store, err := NewMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return NewEngine(ctx, handlers, store)
}

func waitForHealthz(t *testing.T, eng *Engine) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Healthz() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for healthz")
}

func waitForStatus(t *testing.T, eng *Engine, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Status().Status == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for status %q, got %q", want, eng.Status().Status)
}

func waitForResult(t *testing.T, eng *Engine, id string) *TaskResult {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := eng.GetResult(id); r != nil && r.CompletedAt != nil {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for result %s", id)
	return nil
}

func cancelRegistrySize(eng *Engine) int {
	eng.mu.Lock()
	defer eng.mu.Unlock()
	return len(eng.cancels)
}

// --- Submit tests ---

func TestSubmitAccepts(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, err := eng.Submit(Task{Type: TaskMarkReady})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestSubmitCapturesInBandResult(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return json.RawMessage(`{"genesisHash":"deadbeef"}`), nil
		},
	})

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, eng, id)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if string(result.Result) != `{"genesisHash":"deadbeef"}` {
		t.Fatalf("result payload = %q, want in-band genesisHash", string(result.Result))
	}
}

// A ctx-cancelled run (engine shutdown mid-task) must be left 'running' for
// RehydrateStaleTasks, not persisted as Failed — else an in-flight sign-tx is
// stranded (its result/marker never revisited).
func TestRunTaskCtxCancel_LeavesRunningForRehydrate(t *testing.T) {
	ran := make(chan struct{})
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			close(ran)
			return nil, context.Canceled // simulates a poll truncated by shutdown
		},
	})

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	<-ran
	time.Sleep(50 * time.Millisecond) // let runTask's post-handler guard run

	r := eng.GetResult(id)
	if r == nil {
		t.Fatal("task row missing")
	}
	if r.Status != TaskStatusRunning {
		t.Fatalf("ctx-cancelled task must stay running for rehydration, got %q", r.Status)
	}
	if r.CompletedAt != nil {
		t.Fatal("truncated task must not be marked terminal")
	}
}

func TestSubmitNoResultPayloadIsNil(t *testing.T) {
	// Backward-compat: a handler that emits nothing leaves Result nil and
	// the field is omitted from the wire, so the deployed controller is
	// unaffected.
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, eng, id)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("status = %q, want completed", result.Status)
	}
	if result.Result != nil {
		t.Fatalf("result payload = %q, want nil for a handler that emits nothing", string(result.Result))
	}
	out, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(out), `"result"`) {
		t.Fatalf("nil result must be omitted from the wire; got %s", out)
	}
}

func TestSubmitFailedTaskStampsResult(t *testing.T) {
	// The engine stamps the handler's returned result on both the success
	// and error paths, so a failed run still carries its (partial) result.
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return json.RawMessage(`{"genesisHash":"partial"}`), errors.New("boom")
		},
	})

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	result := waitForResult(t, eng, id)
	if result.Status != TaskStatusFailed {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if result.Error != "boom" {
		t.Fatalf("error = %q, want boom", result.Error)
	}
	if string(result.Result) != `{"genesisHash":"partial"}` {
		t.Fatalf("failed task should carry its result; got %q", string(result.Result))
	}
}

func TestSubmitRejectsUnknownType(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{})

	_, err := eng.Submit(Task{Type: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for unknown task type")
	}
}

func TestSubmitCallerProvidedID(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	const customID = "aaaaaaaa-1111-2222-3333-444444444444"
	id, err := eng.Submit(Task{ID: customID, Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if id != customID {
		t.Fatalf("expected ID %q, got %q", customID, id)
	}

	result := waitForResult(t, eng, id)
	if result.ID != customID {
		t.Fatalf("result ID = %q, want %q", result.ID, customID)
	}
}

func TestSubmitInvalidIDReturnsTypedError(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	_, err := eng.Submit(Task{ID: "not-a-uuid", Type: TaskConfigPatch})
	if err == nil {
		t.Fatal("expected error for non-UUID ID")
	}
	if !errors.Is(err, ErrInvalidTaskID) {
		t.Fatalf("expected ErrInvalidTaskID, got: %v", err)
	}
}

func TestSubmitDedupExistingActive(t *testing.T) {
	started := make(chan struct{})
	blocked := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-blocked
			return nil, nil
		},
	}, newTestStore(t))

	const dedupID = "bbbbbbbb-1111-2222-3333-444444444444"
	id1, err := eng.Submit(Task{ID: dedupID, Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	id2, err := eng.Submit(Task{ID: dedupID, Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("dedup should return same ID: got %q and %q", id1, id2)
	}

	close(blocked)
}

func TestSubmitDedupExistingCompleted(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	const dedupID = "cccccccc-1111-2222-3333-444444444444"
	id1, _ := eng.Submit(Task{ID: dedupID, Type: TaskConfigPatch})
	waitForResult(t, eng, id1)

	id2, err := eng.Submit(Task{ID: dedupID, Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("dedup submit: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("dedup should return same ID: got %q and %q", id1, id2)
	}
}

func TestSubmitNoIDGeneratesUUID(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id1, _ := eng.Submit(Task{Type: TaskConfigPatch})
	id2, _ := eng.Submit(Task{Type: TaskConfigPatch})

	if id1 == "" || id2 == "" {
		t.Fatal("expected non-empty generated IDs")
	}
	if id1 == id2 {
		t.Fatal("generated IDs should be unique")
	}
}

func TestSubmitConcurrent(t *testing.T) {
	var callCount atomic.Int32
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			callCount.Add(1)
			time.Sleep(20 * time.Millisecond)
			return nil, nil
		},
	})

	id1, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	id2, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}

	waitForResult(t, eng, id1)
	waitForResult(t, eng, id2)

	if c := callCount.Load(); c != 2 {
		t.Fatalf("expected 2 concurrent executions, got %d", c)
	}
}

func TestMarkReadySetsHealthz(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	if eng.Healthz() {
		t.Fatal("healthz should be false before mark-ready")
	}

	_, _ = eng.Submit(Task{Type: TaskMarkReady})
	waitForHealthz(t, eng)

	if !eng.Healthz() {
		t.Fatal("healthz should be true after mark-ready")
	}
}

func TestHealthzMonotonicity(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return nil, context.DeadlineExceeded
		},
	})

	id, _ := eng.Submit(Task{Type: TaskMarkReady})
	waitForHealthz(t, eng)
	waitForResult(t, eng, id)

	id2, _ := eng.Submit(Task{Type: TaskConfigPatch})
	waitForResult(t, eng, id2)

	if !eng.Healthz() {
		t.Fatal("healthz should remain true after runtime failure")
	}
}

func TestStatusReflectsReady(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	if eng.Status().Status != "Initializing" {
		t.Fatalf("expected Initializing initially, got %q", eng.Status().Status)
	}

	_, _ = eng.Submit(Task{Type: TaskMarkReady})
	waitForStatus(t, eng, "Ready")

	if eng.Status().Status != "Ready" {
		t.Fatalf("expected Ready after mark-ready, got %q", eng.Status().Status)
	}
}

// --- Result tests ---

func TestGetResultReturnsCompleted(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	result := waitForResult(t, eng, id)

	if result.ID != id {
		t.Fatalf("expected ID %q, got %q", id, result.ID)
	}
	if result.Type != string(TaskConfigPatch) {
		t.Fatalf("expected type %q, got %q", TaskConfigPatch, result.Type)
	}
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected status %q, got %q", TaskStatusCompleted, result.Status)
	}
	if result.Error != "" {
		t.Fatalf("expected no error, got %q", result.Error)
	}
}

func TestGetResultReturnsFailure(t *testing.T) {
	handlerErr := errors.New("handler failed")
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, handlerErr },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	result := waitForResult(t, eng, id)

	if result.Status != TaskStatusFailed {
		t.Fatalf("expected status %q, got %q", TaskStatusFailed, result.Status)
	}
	if result.Error != handlerErr.Error() {
		t.Fatalf("expected error %q, got %q", handlerErr.Error(), result.Error)
	}
}

func TestGetResultReturnsRunning(t *testing.T) {
	started := make(chan struct{})
	blocked := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-blocked
			return nil, nil
		},
	}, newTestStore(t))

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	result := eng.GetResult(id)
	if result == nil {
		t.Fatal("expected non-nil result for active task")
	}
	if result.Status != TaskStatusRunning {
		t.Fatalf("expected status %q, got %q", TaskStatusRunning, result.Status)
	}
	if result.CompletedAt != nil {
		t.Fatal("expected CompletedAt to be nil for running task")
	}

	close(blocked)
	completed := waitForResult(t, eng, id)
	if completed.Status != TaskStatusCompleted {
		t.Fatalf("expected status %q after completion, got %q", TaskStatusCompleted, completed.Status)
	}
}

func TestRecentResultsReturnsAll(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	var ids []string
	for i := 0; i < 7; i++ {
		id, _ := eng.Submit(Task{Type: TaskConfigPatch})
		ids = append(ids, id)
	}
	for _, id := range ids {
		waitForResult(t, eng, id)
	}

	results := eng.RecentResults()
	if len(results) != 7 {
		t.Fatalf("expected 7 results, got %d", len(results))
	}
}

func TestRecentResultsIncludesActive(t *testing.T) {
	started := make(chan struct{})
	blocked := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-blocked
			return nil, nil
		},
	}, newTestStore(t))

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	results := eng.RecentResults()
	if len(results) != 1 {
		t.Fatalf("expected 1 result (active), got %d", len(results))
	}
	if results[0].ID != id {
		t.Fatalf("expected ID %q, got %q", id, results[0].ID)
	}
	if results[0].Status != TaskStatusRunning {
		t.Fatalf("expected status %q, got %q", TaskStatusRunning, results[0].Status)
	}

	close(blocked)
}

func TestRemoveResult(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	waitForResult(t, eng, id)

	if deleted, err := eng.RemoveResult(id); err != nil || !deleted {
		t.Fatalf("expected remove to return (true, nil), got (%v, %v)", deleted, err)
	}
	if deleted, err := eng.RemoveResult(id); err != nil || deleted {
		t.Fatalf("second remove should return (false, nil), got (%v, %v)", deleted, err)
	}
	if eng.GetResult(id) != nil {
		t.Fatal("expected nil after removal")
	}
}

func TestRemoveActiveTaskCancels(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(ctx context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		},
	}, newTestStore(t))

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	if deleted, err := eng.RemoveResult(id); err != nil || !deleted {
		t.Fatalf("expected remove to return (true, nil), got (%v, %v)", deleted, err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("task goroutine did not observe cancellation after RemoveResult")
	}
	if eng.GetResult(id) != nil {
		t.Fatal("expected nil after removal")
	}
}

// A task that completes on its own must not leak its cancel func in the
// registry — otherwise every finished task pins a context until shutdown.
func TestCancelRegistryClearedOnCompletion(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	waitForResult(t, eng, id)

	// clearCancel runs in the goroutine's defer, just after the result is
	// persisted, so poll rather than reading the registry once.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cancelRegistrySize(eng) == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("cancel func leaked after completion: registry size %d", cancelRegistrySize(eng))
}

// Rehydrated tasks must get a per-task cancellable context too, not the engine
// root — else a stale task resumed after a crash is un-cancellable and DELETE
// would orphan its goroutine.
func TestRemoveRehydratedTaskCancels(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	store := newTestStore(t)
	const id = "cccccccc-1111-2222-3333-444444444444"
	if err := store.Save(&TaskResult{
		ID: id, Type: string(TaskConfigPatch), Status: TaskStatusRunning, Run: 1,
		SubmittedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed stale task: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(ctx context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		},
	}, store)
	eng.RehydrateStaleTasks()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for rehydrated task to start")
	}

	if deleted, err := eng.RemoveResult(id); err != nil || !deleted {
		t.Fatalf("expected remove to return (true, nil), got (%v, %v)", deleted, err)
	}
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("rehydrated task goroutine did not observe cancellation")
	}
	if eng.GetResult(id) != nil {
		t.Fatal("expected nil after removal")
	}
}

// A superseded registration's late cleanup must not cancel or drop a newer
// registration's entry under the same ID: cleanup is a compare-and-delete on the
// generation the registration minted, so the newer run's live context and entry
// survive. Driving newTaskContext/clearCancel directly makes the interleaving
// deterministic under -race.
func TestClearCancelIgnoresSupersededRun(t *testing.T) {
	eng := newTestEngine(t, nil)
	const id = "aaaaaaaa-1111-2222-3333-444444444444"

	eng.mu.Lock()
	_, gen1 := eng.newTaskContext(id)        // first registration
	retryCtx, gen2 := eng.newTaskContext(id) // second overwrites the entry
	eng.mu.Unlock()

	eng.clearCancel(id, gen1) // the superseded registration's late defer must be a no-op

	if retryCtx.Err() != nil {
		t.Fatal("the superseded run's stale clearCancel cancelled the live context")
	}
	if cancelRegistrySize(eng) != 1 {
		t.Fatalf("the live entry must survive the superseded run's cleanup; size %d", cancelRegistrySize(eng))
	}

	eng.clearCancel(id, gen2) // the live registration's own cleanup still cancels and removes it
	if retryCtx.Err() == nil {
		t.Fatal("the live run's own clearCancel must cancel its context")
	}
	if cancelRegistrySize(eng) != 0 {
		t.Fatalf("the live run's entry must be removed by its own clearCancel; size %d", cancelRegistrySize(eng))
	}
}

// A resubmit after a delete reuses the row-scoped run number (run resets to 1
// once the row is gone), so cleanup must key on the never-reused generation, not
// run. When a registration's context is cancelled and its entry removed (as a
// successful RemoveResult does) and a fresh registration then lands under the
// same ID with the same run number before the first's goroutine wakes, the
// first's late cleanup must leave the fresh registration's context and entry
// intact. Driven through the real registry mutation path so the run-number
// collision is genuine, not simulated.
func TestClearCancelIgnoresSupersededRunAfterResubmit(t *testing.T) {
	eng := newTestEngine(t, nil)
	const id = "aaaaaaaa-1111-2222-3333-444444444444"

	// Registration A. In a live run this is Submit's newTaskContext with run 1.
	eng.mu.Lock()
	ctxA, genA := eng.newTaskContext(id)
	eng.mu.Unlock()

	// RemoveResult cancels A's context and removes its entry on a successful
	// delete. A's goroutine has not yet woken to run its deferred clearCancel.
	eng.mu.Lock()
	if entry, ok := eng.cancels[id]; ok && entry.gen == genA {
		entry.cancel()
		delete(eng.cancels, id)
	}
	eng.mu.Unlock()
	if ctxA.Err() == nil {
		t.Fatal("the delete should have cancelled registration A's context")
	}

	// A fresh resubmit lands under the same ID. run would again be 1 here (the
	// row is gone), but the generation is strictly newer.
	eng.mu.Lock()
	ctxB, genB := eng.newTaskContext(id)
	eng.mu.Unlock()
	if genB <= genA {
		t.Fatalf("generation must be strictly increasing: genA=%d genB=%d", genA, genB)
	}

	// A finally wakes and runs its deferred cleanup. Under a run-keyed registry
	// both A and B carry run 1, so this would match and cancel B; keyed on the
	// generation it is a no-op.
	eng.clearCancel(id, genA)

	if ctxB.Err() != nil {
		t.Fatal("registration A's stale cleanup cancelled the resubmit's live context")
	}
	if cancelRegistrySize(eng) != 1 {
		t.Fatalf("the resubmit's entry must survive A's stale cleanup; size %d", cancelRegistrySize(eng))
	}
}

// toggleDeleteFailStore makes store.Delete fail on demand so a DELETE can be
// driven through the failure path and then a retry through the success path.
type toggleDeleteFailStore struct {
	*SQLiteStore
	failDelete atomic.Bool
}

func (s *toggleDeleteFailStore) Delete(id string) (bool, error) {
	if s.failDelete.Load() {
		return false, fmt.Errorf("simulated delete failure")
	}
	return s.SQLiteStore.Delete(id)
}

// When store.Delete fails, RemoveResult still cancels the goroutine (work stops)
// but surfaces the error and leaves the row 'running' so it stays recoverable —
// a DELETE retry (or rehydration on restart) can still act on it. It must not
// report the row removed, and it must not drop the registry entry, so the row is
// never stranded 'running' with no goroutine that Submit's dedup would refuse to
// re-run.
func TestRemoveResultFailedDeleteDoesNotStrand(t *testing.T) {
	inner, err := NewMemoryStore()
	if err != nil {
		t.Fatalf("memory store: %v", err)
	}
	t.Cleanup(func() { _ = inner.Close() })
	store := &toggleDeleteFailStore{SQLiteStore: inner}
	store.failDelete.Store(true)

	started := make(chan struct{})
	stopped := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(ctx context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-ctx.Done()
			close(stopped)
			return nil, ctx.Err()
		},
	}, store)

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	deleted, err := eng.RemoveResult(id)
	if err == nil {
		t.Fatal("RemoveResult must surface the store.Delete failure so the caller can retry")
	}
	if deleted {
		t.Fatal("RemoveResult must not report the row removed when store.Delete failed")
	}

	// The work is still cancelled — cancellation is unconditional.
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("task goroutine did not observe cancellation despite the delete failure")
	}

	// The row survives and stays 'running', so it remains actionable.
	r := eng.GetResult(id)
	if r == nil {
		t.Fatal("row must survive a failed delete so it stays recoverable")
	}
	if r.Status != TaskStatusRunning {
		t.Fatalf("row status = %q, want running (recoverable)", r.Status)
	}

	// A DELETE retry against a now-healthy store recovers it.
	store.failDelete.Store(false)
	deleted, err = eng.RemoveResult(id)
	if err != nil {
		t.Fatalf("DELETE retry: %v", err)
	}
	if !deleted {
		t.Fatal("DELETE retry should report the row removed")
	}
	if eng.GetResult(id) != nil {
		t.Fatal("row should be gone after a successful DELETE retry")
	}
}

// A handler that surfaces cancellation as a non-context.Canceled error must not
// resurrect a Failed row after DELETE removed it: the ctx.Err() guard keys the
// no-op on the cancelled context, not only on a context.Canceled-wrapping error.
func TestRunTaskSyncSuppressesErrorUnderCancellation(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return nil, errors.New("boom")
		},
	})

	const id = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the task's context is already cancelled, as after RemoveResult
	eng.runTaskSync(WithTaskID(ctx, id), id, TaskConfigPatch, eng.handlers[TaskConfigPatch], nil, time.Now().UTC(), 1, 1)

	if r := eng.GetResult(id); r != nil {
		t.Fatalf("non-context error under a cancelled ctx must not persist; got %q row", r.Status)
	}
}

// A per-task deadline surfaces as context.DeadlineExceeded, NOT context.Canceled.
// The suppression guard in runTaskSync keys only on Canceled, so a timed-out task
// must fall through to Failed-persistence rather than being stranded 'running'.
func TestRunTaskSyncPersistsFailedOnDeadlineExceeded(t *testing.T) {
	cases := []struct {
		name string
		id   string
		ctx  func(t *testing.T) context.Context
	}{
		{
			name: "handler-internal deadline with a live task context",
			id:   "11111111-1111-1111-1111-111111111111",
			ctx:  func(*testing.T) context.Context { return context.Background() },
		},
		{
			name: "task context itself deadline-exceeded",
			id:   "22222222-2222-2222-2222-222222222222",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
				t.Cleanup(cancel)
				<-ctx.Done()
				return ctx
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng := newTestEngine(t, map[TaskType]TaskHandler{
				TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
					return nil, context.DeadlineExceeded
				},
			})

			ctx := WithTaskID(tc.ctx(t), tc.id)
			eng.runTaskSync(ctx, tc.id, TaskConfigPatch, eng.handlers[TaskConfigPatch], nil, time.Now().UTC(), 1, 1)

			r := eng.GetResult(tc.id)
			if r == nil {
				t.Fatal("DeadlineExceeded must persist a row; got nil (wrongly suppressed)")
			}
			if r.Status != TaskStatusFailed {
				t.Fatalf("status = %q, want %q", r.Status, TaskStatusFailed)
			}
			if !strings.Contains(r.Error, context.DeadlineExceeded.Error()) {
				t.Fatalf("error = %q, want it to carry %q", r.Error, context.DeadlineExceeded.Error())
			}
		})
	}
}

// --- Context cancellation ---

func TestContextCancellationStopsEngine(t *testing.T) {
	var mu sync.Mutex
	executed := 0
	ctx, cancel := context.WithCancel(context.Background())
	store, _ := NewMemoryStore()
	t.Cleanup(func() { store.Close() })
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			mu.Lock()
			executed++
			mu.Unlock()
			return nil, nil
		},
	}, store)

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	waitForResult(t, eng, id)
	cancel()

	mu.Lock()
	if executed != 1 {
		t.Fatalf("expected 1 execution, got %d", executed)
	}
	mu.Unlock()
}

// --- Long-running task tests ---

func TestLongRunningTaskCompletion(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	result := waitForResult(t, eng, id)

	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected status %q, got %q", TaskStatusCompleted, result.Status)
	}
}

func TestLongRunningTaskFailure(t *testing.T) {
	handlerErr := errors.New("fatal crash")
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, handlerErr },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	result := waitForResult(t, eng, id)

	if result.Status != TaskStatusFailed {
		t.Fatalf("expected status %q, got %q", TaskStatusFailed, result.Status)
	}
	if result.Error != handlerErr.Error() {
		t.Fatalf("expected error %q, got %q", handlerErr.Error(), result.Error)
	}
}

func TestLongRunningTaskDoesNotBlockOthers(t *testing.T) {
	bgStarted := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(ctx context.Context, _ map[string]any) (json.RawMessage, error) {
			close(bgStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	}, newTestStore(t))

	_, _ = eng.Submit(Task{Type: TaskConfigPatch})

	select {
	case <-bgStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for long-running task to start")
	}

	id, err := eng.Submit(Task{Type: TaskMarkReady})
	if err != nil {
		t.Fatalf("second submit should not be blocked: %v", err)
	}
	waitForResult(t, eng, id)
}

// --- Status-aware Submit tests ---

func TestSubmitReExecutesFailedTask(t *testing.T) {
	calls := 0
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("transient failure")
			}
			return nil, nil
		},
	})

	const taskID = "dddddddd-1111-2222-3333-444444444444"

	// First submit: task fails.
	id1, err := eng.Submit(Task{ID: taskID, Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("first submit: %v", err)
	}
	r1 := waitForResult(t, eng, id1)
	if r1.Status != TaskStatusFailed {
		t.Fatalf("expected failed, got %s", r1.Status)
	}
	if r1.Run != 1 {
		t.Fatalf("expected run=1, got %d", r1.Run)
	}

	// Second submit with same ID: should re-execute.
	id2, err := eng.Submit(Task{ID: taskID, Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("expected same ID, got %q and %q", id1, id2)
	}

	r2 := waitForResult(t, eng, id2)
	if r2.Status != TaskStatusCompleted {
		t.Fatalf("expected completed on retry, got %s", r2.Status)
	}
	if r2.Run != 2 {
		t.Fatalf("expected run=2, got %d", r2.Run)
	}
	if calls != 2 {
		t.Fatalf("expected handler called twice, got %d", calls)
	}
}

func TestSubmitReExecutesFailedTaskThatFailsAgain(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return nil, errors.New("persistent failure")
		},
	})

	const taskID = "eeeeeeee-1111-2222-3333-444444444444"

	id, _ := eng.Submit(Task{ID: taskID, Type: TaskConfigPatch})
	waitForResult(t, eng, id)

	// Re-submit: still fails.
	eng.Submit(Task{ID: taskID, Type: TaskConfigPatch})
	r := waitForResult(t, eng, id)

	if r.Status != TaskStatusFailed {
		t.Fatalf("expected failed, got %s", r.Status)
	}
	if r.Run != 2 {
		t.Fatalf("expected run=2, got %d", r.Run)
	}
}

func TestSubmitDoesNotIncrementRunOnRehydration(t *testing.T) {
	// Create a store with a stale running task (simulates pod crash).
	store := newTestStore(t)
	now := time.Now().UTC()
	_ = store.Save(&TaskResult{
		ID:          "ffffffff-1111-2222-3333-444444444444",
		Type:        string(TaskConfigPatch),
		Status:      TaskStatusRunning,
		Run:         1,
		SubmittedAt: now,
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	}, store)
	eng.RehydrateStaleTasks()

	r := waitForResult(t, eng, "ffffffff-1111-2222-3333-444444444444")
	if r.Run != 1 {
		t.Fatalf("expected run=1 after rehydration (not incremented), got %d", r.Run)
	}
	if r.Status != TaskStatusCompleted {
		t.Fatalf("expected completed after rehydration, got %s", r.Status)
	}
}

func TestSubmitConcurrentSameFailedID(t *testing.T) {
	started := make(chan struct{}, 2)
	blocked := make(chan struct{})
	var callCount atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store := newTestStore(t)
	eng := NewEngine(ctx, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			callCount.Add(1)
			started <- struct{}{}
			<-blocked
			return nil, nil
		},
	}, store)

	const taskID = "11111111-2222-3333-4444-555555555555"

	// Seed a failed task directly in the store.
	now := time.Now().UTC()
	_ = store.Save(&TaskResult{
		ID:          taskID,
		Type:        string(TaskConfigPatch),
		Status:      TaskStatusFailed,
		Run:         1,
		Error:       "failed",
		SubmittedAt: now,
		CompletedAt: &now,
	})

	// Two concurrent submits of the same failed ID.
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			eng.Submit(Task{ID: taskID, Type: TaskConfigPatch})
		}()
	}
	wg.Wait()

	// Unblock the handler and wait for completion.
	close(blocked)
	waitForResult(t, eng, taskID)

	// The mutex serializes Submit: the first sees "failed" and re-executes,
	// the second sees "running" (from the first's Save) and no-ops.
	if c := callCount.Load(); c != 1 {
		t.Fatalf("expected exactly 1 re-execution, got %d", c)
	}
}

func TestSubmitRunFieldOnFirstSubmit(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})

	id, _ := eng.Submit(Task{Type: TaskConfigPatch})
	r := waitForResult(t, eng, id)

	if r.Run != 1 {
		t.Fatalf("expected run=1 on first submit, got %d", r.Run)
	}
}

func TestTaskErrorProducesRichErrorString(t *testing.T) {
	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return nil, &TaskError{
				Task:      "config-patch",
				Operation: "S3",
				Message:   "bucket not found",
				Hint:      "check SEI_SNAPSHOT_BUCKET",
			}
		},
	})

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	r := waitForResult(t, eng, id)
	if r.Status != TaskStatusFailed {
		t.Fatalf("expected Failed, got %s", r.Status)
	}
	if !strings.Contains(r.Error, "config-patch") {
		t.Errorf("error should contain task name, got: %s", r.Error)
	}
	if !strings.Contains(r.Error, "bucket not found") {
		t.Errorf("error should contain message, got: %s", r.Error)
	}
	if !strings.Contains(r.Error, "hint:") {
		t.Errorf("error should contain hint, got: %s", r.Error)
	}
}

func TestSubmitHandlerPanicBecomesFailedTask(t *testing.T) {
	before := testutil.ToFloat64(taskPanics.WithLabelValues(string(TaskConfigPatch)))

	eng := newTestEngine(t, map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			panic("kaboom")
		},
	})

	id, err := eng.Submit(Task{Type: TaskConfigPatch})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}

	r := waitForResult(t, eng, id)
	if r.Status != TaskStatusFailed {
		t.Fatalf("panicking handler should produce Failed, got %s", r.Status)
	}
	if !strings.Contains(r.Error, "panicked") || !strings.Contains(r.Error, "kaboom") {
		t.Errorf("error should describe the panic, got: %s", r.Error)
	}

	after := testutil.ToFloat64(taskPanics.WithLabelValues(string(TaskConfigPatch)))
	if after != before+1 {
		t.Errorf("taskPanics delta = %v, want 1", after-before)
	}
}
