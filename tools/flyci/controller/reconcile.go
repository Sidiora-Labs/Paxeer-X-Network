package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	metadataJobID  = "flyci_job_id"
	metadataRunID  = "flyci_run_id"
	finishedGrace  = 5 * time.Minute
	statusQueued   = "queued"
	statusProgress = "in_progress"
	statusDone     = "completed"
)

type pollSummary struct {
	Selected  int
	Created   int
	Destroyed int
	Live      int
}

type reconciler struct {
	qualification *qualificationRegistry
	cfg           config
	github        *githubClient
	fly           *flyClient
	log           *slog.Logger
	now           func() time.Time
	accepted      map[string]bool
}

func newReconciler(cfg config, github *githubClient, fly *flyClient, log *slog.Logger) *reconciler {
	return &reconciler{
		cfg:      cfg,
		github:   github,
		fly:      fly,
		log:      log,
		now:      time.Now,
		accepted: acceptedLabels(cfg.Labels),
	}
}

func acceptedLabels(configured []string) map[string]bool {
	accepted := map[string]bool{"self-hosted": true, "linux": true, "x64": true}
	for _, label := range configured {
		accepted[strings.ToLower(strings.TrimSpace(label))] = true
	}
	return accepted
}

func selectJobs(jobs []workflowJob, accepted map[string]bool) []workflowJob {
	var selected []workflowJob
	for _, job := range jobs {
		if job.Status != statusQueued || len(job.Labels) == 0 {
			continue
		}
		fits := true
		for _, label := range job.Labels {
			if !accepted[strings.ToLower(strings.TrimSpace(label))] {
				fits = false
				break
			}
		}
		if fits {
			selected = append(selected, job)
		}
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].ID < selected[j].ID })
	return selected
}

func runnerName(jobID int64) string {
	return "fly-" + strconv.FormatInt(jobID, 10)
}

func machineJob(m machine) (jobID, runID int64, managed bool) {
	rawJob, ok := m.Config.Metadata[metadataJobID]
	if !ok {
		return 0, 0, false
	}
	rawRun := m.Config.Metadata[metadataRunID]
	jobID, jobErr := strconv.ParseInt(rawJob, 10, 64)
	runID, runErr := strconv.ParseInt(rawRun, 10, 64)
	if jobErr != nil || runErr != nil || jobID <= 0 || runID <= 0 ||
		strconv.FormatInt(jobID, 10) != rawJob || strconv.FormatInt(runID, 10) != rawRun || m.Name != runnerName(jobID) {
		return 0, 0, false
	}
	return jobID, runID, true
}

type jobIndex struct {
	byID       map[int64]workflowJob
	byRun      map[int64][]workflowJob
	byRunner   map[string]map[int64]workflowJob
	conflicts  map[string]bool
	incomplete bool
}

func sameAssignment(a, b workflowJob) bool {
	if a.ID != b.ID || a.RunID != b.RunID || a.RunnerID != b.RunnerID || a.RunnerName != b.RunnerName || a.Status != b.Status || a.Conclusion != b.Conclusion {
		return false
	}
	if (a.CompletedAt == nil) != (b.CompletedAt == nil) {
		return false
	}
	return a.CompletedAt == nil || a.CompletedAt.Equal(*b.CompletedAt)
}

func (idx *jobIndex) add(runID int64, jobs []workflowJob) {
	if idx.byRun == nil {
		idx.byRun = make(map[int64][]workflowJob)
	}
	if idx.byID == nil {
		idx.byID = make(map[int64]workflowJob)
	}
	if idx.byRunner == nil {
		idx.byRunner = make(map[string]map[int64]workflowJob)
	}
	if idx.conflicts == nil {
		idx.conflicts = make(map[string]bool)
	}
	idx.byRun[runID] = jobs
	for _, job := range jobs {
		if old, ok := idx.byID[job.ID]; ok && !sameAssignment(old, job) {
			idx.conflicts[old.RunnerName] = true
			idx.conflicts[job.RunnerName] = true
		}
		if job.ID <= 0 || job.RunID != runID {
			idx.incomplete = true
			continue
		}
		for _, old := range idx.byID {
			if job.RunnerID > 0 && old.RunnerID == job.RunnerID && old.RunnerName != job.RunnerName {
				idx.conflicts[old.RunnerName] = true
				idx.conflicts[job.RunnerName] = true
			}
		}
		idx.byID[job.ID] = job
		if job.RunnerName == "" {
			continue
		}
		if idx.byRunner[job.RunnerName] == nil {
			idx.byRunner[job.RunnerName] = make(map[int64]workflowJob)
		}
		idx.byRunner[job.RunnerName][job.ID] = job
	}
}

func (idx *jobIndex) assignment(m machine) (workflowJob, bool) {
	if idx.incomplete || idx.conflicts[m.Name] {
		return workflowJob{}, false
	}
	jobs := idx.byRunner[m.Name]
	if len(jobs) != 1 {
		return workflowJob{}, false
	}
	for _, job := range jobs {
		if job.RunnerID <= 0 || job.RunnerName != m.Name || job.ID <= 0 || job.RunID <= 0 {
			return workflowJob{}, false
		}
		return job, true
	}
	return workflowJob{}, false
}

func (r *reconciler) machineRunHistory(ctx context.Context, machines []machine, idx *jobIndex) {
	for _, m := range machines {
		_, runID, managed := machineJob(m)
		if !managed || m.State == "destroyed" || m.State == "destroying" {
			continue
		}
		if _, present := idx.byRun[runID]; present {
			continue
		}
		jobs, err := r.github.listJobs(ctx, runID)
		if err != nil {
			idx.incomplete = true
			idx.byRun[runID] = nil
			r.log.Error("machine job history unavailable", "machine_id", m.ID, "trigger_run_id", runID)
			continue
		}
		idx.add(runID, jobs)
	}
}

func (r *reconciler) cleanupLog(message string, m machine, reason string, jobID, runID int64, idx *jobIndex, failure error) {
	attributes := []any{"machine_id", m.ID, "trigger_job_id", jobID, "trigger_run_id", runID, "reason", reason}
	if actual, known := idx.assignment(m); known {
		attributes = append(attributes, "actual_job_id", actual.ID, "actual_run_id", actual.RunID, "runner_id", actual.RunnerID, "runner_name", actual.RunnerName, "actual_status", actual.Status)
	} else {
		attributes = append(attributes, "assignment", "unknown")
	}
	if failure != nil {
		attributes = append(attributes, "error_type", fmt.Sprintf("%T", failure))
		r.log.Error(message, attributes...)
	} else {
		r.log.Info(message, attributes...)
	}
}

func (r *reconciler) reconcile(ctx context.Context) (pollSummary, error) {
	defer r.github.endPoll()
	var summary pollSummary

	idx := &jobIndex{byID: make(map[int64]workflowJob), byRun: make(map[int64][]workflowJob)}
	var active []workflowJob
	seenRuns := make(map[int64]bool)
	for _, status := range []string{statusQueued, statusProgress} {
		runs, err := r.github.listRuns(ctx, status)
		if err != nil {
			if ctx.Err() != nil {
				return summary, ctx.Err()
			}
			idx.incomplete = true
			r.log.Error("list workflow runs failed", "status", status, "error", err)
			continue
		}
		for _, run := range runs {
			if seenRuns[run.ID] {
				continue
			}
			seenRuns[run.ID] = true
			jobs, err := r.github.listJobs(ctx, run.ID)
			if err != nil {
				if ctx.Err() != nil {
					return summary, ctx.Err()
				}
				idx.incomplete = true
				r.log.Error("list jobs failed", "run_id", run.ID, "error", err)
				continue
			}
			idx.add(run.ID, jobs)
			active = append(active, jobs...)
		}
	}

	machines, err := r.fly.listMachines(ctx)
	if err != nil {
		return summary, fmt.Errorf("list machines: %w", err)
	}

	r.machineRunHistory(ctx, machines, idx)

	live := make(map[int64]bool)
	for _, m := range machines {
		jobID, runID, managed := machineJob(m)
		if !managed || m.State == "destroyed" || m.State == "destroying" {
			continue
		}
		if reason := r.destroyReason(ctx, m, jobID, runID, idx); reason != "" {
			if err := r.fly.destroyMachine(ctx, m.ID); err != nil {
				r.cleanupLog("destroy machine failed", m, reason, jobID, runID, idx, err)
			} else {
				r.cleanupLog("machine destroyed", m, reason, jobID, runID, idx, nil)
				summary.Destroyed++
				continue
			}
		}
		live[jobID] = true
		if actual, known := idx.assignment(m); known {
			live[actual.ID] = true
		}
		summary.Live++
	}

	selected := selectJobs(active, r.accepted)
	summary.Selected = len(selected)
	for i, job := range selected {
		if ctx.Err() != nil {
			return summary, ctx.Err()
		}
		if live[job.ID] {
			continue
		}
		if summary.Live >= r.cfg.MaxMachines {
			r.log.Warn("machine ceiling reached", "max_machines", r.cfg.MaxMachines, "waiting_jobs", len(selected)-i, "job_id", job.ID)
			break
		}
		name := runnerName(job.ID)
		jit, err := r.github.generateJITConfig(ctx, name, r.cfg.Labels)
		if err != nil {
			r.log.Error("mint runner configuration failed", "job_id", job.ID, "run_id", job.RunID, "error", err)
			continue
		}
		created, err := r.fly.createMachine(ctx, r.machineRequest(job, name, jit.EncodedJITConfig))
		if err != nil {
			r.log.Error("create machine failed", "job_id", job.ID, "run_id", job.RunID, "runner_id", jit.Runner.ID, "error", err)
			continue
		}
		live[job.ID] = true
		summary.Live++
		summary.Created++
		r.log.Info("machine created", "job_id", job.ID, "run_id", job.RunID, "machine_id", created.ID, "runner", name)
	}
	return summary, nil
}

func (r *reconciler) destroyReason(ctx context.Context, m machine, jobID, runID int64, idx *jobIndex) string {
	ownerJob, ownerRun, managed := machineJob(m)
	if !managed || ownerJob != jobID || ownerRun != runID || m.State == "destroyed" || m.State == "destroying" {
		return ""
	}
	if m.State == "stopped" {
		return "stopped"
	}
	if r.qualification != nil && r.qualification.protectsAssigned(m, idx) {
		return ""
	}
	for _, observed := range idx.byRunner[m.Name] {
		if observed.RunnerID > 0 && observed.Status != statusDone {
			return ""
		}
	}
	actual, known := idx.assignment(m)
	now := r.now()
	if known {
		if actual.Status != statusDone {
			return ""
		}
		if actual.CompletedAt == nil || m.CreatedAt.IsZero() || actual.CompletedAt.Before(m.CreatedAt) || actual.CompletedAt.After(now) {
			return ""
		}
		if now.Sub(*actual.CompletedAt) > finishedGrace {
			return "finished"
		}
		return ""
	}
	if !m.CreatedAt.IsZero() && now.Sub(m.CreatedAt) > r.cfg.OrphanTTL {
		return "orphaned"
	}
	return ""
}

func (r *reconciler) machineRequest(job workflowJob, name, encodedJIT string) createMachineRequest {
	return createMachineRequest{
		Name:   name,
		Region: r.cfg.Region,
		Config: machineConfig{
			Image: r.cfg.RunnerImage,
			Env: map[string]string{
				"RUNNER_JITCONFIG":    encodedJIT,
				"RUNNER_IDLE_TIMEOUT": strconv.Itoa(r.cfg.IdleTimeout),
			},
			Guest: machineGuest{
				CPUKind:  r.cfg.CPUKind,
				CPUs:     r.cfg.CPUs,
				MemoryMB: r.cfg.MemoryMB,
			},
			AutoDestroy: true,
			Restart:     machineRestart{Policy: "no"},
			Metadata: map[string]string{
				metadataJobID: strconv.FormatInt(job.ID, 10),
				metadataRunID: strconv.FormatInt(job.RunID, 10),
			},
		},
	}
}
