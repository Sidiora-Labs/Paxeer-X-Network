package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testRig struct {
	gh   *githubServer
	fly  *flyServer
	rec  *reconciler
	logs *bytes.Buffer
}

func newTestRig(t *testing.T, adjust func(*config)) *testRig {
	t.Helper()
	gh := newGitHubServer(t)
	fly := newFlyServer(t)
	cfg := testConfig(gh.srv.URL, fly.srv.URL)
	if adjust != nil {
		adjust(&cfg)
	}
	httpClient := &http.Client{Timeout: 5 * time.Second}
	github, err := newGitHubClient(cfg, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	github.now = func() time.Time { return testNow }
	github.sleep = (&recordedSleeps{}).sleep
	flyAPI, err := newFlyClient(cfg, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	rec := newReconciler(cfg, github, flyAPI, slog.New(slog.NewTextHandler(logs, nil)))
	rec.now = func() time.Time { return testNow }
	return &testRig{gh: gh, fly: fly, rec: rec, logs: logs}
}

func (r *testRig) poll(t *testing.T) pollSummary {
	t.Helper()
	summary, err := r.rec.reconcile(context.Background())
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	return summary
}

func createdNames(creates []map[string]any) []string {
	names := make([]string, 0, len(creates))
	for _, c := range creates {
		name, _ := c["name"].(string)
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func mintedNames(mints []map[string]any) []string {
	return createdNames(mints)
}

func queued(id, run int64, labels ...string) ghJob {
	return ghJob{ID: id, RunID: run, Status: "queued", Labels: labels}
}

func managed(id, state string, jobID, runID string, created time.Time) flyMachine {
	return flyMachine{ID: id, Name: "fly-" + jobID, State: state, CreatedAt: created, Metadata: map[string]string{metadataJobID: jobID, metadataRunID: runID}}
}

func TestReconcileSelectsOnlyQueuedJobsWithAcceptedLabels(t *testing.T) {
	rig := newTestRig(t, func(c *config) { c.Labels = []string{"fly-linux", "Fly-Big"} })
	rig.gh.addRun("queued", 100,
		queued(1, 100, "self-hosted", "Linux", "X64", "FLY-LINUX"),
		queued(2, 100, "fly-linux"),
		queued(3, 100, "ubuntu-24.04"),
		queued(4, 100, "self-hosted", "layerx-testnet"),
		queued(5, 100, "fly-linux", "gpu"),
		queued(6, 100),
		queued(7, 100, "fly-big"),
	)
	rig.gh.addRun("in_progress", 200,
		ghJob{ID: 8, RunID: 200, Status: "in_progress", Labels: []string{"fly-linux"}},
		ghJob{ID: 9, RunID: 200, Status: "completed", Conclusion: "success", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-time.Minute)},
		queued(10, 200, "self-hosted", "linux", "x64"),
	)

	summary := rig.poll(t)
	want := []string{"fly-1", "fly-10", "fly-2", "fly-7"}
	if got := createdNames(rig.fly.createRequests()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("created = %v, want %v", got, want)
	}
	if got := mintedNames(rig.gh.mintRequests()); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("minted = %v, want %v", got, want)
	}
	if summary.Selected != 4 || summary.Created != 4 || summary.Live != 4 {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestReconcileCreatesMachineWithJITConfigAsOnlyCredential(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("queued", 300, queued(31, 300, "fly-linux"))

	rig.poll(t)
	mints := rig.gh.mintRequests()
	if len(mints) != 1 {
		t.Fatalf("mints = %v", mints)
	}
	if mints[0]["name"] != "fly-31" || mints[0]["runner_group_id"] != float64(1) || mints[0]["work_folder"] != "_work" {
		t.Fatalf("mint = %v", mints[0])
	}
	creates := rig.fly.createRequests()
	if len(creates) != 1 {
		t.Fatalf("creates = %v", creates)
	}
	body := creates[0]
	if body["name"] != "fly-31" || body["region"] != "iad" {
		t.Fatalf("create = %v", body)
	}
	cfg, _ := body["config"].(map[string]any)
	if cfg["image"] != "registry.fly.io/paxeer-ci-runners:test" || cfg["auto_destroy"] != true {
		t.Fatalf("config = %v", cfg)
	}
	if restart, _ := cfg["restart"].(map[string]any); restart["policy"] != "no" {
		t.Fatalf("restart = %v", cfg["restart"])
	}
	guest, _ := cfg["guest"].(map[string]any)
	if guest["cpu_kind"] != "performance" || guest["cpus"] != float64(4) || guest["memory_mb"] != float64(16384) {
		t.Fatalf("guest = %v", guest)
	}
	env, _ := cfg["env"].(map[string]any)
	if len(env) != 2 || env["RUNNER_JITCONFIG"] != testEncodedJIT || env["RUNNER_IDLE_TIMEOUT"] != "900" {
		t.Fatalf("env = %v", env)
	}
	meta, _ := cfg["metadata"].(map[string]any)
	if len(meta) != 2 || meta[metadataJobID] != "31" || meta[metadataRunID] != "300" {
		t.Fatalf("metadata = %v", meta)
	}
	raw := rig.logs.String()
	if strings.Contains(raw, testEncodedJIT) || strings.Contains(raw, testGitHubToken) || strings.Contains(raw, testFlyToken) {
		t.Fatalf("a credential reached the log:\n%s", raw)
	}
}

func TestReconcileCreatesOneMachinePerJob(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("queued", 400, queued(41, 400, "fly-linux"), queued(42, 400, "fly-linux"))
	rig.gh.addRun("in_progress", 400)
	rig.fly.addMachine(managed("m-41", "started", "41", "400", testNow.Add(-time.Minute)))

	rig.poll(t)
	rig.poll(t)
	if got := createdNames(rig.fly.createRequests()); strings.Join(got, ",") != "fly-42" {
		t.Fatalf("created = %v, want only fly-42", got)
	}
	if got := mintedNames(rig.gh.mintRequests()); strings.Join(got, ",") != "fly-42" {
		t.Fatalf("minted = %v, want only fly-42", got)
	}
	if got := rig.fly.deleted(); len(got) != 0 {
		t.Fatalf("deleted = %v", got)
	}
}

func TestReconcileNeverExceedsMachineCeiling(t *testing.T) {
	rig := newTestRig(t, func(c *config) { c.MaxMachines = 2 })
	rig.gh.addRun("queued", 500, queued(51, 500, "fly-linux"), queued(52, 500, "fly-linux"), queued(53, 500, "fly-linux"))
	rig.gh.addRun("in_progress", 501, ghJob{ID: 50, RunID: 501, Status: "in_progress", Labels: []string{"fly-linux"}})
	rig.fly.addMachine(managed("m-50", "started", "50", "501", testNow.Add(-time.Minute)))

	summary := rig.poll(t)
	if got := createdNames(rig.fly.createRequests()); strings.Join(got, ",") != "fly-51" {
		t.Fatalf("created = %v, want only fly-51", got)
	}
	if summary.Live != 2 || summary.Created != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if len(rig.gh.mintRequests()) != 1 {
		t.Fatalf("minted beyond the ceiling: %v", rig.gh.mintRequests())
	}
	if !strings.Contains(rig.logs.String(), "machine ceiling reached") {
		t.Fatalf("no ceiling log:\n%s", rig.logs.String())
	}
}

func TestReconcileFailedMintIsLoggedAndSkipped(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("queued", 600, queued(61, 600, "fly-linux"), queued(62, 600, "fly-linux"))
	rig.gh.mintFail["fly-61"] = http.StatusInternalServerError

	summary := rig.poll(t)
	if got := createdNames(rig.fly.createRequests()); strings.Join(got, ",") != "fly-62" {
		t.Fatalf("created = %v, want only fly-62", got)
	}
	if summary.Created != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	logs := rig.logs.String()
	if !strings.Contains(logs, "mint runner configuration failed") || !strings.Contains(logs, "job_id=61") {
		t.Fatalf("failed mint not logged with job id:\n%s", logs)
	}

	delete(rig.gh.mintFail, "fly-61")
	rig.poll(t)
	if got := createdNames(rig.fly.createRequests()); strings.Join(got, ",") != "fly-61,fly-62" {
		t.Fatalf("created after retry = %v", got)
	}
}

func TestReconcileFailedCreateIsLoggedAndSkipped(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("queued", 700, queued(71, 700, "fly-linux"), queued(72, 700, "fly-linux"))
	rig.fly.createFail["fly-71"] = http.StatusUnprocessableEntity

	summary := rig.poll(t)
	if summary.Created != 1 || summary.Live != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	logs := rig.logs.String()
	if !strings.Contains(logs, "create machine failed") || !strings.Contains(logs, "job_id=71") {
		t.Fatalf("failed create not logged with job id:\n%s", logs)
	}

	delete(rig.fly.createFail, "fly-71")
	summary = rig.poll(t)
	if summary.Created != 1 || summary.Live != 2 {
		t.Fatalf("summary after retry = %+v", summary)
	}
}

func TestReconcileDestroysMachineOfFinishedJob(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("", 800,
		ghJob{RunnerID: 7081, RunnerName: "fly-81", ID: 81, RunID: 800, Status: "completed", Conclusion: "success", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-6 * time.Minute)},
		ghJob{RunnerID: 7082, RunnerName: "fly-82", ID: 82, RunID: 800, Status: "completed", Conclusion: "failure", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-2 * time.Minute)},
	)
	rig.gh.addRun("in_progress", 801,
		ghJob{RunnerID: 7083, RunnerName: "fly-83", ID: 83, RunID: 801, Status: "completed", Conclusion: "success", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-10 * time.Minute)},
		ghJob{RunnerID: 7084, RunnerName: "fly-84", ID: 84, RunID: 801, Status: "in_progress", Labels: []string{"fly-linux"}},
	)
	rig.fly.addMachine(managed("m-81", "started", "81", "800", testNow.Add(-20*time.Minute)))
	rig.fly.addMachine(managed("m-82", "started", "82", "800", testNow.Add(-20*time.Minute)))
	rig.fly.addMachine(managed("m-83", "started", "83", "801", testNow.Add(-20*time.Minute)))
	rig.fly.addMachine(managed("m-84", "started", "84", "801", testNow.Add(-20*time.Minute)))

	summary := rig.poll(t)
	got := rig.fly.deleted()
	sort.Strings(got)
	if strings.Join(got, ",") != "m-81,m-83" {
		t.Fatalf("deleted = %v, want m-81,m-83", got)
	}
	if summary.Destroyed != 2 || summary.Live != 2 {
		t.Fatalf("summary = %+v", summary)
	}
	if !strings.Contains(rig.logs.String(), "reason=finished") {
		t.Fatalf("no finished log:\n%s", rig.logs.String())
	}
}

func TestReconcileDestroysOrphanedMachine(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("in_progress", 900,
		ghJob{ID: 91, RunID: 900, Status: "in_progress", Labels: []string{"fly-linux"}},
		ghJob{ID: 92, RunID: 900, Status: "in_progress", Labels: []string{"fly-linux"}},
	)
	rig.fly.addMachine(managed("m-91", "started", "91", "900", testNow.Add(-3*time.Hour-time.Minute)))
	rig.fly.addMachine(managed("m-92", "started", "92", "900", testNow.Add(-2*time.Hour)))

	summary := rig.poll(t)
	if got := rig.fly.deleted(); strings.Join(got, ",") != "m-91" {
		t.Fatalf("deleted = %v, want m-91", got)
	}
	if summary.Destroyed != 1 || summary.Live != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if !strings.Contains(rig.logs.String(), "reason=orphaned") {
		t.Fatalf("no orphan log:\n%s", rig.logs.String())
	}
}

func TestReconcileDestroysStoppedMachine(t *testing.T) {
	rig := newTestRig(t, nil)
	rig.gh.addRun("in_progress", 1000,
		ghJob{ID: 101, RunID: 1000, Status: "in_progress", Labels: []string{"fly-linux"}},
		ghJob{ID: 102, RunID: 1000, Status: "in_progress", Labels: []string{"fly-linux"}},
	)
	rig.fly.addMachine(managed("m-101", "stopped", "101", "1000", testNow.Add(-10*time.Minute)))
	rig.fly.addMachine(managed("m-102", "started", "102", "1000", testNow.Add(-10*time.Minute)))

	summary := rig.poll(t)
	if got := rig.fly.deleted(); strings.Join(got, ",") != "m-101" {
		t.Fatalf("deleted = %v, want m-101", got)
	}
	if summary.Destroyed != 1 || summary.Live != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if !strings.Contains(rig.logs.String(), "reason=stopped") {
		t.Fatalf("no stopped log:\n%s", rig.logs.String())
	}
}

func TestReconcileNeverTouchesUnmanagedMachines(t *testing.T) {
	rig := newTestRig(t, func(c *config) { c.MaxMachines = 1 })
	rig.gh.addRun("queued", 1100, queued(111, 1100, "fly-linux"))
	rig.fly.addMachine(flyMachine{ID: "m-foreign-stopped", Name: "builder", State: "stopped", CreatedAt: testNow.Add(-48 * time.Hour)})
	rig.fly.addMachine(flyMachine{ID: "m-foreign-meta", Name: "other", State: "stopped", CreatedAt: testNow.Add(-48 * time.Hour), Metadata: map[string]string{"role": "cache"}})

	summary := rig.poll(t)
	if got := rig.fly.deleted(); len(got) != 0 {
		t.Fatalf("deleted unmanaged machines: %v", got)
	}
	if summary.Created != 1 {
		t.Fatalf("unmanaged machines counted against the ceiling: %+v", summary)
	}
}

func actualAssignmentMachine(job, run int64, age time.Duration) machine {
	return machine{ID: "machine-" + strconv.FormatInt(job, 10), Name: runnerName(job), State: "started", CreatedAt: testNow.Add(-age), Config: machineConfig{Metadata: map[string]string{metadataJobID: strconv.FormatInt(job, 10), metadataRunID: strconv.FormatInt(run, 10)}}}
}

func actualAssignmentReconciler(logs *bytes.Buffer) *reconciler {
	return &reconciler{cfg: config{OrphanTTL: 3 * time.Hour}, now: func() time.Time { return testNow }, log: slog.New(slog.NewTextHandler(logs, nil))}
}

func actualAssignmentJobs(t *testing.T) []workflowJob {
	t.Helper()
	body := []byte(`{"total_count":2,"jobs":[{"id":81,"run_id":800,"status":"completed","conclusion":"success","runner_id":7082,"runner_name":"fly-82","labels":["self-hosted","fly-linux"],"started_at":"2026-03-01T11:40:00Z","completed_at":"2026-03-01T11:50:00Z"},{"id":82,"run_id":800,"status":"in_progress","conclusion":null,"runner_id":7081,"runner_name":"fly-81","labels":["self-hosted","fly-linux"],"started_at":"2026-03-01T11:40:00Z","completed_at":null}]}`)
	jobs, err := decodeWorkflowJobs(body, 800)
	if err != nil {
		t.Fatal(err)
	}
	return jobs
}

func TestReconcileActualAssignmentCrossed(t *testing.T) {
	jobs := actualAssignmentJobs(t)
	idx := &jobIndex{}
	idx.add(800, jobs)
	rec := actualAssignmentReconciler(&bytes.Buffer{})
	for _, age := range []time.Duration{time.Hour, 48 * time.Hour} {
		for poll := 0; poll < 3; poll++ {
			for _, tc := range []struct {
				job  int64
				want string
			}{{81, ""}, {82, "finished"}} {
				m := actualAssignmentMachine(tc.job, 800, age)
				if got := rec.destroyReason(context.Background(), m, tc.job, 800, idx); got != tc.want {
					t.Fatalf("poll %d age %s machine %s: %q want %q", poll, age, m.Name, got, tc.want)
				}
			}
		}
	}
	if jobs[0].RunnerID != 7082 || jobs[0].RunnerName != "fly-82" || jobs[0].StartedAt == nil {
		t.Fatal("documented assignment fields lost")
	}
}

func TestReconcileActualAssignmentDifferentRuns(t *testing.T) {
	jobs := actualAssignmentJobs(t)
	jobs[1].RunID = 900
	for _, reverse := range []bool{false, true} {
		idx := &jobIndex{}
		if reverse {
			idx.add(900, jobs[1:])
			idx.add(800, jobs[:1])
		} else {
			idx.add(800, jobs[:1])
			idx.add(900, jobs[1:])
		}
		rec := actualAssignmentReconciler(&bytes.Buffer{})
		for _, tc := range []struct {
			job, run int64
			want     string
		}{{81, 800, ""}, {82, 900, "finished"}} {
			m := actualAssignmentMachine(tc.job, tc.run, time.Hour)
			if got := rec.destroyReason(context.Background(), m, tc.job, tc.run, idx); got != tc.want {
				t.Fatalf("reverse %v machine %s: %q", reverse, m.Name, got)
			}
		}
	}
}

func TestReconcileActualAssignmentUnknown(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*jobIndex)
	}{
		{"absent", func(idx *jobIndex) { idx.add(800, nil) }},
		{"null", func(idx *jobIndex) {
			jobs, e := decodeWorkflowJobs([]byte(`{"jobs":[{"id":81,"run_id":800,"status":"completed","runner_id":null,"runner_name":null,"completed_at":"2026-03-01T11:50:00Z"}]}`), 800)
			if e != nil {
				t.Fatal(e)
			}
			idx.add(800, jobs)
		}},
		{"zero_runner", func(idx *jobIndex) { j := actualAssignmentJobs(t)[0]; j.RunnerID = 0; idx.add(800, []workflowJob{j}) }},
		{"missing_name", func(idx *jobIndex) {
			j := actualAssignmentJobs(t)[0]
			j.RunnerName = ""
			idx.add(800, []workflowJob{j})
		}},
		{"incomplete_inventory", func(idx *jobIndex) { idx.add(800, actualAssignmentJobs(t)); idx.incomplete = true }},
		{"multiple_jobs", func(idx *jobIndex) {
			jobs := actualAssignmentJobs(t)
			jobs[1].RunnerName = "fly-82"
			jobs[1].RunnerID = 7082
			idx.add(800, jobs)
		}},
		{"runner_id_conflict", func(idx *jobIndex) { jobs := actualAssignmentJobs(t); jobs[1].RunnerID = 7082; idx.add(800, jobs) }},
		{"assignment_changed", func(idx *jobIndex) {
			jobs := actualAssignmentJobs(t)
			idx.add(800, jobs)
			jobs[0].RunnerName = "fly-other"
			idx.add(800, jobs[:1])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := &jobIndex{}
			tc.alter(idx)
			m := actualAssignmentMachine(82, 800, time.Hour)
			rec := actualAssignmentReconciler(&bytes.Buffer{})
			if _, known := idx.assignment(m); known {
				t.Fatal("uncertain assignment admitted")
			}
			if got := rec.destroyReason(context.Background(), m, 82, 800, idx); got != "" {
				t.Fatalf("unknown assignment destroyed: %s", got)
			}
		})
	}
}

func TestReconcileActualAssignmentBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		age, timeSinceCompletion time.Duration
		state                    string
		assigned, missingTime    bool
		want                     string
	}{
		{"finished", time.Hour, 6 * time.Minute, "started", true, false, "finished"},
		{"grace_exact", time.Hour, 5 * time.Minute, "started", true, false, ""},
		{"grace_recent", time.Hour, time.Minute, "started", true, false, ""},
		{"future_completion", time.Hour, -time.Minute, "started", true, false, ""},
		{"stale_previous_machine", time.Hour, 2 * time.Hour, "started", true, false, ""},
		{"missing_completion", 48 * time.Hour, 0, "started", true, true, ""},
		{"unknown_before_ttl", 2 * time.Hour, 0, "started", false, false, ""},
		{"unknown_exact_ttl", 3 * time.Hour, 0, "started", false, false, ""},
		{"unknown_after_ttl", 3*time.Hour + time.Second, 0, "started", false, false, "orphaned"},
		{"stopped", time.Minute, 0, "stopped", false, false, "stopped"},
		{"already_destroyed", 48 * time.Hour, 6 * time.Minute, "destroyed", true, false, ""},
		{"already_destroying", 48 * time.Hour, 6 * time.Minute, "destroying", true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := &jobIndex{}
			if tc.assigned {
				j := actualAssignmentJobs(t)[0]
				when := testNow.Add(-tc.timeSinceCompletion)
				j.CompletedAt = &when
				if tc.missingTime {
					j.CompletedAt = nil
				}
				idx.add(800, []workflowJob{j})
			}
			m := actualAssignmentMachine(82, 800, tc.age)
			m.State = tc.state
			rec := actualAssignmentReconciler(&bytes.Buffer{})
			for poll := 0; poll < 2; poll++ {
				if got := rec.destroyReason(context.Background(), m, 82, 800, idx); got != tc.want {
					t.Fatalf("poll %d got %q want %q", poll, got, tc.want)
				}
			}
		})
	}
}

func TestReconcileActualAssignmentActiveProtection(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		idx := &jobIndex{}
		idx.add(800, actualAssignmentJobs(t))
		idx.incomplete = incomplete
		m := actualAssignmentMachine(81, 800, 48*time.Hour)
		if got := actualAssignmentReconciler(&bytes.Buffer{}).destroyReason(context.Background(), m, 81, 800, idx); got != "" {
			t.Fatalf("active assignment removed with incomplete=%v: %s", incomplete, got)
		}
	}
}

func TestReconcileActualAssignmentUnmanaged(t *testing.T) {
	for _, tc := range []struct {
		name  string
		alter func(*machine)
	}{
		{"no_metadata", func(m *machine) { m.Config.Metadata = nil }},
		{"missing_job", func(m *machine) { delete(m.Config.Metadata, metadataJobID) }},
		{"missing_run", func(m *machine) { delete(m.Config.Metadata, metadataRunID) }},
		{"invalid_job", func(m *machine) { m.Config.Metadata[metadataJobID] = "bad" }},
		{"noncanonical_job", func(m *machine) { m.Config.Metadata[metadataJobID] = "082" }},
		{"invalid_run", func(m *machine) { m.Config.Metadata[metadataRunID] = "-800" }},
		{"wrong_name", func(m *machine) { m.Name = "owner-machine" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := &jobIndex{}
			idx.add(800, actualAssignmentJobs(t))
			rec := actualAssignmentReconciler(&bytes.Buffer{})
			for _, state := range []string{"started", "stopped"} {
				m := actualAssignmentMachine(82, 800, 48*time.Hour)
				m.State = state
				tc.alter(&m)
				if _, _, managed := machineJob(m); managed {
					t.Fatal("unmanaged machine admitted")
				}
				if got := rec.destroyReason(context.Background(), m, 82, 800, idx); got != "" {
					t.Fatalf("unmanaged machine removed: %s", got)
				}
			}
		})
	}
}

func TestReconcileActualAssignmentDecoder(t *testing.T) {
	for _, body := range []string{`{}`, `{"jobs":null}`, `{"jobs":[{"id":1,"run_id":801}]}`, `{"jobs":[{"id":0,"run_id":800}]}`, `{"jobs":[{"id":1,"run_id":800},{"id":1,"run_id":800}]}`, `{"jobs":[{"id":1,"run_id":800,"runner_id":"1"}]}`, `{"jobs":[`} {
		if _, err := decodeWorkflowJobs([]byte(body), 800); err == nil {
			t.Fatalf("invalid inventory accepted: %s", body)
		}
	}
	jobs, err := decodeWorkflowJobs([]byte(`{"total_count":1,"jobs":[{"id":399444496,"run_id":29679449,"status":"completed","conclusion":"success","runner_id":1,"runner_name":"my runner","started_at":"2020-01-20T17:42:40Z","completed_at":"2020-01-20T17:44:39Z"}]}`), 29679449)
	if err != nil || len(jobs) != 1 || jobs[0].RunnerID != 1 || jobs[0].RunnerName != "my runner" {
		t.Fatalf("documented job response lost assignment: %+v %v", jobs, err)
	}
}

func TestReconcileActualAssignmentEvidence(t *testing.T) {
	logs := &bytes.Buffer{}
	rec := actualAssignmentReconciler(logs)
	idx := &jobIndex{}
	idx.add(800, actualAssignmentJobs(t))
	m := actualAssignmentMachine(82, 900, time.Hour)
	rec.cleanupLog("machine destroyed", m, "finished", 82, 900, idx, nil)
	rec.cleanupLog("destroy machine failed", m, "finished", 82, 900, idx, errors.New("untrusted-provider-body"))
	for _, want := range []string{"trigger_job_id=82", "trigger_run_id=900", "actual_job_id=81", "actual_run_id=800", "runner_id=7082", "runner_name=fly-82", "actual_status=completed"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("missing evidence %s: %s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "untrusted-provider-body") {
		t.Fatal("provider body reached cleanup log")
	}
	logs.Reset()
	rec.cleanupLog("machine destroyed", m, "orphaned", 82, 900, &jobIndex{}, nil)
	if !strings.Contains(logs.String(), "assignment=unknown") || strings.Contains(logs.String(), "actual_job_id") {
		t.Fatal("unknown assignment misreported")
	}
}

func TestReconcileActualAssignmentDurableProtection(t *testing.T) {
	registry, req := qTestRegistry(t)
	if _, err := registry.submit(req); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.advance(req.LogicalID, "prepared", "dispatch_intent", 0, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.advance(req.LogicalID, "dispatch_intent", "run_bound", 800, 1, ""); err != nil {
		t.Fatal(err)
	}
	rec := actualAssignmentReconciler(&bytes.Buffer{})
	rec.qualification = registry
	for _, known := range []bool{false, true} {
		idx := &jobIndex{}
		if known {
			idx.add(800, actualAssignmentJobs(t))
		}
		m := actualAssignmentMachine(82, 900, 48*time.Hour)
		if got := rec.destroyReason(context.Background(), m, 82, 900, idx); got != "" {
			t.Fatalf("uncollected durable assignment removed known=%v: %s", known, got)
		}
	}
	reopened, err := newQualificationRegistry(registry.root)
	if err != nil {
		t.Fatal(err)
	}
	rec.qualification = reopened
	if got := rec.destroyReason(context.Background(), actualAssignmentMachine(82, 900, 48*time.Hour), 82, 900, &jobIndex{}); got != "" {
		t.Fatalf("durable protection lost after reopen: %s", got)
	}
}
