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
	cfg      config
	github   *githubClient
	fly      *flyClient
	log      *slog.Logger
	now      func() time.Time
	accepted map[string]bool
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
	jobID, _ = strconv.ParseInt(rawJob, 10, 64)
	runID, _ = strconv.ParseInt(m.Config.Metadata[metadataRunID], 10, 64)
	return jobID, runID, true
}

type jobIndex struct {
	byID  map[int64]workflowJob
	byRun map[int64][]workflowJob
}

func (idx *jobIndex) add(runID int64, jobs []workflowJob) {
	idx.byRun[runID] = jobs
	for _, job := range jobs {
		idx.byID[job.ID] = job
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

	live := make(map[int64]bool)
	for _, m := range machines {
		jobID, runID, managed := machineJob(m)
		if !managed || m.State == "destroyed" || m.State == "destroying" {
			continue
		}
		if reason := r.destroyReason(ctx, m, jobID, runID, idx); reason != "" {
			if err := r.fly.destroyMachine(ctx, m.ID); err != nil {
				r.log.Error("destroy machine failed", "machine_id", m.ID, "job_id", jobID, "reason", reason, "error", err)
			} else {
				r.log.Info("machine destroyed", "machine_id", m.ID, "job_id", jobID, "reason", reason)
				summary.Destroyed++
				continue
			}
		}
		live[jobID] = true
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
	if m.State == "stopped" {
		return "stopped"
	}
	now := r.now()
	if !m.CreatedAt.IsZero() && now.Sub(m.CreatedAt) > r.cfg.OrphanTTL {
		return "orphaned"
	}
	if jobID == 0 || runID == 0 {
		return ""
	}
	job, found := idx.byID[jobID]
	if !found {
		if _, fetched := idx.byRun[runID]; !fetched {
			jobs, err := r.github.listJobs(ctx, runID)
			if err != nil {
				r.log.Error("list jobs of machine run failed", "machine_id", m.ID, "job_id", jobID, "run_id", runID, "error", err)
				return ""
			}
			idx.add(runID, jobs)
			job, found = idx.byID[jobID]
		}
	}
	if !found || job.Status != statusDone || job.CompletedAt == nil {
		return ""
	}
	if now.Sub(*job.CompletedAt) > finishedGrace {
		return "finished"
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
