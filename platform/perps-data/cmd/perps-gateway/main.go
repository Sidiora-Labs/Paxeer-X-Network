// Command perps-gateway serves the perps REST routes, the shared WebSocket,
// the spot user socket and GraphQL from schema perps and the PERPS_V1
// JetStream stream. It exits non-zero on a configuration, database or stream
// failure and when the topic hub stops.
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

	"github.com/nats-io/nats.go"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/gateway"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

// Bounds of the HTTP server and the NATS connection.
const (
	shutdownTimeout    = 5 * time.Second
	readHeaderTimeout  = 5 * time.Second
	natsConnectionName = "perps-gateway"
	natsReconnectWait  = time.Second
	natsMaxReconnects  = -1
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("perps-gateway stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(config.Gateway)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger.Info("perps-gateway starting", slog.Any("config", *cfg))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	nc, err := nats.Connect(cfg.NATSURL, nats.Name(natsConnectionName), nats.MaxReconnects(natsMaxReconnects), nats.ReconnectWait(natsReconnectWait))
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()

	node, err := source.NewRPC(ctx, source.RPCOptions{URLs: cfg.RPCURLs, Logger: logger})
	if err != nil {
		return fmt.Errorf("rpc: %w", err)
	}
	defer node.Close()

	metrics, err := telemetry.New(config.Gateway)
	if err != nil {
		return err
	}
	srv, err := gateway.NewServer(ctx, gateway.Options{
		DB:         db,
		NATS:       nc,
		Chain:      cfg.ChainID,
		AuthSecret: cfg.AuthSecret,
		Node:       node,
		Metrics:    metrics,
		Logger:     logger,
	})
	if err != nil {
		return fmt.Errorf("gateway: %w", err)
	}

	listener, err := net.Listen("tcp", ":"+strconv.FormatUint(uint64(cfg.Port), 10))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: readHeaderTimeout}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	runErr := make(chan error, 1)
	go func() { runErr <- srv.Run(ctx) }()

	var result error
	select {
	case err := <-runErr:
		if err != nil && !errors.Is(err, context.Canceled) {
			result = fmt.Errorf("gateway run: %w", err)
		}
	case err := <-serveErr:
		result = fmt.Errorf("http server: %w", err)
		stop()
		<-runErr
	}
	stop()
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil && !errors.Is(err, http.ErrServerClosed) {
		result = errors.Join(result, fmt.Errorf("http shutdown: %w", err))
	}
	return result
}
