// Command perps-explorer serves the chain explorer REST routes of the perps
// data schema from a read-only Postgres session. It exits non-zero on a
// configuration or database failure.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/explorer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/ratelimit"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

// Bounds of the HTTP server and the per-client request budget.
const (
	clientRequests    = 120
	clientWindow      = time.Minute
	maxInFlight       = 10000
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 5 * time.Second
	readOnlyParam     = "default_transaction_read_only"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("perps-explorer stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(config.Explorer)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger.Info("perps-explorer starting", slog.Any("config", *cfg))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dsn, err := readOnly(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	db, err := store.Open(dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	metrics, err := telemetry.New(config.Explorer)
	if err != nil {
		return err
	}
	srv, err := explorer.NewServer(db, explorer.Options{
		LinkBase: cfg.ExplorerTxBase,
		Requests: clientRequests,
		Window:   clientWindow,
		Metrics:  metrics,
	})
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	httpx.RegisterHealth(mux, httpx.HealthDeps{Postgres: db.PingContext})
	mux.Handle("GET /metrics", metrics)
	mux.Handle("/api/", srv)
	listener, err := net.Listen("tcp", ":"+strconv.FormatUint(uint64(cfg.Port), 10))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Handler: ratelimit.NewLimiter(maxInFlight).Middleware(mux), ReadHeaderTimeout: readHeaderTimeout}
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

// readOnly makes every session of the DSN read-only through the
// default_transaction_read_only run-time parameter, in URL or key=value form.
func readOnly(dsn string) (string, error) {
	u, err := url.Parse(dsn)
	if err == nil && (u.Scheme == "postgres" || u.Scheme == "postgresql") {
		q := u.Query()
		q.Set(readOnlyParam, "on")
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	if err == nil && u.Scheme != "" {
		return "", errors.New("DATABASE_URL needs postgres or postgresql")
	}
	return dsn + " " + readOnlyParam + "=on", nil
}
