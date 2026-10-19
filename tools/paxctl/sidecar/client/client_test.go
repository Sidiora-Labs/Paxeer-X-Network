package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func newTestClient(t *testing.T, handler http.Handler) *SidecarClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewSidecarClient(srv.URL)
	if err != nil {
		t.Fatalf("NewSidecarClient: %v", err)
	}
	return c
}

func TestStatus_OK(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/status" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(StatusResponse{Status: Ready})
	}))

	resp, err := c.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if resp.Status != Ready {
		t.Errorf("Status = %q, want %q", resp.Status, Ready)
	}
}

func TestStatus_ServerError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))

	_, err := c.Status(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestSubmitTask_HTTP201(t *testing.T) {
	taskID := uuid.New()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/tasks" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var req TaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Type != TaskTypeSnapshotRestore {
			t.Errorf("Type = %q, want %q", req.Type, TaskTypeSnapshotRestore)
		}
		if req.Params == nil {
			t.Fatal("Params is nil")
		}
		params := *req.Params
		if params["targetHeight"] != float64(100000000) {
			t.Errorf("targetHeight = %v, want 100000000", params["targetHeight"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(TaskSubmitResponse{Id: taskID})
	}))

	task := SnapshotRestoreTask{
		TargetHeight: 100000000,
	}
	id, err := c.SubmitSnapshotRestoreTask(context.Background(), task)
	if err != nil {
		t.Fatalf("SubmitSnapshotRestoreTask() error = %v", err)
	}
	if id != taskID {
		t.Errorf("returned id = %s, want %s", id, taskID)
	}
}

func TestSubmitTask_HTTP202(t *testing.T) {
	taskID := uuid.New()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(TaskSubmitResponse{Id: taskID})
	}))

	id, err := c.SubmitTask(context.Background(), TaskRequest{Type: TaskTypeMarkReady})
	if err != nil {
		t.Fatalf("SubmitTask() error = %v", err)
	}
	if id != taskID {
		t.Errorf("returned id = %s, want %s", id, taskID)
	}
}

func TestSubmitTask_HTTP202_MalformedBody(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`not json`))
	}))

	_, err := c.SubmitTask(context.Background(), TaskRequest{Type: TaskTypeMarkReady})
	if err == nil {
		t.Fatal("expected error for malformed 202 body")
	}
}

func TestSubmitTask_HTTP202_NilUUID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(TaskSubmitResponse{Id: uuid.Nil})
	}))

	_, err := c.SubmitTask(context.Background(), TaskRequest{Type: TaskTypeMarkReady})
	if err == nil {
		t.Fatal("expected error for nil UUID in 202 response")
	}
	if !strings.Contains(err.Error(), "nil task ID") {
		t.Errorf("error = %v, expected to contain 'nil task ID'", err)
	}
}

func TestSubmitTask_HTTP201_NilUUID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(TaskSubmitResponse{Id: uuid.Nil})
	}))

	_, err := c.SubmitTask(context.Background(), TaskRequest{Type: TaskTypeMarkReady})
	if err == nil {
		t.Fatal("expected error for nil UUID in 201 response")
	}
	if !strings.Contains(err.Error(), "nil task ID") {
		t.Errorf("error = %v, expected to contain 'nil task ID'", err)
	}
}

func TestSubmitTask_ServerError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))

	_, err := c.SubmitTask(context.Background(), TaskRequest{Type: TaskTypeMarkReady})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, expected to contain '500'", err)
	}
}

func TestSubmitTask_Conflict(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))

	_, err := c.SubmitTask(context.Background(), TaskRequest{Type: TaskTypeMarkReady})
	if err == nil {
		t.Fatal("expected error for 409 response")
	}
	if !strings.Contains(err.Error(), "409") {
		t.Errorf("error = %v, expected to contain '409'", err)
	}
}

func TestSubmitTask_BadRequest(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "unknown task type"})
	}))

	_, err := c.SubmitTask(context.Background(), TaskRequest{Type: "invalid"})
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if !strings.Contains(err.Error(), "unknown task type") {
		t.Errorf("error = %v, expected to contain 'unknown task type'", err)
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error = %v, expected to contain the task type 'invalid'", err)
	}
}

func TestSubmitTask_ValidationFailure(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called when validation fails")
	}))

	// ConfigPatchTask requires at least one file — empty should fail validation.
	_, err := c.SubmitConfigPatchTask(context.Background(), ConfigPatchTask{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error = %v, expected to contain 'validation failed'", err)
	}
}

func TestListTasks_OK(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/tasks" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]TaskResult{
			{Id: uuid.New(), Type: "mark-ready"},
		})
	}))

	results, err := c.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Type != "mark-ready" {
		t.Errorf("Type = %q, want mark-ready", results[0].Type)
	}
}

func TestGetTask_OK(t *testing.T) {
	taskID := uuid.New()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(TaskResult{Id: taskID, Type: "config-patch"})
	}))

	result, err := c.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("GetTask() error = %v", err)
	}
	if result.Type != "config-patch" {
		t.Errorf("Type = %q, want config-patch", result.Type)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "not found"})
	}))

	_, err := c.GetTask(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestDeleteTask_OK(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method: %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := c.DeleteTask(context.Background(), uuid.New()); err != nil {
		t.Fatalf("DeleteTask() error = %v", err)
	}
}

func TestDeleteTask_NotFound(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Error: "not found"})
	}))

	err := c.DeleteTask(context.Background(), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestHealthz_OK(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/healthz" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	healthy, err := c.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz() error = %v", err)
	}
	if !healthy {
		t.Error("Healthz() = false, want true")
	}
}

func TestHealthz_ServiceUnavailable(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	healthy, err := c.Healthz(context.Background())
	if err != nil {
		t.Fatalf("Healthz() error = %v", err)
	}
	if healthy {
		t.Error("Healthz() = true, want false")
	}
}

func TestHealthz_UnexpectedStatus(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))

	_, err := c.Healthz(context.Background())
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
}

func TestNewSidecarClientFromPodDNS_URLFormat(t *testing.T) {
	c, err := NewSidecarClientFromPodDNS("sei-node", "default", 7777)
	if err != nil {
		t.Fatalf("NewSidecarClientFromPodDNS: %v", err)
	}
	inner := c.inner.ClientInterface.(*Client)
	want := "http://sei-node-0.sei-node.default.svc.cluster.local:7777/"
	if inner.Server != want {
		t.Errorf("Server = %q, want %q", inner.Server, want)
	}
}

func TestNewSidecarClientFromPodDNS_DefaultPort(t *testing.T) {
	c, err := NewSidecarClientFromPodDNS("mynode", "prod", 0)
	if err != nil {
		t.Fatalf("NewSidecarClientFromPodDNS: %v", err)
	}
	inner := c.inner.ClientInterface.(*Client)
	want := "http://mynode-0.mynode.prod.svc.cluster.local:7777/"
	if inner.Server != want {
		t.Errorf("Server = %q, want %q", inner.Server, want)
	}
}

func TestSubmitAwaitConditionTask(t *testing.T) {
	taskID := uuid.New()
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req TaskRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if req.Type != TaskTypeAwaitCondition {
			t.Errorf("Type = %q, want %q", req.Type, TaskTypeAwaitCondition)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(TaskSubmitResponse{Id: taskID})
	}))

	id, err := c.SubmitAwaitConditionTask(context.Background(), AwaitConditionTask{
		Condition:    ConditionHeight,
		TargetHeight: 1000,
		Action:       ActionSIGTERM,
	})
	if err != nil {
		t.Fatalf("SubmitAwaitConditionTask() error = %v", err)
	}
	if id != taskID {
		t.Errorf("returned id = %s, want %s", id, taskID)
	}
}

func TestSubmitAwaitConditionTask_ValidationError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("server should not be called when validation fails")
	}))

	_, err := c.SubmitAwaitConditionTask(context.Background(), AwaitConditionTask{
		Condition:    ConditionHeight,
		TargetHeight: -1,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "validation failed") {
		t.Errorf("error = %v, expected to contain 'validation failed'", err)
	}
}

func TestListTasks_EmptyArray(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))

	results, err := c.ListTasks(context.Background())
	if err != nil {
		t.Fatalf("ListTasks() error = %v", err)
	}
	if results == nil {
		t.Fatal("ListTasks() returned nil, want empty slice")
	}
	if len(results) != 0 {
		t.Errorf("got %d results, want 0", len(results))
	}
}

func TestConfigPatchTask_Validate_Empty(t *testing.T) {
	task := ConfigPatchTask{}
	if err := task.Validate(); err == nil {
		t.Fatal("expected validation error for empty ConfigPatchTask")
	}
}

func TestConfigPatchTask_Validate_OK(t *testing.T) {
	task := ConfigPatchTask{
		Files: map[string]map[string]any{
			"config.toml": {"p2p": map[string]any{"seeds": "foo"}},
		},
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestGetNodeID_OK(t *testing.T) {
	want := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/node-id" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"nodeId": want})
	}))

	got, err := c.GetNodeID(context.Background())
	if err != nil {
		t.Fatalf("GetNodeID() error = %v", err)
	}
	if got != want {
		t.Errorf("GetNodeID() = %q, want %q", got, want)
	}
}

func TestGetNodeID_ServerError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not ready", http.StatusInternalServerError)
	}))

	_, err := c.GetNodeID(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, expected to contain '500'", err)
	}
}

func TestGetNodeID_EmptyNodeID(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"nodeId": ""})
	}))

	_, err := c.GetNodeID(context.Background())
	if err == nil {
		t.Fatal("expected error for empty nodeId")
	}
	if !strings.Contains(err.Error(), "missing nodeId") {
		t.Errorf("error = %v, expected to contain 'missing nodeId'", err)
	}
}
