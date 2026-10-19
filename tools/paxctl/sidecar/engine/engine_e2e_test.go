package engine

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newFileStore opens a file-backed SQLite store in a temp directory.
// The DB file and WAL/SHM files are cleaned up automatically by t.TempDir.
func newFileStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "sidecar.db")
	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("open file store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dbPath
}

// reopenStore closes the existing store and opens a new one against the
// same DB file, simulating a process restart.
func reopenStore(t *testing.T, old *SQLiteStore, dbPath string) *SQLiteStore {
	t.Helper()
	if err := old.Close(); err != nil {
		t.Fatalf("close old store: %v", err)
	}
	store, err := openStore(dbPath)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestE2E_TaskLifecycle(t *testing.T) {
	store, dbPath := newFileStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var handlerCalls atomic.Int32
	handlers := map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, params map[string]any) (json.RawMessage, error) {
			handlerCalls.Add(1)
			if params["fail"] == true {
				return nil, errors.New("intentional failure")
			}
			return nil, nil
		},
		TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			return nil, nil
		},
	}

	eng := NewEngine(ctx, handlers, store)

	// --- Phase 1: Submit tasks and verify completion ---

	t.Run("submit_and_complete", func(t *testing.T) {
		// Submit a successful task with a deterministic ID.
		const successID = "11111111-1111-1111-1111-111111111111"
		id, err := eng.Submit(Task{
			ID:     successID,
			Type:   TaskConfigPatch,
			Params: map[string]any{"file": "config.toml"},
		})
		if err != nil {
			t.Fatalf("submit: %v", err)
		}
		if id != successID {
			t.Fatalf("expected ID %q, got %q", successID, id)
		}

		result := waitForResult(t, eng, id)
		if result.Status != TaskStatusCompleted {
			t.Fatalf("expected completed, got %q", result.Status)
		}
		if result.Error != "" {
			t.Fatalf("expected no error, got %q", result.Error)
		}
		if result.CompletedAt == nil {
			t.Fatal("expected CompletedAt to be set")
		}
	})

	t.Run("submit_and_fail", func(t *testing.T) {
		// Submit a task that will fail.
		const failID = "22222222-2222-2222-2222-222222222222"
		id, err := eng.Submit(Task{
			ID:     failID,
			Type:   TaskConfigPatch,
			Params: map[string]any{"fail": true},
		})
		if err != nil {
			t.Fatalf("submit: %v", err)
		}

		result := waitForResult(t, eng, id)
		if result.Status != TaskStatusFailed {
			t.Fatalf("expected failed, got %q", result.Status)
		}
		if result.Error != "intentional failure" {
			t.Fatalf("expected error %q, got %q", "intentional failure", result.Error)
		}
	})

	t.Run("dedup_completed", func(t *testing.T) {
		// Re-submitting with the same ID should return existing without executing.
		before := handlerCalls.Load()
		const successID = "11111111-1111-1111-1111-111111111111"
		id, err := eng.Submit(Task{ID: successID, Type: TaskConfigPatch})
		if err != nil {
			t.Fatalf("dedup submit: %v", err)
		}
		if id != successID {
			t.Fatalf("dedup should return same ID")
		}
		// Handler should not have been called again.
		time.Sleep(50 * time.Millisecond)
		if handlerCalls.Load() != before {
			t.Fatal("handler should not have been called for deduped task")
		}
	})

	t.Run("mark_ready", func(t *testing.T) {
		if eng.Healthz() {
			t.Fatal("should not be ready yet")
		}
		id, _ := eng.Submit(Task{Type: TaskMarkReady})
		waitForResult(t, eng, id)
		if !eng.Healthz() {
			t.Fatal("should be ready after mark-ready")
		}
	})

	// --- Phase 2: Verify RecentResults lists everything ---

	t.Run("recent_results", func(t *testing.T) {
		results := eng.RecentResults()
		if len(results) < 3 {
			t.Fatalf("expected at least 3 results, got %d", len(results))
		}

		// Verify newest-first ordering.
		for i := 1; i < len(results); i++ {
			if results[i].SubmittedAt.After(results[i-1].SubmittedAt) {
				t.Fatalf("results not ordered newest-first at index %d", i)
			}
		}
	})

	// --- Phase 3: Remove a task ---

	t.Run("remove_task", func(t *testing.T) {
		const failID = "22222222-2222-2222-2222-222222222222"
		if deleted, err := eng.RemoveResult(failID); err != nil || !deleted {
			t.Fatalf("expected remove to return (true, nil), got (%v, %v)", deleted, err)
		}
		if eng.GetResult(failID) != nil {
			t.Fatal("expected nil after removal")
		}
		if deleted, err := eng.RemoveResult(failID); err != nil || deleted {
			t.Fatalf("second remove should return (false, nil), got (%v, %v)", deleted, err)
		}
	})

	// --- Phase 5: Simulate restart — close store, reopen, verify persistence ---

	t.Run("survives_restart", func(t *testing.T) {
		// Cancel the engine context and close the store.
		cancel()
		store2 := reopenStore(t, store, dbPath)

		ctx2, cancel2 := context.WithCancel(context.Background())
		defer cancel2()
		eng2 := NewEngine(ctx2, handlers, store2)

		// The successful task should still be there.
		const successID = "11111111-1111-1111-1111-111111111111"
		result := eng2.GetResult(successID)
		if result == nil {
			t.Fatal("completed task should survive restart")
		}
		if result.Status != TaskStatusCompleted {
			t.Fatalf("expected completed, got %q", result.Status)
		}
		if result.Params["file"] != "config.toml" {
			t.Fatalf("params not preserved: %v", result.Params)
		}

		// The removed task should stay removed.
		const failID = "22222222-2222-2222-2222-222222222222"
		if eng2.GetResult(failID) != nil {
			t.Fatal("removed task should not reappear after restart")
		}

		// RecentResults should return persisted results.
		results := eng2.RecentResults()
		if len(results) < 2 {
			t.Fatalf("expected at least 2 results after restart, got %d", len(results))
		}

		// Dedup should work against persisted state.
		id, err := eng2.Submit(Task{ID: successID, Type: TaskConfigPatch})
		if err != nil {
			t.Fatalf("dedup after restart: %v", err)
		}
		if id != successID {
			t.Fatalf("dedup should return existing ID")
		}

		// New tasks should work on the reopened store.
		const newID = "44444444-4444-4444-4444-444444444444"
		newTaskID, err := eng2.Submit(Task{ID: newID, Type: TaskConfigPatch})
		if err != nil {
			t.Fatalf("new task after restart: %v", err)
		}
		waitForResult(t, eng2, newTaskID)
		newResult := eng2.GetResult(newTaskID)
		if newResult.Status != TaskStatusCompleted {
			t.Fatalf("new task should complete, got %q", newResult.Status)
		}
	})
}

func TestE2E_StaleTaskRehydration(t *testing.T) {
	store, dbPath := newFileStore(t)

	// Simulate a previous crash: insert a task left as "running".
	stale := &TaskResult{
		ID:          "66666666-6666-6666-6666-666666666666",
		Type:        "config-patch",
		Status:      TaskStatusRunning,
		SubmittedAt: time.Now().UTC(),
	}
	store.Save(stale)

	// Reopen store and create a new engine (simulates restart).
	store2 := reopenStore(t, store, dbPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	handlers := map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	}
	eng := NewEngine(ctx, handlers, store2)
	eng.RehydrateStaleTasks()

	// Stale task should be re-executed and complete successfully.
	result := waitForResult(t, eng, stale.ID)
	if result.Status != TaskStatusCompleted {
		t.Fatalf("expected rehydrated task to complete, got %q", result.Status)
	}
}

func TestE2E_ConcurrentSubmit(t *testing.T) {
	store, _ := newFileStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var execCount atomic.Int32
	handlers := map[TaskType]TaskHandler{
		TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			execCount.Add(1)
			time.Sleep(10 * time.Millisecond)
			return nil, nil
		},
	}

	eng := NewEngine(ctx, handlers, store)

	// Submit 10 tasks concurrently.
	const n = 10
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ids[i], errs[i] = eng.Submit(Task{Type: TaskConfigPatch})
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
	}

	// Wait for all to complete.
	for _, id := range ids {
		waitForResult(t, eng, id)
	}

	if c := execCount.Load(); c != int32(n) {
		t.Fatalf("expected %d executions, got %d", n, c)
	}

	// All should be in the store.
	results := eng.RecentResults()
	if len(results) < n {
		t.Fatalf("expected at least %d results, got %d", n, len(results))
	}
}
