package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"sort"
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
		ghJob{ID: 81, RunID: 800, Status: "completed", Conclusion: "success", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-6 * time.Minute)},
		ghJob{ID: 82, RunID: 800, Status: "completed", Conclusion: "failure", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-2 * time.Minute)},
	)
	rig.gh.addRun("in_progress", 801,
		ghJob{ID: 83, RunID: 801, Status: "completed", Conclusion: "success", Labels: []string{"fly-linux"}, CompletedAt: testNow.Add(-10 * time.Minute)},
		ghJob{ID: 84, RunID: 801, Status: "in_progress", Labels: []string{"fly-linux"}},
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
