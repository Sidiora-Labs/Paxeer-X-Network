package engine

import (
	"encoding/json"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := NewMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStoreSaveAndGet(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Truncate(time.Nanosecond)
	completed := now.Add(time.Second)

	r := &TaskResult{
		ID:          "aaaaaaaa-1111-2222-3333-444444444444",
		Type:        "config-patch",
		Status:      TaskStatusCompleted,
		Params:      map[string]any{"file": "config.toml", "nested": map[string]any{"key": "val"}},
		Error:       "",
		SubmittedAt: now,
		CompletedAt: &completed,
	}

	if err := s.Save(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.ID != r.ID {
		t.Fatalf("ID = %q, want %q", got.ID, r.ID)
	}
	if got.Type != r.Type {
		t.Fatalf("Type = %q, want %q", got.Type, r.Type)
	}
	if got.Status != r.Status {
		t.Fatalf("Status = %q, want %q", got.Status, r.Status)
	}
	if got.Error != r.Error {
		t.Fatalf("Error = %q, want %q", got.Error, r.Error)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected non-nil CompletedAt")
	}

	// Verify nested params survived JSON round-trip.
	nested, ok := got.Params["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", got.Params["nested"])
	}
	if nested["key"] != "val" {
		t.Fatalf("nested.key = %q, want %q", nested["key"], "val")
	}
}

func TestStoreGetNotFound(t *testing.T) {
	s := newTestStore(t)

	got, err := s.Get("nonexistent-id")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestStoreSaveUpsert(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Truncate(time.Nanosecond)

	r := &TaskResult{
		ID:          "bbbbbbbb-1111-2222-3333-444444444444",
		Type:        "config-patch",
		Status:      TaskStatusRunning,
		SubmittedAt: now,
	}
	if err := s.Save(r); err != nil {
		t.Fatalf("first save: %v", err)
	}

	// Update status.
	completed := now.Add(time.Second)
	r.Status = TaskStatusCompleted
	r.CompletedAt = &completed
	if err := s.Save(r); err != nil {
		t.Fatalf("second save: %v", err)
	}

	got, _ := s.Get(r.ID)
	if got.Status != TaskStatusCompleted {
		t.Fatalf("Status = %q after upsert, want %q", got.Status, TaskStatusCompleted)
	}
	if got.CompletedAt == nil {
		t.Fatal("expected CompletedAt after upsert")
	}
}

func TestStoreListOrdering(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		r := &TaskResult{
			ID:          "list-" + string(rune('a'+i)) + "0000000-0000-0000-0000-000000000000",
			Type:        "config-patch",
			Status:      TaskStatusCompleted,
			SubmittedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.Save(r); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	results, err := s.List(10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}

	// Newest first.
	for i := 1; i < len(results); i++ {
		if results[i].SubmittedAt.After(results[i-1].SubmittedAt) {
			t.Fatalf("results not ordered newest-first at index %d", i)
		}
	}
}

func TestStoreListLimit(t *testing.T) {
	s := newTestStore(t)
	base := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	for i := 0; i < 20; i++ {
		r := &TaskResult{
			ID:          "limit-" + string(rune('a'+i)) + "000000-0000-0000-0000-000000000000",
			Type:        "config-patch",
			Status:      TaskStatusCompleted,
			SubmittedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.Save(r); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	results, err := s.List(5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 results, got %d", len(results))
	}
}

func TestStoreDelete(t *testing.T) {
	s := newTestStore(t)

	r := &TaskResult{
		ID:          "cccccccc-1111-2222-3333-444444444444",
		Type:        "config-patch",
		Status:      TaskStatusCompleted,
		SubmittedAt: time.Now(),
	}
	if err := s.Save(r); err != nil {
		t.Fatal(err)
	}

	deleted, err := s.Delete(r.ID)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !deleted {
		t.Fatal("expected delete to return true")
	}

	got, _ := s.Get(r.ID)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestStoreDeleteNotFound(t *testing.T) {
	s := newTestStore(t)

	deleted, err := s.Delete("nonexistent-id")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if deleted {
		t.Fatal("expected delete to return false for nonexistent ID")
	}
}

func TestStoreMigrateIdempotent(t *testing.T) {
	s := newTestStore(t)

	// Running migrate again on the same DB should be a no-op.
	if err := migrate(s.db); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestStorePing(t *testing.T) {
	s := newTestStore(t)
	if err := s.Ping(); err != nil {
		t.Fatalf("Ping on healthy store: %v", err)
	}

	s.Close()
	if err := s.Ping(); err == nil {
		t.Fatal("expected error from Ping on closed store")
	}
}

func TestStoreRunFieldRoundTrip(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Truncate(time.Nanosecond)

	r := &TaskResult{
		ID:          "run-rt-0000-0000-0000-000000000000",
		Type:        "config-patch",
		Status:      TaskStatusFailed,
		Run:         3,
		Error:       "transient",
		SubmittedAt: now,
	}
	if err := s.Save(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Run != 3 {
		t.Fatalf("Run = %d, want 3", got.Run)
	}
}

func TestStoreResultFieldRoundTrip(t *testing.T) {
	s := newTestStore(t)
	now := time.Now().Truncate(time.Nanosecond)

	r := &TaskResult{
		ID:          "res-rt00-0000-0000-0000-000000000000",
		Type:        "assemble-and-upload-genesis",
		Status:      TaskStatusCompleted,
		Run:         1,
		Result:      json.RawMessage(`{"genesisHash":"abc123"}`),
		SubmittedAt: now,
	}
	if err := s.Save(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Result) != `{"genesisHash":"abc123"}` {
		t.Fatalf("Result = %q, want round-tripped payload", string(got.Result))
	}
}

func TestStoreNilResultIsNil(t *testing.T) {
	// A handler that emits no result stores NULL and reads back as nil.
	s := newTestStore(t)

	r := &TaskResult{
		ID:          "res-nil0-0000-0000-0000-000000000000",
		Type:        "config-patch",
		Status:      TaskStatusCompleted,
		SubmittedAt: time.Now(),
	}
	if err := s.Save(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Result != nil {
		t.Fatalf("Result = %q, want nil", string(got.Result))
	}
}

func TestStoreNullableFields(t *testing.T) {
	s := newTestStore(t)

	r := &TaskResult{
		ID:          "dddddddd-1111-2222-3333-444444444444",
		Type:        "snapshot-restore",
		Status:      TaskStatusRunning,
		SubmittedAt: time.Now(),
		// CompletedAt nil; Params nil.
	}
	if err := s.Save(r); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := s.Get(r.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.CompletedAt != nil {
		t.Fatal("expected nil CompletedAt")
	}
}

// TestTxMarkerRoundTrip covers the pre-broadcast idempotency marker store:
// save+get is byte-identical, a missing key returns (nil, nil), and a second
// save for the same TaskID (INSERT OR REPLACE) overwrites cleanly.
func TestTxMarkerRoundTrip(t *testing.T) {
	s := newTestStore(t)

	m := &TxMarker{
		TaskID:        "task-round-trip",
		TxHash:        "ABCDEF0123456789",
		TxBytes:       []byte{0x00, 0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0xFF},
		AccountNumber: 17,
		Sequence:      42,
		ChainID:       "pacific-1",
	}
	if err := s.SaveTxMarker(m); err != nil {
		t.Fatalf("SaveTxMarker: %v", err)
	}

	got, err := s.GetTxMarker(m.TaskID)
	if err != nil {
		t.Fatalf("GetTxMarker: %v", err)
	}
	if got == nil {
		t.Fatal("expected marker, got nil")
	}
	if string(got.TxBytes) != string(m.TxBytes) {
		t.Fatalf("TxBytes not byte-identical: got %x want %x", got.TxBytes, m.TxBytes)
	}
	if got.TxHash != m.TxHash || got.AccountNumber != m.AccountNumber ||
		got.Sequence != m.Sequence || got.ChainID != m.ChainID {
		t.Fatalf("marker fields differ: got %+v want %+v", got, m)
	}

	// Missing key → (nil, nil).
	miss, err := s.GetTxMarker("missing")
	if err != nil {
		t.Fatalf("GetTxMarker(missing): %v", err)
	}
	if miss != nil {
		t.Fatalf("expected nil for missing marker, got %+v", miss)
	}

	// INSERT OR REPLACE: second save with same TaskID overwrites.
	m2 := &TxMarker{
		TaskID:        m.TaskID,
		TxHash:        "1111111111111111",
		TxBytes:       []byte{0x11, 0x22},
		AccountNumber: 100,
		Sequence:      101,
		ChainID:       "atlantic-2",
	}
	if err := s.SaveTxMarker(m2); err != nil {
		t.Fatalf("second SaveTxMarker: %v", err)
	}
	got2, err := s.GetTxMarker(m.TaskID)
	if err != nil {
		t.Fatalf("GetTxMarker after replace: %v", err)
	}
	if got2.TxHash != m2.TxHash || string(got2.TxBytes) != string(m2.TxBytes) ||
		got2.Sequence != m2.Sequence || got2.ChainID != m2.ChainID {
		t.Fatalf("replace did not overwrite cleanly: got %+v want %+v", got2, m2)
	}
}
