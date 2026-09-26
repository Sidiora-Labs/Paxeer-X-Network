package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
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
	summary, err := rec.reconcile(ctx)
	if err != nil {
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
