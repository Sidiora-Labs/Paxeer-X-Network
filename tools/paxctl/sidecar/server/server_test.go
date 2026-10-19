package server

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sei-protocol/seictl/sidecar/engine"
)

func newTestEngine(t *testing.T, handlers map[engine.TaskType]engine.TaskHandler) *engine.Engine {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store, err := engine.NewMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return engine.NewEngine(ctx, handlers, store)
}

func serveHTTP(srv *Server, method, path string, body string) *httptest.ResponseRecorder {
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	return rec
}

func waitForReady(eng *engine.Engine) {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if eng.Healthz() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForTaskResult(eng *engine.Engine, id string) *engine.TaskResult {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := eng.GetResult(id); r != nil && r.CompletedAt != nil {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	return nil
}

func TestLivezReturns200WhenStoreHealthy(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodGet, "/v0/livez", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestLivezReturns200BeforeReady(t *testing.T) {
	// Livez should pass even before mark-ready (healthz would return 503).
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	if eng.Healthz() {
		t.Fatal("expected healthz=false before mark-ready")
	}
	rec := serveHTTP(srv, http.MethodGet, "/v0/livez", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected livez=200 even before mark-ready, got %d", rec.Code)
	}
}

func TestLivezReturns503WhenStoreDown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store, err := engine.NewMemoryStore()
	if err != nil {
		t.Fatal(err)
	}
	eng := engine.NewEngine(ctx, nil, store)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	// Close the backing store to simulate SQLite failure.
	store.Close()

	rec := serveHTTP(srv, http.MethodGet, "/v0/livez", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when store is down, got %d", rec.Code)
	}
}

func TestHealthzReturns503BeforeReady(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodGet, "/v0/healthz", "")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestHealthzReturns200AfterMarkReady(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	_, _ = eng.Submit(engine.Task{Type: engine.TaskMarkReady})
	waitForReady(eng)

	rec := serveHTTP(srv, http.MethodGet, "/v0/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStatusResponse(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskMarkReady: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodGet, "/v0/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var resp engine.StatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Status != "Initializing" {
		t.Fatalf("expected Initializing initially, got %q", resp.Status)
	}

	_, _ = eng.Submit(engine.Task{Type: engine.TaskMarkReady})
	waitForReady(eng)

	rec = serveHTTP(srv, http.MethodGet, "/v0/status", "")
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp.Status != "Ready" {
		t.Fatalf("expected Ready after mark-ready, got %q", resp.Status)
	}
}

func TestPostTaskReturnsID(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	body := `{"type":"config-patch","params":{"peers":["a@1.2.3.4:26656"]}}`
	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["id"] == "" {
		t.Fatal("expected non-empty id")
	}
}

func TestPostTaskWithCallerID(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	body := `{"id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","type":"config-patch"}`
	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if resp["id"] != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("expected caller-provided ID, got %q", resp["id"])
	}
}

func TestPostTaskDedupReturnsExistingID(t *testing.T) {
	started := make(chan struct{})
	blocked := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store, _ := engine.NewMemoryStore()
	t.Cleanup(func() { store.Close() })
	eng := engine.NewEngine(ctx, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-blocked
			return nil, nil
		},
	}, store)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	body := `{"id":"ffffffff-1111-2222-3333-444444444444","type":"config-patch"}`
	rec1 := serveHTTP(srv, http.MethodPost, "/v0/tasks", body)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first submit: expected 201, got %d: %s", rec1.Code, rec1.Body.String())
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	rec2 := serveHTTP(srv, http.MethodPost, "/v0/tasks", body)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("second submit: expected 201, got %d", rec2.Code)
	}

	var resp1, resp2 map[string]string
	_ = json.NewDecoder(rec1.Body).Decode(&resp1)
	_ = json.NewDecoder(rec2.Body).Decode(&resp2)
	if resp1["id"] != resp2["id"] {
		t.Fatalf("dedup should return same ID: %q vs %q", resp1["id"], resp2["id"])
	}

	close(blocked)
}

func TestPostTaskInvalidIDReturns400(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	body := `{"id":"not-a-valid-uuid","type":"config-patch"}`
	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode error: %v", err)
	}
	if resp.Error == "" {
		t.Fatal("expected non-empty error message")
	}
}

func TestPostTaskInvalidJSON(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{not json}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPostTaskMissingType(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{"params":{}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPostTaskUnknownType(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{"type":"nonexistent"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListTasksEmpty(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodGet, "/v0/tasks", "")

	var results []engine.TaskResult
	if err := json.NewDecoder(rec.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected empty array, got %d", len(results))
	}
}

func TestListTasksAfterSubmit(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{"type":"config-patch"}`)
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	waitForTaskResult(eng, resp["id"])

	rec = serveHTTP(srv, http.MethodGet, "/v0/tasks", "")
	var results []engine.TaskResult
	if err := json.NewDecoder(rec.Body).Decode(&results); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestGetTask(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{"type":"config-patch"}`)
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	id := resp["id"]
	waitForTaskResult(eng, id)

	rec = serveHTTP(srv, http.MethodGet, "/v0/tasks/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var result engine.TaskResult
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result.ID != id {
		t.Fatalf("expected ID %q, got %q", id, result.ID)
	}
}

func TestGetTaskInProgress(t *testing.T) {
	started := make(chan struct{})
	blocked := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	store, _ := engine.NewMemoryStore()
	t.Cleanup(func() { store.Close() })
	eng := engine.NewEngine(ctx, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) {
			close(started)
			<-blocked
			return nil, nil
		},
	}, store)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{"type":"config-patch"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	id := resp["id"]

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for task to start")
	}

	rec = serveHTTP(srv, http.MethodGet, "/v0/tasks/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for in-progress task, got %d", rec.Code)
	}

	var result engine.TaskResult
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result.Status != engine.TaskStatusRunning {
		t.Fatalf("expected status %q, got %q", engine.TaskStatusRunning, result.Status)
	}
	if result.CompletedAt != nil {
		t.Fatal("expected CompletedAt to be nil for running task")
	}

	close(blocked)
	waitForTaskResult(eng, id)

	rec = serveHTTP(srv, http.MethodGet, "/v0/tasks/"+id, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for completed task, got %d", rec.Code)
	}
	_ = json.NewDecoder(rec.Body).Decode(&result)
	if result.Status != engine.TaskStatusCompleted {
		t.Fatalf("expected status %q after completion, got %q", engine.TaskStatusCompleted, result.Status)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)
	rec := serveHTTP(srv, http.MethodGet, "/v0/tasks/nonexistent", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteTask(t *testing.T) {
	eng := newTestEngine(t, map[engine.TaskType]engine.TaskHandler{
		engine.TaskConfigPatch: func(_ context.Context, _ map[string]any) (json.RawMessage, error) { return nil, nil },
	})
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodPost, "/v0/tasks", `{"type":"config-patch"}`)
	var resp map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	id := resp["id"]
	waitForTaskResult(eng, id)

	rec = serveHTTP(srv, http.MethodDelete, "/v0/tasks/"+id, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}

	rec = serveHTTP(srv, http.MethodDelete, "/v0/tasks/"+id, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

// writeTestNodeKey generates a real Ed25519 keypair, writes it as a CometBFT
// node_key.json, and returns the expected node ID (hex(SHA256(pubkey)[:20])).
func writeTestNodeKey(t *testing.T, homeDir string) string {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}

	// CometBFT stores the full 64-byte key (seed || pubkey) base64-encoded.
	keyFile := struct {
		PrivKey struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"priv_key"`
	}{
		PrivKey: struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		}{
			Type:  "tendermint/PrivKeyEd25519",
			Value: base64.StdEncoding.EncodeToString(priv),
		},
	}
	data, err := json.Marshal(keyFile)
	if err != nil {
		t.Fatal(err)
	}

	configDir := filepath.Join(homeDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "node_key.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	hash := sha256.Sum256(pub)
	return hex.EncodeToString(hash[:20])
}

func TestNodeID_ReturnsCorrectID(t *testing.T) {
	homeDir := t.TempDir()
	want := writeTestNodeKey(t, homeDir)

	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, homeDir, AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodGet, "/v0/node-id", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		NodeID string `json:"nodeId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result.NodeID != want {
		t.Errorf("nodeId = %q, want %q", result.NodeID, want)
	}
}

func TestNodeID_MissingKeyFile(t *testing.T) {
	eng := newTestEngine(t, nil)
	srv := NewServer(":0", eng, t.TempDir(), AuthnModeUnauthenticated)

	rec := serveHTTP(srv, http.MethodGet, "/v0/node-id", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
}
