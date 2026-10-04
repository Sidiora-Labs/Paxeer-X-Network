package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "qualification-") {
		os.Exit(qCLI(os.Args[1:]))
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}

	httpClient := &http.Client{Timeout: 30 * time.Second}
	github, err := newGitHubClient(cfg, httpClient)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	fly, err := newFlyClient(cfg, httpClient)
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(2)
	}
	rec := newReconciler(cfg, github, fly, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()
	if cfg.QualificationRoot != "" {
		registry, err := newQualificationRegistry(cfg.QualificationRoot)
		if err == nil {
			err = qServe(ctx, registry)
		}
		if err != nil {
			logger.Error("qualification storage unavailable")
			os.Exit(2)
		}
		rec.qualification = registry
	}

	if cfg.Readiness != nil {
		rec.readiness = newReadinessState(*cfg.Readiness)
		if err := startReadiness(ctx, rec.readiness, stop); err != nil {
			logger.Error("private readiness admission failed")
			stop()
			os.Exit(2)
		}
	}

	logger.Info("controller started",
		"repository", cfg.Owner+"/"+cfg.Repo,
		"runner_app", cfg.RunnerApp,
		"labels", cfg.Labels,
		"region", cfg.Region,
		"max_machines", cfg.MaxMachines,
		"poll_interval", cfg.PollInterval.String(),
	)
	run(ctx, rec, cfg.PollInterval, logger)
	logger.Info("controller stopped")
}

func run(ctx context.Context, rec *reconciler, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		poll(ctx, rec, logger)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func poll(ctx context.Context, rec *reconciler, logger *slog.Logger) {
	started := time.Now()
	var observation *readinessPass
	if rec.readiness != nil {
		observation = rec.readiness.begin()
		rec.observation = observation
		defer func() { observation.finish(ctx.Err()); rec.observation = nil }()
	}
	if rec.qualification != nil {
		if observation != nil {
			observation.condition("qualification_storage", qMounted(rec.qualification.root))
		}
		qualificationErr := rec.github.qTick(ctx, rec.qualification)
		observation.observe("qualification", qualificationErr)
		if observation != nil {
			records, inventoryErr := readinessQualificationSnapshot(rec.qualification)
			observation.observe("qualification_inventory", inventoryErr)
			if inventoryErr == nil {
				observation.qualificationRecords(records)
			}
		}
		if err := qualificationErr; err != nil {
			logger.Error("qualification reconciliation refused", "error", err)
		}
	}
	summary, err := rec.reconcile(ctx)
	if err != nil {
		observation.fail("reconcile_failed")
		if ctx.Err() != nil {
			return
		}
		logger.Error("poll failed", "error", err, "duration_ms", time.Since(started).Milliseconds())
		return
	}
	logger.Info("poll completed",
		"selected", summary.Selected,
		"created", summary.Created,
		"destroyed", summary.Destroyed,
		"live", summary.Live,
		"duration_ms", time.Since(started).Milliseconds(),
	)
}
