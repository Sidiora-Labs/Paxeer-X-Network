// Command perps-loadtest runs the full load profile of contracts/data-services.md
// section 22 against the data socket of the gateway named by PERPS_GATEWAY_URL,
// prints the report as JSON and exits non-zero when a rule of the profile is
// violated.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/loadtest"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("perps-loadtest failed", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(config.Loadtest)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	socketURL, err := loadtest.SocketURL(cfg.GatewayURL)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	profile := loadtest.FullProfile()
	logger.Info("perps-loadtest starting", slog.Any("config", *cfg), slog.Int("sockets", profile.Sockets))
	report, runErr := loadtest.Run(ctx, socketURL, profile)
	if report != nil {
		out, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return errors.Join(runErr, fmt.Errorf("report: %w", err))
		}
		if _, err := fmt.Fprintln(os.Stdout, string(out)); err != nil {
			return errors.Join(runErr, fmt.Errorf("report: %w", err))
		}
	}
	if runErr != nil {
		return runErr
	}
	return report.Err()
}
