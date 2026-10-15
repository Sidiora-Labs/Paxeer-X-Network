// Command perps-indexer follows the Paxeer X Network head, writes the perps
// facts and projections to Postgres and publishes them to the PERPS_V1
// JetStream stream. It exits non-zero on a configuration, migration or stream
// failure and when the writer lease is lost.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/nats-io/nats.go"

	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/config"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/decode"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/httpx"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/indexer"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/markets"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/publish"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/source"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/store"
	"github.com/Sidiora-Labs/Paxeer-X-Network/platform/perps-data/internal/telemetry"
)

// Bounds of the outbox relay, the HTTP server and the NATS connection.
const (
	publishBatch        = 100
	publishMaxAttempts  = 20
	publishLease        = 30 * time.Second
	publishTimeout      = 5 * time.Second
	publishRetryDelay   = time.Second
	publishIdleInterval = 50 * time.Millisecond
	shutdownTimeout     = 5 * time.Second
	readHeaderTimeout   = 5 * time.Second
	moduleName          = "perps"
	holderPrefix        = "perps-indexer-"
	holderEntropyBytes  = 8
	natsConnectionName  = "perps-indexer"
	natsReconnectWait   = time.Second
	natsMaxReconnects   = -1
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("perps-indexer stopped", slog.Any("err", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(config.Indexer)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	logger.Info("perps-indexer starting", slog.Any("config", *cfg))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(ctx, db); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	nc, err := nats.Connect(cfg.NATSURL, nats.Name(natsConnectionName), nats.MaxReconnects(natsMaxReconnects), nats.ReconnectWait(natsReconnectWait))
	if err != nil {
		return fmt.Errorf("connect nats: %w", err)
	}
	defer nc.Close()
	if err := publish.EnsureStream(ctx, nc, cfg.ChainID); err != nil {
		return fmt.Errorf("ensure stream: %w", err)
	}

	holder, err := newHolder()
	if err != nil {
		return err
	}
	metrics, err := telemetry.New(config.Indexer)
	if err != nil {
		return err
	}
	registry, err := decode.NewRegistry(metrics, moduleAddress(moduleName))
	if err != nil {
		return err
	}
	rpc, err := source.NewRPC(ctx, source.RPCOptions{URLs: cfg.RPCURLs, Logger: logger})
	if err != nil {
		return fmt.Errorf("rpc source: %w", err)
	}
	defer rpc.Close()
	comet, err := source.NewComet(cfg.CometURLs)
	if err != nil {
		return fmt.Errorf("comet source: %w", err)
	}
	follower, err := source.NewFollower(source.FollowerConfig{DB: db, RPC: rpc, Comet: comet, Chain: cfg.ChainID, Logger: logger})
	if err != nil {
		return fmt.Errorf("follower: %w", err)
	}
	publisher, err := publish.New(db, nc, store.Policy(cfg.ChainID), publish.Config{
		Owner:          holder,
		Batch:          publishBatch,
		MaxAttempts:    publishMaxAttempts,
		LeaseDuration:  publishLease,
		PublishTimeout: publishTimeout,
		RetryDelay:     publishRetryDelay,
		IdleInterval:   publishIdleInterval,
		Logger:         logger,
	})
	if err != nil {
		return fmt.Errorf("publisher: %w", err)
	}
	ix, err := indexer.New(indexer.Deps{
		DB:          db,
		Follower:    follower,
		Registry:    registry,
		Publisher:   publisher,
		Metrics:     metrics,
		Holder:      holder,
		StartHeight: cfg.StartHeight,
		Logger:      logger,
	})
	if err != nil {
		return err
	}

	client, err := ethclient.DialContext(ctx, cfg.RPCURLs[0])
	if err != nil {
		return fmt.Errorf("dial rpc: %w", err)
	}
	defer client.Close()
	go func() {
		err := markets.Retry(ctx, db, client, func(err error, retryIn time.Duration) {
			logger.Warn("markets refresh failed", slog.Any("err", err), slog.Duration("retry_in", retryIn))
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("markets refresh stopped", slog.Any("err", err))
		}
	}()

	mux := http.NewServeMux()
	httpx.RegisterHealth(mux, httpx.HealthDeps{Postgres: db.PingContext, Custom: ix.Checks()})
	mux.Handle("GET /metrics", metrics)
	listener, err := net.Listen("tcp", ":"+strconv.FormatUint(uint64(cfg.Port), 10))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: readHeaderTimeout}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	runErr := make(chan error, 1)
	go func() { runErr <- ix.Run(ctx) }()

	var result error
	select {
	case err := <-runErr:
		if !errors.Is(err, context.Canceled) {
			result = err
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

// moduleAddress is authtypes.NewModuleAddress(name) as an EVM address, the
// module EVM address of contracts/address-table.md (module accounts): the
// first 20 bytes of the SHA-256 of the module name.
func moduleAddress(name string) common.Address {
	sum := sha256.Sum256([]byte(name))
	return common.BytesToAddress(sum[:common.AddressLength])
}

// newHolder names this process in the writer lease and the outbox leases.
func newHolder() (string, error) {
	b := make([]byte, holderEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("holder id: %w", err)
	}
	return holderPrefix + strconv.Itoa(os.Getpid()) + "-" + hex.EncodeToString(b), nil
}
