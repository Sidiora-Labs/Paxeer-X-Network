package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testOwner       = "acme"
	testRepo        = "widgets"
	testGitHubToken = "github-test-token"
	testFlyToken    = "fly-test-token"
	testEncodedJIT  = "ZW5jb2RlZC1qaXQtY29uZmln"
)

var testNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

type ghJob struct {
	RunnerID    int64
	RunnerName  string
	ID          int64
	RunID       int64
	Status      string
	Conclusion  string
	Labels      []string
	CompletedAt time.Time
}

type githubServer struct {
	t   *testing.T
	srv *httptest.Server

	mu         sync.Mutex
	runs       map[string][]int64
	jobs       map[int64][]ghJob
	mintFail   map[string]int
	mints      []map[string]any
	requests   []string
	jobsCalled map[int64]int
}

func newGitHubServer(t *testing.T) *githubServer {
	t.Helper()
	s := &githubServer{
		t:          t,
		runs:       make(map[string][]int64),
		jobs:       make(map[int64][]ghJob),
		mintFail:   make(map[string]int),
		jobsCalled: make(map[int64]int),
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *githubServer) addRun(status string, runID int64, jobs ...ghJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != "" {
		s.runs[status] = append(s.runs[status], runID)
	}
	s.jobs[runID] = append(s.jobs[runID], jobs...)
}

func (s *githubServer) mintRequests() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.mints...)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *githubServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.RequestURI())
	if r.Header.Get("Authorization") != "Bearer "+testGitHubToken || r.Header.Get("X-GitHub-Api-Version") != githubAPIVersion {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "Bad credentials"})
		return
	}
	prefix := "/repos/" + testOwner + "/" + testRepo + "/actions/"
	path := strings.TrimPrefix(r.URL.Path, prefix)
	switch {
	case r.Method == http.MethodGet && path == "runs":
		status := r.URL.Query().Get("status")
		if r.URL.Query().Get("per_page") != "100" {
			s.t.Errorf("runs request without per_page=100: %s", r.URL.RawQuery)
		}
		runs := make([]map[string]any, 0)
		for _, id := range s.runs[status] {
			runs = append(runs, map[string]any{"id": id, "name": "ci", "status": status, "conclusion": nil, "head_branch": "main", "run_attempt": 1})
		}
		writeJSON(w, http.StatusOK, map[string]any{"total_count": len(runs), "workflow_runs": runs})
	case r.Method == http.MethodGet && strings.HasPrefix(path, "runs/") && strings.HasSuffix(path, "/jobs"):
		runID, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(path, "runs/"), "/jobs"), 10, 64)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
			return
		}
		s.jobsCalled[runID]++
		jobs := make([]map[string]any, 0)
		for _, job := range s.jobs[runID] {
			entry := map[string]any{
				"id":           job.ID,
				"run_id":       job.RunID,
				"status":       job.Status,
				"conclusion":   nil,
				"labels":       job.Labels,
				"completed_at": nil,
				"name":         fmt.Sprintf("job-%d", job.ID),
				"runner_name":  nil,
				"runner_id":    nil,
			}
			if job.RunnerID > 0 {
				entry["runner_id"] = job.RunnerID
			}
			if job.RunnerName != "" {
				entry["runner_name"] = job.RunnerName
			}
			if job.Conclusion != "" {
				entry["conclusion"] = job.Conclusion
			}
			if !job.CompletedAt.IsZero() {
				entry["completed_at"] = job.CompletedAt.Format(time.RFC3339)
			}
			jobs = append(jobs, entry)
		}
		writeJSON(w, http.StatusOK, map[string]any{"total_count": len(jobs), "jobs": jobs})
	case r.Method == http.MethodPost && path == "runners/generate-jitconfig":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"message": "Invalid request"})
			return
		}
		s.mints = append(s.mints, body)
		name, _ := body["name"].(string)
		if status, ok := s.mintFail[name]; ok {
			writeJSON(w, status, map[string]any{"message": "Server Error"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"runner": map[string]any{
				"id": 7000 + len(s.mints), "name": name, "os": "unknown", "status": "offline", "busy": false,
				"labels": []map[string]any{{"id": 1, "name": "self-hosted", "type": "read-only"}},
			},
			"encoded_jit_config": testEncodedJIT,
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
	}
}

func testConfig(githubURL, flyURL string) config {
	return config{
		Owner:        testOwner,
		Repo:         testRepo,
		GitHubToken:  testGitHubToken,
		FlyToken:     testFlyToken,
		RunnerApp:    "paxeer-ci-runners",
		RunnerImage:  "registry.fly.io/paxeer-ci-runners:test",
		Labels:       []string{"fly-linux"},
		Region:       "iad",
		CPUKind:      "performance",
		CPUs:         4,
		MemoryMB:     16384,
		MaxMachines:  24,
		PollInterval: 20 * time.Second,
		IdleTimeout:  900,
		OrphanTTL:    3 * time.Hour,
		GitHubAPIURL: githubURL,
		FlyAPIURL:    flyURL,
	}
}

type recordedSleeps struct {
	mu     sync.Mutex
	sleeps []time.Duration
}

func (r *recordedSleeps) sleep(_ context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sleeps = append(r.sleeps, d)
	return nil
}

func newTestGitHubClient(t *testing.T, baseURL string, sleeps *recordedSleeps) *githubClient {
	t.Helper()
	client, err := newGitHubClient(testConfig(baseURL, "http://127.0.0.1:1"), &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return testNow }
	client.sleep = sleeps.sleep
	return client
}

func TestGitHubConditionalRequestReturnsCachedPagesOn304(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		page := r.URL.Query().Get("page")
		seen = append(seen, "page="+page+" if-none-match="+r.Header.Get("If-None-Match"))
		etag := `W/"runs-page-1"`
		ids := []int64{11, 12}
		if page == "2" {
			etag = `W/"runs-page-2"`
			ids = []int64{13}
		}
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		if page == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/widgets/actions/runs?status=queued&per_page=100&page=2>; rel="next", <%s/repos/acme/widgets/actions/runs?status=queued&per_page=100&page=2>; rel="last"`, srv.URL, srv.URL))
		}
		runs := make([]map[string]any, 0)
		for _, id := range ids {
			runs = append(runs, map[string]any{"id": id, "status": "queued"})
		}
		writeJSON(w, http.StatusOK, map[string]any{"total_count": 3, "workflow_runs": runs})
	}))
	defer srv.Close()

	client := newTestGitHubClient(t, srv.URL, &recordedSleeps{})
	for poll := 0; poll < 2; poll++ {
		runs, err := client.listRuns(context.Background(), "queued")
		if err != nil {
			t.Fatalf("poll %d: %v", poll, err)
		}
		if len(runs) != 3 || runs[0].ID != 11 || runs[1].ID != 12 || runs[2].ID != 13 {
			t.Fatalf("poll %d: runs = %+v", poll, runs)
		}
		client.endPoll()
	}
	want := []string{
		"page= if-none-match=",
		"page=2 if-none-match=",
		`page= if-none-match=W/"runs-page-1"`,
		`page=2 if-none-match=W/"runs-page-2"`,
	}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(seen, "\n"), strings.Join(want, "\n"))
	}
}

func TestGitHubRateLimitedResponseSleepsUntilReset(t *testing.T) {
	reset := testNow.Add(90 * time.Second)
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		switch calls {
		case 1:
			w.Header().Set("X-RateLimit-Remaining", "0")
			writeJSON(w, http.StatusForbidden, map[string]any{"message": "API rate limit exceeded"})
		case 2:
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"message": "rate limited"})
		default:
			w.Header().Set("X-RateLimit-Remaining", "4999")
			writeJSON(w, http.StatusOK, map[string]any{"total_count": 1, "jobs": []map[string]any{{"id": 5, "run_id": 9, "status": "queued", "labels": []string{"fly-linux"}, "completed_at": nil}}})
		}
	}))
	defer srv.Close()

	sleeps := &recordedSleeps{}
	client := newTestGitHubClient(t, srv.URL, sleeps)
	jobs, err := client.listJobs(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].ID != 5 || jobs[0].RunID != 9 {
		t.Fatalf("jobs = %+v", jobs)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if len(sleeps.sleeps) != 2 || sleeps.sleeps[0] != 90*time.Second || sleeps.sleeps[1] != 90*time.Second {
		t.Fatalf("sleeps = %v, want two sleeps of 90s", sleeps.sleeps)
	}
}

func TestGitHubExhaustedRemainingDelaysNextRequest(t *testing.T) {
	reset := testNow.Add(45 * time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		writeJSON(w, http.StatusOK, map[string]any{"total_count": 0, "workflow_runs": []any{}})
	}))
	defer srv.Close()

	sleeps := &recordedSleeps{}
	client := newTestGitHubClient(t, srv.URL, sleeps)
	if _, err := client.listRuns(context.Background(), "queued"); err != nil {
		t.Fatal(err)
	}
	if len(sleeps.sleeps) != 0 {
		t.Fatalf("slept before the limit was known: %v", sleeps.sleeps)
	}
	if _, err := client.listRuns(context.Background(), "in_progress"); err != nil {
		t.Fatal(err)
	}
	if len(sleeps.sleeps) != 1 || sleeps.sleeps[0] != 45*time.Second {
		t.Fatalf("sleeps = %v, want one sleep of 45s", sleeps.sleeps)
	}
}

func TestGitHubForbiddenWithoutRateLimitIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4000")
		writeJSON(w, http.StatusForbidden, map[string]any{"message": "Resource not accessible by integration"})
	}))
	defer srv.Close()

	sleeps := &recordedSleeps{}
	client := newTestGitHubClient(t, srv.URL, sleeps)
	_, err := client.generateJITConfig(context.Background(), "fly-1", []string{"fly-linux"})
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("err = %v, want a 403 error", err)
	}
	if len(sleeps.sleeps) != 0 {
		t.Fatalf("sleeps = %v, want none", sleeps.sleeps)
	}
}

func TestGitHubGenerateJITConfigSendsDocumentedRequest(t *testing.T) {
	gh := newGitHubServer(t)
	client := newTestGitHubClient(t, gh.srv.URL, &recordedSleeps{})
	cfg, err := client.generateJITConfig(context.Background(), "fly-42", []string{"fly-linux"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EncodedJITConfig != testEncodedJIT || cfg.Runner.Name != "fly-42" {
		t.Fatalf("config = %+v", cfg)
	}
	mints := gh.mintRequests()
	if len(mints) != 1 {
		t.Fatalf("mints = %v", mints)
	}
	body := mints[0]
	if body["name"] != "fly-42" || body["runner_group_id"] != float64(1) || body["work_folder"] != "_work" {
		t.Fatalf("mint body = %v", body)
	}
	if labels, _ := body["labels"].([]any); len(labels) != 1 || labels[0] != "fly-linux" {
		t.Fatalf("mint labels = %v", body["labels"])
	}
}
