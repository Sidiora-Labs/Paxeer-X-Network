// Command perps-portal renders the markets, account, leaderboard and venue
// statistics pages of the Paxeer X Network perps venue on the server from the
// gateway REST and GraphQL. It is ready while the gateway is ready and exits
// non-zero on a configuration or HTTP server failure.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/portal"
	"github.com/Sidiora-Labs/Paxeer-X-Network/services/platform/perps-data/internal/telemetry"
)

// Bounds of the gateway client and the HTTP server.
const (
	gatewayTimeout    = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 5 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("perps-portal stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(config.Portal)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger.Info("perps-portal starting", slog.Any("config", *cfg))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	metrics, err := telemetry.New(config.Portal)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: gatewayTimeout}
	pages, err := portal.NewPages(cfg.GatewayURL, cfg.ExplorerTxBase, client)
	if err != nil {
		return fmt.Errorf("pages: %w", err)
	}
	srv, err := portal.NewServer(pages, cfg.GatewayURL, metrics, client)
	if err != nil {
		return fmt.Errorf("server: %w", err)
	}

	listener, err := net.Listen("tcp", ":"+strconv.FormatUint(uint64(cfg.Port), 10))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{
		Handler:           srv,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	var result error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		result = fmt.Errorf("http server: %w", err)
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		result = errors.Join(result, fmt.Errorf("http shutdown: %w", err))
	}
	return result
}
