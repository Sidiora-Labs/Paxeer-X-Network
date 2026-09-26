package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type flyMachine struct {
	ID        string
	Name      string
	State     string
	CreatedAt time.Time
	Metadata  map[string]string
}

type flyServer struct {
	t   *testing.T
	srv *httptest.Server
	app string

	mu         sync.Mutex
	machines   []flyMachine
	createFail map[string]int
	creates    []map[string]any
	deletes    []string
	nextID     int
}

func newFlyServer(t *testing.T) *flyServer {
	t.Helper()
	s := &flyServer{t: t, app: "paxeer-ci-runners", createFail: make(map[string]int)}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *flyServer) addMachine(m flyMachine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.machines = append(s.machines, m)
}

func (s *flyServer) createRequests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.creates...)
}

func (s *flyServer) deleted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deletes...)
}

func (s *flyServer) machineJSON(m flyMachine) map[string]any {
	config := map[string]any{
		"image":        "registry.fly.io/paxeer-ci-runners:test",
		"guest":        map[string]any{"cpu_kind": "performance", "cpus": 4, "memory_mb": 16384},
		"auto_destroy": true,
		"restart":      map[string]any{"policy": "no"},
	}
	if m.Metadata != nil {
		config["metadata"] = m.Metadata
	}
	return map[string]any{
		"id":          m.ID,
		"name":        m.Name,
		"state":       m.State,
		"region":      "iad",
		"instance_id": "01H" + m.ID,
		"created_at":  m.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":  m.CreatedAt.UTC().Format(time.RFC3339),
		"config":      config,
	}
}

func (s *flyServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+testFlyToken {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "unauthorized"})
		return
	}
	prefix := "/v1/apps/" + s.app + "/machines"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == prefix:
		out := make([]map[string]any, 0, len(s.machines))
		for _, m := range s.machines {
			out = append(out, s.machineJSON(m))
		}
		writeJSON(w, http.StatusOK, out)
	case r.Method == http.MethodPost && r.URL.Path == prefix:
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid body"})
			return
		}
		s.creates = append(s.creates, body)
		name, _ := body["name"].(string)
		if status, ok := s.createFail[name]; ok {
			writeJSON(w, status, map[string]any{"error": "could not reserve resource for machine"})
			return
		}
		s.nextID++
		m := flyMachine{ID: fmt.Sprintf("m%04d", s.nextID), Name: name, State: "created", CreatedAt: testNow, Metadata: map[string]string{}}
		if cfg, ok := body["config"].(map[string]any); ok {
			if meta, ok := cfg["metadata"].(map[string]any); ok {
				for k, v := range meta {
					m.Metadata[k], _ = v.(string)
				}
			}
		}
		s.machines = append(s.machines, m)
		writeJSON(w, http.StatusOK, s.machineJSON(m))
	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, prefix+"/"):
		id := strings.TrimPrefix(r.URL.Path, prefix+"/")
		if r.URL.Query().Get("force") != "true" {
			writeJSON(w, http.StatusPreconditionFailed, map[string]any{"error": "machine is not stopped"})
			return
		}
		for i, m := range s.machines {
			if m.ID == id {
				s.machines = append(s.machines[:i], s.machines[i+1:]...)
				s.deletes = append(s.deletes, id)
				writeJSON(w, http.StatusOK, map[string]any{"ok": true})
				return
			}
		}
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "machine not found"})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
	}
}

func newTestFlyClient(t *testing.T, baseURL string) *flyClient {
	t.Helper()
	client, err := newFlyClient(testConfig("http://127.0.0.1:1", baseURL), &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFlyClientListCreateDestroy(t *testing.T) {
	fly := newFlyServer(t)
	fly.addMachine(flyMachine{ID: "m-old", Name: "fly-1", State: "started", CreatedAt: testNow.Add(-time.Minute), Metadata: map[string]string{metadataJobID: "1", metadataRunID: "10"}})
	client := newTestFlyClient(t, fly.srv.URL)
	ctx := context.Background()

	machines, err := client.listMachines(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(machines) != 1 || machines[0].ID != "m-old" || machines[0].State != "started" || machines[0].Config.Metadata[metadataJobID] != "1" || !machines[0].CreatedAt.Equal(testNow.Add(-time.Minute)) {
		t.Fatalf("machines = %+v", machines)
	}

	created, err := client.createMachine(ctx, createMachineRequest{
		Name:   "fly-2",
		Region: "iad",
		Config: machineConfig{
			Image:       "registry.fly.io/paxeer-ci-runners:test",
			Env:         map[string]string{"RUNNER_JITCONFIG": testEncodedJIT},
			Guest:       machineGuest{CPUKind: "performance", CPUs: 4, MemoryMB: 16384},
			AutoDestroy: true,
			Restart:     machineRestart{Policy: "no"},
			Metadata:    map[string]string{metadataJobID: "2", metadataRunID: "10"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Name != "fly-2" || created.Config.Metadata[metadataJobID] != "2" {
		t.Fatalf("created = %+v", created)
	}

	if err := client.destroyMachine(ctx, "m-old"); err != nil {
		t.Fatal(err)
	}
	if err := client.destroyMachine(ctx, "m-old"); err != nil {
		t.Fatalf("destroying an already removed machine: %v", err)
	}
	if got := fly.deleted(); len(got) != 1 || got[0] != "m-old" {
		t.Fatalf("deleted = %v", got)
	}
}

func TestFlyClientCreateFailureIsAnError(t *testing.T) {
	fly := newFlyServer(t)
	fly.createFail["fly-3"] = http.StatusUnprocessableEntity
	client := newTestFlyClient(t, fly.srv.URL)
	_, err := client.createMachine(context.Background(), createMachineRequest{Name: "fly-3", Region: "iad"})
	if err == nil || !strings.Contains(err.Error(), "status 422") {
		t.Fatalf("err = %v, want a 422 error", err)
	}
}
